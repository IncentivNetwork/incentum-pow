// Copyright 2026 The go-ethereum Authors
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

package main

import (
	"strings"
	"testing"
)

// TestDumpConfigCarriesOverrideFlags pins that `geth dumpconfig` writes the fork
// overrides it was started with. It used to go through makeConfigNode alone, so the
// TOML it produced had no OverrideWebAuthnStrict — and a node started from that TOML
// instead of the original command line dropped the override, which is the one way a
// node carried on an override moves back to the bundled schedule.
func TestDumpConfigCarriesOverrideFlags(t *testing.T) {
	geth := runGeth(t, "--override.webauthnstrict", "1800000000", "dumpconfig")
	out := diagnostics(t, geth)
	if have, want := geth.ExitStatus(), 0; have != want {
		t.Fatalf("dumpconfig exit status %d, want %d\n%s", have, want, out)
	}
	if !strings.Contains(out, "OverrideWebAuthnStrict = 1800000000") {
		t.Fatalf("dumpconfig dropped --override.webauthnstrict:\n%s", out)
	}
}
