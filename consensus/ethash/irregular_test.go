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
