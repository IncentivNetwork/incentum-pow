# WebAuthnStrict — canonical input for the 0x111 precompile

Written for node operators, validators and reviewers of this change.

| | |
|---|---|
| Fork name | `WebAuthnStrict` |
| Config field | `webauthnStrictTime` (`params.ChainConfig.WebAuthnStrictTime`) |
| Activation | block timestamp, `block.Time >= webauthnStrictTime` |
| Scope | the return value of the precompile at `0x0000…0111` |
| Consensus | yes — the same call can return a different value across the boundary |

## What the precompile does

`0x111` verifies a WebAuthn (passkey) assertion. Callers build its input with
`abi.encodePacked`, which concatenates without delimiters:

```
input = message(32) ‖ signaturePayload(variable) ‖ publicKey.x(32) ‖ publicKey.y(32)
```

`signaturePayload` comes from the transaction and declares its own internal lengths:

```
signaturePayload = authDataLength(4) ‖ authData(authDataLength) ‖ userVerification(1)
                 ‖ clientDataJSONLength(4) ‖ clientDataJSON(clientDataJSONLength)
                 ‖ challengeLocation(4) ‖ responseTypeLocation(4) ‖ r(32) ‖ s(32)
```

so its canonical length is `81 + authDataLength + clientDataJSONLength`, and a canonical
input is exactly `177 + authDataLength + clientDataJSONLength` bytes long. The precompile
locates `r`, `s`, `x` and `y` by walking those declared lengths.

## What changes at activation

Three things, all in `webAuthnVerify.Run` (`core/vm/contracts.go`).

**1. The input must be canonical.** The length check at the end of the walk becomes an
equality: the input has to end exactly at `y`. Before the fork it only had to be *at
least* that long and the bytes past `y` were never read, so the lengths declared inside
the payload were not tied to the bytes the caller appended — the four words the parser
read need not have been the last four words of the input. Every honest input already
ends exactly at `y`, so nothing the replay reconstructed is affected — and what that
does and does not cover is in
[replay-validation.md](replay-validation.md).

**2. The message is hashed without being assembled.** `authenticatorData` is a
sub-slice of the input with spare capacity, so appending the `clientDataJSON` hash to it
wrote 32 bytes over the input buffer — and the input buffer is a slice of the calling
contract's EVM memory. The two parts are now written into the hash in turn and the
concatenation is never materialised, so there is no buffer to write over. No caller reads that region after the call today, but a contract
that did would observe the write, so removing it is a consensus change and belongs
behind the same gate.

**3. `r`, `s`, `x` and `y` are copied verbatim.** They were being re-serialised through
`big.Int`, which drops leading zero bytes, and then copied left-aligned into their
32-byte slots — so a word whose top byte is zero was shifted up one byte and scaled by
256, and a valid assertion was rejected. Each word has a 1-in-256 chance of a zero top
byte, but the four are not alike:

- `r` and `s` are drawn fresh per assertion, so about 1 assertion in 128 was rejected.
  Activation makes those verify. What the wallet then did — an error, a retry, a second
  prompt — is inference: mined operations cannot show attempts that were never mined.
- `x` and `y` are the account's key and never change, so about 1 account in 128 could
  never authorise anything at all. Activation makes those accounts usable, which is a
  state change operators should expect rather than a retry rate quietly dropping. One
  such account exists on mainnet today; see
  [replay-validation.md](replay-validation.md).

Gas is unchanged. The set of precompile addresses is unchanged.

Separately, and not gated on this fork, a precompile called directly by a transaction
is now handed a copy of its input. At that depth the buffer is the transaction's own
calldata, and the pre-fork parser writes into it — see
`core/vm/runtime/precompile_input_test.go`. Nothing inside the EVM can observe a write
at depth 0, so that change alters no state transition and takes effect as soon as the
binary is deployed.

## Why a timestamp and not a block number

`CheckConfigForkOrder` rejects a block-numbered fork scheduled after a timestamped one
("reverted to block ordering"), and this chain's `ShanghaiTime`, `DPoWTime` and
`DynamicMinBaseFeeTime` are all timestamps. A new block-numbered fork would make the
config invalid and the node refuse to start. `WebAuthnStrictTime` therefore follows the
same pattern as `DPoWTime` and `DynamicMinBaseFeeTime`.

Activation is gated on the block's own timestamp, through `params.Rules`, so the first
block with `block.Time >= webauthnStrictTime` is already subject to the new rules. This
differs from `DynamicMinBaseFee`, which is gated on the *parent* timestamp and so lags a
block.

## How the fork is wired

| Where | What |
|---|---|
| `params/config.go` | `WebAuthnStrictTime` field, `IsWebAuthnStrict`, `Rules.IsWebAuthnStrict`, fork-order entry, compatibility check, and the devnet, mainnet and testnet timestamps |
| `core/vm/contracts.go` | `webAuthnVerify.strictInput` selects the pre- or post-fork behaviour; `PrecompiledContractsWebAuthnStrict` is the Berlin set with that one entry replaced, built in `init` so the two cannot drift |
| `core/vm/evm.go` | `(*EVM).precompile` and `ActivePrecompiles` pick that set when `Rules.IsWebAuthnStrict` is set, the same way they pick the Berlin and Istanbul sets |
| `core/genesis.go` | a stored config whose genesis hash **and** chain id match a bundled Incentiv network is replaced by the bundled one, so a node started without a network flag runs the same schedule as one started with it. Two bounds, both on the head: a disagreement about one of the six block-numbered settings `checkCompatible` does not compare blocks the adoption only when the setting is at or below the head, since above it nothing that exists is rewritten; and a disagreement `checkCompatible` *does* report blocks it too, so a start without a flag does not rewind the chain to adopt a schedule — accepting that is what the flag is for. The bound is on the adoption rather than on the whole start: `--override.webauthnstrict` is applied over the stored config after a refusal, so one naming a timestamp already behind the head still rewinds, which is the operator asking for it explicitly. When the adoption is refused the stored config is kept whole and the warning reports the timestamp the node will actually run and the remedy that fits it; if that timestamp is unset the start is refused with the same fields, since the node would otherwise split from its network at activation. A stored timestamp the bundled schedule replaces — a dropped override — is logged too. A genesis specification naming another chain id than the stored one is refused, which is what the wrong network flag is on the mainnet/devnet pair that shares a genesis hash; and a database with such a genesis but no config record refuses to start rather than guess |
| `cmd/utils/flags.go`, `cmd/geth/config.go` | `--override.webauthnstrict` reschedules or arms the fork — it cannot turn it off; the node, `geth import` and `geth export` resolve it through the same `ethconfig.Config.ChainOverrides` |
| `core/forkid` | nothing — `gatherForks` picks the field up by reflection because its name ends in `Time` |

Both behaviours stay in the binary permanently: blocks mined before activation are
replayed with the pre-fork parser, so a node syncing from genesis reaches the same head.

## Peering during the rollout

Because the fork ID folds in fork timestamps, a node carrying `webauthnStrictTime` and a
node without it compute the **same** fork ID hash at any head before activation, so they
peer normally while operators upgrade. From activation the hashes diverge, so an upgraded
node refuses a *new* connection from one that missed the upgrade. Both of those are
asserted in `core/forkid/forkid_webauthn_test.go`, and both are properties of the filter,
which runs once per peer during the eth handshake. Connections that already exist are not
re-checked when the fork activates, so a node that missed the upgrade and stays connected
keeps following until the first block it cannot execute —
[rollout.md](rollout.md) has what that means for the roll-call.

## Deliberately out of scope

These are separate questions about the precompile and its callers, and mixing them into
a consensus change would widen its blast radius for no gain:

- the `userVerification` byte at `input[36+authDataLength]` and what it is allowed to
  relax. The strict parser still reads it as `== 1`, so `0x00` and `0x02` to `0xff` remain
  interchangeable encodings of "not required": a known non-canonicality this fork leaves
  in place, for a later fork to pin if one is ever wanted, since it does not touch what
  this fork is for — tying the declared lengths to the input;
- whether the caller, not the precompile, should bind `challenge` to the operation;
- signature malleability (`high-s`) and the wallet-id bytes the callers strip;
- any length validation the calling contracts do before `abi.encodePacked`, which is
  defence in depth on the contract side and independent of this fork.

## Tests

| File | Covers |
|---|---|
| `core/vm/contracts_webauthn_test.go` | both sides of the fork: canonical input, a payload whose declared lengths move the words, trailing and missing bytes, the input buffer being left alone, words with a zero top byte, a malformed-input corpus, and the gate itself |
| `core/vm/contracts_webauthn_replay_test.go` | real mainnet assertions replayed through both parsers |
| `core/vm/runtime/precompile_input_test.go` | a precompile called directly by a transaction cannot write to the caller's buffer; and, from a contract, that the pre-fork write into the caller's memory happens before activation and not after — it is part of every historical block's state transition, so it has to be gated rather than removed |
| `params/config_webauthn_test.go` | the predicate, `Rules`, fork ordering and config compatibility across the rollout window |
| `core/genesis_webauthn_test.go` | the bundled schedule is applied without a network flag, a stored timestamp that has drifted from it is replaced, `--override.webauthnstrict` still wins, applying an override does not mutate the shared package-level configs, and a stored config that disagrees about any one of the six block-numbered settings is not adopted — table-driven, so no single comparison in `HistoricalForksCompatible` can be dropped unnoticed; that a setting still ahead of the head does *not* block the adoption; and that a start without a flag never resolves to a configuration `CheckCompatible` would rewind for |
| `cmd/geth/exportcmd_test.go` | the override path through a real `geth export` invocation: the stored timestamp passes, a different one is refused, the same again from a TOML config file rather than the command line, a network flag for another chain is left to the genesis mismatch, and `--incentiv-devnet` on a mainnet datadir — the pair that shares a genesis hash — is refused by chain id |
| `core/forkid/forkid_webauthn_test.go` | peering before and after activation |
| `core/vm/harvest_script_test.go`, `core/vm/testdata/webauthn/*_test.js` | what the harvesting scripts decide and the replay cannot re-check: that a 65-byte signature is plain ECDSA whatever its first byte says, because the account checks the length before the version; that a block range which cannot be scanned is refused rather than reported as a range with nothing in it; and that the sampler refuses to merge harvests of different chains or EntryPoints into one fixture, which the replay would believe. One of them, `script_guards_test.js`, runs the harvest and the census themselves inside a throwaway directory holding a stand-in for `ethers` and answering their JSON-RPC from an in-process endpoint, which is what covers the lines that *call* those rules rather than the rules alone. The Go test runs every JS test in that directory through `node`; with no `node` it skips locally and **fails** when `CI` is set, so a green CI result means they ran |

`go test ./core/vm/... ./params/ ./core/forkid/ ./core/ ./cmd/utils/` and
`go test ./cmd/geth/` separately — the `cmd/geth` tests spawn geth subprocesses with
five-second startup budgets, and running them beside `core` saturates the machine, so the
`TestDebugProfile*` cases time out on startup rather than on anything they test.

Run these on a checkout with LF line endings. On a CRLF working copy `gofmt`, `goimports`
and `TestVerification/minisig` all fail on files this change never touches, which buries
anything real.

## Documents

- [replay-validation.md](replay-validation.md) — what happens to real historical traffic
  under the new parser.
- [rollout.md](rollout.md) — the upgrade sequence for validators and RPC nodes.
