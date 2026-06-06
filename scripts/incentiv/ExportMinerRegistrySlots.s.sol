// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "forge-std/Script.sol";
import "forge-std/console2.sol";

contract ExportMinerRegistrySlots is Script {
    address internal constant MINER = address(0xBEEF);

    function run() external pure {
        console2.logBytes32(_mappingSlot(MINER, 0));
        console2.logBytes32(_mappingSlot(MINER, 1));
        console2.logBytes32(_mappingSlot(MINER, 2));
        console2.logBytes32(_mappingSlot(MINER, 3));
    }

    function _mappingSlot(address key, uint256 slot) internal pure returns (bytes32) {
        return keccak256(abi.encode(key, slot));
    }
}
