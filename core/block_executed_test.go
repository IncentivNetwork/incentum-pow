package core

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

type executedWorld struct {
	t       *testing.T
	spec    *Genesis
	genDb   ethdb.Database
	genesis *types.Block
	key     func(*BlockGen, common.Address)
	fixedTx func() *types.Transaction
}

func newExecutedWorld(t *testing.T) *executedWorld {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	config := *params.TestChainConfig
	config.ZeroRewardBlock = big.NewInt(0)
	spec := &Genesis{
		Config:  &config,
		BaseFee: big.NewInt(params.InitialBaseFee),
		Alloc:   GenesisAlloc{sender: {Balance: big.NewInt(params.Ether)}},
	}
	genDb := rawdb.NewMemoryDatabase()
	genesis := spec.MustCommit(genDb)
	signer := types.LatestSigner(&config)
	return &executedWorld{
		t:       t,
		spec:    spec,
		genDb:   genDb,
		genesis: genesis,
		key: func(b *BlockGen, to common.Address) {
			tx, err := types.SignTx(types.NewTransaction(b.TxNonce(sender), to, big.NewInt(1), params.TxGas, b.BaseFee(), nil), signer, key)
			if err != nil {
				t.Fatal(err)
			}
			b.AddTx(tx)
		},
		fixedTx: func() *types.Transaction {
			tx, err := types.SignTx(types.NewTransaction(0, common.Address{9}, big.NewInt(1), params.TxGas, big.NewInt(2*params.InitialBaseFee), nil), signer, key)
			if err != nil {
				t.Fatal(err)
			}
			return tx
		},
	}
}

func (w *executedWorld) transfers(parent *types.Block, n int, to byte) []*types.Block {
	blocks, _ := GenerateChain(w.spec.Config, parent, ethash.NewFaker(), w.genDb, n, func(_ int, b *BlockGen) {
		w.key(b, common.Address{to})
	})
	return blocks
}

func (w *executedWorld) empties(parent *types.Block, n int, offset int64) []*types.Block {
	blocks, _ := GenerateChain(w.spec.Config, parent, ethash.NewFaker(), w.genDb, n, func(_ int, b *BlockGen) {
		if offset != 0 {
			b.OffsetTime(offset)
		}
	})
	return blocks
}

func (w *executedWorld) open(db ethdb.Database, cache *CacheConfig) *BlockChain {
	chain, err := NewBlockChain(db, cache, w.spec, nil, ethash.NewFaker(), vm.Config{}, nil, nil)
	if err != nil {
		w.t.Fatal(err)
	}
	return chain
}

func (w *executedWorld) running(db ethdb.Database, cache *CacheConfig) *BlockChain {
	chain := w.open(db, cache)
	w.t.Cleanup(chain.Stop)
	return chain
}

func (w *executedWorld) insert(chain *BlockChain, blocks []*types.Block) {
	w.t.Helper()
	if n, err := chain.InsertChain(blocks); err != nil {
		w.t.Fatalf("block %d: %v", n, err)
	}
}

func (w *executedWorld) storeBySnapSync(chain *BlockChain, blocks []*types.Block) {
	w.t.Helper()
	headers := make([]*types.Header, len(blocks))
	for i, block := range blocks {
		headers[i] = block.Header()
	}
	if n, err := chain.InsertHeaderChain(headers, 1); err != nil {
		w.t.Fatalf("header %d: %v", n, err)
	}
	if n, err := chain.InsertReceiptChain(blocks, make([]types.Receipts, len(blocks)), 0); err != nil {
		w.t.Fatalf("receipts %d: %v", n, err)
	}
}

func TestExecutedIgnoresEmptyReceiptsEntry(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 8, 1)
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public)
	old.Stop()

	chain := w.running(db, nil)
	base := public[1]
	if chain.HasState(base.Root()) {
		t.Fatalf("the state of #%d is still there", base.NumberU64())
	}
	sibling := w.empties(base, 1, 0)[0]
	header := sibling.Header()
	header.Root = chain.CurrentBlock().Root
	claiming := types.NewBlockWithHeader(header)
	w.insert(chain, []*types.Block{claiming})
	if rawdb.HasBlockExecuted(db, claiming.Hash(), claiming.NumberU64()) {
		t.Fatal("a block stored unexecuted has the mark")
	}
	rawdb.WriteReceipts(db, claiming.Hash(), claiming.NumberU64(), nil)
	if !chain.HasState(claiming.Root()) {
		t.Fatal("the claimed state is gone")
	}
	if chain.HasBlockAndState(claiming.Hash(), claiming.NumberU64()) {
		t.Fatal("an empty receipts entry counts as proof that the block ran")
	}
}

func TestExecutedIgnoresSnapSyncedSideBlock(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	side := w.empties(public[3], 3, 5)
	db := rawdb.NewMemoryDatabase()
	chain := w.running(db, nil)
	w.insert(chain, public)
	w.storeBySnapSync(chain, side)

	block := side[1]
	if chain.GetCanonicalHash(block.NumberU64()) == block.Hash() {
		t.Fatal("the side block is canonical")
	}
	if !rawdb.HasReceipts(db, block.Hash(), block.NumberU64()) || !chain.HasState(block.Root()) {
		t.Fatal("the side block has no receipts or no state under its root")
	}
	if rawdb.HasBlockExecuted(db, block.Hash(), block.NumberU64()) {
		t.Fatal("snap sync wrote the mark")
	}
	if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
		t.Fatal("a side block stored by snap sync counts as executed")
	}
}

func TestExecutedIgnoresCanonicalHeaderAboveHead(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 4, 1)
	above := w.empties(public[3], 3, 0)
	chain := w.running(rawdb.NewMemoryDatabase(), nil)
	w.insert(chain, public)
	w.storeBySnapSync(chain, above)

	block := above[1]
	if chain.GetCanonicalHash(block.NumberU64()) != block.Hash() || !chain.HasState(block.Root()) {
		t.Fatal("the block is not canonical or has no state under its root")
	}
	if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
		t.Fatal("a canonical header above the head counts as executed")
	}
}

func TestExecutedIgnoresCanonicalRewrittenBelowHead(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	fork := w.empties(public[3], 12, 0)
	chain := w.running(rawdb.NewMemoryDatabase(), nil)
	w.insert(chain, public)
	w.storeBySnapSync(chain, fork)

	block, head := fork[2], public[9]
	if block.NumberU64() > head.NumberU64() || chain.CurrentBlock().Hash() != head.Hash() {
		t.Fatal("the fork block is not below the head")
	}
	if chain.GetCanonicalHash(block.NumberU64()) != block.Hash() || !chain.HasState(block.Root()) {
		t.Fatal("the fork block is not canonical or has no state under its root")
	}
	if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
		t.Fatal("a fork block moved into the canonical index below the head counts as executed")
	}
	if !chain.HasBlockAndState(head.Hash(), head.NumberU64()) {
		t.Fatal("the executed head no longer counts as executed")
	}
}

func TestExecutedGenesisWithRewrittenCanonical(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	fork := w.empties(public[3], 12, 0)
	heavier := w.empties(public[0], 40, 0)
	db := rawdb.NewMemoryDatabase()
	chain := w.running(db, nil)
	w.insert(chain, public)
	for _, block := range public {
		rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
	}
	w.storeBySnapSync(chain, fork)

	chain.headAncestry.hashes, chain.headAncestry.head = nil, common.Hash{}
	if !chain.HasBlockAndState(w.genesis.Hash(), 0) {
		t.Fatal("the genesis block does not count as executed")
	}
	if walked := len(chain.headAncestry.hashes); walked != 0 {
		t.Fatalf("walked %d blocks to prove the genesis block", walked)
	}
	w.insert(chain, heavier)
	tip := heavier[len(heavier)-1]
	if chain.CurrentBlock().Hash() != tip.Hash() {
		t.Fatalf("head #%d, want #%d", chain.CurrentBlock().Number, tip.NumberU64())
	}
}

func TestExecutedSurvivesRestart(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	side := w.transfers(public[5], 1, 2)[0]
	db := rawdb.NewMemoryDatabase()
	archive := &CacheConfig{TrieDirtyDisabled: true}
	old := w.open(db, archive)
	w.insert(old, public)
	w.insert(old, []*types.Block{side})
	old.Stop()

	chain := w.running(db, archive)
	if chain.GetCanonicalHash(side.NumberU64()) == side.Hash() {
		t.Fatal("the side block is canonical")
	}
	if !chain.HasBlockAndState(side.Hash(), side.NumberU64()) {
		t.Fatal("an executed side block is not known after a restart")
	}
	head := public[9]
	if !chain.HasBlockAndState(head.Hash(), head.NumberU64()) {
		t.Fatal("the head is not known after a restart")
	}
}

func TestExecutedDatabaseWithoutMarks(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 20, 1)
	side := w.transfers(public[15], 1, 2)[0]
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public)
	w.insert(old, []*types.Block{side})
	old.Stop()
	for _, block := range append(append([]*types.Block{}, public...), side) {
		if !rawdb.HasBlockExecuted(db, block.Hash(), block.NumberU64()) {
			t.Fatalf("no mark on #%d", block.NumberU64())
		}
		rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
	}

	chain := w.running(db, nil)
	head := public[19]
	if chain.CurrentBlock().Hash() != head.Hash() {
		t.Fatalf("head #%d after the restart, want #%d", chain.CurrentBlock().Number, head.NumberU64())
	}
	w.insert(chain, []*types.Block{head})
	if rawdb.HasBlockExecuted(db, head.Hash(), head.NumberU64()) {
		t.Fatal("the canonical head ran again instead of being skipped as known")
	}
	if chain.CurrentBlock().Hash() != head.Hash() {
		t.Fatalf("head moved to #%d", chain.CurrentBlock().Number)
	}
	if chain.HasBlockAndState(side.Hash(), side.NumberU64()) {
		t.Fatal("a side block without a mark counts as executed")
	}
	extension := w.empties(side, 10, 0)
	w.insert(chain, extension)
	tip := extension[len(extension)-1]
	if chain.CurrentBlock().Hash() != tip.Hash() {
		t.Fatalf("head #%d, want #%d", chain.CurrentBlock().Number, tip.NumberU64())
	}
	if !rawdb.HasBlockExecuted(db, side.Hash(), side.NumberU64()) {
		t.Fatal("the side block did not run again when it was adopted")
	}
}

func TestExecutedMarkOnEveryExecutingPath(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 2, 1)
	mined := w.transfers(public[1], 1, 2)[0]
	unset := w.transfers(public[1], 1, 3)[0]
	heavier := w.empties(public[1], 4, 0)
	db := rawdb.NewMemoryDatabase()
	chain := w.running(db, nil)
	w.insert(chain, public)

	statedb, err := state.New(public[1].Root(), chain.stateCache, chain.snaps)
	if err != nil {
		t.Fatal(err)
	}
	receipts, logs, _, err := chain.processor.Process(mined, statedb, vm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.WriteBlockAndSetHead(mined, receipts, logs, statedb, false); err != nil {
		t.Fatal(err)
	}
	if err := chain.InsertBlockWithoutSetHead(unset); err != nil {
		t.Fatal(err)
	}
	w.insert(chain, heavier)
	for name, block := range map[string]*types.Block{"written with the head": mined, "inserted without the head": unset} {
		if chain.GetCanonicalHash(block.NumberU64()) == block.Hash() {
			t.Fatalf("block %s is canonical", name)
		}
		if !chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
			t.Fatalf("block %s does not count as executed", name)
		}
	}
}

func TestExecutedMarkGoesWithSetHead(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db := rawdb.NewMemoryDatabase()
	chain := w.running(db, nil)
	w.insert(chain, public)
	if err := chain.SetHead(5); err != nil {
		t.Fatal(err)
	}
	removed := public[8]
	if rawdb.HasBody(db, removed.Hash(), removed.NumberU64()) {
		t.Fatal("the block is still stored")
	}
	if rawdb.HasBlockExecuted(db, removed.Hash(), removed.NumberU64()) {
		t.Fatal("the mark outlives its block")
	}
}

func TestExecutedRewindKeepsFullHeadAncestry(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		for _, timestamp := range []bool{false, true} {
			for _, target := range []int{3, 9} {
				t.Run(fmt.Sprintf("legacy=%t/time=%t/target=%d", legacy, timestamp, target), func(t *testing.T) {
					w := newExecutedWorld(t)
					public, _ := GenerateChain(w.spec.Config, w.genesis, ethash.NewFaker(), w.genDb, 10, func(_ int, b *BlockGen) {
						b.OffsetTime(-9)
						w.key(b, common.Address{1})
					})
					db := rawdb.NewMemoryDatabase()
					old := w.open(db, nil)
					w.insert(old, public)
					old.Stop()
					if legacy {
						for _, block := range public {
							rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
						}
					}
					chain := w.running(db, nil)
					base := public[1]
					if chain.HasState(base.Root()) {
						t.Fatal("the fork ancestor was not pruned")
					}
					header := w.empties(base, 1, 0)[0].Header()
					header.Root = public[9].Root()
					bad := types.NewBlockWithHeader(header)
					stored := append([]*types.Block{bad}, w.empties(bad, 7, 30)...)
					w.insert(chain, stored)
					if rawdb.HasReceipts(db, bad.Hash(), 3) || rawdb.HasBlockExecuted(db, bad.Hash(), 3) {
						t.Fatal("the invalid side block was executed")
					}
					fork := append(stored, w.empties(stored[len(stored)-1], 12, 0)...)
					headers := make([]*types.Header, len(fork))
					for i, block := range fork {
						headers[i] = block.Header()
					}
					if _, err := chain.InsertHeaderChain(headers, 1); err != nil {
						t.Fatal(err)
					}
					if chain.GetCanonicalHash(10) == public[9].Hash() || chain.CurrentBlock().Hash() != public[9].Hash() {
						t.Fatal("header sync did not replace the index independently of the full head")
					}
					if chain.HasBlockAndState(bad.Hash(), 3) {
						t.Fatal("header sync trusted the side block")
					}
					var err error
					if timestamp {
						err = chain.SetHeadWithTimestamp(stored[target-3].Time())
					} else {
						err = chain.SetHead(uint64(target))
					}
					if err != nil {
						t.Fatal(err)
					}
					want := w.genesis
					if !timestamp && target == 9 {
						want = public[8]
					}
					if timestamp {
						for _, block := range public {
							if block.Time() <= stored[target-3].Time() {
								want = block
							}
						}
					}
					if chain.CurrentBlock().Hash() != want.Hash() {
						t.Fatalf("rewound full head is #%d %x, want #%d %x", chain.CurrentBlock().Number, chain.CurrentBlock().Hash(), want.NumberU64(), want.Hash())
					}
					if chain.HasBlockAndState(bad.Hash(), 3) {
						t.Fatal("rewind trusted a never-executed state transition")
					}
				})
			}
		}
	}
}

func TestExecutedHeadExtensionRepairsCanonicalIndex(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	header := w.empties(public[3], 1, 0)[0].Header()
	header.Root = public[9].Root()
	claiming := types.NewBlockWithHeader(header)
	fork := append([]*types.Block{claiming}, w.empties(claiming, 11, 0)...)
	chain := w.running(rawdb.NewMemoryDatabase(), nil)
	w.insert(chain, public)
	w.storeBySnapSync(chain, fork)
	bad := fork[2]
	if chain.HasBlockAndState(bad.Hash(), bad.NumberU64()) {
		t.Fatal("the snap-synced side block was already trusted")
	}
	w.insert(chain, w.transfers(public[9], 1, 2))
	if chain.HasBlockAndState(bad.Hash(), bad.NumberU64()) {
		t.Fatal("an honest head extension made an unexecuted fork block count as executed")
	}
	for _, block := range public {
		if chain.GetCanonicalHash(block.NumberU64()) != block.Hash() {
			t.Fatalf("canonical index below the executed head points to a fork at #%d", block.NumberU64())
		}
	}
}

func TestExecutedLegacyDisconnectedCanonicalIndex(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	fork := w.empties(public[3], 12, 0)
	db := rawdb.NewMemoryDatabase()
	archive := &CacheConfig{TrieDirtyDisabled: true}
	old := w.open(db, archive)
	w.insert(old, public)
	w.storeBySnapSync(old, fork)
	head := public[9]
	rawdb.WriteCanonicalHash(db, head.Hash(), head.NumberU64())
	rawdb.WriteHeadHeaderHash(db, head.Hash())
	rawdb.WriteHeadFastBlockHash(db, head.Hash())
	for _, block := range public {
		rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
	}
	old.Stop()
	chain := w.running(db, archive)
	block := fork[2]
	if chain.CurrentBlock().Hash() != head.Hash() || chain.GetCanonicalHash(head.NumberU64()) != head.Hash() || chain.GetCanonicalHash(block.NumberU64()) != block.Hash() {
		t.Fatal("the old disconnected canonical layout was not preserved across restart")
	}
	if !chain.HasState(block.Root()) || !rawdb.HasReceipts(db, block.Hash(), block.NumberU64()) {
		t.Fatal("the fork's state and receipts are missing")
	}
	if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
		t.Fatal("a disconnected canonical entry inherited the full head's execution proof")
	}
	if !chain.HasBlockAndState(head.Hash(), head.NumberU64()) {
		t.Fatal("the legacy full head lost its execution proof")
	}
	// The index agrees with the head, so it rejects the head's own ancestors at the wrong
	// entries below it. That is the accepted cost of rejecting side blocks without a walk:
	// these blocks run again if a fork off them is adopted, and nothing is skipped.
	for _, block := range public[4:9] {
		if chain.GetCanonicalHash(block.NumberU64()) == block.Hash() {
			t.Fatalf("the index at #%d was repaired, which tests nothing", block.NumberU64())
		}
		if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
			t.Fatalf("ancestor #%d at a wrong index entry counts as executed without a walk", block.NumberU64())
		}
	}
}

func TestExecutedRewindFollowsFullHeadOverWrongIndex(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	fork := w.empties(public[3], 12, 0)
	db := rawdb.NewMemoryDatabase()
	archive := &CacheConfig{TrieDirtyDisabled: true}
	old := w.open(db, archive)
	w.insert(old, public)
	w.storeBySnapSync(old, fork)
	head := public[9]
	rawdb.WriteCanonicalHash(db, head.Hash(), head.NumberU64())
	rawdb.WriteHeadHeaderHash(db, head.Hash())
	rawdb.WriteHeadFastBlockHash(db, head.Hash())
	old.Stop()
	chain := w.running(db, archive)

	// The index agrees with the head and the header chain follows it, so nothing restores the
	// wrong entries below the head, and the fork blocks there have bodies and present roots.
	// The rewind has to walk the head's own ancestors: by the index it would make a fork
	// block that never ran the full head.
	target, wrong := public[5], fork[1]
	if chain.GetCanonicalHash(target.NumberU64()) != wrong.Hash() || !chain.HasState(wrong.Root()) {
		t.Fatal("the wrong index entry with a present root was not preserved")
	}
	if err := chain.SetHead(target.NumberU64()); err != nil {
		t.Fatal(err)
	}
	if chain.CurrentBlock().Hash() != target.Hash() || chain.CurrentHeader().Hash() != target.Hash() {
		t.Fatalf("full head %x and header head %x after the rewind, want #%d %x", chain.CurrentBlock().Hash(), chain.CurrentHeader().Hash(), target.NumberU64(), target.Hash())
	}
	if !chain.HasBlockAndState(target.Hash(), target.NumberU64()) || chain.HasBlockAndState(wrong.Hash(), wrong.NumberU64()) {
		t.Fatal("the execution proof did not follow the rewound head")
	}
}

func (w *executedWorld) headerReorg(chain *BlockChain, base *types.Block, n int) []*types.Block {
	w.t.Helper()
	fork := w.empties(base, n, 0)
	headers := make([]*types.Header, len(fork))
	for i, block := range fork {
		headers[i] = block.Header()
	}
	if _, err := chain.InsertHeaderChain(headers, 1); err != nil {
		w.t.Fatal(err)
	}
	return fork
}

func (w *executedWorld) legacyArchive(public []*types.Block) (ethdb.Database, *BlockChain) {
	w.t.Helper()
	db := rawdb.NewMemoryDatabase()
	archive := &CacheConfig{TrieDirtyDisabled: true}
	old := w.open(db, archive)
	w.insert(old, public)
	old.Stop()
	for _, block := range public {
		rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
	}
	return db, w.running(db, archive)
}

func (w *executedWorld) replayed(db ethdb.Database, blocks []*types.Block) int {
	var count int
	for _, block := range blocks {
		if rawdb.HasBlockExecuted(db, block.Hash(), block.NumberU64()) {
			count++
		}
	}
	return count
}

func TestExecutedLegacyHeadSurvivesHeaderReorg(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db, chain := w.legacyArchive(public)
	fork := w.headerReorg(chain, public[3], 12)
	head := public[9]
	if chain.GetCanonicalHash(head.NumberU64()) == head.Hash() {
		t.Fatal("the header reorg left the index at the head alone")
	}
	if !chain.HasBlockAndState(head.Hash(), head.NumberU64()) {
		t.Fatal("the legacy full head stopped counting as executed")
	}
	if !chain.HasBlockAndState(public[4].Hash(), 5) {
		t.Fatal("an ancestor of the legacy full head stopped counting as executed")
	}
	if chain.HasBlockAndState(fork[0].Hash(), fork[0].NumberU64()) {
		t.Fatal("a fork header counts as executed")
	}
	w.insert(chain, w.transfers(head, 1, 2))
	if n := w.replayed(db, public); n != 0 {
		t.Fatalf("re-executed %d of %d old blocks", n, len(public))
	}
}

func TestExecutedRewindRestoresHeadMarkers(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db, chain := w.legacyArchive(public)
	w.headerReorg(chain, public[3], 12)
	if err := chain.SetHead(9); err != nil {
		t.Fatal(err)
	}
	head := public[8]
	if chain.CurrentBlock().Hash() != head.Hash() {
		t.Fatalf("full head #%d %x, want #9 %x", chain.CurrentBlock().Number, chain.CurrentBlock().Hash(), head.Hash())
	}
	if chain.CurrentHeader().Hash() != head.Hash() {
		t.Fatalf("head header #%d %x, want #9 %x", chain.CurrentHeader().Number, chain.CurrentHeader().Hash(), head.Hash())
	}
	if chain.CurrentSnapBlock().Hash() != head.Hash() {
		t.Fatalf("snap head #%d, want #9", chain.CurrentSnapBlock().Number)
	}
	for _, block := range public[:9] {
		if chain.GetCanonicalHash(block.NumberU64()) != block.Hash() {
			t.Fatalf("index at #%d points off the full head", block.NumberU64())
		}
	}
	if block := chain.GetBlockByNumber(9); block == nil || block.Hash() != head.Hash() {
		t.Fatal("the head cannot be found by number")
	}
	if hash := chain.GetCanonicalHash(10); hash != (common.Hash{}) {
		t.Fatalf("index above the head still points at %x", hash)
	}
	w.insert(chain, w.transfers(head, 1, 2))
	if n := w.replayed(db, public); n != 0 {
		t.Fatalf("re-executed %d of %d old blocks", n, len(public))
	}
}

func TestExecutedTimestampRewindAfterHeaderReorg(t *testing.T) {
	w := newExecutedWorld(t)
	public, _ := GenerateChain(w.spec.Config, w.genesis, ethash.NewFaker(), w.genDb, 10, func(_ int, b *BlockGen) {
		b.OffsetTime(25)
		w.key(b, common.Address{1})
	})
	_, chain := w.legacyArchive(public)
	w.headerReorg(chain, public[1], 30)
	target := public[3].Time()
	if err := chain.SetHeadWithTimestamp(target); err != nil {
		t.Fatal(err)
	}
	if have := chain.CurrentBlock(); have.Time > target || have.Hash() != public[3].Hash() {
		t.Fatalf("full head #%d with time %d, want #4 with time %d", have.Number, have.Time, target)
	}
	if have := chain.CurrentHeader(); have.Hash() != public[3].Hash() {
		t.Fatalf("head header #%d %x with time %d", have.Number, have.Hash(), have.Time)
	}
}

func TestExecutedTimestampRewindWithOlderHeaderTip(t *testing.T) {
	w := newExecutedWorld(t)
	public, _ := GenerateChain(w.spec.Config, w.genesis, ethash.NewFaker(), w.genDb, 10, func(_ int, b *BlockGen) {
		b.OffsetTime(25)
		w.key(b, common.Address{1})
	})
	_, chain := w.legacyArchive(public)
	fork := w.headerReorg(chain, public[1], 20)
	if chain.CurrentHeader().Hash() != fork[len(fork)-1].Hash() {
		t.Fatal("the fork did not take over the header chain")
	}
	target := fork[len(fork)-1].Time()
	if target >= public[9].Time() {
		t.Fatal("the fork tip is not older than the full head")
	}
	if err := chain.SetHeadWithTimestamp(target); err != nil {
		t.Fatal(err)
	}
	if have := chain.CurrentBlock(); have.Time > target {
		t.Fatalf("full head #%d with time %d is newer than %d", have.Number, have.Time, target)
	}
}

func TestExecutedProofGoesWithSetHead(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db, chain := w.legacyArchive(public)
	if !chain.HasBlockAndState(public[7].Hash(), 8) {
		t.Fatal("the legacy block does not count as executed")
	}
	if err := chain.SetHead(5); err != nil {
		t.Fatal(err)
	}
	if rawdb.HasBody(db, public[7].Hash(), 8) {
		t.Fatal("the block is still stored")
	}
	w.storeBySnapSync(chain, public[5:8])
	if chain.CurrentBlock().Number.Uint64() != 5 || !rawdb.HasBody(db, public[7].Hash(), 8) || !chain.HasState(public[7].Root()) {
		t.Fatal("the block was not stored back above the head")
	}
	if chain.HasBlockAndState(public[7].Hash(), 8) {
		t.Fatal("the proof outlived the rewind")
	}
}

func TestExecutedFrozenBlocks(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 20, 1)
	side := w.transfers(public[3], 1, 2)[0]
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	archive := &CacheConfig{TrieDirtyDisabled: true}
	chain := w.running(db, archive)
	w.insert(chain, public)
	w.insert(chain, []*types.Block{side})
	freezer, ok := db.(interface{ Freeze(threshold uint64) error })
	if !ok {
		t.Fatal("the database cannot be frozen on demand")
	}
	if err := freezer.Freeze(5); err != nil {
		t.Fatal(err)
	}
	frozen, _ := db.Ancients()
	if frozen < 10 {
		t.Fatalf("only %d blocks frozen", frozen)
	}
	old := public[5]
	if old.NumberU64() >= frozen || rawdb.HasBlockExecuted(db, old.Hash(), old.NumberU64()) {
		t.Fatal("the old block is not frozen without its mark")
	}
	chain.headAncestry.hashes, chain.headAncestry.head = nil, common.Hash{}
	if !chain.HasBlockAndState(old.Hash(), old.NumberU64()) {
		t.Fatal("a frozen canonical block does not count as executed")
	}
	if walked := len(chain.headAncestry.hashes); walked != 0 {
		t.Fatalf("walked %d blocks to prove a frozen canonical block", walked)
	}
	if rawdb.HasBlockExecuted(db, side.Hash(), side.NumberU64()) || side.NumberU64() >= frozen {
		t.Fatal("the side block is not below the frozen limit without its mark")
	}
	rawdb.WriteBlock(db, side)
	if !chain.HasState(side.Root()) {
		t.Fatal("the side block has no state")
	}
	if chain.HasBlockAndState(side.Hash(), side.NumberU64()) {
		t.Fatal("a side block at a frozen height counts as executed")
	}
}

func TestExecutedStopFlushesFullHeadAfterHeaderReorg(t *testing.T) {
	for _, bodies := range []bool{false, true} {
		for _, rewind := range []bool{false, true} {
			t.Run(fmt.Sprintf("bodies=%t/rewind=%t", bodies, rewind), func(t *testing.T) {
				w := newExecutedWorld(t)
				public := w.transfers(w.genesis, 10, 1)
				db := rawdb.NewMemoryDatabase()
				chain := w.open(db, nil)
				w.insert(chain, public)
				if bodies {
					w.storeBySnapSync(chain, w.empties(public[3], 12, 0))
				} else {
					w.headerReorg(chain, public[3], 12)
				}
				want := public[9]
				if rewind {
					if err := chain.SetHead(9); err != nil {
						t.Fatal(err)
					}
					want = public[8]
				}
				if chain.CurrentBlock().Hash() != want.Hash() {
					t.Fatalf("full head #%d before the stop, want #%d", chain.CurrentBlock().Number, want.NumberU64())
				}
				chain.Stop()
				restarted := w.running(db, nil)
				if restarted.CurrentBlock().Hash() != want.Hash() {
					t.Fatalf("full head #%d after the restart, want #%d", restarted.CurrentBlock().Number, want.NumberU64())
				}
				if !restarted.HasState(want.Root()) {
					t.Fatal("the state of the full head was not flushed")
				}
			})
		}
	}
}

func TestExecutedRewindAfterHeaderForkOverExtendedHead(t *testing.T) {
	for _, staleIndex := range []bool{false, true} {
		t.Run(fmt.Sprintf("staleIndex=%t", staleIndex), func(t *testing.T) {
			w := newExecutedWorld(t)
			public, _ := GenerateChain(w.spec.Config, w.genesis, ethash.NewFaker(), w.genDb, 11, func(_ int, b *BlockGen) {
				b.OffsetTime(25)
				w.key(b, common.Address{1})
			})
			db, chain := w.legacyArchive(public[:10])
			fork := w.headerReorg(chain, public[3], 13)
			w.insert(chain, public[10:])
			tip := w.empties(fork[len(fork)-1], 1, 0)[0]
			if _, err := chain.InsertHeaderChain([]*types.Header{tip.Header()}, 1); err != nil {
				t.Fatal(err)
			}
			if chain.CurrentHeader().Hash() != tip.Hash() {
				t.Fatal("the header chain did not follow the fork")
			}
			if tip.Time() >= public[10].Time() {
				t.Fatal("the fork tip is not older than the full head")
			}
			if staleIndex {
				for _, block := range public[3:] {
					rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
				}
			}
			if err := chain.SetHeadWithTimestamp(tip.Time()); err != nil {
				t.Fatal(err)
			}
			want := w.genesis
			for _, block := range public {
				if block.Time() <= tip.Time() {
					want = block
				}
			}
			if want == w.genesis {
				t.Fatal("no public block is older than the fork tip")
			}
			if have := chain.CurrentBlock(); have.Hash() != want.Hash() {
				t.Fatalf("full head #%d with time %d, want #%d with time %d", have.Number, have.Time, want.NumberU64(), want.Time())
			}
			if chain.CurrentHeader().Hash() != chain.CurrentBlock().Hash() {
				t.Fatal("the head header is off the full head")
			}
		})
	}
}

func TestExecutedRewindWipesHeaderForkAboveTarget(t *testing.T) {
	for _, target := range []uint64{10, 14, 30} {
		t.Run(fmt.Sprintf("target=%d", target), func(t *testing.T) {
			w := newExecutedWorld(t)
			public := w.transfers(w.genesis, 10, 1)
			db, chain := w.legacyArchive(public)
			fork := w.empties(public[3], 12, 0)
			w.storeBySnapSync(chain, fork)
			for _, block := range fork {
				rawdb.WriteBlockExecuted(db, block.Hash(), block.NumberU64())
			}
			if err := chain.SetHead(target); err != nil {
				t.Fatal(err)
			}
			head := public[9]
			if chain.CurrentBlock().Hash() != head.Hash() || chain.CurrentHeader().Hash() != head.Hash() || chain.CurrentSnapBlock().Hash() != head.Hash() {
				t.Fatalf("head markers are at #%d, #%d and #%d", chain.CurrentBlock().Number, chain.CurrentHeader().Number, chain.CurrentSnapBlock().Number)
			}
			for _, block := range fork {
				number, hash := block.NumberU64(), block.Hash()
				want := number <= target
				if number <= head.NumberU64() {
					continue
				}
				if have := rawdb.HasHeader(db, hash, number); have != want {
					t.Errorf("header of fork #%d stored: %t, want %t", number, have, want)
				}
				if have := rawdb.HasBody(db, hash, number); have != want {
					t.Errorf("body of fork #%d stored: %t, want %t", number, have, want)
				}
				if have := rawdb.HasReceipts(db, hash, number); have != want {
					t.Errorf("receipts of fork #%d stored: %t, want %t", number, have, want)
				}
				if have := rawdb.HasBlockExecuted(db, hash, number); have != want {
					t.Errorf("mark of fork #%d stored: %t, want %t", number, have, want)
				}
				if have := rawdb.ReadCanonicalHash(db, number); have != (common.Hash{}) {
					t.Errorf("index above the head still points at %x", have)
				}
			}
		})
	}
}

func TestExecutedIndexRepairRewritesTxLookups(t *testing.T) {
	for _, rewind := range []bool{false, true} {
		for _, publicHeight := range []int{6, 7} {
			t.Run(fmt.Sprintf("rewind=%t/public=%d", rewind, publicHeight), func(t *testing.T) {
				w := newExecutedWorld(t)
				tx := w.fixedTx()
				withTxAt := func(parent *types.Block, n, height int) []*types.Block {
					blocks, _ := GenerateChain(w.spec.Config, parent, ethash.NewFaker(), w.genDb, n, func(_ int, b *BlockGen) {
						if b.Number().Int64() == int64(height) {
							b.AddTx(tx)
						}
					})
					return blocks
				}
				public := withTxAt(w.genesis, 10, publicHeight)
				db := rawdb.NewMemoryDatabase()
				chain := w.running(db, nil)
				w.insert(chain, public)
				forkHeight := 13 - publicHeight
				w.storeBySnapSync(chain, withTxAt(public[3], 12, forkHeight))
				if entry := rawdb.ReadTxLookupEntry(db, tx.Hash()); entry == nil || *entry != uint64(forkHeight) {
					t.Fatal("the fork did not move the lookup")
				}
				chain.GetTransactionLookup(tx.Hash())
				if rewind {
					if err := chain.SetHead(9); err != nil {
						t.Fatal(err)
					}
				} else {
					w.insert(chain, w.empties(public[9], 1, 0))
				}
				found, hash, number, _ := rawdb.ReadTransaction(db, tx.Hash())
				if found == nil || hash != public[publicHeight-1].Hash() || number != uint64(publicHeight) {
					t.Fatalf("transaction found: %t, in #%d %x, want #%d", found != nil, number, hash, publicHeight)
				}
				if lookup := chain.GetTransactionLookup(tx.Hash()); lookup == nil || lookup.BlockIndex != uint64(publicHeight) {
					t.Fatalf("cached lookup is %+v, want #%d", lookup, publicHeight)
				}
			})
		}
	}
}

func TestExecutedRewindWithHeaderGapRecovers(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public)
	old.Stop()
	chain := w.running(db, nil)
	w.headerReorg(chain, public[3], 12)
	rawdb.DeleteHeader(db, public[6].Hash(), 7)
	if err := chain.SetHead(9); err != nil {
		t.Fatal(err)
	}
	if chain.CurrentBlock().Number.Uint64() != 0 {
		t.Fatalf("full head #%d over a gap in its ancestors, want genesis", chain.CurrentBlock().Number)
	}
	w.insert(chain, w.empties(w.genesis, 30, 0))
}

func TestExecutedHeadExtensionOverHeaderGapKeepsRunning(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 11, 1)
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public[:10])
	old.Stop()
	chain := w.running(db, nil)
	fork := w.headerReorg(chain, public[3], 12)
	rawdb.DeleteHeader(db, public[6].Hash(), 7)

	// The head has state, so a start repairs nothing: exiting on the gap here would exit
	// again on every block. The index is repaired down to the gap and kept below it.
	w.insert(chain, public[10:])
	head := public[10]
	if chain.CurrentBlock().Hash() != head.Hash() || chain.CurrentHeader().Hash() != head.Hash() {
		t.Fatalf("head markers at #%d and #%d after the extension, want #11", chain.CurrentBlock().Number, chain.CurrentHeader().Number)
	}
	for _, block := range public[7:] {
		if rawdb.ReadCanonicalHash(db, block.NumberU64()) != block.Hash() {
			t.Fatalf("index at #%d not repaired down to the gap", block.NumberU64())
		}
	}
	for _, block := range fork[:3] {
		if rawdb.ReadCanonicalHash(db, block.NumberU64()) != block.Hash() {
			t.Fatalf("index at #%d changed below the gap", block.NumberU64())
		}
	}
}

func TestExecutedMissingHeadBlockResets(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 5, 1)
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public)
	old.Stop()
	rawdb.WriteHeadBlockHash(db, common.Hash{1})

	done := make(chan *BlockChain, 1)
	go func() { done <- w.running(db, nil) }()
	select {
	case chain := <-done:
		if chain.CurrentBlock().Hash() != w.genesis.Hash() || chain.CurrentHeader().Hash() != w.genesis.Hash() {
			t.Fatalf("head markers are at #%d and #%d, want genesis", chain.CurrentBlock().Number, chain.CurrentHeader().Number)
		}
		for _, block := range public {
			if !rawdb.HasBody(db, block.Hash(), block.NumberU64()) {
				t.Fatalf("the reset deleted #%d", block.NumberU64())
			}
		}
		w.insert(chain, public)
		if chain.CurrentBlock().Hash() != public[4].Hash() {
			t.Fatalf("head #%d after importing the chain again, want #5", chain.CurrentBlock().Number)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the reset did not return")
	}
}

func TestExecutedRepairKeepsHeaderForkData(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db, chain := w.legacyArchive(public)
	fork := w.empties(public[3], 12, 0)
	w.storeBySnapSync(chain, fork)
	head := public[9]
	if _, err := chain.setHeadBeyondRoot(10, 0, common.Hash{}, true); err != nil {
		t.Fatal(err)
	}
	if chain.CurrentBlock().Hash() != head.Hash() || chain.CurrentHeader().Hash() != head.Hash() || chain.CurrentSnapBlock().Hash() != head.Hash() {
		t.Fatalf("head markers are at #%d, #%d and #%d", chain.CurrentBlock().Number, chain.CurrentHeader().Number, chain.CurrentSnapBlock().Number)
	}
	if chain.GetCanonicalHash(10) != head.Hash() || chain.GetCanonicalHash(11) != (common.Hash{}) {
		t.Fatal("the index does not end at the full head")
	}
	tip := fork[len(fork)-1]
	if !rawdb.HasHeader(db, tip.Hash(), tip.NumberU64()) || !rawdb.HasBody(db, tip.Hash(), tip.NumberU64()) {
		t.Fatal("a repair deleted the fork")
	}
	if chain.HasBlockAndState(tip.Hash(), tip.NumberU64()) {
		t.Fatal("the fork tip counts as executed")
	}
}

func TestExecutedTimestampRewindKeepsForkBelowTarget(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db, chain := w.legacyArchive(public)
	fork := w.empties(public[3], 12, 0)
	w.storeBySnapSync(chain, fork)
	target := fork[8].Time()
	if target < public[9].Time() {
		t.Fatal("the target is older than the full head")
	}
	if err := chain.SetHeadWithTimestamp(target); err != nil {
		t.Fatal(err)
	}
	if chain.CurrentHeader().Hash() != public[9].Hash() {
		t.Fatalf("head header #%d, want #10", chain.CurrentHeader().Number)
	}
	for _, block := range fork {
		if block.NumberU64() <= 10 {
			continue
		}
		want := block.Time() <= target
		if have := rawdb.HasHeader(db, block.Hash(), block.NumberU64()); have != want {
			t.Errorf("fork #%d with time %d stored: %t, want %t with target %d", block.NumberU64(), block.Time(), have, want, target)
		}
	}
}

func TestExecutedHeadWriteRefreshesTxLookupCache(t *testing.T) {
	w := newExecutedWorld(t)
	tx := w.fixedTx()
	public, _ := GenerateChain(w.spec.Config, w.genesis, ethash.NewFaker(), w.genDb, 11, func(_ int, b *BlockGen) {
		if b.Number().Int64() == 11 {
			b.AddTx(tx)
		}
	})
	// A second later than the public blocks, or the fork would repeat them hash for hash.
	fork, _ := GenerateChain(w.spec.Config, public[3], ethash.NewFaker(), w.genDb, 12, func(_ int, b *BlockGen) {
		b.OffsetTime(1)
		if b.Number().Int64() == 11 {
			b.AddTx(tx)
		}
	})
	if fork[6].Hash() == public[10].Hash() {
		t.Fatal("the fork block with the transaction is the public one")
	}
	chain := w.running(rawdb.NewMemoryDatabase(), nil)
	w.insert(chain, public[:10])
	w.storeBySnapSync(chain, fork)
	if lookup := chain.GetTransactionLookup(tx.Hash()); lookup == nil || lookup.BlockHash != fork[6].Hash() {
		t.Fatalf("cached lookup is %+v, want the fork block", lookup)
	}
	w.insert(chain, public[10:])
	if lookup := chain.GetTransactionLookup(tx.Hash()); lookup == nil || lookup.BlockHash != public[10].Hash() {
		t.Fatalf("cached lookup is %+v, want the executed block", lookup)
	}
}

func TestExecutedNormalImportKeepsTxLookupCache(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 3, 1)
	chain := w.running(rawdb.NewMemoryDatabase(), nil)
	w.insert(chain, public[:2])
	tx := public[0].Transactions()[0]
	if chain.GetTransactionLookup(tx.Hash()) == nil {
		t.Fatal("the transaction is not found")
	}
	if !chain.txLookupCache.Contains(tx.Hash()) {
		t.Fatal("the lookup was not cached")
	}
	w.insert(chain, public[2:])
	if !chain.txLookupCache.Contains(tx.Hash()) {
		t.Fatal("a normal head extension purged the lookup cache")
	}
}

func TestExecutedReorgPurgesTxLookupCache(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 3, 1)
	// A second after its parent, the sibling is harder than the head it replaces.
	sibling := w.empties(public[1], 1, -9)[0]
	chain := w.running(rawdb.NewMemoryDatabase(), nil)
	w.insert(chain, public)
	tx := public[2].Transactions()[0]
	if lookup := chain.GetTransactionLookup(tx.Hash()); lookup == nil || lookup.BlockHash != public[2].Hash() {
		t.Fatalf("lookup is %+v, want the head block", lookup)
	}
	w.insert(chain, []*types.Block{sibling})
	if chain.CurrentBlock().Hash() != sibling.Hash() {
		t.Fatal("the heavier sibling did not become the head")
	}
	if lookup := chain.GetTransactionLookup(tx.Hash()); lookup != nil {
		t.Fatalf("lookup of a dropped transaction is %+v, want none", lookup)
	}
}

func TestExecutedAncestryChecksShareOneWalk(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 30, 1)
	header := w.empties(public[3], 1, 0)[0].Header()
	header.Root = public[29].Root()
	claiming := types.NewBlockWithHeader(header)
	side := append([]*types.Block{claiming}, w.empties(claiming, 10, 0)...)
	db, chain := w.legacyArchive(public)
	for _, block := range side {
		rawdb.WriteBlock(db, block)
	}
	walked := func() int { return len(chain.headAncestry.hashes) }

	chain.headAncestry.hashes = nil
	chain.headAncestry.head = common.Hash{}
	for _, block := range side {
		if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
			t.Fatalf("side block #%d counts as executed", block.NumberU64())
		}
	}
	if have := walked(); have != 0 {
		t.Fatalf("walked %d blocks for side blocks the agreeing index rejects", have)
	}
	for i := 0; i < 2; i++ {
		if !chain.HasBlockAndState(public[9].Hash(), 10) {
			t.Fatal("a keyless ancestor does not count as executed")
		}
	}
	want := 21
	if have := walked(); have != want {
		t.Fatalf("kept %d hashes, want %d", have, want)
	}
	reads := chain.headAncestry.hashes[20]
	chain.headAncestry.hashes[20] = common.Hash{1}
	if chain.HasBlockAndState(public[9].Hash(), 10) {
		t.Fatal("the second check did not use the shared walk")
	}
	chain.headAncestry.hashes[20] = reads

	w.headerReorg(chain, public[3], 40)
	for _, block := range side {
		if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
			t.Fatalf("side block #%d counts as executed after the header reorg", block.NumberU64())
		}
	}
	if have := walked(); have != 26 {
		t.Fatalf("kept %d hashes after checking every side block, want 26", have)
	}
	next := w.transfers(public[29], 1, 2)
	w.insert(chain, next)
	if !chain.HasBlockAndState(public[9].Hash(), 10) {
		t.Fatal("a keyless ancestor stopped counting once the head moved")
	}
	if chain.headAncestry.head != next[0].Hash() {
		t.Fatal("the walk did not follow the new head")
	}
}

func TestExecutedHeadExtensionKeepsSharedWalk(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 30, 1)
	next := w.transfers(public[29], 1, 1)
	db, chain := w.legacyArchive(public)
	if !chain.HasBlockAndState(public[9].Hash(), 10) {
		t.Fatal("a keyless ancestor does not count as executed")
	}
	if have := len(chain.headAncestry.hashes); have != 21 {
		t.Fatalf("kept %d hashes, want 21", have)
	}
	// A poisoned entry shows whether the next check walks again or keeps this walk.
	reads := chain.headAncestry.hashes[20]
	chain.headAncestry.hashes[20] = common.Hash{1}
	w.insert(chain, next)
	rawdb.DeleteBlockExecuted(db, next[0].Hash(), next[0].NumberU64())
	if chain.HasBlockAndState(public[9].Hash(), 10) {
		t.Fatal("the check after the head extension walked again")
	}
	chain.headAncestry.hashes[21] = reads
	if chain.headAncestry.head != next[0].Hash() || len(chain.headAncestry.hashes) != 22 || chain.headAncestry.hashes[0] != next[0].Hash() {
		t.Fatalf("walk kept for %x with %d hashes, want the new head and 22", chain.headAncestry.head, len(chain.headAncestry.hashes))
	}
	for i, block := range []*types.Block{next[0], public[29], public[9]} {
		if !chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
			t.Fatalf("block %d of the extended head's chain does not count as executed", i)
		}
	}
	side := w.empties(public[8], 1, 0)[0]
	rawdb.WriteBlock(db, side)
	if chain.HasBlockAndState(side.Hash(), side.NumberU64()) {
		t.Fatal("a side block counts as executed after the head extension")
	}
}

func TestExecutedHeadGrowthByBatchKeepsSharedWalk(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 30, 1)
	next := w.transfers(public[29], 30, 1)
	db, chain := w.legacyArchive(public)
	if !chain.HasBlockAndState(public[9].Hash(), 10) {
		t.Fatal("a keyless ancestor does not count as executed")
	}
	if have := len(chain.headAncestry.hashes); have != 21 {
		t.Fatalf("kept %d hashes, want 21", have)
	}

	// Three blocks arrive between two checks: the gap is walked and the kept walk stays.
	reads := chain.headAncestry.hashes[20]
	chain.headAncestry.hashes[20] = common.Hash{1}
	w.insert(chain, next[:3])
	for _, block := range next[:3] {
		rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
	}
	if chain.HasBlockAndState(public[9].Hash(), 10) {
		t.Fatal("the check after three new blocks walked again")
	}
	chain.headAncestry.hashes[23] = reads
	if len(chain.headAncestry.hashes) != 24 || chain.headAncestry.hashes[0] != next[2].Hash() || chain.headAncestry.hashes[2] != next[0].Hash() {
		t.Fatalf("kept %d hashes, want 24 with the new blocks in front", len(chain.headAncestry.hashes))
	}
	for _, block := range []*types.Block{next[2], next[0], public[29], public[9]} {
		if !chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
			t.Fatalf("block #%d of the head's chain does not count as executed", block.NumberU64())
		}
	}

	// A gap longer than the kept walk is not worth walking: the walk starts over.
	w.insert(chain, next[3:])
	for _, block := range next[3:] {
		rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
	}
	if !chain.HasBlockAndState(next[20].Hash(), next[20].NumberU64()) {
		t.Fatal("a keyless ancestor does not count as executed after the batch")
	}
	if have, want := len(chain.headAncestry.hashes), int(next[29].NumberU64()-next[20].NumberU64())+1; have != want {
		t.Fatalf("kept %d hashes after a gap longer than the walk, want %d", have, want)
	}
}

func TestExecutedSiblingHeadStartsWalkOver(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	sibling := w.transfers(public[3], 7, 2)
	_, chain := w.legacyArchive(public)
	if !chain.HasBlockAndState(public[4].Hash(), 5) {
		t.Fatal("a keyless ancestor of the legacy head does not count as executed")
	}
	cached := chain.headAncestry.head
	if cached != public[9].Hash() {
		t.Fatalf("the walk is kept for %x, want the legacy head", cached)
	}

	// The sibling head is one block higher but not a child of the kept head, so the walk
	// must start over: kept, it would name the old chain's blocks as the head's ancestors.
	w.insert(chain, sibling)
	head := sibling[len(sibling)-1]
	if chain.CurrentBlock().Hash() != head.Hash() || head.NumberU64() != 11 || head.ParentHash() == public[9].Hash() {
		t.Fatalf("head #%d %x, want the sibling chain's #11", chain.CurrentBlock().Number, chain.CurrentBlock().Hash())
	}
	if chain.headAncestry.head != cached {
		t.Fatalf("a check ran under another head before the test: %x", chain.headAncestry.head)
	}
	w.headerReorg(chain, public[3], 12)
	if chain.GetCanonicalHash(11) == head.Hash() {
		t.Fatal("the index still agrees with the head, so the walk is not reached")
	}
	if chain.HasBlockAndState(public[8].Hash(), 9) {
		t.Fatal("a keyless block of the old chain counts as executed under the sibling head")
	}
	if !chain.HasBlockAndState(sibling[4].Hash(), 9) {
		t.Fatal("an ancestor of the sibling head does not count as executed")
	}
}

func TestExecutedKeptWalkEndsAtFrozenLimit(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 30, 1)
	next := w.transfers(public[29], 1, 1)
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	chain := w.running(db, &CacheConfig{TrieDirtyDisabled: true})
	w.insert(chain, public)
	for _, block := range public {
		rawdb.DeleteBlockExecuted(db, block.Hash(), block.NumberU64())
	}
	if !chain.HasBlockAndState(public[9].Hash(), 10) {
		t.Fatal("a keyless ancestor does not count as executed")
	}
	if have := len(chain.headAncestry.hashes); have != 21 {
		t.Fatalf("kept %d hashes, want 21", have)
	}
	freezer, ok := db.(interface{ Freeze(threshold uint64) error })
	if !ok {
		t.Fatal("the database cannot be frozen on demand")
	}
	if err := freezer.Freeze(5); err != nil {
		t.Fatal(err)
	}
	frozen, _ := db.Ancients()

	// The head grows by one block and a keyless block above the frozen limit is checked: the
	// kept walk is extended, and its part below the frozen limit is dropped, as the frozen
	// table answers there.
	w.insert(chain, next)
	rawdb.DeleteBlockExecuted(db, next[0].Hash(), next[0].NumberU64())
	block := public[27]
	if block.NumberU64() < frozen {
		t.Fatalf("block #%d is below the frozen limit %d", block.NumberU64(), frozen)
	}
	if !chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
		t.Fatal("a keyless ancestor above the frozen limit does not count as executed")
	}
	want := int(next[0].NumberU64() - frozen + 1)
	if have := len(chain.headAncestry.hashes); have != want {
		t.Fatalf("kept %d hashes with the frozen limit at %d, want %d", have, frozen, want)
	}
}

func TestExecutedIgnoresFrozenBlockAboveHead(t *testing.T) {
	w := newExecutedWorld(t)
	blocks := w.empties(w.genesis, 20, 0)
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	chain := w.running(db, nil)
	headers := make([]*types.Header, len(blocks))
	for i, block := range blocks {
		headers[i] = block.Header()
	}
	if _, err := chain.InsertHeaderChain(headers, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := chain.InsertReceiptChain(blocks, make([]types.Receipts, len(blocks)), 15); err != nil {
		t.Fatal(err)
	}
	frozen, _ := db.Ancients()
	block := blocks[4]
	if block.NumberU64() >= frozen || chain.CurrentBlock().Number.Uint64() != 0 {
		t.Fatalf("block #%d is not frozen above the full head: frozen %d, head #%d", block.NumberU64(), frozen, chain.CurrentBlock().Number)
	}
	if !chain.HasState(block.Root()) {
		t.Fatal("the block has no state under its root")
	}
	if chain.HasBlockAndState(block.Hash(), block.NumberU64()) {
		t.Fatal("a frozen block above the full head counts as executed")
	}
}

func TestExecutedSideChainOverCanonicalHeaderWithoutBody(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	fork := w.empties(public[3], 17, 0)
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public)
	old.Stop()
	chain := w.running(db, nil)
	if chain.HasState(public[3].Root()) {
		t.Fatal("the fork ancestor was not pruned")
	}
	headers := make([]*types.Header, len(fork))
	for i, block := range fork {
		headers[i] = block.Header()
	}
	if _, err := chain.InsertHeaderChain(headers, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := chain.InsertReceiptChain(fork[:1], make([]types.Receipts, 1), 0); err != nil {
		t.Fatal(err)
	}
	second := fork[1]
	if chain.GetCanonicalHash(second.NumberU64()) != second.Hash() || chain.HasBlock(second.Hash(), second.NumberU64()) {
		t.Fatal("the second fork block is not a canonical header without a body")
	}
	if n, err := chain.InsertChain(fork[1:]); err != nil {
		t.Fatalf("block %d: %v", n, err)
	}
	tip := fork[len(fork)-1]
	if chain.CurrentBlock().Hash() != tip.Hash() {
		t.Fatalf("head #%d, want the heavier fork at #%d", chain.CurrentBlock().Number, tip.NumberU64())
	}
	for _, block := range fork {
		if !rawdb.HasBlockExecuted(db, block.Hash(), block.NumberU64()) {
			t.Fatalf("fork #%d was adopted without running", block.NumberU64())
		}
	}
}

func TestExecutedSideChainOverCanonicalForkBlocks(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	// A second apart, each fork block is harder than the head's: heavier at the same height.
	fork := w.empties(public[3], 6, -9)
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public)
	old.Stop()
	chain := w.running(db, nil)
	if chain.HasState(public[3].Root()) {
		t.Fatal("the fork ancestor was not pruned")
	}
	w.storeBySnapSync(chain, fork)
	head, tip := public[9], fork[len(fork)-1]
	if chain.CurrentBlock().Hash() != head.Hash() || tip.NumberU64() != head.NumberU64() {
		t.Fatal("the fork tip is not at the height of the full head")
	}
	if chain.GetTd(tip.Hash(), tip.NumberU64()).Cmp(chain.GetTd(head.Hash(), head.NumberU64())) <= 0 {
		t.Fatal("the fork is not heavier than the full head")
	}
	for _, block := range fork {
		if chain.GetCanonicalHash(block.NumberU64()) != block.Hash() || !chain.HasBlock(block.Hash(), block.NumberU64()) {
			t.Fatalf("fork #%d is not a canonical block with a body", block.NumberU64())
		}
	}
	w.insert(chain, fork)
	if chain.CurrentBlock().Hash() != tip.Hash() {
		t.Fatalf("head #%d, want the heavier fork at #%d", chain.CurrentBlock().Number, tip.NumberU64())
	}
	for _, block := range fork {
		if !rawdb.HasBlockExecuted(db, block.Hash(), block.NumberU64()) {
			t.Fatalf("fork #%d was adopted without running", block.NumberU64())
		}
	}
}

func TestExecutedReimportRestoresHeadChainBody(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 10, 1)
	db := rawdb.NewMemoryDatabase()
	old := w.open(db, nil)
	w.insert(old, public)
	old.Stop()
	chain := w.running(db, nil)
	lost := public[5]
	rawdb.DeleteBody(db, lost.Hash(), lost.NumberU64())
	if chain.HasState(public[4].Root()) || chain.HasBlock(lost.Hash(), lost.NumberU64()) {
		t.Fatal("the parent state was not pruned or the body is still there")
	}
	w.insert(chain, public[5:8])
	if !chain.HasBlock(lost.Hash(), lost.NumberU64()) {
		t.Fatal("a head ancestor without a body was skipped as a canonical block")
	}
	if chain.CurrentBlock().Hash() != public[9].Hash() {
		t.Fatalf("head moved to #%d", chain.CurrentBlock().Number)
	}
}

func TestExecutedKnownForkBlockOverRewrittenIndexMovesHead(t *testing.T) {
	w := newExecutedWorld(t)
	engine := ethash.NewFullFaker()
	archive := &CacheConfig{TrieDirtyDisabled: true}
	canon, _ := GenerateChain(w.spec.Config, w.genesis, engine, w.genDb, 60, func(_ int, b *BlockGen) {
		w.key(b, common.Address{1})
	})
	side, _ := GenerateChain(w.spec.Config, canon[29], engine, w.genDb, 4, func(_ int, b *BlockGen) {
		b.OffsetTime(1)
	})
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, archive, w.spec, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.Stop)
	w.insert(chain, canon)
	w.insert(chain, side)
	// The last side block weighs one more than #50 and less than #60: a side block that
	// executes with the key, and the head stays on the canonical chain.
	td := func(block *types.Block) *big.Int { return chain.GetTd(block.Hash(), block.NumberU64()) }
	heavy, _ := GenerateChain(w.spec.Config, side[3], engine, w.genDb, 1, func(_ int, b *BlockGen) {
		b.SetDifficulty(new(big.Int).Add(new(big.Int).Sub(td(canon[49]), td(side[3])), big.NewInt(1)))
	})
	side = append(side, heavy[0])
	w.insert(chain, heavy)
	last := side[len(side)-1]
	if chain.CurrentBlock().Hash() != canon[59].Hash() || !rawdb.HasBlockExecuted(db, last.Hash(), last.NumberU64()) {
		t.Fatal("the side chain moved the head, or its last block did not run")
	}
	if err := chain.SetHead(50); err != nil {
		t.Fatal(err)
	}
	headers := make([]*types.Header, len(side))
	for i, block := range side {
		headers[i] = block.Header()
	}
	if _, err := chain.InsertHeaderChain(headers, 1); err != nil {
		t.Fatal(err)
	}
	if chain.CurrentBlock().Hash() != canon[49].Hash() || chain.GetCanonicalHash(last.NumberU64()) != last.Hash() {
		t.Fatal("the header reorg did not put the index on the side chain below the full head")
	}
	if td(last).Cmp(td(canon[49])) <= 0 {
		t.Fatal("the side chain is not heavier than the full head")
	}
	w.insert(chain, side)
	if chain.CurrentBlock().Hash() != last.Hash() {
		t.Fatalf("head #%d, want the heavier known side block #%d", chain.CurrentBlock().Number, last.NumberU64())
	}
}

func TestExecutedKnownAncestorAtWrongIndexEntryKeepsHead(t *testing.T) {
	engine := ethash.NewFaker()
	gspec := knownBlockGenesis()
	_, canon := knownBlockCanonical(t, gspec, engine)
	// Past the terminal total difficulty fork choice selects every block, so only the walk
	// keeps a known ancestor of the head from being written as the head.
	ttd := new(big.Int).Set(params.GenesisDifficulty)
	for _, block := range canon[:2] {
		ttd.Add(ttd, block.Difficulty())
	}
	gspec.Config.TerminalTotalDifficulty = ttd
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, &CacheConfig{TrieDirtyDisabled: true}, gspec, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(chain.Stop)
	if n, err := chain.InsertChain(canon); err != nil {
		t.Fatalf("block %d: %v", n, err)
	}
	head, ancestor := chain.CurrentBlock(), canon[5]
	rawdb.WriteCanonicalHash(db, common.Hash{0xde, 0xad}, ancestor.NumberU64())
	if head.Hash() != canon[7].Hash() || chain.GetCanonicalHash(head.Number.Uint64()) != head.Hash() {
		t.Fatal("the index does not agree with the head at the head's height")
	}
	if chain.onHeadChain(head, ancestor.Hash(), ancestor.NumberU64()) || !chain.isHeadAncestor(head, ancestor) {
		t.Fatal("the wrong index entry does not make onHeadChain reject a true ancestor")
	}
	if n, err := chain.InsertChain(types.Blocks{ancestor}); err != nil {
		t.Fatalf("block %d: %v", n, err)
	}
	if chain.CurrentBlock().Hash() != head.Hash() {
		t.Fatalf("head moved back to its ancestor #%d, want #%d", chain.CurrentBlock().Number, head.Number)
	}
}

func TestExecutedRewindBelowFrozenLimitFollowsFullHead(t *testing.T) {
	w := newExecutedWorld(t)
	public := w.transfers(w.genesis, 20, 1)
	db, err := rawdb.NewDatabaseWithFreezer(rawdb.NewMemoryDatabase(), t.TempDir(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	archive := &CacheConfig{TrieDirtyDisabled: true}
	old := w.open(db, archive)
	w.insert(old, public)
	freezer, ok := db.(interface{ Freeze(threshold uint64) error })
	if !ok {
		t.Fatal("the database cannot be frozen on demand")
	}
	if err := freezer.Freeze(5); err != nil {
		t.Fatal(err)
	}
	frozen, _ := db.Ancients()
	head := public[13]
	if head.NumberU64() >= frozen-1 {
		t.Fatalf("the head #%d is not below the frozen limit %d", head.NumberU64(), frozen)
	}
	fork := w.headerReorg(old, public[11], 18)
	if old.CurrentHeader().Hash() != fork[len(fork)-1].Hash() {
		t.Fatal("the header chain did not follow the fork")
	}
	old.Stop()
	rawdb.WriteHeadBlockHash(db, head.Hash())

	// The start truncates the ancient store to the head, and the restore does nothing below
	// the frozen limit, so the rewind itself has to follow the full head's ancestors and
	// not the fork headers at the same heights.
	chain := w.running(db, archive)
	if chain.CurrentBlock().Hash() != head.Hash() {
		t.Fatalf("full head #%d %x, want #%d %x", chain.CurrentBlock().Number, chain.CurrentBlock().Hash(), head.NumberU64(), head.Hash())
	}
	if !chain.HasBlockAndState(head.Hash(), head.NumberU64()) {
		t.Fatal("the full head does not count as executed")
	}
}
