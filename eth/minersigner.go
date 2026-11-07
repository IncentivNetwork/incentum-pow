// Copyright 2026 IncentivNetwork
// SPDX-License-Identifier: LGPL-3.0-or-later
//
// This file is part of incentum-pow, a fork of go-ethereum.

package eth

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/keystore"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/log"
)

// loadMinerSigner decrypts the keystore file mined blocks are signed with. The key goes
// to the engine and nowhere else, so the file must not sit in the keystore directory: the
// account manager would pick it up there as an ordinary account.
func loadMinerSigner(keyFile, passwordFile, keystoreDir string) (*ecdsa.PrivateKey, error) {
	if passwordFile == "" {
		return nil, errors.New("the signer key needs a password file")
	}
	if keystoreDir != "" {
		abs, err := resolvePath(keyFile)
		if err != nil {
			return nil, err
		}
		dir, err := resolvePath(keystoreDir)
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(dir, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("the signer key %s is inside the keystore directory", keyFile)
		}
	}
	for _, file := range []string{keyFile, passwordFile} {
		info, err := os.Stat(file)
		if err != nil {
			return nil, err
		}
		if runtime.GOOS != "windows" && info.Mode().Perm()&minerSignerFileMask(file) != 0 {
			return nil, fmt.Errorf("%s is accessible to its group or to other users", file)
		}
	}
	keyJSON, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	text, err := os.ReadFile(passwordFile)
	if err != nil {
		return nil, err
	}
	password := strings.TrimRight(strings.SplitN(string(text), "\n", 2)[0], "\r")
	key, err := keystore.DecryptKey(keyJSON, password)
	if errors.Is(err, keystore.ErrDecrypt) {
		return nil, fmt.Errorf("%s: %w", keyFile, err)
	}
	if err != nil {
		// Any other error describes the file's content, and with the two flags swapped
		// this file is the password.
		return nil, fmt.Errorf("%s is not a usable keystore file", keyFile)
	}
	return key.PrivateKey, nil
}

// minerSignerFileMask returns the permission bits a signer key or password file must not have.
// The password is plaintext, and the key of a coinbase also controls its stake, so the group is
// kept out as well as everyone else, as ssh does for a private key.
//
// A copy systemd made in $CREDENTIALS_DIRECTORY is the exception. For a unit with User= it grants
// that user read access through an ACL where the filesystem has them, and the mask of an ACL shows
// in the group bits, so the copy reads 0440 with nobody else able to read it.
func minerSignerFileMask(file string) os.FileMode {
	if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
		if abs, err := filepath.Abs(file); err == nil && filepath.Dir(abs) == filepath.Clean(dir) {
			return 0o007
		}
	}
	return 0o077
}

// resolvePath returns path as an absolute one with its symlinks resolved, so that a link to a
// file in the keystore directory, or a keystore directory behind a link, is still recognised.
// The account manager reads the real file there and skips symlinks in the directory, so a link
// there that points out exposes nothing. A path that does not resolve, such as a pipe, is
// taken as it is.
func resolvePath(path string) (string, error) {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return filepath.Abs(path)
}

// minerSignerLoaded reports whether the engine holds a key to sign mined blocks with.
func (s *Ethereum) minerSignerLoaded() bool {
	pow, ok := s.engine.(*ethash.Ethash)
	if !ok {
		return false
	}
	_, loaded := pow.MinerSigner()
	return loaded
}

// checkMinerSigner refuses a miner that could not sign its blocks. It goes by whether the
// chain lists signers at all rather than by whether the threshold has been reached: below
// the threshold a miner without a key looks healthy, and would go silent on reaching it. A
// key that fits an epoch still to come is accepted, because a producer changing keys restarts
// onto it just below that epoch's threshold. Passing therefore does not mean the key can sign
// the next block: until the block that crosses, such a key assembles nothing. One that fits
// only epochs the head has left is refused.
func (s *Ethereum) checkMinerSigner(etherbase common.Address) error {
	pow, ok := s.engine.(*ethash.Ethash)
	if !ok || !pow.MinerSignersActive(s.blockchain.Config()) {
		return nil
	}
	epochs := s.blockchain.Config().MinerSignerEpochs
	signer, loaded := pow.MinerSigner()
	if !loaded {
		if s.config.Miner.NoSigner {
			log.Warn("Mining without a signer on a chain that lists them, blocks stop at the signature threshold")
			return nil
		}
		return errors.New("this chain lists miner signers and no signer key is loaded: set --miner.signerkey and --miner.signerpassword")
	}
	head := s.blockchain.CurrentBlock()
	td := s.blockchain.GetTd(head.Hash(), head.Number.Uint64())
	for i, epoch := range epochs {
		// A block on this head is past an epoch once the head has reached the next one.
		if i+1 < len(epochs) && td != nil && epochs[i+1].TD.Cmp(td) <= 0 {
			continue
		}
		want, listed := epoch.Signers[etherbase]
		if !listed {
			want = etherbase
		}
		if want == signer {
			return nil
		}
	}
	return fmt.Errorf("miner signer %s cannot sign for etherbase %s", signer, etherbase)
}
