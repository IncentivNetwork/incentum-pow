// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package misc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus/minbasefee"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/params"
)

// ErrDynamicMinBaseFeeNilStateDB is returned by CalcBaseFee when the dynamic
// min base fee fork is active for the parent but the caller did not provide a
// stateDB. Non-consensus callers (GraphQL nextBaseFee, eth_feeHistory, gas
// price helpers) that legitimately cannot hold parent state should match this
// sentinel with errors.Is and degrade gracefully; any other error returned
// from CalcBaseFee indicates a genuine fault and must not be swallowed.
var ErrDynamicMinBaseFeeNilStateDB = errors.New("dynamic min base fee fork active but stateDB is nil")

// Metrics for monitoring minimum base fee. Values that are wei-denominated are
// reported in gwei (wei / 1e9) so they stay representable as int64 even at the
// upper bound of MAX_MIN_BASE_FEE (100 ether); reading the raw wei into Int64()
// silently overflows around 9.22 ether and would corrupt the only out-of-band
// signal the consensus layer has after activation.
var (
	minBaseFeeGwei             = metrics.NewRegisteredGauge("chain/minbasefee/current_gwei", nil)
	minBaseFeeFromContractGwei = metrics.NewRegisteredGauge("chain/minbasefee/contract_gwei", nil)
	minBaseFeeReadErrorsMeter  = metrics.NewRegisteredMeter("chain/minbasefee/readerrors", nil)
	minBaseFeeActiveGauge      = metrics.NewRegisteredGauge("chain/minbasefee/active", nil)
	baseFeeBeforeFloorGwei     = metrics.NewRegisteredGauge("chain/basefee/beforefloor_gwei", nil)
	baseFeeAfterFloorGwei      = metrics.NewRegisteredGauge("chain/basefee/afterfloor_gwei", nil)
)

// weiToGweiClamped converts a wei value to gwei for gauge reporting. Values
// that still exceed int64 (extremely large floors) are clamped to the int64
// max so the gauge stays positive and useful for alerting rather than wrapping
// to a negative number.
func weiToGweiClamped(wei *big.Int) int64 {
	if wei == nil {
		return 0
	}
	gwei := new(big.Int).Quo(wei, big.NewInt(params.GWei))
	if gwei.IsInt64() {
		return gwei.Int64()
	}
	return int64(^uint64(0) >> 1)
}

// VerifyEip1559Header verifies some header attributes which were changed in EIP-1559,
// - gas limit check
// - basefee check
//
// When the dynamic min base fee fork is active for parent.Time and stateDB is
// nil (the standard header-only verification entry from consensus engines,
// from header-first / snap-sync, and from light-client paths), the exact
// equality basefee check is deferred to the block-execution path because the
// contract floor read requires parent state. In that case the header is still
// rejected if `header.BaseFee < calcRawBaseFee(parent)` - the floor can only
// raise the result, never lower it, so the raw EIP-1559 value is a strict
// lower bound that holds without state. core.StateProcessor.Process later
// reruns the full check against the parent post-state and rejects mismatched
// BaseFee there.
func VerifyEip1559Header(config *params.ChainConfig, parent, header *types.Header, stateDB *state.StateDB) error {
	// Verify that the gas limit remains within allowed bounds
	parentGasLimit := parent.GasLimit
	if !config.IsLondon(parent.Number) {
		parentGasLimit = parent.GasLimit * config.ElasticityMultiplier()
	}
	if err := VerifyGaslimit(parentGasLimit, header.GasLimit); err != nil {
		return err
	}
	// Verify the header is not malformed
	if header.BaseFee == nil {
		return fmt.Errorf("header is missing baseFee")
	}
	// Defer the exact-equality basefee check to the block-execution path when
	// the dynamic fork is active but no state is available here. Still enforce
	// the state-free lower and upper bounds so a peer cannot ship a header with
	// an arbitrarily low or high BaseFee and have header-only validation accept
	// it. The contract floor can only raise the result up to
	// params.DynamicMinBaseFeeUpperWei (the on-chain MAX_MIN_BASE_FEE mirror),
	// so any header above max(rawBaseFee, DynamicMinBaseFeeUpperWei) is
	// impossible regardless of contract state.
	if config.IsDynamicMinBaseFee(parent.Time) && stateDB == nil {
		rawBaseFee := calcRawBaseFee(config, parent)
		if header.BaseFee.Cmp(rawBaseFee) < 0 {
			return fmt.Errorf("invalid baseFee: have %s, below raw EIP-1559 value %s (floor check deferred)",
				header.BaseFee, rawBaseFee)
		}
		maxAllowed := math.BigMax(rawBaseFee, params.DynamicMinBaseFeeUpperWei)
		if header.BaseFee.Cmp(maxAllowed) > 0 {
			return fmt.Errorf("invalid baseFee: have %s, above max stateless allowed %s (floor check deferred)",
				header.BaseFee, maxAllowed)
		}
		return nil
	}
	// Verify the baseFee is correct based on the parent header.
	expectedBaseFee, err := CalcBaseFee(config, parent, stateDB)
	if err != nil {
		return fmt.Errorf("baseFee verification: %w", err)
	}
	if header.BaseFee.Cmp(expectedBaseFee) != 0 {
		return fmt.Errorf("invalid baseFee: have %s, want %s, parentBaseFee %s, parentGasUsed %d",
			header.BaseFee, expectedBaseFee, parent.BaseFee, parent.GasUsed)
	}
	return nil
}

// CalcBaseFee calculates the basefee of the header.
//
// When DynamicMinBaseFee fork is active for the parent timestamp, stateDB is
// required (the contract floor is read from it) and any error reading the
// contract is returned as a hard error - there is no silent fallback to the
// legacy hard-coded floor. Pre-activation, or for callers that do not require
// the contract floor (callers passing nil stateDB on a fork-inactive chain),
// the function returns the value computed from the parent header alone.
//
// The floor is applied uniformly to the result in all three branches (gas used
// below, at, or above target), so a governance-driven floor increase takes
// effect on the next block regardless of which branch fires.
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, stateDB *state.StateDB) (*big.Int, error) {
	// Compute the raw EIP-1559 base fee from the parent header alone.
	baseFee := calcRawBaseFee(config, parent)

	// On the first EIP-1559 block the raw value is the initial base fee and
	// no floor is applied (matches the pre-fork behaviour CalcBaseFee had
	// before the dynamic floor was introduced).
	if !config.IsLondon(parent.Number) {
		return baseFee, nil
	}

	// Resolve the active floor. Returns common.Big0 when no floor applies.
	floor, err := computeMinBaseFeeFloor(config, parent, stateDB)
	if err != nil {
		return nil, err
	}
	result := math.BigMax(baseFee, floor)
	if stateDB != nil {
		// Block-path metrics only: skip the prediction/RPC nil-stateDB callers
		// (GraphQL nextBaseFee, eth_feeHistory, eth_gasPrice, pending-tx
		// helpers) so dashboards reflect the most recent state-aware
		// CalcBaseFee result rather than the last RPC prediction. Same
		// policy as `chain/minbasefee/active` and `chain/minbasefee/readerrors`.
		baseFeeBeforeFloorGwei.Update(weiToGweiClamped(baseFee))
		baseFeeAfterFloorGwei.Update(weiToGweiClamped(result))
	}
	return result, nil
}

// calcRawBaseFee returns the EIP-1559 base fee computed from the parent header
// alone, before any min base fee floor is applied. This is exposed so that
// callers without parent state (header-only verification under the dynamic min
// base fee fork) can still enforce the lower-bound invariant that the floor
// only raises the result, never lowers it.
func calcRawBaseFee(config *params.ChainConfig, parent *types.Header) *big.Int {
	if !config.IsLondon(parent.Number) {
		return new(big.Int).SetUint64(params.InitialBaseFee)
	}

	parentGasTarget := parent.GasLimit / config.ElasticityMultiplier()

	switch {
	case parent.GasUsed == parentGasTarget:
		return new(big.Int).Set(parent.BaseFee)
	case parent.GasUsed > parentGasTarget:
		// max(1, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num := new(big.Int).SetUint64(parent.GasUsed - parentGasTarget)
		denom := new(big.Int)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		baseFeeDelta := math.BigMax(num, common.Big1)
		return new(big.Int).Add(parent.BaseFee, baseFeeDelta)
	default:
		num := new(big.Int).SetUint64(parentGasTarget - parent.GasUsed)
		denom := new(big.Int)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		return new(big.Int).Sub(parent.BaseFee, num)
	}
}

// computeMinBaseFeeFloor returns the minimum base fee in effect for the block
// following parent. Priority: dynamic contract floor (when fork active) over
// the legacy hard-coded floor over no floor (returns common.Big0).
func computeMinBaseFeeFloor(config *params.ChainConfig, parent *types.Header, stateDB *state.StateDB) (*big.Int, error) {
	// nextBlockNum addresses the contract's block-based configHistory selection;
	// the fork-activation check itself is timestamp-based and uses parent.Time as a
	// conservative proxy for the new block's intended timestamp.
	nextBlockNum := new(big.Int).Add(parent.Number, common.Big1)

	if config.IsDynamicMinBaseFee(parent.Time) {
		if stateDB == nil {
			// Non-consensus callers (GraphQL nextBaseFee, fee history, gas price,
			// pending helpers) routinely pass nil stateDB and accept the error.
			// Intentionally leave the active gauge alone here so an RPC error
			// path cannot mask a successful import or be mistaken for a
			// contract-read outage by dashboards.
			return nil, fmt.Errorf("%w (parent time %d)", ErrDynamicMinBaseFeeNilStateDB, parent.Time)
		}
		minimumBaseFee, err := readMinBaseFeeFromContract(config, stateDB, nextBlockNum)
		if err != nil {
			minBaseFeeReadErrorsMeter.Mark(1)
			minBaseFeeActiveGauge.Update(0)
			return nil, fmt.Errorf("failed to read min base fee from contract for block %s: %w", nextBlockNum, err)
		}
		minBaseFeeActiveGauge.Update(1)
		minBaseFeeFromContractGwei.Update(weiToGweiClamped(minimumBaseFee))
		minBaseFeeGwei.Update(weiToGweiClamped(minimumBaseFee))
		return minimumBaseFee, nil
	}
	// Block-path metrics only: skip nil-stateDB prediction/header-only callers
	// (GraphQL nextBaseFee, eth_feeHistory, etc.) so a historical pre-activation
	// prediction call cannot reset gauges that should reflect the most recent
	// state-aware CalcBaseFee result. Same policy as readerrors / active in
	// the dynamic-active branch above and the beforefloor/afterfloor gauges in
	// CalcBaseFee.
	if stateDB != nil {
		minBaseFeeActiveGauge.Update(0)
		minBaseFeeFromContractGwei.Update(0)
	}

	if config.IsMinBaseFee(nextBlockNum) {
		var minimumBaseFee *big.Int
		if config.IsMinBaseFeeChange(nextBlockNum) {
			minimumBaseFee = new(big.Int).SetUint64(params.MinBaseFeeUpdated)
		} else {
			minimumBaseFee = new(big.Int).SetUint64(params.MinimumBaseFee)
		}
		if stateDB != nil {
			minBaseFeeGwei.Update(weiToGweiClamped(minimumBaseFee))
		}
		return minimumBaseFee, nil
	}
	if stateDB != nil {
		minBaseFeeGwei.Update(0)
	}
	return common.Big0, nil
}

// readMinBaseFeeFromContract reads the minimum base fee from the governance contract
func readMinBaseFeeFromContract(config *params.ChainConfig, stateDB *state.StateDB, blockNumber *big.Int) (*big.Int, error) {
	if stateDB == nil {
		return nil, fmt.Errorf("stateDB is required for dynamic min base fee")
	}

	// Check if contract address is configured
	addr := config.GetMinBaseFeeContractAddr()
	if addr == (common.Address{}) {
		return nil, fmt.Errorf("MinBaseFeeContractAddr not configured")
	}

	reader := minbasefee.NewReader(addr)
	minBaseFee, err := reader.ReadMinBaseFee(stateDB, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to read from contract: %w", err)
	}

	// Validate against the canonical bounds (single source of truth in params,
	// kept in sync with Solidity MIN_MIN_BASE_FEE / MAX_MIN_BASE_FEE).
	if minBaseFee.Cmp(params.DynamicMinBaseFeeLowerWei) < 0 {
		return nil, fmt.Errorf("min base fee from contract %s below lower bound %s", minBaseFee, params.DynamicMinBaseFeeLowerWei)
	}
	if minBaseFee.Cmp(params.DynamicMinBaseFeeUpperWei) > 0 {
		return nil, fmt.Errorf("min base fee from contract %s above upper bound %s", minBaseFee, params.DynamicMinBaseFeeUpperWei)
	}

	return minBaseFee, nil
}
