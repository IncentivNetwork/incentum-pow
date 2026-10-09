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

// Behavioural pinning for the WebAuthn precompile (0x111) on both sides of the
// WebAuthnStrict fork. The pre-fork expectations are as load-bearing as the
// post-fork ones: blocks mined before activation are replayed with the pre-fork
// parser, so its behaviour must not drift.
//
// Everything here uses freshly generated throwaway P-256 keys. No network, no
// account, no key from any live chain.

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/params"
)

// webAuthnAssertion is a complete WebAuthn assertion over some 32-byte message,
// plus the key that produced it.
type webAuthnAssertion struct {
	message        []byte
	authData       []byte
	userVerified   byte
	clientDataJSON string
	r, s, x, y     []byte // 32-byte big-endian words, as the wire format carries them

	// challengeLoc and responseTypeLoc override where the payload claims the two
	// clientDataJSON fields start. Unset, payload() finds them, which is what a wallet
	// does; a test sets one when it needs the precompile to read a field that is there
	// but wrong, rather than be turned away by the bounds check in front of it.
	challengeLoc, responseTypeLoc *uint32
}

// locationOf returns the override if there is one, and otherwise where the field
// actually starts.
func (a webAuthnAssertion) locationOf(override *uint32, needle string) uint32 {
	if override != nil {
		return *override
	}
	return uint32(strings.Index(a.clientDataJSON, needle))
}

// newWebAuthnAssertion signs message with a fresh key and returns the assertion.
// Unless keepLeadingZero is set it retries until none of r, s, x, y has a zero
// top byte, so a test that is not about the word encoding cannot flake on it.
func newWebAuthnAssertion(t *testing.T, message []byte, keepLeadingZero bool) webAuthnAssertion {
	t.Helper()
	return assertionMatching(t, message, "the requested word shape", func(a webAuthnAssertion) bool {
		return keepLeadingZero == a.hasLeadingZeroWord()
	})
}

// newWebAuthnAssertionZeroIn signs message until the named word — "r", "s", "x" or "y" — is
// the *only* one with a zero top byte, so a test can cover that word rather than whichever
// word a random assertion happened to produce.
//
// The other three have to be non-zero for the pre-fork half of such a test to mean anything:
// pre-fork, any zero-topped word is enough to make the verification fail, so an assertion
// carrying two of them would still be rejected after the old encoding was repaired for the
// word under test. That is about one run in 85 — three other words, one in 256 each — and it
// would be a silent gap rather than a failure. Searching for exactly one zero costs the same
// ~256 assertions, since the other three are overwhelmingly non-zero already.
func newWebAuthnAssertionZeroIn(t *testing.T, message []byte, word string) webAuthnAssertion {
	t.Helper()
	return assertionMatching(t, message, "a zero top byte in "+word+" and in no other word", func(a webAuthnAssertion) bool {
		if a.word(t, word)[0] != 0 {
			return false
		}
		for _, other := range []string{"r", "s", "x", "y"} {
			if other != word && a.word(t, other)[0] == 0 {
				return false
			}
		}
		return true
	})
}

// word returns r, s, x or y by name.
func (a webAuthnAssertion) word(t *testing.T, name string) []byte {
	t.Helper()
	switch name {
	case "r":
		return a.r
	case "s":
		return a.s
	case "x":
		return a.x
	case "y":
		return a.y
	}
	t.Fatalf("unknown word %q", name)
	return nil
}

// assertionMatching generates assertions until want accepts one, or gives up and says
// which shape it was looking for.
func assertionMatching(t *testing.T, message []byte, what string, want func(webAuthnAssertion) bool) webAuthnAssertion {
	t.Helper()
	for attempt := 0; attempt < 100000; attempt++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		authData := make([]byte, 37) // 32-byte rpIdHash ‖ flags ‖ 4-byte counter
		authData[32] = flagUP
		binary.BigEndian.PutUint32(authData[33:37], uint32(attempt))

		clientDataJSON := `{"type":"webauthn.get","challenge":"` +
			base64.RawURLEncoding.EncodeToString(message) +
			`","origin":"https://example.invalid"}`

		clientDataJSONHash := sha256.Sum256([]byte(clientDataJSON))
		signed := sha256.Sum256(append(append([]byte{}, authData...), clientDataJSONHash[:]...))

		r, s, err := ecdsa.Sign(rand.Reader, key, signed[:])
		if err != nil {
			t.Fatal(err)
		}
		a := webAuthnAssertion{
			message:        message,
			authData:       authData,
			clientDataJSON: clientDataJSON,
			r:              word32(r), s: word32(s), x: word32(key.X), y: word32(key.Y),
		}
		if want(a) {
			return a
		}
	}
	t.Fatalf("could not generate an assertion with %s", what)
	return webAuthnAssertion{}
}

func (a webAuthnAssertion) hasLeadingZeroWord() bool {
	for _, w := range [][]byte{a.r, a.s, a.x, a.y} {
		if w[0] == 0 {
			return true
		}
	}
	return false
}

// payload returns signaturePayload exactly as a wallet produces it: it ends after
// s, and the caller is the one that appends the account's public key.
func (a webAuthnAssertion) payload() []byte {
	cdj := []byte(a.clientDataJSON)
	var p []byte
	p = append(p, be32(uint32(len(a.authData)))...)
	p = append(p, a.authData...)
	p = append(p, a.userVerified)
	p = append(p, be32(uint32(len(cdj)))...)
	p = append(p, cdj...)
	p = append(p, be32(a.locationOf(a.challengeLoc, `"challenge":"`))...)
	p = append(p, be32(a.locationOf(a.responseTypeLoc, `"type":"webauthn.get"`))...)
	p = append(p, a.r...)
	p = append(p, a.s...)
	return p
}

// input reproduces what the calling contract hands to 0x111:
// abi.encodePacked(message, signaturePayload, publicKey.x, publicKey.y).
// Each call returns a fresh backing array, because the pre-fork precompile
// writes into the buffer it is given.
func (a webAuthnAssertion) input(payload, keyX, keyY []byte) []byte {
	in := make([]byte, 0, 32+len(payload)+len(keyX)+len(keyY))
	in = append(in, a.message...)
	in = append(in, payload...)
	in = append(in, keyX...)
	in = append(in, keyY...)
	return in
}

// canonicalInput is the honest encoding: the payload ends after s and the real
// key is the last 64 bytes.
func (a webAuthnAssertion) canonicalInput() []byte {
	return a.input(a.payload(), a.x, a.y)
}

func be32(v uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, v)
	return b
}

// word32 is the 32-byte big-endian encoding of n, leading zeros included.
func word32(n *big.Int) []byte {
	return math.PaddedBigBytes(n, 32)
}

func randomKeyWords(t *testing.T) (x, y []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return word32(key.X), word32(key.Y)
}

// runWebAuthn calls the precompile in pre-fork or post-fork mode and reports
// whether it returned the 32-byte word 1.
func runWebAuthn(t *testing.T, strict bool, input []byte) bool {
	t.Helper()
	ret, err := (&webAuthnVerify{strictInput: strict}).Run(input)
	if err != nil {
		t.Fatalf("Run returned an error: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("Run returned %d bytes, want 32", len(ret))
	}
	if bytes.Equal(ret, false32Byte) {
		return false
	}
	if !bytes.Equal(ret, true32Byte) {
		t.Fatalf("Run returned neither 0 nor 1: %x", ret)
	}
	return true
}

// TestWebAuthnVerifyCanonicalInput is the compatibility floor: the encoding real
// wallets produce verifies identically on both sides of the fork, and a canonical
// input carrying some other account's key is rejected on both sides.
func TestWebAuthnVerifyCanonicalInput(t *testing.T) {
	message := sha256.Sum256([]byte("TestWebAuthnVerifyCanonicalInput"))
	a := newWebAuthnAssertion(t, message[:], false)
	otherX, otherY := randomKeyWords(t)

	for _, strict := range []bool{false, true} {
		if !runWebAuthn(t, strict, a.canonicalInput()) {
			t.Errorf("strict=%v: canonical input with the signing key was rejected", strict)
		}
		if runWebAuthn(t, strict, a.input(a.payload(), otherX, otherY)) {
			t.Errorf("strict=%v: canonical input with an unrelated key was accepted", strict)
		}
	}
}

// TestWebAuthnVerifyNonCanonicalWordPositions pins the fork's reason for existing:
// a payload whose declared lengths place x and y inside itself, so the last four
// words of the input are not the ones the parser reads. Pre-fork that input is
// accepted; post-fork it is not canonical and is rejected.
func TestWebAuthnVerifyNonCanonicalWordPositions(t *testing.T) {
	message := sha256.Sum256([]byte("TestWebAuthnVerifyNonCanonicalWordPositions"))
	a := newWebAuthnAssertion(t, message[:], false)

	// The payload is extended with the signing key, so the parser locates x,y
	// inside it; the key the caller appends becomes trailing bytes it never reads.
	extended := append(append(append([]byte{}, a.payload()...), a.x...), a.y...)
	accountX, accountY := randomKeyWords(t)

	// Each run gets its own buffer. Sharing one would hand the post-fork run an
	// input the pre-fork run had already written a hash into, over
	// clientDataJSONLength among other things, so it would reject a corrupted
	// length rather than this input's length and the assertion below would hold
	// however the length rule behaved.
	build := func() []byte { return a.input(extended, accountX, accountY) }

	if want := 32 + len(a.payload()) + 64 + 64; len(build()) != want {
		t.Fatalf("input length %d, want %d", len(build()), want)
	}
	if !runWebAuthn(t, false, build()) {
		t.Error("pre-fork: the non-canonical input was rejected; pre-fork behaviour has " +
			"drifted, blocks mined before activation may no longer replay")
	}
	if runWebAuthn(t, true, build()) {
		t.Error("post-fork: the non-canonical input was accepted")
	}
}

// TestWebAuthnVerifyTrailingBytes checks the bound itself rather than an attack
// built on it: post-fork the input has to end exactly at the last field, so a
// single extra byte is as fatal as a whole appended key, while anything shorter
// than canonical was already rejected before the fork.
func TestWebAuthnVerifyTrailingBytes(t *testing.T) {
	message := sha256.Sum256([]byte("TestWebAuthnVerifyTrailingBytes"))
	a := newWebAuthnAssertion(t, message[:], false)

	// Each case builds its input afresh, because the pre-fork parser writes into
	// the buffer it is handed.
	for _, tc := range []struct {
		name     string
		input    func() []byte
		preFork  bool
		postFork bool
	}{
		{"canonical", a.canonicalInput, true, true},
		{"one trailing byte", func() []byte { return append(a.canonicalInput(), 0x00) }, true, false},
		{"64 trailing bytes", func() []byte { return append(a.canonicalInput(), make([]byte, 64)...) }, true, false},
		{"one byte short", func() []byte { in := a.canonicalInput(); return in[:len(in)-1] }, false, false},
	} {
		if got := runWebAuthn(t, false, tc.input()); got != tc.preFork {
			t.Errorf("pre-fork %q: accepted=%v, want %v", tc.name, got, tc.preFork)
		}
		if got := runWebAuthn(t, true, tc.input()); got != tc.postFork {
			t.Errorf("post-fork %q: accepted=%v, want %v", tc.name, got, tc.postFork)
		}
	}
}

// TestWebAuthnVerifyInputNotMutated covers the aliasing fix. authenticatorData is
// a sub-slice of the input with spare capacity, so pre-fork the message buffer is
// assembled by writing 32 bytes over whatever follows it — in a real call, the
// calling contract's memory. Post-fork the input is left alone.
func TestWebAuthnVerifyInputNotMutated(t *testing.T) {
	message := sha256.Sum256([]byte("TestWebAuthnVerifyInputNotMutated"))
	a := newWebAuthnAssertion(t, message[:], false)

	strictInput := a.canonicalInput()
	before := append([]byte{}, strictInput...)
	if !runWebAuthn(t, true, strictInput) {
		t.Fatal("post-fork: canonical input was rejected")
	}
	if !bytes.Equal(strictInput, before) {
		t.Errorf("post-fork: Run modified its input\n before %x\n after  %x", before, strictInput)
	}

	// The pre-fork write is asserted so that the fork is known to be the only thing
	// that stops it, and so a future edit cannot quietly change pre-fork consensus.
	legacyInput := a.canonicalInput()
	before = append([]byte{}, legacyInput...)
	runWebAuthn(t, false, legacyInput)
	if bytes.Equal(legacyInput, before) {
		t.Error("pre-fork: Run no longer writes into its input; pre-fork behaviour has drifted")
	}
	mutatedFrom := 36 + len(a.authData)
	if !bytes.Equal(legacyInput[:mutatedFrom], before[:mutatedFrom]) ||
		!bytes.Equal(legacyInput[mutatedFrom+32:], before[mutatedFrom+32:]) {
		t.Errorf("pre-fork: the write landed outside the 32 bytes after authenticatorData")
	}
}

// TestWebAuthnVerifyLeadingZeroWords covers the word-encoding fix, once for each of the
// four words. Pre-fork each of r, s, x, y is re-serialised through big.Int, which drops
// leading zero bytes, and copied left-aligned into a 32-byte slot — so a word with a zero
// top byte is scaled up by 256 and an otherwise valid assertion is rejected. About 1 in 65
// assertions has such a word.
//
// One case per word, not one assertion with a zero top byte somewhere. The strict path
// copies all four words with a single copy today, so one case would exercise the same line
// — but it would stop covering three of the words the moment anything handles them
// separately, and it would say so only in the quarter of runs where the random assertion
// happened to carry its zero in the word that broke. Searching per word costs milliseconds
// and makes each run cover all four.
//
// Each case also requires the other three words to be non-zero, which is what makes the
// pre-fork assertion in it specific to the word under test rather than to whichever word
// happened to carry a zero.
func TestWebAuthnVerifyLeadingZeroWords(t *testing.T) {
	for _, word := range []string{"r", "s", "x", "y"} {
		word := word
		t.Run(word, func(t *testing.T) {
			message := sha256.Sum256([]byte("TestWebAuthnVerifyLeadingZeroWords/" + word))
			a := newWebAuthnAssertionZeroIn(t, message[:], word)
			for _, w := range []string{"r", "s", "x", "y"} {
				if zero := a.word(t, w)[0] == 0; zero != (w == word) {
					t.Fatalf("generated assertion has a zero top byte in %s, want it in %s alone", w, word)
				}
			}

			if !runWebAuthn(t, true, a.canonicalInput()) {
				t.Errorf("post-fork: valid assertion with a zero top byte in %s was rejected", word)
			}
			if runWebAuthn(t, false, a.canonicalInput()) {
				t.Errorf("pre-fork: assertion with a zero top byte in %s verified; pre-fork behaviour has drifted", word)
			}
		})
	}
}

// TestWebAuthnPrecompileForkGate checks the wiring: which implementation 0x111
// resolves to is decided by the block timestamp against WebAuthnStrictTime, and
// nothing else about the precompile set changes.
func TestWebAuthnPrecompileForkGate(t *testing.T) {
	const activation = 1_000_000

	config := *params.TestChainConfig
	config.WebAuthnStrictTime = func() *uint64 { v := uint64(activation); return &v }()

	// precompile resolution does not read state, so the EVM needs no StateDB here.
	for _, tc := range []struct {
		time       uint64
		wantStrict bool
	}{
		{activation - 1, false},
		{activation, true},
		{activation + 1, true},
	} {
		evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: tc.time}, TxContext{}, nil, &config, Config{})
		p, ok := evm.precompile(webAuthnVerifyAddress())
		if !ok {
			t.Fatalf("time %d: 0x111 is not a precompile", tc.time)
		}
		if got := p.(*webAuthnVerify).strictInput; got != tc.wantStrict {
			t.Errorf("time %d: strictInput=%v, want %v", tc.time, got, tc.wantStrict)
		}
		// The address set is unchanged by the fork, and so is the gas schedule. Both
		// have to be compared against something the fork cannot move: the addresses
		// against Berlin's own set rather than its size, and the gas against the
		// numbers, since a schedule is a consensus quantity and comparing a method
		// with itself pins nothing.
		if got, want := sortedAddresses(ActivePrecompiles(config.Rules(big.NewInt(1), false, tc.time))), sortedAddresses(PrecompiledAddressesBerlin); !reflect.DeepEqual(got, want) {
			t.Errorf("time %d: the active precompile set changed:\n got %v\nwant %v", tc.time, got, want)
		}
		if got, want := p.RequiredGas(nil), uint64(3000+6900); got != want {
			t.Errorf("time %d: RequiredGas=%d, want %d", tc.time, got, want)
		}
	}

	// The fork must change one entry and nothing else, so that a precompile added
	// to the Berlin set cannot go missing after activation.
	if len(PrecompiledContractsWebAuthnStrict) != len(PrecompiledContractsBerlin) {
		t.Errorf("the strict set has %d entries, Berlin has %d",
			len(PrecompiledContractsWebAuthnStrict), len(PrecompiledContractsBerlin))
	}
	for addr, berlin := range PrecompiledContractsBerlin {
		strict, ok := PrecompiledContractsWebAuthnStrict[addr]
		switch {
		case !ok:
			t.Errorf("%s is in the Berlin set but not in the strict set", addr)
		case addr == webAuthnVerifyAddress():
			if !strict.(*webAuthnVerify).strictInput {
				t.Error("the strict set carries the pre-fork WebAuthn parser")
			}
			if berlin.(*webAuthnVerify).strictInput {
				t.Error("the Berlin set carries the post-fork WebAuthn parser")
			}
		case strict != berlin:
			t.Errorf("%s differs between the Berlin and strict sets", addr)
		}
	}

	// With no activation timestamp the precompile keeps its pre-fork behaviour
	// forever, which is what every chain other than the Incentiv networks gets.
	evm := NewEVM(BlockContext{BlockNumber: big.NewInt(1), Time: activation}, TxContext{}, nil, params.TestChainConfig, Config{})
	p, _ := evm.precompile(webAuthnVerifyAddress())
	if p.(*webAuthnVerify).strictInput {
		t.Error("unset WebAuthnStrictTime activated the fork")
	}
}

// TestWebAuthnVerifyMalformedInput runs a corpus of malformed and boundary inputs
// through both parsers. Neither may panic or return anything other than a 32-byte
// word, whatever the declared lengths claim. The strict parser has three branches
// of its own — the equality check, the single-slice word copy and the streaming
// hash — and every one of them indexes the input, so the corpus has to reach both
// sides of the fork rather than only the pre-fork parser the older tests use.
func TestWebAuthnVerifyMalformedInput(t *testing.T) {
	message := sha256.Sum256([]byte("TestWebAuthnVerifyMalformedInput"))
	a := newWebAuthnAssertion(t, message[:], false)
	canonical := a.canonicalInput()

	// withUint32 returns canonical with the big-endian word at off replaced.
	withUint32 := func(off int, v uint32) []byte {
		in := a.canonicalInput()
		binary.BigEndian.PutUint32(in[off:off+4], v)
		return in
	}
	cdjLenOffset := 32 + 4 + len(a.authData) + 1

	corpus := map[string][]byte{
		"empty":                          {},
		"one byte":                       {0x00},
		"just under the floor":           make([]byte, 294),
		"exactly at the floor":           make([]byte, 295),
		"over the ceiling":               make([]byte, 8193),
		"authDataLength max":             withUint32(32, ^uint32(0)),
		"authDataLength overshoot":       withUint32(32, uint32(len(canonical))),
		"authDataLength zero":            withUint32(32, 0),
		"clientDataJSONLength max":       withUint32(cdjLenOffset, ^uint32(0)),
		"clientDataJSONLength overshoot": withUint32(cdjLenOffset, uint32(len(canonical))),
		"challengeLocation max":          withUint32(len(canonical)-136, ^uint32(0)),
		"responseTypeLocation max":       withUint32(len(canonical)-132, ^uint32(0)),
		"truncated mid-payload":          canonical[:len(canonical)/2],
		"truncated by one":               canonical[:len(canonical)-1],
		"one byte over":                  append(a.canonicalInput(), 0),
	}
	for i := 176; i <= 178; i++ {
		corpus[fmt.Sprintf("all zero, %d bytes", i)] = make([]byte, i)
	}

	// Everything above is rejected at or before the 295-byte floor, or at the
	// declared-length bounds. These get past both, so they are what reaches the
	// length check, the word copy and the streaming hash.
	zeroWords := a.canonicalInput()
	copy(zeroWords[len(zeroWords)-128:], make([]byte, 128))
	corpus["canonical shape, zero signature and key"] = zeroWords

	corpus["authDataLength one too large"] = withUint32(32, uint32(len(a.authData)+1))
	corpus["authDataLength one too small"] = withUint32(32, uint32(len(a.authData)-1))
	corpus["clientDataJSONLength one too large"] = withUint32(cdjLenOffset, uint32(len(a.clientDataJSON)+1))
	corpus["clientDataJSONLength one too small"] = withUint32(cdjLenOffset, uint32(len(a.clientDataJSON)-1))
	corpus["challengeLocation just past the end"] = withUint32(len(canonical)-136, uint32(len(a.clientDataJSON)))
	corpus["responseTypeLocation just past the end"] = withUint32(len(canonical)-132, uint32(len(a.clientDataJSON)))
	corpus["padded to the ceiling"] = append(a.canonicalInput(), make([]byte, 8192-len(canonical))...)

	// A canonical input with a longer authenticatorData, so the four words sit
	// deeper into the input. The pre-fork append lands just past authenticatorData
	// either way — on the userVerification byte, the declared clientDataJSON length
	// and the start of the JSON — but here it does so further from the end.
	long := newWebAuthnAssertion(t, message[:], false)
	long.authData = append(long.authData, make([]byte, 64)...)
	corpus["long authenticatorData"] = long.canonicalInput()

	for name, input := range corpus {
		for _, strict := range []bool{false, true} {
			in := append([]byte{}, input...)
			ret, err := (&webAuthnVerify{strictInput: strict}).Run(in)
			if err != nil {
				t.Errorf("strict=%v %q: returned an error: %v", strict, name, err)
				continue
			}
			if len(ret) != 32 {
				t.Errorf("strict=%v %q: returned %d bytes, want 32", strict, name, len(ret))
			}
			if strict && !bytes.Equal(in, input) {
				t.Errorf("strict=%v %q: the input was modified", strict, name)
			}
		}
	}
}

// sortedAddresses copies addrs and sorts it, so two precompile sets can be compared as
// sets — the maps they are built from iterate in no particular order.
func sortedAddresses(addrs []common.Address) []common.Address {
	out := append([]common.Address(nil), addrs...)
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i][:], out[j][:]) < 0 })
	return out
}

// signAssertion produces a cryptographically valid assertion over whatever
// clientDataJSON and authenticator flags it is given. newWebAuthnAssertion always
// produces a well-formed one, which is what most tests want; this one exists so that a
// test can make the signature check pass while the check above it should fail, and so
// tell the two apart. Words with a zero top byte are avoided so the pre-fork parser does
// not reject for an unrelated reason.
func signAssertion(t *testing.T, message []byte, clientDataJSON string, flags byte) webAuthnAssertion {
	t.Helper()
	for attempt := 0; attempt < 100000; attempt++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		authData := make([]byte, 37)
		authData[32] = flags
		binary.BigEndian.PutUint32(authData[33:37], uint32(attempt))

		clientDataJSONHash := sha256.Sum256([]byte(clientDataJSON))
		signed := sha256.Sum256(append(append([]byte{}, authData...), clientDataJSONHash[:]...))
		r, s, err := ecdsa.Sign(rand.Reader, key, signed[:])
		if err != nil {
			t.Fatal(err)
		}
		a := webAuthnAssertion{
			message:        message,
			authData:       authData,
			clientDataJSON: clientDataJSON,
			r:              word32(r), s: word32(s), x: word32(key.X), y: word32(key.Y),
		}
		if !a.hasLeadingZeroWord() {
			return a
		}
	}
	t.Fatal("could not generate an assertion without a zero top byte")
	return webAuthnAssertion{}
}

// accepts runs the assertion's input through both parsers and reports the verdict,
// failing if they disagree — none of the cases below is about the encoding, so a
// difference across the fork would mean this test is measuring the wrong thing.
func (a webAuthnAssertion) accepts(t *testing.T, message []byte) bool {
	t.Helper()
	build := func() []byte {
		in := append([]byte{}, message...)
		in = append(in, a.payload()...)
		in = append(in, a.x...)
		return append(in, a.y...)
	}
	var verdicts []bool
	for _, strict := range []bool{false, true} {
		ret, err := (&webAuthnVerify{strictInput: strict}).Run(build())
		if err != nil {
			t.Fatalf("strict=%v: %v", strict, err)
		}
		if len(ret) != 32 {
			t.Fatalf("strict=%v: returned %d bytes, want 32", strict, len(ret))
		}
		verdicts = append(verdicts, ret[31] == 1)
	}
	if verdicts[0] != verdicts[1] {
		t.Fatalf("the two parsers disagree (pre-fork %v, strict %v) on an input that is canonical", verdicts[0], verdicts[1])
	}
	return verdicts[0]
}

// TestWebAuthnVerifyBindsTheChallenge pins the check that makes an assertion good for
// one operation only. Whether the precompile or its caller should be the one binding the
// challenge is a separate question this fork deliberately leaves alone — which is a
// reason to pin the behaviour, not to leave it unpinned.
//
// The assertion here is cryptographically valid: the signature covers
// authenticatorData ‖ sha256(clientDataJSON), and clientDataJSON is self-consistent. Only
// the message it is presented against is a different one. Without the challenge check
// the signature verifies and the call returns 1, which is any past assertion authorising
// any later operation.
func TestWebAuthnVerifyBindsTheChallenge(t *testing.T) {
	var signedFor, presentedAs [32]byte
	for i := range signedFor {
		signedFor[i] = byte(i)
		presentedAs[i] = byte(i) ^ 0xff
	}

	a := newWebAuthnAssertion(t, signedFor[:], false)
	if !a.accepts(t, signedFor[:]) {
		t.Fatal("the assertion does not verify against its own message, so this test proves nothing")
	}
	if a.accepts(t, presentedAs[:]) {
		t.Error("an assertion signed for one message was accepted for another: the challenge is not bound")
	}
}

// TestWebAuthnVerifyChecksTheResponseType pins the other clientDataJSON check. Same
// shape as the challenge test: the signature covers this clientDataJSON, so if the type
// is not checked the call succeeds and a "webauthn.create" ceremony passes as an
// assertion.
func TestWebAuthnVerifyChecksTheResponseType(t *testing.T) {
	var message [32]byte
	for i := range message {
		message[i] = byte(i)
	}
	challenge := base64.RawURLEncoding.EncodeToString(message[:])

	good := signAssertion(t, message[:], `{"type":"webauthn.get","challenge":"`+challenge+`","origin":"https://example.invalid"}`, flagUP)
	if !good.accepts(t, message[:]) {
		t.Fatal("the control assertion does not verify, so this test proves nothing")
	}

	// The type is wrong by one letter, and responseTypeLocation points at it. Letting
	// payload() search for `"type":"webauthn.get"` instead would return -1, and the
	// bounds check in front of the comparison would reject the input before the
	// comparison ran — which is a different check, and would leave this one unpinned.
	bad := signAssertion(t, message[:], `{"type":"webauthn.put","challenge":"`+challenge+`","origin":"https://example.invalid"}`, flagUP)
	bad.responseTypeLoc = &[]uint32{1}[0]
	if bad.accepts(t, message[:]) {
		t.Error("an assertion whose clientDataJSON is not a webauthn.get was accepted")
	}
}

// TestWebAuthnVerifyChecksAuthenticatorFlags pins checkAuthFlags. The flags are covered
// by the signature, so each case here is a valid assertion that differs only in what the
// authenticator reported; without the checks every one of them is accepted.
//
// The userVerification byte in the payload — what it is allowed to relax — is another
// question this fork leaves alone, so the requireUserVerification case is pinned as it
// behaves today rather than as it might.
func TestWebAuthnVerifyChecksAuthenticatorFlags(t *testing.T) {
	var message [32]byte
	for i := range message {
		message[i] = byte(i)
	}
	clientDataJSON := `{"type":"webauthn.get","challenge":"` +
		base64.RawURLEncoding.EncodeToString(message[:]) + `","origin":"https://example.invalid"}`

	for _, tc := range []struct {
		name         string
		flags        byte
		userVerified byte
		want         bool
	}{
		{"user present", flagUP, 0, true},
		{"user not present", 0, 0, false},
		{"verification required and reported", flagUP | flagUV, 1, true},
		{"verification required and not reported", flagUP, 1, false},
		{"verification reported but not required", flagUP | flagUV, 0, true},
		{"backed up without being eligible", flagUP | flagBS, 0, false},
		{"backup eligible and backed up", flagUP | flagBE | flagBS, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := signAssertion(t, message[:], clientDataJSON, tc.flags)
			a.userVerified = tc.userVerified
			if got := a.accepts(t, message[:]); got != tc.want {
				t.Errorf("accepted=%v, want %v", got, tc.want)
			}
		})
	}
}

// TestWebAuthnVerifyAddressIsPinned states the precompile's address somewhere other than
// where it is defined.
//
// Before this branch all five precompile maps repeated the literal, so the value was
// asserted five times over. They now share webAuthnVerifyAddress, which is better to read
// and worse to check: every test in this file reaches the precompile through that helper,
// so a changed value would be followed by the tests and still pass, while the shipped
// binary gated strict parsing on a different address. common.Address cannot be a const, so
// a test is where the second statement of the value goes.
func TestWebAuthnVerifyAddressIsPinned(t *testing.T) {
	want := common.HexToAddress("0x0000000000000000000000000000000000000111")
	if webAuthnVerifyAddress() != want {
		t.Fatalf("webAuthnVerifyAddress = %s, want %s", webAuthnVerifyAddress(), want)
	}
	// And the fork's set is the Berlin one with the entry at *that* address replaced.
	strict, ok := PrecompiledContractsWebAuthnStrict[want]
	if !ok {
		t.Fatalf("the strict set has no precompile at %s", want)
	}
	v, ok := strict.(*webAuthnVerify)
	if !ok || !v.strictInput {
		t.Errorf("the strict set holds %T at %s, want a webAuthnVerify with strictInput set", strict, want)
	}
	berlin, ok := PrecompiledContractsBerlin[want].(*webAuthnVerify)
	if !ok || berlin.strictInput {
		t.Errorf("the Berlin set holds %T at %s, want a webAuthnVerify without strictInput", PrecompiledContractsBerlin[want], want)
	}
}
