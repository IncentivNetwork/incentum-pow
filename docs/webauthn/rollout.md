# WebAuthnStrict — rollout

Written for validators, node operators and whoever runs the release.

`WebAuthnStrict` changes what the `0x111` precompile returns for non-canonically encoded
input, so it is a hard fork. A node that reaches the activation timestamp on a binary
that does not know the fork will follow a different chain. This document is the sequence
that avoids that.

See [README.md](README.md) for what the fork changes and
[replay-validation.md](replay-validation.md) for the compatibility measurement: the rule
rejects nothing in the 34,300 mainnet operations the replay could reconstruct. The two
steps from there to "nothing in live traffic" are the residual and the traced input, both
gates below.

## Activation timestamps

| Network | `webauthnStrictTime` | UTC |
|---|---|---|
| devnet (12730) | `1791291600` | 2026-10-06 13:00:00 |
| testnet (28802) | `1854446400` | 2028-10-06 12:00:00 (automatic if unchanged) |
| mainnet (24101) | `1791504000` | 2026-10-09 00:00:00 |

The earlier 07:00 and 11:00 UTC devnet windows passed before installation. The
operator selected 13:00 UTC for activation, and both known devnet nodes were updated
before that boundary. For a future activation, record the operator agreement and
pre-upgrade inventory, install the reviewed build, then restart and check every node
before the boundary. Move the bundled timestamp before tagging if that cannot finish
in time. A node that stored an earlier time may remain on it; see "Per-node procedure"
below.

Before this upgrade, the operator inventoried two devnet Geth nodes, one mining and
one sync-only. Both ran `v1.11.8-3ddacd84` with `--incentiv-devnet`, no WebAuthnStrict
override, chain ID 12730, the expected genesis, and no `webauthnStrictTime`. Neither
had run this branch, so the old 07:00 UTC schedule was not installed on those
datadirs. Both nodes were then upgraded to `f4d09ba9` and reported the effective fork
time `1791291600`. After restart they were peered and advancing before activation;
afterward both continued on the same chain, confirmed by a shared post-fork block.
After activation, direct `eth_call` to `0x111` returned `1` for a canonical input from
the committed fixture and `0` for the same input with one trailing byte. This checks
the live precompile. A UserOperation carrying a non-canonically encoded
`signaturePayload` was then put through `EntryPoint.handleOps`: validation reverted with
`AA24 signature error` under `eth_call`, and once as a sent transaction, which mined with
receipt status `0`. No balance changed; the submitting account paid the fee and consumed
its nonce. The positive side was then checked directly against the live precompile: on
2026-10-08, at devnet head 2285809, a freshly generated P-256 key signed a real WebAuthn
assertion and `eth_call` to `0x111` returned 1 for the canonical input and 0 for the same
input with one trailing byte. That exercises the one element the fork changes, on the live
chain, with a real assertion rather than the committed fixture. The full owner-signed
`UserOperation` through `EntryPoint` was then run on devnet too (next item): a passkey
account the factory deployed validated a real assertion through the account envelope and
the nested `0x111` call, and the operation executed.

Of those two checks only the first distinguishes the parsers. The direct call returns `1`
before activation and `0` after it for the same bytes, which is the change itself.
`AA24 signature error` is the account's generic signature failure: a payload that is wrong
in some other way — a mismatched `userOpHash`, chain id or EntryPoint, or a mislaid field —
produces it too, so the rejection through `handleOps` shows the path refuses the operation
without saying which rule refused it. What would attribute it to this fork is the inner
call, and that has not been done: it is an unchecked item below, not a result recorded here.

Two ways to do it when it is done. The stronger one needs no second trace: take the inner
`0x111` call's `input` from `debug_traceTransaction` with `callTracer` over IPC and run
those bytes through both parsers in one process:

```
cd core/vm && WEBAUTHN_INPUT=0x<input> WEBAUTHN_EXPECT=1,0 \
  go test -run 'TestWebAuthnVerifyCapturedInput$' -v .
```

The test logs what each parser returned, and `WEBAUTHN_EXPECT=1,0` — accepted before the
fork, rejected after it — is the shape the attribution has, so it fails on anything else.
Same bytes, both answers, no state to match. The replay harness (`WEBAUTHN_OPS_FILE`) is
the wrong tool here: it is a compatibility check over mined mainnet operations, so it
refuses a file from any other chain id and counts exactly this outcome as the regression it
exists to catch. One thing about the trace: a node that is not an archive node re-executes
at most `reexec` blocks (128 by default, `eth/tracers/api.go`) to reach the state before the
transaction, so for an older transaction pass a larger `reexec` in the trace config or use
an archive node.

The other is two `debug_traceCall`s against the *same* block — the latest will do, since
the account, the beacon and the registry are read as they stand — differing only in
`blockOverrides.time`: `BlockOverrides` reaches the precompile set through the block
context, since `NewEVM` derives the chain rules from `blockCtx.Time`
(`eth/tracers/api.go`, `core/vm/evm.go`).

What does not work is comparing `debug_traceTransaction` with `debug_traceCall`, which is
the obvious thing to reach for. They load different state — `StateAtTransaction` before the
transaction inside its block against `StateAtBlock` after the referenced block — and the
time override does not change which. Anything else in that block touching the account, the
beacon or the registry would land in the difference and look like the fork.

The identifiers from that run are kept in the operator's own notes rather than in this file, which
describes the rule and how to check it, not one incident's transaction hashes.

A node that stored an earlier bundled activation time — from a build that carried a
different time before this one — has been strict since that time passed. Upgrading such a
node does not bring it back on its own: its head is past the stored time, so taking the
bundled time would rewind, and a flagless start refuses that and only warns, with the old
value in the line. Restart it with `--incentiv-mainnet`, which takes the bundled time and
rewinds to before the old one; read any earlier bundled value on the roll-call as exactly
this case.
Of the mainnet gates below, the replay re-run, the residual breakdown, the implementation
inventory, the traced-input comparison and the valid devnet operation were completed and are
recorded in [replay-validation.md](replay-validation.md); the node inventory is complete, and the roll-call
is the one gate still open, with hours rather than days to close it, and less lead time than this
document asks for further down; the operator chose the window knowing that. The roll-call is the gate that decides whether it
holds, and the override below is how it is held if it cannot — on every node already
carrying the release, since a node still on the previous one has no such flag and no fork
to hold, until a binary with a moved timestamp is everywhere. A fleet that is only partly
upgraded at the timestamp is the case to avoid, not to manage: from then on the upgraded
nodes refuse new connections from the rest, and the first non-canonical `0x111` call in a
block splits the chain.
The distant testnet date postpones testnet work, but it is an executable consensus
schedule, not an inert placeholder. It activates automatically if unchanged. Agree its
window with the testnet operators before any rollout, or remove or move it well before
2028-10-06; that decision is tracked as a follow-up outside this document.

The reason to settle an armed window early rather than at tagging time is that the cost
is not symmetric. Moving a window before the tag is an edit to one file. Moving it after means
`--override.webauthnstrict` on every node until a corrected binary is built and rolled
out, which is the stop-gap this document argues against twice. For a later activation
release, tag, build, install and restart every devnet node before its newly agreed
timestamp, then check its transition before a separate mainnet activation release.
Each armed window must leave every validator and RPC node time to upgrade and restart.
A devnet whose miners are split across the boundary is a failed rehearsal.

A window that slips is corrected with `--override.webauthnstrict <timestamp>`, which
moves or arms the fork on a node already running the release — it cannot disarm it, see
below. **Do not
correct a window by re-tagging**: nodes already running the first tag keep the original
timestamp, so the network ends up split three ways — old binary, tag one and tag two —
instead of two. Changing the timestamp *after* activation is not possible at all
without rewinding the chain (see "Point of no return").

**The override is a stop-gap, not a new schedule, and it has to stay on the command
line.** It is re-applied at every start, with or without a network flag, and dropping it does
not put the node back where it was. Before the bundled timestamp a no-flag start resolves
to the bundled schedule again, so the node returns to it. Once the head is past that
timestamp the adoption is refused instead — taking it would rewind — so the node keeps
the timestamp its stored config already holds, which is the override's. It does not come
back to the fleet's schedule and it does not rewind; it carries on quietly on its own
one, with a warning naming the timestamp it will use. A unit file edited, a binary
upgraded, an operator who did not know the flag was there: any of those leaves that node
on a different activation moment from the rest of the fleet. So if a window moves, hold the line with the override, ship a binary
carrying the corrected timestamp, and keep the override on every command line until that
binary is everywhere. The roll-call has to check the timestamp, not just the version.

It can postpone the fork but not turn it off: the flag takes a `*uint64`, so there is no
value meaning "unset", and `0` is rejected by `CheckConfigForkOrder` for preceding the
timestamp fork configured before it — `dynamicMinBaseFeeTime` on mainnet and devnet,
`shanghaiTime` on testnet. A far-future timestamp is the only lever,
and it changes the fork ID's next-fork field, so a node carrying one will not peer with
the rest of the fleet once they diverge.

## What else this release carries

This change is based on `develop`, which is already past `v1.11.10`. In the
2026-09-26 survey, miners advertised `v1.11.8` in `extraData` and both public RPC
endpoints reported `Geth/v1.11.8-stable-af5b5474`, while other nodes had already
taken `v1.11.10`. Check current versions on the roll-call: a miner still on `v1.11.8`
will receive a `v1.11.10` upgrade along with this fork. One change in that release
is operator-visible on its own:

- **`debug` RPC is fail-closed in `v1.11.10`.** A node with `debug` in `--http.api` or
  `--ws.api` and neither `--{http,ws}.debug-profile` nor `--{http,ws}.allow-unsafe-debug`
  **refuses to start**. Archive nodes are the ones carrying `debug`. Every unit file has
  to be checked against `docs/dpow/DPOW_NODE_OPERATOR_GUIDE.md` §3.1 before the binary
  goes out, or nodes will not come back up after the upgrade — before the fork is
  anywhere near activating.
- **JSON-RPC batches are capped at 100 requests in `v1.11.10`**, on HTTP and WS alike:
  `node.DefaultBatchLimit` is 100, and `rpc/handler.go` answers a larger batch with
  `-32600 batch too large` rather than trimming it. An indexer or wallet backend that
  sends larger batches — Blockscout's fetchers are commonly set to several hundred —
  stops working after the upgrade. Raise `--http.rpc.batch-limit` and
  `--ws.rpc.batch-limit` on the RPC nodes such clients use (`0` is unlimited), or have
  the clients batch smaller, before the binary goes out.
- `v1.11.10` also brings the `monitor` RPC namespace and the lint and workflow
  work in that release.
- `geth import` opens the database **read-write**, so the guard above does not apply to
  it and nothing stops it resolving a different schedule from the node's. On a node being
  carried on `--override.webauthnstrict`, running `geth import` without the same override
  resolves to the bundled timestamp, and if the node is already past it that is a
  `ConfigCompatError`, which `NewBlockChain` acts on by rewinding. Pass the override to
  `geth import` too, or do not run it on an overridden node.
- `geth export` cannot open a datadir this binary has not started on yet. The setup
  path persists whatever config it settles on, and `export` opens the database
  read-only, so the write cannot happen. This is not new — it happens for any release
  that changes a bundled config — but this is such a release. It no longer takes the
  process down from inside `rawdb`: `export` works out the same configuration a start
  would settle on, overrides included, and refuses with a message naming the fix. Run
  `geth export` after a normal start, or with the binary that wrote the datadir, and
  pass it the same `--override.webauthnstrict` the node runs with, if any.

Three consequences for this rollout. The upgrade step is a `v1.11.10` upgrade for any
node that has not already taken it, so it wants that release's own checklist alongside
this one. The roll-call has to confirm nodes came back up at all, not just that they
carry the right timestamp. And it has to record the version each node came back on,
because which nodes face the `debug` break depends on where each one started from —
`web3_clientVersion` answers that, and for a miner so does the `extraData` of a block
it produced.

Those answer which release a node is on. Whether its binary carries this fork is a second
question. `params/version.go` reads `1.11.11-stable` from the version bump on this branch,
so a release build answers it by number: `web3_clientVersion` reports `Geth/v1.11.11-…`,
and a miner's `extraData`, which `makeExtraData` builds from major, minor and patch only,
reads `1.11.11` as well. A build from an earlier commit of the branch — the devnet
rehearsal binary is one — still reads `1.11.10-stable`, the same as the release without
the fork, and for those only the commit answers. `cmd/geth` builds the client version
with `VersionWithCommit`, and `internal/version` takes the commit from the build script's
ldflags or, failing that, from `vcs.revision` in the build metadata — so a build made the
usual way reports something like `Geth/v1.11.10-stable-f4d09ba9` and the suffix identifies
it. A build made without that metadata reports the bare version and identifies nothing,
which is the case to watch for rather than assume away.

Identifying the binary is still not the same as knowing what it will do.
`admin_nodeInfo.protocols.eth.config.webauthnStrictTime` is the field that says which
schedule the running node is on — including when an override rather than the bundled config
put it there — which is why the roll-call collects the timestamp and not just the version.

## Before the release is tagged

- [x] The devnet operator selected 13:00 UTC. Both known nodes were inventoried,
      upgraded and checked before activation; both reported the expected effective
      fork time and continued on the same chain afterward. The rehearsal ran an
      untagged build from this branch, which is what a rehearsal is for; the tagged
      release still has to go through the items below. Mainnet is armed for 2026-10-09
      00:00 UTC.
- [x] A valid passkey assertion is accepted by the strict `0x111` on live devnet. On
      2026-10-08, at devnet head 2285809 (chainId 12730, already past its 13:00 UTC
      activation), a freshly generated P-256 key signed a real WebAuthn assertion and
      `eth_call` to `0x111` returned 1 for the canonical input and 0 for the same input
      with one trailing byte. This is the positive half the rejection checks did not cover,
      run against the live chain with a real assertion rather than the committed fixture,
      and it exercises the one element the fork changes. The check is
      `core/vm/testdata/webauthn/live_passkey_check.js`, read-only, and
      `core/vm/live_passkey_check_test.go` runs the input it packs through both parsers in
      CI, so the layout it signs is the one the parsers see. The account's
      signature-envelope parse and the EntryPoint plumbing are unchanged by the fork and
      the contracts' test-suite covers them with a mocked precompile; the combination not
      yet exercised end to end — a real assertion through the account and EntryPoint into
      the real `0x111` — is the next item.
- [x] A full owner-signed passkey `UserOperation` ran through `EntryPoint` on devnet under
      the strict parser, and executed — not just the absence of a revert. On 2026-10-08 a
      fresh P-256 key was generated, the factory deployed a passkey account owning it, and
      `EntryPoint.handleOps` was simulated with `eth_call` (account funded by a state
      override, so `AA21` could not mask the signature check) and then sent once. The
      transaction mined at block 2293899 with receipt status 1 and a `UserOperationEvent`
      with `success=true`, so the account computed `userOpHash`, parsed its own signature
      envelope and made the nested call into the real `0x111` — the one combination the
      precompile check above and the contracts' mocked-precompile tests do not cover
      together. Self-funded from the deployer EOA, no paymaster; the tx hash is in the pull
      request. The execution phase was a no-op (empty `callData`), which still runs the full
      validate-and-execute flow.
- [ ] The rejection seen on devnet is attributed to this rule rather than to signature
      validation in general: the inner `0x111` input is pulled from a trace and run through
      both parsers (`TestWebAuthnVerifyCapturedInput` with `WEBAUTHN_EXPECT=1,0`), or two
      `debug_traceCall`s on one block differ only in `blockOverrides.time`. Record the
      result without the identifiers. `AA24` on its own does not establish which rule
      refused the operation. Not a tag gate, and left open as a record: the behavioural tests already run the same
      malformed bytes through both parsers — the pre-fork one accepts them, the strict one
      returns 0 — and `live_passkey_check_test.go` shows the same split on a real signature (1 with no trailing byte, 0 with one), the mainnet replay confirming only that real inputs are canonical and accepted by both parsers, so the distinction a trace
      would attribute is established off-chain; the live devnet check adds the strict `0x111`
      returning 1 for canonical input and 0 for a trailing byte. If it is ever attributed
      on-chain, the tool is the trace input through `TestWebAuthnVerifyCapturedInput`, named
      above, not `trace_passkey_ops.js`, which samples the mainnet fixture, refuses another
      chain id, and compares inputs rather than running the parsers.
- [x] `params/version.go` is bumped to `1.11.11` on this branch, so that a node carrying
      the fork differs from `v1.11.10` by its version number and not only by a commit
      suffix the build may or may not carry — and so that a miner's `extraData`, which
      holds no commit, distinguishes them at all. Tag the release `v1.11.11-<theme>-mainnet`
      from the merge commit that contains the bump (`docs/release-process.md`). The
      effective timestamp over IPC stays the check on what a node will actually do.
- [x] Testnet's automatic timestamp is agreed or removed rather than left to fire. It is
      set to 2028-10-06 12:00 UTC in the bundled config, which activates the fork there
      with no rollout behind it. On 2026-10-08 the operator decided to leave it in this
      release and to set testnet's date in a change of its own when testnet is actually
      upgraded, which is the rollout that timestamp has to be chosen for; until then it
      is the distant placeholder it reads as.
- [x] Any node being carried on `--override.webauthnstrict` is on the roll-call as such,
      and a binary with the corrected bundled timestamp is scheduled to replace it. None:
      every mainnet node read on 2026-10-08 starts with its network flag and no override.
- [x] The set of nodes that must upgrade is written down: every miner/validator and
      RPC node. Check whether any bundler actually runs on this network; if none does,
      record "none" rather than adding a service for this release. A running bundler
      should use an upgraded RPC node because an old one answers `eth_call` and
      `eth_estimateGas` with the pre-fork parser and can accept an operation the miner
      then rejects. Bundlers do not determine the chain's consensus schedule. Written
      down on 2026-10-08 in the operator's inventory, cross-checked against the
      infrastructure inventory: fourteen geth nodes — four miners (three of them currently producing blocks), six RPC
      nodes and four archive nodes — and three bundler hosts, each of which runs behind an
      RPC node from that set and so needs that node upgraded. Hosts that were
      decommissioned, and one peer that is not under the operator's control and does not
      produce blocks, are outside the list; the fork ID keeps that peer out once its
      connection breaks.
- [x] Replay validation re-run against a head close to the release date: on 2026-10-07
      the window 6,021,489 – 6,201,887, which starts where the committed corpus ends,
      replayed 1,421 operations from 37 accounts with zero regressions and zero
      non-canonical payloads, and the six measured windows re-harvested the same morning
      replayed identically ([replay-validation.md](replay-validation.md), "Re-run on
      2026-10-07").
- [x] Which implementation the deployed accounts run is read off the chain rather than
      assumed. On 2026-10-07, 1,822 of the 1,836 passkey-owned accounts are behind a
      beacon whose implementation has been the hardened account build since
      block 6,047,282 — it refuses a non-canonical payload on the contract side
      (`WebAuthnPayload.isCanonical`) and packs nothing after `publicKey.y` — and the
      other 14 run the original implementation from the committed factory deployment,
      which packs the same layout and checks nothing, and whose beacon is owned by the
      deterministic deployment proxy, so it cannot be upgraded. So this fork is the only
      remedy for those 14, closes the gap for whatever implementation comes next, and is a
      second check for the rest. [replay-validation.md](replay-validation.md), "Which
      implementation the accounts run", has the addresses and the method; the first
      beacon's owner can change its implementation, so re-read it before activation.
- [x] The replay residual is resolved into its reasons. The six non-nested windows that
      record both tallies were re-harvested on 2026-10-07 with the committed script, which
      keeps the breakdown: of the 6,880 events from passkey-owned senders that were not
      replayed, all 6,880 are `notPasskeySig` — the signature took the account's ECDSA
      branch and never reached `0x111` — and `unmatched`, `shortSig`, `txUnavailable` and
      `undecodable` are 0 in every window. The reconstruction reaches every passkey-signed
      operation in those windows; what it leaves out is outside the fork's question.
      [replay-validation.md](replay-validation.md), "Re-run on 2026-10-07", has the table.
      A passkey-owned account has no owner, so those operations were recovery-signed;
      that it is a third of them is a question for the wallet team, not a gate here.
- [x] The traced `0x111` input comparison was done on 2026-10-08 against a mainnet archive
      node (`debug_traceTransaction`, `callTracer`), across the three implementations the
      sampled operations ran under, each identified from the sender's beacon and its
      `Upgraded` history: `0x43fb...fc6c` (19 ops) and `0x045f...a867` (all 3 fixture ops)
      matched the reconstructed input byte for byte, and `0x7f9f...74c7` (six recent ops) fed
      canonical input returning 1. No traced op carried bytes after `publicKey.y`. The only
      implementation with no contract-side check, `0x045f...a867`, is fully covered by the
      fixture's three ops (two senders). `docs/webauthn/replay-validation.md`, "Traced-input
      comparison", has the method and hashes.
- [x] How every live node is initialised and started is confirmed from its unit file,
      not assumed: whether it carries a network flag, and what its stored chain config
      holds. A node whose stored config disagrees about the block-numbered forks keeps
      that config and never activates (see "Per-node procedure"), so this
      decides whether the timestamps below are safe. `admin_nodeInfo ->
      protocols.eth.config` on each node answers the second half. Done on 2026-10-08
      for all fourteen: every unit starts the node with its network flag, every stored
      config is the bundled mainnet one with no `webauthnStrictTime` yet — the shape the
      adoption handles — RPC is bound to loopback everywhere, and the four archive nodes
      already run `v1.11.10` with the `debug` profile the fail-closed rule requires. The
      filled-in roll-call stays with the operator, as "Roll-call" below asks.
- [ ] The release binary is built from a clean checkout of the tagged commit, or taken
      from a CI artifact — not from a working copy. A working copy can hold an
      uncommitted change to a consensus rule and still produce a binary that looks like
      the reviewed one.
- [ ] `go test ./core/vm/... ./params/ ./core/forkid/ ./core/` green on the release commit,
      then `go test ./cmd/geth/ ./cmd/utils/` as a second command, and
      `go run build/ci.go lint` clean. The two commands are deliberate: the `cmd/geth`
      tests spawn geth subprocesses with five-second startup budgets, so running that
      package beside `core` on one machine times out the `TestDebugProfile*` cases on
      startup and the red says nothing about the release. CI shards the packages, which
      keeps them apart on its own.
- [ ] **CI** green on the release commit, not just a local run. The two are not the same
      check: a working copy with CRLF endings fails `gofmt`, `goimports` and
      `TestVerification/minisig` on files nothing here touches, and a local run skips the
      shard layout CI uses. `ethclient`'s `TestEthClient/TxInBlockInterrupted` is a known
      flake on this repository, diagnosed separately; if it is the only failure, re-run the job and record that
      the re-run passed rather than reading the first result as a branch problem.

## Sequence

Devnet is the only live rehearsal before mainnet. Its node upgrade and chain
transition completed at 13:00 UTC, and direct precompile calls returned `1` for
canonical input and `0` for over-long input. A full owner-signed passkey UserOperation
through EntryPoint executed there on 2026-10-08, and the traced-input comparison on mainnet
was done the same day. Mainnet is armed for 2026-10-09 00:00 UTC.

1. **devnet.** Both known nodes were upgraded before 13:00 UTC; block production,
   peering, a common post-fork block and direct precompile acceptance/rejection
   were confirmed. A UserOperation with a non-canonical `signaturePayload` was rejected
   through `EntryPoint.handleOps`, both in a read-only simulation and in a sent transaction
   that mined with receipt status `0`. The positive side was confirmed on 2026-10-08
   against the live precompile — a real P-256 assertion through `0x111` returned 1
   canonical, 0 with a trailing byte (head 2285809). The full owner-signed `UserOperation`
   through `EntryPoint` was also run on devnet: a factory-deployed passkey account's
   operation executed through `handleOps` (block 2293899, `UserOperationEvent success=true`).
2. **mainnet, this release, 2026-10-09 00:00 UTC.** Tag from the merge commit that
   carries the version bump — the `release` job in `build.yml` runs on any `v*` tag and
   publishes a GitHub Release with binaries, so tag only when the rollout is ready to
   proceed — build from a clean checkout, upgrade every node in the list above, run the
   roll-call, *then* let it pass. The
   fleet's deployment role installs the client from a GitHub release of a named
   repository, with a credential, and checks `geth version` against an expected commit;
   so it is given the repository, the tag, and the release's commit, and the
   nodes are not rebuilt from source in place. Not every node is in that role's
   inventory — the block producers are not — and a node outside it takes the same
   release archive by hand: verify the published checksum, keep the previous binary
   beside the new one, replace the file the unit starts, restart the unit, and read the
   version and `webauthnStrictTime` back over IPC before moving to the next node. A
   `git pull` and rebuild on the host is not a route for any node, since what it builds
   is not the release that was checked. If the roll-call cannot finish before the
   timestamp, hold the window with `--override.webauthnstrict` on every upgraded node
   rather than let it pass with a node missing — a node not yet upgraded has no flag to
   pass and nothing to hold — and "Point of no return" below says what that costs.

## Per-node procedure

An armed activation timestamp must be in the future when the binary is installed, so
`isForkTimestampIncompatible(nil, &future, headTime)` is false and the existing chain
database is accepted as it is. Mainnet's bundled time is 2026-10-09 00:00 UTC, ahead of
the head of any node upgraded before it; testnet's distant timestamp does not call for a
rollout now. Devnet's 13:00 UTC time
was ahead of the heads on both inventoried nodes when they were upgraded. A different
datadir that had already stored and passed the old 07:00 UTC time may keep that value
on a no-flag start; a network-flag start can instead return `ConfigCompatError` and
rewind. Check every node's effective config and head before assuming it took the new time.

A node started without a network flag used to reuse the chain config stored in its
database, so a newly bundled timestamp was never applied. That is no longer the case
for these chains: the stored genesis hash and chain id together identify the network —
the hash alone would not, since mainnet and devnet share one, and the id alone would
claim any private chain using it — and the bundled schedule is then applied either way.
A unit file with no network flag therefore picks up the schedule; the run script in
`TESTNET_V2_DEPLOYMENT.md`, which carries `--networkid` and nothing else, is that shape.
The schedule is not the whole of a node's configuration, though: the unit in
`docs/dpow/DPOW_NODE_OPERATOR_GUIDE.md` §3.2 carries neither a network flag nor
`--networkid`, and `ethconfig.Defaults.NetworkId` is 1, which no `SetEthConfig` branch
derives from the stored config — so a node in that shape runs on network id 1 and peers
with nothing. Whatever this section says about schedules, that is not a form to copy for
an Incentiv node; fixing the template itself is tracked as a follow-up and belongs to
that guide rather than to this branch.

With one exception, and it is the reason to read a node's log after the upgrade rather
than assume. `checkCompatible` compares none of `feePoolBlock`, `minBaseFeeBlock`,
`minBaseFeeChangeHeight`, `zeroRewardBlock`, `fastBlock` or
`irregularStateChangeHeight`, so taking the bundled config over a stored one that
disagrees about them would apply those rules to blocks already mined, silently. When
they disagree the stored config is kept whole instead, and the node logs:

```
WARN Stored chain config disagrees with the schedule this binary ships for this network
```

Such a node does not activate WebAuthnStrict, so it must not be left in that state
across the boundary. It is also not following the network already — the settings in
question have been in force for months — so the warning is a sign the datadir was built
from something other than this network's genesis specification. Starting once with the
network flag adopts the bundled schedule, but it is not a repair for a chain that has
already been built under other rules: `CheckCompatible` does not compare these settings,
so no rewind is computed from them — they are written over the stored ones and blocks already
mined without them are suddenly subject to them. For such a datadir the way back to the
network is a resync from genesis.

What this release does about that is narrow. A refusal that leaves the node *without*
`webauthnStrictTime` is a failed start, not a warning, once this network's bundled
activation is within 30 days or already behind — measured from the later of the node's
head and its clock, so a node restored from an old backup is judged by today's date and
not by where its chain stopped: such a node would
follow a different chain from activation, and a start that only warned left it running and
looking healthy until then. The log line is written first, with the same `cause` and `fix`
fields, and the error repeats them. Further out — testnet's window is in 2028, and still
to be agreed — the same refusal stays the warning it always was, so a stand that ran on
the previous release does not stop on this one for a fork two years away. A refusal that
leaves the node with a fork time of its own — one the stored config already carries, or
`--override.webauthnstrict` — is only warned about at any distance, since that node will
activate, at that time. A node that means to activate at a different time says so with
`--override.webauthnstrict=<future timestamp>`; the override cannot turn the fork off, and
a timestamp already behind the head rewinds the chain. A local stand built from the
published `genesis.json` cannot take the override at all: its stored config has no
`shanghaiTime`, and `CheckConfigForkOrder` refuses a `webauthnStrictTime` ahead of an
unset one, so its way out is a `chainId` of its own and a resync, as
`TESTNET_V2_DEPLOYMENT.md` advises anyway. The refusal says both. And this release does not gate
the network-flag path on the same comparison, because that is how this
chain ships block-numbered settings at all — `minBaseFeeBlock` and
`irregularStateChangeHeight` reached running nodes exactly that way — and a node picking
up the next one would be refused instead.

The fix that would close it properly is to add those six settings to
`ChainConfig.checkCompatible`, where `isForkBlockIncompatible` already draws the right
line: a setting introduced for a block still ahead of the head is accepted, one that
changes history produces a `ConfigCompatError` and a rewind. It is not in this branch on
purpose. On any node whose stored config lacks a setting that is already behind its head
it means a rewind to that block and a resync, so it has to be done knowing which nodes
those are — which is the inventory in the checklist above, and the reason that item
is a gate rather than a nicety. Step 3 below is where that line would appear.

A consequence worth stating for anyone running a private chain: this recognition is by
genesis hash **and** chain id, so a chain with its own genesis is unaffected whatever id
it uses — but a chain initialised from a genesis this project publishes is that network
by construction and does get the bundled schedule. The `genesis.json` in
`TESTNET_V2_DEPLOYMENT.md` is one of those, and what happens to such a stand depends on
when it is started. **On the first start, before it has mined anything, the bundled
schedule is taken** — the head is the genesis block, so neither bound has anything to
protect, and the stand is on testnet's rules from its first block. That is the right
answer for a chain whose genesis and chain id are testnet's, and it is the case the
documented procedure produces. Once it has mined blocks under the bare config the answer
flips: its head timestamp is past `shanghaiTime`, so taking the schedule would rewind, and
the adoption is refused with the warning instead. Either way, a private stand should
change `chainId` in that file rather than rely on the outcome; that document says so.

There is a further consequence for nodes that have been offline a long time, and it is
what the second bound is for. If the stored config is missing a fork that has **already**
fired, adopting the bundled schedule would produce a `CheckCompatible` error and
`NewBlockChain` would act on it by rewinding the chain. A start without a network flag
does not ask for that, so it does not happen: the adoption is refused, the stored config
is kept and the warning above is logged. Such a node still has to be dealt with, but deliberately: start it with its network flag,
which does take the schedule, rewinds to before the fork that had already fired, and
re-syncs from there. Its warning names that as the fix:

```
WARN This binary's schedule for this network was not taken because it would rewind the chain
```

**A node that reaches this release after activation is in exactly this state**, and it is
not an exotic one — it is a node that has been following the chain the whole time and
simply got the binary late. Everything in its stored config agrees except the fork time it
is missing, and that fork has fired, so a flagless restart refuses the schedule — and
since that would leave the node on the pre-fork rules, it refuses to start at all, with
the remedy in the error. This is the one case the roll-call below is for, and the remedy
is the network flag, not a resync from genesis. A node restored from an old backup, or left stopped since before
`DynamicMinBaseFee`, reaches the same state by a different route; check that one with the
network flag and a spare datadir before pointing it at the real one.

What that does *not* do is make the timestamp negotiable. When the configuration a start
settles on disagrees with the stored one about a fork that has already fired,
`CheckCompatible` returns a compat error and `NewBlockChain` **rewinds the chain** to
before it and rewrites the stored config — it does not refuse. That is the designed
behaviour and it re-validates the affected blocks under the right rules, but it means an
unexpected disagreement costs a resync rather than a failed start.

Which starts can reach it: one with a network flag, since the flag's schedule is used
whatever the database says; and one carrying `--override.webauthnstrict` with a timestamp
already behind the head, since the override is applied over the stored config after the
adoption is refused. A plain no-flag start cannot — that is the bound described above,
and it is why such a node stays where it is instead. Step 4 below is what catches an
unexpected one before it bites.

1. Install the release binary.
2. Restart the node, with its network flag — and check which one. Incentiv mainnet and
   devnet share a genesis hash, so the hash cannot tell the two apart; starting a mainnet
   datadir with `--incentiv-devnet` is refused by chain id, with both ids in the message,
   the same way `geth export` refuses it. It used to be written through: `checkCompatible`
   reports the chain-id change as an incompatibility at block 0, the setup path discards
   those, and the stored `chainId` was rewritten from 24101 to 12730 with no error and no
   rewind.
3. Check the startup log. It must contain none of the three lines below. The first two
   are refusals — a node logging either of them kept its own configuration instead of
   this binary's schedule, and if that leaves it without `webauthnStrictTime` it does not
   start, with the same fields repeated in the error. The third is not a refusal: the
   bundled schedule was taken over a stored timestamp that differed, which is what a
   dropped `--override.webauthnstrict` looks like, and such a node is back on the fleet's
   schedule whether or not that was intended:

   ```
   WARN Stored chain config disagrees with the schedule this binary ships for this network
   WARN This binary's schedule for this network was not taken because it would rewind the chain
   WARN Stored WebAuthnStrict timestamp replaced by the schedule this binary ships
   ```

   Read the fields on a refusal rather than assuming what it means, because a refusal does
   not by itself say the fork is off:

   - `webauthnStrictTime=unset` — the fork would not activate on this node at all. Within
     30 days of this network's bundled activation the start was refused and the node is
     not running; further out it runs, and has to be dealt with before then.
   - `webauthnStrictTime=<a value>` — it will activate, at that value. If it is not the
     one the rest of the fleet reports, this node is on a schedule of its own; that is
     expected on a node being carried on `--override.webauthnstrict` and is a problem
     otherwise.
   - `cause` names what the two configurations disagreed about, and `fix` is the remedy
     for *that* — which differs between the two refusals. A disagreement about the
     block-numbered settings is not repaired by the network flag; the flag writes them
     over blocks already mined without a rewind being computed from them, and the way back is a resync from
     genesis. A disagreement about a fork timestamp that has already fired *is* repaired
     by the flag, which rewinds to before it. Follow the `fix` field, not the first
     remedy that comes to mind.

   A third line, `Network flag replaces block-numbered settings this database disagrees
   with`, is not a refusal: the flag's schedule *was* written, and it says those settings
   now apply to blocks already mined. On a devnet node the fork table
   (`core/blockchain.go` logs `ChainConfig.Description()`) must name
   `WebAuthnStrict @1791291600 (2026-10-06T13:00:00Z)`. A line naming the old
   07:00 UTC devnet timestamp means that node is still on the old schedule and
   needs investigation before rollout. On a mainnet node the line must read
   `WebAuthnStrict @1791504000 (2026-10-09T00:00:00Z)`.

4. Check the same value over RPC, which is the form that is easy to collect from every
   node at once:

   ```
   admin_nodeInfo -> protocols.eth.config.webauthnStrictTime
   ```

   Over IPC (`geth attach <datadir>/geth.ipc`). The `admin` namespace is deliberately
   not among the HTTP APIs the operator guide's example unit exposes, and the `monitor`
   namespace is not a substitute — `monitorAPI.NodeInfo` does not return the chain
   config.

5. Confirm the node is syncing and peered: `eth_blockNumber` advancing,
   `net_peerCount` at its normal level.

Two other cases refuse to start. One is the wrong network flag: Incentiv mainnet and
devnet share a genesis hash, and a flag naming the other chain id is refused with both
ids in the message rather than written over the stored one. The other is a database that
holds an Incentiv genesis block but no chain config record at all. The genesis hash cannot say which network it is,
since mainnet and devnet share one, so there is nothing safe to assume and the node
says:

```
genesis 0x… belongs to an Incentiv network but the database holds no chain config;
start with the network flag (--incentiv-mainnet, --incentiv-testnet or --incentiv-devnet)
so the right schedule is written
```

Starting once with the network flag writes the schedule and the node runs without it
afterwards. The read-only chain commands, `geth export` among them, refuse for the same
reason and point at the same fix.

Step 4 is still worth doing even though the schedule is now applied without the flag:
it is what tells you a node came back on the timestamp you expect rather than one an
override left behind, and it is the field the roll-call collects.

## Roll-call before activation

On each host on the roll-call, use the local IPC socket to read the running client's
configuration and head. The pre-upgrade binary omits the new field; after the upgrade
each node must report the activation timestamp bundled for its network — the table at
the top of this document has them, and the devnet rehearsal used its own.

Binary, unit and datadir differ per deployment; `systemctl show <unit> -p ExecStart` (or
the operator's own inventory) is what resolves them on a host, and the commands below are
written against whatever that resolves to:

```sh
geth attach --exec 'admin.nodeInfo.name' <datadir>/geth.ipc
geth attach --exec 'admin.nodeInfo.protocols.eth.config' <datadir>/geth.ipc
geth attach --exec 'eth.getBlock("latest").timestamp' <datadir>/geth.ipc
geth attach --exec 'eth.blockNumber' <datadir>/geth.ipc
geth attach --exec 'net.peerCount' <datadir>/geth.ipc
```

Keep the filled-in roll-call — hosts, paths, versions and timestamps — out of this file.
It belongs with the operator's own notes, because this document describes how to do the
rollout, not where one network's nodes live.

Collect `admin_nodeInfo.name` (the client version) and
`admin_nodeInfo.protocols.eth.config.webauthnStrictTime`
from every node on the list and check them off — the timestamp, not just the version,
because a node held on an override reports the overridden value and a node that lost its
override reports the bundled one. On mainnet the only right answer is `1791504000`; a node
reporting any other value ran a build from before this time was set and
needs the restart "Activation timestamps" describes. The fork is safe to let fire only when every node
reports the same timestamp. This is a manual gate on purpose: there is no on-chain
signal that tells you a validator is still on the old binary before it produces a block
you reject.

## What to watch across the boundary

| Signal | Expected |
|---|---|
| Block production | unchanged; no gap at the boundary |
| `net_peerCount` on upgraded nodes | unchanged |
| Fork ID rejections of **new** connections | zero if the roll-call was complete; each one is a node that missed the upgrade and has since tried to reconnect. The signal is `Ethereum handshake failed` in `eth/handler.go`, logged at `Debug`, so watching for it needs `--verbosity 4` on at least one node. Zero is not evidence on its own: the filter never runs again on a connection that is already open, so a node that missed the upgrade and stayed connected produces no rejection at all — the paragraph below says where that does show |
| Passkey `UserOperationEvent` rate and success | unchanged |
| Reverted `handleOps` bundles | unchanged |
| `core/vm/testdata/webauthn/live_passkey_check.js <rpc-url>` against an upgraded RPC node | three `PASS` lines from activation on. Before it the trailing-byte line reads `FAIL`, which is the pre-fork parser answering — so the same command run before the boundary tells you which parser that node is on, with nothing sent and nothing spent |

The fork ID is half a safety net, and it is worth knowing which half. `webauthnStrictTime`
folds into the fork ID, so from activation onwards an upgraded node refuses a *new*
connection from one that missed the upgrade: that node cannot rejoin after a restart, and
each refusal is a node to go and fix.

It does not close the connections that already exist. The filter runs once per peer,
inside the eth handshake — `eth/handler.go` builds it as "constant across the lifetime of
the node" and `eth/protocols/eth/handshake.go` applies it while reading the peer's status —
and nothing re-checks an established peer when a fork activates. A node that missed the
upgrade and stays connected therefore keeps following the chain until it meets a block it
cannot execute, and what shows that is its own log and its own head, not a drop on the
upgraded side.

So the roll-call before activation is the plan, and the fork ID only keeps a missed node
out once its connection breaks. Treat "the peers are still there and the height is
rising" as evidence of nothing in particular: it is equally what a node that missed the
upgrade looks like until the first block it rejects.

## Point of no return

Before activation, rolling back is free: redeploy the previous binary, or push the fork
out of reach in place with `--override.webauthnstrict` set to a timestamp far past the
window. The override cannot turn it off — a `*uint64` carries no value meaning "never",
and `0` is rejected for preceding whichever timestamp fork comes before it in the
ordering — so postponing it is the whole of the
lever. Fork IDs match below the activation timestamp, so a mixed network peers normally
(`core/forkid/forkid_webauthn_test.go` asserts this); a node postponed on its own
diverges from the fleet's fork ID once they pass the bundled timestamp and it does not.

One part of the release is *not* gated and so is not covered by that: a precompile
called directly by a transaction is handed a copy of its input from the moment the
binary starts. It changes no state transition, so it needs no coordination, but
rolling back the binary rolls it back too.

After activation there is no clean rollback, and the failure mode is a rewind rather
than a refusal. Moving or removing `webauthnStrictTime` once the head has passed it
produces a `ConfigCompatError`, and `NewBlockChain` acts on it: it rewinds the chain to
before the fork and writes the new config (`core/blockchain.go`, "Rewinding chain to
upgrade configuration"). A node that loses its `--override.webauthnstrict` after
activation therefore does not fail to start. What it does next depends on whether it
carries a network flag: with one, it silently rewinds and re-syncs on a different
schedule from the rest of the network. Without one, the adoption that would have done
that is refused, so it does not rewind at all — it keeps the override's timestamp from
its stored config and carries on, on a schedule the fleet is not running. Watch for both:
a node that started and went backwards, and a node whose reported
`webauthnStrictTime` is not the one the fleet has. The second is the quieter of the two
and only the roll-call finds it.

So undoing the fork means a coordinated rewind past the activation block on every node,
and neither failure mode announces itself as a node refusing to start.

So: the decision point is the roll-call, and the schedule must leave enough room that the
roll-call can fail and the timestamp can still be moved.

## If a node arrives after activation

A node upgraded after its network's timestamp has passed does not activate the fork by
restarting: everything in its stored config agrees except the fork time it is missing, and
that fork has fired, so a flagless start refuses the schedule rather than rewinding the
chain behind the operator's back. It logs

```
WARN This binary's schedule for this network was not taken because it would rewind the chain
```

and, because that leaves it without the fork, refuses to start, repeating the line's
`cause` and `fix` in the error. It is not running on the wrong rules and it is not
silently following a wrong chain; it is down until the operator acts, which is the
loudest of the failure modes available here. (Had it run, its fork ID would differ from
the fleet's from activation, so upgraded peers would drop it rather than feed it blocks —
`core/forkid/forkid_webauthn_test.go` asserts the divergence.)

1. Restart it **with its network flag**. That takes the schedule, and `NewBlockChain`
   rewinds to before the activation timestamp and re-syncs from there — with the fork in
   force this time.
2. Confirm as in the per-node procedure: the fork table line, then
   `admin_nodeInfo -> protocols.eth.config.webauthnStrictTime` over IPC.
3. Expect a resync of the blocks after activation. How long depends on how late the node
   is, which is the argument for the roll-call happening before the timestamp rather than
   after.

Nothing here needs the timestamp moved, and moving it is not the fix — see "Point of no
return".

## If passkey operations start failing after activation

That would mean a payload encoding that the replay validation did not see. In order:

1. Capture a failing operation's `signature` bytes and the account's `publicKey()`, build
   the input the account builds — `userOpHash ‖ signature[3:] ‖ publicKey.x ‖ publicKey.y`,
   or take it straight from a `callTracer` trace — and run it through both parsers:

   ```
   cd core/vm && WEBAUTHN_INPUT=0x<input> go test -run 'TestWebAuthnVerifyCapturedInput$' -v .
   ```

   The log says what each parser returned; `1` then `0` is an encoding the fork rejects.
   The replay harness says *why* — its canonical-length check names the declared lengths
   and the actual one — and takes the operation as a one-record fixture with
   `WEBAUTHN_OPS_MIN=1`, failing on it, which here is the answer rather than a problem:

   ```
   cd core/vm && WEBAUTHN_OPS_FILE=/path/to/one-op.json WEBAUTHN_OPS_MIN=1 \
     go test -run TestWebAuthnVerifyMainnetReplay -v .
   ```

   The file needs `source.chainId` 24101, `source.entryPoint`
   `0x3eC61c5633BBD7Afa9144C6610930489736a72d4`, and an `ops` array; see
   `core/vm/testdata/webauthn/README.md` for the field names.
2. If it is not canonical, the fix is on the client that produced it, not on the chain:
   its declared lengths do not account for its own length.
3. Do not attempt to move `webauthnStrictTime`. Treat this as a client incident.

The pre-activation replay is what makes this branch unlikely; it is not a substitute for
running it.
