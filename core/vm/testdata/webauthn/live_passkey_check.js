#!/usr/bin/env node
/*
 * The positive half of the compatibility check, against a live chain: does the strict
 * 0x111 still accept what a passkey produces?
 *
 * A fresh prime256v1 (P-256) key signs a genuine WebAuthn assertion over a fresh
 * challenge, the assertion is laid out exactly as an Incentiv account packs the 0x111
 * input, and 0x111 is called through eth_call. On a chain where the fork is active it
 * must return 1 for that input, 0 for the same input with one byte appended, and 0 for
 * a key that did not sign it. Before activation the second line reads FAIL — the
 * pre-fork parser ignores trailing bytes — which is also a direct way to see whether
 * the node answering is running the fork.
 *
 * READ-ONLY: eth_call runs the precompile without a transaction, so no account, no
 * funding and no gas. Nothing beyond the three results and the endpoint's host is
 * printed. The only part of the passkey path this fork changes is the 0x111 call;
 * the account's signature envelope and EntryPoint are unchanged by it and covered by
 * the contracts' own test-suite.
 *
 * Usage:
 *   node live_passkey_check.js <rpc-url>     exit 0 when every expectation held
 *   node live_passkey_check.js --print-input one canonical input as hex, no network;
 *                                            core/vm/live_passkey_check_test.go runs it
 *                                            through both parsers, so the layout here
 *                                            cannot drift from the account's unnoticed
 *
 * Node 18 or newer; built-in crypto and fetch, nothing to install.
 */
'use strict'
const crypto = require('crypto')

const PRECOMPILE = '0x0000000000000000000000000000000000000111'
// The relying party the deployed wallets use; any origin works for the precompile, which
// checks the challenge and the response type, not the origin.
const ORIGIN = 'portal.incentiv.io'

const be32 = (n) => { const b = Buffer.alloc(4); b.writeUInt32BE(n >>> 0); return b }

// One canonical 0x111 input — message ‖ payload ‖ x ‖ y — from a fresh key over a fresh
// 32-byte challenge, plus the same message and payload under a key that did not sign it.
function buildAssertion() {
  const message = crypto.randomBytes(32)
  const authData = Buffer.concat([
    crypto.createHash('sha256').update(ORIGIN).digest(), // rpIdHash
    Buffer.from([0x01]), // flags: user present
    be32(1), // signCount
  ])
  const clientDataJSON = Buffer.from(
    `{"type":"webauthn.get","challenge":"${message.toString('base64url')}","origin":"https://${ORIGIN}","crossOrigin":false}`,
    'utf8'
  )
  const challengeLocation = clientDataJSON.indexOf(Buffer.from('"challenge":"'))
  const responseTypeLocation = clientDataJSON.indexOf(Buffer.from('"type":"webauthn.get"'))
  const clientDataHash = crypto.createHash('sha256').update(clientDataJSON).digest()
  // Retry until none of r, s, x, y has a leading zero byte. The pre-fork parser re-serialises
  // each word through big.Int, so a leading zero makes it reject; live_passkey_check_test.go
  // runs this one input through BOTH parsers and a canonical input must verify on each. The
  // strict parser accepts leading zeros — covered separately by the Go unit tests — so trimming
  // them here only keeps this shared fixture unambiguous (about one generation in 64 retries).
  let x, y, rs
  for (;;) {
    const kp = crypto.generateKeyPairSync('ec', { namedCurve: 'prime256v1' })
    const jwk = kp.publicKey.export({ format: 'jwk' })
    x = Buffer.from(jwk.x, 'base64url')
    y = Buffer.from(jwk.y, 'base64url')
    rs = crypto.sign('sha256', Buffer.concat([authData, clientDataHash]), {
      key: kp.privateKey,
      dsaEncoding: 'ieee-p1363',
    })
    if (x[0] !== 0 && y[0] !== 0 && rs[0] !== 0 && rs[32] !== 0) break
  }
  const payload = Buffer.concat([
    be32(authData.length), authData,
    Buffer.from([0x00]), // requireUserVerification
    be32(clientDataJSON.length), clientDataJSON,
    be32(challengeLocation), be32(responseTypeLocation),
    rs.subarray(0, 32), rs.subarray(32, 64),
  ])
  const other = crypto.generateKeyPairSync('ec', { namedCurve: 'prime256v1' }).publicKey.export({ format: 'jwk' })
  return {
    input: Buffer.concat([message, payload, x, y]),
    wrongKeyInput: Buffer.concat([message, payload, Buffer.from(other.x, 'base64url'), Buffer.from(other.y, 'base64url')]),
  }
}

const isOne = (hex) => /^0x0*1$/.test(hex)
const isZero = (hex) => hex === '0x' || /^0x0+$/.test(hex)

async function main() {
  const arg = process.argv[2]
  if (arg === '--print-input') {
    process.stdout.write('0x' + buildAssertion().input.toString('hex') + '\n')
    return
  }
  const rpcURL = arg || process.env.RPC
  if (!rpcURL) {
    console.error('usage: live_passkey_check.js <rpc-url> | --print-input')
    process.exit(2)
  }
  const rpc = async (method, params) => {
    const res = await fetch(rpcURL, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
    })
    const j = await res.json()
    if (j.error) throw new Error(`${method}: ${j.error.message}`)
    return j.result
  }
  const call = (data) => rpc('eth_call', [{ to: PRECOMPILE, data: '0x' + data.toString('hex') }, 'latest'])

  const chainId = parseInt(await rpc('eth_chainId', []), 16)
  const head = parseInt(await rpc('eth_blockNumber', []), 16)
  console.log(`RPC ${new URL(rpcURL).host}  chainId ${chainId}  head ${head}`)

  const { input, wrongKeyInput } = buildAssertion()
  const canonical = await call(input)
  const trailing = await call(Buffer.concat([input, Buffer.from([0x00])]))
  const wrongKey = await call(wrongKeyInput)

  const checks = [
    ['valid assertion returns 1 (the strict parser accepts a real passkey assertion)', isOne(canonical), canonical],
    ['valid + trailing byte returns 0 (non-canonical input is rejected; FAIL here means the pre-fork parser)', isZero(trailing), trailing],
    ['mismatched key returns 0', isZero(wrongKey), wrongKey],
  ]
  let ok = true
  for (const [label, pass, raw] of checks) {
    console.log(`${pass ? 'PASS' : 'FAIL'}  ${label}  (0x111 -> ${raw})`)
    ok = ok && pass
  }
  if (!ok) {
    console.error('\nAt least one expectation did not hold.')
    process.exit(1)
  }
  console.log('\nAll checks passed: the strict precompile on this chain accepts a real passkey assertion and rejects non-canonical input.')
}

main().catch((e) => {
  console.error('ERROR', e.message)
  process.exit(2)
})
