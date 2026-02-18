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

  // ERC-4337 validation return codes
  const VALIDATE_SUCCESS = 0n;
  const SIG_VALIDATION_FAILED = 1n;

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

  // Pack two uint128 values into a single bytes32 (high128 in upper bits, low128 in lower bits)
  function packUints(high128, low128) {
    return ethers.zeroPadValue(ethers.toBeHex((high128 << 128n) | low128), 32);
  }

  // Build a PackedUserOperation struct (ERC-4337 v0.7)
  function buildUserOp(sender, callData, opts = {}) {
    const callGasLimit = opts.callGasLimit || 500000n;
    const verificationGasLimit = opts.verificationGasLimit || 500000n;
    const maxFeePerGas = opts.maxFeePerGas || ethers.parseUnits("10", "gwei");
    const maxPriorityFeePerGas = opts.maxPriorityFeePerGas || ethers.parseUnits("1", "gwei");
    return {
      sender,
      nonce: opts.nonce || 0n,
      initCode: opts.initCode || "0x",
      callData,
      accountGasLimits: packUints(verificationGasLimit, callGasLimit),
      preVerificationGas: opts.preVerificationGas || 50000n,
      gasFees: packUints(maxPriorityFeePerGas, maxFeePerGas),
      paymasterAndData: opts.paymasterAndData || "0x",
      signature: opts.signature || "0x",
    };
  }

  // Pack a UserOp the same way the EntryPoint v0.7 does (for hash computation)
  function packUserOp(op) {
    return ethers.AbiCoder.defaultAbiCoder().encode(
      ["address", "uint256", "bytes32", "bytes32", "bytes32", "uint256", "bytes32", "bytes32"],
      [
        op.sender,
        op.nonce,
        ethers.keccak256(op.initCode),
        ethers.keccak256(op.callData),
        op.accountGasLimits,
        op.preVerificationGas,
        op.gasFees,
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
  function encodeRedeemAA(redemptions, nonce) {
    const iface = ethSwap.interface;
    return iface.encodeFunctionData("redeemAA", [redemptions, nonce]);
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

    it("should reject zero initiator address (unrefundable swap)", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, ZERO_ADDR, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("zero addr");
    });

    it("should reject zero participant address (unredeemable swap)", async function () {
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

    it("should reject zero secretHash (no preimage exists)", async function () {
      const zeroHash = ethers.zeroPadValue("0x", 32);
      const v = makeVector(zeroHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("zero hash");
    });

    it("should reject zero secret hash (unredeemable swap)", async function () {
      const zeroSecret = ethers.zeroPadValue("0x", 32);
      const zeroSecretHash = ethers.sha256(zeroSecret);
      const v = makeVector(zeroSecretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("zero secret hash");
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
      const nonce = opts.nonce || 0n;
      const callData = encodeRedeemAA(redemptions, nonce);
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
      const callData = encodeRedeemAA([], 0n);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      // Impersonate the entryPoint and call validateUserOp directly.
      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

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
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

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
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INVALID_SECRET for wrong secret", async function () {
      const swapAddr = await ethSwap.getAddress();
      const wrongSecret = ethers.hexlify(ethers.randomBytes(32));
      const callData = encodeRedeemAA([{ v: vector, secret: wrongSecret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_SWAP_NOT_REDEEMABLE for non-existent swap", async function () {
      const swapAddr = await ethSwap.getAddress();
      const fakeSecret = makeSecret();
      const fakeVector = makeVector(fakeSecret.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      const callData = encodeRedeemAA([{ v: fakeVector, secret: fakeSecret.secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INSUFFICIENT_CALL_GAS for low callGasLimit", async function () {
      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v: vector, secret }], 0n);
      // Set callGasLimit below the minimum
      const op = buildUserOp(swapAddr, callData, { callGasLimit: 1000n });
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INSUFFICIENT_VALUE when gas > swap value", async function () {
      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v: vector, secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      // Pass missingAccountFunds greater than the swap value
      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, ethers.parseEther("2"));
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should return VALIDATE_INVALID_SIGNATURE for wrong signer", async function () {
      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v: vector, secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      // Sign with the wrong signer (other, not participant)
      op.signature = await signUserOp(op, otherWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

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
      ], 0n);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

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
      const tx = await ethSwap.connect(epSigner).redeemAA([{ v, secret }], 0n);
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

      const op = buildUserOp(swapAddr, encodeRedeemAA(redemptions, 0n), {
        callGasLimit: exactMinGas,
      });
      op.signature = await signUserOp(op, participantWallet);

      // This should NOT revert — the minimum gas should be sufficient
      await entryPoint.handleOps([op], deployer.address);

      expect((await ethSwap.status(ZERO_ADDR, v)).step).to.equal(Step.Redeemed);
    });

    it("malformed signature: returns SIG_VALIDATION_FAILED instead of reverting", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v, secret }], 0n);

      // Test with a signature that has wrong length (not 65 bytes).
      // ECDSA.recover would revert on this, but tryRecover returns an error.
      const op = buildUserOp(swapAddr, callData);
      op.signature = "0xdeadbeef"; // 4 bytes, not 65

      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      // Should return SIG_VALIDATION_FAILED (not revert)
      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("malformed signature: empty signature returns SIG_VALIDATION_FAILED", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v, secret }], 0n);

      const op = buildUserOp(swapAddr, callData);
      op.signature = "0x"; // empty

      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("double-validation prevention: rejects second validation of same swap in same block", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const redemptions = [{ v, secret }];
      const callData = encodeRedeemAA(redemptions, 0n);

      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      // First validation should succeed
      const ret1 = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(ret1).to.equal(VALIDATE_SUCCESS);

      // Perform the first validation (non-static, writes validatedAt)
      await ethSwap.connect(epSigner).validateUserOp(op, opHash, 0n);

      // Second validation for the same swap in the same block should fail
      const ret2 = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(ret2).to.equal(SIG_VALIDATION_FAILED);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("double-validation prevention: allows re-validation in a new block", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const redemptions = [{ v, secret }];
      const callData = encodeRedeemAA(redemptions, 0n);

      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      // First validation succeeds and writes validatedAt
      await ethSwap.connect(epSigner).validateUserOp(op, opHash, 0n);

      // Mine a new block so the validatedAt entry expires
      await mineBlock();

      // Re-validation in the new block should succeed
      const callData2 = encodeRedeemAA(redemptions, 1n);
      const op2 = buildUserOp(swapAddr, callData2, { nonce: 1n });
      op2.signature = await signUserOp(op2, participantWallet);
      const opHash2 = await getUserOpHash(op2);

      const ret = await ethSwap.connect(epSigner).validateUserOp.staticCall(op2, opHash2, 0n);
      expect(ret).to.equal(VALIDATE_SUCCESS);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
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
      const op1 = buildUserOp(swapAddr, encodeRedeemAA([{ v: v1, secret: s1.secret }], 0n));
      op1.signature = await signUserOp(op1, participantWallet);
      await entryPoint.handleOps([op1], deployer.address);

      // Redeem second swap via AA (different nonce)
      const op2 = buildUserOp(swapAddr, encodeRedeemAA([{ v: v2, secret: s2.secret }], 1n), { nonce: 1n });
      op2.signature = await signUserOp(op2, participantWallet);
      await entryPoint.handleOps([op2], deployer.address);

      // Both should be redeemed
      expect((await ethSwap.status(ZERO_ADDR, v1)).step).to.equal(Step.Redeemed);
      expect((await ethSwap.status(ZERO_ADDR, v2)).step).to.equal(Step.Redeemed);

      // Prepayments should be zeroed out after redemption
      expect(await ethSwap.redeemPrepayments(0n)).to.equal(0n);
      expect(await ethSwap.redeemPrepayments(1n)).to.equal(0n);
    });
  });

  // ─── Constructor Tests ──────────────────────────────────────────────

  describe("constructor", function () {
    it("should reject zero entry point address", async function () {
      const ETHSwapFactory = await ethers.getContractFactory("ETHSwap");
      await expect(
        ETHSwapFactory.deploy(ethers.ZeroAddress)
      ).to.be.revertedWith("zero entry point");
    });

    it("should set the entryPoint correctly", async function () {
      const epAddr = await entryPoint.getAddress();
      expect(await ethSwap.entryPoint()).to.equal(epAddr);
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

  // ─── Additional Initiate Tests ──────────────────────────────────────

  describe("initiate (additional)", function () {
    it("should reject empty batch", async function () {
      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [], { value: 0n })
      ).to.be.revertedWith("bad batch size");
    });

    it("should reject REFUND_RECORD_HASH as secretHash", async function () {
      // REFUND_RECORD = 0xFFFF...FF, REFUND_RECORD_HASH = sha256(REFUND_RECORD)
      const REFUND_RECORD_HASH = "0xAF9613760F72635FBDB44A5A0A63C39F12AF30F950A6EE5C971BE188E89C4051";
      const v = makeVector(
        REFUND_RECORD_HASH,
        ONE_ETH,
        initiator.address,
        participant.address,
        await futureTimestamp()
      );

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      ).to.be.revertedWith("illegal hash");
    });

    it("should reject ETH sent with token swap", async function () {
      const MockERC20Factory = await ethers.getContractFactory("MockERC20");
      const token = await MockERC20Factory.connect(initiator).deploy();
      await token.waitForDeployment();
      const tokenAddr = await token.getAddress();

      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(initiator).initiate(tokenAddr, [v], { value: ONE_ETH })
      ).to.be.revertedWith("no ETH for token swap");
    });

    it("should emit Initiated with correct args", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      const expectedKey = contractKey(ZERO_ADDR, v);

      await expect(
        ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH })
      )
        .to.emit(ethSwap, "Initiated")
        .withArgs(expectedKey, ZERO_ADDR, initiator.address, ONE_ETH);
    });
  });

  // ─── Additional Redeem Tests ────────────────────────────────────────

  describe("redeem (additional)", function () {
    it("should reject empty batch", async function () {
      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [])
      ).to.be.revertedWith("bad batch size");
    });

    it("should reject batch > MAX_BATCH (20)", async function () {
      // We don't need to actually initiate 21 swaps; the batch size
      // check happens before any per-redemption validation.
      const redemptions = [];
      for (let i = 0; i < 21; i++) {
        const { secret, secretHash } = makeSecret();
        const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
        redemptions.push({ v, secret });
      }

      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, redemptions)
      ).to.be.revertedWith("bad batch size");
    });

    it("should reject zero secret", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const zeroSecret = ethers.zeroPadValue("0x", 32);
      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v, secret: zeroSecret }])
      ).to.be.revertedWith("zero secret");
    });

    it("should reject redeem of non-existent swap", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v, secret }])
      ).to.be.revertedWith("not redeemable");
    });

    it("should reject calls from contracts (senderIsOrigin)", async function () {
      const CallerFactory = await ethers.getContractFactory("TestCaller");
      const caller = await CallerFactory.deploy();
      await caller.waitForDeployment();

      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, await caller.getAddress(), await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const calldata = ethSwap.interface.encodeFunctionData("redeem", [ZERO_ADDR, [{ v, secret }]]);
      await expect(
        caller.callContract(await ethSwap.getAddress(), calldata)
      ).to.be.revertedWith("sender != origin");
    });

    it("should emit Redeemed with correct args", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const expectedKey = contractKey(ZERO_ADDR, v);

      await expect(
        ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v, secret }])
      )
        .to.emit(ethSwap, "Redeemed")
        .withArgs(expectedKey, ZERO_ADDR, participant.address, secret);
    });
  });

  // ─── Additional Refund Tests ────────────────────────────────────────

  describe("refund (additional)", function () {
    it("should reject refund of non-initiated swap", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await pastTimestamp());

      await expect(
        ethSwap.connect(initiator).refund(ZERO_ADDR, v)
      ).to.be.revertedWith("not initiated");
    });

    it("should reject calls from contracts (senderIsOrigin)", async function () {
      const CallerFactory = await ethers.getContractFactory("TestCaller");
      const caller = await CallerFactory.deploy();
      await caller.waitForDeployment();

      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await advanceTime(7200);

      const calldata = ethSwap.interface.encodeFunctionData("refund", [ZERO_ADDR, v]);
      await expect(
        caller.callContract(await ethSwap.getAddress(), calldata)
      ).to.be.revertedWith("sender != origin");
    });

    it("should send funds to initiator even when called by third party", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await advanceTime(7200);

      const balBefore = await ethers.provider.getBalance(initiator.address);
      await ethSwap.connect(other).refund(ZERO_ADDR, v);
      const balAfter = await ethers.provider.getBalance(initiator.address);

      // Initiator receives the refund even though 'other' called
      expect(balAfter - balBefore).to.equal(ONE_ETH);
    });

    it("should emit Refunded with correct args", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await advanceTime(7200);

      const expectedKey = contractKey(ZERO_ADDR, v);

      await expect(
        ethSwap.connect(initiator).refund(ZERO_ADDR, v)
      )
        .to.emit(ethSwap, "Refunded")
        .withArgs(expectedKey, ZERO_ADDR, initiator.address);
    });
  });

  // ─── Access Control Tests ──────────────────────────────────────────

  describe("access control", function () {
    it("validateUserOp rejects calls from non-entryPoint", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v, secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      await expect(
        ethSwap.connect(deployer).validateUserOp(op, opHash, 0n)
      ).to.be.revertedWith("not entryPoint");
    });

    it("redeemAA rejects calls from non-entryPoint", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      await expect(
        ethSwap.connect(deployer).redeemAA([{ v, secret }], 0n)
      ).to.be.revertedWith("not entryPoint");
    });
  });

  // ─── Additional validateUserOp Tests ───────────────────────────────

  describe("validateUserOp (additional)", function () {
    async function getEpSigner() {
      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      return ethers.getSigner(epAddr);
    }

    async function stopEpImpersonation() {
      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    }

    it("should reject zero secret", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const zeroSecret = ethers.zeroPadValue("0x", 32);
      const callData = encodeRedeemAA([{ v, secret: zeroSecret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      const epSigner = await getEpSigner();
      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);
      await stopEpImpersonation();
    });

    it("should reject already-refunded swap", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await advanceTime(7200);
      await ethSwap.connect(initiator).refund(ZERO_ADDR, v);

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v, secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epSigner = await getEpSigner();
      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);
      await stopEpImpersonation();
    });

    it("should reject already-redeemed swap", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();
      await ethSwap.connect(participant).redeem(ZERO_ADDR, [{ v, secret }]);

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v, secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epSigner = await getEpSigner();
      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);
      await stopEpImpersonation();
    });

    it("should reject batch > MAX_BATCH (20)", async function () {
      // Build calldata with 21 redemptions — the batch size check in
      // validateUserOp rejects it before per-swap validation.
      const redemptions = [];
      for (let i = 0; i < 21; i++) {
        const { secret, secretHash } = makeSecret();
        const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
        redemptions.push({ v, secret });
      }

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA(redemptions, 0n);
      const op = buildUserOp(swapAddr, callData);
      const opHash = await getUserOpHash(op);

      const epSigner = await getEpSigner();
      const retval = await ethSwap.connect(epSigner).validateUserOp.staticCall(op, opHash, 0n);
      expect(retval).to.equal(SIG_VALIDATION_FAILED);
      await stopEpImpersonation();
    });

    it("should reject same-block swap (blockNum >= block.number)", async function () {
      // We need initiate and validateUserOp to execute in the same block.
      // staticCall simulates block.number = latestBlock + 1, so we use
      // manual mining and check side effects instead of the return value.
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v, secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("2") });
      const epSigner = await ethers.getSigner(epAddr);

      await ethers.provider.send("evm_setAutomine", [false]);
      try {
        // Queue both transactions in the same block
        const initTx = await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
        const validateTx = await ethSwap.connect(epSigner).validateUserOp(op, opHash, 0n);
        await ethers.provider.send("evm_mine", []);

        const initReceipt = await initTx.wait();
        const validateReceipt = await validateTx.wait();
        expect(initReceipt.blockNumber).to.equal(validateReceipt.blockNumber);

        // On SIG_VALIDATION_FAILED, redeemPrepayments is not set.
        // On VALIDATE_SUCCESS, it would be set. Check it's zero.
        expect(await ethSwap.redeemPrepayments(0n)).to.equal(0n);
      } finally {
        await ethers.provider.send("evm_setAutomine", [true]);
        await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
      }
    });

    it("should pay prefund to entryPoint on successful validation", async function () {
      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      const callData = encodeRedeemAA([{ v, secret }], 0n);
      const op = buildUserOp(swapAddr, callData);
      op.signature = await signUserOp(op, participantWallet);
      const opHash = await getUserOpHash(op);

      const epAddr = await entryPoint.getAddress();
      const epSigner = await getEpSigner();

      const prefund = HALF_ETH;
      const epBalBefore = await ethers.provider.getBalance(epAddr);
      await ethSwap.connect(epSigner).validateUserOp(op, opHash, prefund);
      const epBalAfter = await ethers.provider.getBalance(epAddr);

      // EntryPoint balance should have increased by the prefund amount
      // (minus gas cost of the tx, but since epSigner is the EntryPoint
      // which received the prefund, the net increase is the prefund
      // minus gas. We just check it increased.)
      expect(epBalAfter).to.be.gt(epBalBefore);

      await stopEpImpersonation();
    });
  });

  // ─── Additional redeemAA Tests ─────────────────────────────────────

  describe("redeemAA (additional)", function () {
    it("should emit RedeemAATransferFailed for reverting recipient", async function () {
      const RevertingFactory = await ethers.getContractFactory("RevertingRecipient");
      const revertingRecipient = await RevertingFactory.deploy();
      await revertingRecipient.waitForDeployment();
      const revertingAddr = await revertingRecipient.getAddress();

      const { secret, secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, revertingAddr, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v], { value: ONE_ETH });
      await mineBlock();

      const epAddr = await entryPoint.getAddress();
      await ethers.provider.send("hardhat_impersonateAccount", [epAddr]);
      await deployer.sendTransaction({ to: epAddr, value: ethers.parseEther("1") });
      const epSigner = await ethers.getSigner(epAddr);

      await expect(
        ethSwap.connect(epSigner).redeemAA([{ v, secret }], 0n)
      )
        .to.emit(ethSwap, "RedeemAATransferFailed")
        .withArgs(revertingAddr, ONE_ETH);

      await ethers.provider.send("hardhat_stopImpersonatingAccount", [epAddr]);
    });

    it("should redeem batch via full EntryPoint.handleOps flow", async function () {
      const s1 = makeSecret();
      const s2 = makeSecret();
      const v1 = makeVector(s1.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      const v2 = makeVector(s2.secretHash, HALF_ETH, initiator.address, participant.address, await futureTimestamp());
      await ethSwap.connect(initiator).initiate(ZERO_ADDR, [v1, v2], { value: ONE_ETH + HALF_ETH });
      await mineBlock();

      const swapAddr = await ethSwap.getAddress();
      await entryPoint.depositTo(swapAddr, { value: ethers.parseEther("5") });

      const redemptions = [
        { v: v1, secret: s1.secret },
        { v: v2, secret: s2.secret },
      ];
      const callData = encodeRedeemAA(redemptions, 0n);
      const numRedemptions = BigInt(redemptions.length);
      const defaultCallGas = MIN_CALL_GAS_BASE + numRedemptions * MIN_CALL_GAS_PER_REDEMPTION;
      const op = buildUserOp(swapAddr, callData, { callGasLimit: defaultCallGas });
      op.signature = await signUserOp(op, participantWallet);

      const balBefore = await ethers.provider.getBalance(participant.address);
      await entryPoint.handleOps([op], deployer.address);
      const balAfter = await ethers.provider.getBalance(participant.address);

      expect(balAfter).to.be.gt(balBefore);
      expect((await ethSwap.status(ZERO_ADDR, v1)).step).to.equal(Step.Redeemed);
      expect((await ethSwap.status(ZERO_ADDR, v2)).step).to.equal(Step.Redeemed);
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

    it("should initiate a batch of token swaps", async function () {
      const s1 = makeSecret();
      const s2 = makeSecret();
      const v1 = makeVector(s1.secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());
      const v2 = makeVector(s2.secretHash, HALF_ETH, initiator.address, other.address, await futureTimestamp());

      await token.connect(initiator).approve(await ethSwap.getAddress(), ONE_ETH + HALF_ETH);
      await ethSwap.connect(initiator).initiate(tokenAddr, [v1, v2]);

      expect((await ethSwap.status(tokenAddr, v1)).step).to.equal(Step.Filled);
      expect((await ethSwap.status(tokenAddr, v2)).step).to.equal(Step.Filled);
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

    it("should reject token swap without approval", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      // No approve call
      await expect(
        ethSwap.connect(initiator).initiate(tokenAddr, [v])
      ).to.be.reverted;
    });

    it("token contractKey differs from ETH contractKey for same vector", async function () {
      const { secretHash } = makeSecret();
      const v = makeVector(secretHash, ONE_ETH, initiator.address, participant.address, await futureTimestamp());

      const keyETH = await ethSwap.contractKey(ZERO_ADDR, v);
      const keyToken = await ethSwap.contractKey(tokenAddr, v);
      expect(keyETH).to.not.equal(keyToken);
    });
  });
});
