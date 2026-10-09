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

package runtime

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

// webAuthnReachableInput builds input for 0x111 that passes every check the
// precompile makes before it verifies the signature. The signature words are left
// zero: no key is involved and nothing verifies. It is only here to reach the code
// that writes into the input buffer.
func webAuthnReachableInput() []byte {
	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(i)
	}
	authData := make([]byte, 37)
	authData[32] = 0x01 // User Present

	clientDataJSON := `{"type":"webauthn.get","challenge":"` +
		base64.RawURLEncoding.EncodeToString(challenge) + `","origin":"https://example.invalid"}`

	be32 := func(v int) []byte {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(v))
		return b
	}

	var in []byte
	in = append(in, challenge...)
	in = append(in, be32(len(authData))...)
	in = append(in, authData...)
	in = append(in, 0) // requireUserVerification
	in = append(in, be32(len(clientDataJSON))...)
	in = append(in, clientDataJSON...)
	in = append(in, be32(strings.Index(clientDataJSON, `"challenge":"`))...)
	in = append(in, be32(strings.Index(clientDataJSON, `"type":"webauthn.get"`))...)
	in = append(in, make([]byte, 128)...) // r ‖ s ‖ x ‖ y
	return in
}

// TestPrecompileDoesNotWriteToTopLevelInput guards the invariant that a precompile
// called directly by a transaction cannot reach the caller's buffer. At that depth
// the buffer is the transaction's own calldata, which has to stay exactly as it was
// signed.
//
// The WebAuthn precompile is used because its pre-fork parser does write into its
// input — see webAuthnVerify.Run — which makes it the case that has to hold.
func TestPrecompileDoesNotWriteToTopLevelInput(t *testing.T) {
	input := webAuthnReachableInput()
	// Trim the slice to its exact length the way an RLP-decoded transaction's
	// calldata arrives, so that any in-place write has to land inside it.
	calldata := make([]byte, len(input))
	copy(calldata, input)
	before := common.CopyBytes(calldata)

	statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Call(common.HexToAddress("0x0000000000000000000000000000000000000111"), calldata, &Config{State: statedb}); err != nil {
		t.Fatalf("call to 0x111 failed: %v", err)
	}
	if !bytes.Equal(calldata, before) {
		for i := range before {
			if before[i] != calldata[i] {
				t.Fatalf("the precompile wrote into the caller's buffer at offset %d; at depth 0 that buffer is the transaction's calldata", i)
			}
		}
	}
}

// TestWebAuthnInputWriteInsideEVMIsGated pins the other half of the aliasing change:
// what a *contract* sees. A precompile called from a contract is handed a slice of that
// contract's memory, and the pre-fork parser writes the clientDataJSON hash into it.
// That write is part of the state transition for every block mined before the fork, so
// replaying history has to reproduce it — a node that stopped writing at all, rather
// than from the fork onwards, would compute different states for old blocks.
//
// The depth matters. (*EVM).Call copies its input at depth 0 only, where the buffer is
// the transaction's calldata and nothing in the EVM can observe the write; the caller
// here is a contract, so no copy is taken and the write has to be visible. The caller
// uses CALL rather than STATICCALL deliberately: StaticCall, CallCode and DelegateCall
// exist only as opcodes and so are never entered at depth 0, which is why the copy
// lives in Call alone — and it means only a CALL exercises that condition. Widening it
// to every depth passes every other test in the tree.
func TestWebAuthnInputWriteInsideEVMIsGated(t *testing.T) {
	const activation = uint64(1_800_000_000)

	input := webAuthnReachableInput()
	// Where the pre-fork parser appends: just past authenticatorData, which starts at
	// 36 and is 37 bytes long in this fixture.
	const probe = 36 + 37
	untouched := common.BytesToHash(input[probe : probe+32])

	// Copy the calldata into memory, hand the precompile a slice of it, then return
	// the 32 bytes the append would have landed on.
	code := []byte{
		byte(vm.CALLDATASIZE), byte(vm.PUSH1), 0x00, byte(vm.PUSH1), 0x00, byte(vm.CALLDATACOPY),
		byte(vm.PUSH1), 0x00, // retSize
		byte(vm.PUSH1), 0x00, // retOffset
		byte(vm.CALLDATASIZE), // argsSize
		byte(vm.PUSH1), 0x00,  // argsOffset
		byte(vm.PUSH1), 0x00, // value
		byte(vm.PUSH2), 0x01, 0x11, // 0x111
		byte(vm.GAS), byte(vm.CALL), byte(vm.POP),
		byte(vm.PUSH1), probe, byte(vm.MLOAD),
		byte(vm.PUSH1), 0x00, byte(vm.MSTORE),
		byte(vm.PUSH1), 0x20, byte(vm.PUSH1), 0x00, byte(vm.RETURN),
	}

	run := func(t *testing.T, time uint64) common.Hash {
		t.Helper()
		cfg := *params.AllEthashProtocolChanges
		cfg.WebAuthnStrictTime = &[]uint64{activation}[0]
		statedb, err := state.New(common.Hash{}, state.NewDatabase(rawdb.NewMemoryDatabase()), nil)
		if err != nil {
			t.Fatal(err)
		}
		ret, _, err := Execute(code, input, &Config{ChainConfig: &cfg, Time: time, State: statedb})
		if err != nil {
			t.Fatalf("running the caller failed: %v", err)
		}
		if len(ret) != 32 {
			t.Fatalf("caller returned %d bytes, want 32", len(ret))
		}
		return common.BytesToHash(ret)
	}

	if got := run(t, activation-1); got == untouched {
		t.Error("before activation the precompile did not write into the caller's memory; that write is part of every historical block's state transition")
	}
	if got := run(t, activation); got != untouched {
		t.Errorf("after activation the precompile still wrote into the caller's memory: %s, want the original %s", got, untouched)
	}
}
