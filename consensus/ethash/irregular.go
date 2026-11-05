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

	affectedAddresses2 = []common.Address{
		common.HexToAddress("0x232c7a84ff323a977f3Ea494A3b714C5FBAD2b5c"),
		common.HexToAddress("0xeEE207588Ff341B438046dD161376cdDD08C0e5e"),
	}
)

func applyIrregularStateChange(config *params.ChainConfig, blockNumber *big.Int, statedb *state.StateDB) {
	switch {
	case config.IsIrregularStateChange(blockNumber):
		moveFullBalance(blockNumber, statedb, affectedAddress, beneficiaryAddress, true)
	case config.IsIrregularStateChange2(blockNumber):
		for _, affected := range affectedAddresses2 {
			moveFullBalance(blockNumber, statedb, affected, beneficiaryAddress, false)
		}
	}
}

func moveFullBalance(blockNumber *big.Int, statedb *state.StateDB, affected, beneficiary common.Address, resetNonce bool) {
	balance := statedb.GetBalance(affected)
	if balance.Sign() == 0 {
		log.Warn("Irregular state change: affected address has zero balance", "address", affected)
		return
	}

	statedb.SubBalance(affected, balance)
	statedb.AddBalance(beneficiary, balance)

	// The first correction resets the nonce to match its deployed behavior; the
	// second keeps it, since resetting to zero can make earlier signed transactions
	// replayable if the account is refunded.
	if resetNonce {
		statedb.SetNonce(affected, 0)
	}

	log.Info("Irregular state change applied",
		"block", blockNumber,
		"from", affected,
		"to", beneficiary,
		"amount", balance,
	)
}
