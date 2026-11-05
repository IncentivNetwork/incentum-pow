// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package ethash

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/params"
)

func TestApplyIrregularStateChange(t *testing.T) {
	initialBalance := big.NewInt(1000000000000000000)

	config := &params.ChainConfig{
		IrregularStateChangeHeight: big.NewInt(100),
	}

	db := rawdb.NewMemoryDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	statedb.SetBalance(affectedAddress, initialBalance)
	statedb.SetNonce(affectedAddress, 5)

	applyIrregularStateChange(config, big.NewInt(100), statedb)

	affectedBalance := statedb.GetBalance(affectedAddress)
	receivedBalance := statedb.GetBalance(beneficiaryAddress)
	affectedNonce := statedb.GetNonce(affectedAddress)

	if affectedBalance.Cmp(big.NewInt(0)) != 0 {
		t.Errorf("Expected affected address balance to be 0, got %v", affectedBalance)
	}

	if receivedBalance.Cmp(initialBalance) != 0 {
		t.Errorf("Expected beneficiary balance to be %v, got %v", initialBalance, receivedBalance)
	}

	if affectedNonce != 0 {
		t.Errorf("Expected affected address nonce to be 0, got %v", affectedNonce)
	}
}

func TestApplyIrregularStateChangeWrongBlock(t *testing.T) {
	initialBalance := big.NewInt(1000000000000000000)

	config := &params.ChainConfig{
		IrregularStateChangeHeight: big.NewInt(100),
	}

	db := rawdb.NewMemoryDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	statedb.SetBalance(affectedAddress, initialBalance)

	applyIrregularStateChange(config, big.NewInt(99), statedb)

	affectedBalance := statedb.GetBalance(affectedAddress)

	if affectedBalance.Cmp(initialBalance) != 0 {
		t.Errorf("Expected affected address balance to remain %v, got %v", initialBalance, affectedBalance)
	}
}

func TestApplyIrregularStateChange2(t *testing.T) {
	balance0 := big.NewInt(1000000000000000000)
	balance1 := big.NewInt(2000000000000000000)

	config := &params.ChainConfig{
		IrregularStateChangeHeight2: big.NewInt(200),
	}

	db := rawdb.NewMemoryDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	statedb.SetBalance(affectedAddresses2[0], balance0)
	statedb.SetNonce(affectedAddresses2[0], 5)
	statedb.SetBalance(affectedAddresses2[1], balance1)
	statedb.SetNonce(affectedAddresses2[1], 9)

	applyIrregularStateChange(config, big.NewInt(200), statedb)

	if b := statedb.GetBalance(affectedAddresses2[0]); b.Sign() != 0 {
		t.Errorf("Expected affected address 0 balance to be 0, got %v", b)
	}
	if b := statedb.GetBalance(affectedAddresses2[1]); b.Sign() != 0 {
		t.Errorf("Expected affected address 1 balance to be 0, got %v", b)
	}
	if n := statedb.GetNonce(affectedAddresses2[0]); n != 5 {
		t.Errorf("Expected affected address 0 nonce preserved at 5, got %v", n)
	}
	if n := statedb.GetNonce(affectedAddresses2[1]); n != 9 {
		t.Errorf("Expected affected address 1 nonce preserved at 9, got %v", n)
	}

	want := new(big.Int).Add(balance0, balance1)
	if b := statedb.GetBalance(beneficiaryAddress); b.Cmp(want) != 0 {
		t.Errorf("Expected beneficiary balance to be %v, got %v", want, b)
	}
}

func TestApplyIrregularStateChange2WrongBlock(t *testing.T) {
	balance := big.NewInt(1000000000000000000)

	config := &params.ChainConfig{
		IrregularStateChangeHeight2: big.NewInt(200),
	}

	db := rawdb.NewMemoryDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	statedb.SetBalance(affectedAddresses2[0], balance)

	applyIrregularStateChange(config, big.NewInt(199), statedb)

	if b := statedb.GetBalance(affectedAddresses2[0]); b.Cmp(balance) != 0 {
		t.Errorf("Expected affected address 0 balance to remain %v below the height, got %v", balance, b)
	}

	// The correction is one-shot: it must not fire on any block after the height either.
	applyIrregularStateChange(config, big.NewInt(201), statedb)

	if b := statedb.GetBalance(affectedAddresses2[0]); b.Cmp(balance) != 0 {
		t.Errorf("Expected affected address 0 balance to remain %v above the height, got %v", balance, b)
	}
}

func TestIrregularStateChange2MainnetConfig(t *testing.T) {
	cfg := params.IncentivMainnetChainConfig

	if cfg.IrregularStateChangeHeight2 == nil || cfg.IrregularStateChangeHeight2.Uint64() != 6676700 {
		t.Errorf("mainnet IrregularStateChangeHeight2 = %v, want 6676700", cfg.IrregularStateChangeHeight2)
	}

	want := []common.Address{
		common.HexToAddress("0x232c7a84ff323a977f3Ea494A3b714C5FBAD2b5c"),
		common.HexToAddress("0xeEE207588Ff341B438046dD161376cdDD08C0e5e"),
	}
	if len(affectedAddresses2) != len(want) {
		t.Fatalf("affectedAddresses2 has %d entries, want %d", len(affectedAddresses2), len(want))
	}
	for i, a := range want {
		if affectedAddresses2[i] != a {
			t.Errorf("affectedAddresses2[%d] = %s, want %s", i, affectedAddresses2[i].Hex(), a.Hex())
		}
	}

	if beneficiaryAddress != common.HexToAddress("0xcc5d81498Ff476AaEB3DEf2f3576Af8a33C28eFE") {
		t.Errorf("beneficiaryAddress = %s", beneficiaryAddress.Hex())
	}
}

func TestApplyIrregularStateChange2ZeroBalance(t *testing.T) {
	config := &params.ChainConfig{
		IrregularStateChangeHeight2: big.NewInt(200),
	}

	db := rawdb.NewMemoryDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	// Zero balance but a pending nonce.
	statedb.SetNonce(affectedAddresses2[0], 7)

	applyIrregularStateChange(config, big.NewInt(200), statedb)

	// Nothing to move: the account is left untouched and its nonce is preserved.
	// Resetting it to zero would only make its earlier transactions replayable.
	if n := statedb.GetNonce(affectedAddresses2[0]); n != 7 {
		t.Errorf("Expected zero-balance affected nonce to remain 7, got %d", n)
	}
}

func TestApplyIrregularStateChangeHeightsIndependent(t *testing.T) {
	config := &params.ChainConfig{
		IrregularStateChangeHeight:  big.NewInt(100),
		IrregularStateChangeHeight2: big.NewInt(200),
	}

	balance := big.NewInt(1000000000000000000)
	onlyIn2 := affectedAddresses2[0] // belongs only to the second correction

	db := rawdb.NewMemoryDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	statedb.SetBalance(onlyIn2, balance)

	// At the first height the second correction's address must not be touched.
	applyIrregularStateChange(config, big.NewInt(100), statedb)
	if b := statedb.GetBalance(onlyIn2); b.Cmp(balance) != 0 {
		t.Errorf("Expected second-correction address untouched at first height, got %v", b)
	}

	// At the second height it is drained.
	applyIrregularStateChange(config, big.NewInt(200), statedb)
	if b := statedb.GetBalance(onlyIn2); b.Sign() != 0 {
		t.Errorf("Expected second-correction address drained at second height, got %v", b)
	}
}
