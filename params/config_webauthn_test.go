// Copyright 2026 The go-ethereum Authors
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

package params

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

// TestIsWebAuthnStrict covers the activation predicate, including the two values
// that mean something other than a moment in time.
func TestIsWebAuthnStrict(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   *uint64
		time uint64
		want bool
	}{
		{"unset never activates", nil, 1 << 40, false},
		{"zero is always active", newUint64(0), 0, true},
		{"before activation", newUint64(1000), 999, false},
		{"at activation", newUint64(1000), 1000, true},
		{"after activation", newUint64(1000), 1001, true},
	} {
		c := &ChainConfig{WebAuthnStrictTime: tc.at}
		if got := c.IsWebAuthnStrict(tc.time); got != tc.want {
			t.Errorf("%s: IsWebAuthnStrict(%d) = %v, want %v", tc.name, tc.time, got, tc.want)
		}
	}
}

// TestWebAuthnStrictRules checks that the predicate reaches the EVM, which reads
// the fork through Rules and nothing else.
func TestWebAuthnStrictRules(t *testing.T) {
	c := &ChainConfig{ChainID: big.NewInt(1), BerlinBlock: big.NewInt(0), WebAuthnStrictTime: newUint64(1000)}
	if c.Rules(big.NewInt(1), false, 999).IsWebAuthnStrict {
		t.Error("Rules reports the fork active before its timestamp")
	}
	if !c.Rules(big.NewInt(1), false, 1000).IsWebAuthnStrict {
		t.Error("Rules reports the fork inactive at its timestamp")
	}

	// The strict precompile set is the Berlin set with one entry replaced, and
	// CheckConfigForkOrder never compares block forks with timestamped ones, so a
	// config can arm this fork below berlinBlock — reachable with a custom genesis
	// or --override.webauthnstrict. Below Berlin the rule has to stay off, or those
	// blocks would be priced with the Berlin set.
	late := &ChainConfig{ChainID: big.NewInt(1), BerlinBlock: big.NewInt(100), WebAuthnStrictTime: newUint64(0)}
	if late.Rules(big.NewInt(99), false, 1000).IsWebAuthnStrict {
		t.Error("Rules armed the fork below berlinBlock; blocks there would get the Berlin precompile set")
	}
	if !late.Rules(big.NewInt(100), false, 1000).IsWebAuthnStrict {
		t.Error("Rules did not arm the fork at berlinBlock")
	}
}

// TestWebAuthnStrictForkOrder guards the ordering rule that forced this fork to be
// timestamp-based: geth requires block forks before time forks, and time forks in
// chronological order. The shipped Incentiv configs must satisfy it.
func TestWebAuthnStrictForkOrder(t *testing.T) {
	for name, c := range map[string]*ChainConfig{
		"mainnet": IncentivMainnetChainConfig,
		"testnet": IncentivTestnetChainConfig,
		"devnet":  IncentivDevnetChainConfig,
	} {
		if c.WebAuthnStrictTime == nil {
			t.Errorf("%s: WebAuthnStrictTime is unset, the fork would never activate", name)
			continue
		}
		if err := c.CheckConfigForkOrder(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if *c.WebAuthnStrictTime < *c.ShanghaiTime {
			t.Errorf("%s: WebAuthnStrictTime %d precedes ShanghaiTime %d",
				name, *c.WebAuthnStrictTime, *c.ShanghaiTime)
		}
		if c.DynamicMinBaseFeeTime != nil && *c.WebAuthnStrictTime < *c.DynamicMinBaseFeeTime {
			t.Errorf("%s: WebAuthnStrictTime %d precedes DynamicMinBaseFeeTime %d",
				name, *c.WebAuthnStrictTime, *c.DynamicMinBaseFeeTime)
		}
	}
	// And it has to be rejected when it is scheduled before the fork it follows.
	// Start from a config whose block forks are all present, or CheckConfigForkOrder
	// stops at the first missing one and never reaches the entry under test.
	out := *AllEthashProtocolChanges
	out.ShanghaiTime = newUint64(2000)
	out.DynamicMinBaseFeeTime = newUint64(3000)
	out.WebAuthnStrictTime = newUint64(2500)
	err := out.CheckConfigForkOrder()
	if err == nil {
		t.Fatal("an out-of-order WebAuthnStrictTime was accepted")
	}
	if !strings.Contains(err.Error(), "webauthnStrictTime") {
		t.Errorf("CheckConfigForkOrder failed on something else: %v", err)
	}
}

func TestIncentivWebAuthnStrictSchedule(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *ChainConfig
		year int
		day  int
		hour int
	}{
		{"mainnet", IncentivMainnetChainConfig, 2026, 9, 0},
		{"devnet", IncentivDevnetChainConfig, 2026, 6, 13},
		{"testnet", IncentivTestnetChainConfig, 2028, 6, 12},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := uint64(time.Date(tc.year, time.October, tc.day, tc.hour, 0, 0, 0, time.UTC).Unix())
			if tc.cfg.WebAuthnStrictTime == nil || *tc.cfg.WebAuthnStrictTime != want {
				t.Fatalf("WebAuthnStrictTime = %v, want %d", tc.cfg.WebAuthnStrictTime, want)
			}
			if tc.cfg.IsWebAuthnStrict(want-1) || !tc.cfg.IsWebAuthnStrict(want) {
				t.Fatal("activation does not match the scheduled UTC boundary")
			}
		})
	}
}

// TestIncentivBerlinPrecedesWebAuthnStrict pins what core/vm relies on when it
// selects the WebAuthnStrict precompile set ahead of the block-based ones.
//
// That set is the Berlin set with one entry replaced, and CheckConfigForkOrder
// never compares a block fork with a timestamped one, so nothing in the config
// format stops one from activating WebAuthnStrict before Berlin — which would hand
// out Berlin pricing early. Rules.IsWebAuthnStrict refuses to be set without
// IsBerlin, so a node cannot reach that state; this pins the bundled configs so the
// gate never has to do that work, and so the fork fires when its timestamp says.
func TestIncentivBerlinPrecedesWebAuthnStrict(t *testing.T) {
	for name, c := range map[string]*ChainConfig{
		"mainnet": IncentivMainnetChainConfig,
		"testnet": IncentivTestnetChainConfig,
		"devnet":  IncentivDevnetChainConfig,
	} {
		if c.WebAuthnStrictTime == nil {
			continue
		}
		if c.BerlinBlock == nil {
			t.Errorf("%s: WebAuthnStrict is armed but berlinBlock is unset", name)
			continue
		}
		if c.BerlinBlock.Sign() != 0 {
			t.Errorf("%s: berlinBlock is %v, not genesis; the WebAuthnStrict precompile set "+
				"would apply Berlin pricing below that block", name, c.BerlinBlock)
		}
	}
}

// TestCheckCompatibleWebAuthnStrict covers the rollout window. Arming the fork on a
// node whose head is still short of the activation timestamp must leave the chain
// DB alone; moving the timestamp once it has fired must not be silently accepted,
// because the two binaries would then disagree on already-imported blocks.
func TestCheckCompatibleWebAuthnStrict(t *testing.T) {
	for _, tc := range []struct {
		name     string
		stored   *ChainConfig
		new      *ChainConfig
		headTime uint64
		wantErr  bool
	}{
		{
			name:     "arming an unarmed config before activation",
			stored:   &ChainConfig{},
			new:      &ChainConfig{WebAuthnStrictTime: newUint64(2000)},
			headTime: 1000,
			wantErr:  false,
		},
		{
			name:     "rescheduling before activation",
			stored:   &ChainConfig{WebAuthnStrictTime: newUint64(2000)},
			new:      &ChainConfig{WebAuthnStrictTime: newUint64(3000)},
			headTime: 1000,
			wantErr:  false,
		},
		{
			name:     "rescheduling after activation",
			stored:   &ChainConfig{WebAuthnStrictTime: newUint64(2000)},
			new:      &ChainConfig{WebAuthnStrictTime: newUint64(3000)},
			headTime: 2500,
			wantErr:  true,
		},
		{
			name:     "disarming after activation",
			stored:   &ChainConfig{WebAuthnStrictTime: newUint64(2000)},
			new:      &ChainConfig{},
			headTime: 2500,
			wantErr:  true,
		},
	} {
		err := tc.stored.CheckCompatible(tc.new, 100, tc.headTime)
		if tc.wantErr && err == nil {
			t.Errorf("%s: no error, want one", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
