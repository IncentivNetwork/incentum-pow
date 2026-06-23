// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

import "forge-std/Script.sol";
import "../contracts/minbasefee/MinBaseFeeGovernor.sol";

/**
 * @title DeployMinBaseFeeGovernorMainnet
 * @notice Deploys MinBaseFeeGovernor to the Incentiv mainnet (chainId 24101).
 *
 * Hardcoded production constants — no env-driven overrides — to make the
 * broadcast artifact and the script source the canonical record of what was
 * deployed. Sister script for devnet:
 * script/DeployMinBaseFeeGovernorDevnet.s.sol.
 *
 * Parameters
 * - Governance: the existing DPoW Governance Safe at
 *   0x10D9dEEb09bA23b2bD9739F698b3dFa9D8F95Ad4. The Safe was deployed and
 *   hardened during DPOW-008-3 (CANCELLER_ROLE granted to Guardian Safe,
 *   revoked from Governance Safe, DEFAULT_ADMIN_ROLE renounced by deployer).
 *   Reuse rationale: same operational signers and same key custody govern
 *   both consensus-critical features (DPoW miner authorization and the
 *   dynamic min base fee); splitting custody into a second Safe would add
 *   operational overhead without reducing real risk of compromise.
 * - Initial floor: 12 600 gwei = `MinBaseFeeUpdated` constant. Matches the
 *   current legacy floor on mainnet (`MinBaseFeeBlock = 1_295_000` and
 *   `MinBaseFeeChangeHeight = 1_582_500` are both far behind head), so
 *   activation triggers zero fee-market jump.
 * - Activation block 0: contract reader returns configHistory[0] for any
 *   block query before the first proposed activation, which is the
 *   "active since genesis" semantic. Future proposals will use real
 *   activationBlock values constrained by MIN_ACTIVATION_DELAY_BLOCKS.
 * - Timelock 24 h: deliberately tighter than the 2-day industry default
 *   (Compound / Aave / Uniswap) and looser than the 12 h business
 *   preference, calibrated for incident response by a global multisig
 *   without losing weekend / off-hours coverage of the reaction window.
 * - Activation delay 13 000 blocks: ~18 h at 5 s/block, sufficient lead
 *   time for the chain to observe a pending change before it activates,
 *   independent of timelock.
 *
 * Deploy
 *
 *   export MAINNET_RPC=https://rpc.incentiv.io
 *
 *   forge script script/DeployMinBaseFeeGovernorMainnet.s.sol \
 *     --rpc-url $MAINNET_RPC \
 *     --ledger --sender 0xd2CC08D9AFaBb57BdF2216ED15fceaa9993F3B7b \
 *     --broadcast -vvvv
 *
 * The contract address is determined by CREATE(deployer, nonce) — record it
 * from the broadcast log; it goes into IncentivMainnetChainConfig.
 * MinBaseFeeContractAddr in a follow-up parameter PR.
 */
contract DeployMinBaseFeeGovernorMainnet is Script {
    // --- Mainnet identity ---
    uint256 constant MAINNET_CHAIN_ID = 24_101;
    address constant DEPLOYER = 0xd2CC08D9AFaBb57BdF2216ED15fceaa9993F3B7b;
    address constant GOVERNANCE_SAFE = 0x10D9dEEb09bA23b2bD9739F698b3dFa9D8F95Ad4;

    // --- Production parameters ---
    uint256 constant TIMELOCK_DELAY = 24 hours;          // 86 400 s
    uint256 constant ACTIVATION_DELAY_BLOCKS = 13_000;    // ~18 h at 5 s/block

    // Initial floor matches the current legacy MinBaseFeeUpdated constant.
    // Zero fee-market jump at activation.
    uint256 constant INITIAL_MIN_BASE_FEE = 12_600 gwei;

    // Active since genesis semantics (see header docstring).
    uint256 constant INITIAL_ACTIVATION_BLOCK = 0;

    function run() external {
        require(block.chainid == MAINNET_CHAIN_ID, "DeployMinBaseFeeGovernorMainnet: wrong chain, expected mainnet (24101)");
        require(msg.sender == DEPLOYER, "DeployMinBaseFeeGovernorMainnet: --sender must equal the configured DEPLOYER constant");
        require(GOVERNANCE_SAFE.code.length > 0, "DeployMinBaseFeeGovernorMainnet: GOVERNANCE_SAFE address has no code on this chain");

        console.log("=== MinBaseFeeGovernor Mainnet Deployment ===");
        console.log("Chain ID:                   ", block.chainid);
        console.log("Deployer:                   ", DEPLOYER);
        console.log("Governance Safe (existing): ", GOVERNANCE_SAFE);
        console.log("Initial minBaseFee (wei):   ", INITIAL_MIN_BASE_FEE);
        console.log("Initial activationBlock:    ", INITIAL_ACTIVATION_BLOCK);
        console.log("minTimelockDelay (seconds): ", TIMELOCK_DELAY);
        console.log("minActivationDelayBlocks:   ", ACTIVATION_DELAY_BLOCKS);

        vm.startBroadcast();

        MinBaseFeeGovernor governor = new MinBaseFeeGovernor(
            GOVERNANCE_SAFE,
            INITIAL_MIN_BASE_FEE,
            INITIAL_ACTIVATION_BLOCK,
            TIMELOCK_DELAY,
            ACTIVATION_DELAY_BLOCKS
        );

        vm.stopBroadcast();

        console.log("MinBaseFeeGovernor deployed at:", address(governor));

        // Deploy-time verification: read back every constructor-determined
        // value (public state, both immutables, and the seeded configHistory[0])
        // and require-match. Catches argument transposition or storage layout
        // drift early — fails the broadcast atomically rather than emitting a
        // mismatched contract into IncentivMainnetChainConfig.
        uint256 currentFloor = governor.getCurrentMinBaseFee();
        uint256 configCount = governor.getConfigHistoryLength();
        address recordedGovernance = governor.governance();
        uint256 recordedTimelock = governor.timelockDelay();
        uint256 minTimelockImmutable = governor.MIN_TIMELOCK_DELAY();
        uint256 minActivationImmutable = governor.MIN_ACTIVATION_DELAY_BLOCKS();
        (uint256 initialMinBaseFee, uint256 initialActivationBlock,) = governor.configHistory(0);

        require(currentFloor == INITIAL_MIN_BASE_FEE, "deploy verify: currentMinBaseFee mismatch");
        require(configCount == 1, "deploy verify: configHistory length mismatch");
        require(recordedGovernance == GOVERNANCE_SAFE, "deploy verify: governance mismatch");
        require(recordedTimelock == TIMELOCK_DELAY, "deploy verify: timelockDelay mismatch");
        require(minTimelockImmutable == TIMELOCK_DELAY, "deploy verify: MIN_TIMELOCK_DELAY mismatch");
        require(minActivationImmutable == ACTIVATION_DELAY_BLOCKS, "deploy verify: MIN_ACTIVATION_DELAY_BLOCKS mismatch");
        require(initialMinBaseFee == INITIAL_MIN_BASE_FEE, "deploy verify: configHistory[0].minBaseFee mismatch");
        require(initialActivationBlock == INITIAL_ACTIVATION_BLOCK, "deploy verify: configHistory[0].activationBlock mismatch");

        console.log("");
        console.log("=== Deploy-time verification (read-back) ===");
        console.log("getCurrentMinBaseFee():       ", currentFloor);
        console.log("getConfigHistoryLength():     ", configCount);
        console.log("governance():                 ", recordedGovernance);
        console.log("timelockDelay():              ", recordedTimelock);
        console.log("MIN_TIMELOCK_DELAY():         ", minTimelockImmutable);
        console.log("MIN_ACTIVATION_DELAY_BLOCKS():", minActivationImmutable);
        console.log("configHistory[0].minBaseFee: ", initialMinBaseFee);
        console.log("configHistory[0].activate:   ", initialActivationBlock);

        console.log("");
        console.log("=== NEXT STEPS ===");
        console.log("1. Record the deployed address above and add it to");
        console.log("   IncentivMainnetChainConfig.MinBaseFeeContractAddr in");
        console.log("   params/config.go, together with DynamicMinBaseFeeTime set");
        console.log("   to the agreed near-future activation timestamp.");
        console.log("2. Pin both values in params/config_test.go");
        console.log("   (TestIncentivNetworkDynamicMinBaseFeeValues).");
        console.log("3. Merge the parameter PR; build the mainnet binary from the");
        console.log("   merge commit; restart every mainnet node before activation.");
        console.log("4. Per-node verification + end-to-end validation under the");
        console.log("   mainnet rollout sub-issue.");
    }
}
