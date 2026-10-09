#!/usr/bin/env node
/*
 * Covers classifySignature in harvest_passkey_ops.js, which decides whether a mined
 * operation is replayed as a passkey assertion. It is the one piece of the harvest the Go
 * replay cannot re-check: the replay reads inputs that have already been reconstructed, so
 * a misclassification only shows up if the resulting bytes happen to fail both parsers.
 *
 * The case worth pinning is the one the script got wrong: the account checks
 * signature.length == 65 before it reads the version byte, so a 65-byte ECDSA signature is
 * ECDSA even when its first byte is 0x01 — one in 256 of them.
 *
 * No dependencies — the rule lives in classify_signature.js for exactly that reason, since
 * harvest_passkey_ops.js needs ethers and CI installs no node_modules.
 *
 * Run: node classify_signature_test.js
 */
const assert = require('node:assert/strict')
const { classifySignature } = require('./classify_signature.js')

const sig = (length, first) => {
  const b = Buffer.alloc(length, 0xab)
  if (length > 0 && first !== undefined) b[0] = first
  return b
}

// A 65-byte signature is plain ECDSA whatever its first byte says. 0x01 is the case that
// used to be misread as a passkey assertion.
assert.equal(classifySignature(sig(65, 0x01)), 'notPasskeySig', '65-byte signature starting 0x01')
assert.equal(classifySignature(sig(65, 0x00)), 'notPasskeySig', '65-byte signature starting 0x00')
assert.equal(classifySignature(sig(65, 0x02)), 'notPasskeySig', '65-byte signature starting 0x02')

// A real passkey signature: version 0x01 and some other length. The shortest canonical
// payload is 81 bytes, so 3 + 81 is the floor for a well-formed one.
assert.equal(classifySignature(sig(3 + 81, 0x01)), 'passkey', 'versioned passkey signature')
assert.equal(classifySignature(sig(200, 0x01)), 'passkey', 'longer passkey signature')

// Version 0 is the Incentiv ECDSA path; anything else the account rejects outright.
assert.equal(classifySignature(sig(200, 0x00)), 'notPasskeySig', 'versioned ECDSA signature')
assert.equal(classifySignature(sig(200, 0x07)), 'notPasskeySig', 'unknown version')

// Too short to hold the 3-byte prefix and a payload.
assert.equal(classifySignature(sig(0)), 'shortSig', 'empty signature')
assert.equal(classifySignature(sig(3, 0x01)), 'shortSig', 'prefix only')

console.log('classify_signature_test: ok')
