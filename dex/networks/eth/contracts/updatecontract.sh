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

VERSION=$1
PKG_NAME=v${VERSION}
CONTRACT_NAME=ETHSwap
SOLIDITY_FILE=./${CONTRACT_NAME}V${VERSION}.sol
if [ ! -f ${SOLIDITY_FILE} ]
then
    echo "${SOLIDITY_FILE} does not exist" >&2
    exit 1
fi

mkdir temp

solc --abi --bin --bin-runtime --overwrite --optimize ${SOLIDITY_FILE} -o ./temp/
BYTECODE=$(<./temp/${CONTRACT_NAME}.bin-runtime)

cat > "./${PKG_NAME}/BinRuntimeV${VERSION}.go" <<EOF
// Code generated - DO NOT EDIT.
// This file is a generated binding and any manual changes will be lost.

package ${PKG_NAME}

const ${CONTRACT_NAME}RuntimeBin = "${BYTECODE}"
EOF

abigen --abi ./temp/${CONTRACT_NAME}.abi --bin ./temp/${CONTRACT_NAME}.bin --pkg ${PKG_NAME} \
 --type ${CONTRACT_NAME} --out ./${PKG_NAME}/contract.go

BYTECODE=$(<./temp/${CONTRACT_NAME}.bin)

for HARNESS_PATH in "$(realpath ../../../testing/eth/harness.sh)" "$(realpath ../../../testing/polygon/harness.sh)"; do
  sed -i.tmp "s/ETH_SWAP_V${VERSION}=.*/ETH_SWAP_V${VERSION}=\"${BYTECODE}\"/" "${HARNESS_PATH}"
  # mac needs a temp file specified above.
  rm "${HARNESS_PATH}.tmp"
done

rm -fr temp
