// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package eth

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/params"
	"github.com/google/uuid"
)

// writeSignerKeystore writes key encrypted under password, and the password file with
// content, returning both paths.
func writeSignerKeystore(t *testing.T, dir string, key *ecdsa.PrivateKey, password, content string) (string, string) {
	t.Helper()
	enc, err := keystore.EncryptKey(&keystore.Key{
		Id:         uuid.New(),
		Address:    crypto.PubkeyToAddress(key.PublicKey),
		PrivateKey: key,
	}, password, keystore.LightScryptN, keystore.LightScryptP)
	if err != nil {
		t.Fatal(err)
	}
	keyFile, passwordFile := filepath.Join(dir, "signer.json"), filepath.Join(dir, "signer.pass")
	if err := os.WriteFile(keyFile, enc, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(passwordFile, []byte(content), 0o400); err != nil {
		t.Fatal(err)
	}
	return keyFile, passwordFile
}

func TestLoadMinerSigner(t *testing.T) {
	key, _ := crypto.GenerateKey()

	for _, tt := range []struct {
		name    string
		content string
	}{
		{"bare", "secret"},
		{"newline", "secret\n"},
		{"dos line ending", "secret\r\n"},
		{"further lines", "secret\nanother\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", tt.content)
			got, err := loadMinerSigner(keyFile, passwordFile, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if !got.Equal(key) {
				t.Fatal("loaded a different key")
			}
		})
	}
	t.Run("wrong password", func(t *testing.T) {
		const supplied = "not-the-key-password"
		keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", supplied)
		if _, err := loadMinerSigner(keyFile, passwordFile, ""); err == nil || err.Error() != keyFile+": "+keystore.ErrDecrypt.Error() {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("the two flags swapped", func(t *testing.T) {
		// A parser error would quote from the password file, whatever it holds.
		for _, password := range []string{"secret", "falsX", "12345678"} {
			keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, password, password)
			_, err := loadMinerSigner(passwordFile, keyFile, "")
			if err == nil || err.Error() != passwordFile+" is not a usable keystore file" {
				t.Fatalf("password %q: got %v", password, err)
			}
		}
	})
	t.Run("no password file", func(t *testing.T) {
		keyFile, _ := writeSignerKeystore(t, t.TempDir(), key, "secret", "secret")
		if _, err := loadMinerSigner(keyFile, "", ""); err == nil || !strings.Contains(err.Error(), "password file") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("missing key file", func(t *testing.T) {
		_, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", "secret")
		if _, err := loadMinerSigner(filepath.Join(t.TempDir(), "absent"), passwordFile, ""); err == nil {
			t.Fatal("got no error")
		}
	})
	t.Run("inside the keystore directory", func(t *testing.T) {
		dir := t.TempDir()
		keyFile, passwordFile := writeSignerKeystore(t, dir, key, "secret", "secret")
		if _, err := loadMinerSigner(keyFile, passwordFile, dir); err == nil || !strings.Contains(err.Error(), "keystore directory") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("inside the keystore directory under a name starting with two dots", func(t *testing.T) {
		dir := t.TempDir()
		keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", "secret")
		named := filepath.Join(dir, "..signer")
		if err := os.Rename(keyFile, named); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMinerSigner(named, passwordFile, dir); err == nil || !strings.Contains(err.Error(), "keystore directory") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("beside the keystore directory", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "keystore")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		keyFile, passwordFile := writeSignerKeystore(t, parent, key, "secret", "secret")
		if _, err := loadMinerSigner(keyFile, passwordFile, dir); err != nil {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("inside the keystore directory through a symlink", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("creating a symlink needs a privilege")
		}
		dir := t.TempDir()
		keyFile, passwordFile := writeSignerKeystore(t, dir, key, "secret", "secret")
		linkFile, linkDir := filepath.Join(t.TempDir(), "signer.json"), filepath.Join(t.TempDir(), "keystore")
		if err := os.Symlink(keyFile, linkFile); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(dir, linkDir); err != nil {
			t.Fatal(err)
		}
		if _, err := loadMinerSigner(linkFile, passwordFile, dir); err == nil || !strings.Contains(err.Error(), "keystore directory") {
			t.Fatalf("key file behind a symlink: got %v", err)
		}
		if _, err := loadMinerSigner(keyFile, passwordFile, linkDir); err == nil || !strings.Contains(err.Error(), "keystore directory") {
			t.Fatalf("keystore directory behind a symlink: got %v", err)
		}
	})
	t.Run("through a path that does not resolve", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("needs /proc/self/fd")
		}
		keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", "secret")
		blob, err := os.ReadFile(keyFile)
		if err != nil {
			t.Fatal(err)
		}
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		go func() {
			w.Write(blob)
			w.Close()
		}()
		// What --miner.signerkey <(...) hands over: a link to a pipe, which can be read and
		// does not resolve.
		path := fmt.Sprintf("/proc/self/fd/%d", r.Fd())
		if _, err := filepath.EvalSymlinks(path); err == nil {
			t.Fatalf("%s resolves, this proves nothing", path)
		}
		got, err := loadMinerSigner(path, passwordFile, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(key) {
			t.Fatal("loaded a different key")
		}
	})
	t.Run("accessible to the group or others", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no such permission bits")
		}
		keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", "secret")
		for _, file := range []string{keyFile, passwordFile} {
			for _, mode := range []os.FileMode{0o404, 0o402, 0o401, 0o440, 0o420, 0o410, 0o640, 0o660} {
				if err := os.Chmod(file, mode); err != nil {
					t.Fatal(err)
				}
				if _, err := loadMinerSigner(keyFile, passwordFile, ""); err == nil || !strings.Contains(err.Error(), "its group or to other users") {
					t.Fatalf("%s mode %o: got %v", file, mode, err)
				}
			}
			// Only the owner may touch it, whichever way.
			for _, mode := range []os.FileMode{0o400, 0o600} {
				if err := os.Chmod(file, mode); err != nil {
					t.Fatal(err)
				}
				if _, err := loadMinerSigner(keyFile, passwordFile, ""); err != nil {
					t.Fatalf("%s mode %o: %v", file, mode, err)
				}
			}
			if err := os.Chmod(file, 0o400); err != nil {
				t.Fatal(err)
			}
		}
	})
	// What LoadCredential= leaves on a filesystem with ACLs when the unit sets User=: mode 0440,
	// checked on systemd 255. The group bits are the mask of the ACL that lets the user read.
	t.Run("a copy in the credentials directory", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no such permission bits")
		}
		dir := t.TempDir()
		keyFile, passwordFile := writeSignerKeystore(t, dir, key, "secret", "secret")
		chmod := func(file string, mode os.FileMode) {
			t.Helper()
			if err := os.Chmod(file, mode); err != nil {
				t.Fatal(err)
			}
		}
		load := func() error {
			_, err := loadMinerSigner(keyFile, passwordFile, "")
			return err
		}
		chmod(keyFile, 0o440)
		chmod(passwordFile, 0o440)

		t.Setenv("CREDENTIALS_DIRECTORY", dir)
		if err := load(); err != nil {
			t.Fatalf("group bits there: %v", err)
		}
		t.Setenv("CREDENTIALS_DIRECTORY", dir+string(filepath.Separator))
		if err := load(); err != nil {
			t.Fatalf("with a trailing separator: %v", err)
		}
		t.Setenv("CREDENTIALS_DIRECTORY", dir)
		chmod(passwordFile, 0o444)
		if err := load(); err == nil {
			t.Fatal("accepted a copy readable by other users")
		}
		chmod(passwordFile, 0o440)

		// The exception goes by where the file is, and the rest of the rule stays.
		for name, env := range map[string]string{
			"another directory": t.TempDir(),
			"a parent":          filepath.Dir(dir),
			"unset":             "",
		} {
			t.Setenv("CREDENTIALS_DIRECTORY", env)
			if err := load(); err == nil {
				t.Fatalf("%s: accepted group bits", name)
			}
		}
		// Each file is judged by its own place.
		elsewhere, _ := writeSignerKeystore(t, t.TempDir(), key, "secret", "secret")
		chmod(elsewhere, 0o440)
		t.Setenv("CREDENTIALS_DIRECTORY", dir)
		if _, err := loadMinerSigner(elsewhere, passwordFile, ""); err == nil {
			t.Fatal("accepted group bits on a key outside the directory")
		}
	})
}

// newMinerSignerNode starts a node built through New, on a chain whose only epoch is out of
// reach. TestLoadMinerSigner and TestCheckMinerSigner cover the parts; this is about New and
// StartMining using them. It is started so that closing it stops the backend.
func newMinerSignerNode(t *testing.T, keystoreDir string, etherbase common.Address, signers map[common.Address]common.Address, keyFile, passwordFile string) (*Ethereum, error) {
	t.Helper()
	p2pKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	stack, err := node.New(&node.Config{KeyStoreDir: keystoreDir, P2P: p2p.Config{PrivateKey: p2pKey, NoDiscovery: true}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stack.Close() })

	chainConfig := *params.AllEthashProtocolChanges
	chainConfig.MinerSignerEpochs = []params.MinerSignerEpoch{{TD: big.NewInt(1 << 60), Signers: signers}}
	config := ethconfig.Config{
		Genesis: &core.Genesis{Config: &chainConfig, Difficulty: params.GenesisDifficulty, GasLimit: 8000000},
		Ethash:  ethash.Config{PowMode: ethash.ModeTest, CachesInMem: 1},
	}
	config.Miner.Etherbase = etherbase
	config.Miner.GasPrice = big.NewInt(1)
	config.Miner.SignerKeyFile, config.Miner.SignerPasswordFile = keyFile, passwordFile

	backend, err := New(stack, &config)
	if err != nil {
		return nil, err
	}
	if err := stack.Start(); err != nil {
		t.Fatal(err)
	}
	return backend, nil
}

// A key that cannot be decrypted, or that sits in the keystore directory, fails the start.
func TestNodeRefusesSignerKey(t *testing.T) {
	key, _ := crypto.GenerateKey()
	signers := map[common.Address]common.Address{{0x47}: crypto.PubkeyToAddress(key.PublicKey)}

	keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", "wrong")
	if _, err := newMinerSignerNode(t, "", common.Address{0x47}, signers, keyFile, passwordFile); err == nil || !strings.Contains(err.Error(), "miner signer") {
		t.Errorf("wrong password: got %v", err)
	}
	dir := t.TempDir()
	keyFile, passwordFile = writeSignerKeystore(t, dir, key, "secret", "secret")
	if _, err := newMinerSignerNode(t, dir, common.Address{0x47}, signers, keyFile, passwordFile); err == nil || !strings.Contains(err.Error(), "keystore directory") {
		t.Errorf("inside the keystore directory: got %v", err)
	}
}

// A chain that lists signers does not start mining without a key.
func TestStartMiningRefusesWithoutSigner(t *testing.T) {
	key, _ := crypto.GenerateKey()
	signers := map[common.Address]common.Address{{0x47}: crypto.PubkeyToAddress(key.PublicKey)}
	backend, err := newMinerSignerNode(t, "", common.Address{0x47}, signers, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.StartMining(1); err == nil || !strings.Contains(err.Error(), "no signer key") {
		t.Fatalf("got %v", err)
	}
	// A node with no key is not set up to mine, and its etherbase signs nothing.
	if !NewMinerAPI(backend).SetEtherbase(common.Address{0xff}) {
		t.Fatal("refused an etherbase on a node that is not set up to mine")
	}
}

// The key in the config reaches the engine. StartMining refuses an etherbase it cannot sign
// for, and once mining miner_setEtherbase does.
func TestStartMiningWithSigner(t *testing.T) {
	key, _ := crypto.GenerateKey()
	signer := crypto.PubkeyToAddress(key.PublicKey)
	coinbase, other, foreign := common.Address{0x47}, common.Address{0x48}, common.Address{0xff}
	keyFile, passwordFile := writeSignerKeystore(t, t.TempDir(), key, "secret", "secret")
	backend, err := newMinerSignerNode(t, "", coinbase, map[common.Address]common.Address{coinbase: signer, other: signer}, keyFile, passwordFile)
	if err != nil {
		t.Fatal(err)
	}
	if have, ok := backend.engine.(*ethash.Ethash).MinerSigner(); !ok || have != signer {
		t.Fatalf("engine signer %x %v, want %x", have, ok, signer)
	}
	api := NewMinerAPI(backend)
	wantEtherbase := func(what string, want common.Address) {
		t.Helper()
		if have, err := backend.Etherbase(); err != nil || have != want {
			t.Fatalf("%s: etherbase %x %v, want %x", what, have, err, want)
		}
	}
	waitMining := func(want bool) {
		t.Helper()
		for deadline := time.Now().Add(20 * time.Second); backend.IsMining() != want; time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("mining never became %v", want)
			}
		}
	}
	// StartMining checks whatever the etherbase is.
	backend.SetEtherbase(foreign)
	if err := backend.StartMining(1); err == nil || !strings.Contains(err.Error(), "cannot sign") {
		t.Fatalf("started on an etherbase the key cannot sign for: %v", err)
	}
	backend.SetEtherbase(coinbase)
	// A loaded key decides whether or not the miner is running.
	if api.SetEtherbase(foreign) {
		t.Fatal("took an address the key cannot sign for on a node that is not mining")
	}
	wantEtherbase("after the refusal while not mining", coinbase)
	if err := backend.StartMining(1); err != nil {
		t.Fatal(err)
	}
	defer backend.StopMining()
	waitMining(true)
	if api.SetEtherbase(foreign) {
		t.Fatal("moved a running miner to an address its key cannot sign for")
	}
	wantEtherbase("after the refusal", coinbase)
	if !api.SetEtherbase(other) {
		t.Fatal("refused an address the key can sign for")
	}
	wantEtherbase("after the move", other)
	// Nothing checks again when a sync ends and the worker resumes, so a pause changes nothing.
	mux := backend.EventMux()
	if err := mux.Post(downloader.StartEvent{}); err != nil {
		t.Fatal(err)
	}
	waitMining(false)
	if api.SetEtherbase(foreign) {
		t.Fatal("took an address the key cannot sign for while paused for a sync")
	}
	wantEtherbase("after the refusal during a pause", other)
	if err := mux.Post(downloader.DoneEvent{}); err != nil {
		t.Fatal(err)
	}
	waitMining(true)
	wantEtherbase("after the pause", other)
}

func TestCheckMinerSigner(t *testing.T) {
	var (
		key, _    = crypto.GenerateKey()
		other, _  = crypto.GenerateKey()
		signer    = crypto.PubkeyToAddress(key.PublicKey)
		otherAddr = crypto.PubkeyToAddress(other.PublicKey)
		coinbase  = common.Address{0x47}
		rotated   = common.Address{0x48}
	)
	run := func(engine *ethash.Ethash, epochs []params.MinerSignerEpoch, config ethconfig.Config, key *ecdsa.PrivateKey, etherbase common.Address) error {
		chainConfig := *params.TestChainConfig
		chainConfig.MinerSignerEpochs = epochs
		engine.AuthorizeMiner(key)
		spec := &core.Genesis{Config: &chainConfig, BaseFee: big.NewInt(params.InitialBaseFee)}
		chain, err := core.NewBlockChain(rawdb.NewMemoryDatabase(), nil, spec, nil, engine, vm.Config{}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer chain.Stop()
		return (&Ethereum{config: &config, engine: engine, blockchain: chain}).checkMinerSigner(etherbase)
	}
	epochs := []params.MinerSignerEpoch{
		{TD: big.NewInt(1 << 40), Signers: map[common.Address]common.Address{coinbase: signer}},
		{TD: big.NewInt(1 << 41), Signers: map[common.Address]common.Address{rotated: signer}},
	}
	// The head of the test chain is past both of these.
	rotation := []params.MinerSignerEpoch{
		{TD: big.NewInt(1), Signers: map[common.Address]common.Address{coinbase: signer}},
		{TD: big.NewInt(2), Signers: map[common.Address]common.Address{coinbase: otherAddr}},
	}
	shutOut := []params.MinerSignerEpoch{
		{TD: big.NewInt(1), Signers: map[common.Address]common.Address{coinbase: signer}},
		{TD: big.NewInt(2), Signers: map[common.Address]common.Address{otherAddr: {0xde}}},
	}
	// The head of the test chain, the genesis block, sits exactly on the second of these.
	retiredAtHead := []params.MinerSignerEpoch{
		{TD: big.NewInt(1), Signers: map[common.Address]common.Address{coinbase: signer}},
		{TD: new(big.Int).Set(params.GenesisDifficulty), Signers: map[common.Address]common.Address{coinbase: otherAddr}},
	}
	// The head of the test chain is in the first of these, with the second still to come.
	retiringAhead := []params.MinerSignerEpoch{
		{TD: big.NewInt(1), Signers: map[common.Address]common.Address{coinbase: signer}},
		{TD: big.NewInt(1 << 40), Signers: map[common.Address]common.Address{coinbase: otherAddr}},
	}
	listedElsewhere := []params.MinerSignerEpoch{
		{TD: big.NewInt(1 << 40), Signers: map[common.Address]common.Address{otherAddr: signer}},
	}
	for _, tt := range []struct {
		name      string
		epochs    []params.MinerSignerEpoch
		noSigner  bool
		fake      bool
		key       *ecdsa.PrivateKey
		etherbase common.Address
		want      string
	}{
		{name: "listed", epochs: epochs, key: key, etherbase: coinbase},
		{name: "listed in a later epoch", epochs: epochs, key: key, etherbase: rotated},
		{name: "no key", epochs: epochs, etherbase: coinbase, want: "no signer key is loaded"},
		{name: "no key allowed", epochs: epochs, noSigner: true, etherbase: coinbase},
		{name: "another key", epochs: epochs, key: other, etherbase: coinbase, want: "cannot sign"},
		{name: "another key with nosigner", epochs: epochs, noSigner: true, key: other, etherbase: coinbase, want: "cannot sign"},
		{name: "not listed, own key", epochs: epochs, key: other, etherbase: otherAddr},
		{name: "not listed, another key", epochs: epochs, key: key, etherbase: common.Address{0xff}, want: "cannot sign"},
		{name: "listed with another signer, own key", epochs: listedElsewhere, key: other, etherbase: otherAddr, want: "cannot sign"},
		{name: "key retired by an epoch the head has reached", epochs: rotation, key: key, etherbase: coinbase, want: "cannot sign"},
		{name: "key that replaced it", epochs: rotation, key: other, etherbase: coinbase},
		{name: "shut out by an epoch the head has reached", epochs: shutOut, key: other, etherbase: otherAddr, want: "cannot sign"},
		{name: "key retired by an epoch the head sits on", epochs: retiredAtHead, key: key, etherbase: coinbase, want: "cannot sign"},
		{name: "key of the epoch the head is in", epochs: retiringAhead, key: key, etherbase: coinbase},
		{name: "chain lists nobody", etherbase: coinbase},
		{name: "chain lists nobody, key loaded", key: key, etherbase: coinbase},
		{name: "fake mode skips the rule", epochs: epochs, fake: true, etherbase: coinbase},
		{name: "fake mode skips the rule, another key", epochs: epochs, fake: true, key: other, etherbase: coinbase},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var config ethconfig.Config
			config.Miner.NoSigner = tt.noSigner
			engine := ethash.NewTester(nil, false)
			if tt.fake {
				engine = ethash.NewFaker()
			}
			defer engine.Close()
			err := run(engine, tt.epochs, config, tt.key, tt.etherbase)
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("got %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("got %v, want an error with %q", err, tt.want)
			}
		})
	}
}
