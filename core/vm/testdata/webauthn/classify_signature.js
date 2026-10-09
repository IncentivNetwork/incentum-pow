/*
 * classifySignature decides which path an account takes for a UserOperation's signature.
 * Its own file, with no dependencies, so that classify_signature_test.js can require it
 * without pulling in ethers — harvest_passkey_ops.js needs ethers and CI installs no
 * node_modules, so a test that reached the rule through the harvest script could not run
 * there.
 */
'use strict'

// The order matters and it is the account's order:
// BaseIncentivAccount._internalSignatureValidation checks signature.length == 65 first and
// treats such a signature as plain ECDSA whatever its first byte says, reading signature[0]
// only for every other length. Reading the version first classified a 65-byte recovery
// signature that happens to start with 0x01 — one in 256 of them — as a passkey operation,
// and a passkey input was rebuilt from it.
//
// Returns the name of one of harvest_passkey_ops.js's `skipped` counters, or 'passkey'.
function classifySignature(sig) {
  if (sig.length === 65) return 'notPasskeySig' // plain ECDSA, version byte not consulted
  if (sig.length <= 3) return 'shortSig' // no room for the 3-byte prefix and a payload
  if (sig[0] !== 0x01) return 'notPasskeySig' // version 0 is ECDSA, anything else is rejected
  return 'passkey'
}

module.exports = { classifySignature }
