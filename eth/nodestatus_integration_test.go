// Copyright 2017 The go-ethereum Authors
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

package eth

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
)

// TestNodeStatusIntegrationGenesisOnly tests the NodeStatus function
// using a minimal setup with only genesis block.
func TestNodeStatusIntegrationGenesisOnly(t *testing.T) {
	// Create a minimal genesis
	genesis := &core.Genesis{
		Config:     params.AllEthashProtocolChanges,
		Alloc:      core.GenesisAlloc{},
		Difficulty: big.NewInt(1),
		GasLimit:   8000000,
	}

	// Generate a private key for P2P
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}

	// Create a test node config
	nodeConfig := &node.Config{
		P2P: p2p.Config{
			PrivateKey:  key,
			MaxPeers:    0, // No peers for testing
			NoDiscovery: true,
		},
	}

	// Create the node
	stack, err := node.New(nodeConfig)
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	// Create Ethereum backend with minimal config
	ethConfig := ethconfig.Config{
		Genesis: genesis,
	}

	eth, err := New(stack, &ethConfig)
	if err != nil {
		t.Fatalf("Failed to create Ethereum instance: %v", err)
	}
	defer stack.Close()

	// Start the node to initialize components
	if err := stack.Start(); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Create API instance
	api := NewEthereumAPI(eth)

	// Call NodeStatus
	status, err := api.NodeStatus()
	if err != nil {
		t.Fatalf("NodeStatus failed: %v", err)
	}

	// Verify precise expected values for minimal setup
	if status.BlockNumber != 0 {
		t.Errorf("BlockNumber should be 0 (genesis block), got %d", status.BlockNumber)
	}
	if status.PeerCount != 0 {
		t.Errorf("PeerCount should be 0 (no peers configured), got %d", status.PeerCount)
	}
	if status.TxpoolPending != 0 {
		t.Errorf("TxpoolPending should be 0 (empty pool), got %d", status.TxpoolPending)
	}
	if status.TxpoolQueued != 0 {
		t.Errorf("TxpoolQueued should be 0 (empty pool), got %d", status.TxpoolQueued)
	}
	if status.BlockHash == (common.Hash{}) {
		t.Errorf("BlockHash should not be zero (genesis hash)")
	}
	if status.Mining {
		t.Errorf("Mining should be false (not mining), got %v", status.Mining)
	}
	if status.Syncing == nil {
		if status.IsReady {
			t.Errorf("IsReady should be false (no peers), got %v", status.IsReady)
		}
		t.Errorf("Syncing should not be nil")
	}
}

// TestNodeStatusIntegrationSingleNodeMining tests the NodeStatus function with a single node
// that mines a block with a transaction.
func TestNodeStatusIntegrationSingleNodeMining(t *testing.T) {
	// Generate a private key for funded account
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key: %v", err)
	}
	addr := crypto.PubkeyToAddress(key.PublicKey)

	// Create a minimal genesis with funded account
	genesis := &core.Genesis{
		Config: params.AllEthashProtocolChanges,
		Alloc: core.GenesisAlloc{
			addr: {Balance: big.NewInt(1000000000000000000)}, // 1 ETH
		},
		Difficulty: big.NewInt(1),
		GasLimit:   8000000,
	}

	// Generate a private key for P2P
	key2, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate private key for P2P: %v", err)
	}

	// Create a test node config (no peers allowed)
	nodeConfig := &node.Config{
		P2P: p2p.Config{
			PrivateKey:  key2,
			MaxPeers:    0, // No peers for testing
			NoDiscovery: true,
		},
	}

	// Create the node
	stack, err := node.New(nodeConfig)
	if err != nil {
		t.Fatalf("Failed to create test node: %v", err)
	}

	// Create Ethereum backend with minimal config
	ethConfig := ethconfig.Config{
		Genesis: genesis,
		Ethash: ethash.Config{
			CachesInMem: 1,
		},
	}
	ethConfig.Miner.Etherbase = addr
	ethConfig.Miner.GasPrice = big.NewInt(1)

	eth, err := New(stack, &ethConfig)
	if err != nil {
		t.Fatalf("Failed to create Ethereum instance: %v", err)
	}
	defer stack.Close()

	// Start the node to initialize components
	if err := stack.Start(); err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Add a transaction to the txpool, then mine the first block
	nonce := uint64(0)
	toAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	tx := types.NewTransaction(nonce, toAddr, big.NewInt(1000), 21000, big.NewInt(1000000000), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(big.NewInt(1337)), key)
	if err != nil {
		t.Fatalf("Failed to sign tx: %v", err)
	}
	if err := eth.TxPool().AddLocal(signedTx); err != nil {
		t.Fatalf("Failed to add tx to pool: %v", err)
	}

	// Subscribe to chain head events to wait for new block
	headCh := make(chan core.ChainHeadEvent, 1)
	sub := eth.BlockChain().SubscribeChainHeadEvent(headCh)
	defer sub.Unsubscribe()

	// Start mining to mine the first block with the transaction
	if err := eth.StartMining(1); err != nil {
		t.Fatalf("Failed to start mining: %v", err)
	}

	// Wait for mining to produce a block
	select {
	case <-headCh:
		// Block mined
	case <-time.After(2 * time.Minute):
		t.Fatal("Timeout waiting for block to be mined")
	}

	eth.Miner().Stop()

	// Wait for miner to fully stop and txpool to be cleaned up.
	// We poll a few times with a short sleep to let background cleanup finish.
	const (
		waitInterval = 100 * time.Millisecond
		waitTimeout  = 10 * time.Second
	)

	deadline := time.Now().Add(waitTimeout)
	for {
		// Check mining status and txpool stats
		mining := eth.IsMining()
		pending, queued := eth.TxPool().Stats()

		if !mining && pending == 0 {
			// desired stable state reached
			break
		}

		if time.Now().After(deadline) {
			// Provide diagnostics to help debugging if test still fails
			t.Fatalf("timed out waiting for miner stop/txpool cleanup: mining=%v pending=%d queued=%d",
				mining, pending, queued)
		}
		time.Sleep(waitInterval)
	}

	// Create API instance
	api := NewEthereumAPI(eth)

	// Call NodeStatus
	status, err := api.NodeStatus()
	if err != nil {
		t.Fatalf("NodeStatus failed: %v", err)
	}

	// Verify precise expected values
	if status.BlockNumber != 1 {
		t.Errorf("BlockNumber should be 1 (first mined block), got %d", status.BlockNumber)
	}
	if status.PeerCount != 0 {
		t.Errorf("PeerCount should be 0 (no peers configured), got %d", status.PeerCount)
	}
	if status.TxpoolPending != 0 {
		t.Errorf("TxpoolPending should be 0 (tx mined into block), got %d", status.TxpoolPending)
	}
	if status.TxpoolQueued != 0 {
		t.Errorf("TxpoolQueued should be 0 (empty queue), got %d", status.TxpoolQueued)
	}
	if status.BlockHash == (common.Hash{}) {
		t.Errorf("BlockHash should not be zero (latest block hash)")
	}
	if status.Mining {
		t.Errorf("Mining should be false (not mining), got %v", status.Mining)
	}
	if status.Syncing == nil {
		if status.IsReady {
			t.Errorf("IsReady should be false (no peers), got %v", status.IsReady)
		}
		t.Errorf("Syncing should not be nil")
	}
}
