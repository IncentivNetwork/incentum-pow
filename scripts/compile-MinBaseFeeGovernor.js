const solc = require('solc');
const fs = require('fs');
const path = require('path');

// Find project root (where package.json is)
const projectRoot = path.resolve(__dirname, '..');

// Read the contract source
const contractPath = path.join(projectRoot, 'contracts', 'minbasefee', 'MinBaseFeeGovernor.sol');
const source = fs.readFileSync(contractPath, 'utf8');

// Prepare input for compiler
const input = {
    language: 'Solidity',
    sources: {
        'MinBaseFeeGovernor.sol': {
            content: source
        }
    },
    settings: {
        optimizer: {
            enabled: true,
            runs: 200
        },
        evmVersion: 'paris', // Use Paris EVM version to avoid PUSH0 opcode
        outputSelection: {
            '*': {
                '*': ['abi', 'evm.bytecode']
            }
        }
    }
};

// Compile
console.log('Compiling MinBaseFeeGovernor.sol...');
const output = JSON.parse(solc.compile(JSON.stringify(input)));

// Check for errors
if (output.errors) {
    const errors = output.errors.filter(e => e.severity === 'error');
    if (errors.length > 0) {
        console.error('Compilation errors:');
        errors.forEach(err => console.error(err.formattedMessage));
        process.exit(1);
    }

    // Show warnings
    const warnings = output.errors.filter(e => e.severity === 'warning');
    if (warnings.length > 0) {
        console.warn('Compilation warnings:');
        warnings.forEach(warn => console.warn(warn.formattedMessage));
    }
}

// Extract contract data
const contract = output.contracts['MinBaseFeeGovernor.sol']['MinBaseFeeGovernor'];

if (!contract) {
    console.error('Contract not found in compilation output');
    process.exit(1);
}

// Save ABI
const abiPath = path.join(projectRoot, 'contracts', 'minbasefee', 'MinBaseFeeGovernor.abi');
fs.writeFileSync(abiPath, JSON.stringify(contract.abi, null, 2));
console.log(`ABI saved to ${abiPath}`);

// Save bytecode
const binPath = path.join(projectRoot, 'contracts', 'minbasefee', 'MinBaseFeeGovernor.bin');
fs.writeFileSync(binPath, contract.evm.bytecode.object);
console.log(`Bytecode saved to ${binPath}`);

console.log('Compilation successful!');
