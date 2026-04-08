// SPDX-License-Identifier: GPL-3.0-or-later
pragma solidity 0.8.23;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";

contract WCENT is ERC20, ReentrancyGuard {
    constructor() ERC20("Wrapped CENT", "WCENT") { }

    /**
     * @dev Deposit CENT and mint WCENT 1:1 to msg.sender.
     * msg.value is the amount of CENT deposited.
     */
    function deposit() external payable {
        require(msg.value > 0, "Deposit amount must be greater than zero");

        _mint(msg.sender, msg.value);
    }

    /**
     * @dev Withdraw CENT by burning WCENT 1:1 from msg.sender.
     * @param amount The amount of WCENT to burn and withdraw as CENT.
     */
    function withdraw(uint256 amount) external nonReentrant {
        require(amount > 0, "Withdraw amount must be greater than zero");

        _burn(msg.sender, amount);

        (bool success,) = payable(msg.sender).call{ value: amount }("");
        require(success, "CENT transfer failed");
    }

    receive() external payable {
        revert("Direct CENT transfers not allowed");
    }

    fallback() external payable {
        revert("Fallback function not allowed");
    }
}
