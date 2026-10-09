#!/usr/bin/env node
/*
 * Runs trace_passkey_ops.js against a stub node in this process, so each of its verdicts is
 * reached on known traces rather than trusted. The comparison is bound to the operation's
 * validateUserOp call — the 0x111 input fed under the CALL to the account whose calldata carries
 * the operation's userOpHash — so the stub exercises the cases that binding has to get right: a
 * canonical validation call passes; a trailing byte on it fails with the tail named; a tampered
 * validation call is not rescued by a canonical call elsewhere, whether in another frame or in
 * the same account's execution phase; a node on another chain is refused; a node that cannot
 * trace confirms nothing; a partial trace is not a pass; and a canonical 0x111 call that is not a
 * validation call confirms nothing. No network beyond localhost.
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
const path = require('node:path')

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

// What the stub answers is decided per case.
let mode = { chainId: fixture.source.chainId, tail: '', canTrace: true }
// Counts debug_traceTransaction calls so a case can let only the first one succeed, standing in
// for an archive node that can trace part of the sample but not the rest.
let traced = 0

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
        return reply(handleOps([validateFrame(op, op.input + mode.tail)]))
      }
      default:
        return reply(null, { code: -32601, message: `unexpected method ${method}` })
    }
  })
})

function run(url, sample) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [SCRIPT, url, String(sample)], { stdio: ['ignore', 'pipe', 'pipe'] })
    let stdout = ''
    let stderr = ''
    child.stdout.on('data', (c) => (stdout += c))
    child.stderr.on('data', (c) => (stderr += c))
    child.on('close', (code) => resolve({ code, stdout, stderr }))
  })
}

server.listen(0, '127.0.0.1', async () => {
  const url = `http://127.0.0.1:${server.address().port}`
  try {
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
  }
})
