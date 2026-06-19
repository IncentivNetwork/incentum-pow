package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// TestDynamicMinBaseFee_Disabled tests that dynamic min base fee is disabled
// when fork is not activated (uses hardcoded values)
func TestDynamicMinBaseFee_Disabled(t *testing.T) {
	config := &params.ChainConfig{
		ChainID:                big.NewInt(1),
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
		MinBaseFeeBlock:        big.NewInt(0),
		MinBaseFeeChangeHeight: big.NewInt(10),
		// DynamicMinBaseFeeTime is NOT set - feature disabled
		Ethash: new(params.EthashConfig),
	}

	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	genesis := &Genesis{
		Config:     config,
		Alloc:      GenesisAlloc{},
		ExtraData:  []byte("test genesis"),
		Timestamp:  9000,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Difficulty: big.NewInt(0),
	}

	chain, err := NewBlockChain(db, nil, genesis, nil, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create chain: %v", err)
	}
	defer chain.Stop()

	// Generate enough empty blocks that the raw EIP-1559 base fee decays
	// strictly below the post-change legacy floor. Math (empty blocks, GasLimit
	// 30M, target 15M, denominator 8, genesis BaseFee 1 gwei):
	//   * blocks 1..9 sit pinned at MinimumBaseFee = 40000 gwei (raw wants 0,
	//     floor clamps), so block 5 == 40000 gwei exactly.
	//   * block 10 lowers the floor to MinBaseFeeUpdated = 12600 gwei; raw at
	//     block 10 = 40000 * 7/8 = 35000 gwei, max(raw, 12600) = 35000.
	//   * raw then decays by 7/8 each empty block: block 18 raw ≈ 12026 gwei,
	//     first time below the new floor. From block 18 onward the floor pins
	//     the result at 12600 gwei.
	// 25 blocks gives a comfortable margin past the pinning boundary.
	blocks, _ := GenerateChain(config, chain.Genesis(), engine, db, 25, nil)

	// Verify blocks are valid
	if _, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert blocks: %v", err)
	}

	// Block 5 must equal the pre-change MinimumBaseFee floor exactly.
	block5 := chain.GetBlockByNumber(5)
	if block5 == nil {
		t.Fatal("block 5 not found")
	}

	// Block 25 must equal the post-change MinBaseFeeUpdated floor exactly. By
	// block 25 the raw EIP-1559 value has decayed well below the floor, so
	// equality here proves that max(rawBaseFee, floor) actually pins to the
	// floor. A weaker BaseFee >= floor invariant would always hold and prove
	// nothing about the floor-application code path.
	block25 := chain.GetBlockByNumber(25)
	if block25 == nil {
		t.Fatal("block 25 not found")
	}

	t.Logf("Block 5 base fee: %s", block5.BaseFee())
	t.Logf("Block 25 base fee: %s", block25.BaseFee())

	expected5 := new(big.Int).SetUint64(params.MinimumBaseFee)
	if block5.BaseFee() == nil || block5.BaseFee().Cmp(expected5) != 0 {
		t.Fatalf("block 5 baseFee = %v, want legacy floor %v", block5.BaseFee(), expected5)
	}
	expected25 := new(big.Int).SetUint64(params.MinBaseFeeUpdated)
	if block25.BaseFee() == nil || block25.BaseFee().Cmp(expected25) != 0 {
		t.Fatalf("block 25 baseFee = %v, want legacy updated floor %v", block25.BaseFee(), expected25)
	}
}

// TestMinBaseFee_Calculation tests the calculation logic remains correct
func TestMinBaseFee_Calculation(t *testing.T) {
	tests := []struct {
		name             string
		parentBaseFee    uint64
		parentGasUsed    uint64
		parentGasLimit   uint64
		nextBlock        uint64
		minBaseFeeBlock  *big.Int
		minBaseFeeChange *big.Int
		expectedMin      uint64
	}{
		{
			name:             "Before MinBaseFee fork",
			parentBaseFee:    1000000000,
			parentGasUsed:    10000000,
			parentGasLimit:   30000000,
			nextBlock:        5,
			minBaseFeeBlock:  big.NewInt(10),
			minBaseFeeChange: nil,
			expectedMin:      0, // No minimum
		},
		{
			name:             "After MinBaseFee fork, before change",
			parentBaseFee:    10000000000000,
			parentGasUsed:    10000000,
			parentGasLimit:   30000000,
			nextBlock:        15,
			minBaseFeeBlock:  big.NewInt(10),
			minBaseFeeChange: big.NewInt(20),
			expectedMin:      params.MinimumBaseFee, // 40000 gwei
		},
		{
			name:             "After MinBaseFeeChange",
			parentBaseFee:    5000000000000,
			parentGasUsed:    10000000,
			parentGasLimit:   30000000,
			nextBlock:        25,
			minBaseFeeBlock:  big.NewInt(10),
			minBaseFeeChange: big.NewInt(20),
			expectedMin:      params.MinBaseFeeUpdated, // 12600 gwei
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &params.ChainConfig{
				LondonBlock:            big.NewInt(0),
				MinBaseFeeBlock:        tt.minBaseFeeBlock,
				MinBaseFeeChangeHeight: tt.minBaseFeeChange,
				Ethash:                 new(params.EthashConfig),
			}

			parent := &types.Header{
				Number:   big.NewInt(int64(tt.nextBlock - 1)),
				GasLimit: tt.parentGasLimit,
				GasUsed:  tt.parentGasUsed,
				BaseFee:  new(big.Int).SetUint64(tt.parentBaseFee),
			}

			baseFee, err := misc.CalcBaseFee(config, parent, nil)
			if err != nil {
				t.Fatalf("CalcBaseFee returned error: %v", err)
			}

			if tt.expectedMin > 0 {
				minFee := new(big.Int).SetUint64(tt.expectedMin)
				if baseFee.Cmp(minFee) < 0 {
					t.Errorf("Base fee %s is below minimum %s", baseFee, minFee)
				}
			}

			t.Logf("Calculated base fee: %s (min: %d)", baseFee, tt.expectedMin)
		})
	}
}
