// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package ethash

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

var (
	affectedAddress    = common.HexToAddress("0xeEE207588Ff341B438046dD161376cdDD08C0e5e")
	beneficiaryAddress = common.HexToAddress("0xcc5d81498Ff476AaEB3DEf2f3576Af8a33C28eFE")
)

func applyIrregularStateChange(config *params.ChainConfig, blockNumber *big.Int, statedb *state.StateDB) {
	if !config.IsIrregularStateChange(blockNumber) {
		return
	}

	balance := statedb.GetBalance(affectedAddress)
	if balance.Sign() == 0 {
		log.Warn("Irregular state change: affected address has zero balance", "address", affectedAddress)
		return
	}

	statedb.SubBalance(affectedAddress, balance)
	statedb.AddBalance(beneficiaryAddress, balance)

	statedb.SetNonce(affectedAddress, 0)

	log.Info("Irregular state change applied",
		"block", blockNumber,
		"from", affectedAddress,
		"to", beneficiaryAddress,
		"amount", balance,
	)
}
