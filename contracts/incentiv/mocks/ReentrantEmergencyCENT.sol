// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

interface IMinerRegistryFinalizeLike {
    function finalizeUnstake() external;
}

contract ReentrantEmergencyCENT is ERC20 {
    address public registry;
    bool internal entered;

    constructor() ERC20("Reentrant Emergency CENT", "reCENTE") { }

    function setRegistry(address registry_) external {
        registry = registry_;
    }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }

    function transfer(address to, uint256 value) public override returns (bool) {
        if (!entered && registry != address(0)) {
            entered = true;
            IMinerRegistryFinalizeLike(registry).finalizeUnstake();
            entered = false;
        }

        return super.transfer(to, value);
    }
}
