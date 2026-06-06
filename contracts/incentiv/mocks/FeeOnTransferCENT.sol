// SPDX-License-Identifier: MIT
pragma solidity ^0.8.23;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

contract FeeOnTransferCENT is ERC20 {
    uint256 internal constant FEE = 1;

    constructor() ERC20("Fee On Transfer CENT", "fCENT") { }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }

    function transferFrom(address from, address to, uint256 value) public override returns (bool) {
        _spendAllowance(from, _msgSender(), value);

        uint256 receivedAmount = value - FEE;

        _update(from, to, receivedAmount);
        _update(from, address(0), FEE);

        return true;
    }
}
