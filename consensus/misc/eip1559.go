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

var (
	// Metrics for monitoring minimum base fee
	minBaseFeeGauge             = metrics.NewRegisteredGauge("chain/minbasefee/current", nil)
	minBaseFeeFromContractGauge = metrics.NewRegisteredGauge("chain/minbasefee/contract", nil)
	minBaseFeeReadErrorsMeter   = metrics.NewRegisteredMeter("chain/minbasefee/readerrors", nil)
	minBaseFeeActiveGauge       = metrics.NewRegisteredGauge("chain/minbasefee/active", nil)
	baseFeeBeforeFloorGauge     = metrics.NewRegisteredGauge("chain/basefee/beforefloor", nil)
	baseFeeAfterFloorGauge      = metrics.NewRegisteredGauge("chain/basefee/afterfloor", nil)
)

// VerifyEip1559Header verifies some header attributes which were changed in EIP-1559,
// - gas limit check
// - basefee check
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
	baseFeeBeforeFloorGauge.Update(baseFee.Int64())
	result := math.BigMax(baseFee, floor)
	baseFeeAfterFloorGauge.Update(result.Int64())
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
		minBaseFeeFromContractGauge.Update(minimumBaseFee.Int64())
		minBaseFeeGauge.Update(minimumBaseFee.Int64())
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
	if config.MinBaseFeeContractAddr == (common.Address{}) {
		return nil, fmt.Errorf("MinBaseFeeContractAddr not configured")
	}

	reader := minbasefee.NewReader(config.MinBaseFeeContractAddr)
	minBaseFee, err := reader.ReadMinBaseFee(stateDB, blockNumber)
	if err != nil {
		return nil, fmt.Errorf("failed to read from contract: %w", err)
	}

	// Validate that the value is reasonable (non-zero and not too large)
	if minBaseFee.Sign() <= 0 {
		return nil, fmt.Errorf("invalid min base fee from contract: %s (must be positive)", minBaseFee)
	}

	// Sanity check: min base fee shouldn't be absurdly large (e.g., > 1000 ETH)
	maxReasonable := new(big.Int).Mul(big.NewInt(1000), big.NewInt(params.Ether))
	if minBaseFee.Cmp(maxReasonable) > 0 {
		return nil, fmt.Errorf("min base fee from contract too large: %s (max %s)", minBaseFee, maxReasonable)
	}

	return minBaseFee, nil
}
