// SPDX-License-Identifier: MIT
pragma solidity 0.8.23;

import "forge-std/Script.sol";
import "@openzeppelin/contracts/governance/TimelockController.sol";
import "../contracts/incentiv/MinerRegistry.sol";

/**
 * @title DeployMainnet
 * @notice Deploys TimelockController + MinerRegistry to Incentiv Mainnet (chain id 24101).
 *         Reuses the already-deployed mainnet WCENT at
 *         0xB0f0A14A50F14dc9e6476d61C00cF0375Dd4EB04 (no new WCENT is deployed).
 *
 * The TimelockController is deployed with:
 *   - A TEMPORARY 60-second minDelay (raised to the production 7 days via
 *     `updateDelay(604800)` after the integration tests in DPOW-008-3).
 *   - proposers = [Governance Safe]
 *   - executors = [address(0)] (open executor — anyone may execute() after the delay)
 *   - admin     = deployer EOA (temporary; renounced after manual hardening in DPOW-008-3)
 *
 * The script ALSO hardens CANCELLER_ROLE as part of the same script run. With
 * normal `forge script --broadcast`, the deploy and role changes are submitted
 * as sequential on-chain transactions, not one atomic operation; a mid-run revert
 * leaves partial state. The deployer holds DEFAULT_ADMIN_ROLE from the Timelock
 * deployment onward, so the grant/revoke transactions skip the timelock delay:
 *   - grantRole(CANCELLER_ROLE, Guardian Safe)
 *   - revokeRole(CANCELLER_ROLE, Governance Safe)
 *
 * After this script returns, the Timelock holds:
 *   PROPOSER_ROLE      -> Governance Safe (auto-granted in constructor)
 *   CANCELLER_ROLE     -> Guardian Safe   (granted here; auto-grant to Governance Safe revoked here)
 *   EXECUTOR_ROLE      -> address(0)      (open executor, auto-granted in constructor)
 *   DEFAULT_ADMIN_ROLE -> deployer EOA + the Timelock itself
 * The deployer's DEFAULT_ADMIN_ROLE is NOT renounced by the script — it must be
 * renounced manually in DPOW-008-3 after the integration tests succeed.
 *
 * Manual follow-up steps (cannot live in a script — they require multisig signatures
 * or a separate broadcast):
 *   A. Integration test 1: schedule -> cancel (Governance + Guardian)
 *   B. Integration test 2: schedule -> wait 60s -> execute (Governance + anyone)
 *   C. Governance Safe schedules `timelock.updateDelay(604800)`, wait 60s, execute
 *   D. Deployer: `renounceRole(DEFAULT_ADMIN_ROLE, deployer)`
 *   E. Re-verify the final role state on-chain (`hasRole` queries)
 *
 * Usage:
 *   forge script script/DeployMainnet.s.sol \
 *     --rpc-url $MAINNET_RPC \
 *     --ledger --sender 0xd2CC08D9AFaBb57BdF2216ED15fceaa9993F3B7b \
 *     --broadcast -vvvv
 */
contract DeployMainnet is Script {
    // --- Parameters (final mainnet values per DPoW spec §1.4) ---
    uint256 constant TIMELOCK_DELAY_TEMP = 60;           // temporary; raised to 7 days after integration tests
    uint256 constant MATURITY_TIME       = 86_400;       // 24 hours
    uint256 constant MATURITY_BLOCKS     = 17_280;       // ~24 hours at 5 s/block
    uint256 constant UNSTAKE_DELAY       = 604_800;      // 7 days

    // --- Mainnet addresses (chain 24101) ---
    address constant WCENT           = 0xB0f0A14A50F14dc9e6476d61C00cF0375Dd4EB04;
    address constant GOVERNANCE_SAFE = 0x10D9dEEb09bA23b2bD9739F698b3dFa9D8F95Ad4;
    address constant GUARDIAN_SAFE   = 0x482Fd68377310ec984bcA521e2020D89e1A93CBc;
    address constant DEPLOYER        = 0xd2CC08D9AFaBb57BdF2216ED15fceaa9993F3B7b;

    function run() external {
        require(block.chainid == 24_101, "DeployMainnet: wrong chain, expected Incentiv mainnet (24101)");
        require(msg.sender == DEPLOYER, "DeployMainnet: --sender must equal the configured DEPLOYER constant");

        // Sanity-check the hardcoded contract addresses are actual contracts (best-effort guard against typos).
        require(WCENT.code.length > 0,           "DeployMainnet: WCENT address has no code on this chain");
        require(GOVERNANCE_SAFE.code.length > 0, "DeployMainnet: GOVERNANCE_SAFE address has no code on this chain");
        require(GUARDIAN_SAFE.code.length > 0,   "DeployMainnet: GUARDIAN_SAFE address has no code on this chain");

        console.log("=== DPoW Mainnet Deployment ===");
        console.log("Chain ID:        ", block.chainid);
        console.log("Deployer:        ", DEPLOYER);
        console.log("WCENT (existing):", WCENT);
        console.log("Governance Safe: ", GOVERNANCE_SAFE);
        console.log("Guardian Safe:   ", GUARDIAN_SAFE);

        vm.startBroadcast();

        // 1. Deploy TimelockController with temporary 60s minDelay and the deployer as admin.
        address[] memory proposers = new address[](1);
        proposers[0] = GOVERNANCE_SAFE;
        address[] memory executors = new address[](1);
        executors[0] = address(0); // open executor

        TimelockController timelock = new TimelockController(
            TIMELOCK_DELAY_TEMP,
            proposers,
            executors,
            DEPLOYER
        );
        console.log("TimelockController deployed at: ", address(timelock));

        // 2. Harden CANCELLER_ROLE under the deployer's temporary DEFAULT_ADMIN_ROLE.
        //    These calls are instant — no timelock delay applies to admin-driven role changes.
        bytes32 cancellerRole = timelock.CANCELLER_ROLE();
        timelock.grantRole(cancellerRole, GUARDIAN_SAFE);
        timelock.revokeRole(cancellerRole, GOVERNANCE_SAFE);
        console.log("CANCELLER_ROLE granted to Guardian Safe, revoked from Governance Safe");

        // 3. Deploy MinerRegistry. The 5-argument constructor; STAKE_AMOUNT comes from the contract
        //    constant (26M after DPOW-008-1) and is not passed as a parameter.
        MinerRegistry registry = new MinerRegistry(
            WCENT,
            address(timelock),
            MATURITY_TIME,
            MATURITY_BLOCKS,
            UNSTAKE_DELAY
        );
        console.log("MinerRegistry deployed at:      ", address(registry));
        console.log("STAKE_AMOUNT (from contract):   ", registry.STAKE_AMOUNT());

        vm.stopBroadcast();

        console.log("");
        console.log("=== MANUAL NEXT STEPS (DPOW-008-3) ===");
        console.log("A. Integration test 1: Governance schedules a no-op, Guardian cancels");
        console.log("B. Integration test 2: Governance schedules a no-op, wait 60s, anyone executes");
        console.log("C. Governance schedules timelock.updateDelay(604800), wait 60s, execute");
        console.log("D. Deployer: renounceRole(DEFAULT_ADMIN_ROLE, deployer)");
        console.log("E. Verify final role state via hasRole(...) queries");
        console.log("F. Distribute WCENT (130M total) to the 5 miners and stake (DPOW-008-3)");
        console.log("G. After maturity: open the chain-config PR with deployed addresses (DPOW-008-4)");
    }
}
