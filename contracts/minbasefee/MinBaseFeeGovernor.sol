// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.0;

/**
 * @title MinBaseFeeGovernor
 * @dev Governance contract for managing the minimum base fee threshold in Incentum network.
 *
 * This contract stores the minimum base fee value that acts as a floor in the EIP-1559
 * base fee calculation. The actual base fee is calculated dynamically by the client
 * using the standard EIP-1559 formula, but cannot fall below the minimum value stored here.
 *
 * Changes to the minimum base fee are controlled by governance through a timelock mechanism.
 * New values must be proposed, wait for timelock delay (minimum 2 days), then executed.
 */
contract MinBaseFeeGovernor {

    /// @notice Configuration for minimum base fee at a specific activation block
    struct MinBaseFeeConfig {
        uint256 minBaseFee;      // Minimum base fee in wei
        uint256 activationBlock; // Block number when this config becomes active
        uint256 timestamp;       // Timestamp when config was set
    }

    /// @notice Proposal for changing minimum base fee
    struct Proposal {
        uint256 minBaseFee;      // Proposed minimum base fee in wei
        uint256 activationBlock; // Block number when it becomes active
        uint256 proposedAt;      // Timestamp when proposal was created
        bool executed;           // Whether proposal has been executed
    }

    /// @notice Governance address that can propose and execute changes
    address public governance;

    /// @notice History of all minimum base fee configurations
    /// @dev Stored in chronological order by activationBlock for binary search
    MinBaseFeeConfig[] public configHistory;

    /// @notice Pending proposals mapped by proposal ID
    mapping(bytes32 => Proposal) public proposals;

    /// @notice Minimum timelock delay in seconds. Set at deployment.
    /// @dev Production deployments use 2 days; devnet deployments use a shorter value
    ///      (typically 10 minutes) so a full governance cycle can be exercised within
    ///      a single test window. Immutable so the deployed value cannot be lowered later.
    uint256 public immutable MIN_TIMELOCK_DELAY;

    /// @notice Current timelock delay (can be updated by governance, but not below MIN_TIMELOCK_DELAY)
    uint256 public timelockDelay;

    /// @notice Minimum delay in blocks before activation. Set at deployment.
    /// @dev Production deployments use 13000 (~18 hours at 5-second blocks); devnet
    ///      deployments use a shorter value (typically 100 blocks). Immutable so the
    ///      deployed value cannot be lowered later.
    uint256 public immutable MIN_ACTIVATION_DELAY_BLOCKS;

    /// @notice Maximum allowed minimum base fee (100 ETH)
    uint256 public constant MAX_MIN_BASE_FEE = 100 ether;

    /// @notice Minimum allowed minimum base fee (1 gwei)
    uint256 public constant MIN_MIN_BASE_FEE = 1 gwei;

    /// @notice Maximum change percentage per update (200% = can increase 3x or decrease to 1/3)
    uint256 public constant MAX_CHANGE_PERCENT = 200;

    /// @notice Paused state - when true, returns fallback value
    bool public paused;

    /// @notice Fallback minimum base fee used when contract is paused
    uint256 public fallbackMinBaseFee = 12600 gwei;

    /// @notice Emitted when a new minimum base fee is proposed
    event MinBaseFeeProposed(
        bytes32 indexed proposalId,
        uint256 minBaseFee,
        uint256 activationBlock,
        uint256 proposedAt,
        uint256 executeAfter
    );

    /// @notice Emitted when a proposal is executed
    event ProposalExecuted(
        bytes32 indexed proposalId,
        uint256 minBaseFee,
        uint256 activationBlock
    );

    /// @notice Emitted when a proposal is cancelled
    event ProposalCancelled(bytes32 indexed proposalId);

    /// @notice Emitted when a new minimum base fee configuration is added
    event MinBaseFeeScheduled(
        uint256 indexed configIndex,
        uint256 minBaseFee,
        uint256 activationBlock,
        uint256 timestamp
    );

    /// @notice Emitted when governance address is changed
    event GovernanceTransferred(address indexed previousGovernance, address indexed newGovernance);

    /// @notice Emitted when timelock delay is updated
    event TimelockDelayUpdated(uint256 oldDelay, uint256 newDelay);

    /// @notice Emitted when contract is paused
    event Paused();

    /// @notice Emitted when contract is unpaused
    event Unpaused();

    /// @notice Emitted when fallback min base fee is updated
    event FallbackMinBaseFeeUpdated(uint256 oldValue, uint256 newValue);

    /// @notice Thrown when caller is not governance
    error OnlyGovernance();

    /// @notice Thrown when activation block is in the past or too soon
    error ActivationTooSoon(uint256 activationBlock, uint256 minActivationBlock);

    /// @notice Thrown when activation block is not after the last scheduled activation
    error ActivationBlockNotSequential(uint256 activationBlock, uint256 lastActivationBlock);

    /// @notice Thrown when min base fee is zero
    error MinBaseFeeZero();

    /// @notice Thrown when min base fee is out of allowed bounds
    error MinBaseFeeOutOfBounds(uint256 value, uint256 min, uint256 max);

    /// @notice Thrown when proposed change is too large
    error ChangeTooLarge(uint256 newValue, uint256 currentValue, uint256 maxAllowed);

    /// @notice Thrown when new governance address is zero
    error GovernanceZero();

    /// @notice Thrown when config index is out of bounds
    error InvalidConfigIndex(uint256 index, uint256 length);

    /// @notice Thrown when proposal is not found
    error ProposalNotFound(bytes32 proposalId);

    /// @notice Thrown when timelock has not expired yet
    error TimelockNotExpired(uint256 currentTime, uint256 executeAfter);

    /// @notice Thrown when proposal has already been executed
    error ProposalAlreadyExecuted(bytes32 proposalId);

    /// @notice Thrown when trying to set invalid timelock delay
    error InvalidTimelockDelay(uint256 delay);

    /// @notice Thrown when trying to propose while paused
    error ContractPaused();

    modifier onlyGovernance() {
        if (msg.sender != governance) revert OnlyGovernance();
        _;
    }

    modifier whenNotPaused() {
        if (paused) revert ContractPaused();
        _;
    }

    /**
     * @notice Initialize the contract with genesis minimum base fee and per-deployment delays
     * @param _governance Address of governance/timelock contract
     * @param _initialMinBaseFee Initial minimum base fee in wei
     * @param _activationBlock Block number when initial config becomes active (typically 0 for genesis)
     * @param _minTimelockDelay Minimum timelock delay in seconds (production: 2 days; devnet: shorter)
     * @param _minActivationDelayBlocks Minimum activation delay in blocks (production: 13000; devnet: shorter)
     */
    constructor(
        address _governance,
        uint256 _initialMinBaseFee,
        uint256 _activationBlock,
        uint256 _minTimelockDelay,
        uint256 _minActivationDelayBlocks
    ) {
        if (_governance == address(0)) revert GovernanceZero();
        if (_initialMinBaseFee == 0) revert MinBaseFeeZero();

        governance = _governance;
        MIN_TIMELOCK_DELAY = _minTimelockDelay;
        timelockDelay = _minTimelockDelay;
        MIN_ACTIVATION_DELAY_BLOCKS = _minActivationDelayBlocks;

        configHistory.push(MinBaseFeeConfig({
            minBaseFee: _initialMinBaseFee,
            activationBlock: _activationBlock,
            timestamp: block.timestamp
        }));

        emit MinBaseFeeScheduled(0, _initialMinBaseFee, _activationBlock, block.timestamp);
        emit GovernanceTransferred(address(0), _governance);
    }

    /**
     * @notice Get the current active minimum base fee
     * @return The minimum base fee in wei that is active at current block
     */
    function getCurrentMinBaseFee() external view returns (uint256) {
        if (paused) {
            return fallbackMinBaseFee;
        }
        return getMinBaseFeeForBlock(block.number);
    }

    /**
     * @notice Get the minimum base fee for a specific block number
     * @param blockNumber The block number to query
     * @return The minimum base fee in wei that was/is active at that block
     * @dev Uses binary search for efficient lookup in historical data
     */
    function getMinBaseFeeForBlock(uint256 blockNumber) public view returns (uint256) {
        uint256 length = configHistory.length;

        // Binary search to find the right config
        // We want the last config where activationBlock <= blockNumber
        uint256 left = 0;
        uint256 right = length - 1;
        uint256 resultIndex = 0;

        while (left <= right) {
            uint256 mid = (left + right) / 2;

            if (configHistory[mid].activationBlock <= blockNumber) {
                resultIndex = mid;
                left = mid + 1;
            } else {
                if (mid == 0) break;
                right = mid - 1;
            }
        }

        return configHistory[resultIndex].minBaseFee;
    }

    /**
     * @notice Propose a new minimum base fee to activate at a future block
     * @param _minBaseFee New minimum base fee in wei
     * @param _activationBlock Block number when new value becomes active
     * @return proposalId Unique identifier for this proposal
     * @dev Requires activation block to be at least MIN_ACTIVATION_DELAY_BLOCKS in the future
     * @dev Requires timelock delay before execution
     */
    function proposeMinBaseFee(uint256 _minBaseFee, uint256 _activationBlock)
        external
        onlyGovernance
        whenNotPaused
        returns (bytes32)
    {
        // Validate min base fee is not zero
        if (_minBaseFee == 0) revert MinBaseFeeZero();

        // Validate min base fee is within bounds
        if (_minBaseFee < MIN_MIN_BASE_FEE || _minBaseFee > MAX_MIN_BASE_FEE) {
            revert MinBaseFeeOutOfBounds(_minBaseFee, MIN_MIN_BASE_FEE, MAX_MIN_BASE_FEE);
        }

        // Validate activation block is far enough in the future
        uint256 minActivationBlock = block.number + MIN_ACTIVATION_DELAY_BLOCKS;
        if (_activationBlock < minActivationBlock) {
            revert ActivationTooSoon(_activationBlock, minActivationBlock);
        }

        // Ensure activation blocks are sequential
        uint256 lastActivationBlock = configHistory[configHistory.length - 1].activationBlock;
        if (_activationBlock <= lastActivationBlock) {
            revert ActivationBlockNotSequential(_activationBlock, lastActivationBlock);
        }

        // Validate change is not too large (prevent extreme changes)
        uint256 currentMinBaseFee = getMinBaseFeeForBlock(block.number);
        uint256 maxAllowed = currentMinBaseFee * (100 + MAX_CHANGE_PERCENT) / 100;
        uint256 minAllowed = currentMinBaseFee * 100 / (100 + MAX_CHANGE_PERCENT);

        if (_minBaseFee > maxAllowed || _minBaseFee < minAllowed) {
            revert ChangeTooLarge(_minBaseFee, currentMinBaseFee, maxAllowed);
        }

        // Create proposal
        bytes32 proposalId = keccak256(abi.encode(_minBaseFee, _activationBlock, block.timestamp));

        uint256 executeAfter = block.timestamp + timelockDelay;

        proposals[proposalId] = Proposal({
            minBaseFee: _minBaseFee,
            activationBlock: _activationBlock,
            proposedAt: block.timestamp,
            executed: false
        });

        emit MinBaseFeeProposed(
            proposalId,
            _minBaseFee,
            _activationBlock,
            block.timestamp,
            executeAfter
        );

        return proposalId;
    }

    /**
     * @notice Execute a proposal after timelock delay has passed
     * @param proposalId Unique identifier of the proposal to execute
     * @dev Can only be called after timelock delay has passed
     */
    function executeProposal(bytes32 proposalId) external onlyGovernance {
        Proposal storage proposal = proposals[proposalId];

        // Validate proposal exists
        if (proposal.proposedAt == 0) revert ProposalNotFound(proposalId);

        // Validate proposal has not been executed
        if (proposal.executed) revert ProposalAlreadyExecuted(proposalId);

        // Validate timelock has expired
        uint256 executeAfter = proposal.proposedAt + timelockDelay;
        if (block.timestamp < executeAfter) {
            revert TimelockNotExpired(block.timestamp, executeAfter);
        }

        // Execute: add configuration to history
        configHistory.push(MinBaseFeeConfig({
            minBaseFee: proposal.minBaseFee,
            activationBlock: proposal.activationBlock,
            timestamp: block.timestamp
        }));

        // Mark as executed
        proposal.executed = true;

        emit ProposalExecuted(proposalId, proposal.minBaseFee, proposal.activationBlock);
        emit MinBaseFeeScheduled(
            configHistory.length - 1,
            proposal.minBaseFee,
            proposal.activationBlock,
            block.timestamp
        );
    }

    /**
     * @notice Cancel a pending proposal before it is executed
     * @param proposalId Unique identifier of the proposal to cancel
     */
    function cancelProposal(bytes32 proposalId) external onlyGovernance {
        Proposal storage proposal = proposals[proposalId];

        // Validate proposal exists
        if (proposal.proposedAt == 0) revert ProposalNotFound(proposalId);

        // Validate proposal has not been executed
        if (proposal.executed) revert ProposalAlreadyExecuted(proposalId);

        // Delete proposal
        delete proposals[proposalId];

        emit ProposalCancelled(proposalId);
    }

    /**
     * @notice Get information about a proposal
     * @param proposalId Unique identifier of the proposal
     * @return minBaseFee Proposed minimum base fee
     * @return activationBlock Proposed activation block
     * @return proposedAt Timestamp when proposed
     * @return executed Whether proposal has been executed
     * @return executeAfter Timestamp after which proposal can be executed
     * @return canExecute Whether proposal can be executed now
     */
    function getProposal(bytes32 proposalId) external view returns (
        uint256 minBaseFee,
        uint256 activationBlock,
        uint256 proposedAt,
        bool executed,
        uint256 executeAfter,
        bool canExecute
    ) {
        Proposal memory proposal = proposals[proposalId];
        executeAfter = proposal.proposedAt + timelockDelay;
        canExecute = !proposal.executed &&
                     proposal.proposedAt > 0 &&
                     block.timestamp >= executeAfter;

        return (
            proposal.minBaseFee,
            proposal.activationBlock,
            proposal.proposedAt,
            proposal.executed,
            executeAfter,
            canExecute
        );
    }

    /**
     * @notice Pause the contract - returns fallback value when paused
     * @dev Can only be called by governance in emergency situations
     */
    function pause() external onlyGovernance {
        paused = true;
        emit Paused();
    }

    /**
     * @notice Unpause the contract - resume normal operation
     * @dev Can only be called by governance
     */
    function unpause() external onlyGovernance {
        paused = false;
        emit Unpaused();
    }

    /**
     * @notice Update the timelock delay
     * @param newDelay New timelock delay in seconds
     * @dev Cannot be set below MIN_TIMELOCK_DELAY
     */
    function setTimelockDelay(uint256 newDelay) external onlyGovernance {
        if (newDelay < MIN_TIMELOCK_DELAY) {
            revert InvalidTimelockDelay(newDelay);
        }

        uint256 oldDelay = timelockDelay;
        timelockDelay = newDelay;

        emit TimelockDelayUpdated(oldDelay, newDelay);
    }

    /**
     * @notice Update the fallback minimum base fee (used when paused)
     * @param newFallback New fallback value in wei
     */
    function setFallbackMinBaseFee(uint256 newFallback) external onlyGovernance {
        if (newFallback == 0) revert MinBaseFeeZero();

        uint256 oldValue = fallbackMinBaseFee;
        fallbackMinBaseFee = newFallback;

        emit FallbackMinBaseFeeUpdated(oldValue, newFallback);
    }

    /**
     * @notice Get the total number of configurations in history
     * @return The length of configHistory array
     */
    function getConfigHistoryLength() external view returns (uint256) {
        return configHistory.length;
    }

    /**
     * @notice Get a specific configuration from history
     * @param index Index in the configHistory array
     * @return config The MinBaseFeeConfig at that index
     */
    function getConfigByIndex(uint256 index) external view returns (MinBaseFeeConfig memory) {
        if (index >= configHistory.length) {
            revert InvalidConfigIndex(index, configHistory.length);
        }
        return configHistory[index];
    }

    /**
     * @notice Get all configurations (useful for testing/debugging)
     * @return All MinBaseFeeConfig entries
     * @dev Warning: This can be gas-expensive for large histories. Use pagination in production.
     */
    function getAllConfigs() external view returns (MinBaseFeeConfig[] memory) {
        return configHistory;
    }

    /**
     * @notice Transfer governance to a new address
     * @param newGovernance New governance address
     * @dev Can only be called by current governance
     */
    function transferGovernance(address newGovernance) external onlyGovernance {
        if (newGovernance == address(0)) revert GovernanceZero();

        address previousGovernance = governance;
        governance = newGovernance;

        emit GovernanceTransferred(previousGovernance, newGovernance);
    }
}

