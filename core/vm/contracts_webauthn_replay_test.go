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

// Replay of real passkey assertions against both sides of the WebAuthnStrict fork.
// The fork tightens what 0x111 accepts, so the question that decides whether it can
// be activated at all is whether it rejects anything real wallets actually produce.
// Every record here is a mined Incentiv mainnet UserOperation: the precompile
// returned 1 for it under the pre-fork parser, so a post-fork rejection would be a
// wallet locked out of its account.
//
// What this replay answers is narrower than that question, and the difference is in the
// fixture: `input` is reassembled from the event, the transaction and the account's key
// rather than read off the call. So the result is that the rule rejects nothing in the
// reconstruction. Bytes a wallet appended past the payload, and the operations the harvest
// could not rebuild, are outside it; docs/webauthn/replay-validation.md lists both under
// "Coverage limits", and rollout.md makes each a gate before mainnet activation.
//
// The committed fixture is a sample; see core/vm/testdata/webauthn/README.md for how
// it was produced and how to replay a full harvest over this same test by pointing
// WEBAUTHN_OPS_FILE at it.
//
// Every input in the committed sample is canonically encoded, which is the point: it is a
// compatibility corpus. It does not pin the rule itself — the tests in
// contracts_webauthn_test.go do that.

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

const replayEntryPoint = "0x3eC61c5633BBD7Afa9144C6610930489736a72d4"

type webAuthnOpFixture struct {
	Source struct {
		ChainID    int    `json:"chainId"`
		EntryPoint string `json:"entryPoint"`
		Note       string `json:"note"`
	} `json:"source"`
	Ops []struct {
		Block   uint64 `json:"block"`
		TxHash  string `json:"txHash"`
		Sender  string `json:"sender"`
		Input   string `json:"input"`
		Comment string `json:"comment,omitempty"`
	} `json:"ops"`
}

func (f webAuthnOpFixture) validateSource() error {
	if f.Source.ChainID != 24101 {
		return fmt.Errorf("fixture chainId is %d, want 24101 (Incentiv mainnet)", f.Source.ChainID)
	}
	if !common.IsHexAddress(f.Source.EntryPoint) || !strings.EqualFold(f.Source.EntryPoint, replayEntryPoint) {
		return fmt.Errorf("fixture entryPoint is %q, want %s", f.Source.EntryPoint, replayEntryPoint)
	}
	return nil
}

func TestWebAuthnReplayFixtureSource(t *testing.T) {
	for _, tc := range []struct {
		name      string
		source    string
		wantError string
	}{
		{"committed address", `{"chainId":24101,"entryPoint":"0x3ec61c5633bbd7afa9144c6610930489736a72d4"}`, ""},
		{"mixed case", `{"chainId":24101,"entryPoint":"0x3eC61c5633BBD7Afa9144C6610930489736a72d4"}`, ""},
		{"missing address", `{"chainId":24101}`, "entryPoint"},
		{"wrong address", `{"chainId":24101,"entryPoint":"0x0000000000000000000000000000000000000001"}`, "entryPoint"},
		{"invalid address", `{"chainId":24101,"entryPoint":"0x3eC61c"}`, "entryPoint"},
		{"wrong chain", `{"chainId":1,"entryPoint":"0x3ec61c5633bbd7afa9144c6610930489736a72d4"}`, "chainId"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fixture webAuthnOpFixture
			if err := json.Unmarshal([]byte(`{"source":`+tc.source+`}`), &fixture); err != nil {
				t.Fatal(err)
			}
			err := fixture.validateSource()
			if tc.wantError == "" && err != nil {
				t.Fatal(err)
			}
			if tc.wantError != "" && (err == nil || !strings.Contains(err.Error(), tc.wantError)) {
				t.Fatalf("validateSource() = %v, want %s", err, tc.wantError)
			}
		})
	}
}

// canonicalPayloadLength is the length a signaturePayload has when it ends after s,
// which is where the wallet's encoding ends: authDataLength(4) ‖ authData ‖
// userVerification(1) ‖ clientDataJSONLength(4) ‖ clientDataJSON ‖
// challengeLocation(4) ‖ responseTypeLocation(4) ‖ r(32) ‖ s(32).
func canonicalPayloadLength(authDataLen, clientDataJSONLen uint32) int {
	return 81 + int(authDataLen) + int(clientDataJSONLen)
}

func TestWebAuthnVerifyMainnetReplay(t *testing.T) {
	path := os.Getenv("WEBAUTHN_OPS_FILE")
	if path == "" {
		path = "testdata/webauthn/mainnet-passkey-ops.json"
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture webAuthnOpFixture
	if err := json.Unmarshal(blob, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	// A fixture from another chain or EntryPoint would pass and say nothing, and so would an empty
	// one — a harvest over the wrong block range, or a scan that errored early, must
	// not read as "no regressions".
	if err := fixture.validateSource(); err != nil {
		t.Fatal(err)
	}
	// The corpus floor is its own switch. It used to be lifted by the mere presence of
	// WEBAUTHN_OPS_FILE, so replaying a larger harvest — the case that matters most —
	// also turned the size check off, and a harvest that errored early would have read
	// as a clean run. 500 is the committed sample. An operator replaying a single
	// captured operation, which docs/webauthn/rollout.md tells them to do with a failing
	// one, says so with WEBAUTHN_OPS_MIN=1.
	minOps := 500
	if v := os.Getenv("WEBAUTHN_OPS_MIN"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("WEBAUTHN_OPS_MIN=%q is not a positive integer", v)
		}
		minOps = n
	}
	if len(fixture.Ops) < minOps {
		t.Fatalf("fixture holds %d operations, want at least %d", len(fixture.Ops), minOps)
	}
	t.Logf("replaying %d mainnet passkey operations from %s", len(fixture.Ops), path)

	var (
		preForkAccepted  int
		bothAccepted     int
		falseRejects     int
		newlyAccepted    int
		nonCanonical     int
		zeroTopByteInSig int             // r or s, which is drawn fresh per assertion
		zeroTopByteKeys  map[string]bool // x or y, which is fixed per account
		senders          = map[string]int{}
	)
	zeroTopByteKeys = map[string]bool{}
	for _, op := range fixture.Ops {
		input, err := hex.DecodeString(strings.TrimPrefix(op.Input, "0x"))
		if err != nil {
			t.Fatalf("block %d %s: bad input hex: %v", op.Block, op.TxHash, err)
		}

		// The pre-fork parser writes into the buffer it is given, so each run needs
		// its own copy.
		pre := runWebAuthn(t, false, append([]byte{}, input...))
		post := runWebAuthn(t, true, append([]byte{}, input...))
		switch {
		case pre && post:
			preForkAccepted++
			bothAccepted++
		case pre && !post:
			preForkAccepted++
			falseRejects++
			t.Errorf("block %d tx %s sender %s: accepted before the fork, rejected after it",
				op.Block, op.TxHash, op.Sender)
		case !pre && post:
			newlyAccepted++
		default:
			// Every record was mined, so the pre-fork parser returned 1 for it on
			// chain. Rejecting it here means the replay does not reconstruct what
			// the precompile was handed — a harvester bug, or a key read at head
			// that the account has since rotated — and the "0 false rejects" result
			// says nothing about the operations in that state.
			t.Errorf("block %d tx %s sender %s: rejected by both parsers, though it verified on chain",
				op.Block, op.TxHash, op.Sender)
		}

		// Independently of verification: was the payload canonically encoded, and did
		// any of its four words have a zero top byte? Both are properties of the bytes
		// alone, and both are what the fork changes the treatment of.
		//
		// The shortest canonical input is 32 + 81 + 64 = 177 bytes, so anything below
		// that is malformed rather than short. Check it before slicing: the message,
		// the appended key and the payload's own length prefix have to be there for
		// the reads below to be in range.
		if len(input) < 177 {
			nonCanonical++
			t.Errorf("block %d tx %s: input is %d bytes, the shortest canonical input is 177",
				op.Block, op.TxHash, len(input))
			continue
		}
		var (
			payload     = input[32 : len(input)-64]
			authDataLen = binary.BigEndian.Uint32(payload[0:4])
		)
		if uint64(len(payload)) < 9+uint64(authDataLen) {
			nonCanonical++
			t.Errorf("block %d tx %s: payload is %d bytes, too short for its declared authDataLength %d",
				op.Block, op.TxHash, len(payload), authDataLen)
			continue
		}
		clientDataJSONLen := binary.BigEndian.Uint32(payload[5+authDataLen : 9+authDataLen])
		if len(payload) != canonicalPayloadLength(authDataLen, clientDataJSONLen) {
			nonCanonical++
			t.Errorf("block %d tx %s: payload is %d bytes, canonical length is %d",
				op.Block, op.TxHash, len(payload), canonicalPayloadLength(authDataLen, clientDataJSONLen))
			continue
		}
		senders[op.Sender]++
		words := input[len(input)-128:] // r ‖ s ‖ x ‖ y
		if words[0] == 0 || words[32] == 0 {
			zeroTopByteInSig++
		}
		if words[64] == 0 || words[96] == 0 {
			zeroTopByteKeys[op.Sender] = true
		}
	}

	t.Logf("pre-fork accepted:            %d / %d", preForkAccepted, len(fixture.Ops))
	t.Logf("accepted on both sides:       %d", bothAccepted)
	t.Logf("rejected only after the fork: %d   <- must be 0", falseRejects)
	t.Logf("accepted only after the fork: %d", newlyAccepted)
	t.Logf("non-canonical payloads:       %d   <- must be 0", nonCanonical)
	t.Logf("distinct accounts:            %d", len(senders))
	// r and s are drawn fresh per assertion, so about 1 in 128 assertions has a zero
	// top byte in one of them. Every such assertion is rejected before the fork, so
	// none of them can reach a block: a count near zero here is the live evidence
	// that the word encoding was costing wallets signatures.
	t.Logf("assertions with a zero top byte in r or s: %d (expected ~%d if they were accepted)",
		zeroTopByteInSig, len(fixture.Ops)*2/256)
	t.Logf("accounts whose key has a zero top byte in x or y: %d", len(zeroTopByteKeys))

	if preForkAccepted != len(fixture.Ops) {
		t.Errorf("the pre-fork parser verified %d of %d operations; every one of them verified on chain",
			preForkAccepted, len(fixture.Ops))
	}
	if falseRejects != 0 {
		t.Errorf("%d operation(s) stopped verifying at the fork; it cannot be activated as it stands", falseRejects)
	}
	if nonCanonical != 0 {
		t.Errorf("%d payload(s) are not canonically encoded; the fork would reject them", nonCanonical)
	}
}
