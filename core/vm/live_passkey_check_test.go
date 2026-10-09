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

package vm

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWebAuthnLivePasskeyCheckInput ties testdata/webauthn/live_passkey_check.js to the
// consensus code it is a check of. The script's --print-input mode writes the one input it
// would send to 0x111 on a live chain, and that input goes through both parsers here: both
// must accept it, and the post-fork parser alone must reject it with one byte appended.
// Without this, the layout the script packs could drift from the account's, and a PASS it
// printed against a live chain would say nothing about what wallets produce.
//
// Skipped where node is absent, except in CI, where a skip would make the check
// unverifiable from the log — the same rule as TestWebAuthnFixtureScripts.
func TestWebAuthnLivePasskeyCheckInput(t *testing.T) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatal("node is not installed, so live_passkey_check.js did not run; its input would be unguarded")
		}
		t.Skip("node not installed: run core/vm/testdata/webauthn/live_passkey_check.js --print-input by hand")
	}
	script := filepath.Join("testdata", "webauthn", "live_passkey_check.js")
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(nodeBin, script, "--print-input")
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s --print-input: %v\n%s", script, err, stderr.String())
	}
	input, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(stdout.String()), "0x"))
	if err != nil || len(input) == 0 {
		t.Fatalf("%s --print-input printed %q, want one hex input", script, stdout.String())
	}
	for _, tc := range []struct {
		name   string
		input  []byte
		expect string
	}{
		{"canonical", input, "1,1"},
		{"trailing byte", append(append([]byte{}, input...), 0), "1,0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The pre-fork parser writes into the buffer it is given, so each run gets a copy.
			pre := runWebAuthn(t, false, append([]byte{}, tc.input...))
			post := runWebAuthn(t, true, append([]byte{}, tc.input...))
			if err := webAuthnVerdictMismatch(pre, post, tc.expect); err != nil {
				t.Fatalf("%d bytes: %v", len(tc.input), err)
			}
		})
	}
}
