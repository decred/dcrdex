const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ETHSwapV1", function () {
  let ethSwap;
  let deployer, initiator, participant, other;
  // Wallets for raw signing (needed for EIP-712 signature verification)
  let participantWallet, otherWallet;

  const ZERO_ADDR = ethers.ZeroAddress;
  const ONE_ETH = ethers.parseEther("1");
  const HALF_ETH = ethers.parseEther("0.5");

  // Step enum values
  const Step = { Empty: 0, Filled: 1, Redeemed: 2, Refunded: 3 };

  // Generate a random secret and its sha256 hash
  function makeSecret() {
    const secret = ethers.randomBytes(32);
    const secretHash = ethers.sha256(secret);
    return { secret: ethers.hexlify(secret), secretHash };
  }

  // Create a Vector struct
  function makeVector(secretHash, value, initiatorAddr, participant, refundTimestamp) {
    return {
      secretHash,
      value,
      initiator: initiatorAddr,
      refundTimestamp,
      participant,
    };
  }

  // Compute contractKey matching the Solidity implementation
  function contractKey(token, v) {
    const packed = ethers.concat([
      v.secretHash,
      ethers.zeroPadValue(v.initiator, 20),
      ethers.zeroPadValue(v.participant, 20),
      ethers.zeroPadValue(ethers.toBeHex(v.value), 32),
      ethers.zeroPadValue(ethers.toBeHex(v.refundTimestamp), 8),
      ethers.zeroPadValue(token, 20),
    ]);
    return sha256(packed);
  }

  // sha256 helper
  function sha256(data) {
    return ethers.sha256(data);
  }

  // Get a future timestamp relative to the latest block
  async function futureTimestamp(seconds = 3600) {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp + seconds;
  }

  // Get a past timestamp relative to the latest block
  async function pastTimestamp(seconds = 3600) {
    const block = await ethers.provider.getBlock("latest");
    return block.timestamp - seconds;
  }

  // Mine a block to advance block.number
  async function mineBlock() {
    await ethers.provider.send("evm_mine", []);
  }

  // Advance time and mine
  async function advanceTime(seconds) {
    await ethers.provider.send("evm_increaseTime", [seconds]);
    await mineBlock();
  }

  // EIP-712 signing helper for redeemWithSignature
  async function signRedeem(wallet, feeRecipient, redemptions, relayerFee, nonce, deadline) {
    const chainId = (await ethers.provider.getNetwork()).chainId;
    const contractAddress = await ethSwap.getAddress();

    const domain = {
      name: "ETHSwap",
      version: "1",
      chainId: chainId,
      verifyingContract: contractAddress,
    };

    // Compute redemptionsHash = keccak256(abi.encode(redemptions))
    const redemptionTupleType = "tuple(tuple(bytes32 secretHash, uint256 value, address initiator, uint64 refundTimestamp, address participant) v, bytes32 secret)[]";
    const redemptionsHash = ethers.keccak256(
      ethers.AbiCoder.defaultAbiCoder().encode(
        [redemptionTupleType],
        [redemptions]
      )
    );

    const types = {
      Redeem: [
        { name: "feeRecipient", type: "address" },
        { name: "redemptionsHash", type: "bytes32" },
        { name: "relayerFee", type: "uint256" },
        { name: "nonce", type: "uint256" },
        { name: "deadline", type: "uint256" },
      ],
    };

    const value = {
      feeRecipient: feeRecipient,
      redemptionsHash: redemptionsHash,
      relayerFee: relayerFee,
      nonce: nonce,
      deadline: deadline,
    };

    return wallet.signTypedData(domain, types, value);
  }

  // Hardhat default account private keys (accounts #0 through #3)
  const HARDHAT_KEYS = [
    "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
    "0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
    "0x5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9a804cdab365a",
    "0x7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6",
  ];

  beforeEach(async function () {
    [deployer, initiator, participant, other] = await ethers.getSigners();

    // Create Wallet instances for raw signing (index 2 = participant, index 3 = other)
    participantWallet = new ethers.Wallet(HARDHAT_KEYS[2], ethers.provider);
    otherWallet = new ethers.Wallet(HARDHAT_KEYS[3], ethers.provider);

    // Deploy ETHSwap (no constructor args)
    const ETHSwapFactory = await ethers.getContractFactory("ETHSwap");
    ethSwap = await ETHSwapFactory.deploy();
    await ethSwap.waitForDeployment();
  });

  // ─── Core Swap Tests ─────────────────────────────────────────────────

  describe("initiate", function () {
    it("should initiate a single ETH swap", async function () {
      const { secretHash } = makeSecret();
      const refundTime = await futureTimestamp();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, refundTime);

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.emit(ethSwap, "Initiated");

      const s = await ethSwap.status(ZERO_ADDR, v);
      expect(s.step).to.equal(Step.Filled);
    });

    it("should initiate a batch of swaps", async function () {
      const s1 = makeSecret();
      const s2 = makeSecret();
      const refundTime = await futureTimestamp();
      const v1 = makeVector(s1.secretHash, ONE_ETH, initiator.address, participant.address, refundTime);
      const v2 = makeVector(s2.secretHash, HALF_ETH, initiator.address, other.address, refundTime);

      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v1, v2], {
        value: ONE_ETH + HALF_ETH,
      });

      expect((await ethSwap.status(ZERO_ADDR, v1)).step).to.equal(Step.Filled);
      expect((await ethSwap.status(ZERO_ADDR, v2)).step).to.equal(Step.Filled);
    });

    it("should reject zero value", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, 0n, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: 0n })
      ).to.be.revertedWith("zero value");
    });

    it("should reject zero initiator address", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, ZERO_ADDR, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("zero addr");
    });

    it("should reject zero participant address", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, ZERO_ADDR, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("zero addr");
    });

    it("should reject duplicate swap", async function () {
      const { secretHash } = makeSecret();
      const refundTime = await futureTimestamp();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, refundTime);

      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("already exists");
    });

    it("should reject expired refund timestamp", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await pastTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("bad refund time");
    });

    it("should reject msg.value mismatch", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: HALF_ETH })
      ).to.be.revertedWith("bad ETH value");
    });

    it("should reject batch > MAX_BATCH (20)", async function () {
      const vectors = [];
      let total = 0n;
      for (let i = 0; i < 21; i++) {
        const { secretHash } = makeSecret();
        vectors.push(makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp()));
        total += ONE_ETH;
      }

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, vectors, { value: total })
      ).to.be.revertedWith("bad batch size");
    });

    it("should reject empty batch", async function () {
      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [], { value: 0n })
      ).to.be.revertedWith("bad batch size");
    });

    it("should reject zero secretHash", async function () {
      const zeroHash = ethers.zeroPadValue("0x", 32);
      const v = makeVector(zeroHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("zero hash");
    });

    it("should reject REFUND_RECORD_HASH as secretHash", async function () {
      const REFUND_RECORD_HASH = "0xAF9613760F72635FBDB44A5A0A63C39F12AF30F950A6EE5C971BE188E89C4051";
      const v = makeVector(REFUND_RECORD_HASH, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("illegal hash");
    });

    it("should reject calls from contracts (senderIsOrigin)", async function () {
      const CallerFactory = await ethers.getContractFactory("TestCaller");
      const caller = await CallerFactory.deploy();
      await caller.waitForDeployment();

      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      const calldata = ethSwap.interface.encodeFunctionData("initiate", [ZERO_ADDR, [v]]);
      await expect(
        caller.callContract(await ethSwap.getAddress(), calldata, { value: ONE_ETH })
      ).to.be.revertedWith("sender != origin");
    });
  });

  // ─── Redeem Tests ────────────────────────────────────────────────────

  describe("redeem", function () {
    let secret, secretHash, vector;

    beforeEach(async function () {
      ({ secret, secretHash } = makeSecret());
      vector = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [vector], { value: ONE_ETH });
      await mineBlock();
    });

    it("should redeem a single swap with correct secret", async function () {
      const balBefore = await ethers.provider.getBalance(participant.address);
      const tx = await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }]);
      const receipt = await tx.wait();
      const gasUsed = receipt.gasUsed * receipt.gasPrice;
      const balAfter = await ethers.provider.getBalance(participant.address);

      expect(balAfter - balBefore + gasUsed).to.equal(ONE_ETH);

      const s = await ethSwap.status(ZERO_ADDR, vector);
      expect(s.step).to.equal(Step.Redeemed);
      expect(s.secret).to.equal(secret);
    });

    it("should redeem a batch of swaps", async function () {
      const s2 = makeSecret();
      const v2 = makeVector(s2.secretHash, HALF_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v2], { value: HALF_ETH });
      await mineBlock();

      await ethSwap.connect(participant).redeem(ZERO_ADDR, [
        { v: vector, secret },
        { v: v2, secret: s2.secret },
      ]);

      expect((await ethSwap.status(ZERO_ADDR, vector)).step).to.equal(Step.Redeemed);
      expect((await ethSwap.status(ZERO_ADDR, v2)).step).to.equal(Step.Redeemed);
    });

    it("should reject wrong secret", async function () {
      const wrongSecret = ethers.hexlify(ethers.randomBytes(32));
      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret: wrongSecret }])
      ).to.be.revertedWith("bad secret");
    });

    it("should reject wrong participant", async function () {
      await expect(
        ethSwap.connect(other).redeem(ZERO_ADDR, [{ v: vector, secret }])
      ).to.be.revertedWith("not participant");
    });

    it("should reject already-redeemed swap", async function () {
      await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }]);
      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }])
      ).to.be.revertedWith("not redeemable");
    });

    it("should reject already-refunded swap", async function () {
      await advanceTime(7200);
      await ethSwap.connect(initiator).refund(ZERO_ADDR, vector);
      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }])
      ).to.be.revertedWith("not redeemable");
    });

    it("should reject zero secret", async function () {
      const zeroSecret = ethers.zeroPadValue("0x", 32);
      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret: zeroSecret }])
      ).to.be.revertedWith("zero secret");
    });
  });

  // ─── Refund Tests ────────────────────────────────────────────────────

  describe("refund", function () {
    let secret, secretHash, vector;

    beforeEach(async function () {
      ({ secret, secretHash } = makeSecret());
      vector = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp(3600));
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [vector], { value: ONE_ETH });
      await mineBlock();
    });

    it("should refund after expiry", async function () {
      await advanceTime(7200);

      const balBefore = await ethers.provider.getBalance(initiator.address);
      const tx = await ethSwap.connect(initiator).refund(ZERO_ADDR, vector);
      const receipt = await tx.wait();
      const gasUsed = receipt.gasUsed * receipt.gasPrice;
      const balAfter = await ethers.provider.getBalance(initiator.address);

      expect(balAfter - balBefore + gasUsed).to.equal(ONE_ETH);
      expect((await ethSwap.status(ZERO_ADDR, vector)).step).to.equal(Step.Refunded);
    });

    it("should reject refund before expiry", async function () {
      await expect(
        ethSwap.connect(initiator).refund(ZERO_ADDR, vector)
      ).to.be.revertedWith("not expired");
    });

    it("should reject refund of already-redeemed swap", async function () {
      await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }]);
      await advanceTime(7200);
      await expect(
        ethSwap.connect(initiator).refund(ZERO_ADDR, vector)
      ).to.be.revertedWith("already redeemed");
    });

    it("should reject double refund", async function () {
      await advanceTime(7200);
      await ethSwap.connect(initiator).refund(ZERO_ADDR, vector);
      await expect(
        ethSwap.connect(initiator).refund(ZERO_ADDR, vector)
      ).to.be.revertedWith("already refunded");
    });

    it("should allow anyone to call refund", async function () {
      await advanceTime(7200);
      await expect(
        ethSwap.connect(other).refund(ZERO_ADDR, vector)
      ).to.emit(ethSwap, "Refunded");
      expect((await ethSwap.status(ZERO_ADDR, vector)).step).to.equal(Step.Refunded);
    });
  });

  // ─── Status / View Tests ─────────────────────────────────────────────

  describe("status", function () {
    it("should return Empty for uninitiated", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      const s = await ethSwap.status(ZERO_ADDR, v);
      expect(s.step).to.equal(Step.Empty);
    });

    it("should return Filled after initiate", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      const s = await ethSwap.status(ZERO_ADDR, v);
      expect(s.step).to.equal(Step.Filled);
      expect(s.blockNumber).to.be.gt(0n);
    });

    it("should return Redeemed after redeem (with secret)", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();
      await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v, secret }]);
      const s = await ethSwap.status(ZERO_ADDR, v);
      expect(s.step).to.equal(Step.Redeemed);
      expect(s.secret).to.equal(secret);
    });

    it("isRedeemable returns correct values for each state", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      expect(await ethSwap.isRedeemable(ZERO_ADDR, v)).to.equal(false);
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();
      expect(await ethSwap.isRedeemable(ZERO_ADDR, v)).to.equal(true);
      await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v, secret }]);
      expect(await ethSwap.isRedeemable(ZERO_ADDR, v)).to.equal(false);
    });
  });

  // ─── redeemWithSignature Tests ──────────────────────────────────────

  describe("redeemWithSignature", function () {
    let secret, secretHash, vector;

    beforeEach(async function () {
      ({ secret, secretHash } = makeSecret());
      vector = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [vector], { value: ONE_ETH });
      await mineBlock();
    });

    it("should redeem a single ETH swap with valid signature", async function () {
      const deadline = await futureTimestamp(600);
      const nonce = 0n;
      const relayerFee = ethers.parseEther("0.01");
      const redemptions = [{ v: vector, secret }];

      const sig = await signRedeem(participantWallet, other.address, redemptions, relayerFee, nonce, deadline);

      const participantBalBefore = await ethers.provider.getBalance(participant.address);
      const relayerBalBefore = await ethers.provider.getBalance(other.address);

      const tx = await ethSwap.connect(other).redeemWithSignature(
        redemptions, other.address, relayerFee, nonce, deadline, sig
      );
      const receipt = await tx.wait();
      const gasUsed = receipt.gasUsed * receipt.gasPrice;

      const participantBalAfter = await ethers.provider.getBalance(participant.address);
      const relayerBalAfter = await ethers.provider.getBalance(other.address);

      expect(participantBalAfter - participantBalBefore).to.equal(ONE_ETH - relayerFee);
      expect(relayerBalAfter - relayerBalBefore + gasUsed).to.equal(relayerFee);

      const s = await ethSwap.status(ZERO_ADDR, vector);
      expect(s.step).to.equal(Step.Redeemed);
      expect(s.secret).to.equal(secret);
    });

    it("should redeem a batch of ETH swaps with valid signature", async function () {
      const s2 = makeSecret();
      const v2 = makeVector(s2.secretHash, HALF_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v2], { value: HALF_ETH });
      await mineBlock();

      const deadline = await futureTimestamp(600);
      const nonce = 0n;
      const relayerFee = ethers.parseEther("0.02");
      const redemptions = [
        { v: vector, secret },
        { v: v2, secret: s2.secret },
      ];

      const sig = await signRedeem(participantWallet, other.address, redemptions, relayerFee, nonce, deadline);

      await ethSwap.connect(other).redeemWithSignature(
        redemptions, other.address, relayerFee, nonce, deadline, sig
      );

      expect((await ethSwap.status(ZERO_ADDR, vector)).step).to.equal(Step.Redeemed);
      expect((await ethSwap.status(ZERO_ADDR, v2)).step).to.equal(Step.Redeemed);
    });

    it("should reject wrong signer", async function () {
      const deadline = await futureTimestamp(600);
      const nonce = 0n;
      const relayerFee = 0n;
      const redemptions = [{ v: vector, secret }];

      // Sign with wrong wallet (other, not participant)
      const sig = await signRedeem(otherWallet, deployer.address, redemptions, relayerFee, nonce, deadline);

      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, deployer.address, relayerFee, nonce, deadline, sig
        )
      ).to.be.revertedWith("bad signature");
    });

    it("should reject expired deadline", async function () {
      const deadline = await pastTimestamp(60);
      const nonce = 0n;
      const relayerFee = 0n;
      const redemptions = [{ v: vector, secret }];

      const sig = await signRedeem(participantWallet, deployer.address, redemptions, relayerFee, nonce, deadline);

      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, deployer.address, relayerFee, nonce, deadline, sig
        )
      ).to.be.revertedWith("expired");
    });

    it("should reject wrong nonce", async function () {
      const deadline = await futureTimestamp(600);
      const wrongNonce = 5n;
      const relayerFee = 0n;
      const redemptions = [{ v: vector, secret }];

      const sig = await signRedeem(participantWallet, deployer.address, redemptions, relayerFee, wrongNonce, deadline);

      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, deployer.address, relayerFee, wrongNonce, deadline, sig
        )
      ).to.be.revertedWith("bad nonce");
    });

    it("should prevent replay (second attempt with same nonce fails)", async function () {
      const deadline = await futureTimestamp(600);
      const nonce = 0n;
      const relayerFee = 0n;
      const redemptions = [{ v: vector, secret }];

      const sig = await signRedeem(participantWallet, deployer.address, redemptions, relayerFee, nonce, deadline);

      await ethSwap.connect(deployer).redeemWithSignature(
        redemptions, deployer.address, relayerFee, nonce, deadline, sig
      );

      // Same signature + nonce should fail (nonce already incremented)
      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, deployer.address, relayerFee, nonce, deadline, sig
        )
      ).to.be.revertedWith("bad nonce");
    });

    it("nonce increments correctly", async function () {
      expect(await ethSwap.nonces(participant.address)).to.equal(0n);

      const deadline = await futureTimestamp(600);
      const redemptions = [{ v: vector, secret }];
      const sig = await signRedeem(participantWallet, ZERO_ADDR, redemptions, 0n, 0n, deadline);

      await ethSwap.connect(deployer).redeemWithSignature(
        redemptions, ZERO_ADDR, 0n, 0n, deadline, sig
      );

      expect(await ethSwap.nonces(participant.address)).to.equal(1n);
    });

    it("should pay relayerFee to feeRecipient, remainder to participant", async function () {
      const deadline = await futureTimestamp(600);
      const relayerFee = ethers.parseEther("0.1");
      const redemptions = [{ v: vector, secret }];

      const sig = await signRedeem(participantWallet, other.address, redemptions, relayerFee, 0n, deadline);

      const participantBal = await ethers.provider.getBalance(participant.address);
      const relayerBal = await ethers.provider.getBalance(other.address);

      const tx = await ethSwap.connect(other).redeemWithSignature(
        redemptions, other.address, relayerFee, 0n, deadline, sig
      );
      const receipt = await tx.wait();
      const gasUsed = receipt.gasUsed * receipt.gasPrice;

      const participantBalAfter = await ethers.provider.getBalance(participant.address);
      const relayerBalAfter = await ethers.provider.getBalance(other.address);

      expect(participantBalAfter - participantBal).to.equal(ONE_ETH - relayerFee);
      expect(relayerBalAfter - relayerBal + gasUsed).to.equal(relayerFee);
    });

    it("should reject relayerFee > total", async function () {
      const deadline = await futureTimestamp(600);
      const relayerFee = ethers.parseEther("2"); // > 1 ETH swap value
      const redemptions = [{ v: vector, secret }];

      const sig = await signRedeem(participantWallet, deployer.address, redemptions, relayerFee, 0n, deadline);

      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, deployer.address, relayerFee, 0n, deadline, sig
        )
      ).to.be.revertedWith("fee exceeds total");
    });

    it("relayerFee == 0: entire total goes to participant", async function () {
      const deadline = await futureTimestamp(600);
      const relayerFee = 0n;
      const redemptions = [{ v: vector, secret }];

      const sig = await signRedeem(participantWallet, ZERO_ADDR, redemptions, relayerFee, 0n, deadline);

      const participantBal = await ethers.provider.getBalance(participant.address);

      await ethSwap.connect(deployer).redeemWithSignature(
        redemptions, ZERO_ADDR, relayerFee, 0n, deadline, sig
      );

      const participantBalAfter = await ethers.provider.getBalance(participant.address);
      expect(participantBalAfter - participantBal).to.equal(ONE_ETH);
    });

    it("should reject feeRecipient != msg.sender (MEV protection)", async function () {
      const deadline = await futureTimestamp(600);
      const relayerFee = ethers.parseEther("0.05");
      const redemptions = [{ v: vector, secret }];

      // Sign with other.address as feeRecipient
      const sig = await signRedeem(participantWallet, other.address, redemptions, relayerFee, 0n, deadline);

      // Submit from deployer (msg.sender != feeRecipient) — should be rejected
      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, other.address, relayerFee, 0n, deadline, sig
        )
      ).to.be.revertedWith("feeRecipient must be msg.sender");
    });

    it("feeRecipient = address(0): fee goes to msg.sender", async function () {
      const deadline = await futureTimestamp(600);
      const relayerFee = ethers.parseEther("0.05");
      const redemptions = [{ v: vector, secret }];

      // Sign with zero address as feeRecipient
      const sig = await signRedeem(participantWallet, ZERO_ADDR, redemptions, relayerFee, 0n, deadline);

      const deployerBal = await ethers.provider.getBalance(deployer.address);
      const tx = await ethSwap.connect(deployer).redeemWithSignature(
        redemptions, ZERO_ADDR, relayerFee, 0n, deadline, sig
      );
      const receipt = await tx.wait();
      const gasUsed = receipt.gasUsed * receipt.gasPrice;

      // Swap redeems successfully
      expect((await ethSwap.status(ZERO_ADDR, vector)).step).to.equal(Step.Redeemed);

      // deployer (msg.sender) received the fee
      const deployerBalAfter = await ethers.provider.getBalance(deployer.address);
      expect(deployerBalAfter - deployerBal + gasUsed).to.equal(relayerFee);
    });

    it("should reject participant mismatch in batch", async function () {
      const s2 = makeSecret();
      // Second swap has different participant (other, not participant)
      const v2 = makeVector(s2.secretHash, HALF_ETH, initiator.address, other.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v2], { value: HALF_ETH });
      await mineBlock();

      const deadline = await futureTimestamp(600);
      const redemptions = [
        { v: vector, secret },
        { v: v2, secret: s2.secret },
      ];

      // Sign with participant wallet — v2.participant is 'other', not 'participant'
      const sig = await signRedeem(participantWallet, ZERO_ADDR, redemptions, 0n, 0n, deadline);

      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, ZERO_ADDR, 0n, 0n, deadline, sig
        )
      ).to.be.revertedWith("participant mismatch");
    });

    it("should reject empty redemptions array", async function () {
      const deadline = await futureTimestamp(600);
      // We can't sign with empty redemptions easily since participant = redemptions[0].v.participant
      // But the contract will revert with "bad batch size" before signature check
      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          [], other.address, 0n, 0n, deadline, "0x"
        )
      ).to.be.revertedWith("bad batch size");
    });

    it("should allow contracts as callers (no senderIsOrigin)", async function () {
      const CallerFactory = await ethers.getContractFactory("TestCaller");
      const caller = await CallerFactory.deploy();
      await caller.waitForDeployment();
      const callerAddr = await caller.getAddress();

      const deadline = await futureTimestamp(600);
      const redemptions = [{ v: vector, secret }];
      // Sign with caller contract address as feeRecipient (msg.sender will be the contract)
      const sig = await signRedeem(participantWallet, callerAddr, redemptions, 0n, 0n, deadline);

      const calldata = ethSwap.interface.encodeFunctionData("redeemWithSignature", [
        redemptions, callerAddr, 0n, 0n, deadline, sig,
      ]);

      // Should NOT revert with "sender != origin" — contracts can call this
      await caller.callContract(await ethSwap.getAddress(), calldata);

      expect((await ethSwap.status(ZERO_ADDR, vector)).step).to.equal(Step.Redeemed);
    });

    it("should emit Redeemed event", async function () {
      const deadline = await futureTimestamp(600);
      const redemptions = [{ v: vector, secret }];
      const sig = await signRedeem(participantWallet, ZERO_ADDR, redemptions, 0n, 0n, deadline);
      const expectedKey = contractKey(ZERO_ADDR, vector);

      await expect(
        ethSwap.connect(deployer).redeemWithSignature(
          redemptions, ZERO_ADDR, 0n, 0n, deadline, sig
        )
      )
        .to.emit(ethSwap, "Redeemed")
        .withArgs(expectedKey, ZERO_ADDR, participant.address, secret);
    });
  });

  // ─── ERC20 Token Swap Tests ────────────────────────────────────────

  describe("ERC20 token swaps", function () {
    let token, tokenAddr;

    beforeEach(async function () {
      const MockERC20Factory = await ethers.getContractFactory("MockERC20");
      token = await MockERC20Factory.connect(initiator).deploy();
      await token.waitForDeployment();
      tokenAddr = await token.getAddress();
    });

    it("should initiate a token swap", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await token.connect(initiator).approve(await ethSwap.getAddress(), ONE_ETH);
      await expect(
        ethSwap.connect(initiator).initiate(tokenAddr, [v])
      ).to.emit(ethSwap, "Initiated");

      const s = await ethSwap.status(tokenAddr, v);
      expect(s.step).to.equal(Step.Filled);
    });

    it("should redeem a token swap", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await token.connect(initiator).approve(await ethSwap.getAddress(), ONE_ETH);
      await ethSwap.connect(initiator).initiate(tokenAddr, [v]);
      await mineBlock();

      const balBefore = await token.balanceOf(participant.address);
      await ethSwap.connect(participant).redeem(tokenAddr, [{ v, secret }]);
      const balAfter = await token.balanceOf(participant.address);

      expect(balAfter - balBefore).to.equal(ONE_ETH);
      expect((await ethSwap.status(tokenAddr, v)).step).to.equal(Step.Redeemed);
    });

    it("should refund a token swap", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await token.connect(initiator).approve(await ethSwap.getAddress(), ONE_ETH);
      await ethSwap.connect(initiator).initiate(tokenAddr, [v]);
      await advanceTime(7200);

      const balBefore = await token.balanceOf(initiator.address);
      await ethSwap.connect(initiator).refund(tokenAddr, v);
      const balAfter = await token.balanceOf(initiator.address);

      expect(balAfter - balBefore).to.equal(ONE_ETH);
      expect((await ethSwap.status(tokenAddr, v)).step).to.equal(Step.Refunded);
    });

    it("should reject ETH sent with token swap", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await expect(
        ethSwap.connect(initiator).initiate(tokenAddr, [v], { value: ONE_ETH })
      ).to.be.revertedWith("no ETH for token swap");
    });
  });

  // ─── Pure Function Tests ────────────────────────────────────────────

  describe("contractKey and secretValidates", function () {
    it("contractKey matches off-chain computation", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      const onChainKey = await ethSwap.contractKey(ZERO_ADDR, v);
      const offChainKey = contractKey(ZERO_ADDR, v);
      expect(onChainKey).to.equal(offChainKey);
    });

    it("contractKey differs for different tokens", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      const keyETH = await ethSwap.contractKey(ZERO_ADDR, v);
      const keyToken = await ethSwap.contractKey(participant.address, v);
      expect(keyETH).to.not.equal(keyToken);
    });

    it("secretValidates returns true for matching secret", async function () {
      const { secret, secretHash } = makeSecret();
      expect(await ethSwap.secretValidates(secret, secretHash)).to.equal(true);
    });

    it("secretValidates returns false for non-matching secret", async function () {
      const { secretHash } = makeSecret();
      const wrongSecret = ethers.hexlify(ethers.randomBytes(32));
      expect(await ethSwap.secretValidates(wrongSecret, secretHash)).to.equal(false);
    });
  });

  // ─── REDEEM_TYPEHASH Tests ───────────────────────────────────────────

  describe("REDEEM_TYPEHASH", function () {
    it("should match expected EIP-712 type hash", async function () {
      const expected = ethers.keccak256(
        ethers.toUtf8Bytes("Redeem(address feeRecipient,bytes32 redemptionsHash,uint256 relayerFee,uint256 nonce,uint256 deadline)")
      );
      expect(await ethSwap.REDEEM_TYPEHASH()).to.equal(expected);
    });
  });
});
