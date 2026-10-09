// Copyright 2026 The go-ethereum Authors
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

package ethconfig

import (
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/core"
)

// TestChainOverridesCarriesEveryField checks that every field of
// core.ChainOverrides is actually populated from the matching field of Config.
//
// The full and the light client both take their overrides from Config.ChainOverrides,
// so one missing assignment here means an operator's --override flag is accepted on
// the command line and then silently ignored — which is what happened when
// OverrideWebAuthnStrict was wired into eth/backend.go but not into les/client.go.
// Driving it by reflection means a new override is covered the moment it is added to
// the struct, rather than when somebody remembers to extend this test.
func TestChainOverridesCarriesEveryField(t *testing.T) {
	overridesType := reflect.TypeOf(core.ChainOverrides{})
	configType := reflect.TypeOf(Config{})

	for i := 0; i < overridesType.NumField(); i++ {
		field := overridesType.Field(i)
		if field.Type.Kind() != reflect.Ptr || field.Type.Elem().Kind() != reflect.Uint64 {
			t.Errorf("%s is a %s; this test only knows how to drive *uint64 overrides and needs extending",
				field.Name, field.Type)
			continue
		}
		if _, ok := configType.FieldByName(field.Name); !ok {
			t.Errorf("core.ChainOverrides.%s has no matching field on ethconfig.Config", field.Name)
			continue
		}

		// Set only this field on an otherwise empty config, and check it comes out
		// the other side.
		value := uint64(1_900_000_000 + i)
		cfg := &Config{}
		reflect.ValueOf(cfg).Elem().FieldByName(field.Name).Set(reflect.ValueOf(&value))

		got := reflect.ValueOf(cfg.ChainOverrides()).Field(i)
		if got.IsNil() {
			t.Errorf("Config.%s is set but ChainOverrides() left %s nil; the flag would be accepted and ignored",
				field.Name, field.Name)
			continue
		}
		if v := got.Elem().Uint(); v != value {
			t.Errorf("ChainOverrides().%s = %d, want %d", field.Name, v, value)
		}
	}

	// An empty config must not arm anything.
	if empty := (&Config{}).ChainOverrides(); empty != (core.ChainOverrides{}) {
		t.Errorf("an empty config produced overrides: %+v", empty)
	}
}
