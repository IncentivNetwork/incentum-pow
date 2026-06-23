// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package minbasefee

import (
	"errors"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/event"
)

// Reference imports to suppress errors if they are not otherwise used.
var (
	_ = errors.New
	_ = big.NewInt
	_ = strings.NewReader
	_ = ethereum.NotFound
	_ = bind.Bind
	_ = common.Big1
	_ = types.BloomLookup
	_ = event.NewSubscription
	_ = abi.ConvertType
)

// MinBaseFeeGovernorMinBaseFeeConfig is an auto generated low-level Go binding around an user-defined struct.
type MinBaseFeeGovernorMinBaseFeeConfig struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	Timestamp       *big.Int
}

// MinBaseFeeGovernorMetaData contains all meta data concerning the MinBaseFeeGovernor contract.
var MinBaseFeeGovernorMetaData = &bind.MetaData{
	ABI: "[{\"type\":\"constructor\",\"inputs\":[{\"name\":\"_governance\",\"type\":\"address\",\"internalType\":\"address\"},{\"name\":\"_initialMinBaseFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"_activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"_minTimelockDelay\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"_minActivationDelayBlocks\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"MAX_CHANGE_PERCENT\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MAX_MIN_BASE_FEE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MIN_ACTIVATION_DELAY_BLOCKS\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MIN_MIN_BASE_FEE\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"MIN_TIMELOCK_DELAY\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"cancelProposal\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"configHistory\",\"inputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"timestamp\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"executeProposal\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"getAllConfigs\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"tuple[]\",\"internalType\":\"structMinBaseFeeGovernor.MinBaseFeeConfig[]\",\"components\":[{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"timestamp\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getConfigByIndex\",\"inputs\":[{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"tuple\",\"internalType\":\"structMinBaseFeeGovernor.MinBaseFeeConfig\",\"components\":[{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"timestamp\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getConfigHistoryLength\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getCurrentMinBaseFee\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getMinBaseFeeForBlock\",\"inputs\":[{\"name\":\"blockNumber\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"getProposal\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"proposedAt\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"executed\",\"type\":\"bool\",\"internalType\":\"bool\"},{\"name\":\"executeAfter\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"canExecute\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"governance\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"address\",\"internalType\":\"address\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proposalNonce\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proposals\",\"inputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"outputs\":[{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"proposedAt\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"executed\",\"type\":\"bool\",\"internalType\":\"bool\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"proposeMinBaseFee\",\"inputs\":[{\"name\":\"_minBaseFee\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"_activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[{\"name\":\"\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"setTimelockDelay\",\"inputs\":[{\"name\":\"newDelay\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"function\",\"name\":\"timelockDelay\",\"inputs\":[],\"outputs\":[{\"name\":\"\",\"type\":\"uint256\",\"internalType\":\"uint256\"}],\"stateMutability\":\"view\"},{\"type\":\"function\",\"name\":\"transferGovernance\",\"inputs\":[{\"name\":\"newGovernance\",\"type\":\"address\",\"internalType\":\"address\"}],\"outputs\":[],\"stateMutability\":\"nonpayable\"},{\"type\":\"event\",\"name\":\"GovernanceTransferred\",\"inputs\":[{\"name\":\"previousGovernance\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"},{\"name\":\"newGovernance\",\"type\":\"address\",\"indexed\":true,\"internalType\":\"address\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"MinBaseFeeProposed\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"proposedAt\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"executeAfter\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"MinBaseFeeScheduled\",\"inputs\":[{\"name\":\"configIndex\",\"type\":\"uint256\",\"indexed\":true,\"internalType\":\"uint256\"},{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"timestamp\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProposalCancelled\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"ProposalExecuted\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"indexed\":true,\"internalType\":\"bytes32\"},{\"name\":\"minBaseFee\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"activationBlock\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"event\",\"name\":\"TimelockDelayUpdated\",\"inputs\":[{\"name\":\"oldDelay\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"},{\"name\":\"newDelay\",\"type\":\"uint256\",\"indexed\":false,\"internalType\":\"uint256\"}],\"anonymous\":false},{\"type\":\"error\",\"name\":\"ActivationBlockNotSequential\",\"inputs\":[{\"name\":\"activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"lastActivationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ActivationTooSoon\",\"inputs\":[{\"name\":\"activationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"minActivationBlock\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"ChangeTooLarge\",\"inputs\":[{\"name\":\"newValue\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"currentValue\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"maxAllowed\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"GovernanceZero\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"InvalidConfigIndex\",\"inputs\":[{\"name\":\"index\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"length\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"InvalidTimelockDelay\",\"inputs\":[{\"name\":\"delay\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"MinBaseFeeOutOfBounds\",\"inputs\":[{\"name\":\"value\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"min\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"max\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]},{\"type\":\"error\",\"name\":\"MinBaseFeeZero\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"OnlyGovernance\",\"inputs\":[]},{\"type\":\"error\",\"name\":\"ProposalAlreadyExecuted\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"type\":\"error\",\"name\":\"ProposalNotFound\",\"inputs\":[{\"name\":\"proposalId\",\"type\":\"bytes32\",\"internalType\":\"bytes32\"}]},{\"type\":\"error\",\"name\":\"TimelockNotExpired\",\"inputs\":[{\"name\":\"currentTime\",\"type\":\"uint256\",\"internalType\":\"uint256\"},{\"name\":\"executeAfter\",\"type\":\"uint256\",\"internalType\":\"uint256\"}]}]",
	Bin: "0x60c060405234801561001057600080fd5b506040516112a23803806112a283398101604081905261002f91610225565b6001600160a01b03851661005657604051636e7240cb60e01b815260040160405180910390fd5b8360000361007757604051637d3ec89760e01b815260040160405180910390fd5b633b9aca00841080610091575068056bc75e2d6310000084115b156100cf576040516373071e1360e11b815260048101859052633b9aca00602482015268056bc75e2d63100000604482015260640160405180910390fd5b600080546001600160a01b0387166001600160a01b03199091161781556080839052600383815560a08390526040805160608101825287815260208101878152428284018181526001805480820182559088529351939095027fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf681019390935590517fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf783015592517fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf890910155517f9a59173c869dec4540284263b57c9bcb9c8bdd515e38c5c5e7d2dfeabcabf03e916101dd91889188919283526020830191909152604082015260600190565b60405180910390a26040516001600160a01b038616906000907f5f56bee8cffbe9a78652a74a60705edede02af10b0bbb888ca44b79a0d42ce80908290a3505050505061027a565b600080600080600060a0868803121561023d57600080fd5b85516001600160a01b038116811461025457600080fd5b602087015160408801516060890151608090990151929a91995097965090945092505050565b60805160a051610ff56102ad6000396000818161029301526105be01526000818161015c0152610cea0152610ff56000f3fe608060405234801561001057600080fd5b50600436106101375760003560e01c8063932cde11116100b8578063af267f8f1161007c578063af267f8f1461030e578063cc3c74a114610321578063d38bfff41461032a578063d6a9df491461033d578063e42c5d771461034d578063eef09bad1461037b57600080fd5b8063932cde11146102b5578063980ff6c6146102c857806398e83051146102db5780639973f8ca146102fb5780639bfe72ea1461030357600080fd5b806346841ccf116100ff57806346841ccf146102335780634d07d812146102465780635aa6e6751461024e578063635079561461027957806392636c451461028e57600080fd5b8063130e8bd81461013c578063169070eb1461015757806332ed5b121461017e57806337376ca8146101da578063430694cf146101ef575b600080fd5b610144610384565b6040519081526020015b60405180910390f35b6101447f000000000000000000000000000000000000000000000000000000000000000081565b6101b861018c366004610e46565b600260208190526000918252604090912080546001820154928201546003909201549092919060ff1684565b604080519485526020850193909352918301521515606082015260800161014e565b6101ed6101e8366004610e46565b610394565b005b6102026101fd366004610e46565b610478565b60408051968752602087019590955293850192909252151560608401526080830152151560a082015260c00161014e565b610144610241366004610e5f565b610516565b600154610144565b600054610261906001600160a01b031681565b6040516001600160a01b03909116815260200161014e565b610281610844565b60405161014e9190610e81565b6101447f000000000000000000000000000000000000000000000000000000000000000081565b6101446102c3366004610e46565b6108c1565b6101ed6102d6366004610e46565b61098e565b6102ee6102e9366004610e46565b610c18565b60405161014e9190610ee3565b61014460c881565b610144633b9aca0081565b6101ed61031c366004610e46565b610cbd565b61014460045481565b6101ed610338366004610f04565b610d71565b61014468056bc75e2d6310000081565b61036061035b366004610e46565b610e13565b6040805193845260208401929092529082015260600161014e565b61014460035481565b600061038f436108c1565b905090565b6000546001600160a01b031633146103bf576040516354348f0360e01b815260040160405180910390fd5b600081815260026020526040812080549091036103f75760405163e2acee5b60e01b8152600481018390526024015b60405180910390fd5b600381015460ff16156104205760405163134c704360e11b8152600481018390526024016103ee565b6000828152600260208190526040808320838155600181018490559182018390556003909101805460ff191690555183917f7bf38ee614622e33a85c9c8b9f0dca88e471d781640cddbb75c90e2bdeb6e12591a25050565b60008181526002602081815260408084208151608081018352815481526001820154938101939093529283015490820181905260039283015460ff1615156060830152915483928392839283928392916104d191610f4a565b925080606001511580156104e55750805115155b80156104f15750824210155b815160208301516040840151606090940151919b909a50929850965092945091925050565b600080546001600160a01b03163314610542576040516354348f0360e01b815260040160405180910390fd5b8260000361056357604051637d3ec89760e01b815260040160405180910390fd5b633b9aca0083108061057d575068056bc75e2d6310000083115b156105b7576040516373071e1360e11b815260048101849052633b9aca00602482015268056bc75e2d6310000060448201526064016103ee565b60006105e37f000000000000000000000000000000000000000000000000000000000000000043610f4a565b9050808310156106105760405163151145e760e31b815260048101849052602481018290526044016103ee565b6001805460009190610623908290610f5d565b8154811061063357610633610f70565b90600052602060002090600302016001015490508084116106715760405163edf58b1960e01b815260048101859052602481018290526044016103ee565b600061067c436108c1565b90506000606461068d60c882610f4a565b6106979084610f86565b6106a19190610f9d565b905060006106b160c86064610f4a565b60016106bf60c86064610f4a565b6106ca866064610f86565b6106d49190610f4a565b6106de9190610f5d565b6106e89190610f9d565b9050818811806106f757508088105b1561072657604051633818838160e11b81526004810189905260248101849052604481018390526064016103ee565b60045460408051602081018b9052908101899052426060820152608081019190915260009060a00160408051601f1981840301815291905280516020909101206004805460010190556003549091506000906107829042610f4a565b604080516080810182528c815260208082018d8152428385018181526000606086018181528a82526002958690529087902095518655925160018601555192840192909255516003909201805492151560ff1990931692909217909155905191925083917f81949bf253695b6b74b0e778afee4e722f995caea0f17bad817b528ff624100d9161082d918e918e91879093845260208401929092526040830152606082015260800190565b60405180910390a250955050505050505b92915050565b60606001805480602002602001604051908101604052809291908181526020016000905b828210156108b85783829060005260206000209060030201604051806060016040529081600082015481526020016001820154815260200160028201548152505081526020019060010190610868565b50505050905090565b60018054600091829081906108d69084610f5d565b905060005b81831161095e57600060026108f08486610f4a565b6108fa9190610f9d565b9050866001828154811061091057610910610f70565b9060005260206000209060030201600101541161093c57905080610935816001610f4a565b9350610958565b8060000361094a575061095e565b610955600182610f5d565b92505b506108db565b6001818154811061097157610971610f70565b906000526020600020906003020160000154945050505050919050565b6000546001600160a01b031633146109b9576040516354348f0360e01b815260040160405180910390fd5b600081815260026020526040812080549091036109ec5760405163e2acee5b60e01b8152600481018390526024016103ee565b600381015460ff1615610a155760405163134c704360e11b8152600481018390526024016103ee565b60006003548260020154610a299190610f4a565b905080421015610a555760405163126b74dd60e11b8152426004820152602481018290526044016103ee565b6001805460009190610a68908290610f5d565b81548110610a7857610a78610f70565b906000526020600020906003020160010154905080836001015411610ac057600183015460405163edf58b1960e01b81526004810191909152602481018290526044016103ee565b6040805160608101825284548152600185810180546020840190815242848601908152835480850185556000859052945160039586027fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf681019190915591517fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf7830155517fb10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf890910155918601805460ff1916909117905584549054915186927f2041749a72ed295219ba2be2beac04d48265c5ad481e501a2d6e61131eaffcbb92610bb692909190918252602082015260400190565b60405180910390a260018054610bcc9190610f5d565b835460018501546040805192835260208301919091524282820152517f9a59173c869dec4540284263b57c9bcb9c8bdd515e38c5c5e7d2dfeabcabf03e9181900360600190a250505050565b610c3c60405180606001604052806000815260200160008152602001600081525090565b6001548210610c6c57600154604051635c92342960e11b81526103ee918491600401918252602082015260400190565b60018281548110610c7f57610c7f610f70565b906000526020600020906003020160405180606001604052908160008201548152602001600182015481526020016002820154815250509050919050565b6000546001600160a01b03163314610ce8576040516354348f0360e01b815260040160405180910390fd5b7f0000000000000000000000000000000000000000000000000000000000000000811015610d2c576040516373586edb60e11b8152600481018290526024016103ee565b600380549082905560408051828152602081018490527f0d64018104bbece148774374900c9844b1c388724c82f170dbe557313b34daef910160405180910390a15050565b6000546001600160a01b03163314610d9c576040516354348f0360e01b815260040160405180910390fd5b6001600160a01b038116610dc357604051636e7240cb60e01b815260040160405180910390fd5b600080546001600160a01b038381166001600160a01b0319831681178455604051919092169283917f5f56bee8cffbe9a78652a74a60705edede02af10b0bbb888ca44b79a0d42ce809190a35050565b60018181548110610e2357600080fd5b600091825260209091206003909102018054600182015460029092015490925083565b600060208284031215610e5857600080fd5b5035919050565b60008060408385031215610e7257600080fd5b50508035926020909101359150565b6020808252825182820181905260009190848201906040850190845b81811015610ed757610ec48385518051825260208082015190830152604090810151910152565b9284019260609290920191600101610e9d565b50909695505050505050565b8151815260208083015190820152604080830151908201526060810161083e565b600060208284031215610f1657600080fd5b81356001600160a01b0381168114610f2d57600080fd5b9392505050565b634e487b7160e01b600052601160045260246000fd5b8082018082111561083e5761083e610f34565b8181038181111561083e5761083e610f34565b634e487b7160e01b600052603260045260246000fd5b808202811582820484141761083e5761083e610f34565b600082610fba57634e487b7160e01b600052601260045260246000fd5b50049056fea26469706673582212205ff9e966a2108ba1f57f889d042478cda551d1048f53a618edfc12231324071064736f6c63430008170033",
}

// MinBaseFeeGovernorABI is the input ABI used to generate the binding from.
// Deprecated: Use MinBaseFeeGovernorMetaData.ABI instead.
var MinBaseFeeGovernorABI = MinBaseFeeGovernorMetaData.ABI

// MinBaseFeeGovernorBin is the compiled bytecode used for deploying new contracts.
// Deprecated: Use MinBaseFeeGovernorMetaData.Bin instead.
var MinBaseFeeGovernorBin = MinBaseFeeGovernorMetaData.Bin

// DeployMinBaseFeeGovernor deploys a new Ethereum contract, binding an instance of MinBaseFeeGovernor to it.
func DeployMinBaseFeeGovernor(auth *bind.TransactOpts, backend bind.ContractBackend, _governance common.Address, _initialMinBaseFee *big.Int, _activationBlock *big.Int, _minTimelockDelay *big.Int, _minActivationDelayBlocks *big.Int) (common.Address, *types.Transaction, *MinBaseFeeGovernor, error) {
	parsed, err := MinBaseFeeGovernorMetaData.GetAbi()
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	if parsed == nil {
		return common.Address{}, nil, nil, errors.New("GetABI returned nil")
	}

	address, tx, contract, err := bind.DeployContract(auth, *parsed, common.FromHex(MinBaseFeeGovernorBin), backend, _governance, _initialMinBaseFee, _activationBlock, _minTimelockDelay, _minActivationDelayBlocks)
	if err != nil {
		return common.Address{}, nil, nil, err
	}
	return address, tx, &MinBaseFeeGovernor{MinBaseFeeGovernorCaller: MinBaseFeeGovernorCaller{contract: contract}, MinBaseFeeGovernorTransactor: MinBaseFeeGovernorTransactor{contract: contract}, MinBaseFeeGovernorFilterer: MinBaseFeeGovernorFilterer{contract: contract}}, nil
}

// MinBaseFeeGovernor is an auto generated Go binding around an Ethereum contract.
type MinBaseFeeGovernor struct {
	MinBaseFeeGovernorCaller     // Read-only binding to the contract
	MinBaseFeeGovernorTransactor // Write-only binding to the contract
	MinBaseFeeGovernorFilterer   // Log filterer for contract events
}

// MinBaseFeeGovernorCaller is an auto generated read-only Go binding around an Ethereum contract.
type MinBaseFeeGovernorCaller struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MinBaseFeeGovernorTransactor is an auto generated write-only Go binding around an Ethereum contract.
type MinBaseFeeGovernorTransactor struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MinBaseFeeGovernorFilterer is an auto generated log filtering Go binding around an Ethereum contract events.
type MinBaseFeeGovernorFilterer struct {
	contract *bind.BoundContract // Generic contract wrapper for the low level calls
}

// MinBaseFeeGovernorSession is an auto generated Go binding around an Ethereum contract,
// with pre-set call and transact options.
type MinBaseFeeGovernorSession struct {
	Contract     *MinBaseFeeGovernor // Generic contract binding to set the session for
	CallOpts     bind.CallOpts       // Call options to use throughout this session
	TransactOpts bind.TransactOpts   // Transaction auth options to use throughout this session
}

// MinBaseFeeGovernorCallerSession is an auto generated read-only Go binding around an Ethereum contract,
// with pre-set call options.
type MinBaseFeeGovernorCallerSession struct {
	Contract *MinBaseFeeGovernorCaller // Generic contract caller binding to set the session for
	CallOpts bind.CallOpts             // Call options to use throughout this session
}

// MinBaseFeeGovernorTransactorSession is an auto generated write-only Go binding around an Ethereum contract,
// with pre-set transact options.
type MinBaseFeeGovernorTransactorSession struct {
	Contract     *MinBaseFeeGovernorTransactor // Generic contract transactor binding to set the session for
	TransactOpts bind.TransactOpts             // Transaction auth options to use throughout this session
}

// MinBaseFeeGovernorRaw is an auto generated low-level Go binding around an Ethereum contract.
type MinBaseFeeGovernorRaw struct {
	Contract *MinBaseFeeGovernor // Generic contract binding to access the raw methods on
}

// MinBaseFeeGovernorCallerRaw is an auto generated low-level read-only Go binding around an Ethereum contract.
type MinBaseFeeGovernorCallerRaw struct {
	Contract *MinBaseFeeGovernorCaller // Generic read-only contract binding to access the raw methods on
}

// MinBaseFeeGovernorTransactorRaw is an auto generated low-level write-only Go binding around an Ethereum contract.
type MinBaseFeeGovernorTransactorRaw struct {
	Contract *MinBaseFeeGovernorTransactor // Generic write-only contract binding to access the raw methods on
}

// NewMinBaseFeeGovernor creates a new instance of MinBaseFeeGovernor, bound to a specific deployed contract.
func NewMinBaseFeeGovernor(address common.Address, backend bind.ContractBackend) (*MinBaseFeeGovernor, error) {
	contract, err := bindMinBaseFeeGovernor(address, backend, backend, backend)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernor{MinBaseFeeGovernorCaller: MinBaseFeeGovernorCaller{contract: contract}, MinBaseFeeGovernorTransactor: MinBaseFeeGovernorTransactor{contract: contract}, MinBaseFeeGovernorFilterer: MinBaseFeeGovernorFilterer{contract: contract}}, nil
}

// NewMinBaseFeeGovernorCaller creates a new read-only instance of MinBaseFeeGovernor, bound to a specific deployed contract.
func NewMinBaseFeeGovernorCaller(address common.Address, caller bind.ContractCaller) (*MinBaseFeeGovernorCaller, error) {
	contract, err := bindMinBaseFeeGovernor(address, caller, nil, nil)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorCaller{contract: contract}, nil
}

// NewMinBaseFeeGovernorTransactor creates a new write-only instance of MinBaseFeeGovernor, bound to a specific deployed contract.
func NewMinBaseFeeGovernorTransactor(address common.Address, transactor bind.ContractTransactor) (*MinBaseFeeGovernorTransactor, error) {
	contract, err := bindMinBaseFeeGovernor(address, nil, transactor, nil)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorTransactor{contract: contract}, nil
}

// NewMinBaseFeeGovernorFilterer creates a new log filterer instance of MinBaseFeeGovernor, bound to a specific deployed contract.
func NewMinBaseFeeGovernorFilterer(address common.Address, filterer bind.ContractFilterer) (*MinBaseFeeGovernorFilterer, error) {
	contract, err := bindMinBaseFeeGovernor(address, nil, nil, filterer)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorFilterer{contract: contract}, nil
}

// bindMinBaseFeeGovernor binds a generic wrapper to an already deployed contract.
func bindMinBaseFeeGovernor(address common.Address, caller bind.ContractCaller, transactor bind.ContractTransactor, filterer bind.ContractFilterer) (*bind.BoundContract, error) {
	parsed, err := MinBaseFeeGovernorMetaData.GetAbi()
	if err != nil {
		return nil, err
	}
	return bind.NewBoundContract(address, *parsed, caller, transactor, filterer), nil
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_MinBaseFeeGovernor *MinBaseFeeGovernorRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _MinBaseFeeGovernor.Contract.MinBaseFeeGovernorCaller.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_MinBaseFeeGovernor *MinBaseFeeGovernorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.MinBaseFeeGovernorTransactor.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_MinBaseFeeGovernor *MinBaseFeeGovernorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.MinBaseFeeGovernorTransactor.contract.Transact(opts, method, params...)
}

// Call invokes the (constant) contract method with params as input values and
// sets the output to result. The result type might be a single field for simple
// returns, a slice of interfaces for anonymous returns and a struct for named
// returns.
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerRaw) Call(opts *bind.CallOpts, result *[]interface{}, method string, params ...interface{}) error {
	return _MinBaseFeeGovernor.Contract.contract.Call(opts, result, method, params...)
}

// Transfer initiates a plain transaction to move funds to the contract, calling
// its default method if one is available.
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactorRaw) Transfer(opts *bind.TransactOpts) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.contract.Transfer(opts)
}

// Transact invokes the (paid) contract method with params as input values.
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactorRaw) Transact(opts *bind.TransactOpts, method string, params ...interface{}) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.contract.Transact(opts, method, params...)
}

// MAXCHANGEPERCENT is a free data retrieval call binding the contract method 0x9973f8ca.
//
// Solidity: function MAX_CHANGE_PERCENT() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) MAXCHANGEPERCENT(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "MAX_CHANGE_PERCENT")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXCHANGEPERCENT is a free data retrieval call binding the contract method 0x9973f8ca.
//
// Solidity: function MAX_CHANGE_PERCENT() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) MAXCHANGEPERCENT() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MAXCHANGEPERCENT(&_MinBaseFeeGovernor.CallOpts)
}

// MAXCHANGEPERCENT is a free data retrieval call binding the contract method 0x9973f8ca.
//
// Solidity: function MAX_CHANGE_PERCENT() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) MAXCHANGEPERCENT() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MAXCHANGEPERCENT(&_MinBaseFeeGovernor.CallOpts)
}

// MAXMINBASEFEE is a free data retrieval call binding the contract method 0xd6a9df49.
//
// Solidity: function MAX_MIN_BASE_FEE() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) MAXMINBASEFEE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "MAX_MIN_BASE_FEE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MAXMINBASEFEE is a free data retrieval call binding the contract method 0xd6a9df49.
//
// Solidity: function MAX_MIN_BASE_FEE() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) MAXMINBASEFEE() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MAXMINBASEFEE(&_MinBaseFeeGovernor.CallOpts)
}

// MAXMINBASEFEE is a free data retrieval call binding the contract method 0xd6a9df49.
//
// Solidity: function MAX_MIN_BASE_FEE() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) MAXMINBASEFEE() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MAXMINBASEFEE(&_MinBaseFeeGovernor.CallOpts)
}

// MINACTIVATIONDELAYBLOCKS is a free data retrieval call binding the contract method 0x92636c45.
//
// Solidity: function MIN_ACTIVATION_DELAY_BLOCKS() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) MINACTIVATIONDELAYBLOCKS(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "MIN_ACTIVATION_DELAY_BLOCKS")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MINACTIVATIONDELAYBLOCKS is a free data retrieval call binding the contract method 0x92636c45.
//
// Solidity: function MIN_ACTIVATION_DELAY_BLOCKS() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) MINACTIVATIONDELAYBLOCKS() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MINACTIVATIONDELAYBLOCKS(&_MinBaseFeeGovernor.CallOpts)
}

// MINACTIVATIONDELAYBLOCKS is a free data retrieval call binding the contract method 0x92636c45.
//
// Solidity: function MIN_ACTIVATION_DELAY_BLOCKS() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) MINACTIVATIONDELAYBLOCKS() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MINACTIVATIONDELAYBLOCKS(&_MinBaseFeeGovernor.CallOpts)
}

// MINMINBASEFEE is a free data retrieval call binding the contract method 0x9bfe72ea.
//
// Solidity: function MIN_MIN_BASE_FEE() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) MINMINBASEFEE(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "MIN_MIN_BASE_FEE")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MINMINBASEFEE is a free data retrieval call binding the contract method 0x9bfe72ea.
//
// Solidity: function MIN_MIN_BASE_FEE() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) MINMINBASEFEE() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MINMINBASEFEE(&_MinBaseFeeGovernor.CallOpts)
}

// MINMINBASEFEE is a free data retrieval call binding the contract method 0x9bfe72ea.
//
// Solidity: function MIN_MIN_BASE_FEE() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) MINMINBASEFEE() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MINMINBASEFEE(&_MinBaseFeeGovernor.CallOpts)
}

// MINTIMELOCKDELAY is a free data retrieval call binding the contract method 0x169070eb.
//
// Solidity: function MIN_TIMELOCK_DELAY() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) MINTIMELOCKDELAY(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "MIN_TIMELOCK_DELAY")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// MINTIMELOCKDELAY is a free data retrieval call binding the contract method 0x169070eb.
//
// Solidity: function MIN_TIMELOCK_DELAY() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) MINTIMELOCKDELAY() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MINTIMELOCKDELAY(&_MinBaseFeeGovernor.CallOpts)
}

// MINTIMELOCKDELAY is a free data retrieval call binding the contract method 0x169070eb.
//
// Solidity: function MIN_TIMELOCK_DELAY() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) MINTIMELOCKDELAY() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.MINTIMELOCKDELAY(&_MinBaseFeeGovernor.CallOpts)
}

// ConfigHistory is a free data retrieval call binding the contract method 0xe42c5d77.
//
// Solidity: function configHistory(uint256 ) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 timestamp)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) ConfigHistory(opts *bind.CallOpts, arg0 *big.Int) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	Timestamp       *big.Int
}, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "configHistory", arg0)

	outstruct := new(struct {
		MinBaseFee      *big.Int
		ActivationBlock *big.Int
		Timestamp       *big.Int
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.MinBaseFee = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.ActivationBlock = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.Timestamp = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)

	return *outstruct, err

}

// ConfigHistory is a free data retrieval call binding the contract method 0xe42c5d77.
//
// Solidity: function configHistory(uint256 ) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 timestamp)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) ConfigHistory(arg0 *big.Int) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	Timestamp       *big.Int
}, error) {
	return _MinBaseFeeGovernor.Contract.ConfigHistory(&_MinBaseFeeGovernor.CallOpts, arg0)
}

// ConfigHistory is a free data retrieval call binding the contract method 0xe42c5d77.
//
// Solidity: function configHistory(uint256 ) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 timestamp)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) ConfigHistory(arg0 *big.Int) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	Timestamp       *big.Int
}, error) {
	return _MinBaseFeeGovernor.Contract.ConfigHistory(&_MinBaseFeeGovernor.CallOpts, arg0)
}

// GetAllConfigs is a free data retrieval call binding the contract method 0x63507956.
//
// Solidity: function getAllConfigs() view returns((uint256,uint256,uint256)[])
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) GetAllConfigs(opts *bind.CallOpts) ([]MinBaseFeeGovernorMinBaseFeeConfig, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "getAllConfigs")

	if err != nil {
		return *new([]MinBaseFeeGovernorMinBaseFeeConfig), err
	}

	out0 := *abi.ConvertType(out[0], new([]MinBaseFeeGovernorMinBaseFeeConfig)).(*[]MinBaseFeeGovernorMinBaseFeeConfig)

	return out0, err

}

// GetAllConfigs is a free data retrieval call binding the contract method 0x63507956.
//
// Solidity: function getAllConfigs() view returns((uint256,uint256,uint256)[])
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) GetAllConfigs() ([]MinBaseFeeGovernorMinBaseFeeConfig, error) {
	return _MinBaseFeeGovernor.Contract.GetAllConfigs(&_MinBaseFeeGovernor.CallOpts)
}

// GetAllConfigs is a free data retrieval call binding the contract method 0x63507956.
//
// Solidity: function getAllConfigs() view returns((uint256,uint256,uint256)[])
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) GetAllConfigs() ([]MinBaseFeeGovernorMinBaseFeeConfig, error) {
	return _MinBaseFeeGovernor.Contract.GetAllConfigs(&_MinBaseFeeGovernor.CallOpts)
}

// GetConfigByIndex is a free data retrieval call binding the contract method 0x98e83051.
//
// Solidity: function getConfigByIndex(uint256 index) view returns((uint256,uint256,uint256))
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) GetConfigByIndex(opts *bind.CallOpts, index *big.Int) (MinBaseFeeGovernorMinBaseFeeConfig, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "getConfigByIndex", index)

	if err != nil {
		return *new(MinBaseFeeGovernorMinBaseFeeConfig), err
	}

	out0 := *abi.ConvertType(out[0], new(MinBaseFeeGovernorMinBaseFeeConfig)).(*MinBaseFeeGovernorMinBaseFeeConfig)

	return out0, err

}

// GetConfigByIndex is a free data retrieval call binding the contract method 0x98e83051.
//
// Solidity: function getConfigByIndex(uint256 index) view returns((uint256,uint256,uint256))
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) GetConfigByIndex(index *big.Int) (MinBaseFeeGovernorMinBaseFeeConfig, error) {
	return _MinBaseFeeGovernor.Contract.GetConfigByIndex(&_MinBaseFeeGovernor.CallOpts, index)
}

// GetConfigByIndex is a free data retrieval call binding the contract method 0x98e83051.
//
// Solidity: function getConfigByIndex(uint256 index) view returns((uint256,uint256,uint256))
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) GetConfigByIndex(index *big.Int) (MinBaseFeeGovernorMinBaseFeeConfig, error) {
	return _MinBaseFeeGovernor.Contract.GetConfigByIndex(&_MinBaseFeeGovernor.CallOpts, index)
}

// GetConfigHistoryLength is a free data retrieval call binding the contract method 0x4d07d812.
//
// Solidity: function getConfigHistoryLength() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) GetConfigHistoryLength(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "getConfigHistoryLength")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetConfigHistoryLength is a free data retrieval call binding the contract method 0x4d07d812.
//
// Solidity: function getConfigHistoryLength() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) GetConfigHistoryLength() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.GetConfigHistoryLength(&_MinBaseFeeGovernor.CallOpts)
}

// GetConfigHistoryLength is a free data retrieval call binding the contract method 0x4d07d812.
//
// Solidity: function getConfigHistoryLength() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) GetConfigHistoryLength() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.GetConfigHistoryLength(&_MinBaseFeeGovernor.CallOpts)
}

// GetCurrentMinBaseFee is a free data retrieval call binding the contract method 0x130e8bd8.
//
// Solidity: function getCurrentMinBaseFee() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) GetCurrentMinBaseFee(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "getCurrentMinBaseFee")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetCurrentMinBaseFee is a free data retrieval call binding the contract method 0x130e8bd8.
//
// Solidity: function getCurrentMinBaseFee() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) GetCurrentMinBaseFee() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.GetCurrentMinBaseFee(&_MinBaseFeeGovernor.CallOpts)
}

// GetCurrentMinBaseFee is a free data retrieval call binding the contract method 0x130e8bd8.
//
// Solidity: function getCurrentMinBaseFee() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) GetCurrentMinBaseFee() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.GetCurrentMinBaseFee(&_MinBaseFeeGovernor.CallOpts)
}

// GetMinBaseFeeForBlock is a free data retrieval call binding the contract method 0x932cde11.
//
// Solidity: function getMinBaseFeeForBlock(uint256 blockNumber) view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) GetMinBaseFeeForBlock(opts *bind.CallOpts, blockNumber *big.Int) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "getMinBaseFeeForBlock", blockNumber)

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// GetMinBaseFeeForBlock is a free data retrieval call binding the contract method 0x932cde11.
//
// Solidity: function getMinBaseFeeForBlock(uint256 blockNumber) view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) GetMinBaseFeeForBlock(blockNumber *big.Int) (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.GetMinBaseFeeForBlock(&_MinBaseFeeGovernor.CallOpts, blockNumber)
}

// GetMinBaseFeeForBlock is a free data retrieval call binding the contract method 0x932cde11.
//
// Solidity: function getMinBaseFeeForBlock(uint256 blockNumber) view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) GetMinBaseFeeForBlock(blockNumber *big.Int) (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.GetMinBaseFeeForBlock(&_MinBaseFeeGovernor.CallOpts, blockNumber)
}

// GetProposal is a free data retrieval call binding the contract method 0x430694cf.
//
// Solidity: function getProposal(bytes32 proposalId) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, bool executed, uint256 executeAfter, bool canExecute)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) GetProposal(opts *bind.CallOpts, proposalId [32]byte) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	ProposedAt      *big.Int
	Executed        bool
	ExecuteAfter    *big.Int
	CanExecute      bool
}, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "getProposal", proposalId)

	outstruct := new(struct {
		MinBaseFee      *big.Int
		ActivationBlock *big.Int
		ProposedAt      *big.Int
		Executed        bool
		ExecuteAfter    *big.Int
		CanExecute      bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.MinBaseFee = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.ActivationBlock = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.ProposedAt = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.Executed = *abi.ConvertType(out[3], new(bool)).(*bool)
	outstruct.ExecuteAfter = *abi.ConvertType(out[4], new(*big.Int)).(**big.Int)
	outstruct.CanExecute = *abi.ConvertType(out[5], new(bool)).(*bool)

	return *outstruct, err

}

// GetProposal is a free data retrieval call binding the contract method 0x430694cf.
//
// Solidity: function getProposal(bytes32 proposalId) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, bool executed, uint256 executeAfter, bool canExecute)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) GetProposal(proposalId [32]byte) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	ProposedAt      *big.Int
	Executed        bool
	ExecuteAfter    *big.Int
	CanExecute      bool
}, error) {
	return _MinBaseFeeGovernor.Contract.GetProposal(&_MinBaseFeeGovernor.CallOpts, proposalId)
}

// GetProposal is a free data retrieval call binding the contract method 0x430694cf.
//
// Solidity: function getProposal(bytes32 proposalId) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, bool executed, uint256 executeAfter, bool canExecute)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) GetProposal(proposalId [32]byte) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	ProposedAt      *big.Int
	Executed        bool
	ExecuteAfter    *big.Int
	CanExecute      bool
}, error) {
	return _MinBaseFeeGovernor.Contract.GetProposal(&_MinBaseFeeGovernor.CallOpts, proposalId)
}

// Governance is a free data retrieval call binding the contract method 0x5aa6e675.
//
// Solidity: function governance() view returns(address)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) Governance(opts *bind.CallOpts) (common.Address, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "governance")

	if err != nil {
		return *new(common.Address), err
	}

	out0 := *abi.ConvertType(out[0], new(common.Address)).(*common.Address)

	return out0, err

}

// Governance is a free data retrieval call binding the contract method 0x5aa6e675.
//
// Solidity: function governance() view returns(address)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) Governance() (common.Address, error) {
	return _MinBaseFeeGovernor.Contract.Governance(&_MinBaseFeeGovernor.CallOpts)
}

// Governance is a free data retrieval call binding the contract method 0x5aa6e675.
//
// Solidity: function governance() view returns(address)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) Governance() (common.Address, error) {
	return _MinBaseFeeGovernor.Contract.Governance(&_MinBaseFeeGovernor.CallOpts)
}

// ProposalNonce is a free data retrieval call binding the contract method 0xcc3c74a1.
//
// Solidity: function proposalNonce() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) ProposalNonce(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "proposalNonce")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// ProposalNonce is a free data retrieval call binding the contract method 0xcc3c74a1.
//
// Solidity: function proposalNonce() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) ProposalNonce() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.ProposalNonce(&_MinBaseFeeGovernor.CallOpts)
}

// ProposalNonce is a free data retrieval call binding the contract method 0xcc3c74a1.
//
// Solidity: function proposalNonce() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) ProposalNonce() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.ProposalNonce(&_MinBaseFeeGovernor.CallOpts)
}

// Proposals is a free data retrieval call binding the contract method 0x32ed5b12.
//
// Solidity: function proposals(bytes32 ) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, bool executed)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) Proposals(opts *bind.CallOpts, arg0 [32]byte) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	ProposedAt      *big.Int
	Executed        bool
}, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "proposals", arg0)

	outstruct := new(struct {
		MinBaseFee      *big.Int
		ActivationBlock *big.Int
		ProposedAt      *big.Int
		Executed        bool
	})
	if err != nil {
		return *outstruct, err
	}

	outstruct.MinBaseFee = *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)
	outstruct.ActivationBlock = *abi.ConvertType(out[1], new(*big.Int)).(**big.Int)
	outstruct.ProposedAt = *abi.ConvertType(out[2], new(*big.Int)).(**big.Int)
	outstruct.Executed = *abi.ConvertType(out[3], new(bool)).(*bool)

	return *outstruct, err

}

// Proposals is a free data retrieval call binding the contract method 0x32ed5b12.
//
// Solidity: function proposals(bytes32 ) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, bool executed)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) Proposals(arg0 [32]byte) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	ProposedAt      *big.Int
	Executed        bool
}, error) {
	return _MinBaseFeeGovernor.Contract.Proposals(&_MinBaseFeeGovernor.CallOpts, arg0)
}

// Proposals is a free data retrieval call binding the contract method 0x32ed5b12.
//
// Solidity: function proposals(bytes32 ) view returns(uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, bool executed)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) Proposals(arg0 [32]byte) (struct {
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	ProposedAt      *big.Int
	Executed        bool
}, error) {
	return _MinBaseFeeGovernor.Contract.Proposals(&_MinBaseFeeGovernor.CallOpts, arg0)
}

// TimelockDelay is a free data retrieval call binding the contract method 0xeef09bad.
//
// Solidity: function timelockDelay() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCaller) TimelockDelay(opts *bind.CallOpts) (*big.Int, error) {
	var out []interface{}
	err := _MinBaseFeeGovernor.contract.Call(opts, &out, "timelockDelay")

	if err != nil {
		return *new(*big.Int), err
	}

	out0 := *abi.ConvertType(out[0], new(*big.Int)).(**big.Int)

	return out0, err

}

// TimelockDelay is a free data retrieval call binding the contract method 0xeef09bad.
//
// Solidity: function timelockDelay() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) TimelockDelay() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.TimelockDelay(&_MinBaseFeeGovernor.CallOpts)
}

// TimelockDelay is a free data retrieval call binding the contract method 0xeef09bad.
//
// Solidity: function timelockDelay() view returns(uint256)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorCallerSession) TimelockDelay() (*big.Int, error) {
	return _MinBaseFeeGovernor.Contract.TimelockDelay(&_MinBaseFeeGovernor.CallOpts)
}

// CancelProposal is a paid mutator transaction binding the contract method 0x37376ca8.
//
// Solidity: function cancelProposal(bytes32 proposalId) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactor) CancelProposal(opts *bind.TransactOpts, proposalId [32]byte) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.contract.Transact(opts, "cancelProposal", proposalId)
}

// CancelProposal is a paid mutator transaction binding the contract method 0x37376ca8.
//
// Solidity: function cancelProposal(bytes32 proposalId) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) CancelProposal(proposalId [32]byte) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.CancelProposal(&_MinBaseFeeGovernor.TransactOpts, proposalId)
}

// CancelProposal is a paid mutator transaction binding the contract method 0x37376ca8.
//
// Solidity: function cancelProposal(bytes32 proposalId) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactorSession) CancelProposal(proposalId [32]byte) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.CancelProposal(&_MinBaseFeeGovernor.TransactOpts, proposalId)
}

// ExecuteProposal is a paid mutator transaction binding the contract method 0x980ff6c6.
//
// Solidity: function executeProposal(bytes32 proposalId) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactor) ExecuteProposal(opts *bind.TransactOpts, proposalId [32]byte) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.contract.Transact(opts, "executeProposal", proposalId)
}

// ExecuteProposal is a paid mutator transaction binding the contract method 0x980ff6c6.
//
// Solidity: function executeProposal(bytes32 proposalId) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) ExecuteProposal(proposalId [32]byte) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.ExecuteProposal(&_MinBaseFeeGovernor.TransactOpts, proposalId)
}

// ExecuteProposal is a paid mutator transaction binding the contract method 0x980ff6c6.
//
// Solidity: function executeProposal(bytes32 proposalId) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactorSession) ExecuteProposal(proposalId [32]byte) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.ExecuteProposal(&_MinBaseFeeGovernor.TransactOpts, proposalId)
}

// ProposeMinBaseFee is a paid mutator transaction binding the contract method 0x46841ccf.
//
// Solidity: function proposeMinBaseFee(uint256 _minBaseFee, uint256 _activationBlock) returns(bytes32)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactor) ProposeMinBaseFee(opts *bind.TransactOpts, _minBaseFee *big.Int, _activationBlock *big.Int) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.contract.Transact(opts, "proposeMinBaseFee", _minBaseFee, _activationBlock)
}

// ProposeMinBaseFee is a paid mutator transaction binding the contract method 0x46841ccf.
//
// Solidity: function proposeMinBaseFee(uint256 _minBaseFee, uint256 _activationBlock) returns(bytes32)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) ProposeMinBaseFee(_minBaseFee *big.Int, _activationBlock *big.Int) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.ProposeMinBaseFee(&_MinBaseFeeGovernor.TransactOpts, _minBaseFee, _activationBlock)
}

// ProposeMinBaseFee is a paid mutator transaction binding the contract method 0x46841ccf.
//
// Solidity: function proposeMinBaseFee(uint256 _minBaseFee, uint256 _activationBlock) returns(bytes32)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactorSession) ProposeMinBaseFee(_minBaseFee *big.Int, _activationBlock *big.Int) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.ProposeMinBaseFee(&_MinBaseFeeGovernor.TransactOpts, _minBaseFee, _activationBlock)
}

// SetTimelockDelay is a paid mutator transaction binding the contract method 0xaf267f8f.
//
// Solidity: function setTimelockDelay(uint256 newDelay) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactor) SetTimelockDelay(opts *bind.TransactOpts, newDelay *big.Int) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.contract.Transact(opts, "setTimelockDelay", newDelay)
}

// SetTimelockDelay is a paid mutator transaction binding the contract method 0xaf267f8f.
//
// Solidity: function setTimelockDelay(uint256 newDelay) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) SetTimelockDelay(newDelay *big.Int) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.SetTimelockDelay(&_MinBaseFeeGovernor.TransactOpts, newDelay)
}

// SetTimelockDelay is a paid mutator transaction binding the contract method 0xaf267f8f.
//
// Solidity: function setTimelockDelay(uint256 newDelay) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactorSession) SetTimelockDelay(newDelay *big.Int) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.SetTimelockDelay(&_MinBaseFeeGovernor.TransactOpts, newDelay)
}

// TransferGovernance is a paid mutator transaction binding the contract method 0xd38bfff4.
//
// Solidity: function transferGovernance(address newGovernance) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactor) TransferGovernance(opts *bind.TransactOpts, newGovernance common.Address) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.contract.Transact(opts, "transferGovernance", newGovernance)
}

// TransferGovernance is a paid mutator transaction binding the contract method 0xd38bfff4.
//
// Solidity: function transferGovernance(address newGovernance) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorSession) TransferGovernance(newGovernance common.Address) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.TransferGovernance(&_MinBaseFeeGovernor.TransactOpts, newGovernance)
}

// TransferGovernance is a paid mutator transaction binding the contract method 0xd38bfff4.
//
// Solidity: function transferGovernance(address newGovernance) returns()
func (_MinBaseFeeGovernor *MinBaseFeeGovernorTransactorSession) TransferGovernance(newGovernance common.Address) (*types.Transaction, error) {
	return _MinBaseFeeGovernor.Contract.TransferGovernance(&_MinBaseFeeGovernor.TransactOpts, newGovernance)
}

// MinBaseFeeGovernorGovernanceTransferredIterator is returned from FilterGovernanceTransferred and is used to iterate over the raw logs and unpacked data for GovernanceTransferred events raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorGovernanceTransferredIterator struct {
	Event *MinBaseFeeGovernorGovernanceTransferred // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *MinBaseFeeGovernorGovernanceTransferredIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MinBaseFeeGovernorGovernanceTransferred)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(MinBaseFeeGovernorGovernanceTransferred)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *MinBaseFeeGovernorGovernanceTransferredIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MinBaseFeeGovernorGovernanceTransferredIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MinBaseFeeGovernorGovernanceTransferred represents a GovernanceTransferred event raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorGovernanceTransferred struct {
	PreviousGovernance common.Address
	NewGovernance      common.Address
	Raw                types.Log // Blockchain specific contextual infos
}

// FilterGovernanceTransferred is a free log retrieval operation binding the contract event 0x5f56bee8cffbe9a78652a74a60705edede02af10b0bbb888ca44b79a0d42ce80.
//
// Solidity: event GovernanceTransferred(address indexed previousGovernance, address indexed newGovernance)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) FilterGovernanceTransferred(opts *bind.FilterOpts, previousGovernance []common.Address, newGovernance []common.Address) (*MinBaseFeeGovernorGovernanceTransferredIterator, error) {

	var previousGovernanceRule []interface{}
	for _, previousGovernanceItem := range previousGovernance {
		previousGovernanceRule = append(previousGovernanceRule, previousGovernanceItem)
	}
	var newGovernanceRule []interface{}
	for _, newGovernanceItem := range newGovernance {
		newGovernanceRule = append(newGovernanceRule, newGovernanceItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.FilterLogs(opts, "GovernanceTransferred", previousGovernanceRule, newGovernanceRule)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorGovernanceTransferredIterator{contract: _MinBaseFeeGovernor.contract, event: "GovernanceTransferred", logs: logs, sub: sub}, nil
}

// WatchGovernanceTransferred is a free log subscription operation binding the contract event 0x5f56bee8cffbe9a78652a74a60705edede02af10b0bbb888ca44b79a0d42ce80.
//
// Solidity: event GovernanceTransferred(address indexed previousGovernance, address indexed newGovernance)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) WatchGovernanceTransferred(opts *bind.WatchOpts, sink chan<- *MinBaseFeeGovernorGovernanceTransferred, previousGovernance []common.Address, newGovernance []common.Address) (event.Subscription, error) {

	var previousGovernanceRule []interface{}
	for _, previousGovernanceItem := range previousGovernance {
		previousGovernanceRule = append(previousGovernanceRule, previousGovernanceItem)
	}
	var newGovernanceRule []interface{}
	for _, newGovernanceItem := range newGovernance {
		newGovernanceRule = append(newGovernanceRule, newGovernanceItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.WatchLogs(opts, "GovernanceTransferred", previousGovernanceRule, newGovernanceRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MinBaseFeeGovernorGovernanceTransferred)
				if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "GovernanceTransferred", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseGovernanceTransferred is a log parse operation binding the contract event 0x5f56bee8cffbe9a78652a74a60705edede02af10b0bbb888ca44b79a0d42ce80.
//
// Solidity: event GovernanceTransferred(address indexed previousGovernance, address indexed newGovernance)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) ParseGovernanceTransferred(log types.Log) (*MinBaseFeeGovernorGovernanceTransferred, error) {
	event := new(MinBaseFeeGovernorGovernanceTransferred)
	if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "GovernanceTransferred", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MinBaseFeeGovernorMinBaseFeeProposedIterator is returned from FilterMinBaseFeeProposed and is used to iterate over the raw logs and unpacked data for MinBaseFeeProposed events raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorMinBaseFeeProposedIterator struct {
	Event *MinBaseFeeGovernorMinBaseFeeProposed // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *MinBaseFeeGovernorMinBaseFeeProposedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MinBaseFeeGovernorMinBaseFeeProposed)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(MinBaseFeeGovernorMinBaseFeeProposed)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *MinBaseFeeGovernorMinBaseFeeProposedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MinBaseFeeGovernorMinBaseFeeProposedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MinBaseFeeGovernorMinBaseFeeProposed represents a MinBaseFeeProposed event raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorMinBaseFeeProposed struct {
	ProposalId      [32]byte
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	ProposedAt      *big.Int
	ExecuteAfter    *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterMinBaseFeeProposed is a free log retrieval operation binding the contract event 0x81949bf253695b6b74b0e778afee4e722f995caea0f17bad817b528ff624100d.
//
// Solidity: event MinBaseFeeProposed(bytes32 indexed proposalId, uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, uint256 executeAfter)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) FilterMinBaseFeeProposed(opts *bind.FilterOpts, proposalId [][32]byte) (*MinBaseFeeGovernorMinBaseFeeProposedIterator, error) {

	var proposalIdRule []interface{}
	for _, proposalIdItem := range proposalId {
		proposalIdRule = append(proposalIdRule, proposalIdItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.FilterLogs(opts, "MinBaseFeeProposed", proposalIdRule)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorMinBaseFeeProposedIterator{contract: _MinBaseFeeGovernor.contract, event: "MinBaseFeeProposed", logs: logs, sub: sub}, nil
}

// WatchMinBaseFeeProposed is a free log subscription operation binding the contract event 0x81949bf253695b6b74b0e778afee4e722f995caea0f17bad817b528ff624100d.
//
// Solidity: event MinBaseFeeProposed(bytes32 indexed proposalId, uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, uint256 executeAfter)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) WatchMinBaseFeeProposed(opts *bind.WatchOpts, sink chan<- *MinBaseFeeGovernorMinBaseFeeProposed, proposalId [][32]byte) (event.Subscription, error) {

	var proposalIdRule []interface{}
	for _, proposalIdItem := range proposalId {
		proposalIdRule = append(proposalIdRule, proposalIdItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.WatchLogs(opts, "MinBaseFeeProposed", proposalIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MinBaseFeeGovernorMinBaseFeeProposed)
				if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "MinBaseFeeProposed", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseMinBaseFeeProposed is a log parse operation binding the contract event 0x81949bf253695b6b74b0e778afee4e722f995caea0f17bad817b528ff624100d.
//
// Solidity: event MinBaseFeeProposed(bytes32 indexed proposalId, uint256 minBaseFee, uint256 activationBlock, uint256 proposedAt, uint256 executeAfter)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) ParseMinBaseFeeProposed(log types.Log) (*MinBaseFeeGovernorMinBaseFeeProposed, error) {
	event := new(MinBaseFeeGovernorMinBaseFeeProposed)
	if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "MinBaseFeeProposed", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MinBaseFeeGovernorMinBaseFeeScheduledIterator is returned from FilterMinBaseFeeScheduled and is used to iterate over the raw logs and unpacked data for MinBaseFeeScheduled events raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorMinBaseFeeScheduledIterator struct {
	Event *MinBaseFeeGovernorMinBaseFeeScheduled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *MinBaseFeeGovernorMinBaseFeeScheduledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MinBaseFeeGovernorMinBaseFeeScheduled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(MinBaseFeeGovernorMinBaseFeeScheduled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *MinBaseFeeGovernorMinBaseFeeScheduledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MinBaseFeeGovernorMinBaseFeeScheduledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MinBaseFeeGovernorMinBaseFeeScheduled represents a MinBaseFeeScheduled event raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorMinBaseFeeScheduled struct {
	ConfigIndex     *big.Int
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	Timestamp       *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterMinBaseFeeScheduled is a free log retrieval operation binding the contract event 0x9a59173c869dec4540284263b57c9bcb9c8bdd515e38c5c5e7d2dfeabcabf03e.
//
// Solidity: event MinBaseFeeScheduled(uint256 indexed configIndex, uint256 minBaseFee, uint256 activationBlock, uint256 timestamp)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) FilterMinBaseFeeScheduled(opts *bind.FilterOpts, configIndex []*big.Int) (*MinBaseFeeGovernorMinBaseFeeScheduledIterator, error) {

	var configIndexRule []interface{}
	for _, configIndexItem := range configIndex {
		configIndexRule = append(configIndexRule, configIndexItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.FilterLogs(opts, "MinBaseFeeScheduled", configIndexRule)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorMinBaseFeeScheduledIterator{contract: _MinBaseFeeGovernor.contract, event: "MinBaseFeeScheduled", logs: logs, sub: sub}, nil
}

// WatchMinBaseFeeScheduled is a free log subscription operation binding the contract event 0x9a59173c869dec4540284263b57c9bcb9c8bdd515e38c5c5e7d2dfeabcabf03e.
//
// Solidity: event MinBaseFeeScheduled(uint256 indexed configIndex, uint256 minBaseFee, uint256 activationBlock, uint256 timestamp)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) WatchMinBaseFeeScheduled(opts *bind.WatchOpts, sink chan<- *MinBaseFeeGovernorMinBaseFeeScheduled, configIndex []*big.Int) (event.Subscription, error) {

	var configIndexRule []interface{}
	for _, configIndexItem := range configIndex {
		configIndexRule = append(configIndexRule, configIndexItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.WatchLogs(opts, "MinBaseFeeScheduled", configIndexRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MinBaseFeeGovernorMinBaseFeeScheduled)
				if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "MinBaseFeeScheduled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseMinBaseFeeScheduled is a log parse operation binding the contract event 0x9a59173c869dec4540284263b57c9bcb9c8bdd515e38c5c5e7d2dfeabcabf03e.
//
// Solidity: event MinBaseFeeScheduled(uint256 indexed configIndex, uint256 minBaseFee, uint256 activationBlock, uint256 timestamp)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) ParseMinBaseFeeScheduled(log types.Log) (*MinBaseFeeGovernorMinBaseFeeScheduled, error) {
	event := new(MinBaseFeeGovernorMinBaseFeeScheduled)
	if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "MinBaseFeeScheduled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MinBaseFeeGovernorProposalCancelledIterator is returned from FilterProposalCancelled and is used to iterate over the raw logs and unpacked data for ProposalCancelled events raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorProposalCancelledIterator struct {
	Event *MinBaseFeeGovernorProposalCancelled // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *MinBaseFeeGovernorProposalCancelledIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MinBaseFeeGovernorProposalCancelled)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(MinBaseFeeGovernorProposalCancelled)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *MinBaseFeeGovernorProposalCancelledIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MinBaseFeeGovernorProposalCancelledIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MinBaseFeeGovernorProposalCancelled represents a ProposalCancelled event raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorProposalCancelled struct {
	ProposalId [32]byte
	Raw        types.Log // Blockchain specific contextual infos
}

// FilterProposalCancelled is a free log retrieval operation binding the contract event 0x7bf38ee614622e33a85c9c8b9f0dca88e471d781640cddbb75c90e2bdeb6e125.
//
// Solidity: event ProposalCancelled(bytes32 indexed proposalId)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) FilterProposalCancelled(opts *bind.FilterOpts, proposalId [][32]byte) (*MinBaseFeeGovernorProposalCancelledIterator, error) {

	var proposalIdRule []interface{}
	for _, proposalIdItem := range proposalId {
		proposalIdRule = append(proposalIdRule, proposalIdItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.FilterLogs(opts, "ProposalCancelled", proposalIdRule)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorProposalCancelledIterator{contract: _MinBaseFeeGovernor.contract, event: "ProposalCancelled", logs: logs, sub: sub}, nil
}

// WatchProposalCancelled is a free log subscription operation binding the contract event 0x7bf38ee614622e33a85c9c8b9f0dca88e471d781640cddbb75c90e2bdeb6e125.
//
// Solidity: event ProposalCancelled(bytes32 indexed proposalId)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) WatchProposalCancelled(opts *bind.WatchOpts, sink chan<- *MinBaseFeeGovernorProposalCancelled, proposalId [][32]byte) (event.Subscription, error) {

	var proposalIdRule []interface{}
	for _, proposalIdItem := range proposalId {
		proposalIdRule = append(proposalIdRule, proposalIdItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.WatchLogs(opts, "ProposalCancelled", proposalIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MinBaseFeeGovernorProposalCancelled)
				if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "ProposalCancelled", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseProposalCancelled is a log parse operation binding the contract event 0x7bf38ee614622e33a85c9c8b9f0dca88e471d781640cddbb75c90e2bdeb6e125.
//
// Solidity: event ProposalCancelled(bytes32 indexed proposalId)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) ParseProposalCancelled(log types.Log) (*MinBaseFeeGovernorProposalCancelled, error) {
	event := new(MinBaseFeeGovernorProposalCancelled)
	if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "ProposalCancelled", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MinBaseFeeGovernorProposalExecutedIterator is returned from FilterProposalExecuted and is used to iterate over the raw logs and unpacked data for ProposalExecuted events raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorProposalExecutedIterator struct {
	Event *MinBaseFeeGovernorProposalExecuted // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *MinBaseFeeGovernorProposalExecutedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MinBaseFeeGovernorProposalExecuted)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(MinBaseFeeGovernorProposalExecuted)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *MinBaseFeeGovernorProposalExecutedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MinBaseFeeGovernorProposalExecutedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MinBaseFeeGovernorProposalExecuted represents a ProposalExecuted event raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorProposalExecuted struct {
	ProposalId      [32]byte
	MinBaseFee      *big.Int
	ActivationBlock *big.Int
	Raw             types.Log // Blockchain specific contextual infos
}

// FilterProposalExecuted is a free log retrieval operation binding the contract event 0x2041749a72ed295219ba2be2beac04d48265c5ad481e501a2d6e61131eaffcbb.
//
// Solidity: event ProposalExecuted(bytes32 indexed proposalId, uint256 minBaseFee, uint256 activationBlock)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) FilterProposalExecuted(opts *bind.FilterOpts, proposalId [][32]byte) (*MinBaseFeeGovernorProposalExecutedIterator, error) {

	var proposalIdRule []interface{}
	for _, proposalIdItem := range proposalId {
		proposalIdRule = append(proposalIdRule, proposalIdItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.FilterLogs(opts, "ProposalExecuted", proposalIdRule)
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorProposalExecutedIterator{contract: _MinBaseFeeGovernor.contract, event: "ProposalExecuted", logs: logs, sub: sub}, nil
}

// WatchProposalExecuted is a free log subscription operation binding the contract event 0x2041749a72ed295219ba2be2beac04d48265c5ad481e501a2d6e61131eaffcbb.
//
// Solidity: event ProposalExecuted(bytes32 indexed proposalId, uint256 minBaseFee, uint256 activationBlock)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) WatchProposalExecuted(opts *bind.WatchOpts, sink chan<- *MinBaseFeeGovernorProposalExecuted, proposalId [][32]byte) (event.Subscription, error) {

	var proposalIdRule []interface{}
	for _, proposalIdItem := range proposalId {
		proposalIdRule = append(proposalIdRule, proposalIdItem)
	}

	logs, sub, err := _MinBaseFeeGovernor.contract.WatchLogs(opts, "ProposalExecuted", proposalIdRule)
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MinBaseFeeGovernorProposalExecuted)
				if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "ProposalExecuted", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseProposalExecuted is a log parse operation binding the contract event 0x2041749a72ed295219ba2be2beac04d48265c5ad481e501a2d6e61131eaffcbb.
//
// Solidity: event ProposalExecuted(bytes32 indexed proposalId, uint256 minBaseFee, uint256 activationBlock)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) ParseProposalExecuted(log types.Log) (*MinBaseFeeGovernorProposalExecuted, error) {
	event := new(MinBaseFeeGovernorProposalExecuted)
	if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "ProposalExecuted", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}

// MinBaseFeeGovernorTimelockDelayUpdatedIterator is returned from FilterTimelockDelayUpdated and is used to iterate over the raw logs and unpacked data for TimelockDelayUpdated events raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorTimelockDelayUpdatedIterator struct {
	Event *MinBaseFeeGovernorTimelockDelayUpdated // Event containing the contract specifics and raw log

	contract *bind.BoundContract // Generic contract to use for unpacking event data
	event    string              // Event name to use for unpacking event data

	logs chan types.Log        // Log channel receiving the found contract events
	sub  ethereum.Subscription // Subscription for errors, completion and termination
	done bool                  // Whether the subscription completed delivering logs
	fail error                 // Occurred error to stop iteration
}

// Next advances the iterator to the subsequent event, returning whether there
// are any more events found. In case of a retrieval or parsing error, false is
// returned and Error() can be queried for the exact failure.
func (it *MinBaseFeeGovernorTimelockDelayUpdatedIterator) Next() bool {
	// If the iterator failed, stop iterating
	if it.fail != nil {
		return false
	}
	// If the iterator completed, deliver directly whatever's available
	if it.done {
		select {
		case log := <-it.logs:
			it.Event = new(MinBaseFeeGovernorTimelockDelayUpdated)
			if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
				it.fail = err
				return false
			}
			it.Event.Raw = log
			return true

		default:
			return false
		}
	}
	// Iterator still in progress, wait for either a data or an error event
	select {
	case log := <-it.logs:
		it.Event = new(MinBaseFeeGovernorTimelockDelayUpdated)
		if err := it.contract.UnpackLog(it.Event, it.event, log); err != nil {
			it.fail = err
			return false
		}
		it.Event.Raw = log
		return true

	case err := <-it.sub.Err():
		it.done = true
		it.fail = err
		return it.Next()
	}
}

// Error returns any retrieval or parsing error occurred during filtering.
func (it *MinBaseFeeGovernorTimelockDelayUpdatedIterator) Error() error {
	return it.fail
}

// Close terminates the iteration process, releasing any pending underlying
// resources.
func (it *MinBaseFeeGovernorTimelockDelayUpdatedIterator) Close() error {
	it.sub.Unsubscribe()
	return nil
}

// MinBaseFeeGovernorTimelockDelayUpdated represents a TimelockDelayUpdated event raised by the MinBaseFeeGovernor contract.
type MinBaseFeeGovernorTimelockDelayUpdated struct {
	OldDelay *big.Int
	NewDelay *big.Int
	Raw      types.Log // Blockchain specific contextual infos
}

// FilterTimelockDelayUpdated is a free log retrieval operation binding the contract event 0x0d64018104bbece148774374900c9844b1c388724c82f170dbe557313b34daef.
//
// Solidity: event TimelockDelayUpdated(uint256 oldDelay, uint256 newDelay)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) FilterTimelockDelayUpdated(opts *bind.FilterOpts) (*MinBaseFeeGovernorTimelockDelayUpdatedIterator, error) {

	logs, sub, err := _MinBaseFeeGovernor.contract.FilterLogs(opts, "TimelockDelayUpdated")
	if err != nil {
		return nil, err
	}
	return &MinBaseFeeGovernorTimelockDelayUpdatedIterator{contract: _MinBaseFeeGovernor.contract, event: "TimelockDelayUpdated", logs: logs, sub: sub}, nil
}

// WatchTimelockDelayUpdated is a free log subscription operation binding the contract event 0x0d64018104bbece148774374900c9844b1c388724c82f170dbe557313b34daef.
//
// Solidity: event TimelockDelayUpdated(uint256 oldDelay, uint256 newDelay)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) WatchTimelockDelayUpdated(opts *bind.WatchOpts, sink chan<- *MinBaseFeeGovernorTimelockDelayUpdated) (event.Subscription, error) {

	logs, sub, err := _MinBaseFeeGovernor.contract.WatchLogs(opts, "TimelockDelayUpdated")
	if err != nil {
		return nil, err
	}
	return event.NewSubscription(func(quit <-chan struct{}) error {
		defer sub.Unsubscribe()
		for {
			select {
			case log := <-logs:
				// New log arrived, parse the event and forward to the user
				event := new(MinBaseFeeGovernorTimelockDelayUpdated)
				if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "TimelockDelayUpdated", log); err != nil {
					return err
				}
				event.Raw = log

				select {
				case sink <- event:
				case err := <-sub.Err():
					return err
				case <-quit:
					return nil
				}
			case err := <-sub.Err():
				return err
			case <-quit:
				return nil
			}
		}
	}), nil
}

// ParseTimelockDelayUpdated is a log parse operation binding the contract event 0x0d64018104bbece148774374900c9844b1c388724c82f170dbe557313b34daef.
//
// Solidity: event TimelockDelayUpdated(uint256 oldDelay, uint256 newDelay)
func (_MinBaseFeeGovernor *MinBaseFeeGovernorFilterer) ParseTimelockDelayUpdated(log types.Log) (*MinBaseFeeGovernorTimelockDelayUpdated, error) {
	event := new(MinBaseFeeGovernorTimelockDelayUpdated)
	if err := _MinBaseFeeGovernor.contract.UnpackLog(event, "TimelockDelayUpdated", log); err != nil {
		return nil, err
	}
	event.Raw = log
	return event, nil
}
