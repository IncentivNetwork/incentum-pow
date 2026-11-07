// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package ethash

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"math/big"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

// minerSigChain is the part of a chain header verification reads.
type minerSigChain struct {
	config  *params.ChainConfig
	headers map[common.Hash]*types.Header
	tds     map[common.Hash]*big.Int
}

func (c *minerSigChain) Config() *params.ChainConfig  { return c.config }
func (c *minerSigChain) CurrentHeader() *types.Header { return nil }
func (c *minerSigChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	return c.headers[hash]
}
func (c *minerSigChain) GetHeaderByNumber(number uint64) *types.Header { return nil }
func (c *minerSigChain) GetHeaderByHash(hash common.Hash) *types.Header {
	return c.headers[hash]
}
func (c *minerSigChain) GetTd(hash common.Hash, number uint64) *big.Int { return c.tds[hash] }

// minerSigFixture is a chain whose head sits one block below the signature threshold.
type minerSigFixture struct {
	engine   *Ethash
	chain    *minerSigChain
	parent   *types.Header
	key      *ecdsa.PrivateKey
	coinbase common.Address
}

var minerSigKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")

func newMinerSigFixture(t *testing.T) *minerSigFixture {
	t.Helper()
	coinbase := common.HexToAddress("0x47d9940000000000000000000000000000000001")
	parent := &types.Header{
		Number:     big.NewInt(10),
		Time:       1000,
		Difficulty: new(big.Int).Set(params.MinimumDifficulty),
		GasLimit:   30_000_000,
		UncleHash:  types.EmptyUncleHash,
	}
	parentTD := new(big.Int).Mul(params.MinimumDifficulty, big.NewInt(10))
	config := &params.ChainConfig{
		ChainID:             big.NewInt(24101),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		FastBlock:           big.NewInt(0),
		Ethash:              new(params.EthashConfig),
		MinerSignerEpochs: []params.MinerSignerEpoch{{
			TD:      new(big.Int).Add(parentTD, big.NewInt(1)),
			Signers: map[common.Address]common.Address{coinbase: crypto.PubkeyToAddress(minerSigKey.PublicKey)},
		}},
	}
	return &minerSigFixture{
		engine: &Ethash{config: Config{PowMode: ModeNormal}},
		chain: &minerSigChain{
			config:  config,
			headers: map[common.Hash]*types.Header{parent.Hash(): parent},
			tds:     map[common.Hash]*big.Int{parent.Hash(): parentTD},
		},
		parent:   parent,
		key:      minerSigKey,
		coinbase: coinbase,
	}
}

// child returns an unsigned header on top of parent, valid in all but the signature.
func (f *minerSigFixture) child(parent *types.Header) *types.Header {
	time := parent.Time + 5
	return &types.Header{
		ParentHash: parent.Hash(),
		UncleHash:  types.EmptyUncleHash,
		Coinbase:   f.coinbase,
		Root:       common.Hash{0x01},
		Difficulty: CalcDifficulty(f.chain.config, time, parent),
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   parent.GasLimit,
		Time:       time,
	}
}

// sign appends a signature by key over the header as it stands.
func (f *minerSigFixture) sign(header *types.Header, key *ecdsa.PrivateKey) {
	digest, err := f.engine.minerSignatureHash(f.chain.config.ChainID, header, header.Extra)
	if err != nil {
		panic(err)
	}
	signature, err := crypto.Sign(digest[:], key)
	if err != nil {
		panic(err)
	}
	header.Extra = append(append([]byte{}, header.Extra...), signature...)
}

// signParent gives the head of the chain a valid signature, for a threshold moved below it: a
// stored parent past the threshold is judged too.
func (f *minerSigFixture) signParent() {
	td := f.chain.tds[f.parent.Hash()]
	delete(f.chain.headers, f.parent.Hash())
	delete(f.chain.tds, f.parent.Hash())
	f.parent.Coinbase = f.coinbase
	f.sign(f.parent, f.key)
	f.chain.headers[f.parent.Hash()] = f.parent
	f.chain.tds[f.parent.Hash()] = td
}

func (f *minerSigFixture) signed() *types.Header {
	header := f.child(f.parent)
	header.Extra = []byte("vanity")
	f.sign(header, f.key)
	return header
}

func TestMinerSignatureAccepted(t *testing.T) {
	f := newMinerSigFixture(t)
	if err := f.engine.VerifyHeader(f.chain, f.signed(), false); err != nil {
		t.Fatalf("vanity and signature: %v", err)
	}
	header := f.child(f.parent)
	f.sign(header, f.key)
	if err := f.engine.VerifyHeader(f.chain, header, false); err != nil {
		t.Fatalf("signature alone: %v", err)
	}
	header = f.child(f.parent)
	header.Extra = make([]byte, params.MaximumExtraDataSize)
	f.sign(header, f.key)
	if err := f.engine.VerifyHeader(f.chain, header, false); err != nil {
		t.Fatalf("longest vanity: %v", err)
	}
}

func TestMinerSignatureRejected(t *testing.T) {
	stranger, _ := crypto.GenerateKey()
	n := crypto.S256().Params().N

	for _, tt := range []struct {
		name   string
		build  func(f *minerSigFixture) *types.Header
		reason string
	}{
		{"unsigned", func(f *minerSigFixture) *types.Header {
			return f.child(f.parent)
		}, "extra-data length 0"},
		{"vanity without a signature", func(f *minerSigFixture) *types.Header {
			header := f.child(f.parent)
			header.Extra = []byte("vanity")
			return header
		}, "extra-data length 6"},
		{"longest unsigned extra-data", func(f *minerSigFixture) *types.Header {
			header := f.child(f.parent)
			header.Extra = make([]byte, crypto.SignatureLength-1)
			return header
		}, "extra-data length 64"},
		{"vanity over the limit", func(f *minerSigFixture) *types.Header {
			header := f.child(f.parent)
			header.Extra = make([]byte, params.MaximumExtraDataSize+1)
			f.sign(header, f.key)
			return header
		}, "extra-data length 98"},
		{"signed by a stranger", func(f *minerSigFixture) *types.Header {
			header := f.child(f.parent)
			f.sign(header, stranger)
			return header
		}, "signed by"},
		{"crossing the threshold with an unlisted coinbase", func(f *minerSigFixture) *types.Header {
			header := f.child(f.parent)
			header.Coinbase = crypto.PubkeyToAddress(stranger.PublicKey)
			f.sign(header, stranger)
			return header
		}, "cannot cross the threshold"},
		{"coinbase key where another signer is listed", func(f *minerSigFixture) *types.Header {
			f.chain.config.MinerSignerEpochs[0].Signers = map[common.Address]common.Address{
				crypto.PubkeyToAddress(f.key.PublicKey): crypto.PubkeyToAddress(stranger.PublicKey),
			}
			header := f.child(f.parent)
			header.Coinbase = crypto.PubkeyToAddress(f.key.PublicKey)
			f.sign(header, f.key)
			return header
		}, "signed by"},
		{"signed for another chain", func(f *minerSigFixture) *types.Header {
			header := f.child(f.parent)
			digest, err := f.engine.minerSignatureHash(big.NewInt(12730), header, nil)
			if err != nil {
				panic(err)
			}
			header.Extra, _ = crypto.Sign(digest[:], f.key)
			return header
		}, "signed by"},
		{"high S", func(f *minerSigFixture) *types.Header {
			header := f.signed()
			signature := header.Extra[len(header.Extra)-crypto.SignatureLength:]
			s := new(big.Int).SetBytes(signature[32:64])
			new(big.Int).Sub(n, s).FillBytes(signature[32:64])
			signature[64] ^= 1
			return header
		}, "malformed signature"},
		{"V above 1", func(f *minerSigFixture) *types.Header {
			header := f.signed()
			header.Extra[len(header.Extra)-1] += 4
			return header
		}, "malformed signature"},
		{"zero signature", func(f *minerSigFixture) *types.Header {
			header := f.child(f.parent)
			header.Extra = make([]byte, crypto.SignatureLength)
			return header
		}, "malformed signature"},
		{"withdrawals hash set", func(f *minerSigFixture) *types.Header {
			header := f.signed()
			header.WithdrawalsHash = &types.EmptyWithdrawalsHash
			return header
		}, "withdrawals hash"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMinerSigFixture(t)
			err := f.engine.VerifyHeader(f.chain, tt.build(f), false)
			if !errors.Is(err, consensus.ErrInvalidMinerSignature) || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("got %v, want %v with %q", err, consensus.ErrInvalidMinerSignature, tt.reason)
			}
		})
	}
}

// Past the crossing a coinbase that is not listed signs with its own key, and nothing else
// signs for it. Whether it may mine at all is for the registry at execution.
func TestMinerSignatureUnlistedCoinbase(t *testing.T) {
	stranger, _ := crypto.GenerateKey()
	strangerAddr := crypto.PubkeyToAddress(stranger.PublicKey)
	build := func(t *testing.T, coinbase common.Address, key *ecdsa.PrivateKey) (*minerSigFixture, *types.Header) {
		f := newMinerSigFixture(t)
		f.chain.config.MinerSignerEpochs[0].TD = new(big.Int).Set(params.MinimumDifficulty)
		f.signParent()
		header := f.child(f.parent)
		header.Coinbase = coinbase
		if key != nil {
			f.sign(header, key)
		}
		return f, header
	}
	f, header := build(t, strangerAddr, stranger)
	if err := f.engine.VerifyHeader(f.chain, header, false); err != nil {
		t.Fatalf("own key: %v", err)
	}
	for name, tt := range map[string]struct {
		key    *ecdsa.PrivateKey
		reason string
	}{
		"signed by another key": {minerSigKey, "signed by"},
		"unsigned":              {nil, "extra-data length 0"},
	} {
		f, header := build(t, strangerAddr, tt.key)
		err := f.engine.VerifyHeader(f.chain, header, false)
		if !errors.Is(err, consensus.ErrInvalidMinerSignature) || !strings.Contains(err.Error(), tt.reason) {
			t.Errorf("%s: got %v, want %q", name, err, tt.reason)
		}
	}
	// A parent exactly at the threshold has crossed already, so the next block is past it.
	f, header = build(t, strangerAddr, stranger)
	f.chain.config.MinerSignerEpochs[0].TD = new(big.Int).Set(f.chain.tds[f.parent.Hash()])
	if err := f.engine.VerifyHeader(f.chain, header, false); err != nil {
		t.Fatalf("parent at the threshold: %v", err)
	}
	// The block crossing a later epoch needs a listed signer too, whoever mined before it.
	f, header = build(t, strangerAddr, stranger)
	f.chain.config.MinerSignerEpochs = append(f.chain.config.MinerSignerEpochs, params.MinerSignerEpoch{
		TD:      new(big.Int).Add(f.chain.tds[f.parent.Hash()], common.Big1),
		Signers: f.chain.config.MinerSignerEpochs[0].Signers,
	})
	err := f.engine.VerifyHeader(f.chain, header, false)
	if !errors.Is(err, consensus.ErrInvalidMinerSignature) || !strings.Contains(err.Error(), "cannot cross the threshold") {
		t.Errorf("crossing a later epoch: got %v", err)
	}
}

// Every field the seal covers has to be covered by the signature too. A signature taken
// from a block of the listed signer must not validate anything else.
func TestMinerSignatureCoversHeader(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*types.Header)
	}{
		{"parent hash", func(h *types.Header) { h.ParentHash = common.Hash{0xff} }},
		{"uncle hash", func(h *types.Header) { h.UncleHash = common.Hash{0xff} }},
		{"root", func(h *types.Header) { h.Root = common.Hash{0xff} }},
		{"tx hash", func(h *types.Header) { h.TxHash = common.Hash{0xff} }},
		{"receipt hash", func(h *types.Header) { h.ReceiptHash = common.Hash{0xff} }},
		{"bloom", func(h *types.Header) { h.Bloom[0] = 0xff }},
		{"difficulty", func(h *types.Header) { h.Difficulty = new(big.Int).Add(h.Difficulty, common.Big1) }},
		{"number", func(h *types.Header) { h.Number = new(big.Int).Add(h.Number, common.Big1) }},
		{"gas limit", func(h *types.Header) { h.GasLimit++ }},
		{"gas used", func(h *types.Header) { h.GasUsed++ }},
		{"time", func(h *types.Header) { h.Time++ }},
		{"vanity", func(h *types.Header) { h.Extra[0] ^= 0xff }},
		{"base fee", func(h *types.Header) { h.BaseFee = big.NewInt(1) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMinerSigFixture(t)
			header := f.signed()
			tt.mutate(header)
			epoch := &f.chain.config.MinerSignerEpochs[0]
			err := f.engine.verifyMinerSignature(f.chain.config, header, epoch.TD, epoch)
			if !errors.Is(err, consensus.ErrInvalidMinerSignature) {
				t.Fatalf("got %v, want %v", err, consensus.ErrInvalidMinerSignature)
			}
		})
	}
	// The coinbase picks the signer, so changing it fails on the listing instead.
	f := newMinerSigFixture(t)
	header := f.signed()
	header.Coinbase = common.Address{0xff}
	if err := f.engine.VerifyHeader(f.chain, header, false); !errors.Is(err, consensus.ErrInvalidMinerSignature) {
		t.Fatalf("coinbase: got %v", err)
	}
}

// ExcessDataGas is in neither the digest nor the seal, so it must not change the hash of a signed
// block. The generated Header.EncodeRLP leaves it out, unlike the reflection codec its struct tag
// suggests, and on the wire it needs a whole WithdrawalsHash before it, which the rule refuses.
func TestMinerSignatureExcessDataGas(t *testing.T) {
	f := newMinerSigFixture(t)
	header := f.child(f.parent)
	header.BaseFee = big.NewInt(7)
	header.Extra = []byte("vanity")
	f.sign(header, f.key)
	epoch := &f.chain.config.MinerSignerEpochs[0]
	if err := f.engine.verifyMinerSignature(f.chain.config, header, epoch.TD, epoch); err != nil {
		t.Fatalf("signed header: %v", err)
	}

	carrying := types.CopyHeader(header)
	carrying.ExcessDataGas = big.NewInt(1)
	if carrying.Hash() != header.Hash() {
		t.Fatal("ExcessDataGas changes the block hash")
	}
	enc, err := rlp.EncodeToBytes(carrying)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.Header
	if err := rlp.DecodeBytes(enc, &decoded); err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if decoded.ExcessDataGas != nil || decoded.Hash() != header.Hash() {
		t.Fatalf("round trip: excess %v, hash %s, want none and %s", decoded.ExcessDataGas, decoded.Hash(), header.Hash())
	}

	// What a peer can send, which the header's own encoder cannot produce.
	wire := func(withdrawals interface{}) []byte {
		raw, err := rlp.EncodeToBytes([]interface{}{
			header.ParentHash, header.UncleHash, header.Coinbase, header.Root, header.TxHash, header.ReceiptHash,
			header.Bloom, header.Difficulty, header.Number, header.GasLimit, header.GasUsed, header.Time, header.Extra,
			header.MixDigest, header.Nonce, header.BaseFee, withdrawals, big.NewInt(1),
		})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	for name, withdrawals := range map[string]interface{}{
		"empty string": []byte{},
		"empty list":   []interface{}{},
		"short hash":   make([]byte, common.HashLength-1),
	} {
		if err := rlp.DecodeBytes(wire(withdrawals), new(types.Header)); err == nil {
			t.Errorf("%s in place of the withdrawals hash decodes", name)
		}
	}
	decoded = types.Header{}
	if err := rlp.DecodeBytes(wire(common.Hash{}), &decoded); err != nil {
		t.Fatalf("whole withdrawals hash: %v", err)
	}
	if decoded.ExcessDataGas == nil {
		t.Fatal("the field did not come through the wire")
	}
	err = f.engine.verifyMinerSignature(f.chain.config, &decoded, epoch.TD, epoch)
	if !errors.Is(err, consensus.ErrInvalidMinerSignature) || !strings.Contains(err.Error(), "withdrawals hash") {
		t.Fatalf("got %v, want %v with a withdrawals hash", err, consensus.ErrInvalidMinerSignature)
	}
}

// The seal commits to the signature: SealHash hashes the whole extra-data.
func TestSealHashCommitsToMinerSignature(t *testing.T) {
	f := newMinerSigFixture(t)
	header := f.signed()
	sealhash := f.engine.SealHash(header)

	other := types.CopyHeader(header)
	other.Extra[len(other.Extra)-2] ^= 1
	if f.engine.SealHash(other) == sealhash {
		t.Fatal("SealHash ignores the signature")
	}
	nonced := types.CopyHeader(header)
	nonced.Nonce = types.BlockNonce{1}
	nonced.MixDigest = common.Hash{1}
	if f.engine.SealHash(nonced) != sealhash {
		t.Fatal("SealHash depends on the seal fields")
	}
}

// These two pin consensus formats: a change here splits the network from nodes that
// signed or sealed under the old one.
func TestMinerSignatureFormatsPinned(t *testing.T) {
	engine := new(Ethash)
	header := &types.Header{
		ParentHash:  common.HexToHash("0x01"),
		UncleHash:   common.HexToHash("0x02"),
		Coinbase:    common.HexToAddress("0x03"),
		Root:        common.HexToHash("0x04"),
		TxHash:      common.HexToHash("0x05"),
		ReceiptHash: common.HexToHash("0x06"),
		Difficulty:  big.NewInt(7),
		Number:      big.NewInt(8),
		GasLimit:    9,
		GasUsed:     10,
		Time:        11,
		Extra:       []byte("vanity"),
		BaseFee:     big.NewInt(12),
	}
	if have, want := engine.SealHash(header), common.HexToHash("0x781635010bc7d887b789999412eee31ad05332b8cc8634e880a248fa2189f3ab"); have != want {
		t.Errorf("SealHash: have %x, want %x", have, want)
	}
	have, err := engine.minerSignatureHash(big.NewInt(24101), header, header.Extra)
	if err != nil {
		t.Fatal(err)
	}
	if want := common.HexToHash("0x474c9aa075ba0ed1eb93d36723c344e2dbe51b4756f460df4a2a4cd0bde492a7"); have != want {
		t.Errorf("minerSignatureHash: have %x, want %x", have, want)
	}
}

// RLP cannot encode a negative chain id. CheckMinerSignerConfig refuses one, and a config that
// gets past it still must not leave every header with the digest of nothing, under which one
// signature would fit them all.
func TestMinerSignatureUnencodableChainID(t *testing.T) {
	f := newMinerSigFixture(t)
	f.chain.config.ChainID = big.NewInt(-1)

	header := f.child(f.parent)
	var err error
	if header.Extra, err = crypto.Sign(crypto.Keccak256(nil), f.key); err != nil {
		t.Fatal(err)
	}
	if err := f.engine.VerifyHeader(f.chain, header, false); !errors.Is(err, consensus.ErrInvalidMinerSignature) {
		t.Fatalf("verifying: got %v", err)
	}
	f.engine.AuthorizeMiner(f.key)
	if _, err := f.assemble(t, f.child(f.parent)); err == nil || !strings.Contains(err.Error(), "miner signature") {
		t.Fatalf("assembling: got %v", err)
	}
}

// A stored parent is judged when its child arrives, because a block can be in the database
// without having passed the rule: a binary without it stored the block, and a rewind only follows
// the canonical chain. Parents inside a batch are the batch's own business.
func TestMinerSignatureStoredParent(t *testing.T) {
	threshold := func(f *minerSigFixture) *big.Int { return f.chain.config.MinerSignerEpochs[0].TD }
	// stored puts parent in the chain with the given total difficulty, and returns its child.
	stored := func(f *minerSigFixture, parent *types.Header, td *big.Int) *types.Header {
		f.chain.headers[parent.Hash()] = parent
		f.chain.tds[parent.Hash()] = td
		child := f.child(parent)
		f.sign(child, f.key)
		return child
	}
	verify := func(f *minerSigFixture, child *types.Header) []error {
		_, results := f.engine.VerifyHeaders(f.chain, []*types.Header{child}, []bool{false})
		return []error{f.engine.VerifyHeader(f.chain, child, false), <-results}
	}
	for _, tt := range []struct {
		name   string
		parent func(f *minerSigFixture) (*types.Header, *big.Int)
		reason string
	}{
		{"unsigned and past the threshold", func(f *minerSigFixture) (*types.Header, *big.Int) {
			return f.child(f.parent), new(big.Int).Mul(threshold(f), common.Big2)
		}, "extra-data length 0"},
		{"unsigned and crossing", func(f *minerSigFixture) (*types.Header, *big.Int) {
			return f.child(f.parent), new(big.Int).Set(threshold(f))
		}, "extra-data length 0"},
		{"crossing with an unlisted coinbase", func(f *minerSigFixture) (*types.Header, *big.Int) {
			header := f.child(f.parent)
			header.Coinbase = common.Address{0xff}
			return header, new(big.Int).Set(threshold(f))
		}, "cannot cross the threshold"},
		{"signed", func(f *minerSigFixture) (*types.Header, *big.Int) {
			return f.signed(), new(big.Int).Mul(threshold(f), common.Big2)
		}, ""},
		{"unsigned below the threshold", func(f *minerSigFixture) (*types.Header, *big.Int) {
			return f.child(f.parent), new(big.Int).Sub(threshold(f), common.Big1)
		}, ""},
		{"the genesis block past the threshold", func(f *minerSigFixture) (*types.Header, *big.Int) {
			header := f.child(f.parent)
			header.Number = new(big.Int)
			return header, new(big.Int).Set(threshold(f))
		}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMinerSigFixture(t)
			parent, td := tt.parent(f)
			for i, err := range verify(f, stored(f, parent, td)) {
				path := []string{"VerifyHeader", "VerifyHeaders"}[i]
				if tt.reason == "" {
					if err != nil {
						t.Errorf("%s: %v", path, err)
					}
				} else if !errors.Is(err, consensus.ErrInvalidMinerSignature) || !strings.Contains(err.Error(), tt.reason) {
					t.Errorf("%s: got %v, want an error with %q", path, err, tt.reason)
				}
			}
		})
	}
	// Inside a batch only the first parent is the database's. The block after it is checked as
	// its own header, and a failure of the parent is reported once.
	t.Run("batch", func(t *testing.T) {
		f := newMinerSigFixture(t)
		first := f.signed()
		second := f.child(first)
		f.sign(second, f.key)
		_, results := f.engine.VerifyHeaders(f.chain, []*types.Header{first, second}, []bool{false, false})
		for _, header := range []*types.Header{first, second} {
			if err := <-results; err != nil {
				t.Errorf("#%d: %v", header.Number, err)
			}
		}
	})
}

// Below the threshold nothing changes: no signature is needed, and the old limit holds.
func TestMinerSignatureBelowThreshold(t *testing.T) {
	f := newMinerSigFixture(t)
	f.chain.config.MinerSignerEpochs[0].TD = new(big.Int).Mul(params.MinimumDifficulty, big.NewInt(100))

	header := f.child(f.parent)
	header.Coinbase = common.Address{0xff}
	header.Extra = make([]byte, params.MaximumExtraDataSize)
	if err := f.engine.VerifyHeader(f.chain, header, false); err != nil {
		t.Fatalf("unsigned header below the threshold: %v", err)
	}
	header = f.child(f.parent)
	header.Extra = make([]byte, params.MaximumExtraDataSize+1)
	if err := f.engine.VerifyHeader(f.chain, header, false); err == nil || !strings.Contains(err.Error(), "extra-data too long") {
		t.Fatalf("long extra-data below the threshold: %v", err)
	}
}

func TestMinerSignatureRuleOff(t *testing.T) {
	// No epochs configured: no total difficulty is read, so a missing one does not matter.
	f := newMinerSigFixture(t)
	f.chain.config.MinerSignerEpochs = nil
	f.chain.tds = nil
	if err := f.engine.VerifyHeader(f.chain, f.child(f.parent), false); err != nil {
		t.Fatalf("no epochs: %v", err)
	}
	// The fake modes skip it, as they skip the seal and the DPoW registry: when verifying,
	// in the startup check, and when assembling, which then reads no total difficulty.
	for name, engine := range map[string]*Ethash{"fake": NewFaker(), "full fake": NewFullFaker()} {
		f = newMinerSigFixture(t)
		f.engine = engine
		header := f.child(f.parent)
		if err := engine.VerifyHeader(f.chain, header, false); err != nil {
			t.Fatalf("%s mode, VerifyHeader: %v", name, err)
		}
		if err := engine.VerifyMinerSignature(f.chain.config, header, f.chain.config.MinerSignerEpochs[0].TD); err != nil {
			t.Fatalf("%s mode, VerifyMinerSignature: %v", name, err)
		}
		f.chain.tds = nil
		block, err := f.assemble(t, header)
		if err != nil {
			t.Fatalf("%s mode, assembling: %v", name, err)
		}
		if len(block.Extra()) != 0 {
			t.Fatalf("%s mode, assembling: extra-data %x", name, block.Extra())
		}
	}
	// ModeFake treats every header as below the threshold, so it keeps the 32-byte limit.
	f = newMinerSigFixture(t)
	f.engine = NewFaker()
	if err := f.engine.VerifyHeader(f.chain, f.signed(), false); err == nil || !strings.Contains(err.Error(), "extra-data too long") {
		t.Fatalf("fake mode, signed header: %v", err)
	}
}

func TestMinerSignatureUnknownParentTD(t *testing.T) {
	f := newMinerSigFixture(t)
	f.chain.tds = nil
	if err := f.engine.VerifyHeader(f.chain, f.signed(), false); !errors.Is(err, consensus.ErrUnknownAncestor) {
		t.Fatalf("got %v, want %v", err, consensus.ErrUnknownAncestor)
	}
	// The miner too: unknown is an error, not an unsigned block.
	f.engine.AuthorizeMiner(f.key)
	if _, err := f.assemble(t, f.child(f.parent)); !errors.Is(err, consensus.ErrUnknownAncestor) {
		t.Fatalf("assembling: got %v, want %v", err, consensus.ErrUnknownAncestor)
	}
}

// A batch reads its parents' total difficulty from itself, so the rule starts at the
// right header in the middle of a batch.
func TestVerifyHeadersAcrossThreshold(t *testing.T) {
	f := newMinerSigFixture(t)
	// Two headers fit below the threshold, the third reaches it.
	f.chain.config.MinerSignerEpochs[0].TD = new(big.Int).Mul(params.MinimumDifficulty, big.NewInt(13))

	first := f.child(f.parent)
	second := f.child(first)
	third := f.child(second)
	verify := func(headers []*types.Header) []error {
		seals := make([]bool, len(headers))
		_, results := f.engine.VerifyHeaders(f.chain, headers, seals)
		errs := make([]error, len(headers))
		for i := range headers {
			errs[i] = <-results
		}
		return errs
	}
	errs := verify([]*types.Header{first, second, third})
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("below the threshold: %v", errs)
	}
	if !errors.Is(errs[2], consensus.ErrInvalidMinerSignature) {
		t.Fatalf("unsigned at the threshold: %v", errs[2])
	}
	f.sign(third, f.key)
	for i, err := range verify([]*types.Header{first, second, third}) {
		if err != nil {
			t.Fatalf("header %d of a valid batch: %v", i, err)
		}
	}
	// An unknown first parent fails every header rather than panicking on a nil sum.
	f.chain.tds = nil
	for i, err := range verify([]*types.Header{first, second, third}) {
		if !errors.Is(err, consensus.ErrUnknownAncestor) {
			t.Fatalf("header %d with an unknown parent: %v", i, err)
		}
	}
}

// A difficulty of any size is added into every total after it, so a response of small headers
// behind one huge difficulty must not cost the node a copy of it for each. The sums stop at
// that header, which its worker reports, as a header response gets no sanity check before.
func TestVerifyHeadersBoundsOversizedDifficulty(t *testing.T) {
	f := newMinerSigFixture(t)
	const batch, oversized = 64, 5

	huge := new(big.Int).Lsh(common.Big1, 1<<21) // A quarter of a megabyte
	headers := make([]*types.Header, batch)
	for i, parent := 0, f.parent; i < batch; i++ {
		headers[i] = f.child(parent)
		if i == oversized {
			headers[i].Difficulty = huge
		}
		parent = headers[i]
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	tds := f.engine.batchTDs(f.chain, headers)
	runtime.ReadMemStats(&after)
	if spent := after.TotalAlloc - before.TotalAlloc; spent > 1<<20 {
		t.Errorf("summing the batch allocated %d bytes, want less than a megabyte", spent)
	}
	for i, td := range tds {
		if (td != nil) != (i < oversized) {
			t.Errorf("header %d has a total difficulty: %v, want %v", i, td != nil, i < oversized)
		}
	}
	_, results := f.engine.VerifyHeaders(f.chain, headers, make([]bool, batch))
	for i := range headers {
		err := <-results
		if i == oversized && (err == nil || !strings.Contains(err.Error(), "too large block difficulty")) {
			t.Errorf("the oversized header: got %v, want its difficulty refused", err)
		}
	}
}

func (f *minerSigFixture) assemble(t *testing.T, header *types.Header) (*types.Block, error) {
	t.Helper()
	statedb, err := state.New(types.EmptyRootHash, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	if err != nil {
		t.Fatal(err)
	}
	return f.engine.FinalizeAndAssemble(f.chain, header, statedb, nil, nil, nil, nil)
}

func TestFinalizeAndAssembleSigns(t *testing.T) {
	f := newMinerSigFixture(t)
	f.engine.AuthorizeMiner(f.key)
	if signer, ok := f.engine.MinerSigner(); !ok || signer != crypto.PubkeyToAddress(f.key.PublicKey) {
		t.Fatalf("MinerSigner: %x %v", signer, ok)
	}
	// The worker shares this slice between all its blocks. Spare capacity is the
	// dangerous case: an append in place would write into it.
	shared := make([]byte, 6, 200)
	copy(shared, "vanity")
	header := f.child(f.parent)
	header.Extra = shared

	block, err := f.assemble(t, header)
	if err != nil {
		t.Fatal(err)
	}
	if have, want := len(block.Extra()), 6+crypto.SignatureLength; have != want {
		t.Fatalf("extra-data length %d, want %d", have, want)
	}
	if string(block.Extra()[:6]) != "vanity" {
		t.Fatalf("vanity lost: %x", block.Extra())
	}
	if err := f.engine.VerifyHeader(f.chain, block.Header(), false); err != nil {
		t.Fatalf("assembled block fails verification: %v", err)
	}
	if len(header.Extra) != 6 {
		t.Fatalf("caller's header was changed: %x", header.Extra)
	}
	if !bytes.Equal(shared[:cap(shared)], append([]byte("vanity"), make([]byte, cap(shared)-6)...)) {
		t.Fatal("signature written into the shared extra-data slice")
	}
	// Assembling the same content again gives the same seal hash, which is what the
	// worker's duplicate task check compares.
	again, err := f.assemble(t, header)
	if err != nil {
		t.Fatal(err)
	}
	if f.engine.SealHash(again.Header()) != f.engine.SealHash(block.Header()) {
		t.Fatal("signing the same content changed the seal hash")
	}
}

func TestFinalizeAndAssembleRefuses(t *testing.T) {
	stranger, _ := crypto.GenerateKey()
	for _, tt := range []struct {
		name   string
		key    *ecdsa.PrivateKey
		mutate func(*types.Header)
		reason string
	}{
		{"no key", nil, nil, "no signer key"},
		{"another key", stranger, nil, "signer key is"},
		{"crossing with an unlisted coinbase", minerSigKey, func(h *types.Header) { h.Coinbase = common.Address{0xff} }, "cannot cross the threshold"},
		{"vanity too long", minerSigKey, func(h *types.Header) { h.Extra = make([]byte, params.MaximumExtraDataSize+1) }, "extra-data too long"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newMinerSigFixture(t)
			f.engine.AuthorizeMiner(tt.key)
			header := f.child(f.parent)
			if tt.mutate != nil {
				tt.mutate(header)
			}
			if _, err := f.assemble(t, header); err == nil || !strings.Contains(err.Error(), tt.reason) {
				t.Fatalf("got %v, want an error with %q", err, tt.reason)
			}
		})
	}
}

// No signature where the rule does not apply, whether or not a key is loaded.
func TestFinalizeAndAssembleUnsigned(t *testing.T) {
	f := newMinerSigFixture(t)
	f.engine.AuthorizeMiner(f.key)
	f.chain.config.MinerSignerEpochs[0].TD = new(big.Int).Mul(params.MinimumDifficulty, big.NewInt(100))
	header := f.child(f.parent)
	header.Extra = []byte("vanity")
	block, err := f.assemble(t, header)
	if err != nil {
		t.Fatal(err)
	}
	if string(block.Extra()) != "vanity" {
		t.Fatalf("below the threshold: %x", block.Extra())
	}
	f.chain.config.MinerSignerEpochs = nil
	if block, err = f.assemble(t, header); err != nil || string(block.Extra()) != "vanity" {
		t.Fatalf("no epochs: %x %v", block.Extra(), err)
	}
}

// With real PoW: what the worker produces seals and verifies, and the seal commits to the
// signature, so nobody can derive a second block from a sealed one for free.
func TestMinerSignatureSealed(t *testing.T) {
	f := newMinerSigFixture(t)
	f.engine = NewTester(nil, false)
	defer f.engine.Close()
	f.engine.AuthorizeMiner(f.key)

	seal := func(block *types.Block) *types.Block {
		results := make(chan *types.Block, 1)
		if err := f.engine.Seal(nil, block, results, nil); err != nil {
			t.Fatal(err)
		}
		select {
		case sealed := <-results:
			return sealed
		case <-time.After(2 * time.Minute):
			t.Fatal("sealing timed out")
			return nil
		}
	}
	assembled, err := f.assemble(t, f.child(f.parent))
	if err != nil {
		t.Fatal(err)
	}
	sealed := seal(assembled)
	if err := f.engine.VerifyHeader(f.chain, sealed.Header(), true); err != nil {
		t.Fatalf("sealed block: %v", err)
	}
	// Flip S and V: the same signer recovers from it, so only the seal stands in the way
	// once the low-S check is out of the picture. Hence verifySeal directly.
	flipped := sealed.Header()
	signature := flipped.Extra[len(flipped.Extra)-crypto.SignatureLength:]
	s := new(big.Int).SetBytes(signature[32:64])
	new(big.Int).Sub(crypto.S256().Params().N, s).FillBytes(signature[32:64])
	signature[64] ^= 1
	if err := f.engine.verifySeal(nil, flipped, false); !errors.Is(err, errInvalidMixDigest) {
		t.Fatalf("seal over a flipped signature: %v", err)
	}
	// Dropping the signature for the unsigned rule fails on the rule first.
	stripped := sealed.Header()
	stripped.Extra = nil
	if err := f.engine.VerifyHeader(f.chain, stripped, true); !errors.Is(err, consensus.ErrInvalidMinerSignature) {
		t.Fatalf("signature removed: %v", err)
	}
	// Someone without the key can only re-mine the same content, which is a sibling
	// with the same state and no use for building on: its children still need a signature.
	twin := seal(assembled)
	if err := f.engine.VerifyHeader(f.chain, twin.Header(), true); err != nil {
		t.Fatalf("re-mined block: %v", err)
	}
	if twin.Root() != sealed.Root() || twin.TxHash() != sealed.TxHash() {
		t.Fatal("re-mined block changed the content")
	}
}

// A miner behind eth_getWork never sees the key. It solves the seal hash the node hands out,
// which covers the signature, and what it submits is the signed block.
func TestMinerSignatureRemoteSealer(t *testing.T) {
	f := newMinerSigFixture(t)
	f.engine = NewTester(nil, false)
	defer f.engine.Close()
	f.engine.AuthorizeMiner(f.key)
	f.engine.SetThreads(-1)

	assembled, err := f.assemble(t, f.child(f.parent))
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan *types.Block, 1)
	if err := f.engine.Seal(nil, assembled, results, nil); err != nil {
		t.Fatal(err)
	}
	api := &API{f.engine}
	var work [4]string
	for i := 0; ; i++ {
		if work, err = api.GetWork(); err == nil {
			break
		}
		if i == 100 {
			t.Fatalf("no work handed out: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if want := f.engine.SealHash(assembled.Header()).Hex(); work[0] != want {
		t.Fatalf("work for %s, want %s", work[0], want)
	}
	// Stands in for the external miner.
	external := NewTester(nil, false)
	defer external.Close()
	solved := make(chan *types.Block, 1)
	if err := external.Seal(nil, assembled, solved, nil); err != nil {
		t.Fatal(err)
	}
	solution := <-solved
	if !api.SubmitWork(solution.Header().Nonce, f.engine.SealHash(assembled.Header()), solution.MixDigest()) {
		t.Fatal("the solution was refused")
	}
	select {
	case block := <-results:
		if err := f.engine.VerifyHeader(f.chain, block.Header(), true); err != nil {
			t.Fatalf("submitted block: %v", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("no block came back")
	}
}
