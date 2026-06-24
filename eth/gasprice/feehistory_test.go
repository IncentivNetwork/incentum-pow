// Copyright 2021 The go-ethereum Authors
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

package gasprice

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
)

// stubFeeHistoryBackend implements the minimal OracleBackend subset that the
// processBlock unit tests below exercise. processBlock only reads
// ChainConfig(); the remaining methods are unused stubs so the stub stays
// small and obviously side-effect-free.
type stubFeeHistoryBackend struct {
	config *params.ChainConfig
}

func (b *stubFeeHistoryBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	return nil, nil
}
func (b *stubFeeHistoryBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	return nil, nil
}
func (b *stubFeeHistoryBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	return nil, nil
}
func (b *stubFeeHistoryBackend) PendingBlockAndReceipts() (*types.Block, types.Receipts) {
	return nil, nil
}
func (b *stubFeeHistoryBackend) ChainConfig() *params.ChainConfig { return b.config }
func (b *stubFeeHistoryBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return nil
}

// TestProcessBlockDynamicMinBaseFeeFallback covers the regression that broke
// the ERC-4337 bundler stack after DynamicMinBaseFeeTime activation: with no
// stateDB available, misc.CalcBaseFee returns ErrDynamicMinBaseFeeNilStateDB,
// and processBlock must degrade to a parent.BaseFee approximation rather than
// surfacing a -32000 RPC error. It also asserts the defensive-copy property
// so a future mutation of bf.results.nextBaseFee cannot reach the underlying
// header.BaseFee, and re-asserts that misc.CalcBaseFee itself still returns
// the sentinel (so the two sides cannot silently drift apart).
func TestProcessBlockDynamicMinBaseFeeFallback(t *testing.T) {
	dmbfActivation := uint64(1000)
	contractAddr := common.HexToAddress("0x2Ca84D9e3CCC362FfFE5B669174dC86b98F362AF")
	config := &params.ChainConfig{
		ChainID:                big.NewInt(1337),
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  &dmbfActivation,
		MinBaseFeeContractAddr: &contractAddr,
	}

	parentBaseFee := big.NewInt(12_600_000_000_000)
	header := &types.Header{
		Number:   big.NewInt(100),
		Time:     dmbfActivation + 100, // post-activation: contract floor needs state
		GasLimit: 30_000_000,
		GasUsed:  15_000_000,
		BaseFee:  parentBaseFee,
	}
	bf := &blockFees{
		blockNumber: header.Number.Uint64(),
		header:      header,
	}
	oracle := &Oracle{backend: &stubFeeHistoryBackend{config: config}}

	oracle.processBlock(bf, nil)

	if bf.err != nil {
		t.Fatalf("processBlock surfaced an error instead of falling back: %v", bf.err)
	}
	if bf.results.nextBaseFee == nil {
		t.Fatal("expected non-nil nextBaseFee after the DMBF nil-stateDB fallback")
	}
	if bf.results.nextBaseFee.Cmp(parentBaseFee) != 0 {
		t.Fatalf("expected nextBaseFee == parent.BaseFee (%s), got %s", parentBaseFee, bf.results.nextBaseFee)
	}
	if bf.results.nextBaseFee == parentBaseFee {
		t.Fatal("fallback aliased parent.BaseFee directly; expected a defensive copy")
	}

	// Independent sanity: the sentinel returned by misc.CalcBaseFee must
	// stay matchable with errors.Is. If a future refactor stops wrapping
	// ErrDynamicMinBaseFeeNilStateDB the fallback above silently turns into
	// the old error-propagation behaviour, so guard the contract here.
	if _, err := misc.CalcBaseFee(config, header, nil); !errors.Is(err, misc.ErrDynamicMinBaseFeeNilStateDB) {
		t.Fatalf("misc.CalcBaseFee(nil stateDB) under active DMBF must return ErrDynamicMinBaseFeeNilStateDB, got %v", err)
	}
}

func TestFeeHistory(t *testing.T) {
	var cases = []struct {
		pending             bool
		maxHeader, maxBlock uint64
		count               uint64
		last                rpc.BlockNumber
		percent             []float64
		expFirst            uint64
		expCount            int
		expErr              error
	}{
		{false, 1000, 1000, 10, 30, nil, 21, 10, nil},
		{false, 1000, 1000, 10, 30, []float64{0, 10}, 21, 10, nil},
		{false, 1000, 1000, 10, 30, []float64{20, 10}, 0, 0, errInvalidPercentile},
		{false, 1000, 1000, 1000000000, 30, nil, 0, 31, nil},
		{false, 1000, 1000, 1000000000, rpc.LatestBlockNumber, nil, 0, 33, nil},
		{false, 1000, 1000, 10, 40, nil, 0, 0, errRequestBeyondHead},
		{true, 1000, 1000, 10, 40, nil, 0, 0, errRequestBeyondHead},
		{false, 20, 2, 100, rpc.LatestBlockNumber, nil, 13, 20, nil},
		{false, 20, 2, 100, rpc.LatestBlockNumber, []float64{0, 10}, 31, 2, nil},
		{false, 20, 2, 100, 32, []float64{0, 10}, 31, 2, nil},
		{false, 1000, 1000, 1, rpc.PendingBlockNumber, nil, 0, 0, nil},
		{false, 1000, 1000, 2, rpc.PendingBlockNumber, nil, 32, 1, nil},
		{true, 1000, 1000, 2, rpc.PendingBlockNumber, nil, 32, 2, nil},
		{true, 1000, 1000, 2, rpc.PendingBlockNumber, []float64{0, 10}, 32, 2, nil},
		{false, 1000, 1000, 2, rpc.FinalizedBlockNumber, []float64{0, 10}, 24, 2, nil},
		{false, 1000, 1000, 2, rpc.SafeBlockNumber, []float64{0, 10}, 24, 2, nil},
	}
	for i, c := range cases {
		config := Config{
			MaxHeaderHistory: c.maxHeader,
			MaxBlockHistory:  c.maxBlock,
		}
		backend := newTestBackend(t, big.NewInt(16), c.pending)
		oracle := NewOracle(backend, config)

		first, reward, baseFee, ratio, err := oracle.FeeHistory(context.Background(), c.count, c.last, c.percent)
		backend.teardown()
		expReward := c.expCount
		if len(c.percent) == 0 {
			expReward = 0
		}
		expBaseFee := c.expCount
		if expBaseFee != 0 {
			expBaseFee++
		}

		if first.Uint64() != c.expFirst {
			t.Fatalf("Test case %d: first block mismatch, want %d, got %d", i, c.expFirst, first)
		}
		if len(reward) != expReward {
			t.Fatalf("Test case %d: reward array length mismatch, want %d, got %d", i, expReward, len(reward))
		}
		if len(baseFee) != expBaseFee {
			t.Fatalf("Test case %d: baseFee array length mismatch, want %d, got %d", i, expBaseFee, len(baseFee))
		}
		if len(ratio) != c.expCount {
			t.Fatalf("Test case %d: gasUsedRatio array length mismatch, want %d, got %d", i, c.expCount, len(ratio))
		}
		if err != c.expErr && !errors.Is(err, c.expErr) {
			t.Fatalf("Test case %d: error mismatch, want %v, got %v", i, c.expErr, err)
		}
	}
}
