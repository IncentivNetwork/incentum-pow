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
