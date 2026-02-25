const hre = require("hardhat");

async function main() {
  const ETHSwap = await hre.ethers.getContractFactory("ETHSwap");
  const ethSwap = await ETHSwap.deploy("0x0000000071727De22E5E9d8BAf0edAc6f37da032");
  await ethSwap.waitForDeployment();
  console.log("Contract deployed to:", await ethSwap.getAddress());
}

main()
  .then(() => process.exit(0))
  .catch((error) => {
    console.error(error);
    process.exit(1);
  });
