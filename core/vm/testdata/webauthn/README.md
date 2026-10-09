# `mainnet-passkey-ops.json`

Real passkey assertions from Incentiv mainnet (chain id 24101), used by
`TestWebAuthnVerifyMainnetReplay` in `core/vm/contracts_webauthn_replay_test.go` to check
that the `WebAuthnStrict` fork rejects none of them — a result about the reconstruction
described below rather than about every byte those wallets sent.

Each entry is one `UserOperation` that was mined, so the `0x111` precompile returned 1 for
it under the pre-fork parser. `input` is **reconstructed**, not observed — the event, the
transaction's calldata and the account's key, assembled in the layout the account
implementation uses:

```
input = userOpHash(32) ‖ signaturePayload(variable) ‖ publicKey.x(32) ‖ publicKey.y(32)
```

which is what `BaseIncentivAccount._internalSignatureValidation` builds with
`abi.encodePacked`. `userOpHash` comes from the `UserOperationEvent` topic and
`signaturePayload` is `signature[3:]`, both straight out of the transaction;
`publicKey.x/y` are read from the account. Everything here is public chain data. The
signatures are over operations that have already executed, and replaying one verifies a
signature — it does not authorise anything.

## Provenance

The committed file is a deterministic sample of a larger harvest, kept small enough to
belong in the repository while covering every account that appeared in the harvest. The
sample's `source.windows` records the block ranges it was drawn from and
`source.sampledFrom` how many operations it was drawn from.

Two things about that metadata, so it is not read for more than it says:

- `source.windows` has eleven entries and `source.sampledFrom` adds them up, but one of
  them (3,500,000 – 3,505,000, 741 operations) lies inside another (3,500,000 –
  3,510,000, 1,170), so those 741 are counted twice. The number of distinct operations
  is 34,300, which is what `docs/webauthn/replay-validation.md` reports and what its
  ten-row table sums to.
- Four windows — 4,000,000 – 4,020,000; 4,500,000 – 4,520,000; 5,000,000 – 5,021,487;
  and 5,021,488 – 6,021,488, 21,585 operations between them — carry no
  `entryPointRevisions`, which `harvest_passkey_ops.js` as committed always emits. They
  were harvested with an earlier revision of it. The operations themselves are chain
  data and replay the same, but re-running the committed script will not reproduce those
  window records byte for byte.
- The per-window figures are the operations reconstructed as passkey operations and, where
  present, the `UserOperationEvent` count by EntryPoint revision. Both are counted over
  passkey-owned senders only — `harvest_passkey_ops.js` drops every event whose sender has no
  passkey key before either tally — so the gap between them is *not* non-passkey accounts.
  It is operations from passkey-owned accounts that were not reconstructed, for one of
  three reasons `harvest_passkey_ops.js` as committed separates: `notPasskeySig` (the
  operation was signed by another path and never reached `0x111`), `unmatched` and
  `shortSig` (reconstruction failures, which are a coverage gap). The harvests behind the
  committed file did not record that breakdown per window, and neither did the sampler of the
  time; the sampler now carries `skipped` through, so a fresh harvest records it.
  `docs/webauthn/replay-validation.md` has the number for the windows that can be measured
  today.
- Because of the two points above, `sample_passkey_ops.js` treats `entryPointRevisions` and
  `skipped` as optional — the documented command would otherwise refuse the files it names —
  and writes `null` for whichever a harvest does not carry, so an unrecorded count cannot be
  read as a window with nothing skipped. `passkeyOps` is the one it requires, and it has to
  equal the number of operations in the file, which have to fall inside the range it declares. A
  `skipped` that is there has to carry all five of its counters, since every revision of the
  harvest writes all five; and then its three reconstruction failures plus `passkeyOps` have to
  add up to the events `entryPointRevisions` counted, which is the residual arithmetic. A window whose
  operations could none of them be reconstructed keeps its record too — its counts are the
  measurement of that gap, and leaving it out would publish the range as having nothing
  missing.

Twelve files sit next to this file — five scripts, the two modules they share, and five
tests — so the sample, the full replay, the live check and the traced-input comparison can be
reproduced without hunting for them:

| File | |
|---|---|
| `harvest_passkey_ops.js` | the harvest. Needs `classify_signature.js` beside it |
| `script_guards_test.js` | runs the harvest and the census themselves against a stub endpoint; the same Go test runs it |
| `classify_signature.js` | which path an account takes for a signature. No dependencies |
| `classify_signature_test.js` | covers that rule; `core/vm/harvest_script_test.go` runs it |
| `block_range.js` | when a block range cannot be scanned, and when a scan step cannot be used. All three scripts here apply it, no dependencies |
| `block_range_test.js` | covers that rule; the same Go test runs it |
| `sample_passkey_ops.js` | cuts a harvest down to the committed fixture. No dependencies |
| `sample_passkey_ops_test.js` | covers its merge checks; the same Go test runs it |
| `census_passkey_keys.js` | counts passkey-owned accounts and their key shapes |
| `trace_passkey_ops.js` | the observation half of the traced-input comparison: for a sample of the fixture's operations, the inner `0x111` input from `debug_traceTransaction` compared byte for byte with the reconstruction. Needs a node that serves `debug` on the fixture's chain. No dependencies |
| `trace_passkey_ops_test.js` | reaches each of its verdicts against a stub node; the same Go test runs it |
| `live_passkey_check.js` | the positive check against a live chain: a fresh P-256 key signs a real assertion and `eth_call` asks `0x111` for 1 canonical, 0 with a trailing byte, 0 under another key. `--print-input` hands the same input to `core/vm/live_passkey_check_test.go`, which runs it through both parsers. No dependencies |

`harvest_passkey_ops.js` requires `./classify_signature.js` and `./block_range.js`; the census and
the sampler require the second of them. Node resolves those next to the script rather than next to
the working directory, so copying one somewhere on its own fails with `Cannot find module`. Take
the directory.

They are read-only — getLogs, getTransactionByHash, getBlockByNumber and eth_call, no
writes and no signing. The two that reach the chain, the harvest and the census, need Node
and `ethers` v5; this repository ships no `package.json`, so run them from the contracts
repository, which already depends on it, or `npm i ethers@5` next to them. The sampler,
`classify_signature.js` and its test need Node alone, which is why the test can run in CI.

```
# harvest every passkey operation in a block range
RPC=https://rpc-fr-1.incentiv.io FROM_BLOCK=… TO_BLOCK=… OUT=ops.json \
  node harvest_passkey_ops.js

# merge harvests and write the committed sample
node sample_passkey_ops.js mainnet-passkey-ops.json 500 ops1.json ops2.json …
```

## Replaying a full harvest

The test reads `WEBAUTHN_OPS_FILE` when it is set, so a harvest that is too large to
commit can be replayed through exactly the same code:

```
cd core/vm
WEBAUTHN_OPS_FILE=/path/to/ops.json go test -run TestWebAuthnVerifyMainnetReplay -v .
```

The corpus floor is a separate switch: the test wants at least 500 operations from any
fixture, so that a harvest which errored early cannot read as a clean run. Lower it
explicitly when a small file is the point — one captured operation during an incident:

```
WEBAUTHN_OPS_FILE=/path/to/one-op.json WEBAUTHN_OPS_MIN=1 \
  go test -run TestWebAuthnVerifyMainnetReplay -v .
```

The result belongs in `docs/webauthn/replay-validation.md`, which records the windows and
counts each validation run covered. Re-run it against a head close to the release date
before activating the fork on any network: the coverage argument is about the encodings
live clients produce, and that is a claim with an expiry date.
