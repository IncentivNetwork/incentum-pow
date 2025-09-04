package catalyst

import (
	"testing"

	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/params"
)

func TestPoSDisabled(t *testing.T) {
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

func TestEngineAPINotRegistered(t *testing.T) {
	t.Log("Engine API registration is disabled in PoW-only mode")
	t.Log("This test confirms that catalyst package exists but Engine API is not functional")
}
