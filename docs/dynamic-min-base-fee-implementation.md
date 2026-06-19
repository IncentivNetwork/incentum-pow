# Dynamic Min Base Fee Implementation

## Overview

The Incentum network's EIP-1559 base fee is computed from the parent header alone (standard formula) and then floored to a value read from the on-chain `MinBaseFeeGovernor` contract. Governance can adjust the floor without a client upgrade once the `DynamicMinBaseFeeTime` fork has activated. Before activation, and on chains that never activate the fork, the floor falls through to the legacy hard-coded values (`MinimumBaseFee` / `MinBaseFeeUpdated`).

## Motivation

Previously, the minimum base fee was hard-coded in `params/protocol_params.go`:

- `MinimumBaseFee = 40000 gwei` (initial value)
- `MinBaseFeeUpdated = 12600 gwei` (updated value)

Changing either constant required a client upgrade and a hard fork. The dynamic implementation lets a governance contract change the floor through a timelock-gated proposal cycle, on-chain, with no client change required after the activation fork.

## Activation

The fork activation is timestamp-based:

```go
type ChainConfig struct {
    // ...
    DynamicMinBaseFeeTime  *uint64         `json:"dynamicMinBaseFeeTime,omitempty"`
    MinBaseFeeContractAddr *common.Address `json:"minBaseFeeContractAddr,omitempty"`
}

func (c *ChainConfig) IsDynamicMinBaseFee(time uint64) bool {
    return isTimestampForked(c.DynamicMinBaseFeeTime, time)
}
```

Both fields are optional. `CheckMinBaseFeeConfig` (called at chain-config load) enforces the invariant that `DynamicMinBaseFeeTime != nil ⇒ MinBaseFeeContractAddr ≠ nil and ≠ zero-address`; misconfigured chains refuse to start. `CheckCompatible` covers the same two fields so an upgrade attempting to change `DynamicMinBaseFeeTime` or `MinBaseFeeContractAddr` after activation is rejected with a clear error.

## Base fee formula

`CalcBaseFee` returns `(*big.Int, error)`. The raw base fee is computed from the parent header in the usual EIP-1559 way, and a floor is applied to the result in all three branches (gas-used below, at, and above target):

```
rawBaseFee = EIP1559(parent.BaseFee, parent.GasUsed, parent.GasLimit)
floor      = computeMinBaseFeeFloor(config, parent, stateDB)
baseFee    = max(rawBaseFee, floor)
```

Applying the floor uniformly means a governance-driven floor increase takes effect on the next block regardless of which gas-usage branch fires.

### Floor resolution

`computeMinBaseFeeFloor` picks the floor in priority order:

1. If `IsDynamicMinBaseFee(parent.Time)` — read the floor from `MinBaseFeeGovernor` storage at the parent's post-state root. A read failure or a missing `stateDB` returns a hard error.
2. Else if the legacy `MinBaseFeeBlock` fork is active for the next block — return `MinimumBaseFee` or `MinBaseFeeUpdated` (depending on `MinBaseFeeChangeHeight`).
3. Else — no floor (`common.Big0`).

The dynamic and legacy paths are mutually exclusive in time: after `DynamicMinBaseFeeTime` fires, the legacy hard-coded values are no longer consulted on that chain.

The fork-activation check uses `parent.Time` as a conservative proxy for the next block's intended timestamp - because the next block's `Time` is always `>= parent.Time`, this keeps the predicate consistent between miner and verifier without requiring the new block's timestamp at floor-resolution time.

## Where the contract is read

EIP-1559 base fee was historically header-only verifiable. Adding a contract-derived floor creates a new state dependency, which cannot be evaluated in the header-only verification path (`VerifyEip1559Header` runs without `stateDB` during block import, snap sync, and headers-first sync).

The shipped implementation resolves this by running the floor verification at block-execution time, where the parent post-state root is already attached:

- **Miner** (`miner/worker.go`): when the dynamic fork is active for `parent.Time`, the miner loads `parent`'s post-state via `chain.StateAt(parent.Root)` and passes it to `CalcBaseFee` while sealing. A state-load failure aborts the seal; the miner does not produce a block with the wrong floor.
- **Verifier** (`core/state_processor.go`): before applying any transaction of the block being processed, `Process` recomputes the expected base fee using the parent header and the `stateDB` it received (which is the parent's post-state at this point) and rejects the block if `header.BaseFee` does not match.

Both sides read the same contract storage from the same state root, so the floor used at sealing always matches the floor enforced at verification.

### Nodes that do not execute blocks

Headers-first / snap-sync phases and light clients do not have parent state available. They accept the block header's claimed `BaseFee` provisionally; full nodes verify the floor when they execute the block. This is an explicit trade-off: light clients on this chain do not independently verify any state-dependent rule, and the dynamic floor is treated the same way.

## Consensus-safety

After activation, contract-read failures (corrupted storage, missing config, value out of bounds) return a hard error from `CalcBaseFee` and from the `state_processor` guard. There is no silent fallback to the legacy hard-coded floor for a fork-active chain — a soft fallback would allow nodes that fail to read to diverge from nodes that succeed and would silently split the chain. Misconfigured chains halt at startup via `CheckMinBaseFeeConfig`; corrupt-state-at-runtime halts the affected node.

Non-consensus callers (txpool pending-fee snapshot, `eth_gasPrice`/`eth_feeHistory`, GraphQL `nextBaseFee`, RPC `NewRPCPendingTransaction`) accept the error and either drop the prediction or return a JSON-RPC error to the caller, rather than guessing.

## MinBaseFeeGovernor (contract)

Located at `contracts/minbasefee/MinBaseFeeGovernor.sol`.

### Storage and bounds

```solidity
struct MinBaseFeeConfig {
    uint256 minBaseFee;
    uint256 activationBlock;
    uint256 timestamp;
}

address public governance;                              // slot 0
MinBaseFeeConfig[] public configHistory;                // slot 1 (length); elements at keccak256(1)
mapping(bytes32 => Proposal) public proposals;          // slot 2
uint256 public immutable MIN_TIMELOCK_DELAY;            // set at deployment
uint256 public timelockDelay;                           // slot 3
uint256 public immutable MIN_ACTIVATION_DELAY_BLOCKS;   // set at deployment

uint256 public constant MIN_MIN_BASE_FEE  = 1 gwei;
uint256 public constant MAX_MIN_BASE_FEE  = 100 ether;
uint256 public constant MAX_CHANGE_PERCENT = 200;  // ±200% per proposal
```

`MIN_MIN_BASE_FEE` and `MAX_MIN_BASE_FEE` are the canonical bounds. The consensus base-fee path (`consensus/misc/eip1559.go`, after the storage reader returns) rejects any value outside `[DynamicMinBaseFeeLowerWei, DynamicMinBaseFeeUpperWei]` in `params/protocol_params.go`, which mirror the Solidity constants. Both the constructor and `proposeMinBaseFee` enforce the same `[MIN_MIN_BASE_FEE, MAX_MIN_BASE_FEE]` range.

### Constructor

```solidity
constructor(
    address _governance,
    uint256 _initialMinBaseFee,
    uint256 _activationBlock,
    uint256 _minTimelockDelay,
    uint256 _minActivationDelayBlocks
)
```

`_minTimelockDelay` and `_minActivationDelayBlocks` are deployment-time parameters (held as immutables, not settable later). Production deployments use 2 days / 13000 blocks; devnet deployments use shorter values (e.g. 10 minutes / 100 blocks) so a full governance cycle fits inside a single test window. The choice is enforced by the deployment script per chain ID, not by an on-chain absolute floor.

### Governance flow

`proposeMinBaseFee(newMinBaseFee, activationBlock)`:

- callable only by `governance`;
- `newMinBaseFee` must be inside `[MIN_MIN_BASE_FEE, MAX_MIN_BASE_FEE]`;
- `newMinBaseFee` must be within `±MAX_CHANGE_PERCENT` of the current effective floor;
- `activationBlock` must be `≥ block.number + MIN_ACTIVATION_DELAY_BLOCKS`;
- `activationBlock` must be strictly greater than the current `configHistory[last].activationBlock`;
- returns a `proposalId`; emits `MinBaseFeeProposed`.

`executeProposal(proposalId)`:

- callable only by `governance`;
- requires `block.timestamp ≥ proposedAt + timelockDelay`;
- **re-validates** that `proposal.activationBlock > configHistory[last].activationBlock` at execute time (two pending proposals whose `activationBlock` values were both above the tail at propose-time can still violate sortedness if executed out of activation order; the re-check rejects the out-of-order execution);
- pushes the new config to `configHistory` and emits `ProposalExecuted` + `MinBaseFeeScheduled`.

`setTimelockDelay(newDelay)`: callable only by `governance`, must be `≥ MIN_TIMELOCK_DELAY`.

`transferGovernance(newGovernance)`: callable only by current `governance`, target must be non-zero.

There is no `pause` lever and no fallback value in the contract. A misbehaving floor is corrected by proposing a new configuration through the normal cycle.

## State reader (`consensus/minbasefee/reader.go`)

The reader pulls `configHistory` directly from `stateDB.GetState(contractAddr, slot)` - no EVM call, no gas, no reentrancy surface.

- Slot `0` holds `governance`.
- Slot `1` holds `configHistory.length`. The reader rejects lengths that do not fit in `uint64`.
- `configHistory` elements live at `keccak256(slot=1) + 3 * index`, with three 32-byte words per element (`minBaseFee`, `activationBlock`, `timestamp`).
- `findConfigForBlock` does a binary search over the array to find the last entry with `activationBlock ≤ blockNumber`. For `blockNumber < configHistory[0].activationBlock`, it returns `configHistory[0]` to match the Solidity `getMinBaseFeeForBlock` semantics.

The block number used for the lookup is `parent.Number + 1`. The fork-activation check that gates the dynamic path uses `parent.Time`; these two predicates serve different purposes and intentionally use different units.

## Metrics

All exposed under `chain/minbasefee/*` and `chain/basefee/*` internally; the Prometheus exporter replaces `/` with `_`, so they appear as `chain_minbasefee_*` / `chain_basefee_*` on the metrics endpoint. Wei-denominated metrics are reported in gwei so they stay inside `int64` even at the upper bound of 100 ether; raw-wei reporting would silently overflow `int64` around 9.22 ether.

| Metric | Type | Meaning |
|---|---|---|
| `chain/minbasefee/active` | gauge | 1 after a state-aware `CalcBaseFee` call successfully read and applied the dynamic floor; 0 after a state-aware contract read failed or when the dynamic fork is inactive. Non-consensus callers that invoke `CalcBaseFee(..., nil)` do not update this gauge. |
| `chain/minbasefee/current_gwei` | gauge | Floor from the most recent state-aware `CalcBaseFee` call (block-path). Non-consensus callers that invoke `CalcBaseFee(..., nil)` do not update this gauge. |
| `chain/minbasefee/contract_gwei` | gauge | Raw contract floor most recently read by a state-aware dynamic-floor call. Non-consensus callers that invoke `CalcBaseFee(..., nil)` do not update this gauge. |
| `chain/minbasefee/readerrors` | meter | Contract-read failures since process start (only counted when stateDB is available and the read itself fails or returns an out-of-bounds value). Non-consensus callers that invoke `CalcBaseFee(..., nil)` are not counted here. Any non-zero value after activation is an alert condition. |
| `chain/basefee/beforefloor_gwei` | gauge | `rawBaseFee` (EIP-1559 result, before flooring) from state-aware `CalcBaseFee` calls. Non-consensus callers that invoke `CalcBaseFee(..., nil)` do not update this gauge. |
| `chain/basefee/afterfloor_gwei` | gauge | `max(rawBaseFee, floor)` (header.BaseFee) from state-aware `CalcBaseFee` calls. Non-consensus callers that invoke `CalcBaseFee(..., nil)` do not update this gauge. |

## Code layout

- `params/config.go` - `DynamicMinBaseFeeTime`, `MinBaseFeeContractAddr`, `IsDynamicMinBaseFee`, `CheckMinBaseFeeConfig`, `CheckCompatible` coverage.
- `params/protocol_params.go` - `DynamicMinBaseFeeLowerWei`, `DynamicMinBaseFeeUpperWei`.
- `consensus/misc/eip1559.go` - `CalcBaseFee`, `VerifyEip1559Header`, `computeMinBaseFeeFloor`, `readMinBaseFeeFromContract`, metrics.
- `consensus/minbasefee/reader.go` - storage-layout-aware reader.
- `contracts/minbasefee/MinBaseFeeGovernor.sol` - governance contract.
- `core/state_processor.go` - block-execution-time floor guard.
- `miner/worker.go` - state-aware base fee computation at sealing time.
