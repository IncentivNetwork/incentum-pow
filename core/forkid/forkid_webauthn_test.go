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

package forkid

import (
	"testing"

	"github.com/ethereum/go-ethereum/params"
)

// TestIncentivMainnetWebAuthnStrictForkIDs covers what the fork means for peering,
// which is what decides whether the rollout window is safe and what happens to a
// node that misses it. WebAuthnStrictTime ends in "Time", so gatherForks folds it
// into the fork ID by reflection: before activation the hash is unchanged, so
// upgraded and un-upgraded nodes peer freely while operators restart; from
// activation the hash diverges, so the filter rejects the old binary's ID.
//
// That is a statement about the filter, which eth/protocols/eth/handshake.go
// applies once per peer while reading its status. It does not say anything about
// peers already connected when the fork activates: nothing re-runs the filter on
// them, so a node that missed the upgrade is kept out when it next connects
// rather than disconnected where it stands.
func TestIncentivMainnetWebAuthnStrictForkIDs(t *testing.T) {
	cfg := *params.IncentivMainnetChainConfig
	genesis := params.IncentivMainnetGenesisHash

	if cfg.WebAuthnStrictTime == nil || cfg.DynamicMinBaseFeeTime == nil {
		t.Fatal("test requires a mainnet config with WebAuthnStrictTime and DynamicMinBaseFeeTime set")
	}
	// The shipped activation, so the transition under test is the one the fleet will see.
	activation := *cfg.WebAuthnStrictTime
	if activation < *cfg.DynamicMinBaseFeeTime+60 {
		t.Fatalf("test precondition broken: WebAuthnStrictTime=%d must be >= DynamicMinBaseFeeTime=%d + 60s",
			activation, *cfg.DynamicMinBaseFeeTime)
	}

	// The binary operators run today: same config, minus this fork.
	current := cfg
	current.WebAuthnStrictTime = nil

	// Head one minute before activation — past every other fork this chain has.
	head := uint64(6_021_488)
	before := activation - 60

	currentID := NewID(&current, genesis, head, before)
	upgradedID := NewID(&cfg, genesis, head, before)

	if currentID.Hash != upgradedID.Hash {
		t.Fatalf("fork ID hash differs before activation, which would split peering during the rollout:\n  current  = %#v\n  upgraded = %#v",
			currentID, upgradedID)
	}
	if currentID.Next != 0 {
		t.Errorf("current binary advertises a next fork: have=%d want=0", currentID.Next)
	}
	if upgradedID.Next != activation {
		t.Errorf("upgraded binary Next: have=%d want=%d", upgradedID.Next, activation)
	}

	// During the rollout window each side must accept the other.
	upgradedFilter := newFilter(&cfg, genesis, func() (uint64, uint64) { return head, before })
	if err := upgradedFilter(currentID); err != nil {
		t.Errorf("upgraded node rejected a not-yet-upgraded peer before activation: %v", err)
	}
	currentFilter := newFilter(&current, genesis, func() (uint64, uint64) { return head, before })
	if err := currentFilter(upgradedID); err != nil {
		t.Errorf("not-yet-upgraded node rejected an upgraded peer before activation: %v", err)
	}

	// From activation the two are on different networks, and the un-upgraded node
	// is dropped rather than left to follow a chain of its own.
	atActivationID := NewID(&cfg, genesis, head, activation)
	if atActivationID.Hash == upgradedID.Hash {
		t.Fatal("fork ID hash did not change at activation")
	}
	afterFilter := newFilter(&cfg, genesis, func() (uint64, uint64) { return head, activation })
	if err := afterFilter(currentID); err == nil {
		t.Error("upgraded node still accepted a not-yet-upgraded peer after activation")
	}
}
