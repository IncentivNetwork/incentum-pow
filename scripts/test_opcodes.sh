#!/bin/bash

echo "=== Testing PoW opcodes ==="

TEST_DIR="/tmp/geth_opcode_test"
rm -rf $TEST_DIR
mkdir -p $TEST_DIR

cat > $TEST_DIR/genesis.json << 'EOF'
{
  "config": {
    "chainId": 1337,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "istanbulBlock": 0,
    "berlinBlock": 0,
    "londonBlock": 0,
    "shanghaiTime": 0,
    "ethash": {}
  },
  "difficulty": "0x400000",
  "gasLimit": "0x8000000",
  "alloc": {
    "0x7df9a875a174b3bc565e6424a0050ebc1b2d1d82": { "balance": "300000" }
  }
}
EOF

echo "1. Initializing test blockchain..."
./geth --datadir $TEST_DIR init $TEST_DIR/genesis.json

echo "2. Starting geth in background..."
timeout 15s ./geth --datadir $TEST_DIR --http --http.api eth,net,web3 --mine --miner.threads 1 --miner.etherbase 0x7df9a875a174b3bc565e6424a0050ebc1b2d1d82 --nodiscover --maxpeers 0 > $TEST_DIR/geth.log 2>&1 &

GETH_PID=$!
sleep 8

echo "3. Testing DIFFICULTY opcode (0x44)..."
DIFFICULTY_BYTECODE="0x44" # DIFFICULTY opcode
DIFFICULTY_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"data\":\"$DIFFICULTY_BYTECODE\"},\"latest\"],\"id\":1}" http://localhost:8545 2>/dev/null || echo "Connection failed")

echo "4. Testing contract with DIFFICULTY..."
DIFFICULTY_CONTRACT="0x44600052602060006000f3" # DIFFICULTY PUSH1 0x00 MSTORE PUSH1 0x20 PUSH1 0x00 PUSH1 0x00 RETURN
DIFFICULTY_CONTRACT_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"data\":\"$DIFFICULTY_CONTRACT\"},\"latest\"],\"id\":1}" http://localhost:8545 2>/dev/null || echo "Connection failed")

echo "5. Getting current block info..."
BLOCK_INFO=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}' http://localhost:8545 2>/dev/null || echo "Connection failed")

kill $GETH_PID 2>/dev/null
wait $GETH_PID 2>/dev/null

echo ""
echo "=== RESULTS ==="
echo "DIFFICULTY opcode result: $DIFFICULTY_RESULT"
echo "DIFFICULTY contract result: $DIFFICULTY_CONTRACT_RESULT"
echo "Block info: $BLOCK_INFO" | jq '.result.difficulty' 2>/dev/null || echo "Block info: $BLOCK_INFO"

echo ""
echo "=== LOG ANALYSIS ==="
echo "Checking for mining activity..."
grep -i "mined.*block\|commit.*new.*work" $TEST_DIR/geth.log | head -3 || echo "No mining messages found"

rm -rf $TEST_DIR

echo ""
echo "=== TEST COMPLETE ==="
