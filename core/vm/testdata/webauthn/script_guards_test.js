#!/usr/bin/env node
/*
 * Runs the two scripts that reach the chain — harvest_passkey_ops.js and census_passkey_keys.js —
 * so that the guards they apply are covered where they apply them and not only where their rules
 * live.
 *
 * block_range_test.js covers the rules in block_range.js. That leaves the lines that call them: a
 * deleted call would keep every other test green, which is the gap this file closes. Reaching those
 * lines means getting each script to load and to answer the calls it makes before its guard, so both
 * are run inside a throwaway directory holding
 *
 *   - copies of the two scripts and the modules they need, and
 *   - node_modules/ethers/ with a stand-in for the few helpers they touch on the way to the guard:
 *     two Interfaces and an event topic for the harvest, a provider for the census.
 *
 * Node resolves `require('ethers')` next to the requiring file, so the copies find the stand-in and
 * nothing is installed. The harvest talks JSON-RPC over `fetch`, which an http server in this
 * process answers and logs method by method; the census goes through ethers' provider, which the
 * stand-in replaces and which announces every call made of it, its construction included — the real
 * one starts detecting the network as soon as it exists. Between them every request either script
 * makes is visible, which is how "refused before any request" gets checked rather than assumed.
 *
 * No dependencies, no network beyond localhost, and the directory is removed at the end:
 *
 *   node script_guards_test.js
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

// Enough of ethers v5 for both scripts to load and reach their guards. The harvest builds two
// Interfaces at module scope and needs the topic of the event it scans for and the publicKey()
// selector; getAddress is used when the publicKey() cases return a log. The census works
// through a provider, so the stand-in answers its three calls and announces each of them, and its
// own construction, with a `[stub]` line on stdout — the markers its cases assert on. With
// STUB_GETLOGS_FAILS set it throws from getLogs the way the real one does, endpoint and all.
const ETHERS_STUB = `'use strict'
const HASH = '0x' + '11'.repeat(32)
class Interface {
  constructor(abi) {
    this.abi = abi
  }
  getEventTopic() {
    return HASH
  }
  parseTransaction() {
    throw new Error('stubbed ethers: parseTransaction is beyond what this test needs')
  }
}
class JsonRpcProvider {
  constructor(url) {
    console.log('[stub] JsonRpcProvider')
    this.url = url
  }
  async getNetwork() {
    console.log('[stub] getNetwork')
    return { chainId: 24101 }
  }
  async getBlockNumber() {
    console.log('[stub] getBlockNumber')
    return 100000000
  }
  async getLogs() {
    console.log('[stub] getLogs')
    if (process.env.STUB_GETLOGS_FAILS) {
      // What ethers v5 throws from fetchJson: the endpoint, quoted, inside the message.
      const err = new Error('missing response (requestBody="{}", requestMethod="POST", url="' + this.url + '", code=SERVER_ERROR, version=web/5.7.1)')
      err.url = this.url
      throw err
    }
    return []
  }
}
module.exports = {
  ethers: {
    providers: { JsonRpcProvider },
    utils: {
      Interface,
      id: () => HASH,
      getAddress: (a) => a,
      hexDataSlice: () => '0x' + '00'.repeat(20),
      hexZeroPad: (a) => a,
    },
  },
}
`

const SCRIPTS = ['harvest_passkey_ops.js', 'census_passkey_keys.js', 'classify_signature.js', 'block_range.js']

function sandbox() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'script-guards-'))
  for (const name of SCRIPTS) fs.copyFileSync(path.join(__dirname, name), path.join(dir, name))
  const stub = path.join(dir, 'node_modules', 'ethers')
  fs.mkdirSync(stub, { recursive: true })
  fs.writeFileSync(path.join(stub, 'package.json'), JSON.stringify({ name: 'ethers', version: '5.0.0-stub', main: 'index.js' }))
  fs.writeFileSync(path.join(stub, 'index.js'), ETHERS_STUB)
  return dir
}

// The three calls the harvest makes to learn the chain and its head, and empty logs for anything
// past them. Tests can replace answers to exercise publicKey() errors.
function endpoint() {
  const methods = []
  const answers = {
    eth_chainId: '0x5e25', // 24101, the chain the fixture is from
    web3_clientVersion: 'Geth/v1.11.8-stable/linux-amd64/go1.22.2',
    eth_blockNumber: '0x5f5e100',
    eth_getLogs: [],
  }
  const server = http.createServer((req, res) => {
    let body = ''
    req.on('data', (chunk) => (body += chunk))
    req.on('end', () => {
      let sent
      try {
        sent = JSON.parse(body)
      } catch (e) {
        res.writeHead(400).end()
        return
      }
      const list = Array.isArray(sent) ? sent : [sent]
      for (const r of list) methods.push(r.method)
      const out = list.map((r) => {
        const answer = answers[r.method]
        return { jsonrpc: '2.0', id: r.id, ...(answer && answer.error ? { error: answer.error } : { result: answer === undefined ? null : answer }) }
      })
      res.writeHead(200, { 'content-type': 'application/json' })
      res.end(JSON.stringify(Array.isArray(sent) ? out : out[0]))
    })
  })
  return {
    server,
    listen: () => new Promise((done) => server.listen(0, '127.0.0.1', () => done(`http://127.0.0.1:${server.address().port}`))),
    calls: () => methods.length,
    methodsSince: (n) => methods.slice(n),
    setAnswer: (method, answer) => { answers[method] = answer },
  }
}

// A removed guard turns LOG_CHUNK=0 into a loop that never ends, so a child that outlives this is
// killed and the case fails on `killed` rather than hanging the suite.
const KILL_AFTER_MS = 15000

function run(dir, script, rpc, env, label) {
  return new Promise((done) => {
    const out = path.join(dir, `${label.replace(/[^\w.-]/g, '_')}.json`)
    const child = spawn(process.execPath, [path.join(dir, script)], {
      cwd: dir,
      env: { ...process.env, RPC: rpc, OUT: out, ...env },
    })
    let stderr = ''
    let stdout = ''
    child.stderr.on('data', (chunk) => (stderr += chunk))
    child.stdout.on('data', (chunk) => (stdout += chunk))
    let killed = false
    const timer = setTimeout(() => {
      killed = true
      child.kill()
    }, KILL_AFTER_MS)
    child.on('close', (code) => {
      clearTimeout(timer)
      done({ code, stdout, stderr, killed, out, wrote: fs.existsSync(out) })
    })
  })
}

// Ranges no scan can walk. Unguarded, each one is a range nobody looked at reported as a range with
// nothing in it — by the harvest as a written file, by the census as a published count. Each is
// wrong on its face, so each has to be refused before the chain is asked anything: the last leaves
// TO_BLOCK to its default, which is the head, and still has nothing to wait for.
const BAD_RANGES = [
  ['backwards', { FROM_BLOCK: '5000', TO_BLOCK: '4000' }, /range runs backwards: 5000\.\.4000/],
  ['unparseable', { FROM_BLOCK: 'abc', TO_BLOCK: '4000' }, /fromBlock is not a whole block number/],
  ['negative', { FROM_BLOCK: '-5', TO_BLOCK: '4000' }, /fromBlock is not a whole block number/],
  ['fractional', { FROM_BLOCK: '1.5', TO_BLOCK: '4000' }, /fromBlock is not a whole block number/],
  ['beyond 2^53', { FROM_BLOCK: '1', TO_BLOCK: '9007199254740994' }, /toBlock is not a whole block number/],
  ['unparseable, head defaulted', { FROM_BLOCK: 'abc', TO_BLOCK: '' }, /fromBlock is not a whole block number/],
]

// A FROM_BLOCK past the head is the one bad range the environment cannot show: TO_BLOCK defaults
// to the head, which only the chain knows. Both stand-ins report a head of 100000000.
const PAST_HEAD = { FROM_BLOCK: '200000000', TO_BLOCK: '' }
const PAST_HEAD_REFUSAL = /range runs backwards: 200000000\.\.100000000/
const FUTURE_RANGES = [
  ['fully future', { FROM_BLOCK: '200000000', TO_BLOCK: '300000000' }, /toBlock 300000000 is beyond chain head 100000000/],
  ['partly future', { FROM_BLOCK: '99999999', TO_BLOCK: '100000001' }, /toBlock 100000001 is beyond chain head 100000000/],
]

async function harvestGuards(dir, url, rpc) {
  // The range guard, at the line that calls it. Every one of these would otherwise be a harvest of
  // zero operations for a range the scan never walked, written with exit 0.
  for (const [label, env, pattern] of BAD_RANGES) {
    const before = rpc.calls()
    const r = await run(dir, 'harvest_passkey_ops.js', url, env, `harvest ${label}`)
    assert.equal(r.killed, false, `harvest ${label}: the script did not exit`)
    assert.equal(r.code, 2, `harvest ${label}: exit ${r.code}, want 2\n${r.stderr}`)
    assert.match(r.stderr, pattern, `harvest ${label}`)
    assert.equal(r.wrote, false, `harvest ${label}: wrote a harvest anyway`)
    assert.deepEqual(rpc.methodsSince(before), [], `harvest ${label}: requests went out before the range was refused`)
  }

  // Past the head: refused once the head is known, and with nothing asked of the chain after it.
  const beforePastHead = rpc.calls()
  const past = await run(dir, 'harvest_passkey_ops.js', url, PAST_HEAD, 'harvest past head')
  assert.equal(past.killed, false, 'harvest past head: the script did not exit')
  assert.equal(past.code, 2, `harvest past head: exit ${past.code}, want 2\n${past.stderr}`)
  assert.match(past.stderr, PAST_HEAD_REFUSAL, 'harvest past head')
  assert.equal(past.wrote, false, 'harvest past head: wrote a harvest anyway')
  assert.deepEqual(rpc.methodsSince(beforePastHead), ['eth_chainId', 'web3_clientVersion', 'eth_blockNumber'], 'harvest past head: it asked for more than the head')

  for (const [label, env, pattern] of FUTURE_RANGES) {
    const before = rpc.calls()
    const r = await run(dir, 'harvest_passkey_ops.js', url, env, `harvest ${label}`)
    assert.equal(r.code, 2, `harvest ${label}: exit ${r.code}, want 2\n${r.stderr}`)
    assert.match(r.stderr, pattern)
    assert.equal(r.wrote, false, `harvest ${label}: wrote a harvest anyway`)
    assert.deepEqual(rpc.methodsSince(before), ['eth_chainId', 'web3_clientVersion', 'eth_blockNumber'], `harvest ${label}: scanned after reading the head`)
  }

  // The step guards, at their line, and before the first request: LOG_CHUNK=0 against a live
  // endpoint used to spin rather than fail, so "no calls arrived" is the half worth pinning.
  for (const [label, env] of [
    ['LOG_CHUNK=0', { FROM_BLOCK: '1', TO_BLOCK: '100000', LOG_CHUNK: '0' }],
    ['LOG_CHUNK unparseable', { FROM_BLOCK: '1', TO_BLOCK: '100000', LOG_CHUNK: 'abc' }],
    ['LOG_CHUNK negative', { FROM_BLOCK: '1', TO_BLOCK: '100000', LOG_CHUNK: '-50000' }],
    ['BATCH=0', { FROM_BLOCK: '1', TO_BLOCK: '100', BATCH: '0' }],
  ]) {
    const before = rpc.calls()
    const r = await run(dir, 'harvest_passkey_ops.js', url, env, `harvest ${label}`)
    assert.equal(r.killed, false, `${label}: the script did not exit — the step guard is gone and the loop never ends`)
    assert.equal(r.code, 2, `${label}: exit ${r.code}, want 2\n${r.stderr}`)
    assert.match(r.stderr, /not a whole number of 1 or more/, label)
    assert.equal(r.wrote, false, `${label}: wrote a harvest anyway`)
    assert.equal(rpc.calls(), before, `${label}: ${rpc.calls() - before} request(s) went out before the guard`)
  }

  // And a range that can be walked still produces its harvest, so the guards are not refusing the
  // ordinary case. The endpoint reports no logs, so this is an honestly empty window: the range was
  // scanned, which is the distinction the sampler then publishes.
  //
  // The endpoint is given with a path and a query here, standing in for the key a hosted endpoint
  // carries: what the harvest records as provenance is the origin, and the rest must not reach the
  // file, which is the one that gets committed.
  const credentialed = `${url}/v3/SECRETKEY?token=SECRETTOKEN`
  const r = await run(dir, 'harvest_passkey_ops.js', credentialed, { FROM_BLOCK: '10', TO_BLOCK: '10' }, 'harvest valid')
  assert.equal(r.killed, false, 'harvest valid: the script did not exit')
  assert.equal(r.code, 0, `harvest valid: exit ${r.code}\n${r.stderr}`)
  assert.equal(r.wrote, true, 'harvest valid: wrote no harvest')
  const doc = JSON.parse(fs.readFileSync(r.out, 'utf8'))
  assert.equal(doc.source.chainId, 24101)
  assert.equal(doc.source.fromBlock, 10)
  assert.equal(doc.source.toBlock, 10)
  assert.equal(doc.counts.passkeyOps, 0)
  assert.deepEqual(doc.ops, [])
  assert.equal(doc.source.rpc, undefined, 'the harvest recorded the endpoint it read from')
  // Not in the file, and not in what it said while writing it: a diagnostic quoting the value is
  // the same leak moved into a log.
  const written = fs.readFileSync(r.out, 'utf8')
  for (const [where, text] of [
    ['the harvest file', written],
    ['stderr', r.stderr],
    ['stdout', r.stdout],
  ]) {
    assert.doesNotMatch(text, /SECRETKEY|SECRETTOKEN/, `the endpoint credentials reached ${where}`)
  }
}

async function harvestPublicKeyErrors(dir, url, rpc) {
  const sender = '0x' + '22'.repeat(20)
  rpc.setAnswer('eth_getLogs', [{
    transactionHash: '0x' + '33'.repeat(32),
    blockNumber: '0xa',
    logIndex: '0x0',
    topics: ['0x' + '11'.repeat(32), '0x' + '44'.repeat(32), '0x' + '00'.repeat(12) + sender.slice(2)],
    data: '0x' + '00'.repeat(32) + '00'.repeat(31) + '01',
  }])
  try {
    for (const [label, error, code] of [
      ['missing state', { code: -32000, message: 'missing trie node' }, 1],
      ['missing header', { code: -32000, message: 'header not found' }, 1],
      ['mislabelled revert', { code: -32000, message: 'execution reverted' }, 1],
      ['EVM revert', { code: 3, message: 'execution reverted' }, 0],
    ]) {
      rpc.setAnswer('eth_call', { error })
      const before = rpc.calls()
      const r = await run(dir, 'harvest_passkey_ops.js', url, { FROM_BLOCK: '10', TO_BLOCK: '10' }, `publicKey ${label}`)
      assert.equal(r.code, code, `${label}: exit ${r.code}, want ${code}\n${r.stderr}`)
      assert.deepEqual(rpc.methodsSince(before), ['eth_chainId', 'web3_clientVersion', 'eth_blockNumber', 'eth_getLogs', 'eth_call'], `${label}: unexpected RPC calls`)
      if (code !== 0) {
        assert.equal(r.wrote, false, `${label}: wrote an incomplete harvest`)
        assert.match(r.stderr, new RegExp(error.message))
        assert.match(r.stderr, new RegExp(sender))
      } else {
        assert.equal(r.wrote, true, `${label}: expected a harvest`)
        const doc = JSON.parse(fs.readFileSync(r.out, 'utf8'))
        assert.equal(doc.counts.userOperationEvents, 1)
        assert.equal(doc.counts.passkeySenders, 0)
        assert.equal(doc.counts.passkeyOps, 0)
      }
    }
  } finally {
    rpc.setAnswer('eth_getLogs', [])
  }
}

async function censusGuards(dir, url) {
  // The census publishes the three numbers quoted in docs/webauthn/replay-validation.md and reads
  // the same two variables. Unguarded, a range that cannot be scanned went to the provider, whose
  // answer to it is its own business — an empty one makes a census of zero accounts, printed with
  // exit 0, which reads as a measurement of the chain.
  for (const [label, env, pattern] of BAD_RANGES) {
    const r = await run(dir, 'census_passkey_keys.js', url, env, `census ${label}`)
    assert.equal(r.killed, false, `census ${label}: the script did not exit`)
    assert.equal(r.code, 2, `census ${label}: exit ${r.code}, want 2\n${r.stderr}${r.stdout}`)
    assert.match(r.stderr, pattern, `census ${label}`)
    assert.doesNotMatch(r.stdout, /\[stub\]/, `census ${label}: it reached the provider before the range was refused`)
    assert.doesNotMatch(r.stdout, /accounts initialised/, `census ${label}: it published a census anyway`)
  }

  // Past the head: the provider is asked for the network and the head, and for nothing after them.
  const past = await run(dir, 'census_passkey_keys.js', url, PAST_HEAD, 'census past head')
  assert.equal(past.killed, false, 'census past head: the script did not exit')
  assert.equal(past.code, 2, `census past head: exit ${past.code}, want 2\n${past.stderr}${past.stdout}`)
  assert.match(past.stderr, PAST_HEAD_REFUSAL, 'census past head')
  assert.match(past.stdout, /\[stub\] getBlockNumber/, 'census past head: it refused without learning the head')
  assert.doesNotMatch(past.stdout, /\[stub\] getLogs/, 'census past head: it asked the chain for logs anyway')
  assert.doesNotMatch(past.stdout, /accounts initialised/, 'census past head: it published a census anyway')

  for (const [label, env, pattern] of FUTURE_RANGES) {
    const r = await run(dir, 'census_passkey_keys.js', url, env, `census ${label}`)
    assert.equal(r.code, 2, `census ${label}: exit ${r.code}, want 2\n${r.stderr}${r.stdout}`)
    assert.match(r.stderr, pattern)
    assert.match(r.stdout, /\[stub\] getBlockNumber/, `census ${label}: did not read the head`)
    assert.doesNotMatch(r.stdout, /\[stub\] getLogs|accounts initialised/, `census ${label}: scanned or published a census`)
  }

  // A range that can be scanned still produces its census, over logs the stand-in reports as none.
  // The endpoint carries a path and a query, standing in for a hosted endpoint's key: neither the
  // census nor its failure below may quote it.
  const credentialed = `${url}/v3/SECRETKEY?token=SECRETTOKEN`
  const r = await run(dir, 'census_passkey_keys.js', credentialed, { FROM_BLOCK: '10', TO_BLOCK: '20' }, 'census valid')
  assert.equal(r.killed, false, 'census valid: the script did not exit')
  assert.equal(r.code, 0, `census valid: exit ${r.code}\n${r.stderr}`)
  assert.match(r.stdout, /\[stub\] getLogs/, 'census valid: it never asked for logs')
  assert.match(r.stdout, /blocks 10\.\.20/, 'census valid: it did not report the range it scanned')
  assert.match(r.stdout, /accounts initialised: +0/, 'census valid: it did not publish a census')
  assert.doesNotMatch(r.stdout + r.stderr, /SECRETKEY|SECRETTOKEN/, 'census valid: the endpoint credentials reached the output')

  // A provider failure quotes the endpoint — ethers v5 puts `url="…"` into its SERVER_ERROR and
  // TIMEOUT messages — so the failure has to say what happened without saying where. The
  // harvest's guard above covers its own output; this is the census's.
  const failed = await run(dir, 'census_passkey_keys.js', credentialed, { FROM_BLOCK: '10', TO_BLOCK: '20', STUB_GETLOGS_FAILS: '1' }, 'census provider error')
  assert.equal(failed.killed, false, 'census provider error: the script did not exit')
  assert.equal(failed.code, 1, `census provider error: exit ${failed.code}, want 1\n${failed.stderr}`)
  assert.match(failed.stderr, /FATAL missing response .*code=SERVER_ERROR/, 'census provider error: the failure lost its message')
  for (const [where, text] of [
    ['stderr', failed.stderr],
    ['stdout', failed.stdout],
  ]) {
    assert.doesNotMatch(text, /SECRETKEY|SECRETTOKEN/, `census provider error: the endpoint credentials reached ${where}`)
  }
}

async function main() {
  const dir = sandbox()
  const rpc = endpoint()
  const url = await rpc.listen()
  try {
    await harvestGuards(dir, url, rpc)
    await harvestPublicKeyErrors(dir, url, rpc)
    await censusGuards(dir, url)
  } finally {
    rpc.server.close()
    fs.rmSync(dir, { recursive: true, force: true })
  }
}

main().then(
  () => console.log('harvest and census guards: ok'),
  (err) => {
    console.error((err && err.stack) || err)
    process.exit(1)
  }
)
