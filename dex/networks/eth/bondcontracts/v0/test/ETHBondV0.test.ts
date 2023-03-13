import { expect } from "chai";
import { ethers } from "hardhat";
import { Contract, Signer } from "ethers";

describe("ETHBond", () => {
  let ethBond: Contract;
  let owner: Signer;
  let addr1: Signer;
  let addr2: Signer;

  const accountID = ethers.utils.id("account1");
  const bondID1 = ethers.utils.id("bond1");
  const bondID2 = ethers.utils.id("bond2");
  const bondID3 = ethers.utils.id("bond3");
  const bondID4 = ethers.utils.id("bond4");
  const bondID5 = ethers.utils.id("bond5");
  

  beforeEach(async () => {
    const EthBondFactory = await ethers.getContractFactory("ETHBond");
    [owner, addr1, addr2] = await ethers.getSigners();
    ethBond = await EthBondFactory.deploy();
  });

  it("should create a bond", async () => {
    await ethBond.connect(addr1).createBond(accountID, bondID1, 1000, { value: ethers.utils.parseEther("1") });
    const bond = await ethBond.bondData(accountID, bondID1);
    const bondIDs = (await ethBond.bondsForAccount(accountID)).map((bondData: any) => bondData[0]);

    expect(bond.value.toString()).to.equal(ethers.utils.parseEther("1").toString());
    expect(bond.locktime).to.equal(1000);
    expect(bond.owner).to.equal(await addr1.getAddress());
    expect(bondIDs).to.deep.equal([bondID1]);
  });

  it("should extend the locktime of a bond without changing the amount", async () => {
    const bondID2 = ethers.utils.hexlify(ethers.utils.randomBytes(32));
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, 1000, { value: ethers.utils.parseEther("1") });
    const newLocktime = 2000;
  
    await ethBond.connect(addr1).updateBonds(accountID, [bondID1], [bondID2], ethers.utils.parseEther("1"), newLocktime);
  
    const bond1AfterUpdate = await ethBond.bondData(accountID, bondID1);
    const bond2 = await ethBond.bondData(accountID, bondID2);
    const bondIDs = (await ethBond.bondsForAccount(accountID)).map((bondData: any) => bondData[0]);
  
    expect(bond1AfterUpdate.value.toString()).to.equal("0");
    expect(bond2.value.toString()).to.equal(ethers.utils.parseEther("1").toString());
    expect(bond2.locktime.toString()).to.equal(newLocktime.toString());
    expect(bondIDs).to.deep.equal([bondID2]);
  });
  
  it("should extend the locktime and decrease the amount of a bond, resulting in two new bonds", async () => {  
    await ethBond.connect(addr1).createBond(accountID, bondID1, 1000, { value: ethers.utils.parseEther("1") });
    const newLocktime = 2000;
  
    await ethBond.connect(addr1).updateBonds(accountID, [bondID1], [bondID2, bondID3], ethers.utils.parseEther("0.5"), newLocktime);
  
    const bond1AfterUpdate = await ethBond.bondData(accountID, bondID1);
    const bond2 = await ethBond.bondData(accountID, bondID2);
    const bond3 = await ethBond.bondData(accountID, bondID3);
    const bondIDs = (await ethBond.bondsForAccount(accountID)).map((bondData: any) => bondData[0]);

  
    expect(bond1AfterUpdate.value.toString()).to.equal("0");
    expect(bond2.value.toString()).to.equal(ethers.utils.parseEther("0.5").toString());
    expect(bond2.locktime.toString()).to.equal("1000");
    expect(bond3.value.toString()).to.equal(ethers.utils.parseEther("0.5").toString());
    expect(bond3.locktime.toString()).to.equal(newLocktime.toString());
    expect(bondIDs).to.deep.equal([bondID3, bondID2]);
  });

  it("should update three bonds to output two bonds, one with 0.05 ETH and one with 0.25 ETH", async () => {
    const amount = ethers.utils.parseEther("0.1");
    const newLocktime = 2000;
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, 1000, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID2, 1100, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID3, 1200, { value: amount });
  
    await ethBond.connect(addr1).updateBonds(accountID, [bondID1, bondID2, bondID3], [bondID4, bondID5], ethers.utils.parseEther("0.25"), newLocktime);
  
    const bond1AfterUpdate = await ethBond.bondData(accountID, bondID1);
    const bond2AfterUpdate = await ethBond.bondData(accountID, bondID2);
    const bond3AfterUpdate = await ethBond.bondData(accountID, bondID3);
    const bond4 = await ethBond.bondData(accountID, bondID4);
    const bond5 = await ethBond.bondData(accountID, bondID5);
    const bondIDs = (await ethBond.bondsForAccount(accountID)).map((bondData: any) => bondData[0]);

  
    expect(bond1AfterUpdate.value.toString()).to.equal("0");
    expect(bond2AfterUpdate.value.toString()).to.equal("0");
    expect(bond3AfterUpdate.value.toString()).to.equal("0");
    expect(bond4.value.toString()).to.equal(ethers.utils.parseEther("0.05").toString());
    expect(bond4.locktime.toString()).to.equal("1000");
    expect(bond5.value.toString()).to.equal(ethers.utils.parseEther("0.25").toString());
    expect(bond5.locktime.toString()).to.equal(newLocktime.toString());
    expect(bondIDs).to.deep.equal([bondID5, bondID4]);
  });  

  it("should update three bonds to have an amount of 0.4 ETH", async () => {  
    const amount = ethers.utils.parseEther("0.1");
    const newLocktime = 2000;
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, 1000, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID2, 1100, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID3, 1200, { value: amount });
  
    await ethBond.connect(addr1).updateBonds(accountID, [bondID1, bondID2, bondID3], [bondID4], ethers.utils.parseEther("0.4"), newLocktime, { value: amount });
  
    const bond1AfterUpdate = await ethBond.bondData(accountID, bondID1);
    const bond2AfterUpdate = await ethBond.bondData(accountID, bondID2);
    const bond3AfterUpdate = await ethBond.bondData(accountID, bondID3);
    const bond4 = await ethBond.bondData(accountID, bondID4);
    const bondIDs = (await ethBond.bondsForAccount(accountID)).map((bondData: any) => bondData[0]);

  
    expect(bond1AfterUpdate.value.toString()).to.equal("0");
    expect(bond2AfterUpdate.value.toString()).to.equal("0");
    expect(bond3AfterUpdate.value.toString()).to.equal("0");
    expect(bond4.value.toString()).to.equal(ethers.utils.parseEther("0.4").toString());
    expect(bond4.locktime.toString()).to.equal(newLocktime.toString());
    expect(bondIDs).to.deep.equal([bondID4]);
  });

  it("should fail if the new bond ID already exists", async () => {  
    const amount = ethers.utils.parseEther("0.1");
    const newLocktime = 2000;
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, 1000, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID2, 1000, { value: amount });
  
    await expect(ethBond.connect(addr1).updateBonds(accountID, [bondID1, bondID2], [bondID2], ethers.utils.parseEther("0.2"), newLocktime)).to.be.revertedWith("new bond already exists");
  });

  it("should fail if the updated locktime is less than the latest bond's locktime", async () => {  
    const amount = ethers.utils.parseEther("0.1");
    const locktime = 1000;
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, locktime, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID2, locktime + 100, { value: amount });
  
    await expect(ethBond.connect(addr1).updateBonds(accountID, [bondID1, bondID2], [bondID3], ethers.utils.parseEther("0.2"), locktime)).to.be.revertedWith("locktime cannot decrease");
  });

  it("should fail if the bonds are not sorted by locktime", async () => {  
    const amount = ethers.utils.parseEther("0.1");
    const locktime = 1000;
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, locktime, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID2, locktime - 100, { value: amount });
  
    await expect(ethBond.connect(addr1).updateBonds(accountID, [bondID1, bondID2], [bondID3], ethers.utils.parseEther("0.2"), locktime + 1000)).to.be.revertedWith("bonds must be sorted by locktime");
  });  
  
  it("should fail if the bond owner is not the same as the msg.sender", async () => {
    const bondID2 = ethers.utils.hexlify(ethers.utils.randomBytes(32));
    const newBondID = ethers.utils.hexlify(ethers.utils.randomBytes(32));
  
    const amount = ethers.utils.parseEther("0.1");
    const locktime = 1000;
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, locktime, { value: amount });
    await ethBond.connect(addr2).createBond(accountID, bondID2, locktime, { value: amount });
  
    await expect(ethBond.connect(addr1).updateBonds(accountID, [bondID1, bondID2], [newBondID], ethers.utils.parseEther("0.2"), locktime + 1000)).to.be.revertedWith("bond owner != msg.sender");
  });

  it("should fail if the funds sent with the update call are incorrect", async () => {
    const amount = ethers.utils.parseEther("0.1");
    const locktime = 1000;
  
    await ethBond.connect(addr1).createBond(accountID, bondID1, locktime, { value: amount });
    await ethBond.connect(addr1).createBond(accountID, bondID2, locktime, { value: amount });
  
    await expect(ethBond.connect(addr1).updateBonds(accountID, [bondID1, bondID2], [bondID3], ethers.utils.parseEther("0.3"), locktime + 1000, { value: ethers.utils.parseEther("0.05") })).to.be.revertedWith("insufficient funds");
  });  
  
  it("should refund a bond", async () => {
    await ethBond.connect(addr1).createBond(accountID, bondID1, 1000, { value: ethers.utils.parseEther("1") });
  
    const bond1 = await ethBond.bondData(accountID, bondID1);
    await ethers.provider.send("evm_increaseTime", [bond1.locktime.toNumber()]);
    await ethers.provider.send("evm_mine");
  
    const balanceBefore = await addr1.getBalance();
  
    const refundTx = await ethBond.connect(addr1).refundBond(accountID, bondID1);
    const refundReceipt = await refundTx.wait();
  
    const gasPrice = refundReceipt.gasUsed * refundReceipt.effectiveGasPrice;
  
    const bond1AfterRefund = await ethBond.bondData(accountID, bondID1);
    const balanceAfter = await addr1.getBalance();
    const bondIDs = (await ethBond.bondsForAccount(accountID)).map((bondData: any) => bondData[0]);

    expect(bond1AfterRefund.value.toString()).to.equal("0");
    expect(balanceAfter.sub(balanceBefore).add(gasPrice).toString()).to.equal(ethers.utils.parseEther("1").toString());
    expect(bondIDs).to.deep.equal([]);
  });
});
