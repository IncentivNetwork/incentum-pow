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

package misc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func u64ptr(v uint64) *uint64 { return &v }

// BenchmarkCalcBaseFee_Hardcoded benchmarks the original hardcoded min base fee approach
func BenchmarkCalcBaseFee_Hardcoded(b *testing.B) {
	config := &params.ChainConfig{
		LondonBlock:            big.NewInt(0),
		MinBaseFeeBlock:        big.NewInt(100),
		MinBaseFeeChangeHeight: big.NewInt(200),
	}

	parent := &types.Header{
		Number:   big.NewInt(299),
		Time:     200,
		GasLimit: 10000000,
		GasUsed:  5000000,
		BaseFee:  big.NewInt(10000000000), // 10 gwei
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CalcBaseFee(config, parent, nil)
	}
}

// BenchmarkCalcBaseFee_ContractRead benchmarks CalcBaseFee with contract-based min base fee
func BenchmarkCalcBaseFee_ContractRead(b *testing.B) {
	// Setup state DB with contract
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")

	// Setup contract state with 1 config entry
	setupContractState(stateDB, contractAddr, 1)

	config := &params.ChainConfig{
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  u64ptr(100),
		MinBaseFeeContractAddr: &contractAddr,
	}

	parent := &types.Header{
		Number:   big.NewInt(299),
		Time:     200,
		GasLimit: 10000000,
		GasUsed:  5000000,
		BaseFee:  big.NewInt(10000000000), // 10 gwei
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CalcBaseFee(config, parent, stateDB)
	}
}

// BenchmarkCalcBaseFee_ContractRead_History10 benchmarks with 10 config entries
func BenchmarkCalcBaseFee_ContractRead_History10(b *testing.B) {
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	setupContractState(stateDB, contractAddr, 10)

	config := &params.ChainConfig{
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  u64ptr(100),
		MinBaseFeeContractAddr: &contractAddr,
	}

	parent := &types.Header{
		Number:   big.NewInt(299),
		Time:     200,
		GasLimit: 10000000,
		GasUsed:  5000000,
		BaseFee:  big.NewInt(10000000000),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CalcBaseFee(config, parent, stateDB)
	}
}

// BenchmarkCalcBaseFee_ContractRead_History100 benchmarks with 100 config entries
func BenchmarkCalcBaseFee_ContractRead_History100(b *testing.B) {
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	setupContractState(stateDB, contractAddr, 100)

	config := &params.ChainConfig{
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  u64ptr(100),
		MinBaseFeeContractAddr: &contractAddr,
	}

	parent := &types.Header{
		Number:   big.NewInt(299),
		Time:     200,
		GasLimit: 10000000,
		GasUsed:  5000000,
		BaseFee:  big.NewInt(10000000000),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = CalcBaseFee(config, parent, stateDB)
	}
}

// BenchmarkVerifyEip1559Header_Full benchmarks full header verification with contract reads
func BenchmarkVerifyEip1559Header_Full(b *testing.B) {
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	contractAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	setupContractState(stateDB, contractAddr, 10)

	config := &params.ChainConfig{
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  u64ptr(100),
		MinBaseFeeContractAddr: &contractAddr,
	}

	parent := &types.Header{
		Number:   big.NewInt(299),
		Time:     200,
		GasLimit: 10000000,
		GasUsed:  5000000,
		BaseFee:  big.NewInt(12600000000000), // 12600 gwei
	}

	// Calculate expected base fee for child
	expectedBaseFee, _ := CalcBaseFee(config, parent, stateDB)

	header := &types.Header{
		Number:   big.NewInt(300),
		GasLimit: 10000000,
		GasUsed:  5000000,
		BaseFee:  expectedBaseFee,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = VerifyEip1559Header(config, parent, header, stateDB)
	}
}

// setupContractState creates a mock contract state with the specified number of config entries
func setupContractState(stateDB *state.StateDB, contractAddr common.Address, numConfigs int) {
	// Write configHistory.length (slot 1)
	lengthSlot := common.BigToHash(big.NewInt(1))
	stateDB.SetState(contractAddr, lengthSlot, common.BigToHash(big.NewInt(int64(numConfigs))))

	// Calculate array start position: keccak256(1)
	arrayStartSlot := common.BigToHash(big.NewInt(1))
	arrayStart := new(big.Int).SetBytes(crypto.Keccak256(arrayStartSlot.Bytes()))

	// Write config entries
	for i := 0; i < numConfigs; i++ {
		baseSlot := new(big.Int).Add(arrayStart, big.NewInt(int64(i*3)))

		// minBaseFee = 12600 gwei
		minBaseFee := big.NewInt(12600000000000)
		stateDB.SetState(contractAddr, common.BigToHash(baseSlot), common.BigToHash(minBaseFee))

		// activationBlock = 100 + i*10000 (spread out activations)
		activationBlock := big.NewInt(int64(100 + i*10000))
		activationSlot := new(big.Int).Add(baseSlot, big.NewInt(1))
		stateDB.SetState(contractAddr, common.BigToHash(activationSlot), common.BigToHash(activationBlock))

		// timestamp = current time (not critical for benchmarks)
		timestamp := big.NewInt(int64(1704067200 + i*86400)) // arbitrary timestamps
		timestampSlot := new(big.Int).Add(baseSlot, big.NewInt(2))
		stateDB.SetState(contractAddr, common.BigToHash(timestampSlot), common.BigToHash(timestamp))
	}

	// Commit the state to ensure it's persisted
	stateDB.Finalise(true)
}
