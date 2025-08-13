// Copyright 2019 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

// Package utils contains internal helper functions for go-ethereum commands.
package utils

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/core"
	"github.com/urfave/cli/v2"
)

func Test_SplitTagsFlag(t *testing.T) {
	tests := []struct {
		name string
		args string
		want map[string]string
	}{
		{
			"2 tags case",
			"host=localhost,bzzkey=123",
			map[string]string{
				"host":   "localhost",
				"bzzkey": "123",
			},
		},
		{
			"1 tag case",
			"host=localhost123",
			map[string]string{
				"host": "localhost123",
			},
		},
		{
			"empty case",
			"",
			map[string]string{},
		},
		{
			"garbage",
			"smth=smthelse=123",
			map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SplitTagsFlag(tt.args); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitTagsFlag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIncentivTestnetFlag(t *testing.T) {
	// Test that MakeGenesis creates the correct genesis for Incentiv testnet
	app := &cli.App{
		Flags: []cli.Flag{
			IncentivTestnetFlag,
		},
		Action: func(ctx *cli.Context) error {
			genesis := MakeGenesis(ctx)
			if genesis == nil {
				t.Fatal("Expected genesis block, got nil")
			}

			if genesis.Config.ChainID.Uint64() != 28802 {
				t.Errorf("Expected chain ID 28802, got %d", genesis.Config.ChainID.Uint64())
			}

			// Verify it matches the default Incentiv testnet genesis
			expected := core.DefaultIncentivTestnetGenesisBlock()
			if genesis.Config.ChainID.Uint64() != expected.Config.ChainID.Uint64() {
				t.Errorf("Genesis chain ID doesn't match default")
			}

			if len(genesis.Alloc) != len(expected.Alloc) {
				t.Errorf("Genesis allocation count doesn't match default")
			}

			return nil
		},
	}

	// Run app with --incentiv-testnet flag
	err := app.Run([]string{"test", "--incentiv-testnet"})
	if err != nil {
		t.Fatalf("Failed to run app: %v", err)
	}
}
