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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/cmd/utils"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/urfave/cli/v2"
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
	geth = runGeth(t, "--override.minersignertd", "2500000000000000", "dumpconfig")
	out = diagnostics(t, geth)
	if have, want := geth.ExitStatus(), 0; have != want {
		t.Fatalf("dumpconfig exit status %d, want %d\n%s", have, want, out)
	}
	if !strings.Contains(out, "OverrideMinerSignerTD = 2500000000000000") {
		t.Fatalf("dumpconfig dropped --override.minersignertd:\n%s", out)
	}
	// And back: the TOML reads back with the override. TestNodeRunsMinerSignerOverride shows a
	// node running it.
	path := filepath.Join(t.TempDir(), "geth.toml")
	if err := os.WriteFile(path, []byte("[Eth]\nOverrideMinerSignerTD = 2500000000000000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	geth = runGeth(t, "--config", path, "dumpconfig")
	out = diagnostics(t, geth)
	if have, want := geth.ExitStatus(), 0; have != want {
		t.Fatalf("dumpconfig from the TOML: exit status %d, want %d\n%s", have, want, out)
	}
	if !strings.Contains(out, "OverrideMinerSignerTD = 2500000000000000") {
		t.Fatalf("the override did not survive the TOML:\n%s", out)
	}
}

// TestDumpConfigCarriesMinerSignerFlags pins that the three flags are registered on the node
// and reach the miner config.
func TestDumpConfigCarriesMinerSignerFlags(t *testing.T) {
	geth := runGeth(t, "--miner.signerkey", "/tmp/signer.json", "--miner.signerpassword", "/tmp/signer.pass", "--miner.nosigner", "dumpconfig")
	out := diagnostics(t, geth)
	if have, want := geth.ExitStatus(), 0; have != want {
		t.Fatalf("dumpconfig exit status %d, want %d\n%s", have, want, out)
	}
	for _, want := range []string{`SignerKeyFile = "/tmp/signer.json"`, `SignerPasswordFile = "/tmp/signer.pass"`, `NoSigner = true`} {
		if !strings.Contains(out, want) {
			t.Errorf("dumpconfig has no %s:\n%s", want, out)
		}
	}
}

// The chain commands declare the override themselves, so the parser takes it after the command
// name too. TestChainCommandsApplyMinerSignerOverride shows them applying it.
func TestChainCommandsTakeMinerSignerOverride(t *testing.T) {
	for _, command := range []*cli.Command{importCommand, exportCommand} {
		if !slices.Contains(command.Flags, cli.Flag(utils.OverrideMinerSignerTD)) {
			t.Errorf("geth %s does not take --%s", command.Name, utils.OverrideMinerSignerTD.Name)
		}
	}
}

const minerSignerGenesis = `{
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
		"chainId"            : 424243,
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
		"ethash"             : {},
		"minerSignerEpochs"  : [{"td": 1000000, "signers": {"0x0100000000000000000000000000000000000000": "0x0200000000000000000000000000000000000000"}}]
	}
}`

// initMinerSignerChain writes a private chain whose first epoch starts at 1000000 and returns
// its datadir.
func initMinerSignerChain(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(path, []byte(minerSignerGenesis), 0600); err != nil {
		t.Fatal(err)
	}
	geth := runGeth(t, "init", path)
	out := diagnostics(t, geth)
	if have := geth.ExitStatus(); have != 0 {
		t.Fatalf("init exit status %d\n%s", have, out)
	}
	return geth.Datadir
}

// TestNodeRunsMinerSignerOverride reads the threshold a real node runs the way the rollout's
// roll-call does, so the whole path is in the way: the flag table, makeFullNode, eth.New and
// NewBlockChain.
func TestNodeRunsMinerSignerOverride(t *testing.T) {
	datadir := initMinerSignerChain(t)
	threshold := func(args ...string) string {
		t.Helper()
		all := []string{"--datadir", datadir, "--authrpc.port", "0", "--syncmode=full", "--port", "0",
			"--nat", "none", "--nodiscover", "--maxpeers", "0", "--cache", "64", "--ipcdisable",
			"--datadir.minfreedisk", "0"}
		all = append(all, args...)
		all = append(all, "--exec", "admin.nodeInfo.protocols.eth.config.minerSignerEpochs[0].td", "console")
		geth := runGeth(t, all...)
		stdout := string(geth.Output())
		geth.WaitExit()
		if have := geth.ExitStatus(); have != 0 {
			t.Fatalf("node exit status %d\n%s\n%s", have, stdout, geth.StderrText())
		}
		return strings.TrimSpace(stdout)
	}
	if have, want := threshold(), "1000000"; have != want {
		t.Fatalf("no override: the node runs %q, want %q", have, want)
	}
	if have, want := threshold("--override.minersignertd", "2000000"), "2000000"; have != want {
		t.Fatalf("flag: the node runs %q, want %q", have, want)
	}
	path := filepath.Join(t.TempDir(), "geth.toml")
	if err := os.WriteFile(path, []byte("[Eth]\nOverrideMinerSignerTD = 3000000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if have, want := threshold("--config", path), "3000000"; have != want {
		t.Fatalf("TOML: the node runs %q, want %q", have, want)
	}
}

// A chain past the threshold on an unsigned block rewinds at startup, which a read-only command
// cannot do. It has to refuse with advice rather than die inside rawdb, and the advice has to
// work. Fake PoW switches the rule off, so that is how the chain gets there.
func TestExportRefusesChainThatNeedsRewind(t *testing.T) {
	datadir := initMinerSignerChain(t)

	var gspec core.Genesis
	if err := json.Unmarshal([]byte(minerSignerGenesis), &gspec); err != nil {
		t.Fatal(err)
	}
	const crossing = 7 // The first block with a total difficulty of 1000000 or more
	_, blocks, _ := core.GenerateChainWithGenesis(&gspec, ethash.NewFaker(), crossing+1, nil)
	chainFile := filepath.Join(t.TempDir(), "chain.rlp")
	file, err := os.Create(chainFile)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, block := range blocks {
		if err := rlp.Encode(file, block); err != nil {
			t.Fatal(err)
		}
	}
	geth := runGeth(t, "--datadir", datadir, "--fakepow", "import", chainFile)
	if text := diagnostics(t, geth); geth.ExitStatus() != 0 {
		t.Fatalf("import exit status %d\n%s", geth.ExitStatus(), text)
	}
	out := filepath.Join(t.TempDir(), "export.rlp")
	geth = runGeth(t, "--datadir", datadir, "export", out)
	text := diagnostics(t, geth)
	if geth.ExitStatus() == 0 {
		t.Fatalf("export of a chain that needs a rewind passed\n%s", text)
	}
	if !strings.Contains(text, fmt.Sprintf("passed a miner signer threshold on block %d", crossing)) || !strings.Contains(text, "read-only command cannot rewind") {
		t.Fatalf("export failed without saying why\n%s", text)
	}
	// The reason the rule rejects the block is there before the operator commits to the rewind.
	if !strings.Contains(text, "invalid miner signature: coinbase 0x0000000000000000000000000000000000000000 is not listed and cannot cross the threshold") {
		t.Fatalf("export did not name the reason the block is rejected\n%s", text)
	}
	// What it advises: a normal start takes the chain below the threshold.
	node := runGeth(t, "--datadir", datadir, "--authrpc.port", "0", "--syncmode=full", "--port", "0",
		"--nat", "none", "--nodiscover", "--maxpeers", "0", "--cache", "64", "--ipcdisable",
		"--datadir.minfreedisk", "0", "--exec", "eth.blockNumber", "console")
	stdout := string(node.Output())
	node.WaitExit()
	if have := node.ExitStatus(); have != 0 {
		t.Fatalf("node exit status %d\n%s\n%s", have, stdout, node.StderrText())
	}
	// Whatever else the start printed, the last line is the answer to the question it was asked.
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	number, err := strconv.Atoi(strings.TrimSpace(lines[len(lines)-1]))
	if err != nil || number >= crossing {
		t.Fatalf("head %q after a start, want below %d", stdout, crossing)
	}
	geth = runGeth(t, "--datadir", datadir, "export", out)
	if text := diagnostics(t, geth); geth.ExitStatus() != 0 {
		t.Fatalf("export after the rewind: exit status %d\n%s", geth.ExitStatus(), text)
	}
}

// TestChainCommandsApplyMinerSignerOverride: export is read-only and refuses a value other than
// the stored one, which shows what import stored.
func TestChainCommandsApplyMinerSignerOverride(t *testing.T) {
	datadir := initMinerSignerChain(t)
	out, empty := filepath.Join(t.TempDir(), "chain.rlp"), filepath.Join(t.TempDir(), "empty.rlp")
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	export := func(value string, want bool) {
		t.Helper()
		geth := runGeth(t, "--datadir", datadir, "export", "--override.minersignertd", value, out)
		text := diagnostics(t, geth)
		if have := geth.ExitStatus() == 0; have != want {
			t.Fatalf("export with %s: status %d, want it to pass: %v\n%s", value, geth.ExitStatus(), want, text)
		}
		if !want && !strings.Contains(text, "read-only command cannot update it") {
			t.Fatalf("export with %s failed for another reason:\n%s", value, text)
		}
	}
	export("2000000", false)
	export("1000000", true)

	geth := runGeth(t, "--datadir", datadir, "import", "--override.minersignertd", "2000000", empty)
	text := diagnostics(t, geth)
	if have := geth.ExitStatus(); have != 0 {
		t.Fatalf("import exit status %d\n%s", have, text)
	}
	export("2000000", true)
	export("1000000", false)
}
