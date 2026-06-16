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
