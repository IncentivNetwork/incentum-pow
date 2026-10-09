#!/usr/bin/env node
/*
 * Counts the passkey-owned accounts on an Incentiv chain and, among them, the keys the
 * pre-fork 0x111 parser cannot verify: those whose x or y has a zero top byte.
 *
 * This is the script behind the three numbers in docs/webauthn/replay-validation.md
 * ("Side finding: one account is currently unable to transact at all"). READ-ONLY:
 * one eth_getLogs plus, optionally, one eth_getLogs per reported account. No writes,
 * no signing, no per-account data printed beyond the addresses it flags.
 *
 * An account is passkey-owned when IncentivAccountInitialized carries owner == 0;
 * initialize enforces XOR(owner != 0, publicKey != 0). The event is
 *   IncentivAccountInitialized(address indexed entryPoint, address indexed owner, bytes32[2] publicKey)
 * so publicKey.x and .y are the two words of its data.
 *
 * Env:
 *   RPC         JSON-RPC endpoint   (default https://rpc-fr-1.incentiv.io)
 *   FROM_BLOCK  first block         (default 0)
 *   TO_BLOCK    last block          (default latest)
 *   CHECK_USAGE set to 1 to also report whether each flagged account ever transacted
 *   ENTRYPOINT  EntryPoint address  (only needed with CHECK_USAGE)
 */
'use strict'
const { ethers } = require('ethers')
const { chainBlockRangeProblem, statedBlockRange } = require('./block_range.js')
// ^ resolved next to this script, so take block_range.js along when copying it somewhere.

const RPC = process.env.RPC || 'https://rpc-fr-1.incentiv.io'
const ENTRYPOINT = process.env.ENTRYPOINT || '0x3eC61c5633BBD7Afa9144C6610930489736a72d4'

async function main() {
  // The same rule the harvest applies, from the same module. Without it a range that cannot be
  // scanned goes to the provider, and what comes back is the provider's business: an empty answer
  // prints a census of zero accounts and exits 0, which reads as a measurement of the chain — and
  // the numbers this script reports are quoted in docs/webauthn/replay-validation.md.
  const refuseRange = (problem) => {
    console.error(`FROM_BLOCK=${JSON.stringify(process.env.FROM_BLOCK)} TO_BLOCK=${JSON.stringify(process.env.TO_BLOCK)}: ${problem}`)
    process.exit(2)
  }
  // What the environment states is checked before the provider exists — ethers v5 starts
  // detecting the network as soon as one is constructed — so a mistyped range is refused even
  // when the endpoint is down. The chain head is checked after it is read.
  const stated = statedBlockRange(process.env)
  if (stated.problem) refuseRange(stated.problem)

  const p = new ethers.providers.JsonRpcProvider(RPC)
  const net = await p.getNetwork()
  const latest = await p.getBlockNumber()
  const fromBlock = stated.fromBlock
  const toBlock = stated.toBlock === null ? latest : stated.toBlock
  const rangeProblem = chainBlockRangeProblem(fromBlock, toBlock, latest)
  if (rangeProblem) refuseRange(rangeProblem)
  const topic0 = ethers.utils.id('IncentivAccountInitialized(address,address,bytes32[2])')

  console.log(`chainId ${net.chainId}  blocks ${fromBlock}..${toBlock}`)
  const logs = await p.getLogs({ fromBlock, toBlock, topics: [topic0] })

  let total = 0
  let passkey = 0
  const flagged = []
  for (const l of logs) {
    total++
    // topics[2] is the indexed owner; zero means the account is passkey-owned.
    if (!/^0x0+$/i.test(ethers.utils.hexDataSlice(l.topics[2], 12))) continue
    passkey++
    const data = l.data.slice(2)
    const x = data.slice(data.length - 128, data.length - 64)
    const y = data.slice(data.length - 64)
    if (x.slice(0, 2) === '00' || y.slice(0, 2) === '00') {
      flagged.push({ account: l.address, block: l.blockNumber, x: '0x' + x, y: '0x' + y })
    }
  }

  const expected = (passkey * 2) / 256
  console.log(`accounts initialised:                    ${total}`)
  console.log(`passkey-owned (owner == 0):              ${passkey}`)
  console.log(`keys with a zero top byte in x or y:     ${flagged.length}  (expected ~${expected.toFixed(1)} if unfiltered)`)

  for (const f of flagged) {
    let usage = ''
    if (process.env.CHECK_USAGE === '1') {
      const topic = ethers.utils.id('UserOperationEvent(bytes32,address,address,uint256,bool,uint256,uint256)')
      const ops = await p.getLogs({
        address: ENTRYPOINT,
        topics: [topic, null, ethers.utils.hexZeroPad(f.account, 32)],
        fromBlock,
        toBlock,
      })
      usage = `  UserOperationEvents=${ops.length}`
    }
    console.log(`  ${f.account}  initialised at ${f.block}${usage}`)
    console.log(`    x ${f.x}`)
    console.log(`    y ${f.y}`)
  }
}

// What the provider throws quotes the endpoint: ethers v5's fetchJson puts `url="…"` into its
// SERVER_ERROR and TIMEOUT messages, and an endpoint URL is where an API key rides along. The
// harvest never prints its endpoint and this script must not either, so both the value and the
// field are stripped before a failure reaches stderr.
const withoutEndpoint = (text) => String(text).split(RPC).join('<RPC>').replace(/url="[^"]*"/g, 'url="<RPC>"')

main().catch((e) => {
  console.error('FATAL', withoutEndpoint((e && e.message) || e))
  process.exit(1)
})
