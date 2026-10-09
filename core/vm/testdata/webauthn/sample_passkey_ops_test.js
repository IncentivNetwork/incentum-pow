#!/usr/bin/env node
/*
 * Tests for sample_passkey_ops.js: the checks that keep harvests which cannot belong to one
 * fixture from being merged into one, and the promise the fixture rests on — every account
 * in the data is represented in the sample.
 *
 * No dependencies and no network. It writes only inside a temporary directory, which it
 * removes:
 *
 *   node sample_passkey_ops_test.js
 *
 * core/vm/harvest_script_test.go runs this through `go test`, CI included.
 */
'use strict'
const assert = require('node:assert/strict')
const { mergeHarvests, pickSample } = require('./sample_passkey_ops.js')

const EP = '0xA1b2C3d4E5f60718293a4B5c6D7e8F9012345678'

const op = (block, tag, sender) => ({
  block,
  txHash: `0xtx${tag}`,
  sender: sender || `0xsender${tag}`,
  input: `0x${tag}`.padEnd(66, '0'),
})

// skips builds the counter table harvest_passkey_ops.js writes: all five of them, initialised
// before the scan, which is why a harvest carrying some of them is refused. A fixture meant to be
// valid goes through here rather than naming the one counter a case cares about.
const skips = (over) => ({ txUnavailable: 0, undecodable: 0, unmatched: 0, notPasskeySig: 0, shortSig: 0, ...over })

// harvest builds a document shaped like harvest_passkey_ops.js writes one. `over` replaces
// whole keys, so a case can drop or corrupt exactly one part of it.
const harvest = (over) =>
  Object.assign(
    {
      source: { chainId: 24101, entryPoint: EP, rpc: 'http://rpc', fromBlock: 1, toBlock: 9, harvestedAt: 'then' },
      counts: { passkeyOps: 1, entryPointRevisions: { 'v0.7': 1 }, skipped: skips() },
      ops: [op(1, 'a')],
    },
    over || {}
  )

const named = (docs) => docs.map((doc, i) => ({ name: `in${i}.json`, doc }))
const refuses = (docs, re) => assert.throws(() => mergeHarvests(named(docs)), re)

// Two windows of the same chain merge, and the first one's source is the one that is kept.
{
  const second = harvest({
    source: { chainId: 24101, entryPoint: EP, rpc: 'http://other', fromBlock: 10, toBlock: 20, harvestedAt: 'now' },
    ops: [op(11, 'b')],
  })
  const { source, windows, all } = mergeHarvests(named([harvest(), second]))
  assert.equal(source.harvestedAt, 'then')
  assert.deepEqual(Object.keys(source).sort(), ['chainId', 'entryPoint', 'harvestedAt'])
  assert.deepEqual(
    windows.map((w) => [w.fromBlock, w.toBlock]),
    [
      [1, 9],
      [10, 20],
    ]
  )
  assert.deepEqual(
    all.map((o) => o.block),
    [1, 11]
  )
}

// A harvest of another chain, or through another EntryPoint, is the mistake worth catching:
// the fixture would declare the first file's chain and the Go replay would believe it.
refuses([harvest(), harvest({ source: { chainId: 11155111, entryPoint: EP, fromBlock: 1, toBlock: 9 } })], /chain 11155111/)
refuses(
  [harvest(), harvest({ source: { chainId: 24101, entryPoint: '0x0000000000000000000000000000000000000111', fromBlock: 1, toBlock: 9 } })],
  /EntryPoint 0x0000000000000000000000000000000000000111/
)

// The same EntryPoint in a different checksum case is the same EntryPoint.
{
  const other = harvest({
    source: { chainId: 24101, entryPoint: EP.toLowerCase(), fromBlock: 10, toBlock: 20 },
    ops: [op(11, 'b')],
  })
  assert.equal(mergeHarvests(named([harvest(), other])).windows.length, 2)
}

// A harvest's endpoint is not carried into the fixture, whatever it holds. An endpoint URL is a
// place an API key rides along — in its path, its query, its userinfo, or the host name itself —
// and the fixture is the file that gets committed. Harvests taken before this was true still have
// the field, so the sampler has to drop it rather than refuse them, and it must not end up in a
// message either.
for (const rpc of [
  'https://rpc.example/v3/SECRETKEY',
  'https://user:pw@rpc.example',
  'https://rpc.example/?key=abc',
  'https://SECRETKEY.rpc.example',
  'rpc.example',
  42,
]) {
  const doc = harvest({ source: { chainId: 24101, entryPoint: EP, rpc, fromBlock: 1, toBlock: 9 } })
  const merged = mergeHarvests(named([doc]))
  assert.equal(merged.windows.length, 1, `a harvest recording ${JSON.stringify(rpc)} was refused`)
  assert.equal(merged.source.rpc, undefined, `the endpoint ${JSON.stringify(rpc)} was carried through`)
  assert.doesNotMatch(JSON.stringify(merged), /SECRETKEY|user:pw/, 'the endpoint reached the merged source')
}

// Shape: anything that is not a harvest, and a harvest missing the provenance the fixture
// publishes. Each of these used to reach the output as `undefined` or a TypeError stack.
refuses([{ ops: [] }], /not a harvest/)
refuses([{ source: {}, ops: [] }], /no chainId or entryPoint/)
// The range rule itself is block_range_test.js's; these pin that the sampler applies it to a
// harvest's declared range and says which file the bad one is.
refuses([harvest({ source: { chainId: 24101, entryPoint: EP } })], /in0\.json: harvest source fromBlock is not a whole block number/)
refuses([harvest({ source: { chainId: 24101, entryPoint: EP, fromBlock: -1, toBlock: 9 } })], /fromBlock is not a whole block number of 0 or more: -1/)
refuses([harvest({ source: { chainId: 24101, entryPoint: EP, fromBlock: 1.5, toBlock: 9 } })], /fromBlock is not a whole block number/)
refuses([harvest({ source: { chainId: 24101, entryPoint: EP, fromBlock: 1, toBlock: Number.MAX_SAFE_INTEGER + 2 } })], /toBlock is not a whole block number/)

// A range that runs backwards is the one the harvest can produce on its own: its scan loop
// walks such a range zero times and writes a harvest of no operations, which the sampler would
// otherwise publish as a window that was measured and found empty. Merged beside a valid
// window, exactly as it would arrive.
refuses(
  [
    harvest(),
    harvest({ source: { chainId: 24101, entryPoint: EP, fromBlock: 10, toBlock: 9 }, counts: { passkeyOps: 0 }, ops: [] }),
  ],
  /range runs backwards: 10\.\.9/
)
refuses([harvest({ counts: { entryPointRevisions: {}, skipped: {} } })], /counts\.passkeyOps/)
refuses([harvest({ counts: { passkeyOps: '1', entryPointRevisions: {}, skipped: {} } })], /counts\.passkeyOps/)
refuses([harvest({ counts: { passkeyOps: -1, entryPointRevisions: {}, skipped: {} } })], /counts\.passkeyOps/)

// The other two counts are optional, and have to be: four of the eleven harvests behind the
// committed fixture carry no entryPointRevisions and none of them carries skipped, so a
// harvest without them is not a corrupt file — it is the input the documented command names.
// They are recorded as null, because a missing key would read as "nothing was skipped".
{
  const early = harvest({ counts: { passkeyOps: 1 } })
  const [w] = mergeHarvests(named([early])).windows
  assert.equal(w.entryPointRevisions, null)
  assert.equal(w.skipped, null)
  assert.equal(w.passkeyOps, 1)
}

// A count that disagrees with the operations in the file, in either direction. This is the
// corruption with no second line of defence: the Go replay asserts on what source declares,
// not on the records, so an inflated count reaches the residual unchallenged.
refuses([harvest({ counts: { passkeyOps: 500 } })], /passkeyOps is 500, but the file holds 1/)
refuses([harvest({ counts: { passkeyOps: 0 } })], /passkeyOps is 0, but the file holds 1/)

// Operations have to lie inside the range the window declares, and carry the three fields the
// replay reads. Without the second check a malformed record reaches the sort as a TypeError.
refuses([harvest({ ops: [op(4000001, 'z')] })], /block 4000001 is outside the declared range 1\.\.9/)
refuses([harvest({ ops: [{ block: 2, txHash: '0xa', sender: '0xb' }] })], /missing txHash, sender or input/)

// A present count table has to be one. Before this, `corrupt` went into the fixture's
// provenance as the value of entryPointRevisions, and a falsy 0 became null — indistinguishable
// from the early harvests that genuinely record nothing.
refuses([harvest({ counts: { passkeyOps: 1, entryPointRevisions: 'corrupt' } })], /entryPointRevisions is "corrupt"/)
refuses([harvest({ counts: { passkeyOps: 1, skipped: 'corrupt' } })], /skipped is "corrupt"/)
refuses([harvest({ counts: { passkeyOps: 1, entryPointRevisions: ['v0.7'] } })], /entryPointRevisions is \["v0.7"\]/)
refuses([harvest({ counts: { passkeyOps: 1, skipped: 0 } })], /skipped is 0/)
refuses([harvest({ counts: { passkeyOps: 1, skipped: skips({ unmatched: -1 }) } })], /skipped\.unmatched is -1/)
refuses([harvest({ counts: { passkeyOps: 1, skipped: skips({ unmatched: 1.5 }) } })], /skipped\.unmatched is 1\.5/)

// Both tables in full have to agree with each other, because that sum is the residual: the
// decoded events are the operations replayed plus the three ways a reconstruction can fail.
// txUnavailable and undecodable are outside it — their transactions were never decoded.
{
  const addsUp = {
    passkeyOps: 1,
    entryPointRevisions: { 'v0.7': 2, 'v0.8': 1 },
    skipped: { txUnavailable: 4, undecodable: 7, unmatched: 1, notPasskeySig: 1, shortSig: 0 },
  }
  assert.equal(mergeHarvests(named([harvest({ counts: addsUp })])).windows.length, 1)
  refuses(
    [harvest({ counts: { ...addsUp, entryPointRevisions: { 'v0.7': 5 } } })],
    /counts do not add up — 5 decoded events, 3 accounted for/
  )

  // Summed as Number, the check had a way around it: two totals that differ by one but round to the
  // same double compared equal. Each value is a safe integer, their sums need not be.
  refuses(
    [
      harvest({
        counts: {
          passkeyOps: 1,
          entryPointRevisions: { 'v0.7': Number.MAX_SAFE_INTEGER, 'v0.8': 2 },
          skipped: skips({ unmatched: Number.MAX_SAFE_INTEGER }),
        },
      }),
    ],
    /counts do not add up — 9007199254740993 decoded events, 9007199254740992 accounted for/
  )

  // Large and consistent still passes, so the exactness is not refusing arithmetic that holds.
  assert.equal(
    mergeHarvests(
      named([
        harvest({
          counts: {
            passkeyOps: 1,
            entryPointRevisions: { 'v0.7': Number.MAX_SAFE_INTEGER },
            skipped: skips({ unmatched: Number.MAX_SAFE_INTEGER - 1 }),
          },
        }),
      ])
    ).windows.length,
    1
  )

  // A partial table is refused, not waved through. No revision of the harvest writes one — all
  // five counters are initialised before the scan — and accepting one was a way around the sum
  // above: omit the term the sum needs and the sum is skipped. This is that escape, closed.
  refuses(
    [harvest({ counts: { passkeyOps: 1, entryPointRevisions: { 'v0.8': 999 }, skipped: { unmatched: 0, shortSig: 0 } } })],
    /counts\.skipped is missing txUnavailable, undecodable, notPasskeySig/
  )
  refuses([harvest({ counts: { passkeyOps: 1, skipped: { unmatched: 1 } } })], /counts\.skipped is missing/)

  // Absent is still absent, which is what the real harvests are.
  assert.equal(mergeHarvests(named([harvest({ counts: { passkeyOps: 1, entryPointRevisions: { 'v0.7': 1 } } })])).windows.length, 1)
}

// A window that reconstructed nothing keeps its record, because the reason it reconstructed
// nothing is the point: a range whose events were all unmatched reports a residual of 17 here,
// and dropping the window would publish it as a range with nothing missing.
{
  const nothing = harvest({
    source: { chainId: 24101, entryPoint: EP, fromBlock: 10, toBlock: 20 },
    counts: { passkeyOps: 0, skipped: skips({ unmatched: 17 }) },
    ops: [],
  })
  const { windows, all } = mergeHarvests(named([harvest(), nothing]))
  assert.equal(windows.length, 2)
  assert.equal(all.length, 1)
  assert.equal(windows[1].passkeyOps, 0)
  assert.deepEqual(windows[1].skipped, skips({ unmatched: 17 }))
}

// It is still checked before it is recorded, because a file passed by mistake is no less the
// wrong file for having reconstructed nothing.
refuses(
  [harvest(), harvest({ source: { chainId: 1, entryPoint: EP, fromBlock: 1, toBlock: 2 }, counts: { passkeyOps: 0 }, ops: [] })],
  /chain 1,/
)

// Every window empty is a different matter: there is no fixture to write.
refuses([harvest({ counts: { passkeyOps: 0 }, ops: [] })], /every harvest is empty/)

// Every account survives a target smaller than the number of accounts: a sample that dropped
// one would be a key shape the replay never sees.
{
  const all = [op(1, 'a'), op(2, 'b'), op(3, 'c'), op(4, 'd', '0xsendera')]
  const ops = pickSample(all, 2)
  assert.deepEqual(
    [...new Set(ops.map((o) => o.sender))].sort(),
    ['0xsendera', '0xsenderb', '0xsenderc']
  )
}

// Overlapping windows deliver the same operation twice; the fixture must carry it once.
{
  const twice = [op(1, 'a'), op(1, 'a'), op(2, 'b')]
  assert.equal(pickSample(twice, 10).length, 2)
}

// Chronological order, whatever order the windows arrived in.
{
  const ops = pickSample([op(5, 'c'), op(1, 'a'), op(3, 'b')], 10)
  assert.deepEqual(
    ops.map((o) => o.block),
    [1, 3, 5]
  )
}

// The command line, in a subprocess. Everything above calls the two exported functions, and
// `require.main === module` means none of main() runs when this file imports them: the argument
// parsing, the TARGET_COUNT check, the reads and the write were covered by nothing. Removing the
// guard on TARGET_COUNT left every test green, which is the gap this closes.
{
  const { spawnSync } = require('node:child_process')
  const fs = require('node:fs')
  const os = require('node:os')
  const path = require('node:path')

  const script = path.join(__dirname, 'sample_passkey_ops.js')
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sample-passkey-ops-'))
  let n = 0
  const write = (name, doc) => {
    const p = path.join(dir, name)
    fs.writeFileSync(p, JSON.stringify(doc))
    return p
  }
  const run = (target, inputs) => {
    const out = path.join(dir, `out${n++}.json`)
    const r = spawnSync(process.execPath, [script, out, String(target), ...inputs], { encoding: 'utf8' })
    return { status: r.status, stderr: r.stderr || '', out, wrote: fs.existsSync(out) }
  }

  const good = write(
    'in0.json',
    harvest({
      counts: { passkeyOps: 2, entryPointRevisions: { 'v0.7': 3 }, skipped: skips({ unmatched: 1 }) },
      ops: [op(1, 'a'), op(2, 'b')],
    })
  )

  // A TARGET_COUNT that is not a positive whole number stops the run. `five` was accepted
  // before: NaN skipped the fill loop and a fixture of whatever the per-account pass came to was
  // written, exit 0. Nothing may be written on any of these.
  for (const bad of ['five', '0', '-3', '2.5', '']) {
    const r = run(bad, [good])
    assert.equal(r.status, 2, `target ${JSON.stringify(bad)} should be refused`)
    assert.match(r.stderr, /TARGET_COUNT|usage:/)
    assert.equal(r.wrote, false, `target ${JSON.stringify(bad)} wrote a fixture anyway`)
  }

  // No arguments at all is the usage message, not a stack trace.
  {
    const r = spawnSync(process.execPath, [script], { encoding: 'utf8' })
    assert.equal(r.status, 2)
    assert.match(r.stderr || '', /usage: sample_passkey_ops\.js/)
  }

  // A refusal from the merge reaches the command line as a message and exit 1 — not a stack —
  // and leaves no half-written fixture behind.
  {
    const wrong = write(
      'wrong.json',
      harvest({
        source: { chainId: 11155111, entryPoint: EP, fromBlock: 1, toBlock: 9 },
        counts: { passkeyOps: 1 },
        ops: [op(3, 'c')],
      })
    )
    const r = run(5, [good, wrong])
    assert.equal(r.status, 1)
    // The names carry their temporary directory, since that is what the command was given.
    assert.match(r.stderr, /^error: .*wrong\.json: chain 11155111, but .*in0\.json is chain 24101$/m)
    assert.equal(r.wrote, false)
  }

  // And the whole command on a valid input, by the content of the file it writes.
  {
    const r = run(5, [good])
    assert.equal(r.status, 0, r.stderr)
    assert.equal(r.wrote, true)
    const doc = JSON.parse(fs.readFileSync(r.out, 'utf8'))
    assert.deepEqual(doc.ops, [op(1, 'a'), op(2, 'b')])
    assert.deepEqual(doc.source.windows, [
      { fromBlock: 1, toBlock: 9, passkeyOps: 2, entryPointRevisions: { 'v0.7': 3 }, skipped: skips({ unmatched: 1 }) },
    ])
    assert.equal(doc.source.chainId, 24101)
    assert.equal(doc.source.entryPoint, EP)
    assert.equal(doc.source.sampledFrom, 2)
    assert.equal(doc.source.distinctAccountsInSample, 2)
    assert.match(doc.source.note, /^Sampled from a larger harvest/)
  }

  fs.rmSync(dir, { recursive: true, force: true })
}

console.log('sample_passkey_ops.js: ok')
