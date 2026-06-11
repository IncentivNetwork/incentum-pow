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

package params

import (
	"encoding/json"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

func TestCheckCompatible(t *testing.T) {
	type test struct {
		stored, new   *ChainConfig
		headBlock     uint64
		headTimestamp uint64
		wantErr       *ConfigCompatError
	}
	tests := []test{
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, headBlock: 0, headTimestamp: 0, wantErr: nil},
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, headBlock: 0, headTimestamp: uint64(time.Now().Unix()), wantErr: nil},
		{stored: AllEthashProtocolChanges, new: AllEthashProtocolChanges, headBlock: 100, wantErr: nil},
		{
			stored:    &ChainConfig{EIP150Block: big.NewInt(10)},
			new:       &ChainConfig{EIP150Block: big.NewInt(20)},
			headBlock: 9,
			wantErr:   nil,
		},
		{
			stored:    AllEthashProtocolChanges,
			new:       &ChainConfig{HomesteadBlock: nil},
			headBlock: 3,
			wantErr: &ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(0),
				NewBlock:      nil,
				RewindToBlock: 0,
			},
		},
		{
			stored:    AllEthashProtocolChanges,
			new:       &ChainConfig{HomesteadBlock: big.NewInt(1)},
			headBlock: 3,
			wantErr: &ConfigCompatError{
				What:          "Homestead fork block",
				StoredBlock:   big.NewInt(0),
				NewBlock:      big.NewInt(1),
				RewindToBlock: 0,
			},
		},
		{
			stored:    &ChainConfig{HomesteadBlock: big.NewInt(30), EIP150Block: big.NewInt(10)},
			new:       &ChainConfig{HomesteadBlock: big.NewInt(25), EIP150Block: big.NewInt(20)},
			headBlock: 25,
			wantErr: &ConfigCompatError{
				What:          "EIP150 fork block",
				StoredBlock:   big.NewInt(10),
				NewBlock:      big.NewInt(20),
				RewindToBlock: 9,
			},
		},
		{
			stored:    &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:       &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(30)},
			headBlock: 40,
			wantErr:   nil,
		},
		{
			stored:    &ChainConfig{ConstantinopleBlock: big.NewInt(30)},
			new:       &ChainConfig{ConstantinopleBlock: big.NewInt(30), PetersburgBlock: big.NewInt(31)},
			headBlock: 40,
			wantErr: &ConfigCompatError{
				What:          "Petersburg fork block",
				StoredBlock:   nil,
				NewBlock:      big.NewInt(31),
				RewindToBlock: 30,
			},
		},
		{
			stored:        &ChainConfig{ShanghaiTime: newUint64(10)},
			new:           &ChainConfig{ShanghaiTime: newUint64(20)},
			headTimestamp: 9,
			wantErr:       nil,
		},
		{
			stored:        &ChainConfig{ShanghaiTime: newUint64(10)},
			new:           &ChainConfig{ShanghaiTime: newUint64(20)},
			headTimestamp: 25,
			wantErr: &ConfigCompatError{
				What:         "Shanghai fork timestamp",
				StoredTime:   newUint64(10),
				NewTime:      newUint64(20),
				RewindToTime: 9,
			},
		},
	}

	for _, test := range tests {
		err := test.stored.CheckCompatible(test.new, test.headBlock, test.headTimestamp)
		if !reflect.DeepEqual(err, test.wantErr) {
			t.Errorf("error mismatch:\nstored: %v\nnew: %v\nheadBlock: %v\nheadTimestamp: %v\nerr: %v\nwant: %v", test.stored, test.new, test.headBlock, test.headTimestamp, err, test.wantErr)
		}
	}
}

func TestConfigRules(t *testing.T) {
	c := &ChainConfig{
		ShanghaiTime: newUint64(500),
	}
	var stamp uint64
	if r := c.Rules(big.NewInt(0), true, stamp); r.IsShanghai {
		t.Errorf("expected %v to not be shanghai", stamp)
	}
	stamp = 500
	if r := c.Rules(big.NewInt(0), true, stamp); !r.IsShanghai {
		t.Errorf("expected %v to be shanghai", stamp)
	}
	stamp = math.MaxInt64
	if r := c.Rules(big.NewInt(0), true, stamp); !r.IsShanghai {
		t.Errorf("expected %v to be shanghai", stamp)
	}
}

func TestCheckDPoWConfig(t *testing.T) {
	addr := common.HexToAddress("0x0000000000000000000000000000000000001234")

	tests := []struct {
		name    string
		cfg     *ChainConfig
		wantErr bool
	}{
		{
			name:    "dpow disabled and no registry",
			cfg:     &ChainConfig{},
			wantErr: false,
		},
		{
			name: "dpow enabled with registry",
			cfg: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr,
			},
			wantErr: false,
		},
		{
			name: "dpow enabled without registry",
			cfg: &ChainConfig{
				DPoWTime: newUint64(100),
			},
			wantErr: true,
		},
		{
			name: "dpow enabled with zero registry address",
			cfg: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: new(common.Address),
			},
			wantErr: true,
		},
		{
			name: "registry without dpow time is ignored",
			cfg: &ChainConfig{
				MinerRegistryAddress: &addr,
			},
			wantErr: false,
		},
		{
			name:    "incentiv mainnet config is valid",
			cfg:     IncentivMainnetChainConfig,
			wantErr: false,
		},
		{
			name:    "incentiv devnet config is valid",
			cfg:     IncentivDevnetChainConfig,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		err := tt.cfg.CheckDPoWConfig()
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: unexpected error state: err=%v wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

func TestDPoWConfigJSONRoundTrip(t *testing.T) {
	addr := common.HexToAddress("0x0000000000000000000000000000000000001234")

	empty := &ChainConfig{}
	blob, err := json.Marshal(empty)
	if err != nil {
		t.Fatalf("marshal empty config: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(blob, &raw); err != nil {
		t.Fatalf("unmarshal empty config json: %v", err)
	}

	for _, field := range []string{
		"dpowTime",
		"minerRegistryAddress",
		"dpowMaturityTime",
		"dpowMaturityBlocks",
	} {
		if _, ok := raw[field]; ok {
			t.Fatalf("expected %s to be omitted from JSON when zero/nil", field)
		}
	}

	cfg := &ChainConfig{
		DPoWTime:             newUint64(100),
		MinerRegistryAddress: &addr,
		DPoWMaturityTime:     300,
		DPoWMaturityBlocks:   60,
	}

	blob, err = json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal full config: %v", err)
	}

	var got ChainConfig
	if err := json.Unmarshal(blob, &got); err != nil {
		t.Fatalf("unmarshal full config json: %v", err)
	}

	if got.DPoWTime == nil || *got.DPoWTime != *cfg.DPoWTime {
		t.Fatalf("unexpected DPoWTime after round-trip: have=%v want=%v", got.DPoWTime, cfg.DPoWTime)
	}
	if got.MinerRegistryAddress == nil || *got.MinerRegistryAddress != addr {
		t.Fatalf("unexpected MinerRegistryAddress after round-trip: have=%v want=%v", got.MinerRegistryAddress, addr)
	}
	if got.DPoWMaturityTime != cfg.DPoWMaturityTime {
		t.Fatalf("unexpected DPoWMaturityTime after round-trip: have=%d want=%d", got.DPoWMaturityTime, cfg.DPoWMaturityTime)
	}
	if got.DPoWMaturityBlocks != cfg.DPoWMaturityBlocks {
		t.Fatalf("unexpected DPoWMaturityBlocks after round-trip: have=%d want=%d", got.DPoWMaturityBlocks, cfg.DPoWMaturityBlocks)
	}
}

func TestDPoWHelpers(t *testing.T) {
	addr := common.HexToAddress("0x0000000000000000000000000000000000001234")

	cfg := &ChainConfig{}
	if cfg.IsDPoW(0) {
		t.Fatalf("expected DPoW to be disabled when DPoWTime is nil")
	}
	if got := cfg.GetMinerRegistryAddress(); got != (common.Address{}) {
		t.Fatalf("unexpected zero registry address default: %v", got.Hex())
	}
	if got := cfg.GetDPoWMaturityTime(); got != 86400 {
		t.Fatalf("unexpected default DPoW maturity time: %d", got)
	}
	if got := cfg.GetDPoWMaturityBlocks(); got.Cmp(big.NewInt(17280)) != 0 {
		t.Fatalf("unexpected default DPoW maturity blocks: %v", got)
	}

	cfg = &ChainConfig{
		DPoWTime:             newUint64(1000),
		MinerRegistryAddress: &addr,
		DPoWMaturityTime:     300,
		DPoWMaturityBlocks:   60,
	}
	if cfg.IsDPoW(999) {
		t.Fatalf("expected DPoW to be inactive before timestamp 1000")
	}
	if !cfg.IsDPoW(1000) {
		t.Fatalf("expected DPoW to be active at timestamp 1000")
	}
	if got := cfg.GetMinerRegistryAddress(); got != addr {
		t.Fatalf("unexpected registry address: %v", got.Hex())
	}
	if got := cfg.GetDPoWMaturityTime(); got != 300 {
		t.Fatalf("unexpected custom DPoW maturity time: %d", got)
	}
	if got := cfg.GetDPoWMaturityBlocks(); got.Cmp(big.NewInt(60)) != 0 {
		t.Fatalf("unexpected custom DPoW maturity blocks: %v", got)
	}
}

func TestIsDPoWImmediateActivation(t *testing.T) {
	addr := common.HexToAddress("0x0000000000000000000000000000000000001234")

	cfg := &ChainConfig{
		DPoWTime:             newUint64(0),
		MinerRegistryAddress: &addr,
	}

	if !cfg.IsDPoW(0) {
		t.Fatalf("expected DPoW to be active at timestamp 0 when DPoWTime is 0")
	}
}

func TestCheckCompatibleDPoW(t *testing.T) {
	addr1 := common.HexToAddress("0x0000000000000000000000000000000000001111")
	addr2 := common.HexToAddress("0x0000000000000000000000000000000000002222")

	tests := []struct {
		name     string
		stored   *ChainConfig
		new      *ChainConfig
		headTime uint64
		wantErr  *ConfigCompatError
	}{
		{
			name:   "dpow time zero on new config does not underflow RewindToTime",
			stored: &ChainConfig{},
			new: &ChainConfig{
				DPoWTime:             newUint64(0),
				MinerRegistryAddress: &addr1,
			},
			headTime: 1,
			wantErr: &ConfigCompatError{
				What:         "DPoW fork timestamp",
				StoredTime:   nil,
				NewTime:      newUint64(0),
				RewindToTime: 0,
			},
		},
		{
			name: "dpow time mismatch before activation is allowed",
			stored: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
			},
			new: &ChainConfig{
				DPoWTime:             newUint64(200),
				MinerRegistryAddress: &addr1,
			},
			headTime: 50,
			wantErr:  nil,
		},
		{
			name:   "dpow time introduced after head is already past activation",
			stored: &ChainConfig{},
			new: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
			},
			headTime: 150,
			wantErr: &ConfigCompatError{
				What:         "DPoW fork timestamp",
				StoredTime:   nil,
				NewTime:      newUint64(100),
				RewindToTime: 99,
			},
		},
		{
			name: "dpow time mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
			},
			new: &ChainConfig{
				DPoWTime:             newUint64(200),
				MinerRegistryAddress: &addr1,
			},
			headTime: 150,
			wantErr: &ConfigCompatError{
				What:         "DPoW fork timestamp",
				StoredTime:   newUint64(100),
				NewTime:      newUint64(200),
				RewindToTime: 99,
			},
		},
		{
			name: "registry mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
			},
			new: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr2,
			},
			headTime: 150,
			wantErr: &ConfigCompatError{
				What:         "DPoW miner registry address",
				StoredTime:   newUint64(100),
				NewTime:      newUint64(100),
				RewindToTime: 99,
			},
		},
		{
			name: "maturity time mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityTime:     300,
			},
			new: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityTime:     600,
			},
			headTime: 150,
			wantErr: &ConfigCompatError{
				What:         "DPoW maturity time",
				StoredTime:   newUint64(100),
				NewTime:      newUint64(100),
				RewindToTime: 99,
			},
		},
		{
			name: "maturity blocks mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityBlocks:   60,
			},
			new: &ChainConfig{
				DPoWTime:             newUint64(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityBlocks:   120,
			},
			headTime: 150,
			wantErr: &ConfigCompatError{
				What:         "DPoW maturity blocks",
				StoredTime:   newUint64(100),
				NewTime:      newUint64(100),
				RewindToTime: 99,
			},
		},
	}

	for _, tt := range tests {
		err := tt.stored.CheckCompatible(tt.new, 0, tt.headTime)
		if !reflect.DeepEqual(err, tt.wantErr) {
			t.Fatalf("%s: unexpected compatibility error: got=%v want=%v", tt.name, err, tt.wantErr)
		}
	}
}
