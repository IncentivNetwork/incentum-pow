#!/bin/bash

echo "=== Detailed DIFFICULTY opcode test ==="

TEST_DIR="/tmp/geth_difficulty_test"
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
    "0x7df9a875a174b3bc565e6424a0050ebc1b2d1d82": { "balance": "300000" },
    "0x1000000000000000000000000000000000000001": {
      "balance": "0",
      "code": "0x44600052602060006000f3"
    }
  }
}
EOF

echo "1. Initializing test blockchain..."
./geth --datadir $TEST_DIR init $TEST_DIR/genesis.json

echo "2. Starting geth in background..."
timeout 10s ./geth --datadir $TEST_DIR --http --http.api eth,net,web3,debug --mine --miner.threads 1 --miner.etherbase 0x7df9a875a174b3bc565e6424a0050ebc1b2d1d82 --nodiscover --maxpeers 0 > $TEST_DIR/geth.log 2>&1 &

GETH_PID=$!
sleep 5

echo "3. Testing deployed contract with DIFFICULTY..."
CONTRACT_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_call","params":[{"to":"0x1000000000000000000000000000000000000001","data":"0x"},"latest"],"id":1}' http://localhost:8545 2>/dev/null || echo "Connection failed")

echo "4. Testing simple DIFFICULTY bytecode..."
SIMPLE_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_call","params":[{"data":"0x44"},"latest"],"id":1}' http://localhost:8545 2>/dev/null || echo "Connection failed")

echo "5. Getting block difficulty..."
BLOCK_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_getBlockByNumber","params":["latest",false],"id":1}' http://localhost:8545 2>/dev/null || echo "Connection failed")

echo "6. Testing debug trace..."
TRACE_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"debug_traceCall","params":[{"data":"0x44"},"latest"],"id":1}' http://localhost:8545 2>/dev/null || echo "Connection failed")

kill $GETH_PID 2>/dev/null
wait $GETH_PID 2>/dev/null

echo ""
echo "=== RESULTS ==="
echo "Contract call result: $CONTRACT_RESULT"
echo "Simple call result: $SIMPLE_RESULT"
echo "Block difficulty: $(echo $BLOCK_RESULT | jq -r '.result.difficulty' 2>/dev/null || echo 'parse error')"
echo "Trace result: $TRACE_RESULT"

rm -rf $TEST_DIR

echo ""
echo "=== TEST COMPLETE ==="
