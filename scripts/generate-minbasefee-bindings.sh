#!/bin/bash
# Script to generate contract ABI, bytecode and Go bindings

set -e

cd "$(dirname "$0")/.."

echo "Compiling MinBaseFeeGovernor.sol..."
node scripts/compile-MinBaseFeeGovernor.js

echo "Generating Go bindings..."
abigen --abi contracts/minbasefee/MinBaseFeeGovernor.abi \
       --bin contracts/minbasefee/MinBaseFeeGovernor.bin \
       --pkg minbasefee \
       --type MinBaseFeeGovernor \
       --out contracts/minbasefee/bindings.go

echo "Contract artifacts generated successfully"
echo "  - contracts/minbasefee/MinBaseFeeGovernor.abi"
echo "  - contracts/minbasefee/MinBaseFeeGovernor.bin"
echo "  - contracts/minbasefee/bindings.go"
