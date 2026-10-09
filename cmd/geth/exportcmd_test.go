// Copyright 2022 The go-ethereum Authors
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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestExport does a basic test of "geth export", exporting the test-genesis.
func TestExport(t *testing.T) {
	outfile := fmt.Sprintf("%v/testExport.out", os.TempDir())
	defer os.Remove(outfile)
	geth := runGeth(t, "--datadir", initGeth(t), "export", outfile)
	geth.WaitExit()
	if have, want := geth.ExitStatus(), 0; have != want {
		t.Errorf("exit error, have %d want %d", have, want)
	}
	have, err := os.ReadFile(outfile)
	if err != nil {
		t.Fatal(err)
	}
	want := common.FromHex("0xf9026bf90266a00000000000000000000000000000000000000000000000000000000000000000a01dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347940000000000000000000000000000000000000000a08758259b018f7bce3d2be2ddb62f325eaeea0a0c188cf96623eab468a4413e03a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421a056e81f171bcc55a6ff8345e692c0f86e5b48e01b996cadc001622fb5e363b421b90100000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000180837a12008080b875000000000000000000000000000000000000000000000000000000000000000002f0d131f1f97aef08aec6e3291b957d9efe71050000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000a00000000000000000000000000000000000000000000000000000000000000000880000000000000000c0c0")
	if !bytes.Equal(have, want) {
		t.Fatalf("wrong content exported")
	}
}

// overrideGenesis is a chain of its own — not one of the bundled networks — carrying a
// WebAuthnStrict schedule, so the stored chain config has a timestamp an override can
// be compared against.
const overrideGenesis = `{
	"alloc"      : {},
	"coinbase"   : "0x0000000000000000000000000000000000000000",
	"difficulty" : "0x20000",
	"extraData"  : "",
	"gasLimit"   : "0x2fefd8",
	"nonce"      : "0x0000000000001347",
	"mixhash"    : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"parentHash" : "0x0000000000000000000000000000000000000000000000000000000000000000",
	"timestamp"  : "0x00",
	"config"     : {
		"chainId"            : 424242,
		"homesteadBlock"     : 0,
		"eip150Block"        : 0,
		"eip155Block"        : 0,
		"eip158Block"        : 0,
		"byzantiumBlock"     : 0,
		"constantinopleBlock": 0,
		"petersburgBlock"    : 0,
		"istanbulBlock"      : 0,
		"berlinBlock"        : 0,
		"londonBlock"        : 0,
		"shanghaiTime"       : 0,
		"webauthnStrictTime" : 1800000000
	}
}`

// diagnostics waits for a geth command to exit and returns everything it printed.
//
// Both streams, because utils.Fatalf chooses between them by platform: on unix the message
// goes to stdout and stderr both, on Windows to stdout alone, since the SameFile check it
// uses to detect a redirect does not work there (cmd/utils/cmd.go). Every assertion below
// is on a Fatalf message, so reading stderr alone passed here and failed on Windows with
// nothing to show for it.
//
// The order is not interchangeable: cmd.Wait closes the stdout pipe, so stdout has to be
// drained before it rather than after, and draining first also keeps a command that prints
// more than a pipe buffer from blocking on a reader that never comes.
//
// Call it once per command. It is the one WaitExit, and a second one would replace the exit
// error with "Wait was already called", which ExitStatus asserts its way through.
func diagnostics(t *testing.T, geth *testgeth) string {
	t.Helper()
	stdout := string(geth.Output())
	geth.WaitExit()
	return stdout + geth.StderrText()
}

// TestExportOverrideIsResolved runs the whole override path through a real command
// invocation, which is the only thing that covers the wiring rather than its parts:
// the flag has to be registered on `export`, folded into the configuration by
// chainOverrides, and resolved by the read-only guard in MakeChain. Deleting any one of
// those three makes a mismatched override look like agreement and the refusal below
// disappears.
//
// The datadir is only a genesis block, as in TestExport above. A read-only command
// cannot write a chain config, so the point is which configuration it decides it would
// need — not whether it can install it.
func TestExportOverrideIsResolved(t *testing.T) {
	genesisPath := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(genesisPath, []byte(overrideGenesis), 0600); err != nil {
		t.Fatal(err)
	}
	init := runGeth(t, "init", genesisPath)
	datadir := init.Datadir
	initText := diagnostics(t, init)
	if have, want := init.ExitStatus(), 0; have != want {
		t.Fatalf("init exit status %d, want %d\n%s", have, want, initText)
	}

	outfile := filepath.Join(t.TempDir(), "chain.rlp")

	// The override the datadir was written with: no difference, so nothing to refuse.
	same := runGeth(t, "--datadir", datadir, "export", "--override.webauthnstrict", "1800000000", outfile)
	sameText := diagnostics(t, same)
	if have, want := same.ExitStatus(), 0; have != want {
		t.Errorf("export with the stored timestamp: exit status %d, want %d\n%s", have, want, sameText)
	}

	// A different one is a schedule this command cannot install, and it has to say so
	// rather than export a chain under a configuration nobody runs.
	moved := runGeth(t, "--datadir", datadir, "export", "--override.webauthnstrict", "1900000000", outfile)
	movedText := diagnostics(t, moved)
	if have := moved.ExitStatus(); have == 0 {
		t.Error("export accepted an override that disagrees with the stored chain config")
	}
	if !strings.Contains(movedText, "read-only command cannot update it") {
		t.Errorf("export failed for the wrong reason:\n%s", movedText)
	}

	// A network flag naming a different chain is a genesis mismatch, not a schedule
	// this command declines to install. The guard has to stay out of the way and let
	// the setup path say so, or it sends the operator to start the node with the flag
	// that is the actual mistake.
	foreign := runGeth(t, "--datadir", datadir, "--incentiv-testnet", "export", outfile)
	foreignText := diagnostics(t, foreign)
	if have := foreign.ExitStatus(); have == 0 {
		t.Error("export accepted a network flag for a different chain")
	}
	if strings.Contains(foreignText, "read-only command cannot update it") {
		t.Errorf("a foreign genesis was reported as a chain configuration difference:\n%s", foreignText)
	} else if !strings.Contains(foreignText, "genesis") {
		t.Errorf("export failed without mentioning the genesis mismatch:\n%s", foreignText)
	}
}

// TestExportRefusesTheWrongIncentivNetwork covers the pair the genesis check cannot
// separate. Incentiv mainnet and devnet share a genesis hash, so a mainnet datadir
// opened with `--incentiv-devnet` gets past that check, and before this it fell into
// the message that offers all three network flags as the fix. Following that advice is
// not harmless: `checkCompatible` reports a chain-id change as an incompatibility at
// block 0, the setup path discards those, and the stored chain id is replaced without an
// error or a rewind. So the refusal has to name the two chains instead.
//
// `import` is used to create the datadir because it opens the database read-write, which
// is the only way to initialise one for a bundled network without running the node.
func TestExportRefusesTheWrongIncentivNetwork(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.rlp")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	init := runGeth(t, "--datadir", t.TempDir(), "--incentiv-devnet", "import", empty)
	datadir := init.Datadir
	initText := diagnostics(t, init)
	if have, want := init.ExitStatus(), 0; have != want {
		t.Fatalf("devnet import exit status %d, want %d\n%s", have, want, initText)
	}

	out := filepath.Join(t.TempDir(), "chain.rlp")
	wrong := runGeth(t, "--datadir", datadir, "--incentiv-mainnet", "export", out)
	text := diagnostics(t, wrong)
	if have := wrong.ExitStatus(); have == 0 {
		t.Error("export accepted the mainnet flag on a devnet datadir")
	}
	for _, id := range []string{"12730", "24101"} {
		if !strings.Contains(text, id) {
			t.Errorf("the refusal does not name chain %s:\n%s", id, text)
		}
	}
	if strings.Contains(text, "--incentiv-mainnet, --incentiv-testnet or --incentiv-devnet") {
		t.Errorf("the wrong network was reported as a schedule difference, offering the network flags as the fix:\n%s", text)
	}
}

// TestExportOverrideFromConfigFile covers the other half of what chainOverrides claims:
// the node's configuration, TOML included. The command line is covered above; an
// override that reaches a node only through its config file has to reach the chain
// commands the same way, or they resolve a schedule the node does not run.
func TestExportOverrideFromConfigFile(t *testing.T) {
	genesisPath := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(genesisPath, []byte(overrideGenesis), 0600); err != nil {
		t.Fatal(err)
	}
	init := runGeth(t, "init", genesisPath)
	datadir := init.Datadir
	initText := diagnostics(t, init)
	if have, want := init.ExitStatus(), 0; have != want {
		t.Fatalf("init exit status %d, want %d\n%s", have, want, initText)
	}

	writeConfig := func(t *testing.T, timestamp uint64) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "geth.toml")
		body := fmt.Sprintf("[Eth]\nOverrideWebAuthnStrict = %d\n", timestamp)
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	out := filepath.Join(t.TempDir(), "chain.rlp")

	// The timestamp the datadir was written with: nothing to refuse.
	same := runGeth(t, "--datadir", datadir, "--config", writeConfig(t, 1800000000), "export", out)
	sameText := diagnostics(t, same)
	if have, want := same.ExitStatus(), 0; have != want {
		t.Errorf("export with the stored timestamp in TOML: exit status %d, want %d\n%s", have, want, sameText)
	}

	// A different one has to be seen, exactly as the flag is.
	moved := runGeth(t, "--datadir", datadir, "--config", writeConfig(t, 1900000000), "export", out)
	movedText := diagnostics(t, moved)
	if have := moved.ExitStatus(); have == 0 {
		t.Error("an override set only in the config file was ignored by export")
	}
	if !strings.Contains(movedText, "read-only command cannot update it") {
		t.Errorf("export failed for the wrong reason:\n%s", movedText)
	}
}
