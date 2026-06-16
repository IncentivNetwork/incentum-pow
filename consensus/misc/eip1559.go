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
	"github.com/ethereum/go-ethereum/log"
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
	expectedBaseFee := CalcBaseFee(config, parent, stateDB)
	if header.BaseFee.Cmp(expectedBaseFee) != 0 {
		return fmt.Errorf("invalid baseFee: have %s, want %s, parentBaseFee %s, parentGasUsed %d",
			header.BaseFee, expectedBaseFee, parent.BaseFee, parent.GasUsed)
	}
	return nil
}

// CalcBaseFee calculates the basefee of the header.
// stateDB is optional and only required when DynamicMinBaseFee fork is active.
func CalcBaseFee(config *params.ChainConfig, parent *types.Header, stateDB *state.StateDB) *big.Int {
	// If the current block is the first EIP-1559 block, return the InitialBaseFee.
	if !config.IsLondon(parent.Number) {
		return new(big.Int).SetUint64(params.InitialBaseFee)
	}

	parentGasTarget := parent.GasLimit / config.ElasticityMultiplier()
	// If the parent gasUsed is the same as the target, the baseFee remains unchanged.
	if parent.GasUsed == parentGasTarget {
		return new(big.Int).Set(parent.BaseFee)
	}

	var (
		num   = new(big.Int)
		denom = new(big.Int)
	)

	if parent.GasUsed > parentGasTarget {
		// If the parent block used more gas than its target, the baseFee should increase.
		// max(1, parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator)
		num.SetUint64(parent.GasUsed - parentGasTarget)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		baseFeeDelta := math.BigMax(num, common.Big1)

		return num.Add(parent.BaseFee, baseFeeDelta)
	} else {
		// Otherwise if the parent block used less gas than its target, the baseFee should decrease.
		// Compute the decrease amount: parentBaseFee * gasUsedDelta / parentGasTarget / baseFeeChangeDenominator,
		// subtract it from parentBaseFee, then apply the minimum base fee floor if the fork is activated.
		num.SetUint64(parentGasTarget - parent.GasUsed)
		num.Mul(num, parent.BaseFee)
		num.Div(num, denom.SetUint64(parentGasTarget))
		num.Div(num, denom.SetUint64(config.BaseFeeChangeDenominator()))
		baseFee := num.Sub(parent.BaseFee, num)

		// Apply minimum base fee floor if MinBaseFee fork is activated for the current block.
		// nextBlockNum addresses the contract's block-based configHistory selection;
		// the fork-activation check itself is timestamp-based and uses parent.Time as a
		// conservative proxy for the new block's intended timestamp (the new block's
		// timestamp will always be >= parent.Time).
		nextBlockNum := new(big.Int).Add(parent.Number, common.Big1)

		// Priority 1: Dynamic min base fee (read from contract)
		if config.IsDynamicMinBaseFee(parent.Time) {
			minBaseFeeActiveGauge.Update(1)
			baseFeeBeforeFloorGauge.Update(baseFee.Int64())

			minimumBaseFee, err := readMinBaseFeeFromContract(config, stateDB, nextBlockNum)
			if err != nil {
				// Log error but don't panic - fall back to previous behavior
				log.Error("Failed to read min base fee from contract, using hardcoded fallback",
					"block", nextBlockNum, "err", err)
				minBaseFeeReadErrorsMeter.Mark(1)
				// Fall through to legacy logic
			} else {
				minBaseFeeFromContractGauge.Update(minimumBaseFee.Int64())
				result := math.BigMax(baseFee, minimumBaseFee)
				baseFeeAfterFloorGauge.Update(result.Int64())
				minBaseFeeGauge.Update(minimumBaseFee.Int64())
				return result
			}
		} else {
			minBaseFeeActiveGauge.Update(0)
		}

		// Priority 2: Legacy hardcoded min base fee logic
		if config.IsMinBaseFee(nextBlockNum) {
			var minimumBaseFee *big.Int
			if config.IsMinBaseFeeChange(nextBlockNum) {
				minimumBaseFee = new(big.Int).SetUint64(params.MinBaseFeeUpdated)
			} else {
				minimumBaseFee = new(big.Int).SetUint64(params.MinimumBaseFee)
			}
			return math.BigMax(baseFee, minimumBaseFee)
		}

		// Before MinBaseFee fork, allow baseFee to decrease to zero
		return math.BigMax(baseFee, common.Big0)
	}
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
