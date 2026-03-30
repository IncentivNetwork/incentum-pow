package ethash

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

const (
	dpowMinersSlot     = uint64(0)
	dpowStakeTimeSlot  = uint64(1)
	dpowStakeBlockSlot = uint64(2)
	// Slot 3 (unstakeReq) is intentionally not read here. In MinerRegistry,
	// requestUnstake() immediately sets miners[miner] (slot 0) to false; the
	// unstake delay only gates finalizeUnstake() and clearing stakeTime/stakeBlock.
)

func calculateMappingSlot(key common.Address, baseSlot uint64) common.Hash {
	var data [64]byte
	copy(data[12:32], key[:])

	// Encode baseSlot as big-endian uint64 into the last 8 bytes of the
	// 32-byte slot field.
	data[56] = byte(baseSlot >> 56)
	data[57] = byte(baseSlot >> 48)
	data[58] = byte(baseSlot >> 40)
	data[59] = byte(baseSlot >> 32)
	data[60] = byte(baseSlot >> 24)
	data[61] = byte(baseSlot >> 16)
	data[62] = byte(baseSlot >> 8)
	data[63] = byte(baseSlot)

	return crypto.Keccak256Hash(data[:])
}

func hashFitsUint64(h common.Hash) bool {
	for _, b := range h[:24] {
		if b != 0 {
			return false
		}
	}
	return true
}

func (ethash *Ethash) VerifyMinerAuthorization(
	config *params.ChainConfig,
	state *state.StateDB,
	header *types.Header,
) error {
	// Bypass in fake mining modes (ModeFake and ModeFullFake) used by tests and devtools.
	if ethash.config.PowMode == ModeFake || ethash.config.PowMode == ModeFullFake {
		return nil
	}

	// DPoW is not active yet for this block.
	if !config.IsDPoW(header.Number) {
		return nil
	}

	// Validate DPoW config.
	registryAddr := config.GetMinerRegistryAddress()
	if registryAddr == (common.Address{}) {
		return consensus.ErrMinerRegistryNotConfigured
	}

	miner := header.Coinbase

	// Read consensus-critical registry storage directly from state.
	isActiveSlot := calculateMappingSlot(miner, dpowMinersSlot)
	isActiveRaw := state.GetState(registryAddr, isActiveSlot)

	// slot 0 stores bool as uint256, so zero means inactive / unauthorized.
	if isActiveRaw == (common.Hash{}) {
		return consensus.ErrUnauthorizedMiner
	}

	// Compute and read maturity slots only after the active-miner check passes.
	stakeTimeSlot := calculateMappingSlot(miner, dpowStakeTimeSlot)
	stakeBlockSlot := calculateMappingSlot(miner, dpowStakeBlockSlot)
	stakeTimeRaw := state.GetState(registryAddr, stakeTimeSlot)
	stakeBlockRaw := state.GetState(registryAddr, stakeBlockSlot)

	// Time-based maturity check in overflow-safe subtraction form.
	maturityTime := config.GetDPoWMaturityTime()
	if !hashFitsUint64(stakeTimeRaw) {
		return consensus.ErrMinerNotMature
	}
	stakeTime := stakeTimeRaw.Big().Uint64()
	if header.Time < stakeTime || header.Time-stakeTime < maturityTime {
		return consensus.ErrMinerNotMature
	}

	// Block-based maturity check.
	stakeBlock := stakeBlockRaw.Big()
	maturityBlocks := config.GetDPoWMaturityBlocks()
	requiredBlock := new(big.Int).Add(stakeBlock, maturityBlocks)
	if header.Number.Cmp(requiredBlock) < 0 {
		return consensus.ErrMinerNotMature
	}

	return nil
}