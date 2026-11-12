# Miner signatures

Written for node operators, whoever runs the release, and reviewers of the rule.

A DPoW block names a staked coinbase, and on its own nothing ties the block to whoever controls that address. This rule does: past a total difficulty threshold, a block must be signed by its coinbase's own key, or by the key the chain config lists for that coinbase. The registry check in `DPOW_SPECIFICATION.md` stays as it is and applies in addition. Entry stays open: anyone who stakes and matures can mine, provided the node can hold the key of the staking address.

## The rule

`ChainConfig.MinerSignerEpochs` is a list of `{td, signers}`, where `signers` maps a coinbase to the address that signs its blocks. A header's own total difficulty is its parent's plus its difficulty. The last epoch whose `td` it has reached applies to it.

Below the first epoch nothing changes: extra-data is at most 32 bytes and carries no signature.

From the first epoch on, in `Ethash.verifyHeader`:

- A coinbase the epoch lists signs with the listed key. Any other coinbase signs with its own key.
- The block that crosses into an epoch, whose parent is below its `td`, must come from a listed coinbase.
- Extra-data is `vanity || signature`: 0 to 32 bytes of vanity, then 65 bytes `R || S || V` with `V` in `{0, 1}` and low `S`.
- The signature is over `keccak256(rlp([tag, chainId, SealHash(header with extra-data = vanity)]))`, with the tag `incentiv/miner-header-sig/v1`.
- `SealHash` itself is unchanged. It hashes the whole extra-data, so the PoW commits to the signature and changing a signature invalidates the seal.

The check runs before the PoW and whatever the seal flag says, so it also covers headers whose seal a sync only samples, and uncles. Whether a coinbase may mine at all stays with the registry, at execution. Blocks are signed in `FinalizeAndAssemble`, before sealing. External miners behind `eth_getWork` see only the seal hash and never need the key.

Fake modes skip the rule, as they skip the seal and the registry, so tests of the rule have to use `ethash.NewTester`. `ModeFake`, which `geth --fakepow import` runs, then treats every header as below the threshold and keeps the 32-byte limit: it accepts an unsigned chain past the threshold and stops at the first signed block with `extra-data too long`. Import a signed chain without `--fakepow`.

## Why total difficulty

A chain cannot get past the threshold without a block from a listed coinbase. Fork choice already compares total difficulty, so a chain below the threshold cannot outweigh one past it, whatever its timestamps say, and `ReorgNeeded` needs no change. Time or height activation does not give this: a chain with old timestamps stays under the old rules however much work it holds, and one signature on top of it would then legitimize that work.

Two more properties follow. The threshold is not part of the fork id, so it is not announced to peers. And it has no entry in `CheckCompatible`, so a changed value raises no `ConfigCompatError`. It can still rewind a node, through the startup check below.

A node that crossed the threshold on an older binary is on a chain the network rejects, and fork choice alone does not take it off if that chain is heavier. At startup `BlockChain` checks the first block past each threshold on its header chain and rewinds below it if that block fails the rule, and a light client does the same with its headers. The check does not know how the node got there, so a threshold that moves onto such a block rewinds the same way. A side block past the threshold at or below the new head stays stored, but it cannot come back: the rule refuses it when it is sent again and judges a stored parent when its child arrives.

A pruned node goes back to the nearest state it has on disk, as far as the genesis block. On the genesis block, a node that never snap synced and has blocks in its ancient store, which is any node past block 90,000, deletes its whole chain and syncs in full. Any other node left on the genesis block with headers above it switches to snap sync whatever `--syncmode` says and logs `Switch sync mode from full sync to snap sync`: one that snap synced, one at or below block 90,000, one that crashed with no state on disk, or one killed during that deletion, which on a long chain takes minutes with no progress line. Blocks below the snap pivot then do not run, so the registry is not checked for them. To sync such a node in full, delete its chain database and its ancient store.

## Why the crossing is listed

The maturity period gives everybody time to see a new miner, but only for a stake in the public chain. A fork below the threshold needs no signatures, so a stake made on a private fork could mature there, unseen, under someone else's coinbase. If its owner could then cross the threshold with their own key, that fork would be valid with no stake ever visible in the public chain.

Requiring a listed coinbase for the crossing stops that fork at the threshold. Past the threshold the problem does not exist: the first block of a fork already needs a key that is mature in the state it forks from, and that is a stake everybody could see.

This rests on execution, because the signature alone lets any key through. A fork counts only once all its blocks have run, from the last block that did. A side block stored unexecuted gets no credit for a state its root names, even one that is in the database. With block rewards at zero an empty block keeps its parent's root, so this is what stops a block by a key in no registry from passing as known. It also stops a block from leading to a stake on a fork below the threshold.

Execution writes a dedicated database mark with the block and its receipts. Receipts alone, including those downloaded by snap sync or left by an older binary, do not prove execution. A block without a mark counts as executed only as the genesis block, as the full-block head or one of its own ancestors, found by following parent hashes, or as a frozen block that matches the immutable frozen canonical table. These rules are permanent, not a transition: a database written before the marks existed has none, and freezing a block deletes its mark. The canonical index is no proof, as header sync rewrites it below the head too. It can only reject a block, and only while it agrees with the head at the head's height, so a true ancestor of the head at a wrong entry runs again if a fork off it is adopted. Old side blocks run again before adoption. A block without a mark whose state is present costs one walk of the head's ancestry, up to 90,000 headers, shared by the checks under the same head. The frozen comparison assumes that the frozen table holds only ancestors of the full head, which holds while no reorg goes deeper than the freeze threshold, the limit the downloader enforces and the freezer itself relies on. Header sync can replace the index independently of the full head, so a rewind first moves the header chain, the index and the snap head back onto the full head, rather than promoting unexecuted blocks from the header chain. A rewind target above the full head is not kept for the header chain then, since the index above the head would name fork blocks whose parents are not canonical: the header head comes down to the full head and the fork is fetched again.

This validation change applies immediately on upgrade, without an activation threshold. It preserves an old canonical chain even if an older binary skipped execution there; the marks cannot detect that history. The rollout must compare the same block hash as well as its total difficulty across nodes. Snap sync still trusts its pivot and does not check the registry below it, as described above. An older binary ignores the marks, and its existing rules and rollback limits still apply; upgrading again re-executes side blocks without a mark if they are adopted. The older binary does not delete the marks either: a block it deletes keeps its mark, and if that block is stored again without running, it counts as executed after the next upgrade.

## What it does not cover

- **Before the crossing** an unsigned fork is valid, and it can reach back as far as the downloader allows (90,000 blocks). If a listed producer signs on top of such a fork before the public chain has crossed, whatever is on it becomes the chain, including a stake. Such a fork can also carry a far higher difficulty than the public chain, so one signature on it can overtake a chain that crossed a few blocks earlier. Its difficulty and gas limit carry over: modelled at 3.1 MH/s, blocks from a difficulty of 8e8 take about 9 hours to get back under 15 seconds, and a gas limit walked down to about 14,000 takes about 7.5 hours to fit a transfer again. Keep devp2p closed to the fleet and bridge credits paused until the crossing plus a few blocks.
- **A listed producer below the threshold** signs on whatever head it has, also long after the crossing. If it is new, restored or rewound and takes a fork below the threshold first, one block from it carries that fork across, and a stake on the fork can then sign. Sync such a producer from the fleet alone before it mines.
- **A staked miner** with enough hash power can still reorg, and so can whoever holds its key. An unlisted coinbase reaches as far back as the last crossing: a fork that starts below a threshold needs a listed block to pass it. A listed coinbase makes that block itself, so whoever holds its signer key can sign a fork across the threshold of any epoch that lists the coinbase with that key. If every epoch so far does, the crossing bounds nothing and only the downloader limits the fork (90,000 blocks), and a stake made on the fork can sign once it has crossed, unseen in the public chain. A coinbase that has since unstaked can do the same, from a block where its stake still stood. The stake is locked, not at risk: `UNSTAKE_DELAY` and the timelock are both 7 days, so governance cannot be sure to seize it. The maturity period is the time to look at a new miner; see below for what can be done in it.
- **A stolen signer key** with enough hash power can reorg until a release with an epoch that lists every coinbase the key signs for with another signer. From that threshold on the key's blocks are rejected, so a fork signed by it cannot pass the threshold. An epoch that does not name an unlisted miner's coinbase does not stop that miner's key: it cannot cross, but a fork that starts past the threshold does not need to.
- **Stake keys** can still stop a producer: `requestUnstake` removes a coinbase from the registry at once. Alert on `UnstakeRequested`. A coinbase listed with a dedicated key keeps its stake key off the hosts. One that signs with its own key, listed or not, cannot: whoever takes that node can also unstake it and, 7 days later, withdraw the stake.
- **Header-only sync** checks the signature but not the registry, because it has no state, and any fresh key can sign for its own address. Run producers, RPC and archive nodes with `--syncmode full`. A node left on the genesis block can fall back to snap sync anyway, as described under "Why total difficulty", so sync such a node from the fleet alone.
- **Re-mining** a published block is possible without the key: same content, new nonce, new hash. Its children still need a signature, so this gives no way to rewrite anything.

## Reacting to a new stake

`MinerStaked` is public, and a stake takes 86,400 seconds and 17,280 blocks to mature. Alert on that event.

`emergencyRemoveMiner` and `setPaused` go through the 7-day timelock, so neither can act inside that window. A release can: add an epoch whose threshold is close, listing that coinbase with a signer nobody holds, such as `0x000000000000000000000000000000000000dEaD`. From that threshold on its blocks are rejected, whatever the registry says, until an epoch leaves the entry out. The list has to keep the producers that are meant to cross and every earlier shut-out. The epoch stops the coinbase it names and no other: a fork started below the threshold by a mature staker can still bring a stake of another coinbase across, if a listed producer signs on top of it.

## Running a producer

```
--miner.signerkey <keystore file> --miner.signerpassword <password file>
```

The key is decrypted at startup and held by the consensus engine only. It is not in the account manager, so no RPC method can reach it, and `--unlock` stays forbidden. Non-mining nodes need no key.

The list can name a coinbase's own key, or a dedicated key that holds no funds and no stake. A dedicated key moves nothing: the coinbase and its stake stay where they are. A lost one is replaced through a new epoch, so it needs no backup. A coinbase that signs with its own key, listed or not, has the key that controls its stake on the node. `stake()` registers `msg.sender` and `--miner.signerkey` reads only a keystore file, so a stake made from a hardware wallet or a contract leaves nothing to load: that coinbase has to be listed with a dedicated key, or stake again from an address with a keystore, which for the same funds means the 7-day unstake first. Getting listed takes a release.

- Generate a dedicated key on the host, after the host has been checked: `geth account new --keystore /etc/incentum/signer`. A coinbase's own key goes on the host as its keystore file, outside the node's keystore directory.
- Keep both files outside the datadir, outside home and outside any snapshot or container mount.
- Own them `root:root` with mode `0400`, and deliver them with systemd `LoadCredential=`. Add `LimitCORE=0`. The example unit in `DPOW_NODE_OPERATOR_GUIDE.md` §3.2 shows both.
- Leave no copy in the keystore directory. The check below sees only the path it is given, and a copy there is an ordinary account.

The node refuses to start when:

- a key file is unreadable, the password is wrong, or either file it is given has a group or other permission bit set, as anything but `0400` or `0600` has. A file directly in `$CREDENTIALS_DIRECTORY` is held to the other bits only: for a unit with `User=`, systemd grants the read through an ACL where the filesystem has them, and the copy then reads `0440`. Under `LoadCredential=` the node is given these private copies, so this never fires for an open source file: check the mode of the sources yourself;
- the key file is inside the keystore directory, where the account manager would pick it up;
- mining is requested on a chain with epochs and no key is loaded, or the key can sign for the etherbase in no epoch the head has yet to leave. A head below the threshold excuses neither, so a producer with no key, or with a key that fits none of those epochs, fails at start rather than going silent at the threshold. A key that fits any of them passes even if it cannot sign the next block; see "Changing signers".

`miner_setEtherbase` returns `false` if the loaded key cannot sign for the new address, whether the miner is running or paused for a sync. A failed block assembly logs `Failed to assemble sealing block`, every five minutes while the failure stays the same. `eth.mining` stays `true` in that case and `eth_getWork` keeps serving the last block assembled, so rigs go on hashing: judge a producer by its blocks on an independent RPC node. An unlisted miner logs this until a listed coinbase has crossed.

## Rollout

1. **Rehearse on devnet** with its own epoch in the bundled config, including a producer with a wrong key and a node left on the old binary.
2. **Settle the signers** and collect their addresses: each producer's own coinbase key, or a dedicated key generated on it, as in "Running a producer".
3. **Close the window** before the release goes out: restrict devp2p to the fleet and pause bridge credits, then check from a host outside the fleet that the devp2p port does not answer. An allowlisted address outside the fleet can deliver a fork directly, so keep only those the team has reviewed.
4. **Prepare the release.** Put the signer list and the threshold in the network's config. List every producer meant to be able to cross. Take the threshold from `totalDifficulty` at the head plus a margin. At about 3.1 MH/s honest work is about 1.1e10 per hour, so a margin of 1e11 is about 9 hours. Choose the margin for how long the rollout takes.
5. **Check the keys on the producers** with the new binary before their restart.
6. **Upgrade every node** following the fleet's upgrade runbook, which is kept with the node inventory and not in this repo, one producer at a time. Operators outside the fleet follow `DPOW_NODE_OPERATOR_GUIDE.md` §4. Producers need `--miner.signerkey` and `--miner.signerpassword` added to the unit in the same restart. A node outside the fleet, such as an exchange's, needs the release before the crossing too: an old binary stops following at the first signed block.
7. **Roll-call** over IPC on every node: the binary, `admin.nodeInfo.protocols.eth.config.minerSignerEpochs` (under `protocols.les` on a light client), and `eth.getBlock(<height>)` for one block, whose `hash` and `totalDifficulty` must be the same on every node. The runbook names the height, a recent block below the head of every node, and takes the expected hash from a node whose chain was checked independently, such as an archive node that executed it in full. A node with another hash is on a chain the fleet does not follow, which can be one that an older binary accepted without running it, and the execution marks cannot detect that: stop the rollout, and resync that node or re-execute its chain from a trusted ancestor with `debug_setHead`. The second only re-executes on a node that was full synced from the genesis block, or whose target is at or above its snap sync pivot: below the pivot the node snap syncs again and executes nothing there, so resync it. On producers: `Loaded miner block signer` with the expected address. Old binaries are not cut off by the fork id here, so this is the only check that they are gone.
8. **Crossing.** Watch an independent RPC node for the first block with a signature (extra-data over 32 bytes: the vanity plus 65, which is 91 for a release binary with the default vanity), and for a block from each producer. In the logs, look for `invalid miner signature`, `extra-data too long`, `Failed to assemble sealing block` and `BAD BLOCK`.
9. **Reopen** devp2p and bridge credits, no earlier than the crossing plus a few blocks on an independent RPC node.

A producer that misses the crossing costs its share of the hash rate until it is fixed. Blocks slow down accordingly and difficulty adjusts. If no listed producer can sign, the chain stops at the threshold, whoever else is staked.

Rolling back to the previous binary works only before the crossing. After it, the only way is forward.

## Holding it back

`--override.minersignertd=<td>` moves the first threshold. The value must be identical on every node, `geth import` included, and the flag stays on the command line until a release bakes the new value in. Use it only while no signed block exists, which you check on an independent RPC node, and read `totalDifficulty` on the node before changing the value.

A value above the head rewinds nothing, so a node whose threshold was raised past its head rejects signed blocks and stops there. At or below the head the startup check judges the first block past the value. If that block fails the rule, as any unsigned block does, the node rewinds below it, and further if its state is not on disk: a mistyped low value can take a node back to the genesis block. `geth export` cannot rewind, so it refuses such a chain until the node has started once. Dropping the flag moves the value too only where the start replaces the stored config: on a network this binary ships a config for, or under a network flag or a genesis file. There the bundled or genesis threshold comes back and the node logs `Stored miner signer epochs replaced`, and a node past it on unsigned blocks rewinds below it and refuses the fleet's blocks until the flag is back and it has fetched them again. Anywhere else, on a private chain or where the bundled config is refused, the value stays where the last start left it and nothing is logged, until the flag is passed again with another value.

During a hold, a producer without a key needs `--miner.nosigner` to start mining.

## Changing signers

Append an epoch with a later `td` in a new release, with its own margin. Never edit or remove an earlier epoch: nodes syncing from scratch verify history against it, and a start that replaces one logs `Stored miner signer epochs replaced`. The crossing into the new epoch needs a coinbase listed in it. Until that crossing a fork below the new threshold has the reach described under "What it does not cover", so keep devp2p closed for it, as for the first one.

Each epoch replaces the list: a coinbase the new one leaves out signs with its own key from that threshold on. That retires a producer's dedicated key, and lets a shut-out coinbase back in with no new stake and no event to alert on, so copy the previous list and change only what is meant to change.

A producer holds one key, read at start, and for a coinbase whose signer changes no key works on both sides of the threshold: the new key assembles nothing until the next block crosses, and the old key assembles nothing from the crossing on. The start check accepts both until the head has reached the epoch, so it does not catch an early swap, and the swap does not belong in the restart onto the release. If every miner that can sign below the threshold swaps early, the chain stops short of it until one goes back to the old key. Leave the old key on one producer, or let any coinbase whose signer does not change carry the chain to the threshold, with the new key on another producer to make the crossing. With a single producer, keep the old key until assembly fails with `signer key is <old>, coinbase <c> needs <new>`, then restart with the new key. A block's difficulty falls as its timestamp ages, so after the restart the same block may no longer cross and the failure names the old key again: load the key the failure names. Once the head has crossed, restart anything still on the old key, which the start check now refuses. If the old key is lost and nothing else mines, set the new epoch's `td` to one above the head, because no margin can be reached.

The startup check looks only at the first block of each epoch, so a node that followed a retired key past a later epoch's first block has to be rewound by hand with `debug_setHead`, with `--mine` off or the new key in place, because the start check refuses the retired one.

## Compatibility

Extra-data grows to at most 97 bytes: the vanity plus 65. The default vanity holds the Go version and OS of the build, so a release binary (go1.22.12, linux) gives 91 and a source build on go1.22.2 gives 90. `web3.py` raises `ExtraDataLengthError` on such a block unless the POA middleware is installed, and with it the field moves to `proofOfAuthorityData`. Anything decoding the vanity as RLP has to cut the last 65 bytes first. ethers, viem and alloy accept the longer field.
