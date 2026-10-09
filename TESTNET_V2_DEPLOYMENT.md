# Testnet V2 Miner Deployment

Follow this guide to build and run an Incentiv Testnet V2 miner.

### 1. Requirements
Make sure you have the following requirements in order to build and run the client:
- Git
- Go v1.22
- make

### 2. Clone and Install

```bash
# Create a directory for the node
mkdir node
cd node

# Clone the client
git clone https://github.com/IncentivNetwork/incentum-pow.git client
cd client

# Build the client
make geth
```

### 3. Prepare Account and Environment

```bash
# Go back to node directory
cd ..

# Create a data directory and generate a new account
mkdir data
./client/build/bin/geth account new --datadir data/
```
**Note:** the password is not written to a file. The run script below does not unlock the
account — mining needs only the address — so nothing reads it, and a password file next to
the keystore is one more thing to leak.

**Note:** Keep track of the generated address!

### 4. Prepare `genesis.json`
Use the following `genesis.json` to initialize the chain. `genesis.json` must be located in the `node/` direcotry.

```json
{
  "config": {
    "chainId": 28802,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0
  },
  "alloc": {
    "0x3d8eBBDa14e61a0f6B278112EcB99cd895Bcbf3e": {
      "balance": "500000000000000000000000000000"
    },
    "0x683d8cb71DC0caa58AD75986292F22d830B87B75": {
      "balance": "500000000000000000000000000000"
    }
  },
  "coinbase": "0x0000000000000000000000000000000000000000",
  "difficulty": "0x1",
  "gasLimit": "0x1C9C380",
  "nonce": "0x0000000000000000000000000000000000000042",
  "mixhash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "parentHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
  "timestamp": "0x00"
}
```

> **A chain initialised from this file is Incentiv testnet, and the first start makes it
> so.** A genesis config does not enter the genesis block hash, so the file hashes to the
> bundled testnet genesis and the client recognises the chain by that hash plus chain id
> 28802. The config in the file carries none of the settings testnet actually runs — no
> `shanghaiTime`, and none of `fastBlock` 319000, `feePoolBlock` or `zeroRewardBlock`
> 471000 — but on the **first** start the head is still the genesis block, nothing has
> been mined under those bare rules, and the client replaces the stored config with
> testnet's own schedule. `WebAuthnStrict` carries a 2028-10-06 timestamp that will
> activate automatically if unchanged; operator agreement or removal before then is
> tracked as a follow-up. There is no near-term rollout. The replacement happens whether or not you pass
> `--incentiv-testnet`.
>
> It stops happening once the chain has blocks. A stand mining at wall-clock time is past
> `shanghaiTime` from its first block, so adopting the schedule would then mean a
> `CheckCompatible` rewind to before 2025-08-14 — back to genesis for such a chain — and a
> start without a network flag does not do that. From that point it keeps what the
> database says, logs `Stored chain config disagrees with the schedule this binary ships
> for this network`, and applies none of testnet's timestamp forks. The same is true past
> block 319000, where the block-numbered settings disagree as well. From 30 days before
> testnet's bundled `webauthnStrictTime` (2028-10-06) that start is refused rather than
> warned about, since the node would then miss the fork; such a stand cannot take
> `--override.webauthnstrict` either, having no `shanghaiTime` for it to follow, and the
> refusal says what is left — the `chainId` of its own that the next paragraph advises.
>
> So the outcome depends on when the node is started, which is a poor thing to depend on:
> **for a private or local stand, change `chainId` (or the `alloc`) in this file** so the
> chain is its own and none of this applies.
>
> To join testnet deliberately rather than by default, start with `--incentiv-testnet`;
> the run script below does. Do it on a **fresh** datadir. On one that has already
> produced or followed blocks under the bare config, the flag is not a repair:
> `CheckCompatible` compares none of the block-numbered settings, so no rewind is computed
> from them: they are written over the stored ones and rules testnet applied from block
> 319000 are suddenly in force for blocks this chain mined without them. That start may
> well rewind anyway — this file's config has no `shanghaiTime`, and a chain mined at
> wall-clock time is past it, so `CheckCompatible` objects to *that* and the chain is
> rewound to before 2025-08-14. That rewind drops every block above its target, which for
> a stand mined in 2026 is the whole chain — so here it does clear the settings' effect, by
> removing everything they were applied to. A chain with history below the rewind target
> keeps them for that part. Either way the way back to testnet from such a datadir is a
> resync from genesis, which in the first case is what the rewind amounts to.

Initialize the chain with `genesis.json`

```bash
./client/build/bin/geth init --datadir data/ genesis.json
```

### 5. Run the Client

It's recommended to create a run script to run the client with the right params. Create the run script with:

```bash
touch run.sh
chmod +x run.sh
vi run.sh
```

Add the following contents:

```bash
#!/bin/bash
./client/build/bin/geth \
  --datadir data \
  --incentiv-testnet \
  --networkid 28802 \
  --http \
  --http.addr "127.0.0.1" \
  --http.port 8545 \
  --http.api "eth,net,web3,txpool,miner" \
  --mine \
  --miner.threads 1 \
  --miner.etherbase "YOUR_ACCOUNT_ADDRESS_HERE" \
  --miner.gasprice "1" \
  --txpool.pricelimit "1" \
  --rpc.allow-unprotected-txs \
  --bootnodes=enode://3cfd9dcbe3333eac423803d967bd9a5f3e44451a5c2e5373c1d26c25c98d7d3194d6623c6fb4f465cc72c38f14807cb993f7a11b71160266c02fbdfcc9661797@57.129.137.109:30303
```
***Notes on the RPC flags***, because four of them used to combine into a way to spend
this node's balance from anywhere:

- **No `--unlock`, no `--password`, no `--allow-insecure-unlock`.** Mining needs
  `--miner.etherbase` and nothing else — the coinbase is an address, not a key. An
  unlocked key, on the other hand, is reachable through `eth_sendTransaction` and
  `eth_sign` in the `eth` namespace, which this node does serve. With the old flags —
  `--http.addr 0.0.0.0`, `--http.vhosts "*"`, `--http.corsdomain "*"` and an
  unlocked account — anyone who could reach port 8545, and any web page the operator
  visited, could sign with that key. `--allow-insecure-unlock` exists precisely to make
  an operator acknowledge that; this document was passing it. If you need to send
  transactions from the node, use `clef` rather than an unlocked keystore.
- **`--http.addr` is `127.0.0.1`, and `--http.vhosts`/`--http.corsdomain` are gone.**
  Reach it over an SSH tunnel or put it behind something that terminates TLS and
  authenticates. If it has to listen on a public interface, that is a deliberate decision
  that belongs with a firewall and a reverse proxy, not with a wildcard `vhosts`.
- **`personal` is gone from `--http.api`** because the node drops it anyway:
  `node/node.go` filters the namespace out unless
  `--rpc.enabledeprecatedpersonal` is passed. Asking for it achieved nothing.
- **`debug` is not there either**, and that one is load-bearing: since `v1.11.10` a node
  *refuses to start* when `debug` is listed on an untrusted transport without
  `--http.debug-profile` (`node/debugprofile.go`, reached from `node.go`). If you want
  it, add a profile — `--http.debug-profile trace-indexer-v1` — rather than the bare
  namespace.

***Note:*** Replace `YOUR_ACCOUNT_ADDRESS_HERE` with the address generated in step 3

***Node:*** Adjust `--miner.threads 1` based on your machines capabilities!

Run the client using:
```bash
./run.sh
```
