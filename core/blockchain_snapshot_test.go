// Copyright 2020 The go-ethereum Authors
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

// Tests that abnormal program termination (i.e.crash) and restart can recovery
// the snapshot properly if the snapshot is enabled.

package core

import (
	"bytes"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

// snapshotTestBasic wraps the common testing fields in the snapshot tests.
type snapshotTestBasic struct {
	chainBlocks   int    // Number of blocks to generate for the canonical chain
	snapshotBlock uint64 // Block number of the relevant snapshot disk layer
	commitBlock   uint64 // Block number for which to commit the state to disk

	expCanonicalBlocks int    // Number of canonical blocks expected to remain in the database (excl. genesis)
	expHeadHeader      uint64 // Block number of the expected head header
	expHeadFastBlock   uint64 // Block number of the expected head fast sync block
	expHeadBlock       uint64 // Block number of the expected head full block
	expSnapshotBottom  uint64 // The block height corresponding to the snapshot disk layer

	// share fields, set in runtime
	datadir string
	db      ethdb.Database
	genDb   ethdb.Database
	engine  consensus.Engine
	gspec   *Genesis
}

func (basic *snapshotTestBasic) prepare(t *testing.T) (*BlockChain, []*types.Block) {
	// Create a temporary persistent database
	datadir := t.TempDir()

	db, err := rawdb.Open(rawdb.OpenOptions{
		Directory:         datadir,
		AncientsDirectory: datadir,
	})
	if err != nil {
		t.Fatalf("Failed to create persistent database: %v", err)
	}
	// Initialize a fresh chain
	var (
		chainConfig = *params.AllEthashProtocolChanges
		gspec       = &Genesis{
			BaseFee: big.NewInt(params.InitialBaseFee),
			Config:  &chainConfig,
		}
		engine = ethash.NewFullFaker()

		// Snapshot is enabled, the first snapshot is created from the Genesis.
		// The snapshot memory allowance is 256MB, it means no snapshot flush
		// will happen during the block insertion.
		cacheConfig = defaultCacheConfig
	)
	// Disable ZeroRewardBlock fork to avoid zero miner rewards disrupting test expectations
	chainConfig.ZeroRewardBlock = nil
	chain, err := NewBlockChain(db, cacheConfig, gspec, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}
	genDb, blocks, _ := GenerateChainWithGenesis(gspec, engine, basic.chainBlocks, func(i int, b *BlockGen) {})

	// Insert the blocks with configured settings.
	var breakpoints []uint64
	if basic.commitBlock > basic.snapshotBlock {
		breakpoints = append(breakpoints, basic.snapshotBlock, basic.commitBlock)
	} else {
		breakpoints = append(breakpoints, basic.commitBlock, basic.snapshotBlock)
	}
	var startPoint uint64
	for _, point := range breakpoints {
		if _, err := chain.InsertChain(blocks[startPoint:point]); err != nil {
			t.Fatalf("Failed to import canonical chain start: %v", err)
		}
		startPoint = point

		if basic.commitBlock > 0 && basic.commitBlock == point {
			chain.stateCache.TrieDB().Commit(blocks[point-1].Root(), false)
		}
		if basic.snapshotBlock > 0 && basic.snapshotBlock == point {
			// Flushing the entire snap tree into the disk, the
			// relevant (a) snapshot root and (b) snapshot generator
			// will be persisted atomically.
			chain.snaps.Cap(blocks[point-1].Root(), 0)
			diskRoot, blockRoot := chain.snaps.DiskRoot(), blocks[point-1].Root()
			if !bytes.Equal(diskRoot.Bytes(), blockRoot.Bytes()) {
				t.Fatalf("Failed to flush disk layer change, want %x, got %x", blockRoot, diskRoot)
			}
		}
	}
	if _, err := chain.InsertChain(blocks[startPoint:]); err != nil {
		t.Fatalf("Failed to import canonical chain tail: %v", err)
	}

	// Set runtime fields
	basic.datadir = datadir
	basic.db = db
	basic.genDb = genDb
	basic.engine = engine
	basic.gspec = gspec
	return chain, blocks
}

func (basic *snapshotTestBasic) verify(t *testing.T, chain *BlockChain, blocks []*types.Block) {
	// Iterate over all the remaining blocks and ensure there are no gaps
	verifyNoGaps(t, chain, true, blocks)
	verifyCutoff(t, chain, true, blocks, basic.expCanonicalBlocks)

	if head := chain.CurrentHeader(); head.Number.Uint64() != basic.expHeadHeader {
		t.Errorf("Head header mismatch: have %d, want %d", head.Number, basic.expHeadHeader)
	}
	if head := chain.CurrentSnapBlock(); head.Number.Uint64() != basic.expHeadFastBlock {
		t.Errorf("Head fast block mismatch: have %d, want %d", head.Number, basic.expHeadFastBlock)
	}
	if head := chain.CurrentBlock(); head.Number.Uint64() != basic.expHeadBlock {
		t.Errorf("Head block mismatch: have %d, want %d", head.Number, basic.expHeadBlock)
	}

	// Check the disk layer, ensure they are matched
	block := chain.GetBlockByNumber(basic.expSnapshotBottom)
	if block == nil {
		t.Errorf("The corresponding block[%d] of snapshot disk layer is missing", basic.expSnapshotBottom)
	} else if !bytes.Equal(chain.snaps.DiskRoot().Bytes(), block.Root().Bytes()) {
		t.Errorf("The snapshot disk layer root is incorrect, want %x, get %x", block.Root(), chain.snaps.DiskRoot())
	}

	// Check the snapshot, ensure it's integrated
	if err := chain.snaps.Verify(block.Root()); err != nil {
		t.Errorf("The disk layer is not integrated %v", err)
	}
}

//nolint:unused
func (basic *snapshotTestBasic) dump() string {
	buffer := new(strings.Builder)

	fmt.Fprint(buffer, "Chain:\n  G")
	for i := 0; i < basic.chainBlocks; i++ {
		fmt.Fprintf(buffer, "->C%d", i+1)
	}
	fmt.Fprint(buffer, " (HEAD)\n\n")

	fmt.Fprintf(buffer, "Commit:   G")
	if basic.commitBlock > 0 {
		fmt.Fprintf(buffer, ", C%d", basic.commitBlock)
	}
	fmt.Fprint(buffer, "\n")

	fmt.Fprintf(buffer, "Snapshot: G")
	if basic.snapshotBlock > 0 {
		fmt.Fprintf(buffer, ", C%d", basic.snapshotBlock)
	}
	fmt.Fprint(buffer, "\n")

	//if crash {
	//	fmt.Fprintf(buffer, "\nCRASH\n\n")
	//} else {
	//	fmt.Fprintf(buffer, "\nSetHead(%d)\n\n", basic.setHead)
	//}
	fmt.Fprintf(buffer, "------------------------------\n\n")

	fmt.Fprint(buffer, "Expected in leveldb:\n  G")
	for i := 0; i < basic.expCanonicalBlocks; i++ {
		fmt.Fprintf(buffer, "->C%d", i+1)
	}
	fmt.Fprintf(buffer, "\n\n")
	fmt.Fprintf(buffer, "Expected head header    : C%d\n", basic.expHeadHeader)
	fmt.Fprintf(buffer, "Expected head fast block: C%d\n", basic.expHeadFastBlock)
	if basic.expHeadBlock == 0 {
		fmt.Fprintf(buffer, "Expected head block     : G\n")
	} else {
		fmt.Fprintf(buffer, "Expected head block     : C%d\n", basic.expHeadBlock)
	}
	if basic.expSnapshotBottom == 0 {
		fmt.Fprintf(buffer, "Expected snapshot disk  : G\n")
	} else {
		fmt.Fprintf(buffer, "Expected snapshot disk  : C%d\n", basic.expSnapshotBottom)
	}
	return buffer.String()
}

func (basic *snapshotTestBasic) teardown() {
	basic.db.Close()
	basic.genDb.Close()
	os.RemoveAll(basic.datadir)
}

// snapshotTest is a test case type for normal snapshot recovery.
// It can be used for testing that restart Geth normally.
type snapshotTest struct {
	snapshotTestBasic
}

func (snaptest *snapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Restart the chain normally
	chain.Stop()
	newchain, err := NewBlockChain(snaptest.db, nil, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()

	snaptest.verify(t, newchain, blocks)
}

// crashSnapshotTest is a test case type for innormal snapshot recovery.
// It can be used for testing that restart Geth after the crash.
type crashSnapshotTest struct {
	snapshotTestBasic
}

func (snaptest *crashSnapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Pull the plug on the database, simulating a hard crash
	db := chain.db
	db.Close()
	chain.stopWithoutSaving()

	// Start a new blockchain back up and see where the repair leads us
	newdb, err := rawdb.Open(rawdb.OpenOptions{
		Directory:         snaptest.datadir,
		AncientsDirectory: snaptest.datadir,
	})

	if err != nil {
		t.Fatalf("Failed to reopen persistent database: %v", err)
	}
	defer newdb.Close()

	// The interesting thing is: instead of starting the blockchain after
	// the crash, we do restart twice here: one after the crash and one
	// after the normal stop. It's used to ensure the broken snapshot
	// can be detected all the time.
	newchain, err := NewBlockChain(newdb, nil, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	newchain.Stop()

	newchain, err = NewBlockChain(newdb, nil, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()

	snaptest.verify(t, newchain, blocks)
}

// gappedSnapshotTest is a test type used to test this scenario:
// - have a complete snapshot
// - restart without enabling the snapshot
// - insert a few blocks
// - restart with enabling the snapshot again
type gappedSnapshotTest struct {
	snapshotTestBasic
	gapped int // Number of blocks to insert without enabling snapshot
}

func (snaptest *gappedSnapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Insert blocks without enabling snapshot if gapping is required.
	chain.Stop()
	gappedBlocks, _ := GenerateChain(snaptest.gspec.Config, blocks[len(blocks)-1], snaptest.engine, snaptest.genDb, snaptest.gapped, func(i int, b *BlockGen) {})

	// Insert a few more blocks without enabling snapshot
	var cacheConfig = &CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		TrieTimeLimit:  5 * time.Minute,
		SnapshotLimit:  0,
	}
	newchain, err := NewBlockChain(snaptest.db, cacheConfig, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	newchain.InsertChain(gappedBlocks)
	newchain.Stop()

	// Restart the chain with enabling the snapshot
	newchain, err = NewBlockChain(snaptest.db, nil, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()

	snaptest.verify(t, newchain, blocks)
}

// setHeadSnapshotTest is the test type used to test this scenario:
// - have a complete snapshot
// - set the head to a lower point
// - restart
type setHeadSnapshotTest struct {
	snapshotTestBasic
	setHead uint64 // Block number to set head back to
}

func (snaptest *setHeadSnapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Rewind the chain if setHead operation is required.
	chain.SetHead(snaptest.setHead)
	chain.Stop()

	newchain, err := NewBlockChain(snaptest.db, nil, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()

	snaptest.verify(t, newchain, blocks)
}

// wipeCrashSnapshotTest is the test type used to test this scenario:
// - have a complete snapshot
// - restart, insert more blocks without enabling the snapshot
// - restart again with enabling the snapshot
// - crash
type wipeCrashSnapshotTest struct {
	snapshotTestBasic
	newBlocks int
}

func (snaptest *wipeCrashSnapshotTest) test(t *testing.T) {
	// It's hard to follow the test case, visualize the input
	// log.Root().SetHandler(log.LvlFilterHandler(log.LvlTrace, log.StreamHandler(os.Stderr, log.TerminalFormat(true))))
	// fmt.Println(tt.dump())
	chain, blocks := snaptest.prepare(t)

	// Firstly, stop the chain properly, with all snapshot journal
	// and state committed.
	chain.Stop()

	config := &CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		TrieTimeLimit:  5 * time.Minute,
		SnapshotLimit:  0,
	}
	newchain, err := NewBlockChain(snaptest.db, config, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	newBlocks, _ := GenerateChain(snaptest.gspec.Config, blocks[len(blocks)-1], snaptest.engine, snaptest.genDb, snaptest.newBlocks, func(i int, b *BlockGen) {})
	newchain.InsertChain(newBlocks)
	newchain.Stop()

	// Restart the chain, the wiper should starts working
	config = &CacheConfig{
		TrieCleanLimit: 256,
		TrieDirtyLimit: 256,
		TrieTimeLimit:  5 * time.Minute,
		SnapshotLimit:  256,
		SnapshotWait:   false, // Don't wait rebuild
	}
	tmp, err := NewBlockChain(snaptest.db, config, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}

	// Simulate the blockchain crash.
	tmp.stopWithoutSaving()

	newchain, err = NewBlockChain(snaptest.db, nil, snaptest.gspec, nil, snaptest.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	defer newchain.Stop()
	snaptest.verify(t, newchain, blocks)
}

// Tests a Geth restart with valid snapshot. Before the shutdown, all snapshot
// journal will be persisted correctly. In this case no snapshot recovery is
// required.
func TestRestartWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit:   G
	// Snapshot: G
	//
	// SetHead(0)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head header    : C8
	// Expected head fast block: C8
	// Expected head block     : C8
	// Expected snapshot disk  : G
	test := &snapshotTest{
		snapshotTestBasic{
			chainBlocks:        8,
			snapshotBlock:      0,
			commitBlock:        0,
			expCanonicalBlocks: 8,
			expHeadHeader:      8,
			expHeadFastBlock:   8,
			expHeadBlock:       8,
			expSnapshotBottom:  0, // Initial disk layer built from genesis
		},
	}
	test.test(t)
	test.teardown()
}

// Tests a Geth was crashed and restarts with a broken snapshot. In this case the
// chain head should be rewound to the point with available state. And also the
// new head should must be lower than disk layer. But there is no committed point
// so the chain should be rewound to genesis and the disk layer should be left
// for recovery.
func TestNoCommitCrashWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit:   G
	// Snapshot: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head header    : C8
	// Expected head fast block: C8
	// Expected head block     : G
	// Expected snapshot disk  : C4
	test := &crashSnapshotTest{
		snapshotTestBasic{
			chainBlocks:        8,
			snapshotBlock:      4,
			commitBlock:        0,
			expCanonicalBlocks: 8,
			expHeadHeader:      8,
			expHeadFastBlock:   8,
			expHeadBlock:       0,
			expSnapshotBottom:  4, // Last committed disk layer, wait recovery
		},
	}
	test.test(t)
	test.teardown()
}

// Tests a Geth was crashed and restarts with a broken snapshot. In this case the
// chain head should be rewound to the point with available state. And also the
// new head should must be lower than disk layer. But there is only a low committed
// point so the chain should be rewound to committed point and the disk layer
// should be left for recovery.
func TestLowCommitCrashWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit:   G, C2
	// Snapshot: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head header    : C8
	// Expected head fast block: C8
	// Expected head block     : C2
	// Expected snapshot disk  : C4
	test := &crashSnapshotTest{
		snapshotTestBasic{
			chainBlocks:        8,
			snapshotBlock:      4,
			commitBlock:        2,
			expCanonicalBlocks: 8,
			expHeadHeader:      8,
			expHeadFastBlock:   8,
			expHeadBlock:       2,
			expSnapshotBottom:  4, // Last committed disk layer, wait recovery
		},
	}
	test.test(t)
	test.teardown()
}

// Tests a Geth was crashed and restarts with a broken snapshot. In this case
// the chain head should be rewound to the point with available state. And also
// the new head should must be lower than disk layer. But there is only a high
// committed point so the chain should be rewound to genesis and the disk layer
// should be left for recovery.
func TestHighCommitCrashWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit:   G, C6
	// Snapshot: G, C4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8
	//
	// Expected head header    : C8
	// Expected head fast block: C8
	// Expected head block     : G
	// Expected snapshot disk  : C4
	test := &crashSnapshotTest{
		snapshotTestBasic{
			chainBlocks:        8,
			snapshotBlock:      4,
			commitBlock:        6,
			expCanonicalBlocks: 8,
			expHeadHeader:      8,
			expHeadFastBlock:   8,
			expHeadBlock:       0,
			expSnapshotBottom:  4, // Last committed disk layer, wait recovery
		},
	}
	test.test(t)
	test.teardown()
}

// Tests a Geth was running with snapshot enabled. Then restarts without
// enabling snapshot and after that re-enable the snapshot again. In this
// case the snapshot should be rebuilt with latest chain head.
func TestGappedNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit:   G
	// Snapshot: G
	//
	// SetHead(0)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head header    : C10
	// Expected head fast block: C10
	// Expected head block     : C10
	// Expected snapshot disk  : C10
	test := &gappedSnapshotTest{
		snapshotTestBasic: snapshotTestBasic{
			chainBlocks:        8,
			snapshotBlock:      0,
			commitBlock:        0,
			expCanonicalBlocks: 10,
			expHeadHeader:      10,
			expHeadFastBlock:   10,
			expHeadBlock:       10,
			expSnapshotBottom:  10, // Rebuilt snapshot from the latest HEAD
		},
		gapped: 2,
	}
	test.test(t)
	test.teardown()
}

// Tests the Geth was running with snapshot enabled and resetHead is applied.
// In this case the head is rewound to the target(with state available). After
// that the chain is restarted and the original disk layer is kept.
func TestSetHeadWithNewSnapshot(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit:   G
	// Snapshot: G
	//
	// SetHead(4)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C4
	// Expected snapshot disk  : G
	test := &setHeadSnapshotTest{
		snapshotTestBasic: snapshotTestBasic{
			chainBlocks:        8,
			snapshotBlock:      0,
			commitBlock:        0,
			expCanonicalBlocks: 4,
			expHeadHeader:      4,
			expHeadFastBlock:   4,
			expHeadBlock:       4,
			expSnapshotBottom:  0, // The initial disk layer is built from the genesis
		},
		setHead: 4,
	}
	test.test(t)
	test.teardown()
}

// Tests the Geth was running with a complete snapshot and then imports a few
// more new blocks on top without enabling the snapshot. After the restart,
// crash happens. Check everything is ok after the restart.
func TestRecoverSnapshotFromWipingCrash(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8 (HEAD)
	//
	// Commit:   G
	// Snapshot: G
	//
	// SetHead(0)
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head header    : C10
	// Expected head fast block: C10
	// Expected head block     : C8
	// Expected snapshot disk  : C10
	test := &wipeCrashSnapshotTest{
		snapshotTestBasic: snapshotTestBasic{
			chainBlocks:        8,
			snapshotBlock:      4,
			commitBlock:        0,
			expCanonicalBlocks: 10,
			expHeadHeader:      10,
			expHeadFastBlock:   10,
			expHeadBlock:       10,
			expSnapshotBottom:  10,
		},
		newBlocks: 2,
	}
	test.test(t)
	test.teardown()
}

// offChainTest wraps the chains shared by the tests where the snapshot disk layer
// is not an ancestor of the head. Every block gets a state root of its own and the
// fake engine skips the seal, so a test runs in a fraction of a second.
type offChainTest struct {
	gspec  *Genesis
	engine *ethash.Ethash
	genDb  ethdb.Database
	canon  []*types.Block // G->C1->...->C10, the heavy chain
}

func newOffChainTest(t *testing.T) *offChainTest {
	t.Helper()

	chainConfig := *params.AllEthashProtocolChanges
	// Disable ZeroRewardBlock fork to avoid zero miner rewards disrupting test expectations
	chainConfig.ZeroRewardBlock = nil

	test := &offChainTest{
		gspec: &Genesis{
			BaseFee: big.NewInt(params.InitialBaseFee),
			Config:  &chainConfig,
		},
		engine: ethash.NewFullFaker(),
	}
	test.genDb, test.canon, _ = GenerateChainWithGenesis(test.gspec, test.engine, 10, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{0x02})
		b.SetDifficulty(big.NewInt(1000000))
	})
	t.Cleanup(func() {
		test.engine.Close()
		test.genDb.Close()
	})
	return test
}

// branch returns blocks on top of the given canonical block, with a coinbase and a
// difficulty of their own so that they differ from the canonical chain.
func (test *offChainTest) branch(parent *types.Block, n int, coinbase byte, difficulty int64) []*types.Block {
	blocks, _ := GenerateChain(test.gspec.Config, parent, test.engine, test.genDb, n, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{coinbase})
		b.SetDifficulty(big.NewInt(difficulty))
	})
	return blocks
}

// light returns blocks on top of the given canonical block that never beat the
// canonical chain.
func (test *offChainTest) light(parent *types.Block, n int) []*types.Block {
	return test.branch(parent, n, 0x01, 1)
}

// heavy returns blocks on top of the given canonical block that beat the whole
// canonical chain.
func (test *offChainTest) heavy(parent *types.Block, n int) []*types.Block {
	return test.branch(parent, n, 0x03, 2000000)
}

// start opens a blockchain on top of the database, the way a node restart does.
func (test *offChainTest) start(t *testing.T, db ethdb.Database) *BlockChain {
	t.Helper()

	chain, err := NewBlockChain(db, defaultCacheConfig, test.gspec, nil, test.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create chain: %v", err)
	}
	return chain
}

// prepareSideChain imports the canonical chain with the state of the given block
// committed to disk, then the side chain, and flushes the snapshot tree into a disk
// layer on the last side block. The limit of zero puts the disk layer where the
// memory limit would, without the weight of the blocks it takes to get there.
func (test *offChainTest) prepareSideChain(t *testing.T, db ethdb.Database, commitBlock int) *BlockChain {
	t.Helper()

	side := test.light(test.canon[1], 4)

	chain := test.start(t, db)
	if _, err := chain.InsertChain(test.canon[:commitBlock]); err != nil {
		t.Fatalf("Failed to import canonical chain start: %v", err)
	}
	if err := chain.stateCache.TrieDB().Commit(test.canon[commitBlock-1].Root(), false); err != nil {
		t.Fatalf("Failed to commit state: %v", err)
	}
	if _, err := chain.InsertChain(test.canon[commitBlock:]); err != nil {
		t.Fatalf("Failed to import canonical chain tail: %v", err)
	}
	if _, err := chain.InsertChain(side); err != nil {
		t.Fatalf("Failed to import side chain: %v", err)
	}
	if head := chain.CurrentBlock(); head.Hash() != test.canon[len(test.canon)-1].Hash() {
		t.Fatalf("Side chain took over, head block %d", head.Number)
	}
	diskRoot := side[len(side)-1].Root()
	if err := chain.snaps.Cap(diskRoot, 0); err != nil {
		t.Fatalf("Failed to flatten snapshots: %v", err)
	}
	if root := rawdb.ReadSnapshotRoot(db); root != diskRoot {
		t.Fatalf("Failed to flush disk layer change, want %x, got %x", diskRoot, root)
	}
	return chain
}

// crashAboveHead imports the canonical chain with the state of C2 committed to disk
// and the snapshot disk layer on C6, then pulls the plug. The head of the next start
// has to go below the disk layer, to C2.
func (test *offChainTest) crashAboveHead(t *testing.T, db ethdb.Database) {
	t.Helper()

	chain := test.start(t, db)
	if _, err := chain.InsertChain(test.canon[:2]); err != nil {
		t.Fatalf("Failed to import canonical chain start: %v", err)
	}
	if err := chain.stateCache.TrieDB().Commit(test.canon[1].Root(), false); err != nil {
		t.Fatalf("Failed to commit state: %v", err)
	}
	if _, err := chain.InsertChain(test.canon[2:6]); err != nil {
		t.Fatalf("Failed to import canonical chain middle: %v", err)
	}
	if err := chain.snaps.Cap(test.canon[5].Root(), 0); err != nil {
		t.Fatalf("Failed to flatten snapshots: %v", err)
	}
	if _, err := chain.InsertChain(test.canon[6:]); err != nil {
		t.Fatalf("Failed to import canonical chain tail: %v", err)
	}
	// Pull the plug on the database, simulating a hard crash
	chain.stopWithoutSaving()
}

// startRecovery restarts after crashAboveHead and checks that the repair went below
// the disk layer and left the snapshot in recovery mode.
func (test *offChainTest) startRecovery(t *testing.T, db ethdb.Database) *BlockChain {
	t.Helper()

	chain := test.start(t, db)
	if head := chain.CurrentBlock(); head.Number.Uint64() != 2 {
		t.Fatalf("Head block mismatch after the first crash: have %d, want %d", head.Number, 2)
	}
	if number := rawdb.ReadSnapshotRecoveryNumber(db); number == nil || *number != 6 {
		t.Fatalf("Recovery number mismatch after the first crash: have %v, want %d", number, 6)
	}
	return chain
}

// verifyHeads checks that the canonical chain has no gaps, and the numbers of the head
// header, which is also the head fast block, and of the head block. It does not look up
// a block by number, so a chain that the repair wiped is reported instead of crashing
// the test, as snapshotTestBasic.verify does.
func verifyHeads(t *testing.T, chain *BlockChain, blocks []*types.Block, header, block uint64) {
	t.Helper()

	verifyNoGaps(t, chain, true, blocks)
	verifyCutoff(t, chain, true, blocks, int(header))

	if head := chain.CurrentHeader(); head.Number.Uint64() != header {
		t.Errorf("Head header mismatch: have %d, want %d", head.Number, header)
	}
	if head := chain.CurrentSnapBlock(); head.Number.Uint64() != header {
		t.Errorf("Head fast block mismatch: have %d, want %d", head.Number, header)
	}
	if head := chain.CurrentBlock(); head.Number.Uint64() != block {
		t.Errorf("Head block mismatch: have %d, want %d", head.Number, block)
	}
}

// verifyDisk checks that the snapshot was rebuilt on the given block: no recovery
// number is left, and the disk layer is on its root and integrated.
func verifyDisk(t *testing.T, chain *BlockChain, block *types.Block) {
	t.Helper()

	if number := rawdb.ReadSnapshotRecoveryNumber(chain.db); number != nil {
		t.Errorf("Recovery number mismatch: have %d, want none", *number)
	}
	if root := chain.snaps.DiskRoot(); root != block.Root() {
		t.Errorf("The snapshot disk layer root is incorrect, want %x, get %x", block.Root(), root)
	}
	if err := chain.snaps.Verify(block.Root()); err != nil {
		t.Errorf("The disk layer is not integrated %v", err)
	}
}

// verifyReplay imports the blocks and checks that the snapshot goes on with them.
func verifyReplay(t *testing.T, chain *BlockChain, blocks []*types.Block) {
	t.Helper()

	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to replay the chain: %v", err)
	}
	root := blocks[len(blocks)-1].Root()
	if chain.snaps.Snapshot(root) == nil {
		t.Fatalf("The snapshot of the head is missing")
	}
	if err := chain.snaps.Verify(root); err != nil {
		t.Errorf("The snapshot is not integrated after the replay: %v", err)
	}
}

// verifyRebuilt checks that the repair left the head on the given block with the
// snapshot rebuilt there, and that the snapshot goes on with the replay.
func verifyRebuilt(t *testing.T, chain *BlockChain, head *types.Block, replay []*types.Block) {
	t.Helper()

	if have := chain.CurrentBlock(); have.Hash() != head.Hash() {
		t.Errorf("Head block mismatch: have %d, want %d", have.Number, head.Number())
	}
	verifyDisk(t, chain, head)
	verifyReplay(t, chain, replay)
}

// Tests a Geth was crashed with the snapshot disk layer on a side chain. The disk
// layer follows whichever block ran last, so a light side chain can move it off the
// canonical chain. The disk root is not an ancestor of the head, so the repair can
// never pass it. The chain should be rewound to the last state on disk and the
// snapshot rebuilt from there, instead of looking for the root all the way down to
// genesis.
func TestOffChainDiskLayerSideChain(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10 (HEAD)
	//             \->S1->S2->S3->S4
	//
	// Commit:   G, C2
	// Snapshot: G, S4
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head header    : C10
	// Expected head fast block: C10
	// Expected head block     : C2
	// Expected snapshot disk  : C2
	test := newOffChainTest(t)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	// Pull the plug on the database, simulating a hard crash
	test.prepareSideChain(t, db, 2).stopWithoutSaving()

	// Start a new blockchain back up and see where the repair leads us
	chain := test.start(t, db)
	defer chain.Stop()

	verifyHeads(t, chain, test.canon, 10, 2)
	verifyDisk(t, chain, test.canon[1])
}

// Tests the same crash as above on a node with an ancient store, where the nearest
// state on the head's chain is above the freezer. The repair must stop there and
// keep the chain. Without that it walks down to genesis, and a head below the
// frozen blocks makes it wipe all of them, so the node has to sync again.
func TestOffChainDiskLayerSideChainWithAncients(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10 (HEAD)
	//             \->S1->S2->S3->S4
	//
	// Commit:   G, C7
	// Snapshot: G, S4
	// Freezer:  G->C1->C2->C3->C4->C5
	//
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head header    : C10
	// Expected head fast block: C10
	// Expected head block     : C7
	// Expected snapshot disk  : C7
	test := newOffChainTest(t)
	datadir := t.TempDir()

	db, err := rawdb.Open(rawdb.OpenOptions{
		Directory:         datadir,
		AncientsDirectory: datadir,
	})
	if err != nil {
		t.Fatalf("Failed to create persistent database: %v", err)
	}
	defer db.Close() // Might double close, should be fine

	chain := test.prepareSideChain(t, db, 7)

	// Force run a freeze cycle
	type freezer interface {
		Freeze(threshold uint64) error
		Ancients() (uint64, error)
	}
	if err := db.(freezer).Freeze(5); err != nil {
		t.Fatalf("Failed to freeze: %v", err)
	}
	if frozen, err := db.(freezer).Ancients(); err != nil || frozen != 6 {
		t.Fatalf("Failed to freeze, have %d blocks and error %v", frozen, err)
	}
	// Pull the plug on the database, simulating a hard crash
	db.Close()
	chain.stopWithoutSaving()

	// Start a new blockchain back up and see where the repair leads us
	db, err = rawdb.Open(rawdb.OpenOptions{
		Directory:         datadir,
		AncientsDirectory: datadir,
	})
	if err != nil {
		t.Fatalf("Failed to reopen persistent database: %v", err)
	}
	defer db.Close()

	chain = test.start(t, db)
	defer chain.Stop()

	verifyHeads(t, chain, test.canon, 10, 7)
	verifyDisk(t, chain, test.canon[6])

	if frozen, err := db.(freezer).Ancients(); err != nil {
		t.Errorf("Failed to retrieve ancient count: %v\n", err)
	} else if frozen != 6 {
		t.Errorf("Frozen block count mismatch: have %d, want %d", frozen, 6)
	}
}

// Tests a Geth was crashed twice in a row. The first crash rewinds the head below
// the snapshot disk layer and leaves the snapshot in recovery mode. The blocks that
// are imported afterwards stay below the disk layer, so the second crash finds the
// disk root above the head, in a block the head does not descend from. The repair
// should go back to the last state on disk again, keep the disk layer and the
// recovery number, and let the snapshot resume once the replay reaches the layer.
func TestOffChainDiskLayerSecondCrash(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10 (HEAD)
	//
	// Commit:   G, C2
	// Snapshot: G, C6
	//
	// CRASH
	// Import C3->C4
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head header    : C4
	// Expected head fast block: C4
	// Expected head block     : C2
	// Expected snapshot disk  : C6
	test := newOffChainTest(t)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	test.crashAboveHead(t, db)

	// The first repair goes below the disk layer and starts the recovery
	chain := test.startRecovery(t, db)
	if _, err := chain.InsertChain(test.canon[2:4]); err != nil {
		t.Fatalf("Failed to import canonical chain after the first crash: %v", err)
	}
	chain.stopWithoutSaving()

	chain = test.start(t, db)
	defer chain.Stop()

	// The replay moved the head header to C4, and the stored blocks above it stay
	verifyHeads(t, chain, test.canon[:4], 4, 2)
	if root := rawdb.ReadSnapshotRoot(db); root != test.canon[5].Root() {
		t.Errorf("The snapshot disk layer root is incorrect, want %x, get %x", test.canon[5].Root(), root)
	}
	if number := rawdb.ReadSnapshotRecoveryNumber(db); number == nil || *number != 6 {
		t.Errorf("Recovery number mismatch: have %v, want %d", number, 6)
	}
	// The replay reaches the disk layer and the snapshot goes on from there
	verifyReplay(t, chain, test.canon)
}

// Tests a Geth was crashed, restarted into the snapshot recovery, reorged onto a
// heavier chain that does not have the disk layer, and crashed again. The recovery
// number of the first crash points at a block the canonical chain no longer has, so
// it must not keep the abandoned disk layer. The snapshot should be rebuilt on the
// last state on disk and be there for the new chain.
func TestOffChainDiskLayerReorgAfterCrash(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//             \->F1->F2->F3->F4->F5->F6 (HEAD)
	//
	// Commit:   G, C2
	// Snapshot: G, C6
	//
	// CRASH
	// Import F1->F2->F3->F4->F5->F6
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->F1->F2->F3->F4->F5->F6
	//
	// Expected head header    : F6
	// Expected head fast block: F6
	// Expected head block     : C2
	// Expected snapshot disk  : C2
	test := newOffChainTest(t)
	fork := test.heavy(test.canon[1], 6)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	test.crashAboveHead(t, db)

	// The first repair goes below the disk layer and starts the recovery, then the
	// heavier chain takes over from C2
	chain := test.startRecovery(t, db)
	if _, err := chain.InsertChain(fork); err != nil {
		t.Fatalf("Failed to import the fork: %v", err)
	}
	if head := chain.CurrentBlock(); head.Hash() != fork[len(fork)-1].Hash() {
		t.Fatalf("The fork did not take over, head block %d", head.Number)
	}
	chain.stopWithoutSaving()

	chain = test.start(t, db)
	defer chain.Stop()

	if head := chain.CurrentHeader(); head.Hash() != fork[len(fork)-1].Hash() {
		t.Errorf("Head header mismatch: have %d, want %d", head.Number, fork[len(fork)-1].Number())
	}
	// The replay of the fork has a snapshot to build on
	verifyRebuilt(t, chain, test.canon[1], fork)
}

// Tests a Geth was crashed, recovered, and crashed again with the snapshot disk layer
// on a side chain. The recovery number of the first crash stays in the database once
// the replay passed the disk layer. It must not keep the side chain layer after the
// second crash. The snapshot should be rebuilt on the last state on disk and be there
// for the canonical chain.
func TestOffChainDiskLayerSideChainAfterRecovery(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10 (HEAD)
	//                             \->L1->L2->L3->L4
	//
	// Commit:   G, C2
	// Snapshot: G, C6, L4
	//
	// CRASH
	// Import C3->C4->C5->C6->C7->C8->C9->C10
	// Import L1->L2->L3->L4
	// CRASH
	//
	// ------------------------------
	//
	// Expected in leveldb:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Expected head header    : C10
	// Expected head fast block: C10
	// Expected head block     : C2
	// Expected snapshot disk  : C2
	test := newOffChainTest(t)
	late := test.light(test.canon[5], 4)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	test.crashAboveHead(t, db)

	// The first repair goes below the disk layer and starts the recovery. The replay
	// passes the disk layer, and a side chain moves it away from the canonical chain
	chain := test.startRecovery(t, db)
	if _, err := chain.InsertChain(test.canon); err != nil {
		t.Fatalf("Failed to replay the canonical chain: %v", err)
	}
	if _, err := chain.InsertChain(late); err != nil {
		t.Fatalf("Failed to import side chain: %v", err)
	}
	if err := chain.snaps.Cap(late[len(late)-1].Root(), 0); err != nil {
		t.Fatalf("Failed to flatten snapshots: %v", err)
	}
	if root := rawdb.ReadSnapshotRoot(db); root != late[len(late)-1].Root() {
		t.Fatalf("Failed to flush disk layer change, want %x, got %x", late[len(late)-1].Root(), root)
	}
	chain.stopWithoutSaving()

	chain = test.start(t, db)
	defer chain.Stop()

	// The replay of the canonical chain has a snapshot to build on
	verifyRebuilt(t, chain, test.canon[1], test.canon)
}

// Tests a Geth was crashed, restarted into the snapshot recovery, reorged onto a
// heavier chain up to the height of the recovery number, and stopped cleanly. The
// state of the head is on disk, so the restart has nothing to repair, but the recovery
// number still points at the abandoned disk layer and must not keep it. The snapshot
// should be rebuilt on the head and be there for the chain that follows.
func TestOffChainDiskLayerReorgThenRestart(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//             \->F1->F2->F3->F4 (HEAD)
	//
	// Commit:   G, C2, F4
	// Snapshot: G, C6
	//
	// CRASH
	// Import F1->F2->F3->F4
	// STOP
	//
	// ------------------------------
	//
	// Expected head block     : F4
	// Expected snapshot disk  : F4
	test := newOffChainTest(t)
	fork := test.heavy(test.canon[1], 6)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	test.crashAboveHead(t, db)

	chain := test.startRecovery(t, db)
	if _, err := chain.InsertChain(fork[:4]); err != nil {
		t.Fatalf("Failed to import the fork: %v", err)
	}
	chain.Stop()

	chain = test.start(t, db)
	defer chain.Stop()

	// The state of the head was on disk, so the head is where the clean stop left it
	if head := chain.CurrentBlock(); head.Hash() != fork[3].Hash() {
		t.Fatalf("Head block mismatch: have %d, want %d", head.Number, fork[3].Number())
	}
	// The rest of the fork has a snapshot to build on
	verifyRebuilt(t, chain, fork[3], fork)
}

// Tests a Geth was crashed, restarted into the snapshot recovery, and crashed again
// after only the first blocks of a heavier chain. A chain that extends the head
// overwrites the canonical index one height at a time, so above the new blocks the
// index still holds the abandoned chain, with the disk layer at the height of the
// recovery number. That entry does not lead back to the head, so the recovery number
// must not keep the abandoned disk layer.
func TestOffChainDiskLayerPartialReorg(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//             \->F1->F2 (HEAD)
	//
	// Commit:   G, C2
	// Snapshot: G, C6
	//
	// CRASH
	// Import F1->F2
	// CRASH
	//
	// ------------------------------
	//
	// Expected head block     : C2
	// Expected snapshot disk  : C2
	test := newOffChainTest(t)
	fork := test.heavy(test.canon[1], 6)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	test.crashAboveHead(t, db)

	chain := test.startRecovery(t, db)
	if _, err := chain.InsertChain(fork[:2]); err != nil {
		t.Fatalf("Failed to import the start of the fork: %v", err)
	}
	// The index above the new blocks still holds the abandoned chain
	if hash := rawdb.ReadCanonicalHash(db, 6); hash != test.canon[5].Hash() {
		t.Fatalf("Canonical index mismatch at 6: have %x, want %x", hash, test.canon[5].Hash())
	}
	chain.stopWithoutSaving()

	chain = test.start(t, db)
	defer chain.Stop()

	// The replay of the fork has a snapshot to build on
	verifyRebuilt(t, chain, test.canon[1], fork)
}

// Tests a Geth was crashed, restarted into the snapshot recovery, and stopped cleanly
// below the disk layer. The state of the head is on disk, so the restart has nothing
// to repair, and the recovery number still leads from the head to the disk layer. The
// snapshot should stay in recovery and resume once the replay reaches the layer.
func TestOffChainDiskLayerRestartInRecovery(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//
	// Commit:   G, C2, C4
	// Snapshot: G, C6
	//
	// CRASH
	// Import C3->C4
	// STOP
	//
	// ------------------------------
	//
	// Expected head block     : C4
	// Expected snapshot disk  : C6
	test := newOffChainTest(t)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	test.crashAboveHead(t, db)

	chain := test.startRecovery(t, db)
	if _, err := chain.InsertChain(test.canon[2:4]); err != nil {
		t.Fatalf("Failed to import canonical chain after the first crash: %v", err)
	}
	chain.Stop()

	chain = test.start(t, db)
	defer chain.Stop()

	if head := chain.CurrentBlock(); head.Number.Uint64() != 4 {
		t.Fatalf("Head block mismatch: have %d, want %d", head.Number, 4)
	}
	if number := rawdb.ReadSnapshotRecoveryNumber(db); number == nil || *number != 6 {
		t.Errorf("Recovery number mismatch: have %v, want %d", number, 6)
	}
	if root := chain.snaps.DiskRoot(); root != test.canon[5].Root() {
		t.Errorf("The snapshot disk layer root is incorrect, want %x, get %x", test.canon[5].Root(), root)
	}
	// The replay reaches the disk layer and the snapshot goes on from there
	verifyReplay(t, chain, test.canon)
}

// Tests the database of a stopped Geth is opened read-only, as geth export does, while
// it holds a recovery number left from an abandoned disk layer. The snapshot is neither
// built nor wiped on such an open and a write would end the process, so the stale number
// has to be left alone.
func TestOffChainDiskLayerReadOnly(t *testing.T) {
	// Chain:
	//   G->C1->C2->C3->C4->C5->C6->C7->C8->C9->C10
	//             \->F1->F2->F3->F4 (HEAD)
	//
	// Commit:   G, C2, F4
	// Snapshot: G, C6
	//
	// CRASH
	// Import F1->F2->F3->F4
	// STOP
	// OPEN READ-ONLY
	//
	// ------------------------------
	//
	// Expected head block     : F4
	// Expected recovery number: 6, untouched
	test := newOffChainTest(t)
	fork := test.heavy(test.canon[1], 6)
	datadir := t.TempDir()

	db, err := rawdb.Open(rawdb.OpenOptions{
		Directory:         datadir,
		AncientsDirectory: datadir,
	})
	if err != nil {
		t.Fatalf("Failed to create persistent database: %v", err)
	}
	defer db.Close() // Might double close, should be fine

	test.crashAboveHead(t, db)

	chain := test.startRecovery(t, db)
	if _, err := chain.InsertChain(fork[:4]); err != nil {
		t.Fatalf("Failed to import the fork: %v", err)
	}
	chain.Stop()
	db.Close()

	// Open the way MakeChain does for a read-only command
	db, err = rawdb.Open(rawdb.OpenOptions{
		Directory:         datadir,
		AncientsDirectory: datadir,
		ReadOnly:          true,
	})
	if err != nil {
		t.Fatalf("Failed to reopen persistent database: %v", err)
	}
	defer db.Close()

	cache := *defaultCacheConfig
	cache.SnapshotNoBuild = true
	chain, err = NewBlockChain(db, &cache, test.gspec, nil, test.engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to recreate chain: %v", err)
	}
	// Stop would try to persist the state, which a read-only database refuses
	defer chain.stopWithoutSaving()

	if head := chain.CurrentBlock(); head.Hash() != fork[3].Hash() {
		t.Errorf("Head block mismatch: have %d, want %d", head.Number, fork[3].Number())
	}
	if number := rawdb.ReadSnapshotRecoveryNumber(db); number == nil || *number != 6 {
		t.Errorf("Recovery number mismatch: have %v, want %d", number, 6)
	}
}

// Tests the walk that looks for a state root among the head and its ancestors. The
// headers of a stored side chain are not on the walk, and a gap in the headers ends
// it, so only the roots above the gap are found.
func TestRootOnChain(t *testing.T) {
	test := newOffChainTest(t)
	side := test.light(test.canon[1], 4)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	chain := test.start(t, db)
	defer chain.Stop()
	if _, err := chain.InsertChain(test.canon); err != nil {
		t.Fatalf("Failed to import canonical chain: %v", err)
	}
	head := chain.CurrentBlock()

	// The side chain is stored, but it is lighter and the head stays where it is
	if _, err := chain.InsertChain(side); err != nil {
		t.Fatalf("Failed to import side chain: %v", err)
	}
	if have := chain.CurrentBlock(); have.Hash() != head.Hash() {
		t.Fatalf("Side chain took over, head block %d", have.Number)
	}
	tests := []struct {
		name string
		root common.Hash
		want bool
	}{
		{"head", test.canon[9].Root(), true},
		{"ancestor", test.canon[1].Root(), true},
		{"genesis", chain.Genesis().Root(), true},
		{"unknown root", common.Hash{0xde, 0xad}, false},
		{"side chain", side[3].Root(), false},
	}
	for _, tt := range tests {
		if have := chain.rootOnChain(head, tt.root); have != tt.want {
			t.Errorf("%s: have %v, want %v", tt.name, have, tt.want)
		}
	}
	// Remove the header of C5, which the walk has to read to get below it
	rawdb.DeleteHeader(db, test.canon[4].Hash(), 5)
	chain.hc.headerCache.Purge()

	if !chain.rootOnChain(head, test.canon[7].Root()) {
		t.Errorf("above the gap: root not found")
	}
	if chain.rootOnChain(head, test.canon[2].Root()) {
		t.Errorf("below the gap: root found")
	}
}

// Tests the check that the canonical chain leads from the head up to the disk layer of
// a recovery number, for the layers and heads it has to refuse as well.
func TestLayerAboveHead(t *testing.T) {
	test := newOffChainTest(t)
	side := test.light(test.canon[1], 4)
	db := rawdb.NewMemoryDatabase()
	defer db.Close()

	chain := test.start(t, db)
	defer chain.Stop()
	if _, err := chain.InsertChain(test.canon); err != nil {
		t.Fatalf("Failed to import canonical chain: %v", err)
	}
	tests := []struct {
		name   string
		head   *types.Header
		number uint64
		root   common.Hash
		want   bool
	}{
		{"layer above the head", test.canon[1].Header(), 6, test.canon[5].Root(), true},
		{"layer on the head", test.canon[5].Header(), 6, test.canon[5].Root(), true},
		{"other root", test.canon[1].Header(), 6, test.canon[6].Root(), false},
		{"no header at the height", test.canon[1].Header(), 11, test.canon[5].Root(), false},
		{"head off the canonical chain", side[0].Header(), 6, test.canon[5].Root(), false},
	}
	for _, tt := range tests {
		if have := chain.layerAboveHead(tt.head, tt.number, tt.root); have != tt.want {
			t.Errorf("%s: have %v, want %v", tt.name, have, tt.want)
		}
	}
	// A gap in the canonical index breaks the link between the head and the layer
	rawdb.DeleteCanonicalHash(db, 4)
	if chain.layerAboveHead(test.canon[1].Header(), 6, test.canon[5].Root()) {
		t.Errorf("gap in the canonical index: layer found")
	}
}
