package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// Test that miner and fee pool get correct shares from both tip and base fee, and base fee is multiplied by gasUsed.
func TestFeePoolRewardDistribution_BaseFeeTimesGasUsed(t *testing.T) {
	gspec := &Genesis{
		Config: &params.ChainConfig{
			ChainID:                  big.NewInt(1337),
			HomesteadBlock:           big.NewInt(0),
			EIP150Block:              big.NewInt(0),
			EIP155Block:              big.NewInt(0),
			EIP158Block:              big.NewInt(0),
			ByzantiumBlock:           big.NewInt(0),
			ConstantinopleBlock:      big.NewInt(0),
			PetersburgBlock:          big.NewInt(0),
			IstanbulBlock:            big.NewInt(0),
			BerlinBlock:              big.NewInt(0),
			LondonBlock:              big.NewInt(0),
			FeePoolBlock:             big.NewInt(0),
			ZeroRewardBlock:          big.NewInt(0),
			Ethash:                   new(params.EthashConfig),
		},
		Alloc:   GenesisAlloc{},
		BaseFee: big.NewInt(params.InitialBaseFee),
	}

	// Fund a sender
	key, _ := crypto.GenerateKey()
	sender := crypto.PubkeyToAddress(key.PublicKey)
	gspec.Alloc[sender] = GenesisAccount{Balance: big.NewInt(1e18)}

	engine := ethash.NewFaker()
	db := rawdb.NewMemoryDatabase()
	genesis := gspec.MustCommit(db)
	chain, _ := NewBlockChain(db, nil, gspec, nil, engine, vm.Config{}, nil, nil)
	defer chain.Stop()

	// Build one block with a single EIP-1559 tx
	blocks, _ := GenerateChain(gspec.Config, genesis, engine, db, 1, func(i int, b *BlockGen) {
		coinbase := common.HexToAddress("0x00000000000000000000000000000000000000c0")
		b.SetCoinbase(coinbase)

		feeCap := new(big.Int).Add(gspec.BaseFee, big.NewInt(2_000_000_000)) // base + 2 gwei
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   gspec.Config.ChainID,
			Nonce:     0,
			To:        &coinbase, // any receiver
			Gas:       50000,
			GasTipCap: big.NewInt(2_000_000_000),
			GasFeeCap: feeCap,
			Value:     big.NewInt(0),
		})
		tx, _ = types.SignTx(tx, types.LatestSigner(gspec.Config), key)
		b.AddTx(tx)
	})
	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("block %d: failed to insert into chain: %v", n, err)
	}

	block := chain.GetBlockByNumber(1)
	state, _ := chain.State()

	gasUsed := block.GasUsed()
	baseFee := block.BaseFee()
	tx := block.Transactions()[0]

	// Compute effective tip: min(GasTipCap, GasFeeCap - BaseFee)
	effectiveTip := new(big.Int).Sub(tx.GasFeeCap(), baseFee)
	if effectiveTip.Cmp(tx.GasTipCap()) > 0 {
		effectiveTip = new(big.Int).Set(tx.GasTipCap())
	}

	// Tip split
	tipFee := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), effectiveTip)
	minerTip := new(big.Int).Mul(tipFee, big.NewInt(params.MinerFeePercent))
	minerTip.Div(minerTip, big.NewInt(params.FeePercentDivisor))
	poolTip := new(big.Int).Mul(tipFee, big.NewInt(params.FeePoolPercent))
	poolTip.Div(poolTip, big.NewInt(params.FeePercentDivisor))

	// Base fee split (multiplied by gasUsed)
	baseFeeTotal := new(big.Int).Mul(new(big.Int).SetUint64(gasUsed), baseFee)
	minerBase := new(big.Int).Mul(baseFeeTotal, big.NewInt(params.MinerFeePercent))
	minerBase.Div(minerBase, big.NewInt(params.FeePercentDivisor))
	poolBase := new(big.Int).Mul(baseFeeTotal, big.NewInt(params.FeePoolPercent))
	poolBase.Div(poolBase, big.NewInt(params.FeePercentDivisor))

	expectedMiner := new(big.Int).Add(minerTip, minerBase)
	expectedPool := new(big.Int).Add(poolTip, poolBase)

	minerBal := state.GetBalance(block.Coinbase())
	if minerBal.Cmp(expectedMiner) != 0 {
		t.Fatalf("miner balance incorrect: expected %v, got %v", expectedMiner, minerBal)
	}

	poolBal := state.GetBalance(gspec.Config.GetFeePoolContractAddress())
	if poolBal.Cmp(expectedPool) != 0 {
		t.Fatalf("fee pool balance incorrect: expected %v, got %v", expectedPool, poolBal)
	}
}
