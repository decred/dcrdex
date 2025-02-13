#!/usr/bin/env bash
#
# 1. Updates contract.go to reflect updated solidity code.
# 2. Generates the runtime bytecoode. This is used to compare against the runtime
#    bytecode on chain in order to verify that the expected contract is deployed.
# 3. Updates the bytecode in the harness test.


PKG_NAME="multibalance"
CONTRACT_NAME="MultiBalanceV0"
SOLIDITY_FILE="./${CONTRACT_NAME}.sol"
if [ ! -f ${SOLIDITY_FILE} ]
then
    echo "${SOLIDITY_FILE} does not exist" >&2
    exit 1
fi

mkdir temp

solc --abi --bin --bin-runtime --overwrite --optimize ${SOLIDITY_FILE} -o ./temp/

abigen --abi ./temp/${CONTRACT_NAME}.abi --bin ./temp/${CONTRACT_NAME}.bin --pkg ${PKG_NAME} \
 --type ${CONTRACT_NAME} --out ./${PKG_NAME}/multibalancev0.go

BYTECODE=$(<./temp/${CONTRACT_NAME}.bin)
echo "${BYTECODE}" | xxd -r -p > "${PKG_NAME}/contract.bin"

rm -fr temp
