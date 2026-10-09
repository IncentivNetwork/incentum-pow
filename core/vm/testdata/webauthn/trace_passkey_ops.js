#!/usr/bin/env node
/*
 * The observation half of the traced-input comparison: for mined passkey operations in the
 * committed fixture, read the inner 0x111 call's `input` off a callTracer trace and compare it
 * byte for byte with the input the harvest reconstructed.
 *
 * The replay rebuilds the 0x111 input — userOpHash ‖ signaturePayload ‖ x ‖ y — and cannot see
 * bytes a caller might append after y, which is exactly the class WebAuthnStrict rejects. Byte
 * equality with the traced input shows the real call ended at y with nothing after it, so the
 * input is canonical as observed and not only as reconstructed. docs/webauthn/replay-validation.md,
 * "Traced-input comparison", records the result.
 *
 * READ-ONLY: eth_chainId, eth_blockNumber and one debug_traceTransaction per operation. It needs
 * a node that serves debug on the fixture's chain — an archive node, typically on its loopback,
 * reached through a tunnel. Nothing beyond block numbers, sender prefixes and the endpoint's host
 * is printed.
 *
 *   node trace_passkey_ops.js <rpc-url> [sampleCount]     default sample 20
 *
 * The sample spreads over the fixture's block range, one operation per sender where possible,
 * and always includes the first and the last. Exit 0 when every traced operation matched, 1 on
 * any mismatch (with the lengths and the tail after the reconstruction), 2 when nothing could be
 * confirmed, and 3 when some operations matched but others could not be traced, since confirming
 * part of the sample says nothing about the rest. trace_passkey_ops_test.js runs it against a
 * stub node for all four outcomes.
 */
'use strict'
const fs = require('node:fs')
const path = require('node:path')

const PRECOMPILE = '0x0000000000000000000000000000000000000111'
// validateUserOp(PackedUserOperation,bytes32,uint256) on this chain's EntryPoint. EntryPoint
// also passes userOpHash to executeUserOp (0x8dd7712f), so the validation frame is matched by
// this selector, not by the hash alone.
const VALIDATE_USEROP = '0x19822f7c'
const FIXTURE = process.env.WEBAUTHN_OPS_FILE || path.join(__dirname, 'mainnet-passkey-ops.json')

const rpcURL = process.argv[2]
if (!rpcURL) {
  console.error('usage: trace_passkey_ops.js <rpc-url> [sampleCount]')
  process.exit(2)
}
const sampleCount = Number(process.argv[3] || 20)

let id = 0
async function rpc(method, params) {
  const res = await fetch(rpcURL, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: ++id, method, params }),
  })
  const j = await res.json()
  if (j.error) throw new Error(`${method}: ${j.error.message}`)
  return j.result
}

// Every 0x111 call input made while the account validated this operation: the inputs nested
// under the account's validateUserOp CALL for this operation. That call is identified exactly — a
// CALL from the EntryPoint to the account, carrying the validateUserOp selector with this
// operation's userOpHash as its second argument. EntryPoint also hands userOpHash to executeUserOp
// (IAccountExecute), from which an account may call 0x111, so matching the selector and the caller
// — not merely the hash in the calldata — keeps an execution call from being read as a validation
// call. Another operation's validateUserOp frame carries a different userOpHash and is excluded,
// so the comparison is tied to this exact operation's signature check.
function validationInputs(frame, account, entryPoint, uopHash, inside, out) {
  const input = frame.input ? frame.input.toLowerCase() : ''
  const isValidation =
    !!frame.from && frame.from.toLowerCase() === entryPoint &&
    !!frame.to && frame.to.toLowerCase() === account &&
    input.startsWith(VALIDATE_USEROP) && input.slice(74, 138) === uopHash
  const within = inside || isValidation
  if (within && frame.to && frame.to.toLowerCase() === PRECOMPILE && input) out.push(input)
  for (const c of frame.calls || []) validationInputs(c, account, entryPoint, uopHash, within, out)
  return out
}

// An even spread across the block range, one operation per sender where possible, and the
// first and last operation always in.
function pickSample(ops, n) {
  const sorted = ops.slice().sort((a, b) => a.block - b.block)
  const seen = new Set()
  const out = []
  const take = (o) => {
    if (out.length >= n || seen.has(o.sender.toLowerCase())) return
    seen.add(o.sender.toLowerCase())
    out.push(o)
  }
  take(sorted[0])
  take(sorted[sorted.length - 1])
  const step = Math.max(1, Math.floor(sorted.length / n))
  for (let i = step; i < sorted.length - 1 && out.length < n; i += step) take(sorted[i])
  return out.sort((a, b) => a.block - b.block)
}

async function main() {
  const fixture = JSON.parse(fs.readFileSync(FIXTURE, 'utf8'))
  const chainId = parseInt(await rpc('eth_chainId', []), 16)
  const head = parseInt(await rpc('eth_blockNumber', []), 16)
  console.log(`RPC ${new URL(rpcURL).host}  chainId ${chainId}  head ${head}  fixture ops ${fixture.ops.length}`)
  if (chainId !== fixture.source.chainId) {
    throw new Error(`chainId ${chainId}, but the fixture is from chain ${fixture.source.chainId}`)
  }

  const sample = pickSample(fixture.ops, sampleCount)
  const entryPoint = fixture.source.entryPoint.toLowerCase()
  console.log(`tracing ${sample.length} ops across blocks ${sample[0].block}..${sample[sample.length - 1].block}\n`)

  let matched = 0
  const mismatches = []
  const unconfirmed = []
  for (const op of sample) {
    let frame
    try {
      frame = await rpc('debug_traceTransaction', [op.txHash, { tracer: 'callTracer' }])
    } catch (e) {
      unconfirmed.push(op)
      console.log(`block ${op.block} ${op.sender.slice(0, 10)} TRACE ERROR ${e.message}`)
      continue
    }
    const want = op.input.toLowerCase()
    const uopHash = want.slice(2, 66) // 32-byte userOpHash hex (no 0x), unique to the operation
    // Bind the comparison to this operation's validateUserOp call: the 0x111 inputs the account
    // fed while validating the operation carrying this userOpHash. Every such call has to be the
    // reconstruction; an execution-phase call or another operation's call cannot stand in for one
    // that carried a trailing byte.
    const forOp = validationInputs(frame, op.sender.toLowerCase(), entryPoint, uopHash, false, [])
    if (forOp.length === 0) {
      unconfirmed.push(op)
      console.log(`block ${op.block} ${op.sender.slice(0, 10)} NO validateUserOp 0x111 call for this operation in the trace`)
    } else if (forOp.every((i) => i === want)) {
      matched++
      console.log(`block ${op.block} ${op.sender.slice(0, 10)} MATCH (${forOp.length} call${forOp.length === 1 ? '' : 's'} to 0x111 for this operation)`)
    } else {
      // At least one 0x111 call for this operation is not the reconstruction — the shape the
      // fork rejects. Report the first such call: its length and the tail after y.
      const bad = forOp.find((i) => i !== want)
      const detail = {
        block: op.block,
        wantLen: (want.length - 2) / 2,
        gotLen: (bad.length - 2) / 2,
        tailAfterWant: bad.startsWith(want) ? bad.slice(want.length) : '(the reconstruction is not a prefix of it)',
      }
      mismatches.push(detail)
      console.log(`block ${op.block} ${op.sender.slice(0, 10)} MISMATCH wantLen=${detail.wantLen} gotLen=${detail.gotLen}`)
    }
  }

  console.log(`\nmatched ${matched} / ${sample.length}   mismatches ${mismatches.length}   unconfirmed ${unconfirmed.length}`)
  if (mismatches.length) {
    for (const m of mismatches) console.log('  ' + JSON.stringify(m))
    console.error('\nAt least one real 0x111 input differs from the reconstruction. Investigate before activation.')
    process.exit(1)
  }
  if (matched === 0) {
    console.error('\nNo operation could be confirmed. Is this an archive node on the right chain?')
    process.exit(2)
  }
  if (unconfirmed.length) {
    console.error(`\n${unconfirmed.length} of ${sample.length} sampled operation(s) could not be traced, so this is not a clean pass: confirming only part of the sample says nothing about the rest. Re-run against a full-history archive node, or investigate those transactions.`)
    process.exit(3)
  }
  console.log('\nOK: every traced operation fed 0x111 exactly the reconstructed input, ending at y with no trailing bytes.')
}

main().catch((e) => {
  console.error('ERROR', e.message)
  process.exit(2)
})
