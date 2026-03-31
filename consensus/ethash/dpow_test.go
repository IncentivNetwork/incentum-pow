// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package ethash

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func buildTestState(t *testing.T, registryAddr, minerAddr common.Address, active bool, stakeTime, stakeBlock uint64) *state.StateDB {
	t.Helper()

	db := rawdb.NewMemoryDatabase()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db), nil)
	if err != nil {
		t.Fatalf("state.New() error: %v", err)
	}

	if active {
		statedb.SetState(registryAddr, calculateMappingSlot(minerAddr, dpowMinersSlot), common.BigToHash(big.NewInt(1)))
	}
	if stakeTime > 0 {
		statedb.SetState(registryAddr, calculateMappingSlot(minerAddr, dpowStakeTimeSlot), common.BigToHash(new(big.Int).SetUint64(stakeTime)))
	}
	if stakeBlock > 0 {
		statedb.SetState(registryAddr, calculateMappingSlot(minerAddr, dpowStakeBlockSlot), common.BigToHash(new(big.Int).SetUint64(stakeBlock)))
	}

	return statedb
}

func TestDPoWVerifyMinerAuthorization_DPoWNotActive(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0x1000000000000000000000000000000000000001")
	registryAddr := common.HexToAddress("0x2000000000000000000000000000000000000002")

	config := &params.ChainConfig{
		DPoWBlock: big.NewInt(1000),
	}

	header := &types.Header{
		Number:   big.NewInt(999),
		Time:     100000,
		Coinbase: minerAddr,
	}

	statedb := buildTestState(t, registryAddr, minerAddr, false, 0, 0)

	if err := engine.VerifyMinerAuthorization(config, statedb, header); err != nil {
		t.Fatalf("expected nil error before DPoW activation, got %v", err)
	}
}

func TestDPoWVerifyMinerAuthorization_NoRegistryAddress(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0x3000000000000000000000000000000000000003")

	config := &params.ChainConfig{
		DPoWBlock: big.NewInt(0),
	}

	header := &types.Header{
		Number:   big.NewInt(0),
		Time:     100000,
		Coinbase: minerAddr,
	}

	statedb := buildTestState(t, common.Address{}, minerAddr, false, 0, 0)

	err := engine.VerifyMinerAuthorization(config, statedb, header)
	if err != consensus.ErrMinerRegistryNotConfigured {
		t.Fatalf("expected %v, got %v", consensus.ErrMinerRegistryNotConfigured, err)
	}
}

func TestDPoWCalculateMappingSlot(t *testing.T) {
	addr := common.HexToAddress("0x742d35Cc6634C0532925a3b844Bc454e4438f44e")

	tests := []struct {
		name     string
		slot     uint64
		expected common.Hash
	}{
		{
			name:     "slot0",
			slot:     0,
			expected: common.HexToHash("0x41a7d76393bc0b36368d20b3103b56772fc110d797174cf40d487aa1a02523db"),
		},
		{
			name:     "slot1",
			slot:     1,
			expected: common.HexToHash("0xe6cf09cef7e3dab5cd457845a92b6ae463d44135cb0ac77c8d85401a6bf6b369"),
		},
		{
			name:     "slot2",
			slot:     2,
			expected: common.HexToHash("0x5298782b9584cee1e8dc46f6b3d4cb5ea619f0624947395828c27d436897a051"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateMappingSlot(addr, tt.slot)
			if got != tt.expected {
				t.Fatalf("slot %d: expected %s, got %s", tt.slot, tt.expected.Hex(), got.Hex())
			}
		})
	}
}

func TestDPoWVerifyMinerAuthorization_MinerNotStaked(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0x4000000000000000000000000000000000000004")
	registryAddr := common.HexToAddress("0x5000000000000000000000000000000000000005")

	config := &params.ChainConfig{
		DPoWBlock:            big.NewInt(0),
		MinerRegistryAddress: &registryAddr,
	}

	header := &types.Header{
		Number:   big.NewInt(20000),
		Time:     100000,
		Coinbase: minerAddr,
	}

	statedb := buildTestState(t, registryAddr, minerAddr, false, 0, 0)

	err := engine.VerifyMinerAuthorization(config, statedb, header)
	if err != consensus.ErrUnauthorizedMiner {
		t.Fatalf("expected %v, got %v", consensus.ErrUnauthorizedMiner, err)
	}
}

func TestDPoWVerifyMinerAuthorization_TimeMatureNotMet(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0x6000000000000000000000000000000000000006")
	registryAddr := common.HexToAddress("0x7000000000000000000000000000000000000007")

	config := &params.ChainConfig{
		DPoWBlock:            big.NewInt(0),
		MinerRegistryAddress: &registryAddr,
	}

	header := &types.Header{
		Number:   big.NewInt(20000),
		Time:     100000,
		Coinbase: minerAddr,
	}

	// stakeTime is only 1 hour old, but block maturity is already satisfied.
	statedb := buildTestState(t, registryAddr, minerAddr, true, header.Time-3600, 100)

	err := engine.VerifyMinerAuthorization(config, statedb, header)
	if err != consensus.ErrMinerNotMature {
		t.Fatalf("expected %v, got %v", consensus.ErrMinerNotMature, err)
	}
}

func TestDPoWVerifyMinerAuthorization_BlockMatureNotMet(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0x8000000000000000000000000000000000000008")
	registryAddr := common.HexToAddress("0x9000000000000000000000000000000000000009")

	config := &params.ChainConfig{
		DPoWBlock:            big.NewInt(0),
		MinerRegistryAddress: &registryAddr,
	}

	header := &types.Header{
		Number:   big.NewInt(20000),
		Time:     100000,
		Coinbase: minerAddr,
	}

	// stakeTime is well past time maturity, but stakeBlock is too recent.
	statedb := buildTestState(t, registryAddr, minerAddr, true, header.Time-90000, 19900)

	err := engine.VerifyMinerAuthorization(config, statedb, header)
	if err != consensus.ErrMinerNotMature {
		t.Fatalf("expected %v, got %v", consensus.ErrMinerNotMature, err)
	}
}

func TestDPoWVerifyMinerAuthorization_FullyAuthorized(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0xa00000000000000000000000000000000000000a")
	registryAddr := common.HexToAddress("0xb00000000000000000000000000000000000000b")

	config := &params.ChainConfig{
		DPoWBlock:            big.NewInt(0),
		MinerRegistryAddress: &registryAddr,
	}

	header := &types.Header{
		Number:   big.NewInt(20000),
		Time:     100000,
		Coinbase: minerAddr,
	}

	statedb := buildTestState(t, registryAddr, minerAddr, true, header.Time-90000, 2000)

	if err := engine.VerifyMinerAuthorization(config, statedb, header); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestDPoWVerifyMinerAuthorization_UnstakedMiner(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0xc00000000000000000000000000000000000000c")
	registryAddr := common.HexToAddress("0xd00000000000000000000000000000000000000d")

	config := &params.ChainConfig{
		DPoWBlock:            big.NewInt(0),
		MinerRegistryAddress: &registryAddr,
	}

	header := &types.Header{
		Number:   big.NewInt(20000),
		Time:     100000,
		Coinbase: minerAddr,
	}

	// requestUnstake() sets miners[miner] to false, so slot 0 is zero.
	statedb := buildTestState(t, registryAddr, minerAddr, false, header.Time-90000, 2000)

	err := engine.VerifyMinerAuthorization(config, statedb, header)
	if err != consensus.ErrUnauthorizedMiner {
		t.Fatalf("expected %v, got %v", consensus.ErrUnauthorizedMiner, err)
	}
}

func TestDPoWVerifyMinerAuthorization_StakeTimeOverflow(t *testing.T) {
	engine := &Ethash{config: Config{PowMode: ModeNormal}}
	minerAddr := common.HexToAddress("0xe00000000000000000000000000000000000000e")
	registryAddr := common.HexToAddress("0xf00000000000000000000000000000000000000f")

	config := &params.ChainConfig{
		DPoWBlock:            big.NewInt(0),
		MinerRegistryAddress: &registryAddr,
	}

	header := &types.Header{
		Number:   big.NewInt(20000),
		Time:     100000,
		Coinbase: minerAddr,
	}

	statedb := buildTestState(t, registryAddr, minerAddr, true, 0, 1)

	// Overwrite stakeTime slot with a value that does not fit into uint64.
	slot := calculateMappingSlot(minerAddr, dpowStakeTimeSlot)
	overflow := crypto.Keccak256Hash([]byte("stake-time-overflow"))
	statedb.SetState(registryAddr, slot, overflow)

	err := engine.VerifyMinerAuthorization(config, statedb, header)
	if err != consensus.ErrMinerNotMature {
		t.Fatalf("expected %v, got %v", consensus.ErrMinerNotMature, err)
	}
}
