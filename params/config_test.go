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
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr,
			},
			wantErr: false,
		},
		{
			name: "dpow enabled without registry",
			cfg: &ChainConfig{
				DPoWBlock: big.NewInt(100),
			},
			wantErr: true,
		},
		{
			name: "dpow enabled with zero registry address",
			cfg: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: new(common.Address),
			},
			wantErr: true,
		},
		{
			name: "registry without dpow block is ignored",
			cfg: &ChainConfig{
				MinerRegistryAddress: &addr,
			},
			wantErr: false,
		},
		{
			name: "negative dpow block is rejected",
			cfg: &ChainConfig{
				DPoWBlock:            big.NewInt(-1),
				MinerRegistryAddress: &addr,
			},
			wantErr: true,
		},
		{
			name: "dpow block exceeding uint64 is rejected",
			cfg: &ChainConfig{
				DPoWBlock:            new(big.Int).Lsh(big.NewInt(1), 64),
				MinerRegistryAddress: &addr,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		err := tt.cfg.CheckDPoWConfig()
		if (err != nil) != tt.wantErr {
			t.Fatalf("%s: unexpected error state: err=%v wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

func TestDPoWHelpers(t *testing.T) {
	addr := common.HexToAddress("0x0000000000000000000000000000000000001234")

	cfg := &ChainConfig{}
	if cfg.IsDPoW(big.NewInt(0)) {
		t.Fatalf("expected DPoW to be disabled when DPoWBlock is nil")
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
		DPoWBlock:            big.NewInt(1000),
		MinerRegistryAddress: &addr,
		DPoWMaturityTime:     300,
		DPoWMaturityBlocks:   60,
	}
	if cfg.IsDPoW(big.NewInt(999)) {
		t.Fatalf("expected DPoW to be inactive before block 1000")
	}
	if !cfg.IsDPoW(big.NewInt(1000)) {
		t.Fatalf("expected DPoW to be active at block 1000")
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
		DPoWBlock:            big.NewInt(0),
		MinerRegistryAddress: &addr,
	}

	if !cfg.IsDPoW(big.NewInt(0)) {
		t.Fatalf("expected DPoW to be active at block 0 when DPoWBlock is 0")
	}
}

func TestCheckCompatibleDPoW(t *testing.T) {
	addr1 := common.HexToAddress("0x0000000000000000000000000000000000001111")
	addr2 := common.HexToAddress("0x0000000000000000000000000000000000002222")

	tests := []struct {
		name      string
		stored    *ChainConfig
		new       *ChainConfig
		headBlock uint64
		wantErr   *ConfigCompatError
	}{
		{
			name: "dpow block mismatch before activation is allowed",
			stored: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr1,
			},
			new: &ChainConfig{
				DPoWBlock:            big.NewInt(200),
				MinerRegistryAddress: &addr1,
			},
			headBlock: 50,
			wantErr:   nil,
		},
		{
			name: "dpow block mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr1,
			},
			new: &ChainConfig{
				DPoWBlock:            big.NewInt(200),
				MinerRegistryAddress: &addr1,
			},
			headBlock: 150,
			wantErr: &ConfigCompatError{
				What:          "DPoW fork block",
				StoredBlock:   big.NewInt(100),
				NewBlock:      big.NewInt(200),
				RewindToBlock: 99,
			},
		},
		{
			name: "registry mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr1,
			},
			new: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr2,
			},
			headBlock: 150,
			wantErr: &ConfigCompatError{
				What:          "DPoW miner registry address",
				StoredBlock:   big.NewInt(100),
				NewBlock:      big.NewInt(100),
				RewindToBlock: 99,
			},
		},
		{
			name: "maturity time mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityTime:     300,
			},
			new: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityTime:     600,
			},
			headBlock: 150,
			wantErr: &ConfigCompatError{
				What:          "DPoW maturity time",
				StoredBlock:   big.NewInt(100),
				NewBlock:      big.NewInt(100),
				RewindToBlock: 99,
			},
		},
		{
			name: "maturity blocks mismatch after activation rewinds",
			stored: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityBlocks:   60,
			},
			new: &ChainConfig{
				DPoWBlock:            big.NewInt(100),
				MinerRegistryAddress: &addr1,
				DPoWMaturityBlocks:   120,
			},
			headBlock: 150,
			wantErr: &ConfigCompatError{
				What:          "DPoW maturity blocks",
				StoredBlock:   big.NewInt(100),
				NewBlock:      big.NewInt(100),
				RewindToBlock: 99,
			},
		},
	}

	for _, tt := range tests {
		err := tt.stored.CheckCompatible(tt.new, tt.headBlock, 0)
		if !reflect.DeepEqual(err, tt.wantErr) {
			t.Fatalf("%s: unexpected compatibility error: got=%v want=%v", tt.name, err, tt.wantErr)
		}
	}
}
