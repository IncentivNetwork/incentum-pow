package minbasefee_test

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/accounts/abi/bind/backends"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/contracts/minbasefee"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
)

// setupTestBackend creates a simulated blockchain for testing
func setupMinBaseFeeTestBackend(t *testing.T) (*backends.SimulatedBackend, *bind.TransactOpts, common.Address) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("Failed to generate key: %v", err)
	}

	auth, err := bind.NewKeyedTransactorWithChainID(key, big.NewInt(1337))
	if err != nil {
		t.Fatalf("Failed to create transactor: %v", err)
	}

	alloc := make(core.GenesisAlloc)
	alloc[auth.From] = core.GenesisAccount{Balance: big.NewInt(1000000000000000000)}

	sim := backends.NewSimulatedBackend(alloc, 10000000)

	return sim, auth, auth.From
}

func TestMinBaseFeeGovernorDeployment(t *testing.T) {
	sim, auth, _ := setupMinBaseFeeTestBackend(t)
	defer sim.Close()

	initialMinBaseFee := big.NewInt(12600000000000) // 12600 gwei
	activationBlock := big.NewInt(0)

	address, tx, contract, err := minbasefee.DeployMinBaseFeeGovernor(
		auth,
		sim,
		auth.From, // governance address
		initialMinBaseFee,
		activationBlock,
	)
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}

	sim.Commit()

	receipt, err := sim.TransactionReceipt(nil, tx.Hash())
	if err != nil {
		t.Fatalf("Failed to get receipt: %v", err)
	}

	if receipt.Status != 1 {
		t.Fatalf("Contract deployment failed")
	}

	t.Logf("Contract deployed at: %s", address.Hex())

	currentMinBaseFee, err := contract.GetCurrentMinBaseFee(nil)
	if err != nil {
		t.Fatalf("Failed to get current min base fee: %v", err)
	}

	if currentMinBaseFee.Cmp(initialMinBaseFee) != 0 {
		t.Errorf("Expected min base fee %v, got %v", initialMinBaseFee, currentMinBaseFee)
	}

	governance, err := contract.Governance(nil)
	if err != nil {
		t.Fatalf("Failed to get governance: %v", err)
	}

	if governance != auth.From {
		t.Errorf("Expected governance %s, got %s", auth.From.Hex(), governance.Hex())
	}
}

func TestMinBaseFeeGovernorTimelock(t *testing.T) {
	sim, auth, _ := setupMinBaseFeeTestBackend(t)
	defer sim.Close()

	initialMinBaseFee := big.NewInt(12600000000000)
	activationBlock := big.NewInt(0)

	_, _, contract, err := minbasefee.DeployMinBaseFeeGovernor(
		auth,
		sim,
		auth.From,
		initialMinBaseFee,
		activationBlock,
	)
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}
	sim.Commit()

	newMinBaseFee := big.NewInt(15000000000000) // 15000 gwei
	newActivationBlock := new(big.Int).Add(sim.Blockchain().CurrentBlock().Number, big.NewInt(20000))

	tx, err := contract.ProposeMinBaseFee(auth, newMinBaseFee, newActivationBlock)
	if err != nil {
		t.Fatalf("Failed to propose min base fee: %v", err)
	}
	sim.Commit()

	receipt, err := sim.TransactionReceipt(nil, tx.Hash())
	if err != nil {
		t.Fatalf("Failed to get receipt: %v", err)
	}

	if receipt.Status != 1 {
		t.Fatalf("Proposal transaction failed")
	}

	var proposalID [32]byte
	for _, log := range receipt.Logs {
		if len(log.Topics) > 0 {
			proposalID = log.Topics[1]
			break
		}
	}

	proposal, err := contract.GetProposal(nil, proposalID)
	if err != nil {
		t.Fatalf("Failed to get proposal: %v", err)
	}

	if !proposal.CanExecute {
		t.Log("Proposal cannot be executed immediately (timelock working)")
	}

	sim.AdjustTime(48 * time.Hour)
	sim.Commit()

	tx, err = contract.ExecuteProposal(auth, proposalID)
	if err != nil {
		t.Fatalf("Failed to execute proposal after timelock: %v", err)
	}
	sim.Commit()

	receipt, err = sim.TransactionReceipt(nil, tx.Hash())
	if err != nil {
		t.Fatalf("Failed to get receipt: %v", err)
	}

	if receipt.Status != 1 {
		t.Fatalf("Execution transaction failed")
	}

	historyLength, err := contract.GetConfigHistoryLength(nil)
	if err != nil {
		t.Fatalf("Failed to get history length: %v", err)
	}

	if historyLength.Cmp(big.NewInt(2)) != 0 {
		t.Errorf("Expected history length 2, got %v", historyLength)
	}
}

func TestMinBaseFeeGovernorAccessControl(t *testing.T) {
	sim, auth, _ := setupMinBaseFeeTestBackend(t)
	defer sim.Close()

	attackerKey, _ := crypto.GenerateKey()
	attackerAuth, _ := bind.NewKeyedTransactorWithChainID(attackerKey, big.NewInt(1337))

	initialMinBaseFee := big.NewInt(12600000000000)
	activationBlock := big.NewInt(0)

	_, _, contract, err := minbasefee.DeployMinBaseFeeGovernor(
		auth,
		sim,
		auth.From,
		initialMinBaseFee,
		activationBlock,
	)
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}
	sim.Commit()

	newMinBaseFee := big.NewInt(15000000000000)
	newActivationBlock := new(big.Int).Add(sim.Blockchain().CurrentBlock().Number, big.NewInt(20000))

	_, err = contract.ProposeMinBaseFee(attackerAuth, newMinBaseFee, newActivationBlock)
	if err == nil {
		t.Errorf("Attacker should not be able to propose")
	}

	tx, err := contract.Pause(attackerAuth)
	if err == nil {
		sim.Commit()
		receipt, _ := sim.TransactionReceipt(nil, tx.Hash())
		if receipt.Status == 1 {
			t.Errorf("Attacker should not be able to pause")
		}
	}
}

func TestMinBaseFeeGovernorSafetyBounds(t *testing.T) {
	sim, auth, _ := setupMinBaseFeeTestBackend(t)
	defer sim.Close()

	initialMinBaseFee := big.NewInt(12600000000000)
	activationBlock := big.NewInt(0)

	_, _, contract, err := minbasefee.DeployMinBaseFeeGovernor(
		auth,
		sim,
		auth.From,
		initialMinBaseFee,
		activationBlock,
	)
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}
	sim.Commit()

	newActivationBlock := new(big.Int).Add(sim.Blockchain().CurrentBlock().Number, big.NewInt(20000))

	_, err = contract.ProposeMinBaseFee(auth, big.NewInt(0), newActivationBlock)
	if err == nil {
		t.Errorf("Should not allow zero min base fee")
	}

	tooLarge := new(big.Int).Mul(big.NewInt(101), big.NewInt(1000000000000000000))
	_, err = contract.ProposeMinBaseFee(auth, tooLarge, newActivationBlock)
	if err == nil {
		t.Errorf("Should not allow min base fee above MAX_MIN_BASE_FEE")
	}

	tooSmall := big.NewInt(500000000)
	_, err = contract.ProposeMinBaseFee(auth, tooSmall, newActivationBlock)
	if err == nil {
		t.Errorf("Should not allow min base fee below MIN_MIN_BASE_FEE")
	}

	tooLargeChange := new(big.Int).Mul(initialMinBaseFee, big.NewInt(4))
	_, err = contract.ProposeMinBaseFee(auth, tooLargeChange, newActivationBlock)
	if err == nil {
		t.Errorf("Should not allow change greater than MAX_CHANGE_PERCENT")
	}

	tooSmallChange := new(big.Int).Div(initialMinBaseFee, big.NewInt(4))
	_, err = contract.ProposeMinBaseFee(auth, tooSmallChange, newActivationBlock)
	if err == nil {
		t.Errorf("Should not allow change less than 1/MAX_CHANGE_PERCENT")
	}
}

func TestMinBaseFeeGovernorPause(t *testing.T) {
	sim, auth, _ := setupMinBaseFeeTestBackend(t)
	defer sim.Close()

	initialMinBaseFee := big.NewInt(12600000000000)
	activationBlock := big.NewInt(0)

	_, _, contract, err := minbasefee.DeployMinBaseFeeGovernor(
		auth,
		sim,
		auth.From,
		initialMinBaseFee,
		activationBlock,
	)
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}
	sim.Commit()

	tx, err := contract.Pause(auth)
	if err != nil {
		t.Fatalf("Failed to pause: %v", err)
	}
	sim.Commit()

	receipt, err := sim.TransactionReceipt(nil, tx.Hash())
	if err != nil {
		t.Fatalf("Failed to get receipt: %v", err)
	}

	if receipt.Status != 1 {
		t.Fatalf("Pause transaction failed")
	}

	paused, err := contract.Paused(nil)
	if err != nil {
		t.Fatalf("Failed to get paused status: %v", err)
	}

	if !paused {
		t.Errorf("Contract should be paused")
	}

	currentMinBaseFee, err := contract.GetCurrentMinBaseFee(nil)
	if err != nil {
		t.Fatalf("Failed to get current min base fee: %v", err)
	}

	fallbackMinBaseFee, err := contract.FallbackMinBaseFee(nil)
	if err != nil {
		t.Fatalf("Failed to get fallback min base fee: %v", err)
	}

	if currentMinBaseFee.Cmp(fallbackMinBaseFee) != 0 {
		t.Errorf("When paused, should return fallback min base fee")
	}

	newActivationBlock := new(big.Int).Add(sim.Blockchain().CurrentBlock().Number, big.NewInt(20000))
	_, err = contract.ProposeMinBaseFee(auth, big.NewInt(15000000000000), newActivationBlock)
	if err == nil {
		t.Errorf("Should not be able to propose when paused")
	}

	tx, err = contract.Unpause(auth)
	if err != nil {
		t.Fatalf("Failed to unpause: %v", err)
	}
	sim.Commit()

	paused, err = contract.Paused(nil)
	if err != nil {
		t.Fatalf("Failed to get paused status: %v", err)
	}

	if paused {
		t.Errorf("Contract should not be paused after unpause")
	}

	currentMinBaseFee, err = contract.GetCurrentMinBaseFee(nil)
	if err != nil {
		t.Fatalf("Failed to get current min base fee: %v", err)
	}

	if currentMinBaseFee.Cmp(initialMinBaseFee) != 0 {
		t.Errorf("After unpause, should return normal min base fee")
	}
}

func TestMinBaseFeeGovernorCancelProposal(t *testing.T) {
	sim, auth, _ := setupMinBaseFeeTestBackend(t)
	defer sim.Close()

	initialMinBaseFee := big.NewInt(12600000000000)
	activationBlock := big.NewInt(0)

	_, _, contract, err := minbasefee.DeployMinBaseFeeGovernor(
		auth,
		sim,
		auth.From,
		initialMinBaseFee,
		activationBlock,
	)
	if err != nil {
		t.Fatalf("Failed to deploy contract: %v", err)
	}
	sim.Commit()

	newMinBaseFee := big.NewInt(15000000000000)
	newActivationBlock := new(big.Int).Add(sim.Blockchain().CurrentBlock().Number, big.NewInt(20000))

	tx, err := contract.ProposeMinBaseFee(auth, newMinBaseFee, newActivationBlock)
	if err != nil {
		t.Fatalf("Failed to propose: %v", err)
	}
	sim.Commit()

	receipt, err := sim.TransactionReceipt(nil, tx.Hash())
	if err != nil {
		t.Fatalf("Failed to get receipt: %v", err)
	}

	var proposalID [32]byte
	for _, log := range receipt.Logs {
		if len(log.Topics) > 0 {
			proposalID = log.Topics[1]
			break
		}
	}

	tx, err = contract.CancelProposal(auth, proposalID)
	if err != nil {
		t.Fatalf("Failed to cancel proposal: %v", err)
	}
	sim.Commit()

	receipt, err = sim.TransactionReceipt(nil, tx.Hash())
	if err != nil {
		t.Fatalf("Failed to get receipt: %v", err)
	}

	if receipt.Status != 1 {
		t.Fatalf("Cancel transaction failed")
	}

	sim.AdjustTime(48 * time.Hour)
	sim.Commit()

	_, err = contract.ExecuteProposal(auth, proposalID)
	if err == nil {
		t.Errorf("Should not be able to execute cancelled proposal")
	}
}
