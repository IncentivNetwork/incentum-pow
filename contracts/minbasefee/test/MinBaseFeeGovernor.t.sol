// SPDX-License-Identifier: LGPL-3.0-only
pragma solidity ^0.8.0;

import "forge-std/Test.sol";
import "../MinBaseFeeGovernor.sol";

contract MinBaseFeeGovernorTest is Test {
    MinBaseFeeGovernor public governor;

    address governance = address(0x1);
    address attacker = address(0x2);

    uint256 initialMinBaseFee = 12600 gwei;
    uint256 activationBlock = 0;

    event MinBaseFeeProposed(
        bytes32 indexed proposalId,
        uint256 minBaseFee,
        uint256 activationBlock,
        uint256 proposedAt,
        uint256 executeAfter
    );

    event ProposalExecuted(
        bytes32 indexed proposalId,
        uint256 minBaseFee,
        uint256 activationBlock
    );

    event ProposalCancelled(bytes32 indexed proposalId);

    event MinBaseFeeScheduled(
        uint256 indexed configIndex,
        uint256 minBaseFee,
        uint256 activationBlock,
        uint256 timestamp
    );

    event TimelockDelayUpdated(uint256 oldDelay, uint256 newDelay);

    function setUp() public {
        vm.prank(governance);
        governor = new MinBaseFeeGovernor(governance, initialMinBaseFee, activationBlock, 2 days, 13000);
    }

    // ========== Basic State Tests ==========

    function test_InitialState() public {
        assertEq(governor.governance(), governance);
        assertEq(governor.getCurrentMinBaseFee(), initialMinBaseFee);
        assertEq(governor.getConfigHistoryLength(), 1);
        assertEq(governor.timelockDelay(), 2 days);
    }

    function test_GetMinBaseFeeForBlock() public {
        uint256 minBaseFee = governor.getMinBaseFeeForBlock(100);
        assertEq(minBaseFee, initialMinBaseFee);
    }

    function test_ConstructorEmitsEvents() public {
        vm.expectEmit(true, false, false, true);
        emit MinBaseFeeScheduled(0, initialMinBaseFee, activationBlock, block.timestamp);

        vm.prank(governance);
        new MinBaseFeeGovernor(governance, initialMinBaseFee, activationBlock, 2 days, 13000);
    }

    // ========== Timelock Tests ==========

    function test_CannotExecuteProposalImmediately() public {
        vm.startPrank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 newActivationBlock = block.number + 20000;

        bytes32 proposalId = governor.proposeMinBaseFee(newMinBaseFee, newActivationBlock);

        vm.expectRevert();
        governor.executeProposal(proposalId);

        vm.stopPrank();
    }

    function test_CanExecuteProposalAfterTimelock() public {
        vm.startPrank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 newActivationBlock = block.number + 20000;

        bytes32 proposalId = governor.proposeMinBaseFee(newMinBaseFee, newActivationBlock);

        vm.warp(block.timestamp + 2 days + 1);

        governor.executeProposal(proposalId);

        assertEq(governor.getConfigHistoryLength(), 2);

        vm.stopPrank();
    }

    function test_CannotProposeActivationTooSoon() public {
        vm.prank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 tooSoonBlock = block.number + 100;

        vm.expectRevert();
        governor.proposeMinBaseFee(newMinBaseFee, tooSoonBlock);
    }

    function test_ProposeEmitsCorrectEvent() public {
        vm.prank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 newActivationBlock = block.number + 20000;

        uint256 executeAfter = block.timestamp + 2 days;

        vm.expectEmit(false, false, false, true);
        emit MinBaseFeeProposed(
            bytes32(0), // proposalId - will be computed, so we skip checking it
            newMinBaseFee,
            newActivationBlock,
            block.timestamp,
            executeAfter
        );

        governor.proposeMinBaseFee(newMinBaseFee, newActivationBlock);
    }

    function test_ExecuteEmitsCorrectEvent() public {
        vm.startPrank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 newActivationBlock = block.number + 20000;

        bytes32 proposalId = governor.proposeMinBaseFee(newMinBaseFee, newActivationBlock);

        vm.warp(block.timestamp + 2 days + 1);

        vm.expectEmit(true, false, false, true);
        emit ProposalExecuted(proposalId, newMinBaseFee, newActivationBlock);

        governor.executeProposal(proposalId);

        vm.stopPrank();
    }

    // ========== Cancel Proposal Tests ==========

    function test_CanCancelProposal() public {
        vm.startPrank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 newActivationBlock = block.number + 20000;

        bytes32 proposalId = governor.proposeMinBaseFee(newMinBaseFee, newActivationBlock);

        vm.expectEmit(true, false, false, true);
        emit ProposalCancelled(proposalId);

        governor.cancelProposal(proposalId);

        vm.warp(block.timestamp + 2 days + 1);
        vm.expectRevert();
        governor.executeProposal(proposalId);

        vm.stopPrank();
    }

    function test_CannotCancelNonExistentProposal() public {
        vm.prank(governance);

        bytes32 fakeProposalId = keccak256("fake");
        vm.expectRevert();
        governor.cancelProposal(fakeProposalId);
    }

    function test_CannotCancelExecutedProposal() public {
        vm.startPrank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 newActivationBlock = block.number + 20000;

        bytes32 proposalId = governor.proposeMinBaseFee(newMinBaseFee, newActivationBlock);

        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(proposalId);

        vm.expectRevert();
        governor.cancelProposal(proposalId);

        vm.stopPrank();
    }

    // ========== Access Control Tests ==========

    function test_OnlyGovernanceCanPropose() public {
        vm.prank(attacker);

        vm.expectRevert();
        governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
    }

    function test_OnlyGovernanceCanExecute() public {
        vm.prank(governance);
        bytes32 proposalId = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);

        vm.warp(block.timestamp + 2 days + 1);

        vm.prank(attacker);
        vm.expectRevert();
        governor.executeProposal(proposalId);
    }

    function test_OnlyGovernanceCanCancel() public {
        vm.prank(governance);
        bytes32 proposalId = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);

        vm.prank(attacker);
        vm.expectRevert();
        governor.cancelProposal(proposalId);
    }

    function test_OnlyGovernanceCanSetTimelockDelay() public {
        vm.prank(attacker);
        vm.expectRevert();
        governor.setTimelockDelay(3 days);
    }

    function test_OnlyGovernanceCanTransferGovernance() public {
        vm.prank(attacker);
        vm.expectRevert();
        governor.transferGovernance(attacker);
    }

    // ========== Safety Bounds Tests ==========

    function test_CannotProposeZeroMinBaseFee() public {
        vm.prank(governance);

        vm.expectRevert();
        governor.proposeMinBaseFee(0, block.number + 20000);
    }

    function test_CannotProposeTooLargeMinBaseFee() public {
        vm.prank(governance);

        uint256 tooLarge = 101 ether;
        vm.expectRevert();
        governor.proposeMinBaseFee(tooLarge, block.number + 20000);
    }

    function test_CannotProposeTooSmallMinBaseFee() public {
        vm.prank(governance);

        uint256 tooSmall = 0.5 gwei;
        vm.expectRevert();
        governor.proposeMinBaseFee(tooSmall, block.number + 20000);
    }

    function test_CanProposeAtMinBound() public {
        // Test that we can propose values approaching the minimum bound
        // through multiple steps (respecting 3x max change constraint)
        vm.prank(governance);
        bytes32 proposalId1 = governor.proposeMinBaseFee(4200 gwei, block.number + 20000); // 12600/3 = min allowed

        vm.warp(block.timestamp + 2 days + 1);
        vm.roll(block.number + 34561);

        vm.prank(governance);
        governor.executeProposal(proposalId1);

        // Verify the new value is active
        assertEq(governor.getMinBaseFeeForBlock(block.number), 4200 gwei);

        // Now we can propose even lower
        vm.prank(governance);
        bytes32 proposalId2 = governor.proposeMinBaseFee(1400 gwei, block.number + 40000); // 4200/3 = 1400

        vm.warp(block.timestamp + 2 days + 1);
        vm.roll(block.number + 40000 + 1);

        vm.prank(governance);
        governor.executeProposal(proposalId2);

        assertEq(governor.getMinBaseFeeForBlock(block.number), 1400 gwei);
    }

    function test_CanProposeAtMaxBound() public {
        vm.prank(governance);

        uint256 maxAllowed = 12600 gwei * 3;
        governor.proposeMinBaseFee(maxAllowed, block.number + 20000);
    }

    function test_CannotProposeChangeTooLarge() public {
        vm.prank(governance);

        uint256 tooLargeChange = 12600 gwei * 4;

        vm.expectRevert();
        governor.proposeMinBaseFee(tooLargeChange, block.number + 20000);
    }

    function test_CannotProposeChangeTooSmall() public {
        vm.prank(governance);

        uint256 tooSmallChange = 12600 gwei / 4;

        vm.expectRevert();
        governor.proposeMinBaseFee(tooSmallChange, block.number + 20000);
    }

    function test_CannotProposeNonSequentialActivation() public {
        vm.startPrank(governance);

        bytes32 p1 = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(p1);

        vm.expectRevert();
        governor.proposeMinBaseFee(18000 gwei, block.number + 15000);

        vm.stopPrank();
    }

    // ========== Timelock Delay Tests ==========

    function test_CanUpdateTimelockDelay() public {
        vm.prank(governance);

        uint256 newDelay = 3 days;

        vm.expectEmit(false, false, false, true);
        emit TimelockDelayUpdated(2 days, newDelay);

        governor.setTimelockDelay(newDelay);

        assertEq(governor.timelockDelay(), newDelay);
    }

    function test_CannotSetTimelockDelayBelowMinimum() public {
        vm.prank(governance);

        vm.expectRevert();
        governor.setTimelockDelay(1 days);
    }

    function test_UpdatedTimelockDelayAffectsNewProposals() public {
        vm.startPrank(governance);

        governor.setTimelockDelay(3 days);

        bytes32 proposalId = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);

        vm.warp(block.timestamp + 2 days + 1);
        vm.expectRevert();
        governor.executeProposal(proposalId);

        vm.warp(block.timestamp + 1 days);
        governor.executeProposal(proposalId);

        vm.stopPrank();
    }

    // ========== Binary Search Tests ==========

    function test_BinarySearchWithMultipleConfigs() public {
        vm.startPrank(governance);

        bytes32 p1 = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(p1);

        bytes32 p2 = governor.proposeMinBaseFee(18000 gwei, block.number + 40000);
        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(p2);

        bytes32 p3 = governor.proposeMinBaseFee(20000 gwei, block.number + 60000);
        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(p3);

        assertEq(governor.getMinBaseFeeForBlock(10000), 12600 gwei);
        assertEq(governor.getMinBaseFeeForBlock(25000), 15000 gwei);
        assertEq(governor.getMinBaseFeeForBlock(45000), 18000 gwei);
        assertEq(governor.getMinBaseFeeForBlock(70000), 20000 gwei);

        vm.stopPrank();
    }

    function test_BinarySearchEdgeCases() public {
        vm.startPrank(governance);

        bytes32 p1 = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(p1);

        assertEq(governor.getMinBaseFeeForBlock(0), 12600 gwei);

        assertEq(governor.getMinBaseFeeForBlock(block.number + 19999), 12600 gwei);
        assertEq(governor.getMinBaseFeeForBlock(block.number + 20000), 15000 gwei);

        vm.stopPrank();
    }

    // ========== GetProposal Tests ==========

    function test_GetProposalReturnsCorrectData() public {
        vm.prank(governance);

        uint256 newMinBaseFee = 15000 gwei;
        uint256 newActivationBlock = block.number + 20000;

        bytes32 proposalId = governor.proposeMinBaseFee(newMinBaseFee, newActivationBlock);

        (
            uint256 minBaseFee,
            uint256 activationBlock,
            uint256 proposedAt,
            bool executed,
            uint256 executeAfter,
            bool canExecute
        ) = governor.getProposal(proposalId);

        assertEq(minBaseFee, newMinBaseFee);
        assertEq(activationBlock, newActivationBlock);
        assertEq(proposedAt, block.timestamp);
        assertFalse(executed);
        assertEq(executeAfter, block.timestamp + 2 days);
        assertFalse(canExecute);
    }

    function test_GetProposalCanExecuteAfterTimelock() public {
        vm.prank(governance);

        bytes32 proposalId = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);

        vm.warp(block.timestamp + 2 days + 1);

        (, , , , , bool canExecute) = governor.getProposal(proposalId);
        assertTrue(canExecute);
    }

    function test_GetProposalAfterExecution() public {
        vm.startPrank(governance);

        bytes32 proposalId = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);

        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(proposalId);

        (, , , bool executed, , bool canExecute) = governor.getProposal(proposalId);
        assertTrue(executed);
        assertFalse(canExecute);

        vm.stopPrank();
    }

    // ========== Helper Function Tests ==========

    function test_GetConfigHistoryLength() public {
        vm.startPrank(governance);

        assertEq(governor.getConfigHistoryLength(), 1);

        bytes32 p1 = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(p1);

        assertEq(governor.getConfigHistoryLength(), 2);

        vm.stopPrank();
    }

    function test_GetConfigByIndex() public {
        MinBaseFeeGovernor.MinBaseFeeConfig memory config = governor.getConfigByIndex(0);

        assertEq(config.minBaseFee, initialMinBaseFee);
        assertEq(config.activationBlock, activationBlock);
    }

    function test_GetConfigByIndexOutOfBounds() public {
        vm.expectRevert();
        governor.getConfigByIndex(999);
    }

    function test_GetAllConfigs() public {
        vm.startPrank(governance);

        bytes32 p1 = governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
        vm.warp(block.timestamp + 2 days + 1);
        governor.executeProposal(p1);

        MinBaseFeeGovernor.MinBaseFeeConfig[] memory configs = governor.getAllConfigs();

        assertEq(configs.length, 2);
        assertEq(configs[0].minBaseFee, initialMinBaseFee);
        assertEq(configs[1].minBaseFee, 15000 gwei);

        vm.stopPrank();
    }

    // ========== Governance Transfer Tests ==========

    function test_CanTransferGovernance() public {
        vm.prank(governance);

        address newGovernance = address(0x3);
        governor.transferGovernance(newGovernance);

        assertEq(governor.governance(), newGovernance);
    }

    function test_CannotTransferToZeroAddress() public {
        vm.prank(governance);

        vm.expectRevert();
        governor.transferGovernance(address(0));
    }

    function test_OldGovernanceCannotProposeAfterTransfer() public {
        vm.prank(governance);

        address newGovernance = address(0x3);
        governor.transferGovernance(newGovernance);

        vm.prank(governance);
        vm.expectRevert();
        governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
    }

    function test_NewGovernanceCanProposeAfterTransfer() public {
        vm.prank(governance);

        address newGovernance = address(0x3);
        governor.transferGovernance(newGovernance);

        vm.prank(newGovernance);
        governor.proposeMinBaseFee(15000 gwei, block.number + 20000);
    }
}
