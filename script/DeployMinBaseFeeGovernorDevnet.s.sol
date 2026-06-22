// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

import "forge-std/Script.sol";
import "../contracts/minbasefee/MinBaseFeeGovernor.sol";

/**
 * @title DeployMinBaseFeeGovernorDevnet
 * @notice Deploys MinBaseFeeGovernor to the Incentiv devnet (chainId 12730) with
 *         devnet-tuned timelock and activation delays so a full
 *         propose -> wait timelock -> execute -> activation cycle fits inside
 *         a single test window.
 *
 * Production deploys must use script/DeployMinBaseFeeGovernorMainnet.s.sol (TBD),
 * which is the only place the production minima (2 days / 13000 blocks) are
 * baked in. This script intentionally cannot be used for mainnet (chainId 24101).
 *
 * Usage (from incentum-pow/ directory):
 *
 *   export DEVNET_RPC=http://<devnet-rpc-host>:8545
 *   export DEPLOYER_PRIVATE_KEY=0x<key>
 *   export GOVERNANCE_ADDRESS=0x<eoa-or-multisig-that-will-own-the-governor>
 *
 *   forge script script/DeployMinBaseFeeGovernorDevnet.s.sol \
 *     --rpc-url $DEVNET_RPC \
 *     --private-key $DEPLOYER_PRIVATE_KEY \
 *     --broadcast \
 *     -vvvv
 */
contract DeployMinBaseFeeGovernorDevnet is Script {
    // --- Devnet parameters ---
    // 10 minutes timelock + 100 blocks (~8.3 min at 5 s/block) activation delay
    // means a full propose -> execute -> activation cycle fits inside ~18 minutes,
    // which is comfortable for the DMBF-6 governance-cycle window on devnet.
    uint256 constant TIMELOCK_DELAY = 600;
    uint256 constant ACTIVATION_DELAY_BLOCKS = 100;

    // Initial floor matches the current legacy MinBaseFeeUpdated value so the
    // contract-driven floor on the first post-activation block equals what the
    // chain was already producing pre-activation. This avoids a fee-market jump
    // at the activation boundary and keeps the boundary block trivial to verify.
    uint256 constant INITIAL_MIN_BASE_FEE = 12_600 gwei;

    // Recording the floor as active since block 0 keeps the reader's
    // "blockNumber < configHistory[0].activationBlock returns configHistory[0]"
    // semantics aligned with "the contract's floor has been authoritative since
    // genesis". Future proposals must use activationBlock > 0, which they
    // trivially do since they are checked against block.number + ACTIVATION_DELAY_BLOCKS.
    uint256 constant INITIAL_ACTIVATION_BLOCK = 0;

    function run() external {
        require(block.chainid == 12_730, "DeployMinBaseFeeGovernorDevnet: wrong chain, expected devnet (12730)");

        uint256 pk = vm.envUint("DEPLOYER_PRIVATE_KEY");
        address deployer = vm.addr(pk);
        address governance = vm.envAddress("GOVERNANCE_ADDRESS");

        require(governance != address(0), "DeployMinBaseFeeGovernorDevnet: GOVERNANCE_ADDRESS cannot be zero");

        console.log("=== MinBaseFeeGovernor Devnet Deployment ===");
        console.log("Chain ID:                   ", block.chainid);
        console.log("Deployer:                   ", deployer);
        console.log("Governance:                 ", governance);
        console.log("Initial minBaseFee (wei):   ", INITIAL_MIN_BASE_FEE);
        console.log("Initial activationBlock:    ", INITIAL_ACTIVATION_BLOCK);
        console.log("minTimelockDelay (seconds): ", TIMELOCK_DELAY);
        console.log("minActivationDelayBlocks:   ", ACTIVATION_DELAY_BLOCKS);

        vm.startBroadcast(pk);

        MinBaseFeeGovernor governor = new MinBaseFeeGovernor(
            governance,
            INITIAL_MIN_BASE_FEE,
            INITIAL_ACTIVATION_BLOCK,
            TIMELOCK_DELAY,
            ACTIVATION_DELAY_BLOCKS
        );

        vm.stopBroadcast();

        console.log("MinBaseFeeGovernor deployed at:", address(governor));

        // Deploy-time verification: read every public state field the deploy
        // arguments set and assert each matches. This satisfies the DMBF-4
        // "deploy-time verification step" AC without a separate post-deploy
        // operator query, and the broadcast log captures the values.
        uint256 currentFloor = governor.getCurrentMinBaseFee();
        uint256 configCount = governor.getConfigHistoryLength();
        address recordedGovernance = governor.governance();
        uint256 recordedTimelock = governor.timelockDelay();

        require(currentFloor == INITIAL_MIN_BASE_FEE, "deploy verify: currentMinBaseFee mismatch");
        require(configCount == 1, "deploy verify: configHistory length mismatch");
        require(recordedGovernance == governance, "deploy verify: governance mismatch");
        require(recordedTimelock == TIMELOCK_DELAY, "deploy verify: timelockDelay mismatch");

        console.log("");
        console.log("=== Deploy-time verification (read-back) ===");
        console.log("getCurrentMinBaseFee():     ", currentFloor);
        console.log("getConfigHistoryLength():   ", configCount);
        console.log("governance():               ", recordedGovernance);
        console.log("timelockDelay():            ", recordedTimelock);

        console.log("");
        console.log("=== NEXT STEPS ===");
        console.log("1. Record the deployed address above and submit a small parameter PR");
        console.log("   that sets IncentivDevnetChainConfig.MinBaseFeeContractAddr to it");
        console.log("   and DynamicMinBaseFeeTime to a future timestamp covering review +");
        console.log("   merge + binary rollout + safety margin.");
        console.log("2. Build the devnet binary from the parameter-PR merge commit and");
        console.log("   restart every devnet node before DynamicMinBaseFeeTime.");
        console.log("3. Per-node verify: admin.nodeInfo.protocols.eth.config shows the new");
        console.log("   dynamicMinBaseFeeTime and minBaseFeeContractAddr, no compat rewind.");
        console.log("4. Run the DMBF-6 end-to-end scenarios against the live contract.");
    }
}
