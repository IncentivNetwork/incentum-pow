// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract MockCENT is ERC20 {
    constructor() ERC20("Mock CENT", "CENT") { }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}
