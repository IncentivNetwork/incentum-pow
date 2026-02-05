# Dynamic Min Base Fee Implementation

## Overview

This document describes the implementation of dynamic minimum base fee management through a governance-controlled smart contract in the Incentum network.

## Motivation

Previously, the minimum base fee was hardcoded in `params/protocol_params.go`:
- `MinimumBaseFee = 40000 gwei` (initial value)
- `MinBaseFeeUpdated = 12600 gwei` (updated value)

These values were applied based on fork blocks defined in chain configuration. Changing these values required a client upgrade and hard fork.

The new implementation allows the minimum base fee to be adjusted dynamically through on-chain governance without requiring client upgrades.

## Architecture

### 1. Smart Contract (`contracts/minbasefee/MinBaseFeeGovernor.sol`)

**Purpose**: Store and manage minimum base fee configurations with historical tracking.

**Key Features**:
- Stores historical configurations with activation blocks
- Governance-controlled updates (via timelock)
- Binary search for efficient historical lookups
- Event emission for all changes

**Storage Layout**:
```solidity
struct MinBaseFeeConfig {
    uint256 minBaseFee;      // Minimum base fee in wei
    uint256 activationBlock; // Block number when active
    uint256 timestamp;       // Timestamp when set
}

MinBaseFeeConfig[] public configHistory;
```

**Main Functions**:
- `getCurrentMinBaseFee()` - Get current active min base fee
- `getMinBaseFeeForBlock(uint256)` - Get min base fee for specific block (historical queries)
- `scheduleMinBaseFee(uint256, uint256)` - Schedule new min base fee (governance only)

**Access Control**: Only the governance address (typically a Timelock contract) can schedule new values.

### 2. State Reader (`consensus/minbasefee/reader.go`)

**Purpose**: Read minimum base fee from contract storage during block validation.

**Key Features**:
- Reads from `stateDB` (avoids external calls during consensus)
- Implements binary search to find active configuration
- Correctly handles Solidity dynamic array storage layout

**Storage Slot Calculation**:
- Array length stored at slot 1
- Array elements start at `keccak256(1)`
- Each struct element occupies 3 consecutive slots (minBaseFee, activationBlock, timestamp)

**Usage**:
```go
reader := minbasefee.NewReader(contractAddress)
minBaseFee, err := reader.ReadMinBaseFee(stateDB, blockNumber)
```

### 3. Core Integration (`consensus/misc/eip1559.go`)

**Modified Functions**:

#### `CalcBaseFee(config, parent, stateDB)`
- **New parameter**: `stateDB *state.StateDB` (optional, can be `nil`)
- **Behavior**:
  - If `DynamicMinBaseFee` fork is active AND `stateDB != nil`: reads from contract
  - Otherwise: uses legacy hardcoded values
  - Falls back gracefully on errors (logs error, uses hardcoded fallback)

#### `VerifyEip1559Header(config, parent, header, stateDB)`
- **New parameter**: `stateDB *state.StateDB` (optional, can be `nil`)
- **Behavior**: Passes `stateDB` to `CalcBaseFee` for validation

**Priority System**:
1. **Dynamic** (contract-based) - if `DynamicMinBaseFeeBlock` fork is active
2. **Legacy hardcoded** - `MinBaseFeeUpdated` or `MinimumBaseFee` based on fork blocks
3. **Zero floor** - before `MinBaseFeeBlock` fork

**Error Handling**:
- Logs errors instead of panicking
- Falls back to legacy behavior on any error
- Validates returned values (non-zero, not absurdly large)

### 4. Chain Configuration (`params/config.go`)

**New Fields**:
```go
type ChainConfig struct {
    // ...
    DynamicMinBaseFeeBlock   *big.Int       `json:"dynamicMinBaseFeeBlock,omitempty"`
    MinBaseFeeContractAddr   common.Address `json:"minBaseFeeContractAddr,omitempty"`
}
```

**New Helper Method**:
```go
func (c *ChainConfig) IsDynamicMinBaseFee(num *big.Int) bool {
    return isBlockForked(c.DynamicMinBaseFeeBlock, num)
}
```

### 5. Consensus Engine Updates

**Modified Files**:
- `consensus/ethash/consensus.go`
- `consensus/clique/clique.go`
- `consensus/beacon/consensus.go`

**Changes**: All engines now pass `nil` as `stateDB` parameter to `VerifyEip1559Header`.

**Reason**: During header-only verification (without state), we fall back to legacy behavior. Full state-based verification happens in `Process` methods.

### 6. Client Integration Points

**All `CalcBaseFee` call sites updated**:
- `core/chain_makers.go` - Block generation (passes `nil`)
- `miner/worker.go` - Mining (passes `nil`)
- `internal/ethapi/api.go` - RPC pending transactions (passes `nil`)
- `eth/gasprice/feehistory.go` - Fee history API (passes `nil`)
- `graphql/graphql.go` - GraphQL API (passes `nil`)
- `core/txpool/txpool.go` - Transaction pool (passes `nil`)
- `core/state_processor_test.go` - Tests (passes `nil`)
- `cmd/evm/internal/t8ntool/transition.go` - EVM tool (passes `nil`)

**Note**: Most call sites pass `nil` for backward compatibility. State-aware calls will be implemented in later phases.

## Implementation Status

### Completed (Phase 1 & 2)

1. **Smart Contract**:
   - `MinBaseFeeGovernor.sol` with full functionality
   - Governance access control
   - Historical configuration tracking
   - Binary search implementation

2. **Go Client Integration**:
   - State reader package (`consensus/minbasefee/reader.go`)
   - `CalcBaseFee` function updated
   - `VerifyEip1559Header` function updated
   - Chain configuration fields added
   - All call sites updated
   - Consensus engines updated

3. **Testing**:
   - Existing unit tests pass (`consensus/misc` tests)
   - Code compiles successfully

### Remaining Work (Phase 3-5)

1. **Solidity Unit Tests**:
   - Contract initialization tests
   - Governance access control tests
   - Binary search correctness tests
   - Edge case handling

2. **Go Integration Tests**:
   - Test with `simulated.Backend`
   - Test contract read during block processing
   - Test fork activation
   - Test historical queries

3. **Performance Benchmarks**:
   - Measure state read overhead
   - Compare dynamic vs hardcoded performance

4. **Documentation**:
   - Storage slot layout documentation
   - Governance process guide
   - Migration guide

5. **Deployment**:
   - Deploy contract to testnet
   - Configure testnet chain config
   - Test end-to-end on testnet
   - Deploy to mainnet
   - Activate fork on mainnet

## Key Design Decisions

### 1. Why read from state instead of calling contract?

During consensus, we cannot make external calls. We read directly from `stateDB` storage, which is already loaded during block processing.

### 2. Why is stateDB optional (can be nil)?

Many parts of the codebase calculate base fee without state context:
- RPC endpoints for pending transactions
- Block template generation
- Historical fee calculations

Passing `nil` triggers fallback to legacy hardcoded values, maintaining backward compatibility.

### 3. Why graceful error handling instead of panicking?

Consensus must be deterministic. If contract read fails (corrupted storage, unexpected format), we fall back to hardcoded values that all nodes agree on, rather than causing chain halt.

### 4. Why maintain legacy hardcoded values?

- Backward compatibility before fork activation
- Fallback if contract read fails
- Support for light clients that may not have full state

### 5. Storage layout for Solidity dynamic arrays

Solidity stores dynamic arrays as:
- Slot N: array length
- `keccak256(N) + index * struct_size`: array elements

Our reader correctly implements this to read `configHistory` array from storage.

## Security Considerations

1. **Governance Control**: Only governance/timelock can update values
2. **Validation**: Client validates returned values (non-zero, reasonable upper bound)
3. **Determinism**: All nodes read same state, produce same results
4. **Fallback Safety**: Errors fall back to known-good hardcoded values
5. **No External Calls**: Direct state reads avoid reentrancy and gas issues

## Future Enhancements

1. **State-aware block generation**: Pass actual state to mining/generation
2. **Caching**: Cache contract reads within same block to reduce overhead
3. **Multi-parameter governance**: Extend to other EIP-1559 parameters
4. **Timelock integration**: Implement full OpenZeppelin Timelock/Governor
5. **Event monitoring**: Index and display governance proposals in block explorer

## References

- Smart Contract: `contracts/minbasefee/MinBaseFeeGovernor.sol`
- State Reader: `consensus/minbasefee/reader.go`
- Core Logic: `consensus/misc/eip1559.go`
- Related Issues: IND-715, IND-716, IND-717, IND-718, IND-719
