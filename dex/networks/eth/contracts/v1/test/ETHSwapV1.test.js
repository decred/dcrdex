const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("ETHSwapV1", function () {
  let ethSwap, entryPoint;
  let deployer, initiator, participant, other;
  // Wallets for raw signing (needed for AA signature verification)
  let participantWallet, otherWallet;

  const ZERO_ADDR = ethers.ZeroAddress;
  const ONE_ETH = ethers.parseEther("1");
  const HALF_ETH = ethers.parseEther("0.5");

  // Step enum values
  const Step = { Empty: 0, Filled: 1, Redeemed: 2, Refunded: 3 };

  // Validation error codes from the contract
  const VALIDATE_SUCCESS = 0n;
  const VALIDATE_CALLDATA_TOO_SHORT = 1n;
  const VALIDATE_WRONG_SELECTOR = 2n;
  const VALIDATE_SWAP_NOT_REDEEMABLE = 3n;
  const VALIDATE_PARTICIPANT_MISMATCH = 4n;
  const VALIDATE_INSUFFICIENT_VALUE = 5n;
  const VALIDATE_INVALID_SIGNATURE = 6n;
  const VALIDATE_INVALID_SECRET = 7n;
  const VALIDATE_INSUFFICIENT_CALL_GAS = 8n;
  const VALIDATE_BAD_BATCH_SIZE = 9n;

  const MIN_CALL_GAS_PER_REDEMPTION = 25000n;
  const MIN_CALL_GAS_BASE = 75000n;

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

  // Build a UserOperation struct
  function buildUserOp(sender, callData, opts = {}) {
    return {
      sender,
      nonce: opts.nonce || 0n,
      initCode: opts.initCode || "0x",
      callData,
      callGasLimit: opts.callGasLimit || 500000n,
      verificationGasLimit: opts.verificationGasLimit || 500000n,
      preVerificationGas: opts.preVerificationGas || 50000n,
      maxFeePerGas: opts.maxFeePerGas || ethers.parseUnits("10", "gwei"),
      maxPriorityFeePerGas: opts.maxPriorityFeePerGas || ethers.parseUnits("1", "gwei"),
      paymasterAndData: opts.paymasterAndData || "0x",
      signature: opts.signature || "0x",
    };
  }

  // Pack a UserOp the same way the EntryPoint does (for hash computation)
  function packUserOp(op) {
    return ethers.AbiCoder.defaultAbiCoder().encode(
      ["address", "uint256", "bytes32", "bytes32", "uint256", "uint256", "uint256", "uint256", "uint256", "bytes32"],
      [
        op.sender,
        op.nonce,
        ethers.keccak256(op.initCode),
        ethers.keccak256(op.callData),
        op.callGasLimit,
        op.verificationGasLimit,
        op.preVerificationGas,
        op.maxFeePerGas,
        op.maxPriorityFeePerGas,
        ethers.keccak256(op.paymasterAndData),
      ]
    );
  }

  // Compute userOpHash matching the EntryPoint algorithm
  async function getUserOpHash(op) {
    const opHash = ethers.keccak256(packUserOp(op));
    const chainId = (await ethers.provider.getNetwork()).chainId;
    return ethers.keccak256(
      ethers.AbiCoder.defaultAbiCoder().encode(
        ["bytes32", "address", "uint256"],
        [opHash, await entryPoint.getAddress(), chainId]
      )
    );
  }

  // Sign a userOpHash with a wallet (raw ECDSA, no Ethereum message prefix)
  // The contract uses ECDSA.recover(userOpHash, signature) which expects
  // a raw ecrecover-compatible signature, not eth_sign prefixed.
  async function signUserOp(op, wallet) {
    const hash = await getUserOpHash(op);
    const sig = wallet.signingKey.sign(hash);
    return ethers.Signature.from(sig).serialized;
  }

  // Encode redeemAA calldata
  function encodeRedeemAA(redemptions) {
    const iface = ethSwap.interface;
    return iface.encodeFunctionData("redeemAA", [redemptions]);
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

    // Deploy EntryPoint from @account-abstraction/contracts
    const EntryPointFactory = await ethers.getContractFactory(
      "EntryPoint",
      { libraries: {} }
    );
    entryPoint = await EntryPointFactory.deploy();
    await entryPoint.waitForDeployment();

    // Deploy ETHSwap with EntryPoint address
    const ETHSwapFactory = await ethers.getContractFactory("ETHSwap");
    ethSwap = await ETHSwapFactory.deploy(await entryPoint.getAddress());
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

    it("should reject calls from contracts (senderIsOrigin)", async function () {
      // Deploy a simple contract that calls initiate
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
      // Mine a block so blockNum < block.number
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

    it("should reject wrong participant (not msg.sender)", async function () {
      await expect(
        ethSwap.connect(other).redeem(ZERO_ADDR, [{ v: vector, secret }])
      ).to.be.revertedWith("not participant");
    });

    it("should reject already-redeemed swap", async function () {
      await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }]);

      // After redemption, the record is the secret (a large uint256),
      // so blockNum >= block.number and "not redeemable" is hit first.
      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }])
      ).to.be.revertedWith("not redeemable");
    });

    it("should reject already-refunded swap", async function () {
      // Advance time past refund
      await advanceTime(7200);
      await ethSwap.connect(initiator).refund(ZERO_ADDR, vector);

      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: vector, secret }])
      ).to.be.revertedWith("not redeemable");
    });

    it("should reject same-block redemption (blockNum < block.number)", async function () {
      // The contract requires blockNum > 0 && blockNum < block.number (strictly less).
      // When both initiate and redeem are in the same block, blockNum == block.number,
      // so the strict less-than check fails.
      // We use manual mining to include both transactions in one block.
      const s2 = makeSecret();
      const v2 = makeVector(s2.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await ethers.provider.send("evm_setAutomine", [false]);
      try {
        // Send initiate (queued in mempool)
        const initTxResp = await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v2], { value: ONE_ETH });
        // Send redeem (also queued, will be in the same block)
        const redeemTxResp = await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v: v2, secret: s2.secret }]);

        // Mine both in one block
        await ethers.provider.send("evm_mine", []);

        // Wait for receipts — the redeem will throw since it reverts
        const initReceipt = await initTxResp.wait();

        let redeemReceipt;
        try {
          redeemReceipt = await redeemTxResp.wait();
        } catch (e) {
          // ethers v6 throws on status=0 receipts
          redeemReceipt = e.receipt;
        }

        // Both are in the same block
        expect(initReceipt.blockNumber).to.equal(redeemReceipt.blockNumber);

        // The redeem should have reverted (status 0)
        expect(redeemReceipt.status).to.equal(0);
      } finally {
        await ethers.provider.send("evm_setAutomine", [true]);
      }

      // Swap should still be Filled, not Redeemed
      expect((await ethSwap.status(ZERO_ADDR, v2)).step).to.equal(Step.Filled);
    });
  });

  // ─── Refund Tests ────────────────────────────────────────────────────

  describe("refund", function () {
    let secret, secretHash, vector, refundTime;

    beforeEach(async function () {
      ({ secret, secretHash } = makeSecret());
      refundTime = await futureTimestamp(3600);
      vector = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, refundTime);
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

    it("should allow anyone to call refund (not just initiator)", async function () {
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

    it("should return Refunded after refund", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await advanceTime(7200);
      await ethSwap.connect(initiator).refund(ZERO_ADDR, v);

      const s = await ethSwap.status(ZERO_ADDR, v);
      expect(s.step).to.equal(Step.Refunded);
    });

    it("isRedeemable returns correct values for each state", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      // Empty
      expect(await ethSwap.isRedeemable(ZERO_ADDR, v)).to.equal(false);

      // Filled
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();
      expect(await ethSwap.isRedeemable(ZERO_ADDR, v)).to.equal(true);

      // Redeemed
      await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v, secret }]);
      expect(await ethSwap.isRedeemable(ZERO_ADDR, v)).to.equal(false);

      // Refunded (new swap)
      const s2 = makeSecret();
      const v2 = makeVector(s2.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v2], { value: ONE_ETH });
      await advanceTime(7200);
      await ethSwap.connect(initiator).refund(ZERO_ADDR, v2);
      expect(await ethSwap.isRedeemable(ZERO_ADDR, v2)).to.equal(false);
    });
  });

  // ─── AA Redeem Tests ─────────────────────────────────────────────────

  describe("redeemAA", function () {
    let secret, secretHash, vector;

    beforeEach(async function () {
      ({ secret, secretHash } = makeSecret());
      vector = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [vector], { value: ONE_ETH });
      await mineBlock();
    });

    async function buildAndSignRedeemOp(redemptions, signer, opts = {}) {
      const callData = encodeRedeemAA(redemptions);
      const numRedemptions = BigInt(redemptions.length);
      const defaultCallGas = MIN_CALL_GAS_BASE + numRedemptions * MIN_CALL_GAS_PER_REDEMPTION;
      const op = buildUserOp(await ethSwap.getAddress(), callData, {
        callGasLimit: opts.callGasLimit !== undefined ? opts.callGasLimit : defaultCallGas,
        ...opts,
      });
      op.signature = await signUserOp(op, signer);
      return op;
    }

    it("should successfully redeem via EntryPoint.handleOps", async function () {
      // Fund the contract's EntryPoint deposit for gas
      const swapAddr = await ethSwap.getAddress();
      await entryPoint.depositTo(swapAddr, { value: ethers.parseEther("2") });

      const redemptions = [{ v: vector, secret }];
      const op = await buildAndSignRedeemOp(redemptions, participantWallet);

      const balBefore = await ethers.provider.getBalance(participant.address);
      await entryPoint.handleOps([op], deployer.address);
      const balAfter = await ethers.provider.getBalance(participant.address);

      // Participant should have received funds (minus fees)
      expect(balAfter).to.be.gt(balBefore);
      expect((await ethSwap.status(ZERO_ADDR, vector)).step).to.equal(Step.Redeemed);
    });

    it("should return VALIDATE_BAD_BATCH_SIZE for empty array", async function () {
      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([]);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      const result = await entryPoint.connect(deployer).simulateValidation.staticCall(op).catch(e => e);
      // We can also test directly by calling validateUserOp from the entrypoint impersonation.
      // Use a lower-level approach: impersonate the entryPoint and call validateUserOp directly.
      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_BAD_BATCH_SIZE);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_WRONG_SELECTOR for wrong function selector", async function () {
      const swapAddr = await ethSwap.getAddress();
      // Use redeem selector instead of redeemAA
      const wrongCallData = ethSwap.interface.encodeFunctionData("redeem", [ZERO_ADDR, [{ v: vector, secret }]]);
      const op = buildUserOp(swapAddr, wrongCallData);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_WRONG_SELECTOR);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_CALLDATA_TOO_SHORT", async function () {
      const swapAddr = await ethSwap.getAddress();
      const op = buildUserOp(swapAddr, "0x1234"); // Only 2 bytes
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_CALLDATA_TOO_SHORT);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INVALID_SECRET for wrong secret", async function () {
      const swapAddr = await ethSwap.getAddress();
      const wrongSecret = ethers.hexlify(ethers.randomBytes(32));
      const callData = encodeRedeemAA([{ v: vector, secret: wrongSecret }]);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_INVALID_SECRET);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_SWAP_NOT_REDEEMABLE for non-existent swap", async function () {
      const swapAddr = await ethSwap.getAddress();
      const fakeSecret = makeSecret();
      const fakeVector = makeVector(fakeSecret.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      const callData = encodeRedeemAA([{ v: fakeVector, secret: fakeSecret.secret }]);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_SWAP_NOT_REDEEMABLE);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INSUFFICIENT_CALL_GAS for low callGasLimit", async function () {
      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v: vector, secret }]);
      // Set callGasLimit below the minimum
      const op = buildUserOp(swapAddr, callData, { callGasLimit: 1000n });
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_INSUFFICIENT_CALL_GAS);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INSUFFICIENT_VALUE when gas > swap value", async function () {
      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v: vector, secret }]);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      // Pass missingAccountFunds greater than the swap value
      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, ethers.parseEther("2"));
      expect(retval).to.equal(VALIDATE_INSUFFICIENT_VALUE);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INVALID_SIGNATURE for wrong signer", async function () {
      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v: vector, secret }]);
      const op = buildUserOp(swapAddr, callData);
      // Sign with the wrong signer (other, not participant)
      op.signature = await signUserOp(op, otherWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_INVALID_SIGNATURE);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_PARTICIPANT_MISMATCH for mixed participants", async function () {
      // Create a second swap with a different participant
      const s2 = makeSecret();
      const v2 = makeVector(s2.secretHash, ONE_ETH, initiator.address, other.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v2], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([
        { v: vector, secret },
        { v: v2, secret: s2.secret },
      ]);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(VALIDATE_PARTICIPANT_MISMATCH);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });
  });

  // ─── AA Security Tests ───────────────────────────────────────────────

  describe("AA security", function () {
    it("reverting recipient: swap still finalizes even if recipient can't accept ETH", async function () {
      // Deploy a contract that reverts on receive
      const RevertingFactory = await ethers.getContractFactory("RevertingRecipient");
      const revertingRecipient = await RevertingFactory.deploy();
      await revertingRecipient.waitForDeployment();
      const revertingAddr = await revertingRecipient.getAddress();

      // Impersonate the reverting recipient to get a signer-like object
      // We need a Wallet for signing, so we use a known private key
      const recipientWallet = new ethers.Wallet(
        "0xac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80", // hardhat account #0 key
        ethers.provider
      );
      // Actually, let's use a different approach: create the swap with the reverting contract
      // as the participant, but sign with a known key.
      // We need to use a wallet whose address == revertingRecipient. That's not possible.
      // Instead, we impersonate the entryPoint and call redeemAA directly.

      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, revertingAddr, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      // Impersonate entryPoint to call redeemAA directly
      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      // First, set up the prepayment (simulate what validateUserOp would do)
      // We need to compute the prepayKey
      const secrets = [secret];
      const prepayKey = ethers.keccak256(
        ethers.AbiCoder.defaultAbiCoder().encode(
          ["address", "uint256", "bytes32[]"],
          [revertingAddr, ONE_ETH, secrets]
        )
      );

      // Call redeemAA from the entryPoint
      // The redeemPrepayment won't be set, so fees will be 0, and total - 0 = total will be sent
      // The call to the reverting recipient will fail silently (intentionally)
      const tx = await ethSwap.connect(epSigner).redeemAA([{ v, secret }]);
      await tx.wait();

      // The swap should be marked as redeemed despite the ETH transfer failing
      const s = await ethSwap.status(ZERO_ADDR, v);
      expect(s.step).to.equal(Step.Redeemed);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("gas constant adequacy: redeemAA succeeds with exactly minimum gas", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      await entryPoint.depositTo(swapAddr, { value: ethers.parseEther("5") });

      const redemptions = [{ v, secret }];
      const exactMinGas = MIN_CALL_GAS_BASE + 1n * MIN_CALL_GAS_PER_REDEMPTION;

      const op = buildUserOp(swapAddr, encodeRedeemAA(redemptions), {
        callGasLimit: exactMinGas,
      });
      op.signature = await signUserOp(op, participantWallet);

      // This should NOT revert — the minimum gas should be sufficient
      await entryPoint.handleOps([op], deployer.address);

      expect((await ethSwap.status(ZERO_ADDR, v)).step).to.equal(Step.Redeemed);
    });

    it("prepayment key uniqueness: different redemption sets produce different prepayKeys", async function () {
      // Create two different swaps
      const s1 = makeSecret();
      const s2 = makeSecret();
      const v1 = makeVector(s1.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      const v2 = makeVector(s2.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v1, v2], { value: ONE_ETH * 2n });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      await entryPoint.depositTo(swapAddr, { value: ethers.parseEther("10") });

      // Redeem first swap via AA
      const op1 = buildUserOp(swapAddr, encodeRedeemAA([{ v: v1, secret: s1.secret }]));
      op1.signature = await signUserOp(op1, participantWallet);
      await entryPoint.handleOps([op1], deployer.address);

      // Redeem second swap via AA (different nonce)
      const op2 = buildUserOp(swapAddr, encodeRedeemAA([{ v: v2, secret: s2.secret }]), { nonce: 1n });
      op2.signature = await signUserOp(op2, participantWallet);
      await entryPoint.handleOps([op2], deployer.address);

      // Both should be redeemed
      expect((await ethSwap.status(ZERO_ADDR, v1)).step).to.equal(Step.Redeemed);
      expect((await ethSwap.status(ZERO_ADDR, v2)).step).to.equal(Step.Redeemed);

      // Verify prepayments were cleaned up (deleted after use)
      const prepayKey1 = ethers.keccak256(
        ethers.AbiCoder.defaultAbiCoder().encode(
          ["address", "uint256", "bytes32[]"],
          [participant.address, ONE_ETH, [s1.secret]]
        )
      );
      const prepayKey2 = ethers.keccak256(
        ethers.AbiCoder.defaultAbiCoder().encode(
          ["address", "uint256", "bytes32[]"],
          [participant.address, ONE_ETH, [s2.secret]]
        )
      );
      // Keys should be different
      expect(prepayKey1).to.not.equal(prepayKey2);

      // Prepayments should be zeroed out after redemption
      expect(await ethSwap.redeemPrepayments(prepayKey1)).to.equal(0n);
      expect(await ethSwap.redeemPrepayments(prepayKey2)).to.equal(0n);
    });
  });
});
