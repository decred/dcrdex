const hre = require("hardhat");

async function main() {
  const ETHSwap = await hre.ethers.getContractFactory("ETHSwap");
  const ethSwap = await ETHSwap.deploy();
  await ethSwap.waitForDeployment();
  const ethSwapAddr = await ethSwap.getAddress();
  console.log("ETHSwap deployed to:", ethSwapAddr);
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });
