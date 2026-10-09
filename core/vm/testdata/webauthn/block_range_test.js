#!/usr/bin/env node
/*
 * Covers blockRangeProblem, statedBlockRange and scanStepProblem, the rules all three scripts here
 * apply to a block range and to the steps the harvest walks it in.
 *
 * They live in their own module for this test's sake: harvest_passkey_ops.js and
 * census_passkey_keys.js need ethers just to load, so a copy kept inside them could only be reached
 * with ethers installed, which CI does not have. Deleting a rule would then leave every committed
 * test green, which is what this file prevents; script_guards_test.js covers the lines that call them.
 *
 * No dependencies:
 *
 *   node block_range_test.js
 *
 * core/vm/harvest_script_test.go runs it through `go test`, CI included.
 */
'use strict'
const assert = require('node:assert/strict')
const { blockRangeProblem, chainBlockRangeProblem, scanStepProblem, statedBlockRange } = require('./block_range.js')

// Ranges that can be scanned, including the degenerate single block and the window the committed
// fixture's last harvest covered.
assert.equal(blockRangeProblem(0, 0), null)
assert.equal(blockRangeProblem(4000, 4000), null)
assert.equal(blockRangeProblem(5021488, 6021488), null)

// Backwards: the case the harvest produces on its own, as a successful harvest of nothing.
assert.match(blockRangeProblem(5000, 4000), /range runs backwards: 5000\.\.4000/)
assert.match(blockRangeProblem(1, 0), /range runs backwards: 1\.\.0/)

// Anything that is not a block number, in either position. NaN is how an unparseable FROM_BLOCK
// arrives; a string is how a corrupt harvest file arrives.
for (const bad of [NaN, Infinity, -Infinity, -1, 1.5, '4000', '', null, undefined, {}, Number.MAX_SAFE_INTEGER + 2]) {
  assert.match(blockRangeProblem(bad, 9), /^fromBlock is not a whole block number of 0 or more: /, `fromBlock ${String(bad)}`)
  assert.match(blockRangeProblem(1, bad), /^toBlock is not a whole block number of 0 or more: /, `toBlock ${String(bad)}`)
}

// The message names the value, so an operator reading it knows which of the two to fix.
assert.match(blockRangeProblem(-1, 9), /: -1$/)
assert.match(blockRangeProblem(1, NaN), /: NaN$/)

// The range as the environment states it, which is what both scanning scripts check before their
// first request. Unset and empty variables are the defaults, as the scripts have always read them:
// FROM_BLOCK falls back to 0 and TO_BLOCK to the head, which is not known yet, so toBlock is null.
assert.deepEqual(statedBlockRange({}), { fromBlock: 0, toBlock: null, problem: null })
assert.deepEqual(statedBlockRange({ FROM_BLOCK: '', TO_BLOCK: '' }), { fromBlock: 0, toBlock: null, problem: null })
assert.deepEqual(statedBlockRange({ FROM_BLOCK: '10', TO_BLOCK: '20' }), { fromBlock: 10, toBlock: 20, problem: null })
assert.deepEqual(statedBlockRange({ TO_BLOCK: '0' }), { fromBlock: 0, toBlock: 0, problem: null })

// A bad FROM_BLOCK is refused without the head, so the run never asks the chain for it.
assert.match(statedBlockRange({ FROM_BLOCK: 'abc' }).problem, /^fromBlock is not a whole block number of 0 or more: NaN$/)
assert.match(statedBlockRange({ FROM_BLOCK: '-5' }).problem, /^fromBlock is not a whole block number/)

// With both bounds stated the whole rule applies, ordering included — the same answer
// blockRangeProblem gives for the same numbers.
for (const [from, to] of [['5000', '4000'], ['1', '1.5'], ['1', '9007199254740994'], ['abc', '4000'], ['3', '7']]) {
  assert.equal(statedBlockRange({ FROM_BLOCK: from, TO_BLOCK: to }).problem, blockRangeProblem(Number(from), Number(to)), `${from}..${to}`)
}

// Bounds relative to the chain head can only be checked after it is read.
assert.equal(statedBlockRange({ FROM_BLOCK: '200000000' }).problem, null)
assert.equal(statedBlockRange({ FROM_BLOCK: '1', TO_BLOCK: '200000000' }).problem, null)
assert.equal(chainBlockRangeProblem(0, 100, 100), null)
assert.match(chainBlockRangeProblem(101, 101, 100), /toBlock 101 is beyond chain head 100/)
assert.match(chainBlockRangeProblem(99, 101, 100), /toBlock 101 is beyond chain head 100/)
assert.match(chainBlockRangeProblem(200, 100, 100), /range runs backwards/)
assert.match(chainBlockRangeProblem(0, 0, NaN), /latest is not a whole block number/)

// The steps those loops advance by. The defaults, and the one-block step a careful operator
// might pass.
assert.equal(scanStepProblem(1), null)
assert.equal(scanStepProblem(100), null)
assert.equal(scanStepProblem(50000), null)

// Zero hangs the run and NaN ends the loop before its first pass, which is a harvest of nothing
// wearing the same face as a scanned range that was empty.
for (const bad of [0, -1, -50000, NaN, 1.5, Infinity, '100', '', null, undefined, {}, Number.MAX_SAFE_INTEGER + 2]) {
  assert.match(scanStepProblem(bad), /^not a whole number of 1 or more: /, `step ${String(bad)}`)
}
assert.match(scanStepProblem(0), /: 0$/)

console.log('block_range.js: ok')
