// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package ethash

import (
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// fieldsEntries are the engine entry points that reach verifyHeader.
var fieldsEntries = []string{"VerifyHeader", "VerifyHeaders", "VerifyUncles"}

// fieldsChain is a minimal consensus.ChainReader over a handful of headers.
type fieldsChain struct {
	config  *params.ChainConfig
	headers map[common.Hash]*types.Header
	head    *types.Header
}

func (c *fieldsChain) add(header *types.Header) {
	c.headers[header.Hash()] = header
	if c.head == nil || header.Number.Cmp(c.head.Number) > 0 {
		c.head = header
	}
}

func (c *fieldsChain) Config() *params.ChainConfig  { return c.config }
func (c *fieldsChain) CurrentHeader() *types.Header { return c.head }

func (c *fieldsChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	if header := c.headers[hash]; header != nil && header.Number.Uint64() == number {
		return header
	}
	return nil
}

func (c *fieldsChain) GetHeaderByHash(hash common.Hash) *types.Header { return c.headers[hash] }

func (c *fieldsChain) GetHeaderByNumber(number uint64) *types.Header {
	for _, header := range c.headers {
		if header.Number.Uint64() == number {
			return header
		}
	}
	return nil
}

func (c *fieldsChain) GetTd(common.Hash, uint64) *big.Int { return nil }

func (c *fieldsChain) GetBlock(common.Hash, uint64) *types.Block { return nil }

// child derives a header on top of parent the way an honest producer would, so
// that every check before the seal passes. The seal itself is left unsolved.
func (c *fieldsChain) child(t *testing.T, engine *Ethash, parent *types.Header, delta uint64) *types.Header {
	t.Helper()

	ts := parent.Time + delta
	baseFee, err := misc.CalcBaseFee(c.config, parent, nil)
	if err != nil {
		t.Fatalf("failed to calculate the base fee: %v", err)
	}
	return &types.Header{
		ParentHash: parent.Hash(),
		UncleHash:  types.EmptyUncleHash,
		Number:     new(big.Int).Add(parent.Number, common.Big1),
		GasLimit:   parent.GasLimit,
		Time:       ts,
		Difficulty: engine.CalcDifficulty(c, ts, parent),
		BaseFee:    baseFee,
	}
}

// newFieldsChain returns a chain holding only a genesis header, under a config
// that has Shanghai active from the start.
func newFieldsChain() (*fieldsChain, *types.Header) {
	genesis := &types.Header{
		Number:     big.NewInt(0),
		Time:       1,
		Difficulty: new(big.Int).Set(params.MinimumDifficulty),
		GasLimit:   params.GenesisGasLimit,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		UncleHash:  types.EmptyUncleHash,
	}
	chain := &fieldsChain{config: params.TestChainConfig, headers: make(map[common.Hash]*types.Header)}
	chain.add(genesis)
	return chain, genesis
}

// fieldsFixture is an ethash test engine over a chain genesis <- a <- b.
// Candidate headers are built on b and uncles on a.
type fieldsFixture struct {
	engine *Ethash
	chain  *fieldsChain
	a, b   *types.Header
}

func newFieldsFixture(t *testing.T) *fieldsFixture {
	t.Helper()

	engine := NewTester(nil, false)
	t.Cleanup(func() { engine.Close() })

	chain, genesis := newFieldsChain()
	a := chain.child(t, engine, genesis, 5)
	chain.add(a)
	b := chain.child(t, engine, a, 5)
	chain.add(b)
	return &fieldsFixture{engine: engine, chain: chain, a: a, b: b}
}

// verifyAll applies mutate to a candidate header and to an uncle, and runs them
// through every entry point of the engine that reaches verifyHeader. It returns
// the error each entry point reported, keyed by fieldsEntries. VerifyUncles
// always checks the seal of an uncle, so it only takes part when seal is set.
func (f *fieldsFixture) verifyAll(t *testing.T, mutate func(*types.Header), seal bool) map[string]error {
	t.Helper()

	errs := make(map[string]error)

	header := f.chain.child(t, f.engine, f.b, 5)
	mutate(header)
	errs["VerifyHeader"] = f.engine.VerifyHeader(f.chain, header, seal)

	abort, results := f.engine.VerifyHeaders(f.chain, []*types.Header{header}, []bool{seal})
	defer close(abort)
	select {
	case errs["VerifyHeaders"] = <-results:
	case <-time.After(10 * time.Second):
		t.Fatal("VerifyHeaders did not report a result")
	}
	if seal {
		uncle := f.chain.child(t, f.engine, f.a, 7)
		mutate(uncle)
		block := types.NewBlockWithHeader(f.chain.child(t, f.engine, f.b, 5)).WithBody(nil, []*types.Header{uncle})
		errs["VerifyUncles"] = f.engine.VerifyUncles(f.chain, block)
	}
	return errs
}

// TestVerifyRejectsUnsupportedHeaderFields checks that a header carrying a
// field ethash blocks never have is rejected by every verification entry
// point, with the seal check requested and without it. The second run is the
// one that matters for header sync, which checks the seal of only some of the
// headers it downloads, and the rule must not depend on that choice.
func TestVerifyRejectsUnsupportedHeaderFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.Header)
		want   error
	}{
		{
			name:   "withdrawals hash",
			mutate: func(h *types.Header) { h.WithdrawalsHash = &types.EmptyWithdrawalsHash },
			want:   errWithdrawalsHash,
		},
		{
			// The default header encoder does not write this field, so a header carries it
			// alone only when built in memory. From the wire it comes behind a whole
			// withdrawals hash, which is the "both fields" case below.
			name:   "excess data gas",
			mutate: func(h *types.Header) { h.ExcessDataGas = new(big.Int) },
			want:   errExcessDataGas,
		},
		{
			name: "both fields",
			mutate: func(h *types.Header) {
				h.WithdrawalsHash = &types.EmptyWithdrawalsHash
				h.ExcessDataGas = new(big.Int)
			},
			want: errWithdrawalsHash,
		},
	}
	for _, tt := range tests {
		for _, seal := range []bool{true, false} {
			name := tt.name + ", seal check requested"
			entries := fieldsEntries
			if !seal {
				// VerifyUncles always checks the seal of an uncle.
				name, entries = tt.name+", seal check skipped", []string{"VerifyHeader", "VerifyHeaders"}
			}
			t.Run(name, func(t *testing.T) {
				f := newFieldsFixture(t)
				if !f.chain.config.IsShanghai(f.b.Time) {
					t.Fatal("the test config must have Shanghai active")
				}
				errs := f.verifyAll(t, tt.mutate, seal)
				for _, entry := range entries {
					if err, ok := errs[entry]; !ok || !errors.Is(err, tt.want) {
						t.Errorf("%s: have error %v, want %v", entry, err, tt.want)
					}
				}
			})
		}
	}
}

// TestVerifyAcceptsPlainHeader is the control for the test above. A header
// without the unsupported fields gets past the field check: with the seal check
// skipped it verifies, and with it requested it fails on the unsolved seal,
// which is the check that follows.
func TestVerifyAcceptsPlainHeader(t *testing.T) {
	f := newFieldsFixture(t)
	plain := func(*types.Header) {}

	for entry, err := range f.verifyAll(t, plain, false) {
		if err != nil {
			t.Errorf("%s without the seal check: unexpected error %v", entry, err)
		}
	}
	errs := f.verifyAll(t, plain, true)
	for _, entry := range fieldsEntries {
		if err, ok := errs[entry]; !ok || !errors.Is(err, errInvalidMixDigest) {
			t.Errorf("%s with the seal check: have error %v, want %v", entry, err, errInvalidMixDigest)
		}
	}
}

// TestVerifyKnownHeaderFields covers a header the chain already stores, which
// VerifyHeader does not verify again. The rule has no fork gate, so it holds for
// what was stored before the rule as well, and such a header must not pass as
// known. A plain stored header still does: it returns before the seal check, which
// the unsolved seal of the fixture would fail.
func TestVerifyKnownHeaderFields(t *testing.T) {
	f := newFieldsFixture(t)

	plain := f.chain.child(t, f.engine, f.b, 5)
	f.chain.add(plain)
	if err := f.engine.VerifyHeader(f.chain, plain, true); err != nil {
		t.Fatalf("plain stored header: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*types.Header)
		want   error
	}{
		{"withdrawals hash", func(h *types.Header) { h.WithdrawalsHash = &types.EmptyWithdrawalsHash }, errWithdrawalsHash},
		// The default header encoder does not write excess data gas, so a database never holds
		// such a header and this row stores the object itself, as only a memory chain can. The
		// case after the loop is the one a default build can reach.
		{"excess data gas", func(h *types.Header) { h.ExcessDataGas = new(big.Int) }, errExcessDataGas},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := f.chain.child(t, f.engine, f.b, 6+uint64(i))
			tt.mutate(header)
			f.chain.add(header)

			for _, seal := range []bool{true, false} {
				if err := f.engine.VerifyHeader(f.chain, header, seal); !errors.Is(err, tt.want) {
					t.Errorf("seal check %v: have error %v, want %v", seal, err, tt.want)
				}
			}
		})
	}
	// Excess data gas is not part of the hash the default encoder computes, so a header that
	// carries it has the hash of a plain stored one and the chain finds it known (the pin for
	// that is TestMinerSignatureExcessDataGas). The check must refuse the header it is given
	// and leave the stored one valid.
	t.Run("excess data gas on a hash stored without it", func(t *testing.T) {
		stored := f.chain.child(t, f.engine, f.b, 9)
		f.chain.add(stored)
		given := f.chain.child(t, f.engine, f.b, 9)
		given.ExcessDataGas = new(big.Int)
		if given.Hash() != stored.Hash() {
			t.Skip("excess data gas is part of the hash in this build, so the given header is not known")
		}

		for _, seal := range []bool{true, false} {
			if err := f.engine.VerifyHeader(f.chain, given, seal); !errors.Is(err, errExcessDataGas) {
				t.Errorf("given header, seal check %v: have error %v, want %v", seal, err, errExcessDataGas)
			}
		}
		if err := f.engine.VerifyHeader(f.chain, stored, true); err != nil {
			t.Errorf("stored header: have error %v, want none", err)
		}
	})
	// The fake engine keeps accepting it.
	fake := NewFaker()
	header := f.chain.child(t, fake, f.b, 8)
	header.WithdrawalsHash = &types.EmptyWithdrawalsHash
	f.chain.add(header)
	if err := fake.VerifyHeader(f.chain, header, true); err != nil {
		t.Errorf("the fake engine rejected a stored header: %v", err)
	}
}

// TestVerifyFakeEngineSkipsHeaderFieldCheck pins down that the fake engine still
// accepts such a header. It never reaches the seal hash, and
// TestStateProcessorErrors relies on building one under it.
func TestVerifyFakeEngineSkipsHeaderFieldCheck(t *testing.T) {
	engine := NewFaker()
	chain, genesis := newFieldsChain()

	header := chain.child(t, engine, genesis, 5)
	header.WithdrawalsHash = &types.EmptyWithdrawalsHash
	header.ExcessDataGas = new(big.Int)

	if err := engine.VerifyHeader(chain, header, true); err != nil {
		t.Fatalf("the fake engine rejected the header: %v", err)
	}
}
