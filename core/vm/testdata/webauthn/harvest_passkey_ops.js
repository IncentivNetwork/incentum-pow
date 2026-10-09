#!/usr/bin/env node
/*
 * Harvests every historical passkey UserOperation from an Incentiv chain and reconstructs the byte
 * string the 0x111 precompile was handed for each one, so they can be replayed through a patched
 * parser offline. READ-ONLY: getLogs / getTransactionByHash / eth_call only. No writes, no signing.
 *
 * Reconstructed, not observed: the input is assembled from three places, in the layout the account
 * implementation uses. That is sound for everything except bytes a caller might append after y,
 * which the pre-fork parser and the signature both ignore — see the note at the assembly below and
 * the "Coverage limits" section of docs/webauthn/replay-validation.md, which has the trace
 * comparison that closes it.
 *
 * For each UserOperationEvent in a mined EntryPoint.handleOps (or handleAggregatedOps) transaction:
 *   - userOpHash comes from the event topic, so it is the exact `message` the account passed on;
 *   - a 65-byte signature is plain ECDSA whatever its first byte is, and is skipped;
 *     otherwise signature[0] == 0x01 selects the passkey path and signaturePayload = signature[3:];
 *   - publicKey.x/y are read from the account with eth_call.
 * The precompile input is then abi.encodePacked(message, signaturePayload, x, y) — see
 * BaseIncentivAccount._internalSignatureValidation.
 *
 * Env:
 *   RPC         JSON-RPC endpoint            (default https://rpc-fr-1.incentiv.io)
 *   ENTRYPOINT  EntryPoint address           (default 0x3eC61c5633BBD7Afa9144C6610930489736a72d4)
 *   FROM_BLOCK  first block                  (default 0)
 *   TO_BLOCK    last block                   (default latest)
 *   LOG_CHUNK   getLogs span per request     (default 50000)
 *   BATCH       JSON-RPC batch size          (default 100)
 *   PK_BLOCK    block tag for publicKey()    (default latest)
 *   OUT         output file                  (default ./passkey_ops.json)
 */
'use strict'
const fs = require('fs')
const { ethers } = require('ethers')
const { classifySignature } = require('./classify_signature.js')
const { chainBlockRangeProblem, scanStepProblem, statedBlockRange } = require('./block_range.js')

const RPC = process.env.RPC || 'https://rpc-fr-1.incentiv.io'
const EP = (process.env.ENTRYPOINT || '0x3eC61c5633BBD7Afa9144C6610930489736a72d4').toLowerCase()
const LOG_CHUNK = Number(process.env.LOG_CHUNK || 50000)
const BATCH = Number(process.env.BATCH || 100)
const PK_BLOCK = process.env.PK_BLOCK || 'latest'
const OUT = process.env.OUT || 'passkey_ops.json'

const EP_ABI = [
  'event UserOperationEvent(bytes32 indexed userOpHash, address indexed sender, address indexed paymaster, uint256 nonce, bool success, uint256 actualGasCost, uint256 actualGasUsed)',
  'function handleOps((address sender,uint256 nonce,bytes initCode,bytes callData,bytes32 accountGasLimits,uint256 preVerificationGas,bytes32 gasFees,bytes paymasterAndData,bytes signature)[] ops, address beneficiary)',
  'function handleAggregatedOps(((address sender,uint256 nonce,bytes initCode,bytes callData,bytes32 accountGasLimits,uint256 preVerificationGas,bytes32 gasFees,bytes paymasterAndData,bytes signature)[] userOps,address aggregator,bytes signature)[] opsPerAggregator, address beneficiary)',
]
// The EntryPoint at this address has carried both ERC-4337 revisions over the chain's life:
// v0.6 spells a UserOperation out field by field, v0.8 packs the gas fields. Both are tried,
// because the older revision is what the earlier client generations produced.
const EP_ABI_V06 = [
  'function handleOps((address sender,uint256 nonce,bytes initCode,bytes callData,uint256 callGasLimit,uint256 verificationGasLimit,uint256 preVerificationGas,uint256 maxFeePerGas,uint256 maxPriorityFeePerGas,bytes paymasterAndData,bytes signature)[] ops, address beneficiary)',
  'function handleAggregatedOps(((address sender,uint256 nonce,bytes initCode,bytes callData,uint256 callGasLimit,uint256 verificationGasLimit,uint256 preVerificationGas,uint256 maxFeePerGas,uint256 maxPriorityFeePerGas,bytes paymasterAndData,bytes signature)[] userOps,address aggregator,bytes signature)[] opsPerAggregator, address beneficiary)',
]
const iface = new ethers.utils.Interface(EP_ABI)
const ifaceV06 = new ethers.utils.Interface(EP_ABI_V06)
const TOPIC0 = iface.getEventTopic('UserOperationEvent')

// opsOf decodes handleOps/handleAggregatedOps calldata under either revision and returns
// {ops, revision} with the operations in calldata order, or null if the transaction is neither.
function opsOf(calldata) {
  for (const [revision, i] of [['v0.8', iface], ['v0.6', ifaceV06]]) {
    let parsed
    try {
      parsed = i.parseTransaction({ data: calldata })
    } catch (_) {
      continue
    }
    if (parsed.name === 'handleOps') return { ops: parsed.args.ops, revision }
    if (parsed.name === 'handleAggregatedOps') {
      let ops = []
      for (const g of parsed.args.opsPerAggregator) ops = ops.concat(g.userOps)
      return { ops, revision }
    }
  }
  return null
}
const PUBKEY_SELECTOR = ethers.utils.id('publicKey()').slice(0, 10)

let rpcId = 0
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

// rpc sends one JSON-RPC batch, retrying transport failures. Only an EVM revert of
// publicKey() means the sender has no readable key; node errors must stop the harvest.
async function rpc(calls, tolerant = false) {
  const body = calls.map((c) => ({ jsonrpc: '2.0', id: ++rpcId, method: c.method, params: c.params }))
  let lastErr
  for (let attempt = 0; attempt < 6; attempt++) {
    let arr
    try {
      const res = await fetch(RPC, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify(body.length === 1 ? body[0] : body),
      })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = await res.json()
      arr = Array.isArray(json) ? json : [json]
    } catch (e) {
      lastErr = e
      await sleep(500 * (attempt + 1))
      continue
    }
    const byId = new Map(arr.map((r) => [r.id, r]))
    return body.map((b) => {
      const r = byId.get(b.id)
      if (!r) throw new Error(`no response for id ${b.id}`)
      if (r.error) {
        if (tolerant && b.method === 'eth_call' && r.error.code === 3 &&
            /^execution reverted(?::|$)/i.test(String(r.error.message))) return null
        throw new Error(`${b.method}${b.method === 'eth_call' ? ` to ${b.params[0].to} at ${b.params[1]}` : ''}: ${r.error.message || JSON.stringify(r.error)}`)
      }
      return r.result
    })
  }
  throw new Error(`rpc failed after retries: ${lastErr && lastErr.message}`)
}

const one = async (method, params) => (await rpc([{ method, params }]))[0]
const hex = (n) => '0x' + n.toString(16)

// Runs `calls` in batches of BATCH, reporting progress under `label`.
async function batched(calls, label, tolerant = false) {
  const out = []
  for (let i = 0; i < calls.length; i += BATCH) {
    out.push(...(await rpc(calls.slice(i, i + BATCH), tolerant)))
    if (i % (BATCH * 20) === 0 || i + BATCH >= calls.length) {
      process.stderr.write(`  ${label}: ${Math.min(i + BATCH, calls.length)}/${calls.length}\n`)
    }
  }
  return out
}

async function main() {
  // The two loop steps first, before the first request: a mistyped one should cost nothing, and
  // LOG_CHUNK=0 would otherwise hang this run against a live endpoint rather than fail it.
  for (const [label, value] of [['LOG_CHUNK', LOG_CHUNK], ['BATCH', BATCH]]) {
    const problem = scanStepProblem(value)
    if (problem) {
      console.error(`${label}=${JSON.stringify(process.env[label])}: ${problem}`)
      process.exit(2)
    }
  }

  // A range the scan cannot walk has to stop the run: the loop below would otherwise write a
  // harvest of zero operations for a range it never looked at. The rule is in block_range.js so
  // that block_range_test.js runs this very check — see that file for what each case does here.
  const refuseRange = (problem) => {
    console.error(`FROM_BLOCK=${JSON.stringify(process.env.FROM_BLOCK)} TO_BLOCK=${JSON.stringify(process.env.TO_BLOCK)}: ${problem}`)
    process.exit(2)
  }
  // What the environment states is checked before the first request, so a mistyped range is
  // refused even when the endpoint is down. The chain head is checked after it is read.
  const stated = statedBlockRange(process.env)
  if (stated.problem) refuseRange(stated.problem)

  const chainId = Number(await one('eth_chainId', []))
  const clientVersion = await one('web3_clientVersion', [])
  const latest = Number(await one('eth_blockNumber', []))
  const fromBlock = stated.fromBlock
  const toBlock = stated.toBlock === null ? latest : stated.toBlock
  const rangeProblem = chainBlockRangeProblem(fromBlock, toBlock, latest)
  if (rangeProblem) refuseRange(rangeProblem)
  console.error(`chainId ${chainId}  client ${clientVersion}`)
  console.error(`EntryPoint ${EP}  blocks ${fromBlock}..${toBlock}  publicKey() read at ${PK_BLOCK}`)

  // 1. every UserOperationEvent in the window, in chain order.
  const events = []
  for (let from = fromBlock; from <= toBlock; from += LOG_CHUNK) {
    const to = Math.min(from + LOG_CHUNK - 1, toBlock)
    const logs = await one('eth_getLogs', [{ address: EP, topics: [TOPIC0], fromBlock: hex(from), toBlock: hex(to) }])
    for (const l of logs) {
      events.push({
        txHash: l.transactionHash,
        block: Number(l.blockNumber),
        logIndex: Number(l.logIndex),
        userOpHash: l.topics[1],
        sender: ethers.utils.getAddress('0x' + l.topics[2].slice(26)),
        success: l.data.slice(66, 130) !== '0'.repeat(64),
      })
    }
    process.stderr.write(`  logs: ..${to} (${events.length} events)\n`)
  }
  events.sort((a, b) => a.block - b.block || a.logIndex - b.logIndex)
  console.error(`${events.length} UserOperationEvent(s)`)

  // 2. which senders are passkey-owned, and their key.
  const senders = [...new Set(events.map((e) => e.sender))]
  console.error(`${senders.length} distinct senders`)
  const pkResults = await batched(
    senders.map((a) => ({ method: 'eth_call', params: [{ to: a, data: PUBKEY_SELECTOR }, PK_BLOCK] })),
    'publicKey()',
    true
  )
  const keys = new Map()
  for (let i = 0; i < senders.length; i++) {
    const r = pkResults[i]
    if (typeof r !== 'string' || r.length !== 2 + 128) continue
    const x = '0x' + r.slice(2, 66)
    const y = '0x' + r.slice(66, 130)
    if (/^0x0+$/.test(x) && /^0x0+$/.test(y)) continue // EOA-owned account
    keys.set(senders[i], { x, y })
  }
  console.error(`${keys.size} passkey-owned senders`)

  // 3. the transactions that carried at least one of those senders' ops.
  const wanted = new Map() // txHash -> events in log order
  for (const e of events) {
    if (!keys.has(e.sender)) continue
    if (!wanted.has(e.txHash)) wanted.set(e.txHash, [])
    wanted.get(e.txHash).push(e)
  }
  const txHashes = [...wanted.keys()]
  console.error(`${txHashes.length} transactions to fetch`)
  const txs = await batched(
    txHashes.map((h) => ({ method: 'eth_getTransactionByHash', params: [h] })),
    'transactions'
  )

  // A node with a bounded transaction index answers null for old hashes. Blocks are always
  // served, so recover those from their block, which is one request per block rather than
  // per transaction.
  const missingByBlock = new Map()
  for (let i = 0; i < txHashes.length; i++) {
    if (txs[i]) continue
    const block = wanted.get(txHashes[i])[0].block
    if (!missingByBlock.has(block)) missingByBlock.set(block, [])
    missingByBlock.get(block).push(i)
  }
  if (missingByBlock.size > 0) {
    const blocks = [...missingByBlock.keys()]
    console.error(`${blocks.length} block(s) to fetch for transactions the index no longer serves`)
    const fetched = await batched(
      blocks.map((b) => ({ method: 'eth_getBlockByNumber', params: [hex(b), true] })),
      'blocks'
    )
    for (let b = 0; b < blocks.length; b++) {
      const byHash = new Map(((fetched[b] && fetched[b].transactions) || []).map((t) => [t.hash, t]))
      for (const i of missingByBlock.get(blocks[b])) {
        const t = byHash.get(txHashes[i])
        if (t) txs[i] = t
      }
    }
  }

  // 4. pair each event with its op. Ops emit their events in calldata order, so the
  //    n-th passkey-sender event in a tx is the n-th matching op.
  const records = []
  const skipped = { txUnavailable: 0, undecodable: 0, unmatched: 0, notPasskeySig: 0, shortSig: 0 }
  const entryPointRevisions = {} // which ERC-4337 revision decoded each transaction
  for (let i = 0; i < txHashes.length; i++) {
    const tx = txs[i]
    const evs = wanted.get(txHashes[i])
    if (!tx) {
      skipped.txUnavailable += evs.length
      continue
    }
    const decoded = opsOf(tx.input)
    if (!decoded) {
      skipped.undecodable += evs.length
      continue
    }
    const { ops, revision } = decoded
    entryPointRevisions[revision] = (entryPointRevisions[revision] || 0) + evs.length

    const perSender = new Map() // sender -> queue of ops, in calldata order
    for (const op of ops) {
      const a = ethers.utils.getAddress(op.sender)
      if (!perSender.has(a)) perSender.set(a, [])
      perSender.get(a).push(op)
    }
    for (const e of evs) {
      const queue = perSender.get(e.sender)
      if (!queue || queue.length === 0) {
        skipped.unmatched++
        continue
      }
      const op = queue.shift()
      const sig = Buffer.from((op.signature || '0x').slice(2), 'hex')
      const kind = classifySignature(sig)
      if (kind !== 'passkey') {
        skipped[kind]++
        continue
      }
      const key = keys.get(e.sender)
      const payload = sig.subarray(3)
      records.push({
        block: e.block,
        txHash: e.txHash,
        sender: e.sender,
        userOpHash: e.userOpHash,
        executionSucceeded: e.success,
        payload: '0x' + payload.toString('hex'),
        keyX: key.x,
        keyY: key.y,
        // The precompile input, *rebuilt* from the layout the account implementation
        // uses — abi.encodePacked(userOpHash, signaturePayload, publicKey.x,
        // publicKey.y) — and not read off the call. That matters for one class of
        // input and only one: the pre-fork parser ignores everything after y and the
        // P-256 signature does not cover it, so a caller that appended bytes there
        // would verify today, its rebuilt input would verify too, and the replay could
        // not tell the difference. Every other mistake in this layout produces an input
        // that fails both parsers and shows up in the replay as such.
        //
        // See the "Coverage limits" section of docs/webauthn/replay-validation.md for
        // what is and is not evidence here, and for the trace comparison that closes
        // it.
        input: e.userOpHash + payload.toString('hex') + key.x.slice(2) + key.y.slice(2),
      })
    }
  }

  // The endpoint is deliberately absent from what follows. It was recorded once, then
  // trimmed to scheme and host after a URL turned out to be a place an API key can ride
  // along in — and a host name can be one too, so the trimmed form was not enough either.
  // Nothing downstream reads it: chain id, EntryPoint and the block range are the
  // provenance the replay checks, and the endpoint is the operator's business.
  const out = {
    source: {
      chainId,
      clientVersion,
      entryPoint: EP,
      fromBlock,
      toBlock,
      publicKeyReadAt: PK_BLOCK,
      harvestedAt: new Date().toISOString(),
    },
    counts: {
      userOperationEvents: events.length,
      distinctSenders: senders.length,
      passkeySenders: keys.size,
      transactionsFetched: txHashes.length,
      passkeyOps: records.length,
      entryPointRevisions,
      skipped,
    },
    ops: records,
  }
  fs.writeFileSync(OUT, JSON.stringify(out, null, 1))
  console.error(`\nwrote ${records.length} passkey ops to ${OUT}`)
  console.error(JSON.stringify(out.counts))
}

// Only harvest when run as a program, so that requiring this file cannot start a chain
// scan.
if (require.main === module) {
  main().catch((e) => {
    console.error('FATAL', (e && e.stack) || e)
    process.exit(1)
  })
}
