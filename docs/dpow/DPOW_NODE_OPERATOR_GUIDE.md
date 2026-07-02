# DPoW — Node Operator Upgrade Guide

**Audience**: operators of Incentiv nodes — miners, RPC nodes, archive nodes.
**Companion documents**: `DPOW_SPECIFICATION.md`, `DPOW_MINER_ONBOARDING.md`, `DPOW_GOVERNANCE_RUNBOOK.md`.

---

## 1. What is changing

DPoW (Delegated Proof-of-Work) adds a stake-based miner authorization rule to consensus. From the activation timestamp `DPoWTime` onward (i.e. every block whose `block.timestamp ≥ DPoWTime`), a block is valid only if its coinbase is a staked, matured miner in the `MinerRegistry` contract.

**This is a consensus-breaking hard fork.** Every node — whether it mines or not — must run the DPoW-enabled binary before `DPoWTime`. A node still running old software will accept unauthorized blocks and **fork off the canonical chain**.

| Node type | Must upgrade? | Must stake? |
|---|---|---|
| Miner | ✅ yes | ✅ yes — see `DPOW_MINER_ONBOARDING.md` |
| RPC node | ✅ yes | ❌ no |
| Archive node | ✅ yes | ❌ no |

---

## 2. Before you start

- Know your `systemd` unit name (e.g. `incentum.service`).
- Have the published SHA-256 checksum of the new binary release.
- Schedule the upgrade well before `DPoWTime` — do not wait for activation day.

Install paths differ between nodes. The shell commands in this guide reference the geth binary and data directory through two variables — set them to your node's actual paths before running any command:

```bash
export GETH=/mnt/data/node/client/build/bin/geth   # example — your geth binary
export DATADIR=/mnt/data/node/data                 # example — your geth --datadir
```

---

## 3. Mandatory service-file hardening

Before activation, every node's service file must be hardened. This is not optional: during devnet validation, a botnet actively drained an unlocked miner account through an exposed HTTP RPC. The misconfiguration was `--http.addr 0.0.0.0` + `--unlock` + `--allow-insecure-unlock` + `--rpc.allow-unprotected-txs`.

### 3.1 Hardening checklist

- [ ] `--http.addr 127.0.0.1` — HTTP RPC bound to localhost (or behind an authenticated reverse proxy / IP allowlist; never `0.0.0.0` on a public interface).
- [ ] `--ws.addr 127.0.0.1` — same for WebSocket RPC.
- [ ] **No** `--unlock` — Ethash mining does not need an unlocked etherbase; the etherbase only receives rewards, it signs nothing.
- [ ] **No** `--allow-insecure-unlock`.
- [ ] **No** `--rpc.allow-unprotected-txs`.
- [ ] `--txpool.pricelimit <wei>` — set a minimum gas price so zero-fee spam transactions propagated over P2P are rejected on entry. Pick a value above zero but below the normal transaction-fee level of the network; tune it to the network's actual fee policy (`1000000000` = 1 Gwei is a reasonable starting point, not a universal constant).

> Miners sign their `approve` / `stake` / `requestUnstake` transactions **externally** (with `cast --keystore` or a hardware wallet) and submit them over local IPC or a restricted RPC. The node itself never needs an unlocked account. See `DPOW_MINER_ONBOARDING.md`.

### 3.2 Example hardened `systemd` unit (miner node)

```ini
[Unit]
Description=Incentum Node
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=ubuntu
Group=ubuntu
Restart=on-failure
RestartSec=15
WorkingDirectory=/mnt/data/node
ExecStart=/mnt/data/node/client/build/bin/geth \
  --datadir /mnt/data/node/data \
  --http \
  --http.addr 127.0.0.1 \
  --http.port 8545 \
  --http.api 'eth,net,web3,txpool' \
  --ws \
  --ws.addr 127.0.0.1 \
  --ws.api 'eth,net,web3' \
  --syncmode full \
  --gcmode archive \
  --txpool.pricelimit 1000000000 \
  --mine \
  --miner.threads 4 \
  --miner.etherbase 0x<MINER_COINBASE>
StandardOutput=append:/var/log/geth.log
StandardError=append:/var/log/geth.log

[Install]
WantedBy=multi-user.target
```

> The `/mnt/data/node/...` paths and the `User` / `Group` shown are an example layout — set `WorkingDirectory`, `ExecStart`, `--datadir`, and `User` / `Group` to your node's actual install. systemd does not expand shell variables, so these must be absolute paths.

An RPC or archive node uses the same flags minus `--mine`, `--miner.threads`, `--miner.etherbase`.

> If the network requires external HTTP access (block explorer, monitoring), do not reopen `0.0.0.0`. Use an SSH tunnel, a private network interface, or an authenticated reverse proxy with an IP allowlist.

---

## 4. Upgrade procedure

### 4.1 Stop the node

```bash
sudo systemctl stop incentum.service
```

### 4.2 Back up the current binary

```bash
sudo cp "$GETH" "$GETH.pre-dpow"
```

### 4.3 Install the new binary

Two paths: installing the pre-built release artifact (recommended) or building from source as a verification fallback.

**Install pre-built binary (recommended)**

CI publishes a GitHub Release for each `v*-dpow-mainnet` tag containing a static `linux/amd64` tarball and its SHA-256 checksum file. Download both, verify, extract, and install:

```bash
RELEASE_TAG="<the-actual-release-tag>"   # e.g. v1.11.7-dpow-mainnet
RELEASE_URL="https://github.com/IncentivNetwork/incentum-pow/releases/download/${RELEASE_TAG}"

curl -L -O "${RELEASE_URL}/geth-linux-amd64-${RELEASE_TAG}.tar.gz"
curl -L -O "${RELEASE_URL}/geth-linux-amd64-${RELEASE_TAG}.tar.gz.sha256"

# Verify the tarball BEFORE extracting — must succeed:
sha256sum -c "geth-linux-amd64-${RELEASE_TAG}.tar.gz.sha256"

# Extract (the tarball contains `geth` and `COPYING`):
tar -xzf "geth-linux-amd64-${RELEASE_TAG}.tar.gz"

# Confirm the embedded version and Git commit match the released tag:
./geth version

# Install over the existing binary (uses $GETH from §2, same path that was backed up in §4.2):
sudo install -m 0755 geth "$GETH"
```

The `sha256sum -c` step verifies the **tarball**, not the extracted binary — the CI-generated `.sha256` file references the tarball filename. Running the check before extraction also prevents accidentally installing an unverified binary.

**Build from source (verification fallback)**

Use this to cross-check that the pre-built artifact matches a local build of the same tag, or when a suitable release artifact is not available for the operator's platform.

```bash
cd /mnt/data/node/client   # your client source repo (example path)
git fetch origin --tags
git checkout <release-tag>
go run build/ci.go install ./cmd/geth
./build/bin/geth version   # confirm Git Commit matches the release
```

### 4.4 Apply the hardened service file

Edit `/etc/systemd/system/incentum.service` per §3, then:

```bash
sudo systemctl daemon-reload
```

### 4.5 Verify the embedded chain config

The DPoW activation parameters live in the binary's built-in network `ChainConfig` — they are not in any TOML file, so `geth dumpconfig` does **not** show them. Verify from a running node instead.

At startup geth logs a one-line consensus banner:

```bash
sudo journalctl -u incentum.service | grep -i "Consensus:"
# DPoW-enabled binary with activation set:
#   Consensus: Ethash + DPoW (authorized mining, activates at timestamp <DPoWTime> / <RFC3339 UTC>)
# DPoW code present but inactive (DPoWTime = nil):
#   Consensus: Ethash (proof-of-work)
```

For the full active chain config, attach over IPC:

```bash
"$GETH" attach "$DATADIR/geth.ipc"
```

```javascript
admin.nodeInfo.protocols.eth.config   // shows dpowTime, minerRegistryAddress,
                                      // dpowMaturityTime, dpowMaturityBlocks
```

> The `admin` namespace is intentionally excluded from the hardened `--http.api` / `--ws.api` set (§3.1), so run this command over the local IPC socket. `admin` is not inherently IPC-only — it can be served over HTTP/WS if added to those flags, which the hardening checklist deliberately avoids.

- Before the activation release: `dpowTime` is absent/`null` — DPoW code is present but inert.
- For the activation release: `dpowTime` is set (Unix seconds) and `minerRegistryAddress` is the real deployed contract.

A node configured with `DPoWTime` set but no registry address **refuses to start** (`CheckDPoWConfig` invariant) — this is intentional.

### 4.6 Start and verify

```bash
sudo systemctl start incentum.service
sudo systemctl status incentum.service --no-pager
```

Confirm the node is syncing:

```bash
"$GETH" attach "$DATADIR/geth.ipc"
```

```javascript
eth.blockNumber      // increasing
net.peerCount        // > 0
eth.syncing          // false once caught up
```

---

## 5. Activation day

At the first block with `block.timestamp ≥ DPoWTime`:

- A correctly upgraded node enforces DPoW and follows the canonical chain produced by authorized miners.
- A node still on old software accepts an unauthorized block and **forks off** — it will appear "stuck" on a minority chain.

### 5.1 Health checks

```bash
# latest block, miner, and timestamp — `timestamp` is the activation predicate input,
# compare it against the binary's embedded `DPoWTime` (§4.5) to confirm the chain has
# crossed activation. The block is fetched once so `block`, `miner`, and `timestamp`
# come from the same header even if a new block is mined mid-check.
"$GETH" attach --exec \
  'var b = eth.getBlock("latest"); JSON.stringify({block: b.number, miner: b.miner, timestamp: b.timestamp})' \
  "$DATADIR/geth.ipc"

# consensus errors in the last 15 minutes
sudo journalctl -u incentum.service --since "15 min ago" \
  | grep -ciE 'DPoW|unauthorized|not yet mature|BAD BLOCK|panic'
```

Expected after activation: the latest block's `miner` is always a staked, authorized address; the consensus-error count is `0`.

### 5.2 If your node forked off

Symptoms: your `eth.blockNumber` diverges from public explorers; logs show repeated `DPoW: unauthorized coinbase …` or `Synchronisation failed, dropping peer …`.

1. Confirm you are running the DPoW-enabled binary (`geth version` → expected release commit).
2. Confirm the embedded config has the correct `DPoWTime` and `MinerRegistryAddress` (§4.5).
3. Restart the node. It will re-evaluate peers and re-sync onto the canonical chain.
4. If it still does not converge, stop the node, remove the diverged chain segment by resyncing (`geth removedb` of chaindata or a fresh datadir), and let it re-sync from peers.

---

## 6. Post-upgrade checklist

- [ ] `geth version` shows the expected release commit.
- [ ] Service file passes the §3.1 hardening checklist.
- [ ] geth's HTTP/WS RPC listens on `127.0.0.1` only and is not bound directly to a public interface; any external access is terminated by the reverse proxy (§3.1), which handles authentication / method filtering.
- [ ] Node is synced (`eth.syncing == false`, `net.peerCount > 0`).
- [ ] After activation: latest block miner is an authorized address; zero consensus errors in logs.
- [ ] (Miners only) `isAuthorizedMiner(<your coinbase>)` returns `true` — see `DPOW_MINER_ONBOARDING.md`.
- [ ] Network invariant — `ChainConfig` maturity parameters match the deployed `MinerRegistry` (normally verified once at deployment — see `DPOW_GOVERNANCE_RUNBOOK.md` §9).
