// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package miner

import (
	"errors"
	"math/big"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
)

// minerSigConfig lists testBankAddress with the given signer from td on.
func minerSigConfig(td *big.Int, signer common.Address) *params.ChainConfig {
	config := *params.AllEthashProtocolChanges
	config.MinerSignerEpochs = []params.MinerSignerEpoch{{
		TD:      td,
		Signers: map[common.Address]common.Address{testBankAddress: signer},
	}}
	return &config
}

// The worker assembles, seals and publishes across the threshold with real PoW, and another
// chain accepts every block. It looks tasks up by seal hash on the way back, so a signature
// added at the wrong moment would lose the block here.
func TestWorkerMinesAcrossSignerThreshold(t *testing.T) {
	key, _ := crypto.GenerateKey()
	vanity := "incentiv"
	threshold := new(big.Int).Mul(params.GenesisDifficulty, big.NewInt(3))
	config := minerSigConfig(threshold, crypto.PubkeyToAddress(key.PublicKey))

	engine := ethash.NewTester(nil, false)
	defer engine.Close()
	engine.AuthorizeMiner(key)

	w, b := newTestWorker(t, config, engine, rawdb.NewMemoryDatabase(), 0)
	defer w.close()
	w.setExtra([]byte(vanity))

	verifying := ethash.NewTester(nil, false)
	defer verifying.Close()
	chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), nil, b.genesis, nil, verifying, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Stop()

	sub := w.mux.Subscribe(core.NewMinedBlockEvent{})
	defer sub.Unsubscribe()
	w.start()

	var below, above, txs int
	deadline := time.After(2 * time.Minute)
	for above < 3 {
		b.txPool.AddLocal(b.newRandomTx(false))
		select {
		case ev := <-sub.Chan():
			block := ev.Data.(core.NewMinedBlockEvent).Block
			if _, err := chain.InsertChain(types.Blocks{block}); err != nil {
				t.Fatalf("block %d: %v", block.NumberU64(), err)
			}
			want := len(vanity)
			if chain.GetTd(block.Hash(), block.NumberU64()).Cmp(threshold) >= 0 {
				want += crypto.SignatureLength
				above++
			} else {
				below++
			}
			if have := len(block.Extra()); have != want {
				t.Fatalf("block %d: extra-data length %d, want %d", block.NumberU64(), have, want)
			}
			if !strings.HasPrefix(string(block.Extra()), vanity) {
				t.Fatalf("block %d: vanity lost: %x", block.NumberU64(), block.Extra())
			}
			if have := len(b.chain.GetReceiptsByHash(block.Hash())); have != block.Transactions().Len() {
				t.Fatalf("block %d: %d receipts for %d transactions", block.NumberU64(), have, block.Transactions().Len())
			}
			txs += block.Transactions().Len()
		case <-deadline:
			t.Fatalf("timed out with %d blocks below and %d above the threshold", below, above)
		}
	}
	if below == 0 {
		t.Fatal("no block below the threshold, the test did not cross it")
	}
	if txs == 0 {
		t.Fatal("no block carried a transaction, the receipt count proved nothing")
	}
}

// A miner without the key produces nothing past the threshold, and says so in the log: the
// callers of commit ignore its error, and the miner goes on reporting itself as running.
func TestWorkerReportsMissingSigner(t *testing.T) {
	var reported atomic.Bool
	previous := log.Root().GetHandler()
	log.Root().SetHandler(log.FuncHandler(func(r *log.Record) error {
		if strings.Contains(r.Msg, "Failed to assemble sealing block") {
			reported.Store(true)
		}
		return nil
	}))
	defer log.Root().SetHandler(previous)

	config := minerSigConfig(new(big.Int).Set(params.GenesisDifficulty), common.Address{0x99})
	engine := ethash.NewTester(nil, false)
	defer engine.Close()
	// The backend also builds a spare block, for which it needs a fake engine. Only the
	// worker's engine matters here.
	backend := newTestWorkerBackend(t, config, ethash.NewFaker(), rawdb.NewMemoryDatabase(), 0)
	defer backend.chain.Stop()

	w := newWorker(testConfig, config, engine, backend, new(event.TypeMux), nil, false)
	defer w.close()
	w.setEtherbase(testBankAddress)

	sub := w.mux.Subscribe(core.NewMinedBlockEvent{})
	defer sub.Unsubscribe()
	w.start()

	deadline := time.After(30 * time.Second)
	for !reported.Load() {
		select {
		case ev := <-sub.Chan():
			t.Fatalf("mined block %d without a signer", ev.Data.(core.NewMinedBlockEvent).Block.NumberU64())
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatal("the assembly failure was never logged")
		}
	}
	if !w.isRunning() {
		t.Fatal("the worker stopped")
	}
}

// A miner that cannot assemble still follows the chain, and whoever asks it for the pending block
// or state, such as a wallet after a nonce, gets them on the current head.
func TestWorkerKeepsPendingWhenAssemblyFails(t *testing.T) {
	config := minerSigConfig(new(big.Int).Set(params.GenesisDifficulty), common.Address{0x99})
	engine := ethash.NewTester(nil, false)
	defer engine.Close()
	// As in TestWorkerReportsMissingSigner, only the worker's engine holds to the rule.
	backend := newTestWorkerBackend(t, config, ethash.NewFaker(), rawdb.NewMemoryDatabase(), 0)
	defer backend.chain.Stop()

	w := newWorker(testConfig, config, engine, backend, new(event.TypeMux), nil, false)
	defer w.close()
	w.setEtherbase(testBankAddress)
	w.start()

	blocks, _ := core.GenerateChain(config, backend.chain.Genesis(), backend.chain.Engine(), backend.db, 1, nil)
	if _, err := backend.chain.InsertChain(blocks); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(30 * time.Second)
	for {
		if pending := w.pendingBlock(); pending != nil && pending.ParentHash() == blocks[0].Hash() {
			if pending.Coinbase() != testBankAddress {
				t.Fatalf("pending block of coinbase %s", pending.Coinbase())
			}
			return
		}
		select {
		case <-time.After(50 * time.Millisecond):
		case <-deadline:
			t.Fatalf("no pending block on the head at #%d", blocks[0].NumberU64())
		}
	}
}

// A miner can stay unable to assemble for a day, as while its stake matures, and every round
// fails twice. It says so once in a while, and at once when the reason changes.
func TestWorkerLimitsAssemblyFailureLog(t *testing.T) {
	var logged atomic.Int32
	previous := log.Root().GetHandler()
	log.Root().SetHandler(log.FuncHandler(func(r *log.Record) error {
		if r.Lvl == log.LvlError && strings.Contains(r.Msg, "Failed to assemble sealing block") {
			logged.Add(1)
		}
		return nil
	}))
	defer log.Root().SetHandler(previous)

	want := func(what string, count int32) {
		t.Helper()
		if have := logged.Load(); have != count {
			t.Fatalf("%s: %d lines logged, want %d", what, have, count)
		}
	}
	var (
		w        = new(worker)
		header   = &types.Header{Number: common.Big1, Coinbase: common.Address{0x47}}
		maturing = errors.New("miner not mature")
		noKey    = errors.New("no signer key")
	)
	w.reportAssemblyFailure(header, maturing)
	want("first failure", 1)

	w.reportAssemblyFailure(header, maturing)
	w.reportAssemblyFailure(&types.Header{Number: common.Big2, Coinbase: common.Address{0x47}}, maturing)
	want("the same failure again", 1)

	w.reportAssemblyFailure(header, noKey)
	want("another failure", 2)

	w.assemblyErrLogged = time.Now().Add(-assemblyErrLogInterval)
	w.reportAssemblyFailure(header, noKey)
	want("the same failure after the interval", 3)

	// A block with no coinbase was prepared just before the worker started.
	w.reportAssemblyFailure(&types.Header{Number: common.Big1}, maturing)
	want("a block with no coinbase", 3)
	w.reportAssemblyFailure(header, noKey)
	want("a block with no coinbase leaves the last failure alone", 3)

	// A block assembled in between ends the failure, so one that comes back is a new one. A
	// block with no coinbase says nothing about that either.
	w.reportAssemblySuccess(&types.Header{Number: common.Big1})
	w.reportAssemblyFailure(header, noKey)
	want("a success of a block with no coinbase", 3)
	w.reportAssemblySuccess(header)
	w.reportAssemblyFailure(header, noKey)
	want("the same failure after a block was assembled", 4)
	w.reportAssemblyFailure(header, noKey)
	want("and again", 4)
}
