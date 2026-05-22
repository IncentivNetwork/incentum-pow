# DPoW — Governance Runbook

**Audience**: Timelock proposers/executors (governance multisig signers) and the canceller guardian.
**Companion documents**: `DPOW_SPECIFICATION.md` (§11 Operational & Governance Layer), `DPOW_NODE_OPERATOR_GUIDE.md`, `DPOW_MINER_ONBOARDING.md`.

---

## 1. Governance model

`MinerRegistry`'s two privileged functions — `setPaused(bool)` and `emergencyRemoveMiner(address,address,string)` — are protected by `onlyGovernance`, which requires `msg.sender == address(timelock)`. They cannot be called directly by any EOA or multisig; every privileged action is routed through the OpenZeppelin `TimelockController`.

### 1.1 Roles

| Role | Held by | Capability |
|---|---|---|
| `PROPOSER_ROLE` | governance multisig (Gnosis Safe) | `schedule()` an operation |
| `CANCELLER_ROLE` | governance multisig **and** the independent guardian | `cancel()` a scheduled operation |
| `EXECUTOR_ROLE` | governance multisig | `execute()` an operation after the delay |
| `DEFAULT_ADMIN_ROLE` | the Timelock contract itself (`admin = address(0)` at deploy) | `grantRole` / `revokeRole` — itself timelocked |

OpenZeppelin auto-grants `CANCELLER_ROLE` to every constructor `proposer`. The **independent guardian** (a separate multisig with a different signer set) is granted `CANCELLER_ROLE` post-deployment so cancellation does not depend on the primary governance multisig — see §7.

> **Executor model.** This runbook assumes a *restricted* executor (the governance multisig). OpenZeppelin also supports an *open* executor — granting `EXECUTOR_ROLE` to `address(0)` lets anyone execute an operation once its delay has elapsed. An executor can never run an *unscheduled* operation, so an open executor does not weaken the timelock; it improves liveness (a ready operation cannot be stranded by an unavailable multisig) at the cost of less operational control. Choose deliberately at deployment.

### 1.2 Timing — everything is delayed; only `cancel()` is instant

| Operation | Delay |
|---|---|
| `emergencyRemoveMiner` | full `minDelay` (7 days mainnet) |
| `setPaused` | full `minDelay` |
| `updateDelay` (changes the delay itself) | full *current* `minDelay` |
| `grantRole` / `revokeRole` | full `minDelay` |
| `cancel` | **instant** |

The first malicious operation is therefore always visible on-chain for the full delay window before it can take effect. `cancel()` is the only instant lever — keep it in independent hands.

---

## 2. Environment setup

```bash
export RPC=https://<mainnet-rpc>
export TIMELOCK=0x<timelock-address>
export REGISTRY=0x<miner-registry-address>
export WCENT=0x<wcent-address>

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
cast call $REGISTRY "activeMinerCount()(uint256)"            --rpc-url $RPC   # decremented
cast call $WCENT    "balanceOf(address)(uint256)"    $REFUND --rpc-url $RPC   # = pre-state balance (§4.2) + STAKE_AMOUNT
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

## 7. One-time post-deployment setup — independent guardian

By default the governance multisig is the only `CANCELLER_ROLE` holder. Grant the role to an independent guardian so the cancellation lever is not controlled by the same keys that schedule operations.

`grantRole` is itself `DEFAULT_ADMIN_ROLE`-gated, and that role is held only by the Timelock — so granting is itself a timelocked operation:

```bash
export TARGET=$TIMELOCK   # role grants target the Timelock itself, not the registry
export CANCELLER_ROLE=$(cast call $TIMELOCK "CANCELLER_ROLE()(bytes32)" --rpc-url $RPC)
export GUARDIAN=0x<guardian-safe-address>

export DATA=$(cast calldata "grantRole(bytes32,address)" $CANCELLER_ROLE $GUARDIAN)
export SALT=$(cast keccak "grant-canceller-guardian")
```

Schedule → wait `minDelay` → execute against `target = $TIMELOCK` (note: the target is the Timelock itself, not the registry). Verify:

```bash
cast call $TIMELOCK "hasRole(bytes32,address)(bool)" $CANCELLER_ROLE $GUARDIAN --rpc-url $RPC   # true
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

- [ ] `TimelockController` deployed with `minDelay = 604800` (7 days) and `admin = address(0)`.
- [ ] `proposers` / `executors` set to the governance Gnosis Safe (multisig, hardware-wallet signers).
- [ ] Independent guardian Safe (different signer set) granted `CANCELLER_ROLE` via the §7 procedure.
- [ ] Guardian Safe has `CANCELLER_ROLE` only — not `PROPOSER_ROLE`, not `EXECUTOR_ROLE`.
- [ ] `MinerRegistry.timelock` points at the deployed `TimelockController`.
- [ ] `ChainConfig.DPoWMaturityTime` / `DPoWMaturityBlocks` in the release binary match the deployed `MinerRegistry` immutables (cross-check below).
- [ ] Timelock `CallScheduled` monitoring + guardian alerting is live.
- [ ] A dry-run of schedule → cancel has been rehearsed on devnet/testnet.

Maturity-parameter cross-check — the `ChainConfig` values Geth uses for consensus must equal the contract's `immutable` values:

```bash
cast call $REGISTRY "MATURITY_TIME()(uint256)"   --rpc-url $RPC   # must equal ChainConfig.DPoWMaturityTime
cast call $REGISTRY "MATURITY_BLOCKS()(uint256)" --rpc-url $RPC   # must equal ChainConfig.DPoWMaturityBlocks
cast call $REGISTRY "UNSTAKE_DELAY()(uint256)"   --rpc-url $RPC   # contract-level only; no ChainConfig equivalent
```
