package catalyst

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/beacon/engine"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/params"
)

func TestPoSDisabledInLES(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	gspec := &core.Genesis{
		Config: params.AllEthashProtocolChanges,
		Alloc:  core.GenesisAlloc{},
	}
	genesis := gspec.MustCommit(db)

	defer func() {
		if r := recover(); r != nil {
			if r != "PoS is not supported" {
				t.Errorf("Expected panic 'PoS is not supported', got: %v", r)
			}
		} else {
			t.Error("Expected panic when calling SetPoS(), but no panic occurred")
		}
	}()

	core.GenerateChain(gspec.Config, genesis, ethash.NewFaker(), db, 1, func(i int, gen *core.BlockGen) {
		gen.SetPoS()
	})
}

func TestLESCatalystDisabled(t *testing.T) {
	// Test that PoS functionality is disabled in LES mode for PoW fork
	api := &ConsensusAPI{les: nil} // les is not used in GetPayloadV1

	_, err := api.GetPayloadV1([8]byte{1, 2, 3, 4, 5, 6, 7, 8})
	if err == nil {
		t.Error("Expected error for GetPayloadV1 in LES mode")
	}
	// Check that it's a server error and contains the expected message
	if engErr, ok := err.(*engine.EngineAPIError); ok {
		if engErr.ErrorCode() != -32000 {
			t.Errorf("Unexpected error code: %d", engErr.ErrorCode())
		}
		if engErr.Error() != "Server error" {
			t.Errorf("Unexpected error message: %s", engErr.Error())
		}
		if data := engErr.ErrorData(); data != nil {
			dataStr := fmt.Sprintf("%v", data)
			if !strings.Contains(dataStr, "not supported in light client mode") {
				t.Errorf("Unexpected error data: %v", data)
			}
		} else {
			t.Errorf("ErrorData is nil")
		}
	} else {
		t.Errorf("Expected *engine.EngineAPIError, got %T: %v", err, err)
	}
}
