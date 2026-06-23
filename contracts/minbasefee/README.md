# MinBaseFeeGovernor Contract

Smart contract for managing the dynamic minimum base fee floor in the Incentum
network with timelock-gated governance.

## Quick Start

```solidity
// Deploy. The last two arguments are immutable per-deployment minimums for the
// timelock delay (seconds) and the activation delay (blocks). The Incentiv
// mainnet deployment uses `24 hours` and `13000`; devnet deployments may use
// shorter values.
MinBaseFeeGovernor governor = new MinBaseFeeGovernor(
    governanceAddress,
    12600000000000,  // 12.6k gwei initial min base fee
    0,               // activation block of the initial config
    24 hours,        // minimum timelock delay
    13000            // minimum activation delay (blocks)
);

// Propose change
bytes32 proposalId = governor.proposeMinBaseFee(
    15000000000000,  // 15k gwei new min base fee
    block.number + 20000  // activation in ~28 hours (at 5s/block)
);

// Wait `timelockDelay` (mainnet deployment defaults to 24 hours)...

// Execute
governor.executeProposal(proposalId);

// Query
uint256 currentMinFee = governor.getCurrentMinBaseFee();
```

## Features

- **Timelock Security**: 24-hour delay between proposal and execution on the Incentiv mainnet deployment (per-deployment minimum, immutable).
- **Activation Delay**: 13,000-block delay before a new config takes effect on chain (per-deployment minimum, immutable).
- **Safety Bounds**: Changes limited to ±200 % per proposal, values constrained to `[1 gwei, 100 ether]`.
- **Proposal Cancellation**: Governance can cancel a pending proposal before it executes.
- **Binary Search**: O(log n) historical lookups via the `consensus/minbasefee` Go reader.
- **Full History**: All past configurations preserved in `configHistory[]`.

There is no `pause` lever and no fallback floor in v1; a misbehaving floor is
corrected through the same propose / execute cycle (or by `cancelProposal` if
still pending).

## Documentation

- [Deployment Guide](../../docs/minbasefee/deployment.md) — full deployment and operations guide.
- [Implementation notes](../../docs/dynamic-min-base-fee-implementation.md) — Go consensus integration.

## Testing

```bash
# Compile contract and regenerate Go bindings (Foundry + jq + abigen)
bash scripts/generate-minbasefee-bindings.sh

# Run Foundry tests for the contract itself
FOUNDRY_PROFILE=minbasefee forge test

# Run Go integration tests
go test -v ./contracts/minbasefee/test
```

Go integration tests:

- `TestMinBaseFeeGovernorDeployment`
- `TestMinBaseFeeGovernorTimelock`
- `TestMinBaseFeeGovernorAccessControl`
- `TestMinBaseFeeGovernorSafetyBounds`
- `TestMinBaseFeeGovernorCancelProposal`

## Files

- `MinBaseFeeGovernor.sol` — main contract (source).
- `contract.go` — Go package definition with `//go:generate` directive.
- `bindings.go` — generated Go bindings (committed to the repo).
- `test/integration_test.go` — Go integration tests against a simulated backend.
- `test/MinBaseFeeGovernor.t.sol` — Solidity (Foundry) tests.

Intermediate `MinBaseFeeGovernor.abi` and `.bin` artifacts are produced by
`scripts/generate-minbasefee-bindings.sh` from the Foundry build and are
gitignored.

## Go Integration

```go
import "github.com/ethereum/go-ethereum/contracts/minbasefee"

// Deploy. Mirrors the Solidity constructor's 5 arguments.
address, tx, contract, err := minbasefee.DeployMinBaseFeeGovernor(
    auth,
    backend,
    governanceAddress,
    initialMinBaseFee,
    activationBlock,
    big.NewInt(86400),  // _minTimelockDelay (24 hours on the Incentiv mainnet deployment)
    big.NewInt(13000),  // _minActivationDelayBlocks
)

// Propose
proposalId, err := contract.ProposeMinBaseFee(auth, newMinBaseFee, activationBlock)

// Execute
tx, err := contract.ExecuteProposal(auth, proposalId)

// Query
currentMinBaseFee, err := contract.GetCurrentMinBaseFee(nil)
```

## Monitoring

Prometheus metrics exported in `consensus/misc/eip1559.go`:

- `chain_minbasefee_active` — 1 if the dynamic floor was applied to the most recent block, 0 otherwise.
- `chain_minbasefee_current_gwei` — floor used on the most recent block (gwei).
- `chain_minbasefee_contract_gwei` — last value read from the contract (gwei).
- `chain_minbasefee_readerrors` — count of contract read errors (any non-zero post-activation is an alert).
- `chain_basefee_beforefloor_gwei` / `chain_basefee_afterfloor_gwei` — EIP-1559 base fee before and after the floor is applied (gwei).

## Security

All changes require:

1. Governance address authentication.
2. Timelock wait period (per-deployment minimum, defaults to 2 days).
3. Safety bounds validation (`[1 gwei, 100 ether]`, change ≤ ±200 % per proposal).
4. Activation delay in blocks (per-deployment minimum, defaults to 13 000).

Emergency response is limited to:

- `cancelProposal(proposalId)` — cancel a pending proposal before execution.
- propose a corrective configuration through the normal cycle if a bad value has already been executed (subject to timelock + activation delay).

## License

LGPL-3.0-only
