# DPoW — Miner Onboarding Guide

**Audience**: miner operators who must stake before the DPoW hard fork to keep producing blocks.
**Companion documents**: `DPOW_SPECIFICATION.md`, `DPOW_NODE_OPERATOR_GUIDE.md`, `DPOW_GOVERNANCE_RUNBOOK.md`.

---

## 1. Overview

After the DPoW activation timestamp `DPoWTime` (i.e. every block whose `block.timestamp ≥ DPoWTime`), a block is valid only if its coinbase (etherbase) is a **staked, matured** miner registered in the `MinerRegistry` contract. To keep mining, you must:

1. Acquire `STAKE_AMOUNT` of `CENT` and wrap it into `WCENT`.
2. `approve` and `stake()` it into `MinerRegistry`.
3. Wait for **both** maturity thresholds to elapse.
4. Confirm `isAuthorizedMiner(<your coinbase>) == true` before `DPoWTime`.

If your coinbase is not authorized at `DPoWTime`, your blocks are rejected by the network.

---

## 2. Parameters

| Parameter | Mainnet value |
|---|---|
| `STAKE_AMOUNT` | 26,000,000 WCENT (`26_000_000 * 10^18` base units) |
| `MATURITY_TIME` | 86,400 s (24 h) |
| `MATURITY_BLOCKS` | 17,280 blocks |
| `UNSTAKE_DELAY` | 604,800 s (7 days) |

Authorization requires **both** of these to be satisfied:

```
now        ≥ stakeTime  + MATURITY_TIME
blockNumber ≥ stakeBlock + MATURITY_BLOCKS
```

The two run **in parallel**, not in sequence. At ~5 s/block, 17,280 blocks ≈ 86,400 s, so both resolve in roughly **24 hours** — but the binding one is whichever resolves *later* at the actual block rate. Compute your deadline against both (see §6).

---

## 3. Prerequisites

- Your **miner coinbase address** — the address in `--miner.etherbase`. This is the address that must end up authorized.
- A **keystore file + password** for the address that will pay the stake. With self-only staking, `msg.sender` *is* the registered miner — so the address you stake from is the address that becomes authorized. **Stake from your coinbase address.**
- `STAKE_AMOUNT` of native `CENT`, plus a margin of native `CENT` for gas.
- `cast` (Foundry) installed on the machine you operate from.
- The deployed contract addresses (published by the team):
  - `WCENT` — staking token
  - `MINER_REGISTRY` — the registry

```bash
export RPC=http://localhost:8545         # your node's RPC (local)
export WCENT=0x<wcent-address>
export REGISTRY=0x<miner-registry-address>
export STAKE=26000000000000000000000000  # 26,000,000 * 1e18
export MINER_KEYSTORE=/path/to/keystore/UTC--...--<coinbase-no-0x>
export MINER_PWD_FILE=/path/to/password.txt
export MINER=0x<your-coinbase-address>

# sanity check: the keystore decrypts to your coinbase
cast wallet address --keystore $MINER_KEYSTORE --password-file $MINER_PWD_FILE
```

---

## 4. Staking procedure

> **Operational note**: `cast send --keystore` sometimes runs its preflight `eth_estimateGas` from `address(0)` instead of the keystore account, producing a misleading revert (`AlreadyStaked`, `NotStaked`, …). If you hit that, add `--gas-limit <N>` to skip the estimate, and use `cast call --from <addr> …` to dry-run. Gas limits below are safe defaults.

### 4.1 Wrap native CENT into WCENT

```bash
cast send $WCENT "deposit()" --value 26000000ether \
  --keystore $MINER_KEYSTORE --password-file $MINER_PWD_FILE \
  --rpc-url $RPC --gas-limit 100000

# verify
cast call $WCENT "balanceOf(address)(uint256)" $MINER --rpc-url $RPC
# expect: 26000000000000000000000000
```

### 4.2 Approve the registry

```bash
cast send $WCENT "approve(address,uint256)" $REGISTRY $STAKE \
  --keystore $MINER_KEYSTORE --password-file $MINER_PWD_FILE \
  --rpc-url $RPC --gas-limit 100000

# verify
cast call $WCENT "allowance(address,address)(uint256)" $MINER $REGISTRY --rpc-url $RPC
# expect: 26000000000000000000000000
```

### 4.3 Stake

```bash
cast send $REGISTRY "stake()" \
  --keystore $MINER_KEYSTORE --password-file $MINER_PWD_FILE \
  --rpc-url $RPC --gas-limit 300000
```

Record the stake transaction's block number and timestamp from the receipt — you need them for the maturity calculation:

```bash
cast call $REGISTRY "stakeBlock(address)(uint256)" $MINER --rpc-url $RPC
cast call $REGISTRY "stakeTime(address)(uint256)"  $MINER --rpc-url $RPC
cast call $REGISTRY "miners(address)(bool)"        $MINER --rpc-url $RPC   # true
```

### 4.4 Wait for maturity and verify

```bash
cast call $REGISTRY "isAuthorizedMiner(address)(bool)" $MINER --rpc-url $RPC
```

This returns `false` until **both** maturity thresholds pass, then `true`. Poll it until it flips:

```bash
while [ "$(cast call $REGISTRY 'isAuthorizedMiner(address)(bool)' $MINER --rpc-url $RPC)" != "true" ]; do
  echo "$(date '+%H:%M:%S') not yet authorized..."
  sleep 60
done
echo "AUTHORIZED"
```

> `isAuthorizedMiner()` is the **contract-side** check, evaluated against the registry's own `immutable` `MATURITY_TIME` / `MATURITY_BLOCKS`. Geth consensus uses the matching `ChainConfig` values; for a correctly deployed network the two are identical (verified once at deployment — see `DPOW_GOVERNANCE_RUNBOOK.md` §9), so a `true` result is the authoritative readiness signal.

---

## 5. Staking deadline

You must be **authorized before `DPoWTime`**, with a safety buffer for block-rate variance.

```
latest acceptable stake time  = DPoWTime                       - MATURITY_TIME   - 3600   (1 h safety buffer)
latest acceptable stake block = estimated_DPoWTime_block_height - MATURITY_BLOCKS - 720    (~1 h safety buffer at 5 s/block)
```

The **time** deadline is deterministic — `DPoWTime` is an exact Unix timestamp embedded in the binary. The **block** deadline is only an estimate: the block height that will hold a given future timestamp depends on the actual block-production rate and is not knowable ahead of time, so derive `estimated_DPoWTime_block_height` from the current head height and time, and treat it as approximate.

Stake early enough to satisfy **both**. If blocks are produced faster than 5 s, the time condition binds (MATURITY_BLOCKS is reached sooner, so the wall-clock threshold is the last to fall); if slower, the block condition binds. Do not rely on a fixed "24 h" rule — compute against both and add the buffer.

Obtain the activation `DPoWTime` from the team and stake well ahead of it — do not assume any particular advance-notice window. Treat the staking deadline as a hard cut-off: a miner not authorized at `DPoWTime` cannot produce valid blocks until it stakes and matures afterwards.

---

## 6. After activation — node configuration

Your node must run the DPoW-enabled, hardened binary (see `DPOW_NODE_OPERATOR_GUIDE.md`). Confirm:

- `--miner.etherbase` is exactly the address you staked and matured.
- The node is upgraded and the service file passes the hardening checklist.
- `isAuthorizedMiner(<coinbase>)` is `true`.

Ethash mining does **not** require an unlocked account — do not re-add `--unlock`.

---

## 7. Unstaking lifecycle

Staking is reversible through a two-step, self-managed flow. Both steps are called by the miner.

### 7.1 `requestUnstake()`

```bash
cast send $REGISTRY "requestUnstake()" \
  --keystore $MINER_KEYSTORE --password-file $MINER_PWD_FILE \
  --rpc-url $RPC --gas-limit 300000
```

Effects, **immediately** in the same transaction:

- `miners[m] = false` — mining authorization is revoked at once. There is **no grace period**; your next blocks will be rejected by the network.
- `unstakeRequestTime[m]` is recorded; the `UNSTAKE_DELAY` countdown starts.
- `stakeTime` / `stakeBlock` are preserved (needed for refund accounting).

Do not call `requestUnstake()` unless you intend to stop mining now.

### 7.2 `finalizeUnstake()`

After `UNSTAKE_DELAY` (7 days on mainnet) has elapsed:

```bash
cast send $REGISTRY "finalizeUnstake()" \
  --keystore $MINER_KEYSTORE --password-file $MINER_PWD_FILE \
  --rpc-url $RPC --gas-limit 300000
```

This returns `STAKE_AMOUNT` WCENT to the miner and clears the miner's remaining registry state (`stakeTime`, `stakeBlock`, and the unstake-request fields; `miners[m]` was already `false` since `requestUnstake()`). To unwrap back to native CENT:

```bash
cast send $WCENT "withdraw(uint256)" $STAKE \
  --keystore $MINER_KEYSTORE --password-file $MINER_PWD_FILE \
  --rpc-url $RPC --gas-limit 100000
```

### 7.3 Re-staking

After `finalizeUnstake()`, the address may stake again from scratch: `approve` → `stake()` → wait for a fresh maturity period. Calling `stake()` while an unstake is still pending reverts `UnstakeInProgress`.

---

## 8. Troubleshooting

| Symptom | Cause | Action |
|---|---|---|
| `stake()` reverts `AlreadyStaked` | The address is already an active miner | Nothing to do — you are staked |
| `stake()` reverts `UnstakeInProgress` | A previous `requestUnstake()` was not finalized | Call `finalizeUnstake()` after the delay, then re-stake |
| `stake()` reverts `InsufficientAllowance` / `InsufficientBalance` | Missing `approve` or not enough WCENT | Re-run §4.1 / §4.2 |
| `stake()` reverts `StakingCurrentlyPaused` | Governance paused staking | Wait for governance to unpause |
| `cast send` reverts during gas estimate with a state error | `eth_estimateGas` ran from `address(0)` | Add `--gas-limit <N>`; dry-run with `cast call --from $MINER` |
| `finalizeUnstake()` reverts `UnstakeDelayNotMet` | `UNSTAKE_DELAY` has not elapsed | Wait until `unstakeRequestTime + UNSTAKE_DELAY` |
| `isAuthorizedMiner` stays `false` after 24 h | Block-maturity not yet reached (slow blocks) | Wait until `blockNumber ≥ stakeBlock + MATURITY_BLOCKS` |
| Blocks rejected after activation | Coinbase not the staked address, or not matured | Verify `--miner.etherbase` equals the staked address and `isAuthorizedMiner` is `true` |
