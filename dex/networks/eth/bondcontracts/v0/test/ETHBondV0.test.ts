import { expect } from "chai";
import { ethers } from "hardhat";
import { Contract, Signer } from "ethers";

describe("ETHBond", () => {
  let ethBond: Contract;
  let addr1: Signer;
  let addr2: Signer;

  const accountID = ethers.utils.id("account1");
  let bondIDs : string[] = [];
  let addrs : Signer[] = [];

  const reset = async () => {
    const EthBondFactory = await ethers.getContractFactory("ETHBond");
    const signers = await ethers.getSigners();
    addr1 = signers[0];
    addr2 = signers[1];

    bondIDs = [];
    for (let i = 0; i < 10; i++) {
      bondIDs.push(await signers[i + 3].getAddress());
      addrs.push(signers[i + 3]);
    }
  
    ethBond = await EthBondFactory.deploy();
  }

  beforeEach(reset);

  it("should test gas used", async () => {
    /* === Gas usage ===

    Create 1: 72076 (first creation is higher due to creating the commitment array)
    Create 2: 54976
    Create 3: 54976
   
     # Old
# New      0      1     2      3      4      5      6 
      1  41143, 42793, 50628, 55233, 59839, 64444, 69050
      2  45002, 57281, 52524, 60360, 64965, 69571, 74177
      3  48850, 61829, 67001, 62245, 70080, 74686, 79292
      4  52697, 66376, 71549, 76722, 71965, 79802, 84408
      5  56545, 70924, 76097, 81270, 86443, 81687, 89524
      6  60392, 75472, 80645, 85818, 90992, 96165, 91410
    */

    const eth = (x : number) : BigInt => {
      return ethers.utils.parseEther(x.toString());
    }

    for (let i = 0; i < 3; i++) {
        const createTx = await ethBond.connect(addr1).createBond(accountID, {value: eth(1), locktime: 1000}, {value: eth(1)});
        const receipt = await createTx.wait();
        console.log(`Create ${i+1}: ${receipt.gasUsed}`);
    }

    const updateGasMatrix = [];
    for (let i = 0; i < 7; i++) {
      updateGasMatrix[i] = [];
    }

    const makeBonds = (numBonds: number) : any => {
      const bonds : any = [];
      for (let i = 0; i < numBonds; i++) {
        bonds.push({value: eth(1), locktime: 1000});
      }
      return bonds;
    }
  
    for (let oldBonds = 1; oldBonds < 7; oldBonds++) {
      for (let newBonds = 0; newBonds < 7; newBonds++) {

        await reset();
        
        const initialBond = {value: eth(1), locktime: 1000};
        await ethBond.connect(addr1).createBond(accountID, initialBond, {value: eth(1)});

        let opts = undefined;
        if (oldBonds > 1) {
          await ethBond.connect(addr1).updateBonds(accountID, [initialBond], makeBonds(oldBonds), 0, {value: eth(oldBonds - 1)});
        } else {
          await ethBond.connect(addr1).updateBonds(accountID, [initialBond], makeBonds(oldBonds), 0); 
        }

        opts = undefined;
        let updateTx;
        if (newBonds > oldBonds) {
          updateTx = await ethBond.connect(addr1).updateBonds(accountID, makeBonds(oldBonds), makeBonds(newBonds), 0, {value: eth(newBonds - oldBonds)});
        } else {
          updateTx = await ethBond.connect(addr1).updateBonds(accountID, makeBonds(oldBonds), makeBonds(newBonds), 0);
        }
        const receipt = await updateTx.wait();
        const gas : any = updateGasMatrix[oldBonds]
        gas[newBonds] = receipt.gasUsed;
      }
    }

    // Print gas matrix
    for (let i = 0; i < 7; i++) {
      console.log(updateGasMatrix[i].join(", "));
    }
  });

  it("should emit events after creation and update", async() => {
    const createBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const update = [
      {value: ethers.utils.parseEther("1"), locktime: 2000},
      {value: ethers.utils.parseEther("2"), locktime: 1000}
    ];

    const createTx = await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    const createReceipt = await createTx.wait();
    const createEvent = createReceipt.events[0];
    expect(createEvent.event).to.equal("BondCreated");
    expect(createEvent.args.acctID).to.equal(accountID);
    expect(createEvent.args.index).to.equal(0);
    expect(createEvent.args.bond.value).to.equal(createBond.value);
    expect(createEvent.args.bond.locktime).to.equal(createBond.locktime);

    const createTx2 = await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    const createReceipt2 = await createTx2.wait();
    const createEvent2 = createReceipt2.events[0];
    expect(createEvent2.event).to.equal("BondCreated");
    expect(createEvent2.args.acctID).to.equal(accountID);
    expect(createEvent2.args.index).to.equal(1);
    expect(createEvent2.args.bond.value).to.equal(createBond.value);
    expect(createEvent2.args.bond.locktime).to.equal(createBond.locktime);

    const updateTx = await ethBond.connect(addr1).updateBonds(accountID, [createBond], update, 0, {value: ethers.utils.parseEther("2")});
    const updateReceipt = await updateTx.wait();
    const updateEvent = updateReceipt.events[0];
    expect(updateEvent.event).to.equal("BondUpdated");
    expect(updateEvent.args.acctID).to.equal(accountID);
    expect(updateEvent.args.index).to.equal(0);
    expect(updateEvent.args.oldBonds[0].value).to.equal(createBond.value);
    expect(updateEvent.args.oldBonds[0].locktime).to.equal(createBond.locktime);
    expect(updateEvent.args.newBonds[0].value).to.equal(update[0].value);
    expect(updateEvent.args.newBonds[0].locktime).to.equal(update[0].locktime);
    expect(updateEvent.args.newBonds[1].value).to.equal(update[1].value);
    expect(updateEvent.args.newBonds[1].locktime).to.equal(update[1].locktime);
  });

  it("should update bonds if correct index", async () => {
    const createBond1 = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const createBond2 = {value: ethers.utils.parseEther("2"), locktime: 2000};

    await ethBond.connect(addr1).createBond(accountID, createBond1, { value: createBond1.value });
    await ethBond.connect(addr1).createBond(accountID, createBond2, { value: createBond2.value });

    await expect(ethBond.connect(addr1).updateBonds(accountID, [createBond1], [], 1)).to.be.revertedWith("incorrect commit");
    await expect(ethBond.connect(addr1).updateBonds(accountID, [createBond2], [], 0)).to.be.revertedWith("incorrect commit");

    const balanceBefore = await addr1.getBalance();
    const updateTx1 = await ethBond.connect(addr1).updateBonds(accountID, [createBond1], [], 0);
    const updateTx2 = await ethBond.connect(addr1).updateBonds(accountID, [createBond2], [], 1);
    const tx1Receipt = await updateTx1.wait();
    const tx2Receipt = await updateTx2.wait();
    const totalFee = tx1Receipt.gasUsed * tx1Receipt.effectiveGasPrice + tx2Receipt.gasUsed * tx2Receipt.effectiveGasPrice;
    const balanceAfter = await addr1.getBalance();
    expect(balanceAfter.sub(balanceBefore).add(totalFee).toString()).to.equal(ethers.utils.parseEther("3").toString());
  });

  it("should refund all bonds", async () => {
    const createBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const update1 = [
      {value: ethers.utils.parseEther("1"), locktime: 2000},
      {value: ethers.utils.parseEther("2"), locktime: 1000}
    ];
    const update2 : any = [];
    await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    await ethBond.connect(addr1).updateBonds(accountID, [createBond], update1, 0, { value: ethers.utils.parseEther("2") });
    const balanceBefore = await addr1.getBalance();
    const updateTx = await ethBond.connect(addr1).updateBonds(accountID, update1, update2, 0);
    const updateReceipt = await updateTx.wait();
    const gasPrice = updateReceipt.gasUsed * updateReceipt.effectiveGasPrice;
    const balanceAfter = await addr1.getBalance();
    expect(balanceAfter.sub(balanceBefore).add(gasPrice).toString()).to.equal(ethers.utils.parseEther("3").toString());
  });

  it("should extend bonds, consolidate 2 into one", async () => {
    const createBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const update1 = [
      {value: ethers.utils.parseEther("1"), locktime: 2000},
      {value: ethers.utils.parseEther("2"), locktime: 1000}
    ];
    const update2 = [
      {value: ethers.utils.parseEther("3"), locktime: 4000}
    ];
    await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    await ethBond.connect(addr1).updateBonds(accountID, [createBond], update1, 0, { value: ethers.utils.parseEther("2") });
    await ethBond.connect(addr1).updateBonds(accountID, update1, update2, 0);
  });

  it("should extend bonds, add additional value", async () => {
    const createBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const update1 = [
      {value: ethers.utils.parseEther("1"), locktime: 2000},
      {value: ethers.utils.parseEther("2"), locktime: 1000}
    ];
    const update2 = [
      {value: ethers.utils.parseEther("4"), locktime: 4000}
    ];
    await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    await ethBond.connect(addr1).updateBonds(accountID, [createBond], update1, 0, { value: ethers.utils.parseEther("2") });
    await ethBond.connect(addr1).updateBonds(accountID, update1, update2, 0, { value: ethers.utils.parseEther("1") });
  });

  it("should error if extending with incorrect amount", async () => {
    const createBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const update1 = [
      {value: ethers.utils.parseEther("1"), locktime: 2000},
      {value: ethers.utils.parseEther("2"), locktime: 1000}
    ];
    const update2 = [
      {value: ethers.utils.parseEther("4"), locktime: 4000}
    ];
    await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    await ethBond.connect(addr1).updateBonds(accountID, [createBond], update1, 0, { value: ethers.utils.parseEther("2") });
    await expect(ethBond.connect(addr1).updateBonds(accountID, update1, update2, 0, { value: ethers.utils.parseEther("0.5") })).to.be.revertedWith("incorrect amount");
  });

  it("should extend bonds, extend / refund", async () => {
    const createBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const update1 = [
      {value: ethers.utils.parseEther("1"), locktime: 2000},
      {value: ethers.utils.parseEther("2"), locktime: 1000}
    ];
    const update2 = [
      {value: ethers.utils.parseEther("1.5"), locktime: 4000}
    ];

    await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    await ethBond.connect(addr1).updateBonds(accountID, [createBond], update1, 0, { value: ethers.utils.parseEther("2") });
    const balanceBefore = await addr1.getBalance();
    const updateTx = await ethBond.connect(addr1).updateBonds(accountID, update1, update2, 0);    
    const updateReceipt = await updateTx.wait();
    const gasPrice = updateReceipt.gasUsed * updateReceipt.effectiveGasPrice;
    const balanceAfter = await addr1.getBalance();
    expect(balanceAfter.sub(balanceBefore).add(gasPrice).toString()).to.equal(ethers.utils.parseEther("1.5").toString());
  });

  it("should error if shortening locktime", async() => {
    const createBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const update1 = [
      {value: ethers.utils.parseEther("2"), locktime: 2000},
      {value: ethers.utils.parseEther("1"), locktime: 1000}
    ]
    const update2 = [
      {value: ethers.utils.parseEther("3"), locktime: 1999}
    ];
    await ethBond.connect(addr1).createBond(accountID, createBond, { value: createBond.value });
    await ethBond.connect(addr1).updateBonds(accountID, [createBond], update1, 0, { value: ethers.utils.parseEther("2") });
    await expect(ethBond.connect(addr1).updateBonds(accountID, update1, update2, 0)).to.be.revertedWith("illegal lock time shortening");
  });

  it("should error if new bonds unsorted", async() => {
    const oldBond = {value: ethers.utils.parseEther("1"), locktime: 1000};
    const newBonds = [
      {value: ethers.utils.parseEther("1"), locktime: 1000},
      {value: ethers.utils.parseEther("1"), locktime: 2000}
    ];
    await ethBond.connect(addr1).createBond(accountID, oldBond, { value: oldBond.value });
    await expect(ethBond.connect(addr1).updateBonds(accountID, [oldBond], newBonds, 0, {value: ethers.utils.parseEther("1")})).to.be.revertedWith("bonds not sorted");
  });
});
