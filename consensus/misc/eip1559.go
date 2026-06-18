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
// from header-first / snap-sync, and from light-client paths), the floor
// portion of the basefee check is deliberately skipped: contract floor reads
// require the parent's post-state, which header-only callers do not hold.
// The block's BaseFee will be recomputed against the same parent post-state
// in core.StateProcessor.Process before any transaction runs and a mismatched
// BaseFee is rejected there. Header validation still confirms gas limit and
// that BaseFee is present, so malformed blocks are rejected up front.
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
	// Defer the floor portion of the basefee check to the block-execution path
	// when the dynamic fork is active but no state is available here.
	if config.IsDynamicMinBaseFee(parent.Time) && stateDB == nil {
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
	// If the current block is the first EIP-1559 block, return the InitialBaseFee.
	if !config.IsLondon(parent.Number) {
		return new(big.Int).SetUint64(params.InitialBaseFee), nil
	}

	parentGasTarget := parent.GasLimit / config.ElasticityMultiplier()

	// Compute the raw EIP-1559 base fee from the parent header alone.
	var baseFee *big.Int
	switch {
	case parent.GasUsed == parentGasTarget:
		baseFee = new(big.Int).Set(parent.BaseFee)
	case parent.GasUsed > parentGasTarget:
		// max(1, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num := new(big.Int).SetUint64(parent.GasUsed - parentGasTarget)
		denom := new(big.Int)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		baseFeeDelta := math.BigMax(num, common.Big1)
		baseFee = new(big.Int).Add(parent.BaseFee, baseFeeDelta)
	default:
		num := new(big.Int).SetUint64(parentGasTarget - parent.GasUsed)
		denom := new(big.Int)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		baseFee = new(big.Int).Sub(parent.BaseFee, num)
	}

	// Resolve the active floor. Returns common.Big0 when no floor applies.
	floor, err := computeMinBaseFeeFloor(config, parent, stateDB)
	if err != nil {
		return nil, err
	}
	baseFeeBeforeFloorGwei.Update(weiToGweiClamped(baseFee))
	result := math.BigMax(baseFee, floor)
	baseFeeAfterFloorGwei.Update(weiToGweiClamped(result))
	return result, nil
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
		minBaseFeeActiveGauge.Update(1)
		if stateDB == nil {
			minBaseFeeReadErrorsMeter.Mark(1)
			return nil, fmt.Errorf("dynamic min base fee fork active at parent time %d but stateDB is nil", parent.Time)
		}
		minimumBaseFee, err := readMinBaseFeeFromContract(config, stateDB, nextBlockNum)
		if err != nil {
			minBaseFeeReadErrorsMeter.Mark(1)
			return nil, fmt.Errorf("failed to read min base fee from contract for block %s: %w", nextBlockNum, err)
		}
		minBaseFeeFromContractGwei.Update(weiToGweiClamped(minimumBaseFee))
		minBaseFeeGwei.Update(weiToGweiClamped(minimumBaseFee))
		return minimumBaseFee, nil
	}
	minBaseFeeActiveGauge.Update(0)

	if config.IsMinBaseFee(nextBlockNum) {
		if config.IsMinBaseFeeChange(nextBlockNum) {
			return new(big.Int).SetUint64(params.MinBaseFeeUpdated), nil
		}
		return new(big.Int).SetUint64(params.MinimumBaseFee), nil
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
