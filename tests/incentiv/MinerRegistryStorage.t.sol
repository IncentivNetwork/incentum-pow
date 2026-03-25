// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "forge-std/Test.sol";

import "../../contracts/incentiv/MinerRegistry.sol";
import "../../contracts/incentiv/mocks/MockCENT.sol";

contract MinerRegistryStorageTest is Test {
    uint256 internal constant STAKE_AMOUNT = 100_000_000 * 10 ** 18;

    MinerRegistry internal registry;
    MockCENT internal cent;

    address internal miner = address(0xBEEF);
    address internal timelock = address(0xCAFE);

    function setUp() public {
        cent = new MockCENT();
        registry = new MinerRegistry(address(cent), timelock);

        cent.mint(miner, STAKE_AMOUNT);

        vm.prank(miner);
        cent.approve(address(registry), STAKE_AMOUNT);
    }

    function testConsensusStorageSlots_AreStable() public {
        vm.prank(miner);
        registry.stake();

        bytes32 minersSlot = _mappingSlot(miner, 0);
        bytes32 stakeTimeSlot = _mappingSlot(miner, 1);
        bytes32 stakeBlockSlot = _mappingSlot(miner, 2);
        bytes32 unstakeRequestTimeSlot = _mappingSlot(miner, 3);

        bytes32 rawMiner = vm.load(address(registry), minersSlot);
        bytes32 rawStakeTime = vm.load(address(registry), stakeTimeSlot);
        bytes32 rawStakeBlock = vm.load(address(registry), stakeBlockSlot);
        bytes32 rawUnstakeRequestTime = vm.load(address(registry), unstakeRequestTimeSlot);

        assertEq(uint256(rawMiner), 1);
        assertEq(uint256(rawStakeTime), block.timestamp);
        assertEq(uint256(rawStakeBlock), block.number);
        assertEq(uint256(rawUnstakeRequestTime), 0);
    }

    function _mappingSlot(address key, uint256 slot) internal pure returns (bytes32) {
        return keccak256(abi.encode(key, slot));
    }
}