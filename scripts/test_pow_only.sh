#!/bin/bash

echo "=== Testing PoW-only Geth build ==="

TEST_DIR="/tmp/geth_pow_test"
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
  "difficulty": "0x400",
  "gasLimit": "0x8000000",
  "alloc": {
    "0x7df9a875a174b3bc565e6424a0050ebc1b2d1d82": { "balance": "300000" },
    "0xf41c74c9ae680c1aa78f42e5647a62f353b7bdde": { "balance": "400000" }
  }
}
EOF

echo "1. Initializing test blockchain..."
./geth --datadir $TEST_DIR init $TEST_DIR/genesis.json

echo "2. Starting geth in background for 10 seconds..."
timeout 10s ./geth --datadir $TEST_DIR --http --http.api eth,net,web3 --mine --miner.threads 1 --miner.etherbase 0x7df9a875a174b3bc565e6424a0050ebc1b2d1d82 --nodiscover --maxpeers 0 > $TEST_DIR/geth.log 2>&1 &

GETH_PID=$!
sleep 5

echo "3. Testing if Engine API is disabled..."
ENGINE_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"engine_newPayloadV1","params":[],"id":1}' http://localhost:8545 2>/dev/null || echo "Connection failed")

echo "4. Testing basic eth API..."
ETH_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data '{"jsonrpc":"2.0","method":"eth_chainId","params":[],"id":1}' http://localhost:8545 2>/dev/null || echo "Connection failed")

echo "5. Testing PUSH0 opcode support..."
PUSH0_BYTECODE="0x5f5f5f5f"
PUSH0_RESULT=$(curl -s -X POST -H "Content-Type: application/json" --data "{\"jsonrpc\":\"2.0\",\"method\":\"eth_call\",\"params\":[{\"data\":\"$PUSH0_BYTECODE\"},\"latest\"],\"id\":1}" http://localhost:8545 2>/dev/null || echo "Connection failed")
kill $GETH_PID 2>/dev/null
wait $GETH_PID 2>/dev/null

echo ""
echo "=== RESULTS ==="
echo "Engine API call result: $ENGINE_RESULT"
echo "ETH API call result: $ETH_RESULT"
echo "PUSH0 test result: $PUSH0_RESULT"

echo ""
echo "=== LOG ANALYSIS ==="
echo "Checking for PoW-only mode messages..."
grep -i "pow.*only\|engine.*api.*disabled\|beacon.*consensus.*disabled" $TEST_DIR/geth.log || echo "No PoW-only messages found"

echo ""
echo "Checking for Engine API related errors..."
grep -i "engine.*api\|catalyst\|beacon" $TEST_DIR/geth.log || echo "No Engine API messages found"

echo ""
echo "Checking for mining activity..."
grep -i "mined\|mining\|commit.*new.*work" $TEST_DIR/geth.log | head -3 || echo "No mining messages found"

rm -rf $TEST_DIR

echo ""
echo "=== TEST COMPLETE ==="
