import { Mainnet, createCustomCommon } from '@ethereumjs/common'
import { createLegacyTx } from '@ethereumjs/tx'
import { bytesToHex, hexToBytes } from '@ethereumjs/util'
import { Contract } from 'web3-eth-contract'

// This ABI comes from running 'solc --abi TestToken.sol'
const testTokenABI = [{"inputs":[],"stateMutability":"nonpayable","type":"constructor"},{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"airdrop","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"owner","type":"address"},{"internalType":"address","name":"spender","type":"address"}],"name":"allowance","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"decimals","outputs":[{"internalType":"uint8","name":"","type":"uint8"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"name","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"symbol","outputs":[{"internalType":"string","name":"","type":"string"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"user","type":"address"},{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"testApprove","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"totalSupply","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transfer","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"sender","type":"address"},{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"transferFrom","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"}]


const method = process.argv[2];
var to = process.argv[3];
const valueStr = process.argv[4];
const nonceStr = process.argv[5];
const gasPriceStr = process.argv[6];

if (to != "") {
  to = to.startsWith('0x') ? to : '0x' + to
}
const nonce = +nonceStr;
const gasPrice = +gasPriceStr;
var value = 0;
var data = "";

switch (method) {
  case "send":
    data = process.argv[7];
    data = data.startsWith('0x') ? data : '0x' + data
    value = parseFloat(valueStr) * 1.0e18;
    break;

  case "sendtoken":
    var contractAddr = process.argv[7];
    var decimalsStr = process.argv[8];
    var decimals = +decimalsStr
    contractAddr = contractAddr.startsWith('0x') ? contractAddr : '0x' + contractAddr
    var contract = new Contract(testTokenABI, contractAddr);
    value = parseFloat(valueStr) * 10**decimals;
    data = contract.methods.transfer(to, value).encodeABI();
    to = contractAddr;
    value = 0;
    break;

  case "airdroptoken":
    var contractAddr = process.argv[7];
    var decimalsStr = process.argv[8];
    var decimals = +decimalsStr
    contractAddr = contractAddr.startsWith('0x') ? contractAddr : '0x' + contractAddr
    var contract = new Contract(testTokenABI, contractAddr);
    value = parseFloat(valueStr) * 10**decimals;
    data = contract.methods.airdrop(to, value).encodeABI();
    to = contractAddr;
    value = 0;
    break;
}

const txParams = {
  nonce: "0x" + nonce.toString(16),
  gasPrice: "0x" + gasPrice.toString(16),
  gasLimit: '0xffffff',
  to: to,
  value: "0x" + value.toString(16),
  data: data,
}

const common = createCustomCommon({ chainId: 1337 }, Mainnet)
const tx = createLegacyTx(txParams, { common })

const privateKey = hexToBytes('0xbf557536250f420fd4c4fc5c25925dcf616b121dd99d9b98294d8e89f10f8d32')

const signedTx = tx.sign(privateKey)

const serializedTx = signedTx.serialize()
console.log(bytesToHex(serializedTx))
