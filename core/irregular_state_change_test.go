// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package core

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// The second correction runs from Finalize, so these tests drive it through real block
// generation and import. The unit tests in consensus/ethash call it directly and would
// stay green if Finalize stopped calling it.
const irregularTestHeight = 5

var (
	irregularAffected    = common.HexToAddress("0x232c7a84ff323a977f3Ea494A3b714C5FBAD2b5c")
	irregularEmptied     = common.HexToAddress("0xeEE207588Ff341B438046dD161376cdDD08C0e5e")
	irregularBeneficiary = common.HexToAddress("0xcc5d81498Ff476AaEB3DEf2f3576Af8a33C28eFE")

	irregularFunderKey, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313dac2d8dc1b1b1b1b1")

	irregularArchive = &CacheConfig{TrieCleanLimit: 256, TrieDirtyLimit: 256, TrieTimeLimit: 5 * time.Minute, TrieDirtyDisabled: true}
)

func irregularEther(n int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(n), big.NewInt(params.Ether))
}

// irregularGenesis returns the same genesis block with or without the correction height:
// the chain config is not part of the genesis hash, as with a node before and after the
// upgrade.
func irregularGenesis(height *big.Int) *Genesis {
	config := *params.AllEthashProtocolChanges
	config.IrregularStateChangeHeight2 = height
	return &Genesis{
		Config:   &config,
		BaseFee:  big.NewInt(params.InitialBaseFee),
		GasLimit: 8_000_000,
		Alloc: GenesisAlloc{
			irregularAffected:    {Balance: irregularEther(10), Nonce: 28},
			irregularEmptied:     {Balance: irregularEther(5)},
			irregularBeneficiary: {Balance: new(big.Int), Nonce: 1},
			crypto.PubkeyToAddress(irregularFunderKey.PublicKey): {Balance: irregularEther(1000)},
		},
	}
}

// irregularChain sends the affected address 1 ether in the activation block, which
// Finalize has to sweep, and 2 ether two blocks later, which has to stay.
func irregularChain(t *testing.T, g *Genesis) []*types.Block {
	t.Helper()
	funder := crypto.PubkeyToAddress(irregularFunderKey.PublicKey)
	_, blocks, _ := GenerateChainWithGenesis(g, ethash.NewFaker(), 8, func(i int, b *BlockGen) {
		var value *big.Int
		switch b.Number().Uint64() {
		case irregularTestHeight:
			value = irregularEther(1)
		case irregularTestHeight + 2:
			value = irregularEther(2)
		default:
			return
		}
		tx, err := types.SignTx(types.NewTransaction(b.TxNonce(funder), irregularAffected, value, params.TxGas, b.BaseFee(), nil), types.LatestSigner(g.Config), irregularFunderKey)
		if err != nil {
			t.Fatal(err)
		}
		b.AddTx(tx)
	})
	return blocks
}

func checkIrregularState(t *testing.T, bc *BlockChain, number uint64, affected, beneficiary *big.Int, emptiedExists bool) {
	t.Helper()
	statedb, err := bc.StateAt(bc.GetHeaderByNumber(number).Root)
	if err != nil {
		t.Fatalf("state at %d: %v", number, err)
	}
	if got := statedb.GetBalance(irregularAffected); got.Cmp(affected) != 0 {
		t.Errorf("block %d: affected balance %v, want %v", number, got, affected)
	}
	if got := statedb.GetNonce(irregularAffected); got != 28 {
		t.Errorf("block %d: affected nonce %d, want 28", number, got)
	}
	if got := statedb.GetBalance(irregularBeneficiary); got.Cmp(beneficiary) != 0 {
		t.Errorf("block %d: beneficiary balance %v, want %v", number, got, beneficiary)
	}
	// An account left empty with nonce 0 is removed from the state, the same on every node.
	if got := statedb.Exist(irregularEmptied); got != emptiedExists {
		t.Errorf("block %d: emptied account exists = %v, want %v", number, got, emptiedExists)
	}
}

func TestIrregularStateChange2OnImport(t *testing.T) {
	height := big.NewInt(irregularTestHeight)
	bc, err := NewBlockChain(rawdb.NewMemoryDatabase(), irregularArchive, irregularGenesis(height), nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bc.Stop()
	if n, err := bc.InsertChain(irregularChain(t, irregularGenesis(height))); err != nil {
		t.Fatalf("import failed at index %d: %v", n, err)
	}
	checkIrregularState(t, bc, irregularTestHeight-1, irregularEther(10), new(big.Int), true)
	checkIrregularState(t, bc, irregularTestHeight, new(big.Int), irregularEther(16), false)
	checkIrregularState(t, bc, irregularTestHeight+1, new(big.Int), irregularEther(16), false)
	checkIrregularState(t, bc, irregularTestHeight+2, irregularEther(2), irregularEther(16), false)
	checkIrregularState(t, bc, irregularTestHeight+3, irregularEther(2), irregularEther(16), false)
}

// The stable fork ID keeps mixed peers connected, so what separates them is the state root.
func TestIrregularStateChange2SplitsMixedNodes(t *testing.T) {
	height := big.NewInt(irregularTestHeight)
	corrected := irregularChain(t, irregularGenesis(height))
	uncorrected := irregularChain(t, irregularGenesis(nil))
	if corrected[irregularTestHeight-2].Hash() != uncorrected[irregularTestHeight-2].Hash() {
		t.Fatal("the chains differ before the height")
	}
	for _, tc := range []struct {
		name   string
		height *big.Int
		blocks []*types.Block
	}{
		{"old node, corrected chain", nil, corrected},
		{"new node, uncorrected chain", height, uncorrected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bc, err := NewBlockChain(rawdb.NewMemoryDatabase(), irregularArchive, irregularGenesis(tc.height), nil, ethash.NewFaker(), vm.Config{}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer bc.Stop()
			if _, err := bc.InsertChain(tc.blocks); err == nil || !strings.Contains(err.Error(), "invalid merkle root") {
				t.Fatalf("import returned %v, want an invalid merkle root", err)
			}
			if got := bc.CurrentBlock().Number.Uint64(); got != irregularTestHeight-1 {
				t.Errorf("head %d, want %d", got, irregularTestHeight-1)
			}
		})
	}
}

// A node that imported the height uncorrected and is restarted under the network flag has
// to rewind and then follow the corrected chain, on a pruning node as well as an archive.
func TestIrregularStateChange2LateUpgrade(t *testing.T) {
	height := big.NewInt(irregularTestHeight)
	for _, tc := range []struct {
		name  string
		cache *CacheConfig
	}{
		{"archive", irregularArchive},
		{"pruning", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			old, err := NewBlockChain(db, tc.cache, irregularGenesis(nil), nil, ethash.NewFaker(), vm.Config{}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if n, err := old.InsertChain(irregularChain(t, irregularGenesis(nil))); err != nil {
				t.Fatalf("uncorrected import failed at index %d: %v", n, err)
			}
			old.Stop()

			bc, err := NewBlockChain(db, tc.cache, irregularGenesis(height), nil, ethash.NewFaker(), vm.Config{}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer bc.Stop()
			if got := bc.CurrentBlock().Number.Uint64(); got >= irregularTestHeight {
				t.Fatalf("head %d after the restart, want a rewind below %d", got, irregularTestHeight)
			}
			if got := rawdb.ReadChainConfig(db, bc.Genesis().Hash()).IrregularStateChangeHeight2; got == nil || got.Cmp(height) != 0 {
				t.Errorf("stored height %v after the rewind, want %v", got, height)
			}
			corrected := irregularChain(t, irregularGenesis(height))
			if n, err := bc.InsertChain(corrected); err != nil {
				t.Fatalf("corrected import failed at index %d: %v", n, err)
			}
			if got, want := bc.CurrentBlock().Hash(), corrected[len(corrected)-1].Hash(); got != want {
				t.Fatalf("head %x, want the corrected tip %x", got, want)
			}
			statedb, err := bc.State()
			if err != nil {
				t.Fatal(err)
			}
			if got := statedb.GetBalance(irregularBeneficiary); got.Cmp(irregularEther(16)) != 0 {
				t.Errorf("beneficiary balance %v after the re-apply, want 16 ether", got)
			}
		})
	}
}
