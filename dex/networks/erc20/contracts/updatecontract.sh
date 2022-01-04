#!/usr/bin/env bash
# This script does the following:
#
# 1. Updates contract.go to reflect updated solidity code.
# 2. Generates the runtime bytecoode. This is used to compare against the runtime
#    bytecode on chain in order to verify that the expected contract is deployed.
# 3. Updates the bytecode for ERC20Swap and the test token contract in the harness test.

if [ "$#" -ne 1 ]
then
  echo "Usage: $0 version" >&2
  exit 1
fi

VERSION=$1
PKG_NAME=v${VERSION}
SOLIDITY_FILE=./ERC20SwapV${VERSION}.sol
TEST_TOKEN=./TestToken.sol
if [ ! -f ${SOLIDITY_FILE} ]
then
    echo "${SOLIDITY_FILE} does not exist" >&2
    exit 1
fi
if [ ! -f ${TEST_TOKEN} ]
then
    echo "${TEST_TOKEN} does not exist" >&2
    exit 1
fi

mkdir temp

solc --bin-runtime --optimize ${SOLIDITY_FILE} -o ./temp/
BYTECODE=$(<./temp/ERC20Swap.bin-runtime)

cat > "./${PKG_NAME}/BinRuntimeV${VERSION}.go" <<EOF
// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package ${PKG_NAME}

const ERC20SwapRuntimeBin = "${BYTECODE}"
EOF

CONTRACT_FILE=./${PKG_NAME}/contract.go 
abigen --sol ${SOLIDITY_FILE} --pkg ${PKG_NAME} --out ${CONTRACT_FILE}

solc --bin --optimize ${SOLIDITY_FILE} -o ./temp
BYTECODE=$(<./temp/ERC20Swap.bin)

solc --bin --optimize ${TEST_TOKEN} -o ./temp
TEST_TOKEN_BYTECODE=$(<./temp/TestToken.bin)

sed -i.tmp "s/ERC20_SWAP_V${VERSION}=.*/ERC20_SWAP_V${VERSION}=\"${BYTECODE}\"/" ../../../testing/eth/harness.sh
# mac needs a temp file specified above.
rm ../../../testing/eth/harness.sh.tmp

sed -i.tmp "s/TEST_TOKEN=.*/TEST_TOKEN=\"${TEST_TOKEN_BYTECODE}\"/" ../../../testing/eth/harness.sh
# mac needs a temp file specified above.
rm ../../../testing/eth/harness.sh.tmp

rm -fr temp

if [ "$VERSION" -eq "0" ]; then
  # Replace a few generated types with the ETH contract versions for interface compatibility.
  perl -0pi -e 's/go-ethereum\/event"/go-ethereum\/event"\n\tethv0 "decred.org\/dcrdex\/dex\/networks\/eth\/contracts\/v0"/' $CONTRACT_FILE

  perl -0pi -e 's/\/\/ ERC20SwapInitiation[^}]*}\n\n//' $CONTRACT_FILE
  perl -0pi -e 's/ERC20SwapInitiation/ethv0.ETHSwapInitiation/g' $CONTRACT_FILE

  perl -0pi -e 's/\/\/ ERC20SwapRedemption[^}]*}\n\n//' $CONTRACT_FILE
  perl -0pi -e 's/ERC20SwapRedemption/ethv0.ETHSwapRedemption/g' $CONTRACT_FILE

  perl -0pi -e 's/\/\/ ERC20SwapSwap[^}]*}\n\n//' $CONTRACT_FILE
  perl -0pi -e 's/ERC20SwapSwap/ethv0.ETHSwapSwap/g' $CONTRACT_FILE
fi
