#!/bin/bash
# Regenerate the MinBaseFeeGovernor ABI, runtime bytecode and Go bindings.
#
# Requirements:
#   - foundry (forge)         - already required to build/test the contract
#   - jq                      - JSON extraction
#   - go (for `go run ./cmd/abigen`) OR a stand-alone `abigen` on PATH
#
# No node / npm / solc-js dependency.

set -euo pipefail

cd "$(dirname "$0")/.."

CONTRACT_DIR="contracts/minbasefee"
ARTIFACT="out-minbasefee/MinBaseFeeGovernor.sol/MinBaseFeeGovernor.json"

echo "Compiling MinBaseFeeGovernor.sol via the minbasefee Foundry profile..."
FOUNDRY_PROFILE=minbasefee forge build --silent

if [ ! -f "${ARTIFACT}" ]; then
    echo "ERROR: expected forge artifact at ${ARTIFACT} was not produced." >&2
    exit 1
fi

echo "Extracting ABI and bytecode..."
jq -c '.abi'                                                  "${ARTIFACT}" > "${CONTRACT_DIR}/MinBaseFeeGovernor.abi"
jq -r '.bytecode.object | ltrimstr("0x")'                     "${ARTIFACT}" > "${CONTRACT_DIR}/MinBaseFeeGovernor.bin"

echo "Generating Go bindings..."
if command -v abigen >/dev/null 2>&1; then
    abigen --abi "${CONTRACT_DIR}/MinBaseFeeGovernor.abi" \
           --bin "${CONTRACT_DIR}/MinBaseFeeGovernor.bin" \
           --pkg minbasefee \
           --type MinBaseFeeGovernor \
           --out "${CONTRACT_DIR}/bindings.go"
else
    go run ./cmd/abigen --abi "${CONTRACT_DIR}/MinBaseFeeGovernor.abi" \
                       --bin "${CONTRACT_DIR}/MinBaseFeeGovernor.bin" \
                       --pkg minbasefee \
                       --type MinBaseFeeGovernor \
                       --out "${CONTRACT_DIR}/bindings.go"
fi

echo "Contract artifacts generated successfully"
echo "  - ${CONTRACT_DIR}/MinBaseFeeGovernor.abi"
echo "  - ${CONTRACT_DIR}/MinBaseFeeGovernor.bin"
echo "  - ${CONTRACT_DIR}/bindings.go"
