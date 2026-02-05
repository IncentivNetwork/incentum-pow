# MinBaseFeeGovernor Deployment and Governance Guide

## Overview

The MinBaseFeeGovernor contract manages the minimum base fee threshold for the Incentum network. This guide covers deployment, governance procedures, and operational guidelines.

## Contract Features

### Security Mechanisms

1. **Timelock**: 2-day minimum delay between proposal and execution
2. **Activation Delay**: 13,000 blocks (~18 hours at 5s/block) before changes take effect
3. **Safety Bounds**:
   - Maximum min base fee: 100 ETH
   - Minimum min base fee: 1 gwei
   - Maximum change per proposal: 3x (200% increase) or 1/3x (67% decrease)
4. **Emergency Pause**: Ability to pause the contract and use fallback value
5. **Proposal Cancellation**: Governance can cancel proposals before execution

### Performance

**Contract-based implementation exceeds performance requirements:**
- Overhead: -10% to +3% vs hardcoded constants (target: < 5%)
- CalcBaseFee: ~36-40 ns per call (negligible impact)
- Scales efficiently: O(log n) with 1000+ configs

See [BENCHMARKS.md](BENCHMARKS.md) for detailed performance analysis.

### Key Parameters

- `MIN_TIMELOCK_DELAY`: 2 days (172,800 seconds)
- `MIN_ACTIVATION_DELAY_BLOCKS`: 13,000 blocks (~18 hours at 5s/block)
- `MAX_MIN_BASE_FEE`: 100 ether
- `MIN_MIN_BASE_FEE`: 1 gwei
- `MAX_CHANGE_PERCENT`: 200 (allowing 3x increase or 1/3x decrease)

## Deployment

### Prerequisites

1. Governance address with sufficient funds
2. Initial minimum base fee value (recommended: 12,600 gwei)
3. Activation block number (can be 0 for immediate activation)

### Deployment Steps

1. **Compile the contract and generate Go bindings:**
   ```bash
   # Recommended: use go generate (runs compilation + abigen)
   cd contracts/minbasefee && go generate
   
   # Or manually:
   node scripts/compile-MinBaseFeeGovernor.js
   abigen --abi contracts/minbasefee/MinBaseFeeGovernor.abi --bin contracts/minbasefee/MinBaseFeeGovernor.bin --pkg minbasefee --type MinBaseFeeGovernor --out contracts/minbasefee/bindings.go
   ```

2. **Deploy using your preferred method:**

   **Option A: Using Remix/Hardhat/Truffle:**
   - Import [MinBaseFeeGovernor.sol](contracts/minbasefee/MinBaseFeeGovernor.sol)
   - Constructor parameters:
     - `_governance`: Address of governance account
     - `initialMinBaseFee`: Initial min base fee in wei (e.g., 12600000000000 for 12.6k gwei)
     - `initialActivationBlock`: Block number when initial config activates (0 for genesis)

   **Option B: Using Go bindings:**
   ```go
   import "github.com/ethereum/go-ethereum/contracts/minbasefee"

   initialMinBaseFee := big.NewInt(12600000000000) // 12.6k gwei
   activationBlock := big.NewInt(0)

   address, tx, contract, err := minbasefee.DeployMinBaseFeeGovernor(
       auth,
       client,
       governanceAddress,
       initialMinBaseFee,
       activationBlock,
   )
   ```

3. **Update network configuration:**
   - Add contract address to `params.ChainConfig.MinBaseFeeContractAddr`
   - Set `DynamicMinBaseFeeBlock` to the block where dynamic min base fee activates

4. **Verify deployment:**
   ```bash
   go test -v ./contracts/minbasefee/test -run TestMinBaseFeeGovernorDeployment
   ```

## Governance Procedures

### Proposing a Change

1. **Calculate new min base fee:**
   - Must be between 1 gwei and 100 ETH
   - Cannot change more than 3x or 1/3x from current value
   - Example: If current is 12.6k gwei, new value must be between 4.2k and 37.8k gwei

2. **Determine activation block:**
   - Must be at least 13,000 blocks in the future (~18 hours at 5s/block)
   - Should consider network conditions and community communication time
   - Example: Current block 1,000,000 â†’ minimum activation block 1,013,000

3. **Submit proposal:**
   ```solidity
   bytes32 proposalId = contract.proposeMinBaseFee(
       15000000000000,  // 15k gwei
       1020000          // activation block
   );
   ```

4. **Wait for timelock:**
   - Proposal must wait minimum 2 days before execution
   - During this time, proposal can be cancelled if needed

### Executing a Proposal

1. **Check if proposal is ready:**
   ```solidity
   (uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt,
    bool executed, uint256 executeAfter, bool canExecute) =
       contract.getProposal(proposalId);

   require(canExecute, "Timelock not expired");
   ```

2. **Execute the proposal:**
   ```solidity
   contract.executeProposal(proposalId);
   ```

3. **Verify execution:**
   ```solidity
   uint256 historyLength = contract.getConfigHistoryLength();
   // Should increase by 1
   ```

### Cancelling a Proposal

If a proposal needs to be cancelled (e.g., due to changed network conditions):

```solidity
contract.cancelProposal(proposalId);
```

Note: Cannot cancel already-executed proposals.

### Emergency Procedures

#### Pausing the Contract

If critical issue is discovered:

1. **Pause the contract:**
   ```solidity
   contract.pause();
   ```

2. **Set appropriate fallback value:**
   ```solidity
   contract.setFallbackMinBaseFee(12600000000000); // 12.6k gwei
   ```

3. When paused, `getCurrentMinBaseFee()` returns the fallback value instead of reading from history.

#### Resuming Operations

After issue is resolved:

```solidity
contract.unpause();
```

## Monitoring

### Prometheus Metrics

The following metrics are exported:

- `chain_minbasefee_current`: Current minimum base fee in wei
- `chain_minbasefee_contract`: Value read from contract
- `chain_minbasefee_readerrors`: Count of errors reading from contract
- `chain_minbasefee_active`: Whether dynamic min base fee is active (0 or 1)
- `chain_basefee_beforefloor`: Base fee before applying floor
- `chain_basefee_afterfloor`: Base fee after applying floor

### Log Monitoring

Watch for:
- `MinBaseFeeProposed`: New proposal created
- `ProposalExecuted`: Proposal successfully executed
- `ProposalCancelled`: Proposal cancelled
- `MinBaseFeeScheduled`: New config added to history
- `Paused`/`Unpaused`: Emergency state changes
- Log errors: "Failed to read min base fee from contract"

### Health Checks

Regularly verify:

```solidity
// Check current state
uint256 current = contract.getCurrentMinBaseFee();
bool isPaused = contract.paused();
uint256 historyLength = contract.getConfigHistoryLength();

// Verify history is accessible
for (uint i = 0; i < historyLength; i++) {
    MinBaseFeeConfig memory config = contract.getConfigByIndex(i);
    // Validate config values are reasonable
}
```

## Common Operations

### Querying Current Min Base Fee

```solidity
uint256 currentMinBaseFee = contract.getCurrentMinBaseFee();
```

### Querying Min Base Fee for Specific Block

```solidity
uint256 minBaseFee = contract.getMinBaseFeeForBlock(blockNumber);
```

### Viewing Proposal Details

```solidity
(uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt,
 bool executed, uint256 executeAfter, bool canExecute) =
    contract.getProposal(proposalId);
```

### Listing All Configurations

```solidity
MinBaseFeeConfig[] memory allConfigs = contract.getAllConfigs();
```

### Updating Timelock Delay

```solidity
contract.setTimelockDelay(3 days); // Must be >= MIN_TIMELOCK_DELAY
```

### Transferring Governance

```solidity
contract.transferGovernance(newGovernanceAddress);
```

## Testing

Run integration tests to verify contract functionality:

```bash
# All tests
go test -v ./contracts/minbasefee/test

# Specific test
go test -v ./contracts/minbasefee/test -run TestMinBaseFeeGovernorTimelock
```

## Security Considerations

1. **Timelock Protection**: All changes require 2+ days notice, allowing time for community review
2. **Bounded Changes**: Cannot make extreme changes in single proposal
3. **Activation Delay**: Changes don't take effect immediately, even after execution
4. **Emergency Pause**: Can quickly respond to critical issues
5. **Governance Control**: Only governance address can propose/execute/pause
6. **Historical Immutability**: Past configurations cannot be modified
7. **Fallback Safety**: Pause mechanism provides fallback behavior

## Upgrade Path

The contract does not have upgrade functionality. To upgrade:

1. Deploy new contract with improved logic
2. Update `MinBaseFeeContractAddr` in chain config
3. Coordinate upgrade with network hard fork
4. Old contract remains queryable for historical data

## Appendix: Example Proposal Timeline

**Day 0 - Proposal**
- Block 1,000,000: Submit proposal for 15k gwei, activation at block 1,020,000
- Proposal ID generated and stored
- Community notified

**Day 2 - Execution Window Opens**
- Block ~1,034,560: Timelock expires (2 days = 172,800s = ~34,560 blocks at 5s/block)
- Proposal can now be executed
- `canExecute` returns true

**Day 2-7 - Execution Period**
- Governance executes proposal anytime during this window
- No upper time limit, but prompt execution recommended

**Day 7 - Activation**
- Block 1,020,000: New min base fee takes effect
- Network begins using 15k gwei as minimum
- Previous configurations remain in history for past block queries
