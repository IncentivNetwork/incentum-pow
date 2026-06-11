# DPoW — Governance Runbook

**Audience**: Timelock proposers/executors (governance multisig signers) and the canceller guardian.
**Companion documents**: `DPOW_SPECIFICATION.md` (§11 Operational & Governance Layer), `DPOW_NODE_OPERATOR_GUIDE.md`, `DPOW_MINER_ONBOARDING.md`.

---

## 1. Governance model

`MinerRegistry`'s two privileged functions — `setPaused(bool)` and `emergencyRemoveMiner(address,address,string)` — are protected by `onlyGovernance`, which requires `msg.sender == address(timelock)`. They cannot be called directly by any EOA or multisig; every privileged action is routed through the OpenZeppelin `TimelockController`.

### 1.1 Roles

| Role | Held by | Capability |
|---|---|---|
| `PROPOSER_ROLE` | Governance Safe (Gnosis Safe, 2/3) | `schedule()` an operation |
| `CANCELLER_ROLE` | Guardian Safe only | `cancel()` a scheduled operation |
| `EXECUTOR_ROLE` | `address(0)` (open executor) | `execute()` an operation after the delay |
| `DEFAULT_ADMIN_ROLE` | the Timelock itself; the deployer EOA holds it briefly at deploy time under the admin-renounce bootstrap (§1.3) | `grantRole` / `revokeRole` — itself timelocked once the deployer has renounced |

OpenZeppelin auto-grants `CANCELLER_ROLE` to every constructor `proposer`. On Incentiv mainnet the **independent Guardian Safe** (a separate 2/3 multisig with a non-overlapping signer set) is granted `CANCELLER_ROLE`, and the auto-grant on Governance is revoked during the deploy-time hardening flow (§1.3), so cancellation does not depend on the primary governance multisig. §7 documents the equivalent fallback path through a timelocked role-change, for deployments that do not use the optional admin.

> **Executor model — Incentiv mainnet.** `EXECUTOR_ROLE` is granted to `address(0)` (open executor): any address may `execute()` an operation once its delay has elapsed. An executor can never run an *unscheduled* operation, so an open executor does not weaken the timelock; it improves liveness (a ready operation cannot be stranded by an unavailable multisig) at the cost of less operational control. Other deployments may choose a restricted executor (e.g., the governance multisig) instead.

### 1.2 Timing — everything is delayed; only `cancel()` is instant

| Operation | Delay |
|---|---|
| `emergencyRemoveMiner` | full `minDelay` (7 days mainnet) |
| `setPaused` | full `minDelay` |
| `updateDelay` (changes the delay itself) | full *current* `minDelay` |
| `grantRole` / `revokeRole` | full `minDelay` |
| `cancel` | **instant** |

The first malicious operation is therefore always visible on-chain for the full delay window before it can take effect. `cancel()` is the only instant lever — keep it in independent hands.

### 1.3 Deployment hardening (admin-renounce bootstrap)

Incentiv mainnet uses the OpenZeppelin v5 `TimelockController` optional-`admin` constructor parameter to harden roles during initial setup, instead of paying the 7-day timelock cost for the initial grant/revoke. The script `script/DeployMainnet.s.sol` performs steps 1–3 of the lifecycle below in a single script run as sequential on-chain transactions, not one atomic transaction; a mid-run failure can leave partial state that must be recovered while the deployer still holds the temporary admin role. Steps 4–6 require multisig signatures and are executed manually as part of DPOW-008-3.

1. **Deploy** `TimelockController(minDelay = 60, proposers = [GovernanceSafe], executors = [address(0)], admin = deployerEOA)`. The 60-second delay is **temporary**; the executor is open (anyone may `execute()` after the delay); the deployer is the **temporary admin** and at this point holds `DEFAULT_ADMIN_ROLE`.
2. **Harden roles in the same script run.** Still as admin after the Timelock deployment, the deployer calls:
   ```
   timelock.grantRole(CANCELLER_ROLE, GuardianSafe)
   timelock.revokeRole(CANCELLER_ROLE, GovernanceSafe)
   ```
   Both are instant — no timelock applies to admin-driven role changes. After this, only Guardian can `cancel()`.
3. **Deploy** `MinerRegistry` against the same Timelock and finish the script.
4. **Integration tests** on mainnet against the temporary 60-second delay — see §4 (schedule → cancel via Guardian) and a follow-up schedule → wait → execute (open). These prove the multisig + Ledger + `safe.incentiv.io` chain works end-to-end before the delay is raised.
5. **Raise the delay to production.** Governance schedules `timelock.updateDelay(604800)`, waits 60 s, executes. `minDelay` is now 7 days; any future role change is timelocked.
6. **Renounce admin.**
   ```bash
   DEFAULT_ADMIN_ROLE=$(cast call $TIMELOCK "DEFAULT_ADMIN_ROLE()(bytes32)" --rpc-url $RPC)
   cast send $TIMELOCK "renounceRole(bytes32,address)" $DEFAULT_ADMIN_ROLE $DEPLOYER \
     --ledger --from $DEPLOYER --rpc-url $RPC
   ```
   After this, the only `DEFAULT_ADMIN_ROLE` holder is the Timelock itself; future role changes must go through `schedule → wait minDelay → execute`. Verify by querying `hasRole(DEFAULT_ADMIN_ROLE, deployer) == false`.

> **Why this is safe.** The deployer's window with `DEFAULT_ADMIN_ROLE` is short (the duration of the script + the integration-test cycle, on the order of minutes to a few hours), and during that window the deployer is a hardware-wallet-signed EOA under operator control. The alternative — `admin = address(0)` from genesis — pays a full 7-day delay for every initial role change (no `scheduleBatch` shortcut for the first grant + revoke without admin), and the auto-granted `CANCELLER_ROLE` on Governance remains for that 7-day window, defeating the point of having a Guardian. The OZ v5 `TimelockController` NatSpec explicitly recommends this pattern: *"The optional admin can aid with initial configuration of roles after deployment without being subject to delay, but this role should be subsequently renounced in favor of administration through timelocked proposals."*

---

## 2. Environment setup

```bash
export RPC=https://<mainnet-rpc>
export TIMELOCK=0x<timelock-address>
export REGISTRY=0x<miner-registry-address>
export WCENT=0x<wcent-address>

# Mainnet-specific addresses used by the deploy-time hardening procedure (§1.3)
export DEPLOYER=0xd2CC08D9AFaBb57BdF2216ED15fceaa9993F3B7b           # Ledger-backed EOA, temporary Timelock admin during bootstrap
export GOVERNANCE_SAFE=0x10D9dEEb09bA23b2bD9739F698b3dFa9D8F95Ad4    # 2/3 Safe, holds PROPOSER_ROLE
export GUARDIAN_SAFE=0x482Fd68377310ec984bcA521e2020D89e1A93CBc      # 2/3 Safe with non-overlapping signers, holds CANCELLER_ROLE after §1.3

# zero predecessor — operations in this runbook have no dependency
export ZERO=0x0000000000000000000000000000000000000000000000000000000000000000
```

Signing is done by the Gnosis Safe. The `cast` commands below show the **transaction payload** (`to`, `value`, `data`); submit that payload through the Safe UI / Safe CLI and collect the required signatures. Do not hold a single EOA proposer key on mainnet.

---

## 3. Anatomy of a timelocked operation

A `TimelockController` operation is the tuple `(target, value, data, predecessor, salt)`.

- `target` — the contract to call: `$REGISTRY` for `emergencyRemoveMiner` / `setPaused`, `$TIMELOCK` for `grantRole` / `updateDelay`.
- `value` — native value to send; always `0` for these operations.
- `data` — ABI-encoded call to the target function.
- `predecessor` — id of an operation that must execute first; `ZERO` here.
- `salt` — a unique 32-byte value, so identical operations get distinct ids.

Each procedure below sets `$TARGET`, `$DATA`, and `$SALT` explicitly. The operation id is then:

```bash
cast call $TIMELOCK "hashOperation(address,uint256,bytes,bytes32,bytes32)(bytes32)" \
  $TARGET 0 $DATA $ZERO $SALT --rpc-url $RPC
```

Lifecycle: `schedule` → wait `minDelay` → `execute` (or `cancel` any time before execution).

State queries:

```bash
cast call $TIMELOCK "isOperationPending(bytes32)(bool)" $ID --rpc-url $RPC
cast call $TIMELOCK "isOperationReady(bytes32)(bool)"   $ID --rpc-url $RPC
cast call $TIMELOCK "isOperationDone(bytes32)(bool)"    $ID --rpc-url $RPC
cast call $TIMELOCK "getTimestamp(bytes32)(uint256)"    $ID --rpc-url $RPC  # 0=unset, 1=done, else ready-at
```

---

## 4. Procedure — `emergencyRemoveMiner`

Force-removes a miner and sends `STAKE_AMOUNT` WCENT to `refundRecipient`. Use only for a documented cause: compromised miner key, abandoned/lost-key miner with frozen stake, or a provably malicious miner.

**Choose `refundRecipient` deliberately:**

- Compromised key → the operator's **new secure address** (never the compromised miner address — that refunds the attacker).
- Lost-key / abandoned → a recovery/escrow address controlled by the rightful owner or the foundation.
- Malicious miner → a burn address, e.g. `0x000000000000000000000000000000000000dEaD` — a governance-directed burn of the stake.

> **`emergencyRemoveMiner` is not guaranteed slashing.** On mainnet `UNSTAKE_DELAY` equals `TIMELOCK_DELAY` (both 7 days). The stake can only be redirected or burned while it is still held by the contract. A miner who calls `requestUnstake()` at or before the removal is scheduled can reach `finalizeUnstake()` by the time the removal executes and withdraw first — in which case `emergencyRemoveMiner` still removes the miner but transfers nothing (the `hasStake` check sees all-zero state). It reliably *removes* a miner; it *seizes the stake* only if executed before the miner finalizes unstaking. See `DPOW_SPECIFICATION.md` §8.1.

### 4.1 Build the call data

```bash
export TARGET=$REGISTRY
export MINER=0x<miner-to-remove>
export REFUND=0x<refund-recipient>
export REASON="<documented reason>"

export DATA=$(cast calldata "emergencyRemoveMiner(address,address,string)" \
  $MINER $REFUND "$REASON")

export SALT=$(cast keccak "emergencyRemove-$MINER-$(date +%s)")
```

### 4.2 Record pre-state

```bash
cast call $REGISTRY "miners(address)(bool)"             $MINER  --rpc-url $RPC
cast call $REGISTRY "isAuthorizedMiner(address)(bool)"  $MINER  --rpc-url $RPC
cast call $REGISTRY "activeMinerCount()(uint256)"               --rpc-url $RPC
cast call $WCENT    "balanceOf(address)(uint256)"       $REFUND --rpc-url $RPC
```

### 4.3 Schedule (governance multisig)

Submit through the Safe — payload:

```
to:    $TIMELOCK
value: 0
data:  schedule(address,uint256,bytes,bytes32,bytes32,uint256)
       args: $REGISTRY, 0, $DATA, $ZERO, $SALT, <minDelay>
```

`cast` form of the payload:

```bash
MIN_DELAY=$(cast call $TIMELOCK "getMinDelay()(uint256)" --rpc-url $RPC)
cast calldata "schedule(address,uint256,bytes,bytes32,bytes32,uint256)" \
  $REGISTRY 0 $DATA $ZERO $SALT $MIN_DELAY
```

Record the operation id (§3) and **announce the scheduled operation publicly** — the delay window is a security feature only if the community can see it.

### 4.4 Wait the delay

7 days on mainnet. Verify readiness:

```bash
cast call $TIMELOCK "isOperationReady(bytes32)(bool)" $ID --rpc-url $RPC   # true once ready
```

### 4.5 Execute (governance multisig)

Submit through the Safe — payload:

```
to:    $TIMELOCK
value: 0
data:  execute(address,uint256,bytes,bytes32,bytes32)
       args: $REGISTRY, 0, $DATA, $ZERO, $SALT
```

```bash
cast calldata "execute(address,uint256,bytes,bytes32,bytes32)" \
  $REGISTRY 0 $DATA $ZERO $SALT
```

### 4.6 Verify post-state

```bash
cast call $REGISTRY "miners(address)(bool)"          $MINER  --rpc-url $RPC   # false
cast call $REGISTRY "stakeTime(address)(uint256)"    $MINER  --rpc-url $RPC   # 0
cast call $REGISTRY "stakeBlock(address)(uint256)"   $MINER  --rpc-url $RPC   # 0
cast call $REGISTRY "activeMinerCount()(uint256)"            --rpc-url $RPC   # decremented if the miner was active; unchanged if it had already requested unstake or was inactive
cast call $WCENT    "balanceOf(address)(uint256)"    $REFUND --rpc-url $RPC   # pre-state balance (§4.2) + STAKE_AMOUNT if the stake is still held by the registry; unchanged if already finalized (§4 note)
```

Confirm the `EmergencyRemoval(miner, refundRecipient, reason)` event in the execution receipt.

---

## 5. Procedure — `setPaused`

Pauses or unpauses *new* staking. It does **not** affect existing miners or block production — it only makes `stake()` revert `StakingCurrentlyPaused`.

```bash
export TARGET=$REGISTRY
# pause:   true   |   unpause: false
export DATA=$(cast calldata "setPaused(bool)" true)
export SALT=$(cast keccak "setPaused-$(date +%s)")
```

Then schedule → wait → execute exactly as in §4.3–4.5 with this `$DATA` / `$SALT`. Verify:

```bash
cast call $REGISTRY "stakingPaused()(bool)" --rpc-url $RPC
```

---

## 6. Procedure — cancelling a malicious operation

If a malicious or erroneous operation is scheduled, **any** holder of `CANCELLER_ROLE` can cancel it instantly, at any time before execution. Submit this through the **guardian** Safe (independent signer set) — not a single EOA key — so cancellation does not depend on the same multisig that may itself be compromised.

Payload:

```
to:    $TIMELOCK
value: 0
data:  cancel(bytes32)
       args: $ID
```

`cast` form of the payload (`$ID` = operation id of the malicious scheduled operation):

```bash
cast calldata "cancel(bytes32)" $ID
```

Verify:

```bash
cast call $TIMELOCK "isOperationPending(bytes32)(bool)" $ID --rpc-url $RPC   # false
cast call $TIMELOCK "getTimestamp(bytes32)(uint256)"    $ID --rpc-url $RPC   # 0
```

---

## 7. Fallback setup for deployments without the §1.3 bootstrap

This section applies only to deployments that did **not** use the §1.3 admin-renounce bootstrap (i.e. `TimelockController` was deployed with `admin = address(0)`). The goal is the same final state as §1.3 — Guardian holds `CANCELLER_ROLE`, Governance's constructor auto-grant on `CANCELLER_ROLE` is revoked — but reached through a single timelocked operation instead of inline at deploy time. On Incentiv mainnet this procedure is **not used** because the deploy script already produces the final role state.

`grantRole` / `revokeRole` are `DEFAULT_ADMIN_ROLE`-gated; without an optional admin, that role is held only by the Timelock — so any role change is itself a timelocked operation. Both calls must be in the **same** `scheduleBatch`, otherwise a window opens where Governance can rescind its own removal.

```bash
export TARGET=$TIMELOCK   # role grants target the Timelock itself, not the registry
export CANCELLER_ROLE=$(cast call $TIMELOCK "CANCELLER_ROLE()(bytes32)" --rpc-url $RPC)
export GUARDIAN=0x<guardian-safe-address>
export GOVERNANCE=0x<governance-safe-address>

export GRANT_DATA=$(cast calldata "grantRole(bytes32,address)" $CANCELLER_ROLE $GUARDIAN)
export REVOKE_DATA=$(cast calldata "revokeRole(bytes32,address)" $CANCELLER_ROLE $GOVERNANCE)
export SALT=$(cast keccak "harden-canceller-role")
```

Schedule the **batch** (atomic) → wait `minDelay` → execute. The batch payload:

```bash
MIN_DELAY=$(cast call $TIMELOCK "getMinDelay()(uint256)" --rpc-url $RPC)
cast calldata "scheduleBatch(address[],uint256[],bytes[],bytes32,bytes32,uint256)" \
  "[$TIMELOCK,$TIMELOCK]" "[0,0]" "[$GRANT_DATA,$REVOKE_DATA]" $ZERO $SALT $MIN_DELAY
```

After `minDelay`, execute with the matching `executeBatch(...)` payload (same arrays, no delay arg). Verify the final state:

```bash
cast call $TIMELOCK "hasRole(bytes32,address)(bool)" $CANCELLER_ROLE $GUARDIAN    --rpc-url $RPC   # true
cast call $TIMELOCK "hasRole(bytes32,address)(bool)" $CANCELLER_ROLE $GOVERNANCE  --rpc-url $RPC   # false
```

Do **not** grant the guardian `PROPOSER_ROLE` or `EXECUTOR_ROLE` — it must be canceller-only.

---

## 8. Incident response

| Incident | Immediate action |
|---|---|
| Unrecognized operation scheduled on the Timelock | Guardian verifies it was not authorized; if malicious, `cancel(id)` immediately; investigate which proposer key acted |
| Governance multisig suspected compromised | Guardian cancels all pending operations; rotate Safe signers; schedule `revokeRole` for compromised proposer keys |
| Miner key compromised (attacker mining) | Schedule `emergencyRemoveMiner(miner, <operator-new-address>, reason)`; announce publicly |
| Malicious miner harming the network | Schedule `emergencyRemoveMiner(miner, 0x..dEaD, reason)` (stake burned) |
| All miners removed / chain halted | Coordinated recovery hard fork — see §8.3 |

### 8.1 Monitoring

Watch the Timelock for `CallScheduled` events and alert the guardian on any operation the governance process did not expect:

```bash
# Scan from the Timelock deployment block — never from block 0 on mainnet
# (public RPC nodes reject unbounded ranges or time out).
export TIMELOCK_DEPLOY_BLOCK=<block at which the Timelock was deployed>

cast logs --address $TIMELOCK --from-block $TIMELOCK_DEPLOY_BLOCK --rpc-url $RPC \
  'CallScheduled(bytes32,uint256,address,uint256,bytes,bytes32,uint256)'
```

Every scheduled operation should correspond to a documented, announced governance decision. An unannounced `CallScheduled` is an incident until proven otherwise.

### 8.2 Blast radius (reference)

A fully compromised governance multisig can drain miner stakes and halt block production, but **only** within the DPoW subsystem — it cannot touch ordinary user balances, cannot mint `WCENT`, and cannot alter transaction processing. Worst case is loss of miner stakes plus a halted chain. See `DPOW_SPECIFICATION.md` §11.4.

### 8.3 Recovery from a halted chain

If governance removes every miner (or all authorized miners go offline), block production stops. Recovery is **not** a simple binary swap. The client's config-compatibility check treats any post-activation change to `DPoWBlock`, `MinerRegistryAddress`, `DPoWMaturityTime`, or `DPoWMaturityBlocks` as incompatible, with the rewind target set to the pre-DPoW boundary.

Recovery therefore requires a coordinated social hard fork:

1. Decide the recovery configuration — typically `DPoWBlock = nil` (disable DPoW) or a fresh `MinerRegistryAddress`.
2. Build and publish a recovery binary with that configuration.
3. Every node rewinds to before the original `DPoWBlock` and re-syncs under the new configuration.
4. Coordinate the upgrade window network-wide to avoid a split between rewound and non-rewound nodes.

This is a last-resort procedure; the standing safeguards (multisig, 7-day delay, independent canceller) exist precisely to avoid reaching it.

---

## 9. Pre-mainnet governance checklist

These items must all hold by the time the §1.3 admin-renounce bootstrap is complete (i.e. after step 6 — deployer has renounced `DEFAULT_ADMIN_ROLE`). For ordering and command snippets see §1.3 and the DPOW-008-3 deployment plan.

- [ ] Governance Safe (2/3, hardware-wallet signers) and Guardian Safe (2/3, **non-overlapping** signer set) deployed and smoke-tested on mainnet before the deploy script runs.
- [ ] `TimelockController` deployed with `proposers = [GovernanceSafe]`, `executors = [address(0)]` (open executor), and — after the bootstrap — `minDelay = 604800` (raised from the temporary 60 s in step 5) and no standing admin (deployer renounced `DEFAULT_ADMIN_ROLE` in step 6).
- [ ] Integration tests `schedule → cancel` (Guardian) and `schedule → wait → execute` (open) completed against the temporary 60 s delay during the §1.3 bootstrap window; results recorded.
- [ ] `hasRole(CANCELLER_ROLE, GuardianSafe) == true` and `hasRole(CANCELLER_ROLE, GovernanceSafe) == false` (constructor auto-grant revoked inline by the deploy script per §1.3 step 2; the §7 timelocked procedure is **not** used on Incentiv mainnet).
- [ ] Guardian Safe has `CANCELLER_ROLE` only — not `PROPOSER_ROLE`, not `EXECUTOR_ROLE`.
- [ ] `MinerRegistry.timelock` points at the deployed `TimelockController`.
- [ ] `ChainConfig.DPoWMaturityTime` / `DPoWMaturityBlocks` in the release binary match the deployed `MinerRegistry` immutables (cross-check below).
- [ ] Timelock `CallScheduled` monitoring + Guardian alerting is live.

Maturity-parameter cross-check — the `ChainConfig` values Geth uses for consensus must equal the contract's `immutable` values:

```bash
cast call $REGISTRY "MATURITY_TIME()(uint256)"   --rpc-url $RPC   # must equal ChainConfig.DPoWMaturityTime
cast call $REGISTRY "MATURITY_BLOCKS()(uint256)" --rpc-url $RPC   # must equal ChainConfig.DPoWMaturityBlocks
cast call $REGISTRY "UNSTAKE_DELAY()(uint256)"   --rpc-url $RPC   # contract-level only; no ChainConfig equivalent
```
