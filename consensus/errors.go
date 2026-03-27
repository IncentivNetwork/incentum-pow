// Copyright 2017 The go-ethereum Authors
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

package consensus

import "errors"

var (
	// ErrUnknownAncestor is returned when validating a block requires an ancestor
	// that is unknown.
	ErrUnknownAncestor = errors.New("unknown ancestor")

	// ErrPrunedAncestor is returned when validating a block requires an ancestor
	// that is known, but the state of which is not available.
	ErrPrunedAncestor = errors.New("pruned ancestor")

	// ErrFutureBlock is returned when a block's timestamp is in the future according
	// to the current node.
	ErrFutureBlock = errors.New("block in the future")

	// ErrInvalidNumber is returned if a block's number doesn't equal its parent's
	// plus one.
	ErrInvalidNumber = errors.New("invalid block number")

	// ErrInvalidTerminalBlock is returned if a block is invalid wrt. the terminal
	// total difficulty.
	ErrInvalidTerminalBlock = errors.New("invalid terminal block")

	// ErrUnauthorizedMiner is returned when a block's coinbase address is not
	// registered as an active miner in the MinerRegistry contract.
	ErrUnauthorizedMiner = errors.New("unauthorized miner: address not in DPoW registry")

	// ErrMinerNotMature is returned when a block's coinbase address has staked
	// but the required maturity period (time or block count) has not elapsed.
	ErrMinerNotMature = errors.New("miner stake not yet mature: maturity period not elapsed")

	// ErrMinerRegistryNotConfigured is returned when DPoW is active for the
	// current block number but no MinerRegistryAddress is set in ChainConfig.
	ErrMinerRegistryNotConfigured = errors.New("miner registry not configured: dpow block is set but MinerRegistryAddress is not configured")
)
