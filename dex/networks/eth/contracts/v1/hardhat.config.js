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
    } : {}),
    ...(process.env.AMOY_RPC_URL ? {
      amoy: {
        url: process.env.AMOY_RPC_URL,
        accounts: [process.env.PRIVATE_KEY]
      }
    } : {}),
    ...(process.env.BASE_SEPOLIA_RPC_URL ? {
      baseSepolia: {
        url: process.env.BASE_SEPOLIA_RPC_URL,
        accounts: [process.env.PRIVATE_KEY]
      }
    } : {}),
    ...(process.env.POLYGON_RPC_URL ? {
      polygon: {
        url: process.env.POLYGON_RPC_URL,
        accounts: [process.env.PRIVATE_KEY]
      }
    } : {}),
    ...(process.env.BASE_RPC_URL ? {
      base: {
        url: process.env.BASE_RPC_URL,
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
  etherscan: {
    apiKey: process.env.ETHERSCAN_API_KEY || "",
  },
  sourcify: {
    enabled: true
  },
};
