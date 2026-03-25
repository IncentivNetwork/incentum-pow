// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "@openzeppelin/contracts/governance/TimelockController.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import {
    SafeERC20
} from "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

/**
 * @title MinerRegistry
 * @notice Miner authorization registry for DPoW.
 * @dev Storage slots 0-3 are consensus-critical and MUST NEVER move.
 */
contract MinerRegistry {
    using SafeERC20 for IERC20;

    // ============================================================
    // Constants
    // ============================================================

    uint256 public constant STAKE_AMOUNT = 100_000_000 * 10 ** 18;
    uint256 public constant MATURITY_TIME = 24 hours;
    uint256 public constant MATURITY_BLOCKS = 17_280;
    uint256 public constant UNSTAKE_DELAY = 7 days;

    uint256 private constant _NOT_ENTERED = 1;
    uint256 private constant _ENTERED = 2;

    // ============================================================
    // Immutable config
    // ============================================================

    IERC20 public immutable centToken;
    TimelockController public immutable timelock;

    // ============================================================
    // Consensus-critical storage layout (slots 0-3) - FROZEN
    // DO NOT reorder. DO NOT insert anything before these fields.
    // ============================================================

    /// @custom:storage-slot 0
    mapping(address miner => bool isActive) public miners;

    /// @custom:storage-slot 1
    mapping(address miner => uint256 timestamp) public stakeTime;

    /// @custom:storage-slot 2
    mapping(address miner => uint256 blockNumber) public stakeBlock;

    /// @custom:storage-slot 3
    mapping(address miner => uint256 timestamp) public unstakeRequestTime;

    // ============================================================
    // Non-consensus storage (must stay after slots 0-3)
    // ============================================================

    uint256 public activeMinerCount;
    bool public stakingPaused;
    uint256 private _reentrancyStatus;

    // ============================================================
    // Events
    // ============================================================

    event MinerStaked(
        address indexed miner,
        uint256 stakeTime,
        uint256 stakeBlock
    );
    event UnstakeRequested(address indexed miner, uint256 requestTime);
    event MinerUnstaked(address indexed miner, uint256 amount);
    event EmergencyRemoval(
        address indexed miner,
        address indexed refundRecipient,
        string reason
    );
    event StakingPaused(bool paused);

    // ============================================================
    // Errors
    // ============================================================

    error ZeroAddress();
    error InsufficientBalance();
    error InsufficientAllowance();
    error AlreadyStaked();
    error UnstakeInProgress();
    error NotStaked();
    error UnstakeDelayNotMet();
    error OnlyGovernance();
    error StakingCurrentlyPaused();
    error EmptyReason();
    error InvalidRefundRecipient();
    error ReentrantCall();

    // ============================================================
    // Modifiers
    // ============================================================

    modifier onlyGovernance() {
        if (msg.sender != address(timelock)) revert OnlyGovernance();
        _;
    }

    modifier whenStakingNotPaused() {
        if (stakingPaused) revert StakingCurrentlyPaused();
        _;
    }

    modifier nonReentrant() {
        if (_reentrancyStatus == _ENTERED) revert ReentrantCall();
        _reentrancyStatus = _ENTERED;
        _;
        _reentrancyStatus = _NOT_ENTERED;
    }

    // ============================================================
    // Constructor
    // ============================================================

    constructor(address centToken_, address timelock_) {
        if (centToken_ == address(0) || timelock_ == address(0)) revert ZeroAddress();

        centToken = IERC20(centToken_);
        timelock = TimelockController(payable(timelock_));
        _reentrancyStatus = _NOT_ENTERED;
    }

    // ============================================================
    // Core functions
    // ============================================================

    /// @notice Stakes exactly STAKE_AMOUNT of CENT for the caller.
    /// @dev Self-only staking: msg.sender is always the miner. Third-party staking is intentionally unsupported.
    function stake() external nonReentrant whenStakingNotPaused {
        address miner = msg.sender;

        if (miners[miner]) revert AlreadyStaked();
        if (unstakeRequestTime[miner] != 0) revert UnstakeInProgress();

        if (centToken.balanceOf(msg.sender) < STAKE_AMOUNT) revert InsufficientBalance();
        if (centToken.allowance(msg.sender, address(this)) < STAKE_AMOUNT) revert InsufficientAllowance();

        centToken.safeTransferFrom(msg.sender, address(this), STAKE_AMOUNT);

        miners[miner] = true;
        stakeTime[miner] = block.timestamp;
        stakeBlock[miner] = block.number;
        activeMinerCount++;

        emit MinerStaked(miner, block.timestamp, block.number);
    }

    /// @notice Starts unstaking for the caller and removes them from the active miner set.
    /// @dev stakeTime and stakeBlock are intentionally preserved until finalizeUnstake() so emergencyRemoveMiner() can still determine refund eligibility.
    function requestUnstake() external nonReentrant {
        address miner = msg.sender;

        if (!miners[miner]) revert NotStaked();

        unstakeRequestTime[miner] = block.timestamp;
        miners[miner] = false;
        activeMinerCount--;

        // stakeTime and stakeBlock are intentionally preserved here;
        // they are cleared only in finalizeUnstake() to support emergencyRemoveMiner refund logic.
        emit UnstakeRequested(miner, block.timestamp);
    }

    /// @notice Finalizes unstaking for the caller after UNSTAKE_DELAY and returns the locked stake.
    /// @dev Self-only flow: only the original staker/miner can finalize their unstake.
    function finalizeUnstake() external nonReentrant {
        address miner = msg.sender;
        uint256 requestTime = unstakeRequestTime[miner];

        if (requestTime == 0) revert NotStaked();
        if (block.timestamp < requestTime + UNSTAKE_DELAY) revert UnstakeDelayNotMet();

        delete stakeTime[miner];
        delete stakeBlock[miner];
        delete unstakeRequestTime[miner];

        centToken.safeTransfer(miner, STAKE_AMOUNT);

        emit MinerUnstaked(miner, STAKE_AMOUNT);
    }

    /// @notice Pauses or unpauses new staking.
    /// @dev Governance-only operation.
    /// @param paused True to pause new staking, false to re-enable it.
    function setPaused(bool paused) external onlyGovernance {
        stakingPaused = paused;
        emit StakingPaused(paused);
    }

    /// @notice Force-removes a miner and refunds their locked stake to the specified recipient.
    /// @dev Governance-only emergency path.
    /// @param miner Miner address whose state will be removed.
    /// @param refundRecipient Address that receives the refunded stake.
    /// @param reason Non-empty human-readable reason for the action.
    function emergencyRemoveMiner(
        address miner,
        address refundRecipient,
        string calldata reason
    ) external onlyGovernance nonReentrant {
        if (bytes(reason).length == 0) revert EmptyReason();
        if (refundRecipient == address(0)) revert InvalidRefundRecipient();

        bool wasActive = miners[miner];
        bool hasStake = wasActive || stakeBlock[miner] != 0 || unstakeRequestTime[miner] != 0 || stakeTime[miner] != 0;

        if (wasActive) {
            activeMinerCount--;
        }

        delete miners[miner];
        delete stakeTime[miner];
        delete stakeBlock[miner];
        delete unstakeRequestTime[miner];

        if (hasStake) {
            centToken.safeTransfer(refundRecipient, STAKE_AMOUNT);
        }

        emit EmergencyRemoval(miner, refundRecipient, reason);
    }

    /// @notice Returns whether a miner is currently authorized for block production.
    /// @dev Authorization requires active stake status and both maturity thresholds to be satisfied.
    /// @param miner Miner address to check.
    function isAuthorizedMiner(address miner) external view returns (bool) {
        return miners[miner]
            && block.timestamp >= stakeTime[miner] + MATURITY_TIME
            && block.number >= stakeBlock[miner] + MATURITY_BLOCKS;
    }
}
