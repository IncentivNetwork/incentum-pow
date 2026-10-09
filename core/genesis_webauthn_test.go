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

package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
)

// mainnetTestHead is past every block-numbered setting IncentivMainnetChainConfig
// carries, so a disagreement about one of them is a disagreement about history rather
// than about a block still ahead of the node. mainnetEarlyHead is below the highest of
// them (irregularStateChangeHeight, 2429000) and above the ones at block 0.
const (
	mainnetTestHead  = uint64(9_000_000)
	mainnetEarlyHead = uint64(1_000_000)
)

// Exercise rollout paths against a mainnet activation far from the shipped one, so the
// synthetic heads below sit on the side of it each case needs whatever the bundled date
// is. These tests are serial and restore the shipped value after each case; that value
// is asserted in params/config_webauthn_test.go.
func armMainnetForRolloutTest(t *testing.T) {
	t.Helper()
	cfg := params.IncentivMainnetChainConfig
	shipped := cfg.WebAuthnStrictTime
	activation := uint64(1_850_000_000)
	cfg.WebAuthnStrictTime = &activation
	t.Cleanup(func() { cfg.WebAuthnStrictTime = shipped })
}

// writeSyntheticHead points the database's head header at the given block number and
// timestamp, so a setup path that consults the head sees a chain of that shape. The time
// matters as much as the number: it is what decides the timestamp half of
// CheckCompatible, and a stand whose blocks were mined at wall-clock time is already past
// shanghaiTime however short its chain.
func writeSyntheticHead(t *testing.T, db ethdb.Database, number, time uint64) *types.Header {
	t.Helper()
	header := &types.Header{Number: new(big.Int).SetUint64(number), Time: time, Difficulty: big.NewInt(1)}
	rawdb.WriteHeader(db, header)
	rawdb.WriteHeadHeaderHash(db, header.Hash())
	return header
}

// TestWebAuthnStrictOverride covers how a node started without a network flag picks
// up the fork.
//
// With no genesis, SetupGenesisBlockWithOverride reuses the chain config stored in the
// database rather than dropping AllProtocolChanges onto a private chain. The Incentiv
// networks are exempt: the stored genesis hash and chain id identify them, and the
// bundled schedule is applied as if the flag had been passed, so a missing or drifted
// fork time is corrected instead of being carried forward. An explicit
// --override.webauthnstrict still wins over it.
func TestWebAuthnStrictOverride(t *testing.T) {
	armMainnetForRolloutTest(t)
	// Both after ShanghaiTime and DynamicMinBaseFeeTime, which CheckConfigForkOrder
	// requires — an override that breaks the ordering is rejected, as it should be.
	const stored, override = uint64(1_800_000_000), uint64(1_900_000_000)

	// The real mainnet genesis, so the stored hash and chain id identify the network.
	// A chain config as an earlier binary would have stored it is then written over
	// the one Commit persisted, which is what a node upgrading from that binary has.
	setupErr := func(t *testing.T, storedCfg *params.ChainConfig, overrides *ChainOverrides) (*params.ChainConfig, error) {
		t.Helper()
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
		if err != nil {
			t.Fatal(err)
		}
		if block.Hash() != params.IncentivMainnetGenesisHash {
			t.Fatalf("genesis hash %s, want the bundled %s", block.Hash(), params.IncentivMainnetGenesisHash)
		}
		if storedCfg != nil {
			rawdb.WriteChainConfig(db, block.Hash(), storedCfg)
		}
		// genesis == nil is a node started without a network flag.
		c, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, overrides)
		return c, err
	}

	// A config as an earlier binary would have stored it: no WebAuthnStrictTime.
	baseConfig := func() *params.ChainConfig {
		c := *params.IncentivMainnetChainConfig
		c.WebAuthnStrictTime = nil
		return &c
	}

	// An Incentiv chain id is not a private network: the bundled schedule is applied
	// even without the flag, so a stored config missing the fork still gets it.
	got, err := setupErr(t, baseConfig(), nil)
	if err != nil {
		t.Fatalf("no-flag start on a stored Incentiv config failed: %v", err)
	}
	if got.WebAuthnStrictTime == nil {
		t.Error("the bundled fork time was not applied without a network flag")
	} else if *got.WebAuthnStrictTime != *params.IncentivMainnetChainConfig.WebAuthnStrictTime {
		t.Errorf("webauthnStrictTime = %d, want the bundled %d",
			*got.WebAuthnStrictTime, *params.IncentivMainnetChainConfig.WebAuthnStrictTime)
	}

	// A stored value that differs from the bundled one is replaced too, which is the
	// case a nil-only check used to wave through.
	drifted := baseConfig()
	drifted.WebAuthnStrictTime = &[]uint64{stored}[0]
	got, err = setupErr(t, drifted, nil)
	if err != nil {
		t.Fatalf("no-flag start on a drifted stored config failed: %v", err)
	}
	if *got.WebAuthnStrictTime == stored {
		t.Error("a stored timestamp that differs from the bundled schedule was kept")
	}

	// An explicit override still wins over the bundled schedule.
	got, err = setupErr(t, baseConfig(), &ChainOverrides{OverrideWebAuthnStrict: &[]uint64{override}[0]})
	if err != nil {
		t.Fatalf("override start failed: %v", err)
	}
	if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != override {
		t.Errorf("the override did not win over the bundled schedule: %v", got.WebAuthnStrictTime)
	}

	// Only now, after a call that applies an override, is the shared package-level
	// config worth checking: ChainOverrides.apply must have copied rather than
	// written through the pointer it was handed, or this leaks into every later test.
	if got := params.AllEthashProtocolChanges.WebAuthnStrictTime; got != nil {
		t.Errorf("applying an override mutated params.AllEthashProtocolChanges (webauthnStrictTime=%d)", *got)
	}
	if got := params.IncentivMainnetChainConfig.WebAuthnStrictTime; got == nil || *got == override {
		t.Error("applying an override mutated params.IncentivMainnetChainConfig")
	}
}

// TestRescheduleAcrossOldDeadline covers a node whose stored config carries an earlier
// activation than the bundled one — a build that carried a different time before this one.
// Before the old deadline the bundled schedule
// replaces it and is persisted. After it, a no-flag start keeps the stored one rather
// than rewind, and a flag start reports the ConfigCompatError that the rewind needs. A
// stored config with no fork time at all takes the bundled one either way, since nothing
// it holds has fired.
func TestRescheduleAcrossOldDeadline(t *testing.T) {
	for _, network := range []struct {
		name          string
		cfg           *params.ChainConfig
		genesis       func() *Genesis
		oldActivation uint64
		newActivation uint64
	}{
		{"devnet", params.IncentivDevnetChainConfig, DefaultIncentivDevnetGenesisBlock, 1_791_270_000, 1_791_291_600},
		{"mainnet", params.IncentivMainnetChainConfig, DefaultIncentivMainnetGenesisBlock, 1_791_302_400, 1_791_504_000},
	} {
		network := network
		t.Run(network.name, func(t *testing.T) {
			if at := network.cfg.WebAuthnStrictTime; at == nil || *at != network.newActivation {
				t.Fatalf("%s fork time = %v, want %d", network.name, at, network.newActivation)
			}
			for _, tc := range []struct {
				name      string
				headTime  uint64
				storedOld bool
				want      uint64
			}{
				{"old schedule before its deadline", network.oldActivation - 1, true, network.newActivation},
				{"old schedule after its deadline", network.oldActivation + 1, true, network.oldActivation},
				{"no stored schedule after old deadline", network.oldActivation + 1, false, network.newActivation},
			} {
				t.Run(tc.name, func(t *testing.T) {
					db := rawdb.NewMemoryDatabase()
					tdb := trie.NewDatabase(db)
					block, err := network.genesis().Commit(db, tdb)
					if err != nil {
						t.Fatal(err)
					}
					stored := *network.cfg
					if tc.storedOld {
						oldTime := network.oldActivation
						stored.WebAuthnStrictTime = &oldTime
					} else {
						stored.WebAuthnStrictTime = nil
					}
					rawdb.WriteChainConfig(db, block.Hash(), &stored)
					writeSyntheticHead(t, db, mainnetTestHead, tc.headTime)

					got, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != tc.want {
						t.Errorf("fork time after no-flag start = %v, want %d", got.WebAuthnStrictTime, tc.want)
					}
					if persisted := rawdb.ReadChainConfig(db, block.Hash()); persisted.WebAuthnStrictTime == nil || *persisted.WebAuthnStrictTime != tc.want {
						t.Errorf("fork time in the database = %v, want %d", persisted.WebAuthnStrictTime, tc.want)
					}
					if tc.storedOld && tc.headTime >= network.oldActivation {
						_, _, err = SetupGenesisBlockWithOverride(db, tdb, network.genesis(), nil)
						if _, ok := err.(*params.ConfigCompatError); !ok {
							t.Errorf("late flag start returned %v, want ConfigCompatError", err)
						}
					}
				})
			}
		})
	}
}

// TestWebAuthnStrictOverrideAfterActivation covers detection, not refusal: once the
// head is past activation, moving the timestamp has to surface as a ConfigCompatError
// here. What the node then does with it is NewBlockChain's business, and it rewinds
// the chain rather than refusing to start — see the "Point of no return" section of
// docs/webauthn/rollout.md.
//
// It goes through SetupGenesisBlockWithOverride rather than calling CheckCompatible
// directly, because the no-genesis branch used to hand CheckCompatible the same
// object on both sides, which made every comparison pass.
func TestWebAuthnStrictOverrideAfterActivation(t *testing.T) {
	const activation, moved = uint64(1_800_000_000), uint64(1_900_000_000)

	config := *params.IncentivMainnetChainConfig
	config.WebAuthnStrictTime = &[]uint64{activation}[0]

	db := rawdb.NewMemoryDatabase()
	tdb := trie.NewDatabase(db)
	genesis := &Genesis{Config: &config, BaseFee: big.NewInt(params.InitialBaseFee), Timestamp: activation + 1}
	block, err := genesis.Commit(db, tdb)
	if err != nil {
		t.Fatal(err)
	}
	rawdb.WriteHeadHeaderHash(db, block.Hash())

	_, _, err = SetupGenesisBlockWithOverride(db, tdb, nil, &ChainOverrides{OverrideWebAuthnStrict: &[]uint64{moved}[0]})
	if _, ok := err.(*params.ConfigCompatError); !ok {
		t.Fatalf("moving the timestamp past a head beyond activation returned %v, want a ConfigCompatError", err)
	}
}

// TestWebAuthnStrictOverrideOnFreshDatabase covers the first start of a new node.
// An override that breaks the fork ordering has to be rejected there and then, and a
// valid one has to end up in the database rather than being forgotten on restart —
// both of which used to happen only on the second start.
func TestWebAuthnStrictOverrideOnFreshDatabase(t *testing.T) {
	setup := func(t *testing.T, override uint64) (ethdb.Database, *params.ChainConfig, common.Hash, error) {
		t.Helper()
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		genesis := &Genesis{Config: params.IncentivMainnetChainConfig, BaseFee: big.NewInt(params.InitialBaseFee)}
		cfg, hash, err := SetupGenesisBlockWithOverride(db, tdb, genesis,
			&ChainOverrides{OverrideWebAuthnStrict: &[]uint64{override}[0]})
		return db, cfg, hash, err
	}

	// Earlier than DynamicMinBaseFeeTime, so the schedule is out of order.
	db, _, _, err := setup(t, 1_700_000_000)
	if err == nil {
		t.Error("an out-of-order override was accepted on a fresh database")
	}
	if rawdb.ReadCanonicalHash(db, 0) != (common.Hash{}) {
		t.Error("the genesis was committed even though the override was rejected")
	}

	// A valid one has to be persisted, or the next start silently reverts to the
	// bundled schedule.
	const valid = 1_900_000_000
	db, cfg, hash, err := setup(t, valid)
	if err != nil {
		t.Fatalf("a valid override was rejected: %v", err)
	}
	if cfg.WebAuthnStrictTime == nil {
		t.Fatalf("returned webauthnStrictTime is unset, want %d", uint64(valid))
	} else if *cfg.WebAuthnStrictTime != valid {
		t.Fatalf("returned webauthnStrictTime = %d, want %d", *cfg.WebAuthnStrictTime, uint64(valid))
	}
	stored := rawdb.ReadChainConfig(db, hash)
	if stored == nil {
		t.Fatal("no chain config was written")
	}
	if stored.WebAuthnStrictTime == nil {
		t.Errorf("stored webauthnStrictTime is unset, want %d", uint64(valid))
	} else if *stored.WebAuthnStrictTime != valid {
		t.Errorf("stored webauthnStrictTime = %d, want %d", *stored.WebAuthnStrictTime, uint64(valid))
	}
}

func TestShanghaiOverrideOnFreshDatabase(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	genesis := &Genesis{Config: params.IncentivMainnetChainConfig, BaseFee: big.NewInt(params.InitialBaseFee)}
	shanghai := *genesis.Config.ShanghaiTime + 60
	cfg, hash, err := SetupGenesisBlockWithOverride(db, trie.NewDatabase(db), genesis,
		&ChainOverrides{OverrideShanghai: &shanghai})
	if err != nil {
		t.Fatal(err)
	}
	stored := rawdb.ReadChainConfig(db, hash)
	if stored == nil || stored.ShanghaiTime == nil || *stored.ShanghaiTime != shanghai {
		t.Fatalf("stored Shanghai fork = %v, want %d", stored, shanghai)
	}
	if cfg.ShanghaiTime == nil || *cfg.ShanghaiTime != shanghai {
		t.Fatalf("returned Shanghai fork = %v, want %d", cfg.ShanghaiTime, shanghai)
	}
}

// TestBundledIncentivConfigNeedsBothIdentifiers pins what makes a chain one of ours:
// its genesis hash and its chain id, both. The id alone would claim any chain that
// picked one of these numbers; the hash alone would not separate mainnet from devnet,
// which share one. What this leaves outside is a chain with its own genesis — not a
// chain built from a genesis this project publishes, which is that network by
// construction; see TestDocumentedTestnetGenesisIsTestnet.
func TestBundledIncentivConfigNeedsBothIdentifiers(t *testing.T) {
	mainnet := params.IncentivMainnetChainConfig
	if got := params.BundledIncentivConfig(params.IncentivMainnetGenesisHash, mainnet.ChainID); got != mainnet {
		t.Error("the real mainnet genesis and chain id were not recognised")
	}
	if got := params.BundledIncentivConfig(common.Hash{0xde, 0xad}, mainnet.ChainID); got != nil {
		t.Error("a foreign genesis with an Incentiv chain id was claimed as an Incentiv network")
	}
	if got := params.BundledIncentivConfig(params.IncentivMainnetGenesisHash, big.NewInt(1)); got != nil {
		t.Error("the Incentiv genesis with a foreign chain id was claimed")
	}
	// Mainnet and devnet share a genesis hash, so the id is what separates them.
	if got := params.BundledIncentivConfig(params.IncentivDevnetGenesisHash, params.IncentivDevnetChainConfig.ChainID); got != params.IncentivDevnetChainConfig {
		t.Error("devnet was not told apart from mainnet by chain id")
	}
	testnet := params.IncentivTestnetChainConfig
	if got := params.BundledIncentivConfig(params.IncentivTestnetGenesisHash, testnet.ChainID); got != testnet {
		t.Error("testnet was not recognised")
	}
}

// TestDocumentedTestnetGenesisIsTestnet reads the genesis.json out of
// TESTNET_V2_DEPLOYMENT.md and checks what this binary makes of a chain built from it.
//
// A genesis config does not enter the block hash, so the minimal config in that file
// does not stop it hashing to the testnet genesis — which means a chain initialised from
// the document is matched as testnet. That matching is correct, the way a chain built
// from Ethereum's genesis is Ethereum, but it surprises people standing up a local chain
// from those instructions, so it is pinned here against both sides: change the document
// or the bundled genesis and this fails.
//
// What such a chain then does with the bundled schedule is
// TestNoFlagStartNeverRewinds, which runs the setup path over it rather than inferring
// the answer from the configs.
func TestDocumentedTestnetGenesisIsTestnet(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "TESTNET_V2_DEPLOYMENT.md"))
	if err != nil {
		t.Fatalf("read the deployment document: %v", err)
	}
	// The first fenced json block in the file is the genesis.
	start := bytes.Index(doc, []byte("```json"))
	if start < 0 {
		t.Fatal("no fenced json block in TESTNET_V2_DEPLOYMENT.md")
	}
	start += len("```json")
	end := bytes.Index(doc[start:], []byte("```"))
	if end < 0 {
		t.Fatal("unterminated json block in TESTNET_V2_DEPLOYMENT.md")
	}

	var documented Genesis
	if err := json.Unmarshal(doc[start:start+end], &documented); err != nil {
		t.Fatalf("the genesis.json in the document does not parse: %v", err)
	}

	hash := documented.ToBlock().Hash()
	if hash != params.IncentivTestnetGenesisHash {
		t.Fatalf("the documented genesis hashes to %s, the bundled testnet genesis is %s; "+
			"if that is deliberate, the warning in TESTNET_V2_DEPLOYMENT.md needs revisiting",
			hash, params.IncentivTestnetGenesisHash)
	}
	if got := params.BundledIncentivConfig(hash, documented.Config.ChainID); got != params.IncentivTestnetChainConfig {
		t.Errorf("a chain built from the documented genesis is not matched as testnet")
	}
}

// TestNetworkFlagCannotChangeStoredChainID covers the pair that shares a genesis hash.
// CheckCompatible reports a changed chain id as an incompatibility at block 0, which the
// setup path discards, so --incentiv-devnet on a mainnet datadir used to rewrite the
// stored chain id from 24101 to 12730 with no error and no rewind, and the node then ran
// devnet's schedule on mainnet's chain. The read-only commands refused this already; the
// node has to as well.
func TestNetworkFlagCannotChangeStoredChainID(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	tdb := trie.NewDatabase(db)
	block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
	if err != nil {
		t.Fatal(err)
	}
	if DefaultIncentivDevnetGenesisBlock().ToBlock().Hash() != block.Hash() {
		t.Fatal("mainnet and devnet no longer share a genesis hash, so this test covers nothing")
	}
	stored := *params.IncentivMainnetChainConfig
	rawdb.WriteChainConfig(db, block.Hash(), &stored)
	writeSyntheticHead(t, db, mainnetTestHead, 0)

	_, _, err = SetupGenesisBlockWithOverride(db, tdb, DefaultIncentivDevnetGenesisBlock(), nil)
	if err == nil {
		t.Fatal("--incentiv-devnet on a mainnet datadir was accepted")
	}
	if _, ok := err.(*params.ConfigCompatError); ok {
		t.Fatalf("the wrong flag was reported as a rewind rather than refused: %v", err)
	}
	for _, want := range []string{"24101", "12730"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name chain %s: %v", want, err)
		}
	}
	if persisted := rawdb.ReadChainConfig(db, block.Hash()); persisted.ChainID.Cmp(stored.ChainID) != 0 {
		t.Errorf("the stored chain id was rewritten to %v", persisted.ChainID)
	}

	// The right flag is not a chain id change, so it goes through as before.
	got, _, err := SetupGenesisBlockWithOverride(db, tdb, DefaultIncentivMainnetGenesisBlock(), nil)
	if err != nil {
		t.Fatalf("the mainnet flag on a mainnet datadir failed: %v", err)
	}
	if got.ChainID.Cmp(stored.ChainID) != 0 {
		t.Errorf("chain id %v after the right flag, want %v", got.ChainID, stored.ChainID)
	}

	// The same database with its genesis state missing takes the branch that re-commits
	// the genesis specification, which used to run before the chain id was compared and
	// so wrote the wrong network's configuration and reset the head. It has to be refused
	// the same way, with nothing written.
	headBefore := rawdb.ReadHeadHeaderHash(db)
	rawdb.DeleteLegacyTrieNode(db, block.Root())
	if rawdb.HasLegacyTrieNode(db, block.Root()) {
		t.Fatal("the genesis state root is still there, so this case tests nothing")
	}
	_, _, err = SetupGenesisBlockWithOverride(db, tdb, DefaultIncentivDevnetGenesisBlock(), nil)
	if err == nil {
		t.Fatal("--incentiv-devnet on a mainnet datadir with no genesis state was accepted")
	}
	if !strings.Contains(err.Error(), "24101") || !strings.Contains(err.Error(), "12730") {
		t.Errorf("the refusal does not name both chains: %v", err)
	}
	if persisted := rawdb.ReadChainConfig(db, block.Hash()); persisted.ChainID.Cmp(stored.ChainID) != 0 {
		t.Errorf("the stored chain id was rewritten to %v through the missing-state branch", persisted.ChainID)
	}
	if rawdb.ReadHeadHeaderHash(db) != headBefore {
		t.Error("the refusal moved the head")
	}
	// And the right flag restores the state through that branch, as it always did.
	if _, _, err := SetupGenesisBlockWithOverride(db, tdb, DefaultIncentivMainnetGenesisBlock(), nil); err != nil {
		t.Fatalf("the mainnet flag on a mainnet datadir with no genesis state failed: %v", err)
	}
	if !rawdb.HasLegacyTrieNode(db, block.Root()) {
		t.Error("the right flag did not restore the genesis state")
	}
}

// TestIncentivGenesisWithoutStoredConfig covers a database that holds one of these
// genesis blocks but no chain config record. configOrDefault answers
// AllEthashProtocolChanges there — chain id 1337, none of this chain's forks — and
// writing that would give the node another network's schedule. The genesis hash cannot
// say which Incentiv network it is, since mainnet and devnet share one, so the only
// safe answer is to refuse and let the operator name it.
func TestIncentivGenesisWithoutStoredConfig(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	tdb := trie.NewDatabase(db)
	block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(append([]byte("ethereum-config-"), block.Hash().Bytes()...)); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, nil)
	if err == nil {
		t.Fatalf("started with chain id %v instead of refusing", cfg.ChainID)
	}
	if !strings.Contains(err.Error(), "network flag") {
		t.Errorf("unexpected error: %v", err)
	}
	if rawdb.ReadChainConfig(db, block.Hash()) != nil {
		t.Error("a chain config was written even though startup was refused")
	}

	// With the network flag it starts, and with the right schedule.
	cfg, _, err = SetupGenesisBlockWithOverride(db, tdb, DefaultIncentivMainnetGenesisBlock(), nil)
	if err != nil {
		t.Fatalf("the network flag did not resolve it: %v", err)
	}
	if cfg.ChainID.Cmp(params.IncentivMainnetChainConfig.ChainID) != 0 {
		t.Errorf("chain id %v, want %v", cfg.ChainID, params.IncentivMainnetChainConfig.ChainID)
	}
}

// TestBundledConfigNotAdoptedAcrossHistoricalForks covers the limit on the adoption
// above. checkCompatible compares neither FeePoolBlock, MinBaseFeeBlock,
// MinBaseFeeChangeHeight, ZeroRewardBlock, FastBlock nor IrregularStateChangeHeight, so
// taking the bundled config over a stored one that disagrees about them would apply
// those rules to history that never followed them, with no error and no rewind. When
// they differ the stored config is kept whole — including, deliberately, the fork this
// release adds, which is why such a node is left behind and logs a warning saying so.
func TestBundledConfigNotAdoptedAcrossHistoricalForks(t *testing.T) {
	armMainnetForRolloutTest(t)
	setup := func(t *testing.T, storedCfg *params.ChainConfig, head, time uint64) (*params.ChainConfig, error) {
		t.Helper()
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
		if err != nil {
			t.Fatal(err)
		}
		rawdb.WriteChainConfig(db, block.Hash(), storedCfg)
		writeSyntheticHead(t, db, head, time)
		// genesis == nil is a node started without a network flag.
		got, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, nil)
		if _, ok := err.(*params.ConfigCompatError); ok {
			t.Fatalf("a no-flag start reported a rewind: %v", err)
		}
		if persisted := rawdb.ReadChainConfig(db, block.Hash()); err != nil && !sameChainConfigJSON(t, persisted, storedCfg) {
			t.Error("a refused start changed the stored configuration")
		}
		return got, err
	}

	// An earlier binary's config: the bundled one without the fork this release adds.
	// On its own it is adopted, which TestWebAuthnStrictOverride asserts; here it is
	// the control, so that every case below differs in exactly one field.
	agreeing := *params.IncentivMainnetChainConfig
	agreeing.WebAuthnStrictTime = nil
	if got, err := setup(t, &agreeing, mainnetTestHead, 0); err != nil || got.WebAuthnStrictTime == nil {
		t.Fatalf("the control case was not adopted (%v), so this test proves nothing about the cases below", err)
	}

	// Every field HistoricalForksCompatible compares gets its own case. Covering one of
	// them and assuming the rest is how three of the six came to be droppable
	// without a single test noticing. Mainnet sets all six, so nil is a real
	// disagreement and also the realistic one — it is what a config written from a
	// bare genesis.json looks like.
	for _, tc := range []struct {
		field string
		set   func(*params.ChainConfig)
	}{
		{"feePoolBlock", func(c *params.ChainConfig) { c.FeePoolBlock = nil }},
		{"minBaseFeeBlock", func(c *params.ChainConfig) { c.MinBaseFeeBlock = nil }},
		{"minBaseFeeChangeHeight", func(c *params.ChainConfig) { c.MinBaseFeeChangeHeight = nil }},
		{"zeroRewardBlock", func(c *params.ChainConfig) { c.ZeroRewardBlock = nil }},
		{"fastBlock", func(c *params.ChainConfig) { c.FastBlock = nil }},
		{"irregularStateChangeHeight", func(c *params.ChainConfig) { c.IrregularStateChangeHeight = nil }},
		// Not only a missing setting: one that is set to a different block has to
		// stop the adoption too, which nil-only cases would never exercise.
		{"fastBlock moved", func(c *params.ChainConfig) { c.FastBlock = big.NewInt(999_999) }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			disagreeing := agreeing
			tc.set(&disagreeing)
			if params.HistoricalForksCompatible(&disagreeing, params.IncentivMainnetChainConfig, mainnetTestHead) {
				t.Fatalf("HistoricalForksCompatible does not compare %s, so this case cannot test the adoption", tc.field)
			}
			got, err := setup(t, &disagreeing, mainnetTestHead, 0)
			if err != nil {
				t.Fatalf("a node far from the activation was refused: %v", err)
			}
			if got.WebAuthnStrictTime != nil {
				t.Errorf("the bundled schedule was adopted over a config disagreeing about %s (webauthnStrictTime=%d)", tc.field, *got.WebAuthnStrictTime)
			}
			if !sameChainConfigJSON(t, got, &disagreeing) {
				t.Errorf("the stored config was not kept whole: rules were applied to history that never followed them")
			}
			// Kept whole, and once the activation is near, not run: the kept config has
			// no fork time, so this node would split from the network at activation,
			// and inside the window the start says so instead.
			near := *params.IncentivMainnetChainConfig.WebAuthnStrictTime - 3600
			if _, err := setup(t, &disagreeing, mainnetTestHead, near); err == nil {
				t.Errorf("a node left without the fork over a disagreement about %s was started an hour before activation", tc.field)
			}
		})
	}
}

// TestBundledConfigAdoptedForSettingsAheadOfHead is the other half of the rule above.
// A block-numbered setting the stored config lacks but which is still ahead of the head
// rewrites nothing, so refusing the bundled schedule over it would strand the node for
// no reason — and this chain ships those settings exactly that way, as a value some
// releases ahead of every node. minBaseFeeBlock is 1295000 on mainnet; with the head
// below it the disagreement is about the future, and the schedule is taken.
func TestBundledConfigAdoptedForSettingsAheadOfHead(t *testing.T) {
	armMainnetForRolloutTest(t)
	stored := *params.IncentivMainnetChainConfig
	stored.WebAuthnStrictTime = nil
	stored.MinBaseFeeBlock = nil
	stored.MinBaseFeeChangeHeight = nil
	stored.IrregularStateChangeHeight = nil

	db := rawdb.NewMemoryDatabase()
	tdb := trie.NewDatabase(db)
	block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
	if err != nil {
		t.Fatal(err)
	}
	rawdb.WriteChainConfig(db, block.Hash(), &stored)
	writeSyntheticHead(t, db, mainnetEarlyHead, 0)

	got, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, nil)
	if err != nil {
		t.Fatalf("no-flag start failed: %v", err)
	}
	if got.WebAuthnStrictTime == nil {
		t.Error("the schedule was refused over settings that are still ahead of the head")
	}
	if got.MinBaseFeeBlock == nil {
		t.Error("minBaseFeeBlock was not adopted, so a release shipping one ahead of the head would not reach this node")
	}

	// The same configuration, once the head is past those blocks, is a disagreement
	// about history and must be refused. Without that contrast this test would pass
	// just as well if the head were ignored.
	db2 := rawdb.NewMemoryDatabase()
	tdb2 := trie.NewDatabase(db2)
	block2, err := DefaultIncentivMainnetGenesisBlock().Commit(db2, tdb2)
	if err != nil {
		t.Fatal(err)
	}
	rawdb.WriteChainConfig(db2, block2.Hash(), &stored)
	writeSyntheticHead(t, db2, mainnetTestHead, 0)

	got, _, err = SetupGenesisBlockWithOverride(db2, tdb2, nil, nil)
	if err != nil {
		t.Fatalf("no-flag start failed: %v", err)
	}
	if got.WebAuthnStrictTime != nil || got.MinBaseFeeBlock != nil {
		t.Error("with the head past those blocks the bundled config was adopted anyway")
	}
}

// sameChainConfigJSON compares two configs the way SetupGenesisBlockWithOverride
// decides whether to rewrite one.
func sameChainConfigJSON(t *testing.T, a, b *params.ChainConfig) bool {
	t.Helper()
	ja, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	jb, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(ja, jb)
}

// TestConfigOrStoredAnswersForTheOverrides covers what the read-only chain commands
// compare against. They refuse when the configuration a start would settle on differs
// from the stored one, because they cannot persist the change; so this has to resolve
// the same way the setup path does, overrides included. Leaving the overrides out made
// a node running with one look permanently out of date to its own geth export.
func TestConfigOrStoredAnswersForTheOverrides(t *testing.T) {
	armMainnetForRolloutTest(t)
	const override = uint64(1_900_000_000)

	stored := *params.IncentivMainnetChainConfig
	stored.WebAuthnStrictTime = nil

	// No genesis specification: the bundled schedule, because the block-numbered
	// forks agree.
	var g *Genesis
	head := &types.Header{Number: new(big.Int).SetUint64(mainnetTestHead)}
	got, decision := g.ConfigOrStored(&stored, params.IncentivMainnetGenesisHash, head, nil)
	if !decision.BundledAdopted {
		t.Error("the bundled schedule was not reported as adopted")
	}
	if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != *params.IncentivMainnetChainConfig.WebAuthnStrictTime {
		t.Errorf("without overrides: webauthnStrictTime = %v, want the bundled one", got.WebAuthnStrictTime)
	}

	// The same, with the override the node runs with.
	got, _ = g.ConfigOrStored(&stored, params.IncentivMainnetGenesisHash, head, &ChainOverrides{OverrideWebAuthnStrict: &[]uint64{override}[0]})
	if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != override {
		t.Errorf("with an override: webauthnStrictTime = %v, want %d", got.WebAuthnStrictTime, override)
	}
	if params.IncentivMainnetChainConfig.WebAuthnStrictTime == nil || *params.IncentivMainnetChainConfig.WebAuthnStrictTime == override {
		t.Error("applying an override mutated the bundled package-level config")
	}

	// A config that disagrees about the block-numbered forks is not adopted, so what
	// a start settles on is the stored config, and the override still applies to it.
	// One field per case, for the same reason as in the test above.
	for _, tc := range []struct {
		field string
		set   func(*params.ChainConfig)
	}{
		{"feePoolBlock", func(c *params.ChainConfig) { c.FeePoolBlock = nil }},
		{"minBaseFeeBlock", func(c *params.ChainConfig) { c.MinBaseFeeBlock = nil }},
		{"minBaseFeeChangeHeight", func(c *params.ChainConfig) { c.MinBaseFeeChangeHeight = nil }},
		{"zeroRewardBlock", func(c *params.ChainConfig) { c.ZeroRewardBlock = nil }},
		{"fastBlock", func(c *params.ChainConfig) { c.FastBlock = nil }},
		{"irregularStateChangeHeight", func(c *params.ChainConfig) { c.IrregularStateChangeHeight = nil }},
	} {
		t.Run(tc.field, func(t *testing.T) {
			disagreeing := stored
			tc.set(&disagreeing)
			got, decision := g.ConfigOrStored(&disagreeing, params.IncentivMainnetGenesisHash, head, &ChainOverrides{OverrideWebAuthnStrict: &[]uint64{override}[0]})
			if !decision.BundledRefused {
				t.Errorf("the refusal was not reported, so the setup path would log the wrong thing")
			}
			if !sameChainConfigJSON(t, withWebAuthnStrict(got, nil), &disagreeing) {
				t.Errorf("the bundled config was adopted over a stored one disagreeing about %s", tc.field)
			}
			if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != override {
				t.Errorf("the override was dropped along with the adoption: %v", got.WebAuthnStrictTime)
			}
		})
	}

	// The real mainnet genesis is the one hash the setup path does not treat as a
	// private chain: it takes configOrDefault's answer, params.MainnetChainConfig,
	// whatever the database says. Resolving to the stored config here reported no
	// difference while the next start would have rewritten it.
	ethereum := *params.MainnetChainConfig
	ethereum.ShanghaiTime = nil
	got, _ = g.ConfigOrStored(&ethereum, params.MainnetGenesisHash, head, nil)
	if got.ShanghaiTime == nil {
		t.Error("with the mainnet genesis hash the stored config was reused, but a start would have replaced it")
	}

	// A genesis specification answers for itself, overrides included: that is the
	// path a chain command takes when it is given a network flag.
	g = DefaultIncentivMainnetGenesisBlock()
	got, decision = g.ConfigOrStored(&stored, params.IncentivMainnetGenesisHash, head, &ChainOverrides{OverrideWebAuthnStrict: &[]uint64{override}[0]})
	if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != override {
		t.Errorf("with a genesis specification: webauthnStrictTime = %v, want %d", got.WebAuthnStrictTime, override)
	}
	if !decision.FromGenesis || decision.BundledRefused {
		t.Errorf("a start with a network flag was reported as %+v; the setup path logs a refusal off this and would claim the stored config had been kept", decision)
	}
}

// withWebAuthnStrict returns a copy of cfg with the fork time set to t, so that a
// comparison can ignore the one field an override is expected to change.
func withWebAuthnStrict(cfg *params.ChainConfig, t *uint64) *params.ChainConfig {
	cpy := *cfg
	cpy.WebAuthnStrictTime = t
	return &cpy
}

// documentedTestnetConfig is the config in the genesis.json of TESTNET_V2_DEPLOYMENT.md:
// testnet's chain id and the pre-Shanghai blocks, and none of the settings testnet runs.
func documentedTestnetConfig() *params.ChainConfig {
	return &params.ChainConfig{
		ChainID:             big.NewInt(28802),
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),
		BerlinBlock:         big.NewInt(0),
		LondonBlock:         big.NewInt(0),
	}
}

// TestNoFlagStartNeverRewinds covers the second bound on an implicit adoption. A stand
// built from the genesis.json in TESTNET_V2_DEPLOYMENT.md hashes to testnet's genesis, so
// the bundled schedule is a candidate — but that schedule carries shanghaiTime, and a
// stand mining at wall-clock time is already past it however short its chain. Taking the
// schedule there would hand NewBlockChain a ConfigCompatError, which it acts on by
// rewinding: for such a stand, back to genesis. Nobody asked for that by starting without
// a network flag.
//
// The head time is the whole point of the cases below. With it at zero — which is what
// these tests used to write — the timestamp half of CheckCompatible is suppressed and the
// adoption goes through, which is also why the freshly-initialised case adopts.
//
// The name is about the adoption, not about the whole start: an override is applied over
// the stored config after a refusal, and one naming a timestamp already behind the head
// does rewind. That is the operator asking for it, and
// TestStartupWarningsMatchWhatHappened covers it.
func TestNoFlagStartNeverRewinds(t *testing.T) {
	stored := documentedTestnetConfig()

	for _, tc := range []struct {
		name        string
		head        uint64
		time        uint64
		wantAdopted bool
	}{
		// The state the documented procedure leaves behind: initialised and not yet
		// started, so the head is the genesis block at timestamp 0. Nothing has been
		// mined under the bare config, so there is nothing to protect and the schedule
		// is taken — which is what makes such a chain testnet from its first block.
		{"freshly initialised", 0, 0, true},
		// Short chain, but mined in 2026, so shanghaiTime is behind the head.
		{"a few blocks at wall-clock time", 1_000, 1_790_000_000, false},
		// Below fastBlock 319000, so the block-numbered bound alone would allow it.
		{"just below fastBlock", 318_999, 1_790_000_000, false},
		// Past fastBlock: refused by the block-numbered bound as well.
		{"past fastBlock", 400_000, 1_790_000_000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			tdb := trie.NewDatabase(db)
			block, err := DefaultIncentivTestnetGenesisBlock().Commit(db, tdb)
			if err != nil {
				t.Fatal(err)
			}
			rawdb.WriteChainConfig(db, block.Hash(), stored)
			writeSyntheticHead(t, db, tc.head, tc.time)

			got, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, nil)
			// Testnet's activation is in 2028, so a stand mined in 2026 is far from it:
			// it starts, with the warning. The refusal near the activation is covered in
			// TestStartupWarningsMatchWhatHappened.
			if err != nil {
				t.Fatalf("a start without a network flag must not fail this far from the activation, and must not report a rewind: %v", err)
			}
			if adopted := got.ShanghaiTime != nil; adopted != tc.wantAdopted {
				t.Errorf("bundled schedule adopted=%v, want %v", adopted, tc.wantAdopted)
			}
			if tc.wantAdopted {
				if !sameChainConfigJSON(t, got, params.IncentivTestnetChainConfig) {
					t.Error("the bundled testnet configuration was not adopted in full")
				}
				return
			}
			if got.ShanghaiTime != nil {
				t.Error("the bundled schedule was adopted on a stand that would rewind")
			}
			if !sameChainConfigJSON(t, got, stored) {
				t.Error("the stored configuration was not kept whole")
			}
		})
	}
}

// TestConfigOrStoredDoesNotLeakTheBundledConfig pins the defensive copy. Without it a
// caller with no overrides is handed params.IncentivMainnetChainConfig itself, and geth
// holds the configuration it is given for the life of the process.
func TestConfigOrStoredDoesNotLeakTheBundledConfig(t *testing.T) {
	armMainnetForRolloutTest(t)
	stored := *params.IncentivMainnetChainConfig
	stored.WebAuthnStrictTime = nil

	var g *Genesis
	head := &types.Header{Number: new(big.Int).SetUint64(mainnetTestHead)}
	got, decision := g.ConfigOrStored(&stored, params.IncentivMainnetGenesisHash, head, nil)
	if !decision.BundledAdopted {
		t.Fatal("the bundled schedule was not adopted, so this test proves nothing")
	}
	if got == params.IncentivMainnetChainConfig {
		t.Fatal("the package-level configuration was returned directly")
	}
	marker := uint64(1)
	got.WebAuthnStrictTime = &marker
	if v := params.IncentivMainnetChainConfig.WebAuthnStrictTime; v == nil || *v == marker {
		t.Error("writing to the returned configuration reached the package-level one")
	}
}

// captureWarnings redirects the root logger for the duration of a test and returns what
// it sees at WARN or above, each record rendered as its message followed by its context
// pairs. The warnings in SetupGenesisBlockWithOverride are the node's only account of a
// decision an operator is told to act on — the runbook reads one of them as "this node
// will not activate" — so both the message and the fields are worth asserting. The
// fields especially: most of what an operator acts on is in them, and a first version of
// this helper kept only r.Msg, which made every assertion about their contents vacuous.
func captureWarnings(t *testing.T) func() []string {
	t.Helper()
	var msgs []string
	previous := log.Root().GetHandler()
	log.Root().SetHandler(log.FuncHandler(func(r *log.Record) error {
		if r.Lvl <= log.LvlWarn {
			rendered := r.Msg
			for i := 0; i+1 < len(r.Ctx); i += 2 {
				rendered += fmt.Sprintf(" %v=%v", r.Ctx[i], r.Ctx[i+1])
			}
			msgs = append(msgs, rendered)
		}
		return nil
	}))
	t.Cleanup(func() { log.Root().SetHandler(previous) })
	return func() []string { return msgs }
}

func containsMatch(msgs []string, substr string) bool {
	for _, m := range msgs {
		if strings.Contains(m, substr) {
			return true
		}
	}
	return false
}

// TestStartupWarningsMatchWhatHappened pins the log against the decision. The defect this
// exists for was a warning that said the stored config had been kept while the network
// flag's schedule was being written, and docs/webauthn/rollout.md tells an operator to
// resync a node that logs it.
func TestStartupWarningsMatchWhatHappened(t *testing.T) {
	armMainnetForRolloutTest(t)
	setup := func(t *testing.T, storedCfg *params.ChainConfig, genesis *Genesis, head, time uint64, overrides *ChainOverrides) ([]string, *params.ChainConfig, error) {
		t.Helper()
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
		if err != nil {
			t.Fatal(err)
		}
		rawdb.WriteChainConfig(db, block.Hash(), storedCfg)
		writeSyntheticHead(t, db, head, time)
		warnings := captureWarnings(t)
		got, _, err := SetupGenesisBlockWithOverride(db, tdb, genesis, overrides)
		return warnings(), got, err
	}
	setupOnce := func(t *testing.T, storedCfg *params.ChainConfig, genesis *Genesis, head, time uint64, overrides *ChainOverrides) (*params.ChainConfig, common.Hash, error) {
		t.Helper()
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
		if err != nil {
			t.Fatal(err)
		}
		rawdb.WriteChainConfig(db, block.Hash(), storedCfg)
		writeSyntheticHead(t, db, head, time)
		return SetupGenesisBlockWithOverride(db, tdb, genesis, overrides)
	}
	mustSetup := func(t *testing.T, storedCfg *params.ChainConfig, genesis *Genesis, head, time uint64, overrides *ChainOverrides) []string {
		t.Helper()
		msgs, _, err := setup(t, storedCfg, genesis, head, time, overrides)
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}
		return msgs
	}

	// A stored config that disagrees about a block-numbered setting already behind the
	// head, started *with* the network flag. The flag's schedule is written, so the
	// refusal warning would be a lie; what is true is that those settings are being
	// replaced for blocks already mined.
	t.Run("network flag over a historical disagreement", func(t *testing.T) {
		stored := *params.IncentivMainnetChainConfig
		stored.IrregularStateChangeHeight = nil
		msgs := mustSetup(t, &stored, DefaultIncentivMainnetGenesisBlock(), mainnetTestHead, 0, nil)
		if containsMatch(msgs, "Stored chain config disagrees") || containsMatch(msgs, "would rewind the chain") {
			t.Errorf("a start with the network flag logged a refusal it did not make: %q", msgs)
		}
		if !containsMatch(msgs, "Network flag replaces block-numbered settings") {
			t.Errorf("the replacement was not reported: %q", msgs)
		}
	})

	// The same disagreement without a flag: the refusal is real and the warning is the
	// one the runbook acts on.
	t.Run("no flag over a historical disagreement", func(t *testing.T) {
		stored := *params.IncentivMainnetChainConfig
		stored.IrregularStateChangeHeight = nil
		msgs := mustSetup(t, &stored, nil, mainnetTestHead, 0, nil)
		if !containsMatch(msgs, "Stored chain config disagrees") {
			t.Errorf("the refusal was not reported: %q", msgs)
		}
		// The stored config has a fork time, so the resolved value alone would have
		// produced "nothing further, if this is deliberate" — which leaves out that this
		// node disagrees with the network about a block already mined.
		if !containsMatch(msgs, "resync from genesis under the network flag") {
			t.Errorf("a history refusal was not pointed at a resync: %q", msgs)
		}
		if containsMatch(msgs, "nothing further") {
			t.Errorf("a node disagreeing about blocks already mined was told nothing further is needed: %q", msgs)
		}
	})

	// The same refusal with no fork time in the stored config: the stand built by
	// following TESTNET_V2_DEPLOYMENT.md, past the block-numbered settings testnet
	// carries. The network flag does not rewind such a datadir — it writes those settings
	// over blocks already mined — so the late-node remedy is the wrong instruction here,
	// and rollout.md says as much.
	t.Run("no flag over a historical disagreement with no fork time", func(t *testing.T) {
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		block, err := DefaultIncentivTestnetGenesisBlock().Commit(db, tdb)
		if err != nil {
			t.Fatal(err)
		}
		rawdb.WriteChainConfig(db, block.Hash(), documentedTestnetConfig())
		writeSyntheticHead(t, db, 600_000, 1_790_000_000)
		warnings := captureWarnings(t)
		got, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, nil)
		// Warned, and run: testnet's bundled activation is in 2028, so the node is not
		// about to miss anything, and a stand that ran on the previous release keeps
		// running on this one. The refusal is for a network whose activation is near.
		if err != nil {
			t.Fatalf("a stand two years from its fork was refused: %v", err)
		}
		if got.ShanghaiTime != nil {
			t.Fatal("the schedule was adopted, so this case tests nothing")
		}
		msgs := warnings()
		if !containsMatch(msgs, "webauthnStrictTime=unset") {
			t.Errorf("the line does not report that the fork is unset: %q", msgs)
		}
		if !containsMatch(msgs, "resync from genesis under the network flag") {
			t.Errorf("a history refusal was not pointed at a resync: %q", msgs)
		}
		if containsMatch(msgs, "restart with the network flag, which does take the schedule") {
			t.Errorf("a history refusal was given the late-node remedy, which does not repair this datadir: %q", msgs)
		}
		// The flag does not undo the block-numbered settings, but this datadir has no
		// shanghaiTime either, so the same start does rewind — for a timestamp fork, whose
		// target is before 2025-08-14. A rewind drops the blocks above its target, so what
		// it clears depends on where that falls: here the whole chain is above it and the
		// rewind removes every block the settings were applied to. The remedy has to say a
		// rewind happens rather than leave "does not rewind" to be read as "nothing
		// happens".
		if !containsMatch(msgs, "would still rewind the chain") {
			t.Errorf("the remedy does not say that start rewinds for another reason: %q", msgs)
		}

		// The same stand with its head inside the window before testnet's activation
		// is refused: from there it would miss the fork. It cannot take the override —
		// its config has no shanghaiTime, and CheckConfigForkOrder refuses a
		// webauthnStrictTime ahead of an unset one — so the refusal names the way out
		// for a private stand, a chain id of its own.
		near := *params.IncentivTestnetChainConfig.WebAuthnStrictTime - 24*3600
		writeSyntheticHead(t, db, 600_000, near)
		_, _, err = SetupGenesisBlockWithOverride(db, tdb, nil, nil)
		if err == nil {
			t.Fatal("a stand a day from its fork was started without it")
		}
		if _, ok := err.(*params.ConfigCompatError); ok {
			t.Fatalf("the refusal came back as a rewind: %v", err)
		}
		if !strings.Contains(err.Error(), "chain id of its own") || !strings.Contains(err.Error(), "--override.webauthnstrict=<future timestamp>") {
			t.Errorf("the refusal does not name the ways out: %v", err)
		}
		if persisted := rawdb.ReadChainConfig(db, block.Hash()); !sameChainConfigJSON(t, persisted, documentedTestnetConfig()) {
			t.Error("a refused start changed the stored configuration")
		}
		future := near + 7*24*3600
		if _, _, err := SetupGenesisBlockWithOverride(db, tdb, nil, &ChainOverrides{OverrideWebAuthnStrict: &future}); err == nil || !strings.Contains(err.Error(), "shanghaiTime") {
			t.Errorf("the override on a stand with no shanghaiTime was not refused by the fork order: %v", err)
		}
	})

	// The same stand started *with* the network flag. The schedule is written, the
	// block-numbered settings are applied to blocks already mined — and the start also
	// returns a ConfigCompatError, because the stored config has no shanghaiTime, so
	// NewBlockChain rewinds to before 2025-08-14. For a stand mined in 2026 that is back
	// to genesis. The note on the replacement must not claim no rewind is triggered.
	t.Run("network flag on the documented stand", func(t *testing.T) {
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		block, err := DefaultIncentivTestnetGenesisBlock().Commit(db, tdb)
		if err != nil {
			t.Fatal(err)
		}
		rawdb.WriteChainConfig(db, block.Hash(), documentedTestnetConfig())
		writeSyntheticHead(t, db, 600_000, 1_790_000_000)
		warnings := captureWarnings(t)
		_, _, err = SetupGenesisBlockWithOverride(db, tdb, DefaultIncentivTestnetGenesisBlock(), nil)
		if _, ok := err.(*params.ConfigCompatError); !ok {
			t.Fatalf("the flag on this datadir returned %v, want a ConfigCompatError — the premise of this case", err)
		}
		msgs := warnings()
		if !containsMatch(msgs, "Network flag replaces block-numbered settings") {
			t.Errorf("the replacement was not reported: %q", msgs)
		}
		if containsMatch(msgs, "no rewind is triggered") {
			t.Errorf("the note claims no rewind on a start that does rewind: %q", msgs)
		}
	})

	// A rewind refusal that is not about this fork at all: the stored config never had
	// DPoWTime and the head is past it. webauthnStrictTime is set, so keying the remedy on
	// the resolved value alone would answer "nothing further" while the node sits on a
	// configuration that disagrees with the network about an older fork.
	t.Run("rewind refusal caused by an older fork", func(t *testing.T) {
		stored := *params.IncentivMainnetChainConfig
		stored.DPoWTime = nil
		dpow := *params.IncentivMainnetChainConfig.DPoWTime

		msgs := mustSetup(t, &stored, nil, 6_500_000, dpow+3600, nil)
		if containsMatch(msgs, "nothing further") {
			t.Errorf("a node disagreeing about an older fork was told nothing further is needed: %q", msgs)
		}
		if !containsMatch(msgs, "DPoW fork timestamp") {
			t.Errorf("the line does not name what the two configs disagreed about: %q", msgs)
		}
	})

	// A node that reached this release after activation: everything agrees except the
	// fork time it is missing, and that fork has fired. Adopting would rewind, so it is
	// refused — and the warning has to say that rather than the other thing, because the
	// fix is a restart with the flag rather than a resync from genesis.
	t.Run("late to the release", func(t *testing.T) {
		stored := *params.IncentivMainnetChainConfig
		stored.WebAuthnStrictTime = nil
		activation := *params.IncentivMainnetChainConfig.WebAuthnStrictTime
		msgs, _, err := setup(t, &stored, nil, 6_500_000, activation+3600, nil)
		// Such a node does not start: running it would keep it on the pre-fork rules
		// while the rest of the network has moved on, so the refusal is the error, and
		// the error carries the remedy the warning carries.
		if err == nil {
			t.Fatal("a node late to the release was started on the pre-fork rules")
		}
		if _, ok := err.(*params.ConfigCompatError); ok {
			t.Fatalf("the refusal came back as a rewind: %v", err)
		}
		if !strings.Contains(err.Error(), "restart with the network flag, which does take the schedule") {
			t.Errorf("the error does not offer the remedy that works for this node: %v", err)
		}
		if !containsMatch(msgs, "would rewind the chain") {
			t.Errorf("a node late to the release was not told that a restart with the flag is the fix: %q", msgs)
		}
		if !containsMatch(msgs, "webauthnStrictTime=unset") {
			t.Errorf("the line does not report that the fork is unset on this node: %q", msgs)
		}
		if !containsMatch(msgs, "restart with the network flag, which does take the schedule") {
			t.Errorf("the line does not offer the remedy that works for this node: %q", msgs)
		}
		// The same node with an override ahead of its head starts, on the stored config
		// plus the override and without a rewind: that is the deliberate way to run
		// off the bundled schedule, and the refusal names it.
		if !strings.Contains(err.Error(), "--override.webauthnstrict=<future timestamp>") || !strings.Contains(err.Error(), "cannot turn the fork off") {
			t.Errorf("the refusal does not say what the override can and cannot do: %v", err)
		}
		later := activation + 2*3600
		got, _, err := setupOnce(t, &stored, nil, 6_500_000, activation+3600, &ChainOverrides{OverrideWebAuthnStrict: &later})
		if err != nil {
			t.Fatalf("the same node with an override did not start: %v", err)
		}
		if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != later {
			t.Errorf("webauthnStrictTime = %v, want the override's %d", got.WebAuthnStrictTime, later)
		}
	})

	// A flagless node carried on the override, with the head between the bundled
	// timestamp and the override's. The schedule it would run is the overridden one, so
	// there is nothing to refuse and nothing to warn about — it activates at the override
	// time. Comparing the un-overridden schedule reported a difference the node never had.
	t.Run("no flag with an override past the bundled time", func(t *testing.T) {
		bundled := *params.IncentivMainnetChainConfig.WebAuthnStrictTime
		override := bundled + 14*24*3600

		stored := *params.IncentivMainnetChainConfig
		stored.WebAuthnStrictTime = &override

		msgs := mustSetup(t, &stored, nil, 9_000_000, bundled+3600,
			&ChainOverrides{OverrideWebAuthnStrict: &override})
		if containsMatch(msgs, "will not activate") {
			t.Errorf("a healthy node carried on the override was told it will not activate: %q", msgs)
		}
	})
	// The stored config holds the bundled timestamp, the head is past it, and the
	// operator adds an override that moves the fork later. The bundled schedule is
	// refused — and then the override is applied over the stored config anyway, so the
	// node does activate, at the override's timestamp, after CheckCompatible below
	// rewinds it. Reporting "kept the stored config, so this will not activate" was
	// wrong twice over.
	t.Run("override moves the fork after the head has passed it", func(t *testing.T) {
		bundled := *params.IncentivMainnetChainConfig.WebAuthnStrictTime
		moved := bundled + 14*24*3600

		stored := *params.IncentivMainnetChainConfig

		msgs, _, err := setup(t, &stored, nil, 9_000_000, bundled+3600,
			&ChainOverrides{OverrideWebAuthnStrict: &moved})
		if _, ok := err.(*params.ConfigCompatError); !ok {
			t.Fatalf("moving the fork past a head that has passed it returned %v, want a ConfigCompatError", err)
		}
		if containsMatch(msgs, "will not activate") {
			t.Errorf("a node whose override arms the fork was told it will not activate: %q", msgs)
		}
		if !containsMatch(msgs, fmt.Sprintf("webauthnStrictTime=%d", moved)) {
			t.Errorf("the line does not report the timestamp the node will actually use: %q", msgs)
		}
		if !containsMatch(msgs, "override.webauthnstrict") {
			t.Errorf("the line does not say the override is where that came from: %q", msgs)
		}
		if containsMatch(msgs, "restart with the network flag, which does take the schedule") {
			t.Errorf("the line tells an operator to restart with the network flag, which would discard the override: %q", msgs)
		}
	})

	// A node that was carried on an override, restarted without it. The stored config
	// still holds the override's timestamp, so the bundled schedule is refused, nothing
	// is rewound, and the fork is armed at that timestamp — which is not the fleet's.
	// The line must report it rather than claim the fork is unset, and must not offer the
	// network flag as a repair: on this node the flag would replace that timestamp with
	// the bundled one.
	t.Run("override dropped but still in the stored config", func(t *testing.T) {
		bundled := *params.IncentivMainnetChainConfig.WebAuthnStrictTime
		carried := bundled + 14*24*3600

		stored := *params.IncentivMainnetChainConfig
		stored.WebAuthnStrictTime = &carried

		msgs, got, err := setup(t, &stored, nil, 9_000_000, carried+3600, nil)
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}
		if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != carried {
			t.Fatalf("webauthnStrictTime = %v, want the stored %d", got.WebAuthnStrictTime, carried)
		}
		if containsMatch(msgs, "will not activate") {
			t.Errorf("a node whose stored config arms the fork was told it will not activate: %q", msgs)
		}
		if !containsMatch(msgs, fmt.Sprintf("webauthnStrictTime=%d", carried)) {
			t.Errorf("the line does not report the timestamp the node will actually use: %q", msgs)
		}
		if !containsMatch(msgs, "disagrees") && !containsMatch(msgs, "rewind") {
			t.Errorf("the refusal itself was not reported: %q", msgs)
		}
		if containsMatch(msgs, "restart with the network flag, which does take the schedule") {
			t.Errorf("the line tells an operator to restart with the network flag, which would replace this node's timestamp with the bundled one: %q", msgs)
		}
	})

	// A node restored from an old backup has a head weeks behind the clock. Its refusal is
	// measured from today, not from where its chain stopped: with the activation an hour
	// away by the wall clock, the start is refused although the head is a month short of
	// the window; with the clock also far from the activation, it is the warning.
	t.Run("old backup near the activation", func(t *testing.T) {
		activation := *params.IncentivMainnetChainConfig.WebAuthnStrictTime
		stored := *params.IncentivMainnetChainConfig
		stored.WebAuthnStrictTime = nil
		stored.DynamicMinBaseFeeTime = nil // an older fork the backup never had, so the bundle is refused
		headTime := activation - 31*24*3600

		clockAt := func(t *testing.T, at uint64) {
			t.Helper()
			previous := startupClock
			startupClock = func() time.Time { return time.Unix(int64(at), 0) }
			t.Cleanup(func() { startupClock = previous })
		}

		clockAt(t, activation-3600)
		msgs, got, err := setup(t, &stored, nil, mainnetTestHead, headTime, nil)
		if err == nil {
			t.Fatal("an old backup was started an hour before the activation without the fork")
		}
		if _, ok := err.(*params.ConfigCompatError); ok {
			t.Fatalf("the refusal came back as a rewind: %v", err)
		}
		if got.WebAuthnStrictTime != nil {
			t.Fatal("the bundled schedule was adopted, so this case tests nothing")
		}
		if !containsMatch(msgs, "would rewind the chain") {
			t.Errorf("the refusal was not reported before the start failed: %q", msgs)
		}

		clockAt(t, activation-40*24*3600)
		if _, _, err := setup(t, &stored, nil, mainnetTestHead, headTime, nil); err != nil {
			t.Fatalf("the same backup, with the clock also far from the activation, was refused: %v", err)
		}
	})

	// A node carried on an override, restarted without it before the bundled timestamp:
	// the bundled schedule replaces the stored one, which is the documented behaviour,
	// and the node says so, because the fleet may be waiting on the stored timestamp.
	t.Run("dropped override replaced by the bundled schedule", func(t *testing.T) {
		bundled := *params.IncentivMainnetChainConfig.WebAuthnStrictTime
		carried := bundled + 14*24*3600
		stored := *params.IncentivMainnetChainConfig
		stored.WebAuthnStrictTime = &carried

		msgs, got, err := setup(t, &stored, nil, 9_000_000, bundled-3600, nil)
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}
		if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != bundled {
			t.Fatalf("webauthnStrictTime = %v, want the bundled %d", got.WebAuthnStrictTime, bundled)
		}
		if !containsMatch(msgs, "Stored WebAuthnStrict timestamp replaced") {
			t.Errorf("the replacement was not reported: %q", msgs)
		}
		if !containsMatch(msgs, fmt.Sprintf("stored=%d", carried)) || !containsMatch(msgs, fmt.Sprintf("webauthnStrictTime=%d", bundled)) {
			t.Errorf("the line does not name both timestamps: %q", msgs)
		}

		// With the override still on the command line nothing is replaced.
		msgs, got, err = setup(t, &stored, nil, 9_000_000, bundled-3600, &ChainOverrides{OverrideWebAuthnStrict: &carried})
		if err != nil {
			t.Fatalf("start with the override failed: %v", err)
		}
		if got.WebAuthnStrictTime == nil || *got.WebAuthnStrictTime != carried {
			t.Fatalf("webauthnStrictTime = %v, want the override's %d", got.WebAuthnStrictTime, carried)
		}
		if containsMatch(msgs, "Stored WebAuthnStrict timestamp replaced") {
			t.Errorf("a node still carrying its override was told its timestamp was replaced: %q", msgs)
		}

		// And a stored timestamp that already is the bundled one is not a replacement.
		same := *params.IncentivMainnetChainConfig
		msgs, _, err = setup(t, &same, nil, 9_000_000, bundled-3600, nil)
		if err != nil {
			t.Fatalf("start failed: %v", err)
		}
		if containsMatch(msgs, "Stored WebAuthnStrict timestamp replaced") {
			t.Errorf("a node on the bundled timestamp was told it was replaced: %q", msgs)
		}
	})

	// An override naming a timestamp that has already passed does rewind, on a start with
	// no network flag. That is the operator asking for it explicitly, so it is allowed —
	// but it is the exception to "a start without a network flag never rewinds", and the
	// error has to come back rather than being swallowed by the refusal.
	t.Run("override in the past rewinds without a flag", func(t *testing.T) {
		stored := *params.IncentivMainnetChainConfig
		stored.WebAuthnStrictTime = nil
		passed := *params.IncentivMainnetChainConfig.WebAuthnStrictTime

		_, _, err := setup(t, &stored, nil, 6_500_000, passed+7200,
			&ChainOverrides{OverrideWebAuthnStrict: &passed})
		if _, ok := err.(*params.ConfigCompatError); !ok {
			t.Fatalf("an override naming a timestamp already behind the head returned %v, want a ConfigCompatError", err)
		}
	})
}
