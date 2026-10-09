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
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestWebAuthnFixtureScripts runs the JS tests beside the fixture: the decisions the
// harvesting scripts make that nothing on the Go side re-checks.
//
// TestWebAuthnVerifyMainnetReplay covers their output, not their decisions. It reads inputs
// that have already been reconstructed, so a signature classified down the wrong path only
// surfaces if the bytes that come out happen to fail both parsers. They did, for the case
// that was wrong — the account treats a 65-byte signature as plain ECDSA whatever its first
// byte says, and the script was reading the version byte first — but relying on that is
// relying on an accident of lengths. The sampler's checks are further out of reach again:
// they decide which operations the fixture contains and what it declares them to be, and the
// replay reads that declaration rather than questioning it.
//
// Every *_test.js in the directory runs, rather than a list kept here, and an empty match is
// a failure: a rename that put them all out of reach would otherwise read as a pass.
//
// Driven from here rather than from a workflow step so that they run wherever the Go tests
// run, CI included. Skipped where node is absent, which includes the plain golang image.
func TestWebAuthnFixtureScripts(t *testing.T) {
	scripts, err := filepath.Glob(filepath.Join("testdata", "webauthn", "*_test.js"))
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) == 0 {
		t.Fatal("no *_test.js beside the fixture: the JS checks are not being run by anything")
	}
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		// Skipping where node is absent is fine for a local run, but a skip in CI would
		// make "the scripts are covered" unverifiable from the log — which is how the
		// first version of this test came to pass locally and fail there.
		if os.Getenv("CI") != "" {
			t.Fatalf("node is not installed, so %v did not run; those checks would be unguarded", scripts)
		}
		t.Skip("node not installed: run core/vm/testdata/webauthn/*_test.js by hand")
	}
	for _, script := range scripts {
		t.Run(filepath.Base(script), func(t *testing.T) {
			out, err := exec.Command(nodeBin, script).CombinedOutput()
			if err != nil {
				t.Fatalf("%s failed: %v\n%s", script, err, out)
			}
			t.Logf("%s", bytes.TrimSpace(out))
		})
	}
}
