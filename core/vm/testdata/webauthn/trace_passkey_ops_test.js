#!/usr/bin/env node
/*
 * Runs trace_passkey_ops.js against a stub node in this process, so each of its verdicts is
 * reached on known traces rather than trusted. The comparison is bound to the operation's
 * validateUserOp call — the 0x111 input fed under the CALL to the account whose calldata carries
 * the operation's userOpHash — so the stub exercises the cases that binding has to get right: a
 * canonical validation call passes; a trailing byte on it fails with the tail named; a tampered
 * validation call is not rescued by a canonical call elsewhere, whether in another frame or in
 * the same account's execution phase; a node on another chain is refused; a node that cannot
 * trace confirms nothing; a partial trace is not a pass; a canonical 0x111 call that is not a
 * validation call confirms nothing; and a run without a URL prints the usage and exits 2. The
 * sampler is also called directly, over many shapes of senders and on two shapes whose picks are
 * pinned. No network beyond localhost.
 *
 *   node trace_passkey_ops_test.js
 *
 * core/vm/harvest_script_test.go runs it through `go test`, CI included.
 */
'use strict'
const assert = require('node:assert/strict')
const { spawn } = require('node:child_process')
const fs = require('node:fs')
const http = require('node:http')
const os = require('node:os')
const path = require('node:path')
const { pickSample } = require('./trace_passkey_ops.js')

const SCRIPT = path.join(__dirname, 'trace_passkey_ops.js')
const fixture = JSON.parse(fs.readFileSync(path.join(__dirname, 'mainnet-passkey-ops.json'), 'utf8'))
const byTx = new Map(fixture.ops.map((o) => [o.txHash.toLowerCase(), o]))
const PRECOMPILE = '0x0000000000000000000000000000000000000111'
const OK = '0x' + '0'.repeat(63) + '1'

// A validateUserOp CALL to the account: its calldata carries the operation's userOpHash, as
// EntryPoint passes it, so the script recognises it as the validation frame. The nested 0x111
// call is the signature check, with the given input.
const validateFrame = (op, precompileInput) => ({
  from: fixture.source.entryPoint,
  to: op.sender,
  input: '0x19822f7c' + '0'.repeat(64) + op.input.slice(2, 66), // validateUserOp: userOp ptr word, then userOpHash
  calls: [{ to: PRECOMPILE, input: precompileInput, output: OK }],
})
// The account's execution CALL: its calldata is the operation's own callData, with no
// userOpHash, so it is not a validation frame and its 0x111 call must not be counted.
const executeFrame = (op, precompileInput) => ({
  from: fixture.source.entryPoint,
  to: op.sender,
  input: '0xb61d27f6', // an execute() selector, no userOpHash
  calls: [{ to: PRECOMPILE, input: precompileInput, output: OK }],
})
// An executeUserOp CALL (IAccountExecute): from EntryPoint, carrying userOpHash, but under a
// different selector, so it must not be read as a validation frame even when it calls 0x111.
const executeUserOpFrame = (op, precompileInput) => ({
  from: fixture.source.entryPoint,
  to: op.sender,
  input: '0x8dd7712f' + '0'.repeat(64) + op.input.slice(2, 66), // executeUserOp: userOp ptr word, then userOpHash
  calls: [{ to: PRECOMPILE, input: precompileInput, output: OK }],
})
// A validateUserOp-shaped CALL to the account, but not from the EntryPoint — so not a genuine
// validation call, and its 0x111 call must not be counted even with the right selector and hash.
const foreignValidateFrame = (op, precompileInput) => ({
  from: '0x000000000000000000000000000000000000dead',
  to: op.sender,
  input: '0x19822f7c' + '0'.repeat(64) + op.input.slice(2, 66),
  calls: [{ to: PRECOMPILE, input: precompileInput, output: OK }],
})
const strayCanonical = (op) => ({ to: fixture.source.entryPoint, input: '0x', calls: [{ to: PRECOMPILE, input: op.input, output: OK }] })
const handleOps = (calls) => ({ to: fixture.source.entryPoint, input: '0x', calls })
const flip = (op) => '0x' + (op.input[2] === '0' ? '1' : '0') + op.input.slice(3) // tampered userOpHash prefix

// The sampler on its own, over many shapes of senders: the size is min(n, ops), the picks are
// distinct and ascending, the first and the last operation are in whenever they fit, and two
// neighbouring picks are less than two windows apart, the windows being what the operations
// between the endpoints are cut into.
for (const sender of [() => 0, (i) => i, (i) => i % 3, (i) => i % 10, (i) => Math.floor(i / 2), (i) => Math.floor(i / 7)]) {
  for (let len = 1; len <= 60; len++) {
    const ops = Array.from({ length: len }, (_, i) => ({ block: i + 1, sender: '0x' + sender(i).toString(16).padStart(40, '0') }))
    for (let n = 1; n <= len + 2; n++) {
      const size = Math.min(n, len)
      const blocks = pickSample(ops, n).map((o) => o.block)
      const shape = `${len} operations, n ${n}: ${blocks.join(',')}`
      assert.equal(blocks.length, size, shape)
      assert.deepEqual(blocks, [...new Set(blocks)].sort((a, b) => a - b), shape)
      assert.equal(blocks[0], 1, shape)
      if (size > 1) assert.equal(blocks[size - 1], len, shape)
      if (size > 2) {
        const window = Math.ceil((len - 2) / (size - 2))
        for (let i = 1; i < size; i++) assert.ok(blocks[i] - blocks[i - 1] < 2 * window, shape)
      }
    }
  }
}

// The invariants above hold for any cut of the windows, so two small shapes pin which operations
// the cut picks. With m operations between the endpoints and w windows, numbering those from 0,
// window k starts at floor(k*m/w) and ends before floor((k+1)*m/w); it picks its first operation
// whose sender is not sampled yet, else its first operation. Blocks are 1-based here.
for (const [what, sender, len, n, want] of [
  // Windows {2} and {3, 4}, one sender throughout: the first operation of each is picked.
  ['one sender', () => 0, 5, 4, [1, 2, 3, 5]],
  // Windows {2, 3} and {4, 5, 6}: the first takes sender 1 at once; in the second only its last
  // operation, block 6, has a sender not sampled yet, so the scan has to reach the window's end.
  ['senders in turn', (i) => i % 3, 7, 4, [1, 2, 6, 7]],
]) {
  const ops = Array.from({ length: len }, (_, i) => ({ block: i + 1, sender: '0x' + sender(i).toString(16).padStart(40, '0') }))
  assert.deepEqual(pickSample(ops, n).map((o) => o.block), want, what)
}

// What the stub answers is decided per case.
let mode = { chainId: fixture.source.chainId, tail: '', canTrace: true }
// Counts debug_traceTransaction calls so a case can let only the first one succeed, standing in
// for an archive node that can trace part of the sample but not the rest.
let traced = 0
let tracedTransactions = []

const server = http.createServer((req, res) => {
  let body = ''
  req.on('data', (c) => (body += c))
  req.on('end', () => {
    const { id, method, params } = JSON.parse(body)
    const reply = (result, error) => {
      res.setHeader('content-type', 'application/json')
      res.end(JSON.stringify(error ? { jsonrpc: '2.0', id, error } : { jsonrpc: '2.0', id, result }))
    }
    switch (method) {
      case 'eth_chainId':
        return reply('0x' + mode.chainId.toString(16))
      case 'eth_blockNumber':
        return reply('0x5f5e100')
      case 'debug_traceTransaction': {
        tracedTransactions.push(params[0].toLowerCase())
        if (!mode.canTrace) return reply(null, { code: -32000, message: 'historical state is not available' })
        if (mode.onlyFirst && traced > 0) return reply(null, { code: -32000, message: 'historical state is not available' })
        traced++
        const op = byTx.get(params[0].toLowerCase())
        assert.ok(op, `traced a transaction that is not in the fixture: ${params[0]}`)
        assert.equal(params[1].tracer, 'callTracer')
        if (mode.extraCanonical) {
          // Validation call carries a trailing byte; a separate (non-account) frame makes a
          // canonical call for the operation. The stray call must not rescue it.
          return reply(handleOps([validateFrame(op, op.input + 'cd'), strayCanonical(op)]))
        }
        if (mode.tamperedPrefix) {
          // Validation call has a tampered userOpHash prefix; a separate frame makes a canonical
          // call. The validation call is still read, and still fails.
          return reply(handleOps([validateFrame(op, flip(op)), strayCanonical(op)]))
        }
        if (mode.executionCanonical) {
          // Same account, two frames: a tampered validation call and a canonical execution call.
          // The execution call is not a validation frame, so it must not rescue the operation.
          return reply(handleOps([validateFrame(op, flip(op)), executeFrame(op, op.input)]))
        }
        if (mode.foreignValidate) {
          // A validateUserOp-shaped call not from the EntryPoint: not a genuine validation call.
          return reply(handleOps([foreignValidateFrame(op, op.input)]))
        }
        if (mode.execUserOp) {
          // The only 0x111 call is in an executeUserOp frame, which also carries userOpHash but
          // under a different selector. Matched by selector, it is not a validation frame.
          return reply(handleOps([executeUserOpFrame(op, op.input)]))
        }
        if (mode.noValidation) {
          // A canonical 0x111 call, but only in a plain execution frame — no validateUserOp call.
          return reply(handleOps([executeFrame(op, op.input)]))
        }
        const tail = mode.tailTx === op.txHash ? 'ab' : mode.tail
        return reply(handleOps([validateFrame(op, op.input + tail)]))
      }
      default:
        return reply(null, { code: -32601, message: `unexpected method ${method}` })
    }
  })
})

// A null url runs the script with no arguments at all.
function run(url, sample, fixturePath) {
  return new Promise((resolve) => {
    tracedTransactions = []
    const child = spawn(process.execPath, [SCRIPT, ...(url === null ? [] : [url, String(sample)])], {
      stdio: ['ignore', 'pipe', 'pipe'],
      env: { ...process.env, ...(fixturePath ? { WEBAUTHN_OPS_FILE: fixturePath } : {}) },
    })
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (c) => (stdout += c))
    child.stderr.on('data', (c) => (stderr += c))
    child.on('close', (code) => resolve({ code, stdout, stderr }))
  })
}

server.listen(0, '127.0.0.1', async () => {
  const url = `http://127.0.0.1:${server.address().port}`
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'trace-passkey-sampling-'))
  try {
    const writeFixture = (senders) => {
      const ops = senders.map((sender, i) => ({
        ...fixture.ops[i],
        block: i + 1,
        txHash: '0x' + (i + 1).toString(16).padStart(64, '0'),
        sender: '0x' + sender.repeat(40),
      }))
      for (const op of ops) byTx.set(op.txHash.toLowerCase(), op)
      const file = path.join(dir, 'fixture.json')
      fs.writeFileSync(file, JSON.stringify({ source: fixture.source, ops }))
      return { file, ops }
    }

    // Repeated endpoint senders and stride collisions must not drop endpoints or slots.
    let custom = writeFixture(['a', 'b', 'a', 'c', 'a', 'a'])
    let sampling = await run(url, 3, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.match(sampling.stdout, /matched 3 \/ 3/)
    assert.equal(new Set(tracedTransactions).size, 3)
    assert.ok(tracedTransactions.includes(custom.ops[0].txHash))
    assert.ok(tracedTransactions.includes(custom.ops[5].txHash))
    assert.ok(tracedTransactions.includes(custom.ops[1].txHash), 'prefer a new sender when stride positions collide')

    // A mismatch only on the newest operation must make the run fail.
    mode.tailTx = custom.ops[5].txHash
    sampling = await run(url, 3, custom.file)
    assert.equal(sampling.code, 1, sampling.stdout + sampling.stderr)
    assert.match(sampling.stdout, /matched 2 \/ 3   mismatches 1/)
    assert.doesNotMatch(sampling.stdout, /OK: every traced operation/)
    delete mode.tailTx

    // Endpoints take precedence over sender diversity, including when only two fit.
    sampling = await run(url, 2, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.deepEqual(tracedTransactions, [custom.ops[0].txHash, custom.ops[5].txHash])
    sampling = await run(url, 1, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.deepEqual(tracedTransactions, [custom.ops[0].txHash])

    // With one sender, fill from unvisited operations rather than undersampling.
    custom = writeFixture(['a', 'a', 'a', 'a', 'a', 'a'])
    sampling = await run(url, 5, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.match(sampling.stdout, /matched 5 \/ 5/)
    assert.equal(new Set(tracedTransactions).size, 5)
    sampling = await run(url, 10, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.deepEqual(tracedTransactions, custom.ops.map((o) => o.txHash))

    // Different endpoint senders leave room for another distinct sender.
    custom = writeFixture(['a', 'b', 'a', 'c', 'a', 'd'])
    sampling = await run(url, 3, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.equal(tracedTransactions.length, 3)
    assert.equal(new Set(tracedTransactions.map((tx) => byTx.get(tx).sender)).size, 3)

    // The window prefers a sender not sampled yet, and the last operation's sender counts as
    // sampled although it is added after the windows: of a, c, b, b, b, c the sample is a, b, c.
    custom = writeFixture(['a', 'c', 'b', 'b', 'b', 'c'])
    sampling = await run(url, 3, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.deepEqual(tracedTransactions, [custom.ops[0].txHash, custom.ops[2].txHash, custom.ops[5].txHash])

    // Ten senders take turns over 100 operations and the sample has 20 slots. Preferring unseen
    // senders must not spend the slots on the oldest operations: the sample stays spread over the
    // whole range, no gap wider than two windows, and the newest operations below the last are traced.
    custom = writeFixture(Array.from({ length: 100 }, (_, i) => String(i % 10)))
    sampling = await run(url, 20, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    const blocks = tracedTransactions.map((tx) => byTx.get(tx).block)
    assert.equal(blocks.length, 20)
    assert.equal(new Set(blocks).size, 20)
    assert.equal(blocks[0], 1)
    assert.equal(blocks[blocks.length - 1], 100)
    const window = Math.ceil((100 - 2) / (20 - 2))
    const maxGap = Math.max(...blocks.slice(1).map((b, i) => b - blocks[i]))
    assert.ok(maxGap < 2 * window, `blocks ${blocks.join(',')} leave a gap of ${maxGap}`)

    custom = writeFixture(['a'])
    sampling = await run(url, 3, custom.file)
    assert.equal(sampling.code, 0, sampling.stdout + sampling.stderr)
    assert.deepEqual(tracedTransactions, [custom.ops[0].txHash])

    for (const badCount of [0, -1, 1.5, 'five']) {
      sampling = await run(url, badCount, custom.file)
      assert.equal(sampling.code, 2, sampling.stdout + sampling.stderr)
      assert.match(sampling.stderr, /sampleCount must be a positive whole number/)
      assert.equal(tracedTransactions.length, 0)
    }
    custom = writeFixture([])
    sampling = await run(url, 3, custom.file)
    assert.equal(sampling.code, 2, sampling.stdout + sampling.stderr)
    assert.match(sampling.stderr, /fixture contains no operations/)
    assert.doesNotMatch(sampling.stdout, /OK: every traced operation/)

    // Without a URL the script prints its usage and exits 2, before it reads a fixture or asks a node.
    sampling = await run(null)
    assert.equal(sampling.code, 2, sampling.stdout + sampling.stderr)
    assert.match(sampling.stderr, /usage: trace_passkey_ops\.js <rpc-url>/)
    assert.equal(sampling.stdout, '')

    // Every validation call is the reconstruction: pass, and the sample is what was asked for.
    let r = await run(url, 3)
    assert.equal(r.code, 0, r.stdout + r.stderr)
    assert.match(r.stdout, /matched 3 \/ 3   mismatches 0   unconfirmed 0/)
    assert.match(r.stdout, /OK: every traced operation fed 0x111 exactly the reconstructed input/)

    // One byte after y on the validation call: the verdict names the tail and the lengths.
    mode = { ...mode, tail: 'ab' }
    r = await run(url, 3)
    assert.equal(r.code, 1, r.stdout + r.stderr)
    const m = r.stdout.match(/MISMATCH wantLen=(\d+) gotLen=(\d+)/)
    assert.ok(m, r.stdout)
    assert.equal(Number(m[2]), Number(m[1]) + 1)
    assert.match(r.stdout, /"tailAfterWant":"ab"/)
    assert.match(r.stderr, /differs from the reconstruction/)

    // Another chain is refused before any trace is asked for.
    mode = { chainId: fixture.source.chainId + 1, tail: '', canTrace: true }
    r = await run(url, 3)
    assert.equal(r.code, 2, r.stdout + r.stderr)
    assert.match(r.stderr, /but the fixture is from chain/)
    assert.doesNotMatch(r.stdout, /MATCH|MISMATCH/)

    // A node without the history confirms nothing, and says so rather than passing.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: false }
    r = await run(url, 3)
    assert.equal(r.code, 2, r.stdout + r.stderr)
    assert.match(r.stdout, /matched 0 \/ 3/)
    assert.match(r.stderr, /No operation could be confirmed/)

    // Only part of the sample can be traced: a partial result is not a pass. One op matches,
    // the rest are unconfirmed, and the script must exit non-zero without printing OK.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: true, onlyFirst: true }
    traced = 0
    r = await run(url, 3)
    assert.equal(r.code, 3, r.stdout + r.stderr)
    assert.match(r.stdout, /matched 1 \/ 3   mismatches 0   unconfirmed 2/)
    assert.doesNotMatch(r.stdout, /OK: every traced operation/)
    assert.match(r.stderr, /not a clean pass/)

    // A canonical 0x111 call in another (non-account) frame must not cover for a validation call
    // that carried a trailing byte: the operation is still a mismatch.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: true, extraCanonical: true }
    traced = 0
    r = await run(url, 1)
    assert.equal(r.code, 1, r.stdout + r.stderr)
    assert.match(r.stdout, /MISMATCH wantLen=(\d+) gotLen=(\d+)/)
    assert.match(r.stdout, /"tailAfterWant":"cd"/)
    assert.match(r.stderr, /differs from the reconstruction/)

    // A validation call with a tampered userOpHash prefix is read and fails; a canonical call in
    // another frame does not rescue it.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: true, tamperedPrefix: true }
    traced = 0
    r = await run(url, 1)
    assert.equal(r.code, 1, r.stdout + r.stderr)
    assert.match(r.stdout, /MISMATCH/)
    assert.match(r.stdout, /the reconstruction is not a prefix of it/)

    // Same account, two frames: a tampered validation call and a canonical execution call. The
    // execution call is not a validation frame, so it must not rescue the operation.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: true, executionCanonical: true }
    traced = 0
    r = await run(url, 1)
    assert.equal(r.code, 1, r.stdout + r.stderr)
    assert.match(r.stdout, /MISMATCH/)
    assert.match(r.stdout, /the reconstruction is not a prefix of it/)

    // A canonical 0x111 call only in the execution frame — no validateUserOp call in the trace —
    // confirms nothing rather than passing.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: true, noValidation: true }
    traced = 0
    r = await run(url, 1)
    assert.equal(r.code, 2, r.stdout + r.stderr)
    assert.match(r.stdout, /NO validateUserOp 0x111 call for this operation/)
    assert.doesNotMatch(r.stdout, /MATCH \(/)
    assert.match(r.stderr, /No operation could be confirmed/)

    // The only 0x111 call is in an executeUserOp frame, which also carries userOpHash but under a
    // different selector. It is not a validation call, so nothing is confirmed — matching on the
    // hash alone read it as validation and passed.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: true, execUserOp: true }
    traced = 0
    r = await run(url, 1)
    assert.equal(r.code, 2, r.stdout + r.stderr)
    assert.match(r.stdout, /NO validateUserOp 0x111 call for this operation/)
    assert.doesNotMatch(r.stdout, /MATCH \(/)
    assert.match(r.stderr, /No operation could be confirmed/)

    // A validateUserOp-shaped call not from the EntryPoint is not a genuine validation call, so a
    // canonical 0x111 inside it confirms nothing.
    mode = { chainId: fixture.source.chainId, tail: '', canTrace: true, foreignValidate: true }
    traced = 0
    r = await run(url, 1)
    assert.equal(r.code, 2, r.stdout + r.stderr)
    assert.match(r.stdout, /NO validateUserOp 0x111 call for this operation/)
    assert.doesNotMatch(r.stdout, /MATCH \(/)
    assert.match(r.stderr, /No operation could be confirmed/)

    console.log('trace_passkey_ops_test.js: ok')
  } finally {
    server.close()
    fs.rmSync(dir, { recursive: true, force: true })
  }
})
