// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "forge-std/Test.sol";

import "../../contracts/incentiv/MinerRegistry.sol";
import "../../contracts/incentiv/mocks/MockCENT.sol";
import "../../contracts/incentiv/mocks/ReentrantCENT.sol";
import "../../contracts/incentiv/mocks/ReentrantEmergencyCENT.sol";

contract MinerRegistryTest is Test {
    uint256 internal stakeAmount;

    MinerRegistry internal registry;
    MockCENT internal cent;

    address internal miner = address(0xBEEF);
    address internal timelock = address(0xCAFE);

    event MinerStaked(address indexed payer, address indexed miner, uint256 stakeTime, uint256 stakeBlock);
    event UnstakeRequested(address indexed miner, uint256 requestTime);
    event MinerUnstaked(address indexed miner, uint256 amount);
    event EmergencyRemoval(address indexed miner, address indexed refundRecipient, string reason);
    event StakingPaused(bool paused);

    function setUp() public {
        cent = new MockCENT();
        registry = new MinerRegistry(address(cent), timelock);

        stakeAmount = registry.STAKE_AMOUNT();

        cent.mint(miner, stakeAmount);

        vm.prank(miner);
        cent.approve(address(registry), stakeAmount);
    }

    function testStake_SetsState() public {
        vm.prank(miner);
        registry.stake();

        assertTrue(registry.miners(miner));
        assertEq(registry.stakeTime(miner), block.timestamp);
        assertEq(registry.stakeBlock(miner), block.number);
        assertEq(registry.activeMinerCount(), 1);
        assertEq(cent.balanceOf(miner), 0);
        assertEq(cent.balanceOf(address(registry)), stakeAmount);
    }

    function testStake_EmitsMinerStakedEvent() public {
        vm.expectEmit(true, true, false, true, address(registry));
        emit MinerStaked(miner, miner, block.timestamp, block.number);

        vm.prank(miner);
        registry.stake();
    }

    function testStake_RevertsWithoutAllowance() public {
        address minerNoApproval = address(0xABCD);

        cent.mint(minerNoApproval, stakeAmount);

        vm.expectRevert(MinerRegistry.InsufficientStake.selector);

        vm.prank(minerNoApproval);
        registry.stake();
    }

    function testStake_RevertsWithoutBalance() public {
        address minerNoBalance = address(0xDCBA);

        vm.prank(minerNoBalance);
        cent.approve(address(registry), stakeAmount);

        vm.expectRevert(MinerRegistry.InsufficientStake.selector);

        vm.prank(minerNoBalance);
        registry.stake();
    }

    function testStake_RevertsIfAlreadyStaked() public {
        vm.prank(miner);
        registry.stake();

        vm.expectRevert(MinerRegistry.AlreadyStaked.selector);

        vm.prank(miner);
        registry.stake();
    }

    function testStake_RevertsIfUnstakeAlreadyRequested() public {
        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        vm.expectRevert(MinerRegistry.AlreadyStaked.selector);

        vm.prank(miner);
        registry.stake();
    }

    function testRequestUnstake_DisablesMinerAndSetsRequestTime() public {
        vm.prank(miner);
        registry.stake();

        vm.warp(block.timestamp + 1);

        vm.prank(miner);
        registry.requestUnstake();

        assertFalse(registry.miners(miner));
        assertEq(registry.unstakeRequestTime(miner), block.timestamp);
        assertEq(registry.activeMinerCount(), 0);
    }

    function testRequestUnstake_EmitsUnstakeRequestedEvent() public {
        vm.prank(miner);
        registry.stake();

        vm.warp(block.timestamp + 1);

        vm.expectEmit(true, false, false, true, address(registry));
        emit UnstakeRequested(miner, block.timestamp);

        vm.prank(miner);
        registry.requestUnstake();
    }

    function testRequestUnstake_RevertsIfNotStaked() public {
        vm.expectRevert(MinerRegistry.NotStaked.selector);

        vm.prank(miner);
        registry.requestUnstake();
    }

    function testRequestUnstake_RevertsIfCalledTwice() public {
        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        vm.expectRevert(MinerRegistry.NotStaked.selector);

        vm.prank(miner);
        registry.requestUnstake();
    }

    function testFinalizeUnstake_AfterDelay_ReturnsFundsAndClearsState() public {
        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        vm.warp(block.timestamp + registry.UNSTAKE_DELAY());

        vm.prank(miner);
        registry.finalizeUnstake();

        assertFalse(registry.miners(miner));
        assertEq(registry.stakeTime(miner), 0);
        assertEq(registry.stakeBlock(miner), 0);
        assertEq(registry.unstakeRequestTime(miner), 0);

        assertEq(cent.balanceOf(miner), stakeAmount);
        assertEq(cent.balanceOf(address(registry)), 0);
    }

    function testFinalizeUnstake_EmitsMinerUnstakedEvent() public {
        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        vm.warp(block.timestamp + registry.UNSTAKE_DELAY());

        vm.expectEmit(true, false, false, true, address(registry));
        emit MinerUnstaked(miner, stakeAmount);

        vm.prank(miner);
        registry.finalizeUnstake();
    }

    function testIsAuthorizedMiner_BecomesTrueOnlyAfterMaturity() public {
        vm.prank(miner);
        registry.stake();

        assertFalse(registry.isAuthorizedMiner(miner));

        vm.warp(block.timestamp + registry.MATURITY_TIME());
        vm.roll(block.number + registry.MATURITY_BLOCKS());

        assertTrue(registry.isAuthorizedMiner(miner));
    }

    function testIsAuthorizedMiner_FalseIfOnlyTimePassed() public {
        vm.prank(miner);
        registry.stake();

        vm.warp(block.timestamp + registry.MATURITY_TIME());
        vm.roll(block.number + registry.MATURITY_BLOCKS() - 1);

        assertFalse(registry.isAuthorizedMiner(miner));
    }

    function testIsAuthorizedMiner_FalseIfOnlyBlocksPassed() public {
        vm.prank(miner);
        registry.stake();

        vm.roll(block.number + registry.MATURITY_BLOCKS());
        vm.warp(block.timestamp + registry.MATURITY_TIME() - 1);

        assertFalse(registry.isAuthorizedMiner(miner));
    }

    function testIsAuthorizedMiner_FalseAfterRequestUnstake() public {
        vm.prank(miner);
        registry.stake();

        vm.warp(block.timestamp + registry.MATURITY_TIME());
        vm.roll(block.number + registry.MATURITY_BLOCKS());

        assertTrue(registry.isAuthorizedMiner(miner));

        vm.prank(miner);
        registry.requestUnstake();

        assertFalse(registry.isAuthorizedMiner(miner));
    }

    function testIsAuthorizedMiner_FalseAfterEmergencyRemove() public {
        vm.prank(miner);
        registry.stake();

        vm.warp(block.timestamp + registry.MATURITY_TIME());
        vm.roll(block.number + registry.MATURITY_BLOCKS());

        assertTrue(registry.isAuthorizedMiner(miner));

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, address(0x1234), "removed");

        assertFalse(registry.isAuthorizedMiner(miner));
    }

    function testFinalizeUnstake_RevertsBeforeDelay() public {
        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        vm.expectRevert(MinerRegistry.UnstakeDelayNotMet.selector);

        vm.prank(miner);
        registry.finalizeUnstake();
    }

    function testFinalizeUnstake_RevertsIfNoRequestExists() public {
        vm.expectRevert(MinerRegistry.NotStaked.selector);

        vm.prank(miner);
        registry.finalizeUnstake();
    }

    function testFinalizeUnstake_RevertsIfCalledTwice() public {
        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        vm.warp(block.timestamp + registry.UNSTAKE_DELAY());

        vm.prank(miner);
        registry.finalizeUnstake();

        vm.expectRevert(MinerRegistry.NotStaked.selector);

        vm.prank(miner);
        registry.finalizeUnstake();
    }

    function testSetPaused_BlocksNewStake() public {
        address miner2 = address(0xB0B);

        cent.mint(miner2, stakeAmount);

        vm.prank(miner2);
        cent.approve(address(registry), stakeAmount);

        vm.prank(timelock);
        registry.setPaused(true);

        vm.expectRevert(MinerRegistry.StakingCurrentlyPaused.selector);

        vm.prank(miner2);
        registry.stake();
    }

    function testSetPaused_FalseReenablesStake() public {
        address miner2 = address(0xB0C);

        cent.mint(miner2, stakeAmount);

        vm.prank(miner2);
        cent.approve(address(registry), stakeAmount);

        vm.prank(timelock);
        registry.setPaused(true);

        vm.prank(timelock);
        registry.setPaused(false);

        vm.prank(miner2);
        registry.stake();

        assertTrue(registry.miners(miner2));
        assertEq(registry.activeMinerCount(), 1);
    }

    function testSetPaused_EmitsStakingPausedEvent() public {
        vm.expectEmit(false, false, false, true, address(registry));
        emit StakingPaused(true);

        vm.prank(timelock);
        registry.setPaused(true);

        vm.expectEmit(false, false, false, true, address(registry));
        emit StakingPaused(false);

        vm.prank(timelock);
        registry.setPaused(false);
    }

    function testEmergencyRemoveMiner_RefundsAndClearsState() public {
        address refundRecipient = address(0xD00D);

        vm.prank(miner);
        registry.stake();

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, refundRecipient, "policy violation");

        assertFalse(registry.miners(miner));
        assertEq(registry.stakeTime(miner), 0);
        assertEq(registry.stakeBlock(miner), 0);
        assertEq(registry.unstakeRequestTime(miner), 0);
        assertEq(registry.activeMinerCount(), 0);

        assertEq(cent.balanceOf(refundRecipient), stakeAmount);
        assertEq(cent.balanceOf(address(registry)), 0);
    }

    function testEmergencyRemoveMiner_EmitsEmergencyRemovalEvent() public {
        address refundRecipient = address(0xD00D);

        vm.prank(miner);
        registry.stake();

        vm.expectEmit(true, true, false, true, address(registry));
        emit EmergencyRemoval(miner, refundRecipient, "policy violation");

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, refundRecipient, "policy violation");
    }

    function testEmergencyRemoveMiner_AfterRequestUnstake_RefundsWithoutDoubleDecrement() public {
        address refundRecipient = address(0xAAAA);

        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        assertEq(registry.activeMinerCount(), 0);

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, refundRecipient, "cleanup pending unstake");

        assertFalse(registry.miners(miner));
        assertEq(registry.stakeTime(miner), 0);
        assertEq(registry.stakeBlock(miner), 0);
        assertEq(registry.unstakeRequestTime(miner), 0);
        assertEq(registry.activeMinerCount(), 0);

        assertEq(cent.balanceOf(refundRecipient), stakeAmount);
        assertEq(cent.balanceOf(address(registry)), 0);
    }

    function testEmergencyRemoveMiner_RefundsWhenStakeTimestampIsZero() public {
        address refundRecipient = address(0xABCD);

        vm.warp(0);

        vm.prank(miner);
        registry.stake();

        assertEq(registry.stakeTime(miner), 0);

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, refundRecipient, "zero timestamp");

        assertEq(cent.balanceOf(refundRecipient), stakeAmount);
        assertEq(cent.balanceOf(address(registry)), 0);
    }

    function testEmergencyRemoveMiner_AfterFinalizeUnstake_DoesNotDoubleRefund() public {
        address refundRecipient = address(0xF00D);

        vm.prank(miner);
        registry.stake();

        vm.prank(miner);
        registry.requestUnstake();

        vm.warp(block.timestamp + registry.UNSTAKE_DELAY());

        vm.prank(miner);
        registry.finalizeUnstake();

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, refundRecipient, "cleanup");

        assertEq(cent.balanceOf(miner), stakeAmount);
        assertEq(cent.balanceOf(refundRecipient), 0);
        assertEq(cent.balanceOf(address(registry)), 0);

        assertFalse(registry.miners(miner));
        assertEq(registry.stakeTime(miner), 0);
        assertEq(registry.stakeBlock(miner), 0);
        assertEq(registry.unstakeRequestTime(miner), 0);
    }

    function testEmergencyRemoveMiner_RevertsOnEmptyReason() public {
        vm.prank(miner);
        registry.stake();

        vm.expectRevert(MinerRegistry.EmptyReason.selector);

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, address(0x1234), "");
    }

    function testEmergencyRemoveMiner_RevertsOnZeroRefundRecipient() public {
        vm.prank(miner);
        registry.stake();

        vm.expectRevert(MinerRegistry.InvalidRefundRecipient.selector);

        vm.prank(timelock);
        registry.emergencyRemoveMiner(miner, address(0), "zero recipient");
    }

    function testEmergencyRemoveMiner_OnEmptyState_DoesNotRefundAndDoesNotRevert() public {
        address neverStakedMiner = address(0x9999);
        address refundRecipient = address(0x7777);

        vm.prank(timelock);
        registry.emergencyRemoveMiner(neverStakedMiner, refundRecipient, "cleanup empty state");

        assertFalse(registry.miners(neverStakedMiner));
        assertEq(registry.stakeTime(neverStakedMiner), 0);
        assertEq(registry.stakeBlock(neverStakedMiner), 0);
        assertEq(registry.unstakeRequestTime(neverStakedMiner), 0);
        assertEq(registry.activeMinerCount(), 0);

        assertEq(cent.balanceOf(refundRecipient), 0);
        assertEq(cent.balanceOf(address(registry)), 0);
    }

    function testSetPaused_RevertsForNonGovernance() public {
        vm.expectRevert(MinerRegistry.OnlyGovernance.selector);

        vm.prank(miner);
        registry.setPaused(true);
    }

    function testEmergencyRemoveMiner_RevertsForNonGovernance() public {
        vm.prank(miner);
        registry.stake();

        vm.expectRevert(MinerRegistry.OnlyGovernance.selector);

        vm.prank(miner);
        registry.emergencyRemoveMiner(miner, address(0x1234), "not allowed");
    }

    function testStake_RevertsOnReentrancy() public {
        ReentrantCENT reentrantToken = new ReentrantCENT();
        MinerRegistry reentrantRegistry = new MinerRegistry(address(reentrantToken), timelock);

        reentrantToken.setRegistry(address(reentrantRegistry));
        reentrantToken.mint(miner, stakeAmount);

        vm.prank(miner);
        reentrantToken.approve(address(reentrantRegistry), stakeAmount);

        vm.expectRevert(MinerRegistry.ReentrantCall.selector);

        vm.prank(miner);
        reentrantRegistry.stake();
    }

    function testFinalizeUnstake_RevertsOnReentrancy() public {
        ReentrantEmergencyCENT reentrantToken = new ReentrantEmergencyCENT();
        MinerRegistry reentrantRegistry = new MinerRegistry(address(reentrantToken), timelock);

        reentrantToken.mint(miner, stakeAmount);

        vm.prank(miner);
        reentrantToken.approve(address(reentrantRegistry), stakeAmount);

        vm.prank(miner);
        reentrantRegistry.stake();

        vm.prank(miner);
        reentrantRegistry.requestUnstake();

        vm.warp(block.timestamp + reentrantRegistry.UNSTAKE_DELAY());

        reentrantToken.setRegistry(address(reentrantRegistry));

        vm.expectRevert(MinerRegistry.ReentrantCall.selector);

        vm.prank(miner);
        reentrantRegistry.finalizeUnstake();
    }
}
