#!/usr/bin/env bash
# This script generates the runtime bytecoode for a solidity contract. It is used
# to compare against the runtime bytecode on chain in order to verify that the
# expected contract is deployed.

if [ "$#" -ne 1 ]
then
  echo "Usage: $0 version" >&2
  exit 1
fi

ETH_SWAP_VERSION=$1
SOLIDITY_FILE=ETHSwapV${ETH_SWAP_VERSION}.sol
if [ ! -f ./${SOLIDITY_FILE} ]
then
    echo "${SOLIDITY_FILE} does not exist" >&2
    exit 1
fi

solc --bin-runtime --optimize ${SOLIDITY_FILE} -o .
BYTECODE=$(<ETHSwap.bin-runtime)

cat > "../BinRuntimeV${ETH_SWAP_VERSION}.go" <<EOF
// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package eth

const ETHSwapRuntimeBin = "${BYTECODE}"
EOF

rm ETHSwap.bin-runtime