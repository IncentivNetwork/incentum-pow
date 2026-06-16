# Hybrid Delegated Proof-of-Work (DPoW) — Implementation Specification

## Incentiv Network Miner Authorization System

**Version**: 2.0
**Status**: Living document — reflects the implemented and reviewed code on `develop`
**Primary sources**: `contracts/incentiv/MinerRegistry.sol`, `consensus/ethash/dpow.go`, `core/state_processor.go`, `params/config.go`

---

## Table of Contents

1. [Executive Summary](#1-executive-summary)
2. [Architecture Overview](#2-architecture-overview)
3. [Smart Contract Specification](#3-smart-contract-specification)
4. [Geth Client Modifications](#4-geth-client-modifications)
5. [Configuration & Deployment](#5-configuration--deployment)
6. [Testing Strategy](#6-testing-strategy)
7. [Migration & Hard Fork Plan](#7-migration--hard-fork-plan)
8. [Security Considerations](#8-security-considerations)
9. [Performance Analysis](#9-performance-analysis)
10. [Formal Specification](#10-formal-specification)
11. [Operational & Governance Layer](#11-operational--governance-layer)
12. [Appendix](#12-appendix)

---

## 1. Executive Summary

### 1.1 Objective

Implement a **stake-based mining authorization layer** on top of Ethash Proof-of-Work to:

- Eliminate mercenary mining through a mandatory stake requirement.
- Increase network security via Sybil resistance (capital cost per miner identity).
- Enhance `$CENT` token utility through locked capital.
- Enable governed control of mining permissions (emergency removal of a miner).
- Prevent flash attacks via a dual maturity period.

### 1.2 Technical Approach

1. **Smart contract registry** — an on-chain `MinerRegistry` contract tracks staked miners.
2. **Timelock governance** — an OpenZeppelin `TimelockController` gates all privileged operations behind a delay.
3. **Finalize-level validation** — miner authorization is checked where state is available (`Finalize` / `FinalizeAndAssemble` / `Process`), not in `VerifyHeader`.
4. **Dual maturity** — a miner must satisfy *both* a time-based and a block-based maturity threshold.
5. **Delayed unstaking** — a withdrawal delay prevents hit-and-run attacks and gives governance a response window.
6. **Frozen storage layout** — consensus-critical storage slots 0–3 are fixed forever.
7. **Hard fork activation** — a network-wide upgrade activates DPoW at a specific block height.

### 1.3 Architecture Classification

**System type**: Governed L1 consensus rule with contract-backed state storage.

- L1 protocol-level consensus change (affects block validity).
- Governed blockchain (Timelock + multisig can remove miners).
- State-based authorization (direct storage reads, not EVM execution).
- Consensus-critical: higher criticality than a balance-affecting application contract.

```
L1 Security = Economic Security (staked $CENT)
            + Governance Trust (Timelock + multisig)
            + Social Consensus (community vigilance)
```

### 1.4 Key Parameters

| Parameter | Mainnet value | Rationale |
|---|---|---|
| `STAKE_AMOUNT` | 26,000,000 WCENT | High capital commitment per miner identity — Sybil resistance |
| `MATURITY_TIME` | 86,400 s (24 h) | Time-based delay before a staked miner may produce blocks — prevents flash attacks |
| `MATURITY_BLOCKS` | 17,280 (~24 h at 5 s/block) | Block-based maturity — deterministic, resistant to timestamp manipulation |
| `UNSTAKE_DELAY` | 604,800 s (7 days) | Stake stays locked after `requestUnstake()` — prevents instant hit-and-run withdrawal (see §8.1 for the interaction with `TIMELOCK_DELAY`) |
| `TIMELOCK_DELAY` | 604,800 s (7 days) | Every governance action is visible on-chain for 7 days before it can execute |
| `DPoWBlock` | TBD | `ChainConfig` activation height; set once contracts are deployed |

> `MATURITY_TIME` / `MATURITY_BLOCKS` / `UNSTAKE_DELAY` are `immutable` constructor parameters rather than compile-time `constant`s. The **same contract bytecode** is deployed to devnet and mainnet with different values — see §3.1 and §5.

---

## 2. Architecture Overview

### 2.1 System Components

```
  Miner software
       │  produces blocks
       ▼
  Geth client (DPoW-enabled)
       │
       │  Process()  /  FinalizeAndAssemble()  /  Finalize()
       ▼
  VerifyMinerAuthorization()
       │
       │  reads MinerRegistry storage — 3 slots only:
       │      slot 0   miners[coinbase]
       │      slot 1   stakeTime[coinbase]
       │      slot 2   stakeBlock[coinbase]
       ▼
  accept / reject block

  Supporting contracts:
    WCENT               ERC-20 wrapper for native CENT; the staking token
    TimelockController  governance gate for setPaused / emergencyRemoveMiner
```

Geth reads **only slots 0, 1, 2**. Slot 3 (`unstakeRequestTime`) lies in the frozen consensus-critical region but is **not** consulted by the authorization predicate — `requestUnstake()` already sets `miners[m] = false`, which the slot-0 check catches. See §3.2 and §4.4.

### 2.2 State Access Architecture

**Critical design decision**: authorization uses **direct state storage reads** (`state.GetState()`), not EVM execution (`evm.Call()`).

Properties:
- No EVM gas dependency, no opcode-version dependency, no Solidity-compiler dependency.
- Deterministic state reads.
- Depends on the contract storage layout — slots 0–3 are frozen (§3.2).
- The state itself is produced by ordinary historical EVM execution of `stake()` / `requestUnstake()` / etc.

The canonical implementation is `Ethash.VerifyMinerAuthorization` in `consensus/ethash/dpow.go` — see §4.4 for the exact logic.

### 2.3 Why `Finalize()` / `Process()`, not `VerifyHeader()`

```
VerifyHeader():
  - called during header-only sync
  - state may NOT be available
  ✗ cannot read MinerRegistry storage

Process() / Finalize() / FinalizeAndAssemble():
  - called after transactions have been executed
  - state IS available
  ✓ can read MinerRegistry storage
```

Call order on block import:

```
insertChain()
  → StateProcessor.Process()
      → execute all transactions
      → VerifyMinerAuthorization()      ← imported-block guard (clean error)
      → engine.Finalize()
          → VerifyMinerAuthorization()  ← defense-in-depth (panic)
```

During local block production the same check runs inside `FinalizeAndAssemble()` (clean error, prevents a node from producing an invalid block). See §4.5.

---

## 3. Smart Contract Specification

### 3.1 `MinerRegistry`

Full source: `contracts/incentiv/MinerRegistry.sol`. This section documents the interface and invariants; the file is the source of truth.

**Design decisions:**

- **Self-only staking** — `stake()`, `requestUnstake()`, `finalizeUnstake()` all act on `msg.sender`. Third-party staking is intentionally unsupported; `msg.sender` is always both the token payer and the registered miner.
- **No OpenZeppelin `ReentrancyGuard`** — inheriting it would place `_status` at slot 0, shifting `miners` to slot 1 and silently breaking Geth's consensus reads. Reentrancy protection is implemented manually with `_reentrancyStatus`, declared *after* all consensus-critical fields.
- **Separate pending-unstake sentinel** — `unstakeRequestTime` (slot 3) always stores the *real* `block.timestamp`. A separate private boolean `unstakeRequested` (slot 4) is the "pending unstake" sentinel. This avoids the genesis/private-chain edge case where `block.timestamp == 0` would be indistinguishable from "no request". (Earlier drafts used a `block.timestamp + 1` encoding in slot 3 — that design was replaced and is **no longer used**.)
- **Configurable maturity** — `MATURITY_TIME`, `MATURITY_BLOCKS`, `UNSTAKE_DELAY` are `immutable` constructor parameters, so one bytecode serves all environments.

**Constants and immutables:**

```solidity
uint256 public constant STAKE_AMOUNT = 26_000_000 * 10 ** 18;

IERC20             public immutable centToken;        // WCENT staking token
TimelockController public immutable timelock;         // governance gate
uint256            public immutable MATURITY_TIME;    // seconds
uint256            public immutable MATURITY_BLOCKS;  // blocks
uint256            public immutable UNSTAKE_DELAY;    // seconds
```

**Constructor:**

```solidity
constructor(
    address centToken_,
    address timelock_,
    uint256 maturityTime_,
    uint256 maturityBlocks_,
    uint256 unstakeDelay_
)
```

- Reverts `ZeroAddress` if `centToken_` or `timelock_` is `address(0)`.
- Reverts `InvalidParam` if any of `maturityTime_`, `maturityBlocks_`, `unstakeDelay_` is zero.
- Initializes `_reentrancyStatus = _NOT_ENTERED` (avoids the cold-storage-write penalty on the first guarded call).

**Deployment hardening (admin-renounce bootstrap pattern):**

`MinerRegistry` itself takes no admin parameter; the relevant deployment-time hardening lives on the `TimelockController`. The OpenZeppelin `TimelockController` constructor accepts an optional `admin` address that receives `DEFAULT_ADMIN_ROLE` and can grant/revoke roles **instantly** (no timelock). For mainnet we exploit this for first-time setup, then renounce:

1. Deploy `TimelockController` with a **temporary 60-second** `minDelay`, `proposers = [GovernanceSafe]`, `executors = [address(0)]` (open executor), `admin = deployerEOA`.
2. In the same script run (still under the deployer's `DEFAULT_ADMIN_ROLE` from step 1): `grantRole(CANCELLER_ROLE, GuardianSafe)` and `revokeRole(CANCELLER_ROLE, GovernanceSafe)` — these are sequential on-chain transactions, not atomic; the latter undoes the auto-grant OZ performs in the constructor.
3. Deploy `MinerRegistry` against the same Timelock.
4. Run integration tests on mainnet against the 60-second delay: `schedule → cancel` (Guardian), `schedule → wait → execute` (open).
5. Governance schedules `timelock.updateDelay(604800)`, waits 60 s, executes — `minDelay` becomes the final 7 days.
6. Deployer calls `renounceRole(DEFAULT_ADMIN_ROLE, deployerEOA)`. After this every role change is timelocked.

This is documented at the OZ-level (`TimelockController` constructor NatSpec explicitly recommends renouncing the optional admin after setup) and operationalised in `DPOW_GOVERNANCE_RUNBOOK.md`. The Foundry script `script/DeployMainnet.s.sol` performs steps 1–3 in a single script run as multiple sequential on-chain transactions; it is not one atomic transaction, so a mid-run failure can leave partial state that must be recovered while the deployer still holds the temporary admin role. Steps 4–6 are manual because they require multisig signatures.

**Core functions** (all `nonReentrant`):

| Function | Caller | Effect |
|---|---|---|
| `stake()` | miner | Pulls `STAKE_AMOUNT` WCENT, sets `miners[m]=true`, records `stakeTime`/`stakeBlock`, `activeMinerCount++`. `whenStakingNotPaused`. |
| `requestUnstake()` | miner | Sets `miners[m]=false` immediately, sets `unstakeRequested[m]=true`, records `unstakeRequestTime`, `activeMinerCount--`. `stakeTime`/`stakeBlock` preserved for refund logic. |
| `finalizeUnstake()` | miner | After `UNSTAKE_DELAY`, clears slots 1–4 for the miner (slot 0 was already set `false` by `requestUnstake()`) and transfers `STAKE_AMOUNT` WCENT back. |

**Governance functions** (`onlyGovernance` — caller must be the `TimelockController`):

| Function | Effect |
|---|---|
| `setPaused(bool)` | Pauses/unpauses new staking. Does not affect existing miners or consensus. |
| `emergencyRemoveMiner(address miner, address refundRecipient, string reason)` | Force-clears the miner's state; transfers `STAKE_AMOUNT` to `refundRecipient` if the miner had any stake. `reason` must be non-empty; `refundRecipient` must be non-zero. |

**View function:**

- `isAuthorizedMiner(address) → bool` — for off-chain monitoring (RPC/UI). **Not used by Geth consensus** — Geth reads storage slots directly. Returns `true` iff `miners[m]` and both maturity thresholds are met, using the contract's own `immutable` `MATURITY_TIME`/`MATURITY_BLOCKS`.

**`emergencyRemoveMiner` refund logic** — the `hasStake` predicate:

```solidity
bool hasStake = wasActive
    || unstakeRequested[miner]
    || stakeBlock[miner] != 0
    || unstakeRequestTime[miner] != 0
    || stakeTime[miner] != 0;
```

If `hasStake` is true, `STAKE_AMOUNT` is transferred to `refundRecipient`. If the stake was already withdrawn via `finalizeUnstake()` (all fields zero), no transfer occurs and the call still succeeds (idempotent cleanup). The `unstakeRequested` term covers the genesis edge case where `block.timestamp == 0`.

**Events:**

```solidity
event MinerStaked(address indexed miner, uint256 stakeTime, uint256 stakeBlock);
event UnstakeRequested(address indexed miner, uint256 requestTime);
event MinerUnstaked(address indexed miner, uint256 amount);
event EmergencyRemoval(address indexed miner, address indexed refundRecipient, string reason);
event StakingPaused(bool paused);
```

`MinerStaked` carries a single `miner` field — self-only staking means `msg.sender == miner` always, so there is no separate payer field.

**Custom errors:**

| Error | Trigger |
|---|---|
| `ZeroAddress` | Constructor `centToken_` or `timelock_` is `address(0)` |
| `InvalidParam` | Constructor `maturityTime_`, `maturityBlocks_`, or `unstakeDelay_` is zero |
| `AlreadyStaked` | `stake()` while `miners[m]` is already `true` |
| `UnstakeInProgress` | `stake()` while a previous unstake is still pending (`unstakeRequested[m]`) |
| `InsufficientBalance` | `stake()` caller WCENT balance < `STAKE_AMOUNT` |
| `InsufficientAllowance` | `stake()` caller WCENT allowance to the registry < `STAKE_AMOUNT` |
| `InvalidStakeTransfer` | Post-transfer balance check failed (fee-on-transfer or malformed token) |
| `StakingCurrentlyPaused` | `stake()` while `stakingPaused` is `true` |
| `NotStaked` | `requestUnstake()` with no active stake, or `finalizeUnstake()` with no pending request |
| `UnstakeDelayNotMet` | `finalizeUnstake()` before `UNSTAKE_DELAY` has elapsed |
| `OnlyGovernance` | A governance-only function called by a non-Timelock address |
| `EmptyReason` | `emergencyRemoveMiner()` called with an empty `reason` |
| `InvalidRefundRecipient` | `emergencyRemoveMiner()` called with `refundRecipient == address(0)` |
| `ReentrantCall` | Reentrancy detected by the manual `_reentrancyStatus` guard |

### 3.2 Storage Layout (consensus-critical)

**Slots 0–3 are FROZEN forever. Any change requires a hard fork.**

| Slot | Variable | Type | Read by Geth | Purpose |
|---|---|---|---|---|
| 0 | `miners` | `mapping(address => bool)` | ✅ | Active-miner check |
| 1 | `stakeTime` | `mapping(address => uint256)` | ✅ | Time-maturity check |
| 2 | `stakeBlock` | `mapping(address => uint256)` | ✅ | Block-maturity check |
| 3 | `unstakeRequestTime` | `mapping(address => uint256)` | ✗ (reserved) | Real `block.timestamp` of the unstake request |
| 4 | `unstakeRequested` | `mapping(address => bool)` (private) | ✗ | Pending-unstake sentinel |
| 5 | `activeMinerCount` | `uint256` | ✗ | Monitoring |
| 6 | `stakingPaused` | `bool` | ✗ | Staking pause flag |
| 7 | `_reentrancyStatus` | `uint256` | ✗ | Manual reentrancy guard |

> Geth reads only slots 0, 1, 2. Slot 3 is reserved (not consulted by the current authorization predicate — see §4.4). `immutable` variables (`MATURITY_TIME`, etc.) occupy **no** storage slots; they are embedded in contract bytecode, so the `constant → immutable` conversion did not disturb the layout.

**Storage slot calculation** for `mapping(address => T)` at base slot `S`:

```
slot(key) = keccak256(abi.encode(key, S))
          = keccak256( leftPad32(key) ++ leftPad32(S) )
```

### 3.3 `WCENT` — staking token

`contracts/incentiv/WCENT.sol` is a WETH-style ERC-20 wrapper for the native `CENT` coin:

- `deposit()` (payable) — wraps native `CENT` 1:1 into `WCENT`.
- `withdraw(uint256)` — unwraps `WCENT` back to native `CENT` 1:1.
- Standard ERC-20 `transfer` / `approve` / `transferFrom`.

`MinerRegistry.centToken` points at the deployed `WCENT`. Staking therefore locks an ERC-20 balance, while the underlying value is native `CENT`.

---

## 4. Geth Client Modifications

### 4.1 Modified / new files

| File | Change |
|---|---|
| `params/config.go` | DPoW `ChainConfig` fields + accessors |
| `consensus/errors.go` | DPoW error types |
| `consensus/ethash/dpow.go` (new) | `VerifyMinerAuthorization` + slot helpers |
| `consensus/ethash/consensus.go` | DPoW check in `Finalize` (panic) and `FinalizeAndAssemble` (clean error) |
| `core/state_processor.go` | DPoW check in `Process` for imported blocks (clean error) |

### 4.2 Configuration (`params/config.go`)

`ChainConfig` DPoW fields:

```go
DPoWBlock            *big.Int        `json:"dpowBlock,omitempty"`
MinerRegistryAddress *common.Address `json:"minerRegistryAddress,omitempty"`
DPoWMaturityTime     uint64          `json:"dpowMaturityTime,omitempty"`
DPoWMaturityBlocks   uint64          `json:"dpowMaturityBlocks,omitempty"`
```

Accessors:

```go
func (c *ChainConfig) IsDPoW(num *big.Int) bool          // isBlockForked(DPoWBlock, num)
func (c *ChainConfig) GetMinerRegistryAddress() common.Address
func (c *ChainConfig) GetDPoWMaturityTime()  uint64      // default 86400  if unset
func (c *ChainConfig) GetDPoWMaturityBlocks() *big.Int   // default 17280  if unset
func (c *ChainConfig) CheckDPoWConfig() error            // invariant validation
```

`CheckDPoWConfig()` enforces: if `DPoWBlock != nil`, then `MinerRegistryAddress` must be non-nil and non-zero, and `DPoWBlock` must be a non-negative `uint64`-range value. A node started with `DPoWBlock` set but no registry address refuses to start.

> The `ChainConfig.DPoWMaturityTime` / `DPoWMaturityBlocks` values **must match** the `immutable` values of the deployed `MinerRegistry`. Geth reads maturity thresholds from `ChainConfig`; the contract's `isAuthorizedMiner()` reads them from its own immutables. If they disagree, monitoring tools and consensus disagree.

### 4.3 Error types (`consensus/errors.go`)

```go
ErrUnauthorizedMiner          = errors.New("unauthorized miner: address not in DPoW registry")
ErrMinerNotMature             = errors.New("miner stake not yet mature: maturity period not elapsed")
ErrMinerRegistryNotConfigured = errors.New("miner registry not configured: dpow block is set but registry address is zero")
```

### 4.4 Core validation logic (`consensus/ethash/dpow.go`)

`Ethash.VerifyMinerAuthorization(config, state, header)` performs, in order:

1. **Fake-mode bypass** — returns `nil` if `PowMode` is `ModeFake` or `ModeFullFake` (used by tests / dev tools).
2. **Activation gate** — returns `nil` if `!config.IsDPoW(header.Number)` (DPoW not active for this block).
3. **Registry configured** — returns `ErrMinerRegistryNotConfigured` if the registry address is zero.
4. **Active-miner check** — reads slot 0 (`miners[coinbase]`); returns `ErrUnauthorizedMiner` if the raw value is zero.
5. **Time maturity** — reads slot 1 (`stakeTime`); returns `ErrMinerNotMature` if
   `header.Time < stakeTime` **or** `header.Time - stakeTime < GetDPoWMaturityTime()`
   (the two-part comparison is overflow-safe).
6. **Block maturity** — reads slot 2 (`stakeBlock`); returns `ErrMinerNotMature` if
   `header.Number < stakeBlock + GetDPoWMaturityBlocks()`.

The `hashFitsUint64` helper guards only the slot 1 `stakeTime` read: a value that does not fit in `uint64` is treated as not-mature rather than overflowing the `uint64` conversion. The slot 2 `stakeBlock` value needs no such guard — the block-maturity check runs entirely in `big.Int` arithmetic, with no `uint64` conversion. Slot constants: `dpowMinersSlot = 0`, `dpowStakeTimeSlot = 1`, `dpowStakeBlockSlot = 2`. Slot 3 is intentionally **not** read — `requestUnstake()` already sets `miners[m] = false`, which the active-miner check catches.

`calculateMappingSlot(addr, baseSlot)` computes `keccak256(abi.encode(addr, baseSlot))` — the address left-padded to 32 bytes, concatenated with the 32-byte base slot — matching Solidity's mapping layout (see §3.2 and §12.1).

### 4.5 Integration points

Three call sites, by design (defense-in-depth):

| Site | File | On failure |
|---|---|---|
| `Process()` | `core/state_processor.go` | Returns a wrapped error: `DPoW: unauthorized coinbase <addr> at block <n>: <err>`. This rejects **imported** blocks from peers before `Finalize()` is reached. |
| `FinalizeAndAssemble()` | `consensus/ethash/consensus.go` | Returns `DPoW: local miner <addr> not eligible: <err>`. This prevents a node from **producing** an invalid block locally. |
| `Finalize()` | `consensus/ethash/consensus.go` | `panic` — defense-in-depth. This path is only reachable if the earlier guards were bypassed; the `Process()` guard exists specifically so a peer block never reaches this panic. |

---

## 5. Configuration & Deployment

### 5.1 Deployment sequence (per environment)

1. **Deploy `WCENT`** — the ERC-20 staking-token wrapper.
2. **Deploy `TimelockController`** — OZ contract; `minDelay`, `proposers`, `executors`, `admin` per environment (see §11).
3. **Deploy `MinerRegistry(wcent, timelock, maturityTime, maturityBlocks, unstakeDelay)`** — five constructor arguments.
4. **Record addresses** — set `ChainConfig.MinerRegistryAddress` (and `DPoWMaturityTime` / `DPoWMaturityBlocks` if overriding defaults).
5. **Coordinate the hard fork** — distribute the DPoW-enabled binary; set `DPoWBlock`; all nodes upgrade (see §7 and `DPOW_NODE_OPERATOR_GUIDE.md`).

The Foundry script `script/DeployDevnet.s.sol` performs steps 1–3 for devnet (chain ID 12730) and additionally funds the initial miner. A mainnet equivalent must use a multisig as `GOVERNANCE_ADDRESS` and a 7-day `TimelockController` delay.

### 5.2 Per-environment parameters

| Environment | `MATURITY_TIME` | `MATURITY_BLOCKS` | `UNSTAKE_DELAY` | `TimelockController` delay |
|---|---|---|---|---|
| Devnet | 300 s | 60 | 600 s | 60 s |
| Mainnet | 86,400 s | 17,280 | 604,800 s | 604,800 s |

> **`STAKE_AMOUNT` per environment.** `STAKE_AMOUNT` is a `constant` in `MinerRegistry.sol` — it is baked into the bytecode at compile time and cannot differ between two deployments of the same source. The currently deployed devnet contract carries `STAKE_AMOUNT = 100,000,000 WCENT` baked in (that was the source-code value when devnet was deployed in PR #73). The source-code constant was lowered to `26,000,000 WCENT` for mainnet in DPOW-008-1; the live devnet contract is unaffected by that source change (its bytecode is frozen). A future devnet reset would deploy with the new 26M value.

`ChainConfig` example (devnet, currently deployed):

```go
DPoWBlock:            big.NewInt(274000),
MinerRegistryAddress: newAddress(common.HexToAddress("0xdb6EEC53d173554730e342d6703c4AD3fD78604b")),
DPoWMaturityTime:     300,
DPoWMaturityBlocks:   60,
```

> **Testnet**: `IncentivTestnetChainConfig` ships with `DPoWBlock = nil` and `MinerRegistryAddress = nil` — DPoW disabled. A future testnet activation will set these in a separate PR, following the same procedure used for mainnet in DPOW-008-4.

### 5.3 Devnet deployed addresses (chain ID 12730)

| Contract | Address |
|---|---|
| WCENT | `0xDF989A6a09F2A59c18Ff91DD5D5B779c61511B51` |
| TimelockController | `0xB23dbCdB1DF4e03dd3A201aEb0cF5B7cc8855fC5` |
| MinerRegistry | `0xdb6EEC53d173554730e342d6703c4AD3fD78604b` |

> These addresses and the devnet `DPoWBlock` are accurate as of writing and may change on redeployment. Treat the live `ChainConfig` and on-chain contract state as authoritative.

### 5.4 Mainnet deployed addresses (chain ID 24101)

| Contract | Address |
|---|---|
| WCENT | `0xB0f0A14A50F14dc9e6476d61C00cF0375Dd4EB04` |
| TimelockController | `0xd3bB2D9D781179A5d71C1cA557024BfC687a0CD1` |
| MinerRegistry | `0xbe73e1F106Bd96538Be2a30F2eE94264850aFd7E` |

> These are the live mainnet contracts deployed in DPOW-008-3. WCENT is the pre-existing wrapped-CENT contract, reused (not redeployed). The `TimelockController` is in production state (7-day `minDelay`, Governance Safe as sole `PROPOSER_ROLE`, Guardian Safe as sole `CANCELLER_ROLE`, `address(0)` as open executor, `DEFAULT_ADMIN_ROLE` held only by the Timelock itself). Treat the live `ChainConfig` and on-chain contract state as authoritative.

---

## 6. Testing Strategy

### 6.1 Smart contract tests (Foundry)

`tests/incentiv/MinerRegistry.t.sol` — stake/unstake lifecycle, maturity enforcement, unstake delay, emergency removal, and constructor parameter validation (`testConstructor_RevertsOnZeroMaturityParams`). `tests/incentiv/MinerRegistryStorage.t.sol` — raw storage-slot key export for cross-layer verification against the Go `calculateMappingSlot`.

### 6.2 Geth integration tests

`consensus/ethash/dpow_test.go` — `VerifyMinerAuthorization` against synthetic state: unauthorized miner, immature miner (time and block), mature miner, registry-not-configured, and `calculateMappingSlot` correctness cross-checked against the Solidity-emitted keys.

### 6.3 Devnet end-to-end validation

Eight scenarios on a live 3-node devnet: normal mining, unauthorized-miner rejection, immature-miner rejection, maturity, unstake-disables-mining, emergency removal, pre-DPoW backward compatibility, and re-stake after unstake.

---

## 7. Migration & Hard Fork Plan

DPoW activation is a **consensus-breaking hard fork**. The full operational plan — pre-conditions, miner onboarding, node upgrade, activation, monitoring, rollback — is detailed in:

- `DPOW_NODE_OPERATOR_GUIDE.md` — node upgrade procedure.
- `DPOW_MINER_ONBOARDING.md` — staking and maturity timing for miners.
- `DPOW_GOVERNANCE_RUNBOOK.md` — Timelock operation and incident response.

**Activation invariant**: the first block with `number ≥ DPoWBlock` must have an authorized coinbase. Any node still running pre-DPoW software will accept unauthorized blocks and fork off the canonical chain — therefore *all* nodes must upgrade before `DPoWBlock`.

**Two-step rollout (recommended)**:

1. Release a binary that contains the DPoW code but with `DPoWBlock = nil` for the target network — behavior is unchanged; operators upgrade at their own pace.
2. Once contracts are deployed and an activation block is announced, release a second binary that only changes `DPoWBlock` / `MinerRegistryAddress`. Because every node already runs DPoW-capable code, the activation itself carries no coordination spike.

---

## 8. Security Considerations

### 8.1 Attack vectors

**Flash attack** — mitigated by the dual maturity period: a newly staked miner cannot produce blocks for `MATURITY_TIME` *and* `MATURITY_BLOCKS`. Capital is locked the entire time.

**Sybil / 51% attack** — each miner identity costs `STAKE_AMOUNT` (26M WCENT) of locked capital; an N-identity attack costs `N × STAKE_AMOUNT`.

**Hit-and-run** — `UNSTAKE_DELAY` keeps capital locked for 7 days after `requestUnstake()`, during which `miners[m]` is already `false` (mining authorization is revoked immediately, not after the delay).

> **Limitation — `UNSTAKE_DELAY` equals `TIMELOCK_DELAY`.** Both are 7 days on mainnet. `emergencyRemoveMiner` can only redirect or burn a miner's stake while that stake is still held by the contract. Because a governance removal must itself wait the full `TIMELOCK_DELAY`, a miner who calls `requestUnstake()` at or before the moment governance schedules the removal can reach `finalizeUnstake()` no later than the removal executes — and may withdraw first. `emergencyRemoveMiner` is therefore **not a guaranteed slashing mechanism**: it reliably *removes* a miner, but it can only seize the stake if executed before the miner finalizes their unstake. To make stake seizure guaranteed, deploy with `UNSTAKE_DELAY` strictly greater than `TIMELOCK_DELAY` plus an operational buffer.

**Governance capture** — mitigated by the Timelock delay, multisig proposers/executors, an independent canceller, and on-chain transparency. See §11 for the full operational model.

**Unlocked-account RPC drain** — a node that runs with `--unlock` + `--allow-insecure-unlock` + a publicly reachable HTTP RPC lets anyone submit `eth_sendTransaction` from the unlocked miner account. This is a node-operations misconfiguration, not a protocol flaw. Mitigation is mandatory and documented in §11.5 and `DPOW_NODE_OPERATOR_GUIDE.md`.

### 8.2 Storage layout risk

Consensus depends on the Solidity storage layout of slots 0–3. Inserting or reordering any state variable before or among `miners` / `stakeTime` / `stakeBlock` / `unstakeRequestTime` shifts the slots and breaks consensus. Mitigations: slots 0–3 are frozen; no proxy pattern is used; `_reentrancyStatus` and all non-consensus fields are declared after slot 3; any change requires a hard fork.

---

## 9. Performance Analysis

The per-block DPoW check is: up to three `state.GetState()` reads (slots 0/1/2), a small number of integer comparisons, and slot-key keccak hashing. The cost is negligible relative to transaction execution and block verification, and adds no measurable overhead to import throughput observed during devnet validation.

---

## 10. Formal Specification

### 10.1 State model

```
miners:             𝔸 → {true, false}
stakeTime:          𝔸 → ℕ
stakeBlock:         𝔸 → ℕ
unstakeRequestTime: 𝔸 → ℕ
unstakeRequested:   𝔸 → {true, false}
```

### 10.2 Authorization predicate

```
Authorized(m, B, T) ⟺
      miners[m] = true
    ∧ T ≥ stakeTime[m]  + ChainConfig.GetDPoWMaturityTime()
    ∧ B ≥ stakeBlock[m] + ChainConfig.GetDPoWMaturityBlocks()
```

where `B = header.Number`, `T = header.Time`.

### 10.3 Block validity rule

```
∀ block b with b.number ≥ DPoWBlock:
    Valid(b) ⟹ Authorized(b.coinbase, b.number, b.timestamp)
```

### 10.4 Chain-level invariants

- **INV-1 — no mining before maturity**: every valid block `b ≥ DPoWBlock` has a coinbase whose stake transaction was included far enough in the past to satisfy both maturity thresholds.
- **INV-2 — storage slot stability**: `slot(miners)=0`, `slot(stakeTime)=1`, `slot(stakeBlock)=2`, `slot(unstakeRequestTime)=3` for all contract versions.
- **INV-3 — immediate unstake**: after `requestUnstake()`, `miners[m]=false` in the same transaction; there is no grace period.

### 10.5 Reorg safety

Authorization depends only on contract state (`miners`, `stakeTime`, `stakeBlock`), which is a deterministic function of the canonical chain. A reorg that keeps the stake transaction at the *same* position preserves `stakeTime` / `stakeBlock`, so a previously valid block stays valid. A reorg that **relocates** the stake transaction — into a different block, with a different timestamp — changes `stakeTime` / `stakeBlock`; a block that depended on the old (earlier) values may then fail a maturity check. Precise guarantee: a block remains valid iff, in the reorged chain, the stake transaction is still confirmed early enough that both maturity thresholds hold for that block.

---

## 11. Operational & Governance Layer

This section documents the runtime governance model — how privileged operations are gated, who can act, and what the blast radius of a compromise is. It complements the contract-level spec in §3.

### 11.1 TimelockController role model

`MinerRegistry`'s `onlyGovernance` modifier requires `msg.sender == address(timelock)`. No EOA or multisig can call `setPaused` or `emergencyRemoveMiner` directly — every privileged call is routed through the OpenZeppelin `TimelockController`.

The Timelock is constructed with `(minDelay, proposers[], executors[], admin)`:

| Role | Granted to | Capability |
|---|---|---|
| `PROPOSER_ROLE` | each address in `proposers[]` | `schedule()` an operation |
| `CANCELLER_ROLE` | each address in `proposers[]` (auto-granted) | `cancel()` a scheduled operation |
| `EXECUTOR_ROLE` | each address in `executors[]` | `execute()` an operation after the delay |
| `DEFAULT_ADMIN_ROLE` | the Timelock contract itself (and `admin`, if non-zero) | `grantRole` / `revokeRole` |

On Incentiv mainnet the deployer EOA is passed as `admin` to enable the §3.1 admin-renounce bootstrap: the initial `CANCELLER_ROLE` grant/revoke runs during setup without a 7-day delay, then Governance raises `minDelay` to 7 days and the deployer renounces `DEFAULT_ADMIN_ROLE`. The end-state is no standing admin; all subsequent role changes are timelocked. Deployments that do not need the inline-hardening shortcut may pass `admin = address(0)` directly.

### 11.2 Every governance operation is delayed; only `cancel()` is instant

| Operation | Path | Delay |
|---|---|---|
| `MinerRegistry.emergencyRemoveMiner` | `onlyGovernance` → schedule → execute | full `minDelay` (7 days) |
| `MinerRegistry.setPaused` | `onlyGovernance` → schedule → execute | full `minDelay` (7 days) |
| `TimelockController.updateDelay` | `onlySelf` → schedule → execute | full *current* `minDelay` |
| `TimelockController.grantRole` / `revokeRole` | `DEFAULT_ADMIN_ROLE` (held only by the Timelock) → schedule → execute | full `minDelay` |
| `TimelockController.cancel` | `CANCELLER_ROLE`, called directly | **instant** |

There is no instant governance mutation. Even reducing the delay (`updateDelay(0)`) is itself subject to the current 7-day delay — so the **first** malicious operation is always visible on-chain for the full window before it can take effect.

### 11.3 Independent canceller (required mainnet hardening)

By default, `proposers` are auto-granted `CANCELLER_ROLE`. If the governance multisig is both the sole proposer and the sole canceller, a *compromised* multisig is also the only party able to cancel its own malicious operation — the 7-day window then has no independent backstop.

**Mitigation**: after deployment, grant `CANCELLER_ROLE` to an independent **guardian** address (a separate multisig with a different signer set, e.g. a security/operations team) **and revoke `CANCELLER_ROLE` from the governance multisig**. Granting alone is necessary but not sufficient: if Governance retains `CANCELLER_ROLE` and is compromised, the attacker can cancel any operation aimed at removing them (e.g. `revokeRole(PROPOSER_ROLE, compromisedGovernance)`), creating a deadlock with no on-chain recovery. After revocation, the only canceller is the independent Guardian — a compromised Governance can still schedule malicious operations, but cannot prevent Guardian from cancelling them.

There are two ways to perform the grant + revoke pair:

- **Through the timelock (default for OZ v4 deployments without an optional admin).** Governance schedules a `Timelock.scheduleBatch` containing `grantRole(CANCELLER_ROLE, guardian)` and `revokeRole(CANCELLER_ROLE, governance)`, waits `minDelay`, and executes. Atomicity matters: doing them separately leaves a window where governance can rescind its own removal.
- **Instantly via the optional admin (used on Incentiv mainnet).** The OZ v5 `TimelockController` constructor accepts an `admin` parameter that receives `DEFAULT_ADMIN_ROLE` and can call `grantRole`/`revokeRole` immediately, no timelock. The deployer EOA is the admin during the deploy script; the grant + revoke are submitted as sequential on-chain transactions (not atomic), so they are executed back-to-back and the resulting role state is verified before the admin is renounced. See §3.1 "Deployment hardening (admin-renounce bootstrap pattern)" for the full lifecycle.

Adding the guardian to the constructor `proposers[]` array is **not** equivalent to either of the above — that would also grant it `PROPOSER_ROLE` (propose power), whereas the guardian must be canceller-only.

### 11.4 Compromise blast radius

A fully compromised governance multisig can, each action subject to the 7-day delay:

- `emergencyRemoveMiner` every miner — redirect all staked WCENT to an attacker address and reduce `activeMinerCount` to zero, halting block production.
- `setPaused(true)` — block new miners from staking; combined with the above, the network cannot self-heal without a hard fork.
- `updateDelay(0)` then subsequent instant operations.
- `grantRole` / `revokeRole` — lock the legitimate signers out.

**Blast radius is bounded to the DPoW subsystem.** Governance controls only `MinerRegistry`. It cannot touch ordinary user balances, cannot mint `WCENT` (which is a 1:1 `deposit`/`withdraw` wrapper with no governance mint), and cannot alter transaction processing. The worst case is loss of miner stakes plus a halted chain.

Recovery is a coordinated social hard fork, **not** a simple binary swap. The client's config-compatibility check in `params/config.go` treats any change to `DPoWBlock`, `MinerRegistryAddress`, `DPoWMaturityTime`, or `DPoWMaturityBlocks` *after* DPoW activation as incompatible, with the rewind target set to the pre-DPoW boundary. A recovery release therefore requires every node to rewind to before `DPoWBlock` and re-sync under the new configuration — see `DPOW_GOVERNANCE_RUNBOOK.md` §8.

### 11.5 Node hardening (mandatory)

DPoW mining with Ethash does **not** require an unlocked etherbase account — the etherbase only receives rewards; no signing happens. Every node's service configuration must therefore:

- Bind HTTP/WS RPC to `127.0.0.1` (or place it behind an authenticated proxy) — never `0.0.0.0` on a public interface.
- Run **without** `--unlock` and **without** `--allow-insecure-unlock`.
- Run **without** `--rpc.allow-unprotected-txs`.
- Set `--txpool.pricelimit` above zero to reject zero-fee spam transactions propagated over P2P.

Miner transactions (`approve`, `stake`, `requestUnstake`, …) are signed externally with `cast --keystore` or a hardware wallet and submitted over local IPC or a restricted RPC. See `DPOW_NODE_OPERATOR_GUIDE.md` for the exact service-file template.

---

## 12. Appendix

### 12.1 Storage slot calculation

```
For mapping(address => T) at base slot S:
  slot(key) = keccak256(abi.encode(key, S))
            = keccak256( leftPad32(key) ++ leftPad32(S) )
```

The Go implementation is `calculateMappingSlot` in `consensus/ethash/dpow.go`; the Solidity side is verified by the raw storage-slot key export in `tests/incentiv/MinerRegistryStorage.t.sol`.

### 12.2 Reference implementations

- Geth: `consensus/ethash/dpow.go`, `consensus/ethash/consensus.go`, `core/state_processor.go`, `params/config.go`, `consensus/errors.go`.
- OpenZeppelin: `TimelockController`, `SafeERC20`. `ReentrancyGuard` is intentionally **not** used (slot-0 collision — see §3.1).

### 12.3 Glossary

| Term | Definition |
|---|---|
| DPoW | Delegated Proof-of-Work — Ethash PoW plus stake-based miner authorization |
| Coinbase / etherbase | The miner address credited in a block header |
| Maturity period | The dual time + block delay before a staked miner may produce blocks |
| `Finalize` / `Process` | Consensus engine entry points where post-execution state is available |
| Storage slot | A 32-byte word in contract storage |
| Guardian | An independent address holding `CANCELLER_ROLE` only |

---

**Document version**: 2.0
