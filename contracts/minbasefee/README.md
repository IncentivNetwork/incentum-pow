# MinBaseFeeGovernor Contract

Smart contract for managing dynamic minimum base fee in Incentum network with timelock security.

## Quick Start

```solidity
// Deploy
MinBaseFeeGovernor governor = new MinBaseFeeGovernor(
    governanceAddress,
    12600000000000,  // 12.6k gwei initial min base fee
    0                // activation block
);

// Propose change
bytes32 proposalId = governor.proposeMinBaseFee(
    15000000000000,  // 15k gwei new min base fee
    block.number + 20000  // activation in ~28 hours (at 5s/block)
);

// Wait 2 days...

// Execute
governor.executeProposal(proposalId);

// Query
uint256 currentMinFee = governor.getCurrentMinBaseFee();
```

## Features

- **Timelock Security**: 2-day delay between proposal and execution
- **Safety Bounds**: Changes limited to 3x/1/3x, values between 1 gwei and 100 ETH
- **Emergency Pause**: Quick response to critical issues
- **Binary Search**: O(log n) historical lookups
- **Full History**: All past configurations preserved

## Documentation

- [Deployment Guide](../../docs/minbasefee/deployment.md) - Complete deployment and operations guide

## Testing

```bash
# Compile contract and generate Go bindings
cd contracts/minbasefee && go generate

# Or compile separately:
node scripts/compile-MinBaseFeeGovernor.js
abigen --abi contracts/minbasefee/MinBaseFeeGovernor.abi --bin contracts/minbasefee/MinBaseFeeGovernor.bin --pkg minbasefee --type MinBaseFeeGovernor --out contracts/minbasefee/bindings.go

# Run integration tests
go test -v ./contracts/minbasefee/test

# All tests should pass:
# TestMinBaseFeeGovernorDeployment
# TestMinBaseFeeGovernorTimelock
# TestMinBaseFeeGovernorAccessControl
# TestMinBaseFeeGovernorSafetyBounds
# TestMinBaseFeeGovernorPause
# TestMinBaseFeeGovernorCancelProposal
```

## Files

- `MinBaseFeeGovernor.sol` - Main contract (source)
- `contract.go` - Go package definition with //go:generate directives
- `test/integration_test.go` - Comprehensive integration tests
- `test/MinBaseFeeGovernor.t.sol` - Solidity tests (Foundry)

**Generated files (not in repo, use `go generate`):**
- `MinBaseFeeGovernor.abi` - Contract ABI
- `MinBaseFeeGovernor.bin` - Contract bytecode
- `bindings.go` - Go bindings for integration

## Go Integration

```go
import "github.com/ethereum/go-ethereum/contracts/minbasefee"

// Deploy
address, tx, contract, err := minbasefee.DeployMinBaseFeeGovernor(...)

// Propose
proposalId, err := contract.ProposeMinBaseFee(auth, newMinBaseFee, activationBlock)

// Execute
tx, err := contract.ExecuteProposal(auth, proposalId)

// Query
currentMinBaseFee, err := contract.GetCurrentMinBaseFee(nil)
```

## Monitoring

Prometheus metrics exported in `consensus/misc/eip1559.go`:
- `chain_minbasefee_current` - Current min base fee
- `chain_minbasefee_readerrors` - Contract read errors
- `chain_basefee_beforefloor` / `afterfloor` - Floor application

## Security

All changes require:
1. Governance address authentication
2. 2-day timelock wait period
3. Safety bounds validation (1 gwei - 100 ETH, max 3x change)
4. 13,000 block activation delay

Emergency controls:
- `pause()` - Halt changes, use fallback value
- `cancelProposal()` - Cancel pending proposal
- `setFallbackMinBaseFee()` - Update emergency fallback

## License

LGPL-3.0-only
