# WebAuthnStrict — replay validation

Written for reviewers and for whoever signs off on activating the fork.

`WebAuthnStrict` makes the `0x111` precompile stricter, so the question that decides
whether it can be activated is whether it rejects anything real wallets produce. A
rejection would not be a failed transaction — it would be a passkey account unable to
authorise anything.

This is the measurement that answers it: every passkey `UserOperation` the harvest could
**reconstruct** from the harvested windows was replayed through both parsers, in the same
process. The input is assembled from the event, the transaction and the account rather than
read off the call, and the reconstruction does not reach every operation in those windows —
6,880 of 19,595 decoded events were not rebuilt, and events whose transaction could not be
fetched or decoded are outside that figure too. "Coverage limits" has both, and what they
do and do not establish.

Run on **2026-09-26** against mainnet head **6,022,576**, and re-run on **2026-10-07**
against head **6,201,887** — the section "Re-run on 2026-10-07" below.
Live client at both times: `Geth/v1.11.8-stable-af5b5474/linux-amd64/go1.22.2`, chain id
24101 — i.e. the binary this change is written against is the one running.
The replay fixture is tied to EntryPoint `0x3eC61c5633BBD7Afa9144C6610930489736a72d4`.

## Result

| | |
|---|---|
| Operations replayed | **34,300** |
| Distinct accounts | **403** |
| Block span | 1,000,377 – 6,020,945 |
| Verified under the pre-fork parser | 34,300 / 34,300 |
| Verified under the post-fork parser | 34,300 / 34,300 |
| **Accepted before the fork, rejected after it** | **0** |
| Payloads that were not canonically encoded | 0 |

Every operation that verified before the fork still verifies after it. No account in the
sample loses the ability to sign.

Every payload was canonically encoded: its declared `authDataLength` and
`clientDataJSONLength` accounted for its whole length, `81 + authDataLength +
clientDataJSONLength`, with nothing after `s`. That is observed on the bytes the
transaction carries, and live traffic satisfies it without exception.

The other half of the rule — that the *whole input* ends at `y` — is not observed here,
because the input is rebuilt from the account implementation's layout rather than read off
the call. See "Coverage limits" below for what bounds that and for the trace comparison
that closes it.

## What the harvest covered

| Blocks | Passkey operations |
|---|---|
| 1,000,000 – 1,010,000 | 3 |
| 1,500,000 – 1,510,000 | 701 |
| 2,000,000 – 2,010,000 | 5,059 |
| 2,500,000 – 2,510,000 | 3,500 |
| 3,000,000 – 3,010,000 | 2,282 |
| 3,500,000 – 3,510,000 | 1,170 |
| 4,000,000 – 4,020,000 | 2,863 |
| 4,500,000 – 4,520,000 | 1,278 |
| 5,000,000 – 5,021,487 | 645 |
| 5,021,488 – 6,021,488 (all of it) | 16,799 |

The last window is the most recent ~2 months in full, which is what covers the client
versions in use today. The earlier windows are spread across the chain's history so that
older client generations are represented too. A window at 100,000 – 200,000 was scanned
and contained no `UserOperationEvent` at all; the earliest passkey operation found is at
block 1,000,377.

For each operation the replayed bytes are
`abi.encodePacked(userOpHash, signaturePayload, publicKey.x, publicKey.y)`, where
`userOpHash` comes from the `UserOperationEvent` topic and `signaturePayload` is
`signature[3:]` from the transaction — the same two values the account passed on — and the
key is read from the account. Each part is taken from chain data rather than approximated,
but the assembly is this script's, not the account's: what it cannot see is a caller that
appends to it.

A misassembly does not pass quietly. Every record was mined, so the pre-fork parser
returned 1 for it on chain; a record the replay cannot verify under that parser means the
reconstruction is wrong, and `TestWebAuthnVerifyMainnetReplay` fails on it rather than
counting it. The run reported 34,300 of 34,300 verified pre-fork, so the harvested set
contains none.

## Re-run on 2026-10-07

Two things were re-measured on the morning of the mainnet activation, with the committed
`harvest_passkey_ops.js` against the same endpoint, and replayed through
`TestWebAuthnVerifyMainnetReplay` with `WEBAUTHN_OPS_FILE`.

The window the committed corpus does not reach — from the block after its last one to the
head at the time:

| Blocks | `UserOperationEvent`s | Passkey-owned senders | Passkey operations | Accounts | Rejected after the fork | Non-canonical |
|---|---|---|---|---|---|---|
| 6,021,489 – 6,201,887 | 4,958 | 52 | 1,421 | 37 | 0 | 0 |

So the eleven days of traffic between the first run and activation, which include the
client versions in use on the day, add nothing the rule rejects. 375 events from
passkey-owned senders in that window were ECDSA-signed and never reached `0x111`; none
were skipped for any other reason.

And the six non-nested windows whose records carry both tallies, re-harvested so that the
residual has its reasons. The operation and event counts reproduced the fixture's records
exactly, every operation replayed with zero regressions, and the breakdown is:

| Blocks | Events from passkey-owned senders | Replayed | `notPasskeySig` | `unmatched` | `shortSig` | `txUnavailable` | `undecodable` |
|---|---|---|---|---|---|---|---|
| 1,000,000 – 1,010,000 | 3 | 3 | 0 | 0 | 0 | 0 | 0 |
| 1,500,000 – 1,510,000 | 779 | 701 | 78 | 0 | 0 | 0 | 0 |
| 2,000,000 – 2,010,000 | 7,998 | 5,059 | 2,939 | 0 | 0 | 0 | 0 |
| 2,500,000 – 2,510,000 | 4,262 | 3,500 | 762 | 0 | 0 | 0 | 0 |
| 3,000,000 – 3,010,000 | 3,033 | 2,282 | 751 | 0 | 0 | 0 | 0 |
| 3,500,000 – 3,510,000 | 3,520 | 1,170 | 2,350 | 0 | 0 | 0 | 0 |
| **Total** | **19,595** | **12,715** | **6,880** | **0** | **0** | **0** | **0** |

All 6,880 of the residual are `notPasskeySig`: the signature was 65 bytes, or its version
byte was not `0x01`, so the account took its ECDSA branch and the operation never reached
`0x111`. Not one event was a reconstruction failure, and not one transaction was missing
or undecodable. The reconstruction therefore reaches every passkey-signed operation in
those windows, and the share it does not reach is outside the fork's question. By the
account's rule — the ECDSA branch accepts the owner or the recovery address, and a
passkey-owned account has no owner — those operations were signed by their accounts'
recovery addresses. That this is a third of these accounts' mined operations is a fact
about how the wallet uses that path, worth asking its team about, and not about this
fork.

## Side finding: the word encoding was costing wallets signatures

`r` and `s` are drawn fresh for every assertion, so each has a 1-in-256 chance of a zero
top byte and about 1 assertion in 128 has one. Under the pre-fork parser such an
assertion is rejected, because `big.Int.Bytes()` drops the zero and the left-aligned copy
shifts the word up a byte.

Among the 34,300 replayed operations, the number with a zero top byte in `r` or `s` is
**0**, where **~267** would be expected if they were being accepted. What is measured is
the absence: assertions of that shape do not reach blocks, which is what the pre-fork
parser predicts, since it rejects them. What follows on the client — an error, a retry,
a second prompt — is inference from that rather than something these data observe, since
mined operations cannot show attempts that were never mined.

## Side finding: one account is currently unable to transact at all

`x` and `y` are fixed per account, so a key with a zero top byte fails *every* assertion
under the pre-fork parser and the account can never authorise anything.

Counting registered keys across the whole chain (`IncentivAccountInitialized`, blocks 0 –
6,022,576):

| | |
|---|---|
| Accounts initialised | 44,429 |
| Passkey-owned (`owner == 0`) | 1,836 |
| Passkey keys with a zero top byte in `x` or `y` | **1** |

`core/vm/testdata/webauthn/census_passkey_keys.js` reproduces all three, and
`CHECK_USAGE=1` also reports whether each flagged account ever transacted:

```
$ CHECK_USAGE=1 node core/vm/testdata/webauthn/census_passkey_keys.js
accounts initialised:                    44429
passkey-owned (owner == 0):              1836
keys with a zero top byte in x or y:     1  (expected ~14.3 if unfiltered)
  0x03aAe233DD173197DC6b8a6e0d2E2343Cf85dc2D  initialised at 1747397  UserOperationEvents=0
```

It is a read-only scan, not something CI re-checks; the numbers move as accounts are
created, so re-run it rather than trusting the ones printed here.

That one account (registered at block 1,747,397, zero top byte in `y`) has **never**
emitted a `UserOperationEvent` and holds a zero balance — consistent with a key that
cannot produce a verifiable assertion. Activating the fork makes it usable.

At 1,836 accounts, roughly 14 such keys would be expected. Observing 1 suggests the
registration path filters or retries keys that fail an initial verification, rather than
that the arithmetic is different than described. **Worth confirming with whoever owns the
wallet registration flow** — if it does filter, the count stays low; if it does not, more
such accounts will accumulate until the fork activates.

## Coverage limits

State them rather than round them off:

- **Windows, not the whole chain.** The chain carries millions of `UserOperationEvent`s and
  a complete pass is a multi-hour, multi-gigabyte job. The windows were chosen to cover
  all current traffic in full plus samples of each earlier era. The finding is uniform
  across every window, including the dense mid-chain ones.
- **Route 1 only.** Operations that reach `0x111` through `validateUserOp` leave calldata
  behind and can be replayed. ERC-1271 `isValidSignature` is a `view` call and the
  off-chain login verifier never touches a block, so neither leaves a trace to harvest.
  Both build their input with the same `abi.encodePacked` and the same payload from the
  same clients, so the encoding conclusion carries over; the *count* does not.
- **The inputs are rebuilt, not observed, and that is blind to one class of input.**
  `harvest_passkey_ops.js` constructs `abi.encodePacked(userOpHash, signaturePayload,
  publicKey.x, publicKey.y)` from the transaction's payload and the account's key. It does
  not read the `0x111` call. For almost any error in that assumption the replay would
  notice, because a wrongly rebuilt input fails the pre-fork parser and is counted as
  rejected by both. The exception is the class this fork is about: the pre-fork parser
  ignores everything after `y`, and the P-256 signature covers only `authenticatorData ‖
  sha256(clientDataJSON)`. So a caller that appended bytes after `y` verifies today, its
  rebuilt input verifies too, and the replay cannot tell them apart — while from
  activation its real calls would return 0. Which means the 500 committed inputs are
  canonical partly by construction: `32 + len(payload) + 64` ends at `y` by definition.
  What the replay *does* observe on real bytes is the other half of the rule, that the
  declared `authDataLength` and `clientDataJSONLength` account for the payload's own
  length; every operation satisfied it.

  Two things bound the gap. The account implementation has used that exact layout for its
  whole history — `contracts/incentiv/BaseIncentivAccount.sol:183` and
  `IncentivSignatureVerifier.sol:121` on `develop` both pack nothing after `publicKey.y`,
  and so does the first version of it (`deba99e`, 2025-07-03, as `hash, signature,
  publicKey.x, publicKey.y`). And the fixture records one EntryPoint revision, `v0.8`,
  for the windows that carry the field. Which implementation the deployed accounts
  actually run, and whether its bytecode is that source, was read off the chain on
  2026-10-07 — "Which implementation the accounts run" below — and both implementations
  in use pack that layout.

  The observation itself was then done on 2026-10-08 against a mainnet archive node (see
  "Traced-input comparison" below): the inner `0x111` input of real mined operations matches
  the rebuild byte for byte under the two earlier implementations (`0x43fb` and `0x045f`) and
  is canonical under the hardened one. So "the rule rejects nothing live traffic produces" is
  now observed for the whole input, not only the declared lengths.

- **Provenance of the window records.** The fixture's `source.sampledFrom` is 35,041,
  which is the eleven window records added together; one of them is nested inside
  another, so 741 operations are counted twice and the distinct total is the 34,300 in
  the table above. Four of the windows, 21,585 operations, were harvested with an
  earlier revision of `harvest_passkey_ops.js` and carry no `entryPointRevisions`, so
  the committed script does not reproduce those records exactly. The harvest also did
  not record, per window, *why* the operations it did not reconstruct were skipped. The
  residual itself can be measured in part. Over the **six non-nested windows** that carry
  `entryPointRevisions`, those windows decoded **19,595** `UserOperationEvent`s from
  passkey-owned senders and produced **12,715** replayed records, so **6,880 — 35% — were
  not replayed**. Both tallies are over passkey-owned senders only, so that gap is not
  other accounts.

  It bounds the gap *within decoded transactions* and not the unreplayed share overall.
  `entryPointRevisions` is incremented only after a transaction has been fetched and
  decoded, so events counted as `txUnavailable` (the node no longer serves the
  transaction) or `undecodable` (its top-level call is not `handleOps` or
  `handleAggregatedOps` — a bundler reaching the EntryPoint through a wrapper, say) are
  in neither 19,595 nor 6,880, and how many there were is not recorded either.

  It is three things the harvest separates and the fixture dropped: `notPasskeySig`, an
  operation from a passkey-owned account signed by another path — the version byte at
  `signature[0]` selects it, and only `0x01` reaches `0x111`, so these are outside the
  question rather than missing from it; and `unmatched` and `shortSig`, which are
  reconstruction failures and would be a real gap.

  Which of the three accounts for the 6,880 was not recorded by the original harvests,
  and the first bucket is narrower than it looks. `initialize` requires
  an owner **or** a public key and not both
  (`contracts/incentiv/BaseIncentivAccount.sol:119`), so a passkey-owned account has
  `owner == address(0)`; and the `version == 0` branch at `:172` accepts a signature only
  from `owner` or from a non-zero `recoveryAddress`. An operation that emitted a
  `UserOperationEvent` passed that check, so every `notPasskeySig` event in this count was
  signed by its account's recovery address. The re-run on 2026-10-07 established that this
  is all of them: a third of these accounts' mined operations were recovery-signed, and
  `unmatched` — a reconstruction failure, and a real coverage gap — is zero in every
  window.

  `sample_passkey_ops.js` now carries `skipped` into the fixture, so a fresh harvest
  records the breakdown, and all six windows were re-harvested on 2026-10-07; "Re-run on
  2026-10-07" above has the table.

  One more thing the harvest got wrong until now, which bears on these counts:
  `notPasskeySig` was decided by the version byte alone, while the account checks the
  length first — a 65-byte signature is plain ECDSA whatever `signature[0]` says
  (`BaseIncentivAccount.sol:161`). So a 65-byte recovery signature starting with
  `0x01`, one in 256 of them, was classified as a passkey operation and rebuilt as a
  passkey input. The replay would have failed on it — such a record verifies under neither
  parser — and it did not, so the harvested set contains none; the classifier is fixed for
  the next harvest.
- **Keys read at head.** `publicKey()` was read at the current head, not at each
  operation's block. An account that rotated its key would show up as rejected under the
  pre-fork parser, which is reported separately and was 0 — so no record in the set is
  affected.
- **The sample has an expiry date.** This says what clients produced up to 2026-09-26. Any
  client released after that is outside it. Re-run this before activating on each network.

## Which implementation the accounts run

Read on 2026-10-07 at head 6,201,971, over every passkey-owned account rather than the 403
in the sample: `IncentivAccountInitialized` with `owner == 0` names 1,836 accounts, and
`eth_getCode` on each of them returns one of two byte strings, both the OpenZeppelin
`BeaconProxy` runtime with a beacon address baked in.

| Beacon | Accounts | Of the 403 sampled senders | Implementation (`Upgraded` event, block) |
|---|---|---|---|
| `0xb9a8…eb6e`, owned by the DPoW Governance Safe | 1,822 | 401 | `0x7f9f…74c7` since 6,047,282; `0x43fb…fc6c` before it, from 1,017,503 |
| `0xd350…faa7`, the factory in `deployments/incentiv_24101` | 14 | 2 | `0x045f…a867` since 113,054 |

`0x7f9f…74c7` is the hardened `IncentivAccount` revision in the contracts repository,
not yet on its main branch: its runtime bytecode matches that revision's build byte for
byte apart from the immutable registry address and the metadata hash. That implementation checks the payload's
declared lengths against its length before calling the precompile
(`WebAuthnPayload.isCanonical`) and builds the call as `abi.encodePacked(message, payload,
x, y)`, so since block 6,047,282 the 1,822 accounts behind it have refused a non-canonical
payload on the contract side and handed `0x111` nothing after `y`. `0x045f…a867` is the
`IncentivAccount` embedded in the committed factory deployment artifact, with its four
immutable slots filled in; its source is in that artifact's `solcInputs` and packs
`message, signaturePayload, publicKey.x, publicKey.y` with no check in front. So every
passkey-owned account on the chain today builds the input in the layout the harvest
rebuilds, and 14 of them rely on this fork alone for the canonicality check. Those 14
cannot be moved onto the hardened implementation instead: their beacon's owner is the
deterministic deployment proxy at `0x4e59…956c`, a contract that only forwards `CREATE2`
and cannot call `upgradeTo`, so for them the fork is the only remedy there is.

That is the layout half of the traced-input comparison, answered from bytecode and source.
The observation half — the inner call's `input` read off mined operations — was done on
2026-10-08 against an archive node; see "Traced-input comparison" below. Two limits on this inventory: the first beacon's
owner can change the implementation at any time, so it is a reading at one head, to repeat before
activation; and the operations in the windows above were mined under `0x43fb…fc6c`, whose
source this inventory did not locate — the replay's result about them is about the
payloads wallets produced, which does not depend on which implementation forwarded them.

## Traced-input comparison (2026-10-08)

The traced-input observation: for real mined operations, the inner `0x111` call's
`input` read with `debug_traceTransaction` (`callTracer`) on a mainnet archive node and
compared with what the harvest reconstructs. Read-only, over an SSH tunnel to an archive
node that serves `debug` under the `trace-indexer-v1` profile. The comparison is
`core/vm/testdata/webauthn/trace_passkey_ops.js`, which samples the committed fixture, and
`trace_passkey_ops_test.js` beside it reaches each of its verdicts against a stub node in CI.

The 20-op sample and the six recent operations span three deployed implementations,
determined per operation from the sender's beacon (the immutable EIP-1967 beacon slot) and
that beacon's `Upgraded` history at the operation's block:

- **`0x43fb...fc6c`** — what the Governance beacon (`0xb9a8...`) ran from block 1,017,503
  until the hardening at 6,047,282. 19 of the 20 sampled ops, each a distinct sender, blocks
  1,503,543 - 5,981,125. The implementation inventory could not locate this one's source, so
  the trace is the only check it has: all 19 fed `0x111` the reconstructed input byte for
  byte, ending exactly at `publicKey.y`.
- **`0x045f...a867`** — the factory-artifact implementation behind the `0xd350...` beacon,
  used by the 14 dormant accounts, and the only one this fork alone protects on the contract
  side. All three ops the committed fixture holds under this beacon matched byte for byte
  (blocks 1,000,377, 1,002,394, 1,002,701; two senders) — the sampler reached the first and
  the other two were traced directly, since this implementation has no contract-side check of
  its own.
- **`0x7f9f...74c7`** — the hardened implementation, from block 6,047,282, which rejects a
  non-canonical payload on the contract side as well. Six recent ops near head, each a
  canonical `0x111` input returning 1:
  `0x51c55847b9c854a7f1fb4628566014764d4afddedb81da475cf47d43647dc3be`,
  `0x58b938b9963727fa2459c888e750b89295f8c129e0ab8c8598d44d9819f921b5`,
  `0x24ad20a4acdbc53a234896977066f03dd6bff4a8c76a8e4ef2c446f41a406b6f`,
  `0xa70d98a0c173a7ea11a7c344933a7cde53508e037ff4ce8a4ae8f5eb22f735ee`,
  `0x3927b1521e1e11a29c02e42f707a5a5705d005db0585e102070f6a686ffa0993`,
  `0xa5d70864c8843b7aab6933571c550546a4d197f2bd493984e83a412167a22cc6`.

Every traced operation across the three fed `0x111` the input ending exactly at `publicKey.y`,
with nothing after it, so the input WebAuthnStrict requires is the input real callers already
produce. That is the observation half, with the `0x045f` coverage as noted.

## Reproducing it

Both harvest scripts are in `core/vm/testdata/webauthn/`. They need Node and `ethers`
v5; this repository ships no `package.json`, so run them from the contracts repository,
which already depends on it, or install it alongside them.

The committed fixture is a 500-operation sample of the same data and runs in CI:

```
cd core/vm && go test -run TestWebAuthnVerifyMainnetReplay -v .
```

For the full set, harvest and replay through the same test:

```
FROM_BLOCK=… TO_BLOCK=… OUT=ops.json node harvest_passkey_ops.js
cd core/vm && WEBAUTHN_OPS_FILE=/path/to/ops.json go test -run TestWebAuthnVerifyMainnetReplay -v .
```

The test wants at least 500 operations from any fixture, so that a harvest which errored
early cannot read as a clean run. A smaller file — a single captured operation during an
incident — has to say so with `WEBAUTHN_OPS_MIN=1`, which is the only thing that lowers
the floor.

`core/vm/testdata/webauthn/README.md` has the details, including where the harvesting
scripts live.

## Conclusion

The exact-length rule rejects nothing in what this replay reconstructed — 34,300
operations over 403 accounts and 5 million blocks, zero regressions — and the
word-encoding fix only ever turns a rejection into an acceptance, so it can only widen
what verifies.

That is not quite the same as "nothing in live traffic", and the difference is now one
observation rather than two unknowns. The residual has its reasons: the 6,880 events not
reconstructed were ECDSA-signed and never reached `0x111`. The layout the reconstruction
assumes is the one both deployed implementations build, read off their bytecode and
source, and 1,822 of the 1,836 passkey-owned accounts already refuse a non-canonical
payload on the contract side. Reading the inner call's `input` off mined operations — the last observation — was done on
2026-10-08 (see "Traced-input comparison"): it matches the rebuild byte for byte on the two
earlier implementations and is canonical on the hardened one. On the compatibility question
the fork is safe to activate as far as this replay can see.

That leaves the *coordination* question, which this document does not address:
every node has to be running a binary that knows `webauthnStrictTime` before it fires. See
[rollout.md](rollout.md).
