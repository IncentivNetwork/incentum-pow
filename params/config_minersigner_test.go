// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package params

import (
	"encoding/json"
	"math/big"
	"reflect"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestMinerSignerEpochAt(t *testing.T) {
	first := MinerSignerEpoch{TD: big.NewInt(100), Signers: map[common.Address]common.Address{{1}: {2}}}
	second := MinerSignerEpoch{TD: big.NewInt(200), Signers: map[common.Address]common.Address{{1}: {3}}}
	config := &ChainConfig{MinerSignerEpochs: []MinerSignerEpoch{first, second}}

	for _, tt := range []struct {
		td   int64
		want *big.Int
	}{
		{0, nil},
		{99, nil},
		{100, first.TD},
		{199, first.TD},
		{200, second.TD},
		{1000, second.TD},
	} {
		got := config.MinerSignerEpochAt(big.NewInt(tt.td))
		if tt.want == nil {
			if got != nil {
				t.Errorf("td %d: got epoch at %v, want none", tt.td, got.TD)
			}
			continue
		}
		if got == nil || got.TD.Cmp(tt.want) != 0 {
			t.Errorf("td %d: got %+v, want the epoch at %v", tt.td, got, tt.want)
		}
	}
	if got := new(ChainConfig).MinerSignerEpochAt(big.NewInt(1)); got != nil {
		t.Errorf("no epochs configured: got %+v", got)
	}
	// Each epoch replaces the list: an entry an epoch leaves out is gone from there on.
	dropped := &ChainConfig{MinerSignerEpochs: []MinerSignerEpoch{
		{TD: big.NewInt(100), Signers: map[common.Address]common.Address{{1}: {2}, {7}: {8}}},
		{TD: big.NewInt(200), Signers: map[common.Address]common.Address{{1}: {2}}},
	}}
	if _, listed := dropped.MinerSignerEpochAt(big.NewInt(200)).Signers[common.Address{7}]; listed {
		t.Error("an entry carried over into an epoch that leaves it out")
	}
}

func TestCheckMinerSignerConfig(t *testing.T) {
	signers := map[common.Address]common.Address{{1}: {2}}
	valid := func() *ChainConfig {
		return &ChainConfig{
			ChainID: big.NewInt(1),
			Ethash:  new(EthashConfig),
			MinerSignerEpochs: []MinerSignerEpoch{
				{TD: big.NewInt(100), Signers: signers},
				{TD: big.NewInt(200), Signers: signers},
			},
		}
	}
	if err := valid().CheckMinerSignerConfig(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if err := new(ChainConfig).CheckMinerSignerConfig(); err != nil {
		t.Fatalf("config without epochs rejected: %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*ChainConfig)
		want   string
	}{
		{"not ethash", func(c *ChainConfig) { c.Ethash = nil }, "does not run ethash"},
		{"clique as well", func(c *ChainConfig) { c.Clique = &CliqueConfig{Period: 15, Epoch: 30000} }, "also sets clique"},
		{"clique only", func(c *ChainConfig) { c.Ethash, c.Clique = nil, &CliqueConfig{Period: 15, Epoch: 30000} }, "does not run ethash"},
		{"no chain id", func(c *ChainConfig) { c.ChainID = nil }, "chainId is missing"},
		{"negative chain id", func(c *ChainConfig) { c.ChainID = big.NewInt(-1) }, "not positive"},
		{"zero chain id", func(c *ChainConfig) { c.ChainID = new(big.Int) }, "not positive"},
		{"nil td", func(c *ChainConfig) { c.MinerSignerEpochs[0].TD = nil }, "no positive td"},
		{"zero td", func(c *ChainConfig) { c.MinerSignerEpochs[0].TD = new(big.Int) }, "no positive td"},
		{"same td", func(c *ChainConfig) { c.MinerSignerEpochs[1].TD = big.NewInt(100) }, "does not follow"},
		{"descending td", func(c *ChainConfig) { c.MinerSignerEpochs[1].TD = big.NewInt(50) }, "does not follow"},
		{"nil signers", func(c *ChainConfig) { c.MinerSignerEpochs[0].Signers = nil }, "lists no signers"},
		{"zero coinbase", func(c *ChainConfig) {
			c.MinerSignerEpochs[1].Signers = map[common.Address]common.Address{{}: {2}}
		}, "zero address"},
		{"zero signer", func(c *ChainConfig) {
			c.MinerSignerEpochs[1].Signers = map[common.Address]common.Address{{1}: {}}
		}, "zero address"},
	} {
		config := valid()
		tt.mutate(config)
		err := config.CheckMinerSignerConfig()
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: got %v, want an error containing %q", tt.name, err, tt.want)
		}
	}
}

// The stored chain config is JSON, so the epochs have to survive it unchanged.
func TestMinerSignerEpochsJSON(t *testing.T) {
	td, _ := new(big.Int).SetString("2453189176302816000000", 10)
	config := &ChainConfig{MinerSignerEpochs: []MinerSignerEpoch{
		{TD: td, Signers: map[common.Address]common.Address{{1}: {2}, {3}: {4}}},
	}}
	enc, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var dec ChainConfig
	if err := json.Unmarshal(enc, &dec); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.MinerSignerEpochs, dec.MinerSignerEpochs) {
		t.Fatalf("round trip changed the epochs: %s", enc)
	}
	if enc, _ := json.Marshal(new(ChainConfig)); strings.Contains(string(enc), "minerSignerEpochs") {
		t.Fatalf("an empty config writes the field: %s", enc)
	}
}

// Devnet and mainnet have one epoch each. Testnet gets its own in a release of its own, which
// updates this test. Once crossed, an epoch is history that a node syncing from scratch
// verifies, so its threshold and signers are pinned exactly.
func TestBundledMinerSignerEpochs(t *testing.T) {
	if n := len(IncentivTestnetChainConfig.MinerSignerEpochs); n != 0 {
		t.Errorf("testnet has %d miner signer epochs, want none", n)
	}
	mainnet := IncentivMainnetChainConfig
	if err := mainnet.CheckMinerSignerConfig(); err != nil {
		t.Fatalf("mainnet: %v", err)
	}
	if len(mainnet.MinerSignerEpochs) != 1 {
		t.Fatalf("mainnet has %d miner signer epochs, want one", len(mainnet.MinerSignerEpochs))
	}
	if td := mainnet.MinerSignerEpochs[0].TD; td.Cmp(big.NewInt(2_455_690_300_000_000)) != 0 {
		t.Fatalf("mainnet epoch at %v, want 2455690300000000", td)
	}
	producers := []common.Address{
		common.HexToAddress("0x47D994f172e3b124546804B2761Fb9deBad7169D"),
		common.HexToAddress("0xCe3Cc056a1DfF0C9322C4B223e59E963AE18E65B"),
		common.HexToAddress("0xB000b08374508dEB5FbA1824F337a97197fefb13"),
		common.HexToAddress("0x9237f2e1E98C3ED9d5fd956BC111b96C3B38dA92"),
	}
	signers := mainnet.MinerSignerEpochs[0].Signers
	if len(signers) != len(producers) {
		t.Fatalf("mainnet epoch lists %d signers, want %d", len(signers), len(producers))
	}
	for _, producer := range producers {
		if signers[producer] != producer {
			t.Errorf("mainnet epoch lists %s for %s, want itself", signers[producer], producer)
		}
	}
	devnet := IncentivDevnetChainConfig
	if err := devnet.CheckMinerSignerConfig(); err != nil {
		t.Fatalf("devnet: %v", err)
	}
	miner := common.HexToAddress("0x270875c5cb93F56011365Ba765D8d0397F0baD8C")
	if len(devnet.MinerSignerEpochs) != 1 || devnet.MinerSignerEpochs[0].Signers[miner] != miner {
		t.Fatalf("devnet epochs %+v, want one that lists %s for itself", devnet.MinerSignerEpochs, miner)
	}
	epoch := devnet.MinerSignerEpochs[0]
	if epoch.TD.Cmp(big.NewInt(8_847_620_000_000)) != 0 || len(epoch.Signers) != 1 {
		t.Fatalf("devnet epoch at %v with %d signers, want 8847620000000 with one", epoch.TD, len(epoch.Signers))
	}
}
