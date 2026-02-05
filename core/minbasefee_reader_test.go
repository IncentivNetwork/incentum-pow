package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/minbasefee"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestMinBaseFeeReader_EmptyState(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	
	fakeAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	reader := minbasefee.NewReader(fakeAddr)
	
	_, err := reader.ReadMinBaseFee(stateDB, big.NewInt(0))
	if err == nil {
		t.Error("Expected error when reading from empty contract, got nil")
	}
	
	t.Logf("Correctly returned error for empty state: %v", err)
}

func TestMinBaseFeeReader_NilStateDB(t *testing.T) {
	contractAddr := common.HexToAddress("0x1234567890123456789012345678901234567890")
	reader := minbasefee.NewReader(contractAddr)
	
	_, err := reader.ReadMinBaseFee(nil, big.NewInt(0))
	if err == nil {
		t.Error("Expected error when stateDB is nil, got nil")
	}
	
	if err.Error() != "stateDB is nil" {
		t.Errorf("Expected 'stateDB is nil' error, got: %v", err)
	}
}

func TestMinBaseFeeReader_MockContract(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	
	contractAddr := common.HexToAddress("0x1111111111111111111111111111111111111111")
	governanceAddr := common.HexToAddress("0x2222222222222222222222222222222222222222")
	initialMinBaseFee := big.NewInt(40000000000000)
	activationBlock := big.NewInt(0)
	
	stateDB.SetState(contractAddr, common.BigToHash(big.NewInt(0)), common.BytesToHash(governanceAddr.Bytes()))
	stateDB.SetState(contractAddr, common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(1)))
	
	arraySlot := crypto.Keccak256Hash(common.BigToHash(big.NewInt(1)).Bytes())
	
	stateDB.SetState(contractAddr, arraySlot, common.BigToHash(initialMinBaseFee))
	
	slot1 := new(big.Int).Add(arraySlot.Big(), big.NewInt(1))
	stateDB.SetState(contractAddr, common.BigToHash(slot1), common.BigToHash(activationBlock))
	
	slot2 := new(big.Int).Add(arraySlot.Big(), big.NewInt(2))
	stateDB.SetState(contractAddr, common.BigToHash(slot2), common.BigToHash(big.NewInt(1234567890)))
	
	reader := minbasefee.NewReader(contractAddr)
	
	minBaseFee, err := reader.ReadMinBaseFee(stateDB, big.NewInt(0))
	if err != nil {
		t.Fatalf("Failed to read min base fee: %v", err)
	}
	
	if minBaseFee.Cmp(initialMinBaseFee) != 0 {
		t.Errorf("Expected min base fee %s, got %s", initialMinBaseFee, minBaseFee)
	}
	
	t.Logf("Successfully read min base fee: %s wei (%s gwei)", minBaseFee, new(big.Int).Div(minBaseFee, big.NewInt(1000000000)))
}

func TestMinBaseFeeReader_MultipleConfigs(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	stateDB, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	
	contractAddr := common.HexToAddress("0x3333333333333333333333333333333333333333")
	
	stateDB.SetState(contractAddr, common.BigToHash(big.NewInt(1)), common.BigToHash(big.NewInt(3)))
	
	arraySlot := crypto.Keccak256Hash(common.BigToHash(big.NewInt(1)).Bytes())
	
	configs := []struct {
		minBaseFee      *big.Int
		activationBlock *big.Int
		timestamp       *big.Int
	}{
		{big.NewInt(40000000000000), big.NewInt(0), big.NewInt(1000000000)},
		{big.NewInt(12600000000000), big.NewInt(100), big.NewInt(1000000100)},
		{big.NewInt(8000000000000), big.NewInt(200), big.NewInt(1000000200)},
	}
	
	for i, cfg := range configs {
		offset := int64(i * 3)
		
		minBaseFeeSlot := new(big.Int).Add(arraySlot.Big(), big.NewInt(offset))
		stateDB.SetState(contractAddr, common.BigToHash(minBaseFeeSlot), common.BigToHash(cfg.minBaseFee))
		
		activationSlot := new(big.Int).Add(arraySlot.Big(), big.NewInt(offset+1))
		stateDB.SetState(contractAddr, common.BigToHash(activationSlot), common.BigToHash(cfg.activationBlock))
		
		timestampSlot := new(big.Int).Add(arraySlot.Big(), big.NewInt(offset+2))
		stateDB.SetState(contractAddr, common.BigToHash(timestampSlot), common.BigToHash(cfg.timestamp))
	}
	
	reader := minbasefee.NewReader(contractAddr)
	
	tests := []struct {
		blockNumber     uint64
		expectedMinFee  *big.Int
		description     string
	}{
		{0, big.NewInt(40000000000000), "Block 0: first config"},
		{50, big.NewInt(40000000000000), "Block 50: still first config"},
		{100, big.NewInt(12600000000000), "Block 100: second config activates"},
		{150, big.NewInt(12600000000000), "Block 150: still second config"},
		{200, big.NewInt(8000000000000), "Block 200: third config activates"},
		{300, big.NewInt(8000000000000), "Block 300: still third config"},
	}
	
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			minBaseFee, err := reader.ReadMinBaseFee(stateDB, big.NewInt(int64(tt.blockNumber)))
			if err != nil {
				t.Fatalf("Failed to read min base fee for block %d: %v", tt.blockNumber, err)
			}
			
			if minBaseFee.Cmp(tt.expectedMinFee) != 0 {
				t.Errorf("Block %d: expected %s, got %s", tt.blockNumber, tt.expectedMinFee, minBaseFee)
			}
			
			t.Logf("Block %d: min base fee = %s wei (%s gwei)", 
				tt.blockNumber, 
				minBaseFee, 
				new(big.Int).Div(minBaseFee, big.NewInt(1000000000)))
		})
	}
}
