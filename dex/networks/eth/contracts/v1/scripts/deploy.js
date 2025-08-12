const hre = require("hardhat");

async function main() {
  const ETHSwap = await hre.ethers.getContractFactory("ETHSwap");
  const ethSwap = await ETHSwap.deploy("0x5FF137D4b0FDCD49DcA30c7CF57E578a026d2789");
  await ethSwap.deployed();
  console.log("Contract deployed to:", ethSwap.address);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });