// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

import "forge-std/Script.sol";
import "@openzeppelin/contracts/governance/TimelockController.sol";
import "../contracts/incentiv/WCENT.sol";
import "../contracts/incentiv/MinerRegistry.sol";

/**
 * @title DeployDevnet
 * @notice Deploys WCENT, TimelockController, and MinerRegistry to the Incentiv devnet (chainId 12730).
 *
 * Usage (from incentum-pow/ directory):
 *
 *   export DEVNET_RPC=http://<devnet-rpc-host>:8545
 *   export DEPLOYER_PRIVATE_KEY=0x<key>
 *   export MINER_ADDRESS=0x<miner-coinbase>
 *   export GOVERNANCE_ADDRESS=0x<multisig-or-deployer-for-devnet>
 *
 *   forge script script/DeployDevnet.s.sol \
 *     --rpc-url $DEVNET_RPC \
 *     --private-key $DEPLOYER_PRIVATE_KEY \
 *     --broadcast \
 *     -vvvv
 */
contract DeployDevnet is Script {
    uint256 constant TIMELOCK_DELAY = 60; // 60 s for devnet (vs 2–7 days on mainnet)
    uint256 constant MATURITY_TIME = 300; // 5 min for devnet (vs 24 h on mainnet)
    uint256 constant MATURITY_BLOCKS = 60; // ~5 min at 5 s/block (vs 17 280 on mainnet)
    uint256 constant UNSTAKE_DELAY = 600; // 10 min for devnet (vs 7 days on mainnet)
    uint256 constant STAKE_AMOUNT = 100_000_000 * 10 ** 18;
    uint256 constant MINER_GAS_FUND = 10 ether; // native CENT sent to miner for gas

    function run() external {
        address deployer = vm.addr(vm.envUint("DEPLOYER_PRIVATE_KEY"));
        address miner = vm.envAddress("MINER_ADDRESS");
        address governance = vm.envAddress("GOVERNANCE_ADDRESS");

        console.log("=== DPoW Devnet Deployment ===");
        console.log("Deployer:   ", deployer);
        console.log("Miner:      ", miner);
        console.log("Governance: ", governance);
        console.log("Chain ID:   ", block.chainid);

        vm.startBroadcast(vm.envUint("DEPLOYER_PRIVATE_KEY"));

        // 1. Deploy WCENT
        WCENT wcent = new WCENT();
        console.log("WCENT deployed at:              ", address(wcent));

        // 2. Deploy TimelockController (devnet: 60s delay)
        address[] memory proposers = new address[](1);
        address[] memory executors = new address[](1);
        proposers[0] = governance;
        executors[0] = governance;

        TimelockController timelock = new TimelockController(
            TIMELOCK_DELAY,
            proposers,
            executors,
            address(0) // no admin — fully governed by proposers/executors
        );
        console.log("TimelockController deployed at: ", address(timelock));

        // 3. Deploy MinerRegistry with devnet-specific maturity and unstake parameters
        MinerRegistry registry = new MinerRegistry(
            address(wcent), address(timelock), MATURITY_TIME, MATURITY_BLOCKS, UNSTAKE_DELAY
        );
        console.log("MinerRegistry deployed at:      ", address(registry));

        // 4. Fund miner with native CENT for gas
        (bool sent,) = miner.call{ value: MINER_GAS_FUND }("");
        require(sent, "Failed to fund miner with gas");
        console.log("Funded miner with native CENT for gas: ", MINER_GAS_FUND);

        // 5. Wrap native CENT to WCENT for miner's stake (deployer wraps and sends)
        wcent.deposit{ value: STAKE_AMOUNT }();
        bool transferOk = wcent.transfer(miner, STAKE_AMOUNT);
        require(transferOk, "WCENT transfer to miner failed");
        console.log("Transferred WCENT stake to miner: ", STAKE_AMOUNT);

        vm.stopBroadcast();

        console.log("");
        console.log("=== NEXT STEPS ===");
        console.log("1. Update params/config.go MinerRegistryAddress to:", address(registry));
        console.log("2. Rebuild geth: make geth");
        console.log("3. Deploy new binary to server and restart incentum.service");
        console.log("4. Miner must call: WCENT.approve(MinerRegistry, STAKE_AMOUNT)");
        console.log("5. Miner must call: MinerRegistry.stake()");
        console.log("6. Wait 300s + 60 blocks for maturity");
        console.log("7. Mining with DPoW active!");
    }
}
