// Copyright 2024 The go-ethereum Authors
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

package minbasefee

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// Storage layout of MinBaseFeeGovernor contract
	// slot 0: governance (address)
	// slot 1: configHistory.length
	// keccak256(1): start of configHistory array

	GovernanceSlot    = 0
	ConfigHistorySlot = 1

	// Fields per MinBaseFeeConfig struct (all uint256)
	ConfigFieldCount     = 3
	MinBaseFeeField      = 0 // minBaseFee
	ActivationBlockField = 1 // activationBlock
	TimestampField       = 2 // timestamp
)

// MinBaseFeeConfig represents a historical configuration entry
type MinBaseFeeConfig struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	Timestamp       *big.Int
}

// Reader provides functionality to read minimum base fee from the governance contract
type Reader struct {
	contractAddress common.Address
}

// NewReader creates a new Reader for the given contract address
func NewReader(contractAddr common.Address) *Reader {
	return &Reader{
		contractAddress: contractAddr,
	}
}

// ReadMinBaseFee reads the minimum base fee value for a given block number from contract state
func (r *Reader) ReadMinBaseFee(stateDB *state.StateDB, blockNumber *big.Int) (*big.Int, error) {
	if stateDB == nil {
		return nil, fmt.Errorf("stateDB is nil")
	}

	// Read the length of configHistory array
	length, err := r.readConfigHistoryLength(stateDB)
	if err != nil {
		return nil, fmt.Errorf("failed to read config history length: %w", err)
	}

	if length == 0 {
		return nil, fmt.Errorf("config history is empty")
	}

	// Binary search to find the active configuration
	config, err := r.findConfigForBlock(stateDB, blockNumber, length)
	if err != nil {
		return nil, fmt.Errorf("failed to find config for block %d: %w", blockNumber, err)
	}

	return config.MinBaseFee, nil
}

// readConfigHistoryLength reads the length of the configHistory array from storage.
// It rejects values that do not fit in a uint64 to keep downstream arithmetic safe.
func (r *Reader) readConfigHistoryLength(stateDB *state.StateDB) (uint64, error) {
	lengthSlot := common.BigToHash(big.NewInt(ConfigHistorySlot))
	lengthValue := stateDB.GetState(r.contractAddress, lengthSlot)
	asBig := lengthValue.Big()
	if asBig.BitLen() > 64 {
		return 0, fmt.Errorf("configHistory length %s exceeds uint64", asBig)
	}
	return asBig.Uint64(), nil
}

// readConfigAt reads a MinBaseFeeConfig at the given index in configHistory array
func (r *Reader) readConfigAt(stateDB *state.StateDB, index uint64) (*MinBaseFeeConfig, error) {
	// Calculate the storage location for configHistory array
	// Array elements start at keccak256(ConfigHistorySlot)
	arrayStartSlot := crypto.Keccak256Hash(common.BigToHash(big.NewInt(ConfigHistorySlot)).Bytes())

	// Each MinBaseFeeConfig struct takes 3 consecutive slots. Defence-in-depth:
	// reject indices whose multiplication by ConfigFieldCount would wrap uint64.
	// In practice this is unreachable (reaching configHistory.length > 2^64/3
	// would take ~6e18 governance cycles), but we match the rest of the reader's
	// "reject corrupted storage" convention rather than silently producing a
	// wrapped slot offset.
	if index > (^uint64(0))/ConfigFieldCount {
		return nil, fmt.Errorf("configHistory index %d overflows slot calculation", index)
	}
	elementOffset := index * ConfigFieldCount

	config := &MinBaseFeeConfig{}

	// Read minBaseFee field
	minBaseFeeSlot := new(big.Int).Add(arrayStartSlot.Big(), new(big.Int).SetUint64(elementOffset+MinBaseFeeField))
	minBaseFeeHash := common.BigToHash(minBaseFeeSlot)
	config.MinBaseFee = stateDB.GetState(r.contractAddress, minBaseFeeHash).Big()

	// Read activationBlock field
	activationBlockSlot := new(big.Int).Add(arrayStartSlot.Big(), new(big.Int).SetUint64(elementOffset+ActivationBlockField))
	activationBlockHash := common.BigToHash(activationBlockSlot)
	config.ActivationBlock = stateDB.GetState(r.contractAddress, activationBlockHash).Big()

	// Read timestamp field
	timestampSlot := new(big.Int).Add(arrayStartSlot.Big(), new(big.Int).SetUint64(elementOffset+TimestampField))
	timestampHash := common.BigToHash(timestampSlot)
	config.Timestamp = stateDB.GetState(r.contractAddress, timestampHash).Big()

	return config, nil
}

// findConfigForBlock performs binary search to find the active configuration for a block
// Returns the last config where activationBlock <= blockNumber
func (r *Reader) findConfigForBlock(stateDB *state.StateDB, blockNumber *big.Int, length uint64) (*MinBaseFeeConfig, error) {
	var (
		left   uint64 = 0
		right  uint64 = length - 1
		result *MinBaseFeeConfig
	)

	for left <= right {
		mid := (left + right) / 2

		config, err := r.readConfigAt(stateDB, mid)
		if err != nil {
			return nil, fmt.Errorf("failed to read config at index %d: %w", mid, err)
		}

		// Check if this config's activation block is <= target block
		if config.ActivationBlock.Cmp(blockNumber) <= 0 {
			result = config
			left = mid + 1
		} else {
			if mid == 0 {
				break
			}
			right = mid - 1
		}
	}

	if result == nil {
		// blockNumber is before configHistory[0].activationBlock. Match the Solidity
		// getMinBaseFeeForBlock semantics by returning the first config entry.
		first, err := r.readConfigAt(stateDB, 0)
		if err != nil {
			return nil, fmt.Errorf("failed to read config[0]: %w", err)
		}
		return first, nil
	}

	return result, nil
}

// ReadAllConfigs reads all configurations from the contract (useful for testing)
func (r *Reader) ReadAllConfigs(stateDB *state.StateDB) ([]MinBaseFeeConfig, error) {
	length, err := r.readConfigHistoryLength(stateDB)
	if err != nil {
		return nil, err
	}

	configs := make([]MinBaseFeeConfig, length)
	for i := uint64(0); i < length; i++ {
		config, err := r.readConfigAt(stateDB, i)
		if err != nil {
			return nil, fmt.Errorf("failed to read config at index %d: %w", i, err)
		}
		configs[i] = *config
	}

	return configs, nil
}

// GetContractAddress returns the address of the MinBaseFeeGovernor contract
func (r *Reader) GetContractAddress() common.Address {
	return r.contractAddress
}
