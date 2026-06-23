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
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
)

// BenchmarkReadMinBaseFee_History1 benchmarks reading with 1 config entry
func BenchmarkReadMinBaseFee_History1(b *testing.B) {
	benchmarkReadMinBaseFee(b, 1)
}

// BenchmarkReadMinBaseFee_History10 benchmarks reading with 10 config entries
func BenchmarkReadMinBaseFee_History10(b *testing.B) {
	benchmarkReadMinBaseFee(b, 10)
}

// BenchmarkReadMinBaseFee_History100 benchmarks reading with 100 config entries
func BenchmarkReadMinBaseFee_History100(b *testing.B) {
	benchmarkReadMinBaseFee(b, 100)
}

// BenchmarkReadMinBaseFee_History1000 benchmarks reading with 1000 config entries
func BenchmarkReadMinBaseFee_History1000(b *testing.B) {
	benchmarkReadMinBaseFee(b, 1000)
}

// BenchmarkReadMinBaseFee_ColdCache simulates cold cache reads
func BenchmarkReadMinBaseFee_ColdCache(b *testing.B) {
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	reader := NewReader(contractAddr)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// Create fresh state DB for each iteration (cold cache)
		db := rawdb.NewMemoryDatabase()
		stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
		setupContractState(stateDB, contractAddr, 10)
		b.StartTimer()

		reader.ReadMinBaseFee(stateDB, big.NewInt(299))
	}
}

// BenchmarkReadMinBaseFee_HotCache simulates hot cache reads
func BenchmarkReadMinBaseFee_HotCache(b *testing.B) {
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	reader := NewReader(contractAddr)

	// Setup state once (hot cache scenario)
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	setupContractState(stateDB, contractAddr, 10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader.ReadMinBaseFee(stateDB, big.NewInt(299))
	}
}

// BenchmarkBinarySearch benchmarks the binary search algorithm with different history sizes
func BenchmarkBinarySearch_History100(b *testing.B) {
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	reader := NewReader(contractAddr)

	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	setupContractState(stateDB, contractAddr, 100)

	blockNumber := big.NewInt(500000) // Search in the middle

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader.findConfigForBlock(stateDB, blockNumber, 100)
	}
}

// BenchmarkBinarySearch_History1000 benchmarks binary search with 1000 entries
func BenchmarkBinarySearch_History1000(b *testing.B) {
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	reader := NewReader(contractAddr)

	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	setupContractState(stateDB, contractAddr, 1000)

	blockNumber := big.NewInt(5000000)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader.findConfigForBlock(stateDB, blockNumber, 1000)
	}
}

// benchmarkReadMinBaseFee is a helper function for benchmarking with different history sizes
func benchmarkReadMinBaseFee(b *testing.B, numConfigs int) {
	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	reader := NewReader(contractAddr)

	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	setupContractState(stateDB, contractAddr, numConfigs)

	blockNumber := big.NewInt(299)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		reader.ReadMinBaseFee(stateDB, blockNumber)
	}
}

// setupContractState creates a mock contract state with the specified number of config entries
func setupContractState(stateDB *state.StateDB, contractAddr common.Address, numConfigs int) {
	// Write configHistory.length (slot 1)
	lengthSlot := common.BigToHash(big.NewInt(ConfigHistorySlot))
	stateDB.SetState(contractAddr, lengthSlot, common.BigToHash(big.NewInt(int64(numConfigs))))

	// Calculate array start position: keccak256(ConfigHistorySlot)
	arrayStartSlot := common.BigToHash(big.NewInt(ConfigHistorySlot))
	arrayStart := new(big.Int).SetBytes(crypto.Keccak256(arrayStartSlot.Bytes()))

	// Write config entries with increasing activation blocks
	for i := 0; i < numConfigs; i++ {
		baseSlot := new(big.Int).Add(arrayStart, big.NewInt(int64(i*ConfigFieldCount)))

		// minBaseFee = 12600 gwei (constant for all configs)
		minBaseFee := big.NewInt(12600000000000)
		stateDB.SetState(contractAddr, common.BigToHash(baseSlot), common.BigToHash(minBaseFee))

		// activationBlock = 100 + i*10000 (spread out activations for binary search testing)
		activationBlock := big.NewInt(int64(100 + i*10000))
		activationSlot := new(big.Int).Add(baseSlot, big.NewInt(ActivationBlockField))
		stateDB.SetState(contractAddr, common.BigToHash(activationSlot), common.BigToHash(activationBlock))

		// timestamp = arbitrary timestamps
		timestamp := big.NewInt(int64(1704067200 + i*86400))
		timestampSlot := new(big.Int).Add(baseSlot, big.NewInt(TimestampField))
		stateDB.SetState(contractAddr, common.BigToHash(timestampSlot), common.BigToHash(timestamp))
	}

	// Commit the state to ensure it's persisted
	stateDB.Finalise(true)
}
