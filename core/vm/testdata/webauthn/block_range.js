/*
 * The rules for the block range the scripts here walk and the steps the harvest walks it in. All
 * three callers use them: harvest_passkey_ops.js and census_passkey_keys.js on FROM_BLOCK/TO_BLOCK,
 * sample_passkey_ops.js on the range a harvest declares in its own output.
 *
 * Their own file, with no dependencies, so that the rules those scripts apply are the rules a test
 * can run: the two that reach the chain need ethers just to load, so nothing in CI could reach a
 * copy kept inside them.
 */
'use strict'

// Returns why one bound cannot address a block, or null when it can.
function blockNumberProblem(label, value) {
  if (!Number.isSafeInteger(value) || value < 0) {
    return `${label} is not a whole block number of 0 or more: ${String(value)}`
  }
  return null
}

// Returns why a range cannot be scanned, or null when it can.
//
// The scan loop in harvest_passkey_ops.js steps `from` from fromBlock while `from <= toBlock`:
//
//   - backwards (FROM_BLOCK=5000 TO_BLOCK=4000) runs the body no times at all, so the script
//     writes a harvest of zero operations for a range nobody looked at, and exits 0;
//   - unparseable is the same outcome by another route, because Number('abc') is NaN and
//     `NaN <= toBlock` is false;
//   - fractional or beyond 2^53 cannot address a block, and a string is not a number at all —
//     the sampler reads these from a file, where any JSON value can appear.
//
// sample_passkey_ops.js then publishes such a window in the fixture's provenance as a range that
// was measured and came back empty, which is why both ends check it.
function blockRangeProblem(fromBlock, toBlock) {
  const problem = blockNumberProblem('fromBlock', fromBlock) || blockNumberProblem('toBlock', toBlock)
  if (problem) return problem
  if (fromBlock > toBlock) return `range runs backwards: ${fromBlock}..${toBlock}`
  return null
}

// Reads FROM_BLOCK and TO_BLOCK the way harvest_passkey_ops.js and census_passkey_keys.js take them,
// and returns what is known before the chain is asked anything: fromBlock, toBlock when TO_BLOCK is
// stated or null when it is left to default to the head, and why the stated bounds cannot be scanned.
//
// Both scripts check this before their first request, so a range typed wrongly is refused on its own
// terms even when the endpoint is unreachable. The head is checked separately once known.
function statedBlockRange(env) {
  const fromBlock = Number(env.FROM_BLOCK || 0)
  const toBlock = env.TO_BLOCK ? Number(env.TO_BLOCK) : null
  const problem = toBlock === null ? blockNumberProblem('fromBlock', fromBlock) : blockRangeProblem(fromBlock, toBlock)
  return { fromBlock, toBlock, problem }
}

// A stated upper bound must not claim blocks beyond the chain head were scanned.
function chainBlockRangeProblem(fromBlock, toBlock, latest) {
  const problem = blockRangeProblem(fromBlock, toBlock) || blockNumberProblem('latest', latest)
  if (problem) return problem
  if (toBlock > latest) return `toBlock ${toBlock} is beyond chain head ${latest}`
  return null
}

// Returns why a loop step cannot be used, or null when it can.
//
// LOG_CHUNK is the step of the block scan and BATCH the step of the JSON-RPC batching loop, and
// both loops advance by it from a counter they compare against a limit. A step of zero or less
// never reaches the limit, so the run hangs making the same request forever; an unparseable one
// makes the counter NaN, which compares false at once and ends the loop before its first pass —
// skipping every call it would have made, and producing the same harvest of nothing a backwards
// range does. Neither is reported today: the first looks like a slow scan, the second like an
// empty one.
function scanStepProblem(value) {
  if (!Number.isSafeInteger(value) || value <= 0) {
    return `not a whole number of 1 or more: ${String(value)}`
  }
  return null
}

module.exports = { blockRangeProblem, chainBlockRangeProblem, scanStepProblem, statedBlockRange }
