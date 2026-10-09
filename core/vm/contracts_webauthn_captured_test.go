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

// One captured 0x111 input through both parsers. This is the tool for attributing a
// rejection to the fork rather than to signature validation in general: take the inner
// call's `input` from a callTracer trace and ask what each parser returns for it.
//
// The replay harness in contracts_webauthn_replay_test.go answers a different question —
// whether a corpus of mined mainnet operations keeps verifying — so it refuses a file from
// any other chain and counts "accepted before the fork, rejected after it" as the
// regression it exists to catch. For an input captured from a deliberately non-canonical
// operation that outcome is the expected one, which is what WEBAUTHN_EXPECT states.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
)

// parseWebAuthnExpectation reads WEBAUTHN_EXPECT: "<pre>,<post>", each 0 or 1, for what
// the pre-fork and post-fork parsers are expected to return.
func parseWebAuthnExpectation(s string) (pre, post bool, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return false, false, fmt.Errorf("WEBAUTHN_EXPECT=%q: want <pre>,<post> with each 0 or 1", s)
	}
	var bits [2]bool
	for i, p := range parts {
		switch strings.TrimSpace(p) {
		case "0":
		case "1":
			bits[i] = true
		default:
			return false, false, fmt.Errorf("WEBAUTHN_EXPECT=%q: want <pre>,<post> with each 0 or 1", s)
		}
	}
	return bits[0], bits[1], nil
}

// webAuthnVerdictMismatch reports how the two parsers' answers differ from an expectation,
// or nil when they match.
func webAuthnVerdictMismatch(pre, post bool, expect string) error {
	wantPre, wantPost, err := parseWebAuthnExpectation(expect)
	if err != nil {
		return err
	}
	if pre != wantPre || post != wantPost {
		return fmt.Errorf("pre-fork parser returned %s and post-fork parser returned %s, want %s and %s",
			webAuthnWord(pre), webAuthnWord(post), webAuthnWord(wantPre), webAuthnWord(wantPost))
	}
	return nil
}

func webAuthnWord(accepted bool) string {
	if accepted {
		return "1"
	}
	return "0"
}

// checkCapturedWebAuthnInput is the body of TestWebAuthnVerifyCapturedInput, so that the
// in-tree cases below can run it under t.Setenv.
func checkCapturedWebAuthnInput(t *testing.T) {
	t.Helper()
	raw := os.Getenv("WEBAUTHN_INPUT")
	if raw == "" {
		t.Skip("set WEBAUTHN_INPUT to the hex input of one 0x111 call")
	}
	input, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(raw), "0x"))
	if err != nil {
		t.Fatalf("WEBAUTHN_INPUT is not hex: %v", err)
	}
	// The pre-fork parser writes into the buffer it is given, so each run needs its own copy.
	pre := runWebAuthn(t, false, append([]byte{}, input...))
	post := runWebAuthn(t, true, append([]byte{}, input...))
	t.Logf("%d bytes: pre-fork parser returned %s, post-fork parser returned %s",
		len(input), webAuthnWord(pre), webAuthnWord(post))
	if expect := os.Getenv("WEBAUTHN_EXPECT"); expect != "" {
		if err := webAuthnVerdictMismatch(pre, post, expect); err != nil {
			t.Fatal(err)
		}
	}
}

// TestWebAuthnVerifyCapturedInput runs the input in WEBAUTHN_INPUT — the hex bytes of one
// 0x111 call, as a callTracer reports the inner call's `input` — through both parsers and
// logs what each returned. Without WEBAUTHN_INPUT it is skipped. With WEBAUTHN_EXPECT set to
// "<pre>,<post>" it also fails unless both answers match, so a check that expects a devnet
// rejection to be this rule's can say so:
//
//	cd core/vm && WEBAUTHN_INPUT=0x… WEBAUTHN_EXPECT=1,0 go test -run TestWebAuthnVerifyCapturedInput -v .
func TestWebAuthnVerifyCapturedInput(t *testing.T) {
	checkCapturedWebAuthnInput(t)
}

// TestWebAuthnVerifyCapturedInputCases runs the same check on inputs this package can make,
// so the tool is exercised in CI and not only when someone reaches for it: a canonical
// assertion verifies on both sides, and the same bytes with one appended verify only before
// the fork — the shape a rejection attributed to this rule has.
func TestWebAuthnVerifyCapturedInputCases(t *testing.T) {
	message := sha256.Sum256([]byte("TestWebAuthnVerifyCapturedInputCases"))
	canonical := newWebAuthnAssertion(t, message[:], false).canonicalInput()
	for _, tc := range []struct {
		name   string
		input  []byte
		expect string
	}{
		{"canonical", canonical, "1,1"},
		{"trailing byte", append(append([]byte{}, canonical...), 0), "1,0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WEBAUTHN_INPUT", "0x"+hex.EncodeToString(tc.input))
			t.Setenv("WEBAUTHN_EXPECT", tc.expect)
			checkCapturedWebAuthnInput(t)
		})
	}

	// The expectation is compared, not just parsed: a trailing-byte input stated as
	// verifying on both sides is a mismatch, and so is a canonical one stated as rejected.
	if err := webAuthnVerdictMismatch(true, false, "1,1"); err == nil {
		t.Error("an input rejected after the fork matched an expectation that it verifies on both sides")
	}
	if err := webAuthnVerdictMismatch(true, true, "1,0"); err == nil {
		t.Error("an input verifying on both sides matched an expectation that the fork rejects it")
	}
	if err := webAuthnVerdictMismatch(true, false, " 1 , 0 "); err != nil {
		t.Errorf("spaces around the bits were refused: %v", err)
	}
	// Anything but two bits is refused rather than read as some expectation.
	for _, bad := range []string{"", "1", "1,0,1", "yes,no", "2,0", "1;0"} {
		if _, _, err := parseWebAuthnExpectation(bad); err == nil {
			t.Errorf("WEBAUTHN_EXPECT=%q was accepted", bad)
		}
	}
}
