// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

interface IMinerRegistryLike {
    function stake() external;
}

contract ReentrantCENT is ERC20 {
    address public registry;
    bool internal entered;

    constructor() ERC20("Reentrant CENT", "rCENT") { }

    function setRegistry(address registry_) external {
        registry = registry_;
    }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }

    function transferFrom(address from, address to, uint256 value) public override returns (bool) {
        if (!entered && registry != address(0)) {
            entered = true;
            IMinerRegistryLike(registry).stake();
            entered = false;
        }

        return super.transferFrom(from, to, value);
    }
}
