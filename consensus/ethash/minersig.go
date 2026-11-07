// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package ethash

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"golang.org/x/crypto/sha3"
)

const (
	// minerSignatureMaxExtra is the longest extra-data a signed header can carry: the
	// vanity limit of an unsigned one plus the signature.
	minerSignatureMaxExtra = int(params.MaximumExtraDataSize) + crypto.SignatureLength
)

// minerSignatureDomain keeps the signed digest apart from anything else the same key
// signs. Changing the digest format means a new version here.
var minerSignatureDomain = []byte("incentiv/miner-header-sig/v1")

var _ consensus.MinerSignatureVerifier = (*Ethash)(nil)

// AuthorizeMiner sets the key this engine signs mined blocks with. The key lives here and
// not in the account manager, so no RPC method can make it sign anything or reach it.
func (ethash *Ethash) AuthorizeMiner(key *ecdsa.PrivateKey) {
	ethash.lock.Lock()
	defer ethash.lock.Unlock()

	ethash.minerKey = key
}

// MinerSigner returns the address of the key set by AuthorizeMiner, if any.
func (ethash *Ethash) MinerSigner() (common.Address, bool) {
	ethash.lock.Lock()
	defer ethash.lock.Unlock()

	if ethash.minerKey == nil {
		return common.Address{}, false
	}
	return crypto.PubkeyToAddress(ethash.minerKey.PublicKey), true
}

// MinerSignersActive reports whether this engine applies ChainConfig.MinerSignerEpochs.
// The fake modes skip it, as they skip the seal and the DPoW registry.
func (ethash *Ethash) MinerSignersActive(config *params.ChainConfig) bool {
	return len(config.MinerSignerEpochs) > 0 && ethash.config.PowMode != ModeFake && ethash.config.PowMode != ModeFullFake
}

// headerTD returns the total difficulty of header, read from its parent, which is at
// parentNumber. The caller passes it because the header's own number has not been checked
// yet: that is verifyHeader's job, and a wrong one has to reach it, to be rejected as the
// wrong number it is. It returns nil when the rule is off, so that a chain without epochs
// never depends on it.
func (ethash *Ethash) headerTD(chain consensus.ChainHeaderReader, header *types.Header, parentNumber uint64) (*big.Int, error) {
	if !ethash.MinerSignersActive(chain.Config()) {
		return nil, nil
	}
	parentTD := chain.GetTd(header.ParentHash, parentNumber)
	if parentTD == nil {
		return nil, consensus.ErrUnknownAncestor
	}
	return new(big.Int).Add(parentTD, header.Difficulty), nil
}

// batchTDs returns the total difficulty of every header of a batch. The parents inside
// the batch are not in the database yet, so the sum runs from the first parent. A nil
// result means the rule is off; nil entries mean that parent is unknown, which the
// workers report.
//
// The sum stops at the first header that fails Header.SanityCheck, and the entries from there
// on stay nil. A difficulty of any size is added into every total after it, so one of a
// megabyte in front of a batch of small headers would cost the node a megabyte for each of
// them, before a single header is checked. Headers in a response reach this without the
// sanity check, which only a block announcement gets. The first failing header is reported by
// its worker, and a batch is over at its first error.
func (ethash *Ethash) batchTDs(chain consensus.ChainHeaderReader, headers []*types.Header) []*big.Int {
	if !ethash.MinerSignersActive(chain.Config()) {
		return nil
	}
	tds := make([]*big.Int, len(headers))
	td := chain.GetTd(headers[0].ParentHash, headers[0].Number.Uint64()-1)
	if td == nil {
		return tds
	}
	for i, header := range headers {
		if header.SanityCheck() != nil {
			break
		}
		td = new(big.Int).Add(td, header.Difficulty)
		tds[i] = td
	}
	return tds
}

// minerSignatureHash returns the digest a miner signs: SealHash with the signature cut
// off, so it covers exactly what the PoW covers. The PoW in turn covers the signature,
// because SealHash hashes the whole extra-data, so changing a signature on a sealed
// block invalidates its seal.
//
// An encoding that fails writes nothing, which would leave every header with the same digest,
// so the failure is returned. The one input that can cause it, a negative chain id, is what
// ChainConfig.CheckMinerSignerConfig refuses.
func (ethash *Ethash) minerSignatureHash(chainID *big.Int, header *types.Header, vanity []byte) (hash common.Hash, err error) {
	unsigned := *header
	unsigned.Extra = vanity

	hasher := sha3.NewLegacyKeccak256()
	if err := rlp.Encode(hasher, []interface{}{minerSignatureDomain, chainID, ethash.SealHash(&unsigned)}); err != nil {
		return common.Hash{}, err
	}
	hasher.Sum(hash[:0])
	return hash, nil
}

// VerifyMinerSignature checks a header of the given total difficulty against the rule. It
// is for a caller holding a block already, which VerifyHeader would skip as known.
func (ethash *Ethash) VerifyMinerSignature(config *params.ChainConfig, header *types.Header, td *big.Int) error {
	if !ethash.MinerSignersActive(config) {
		return nil
	}
	epoch := config.MinerSignerEpochAt(td)
	if epoch == nil {
		return nil
	}
	return ethash.verifyMinerSignature(config, header, td, epoch)
}

// verifyStoredParent checks the parent a header found in the database against the rule, td being
// the header's own total difficulty. A block can be stored without having passed the rule: a
// binary without it stored the block on a side chain, and the rewind at startup follows the
// canonical chain only. Sent again such a block is refused, but a child comes with a good
// signature of its own and would put it back on the canonical chain. One signature check per
// batch is the whole cost, as the parents inside a batch are verified by the batch.
func (ethash *Ethash) verifyStoredParent(config *params.ChainConfig, header, parent *types.Header, td *big.Int) error {
	if td == nil || parent.Number.Sign() == 0 {
		return nil
	}
	parentTD := new(big.Int).Sub(td, header.Difficulty)
	epoch := config.MinerSignerEpochAt(parentTD)
	if epoch == nil {
		return nil
	}
	if err := ethash.verifyMinerSignature(config, parent, parentTD, epoch); err != nil {
		return fmt.Errorf("parent #%d %s: %w", parent.Number, parent.Hash(), err)
	}
	return nil
}

// minerSignerFor returns the address that has to sign a header of total difficulty td.
//
// A coinbase the epoch lists signs with the listed key. Any other coinbase signs with its
// own: anyone can stake, the registry decides at execution whether it may mine, and the
// maturity period is the time to look at a new miner.
//
// The block crossing into the epoch is the exception, and only a listed signer can make it.
// A fork below the threshold needs no signatures, so a stake made on a private one could
// mature there unseen and carry the fork past the threshold under its own key. Past the
// threshold that cannot happen: a fork's first block already needs a key that is mature in
// the state it forks from, which is a stake everybody could see. A listed signer that crosses
// on top of such a fork does carry its stake across, which is why devp2p stays closed until
// the crossing.
func minerSignerFor(epoch *params.MinerSignerEpoch, header *types.Header, td *big.Int) (common.Address, error) {
	if signer, listed := epoch.Signers[header.Coinbase]; listed {
		return signer, nil
	}
	if new(big.Int).Sub(td, header.Difficulty).Cmp(epoch.TD) < 0 {
		return common.Address{}, fmt.Errorf("coinbase %s is not listed and cannot cross the threshold", header.Coinbase)
	}
	return header.Coinbase, nil
}

// signMinedBlock returns block with the miner signature appended to its extra-data.
//
// It works on block.Header(), a private copy. The caller's header has to stay unsigned
// because the worker assembles from it again, and its extra-data slice is shared between
// all the blocks a worker builds. The signature is deterministic, so assembling the same
// content twice yields the same seal hash and the worker's duplicate check still holds.
func (ethash *Ethash) signMinedBlock(config *params.ChainConfig, block *types.Block, td *big.Int, epoch *params.MinerSignerEpoch) (*types.Block, error) {
	ethash.lock.Lock()
	key := ethash.minerKey
	ethash.lock.Unlock()

	header := block.Header()
	want, err := minerSignerFor(epoch, header, td)
	if err != nil {
		return nil, fmt.Errorf("miner signature: %w", err)
	}
	if key == nil {
		return nil, fmt.Errorf("miner signature: no signer key, coinbase %s needs %s", header.Coinbase, want)
	}
	if signer := crypto.PubkeyToAddress(key.PublicKey); signer != want {
		return nil, fmt.Errorf("miner signature: signer key is %s, coinbase %s needs %s", signer, header.Coinbase, want)
	}
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return nil, fmt.Errorf("miner signature: extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}
	digest, err := ethash.minerSignatureHash(config.ChainID, header, header.Extra)
	if err != nil {
		return nil, fmt.Errorf("miner signature: %w", err)
	}
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		return nil, fmt.Errorf("miner signature: %w", err)
	}
	header.Extra = append(header.Extra, signature...)
	return block.WithSeal(header), nil
}

// verifyMinerSignature checks that header is signed by the address minerSignerFor names.
// Whether the coinbase may mine at all is for the registry check at execution.
func (ethash *Ethash) verifyMinerSignature(config *params.ChainConfig, header *types.Header, td *big.Int, epoch *params.MinerSignerEpoch) error {
	want, err := minerSignerFor(epoch, header, td)
	if err != nil {
		return fmt.Errorf("%w: %v", consensus.ErrInvalidMinerSignature, err)
	}
	// This field is part of the block hash but of neither the digest nor the seal, so
	// accepting it would give any signed block a second valid hash for free. SealHash
	// also panics on it.
	if header.WithdrawalsHash != nil {
		return fmt.Errorf("%w: withdrawals hash set", consensus.ErrInvalidMinerSignature)
	}
	if n := len(header.Extra); n < crypto.SignatureLength || n > minerSignatureMaxExtra {
		return fmt.Errorf("%w: extra-data length %d", consensus.ErrInvalidMinerSignature, n)
	}
	split := len(header.Extra) - crypto.SignatureLength
	vanity, signature := header.Extra[:split], header.Extra[split:]

	// Checked before recovery because cgo and nocgo builds disagree about V above 3
	// and would split on it. This also requires low S.
	r, s := new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:64])
	if !crypto.ValidateSignatureValues(signature[64], r, s, true) {
		return fmt.Errorf("%w: malformed signature", consensus.ErrInvalidMinerSignature)
	}
	digest, err := ethash.minerSignatureHash(config.ChainID, header, vanity)
	if err != nil {
		return fmt.Errorf("%w: %v", consensus.ErrInvalidMinerSignature, err)
	}
	pubkey, err := crypto.SigToPub(digest[:], signature)
	if err != nil {
		return fmt.Errorf("%w: %v", consensus.ErrInvalidMinerSignature, err)
	}
	if signer := crypto.PubkeyToAddress(*pubkey); signer != want {
		return fmt.Errorf("%w: signed by %s, coinbase %s needs %s", consensus.ErrInvalidMinerSignature, signer, header.Coinbase, want)
	}
	return nil
}
