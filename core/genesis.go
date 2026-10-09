// Copyright 2014 The go-ethereum Authors
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

package core

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

//go:generate go run github.com/fjl/gencodec -type Genesis -field-override genesisSpecMarshaling -out gen_genesis.go
//go:generate go run github.com/fjl/gencodec -type GenesisAccount -field-override genesisAccountMarshaling -out gen_genesis_account.go

var errGenesisNoConfig = errors.New("genesis has no chain configuration")

// IncentivTestnet genesis configuration constants
const (
	IncentivTestnetAddr1    = "0x3d8eBBDa14e61a0f6B278112EcB99cd895Bcbf3e"
	IncentivTestnetAddr2    = "0x683d8cb71DC0caa58AD75986292F22d830B87B75"
	IncentivTestnetBalance1 = "500000000000000000000000000000" // 500,000,000,000 tokens
	IncentivTestnetBalance2 = "500000000000000000000000000000" // 500,000,000,000 tokens
	// IncentivTestnetGasLimit is set to 30M gas (6.4x higher than standard 4.7M)
	// to support higher transaction throughput and complex smart contract operations
	// suitable for testnet environment with increased block capacity
	IncentivTestnetGasLimit = 0x1c9c380 // 30,000,000 gas
)

// IncentivMainnet genesis configuration constants
const (
	IncentivMainnetAddr1    = "0xd2CC08D9AFaBb57BdF2216ED15fceaa9993F3B7b"
	IncentivMainnetBalance1 = "100000000000000000000000000000" // 100,000,000,000 tokens (100 billion tokens, 100% of total supply)
	IncentivMainnetGasLimit = 0x1c9c380                        // 30,000,000 gas
	IncentivMainnetBaseFee  = 1800000000000                    // 1800 gwei in wei
)

// IncentivDevnet genesis configuration constants
const (
	IncentivDevnetAddr1    = "0xd2CC08D9AFaBb57BdF2216ED15fceaa9993F3B7b"
	IncentivDevnetBalance1 = "100000000000000000000000000000" // 100,000,000,000 tokens (100 billion tokens, 100% of total supply)
	IncentivDevnetGasLimit = 0x1c9c380                        // 30,000,000 gas
	IncentivDevnetBaseFee  = 1800000000000                    // 1800 gwei in wei
)

// Genesis specifies the header fields, state of a genesis block. It also defines hard
// fork switch-over blocks through the chain configuration.
type Genesis struct {
	Config     *params.ChainConfig `json:"config"`
	Nonce      uint64              `json:"nonce"`
	Timestamp  uint64              `json:"timestamp"`
	ExtraData  []byte              `json:"extraData"`
	GasLimit   uint64              `json:"gasLimit"   gencodec:"required"`
	Difficulty *big.Int            `json:"difficulty" gencodec:"required"`
	Mixhash    common.Hash         `json:"mixHash"`
	Coinbase   common.Address      `json:"coinbase"`
	Alloc      GenesisAlloc        `json:"alloc"      gencodec:"required"`

	// These fields are used for consensus tests. Please don't use them
	// in actual genesis blocks.
	Number     uint64      `json:"number"`
	GasUsed    uint64      `json:"gasUsed"`
	ParentHash common.Hash `json:"parentHash"`
	BaseFee    *big.Int    `json:"baseFeePerGas"`
}

func ReadGenesis(db ethdb.Database) (*Genesis, error) {
	var genesis Genesis
	stored := rawdb.ReadCanonicalHash(db, 0)
	if (stored == common.Hash{}) {
		return nil, fmt.Errorf("invalid genesis hash in database: %x", stored)
	}
	blob := rawdb.ReadGenesisStateSpec(db, stored)
	if blob == nil {
		return nil, fmt.Errorf("genesis state missing from db")
	}
	if len(blob) != 0 {
		if err := genesis.Alloc.UnmarshalJSON(blob); err != nil {
			return nil, fmt.Errorf("could not unmarshal genesis state json: %s", err)
		}
	}
	genesis.Config = rawdb.ReadChainConfig(db, stored)
	if genesis.Config == nil {
		return nil, fmt.Errorf("genesis config missing from db")
	}
	genesisBlock := rawdb.ReadBlock(db, stored, 0)
	if genesisBlock == nil {
		return nil, fmt.Errorf("genesis block missing from db")
	}
	genesisHeader := genesisBlock.Header()
	genesis.Nonce = genesisHeader.Nonce.Uint64()
	genesis.Timestamp = genesisHeader.Time
	genesis.ExtraData = genesisHeader.Extra
	genesis.GasLimit = genesisHeader.GasLimit
	genesis.Difficulty = genesisHeader.Difficulty
	genesis.Mixhash = genesisHeader.MixDigest
	genesis.Coinbase = genesisHeader.Coinbase

	return &genesis, nil
}

// GenesisAlloc specifies the initial state that is part of the genesis block.
type GenesisAlloc map[common.Address]GenesisAccount

func (ga *GenesisAlloc) UnmarshalJSON(data []byte) error {
	m := make(map[common.UnprefixedAddress]GenesisAccount)
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ga = make(GenesisAlloc)
	for addr, a := range m {
		(*ga)[common.Address(addr)] = a
	}
	return nil
}

// deriveHash computes the state root according to the genesis specification.
func (ga *GenesisAlloc) deriveHash() (common.Hash, error) {
	// Create an ephemeral in-memory database for computing hash,
	// all the derived states will be discarded to not pollute disk.
	db := state.NewDatabase(rawdb.NewMemoryDatabase())
	statedb, err := state.New(common.Hash{}, db, nil)
	if err != nil {
		return common.Hash{}, err
	}
	for addr, account := range *ga {
		statedb.AddBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	return statedb.Commit(false)
}

// flush is very similar with deriveHash, but the main difference is
// all the generated states will be persisted into the given database.
// Also, the genesis state specification will be flushed as well.
func (ga *GenesisAlloc) flush(db ethdb.Database, triedb *trie.Database, blockhash common.Hash) error {
	statedb, err := state.New(common.Hash{}, state.NewDatabaseWithNodeDB(db, triedb), nil)
	if err != nil {
		return err
	}
	for addr, account := range *ga {
		statedb.AddBalance(addr, account.Balance)
		statedb.SetCode(addr, account.Code)
		statedb.SetNonce(addr, account.Nonce)
		for key, value := range account.Storage {
			statedb.SetState(addr, key, value)
		}
	}
	root, err := statedb.Commit(false)
	if err != nil {
		return err
	}
	// Commit newly generated states into disk if it's not empty.
	if root != types.EmptyRootHash {
		if err := triedb.Commit(root, true); err != nil {
			return err
		}
	}
	// Marshal the genesis state specification and persist.
	blob, err := json.Marshal(ga)
	if err != nil {
		return err
	}
	rawdb.WriteGenesisStateSpec(db, blockhash, blob)
	return nil
}

// CommitGenesisState loads the stored genesis state with the given block
// hash and commits it into the provided trie database.
func CommitGenesisState(db ethdb.Database, triedb *trie.Database, blockhash common.Hash) error {
	var alloc GenesisAlloc
	blob := rawdb.ReadGenesisStateSpec(db, blockhash)
	if len(blob) != 0 {
		if err := alloc.UnmarshalJSON(blob); err != nil {
			return err
		}
	} else {
		// Genesis allocation is missing and there are several possibilities:
		// the node is legacy which doesn't persist the genesis allocation or
		// the persisted allocation is just lost.
		// - supported networks(mainnet, testnets), recover with defined allocations
		// - private network, can't recover
		var genesis *Genesis
		switch blockhash {
		case params.MainnetGenesisHash:
			genesis = DefaultGenesisBlock()
		case params.RinkebyGenesisHash:
			genesis = DefaultRinkebyGenesisBlock()
		case params.GoerliGenesisHash:
			genesis = DefaultGoerliGenesisBlock()
		case params.SepoliaGenesisHash:
			genesis = DefaultSepoliaGenesisBlock()
		case params.IncentivTestnetGenesisHash:
			genesis = DefaultIncentivTestnetGenesisBlock()
		case params.IncentivMainnetGenesisHash:
			genesis = DefaultIncentivMainnetGenesisBlock()
		case params.IncentivDevnetGenesisHash:
			genesis = DefaultIncentivDevnetGenesisBlock()
		}
		if genesis != nil {
			alloc = genesis.Alloc
		} else {
			return errors.New("not found")
		}
	}
	return alloc.flush(db, triedb, blockhash)
}

// GenesisAccount is an account in the state of the genesis block.
type GenesisAccount struct {
	Code       []byte                      `json:"code,omitempty"`
	Storage    map[common.Hash]common.Hash `json:"storage,omitempty"`
	Balance    *big.Int                    `json:"balance" gencodec:"required"`
	Nonce      uint64                      `json:"nonce,omitempty"`
	PrivateKey []byte                      `json:"secretKey,omitempty"` // for tests
}

// field type overrides for gencodec
type genesisSpecMarshaling struct {
	Nonce      math.HexOrDecimal64
	Timestamp  math.HexOrDecimal64
	ExtraData  hexutil.Bytes
	GasLimit   math.HexOrDecimal64
	GasUsed    math.HexOrDecimal64
	Number     math.HexOrDecimal64
	Difficulty *math.HexOrDecimal256
	BaseFee    *math.HexOrDecimal256
	Alloc      map[common.UnprefixedAddress]GenesisAccount
}

type genesisAccountMarshaling struct {
	Code       hexutil.Bytes
	Balance    *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	Storage    map[storageJSON]storageJSON
	PrivateKey hexutil.Bytes
}

// storageJSON represents a 256 bit byte array, but allows less than 256 bits when
// unmarshaling from hex.
type storageJSON common.Hash

func (h *storageJSON) UnmarshalText(text []byte) error {
	text = bytes.TrimPrefix(text, []byte("0x"))
	if len(text) > 64 {
		return fmt.Errorf("too many hex characters in storage key/value %q", text)
	}
	offset := len(h) - len(text)/2 // pad on the left
	if _, err := hex.Decode(h[offset:], text); err != nil {
		return fmt.Errorf("invalid hex storage key/value %q", text)
	}
	return nil
}

func (h storageJSON) MarshalText() ([]byte, error) {
	return hexutil.Bytes(h[:]).MarshalText()
}

// GenesisMismatchError is raised when trying to overwrite an existing
// genesis block with an incompatible one.
type GenesisMismatchError struct {
	Stored, New common.Hash
}

func (e *GenesisMismatchError) Error() string {
	return fmt.Sprintf("database contains incompatible genesis (have %x, new %x)", e.Stored, e.New)
}

// ChainOverrides contains the changes to chain config.
type ChainOverrides struct {
	OverrideShanghai *uint64

	// OverrideWebAuthnStrict reschedules or arms the WebAuthnStrict fork without a
	// new binary, on any chain including one this binary ships a schedule for, and
	// takes precedence over that schedule. It cannot turn the fork off: a *uint64
	// carries no value meaning "never", and 0 is rejected by CheckConfigForkOrder for
	// preceding the timestamp fork before it in the ordering — dynamicMinBaseFeeTime on
	// mainnet and devnet, shanghaiTime on testnet. Postponing it far enough is the only
	// lever.
	OverrideWebAuthnStrict *uint64
}

// apply returns config with the requested fork overrides applied. It copies rather
// than writing through the pointer, because the configs it is handed are shared:
// configOrDefault returns package-level values such as params.AllEthashProtocolChanges,
// and the private-network branch hands it the very config CheckCompatible is about to
// compare against. With no overrides it returns its argument unchanged, as upstream
// does, because callers are handed the configuration they passed and some of them go
// on to mutate it.
func (o *ChainOverrides) apply(config *params.ChainConfig) *params.ChainConfig {
	if config == nil || !hasOverrides(o) {
		return config
	}
	cpy := *config
	if o.OverrideShanghai != nil {
		cpy.ShanghaiTime = o.OverrideShanghai
	}
	if o.OverrideWebAuthnStrict != nil {
		cpy.WebAuthnStrictTime = o.OverrideWebAuthnStrict
	}
	return &cpy
}

// incentivBundledConfig reports the configuration this binary ships for the Incentiv
// network that the stored genesis hash and chain id identify together, and whether one
// was found but must not be used.
//
// Two bounds, both on the head. checkCompatible does not compare the block-numbered
// settings, so adopting a config that disagrees about those would apply them to history
// that never followed them, with no error and no rewind — but only a setting at or below
// the head rewrites a block that exists, and this chain ships those settings as a value
// some releases ahead of every node. And a difference checkCompatible *does* object to is
// not taken either: adopting it would hand NewBlockChain a ConfigCompatError, which it
// acts on by rewinding the chain. Nobody asked for that by starting without a network
// flag, so an implicit adoption never rewinds; accepting one is what the flag is for. The
// bound is on the adoption, not on the whole start: an override is applied over the
// stored config afterwards, and one naming a timestamp already behind the head does
// produce a rewind — which is the operator asking for it in as many words.
func incentivBundledConfig(storedcfg *params.ChainConfig, ghash common.Hash, head *types.Header, overrides *ChainOverrides) (adopt *params.ChainConfig, refusal configRefusal, cause string) {
	if storedcfg == nil || head == nil {
		return nil, refusalNone, ""
	}
	bundled := params.BundledIncentivConfig(ghash, storedcfg.ChainID)
	if bundled == nil {
		return nil, refusalNone, ""
	}
	// The candidate is the bundled schedule as this node would run it. An override is
	// part of that: comparing the un-overridden schedule reports a difference the node
	// would never have had, and the warning built on it told a healthy node it would
	// not activate.
	candidate := overrides.apply(bundled)
	// Both answers, always: which refusal fires is decided by the block-numbered
	// comparison first, but a caller describing a history refusal needs to know whether
	// taking the schedule under a network flag would *also* rewind the chain. It often
	// would — the documented testnet genesis.json carries no shanghaiTime — and saying
	// "the flag does not rewind this datadir" on the strength of the block-numbered
	// settings alone was wrong for exactly that case.
	compat := storedcfg.CheckCompatible(candidate, head.Number.Uint64(), head.Time)
	if compat != nil {
		// What disagreed decides what to tell the operator: this fork's timestamp is one
		// thing, an older fork the stored config never had is another.
		cause = compat.What
	}
	if !params.HistoricalForksCompatible(storedcfg, candidate, head.Number.Uint64()) {
		return nil, refusalHistory, cause
	}
	if compat != nil {
		return nil, refusalRewind, cause
	}
	return bundled, refusalNone, ""
}

// SetupGenesisBlock writes or updates the genesis block in db.
// The block that will be used is:
//
//	                     genesis == nil       genesis != nil
//	                  +------------------------------------------
//	db has no genesis |  main-net default  |  genesis
//	db has genesis    |  from DB           |  genesis (if compatible)
//
// The stored chain configuration will be updated if it is compatible (i.e. does not
// specify a fork block below the local head block). In case of a conflict, the
// error is a *params.ConfigCompatError and the new, unwritten config is returned.
//
// The returned chain configuration is never nil.
func SetupGenesisBlock(db ethdb.Database, triedb *trie.Database, genesis *Genesis) (*params.ChainConfig, common.Hash, error) {
	return SetupGenesisBlockWithOverride(db, triedb, genesis, nil)
}

// SameChainID reports whether two chain ids are the same, nil included. A chain id is
// not a fork block, so this is a plain comparison rather than checkCompatible's; the
// setup path and the read-only chain commands both decide the wrong-network case with it.
func SameChainID(a, b *big.Int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(b) == 0
}

// webAuthnStrictRefusalWindow is how far ahead of a network's bundled activation a
// refused schedule stops being a warning and becomes a failed start. A node that keeps a
// configuration without the fork follows a different chain from activation, so it must
// not be left running into it looking healthy. A network whose activation is still far
// off — testnet's is in 2028, and its window is yet to be agreed — gets the warning it
// always got, so a stand that ran on the previous release does not stop on this one for
// a fork two years away.
const webAuthnStrictRefusalWindow = 30 * 24 * 60 * 60

// startupClock is what the refusal below reads the wall clock from. A variable so a test
// can place the clock near an activation that its synthetic head is far from.
var startupClock = time.Now

// webAuthnStrictActivationNear reports whether bundled's activation is within
// webAuthnStrictRefusalWindow of now, or already behind it. "Now" is the later of the
// head's timestamp and the wall clock: a node restored from an old backup has a head
// weeks behind, and the activation it would miss is measured from today, not from where
// its chain stopped. The consensus rules themselves are still chosen by block time; only
// this start-up guard reads the clock.
func webAuthnStrictActivationNear(bundled *params.ChainConfig, headTime uint64) bool {
	if bundled == nil || bundled.WebAuthnStrictTime == nil {
		return false
	}
	now := headTime
	if wall := startupClock().Unix(); wall > 0 && uint64(wall) > now {
		now = uint64(wall)
	}
	return now+webAuthnStrictRefusalWindow >= *bundled.WebAuthnStrictTime
}

// hasOverrides reports whether any fork override was actually requested.
func hasOverrides(overrides *ChainOverrides) bool {
	return overrides != nil && *overrides != ChainOverrides{}
}

// checkConfigInvariants runs the chain-config checks that every startup path has to
// pass, on the configuration that path actually ended up with.
func checkConfigInvariants(config *params.ChainConfig) error {
	if err := config.CheckConfigForkOrder(); err != nil {
		return err
	}
	if err := config.CheckDPoWConfig(); err != nil {
		return err
	}
	return config.CheckMinBaseFeeConfig()
}

func SetupGenesisBlockWithOverride(db ethdb.Database, triedb *trie.Database, genesis *Genesis, overrides *ChainOverrides) (*params.ChainConfig, common.Hash, error) {
	if genesis != nil && genesis.Config == nil {
		return params.AllEthashProtocolChanges, common.Hash{}, errGenesisNoConfig
	}
	// Just commit the new block if there is no stored genesis block.
	stored := rawdb.ReadCanonicalHash(db, 0)
	if (stored == common.Hash{}) {
		if genesis == nil {
			log.Info("Writing default main-net genesis block")
			genesis = DefaultGenesisBlock()
		} else {
			log.Info("Writing custom genesis block")
		}
		// Check the override before committing, so a bad one leaves no database behind.
		// Only the override needs checking here: Commit runs the same three checks on
		// genesis.Config, which is what cfg is when there is nothing to override.
		cfg := overrides.apply(genesis.Config)
		if hasOverrides(overrides) {
			if err := checkConfigInvariants(cfg); err != nil {
				return cfg, common.Hash{}, err
			}
		}
		block, err := genesis.Commit(db, triedb)
		if err != nil {
			return genesis.Config, common.Hash{}, err
		}
		if hasOverrides(overrides) {
			// Commit wrote genesis.Config; the overridden one has to replace it.
			rawdb.WriteChainConfig(db, block.Hash(), cfg)
		}
		return cfg, block.Hash(), nil
	}
	// A chain id is not a schedule. CheckCompatible reports a changed one as an
	// incompatibility at block 0, which the guard at the end of this function discards,
	// so a genesis specification naming another chain id used to be written over the
	// stored one with no error and no rewind — and the branch just below, which
	// re-commits a genesis whose state is missing, wrote it without any comparison at
	// all. Incentiv mainnet and devnet share a genesis hash, so the wrong network flag is
	// exactly such a specification. Checked here, before anything is written, and only
	// for a specification of the stored genesis: a different genesis is a different
	// chain altogether, which the branches below report as the mismatch it is.
	if genesis != nil && genesis.ToBlock().Hash() == stored {
		if storedcfg := rawdb.ReadChainConfig(db, stored); storedcfg != nil && !SameChainID(genesis.Config.ChainID, storedcfg.ChainID) {
			return genesis.Config, stored, fmt.Errorf("this database holds chain %v and the genesis specification names chain %v; "+
				"a chain id cannot change on a database that already has one. Incentiv mainnet and devnet share a genesis hash, "+
				"so check the network flag", storedcfg.ChainID, genesis.Config.ChainID)
		}
	}
	// We have the genesis block in database(perhaps in ancient database)
	// but the corresponding state is missing.
	header := rawdb.ReadHeader(db, stored, 0)
	if header.Root != types.EmptyRootHash && !rawdb.HasLegacyTrieNode(db, header.Root) {
		if genesis == nil {
			genesis = DefaultGenesisBlock()
		}
		// Ensure the stored genesis matches with the given one.
		hash := genesis.ToBlock().Hash()
		if hash != stored {
			return genesis.Config, hash, &GenesisMismatchError{stored, hash}
		}
		cfg := overrides.apply(genesis.Config)
		if hasOverrides(overrides) {
			if err := checkConfigInvariants(cfg); err != nil {
				return cfg, hash, err
			}
		}
		block, err := genesis.Commit(db, triedb)
		if err != nil {
			return genesis.Config, hash, err
		}
		if hasOverrides(overrides) {
			rawdb.WriteChainConfig(db, block.Hash(), cfg)
		}
		return cfg, block.Hash(), nil
	}
	// Check whether the genesis block is already written.
	if genesis != nil {
		hash := genesis.ToBlock().Hash()
		if hash != stored {
			return genesis.Config, hash, &GenesisMismatchError{stored, hash}
		}
	}
	// Get the existing chain configuration.
	storedcfg := rawdb.ReadChainConfig(db, stored)
	if storedcfg == nil {
		log.Warn("Found genesis block without chain config")
		newcfg := overrides.apply(genesis.configOrDefault(stored))
		// newcfg is configOrDefault's guess, and for an Incentiv genesis that is
		// AllEthashProtocolChanges — chain id 1337, with none of this chain's forks.
		// Writing it would hand the node a schedule from a different network. The
		// genesis hash cannot say which Incentiv network this is, since mainnet and
		// devnet share one, so there is nothing to fall back to: refuse and let the
		// operator name it.
		if genesis == nil && params.IsIncentivGenesisHash(stored) {
			return newcfg, common.Hash{}, fmt.Errorf("genesis %s belongs to an Incentiv network but the database holds no chain config; "+
				"start with the network flag (--incentiv-mainnet, --incentiv-testnet or --incentiv-devnet) so the right schedule is written", stored)
		}
		if err := checkConfigInvariants(newcfg); err != nil {
			return newcfg, common.Hash{}, err
		}
		rawdb.WriteChainConfig(db, stored, newcfg)
		return newcfg, stored, nil
	}
	storedData, _ := json.Marshal(storedcfg)
	head := rawdb.ReadHeadHeader(db)
	if head == nil {
		return storedcfg, stored, fmt.Errorf("missing head header")
	}
	// Which configuration a start settles on is decided in one place, ConfigOrStored,
	// so that the read-only chain commands cannot resolve it differently from the node.
	// The branches it covers are the ones this function used to spell out here: a
	// genesis specification answers for itself, the real mainnet hash takes
	// configOrDefault, an Incentiv genesis takes the bundled schedule unless the stored
	// config disagrees about history, and anything else keeps what the database says.
	newcfg, decision := genesis.ConfigOrStored(storedcfg, stored, head, overrides)
	switch {
	case decision.BundledRefused:
		// The refusal says the bundled schedule was not taken. It does not say what the
		// node will run, and those are different questions: an override is applied over
		// the stored config afterwards, and the stored config may already carry a
		// timestamp of its own. Both were being reported as "kept the stored config, so
		// webauthnStrict will not activate here", which is false whenever the resolved
		// value is set — and the advice that went with it, restart with the network flag,
		// would then replace a timestamp the node is deliberately carrying with the
		// bundled one and move it off the fleet's schedule. So the line reports the
		// resolved value.
		message := "Stored chain config disagrees with the schedule this binary ships for this network"
		if decision.RefusedForRewind {
			message = "This binary's schedule for this network was not taken because it would rewind the chain"
		}
		source := "the stored config, kept whole"
		if overrides != nil && overrides.OverrideWebAuthnStrict != nil {
			source = "--override.webauthnstrict, applied over the stored config"
		}
		// The remedy follows the reason before it follows the resolved timestamp. A
		// history refusal is not fixed by the network flag at all: the flag writes those
		// settings over blocks already mined without rewinding, which is what
		// docs/webauthn/rollout.md means by "not a repair". And a rewind refusal caused
		// by some *older* fork is not about this one, so the node is on an incompatible
		// configuration whatever its webauthnStrictTime says.
		var fix string
		switch {
		case decision.RefusedForHistory:
			fix = "resync from genesis under the network flag. The flag does not undo what is already there: CheckCompatible does not compare these settings, so no rewind is computed from them and they are applied to blocks already mined as they are"
			if decision.RewindCause != "" {
				fix += ". That start would still rewind the chain, because the two configurations also disagree about " + decision.RewindCause + ". That rewind drops the blocks above its target, so it undoes this for those and leaves it standing for any below — and on a chain whose whole history is above the target it undoes all of it"
			}
		case decision.RewindCause != params.WebAuthnStrictCompatWhat:
			fix = "restart with the network flag, which does take the schedule and rewinds to before the fork named in cause. This node disagrees with the network about that fork, not about webauthnStrict, so the timestamp above does not settle it"
		case newcfg.WebAuthnStrictTime == nil:
			fix = "restart with the network flag, which does take the schedule: it rewinds to before the fork that had already fired and re-syncs from there, and is how a node that reached this release late catches up"
		default:
			fix = "nothing further, if this is deliberate: the node will activate at the timestamp above, and starting it with its network flag would replace that with the schedule this binary ships — which is the right move only if the timestamp above is the one that is wrong"
		}
		log.Warn(message,
			"chain", storedcfg.ChainID, "genesis", stored,
			"webauthnStrictTime", forkTimeForLog(newcfg.WebAuthnStrictTime),
			"source", source,
			"cause", refusalCauseForLog(decision),
			"fix", fix)
		if newcfg.WebAuthnStrictTime == nil && webAuthnStrictActivationNear(params.BundledIncentivConfig(stored, storedcfg.ChainID), head.Time) {
			// Warned, and then refused. A node that keeps a configuration without the
			// fork follows a different chain from activation, and a start that only
			// warned left it running and looking healthy until then. Only once the
			// activation is near, though: further out the warning stands, and the
			// operator has the time it names. An operator who means to activate at
			// another time says so with --override.webauthnstrict, which the line above
			// would then report as the resolved timestamp; the override cannot turn the
			// fork off, and a timestamp already behind the head rewinds the chain.
			return newcfg, stored, fmt.Errorf("%s, and this node would run without webauthnStrictTime, so it would not activate the fork with the rest of its network "+
				"(cause: %s; fix: %s; to activate at a different time deliberately, pass --override.webauthnstrict=<future timestamp>, "+
				"which cannot turn the fork off and rewinds the chain if the timestamp has passed; a private stand gets a chain id of its own and a resync)",
				message, refusalCauseForLog(decision), fix)
		}
	case decision.BundledAdopted && storedcfg.WebAuthnStrictTime != nil &&
		(overrides == nil || overrides.OverrideWebAuthnStrict == nil) &&
		(newcfg.WebAuthnStrictTime == nil || *newcfg.WebAuthnStrictTime != *storedcfg.WebAuthnStrictTime):
		// Correcting a drifted timestamp is what the adoption is for, but one of the
		// timestamps it corrects is an override's: the earlier start wrote it, this start
		// has no flag, and the node moves back to the bundled schedule. That is the
		// documented behaviour, and it must not be silent, because the fleet may be
		// waiting on the stored timestamp.
		log.Warn("Stored WebAuthnStrict timestamp replaced by the schedule this binary ships",
			"chain", storedcfg.ChainID, "genesis", stored,
			"stored", *storedcfg.WebAuthnStrictTime,
			"webauthnStrictTime", forkTimeForLog(newcfg.WebAuthnStrictTime),
			"note", "an --override.webauthnstrict that is no longer on the command line is not carried over; pass it again if this node is meant to stay on the stored timestamp")
	case decision.FromGenesis && !params.HistoricalForksCompatible(storedcfg, newcfg, head.Number.Uint64()):
		// The flag wins, which is what it is for, but it is worth saying what part of
		// that nothing else will report.
		log.Warn("Network flag replaces block-numbered settings this database disagrees with",
			"chain", storedcfg.ChainID, "genesis", stored,
			"note", "CheckCompatible does not compare these, so no rewind is computed from them and they take effect for blocks already mined as they are. A rewind on this start, if there is one, comes from a timestamp fork instead: it drops the blocks above its own target, so it clears this for those and leaves it standing for any below")
	}
	// Validate the config that will actually be used, whichever branch produced it.
	// Running this on configOrDefault's result instead would check AllEthashProtocolChanges
	// rather than this chain, and reject overrides that are valid for it.
	if err := checkConfigInvariants(newcfg); err != nil {
		return newcfg, common.Hash{}, err
	}
	// Check config compatibility and write the config. Compatibility errors
	// are returned to the caller unless we're already at block zero.
	compatErr := storedcfg.CheckCompatible(newcfg, head.Number.Uint64(), head.Time)
	if compatErr != nil && ((head.Number.Uint64() != 0 && compatErr.RewindToBlock != 0) || (head.Time != 0 && compatErr.RewindToTime != 0)) {
		return newcfg, stored, compatErr
	}
	// Don't overwrite if the old is identical to the new
	if newData, _ := json.Marshal(newcfg); !bytes.Equal(storedData, newData) {
		rawdb.WriteChainConfig(db, stored, newcfg)
	}
	return newcfg, stored, nil
}

// LoadCliqueConfig loads the stored clique config if the chain config
// is already present in database, otherwise, return the config in the
// provided genesis specification. Note the returned clique config can
// be nil if we are not in the clique network.
func LoadCliqueConfig(db ethdb.Database, genesis *Genesis) (*params.CliqueConfig, error) {
	// Load the stored chain config from the database. It can be nil
	// in case the database is empty. Notably, we only care about the
	// chain config corresponds to the canonical chain.
	stored := rawdb.ReadCanonicalHash(db, 0)
	if stored != (common.Hash{}) {
		storedcfg := rawdb.ReadChainConfig(db, stored)
		if storedcfg != nil {
			return storedcfg.Clique, nil
		}
	}
	// Load the clique config from the provided genesis specification.
	if genesis != nil {
		// Reject invalid genesis spec without valid chain config
		if genesis.Config == nil {
			return nil, errGenesisNoConfig
		}
		// If the canonical genesis header is present, but the chain
		// config is missing(initialize the empty leveldb with an
		// external ancient chain segment), ensure the provided genesis
		// is matched.
		if stored != (common.Hash{}) && genesis.ToBlock().Hash() != stored {
			return nil, &GenesisMismatchError{stored, genesis.ToBlock().Hash()}
		}
		return genesis.Config.Clique, nil
	}
	// There is no stored chain config and no new config provided,
	// In this case the default chain config(mainnet) will be used,
	// namely ethash is the specified consensus engine, return nil.
	return nil, nil
}

// configRefusal says why an Incentiv network's bundled schedule was not taken. The two
// reasons want different things said about them: one is a chain that is not following this
// network at all, the other is a chain that is but has arrived at this binary late.
type configRefusal int

const (
	// refusalNone: nothing was refused — either the schedule was taken, or this genesis
	// is not one of ours.
	refusalNone configRefusal = iota
	// refusalHistory: the stored config disagrees about a block-numbered setting that is
	// already at or below the head. checkCompatible does not compare those, so adopting
	// would apply them to blocks already mined with no error and no rewind.
	refusalHistory
	// refusalRewind: checkCompatible itself objects, so adopting would hand NewBlockChain
	// a ConfigCompatError and it would rewind the chain. A start without a network flag
	// does not ask for that.
	refusalRewind
)

// refusalCauseForLog names what the two configurations disagreed about, so the remedy on
// the same line can be checked against it.
func refusalCauseForLog(d ConfigDecision) string {
	if !d.RefusedForHistory {
		return d.RewindCause
	}
	cause := "block-numbered settings CheckCompatible does not compare"
	if d.RewindCause != "" {
		cause += " (and " + d.RewindCause + ")"
	}
	return cause
}

// forkTimeForLog renders a fork timestamp for an operator reading a log line. "unset" is
// the case that matters: it is the one where the node will not activate.
func forkTimeForLog(t *uint64) string {
	if t == nil {
		return "unset"
	}
	return strconv.FormatUint(*t, 10)
}

// ConfigDecision says how ConfigOrStored reached the configuration it returned. A caller
// that wants to say something about that — a log line, a refusal — has to be told rather
// than work it out again: the setup path used to re-derive it and got the network-flag
// case backwards, warning that the stored config had been kept while the flag's schedule
// was being written.
type ConfigDecision struct {
	// FromGenesis: a genesis specification answered, which is a start with a network
	// flag. The stored configuration played no part.
	FromGenesis bool
	// BundledAdopted: this binary's schedule for an Incentiv network was taken over the
	// stored configuration.
	BundledAdopted bool
	// BundledRefused: one was available and was not taken, because the stored
	// configuration disagrees with it about blocks that already exist. The stored
	// configuration was kept whole.
	BundledRefused bool
	// RefusedForRewind narrows BundledRefused to the case where taking the schedule
	// would have rewound the chain — which is what a node that reached this binary after
	// a fork fired looks like, and is fixed by starting it with its network flag.
	RefusedForRewind bool
	// RefusedForHistory narrows BundledRefused to a disagreement about the block-numbered
	// settings checkCompatible does not compare. The network flag is *not* a repair for
	// that one: it writes those settings over blocks already mined without rewinding, so
	// the two refusals need different advice.
	RefusedForHistory bool
	// RewindCause is the ConfigCompatError's What for a RefusedForRewind, so a caller can
	// tell a webauthnStrictTime disagreement from an older fork's.
	RewindCause string
}

// ConfigOrStored returns the chain configuration a startup would settle on for this
// genesis specification, given what the database already holds, the head it holds and
// the overrides it would run with, along with how it got there. It is what the read-only
// chain commands compare against, so that they can refuse before the setup path tries to
// persist a change they cannot make. The overrides belong in the comparison because the
// setup path applies them too: a command run without the override the node runs with
// would otherwise report a difference that is its own doing. head must not be nil.
func (g *Genesis) ConfigOrStored(storedcfg *params.ChainConfig, ghash common.Hash, head *types.Header, overrides *ChainOverrides) (*params.ChainConfig, ConfigDecision) {
	if g != nil {
		return overrides.apply(g.Config), ConfigDecision{FromGenesis: true}
	}
	if storedcfg == nil {
		return nil, ConfigDecision{}
	}
	// The stored config is reused only on the chains the setup path treats as private,
	// which is every genesis but the real mainnet one. With that hash it takes
	// configOrDefault's answer instead, so resolving to the stored config here would
	// miss a rewrite it is about to make.
	if ghash == params.MainnetGenesisHash {
		return overrides.apply(g.configOrDefault(ghash)), ConfigDecision{}
	}
	adopt, refusal, cause := incentivBundledConfig(storedcfg, ghash, head, overrides)
	if adopt != nil {
		// Copy: adopt is a package-level configuration, and what this returns may be
		// held, and mutated, for the life of the process. overrides.apply copies only
		// when there is something to apply.
		cpy := *adopt
		return overrides.apply(&cpy), ConfigDecision{BundledAdopted: true}
	}
	return overrides.apply(storedcfg), ConfigDecision{
		BundledRefused:    refusal != refusalNone,
		RefusedForRewind:  refusal == refusalRewind,
		RefusedForHistory: refusal == refusalHistory,
		RewindCause:       cause,
	}
}

func (g *Genesis) configOrDefault(ghash common.Hash) *params.ChainConfig {
	switch {
	case g != nil:
		return g.Config
	case ghash == params.MainnetGenesisHash:
		return params.MainnetChainConfig
	case ghash == params.SepoliaGenesisHash:
		return params.SepoliaChainConfig
	case ghash == params.RinkebyGenesisHash:
		return params.RinkebyChainConfig
	case ghash == params.GoerliGenesisHash:
		return params.GoerliChainConfig
	default:
		return params.AllEthashProtocolChanges
	}
}

// ToBlock returns the genesis block according to genesis specification.
func (g *Genesis) ToBlock() *types.Block {
	root, err := g.Alloc.deriveHash()
	if err != nil {
		panic(err)
	}
	head := &types.Header{
		Number:     new(big.Int).SetUint64(g.Number),
		Nonce:      types.EncodeNonce(g.Nonce),
		Time:       g.Timestamp,
		ParentHash: g.ParentHash,
		Extra:      g.ExtraData,
		GasLimit:   g.GasLimit,
		GasUsed:    g.GasUsed,
		BaseFee:    g.BaseFee,
		Difficulty: g.Difficulty,
		MixDigest:  g.Mixhash,
		Coinbase:   g.Coinbase,
		Root:       root,
	}
	if g.GasLimit == 0 {
		head.GasLimit = params.GenesisGasLimit
	}
	if g.Difficulty == nil && g.Mixhash == (common.Hash{}) {
		head.Difficulty = params.GenesisDifficulty
	}
	if g.Config != nil && g.Config.IsLondon(common.Big0) {
		if g.BaseFee != nil {
			head.BaseFee = g.BaseFee
		} else {
			head.BaseFee = new(big.Int).SetUint64(params.InitialBaseFee)
		}
	}
	var withdrawals []*types.Withdrawal
	if g.Config != nil && g.Config.IsShanghai(g.Timestamp) {
		head.WithdrawalsHash = &types.EmptyWithdrawalsHash
		withdrawals = make([]*types.Withdrawal, 0)
	}
	return types.NewBlock(head, nil, nil, nil, trie.NewStackTrie(nil)).WithWithdrawals(withdrawals)
}

// Commit writes the block and state of a genesis specification to the database.
// The block is committed as the canonical head block.
func (g *Genesis) Commit(db ethdb.Database, triedb *trie.Database) (*types.Block, error) {
	block := g.ToBlock()
	if block.Number().Sign() != 0 {
		return nil, errors.New("can't commit genesis block with number > 0")
	}
	config := g.Config
	if config == nil {
		config = params.AllEthashProtocolChanges
	}
	if err := checkConfigInvariants(config); err != nil {
		return nil, err
	}
	if config.Clique != nil && len(block.Extra()) < 32+crypto.SignatureLength {
		return nil, errors.New("can't start clique chain without signers")
	}
	// All the checks has passed, flush the states derived from the genesis
	// specification as well as the specification itself into the provided
	// database.
	if err := g.Alloc.flush(db, triedb, block.Hash()); err != nil {
		return nil, err
	}
	rawdb.WriteTd(db, block.Hash(), block.NumberU64(), block.Difficulty())
	rawdb.WriteBlock(db, block)
	rawdb.WriteReceipts(db, block.Hash(), block.NumberU64(), nil)
	rawdb.WriteCanonicalHash(db, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(db, block.Hash())
	rawdb.WriteHeadFastBlockHash(db, block.Hash())
	rawdb.WriteHeadHeaderHash(db, block.Hash())
	rawdb.WriteChainConfig(db, block.Hash(), config)
	return block, nil
}

// MustCommit writes the genesis block and state to db, panicking on error.
// The block is committed as the canonical head block.
// Note the state changes will be committed in hash-based scheme, use Commit
// if path-scheme is preferred.
func (g *Genesis) MustCommit(db ethdb.Database) *types.Block {
	block, err := g.Commit(db, trie.NewDatabase(db))
	if err != nil {
		panic(err)
	}
	return block
}

// DefaultGenesisBlock returns the Ethereum main net genesis block.
func DefaultGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.MainnetChainConfig,
		Nonce:      66,
		ExtraData:  hexutil.MustDecode("0x11bbe8db4e347b4e8c937c1c8370e4b5ed33adb3db69cbdb7a38e1e50b1b82fa"),
		GasLimit:   5000,
		Difficulty: big.NewInt(17179869184),
		Alloc:      decodePrealloc(mainnetAllocData),
	}
}

// DefaultRinkebyGenesisBlock returns the Rinkeby network genesis block.
func DefaultRinkebyGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.RinkebyChainConfig,
		Timestamp:  1492009146,
		ExtraData:  hexutil.MustDecode("0x52657370656374206d7920617574686f7269746168207e452e436172746d616e42eb768f2244c8811c63729a21a3569731535f067ffc57839b00206d1ad20c69a1981b489f772031b279182d99e65703f0076e4812653aab85fca0f00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   4700000,
		Difficulty: big.NewInt(1),
		Alloc:      decodePrealloc(rinkebyAllocData),
	}
}

// DefaultGoerliGenesisBlock returns the Görli network genesis block.
func DefaultGoerliGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.GoerliChainConfig,
		Timestamp:  1548854791,
		ExtraData:  hexutil.MustDecode("0x22466c6578692069732061207468696e6722202d204166726900000000000000e0a2bd4258d2768837baa26a28fe71dc079f84c70000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"),
		GasLimit:   10485760,
		Difficulty: big.NewInt(1),
		Alloc:      decodePrealloc(goerliAllocData),
	}
}

// DefaultSepoliaGenesisBlock returns the Sepolia network genesis block.
func DefaultSepoliaGenesisBlock() *Genesis {
	return &Genesis{
		Config:     params.SepoliaChainConfig,
		Nonce:      0,
		ExtraData:  []byte("Sepolia, Athens, Attica, Greece!"),
		GasLimit:   0x1c9c380,
		Difficulty: big.NewInt(0x20000),
		Timestamp:  1633267481,
		Alloc:      decodePrealloc(sepoliaAllocData),
	}
}

// DefaultIncentivTestnetGenesisBlock returns the Incentiv Testnet genesis block.
func DefaultIncentivTestnetGenesisBlock() *Genesis {
	// Define expected allocation addresses for validation using constants
	expectedAddresses := []string{
		IncentivTestnetAddr1,
		IncentivTestnetAddr2,
	}

	// Validate addresses before creating genesis
	for _, addr := range expectedAddresses {
		if !common.IsHexAddress(addr) {
			panic("DefaultIncentivTestnetGenesisBlock: invalid pre-allocation address: " + addr)
		}
	}

	// Parse balances with proper error handling
	balance1, ok1 := new(big.Int).SetString(IncentivTestnetBalance1, 10)
	if !ok1 {
		panic("DefaultIncentivTestnetGenesisBlock: invalid balance1 string: " + IncentivTestnetBalance1)
	}

	balance2, ok2 := new(big.Int).SetString(IncentivTestnetBalance2, 10)
	if !ok2 {
		panic("DefaultIncentivTestnetGenesisBlock: invalid balance2 string: " + IncentivTestnetBalance2)
	}

	genesis := &Genesis{
		Config:     params.IncentivTestnetChainConfig,
		Nonce:      0x42,
		ExtraData:  []byte{},
		GasLimit:   IncentivTestnetGasLimit,
		Difficulty: big.NewInt(0x1),
		Timestamp:  0,
		Alloc: GenesisAlloc{
			common.HexToAddress(IncentivTestnetAddr1): {
				Balance: balance1,
			},
			common.HexToAddress(IncentivTestnetAddr2): {
				Balance: balance2,
			},
		},
	}

	// Validate genesis hash to ensure consistency
	if genesis.ToBlock().Hash() != params.IncentivTestnetGenesisHash {
		panic("DefaultIncentivTestnetGenesisBlock: genesis hash mismatch - parameters may have been modified incorrectly")
	}

	return genesis
}

// DeveloperGenesisBlock returns the 'geth --dev' genesis block.
func DeveloperGenesisBlock(period uint64, gasLimit uint64, faucet common.Address) *Genesis {
	// Override the default period to the user requested one
	config := *params.AllCliqueProtocolChanges
	config.Clique = &params.CliqueConfig{
		Period: period,
		Epoch:  config.Clique.Epoch,
	}

	// Assemble and return the genesis with the precompiles and faucet pre-funded
	return &Genesis{
		Config:     &config,
		ExtraData:  append(append(make([]byte, 32), faucet[:]...), make([]byte, crypto.SignatureLength)...),
		GasLimit:   gasLimit,
		BaseFee:    big.NewInt(params.InitialBaseFee),
		Difficulty: big.NewInt(1),
		Alloc: map[common.Address]GenesisAccount{
			common.BytesToAddress([]byte{1}): {Balance: big.NewInt(1)}, // ECRecover
			common.BytesToAddress([]byte{2}): {Balance: big.NewInt(1)}, // SHA256
			common.BytesToAddress([]byte{3}): {Balance: big.NewInt(1)}, // RIPEMD
			common.BytesToAddress([]byte{4}): {Balance: big.NewInt(1)}, // Identity
			common.BytesToAddress([]byte{5}): {Balance: big.NewInt(1)}, // ModExp
			common.BytesToAddress([]byte{6}): {Balance: big.NewInt(1)}, // ECAdd
			common.BytesToAddress([]byte{7}): {Balance: big.NewInt(1)}, // ECScalarMul
			common.BytesToAddress([]byte{8}): {Balance: big.NewInt(1)}, // ECPairing
			common.BytesToAddress([]byte{9}): {Balance: big.NewInt(1)}, // BLAKE2b
			faucet:                           {Balance: new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(9))},
		},
	}
}

// DefaultIncentivMainnetGenesisBlock returns the Incentiv Mainnet genesis block.
func DefaultIncentivMainnetGenesisBlock() *Genesis {
	if !common.IsHexAddress(IncentivMainnetAddr1) {
		panic("DefaultIncentivMainnetGenesisBlock: invalid pre-allocation address: " + IncentivMainnetAddr1)
	}

	balance1, ok1 := new(big.Int).SetString(IncentivMainnetBalance1, 10)
	if !ok1 {
		panic("DefaultIncentivMainnetGenesisBlock: invalid balance1 string: " + IncentivMainnetBalance1)
	}

	genesis := &Genesis{
		Config:     params.IncentivMainnetChainConfig,
		Nonce:      0x42,
		ExtraData:  []byte{},
		GasLimit:   IncentivMainnetGasLimit,
		Difficulty: big.NewInt(0x1),
		Timestamp:  0,
		BaseFee:    big.NewInt(IncentivMainnetBaseFee),
		Alloc: GenesisAlloc{
			common.HexToAddress(IncentivMainnetAddr1): {
				Balance: balance1,
			},
		},
	}

	return genesis
}

// DefaultIncentivDevnetGenesisBlock returns the Incentiv Devnet genesis block.
func DefaultIncentivDevnetGenesisBlock() *Genesis {
	if !common.IsHexAddress(IncentivDevnetAddr1) {
		panic("DefaultIncentivDevnetGenesisBlock: invalid pre-allocation address: " + IncentivDevnetAddr1)
	}

	balance1, ok1 := new(big.Int).SetString(IncentivDevnetBalance1, 10)
	if !ok1 {
		panic("DefaultIncentivDevnetGenesisBlock: invalid balance1 string: " + IncentivDevnetBalance1)
	}

	genesis := &Genesis{
		Config:     params.IncentivDevnetChainConfig,
		Nonce:      0x42,
		ExtraData:  []byte{},
		GasLimit:   IncentivDevnetGasLimit,
		Difficulty: big.NewInt(0x1),
		Timestamp:  0,
		BaseFee:    big.NewInt(IncentivDevnetBaseFee),
		Alloc: GenesisAlloc{
			common.HexToAddress(IncentivDevnetAddr1): {
				Balance: balance1,
			},
		},
	}

	// Validate genesis hash to ensure consistency
	if genesis.ToBlock().Hash() != params.IncentivDevnetGenesisHash {
		panic("DefaultIncentivDevnetGenesisBlock: genesis hash mismatch - parameters may have been modified incorrectly")
	}

	return genesis
}
func decodePrealloc(data string) GenesisAlloc {
	var p []struct{ Addr, Balance *big.Int }
	if err := rlp.NewStream(strings.NewReader(data), 0).Decode(&p); err != nil {
		panic(err)
	}
	ga := make(GenesisAlloc, len(p))
	for _, account := range p {
		ga[common.BigToAddress(account.Addr)] = GenesisAccount{Balance: account.Balance}
	}
	return ga
}
