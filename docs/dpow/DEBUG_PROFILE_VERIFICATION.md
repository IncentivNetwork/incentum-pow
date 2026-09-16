# Debug profile verification

This verifies the `trace-indexer-v1` RPC profile from PR #119. The scripts and
tests exercise transport restrictions, real call traces, validation and load
bounds. They do not demonstrate a complete Blockscout indexing deployment.

## Local results

Checked on 2026-09-16. Verification source revision:
`a04e14294568f5b051fe54d1300645f12613f7b2`. Production Go sources are unchanged
from `b301e74ad1c402350561180472a08a228183a5c7`.

| Check | Result |
| --- | --- |
| `test_debug_profile.sh`, Linux binary built with Go 1.22.12 | 68 passed, 0 failed, 1 skipped |
| `test_debug_profile.sh`, image built with the repository Dockerfile | 68 passed, 0 failed, 1 skipped |
| `probe_debug_profile.sh`, local binary and Docker nodes with real transactions | 33 passed, 0 failed, 0 skipped on each |
| Probe with a deliberately smaller local batch cap, binary and Docker | 31 passed, 0 failed, 2 skipped on each |
| `go test ./node ./eth/tracers ./rpc -count=1 -timeout=5m` | Passed |
| CLI debug-profile tests and `TestWSBatchLimitFlag` | Passed |
| `bash -n`, `gofmt`, `git diff --check` | Passed |

The end-to-end script's WS batch-limit check is explicitly delegated to
`cmd/geth/TestWSBatchLimitFlag`, which sends genuine WebSocket batches through
the real CLI process. The smaller-cap probe skips both trace-batch boundaries
because the element cap rejects the batch first. Neither skip is a passed check.
On an empty local chain the probe reports three real-trace checks as skipped.

Additional failure-path checks confirmed that both RPC helpers reject empty,
truncated, duplicate-document and incomplete responses, duplicate/wrong IDs,
HTTP failures and responses containing both a result and an error. Malformed
batch-limit values fail without printing server data. A local negative control
with enough concurrency slots to avoid saturation makes the concurrency phase
fail with exit status 1; missed contention cannot become a successful skip.

## Running the checks

Use a binary or image built from the revision being verified. Existing paths
and image tags are reused by the runner, so rebuild them after source changes.

```sh
go build -o build/bin/geth ./cmd/geth
bash scripts/test_debug_profile.sh

docker build -t incentum-pow:debugprofile .
RUNNER=docker bash scripts/test_debug_profile.sh

go test ./node ./eth/tracers ./rpc -count=1 -timeout=5m
go test ./cmd/geth -run 'TestDebugProfile|TestWSBatchLimitFlag' -count=1 -timeout=5m
```

The local runner creates disposable nodes and sends state-changing requests
only to those nodes. The live probe uses read-only requests or requests missing
required arguments, and stops if its initial restriction checks cannot be
verified. It still performs real tracing and consumes CPU and chain database I/O.

```sh
bash scripts/probe_debug_profile.sh "$RPC_ENDPOINT"
```

`PROBE_TIMEOUT` defaults to 35 seconds, allowing the profile's 30-second request
deadline. `PROBE_SCAN_BLOCKS` defaults to 50; increase it if recent blocks have
no transactions. The scripts return nonzero on failures. Exit status 0 with
skipped checks is not evidence that those checks were exercised.

## Devnet acceptance: pending

No devnet run was performed during this review: its endpoint and deployment
revision were not available. The local results above do not satisfy the devnet
acceptance item in issue #124.

After an authorized devnet run, attach a summary using this format:

```text
Environment: devnet (endpoint omitted)
Date (UTC): <date>
Probe source commit: <full SHA>
Node deployment commit: <full SHA, or explicitly unavailable>
Expected profile: trace-indexer-v1
Result: <passed> passed, <failed> failed, <skipped> skipped
Exit status: <status>
Real transaction and containing-block traces: <verified or not exercised>
Skipped checks: <names and reasons, with configuration values omitted>
```

The probe cannot discover a trustworthy deployment SHA through restricted RPC;
obtain it from deployment records. Do not substitute the probe's SHA for the
node's SHA. Omit endpoints, ports, credentials, addresses, hashes of live
transactions, raw RPC responses and effective node configuration from evidence.
Do not enable shell tracing when collecting a shareable run.
