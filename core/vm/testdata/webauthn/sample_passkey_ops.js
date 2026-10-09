#!/usr/bin/env node
/*
 * Merges harvest_passkey_ops.js outputs and writes the committed test fixture:
 * a deterministic, chronologically spread sample small enough to live in the node repo,
 * keeping at least one operation per distinct account so every key shape in the data is
 * represented. Pure data reshaping — no network access.
 *
 * Usage: node sample_passkey_ops.js OUT.json TARGET_COUNT IN1.json [IN2.json ...]
 */
'use strict'
const fs = require('fs')
const { blockRangeProblem } = require('./block_range.js')

// mergeHarvests concatenates harvests, and refuses the ones that cannot belong to the same
// fixture.
//
// The checks are here because nothing downstream makes them. The fixture declares one
// chainId and one entryPoint, taken from the first input, and the Go replay asserts on that
// declaration rather than on the records — so a harvest of another chain, or of a different
// EntryPoint, merges in silently and the compatibility evidence then describes traffic it
// never replayed. Merging several windows is the normal way to use this script, which is
// exactly when the wrong file gets passed.
//
// Each entry is {name, doc}; the name appears in the errors, so a refusal says which file to
// look at.
function mergeHarvests(entries) {
  if (entries.length === 0) throw new Error('no harvests to merge')
  const windows = []
  let all = []
  let source = null
  let first = null
  for (const { name, doc } of entries) {
    if (!doc || typeof doc !== 'object' || !doc.source || !Array.isArray(doc.ops)) {
      throw new Error(`${name}: not a harvest — expected {source, counts, ops: []}`)
    }
    const s = doc.source
    if (typeof s.chainId !== 'number' || !s.entryPoint) {
      throw new Error(`${name}: harvest source has no chainId or entryPoint`)
    }
    // The range is published as provenance, and since a window that reconstructed nothing is
    // recorded rather than dropped, a range that cannot exist would be published as a range
    // that was measured and came back empty. A reversed one is the case that matters: the
    // harvest's scan loop walks it zero times, so it produces exactly that empty window.
    // The same rule the harvest applies, from the same module, so the two cannot drift apart.
    const rangeProblem = blockRangeProblem(s.fromBlock, s.toBlock)
    if (rangeProblem) {
      throw new Error(`${name}: harvest source ${rangeProblem}`)
    }
    // The counts are provenance the fixture publishes rather than decoration, but only
    // passkeyOps can be required: four of the eleven harvests behind the committed fixture
    // carry no entryPointRevisions, and none of them records skipped per window — both are in
    // README.md under Provenance. Requiring either would refuse the documented reproduction
    // command on the very files it names.
    const c = doc.counts
    if (!c || !Number.isInteger(c.passkeyOps) || c.passkeyOps < 0) {
      throw new Error(`${name}: harvest has no counts.passkeyOps (a count of the operations in it)`)
    }
    // passkeyOps is what the window record publishes; doc.ops is what gets merged under it. A
    // disagreement means the provenance does not describe the records, and it is provenance
    // that gets read: the residual in docs/webauthn/replay-validation.md is computed against
    // these counts, so an inflated one understates the gap. The identity holds in the committed
    // fixture — its eleven windows' passkeyOps add up to exactly its sampledFrom — and
    // harvest_passkey_ops.js writes the count as the length of the same array.
    if (c.passkeyOps !== doc.ops.length) {
      throw new Error(`${name}: counts.passkeyOps is ${c.passkeyOps}, but the file holds ${doc.ops.length} operations`)
    }
    // Either of the optional counts may be missing, but a present one has to be a table of
    // counts. Carrying it through as `value || null` writes a string, an array or a number into
    // the fixture's provenance unexamined, and turns a falsy one — 0 — into null, which reads as
    // "this harvest did not record it" rather than as the corruption it is.
    for (const field of ['entryPointRevisions', 'skipped']) {
      const table = c[field]
      if (table === undefined || table === null) continue
      if (typeof table !== 'object' || Array.isArray(table)) {
        throw new Error(`${name}: counts.${field} is ${JSON.stringify(table)}, expected an object of counts`)
      }
      for (const [k, v] of Object.entries(table)) {
        if (!Number.isSafeInteger(v) || v < 0) {
          throw new Error(`${name}: counts.${field}.${k} is ${JSON.stringify(v)}, expected a whole count of 0 or more`)
        }
      }
    }
    // skipped may be absent — none of the eleven harvests behind the committed fixture records it
    // — but a partial one cannot come from this repository: every revision of
    // harvest_passkey_ops.js initialises all five counters before the scan begins, so a table
    // carrying some of them is a file that was edited or truncated on the way here.
    //
    // Requiring it whole is also what makes the sum below unconditional. Guarding that sum on
    // completeness instead, as this did, left a way around it: entryPointRevisions {'v0.8': 999}
    // beside a skipped of two keys was accepted, because the term it needed was the term missing.
    const SKIPPED_KEYS = ['txUnavailable', 'undecodable', 'unmatched', 'notPasskeySig', 'shortSig']
    const sk = c.skipped
    if (sk) {
      const missing = SKIPPED_KEYS.filter((k) => !Number.isSafeInteger(sk[k]))
      if (missing.length > 0) {
        throw new Error(
          `${name}: counts.skipped is missing ${missing.join(', ')} — every revision of the harvest writes all ${SKIPPED_KEYS.length}`
        )
      }
    }
    // Where a harvest carries both tables, they have to add up. In harvest_passkey_ops.js every
    // event of a decoded transaction ends in exactly one place — a record, or unmatched, or
    // notPasskeySig, or shortSig — and entryPointRevisions counts those same events by revision,
    // so the sum of its values is passkeyOps plus those three. txUnavailable and undecodable are
    // events whose transaction never got decoded, so they are outside both sides of it. This is
    // the arithmetic the residual in docs/webauthn/replay-validation.md is built on — 19,595
    // decoded, 12,715 replayed, 6,880 unaccounted — so a harvest that contradicts it cannot be
    // merged into the evidence.
    //
    // Added as BigInt, not as Number. Each value is a safe integer by the checks above, but their
    // sums need not be: 9007199254740991 + 2 and 1 + 9007199254740991 are different totals that
    // round to the same double, so a pair of tables that disagree by one compared equal and the sum
    // could be stepped around. Every term is exact as a BigInt, and the comparison with it.
    if (c.entryPointRevisions && sk) {
      const decoded = Object.values(c.entryPointRevisions).reduce((a, b) => a + BigInt(b), 0n)
      const accounted = BigInt(c.passkeyOps) + BigInt(sk.unmatched) + BigInt(sk.notPasskeySig) + BigInt(sk.shortSig)
      if (decoded !== accounted) {
        throw new Error(
          `${name}: counts do not add up — ${decoded} decoded events, ${accounted} accounted for ` +
            `(passkeyOps ${c.passkeyOps} + unmatched ${sk.unmatched} + notPasskeySig ${sk.notPasskeySig} + shortSig ${sk.shortSig})`
        )
      }
    }
    // And the range has to contain them. Every operation comes from getLogs over the declared
    // range, so one outside it means the source block range belongs to a different harvest than
    // the operations do — the record would then name a range the fixture never drew from.
    for (const o of doc.ops) {
      if (typeof o.txHash !== 'string' || typeof o.sender !== 'string' || typeof o.input !== 'string') {
        throw new Error(`${name}: an operation is missing txHash, sender or input`)
      }
      if (!Number.isInteger(o.block) || o.block < s.fromBlock || o.block > s.toBlock) {
        throw new Error(`${name}: an operation at block ${o.block} is outside the declared range ${s.fromBlock}..${s.toBlock}`)
      }
    }
    if (source === null) {
      source = s
      first = name
    } else if (s.chainId !== source.chainId) {
      throw new Error(`${name}: chain ${s.chainId}, but ${first} is chain ${source.chainId}`)
    } else if (s.entryPoint.toLowerCase() !== source.entryPoint.toLowerCase()) {
      throw new Error(`${name}: EntryPoint ${s.entryPoint}, but ${first} used ${source.entryPoint}`)
    }
    // A window that reconstructed nothing is recorded all the same. Nothing reconstructed is
    // not nothing to report: every event in that range may have been unmatched or shortSig,
    // which is the coverage gap the measurement before activation exists to show, and dropping
    // the record publishes that gap as zero. It contributes its counts and no operations.
    windows.push({
      fromBlock: s.fromBlock,
      toBlock: s.toBlock,
      passkeyOps: c.passkeyOps,
      entryPointRevisions: c.entryPointRevisions || null,
      // Why the events from passkey-owned senders that were not reconstructed were not:
      // notPasskeySig never reached 0x111 and is no gap, unmatched and shortSig are. Without
      // this the fixture records only the two totals and the residual cannot be read.
      //
      // null rather than a missing key for both of these, because a missing key reads as a
      // window with nothing skipped and no revisions seen, which is the opposite of what an
      // unrecorded count means. The fixture's own windows leave them out, and that is the
      // ambiguity being fixed here.
      skipped: c.skipped || null,
    })
    all = all.concat(doc.ops)
  }
  if (all.length === 0) throw new Error('every harvest is empty: nothing to sample')
  all.sort((a, b) => a.block - b.block || a.txHash.localeCompare(b.txHash) || a.sender.localeCompare(b.sender))
  // Only the three fields the fixture publishes, taken from the first harvest. Returning the
  // harvest's own source object instead would hand the caller everything it recorded — the
  // endpoint among it — and the point of not writing that into the fixture is that it has
  // nowhere left to travel.
  return {
    source: { chainId: source.chainId, entryPoint: source.entryPoint, harvestedAt: source.harvestedAt },
    windows,
    all,
  }
}

// pickSample cuts the merged operations down to `target`: one operation per account first — a
// key with an unusual shape must not be sampled away — then fill up to `target` by taking
// every n-th of the remainder. Overlapping windows deliver the same operation twice, which
// the key folds back together. More accounts than `target` means more than `target` records:
// representing every account is the stronger promise.
function pickSample(all, target) {
  const picked = new Map() // key: block|txHash|sender|input prefix
  const keyOf = (o) => `${o.block}|${o.txHash}|${o.sender}|${o.input.slice(0, 32)}`
  const seenSender = new Set()
  for (const o of all) {
    if (seenSender.has(o.sender)) continue
    seenSender.add(o.sender)
    picked.set(keyOf(o), o)
  }
  const remaining = all.filter((o) => !picked.has(keyOf(o)))
  const want = Math.max(0, target - picked.size)
  if (want > 0 && remaining.length > 0) {
    const step = Math.max(1, Math.floor(remaining.length / want))
    for (let i = 0; i < remaining.length && picked.size < target; i += step) {
      picked.set(keyOf(remaining[i]), remaining[i])
    }
  }
  return [...picked.values()]
    .sort((a, b) => a.block - b.block || a.txHash.localeCompare(b.txHash) || a.sender.localeCompare(b.sender))
    .map((o) => ({ block: o.block, txHash: o.txHash, sender: o.sender, input: o.input }))
}

function main() {
  const [out, targetArg, ...inputs] = process.argv.slice(2)
  if (!out || !targetArg || inputs.length === 0) {
    console.error('usage: sample_passkey_ops.js OUT.json TARGET_COUNT IN1.json [IN2.json ...]')
    process.exit(2)
  }
  const target = Number(targetArg)
  if (!Number.isInteger(target) || target <= 0) {
    // Number('five') is NaN, and NaN left the fill loop out while the one-per-account pass
    // still ran — a fixture of whatever size that came to, written without complaint.
    console.error(`TARGET_COUNT must be a positive whole number, got ${JSON.stringify(targetArg)}`)
    process.exit(2)
  }
  const entries = inputs.map((name) => ({ name, doc: JSON.parse(fs.readFileSync(name, 'utf8')) }))
  const { source, windows, all } = mergeHarvests(entries)
  const ops = pickSample(all, target)

  const doc = {
    source: {
      chainId: source.chainId,
      entryPoint: source.entryPoint,
      // harvestedAt is the first harvest's; the block ranges of all of them are in
      // `windows`. chainId and entryPoint are the two that have to agree, and do.
      //
      // A harvest's endpoint is not carried, even when one records it. An endpoint URL is a
      // place an API key rides along — in its path, its query, its userinfo, or its host name
      // — and this file is the one that gets committed. Dropping it needs no rule and leaves
      // no value for a diagnostic to quote.
      harvestedAt: source.harvestedAt,
      note:
        'Sampled from a larger harvest; see core/vm/testdata/webauthn/README.md. ' +
        'input = abi.encodePacked(userOpHash, signaturePayload, publicKey.x, publicKey.y) ' +
        'for a passkey UserOperation mined on Incentiv mainnet. All values are public chain data.',
      windows,
      sampledFrom: all.length,
      distinctAccountsInSample: new Set(ops.map((o) => o.sender)).size,
    },
    ops,
  }
  fs.writeFileSync(out, JSON.stringify(doc, null, 1) + '\n')
  console.error(`${ops.length} of ${all.length} operations -> ${out} (${new Set(ops.map((o) => o.sender)).size} accounts)`)
}

module.exports = { mergeHarvests, pickSample }

// Only sample when run as a program, so that requiring this file for its test cannot write a
// fixture.
if (require.main === module) {
  try {
    main()
  } catch (e) {
    console.error(`error: ${(e && e.message) || e}`)
    process.exit(1)
  }
}
