// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package core

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// These tests run real test-mode PoW and a live DPoW registry check, because the fake
// modes switch the rule off. Blocks are few: each one is really mined.

// minerSigMaturity is the seconds a stake takes to mature in these tests. Maturity in
// blocks stays at the minimum, so a block far enough ahead in time can use the stake.
const minerSigMaturity = 30

// minerSigWorld is a chain with a listed miner, an unlisted one that has staked and matured
// in the registry, and a funded account that has not staked yet.
type minerSigWorld struct {
	t       *testing.T
	config  *params.ChainConfig // Runs the rule
	legacy  *params.ChainConfig // The same chain as a binary without the rule sees it
	genesis *types.Block
	genDb   ethdb.Database

	listed      common.Address // Coinbase on the signer list
	signerKey   *ecdsa.PrivateKey
	staked      common.Address // Staked coinbase that is not on the list
	stakedKey   *ecdsa.PrivateKey
	attacker    common.Address // Funded, not staked
	attackerKey *ecdsa.PrivateKey

	signer *ethash.Ethash // Holds signerKey
	miner  *ethash.Ethash // Holds stakedKey
	rogue  *ethash.Ethash // Holds attackerKey
	plain  *ethash.Ethash // Holds no key

	stopped map[*BlockChain]bool // Stop is not idempotent, and blocks when called twice
}

// minerSigTD is the total difficulty of block n while blocks stay at minimum difficulty.
func minerSigTD(n int64) *big.Int {
	return new(big.Int).Mul(params.MinimumDifficulty, big.NewInt(n+1))
}

func minerRegistrySlot(addr common.Address, base byte) common.Hash {
	var data [64]byte
	copy(data[12:32], addr[:])
	data[63] = base
	return crypto.Keccak256Hash(data[:])
}

// minerRegistryStub is a registry in which any call stakes the caller: miners[caller] = 1,
// stakeTime[caller] = timestamp, stakeBlock[caller] = number, in the real storage layout.
var minerRegistryStub = []byte{
	byte(vm.CALLER), byte(vm.PUSH1), 0, byte(vm.MSTORE),
	byte(vm.PUSH1), 0, byte(vm.PUSH1), 32, byte(vm.MSTORE),
	byte(vm.PUSH1), 1, byte(vm.PUSH1), 64, byte(vm.PUSH1), 0, byte(vm.KECCAK256), byte(vm.SSTORE),
	byte(vm.PUSH1), 1, byte(vm.PUSH1), 32, byte(vm.MSTORE),
	byte(vm.TIMESTAMP), byte(vm.PUSH1), 64, byte(vm.PUSH1), 0, byte(vm.KECCAK256), byte(vm.SSTORE),
	byte(vm.PUSH1), 2, byte(vm.PUSH1), 32, byte(vm.MSTORE),
	byte(vm.NUMBER), byte(vm.PUSH1), 64, byte(vm.PUSH1), 0, byte(vm.KECCAK256), byte(vm.SSTORE),
	byte(vm.STOP),
}

// newMinerSigWorld builds the world with the first signed block at number firstSigned.
func newMinerSigWorld(t *testing.T, firstSigned int64) *minerSigWorld {
	t.Helper()
	signerKey, _ := crypto.GenerateKey()
	stakedKey, _ := crypto.GenerateKey()
	attackerKey, _ := crypto.GenerateKey()
	w := &minerSigWorld{
		t:           t,
		listed:      common.HexToAddress("0x47d9940000000000000000000000000000000001"),
		signerKey:   signerKey,
		staked:      crypto.PubkeyToAddress(stakedKey.PublicKey),
		stakedKey:   stakedKey,
		attacker:    crypto.PubkeyToAddress(attackerKey.PublicKey),
		attackerKey: attackerKey,
		genDb:       rawdb.NewMemoryDatabase(),
		stopped:     make(map[*BlockChain]bool),
	}
	registry := common.HexToAddress("0xbe73e1F106Bd96538Be2a30F2eE94264850aFd7E")
	start := uint64(time.Now().Add(-time.Hour).Unix())
	fork := start + 1
	w.legacy = &params.ChainConfig{
		ChainID:              big.NewInt(24101),
		HomesteadBlock:       big.NewInt(0),
		EIP150Block:          big.NewInt(0),
		EIP155Block:          big.NewInt(0),
		EIP158Block:          big.NewInt(0),
		ByzantiumBlock:       big.NewInt(0),
		ConstantinopleBlock:  big.NewInt(0),
		PetersburgBlock:      big.NewInt(0),
		IstanbulBlock:        big.NewInt(0),
		BerlinBlock:          big.NewInt(0),
		LondonBlock:          big.NewInt(0),
		FeePoolBlock:         big.NewInt(0),
		FastBlock:            big.NewInt(0),
		ZeroRewardBlock:      big.NewInt(0),
		ShanghaiTime:         &fork,
		DPoWTime:             &fork,
		MinerRegistryAddress: &registry,
		DPoWMaturityTime:     minerSigMaturity,
		DPoWMaturityBlocks:   1,
		Ethash:               new(params.EthashConfig),
	}
	w.config = w.withEpochs(params.MinerSignerEpoch{
		TD:      minerSigTD(firstSigned),
		Signers: map[common.Address]common.Address{w.listed: crypto.PubkeyToAddress(signerKey.PublicKey)},
	})
	w.genesis = w.genesisSpec(w.legacy).MustCommit(w.genDb)
	rawdb.WriteTd(w.genDb, w.genesis.Hash(), 0, w.genesis.Difficulty())

	w.plain = w.engine(nil)
	w.signer = w.engine(signerKey)
	w.miner = w.engine(stakedKey)
	w.rogue = w.engine(attackerKey)
	return w
}

// engine returns a test-mode engine holding key.
func (w *minerSigWorld) engine(key *ecdsa.PrivateKey) *ethash.Ethash {
	engine := ethash.NewTester(nil, false)
	engine.AuthorizeMiner(key)
	w.t.Cleanup(func() { engine.Close() })
	return engine
}

func (w *minerSigWorld) withEpochs(epochs ...params.MinerSignerEpoch) *params.ChainConfig {
	config := *w.legacy
	config.MinerSignerEpochs = epochs
	return &config
}

// withoutRegistry builds blocks the registry would refuse, to see the importing node do it.
func (w *minerSigWorld) withoutRegistry() *params.ChainConfig {
	config := *w.config
	config.DPoWTime, config.MinerRegistryAddress = nil, nil
	return &config
}

func (w *minerSigWorld) genesisSpec(config *params.ChainConfig) *Genesis {
	one := common.BigToHash(common.Big1)
	return &Genesis{
		Config:     config,
		Timestamp:  *config.ShanghaiTime - 1,
		Difficulty: new(big.Int).Set(params.MinimumDifficulty),
		GasLimit:   30_000_000,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Alloc: GenesisAlloc{
			*config.MinerRegistryAddress: {
				Balance: common.Big1,
				Code:    minerRegistryStub,
				Storage: map[common.Hash]common.Hash{
					minerRegistrySlot(w.listed, 0): one,
					minerRegistrySlot(w.listed, 1): one,
					minerRegistrySlot(w.staked, 0): one,
					minerRegistrySlot(w.staked, 1): one,
				},
			},
			w.attacker: {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1000))},
		},
	}
}

// node starts a chain on db that runs under config.
func (w *minerSigWorld) node(db ethdb.Database, config *params.ChainConfig) *BlockChain {
	w.t.Helper()
	chain, err := NewBlockChain(db, nil, w.genesisSpec(config), nil, w.plain, vm.Config{}, nil, nil)
	if err != nil {
		w.t.Fatalf("NewBlockChain: %v", err)
	}
	w.t.Cleanup(func() { w.stop(chain) })
	return chain
}

func (w *minerSigWorld) stop(chain *BlockChain) {
	if !w.stopped[chain] {
		w.stopped[chain] = true
		chain.Stop()
	}
}

func (w *minerSigWorld) seal(engine *ethash.Ethash, block *types.Block) *types.Block {
	w.t.Helper()
	results := make(chan *types.Block, 1)
	if err := engine.Seal(nil, block, results, nil); err != nil {
		w.t.Fatalf("seal: %v", err)
	}
	select {
	case sealed := <-results:
		return sealed
	case <-time.After(2 * time.Minute):
		w.t.Fatal("seal timed out")
		return nil
	}
}

// mineWith really mines one block on parent, step seconds after it, building it under config.
func (w *minerSigWorld) mineWith(config *params.ChainConfig, engine *ethash.Ethash, parent *types.Block, step int64, coinbase common.Address, gen func(*BlockGen)) *types.Block {
	w.t.Helper()
	blocks, _ := GenerateChain(config, parent, engine, w.genDb, 1, func(_ int, b *BlockGen) {
		b.SetCoinbase(coinbase)
		b.OffsetTime(step - 10)
		if gen != nil {
			gen(b)
		}
	})
	sealed := w.seal(engine, blocks[0])
	td := new(big.Int).Add(rawdb.ReadTd(w.genDb, parent.Hash(), parent.NumberU64()), sealed.Difficulty())
	rawdb.WriteTd(w.genDb, sealed.Hash(), sealed.NumberU64(), td)
	return sealed
}

// mine really mines n empty blocks on parent.
func (w *minerSigWorld) mine(config *params.ChainConfig, engine *ethash.Ethash, parent *types.Block, n int, step int64, coinbase common.Address) []*types.Block {
	w.t.Helper()
	out := make([]*types.Block, 0, n)
	for i := 0; i < n; i++ {
		parent = w.mineWith(config, engine, parent, step, coinbase, nil)
		out = append(out, parent)
	}
	return out
}

// stake adds the attacker's stake to the block being built.
func (w *minerSigWorld) stake(b *BlockGen) {
	tx, err := types.SignNewTx(w.attackerKey, types.LatestSigner(w.config), &types.DynamicFeeTx{
		ChainID:   w.config.ChainID,
		Nonce:     b.TxNonce(w.attacker),
		To:        w.config.MinerRegistryAddress,
		Gas:       200_000,
		GasFeeCap: b.BaseFee(),
		GasTipCap: new(big.Int),
	})
	if err != nil {
		w.t.Fatal(err)
	}
	b.AddTx(tx)
}

func (w *minerSigWorld) mustInsert(chain *BlockChain, blocks []*types.Block) {
	w.t.Helper()
	if n, err := chain.InsertChain(blocks); err != nil {
		w.t.Fatalf("block %d: %v", n, err)
	}
}

func (w *minerSigWorld) wantHead(chain *BlockChain, block *types.Block) {
	w.t.Helper()
	if head := chain.CurrentBlock(); head.Hash() != block.Hash() {
		w.t.Fatalf("head #%d %x, want #%d %x", head.Number, head.Hash().Bytes()[:4], block.NumberU64(), block.Hash().Bytes()[:4])
	}
}

func wantMinerSigErr(t *testing.T, err error, reason string) {
	t.Helper()
	if !errors.Is(err, consensus.ErrInvalidMinerSignature) || !strings.Contains(err.Error(), reason) {
		t.Fatalf("got %v, want %v with %q", err, consensus.ErrInvalidMinerSignature, reason)
	}
}

// The chain crosses the threshold with the listed signer. From there on a staked coinbase
// needs its own key or the listed signer: naming one without its key no longer works.
func TestMinerSignatureAcrossThreshold(t *testing.T) {
	w := newMinerSigWorld(t, 3)
	blocks := w.mine(w.config, w.signer, w.genesis, 4, 5, w.listed)
	for i, block := range blocks {
		want := 0
		if block.NumberU64() >= 3 {
			want = crypto.SignatureLength
		}
		if have := len(block.Extra()); have != want {
			t.Fatalf("block %d: extra-data length %d, want %d", i+1, have, want)
		}
	}
	chain := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(chain, blocks)
	head := blocks[len(blocks)-1]
	w.wantHead(chain, head)

	// A staked coinbase, real PoW, no key: what worked before the rule.
	for name, coinbase := range map[string]common.Address{"listed": w.listed, "staked": w.staked} {
		forged := w.mine(w.legacy, w.plain, head, 1, 5, coinbase)
		_, err := chain.InsertChain(forged)
		if !errors.Is(err, consensus.ErrInvalidMinerSignature) {
			t.Fatalf("%s coinbase without a signature: %v", name, err)
		}
		if chain.HasBlock(forged[0].Hash(), forged[0].NumberU64()) {
			t.Fatalf("%s coinbase without a signature: the block was stored", name)
		}
	}
	// A listed coinbase signed by anyone else, here by a miner that has its own stake.
	wrongKey := w.withEpochs(params.MinerSignerEpoch{
		TD:      w.config.MinerSignerEpochs[0].TD,
		Signers: map[common.Address]common.Address{w.listed: w.staked},
	})
	_, err := chain.InsertChain(w.mine(wrongKey, w.miner, head, 1, 5, w.listed))
	wantMinerSigErr(t, err, "signed by")

	// A fresh key is enough to sign for its own address. The registry stops that one.
	unstaked := w.mine(w.withoutRegistry(), w.rogue, head, 1, 5, w.attacker)
	if _, err := chain.InsertChain(unstaked); !errors.Is(err, consensus.ErrUnauthorizedMiner) {
		t.Fatalf("unstaked coinbase with its own key: %v", err)
	}
	w.wantHead(chain, head)

	// The listed signer carries on, the staked miner joins with its own key, and a node
	// syncing from scratch accepts all of it.
	more := w.mine(w.config, w.signer, head, 1, 5, w.listed)
	more = append(more, w.mine(w.config, w.miner, more[0], 1, 5, w.staked)...)
	w.mustInsert(chain, more)
	w.wantHead(chain, more[1])

	fresh := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(fresh, append(blocks, more...))
	w.wantHead(fresh, more[1])
}

// Only a listed signer can make the block that crosses the threshold, however staked the
// miner trying.
func TestMinerSignatureCrossing(t *testing.T) {
	w := newMinerSigWorld(t, 2)
	before := w.mine(w.config, w.signer, w.genesis, 1, 5, w.listed)
	chain := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(chain, before)

	listsMiner := w.withEpochs(params.MinerSignerEpoch{
		TD:      w.config.MinerSignerEpochs[0].TD,
		Signers: map[common.Address]common.Address{w.staked: w.staked},
	})
	_, err := chain.InsertChain(w.mine(listsMiner, w.miner, before[0], 1, 5, w.staked))
	wantMinerSigErr(t, err, "cannot cross the threshold")
	w.wantHead(chain, before[0])

	crossing := w.mine(w.config, w.signer, before[0], 1, 5, w.listed)
	w.mustInsert(chain, crossing)
	w.wantHead(chain, crossing[0])
}

// Anyone can join: a stake in the public chain lets a coinbase mine with its own key once
// it has matured, and not before. The maturity period is the time everybody has to see it.
func TestMinerSignatureOpenEntry(t *testing.T) {
	w := newMinerSigWorld(t, 2)
	blocks := w.mine(w.config, w.signer, w.genesis, 2, 5, w.listed)
	blocks = append(blocks, w.mineWith(w.config, w.signer, blocks[1], 5, w.listed, w.stake))
	head := blocks[len(blocks)-1]

	chain := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(chain, blocks)

	// Too early: the signature is fine, the registry says wait.
	early := w.mine(w.withoutRegistry(), w.rogue, head, 1, minerSigMaturity-1, w.attacker)
	if _, err := chain.InsertChain(early); !errors.Is(err, consensus.ErrMinerNotMature) {
		t.Fatalf("before maturity: %v", err)
	}
	w.wantHead(chain, head)

	mature := w.mine(w.config, w.rogue, head, 1, minerSigMaturity, w.attacker)
	w.mustInsert(chain, mature)
	w.wantHead(chain, mature[0])
}

// A stake made on a private fork never gives the public chain the maturity period to see
// it. The fork needs no signatures below the threshold, so the stake can mature there. What
// stops it is the crossing: without a listed signer the fork stays below the threshold.
func TestMinerSignatureStakeOnPrivateFork(t *testing.T) {
	w := newMinerSigWorld(t, 4)
	public := w.mine(w.config, w.signer, w.genesis, 6, 5, w.listed)
	head := public[len(public)-1]

	// The attacker's whole fork: a stake under a staked coinbase with no key, which is
	// fine below the threshold, then maturity, then his own key from there on.
	fork := []*types.Block{w.mineWith(w.legacy, w.plain, public[0], 1, w.listed, w.stake)}
	fork = append(fork, w.mine(w.legacy, w.plain, fork[0], 1, minerSigMaturity, w.listed)...)

	// Building it needs a configuration that lets him cross, which is the point.
	ownKey := w.withEpochs(params.MinerSignerEpoch{
		TD: w.config.MinerSignerEpochs[0].TD,
		Signers: map[common.Address]common.Address{
			w.listed:   w.config.MinerSignerEpochs[0].Signers[w.listed],
			w.attacker: w.attacker,
		},
	})
	signed := w.mine(ownKey, w.rogue, fork[len(fork)-1], 6, 1, w.attacker)

	// Were he allowed across, the fork would win: the stake is valid in its own state.
	allowed := w.node(rawdb.NewMemoryDatabase(), ownKey)
	w.mustInsert(allowed, public)
	w.mustInsert(allowed, append(fork, signed...))
	w.wantHead(allowed, signed[len(signed)-1])

	// Under the rule the fork stays below the threshold and the public chain stays.
	chain := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(chain, public)
	_, err := chain.InsertChain(append(fork, signed...))
	wantMinerSigErr(t, err, "cannot cross the threshold")
	w.wantHead(chain, head)
	if chain.HasBlock(signed[0].Hash(), signed[0].NumberU64()) {
		t.Fatal("the block crossing the threshold was stored")
	}

	// Past the threshold there is no such fork: its first block already needs a key that
	// is mature in the state it forks from.
	late := w.mine(w.withoutRegistry(), w.rogue, public[4], 1, 1, w.attacker)
	if _, err := chain.InsertChain(late); !errors.Is(err, consensus.ErrUnauthorizedMiner) {
		t.Fatalf("a fork past the threshold by a new key: %v", err)
	}
	w.wantHead(chain, head)
}

// spend changes the state in the block being built, so that block has a root of its own.
func (w *minerSigWorld) spend(b *BlockGen) {
	tx, err := types.SignNewTx(w.attackerKey, types.LatestSigner(w.config), &types.DynamicFeeTx{
		ChainID:   w.config.ChainID,
		Nonce:     b.TxNonce(w.attacker),
		To:        &common.Address{0xaa},
		Value:     common.Big1,
		Gas:       params.TxGas,
		GasFeeCap: b.BaseFee(),
		GasTipCap: new(big.Int),
	})
	if err != nil {
		w.t.Fatal(err)
	}
	b.AddTx(tx)
}

// claimingRoot really mines an empty block on parent, signed by the attacker, that claims
// root as its state. Executing it would show that it does not lead there.
func (w *minerSigWorld) claimingRoot(parent *types.Block, root common.Hash) *types.Block {
	w.t.Helper()
	statedb, err := state.New(parent.Root(), state.NewDatabase(w.genDb), nil)
	if err != nil {
		w.t.Fatal(err)
	}
	baseFee, err := misc.CalcBaseFee(w.config, parent.Header(), statedb)
	if err != nil {
		w.t.Fatal(err)
	}
	timestamp := parent.Time() + 1
	header := &types.Header{
		ParentHash:  parent.Hash(),
		UncleHash:   types.EmptyUncleHash,
		Coinbase:    w.attacker,
		Root:        root,
		TxHash:      types.EmptyRootHash,
		ReceiptHash: types.EmptyRootHash,
		Difficulty:  w.plain.CalcDifficulty(&fakeChainReader{config: w.config, db: w.genDb}, timestamp, parent.Header()),
		Number:      new(big.Int).Add(parent.Number(), common.Big1),
		GasLimit:    parent.GasLimit(),
		Time:        timestamp,
		BaseFee:     baseFee,
	}
	digest, err := rlp.EncodeToBytes([]interface{}{[]byte("incentiv/miner-header-sig/v1"), w.config.ChainID, w.plain.SealHash(header)})
	if err != nil {
		w.t.Fatal(err)
	}
	if header.Extra, err = crypto.Sign(crypto.Keccak256(digest), w.attackerKey); err != nil {
		w.t.Fatal(err)
	}
	sealed := w.seal(w.rogue, types.NewBlockWithHeader(header))
	td := new(big.Int).Add(rawdb.ReadTd(w.genDb, parent.Hash(), parent.NumberU64()), sealed.Difficulty())
	rawdb.WriteTd(w.genDb, sealed.Hash(), sealed.NumberU64(), td)
	return sealed
}

// Past the threshold any key can sign for its own address, so only execution stops one that
// is not in the registry. A pruned node stores a block with pruned ancestors unexecuted, and
// the state under the root such a block names can be there without the block having led to it.
func TestMinerSignatureUnexecutedForksOnPrunedNode(t *testing.T) {
	if raceEnabled {
		t.Skip("too slow under the race detector, TestSideBlockClaimingAnotherState takes the same path")
	}
	w := newMinerSigWorld(t, 3)
	public := make([]*types.Block, 0, TriesInMemory+12)
	parent := w.genesis
	for i := 0; i < TriesInMemory+12; i++ {
		parent = w.mineWith(w.config, w.signer, parent, 5, w.listed, w.spend)
		public = append(public, parent)
	}
	head, base := public[len(public)-1], public[11]
	target := rawdb.ReadTd(w.genDb, head.Hash(), head.NumberU64())

	// The attacker's own key on top of tip, until that outweighs the public chain.
	outweigh := func(config *params.ChainConfig, tip *types.Block) []*types.Block {
		var fork []*types.Block
		for rawdb.ReadTd(w.genDb, tip.Hash(), tip.NumberU64()).Cmp(target) <= 0 {
			tip = w.mineWith(config, w.rogue, tip, 1, w.attacker, nil)
			fork = append(fork, tip)
		}
		return fork
	}
	prunedNode := func() *BlockChain {
		chain := w.node(rawdb.NewMemoryDatabase(), w.config)
		w.mustInsert(chain, public)
		if chain.HasState(base.Root()) {
			t.Fatalf("the state of #%d is still there", base.NumberU64())
		}
		return chain
	}
	wantRefused := func(name string, chain *BlockChain, blocks []*types.Block) {
		t.Helper()
		if _, err := chain.InsertChain(blocks); !errors.Is(err, consensus.ErrUnauthorizedMiner) {
			t.Fatalf("%s: %v", name, err)
		}
		w.wantHead(chain, head)
	}
	// Block rewards are zero, so an empty block keeps its parent's root. Once the node has
	// executed the base on its way to the fork, every block of the fork seems to have its
	// state. This takes no false root and no stake, only a key that is in no registry.
	empty := outweigh(w.withoutRegistry(), base)
	if empty[0].Root() != base.Root() {
		t.Fatal("the empty block has a root of its own")
	}
	wantRefused("empty blocks on a pruned node", prunedNode(), empty)

	// A stake below the threshold, on the genesis state, under a staked coinbase and without
	// a key. The node executes this side block and keeps its state.
	pruned := prunedNode()
	stake := w.mineWith(w.legacy, w.plain, w.genesis, 1, w.listed, w.stake)
	w.mustInsert(pruned, []*types.Block{stake})
	w.wantHead(pruned, head)
	if !pruned.HasState(stake.Root()) {
		t.Fatal("the state of the side block is gone")
	}
	// Past the threshold, on a block with pruned state, a block claims that state. Its
	// children would run on a state in which the attacker has a mature stake nobody saw.
	claiming := w.claimingRoot(base, stake.Root())
	w.mustInsert(pruned, []*types.Block{claiming})
	if !pruned.HasBlock(claiming.Hash(), claiming.NumberU64()) {
		t.Fatal("the block was not stored as a side block")
	}
	if pruned.HasBlockAndState(claiming.Hash(), claiming.NumberU64()) {
		t.Fatal("a block that was never executed counts as having its state")
	}
	// Mining on the claimed state works, so only the importing node can stop this.
	claimed := outweigh(w.config, claiming)
	wantRefused("a claimed state on a pruned node", pruned, claimed)
	wantRefused("a claimed state delivered again", pruned, claimed)

	withEntry := prunedNode()
	w.mustInsert(withEntry, []*types.Block{stake})
	w.mustInsert(withEntry, []*types.Block{claiming})
	rawdb.WriteReceipts(withEntry.db, claiming.Hash(), claiming.NumberU64(), nil)
	if withEntry.HasBlockAndState(claiming.Hash(), claiming.NumberU64()) {
		t.Fatal("an empty receipts entry counts as proof that the block ran")
	}
	wantRefused("a claimed state with an empty receipts entry", withEntry, claimed)

	// A node that executes every side block, and one that never had that state, say the same.
	archive, err := NewBlockChain(rawdb.NewMemoryDatabase(), &CacheConfig{TrieDirtyDisabled: true}, w.genesisSpec(w.config), nil, w.plain, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Stop()
	w.mustInsert(archive, public)
	w.mustInsert(archive, []*types.Block{stake})
	wantRefused("a claimed state on an archive node", archive, append([]*types.Block{claiming}, claimed...))
	wantRefused("a claimed state on a node without it", prunedNode(), append([]*types.Block{claiming}, claimed...))
}

// Block rewards are zero, so an empty block and its empty sibling share a root. A pruned node
// that has lost that state has to take a heavier fork starting with such a block, as every
// other node does, and it still has to run a block that only claims a canonical root.
func TestMinerSignatureSameRootOnPrunedNode(t *testing.T) {
	w := newMinerSigWorld(t, 2)
	// Eight public blocks that all change the state, except the one at index empty if any.
	publicChain := func(empty int) []*types.Block {
		blocks := make([]*types.Block, 0, 8)
		parent := w.genesis
		for i := 0; i < 8; i++ {
			gen := w.spend
			if i == empty {
				gen = nil
			}
			parent = w.mineWith(w.config, w.signer, parent, 5, w.listed, gen)
			blocks = append(blocks, parent)
		}
		return blocks
	}
	// A restart keeps the state of the head and of the block before it, and no other nearby.
	restarted := func(public []*types.Block, pruned *types.Block) *BlockChain {
		db := rawdb.NewMemoryDatabase()
		old := w.node(db, w.config)
		w.mustInsert(old, public)
		w.stop(old)
		chain := w.node(db, w.config)
		w.wantHead(chain, public[len(public)-1])
		if chain.HasState(pruned.Root()) {
			t.Fatalf("the state of #%d is still there", pruned.NumberU64())
		}
		return chain
	}
	public := publicChain(5)
	base, empty := public[4], public[5]
	fork := w.mine(w.config, w.miner, base, 6, 1, w.staked)
	if fork[0].Root() != empty.Root() || empty.Root() != base.Root() {
		t.Fatal("the empty blocks have roots of their own")
	}
	chain := restarted(public, base)
	w.mustInsert(chain, fork)
	w.wantHead(chain, fork[5])

	// A block that claims the canonical root at its height, a state the node kept.
	public = publicChain(-1)
	claiming := w.claimingRoot(public[5], public[6].Root())
	lying := append([]*types.Block{claiming}, w.mine(w.withoutRegistry(), w.rogue, claiming, 4, 1, w.attacker)...)
	chain = restarted(public, public[5])
	if !chain.HasState(claiming.Root()) {
		t.Fatal("the claimed state is gone")
	}
	if _, err := chain.InsertChain(lying); !errors.Is(err, consensus.ErrUnauthorizedMiner) {
		t.Fatalf("a claimed canonical root: %v", err)
	}
	w.wantHead(chain, public[7])
}

// A fork without a listed key stays below the threshold. It can never outweigh a chain past
// it, whatever order a node sees them in. An honest signature on top of it can, on a fork of
// far higher difficulty; at the difficulties here it does not.
func TestMinerSignatureForkStaysBelowThreshold(t *testing.T) {
	w := newMinerSigWorld(t, 4)
	public := w.mine(w.config, w.signer, w.genesis, 6, 5, w.listed)
	head := public[len(public)-1]

	// As many 1 second blocks as fit below the threshold, each heavier than a public one.
	fork := w.mine(w.legacy, w.plain, public[0], 2, 1, w.listed)
	tip := fork[len(fork)-1]
	beyond := w.mine(w.legacy, w.plain, tip, 1, 1, w.listed)

	onPublic := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(onPublic, public)
	w.mustInsert(onPublic, fork)
	w.wantHead(onPublic, head)
	_, err := onPublic.InsertChain(beyond)
	wantMinerSigErr(t, err, "extra-data length 0")
	if onPublic.HasBlock(beyond[0].Hash(), beyond[0].NumberU64()) {
		t.Fatal("the block crossing the threshold unsigned was stored")
	}
	// A listed signer whose head sat on the fork signs on top of it. That block is valid, but
	// at these difficulties it does not make up for the public chain's work above the
	// threshold. On a fork of far higher difficulty it can, for a chain a few blocks past it.
	laundered := w.mine(w.config, w.signer, tip, 1, 1, w.listed)
	w.mustInsert(onPublic, laundered)
	w.wantHead(onPublic, head)

	// A node that sees the fork first takes it, and moves over once the public chain arrives.
	onFork := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(onFork, public[:1])
	w.mustInsert(onFork, fork)
	w.wantHead(onFork, tip)
	w.mustInsert(onFork, public[1:])
	w.wantHead(onFork, head)
}

// The rule does not depend on sampling: the header chain checks the signature on the headers
// whose seal it skips.
func TestMinerSignatureHeaderChain(t *testing.T) {
	w := newMinerSigWorld(t, 2)
	blocks := w.mine(w.config, w.signer, w.genesis, 3, 5, w.listed)
	forged := w.mine(w.legacy, w.plain, blocks[0], 1, 5, w.listed)

	// A checkFreq of 0 skips every seal. Any other value always checks the last header, which
	// here is the forged one.
	headers := []*types.Header{blocks[0].Header(), forged[0].Header()}
	chain := w.node(rawdb.NewMemoryDatabase(), w.config)
	_, err := chain.InsertHeaderChain(headers, 0)
	wantMinerSigErr(t, err, "extra-data length 0")

	headers = nil
	for _, block := range blocks {
		headers = append(headers, block.Header())
	}
	if _, err := chain.InsertHeaderChain(headers, 100); err != nil {
		t.Fatalf("valid header chain: %v", err)
	}
}

// Uncle headers follow the rule at their own total difficulty.
func TestMinerSignatureUncles(t *testing.T) {
	w := newMinerSigWorld(t, 3)
	blocks := w.mine(w.config, w.signer, w.genesis, 4, 5, w.listed)
	chain := w.node(rawdb.NewMemoryDatabase(), w.config)
	w.mustInsert(chain, blocks)

	verify := func(parent *types.Block, uncle *types.Header) error {
		header := &types.Header{ParentHash: parent.Hash(), Number: new(big.Int).Add(parent.Number(), common.Big1)}
		return w.plain.VerifyUncles(chain, types.NewBlockWithHeader(header).WithBody(nil, []*types.Header{uncle}))
	}
	// Siblings of #2 (below the threshold) and #3 (the first one past it). A different
	// timestamp makes them different blocks at the same difficulty.
	below := w.mine(w.legacy, w.plain, blocks[0], 1, 6, w.listed)[0]
	if err := verify(blocks[2], below.Header()); err != nil {
		t.Fatalf("unsigned uncle below the threshold: %v", err)
	}
	unsigned := w.mine(w.legacy, w.plain, blocks[1], 1, 6, w.listed)[0]
	wantMinerSigErr(t, verify(blocks[3], unsigned.Header()), "extra-data length 0")

	signed := w.mine(w.config, w.signer, blocks[1], 1, 6, w.listed)[0]
	if err := verify(blocks[3], signed.Header()); err != nil {
		t.Fatalf("signed uncle: %v", err)
	}
	// A wrong number is reported as that, as it is with the rule off, and not as an unknown
	// ancestor because the total difficulty was looked up under it. The cache of total
	// difficulties goes by hash alone, so it has to be cold for the number to matter.
	wrong := types.CopyHeader(signed.Header())
	wrong.Number = new(big.Int).Add(wrong.Number, common.Big1)
	chain.hc.tdCache.Purge()
	if err := verify(blocks[3], wrong); !errors.Is(err, consensus.ErrInvalidNumber) {
		t.Fatalf("uncle with a wrong number: got %v, want %v", err, consensus.ErrInvalidNumber)
	}
}

// A later epoch changes who signs from its threshold on, shuts a coinbase out by listing a
// signer nobody holds, and leaves the blocks before it valid for a node syncing from scratch.
func TestMinerSignerRotation(t *testing.T) {
	w := newMinerSigWorld(t, 2)
	rotatedKey, _ := crypto.GenerateKey()
	rotated := w.engine(rotatedKey)

	first := w.config.MinerSignerEpochs[0]
	config := w.withEpochs(first, params.MinerSignerEpoch{
		TD: minerSigTD(5),
		Signers: map[common.Address]common.Address{
			w.listed: crypto.PubkeyToAddress(rotatedKey.PublicKey),
			w.staked: common.HexToAddress("0x000000000000000000000000000000000000dEaD"),
		},
	})
	// Before the threshold the unlisted miner still works, with its own key.
	blocks := w.mine(config, w.signer, w.genesis, 3, 5, w.listed)
	blocks = append(blocks, w.mine(config, w.miner, blocks[2], 1, 5, w.staked)...)
	next := w.mine(config, rotated, blocks[3], 2, 5, w.listed)

	chain := w.node(rawdb.NewMemoryDatabase(), config)
	w.mustInsert(chain, blocks)

	// Built under the old list, as the holder of a retired key would.
	_, err := chain.InsertChain(w.mine(w.config, w.signer, blocks[3], 1, 5, w.listed))
	wantMinerSigErr(t, err, "signed by")
	_, err = chain.InsertChain(w.mine(w.config, w.miner, blocks[3], 1, 5, w.staked))
	wantMinerSigErr(t, err, "signed by")

	w.mustInsert(chain, next)
	w.wantHead(chain, next[1])
	_, err = chain.InsertChain(w.mine(w.config, w.miner, next[1], 1, 5, w.staked))
	wantMinerSigErr(t, err, "signed by")

	fresh := w.node(rawdb.NewMemoryDatabase(), config)
	w.mustInsert(fresh, append(blocks, next...))
	w.wantHead(fresh, next[1])
}

// A node that crossed the threshold on a binary without the rule sits on a chain the network
// rejects, possibly a heavier one. Starting the new binary takes it off that chain.
func TestMinerSignatureRewindsLateUpgrade(t *testing.T) {
	w := newMinerSigWorld(t, 3)
	public := w.mine(w.config, w.signer, w.genesis, 4, 5, w.listed)
	stale := w.mine(w.legacy, w.plain, w.genesis, 6, 5, w.listed)

	db := rawdb.NewMemoryDatabase()
	old := w.node(db, w.legacy)
	w.mustInsert(old, stale)
	w.wantHead(old, stale[len(stale)-1])
	w.stop(old)

	// Just below the threshold, and no further: these empty blocks all have the genesis root,
	// so the state of #2 is there.
	upgraded := w.node(db, w.config)
	w.wantHead(upgraded, stale[1])
	w.mustInsert(upgraded, public)
	w.wantHead(upgraded, public[len(public)-1])

	// A valid chain past the threshold stays where it is across a restart.
	w.stop(upgraded)
	restarted := w.node(db, w.config)
	w.wantHead(restarted, public[len(public)-1])
}

// A head that is itself the bad block crossing the threshold comes off too.
func TestMinerSignatureRewindsHeadAtThreshold(t *testing.T) {
	w := newMinerSigWorld(t, 3)
	stale := w.mine(w.legacy, w.plain, w.genesis, 3, 5, w.listed)

	db := rawdb.NewMemoryDatabase()
	old := w.node(db, w.legacy)
	w.mustInsert(old, stale)
	w.wantHead(old, stale[2])
	w.stop(old)

	upgraded := w.node(db, w.config)
	w.wantHead(upgraded, stale[1])
}

// A later epoch rotates a signer. A node that followed the retired key across its threshold
// comes off there, although its head is fine under both lists: only the first block tells.
func TestMinerSignatureRewindsAtLaterEpoch(t *testing.T) {
	w := newMinerSigWorld(t, 2)
	rotatedKey, _ := crypto.GenerateKey()
	rotated := w.withEpochs(w.config.MinerSignerEpochs[0], params.MinerSignerEpoch{
		TD:      minerSigTD(4),
		Signers: map[common.Address]common.Address{w.listed: crypto.PubkeyToAddress(rotatedKey.PublicKey)},
	})
	blocks := w.mine(w.config, w.signer, w.genesis, 4, 5, w.listed)
	blocks = append(blocks, w.mine(w.config, w.miner, blocks[3], 2, 5, w.staked)...)

	db := rawdb.NewMemoryDatabase()
	old := w.node(db, w.config)
	w.mustInsert(old, blocks)
	w.wantHead(old, blocks[5])
	w.stop(old)

	upgraded := w.node(db, rotated)
	w.wantHead(upgraded, blocks[2])
}

// A crash on the old binary loses the state past the threshold. The repair at startup then
// takes the block head back but not the header head, which still names the invalid chain and
// its total difficulty. The check has to see that too.
func TestMinerSignatureRewindsHeaderHeadAfterCrash(t *testing.T) {
	w := newMinerSigWorld(t, 3)
	public := w.mine(w.config, w.signer, w.genesis, 4, 5, w.listed)
	stale := make([]*types.Block, 0, 6)
	parent := w.genesis
	for i := 0; i < 6; i++ {
		parent = w.mineWith(w.legacy, w.plain, parent, 5, w.listed, w.spend)
		stale = append(stale, parent)
	}
	db := rawdb.NewMemoryDatabase()
	old := w.node(db, w.legacy)
	w.mustInsert(old, stale)

	// Nothing is flushed, as after a crash.
	old.stopWithoutSaving()
	w.stopped[old] = true

	upgraded := w.node(db, w.config)
	if have := upgraded.CurrentBlock().Number.Uint64(); have != 0 {
		t.Fatalf("block head #%d, want the genesis block: no state was flushed", have)
	}
	if have := upgraded.CurrentHeader().Number.Uint64(); have != 2 {
		t.Fatalf("header head #%d, want #2 below the threshold", have)
	}
	if upgraded.GetHeaderByNumber(3) != nil {
		t.Fatal("the invalid chain is still canonical past the threshold")
	}
	w.mustInsert(upgraded, public)
	w.wantHead(upgraded, public[len(public)-1])
}

// A start rewinds along the canonical chain, so a side block that crossed on a binary without the
// rule stays stored with its state. Sent again it is refused, but its child comes with a good
// signature of its own, and accepting that child would put the block back on the canonical chain.
func TestMinerSignatureStoredSideBlock(t *testing.T) {
	w := newMinerSigWorld(t, 100)
	// One second apart these weigh a little more than the canonical blocks, so the side chain
	// crosses at #3 and the canonical chain at #4.
	side := w.mine(w.legacy, w.plain, w.genesis, 3, 1, w.listed)
	canon := w.mine(w.legacy, w.plain, w.genesis, 6, 5, w.listed)
	config := w.withEpochs(params.MinerSignerEpoch{
		TD:      rawdb.ReadTd(w.genDb, side[2].Hash(), side[2].NumberU64()),
		Signers: map[common.Address]common.Address{w.listed: crypto.PubkeyToAddress(w.signerKey.PublicKey)},
	})
	db := rawdb.NewMemoryDatabase()
	old := w.node(db, w.legacy)
	w.mustInsert(old, side)
	w.mustInsert(old, canon)
	w.wantHead(old, canon[5])
	w.stop(old)

	chain := w.node(db, config)
	w.wantHead(chain, canon[2])
	if !chain.HasBlockAndState(side[2].Hash(), side[2].NumberU64()) {
		t.Fatal("the side block is gone, which tests nothing")
	}
	child := w.mine(config, w.miner, side[2], 1, 5, w.staked)
	_, err := chain.InsertChain(child)
	wantMinerSigErr(t, err, "extra-data length 0")
	wantMinerSigErr(t, w.plain.VerifyHeader(chain, child[0].Header(), true), "extra-data length 0")
	w.wantHead(chain, canon[2])

	// A valid block as a stored parent is no obstacle, whether it crossed or not.
	honest := w.mine(config, w.signer, canon[2], 3, 5, w.listed)
	for _, block := range honest {
		w.mustInsert(chain, []*types.Block{block})
	}
	w.wantHead(chain, honest[2])
}

func TestOverrideMinerSignerTD(t *testing.T) {
	signers := map[common.Address]common.Address{{1}: {2}}
	first, second := big.NewInt(100), big.NewInt(200)
	config := &params.ChainConfig{ChainID: common.Big1, Ethash: new(params.EthashConfig), MinerSignerEpochs: []params.MinerSignerEpoch{
		{TD: new(big.Int).Set(first), Signers: signers},
		{TD: new(big.Int).Set(second), Signers: signers},
	}}

	moved := uint64(150)
	got := (&ChainOverrides{OverrideMinerSignerTD: &moved}).apply(config)
	if have := got.MinerSignerEpochs[0].TD.Uint64(); have != moved {
		t.Fatalf("first epoch at %d, want %d", have, moved)
	}
	if got.MinerSignerEpochs[1].TD.Cmp(second) != 0 || len(got.MinerSignerEpochs[0].Signers) != 1 {
		t.Fatalf("the override changed more than the first threshold: %+v", got.MinerSignerEpochs)
	}
	// The configs it is handed are shared, so it must not write through.
	if config.MinerSignerEpochs[0].TD.Cmp(first) != 0 {
		t.Fatalf("the override wrote into the bundled config: %v", config.MinerSignerEpochs[0].TD)
	}
	// Past the next epoch the order breaks, which the startup checks turn into a failed start.
	past := uint64(300)
	db := rawdb.NewMemoryDatabase()
	_, _, err := SetupGenesisBlockWithOverride(db, trie.NewDatabase(db), &Genesis{Config: config}, &ChainOverrides{OverrideMinerSignerTD: &past})
	if err == nil || !strings.Contains(err.Error(), "does not follow") {
		t.Fatalf("an override past the next epoch did not fail the start: %v", err)
	}
	// The same order in a genesis specification, which is how a release would carry it.
	bad := *config
	bad.MinerSignerEpochs = []params.MinerSignerEpoch{{TD: second, Signers: signers}, {TD: first, Signers: signers}}
	db = rawdb.NewMemoryDatabase()
	_, _, err = SetupGenesisBlockWithOverride(db, trie.NewDatabase(db), &Genesis{Config: &bad}, nil)
	if err == nil || !strings.Contains(err.Error(), "does not follow") {
		t.Fatalf("a genesis specification with epochs out of order did not fail the start: %v", err)
	}
	// A chain without a list has nothing to move, and must not gain an epoch.
	if got := (&ChainOverrides{OverrideMinerSignerTD: &moved}).apply(new(params.ChainConfig)); len(got.MinerSignerEpochs) != 0 {
		t.Fatalf("the override created epochs: %+v", got.MinerSignerEpochs)
	}
}

// A start resolves the config more than once on an Incentiv chain, and an override with nothing
// to move is still one thing to say. The chain here ships no epochs, whatever mainnet ships.
func TestMinerSignerOverrideWithoutEpochsWarnsOnce(t *testing.T) {
	const warning = "Ignoring --override.minersignertd"
	held := uint64(2_000_000)
	overrides := &ChainOverrides{OverrideMinerSignerTD: &held}
	shipped := params.IncentivMainnetChainConfig.MinerSignerEpochs
	params.IncentivMainnetChainConfig.MinerSignerEpochs = nil
	defer func() { params.IncentivMainnetChainConfig.MinerSignerEpochs = shipped }()

	start := func(t *testing.T, stored []params.MinerSignerEpoch, genesis *Genesis, setup bool) int {
		t.Helper()
		db := rawdb.NewMemoryDatabase()
		tdb := trie.NewDatabase(db)
		if setup {
			block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
			if err != nil {
				t.Fatal(err)
			}
			config := *params.IncentivMainnetChainConfig
			config.MinerSignerEpochs = stored
			rawdb.WriteChainConfig(db, block.Hash(), &config)
			writeSyntheticHead(t, db, 1, 1)
		}
		logged := captureWarnings(t)
		if _, _, err := SetupGenesisBlockWithOverride(db, tdb, genesis, overrides); err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, line := range logged() {
			if strings.Contains(line, warning) {
				count++
			}
		}
		return count
	}
	t.Run("stored chain", func(t *testing.T) {
		if have := start(t, nil, nil, true); have != 1 {
			t.Fatalf("%d warnings", have)
		}
	})
	t.Run("under the network flag", func(t *testing.T) {
		if have := start(t, nil, DefaultIncentivMainnetGenesisBlock(), true); have != 1 {
			t.Fatalf("%d warnings", have)
		}
	})
	t.Run("fresh database", func(t *testing.T) {
		if have := start(t, nil, DefaultIncentivMainnetGenesisBlock(), false); have != 1 {
			t.Fatalf("%d warnings", have)
		}
	})
	t.Run("a chain with epochs", func(t *testing.T) {
		stored := []params.MinerSignerEpoch{{TD: big.NewInt(1_000_000), Signers: map[common.Address]common.Address{{1}: {2}}}}
		shipped := params.IncentivMainnetChainConfig.MinerSignerEpochs
		params.IncentivMainnetChainConfig.MinerSignerEpochs = stored
		defer func() { params.IncentivMainnetChainConfig.MinerSignerEpochs = shipped }()
		if have := start(t, stored, nil, true); have != 0 {
			t.Fatalf("%d warnings", have)
		}
	})
}

// A node that drops the override it was running with moves back to the bundled threshold. With
// its head below that value nothing rewinds, and the node leaves the fleet only when the chain
// gets there, so the log is the one place that can say so in time.
func TestMinerSignerThresholdMoveWarns(t *testing.T) {
	const warning = "Stored miner signer epochs replaced"
	signers := map[common.Address]common.Address{{1}: {2}}
	others := map[common.Address]common.Address{{1}: {3}}
	epochs := func(td int64) []params.MinerSignerEpoch {
		return []params.MinerSignerEpoch{{TD: big.NewInt(td), Signers: signers}}
	}
	bundled, held := int64(1_000_000), uint64(2_000_000)
	flag := &ChainOverrides{OverrideMinerSignerTD: &held}
	withLater := func(list map[common.Address]common.Address) []params.MinerSignerEpoch {
		return append(epochs(bundled), params.MinerSignerEpoch{TD: big.NewInt(3_000_000), Signers: list})
	}
	moved := []string{fmt.Sprintf("epoch=0 stored=%d threshold=%d", held, bundled)}

	shipped := params.IncentivMainnetChainConfig.MinerSignerEpochs
	defer func() { params.IncentivMainnetChainConfig.MinerSignerEpochs = shipped }()

	for _, test := range []struct {
		name      string
		stored    []params.MinerSignerEpoch
		ships     []params.MinerSignerEpoch // epochs(bundled) when left out
		genesis   *Genesis
		overrides *ChainOverrides
		td        uint64
		warns     []string // What the warning names, or nil if there is none
	}{
		{name: "flag dropped", stored: epochs(int64(held)), td: uint64(bundled), warns: moved},
		{name: "flag dropped under the network flag", stored: epochs(int64(held)), genesis: DefaultIncentivMainnetGenesisBlock(), td: uint64(bundled), warns: moved},
		{name: "flag kept", stored: epochs(int64(held)), overrides: flag, td: held},
		{name: "flag first passed", stored: epochs(bundled), overrides: flag, td: held},
		{name: "same value", stored: epochs(bundled), td: uint64(bundled)},
		{name: "first release with epochs", td: uint64(bundled)},
		{name: "signers changed", stored: []params.MinerSignerEpoch{{TD: big.NewInt(bundled), Signers: others}}, td: uint64(bundled), warns: []string{"epoch=0"}},
		{name: "signers changed with the flag kept", stored: []params.MinerSignerEpoch{{TD: new(big.Int).SetUint64(held), Signers: others}}, overrides: flag, td: held, warns: []string{"epoch=0"}},
		{name: "later epoch changed", stored: withLater(signers), ships: withLater(others), td: uint64(bundled), warns: []string{"epoch=1"}},
		{name: "later epoch removed", stored: withLater(signers), td: uint64(bundled), warns: []string{"epoch=1", "stored=3000000", "threshold=<nil>"}},
		{name: "epoch appended", stored: epochs(bundled), ships: withLater(signers), td: uint64(bundled)},
	} {
		t.Run(test.name, func(t *testing.T) {
			params.IncentivMainnetChainConfig.MinerSignerEpochs = epochs(bundled)
			if test.ships != nil {
				params.IncentivMainnetChainConfig.MinerSignerEpochs = test.ships
			}
			db := rawdb.NewMemoryDatabase()
			tdb := trie.NewDatabase(db)
			block, err := DefaultIncentivMainnetGenesisBlock().Commit(db, tdb)
			if err != nil {
				t.Fatal(err)
			}
			stored := *params.IncentivMainnetChainConfig
			stored.MinerSignerEpochs = test.stored
			rawdb.WriteChainConfig(db, block.Hash(), &stored)
			writeSyntheticHead(t, db, 1, 1)

			logged := captureWarnings(t)
			config, _, err := SetupGenesisBlockWithOverride(db, tdb, test.genesis, test.overrides)
			if err != nil {
				t.Fatal(err)
			}
			if have := config.MinerSignerEpochs[0].TD.Uint64(); have != test.td {
				t.Fatalf("threshold %d, want %d", have, test.td)
			}
			if have, want := containsMatch(logged(), warning), test.warns != nil; have != want {
				t.Fatalf("warned: %v, want %v\n%q", have, want, logged())
			}
			for _, name := range test.warns {
				if !containsMatch(logged(), name) {
					t.Fatalf("the warning does not name %s\n%q", name, logged())
				}
			}
			// It cannot tell a dropped flag from a release, so it has to name both.
			if test.warns != nil && (!containsMatch(logged(), "--override.minersignertd") || !containsMatch(logged(), "this binary")) {
				t.Fatalf("the warning does not name both causes\n%q", logged())
			}
		})
	}
}

// Where no bundled config or genesis replaces the stored one, dropping the override moves
// nothing: the value the earlier start stored is the one the node runs, and the log has
// nothing to say. A start with a genesis file replaces it, and says so.
func TestMinerSignerThresholdStaysWithoutReplacement(t *testing.T) {
	const warning = "Stored miner signer epochs replaced"
	var (
		genesis = &Genesis{
			Config: &params.ChainConfig{
				ChainID: big.NewInt(1337),
				Ethash:  new(params.EthashConfig),
				MinerSignerEpochs: []params.MinerSignerEpoch{
					{TD: big.NewInt(1_000_000), Signers: map[common.Address]common.Address{{1}: {2}}},
				},
			},
			Difficulty: params.GenesisDifficulty,
			GasLimit:   8_000_000,
		}
		held = uint64(2_000_000)
		db   = rawdb.NewMemoryDatabase()
		tdb  = trie.NewDatabase(db)
	)
	start := func(g *Genesis, overrides *ChainOverrides) (uint64, bool) {
		t.Helper()
		logged := captureWarnings(t)
		config, _, err := SetupGenesisBlockWithOverride(db, tdb, g, overrides)
		if err != nil {
			t.Fatal(err)
		}
		return config.MinerSignerEpochs[0].TD.Uint64(), containsMatch(logged(), warning)
	}
	if td, warned := start(genesis, &ChainOverrides{OverrideMinerSignerTD: &held}); td != held || warned {
		t.Fatalf("first start with the flag: threshold %d (want %d), warned %v", td, held, warned)
	}
	if td, warned := start(nil, nil); td != held || warned {
		t.Fatalf("start without the flag: threshold %d (want %d, unchanged), warned %v (want no)", td, held, warned)
	}
	if td, warned := start(genesis, nil); td != 1_000_000 || !warned {
		t.Fatalf("start with the genesis file: threshold %d (want 1000000), warned %v (want yes)", td, warned)
	}
}

// A database written before the fields existed takes the new configuration on restart with
// no rewind, because they are not part of the compatibility check.
func TestMinerSignerEpochsAdoptedOnRestart(t *testing.T) {
	w := newMinerSigWorld(t, 3)
	blocks := w.mine(w.legacy, w.plain, w.genesis, 2, 5, w.listed)

	db := rawdb.NewMemoryDatabase()
	old := w.node(db, w.legacy)
	w.mustInsert(old, blocks)
	w.stop(old)

	upgraded := w.node(db, w.config)
	w.wantHead(upgraded, blocks[1])
	if have := len(upgraded.Config().MinerSignerEpochs); have != 1 {
		t.Fatalf("%d epochs after the restart", have)
	}
	stored := rawdb.ReadChainConfig(db, w.genesis.Hash())
	if len(stored.MinerSignerEpochs) != 1 || stored.MinerSignerEpochs[0].TD.Cmp(minerSigTD(3)) != 0 {
		t.Fatalf("stored config: %+v", stored.MinerSignerEpochs)
	}
	next := w.mine(w.config, w.signer, blocks[1], 2, 5, w.listed)
	w.mustInsert(upgraded, next)
	w.wantHead(upgraded, next[1])
}

func TestMinerSignatureRewindAfterHeaderReorg(t *testing.T) {
	w := newMinerSigWorld(t, 2)
	var public []*types.Block
	parent := w.genesis
	for i := 0; i < 12; i++ {
		parent = w.mineWith(w.config, w.signer, parent, 1, w.listed, w.spend)
		public = append(public, parent)
	}
	db := rawdb.NewMemoryDatabase()
	old := w.node(db, w.config)
	w.mustInsert(old, public)
	w.stop(old)
	chain := w.node(db, w.config)
	base := public[2]
	if chain.HasState(base.Root()) {
		t.Fatal("the fork ancestor was not pruned")
	}
	stake := w.mineWith(w.legacy, w.plain, w.genesis, 1, w.listed, w.stake)
	w.mustInsert(chain, []*types.Block{stake})
	claiming := w.claimingRoot(base, stake.Root())
	stored := []*types.Block{claiming}
	for tip := claiming; tip.NumberU64() < public[len(public)-1].NumberU64(); {
		tip = w.mineWith(w.withoutRegistry(), w.rogue, tip, 30, w.attacker, nil)
		stored = append(stored, tip)
	}
	target := rawdb.ReadTd(w.genDb, parent.Hash(), parent.NumberU64())
	tip := stored[len(stored)-1]
	if rawdb.ReadTd(w.genDb, tip.Hash(), tip.NumberU64()).Cmp(target) >= 0 {
		t.Fatal("the stored fork is not strictly lighter than the public chain")
	}
	w.mustInsert(chain, stored)
	if rawdb.HasReceipts(db, claiming.Hash(), claiming.NumberU64()) || chain.HasBlockAndState(claiming.Hash(), claiming.NumberU64()) {
		t.Fatal("the unexecuted block was trusted before the header reorg")
	}
	fork := append([]*types.Block{}, stored...)
	for rawdb.ReadTd(w.genDb, tip.Hash(), tip.NumberU64()).Cmp(target) <= 0 {
		tip = w.mineWith(w.config, w.rogue, tip, 1, w.attacker, nil)
		fork = append(fork, tip)
	}
	headers := make([]*types.Header, len(fork))
	for i, block := range fork {
		headers[i] = block.Header()
	}
	if _, err := chain.InsertHeaderChain(headers, 1); err != nil {
		t.Fatal(err)
	}
	if err := chain.SetHead(claiming.NumberU64()); err != nil {
		t.Fatal(err)
	}
	if chain.CurrentBlock().Hash() == claiming.Hash() || chain.HasBlockAndState(claiming.Hash(), claiming.NumberU64()) {
		t.Fatal("rewind promoted a block that never passed the registry check")
	}
	if _, err := chain.InsertChain(append([]*types.Block{claiming}, stored[1:2]...)); !errors.Is(err, consensus.ErrUnauthorizedMiner) {
		t.Fatalf("reimport after rewind: %v, want unauthorized miner", err)
	}
}
