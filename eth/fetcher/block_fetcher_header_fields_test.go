// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package fetcher

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/rlp"
)

// TestPropagatedBlockWithWithdrawalsHash feeds a propagated block that carries a
// withdrawals hash to a fetcher validating through a real ethash engine, the way
// the handler wires it. The block must be rejected because of the withdrawals
// hash, its sender dropped, and nothing imported.
func TestPropagatedBlockWithWithdrawalsHash(t *testing.T) {
	engine := ethash.NewTester(nil, false)
	defer engine.Close()

	chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), nil, gspec, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create the chain: %v", err)
	}
	defer chain.Stop()

	// Every field checked before the seal derives from the parent header, so the
	// block costs nothing to build.
	parent := chain.Genesis().Header()
	ts := parent.Time + 5
	baseFee, err := misc.CalcBaseFee(chain.Config(), parent, nil)
	if err != nil {
		t.Fatalf("failed to calculate the base fee: %v", err)
	}
	header := &types.Header{
		ParentHash:      parent.Hash(),
		UncleHash:       types.EmptyUncleHash,
		TxHash:          types.EmptyTxsHash,
		ReceiptHash:     types.EmptyReceiptsHash,
		Number:          big.NewInt(1),
		GasLimit:        parent.GasLimit,
		Time:            ts,
		Difficulty:      engine.CalcDifficulty(chain, ts, parent),
		BaseFee:         baseFee,
		WithdrawalsHash: &types.EmptyWithdrawalsHash,
	}
	// Pass the block through the wire encoding and the sanity check, as a peer's
	// message would.
	enc, err := rlp.EncodeToBytes(types.NewBlockWithHeader(header))
	if err != nil {
		t.Fatalf("failed to encode the block: %v", err)
	}
	block := new(types.Block)
	if err := rlp.DecodeBytes(enc, block); err != nil {
		t.Fatalf("failed to decode the block: %v", err)
	}
	if err := block.SanityCheck(); err != nil {
		t.Fatalf("block failed the sanity check: %v", err)
	}
	if block.Header().WithdrawalsHash == nil {
		t.Fatal("the withdrawals hash did not survive the round trip")
	}

	var (
		verified = make(chan error, 1)
		dropped  = make(chan string, 1)
		inserted = make(chan struct{}, 1)
	)
	fetcher := NewBlockFetcher(false, nil, chain.GetBlockByHash,
		func(header *types.Header) error {
			err := chain.Engine().VerifyHeader(chain, header, true)
			select {
			case verified <- err:
			default:
			}
			return err
		},
		func(*types.Block, bool) {},
		func() uint64 { return chain.CurrentBlock().Number.Uint64() },
		nil,
		func(types.Blocks) (int, error) { inserted <- struct{}{}; return 0, nil },
		func(peer string) { dropped <- peer },
	)
	fetcher.Start()
	defer fetcher.Stop()

	fetcher.Enqueue("attacker", block)

	select {
	case peer := <-dropped:
		if peer != "attacker" {
			t.Fatalf("dropped the wrong peer: have %q, want %q", peer, "attacker")
		}
		// Any other failure of the header drops the sender too, so name the reason.
		// The error variable is private to ethash, hence the text.
		select {
		case err := <-verified:
			if err == nil || !strings.Contains(err.Error(), "withdrawals hash") {
				t.Fatalf("the block was rejected for the wrong reason: %v", err)
			}
		default:
			t.Fatal("the validator did not report an error")
		}
	case <-inserted:
		t.Fatal("the block was imported")
	case <-time.After(10 * time.Second):
		t.Fatal("the block was neither rejected nor imported")
	}
}
