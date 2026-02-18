require("@nomicfoundation/hardhat-toolbox");
require('dotenv').config();

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  defaultNetwork: "hardhat",
  networks: {
    hardhat: {},
    ...(process.env.SEPOLIA_RPC_URL ? {
      sepolia: {
        url: process.env.SEPOLIA_RPC_URL,
        accounts: [process.env.PRIVATE_KEY]
      }
    } : {})
  },
  solidity: {
    version: "0.8.23",
    settings: {
      optimizer: {
        enabled: true,
        runs: 1000
      }
    }
  },
};
