package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/minbasefee"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

func TestDynamicMinBaseFee_Integration_GenesisContract(t *testing.T) {
	contractAddr := common.HexToAddress("0x0000000000000000000000000000000000000100")
	governanceAddr := common.HexToAddress("0x0000000000000000000000000000000000000200")
	initialMinBaseFee := big.NewInt(40000000000000)

	config := &params.ChainConfig{
		ChainID:                big.NewInt(1337),
		HomesteadBlock:         big.NewInt(0),
		EIP150Block:            big.NewInt(0),
		EIP155Block:            big.NewInt(0),
		EIP158Block:            big.NewInt(0),
		ByzantiumBlock:         big.NewInt(0),
		ConstantinopleBlock:    big.NewInt(0),
		PetersburgBlock:        big.NewInt(0),
		IstanbulBlock:          big.NewInt(0),
		MuirGlacierBlock:       big.NewInt(0),
		BerlinBlock:            big.NewInt(0),
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  u64(5),
		MinBaseFeeContractAddr: &contractAddr,
		Ethash:                 new(params.EthashConfig),
	}

	alloc := GenesisAlloc{
		contractAddr: GenesisAccount{
			Balance: common.Big0,
			Storage: make(map[common.Hash]common.Hash),
		},
		governanceAddr: GenesisAccount{
			Balance: big.NewInt(1000000000000000000),
		},
	}

	arraySlot := crypto.Keccak256Hash(common.BigToHash(big.NewInt(1)).Bytes())

	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(0))] = common.BytesToHash(governanceAddr.Bytes())
	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(1))] = common.BigToHash(big.NewInt(1))
	alloc[contractAddr].Storage[arraySlot] = common.BigToHash(initialMinBaseFee)
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(1)))] = common.BigToHash(big.NewInt(0))
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(2)))] = common.BigToHash(big.NewInt(1000000000))

	genesis := &Genesis{
		Config:     config,
		Alloc:      alloc,
		ExtraData:  []byte("test genesis"),
		Timestamp:  9000,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Difficulty: big.NewInt(0),
		GasLimit:   30000000,
	}

	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	stateDB, err := chain.State()
	if err != nil {
		t.Fatalf("Failed to get state: %v", err)
	}

	reader := minbasefee.NewReader(contractAddr)
	minBaseFee, err := reader.ReadMinBaseFee(stateDB, big.NewInt(0))
	if err != nil {
		t.Fatalf("Failed to read min base fee from genesis contract: %v", err)
	}

	if minBaseFee.Cmp(initialMinBaseFee) != 0 {
		t.Errorf("Expected min base fee %s, got %s", initialMinBaseFee, minBaseFee)
	}

	t.Logf("Successfully read min base fee from genesis contract: %s gwei", new(big.Int).Div(minBaseFee, big.NewInt(params.GWei)))

	blocks, _ := GenerateChain(config, chain.Genesis(), engine, db, 10, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{1})
	})

	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to insert blocks: %v", err)
	}

	block5 := chain.GetBlockByNumber(5)
	if block5 == nil {
		t.Fatal("Block 5 not found")
	}

	stateDB5, err := chain.StateAt(block5.Root())
	if err != nil {
		t.Fatalf("Failed to get state at block 5: %v", err)
	}

	minBaseFee5, err := reader.ReadMinBaseFee(stateDB5, big.NewInt(5))
	if err != nil {
		t.Fatalf("Failed to read min base fee at block 5: %v", err)
	}

	if minBaseFee5.Cmp(initialMinBaseFee) != 0 {
		t.Errorf("Block 5: expected min base fee %s, got %s", initialMinBaseFee, minBaseFee5)
	}

	t.Logf("Block 5 base fee: %s (min: %s gwei)", block5.BaseFee(), new(big.Int).Div(minBaseFee5, big.NewInt(params.GWei)))
}

func TestDynamicMinBaseFee_Integration_ContractUpdate(t *testing.T) {
	contractAddr := common.HexToAddress("0x0000000000000000000000000000000000000100")
	governanceAddr := common.HexToAddress("0x0000000000000000000000000000000000000200")
	initialMinBaseFee := big.NewInt(40000000000000)
	updatedMinBaseFee := big.NewInt(15000000000000)

	config := &params.ChainConfig{
		ChainID:                big.NewInt(1337),
		HomesteadBlock:         big.NewInt(0),
		EIP150Block:            big.NewInt(0),
		EIP155Block:            big.NewInt(0),
		EIP158Block:            big.NewInt(0),
		ByzantiumBlock:         big.NewInt(0),
		ConstantinopleBlock:    big.NewInt(0),
		PetersburgBlock:        big.NewInt(0),
		IstanbulBlock:          big.NewInt(0),
		MuirGlacierBlock:       big.NewInt(0),
		BerlinBlock:            big.NewInt(0),
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  u64(1),
		MinBaseFeeContractAddr: &contractAddr,
		Ethash:                 new(params.EthashConfig),
	}

	alloc := GenesisAlloc{
		contractAddr: GenesisAccount{
			Balance: common.Big0,
			Storage: make(map[common.Hash]common.Hash),
		},
		governanceAddr: GenesisAccount{
			Balance: big.NewInt(1000000000000000000),
		},
	}

	arraySlot := crypto.Keccak256Hash(common.BigToHash(big.NewInt(1)).Bytes())

	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(0))] = common.BytesToHash(governanceAddr.Bytes())
	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(1))] = common.BigToHash(big.NewInt(2))

	alloc[contractAddr].Storage[arraySlot] = common.BigToHash(initialMinBaseFee)
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(1)))] = common.BigToHash(big.NewInt(0))
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(2)))] = common.BigToHash(big.NewInt(1000000000))

	offset2 := int64(3)
	minBaseFeeSlot2 := new(big.Int).Add(arraySlot.Big(), big.NewInt(offset2))
	alloc[contractAddr].Storage[common.BigToHash(minBaseFeeSlot2)] = common.BigToHash(updatedMinBaseFee)

	activationSlot2 := new(big.Int).Add(arraySlot.Big(), big.NewInt(offset2+1))
	alloc[contractAddr].Storage[common.BigToHash(activationSlot2)] = common.BigToHash(big.NewInt(3))

	timestampSlot2 := new(big.Int).Add(arraySlot.Big(), big.NewInt(offset2+2))
	alloc[contractAddr].Storage[common.BigToHash(timestampSlot2)] = common.BigToHash(big.NewInt(2000000000))

	genesis := &Genesis{
		Config:     config,
		Alloc:      alloc,
		ExtraData:  []byte("test genesis"),
		Timestamp:  9000,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Difficulty: big.NewInt(0),
		GasLimit:   30000000,
	}

	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	blocks, _ := GenerateChain(config, chain.Genesis(), engine, db, 5, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{1})
	})

	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to insert blocks: %v", err)
	}

	reader := minbasefee.NewReader(contractAddr)

	block2 := chain.GetBlockByNumber(2)
	if block2 == nil {
		t.Fatal("Block 2 not found")
	}
	stateDB2, _ := chain.StateAt(block2.Root())
	minBaseFee2, _ := reader.ReadMinBaseFee(stateDB2, big.NewInt(2))
	t.Logf("Block 2 - min base fee: %s gwei (before update activation)", new(big.Int).Div(minBaseFee2, big.NewInt(params.GWei)))

	if minBaseFee2.Cmp(initialMinBaseFee) != 0 {
		t.Errorf("Block 2: expected initial min base fee %s, got %s", initialMinBaseFee, minBaseFee2)
	}

	block3 := chain.GetBlockByNumber(3)
	if block3 == nil {
		t.Fatal("Block 3 not found")
	}
	stateDB3, _ := chain.StateAt(block3.Root())

	configs, err := reader.ReadAllConfigs(stateDB3)
	if err != nil {
		t.Fatalf("Failed to read all configs: %v", err)
	}
	t.Logf("Block 3 - total configs: %d", len(configs))
	for i, cfg := range configs {
		t.Logf("  Config %d: minBaseFee=%s gwei, activationBlock=%d",
			i,
			new(big.Int).Div(cfg.MinBaseFee, big.NewInt(params.GWei)),
			cfg.ActivationBlock)
	}

	minBaseFee3, err := reader.ReadMinBaseFee(stateDB3, big.NewInt(3))
	if err != nil {
		t.Fatalf("Failed to read min base fee at block 3: %v", err)
	}

	if minBaseFee3.Cmp(updatedMinBaseFee) != 0 {
		t.Errorf("Block 3: expected updated min base fee %s, got %s", updatedMinBaseFee, minBaseFee3)
	}

	t.Logf("Block 3 - min base fee: %s gwei (after update activation)", new(big.Int).Div(minBaseFee3, big.NewInt(params.GWei)))

	block4 := chain.GetBlockByNumber(4)
	if block4 == nil {
		t.Fatal("Block 4 not found")
	}
	stateDB4, _ := chain.StateAt(block4.Root())
	minBaseFee4, _ := reader.ReadMinBaseFee(stateDB4, big.NewInt(4))

	if minBaseFee4.Cmp(updatedMinBaseFee) != 0 {
		t.Errorf("Block 4: expected updated min base fee %s, got %s", updatedMinBaseFee, minBaseFee4)
	}

	t.Logf("Block 4 - min base fee: %s gwei (still using updated value)", new(big.Int).Div(minBaseFee4, big.NewInt(params.GWei)))
}

func TestDynamicMinBaseFee_Integration_ForkActivation(t *testing.T) {
	contractAddr := common.HexToAddress("0x0000000000000000000000000000000000000100")
	governanceAddr := common.HexToAddress("0x0000000000000000000000000000000000000200")
	contractMinBaseFee := big.NewInt(20000000000000)

	config := &params.ChainConfig{
		ChainID:                big.NewInt(1337),
		HomesteadBlock:         big.NewInt(0),
		EIP150Block:            big.NewInt(0),
		EIP155Block:            big.NewInt(0),
		EIP158Block:            big.NewInt(0),
		ByzantiumBlock:         big.NewInt(0),
		ConstantinopleBlock:    big.NewInt(0),
		PetersburgBlock:        big.NewInt(0),
		IstanbulBlock:          big.NewInt(0),
		MuirGlacierBlock:       big.NewInt(0),
		BerlinBlock:            big.NewInt(0),
		LondonBlock:            big.NewInt(0),
		MinBaseFeeBlock:        big.NewInt(2),
		MinBaseFeeChangeHeight: big.NewInt(5),
		DynamicMinBaseFeeTime:  u64(8),
		MinBaseFeeContractAddr: &contractAddr,
		Ethash:                 new(params.EthashConfig),
	}

	alloc := GenesisAlloc{
		contractAddr: GenesisAccount{
			Balance: common.Big0,
			Storage: make(map[common.Hash]common.Hash),
		},
		governanceAddr: GenesisAccount{
			Balance: big.NewInt(1000000000000000000),
		},
	}

	arraySlot := crypto.Keccak256Hash(common.BigToHash(big.NewInt(1)).Bytes())

	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(0))] = common.BytesToHash(governanceAddr.Bytes())
	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(1))] = common.BigToHash(big.NewInt(1))
	alloc[contractAddr].Storage[arraySlot] = common.BigToHash(contractMinBaseFee)
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(1)))] = common.BigToHash(big.NewInt(0))
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(2)))] = common.BigToHash(big.NewInt(1000000000))

	genesis := &Genesis{
		Config:     config,
		Alloc:      alloc,
		ExtraData:  []byte("test genesis"),
		Timestamp:  9000,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Difficulty: big.NewInt(0),
		GasLimit:   30000000,
	}

	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	blocks, _ := GenerateChain(config, chain.Genesis(), engine, db, 12, func(i int, b *BlockGen) {
		b.SetCoinbase(common.Address{1})
	})

	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("Failed to insert blocks: %v", err)
	}

	tests := []struct {
		blockNum uint64
		desc     string
	}{
		{1, "before MinBaseFee fork - no min floor"},
		{3, "after MinBaseFee fork - uses 40000 gwei hardcoded"},
		{6, "after MinBaseFeeChange - uses 12600 gwei hardcoded"},
		{9, "after DynamicMinBaseFee fork - uses contract value 20000 gwei"},
	}

	reader := minbasefee.NewReader(contractAddr)

	for _, tt := range tests {
		block := chain.GetBlockByNumber(tt.blockNum)
		if block == nil {
			t.Fatalf("Block %d not found", tt.blockNum)
		}

		stateDB, _ := chain.StateAt(block.Root())

		var minBaseFeeSource string
		var expectedMinBaseFee *big.Int

		if config.IsDynamicMinBaseFee(block.Time()) {
			minBaseFee, err := reader.ReadMinBaseFee(stateDB, big.NewInt(int64(tt.blockNum)))
			if err == nil {
				expectedMinBaseFee = minBaseFee
				minBaseFeeSource = "contract"
			}
		} else if config.IsMinBaseFee(big.NewInt(int64(tt.blockNum))) {
			if config.IsMinBaseFeeChange(big.NewInt(int64(tt.blockNum))) {
				expectedMinBaseFee = big.NewInt(params.MinBaseFeeUpdated)
				minBaseFeeSource = "hardcoded (updated)"
			} else {
				expectedMinBaseFee = big.NewInt(params.MinimumBaseFee)
				minBaseFeeSource = "hardcoded (initial)"
			}
		} else {
			expectedMinBaseFee = common.Big0
			minBaseFeeSource = "none"
		}

		t.Logf("Block %d: %s | base fee: %s | min source: %s | min value: %s gwei",
			tt.blockNum,
			tt.desc,
			block.BaseFee(),
			minBaseFeeSource,
			new(big.Int).Div(expectedMinBaseFee, big.NewInt(params.GWei)))
	}
}

// TestDynamicMinBaseFee_E2E_CalcBaseFeeWithState exercises the end-to-end path:
// CalcBaseFee is invoked with the parent's post-state, the reader pulls the
// floor from MinBaseFeeGovernor storage seeded at genesis, and the floor is
// applied to the EIP-1559 result. By construction this test cannot pass without
// the fork-active branch of CalcBaseFee actually reading the contract, which
// the original feature-branch wiring (stateDB always nil at production call
// sites) made impossible.
func TestDynamicMinBaseFee_E2E_CalcBaseFeeWithState(t *testing.T) {
	contractAddr := common.HexToAddress("0x0000000000000000000000000000000000000100")
	governanceAddr := common.HexToAddress("0x0000000000000000000000000000000000000200")
	// Contract floor: 1500 gwei. Genesis baseFee below the floor so that, when
	// the floor is applied, the next block's baseFee must jump up to the floor.
	floorWei := new(big.Int).Mul(big.NewInt(1500), big.NewInt(params.GWei))
	genesisBaseFee := new(big.Int).Mul(big.NewInt(100), big.NewInt(params.GWei))

	config := &params.ChainConfig{
		ChainID:                big.NewInt(1337),
		HomesteadBlock:         big.NewInt(0),
		EIP150Block:            big.NewInt(0),
		EIP155Block:            big.NewInt(0),
		EIP158Block:            big.NewInt(0),
		ByzantiumBlock:         big.NewInt(0),
		ConstantinopleBlock:    big.NewInt(0),
		PetersburgBlock:        big.NewInt(0),
		IstanbulBlock:          big.NewInt(0),
		MuirGlacierBlock:       big.NewInt(0),
		BerlinBlock:            big.NewInt(0),
		LondonBlock:            big.NewInt(0),
		DynamicMinBaseFeeTime:  u64(0), // active from genesis
		MinBaseFeeContractAddr: &contractAddr,
		Ethash:                 new(params.EthashConfig),
	}

	alloc := GenesisAlloc{
		contractAddr: GenesisAccount{
			Balance: common.Big0,
			Storage: make(map[common.Hash]common.Hash),
		},
		governanceAddr: GenesisAccount{
			Balance: big.NewInt(1000000000000000000),
		},
	}

	// Seed configHistory with a single entry at activationBlock=0 and floor.
	arraySlot := crypto.Keccak256Hash(common.BigToHash(big.NewInt(1)).Bytes())
	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(0))] = common.BytesToHash(governanceAddr.Bytes())
	alloc[contractAddr].Storage[common.BigToHash(big.NewInt(1))] = common.BigToHash(big.NewInt(1))
	alloc[contractAddr].Storage[arraySlot] = common.BigToHash(floorWei)
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(1)))] = common.BigToHash(big.NewInt(0))
	alloc[contractAddr].Storage[common.BigToHash(new(big.Int).Add(arraySlot.Big(), big.NewInt(2)))] = common.BigToHash(big.NewInt(1))

	genesis := &Genesis{
		Config:     config,
		Alloc:      alloc,
		ExtraData:  []byte("dmbf e2e"),
		Timestamp:  10,
		BaseFee:    genesisBaseFee,
		Difficulty: big.NewInt(0),
		GasLimit:   30000000,
	}

	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	chain, err := NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	parent := chain.CurrentHeader()
	parentState, err := chain.StateAt(parent.Root)
	if err != nil {
		t.Fatalf("Failed to get parent state: %v", err)
	}

	// CalcBaseFee with a non-nil state must succeed and return at least the floor.
	got, err := misc.CalcBaseFee(config, parent, parentState)
	if err != nil {
		t.Fatalf("CalcBaseFee with parent state failed: %v", err)
	}
	if got.Cmp(floorWei) < 0 {
		t.Fatalf("expected baseFee >= floor; got=%s floor=%s", got, floorWei)
	}

	// CalcBaseFee with nil state must return an explicit error after activation -
	// this is what gates the consensus path against silent fallback.
	if _, err := misc.CalcBaseFee(config, parent, nil); err == nil {
		t.Fatalf("expected CalcBaseFee with nil state to error post-activation; got nil")
	}
}
