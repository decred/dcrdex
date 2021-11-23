#!/usr/bin/env bash
# This script does 3 things:
#
# 1. Updates contract.go to reflect updated solidity code.
# 2. Generates the runtime bytecoode. This is used to compare against the runtime
#    bytecode on chain in order to verify that the expected contract is deployed.
# 3. Updates the bytecode in the harness test.

if [ "$#" -ne 1 ]
then
  echo "Usage: $0 version" >&2
  exit 1
fi

ETH_SWAP_VERSION=$1
SOLIDITY_FILE=./contracts/ETHSwapV${ETH_SWAP_VERSION}.sol
if [ ! -f ${SOLIDITY_FILE} ]
then
    echo "${SOLIDITY_FILE} does not exist" >&2
    exit 1
fi

mkdir temp

solc --bin-runtime --optimize ${SOLIDITY_FILE} -o ./temp/
BYTECODE=$(<./temp/ETHSwap.bin-runtime)

cat > "./swap/BinRuntimeV${ETH_SWAP_VERSION}.go" <<EOF
// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package swap

const ETHSwapRuntimeBin = "${BYTECODE}"
EOF

abigen --sol ${SOLIDITY_FILE} --pkg swap --out ./swap/contract.go

solc --bin --optimize ${SOLIDITY_FILE} -o ./temp
BYTECODE=$(<./temp/ETHSwap.bin)
sed -i.tmp "s/ETH_SWAP_V${ETH_SWAP_VERSION}=.*/ETH_SWAP_V${ETH_SWAP_VERSION}=\"${BYTECODE}\"/" ../../testing/eth/harness.sh
# mac needs a temp file specified above.
rm ../../testing/eth/harness.sh.tmp

rm -fr temp
