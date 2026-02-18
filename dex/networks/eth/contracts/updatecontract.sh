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
SOLIDITY_FILE=./${PKG_NAME}/contracts/${CONTRACT_NAME}V${VERSION}.sol
if [ ! -f ${SOLIDITY_FILE} ]
then
    echo "${SOLIDITY_FILE} does not exist" >&2
    exit 1
fi

mkdir temp
mkdir -p ${PKG_NAME}

if [ "${VERSION}" -eq "0" ]
then
  solc --abi --bin --bin-runtime --overwrite --optimize ${SOLIDITY_FILE} -o ./temp/
  ABI_FILE=./temp/${CONTRACT_NAME}.abi
  BIN_FILE=./temp/${CONTRACT_NAME}.bin
else
  cd ./${PKG_NAME} && npm install && npx hardhat compile && cd ../

  # Extract ABI and bytecode from Hardhat artifacts.
  ARTIFACT=./${PKG_NAME}/artifacts/contracts/${CONTRACT_NAME}V${VERSION}.sol/${CONTRACT_NAME}.json
  if [ ! -f "${ARTIFACT}" ]
  then
      echo "Hardhat artifact not found: ${ARTIFACT}" >&2
      rm -fr temp
      exit 1
  fi

  node -e "
    const art = require('./${PKG_NAME}/artifacts/contracts/${CONTRACT_NAME}V${VERSION}.sol/${CONTRACT_NAME}.json');
    const fs = require('fs');
    fs.writeFileSync('./temp/${CONTRACT_NAME}.abi', JSON.stringify(art.abi));
    fs.writeFileSync('./temp/${CONTRACT_NAME}.bin', art.bytecode.replace('0x', ''));
  "
  ABI_FILE=./temp/${CONTRACT_NAME}.abi
  BIN_FILE=./temp/${CONTRACT_NAME}.bin
fi

abigen --abi ${ABI_FILE} --bin ${BIN_FILE} --pkg ${PKG_NAME} \
 --type ${CONTRACT_NAME} --out ./${PKG_NAME}/contract.go

BYTECODE=$(<${BIN_FILE})

echo "${BYTECODE}" | xxd -r -p > "v${VERSION}/contract.bin"

rm -fr temp
