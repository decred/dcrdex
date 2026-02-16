// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.18;

import "@account-abstraction/contracts/interfaces/IAccount.sol";
import "@account-abstraction/contracts/core/EntryPoint.sol";
import "@openzeppelin/contracts/security/ReentrancyGuard.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";
import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";

/// @title ETHSwap - Atomic swap contract for DCRDEX
/// @author Decred developers
/// @notice Creates a contract to be deployed on an Ethereum network for atomic swaps.
/// After deployment, it keeps a record of the state of swaps and enables
/// redemption and refund when conditions are met.
/// @dev ETHSwap accomplishes this by holding funds sent to ETHSwap until certain
/// conditions are met. An initiator sends a tx with the Vector(s) to fund and
/// the requisite value to transfer to ETHSwap. At this point the funds belong
/// to the contract, and cannot be accessed by anyone else, not even the
/// contract's deployer. The swap Vector specifies the conditions necessary for
/// refund and redeem.
///
/// ETHSwap has no limits on gas used for any transactions.
///
/// ETHSwap cannot be used by other contracts or by a third party mediating
/// the swap or multisig wallets (except via ERC-4337 account abstraction).
///
/// TOKEN COMPATIBILITY: This contract is NOT compatible with fee-on-transfer
/// tokens or rebasing tokens. Using such tokens will result in accounting
/// mismatches and potential loss of funds. Only use standard ERC20 tokens
/// where transfer(amount) results in exactly amount being received.
///
/// This code should be verifiable as resulting in a certain on-chain contract
/// by compiling with the correct version of solidity and comparing the
/// resulting byte code to the data in the original transaction.
contract ETHSwap is IAccount, ReentrancyGuard {
    using SafeERC20 for IERC20;

    uint256 public constant MAX_BATCH = 20;

    // Minimum gas required per redemption in redeemAA to prevent out-of-gas attacks.
    // This accounts for: storage reads, storage writes, ETH transfer, and loop overhead.
    // Includes buffer for potential EVM gas repricing.
    uint256 internal constant MIN_CALL_GAS_PER_REDEMPTION = 25000;
    // Base gas for redeemAA regardless of redemption count (function overhead, final transfer).
    uint256 internal constant MIN_CALL_GAS_BASE = 75000;

    // validateUserOp return codes for ERC-4337 account abstraction.
    uint256 internal constant VALIDATE_SUCCESS = 0;
    uint256 internal constant VALIDATE_CALLDATA_TOO_SHORT = 1;
    uint256 internal constant VALIDATE_WRONG_SELECTOR = 2;
    uint256 internal constant VALIDATE_SWAP_NOT_REDEEMABLE = 3;
    uint256 internal constant VALIDATE_PARTICIPANT_MISMATCH = 4;
    uint256 internal constant VALIDATE_INSUFFICIENT_VALUE = 5;
    uint256 internal constant VALIDATE_INVALID_SIGNATURE = 6;
    uint256 internal constant VALIDATE_INVALID_SECRET = 7;
    uint256 internal constant VALIDATE_INSUFFICIENT_CALL_GAS = 8;
    uint256 internal constant VALIDATE_BAD_BATCH_SIZE = 9;

    // Step is a type that hold's a contract's current step. Empty is the
    // uninitiated or null value.
    enum Step { Empty, Filled, Redeemed, Refunded }

    struct Status {
        Step step;
        bytes32 secret;
        uint256 blockNumber;
    }

    bytes32 internal constant REFUND_RECORD =
        0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF;

    bytes32 internal constant REFUND_RECORD_HASH =
        0xAF9613760F72635FBDB44A5A0A63C39F12AF30F950A6EE5C971BE188E89C4051;

    address payable public immutable entryPoint;

    // swaps is a map of contract hashes to the "swap record". The swap record
    // has the following interpretation.
    //   if (record == bytes32(0x00)): contract is uninitiated
    //   else if (uint256(record) < block.number && sha256(record) != contract.secretHash):
    //      contract is initiated and redeemable by the participant with the secret.
    //   else if (sha256(record) == contract.secretHash): contract has been redeemed
    //   else if (record == REFUND_RECORD): contract has been refunded
    //   else: invalid record. Should be impossible by construction
    mapping(bytes32 => bytes32) public swaps;

    // redeemPrepayments tracks gas prepayments for ERC-4337 redemptions.
    // Keyed by hash of secrets to ensure uniqueness per UserOp.
    mapping(bytes32 => uint256) public redeemPrepayments;

    event Initiated(
        bytes32 indexed swapKey,
        address indexed token,
        address indexed initiator,
        uint256 value
    );

    event Redeemed(
        bytes32 indexed swapKey,
        address indexed token,
        address indexed participant,
        bytes32 secret
    );

    event Refunded(
        bytes32 indexed swapKey,
        address indexed token,
        address indexed initiator
    );

    event RedeemAATransferFailed(
        address indexed recipient,
        uint256 amount
    );

    // Vector is the information necessary for initialization and redemption
    // or refund. The Vector itself is not stored on-chain. Instead, a key
    // unique to the Vector is generated from the Vector data and keys
    // the swap record.
    struct Vector {
        bytes32 secretHash;
        uint256 value;
        address initiator;
        uint64 refundTimestamp;
        address participant;
    }

    // Redemption is the information necessary to redeem a Vector. Since we
    // don't store the Vector itself, it must be provided as part of the
    // redemption.
    struct Redemption {
        Vector v;
        bytes32 secret;
    }

    /// @notice Constructs the ETHSwap contract.
    /// @param _entryPoint The ERC-4337 EntryPoint contract address for account abstraction support
    constructor(address payable _entryPoint) {
        require(_entryPoint != address(0), "zero entry point");
        entryPoint = _entryPoint;
    }

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    //
    // NOTE: This uses tx.origin which prevents smart contract wallets from
    // using initiate/redeem/refund directly. Smart contract wallet users
    // should use the account abstraction functions (redeemAA) instead.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender, "sender != origin");
        _;
    }

    /// @notice Generates a unique key hash which commits to the contract data.
    /// @dev The generated hash is used as a key in the swaps map.
    /// @param token The token address (address(0) for native ETH)
    /// @param v The Vector containing swap parameters
    /// @return The sha256 hash of the encoded swap parameters
    function contractKey(address token, Vector memory v)
        public
        pure
        returns (bytes32)
    {
        return sha256(
            bytes.concat(
                v.secretHash,
                bytes20(v.initiator),
                bytes20(v.participant),
                bytes32(v.value),
                bytes8(v.refundTimestamp),
                bytes20(token)
            )
        );
    }

    /// @notice Validates that a secret hashes to the expected secret hash.
    /// @param secret The preimage to validate
    /// @param secretHash The expected hash of the secret
    /// @return True if sha256(secret) equals secretHash
    function secretValidates(bytes32 secret, bytes32 secretHash)
        public
        pure
        returns (bool)
    {
        return sha256(bytes.concat(secret)) == secretHash;
    }

    function _retrieve(address token, Vector memory v)
        internal
        view
        returns (bytes32 key, bytes32 record, uint256 blockNum)
    {
        key = contractKey(token, v);
        record = swaps[key];
        blockNum = uint256(record);
    }

    /// @notice Returns the current state of a swap.
    /// @param token The token address (address(0) for native ETH)
    /// @param v The Vector containing swap parameters
    /// @return s The Status struct containing step, secret (if redeemed), and blockNumber (if filled)
    function status(address token, Vector calldata v)
        external
        view
        returns (Status memory s)
    {
        (, bytes32 record, uint256 blockNum) = _retrieve(token, v);

        if (blockNum == 0) {
            s.step = Step.Empty;
        } else if (record == REFUND_RECORD) {
            s.step = Step.Refunded;
        } else if (secretValidates(record, v.secretHash)) {
            s.step = Step.Redeemed;
            s.secret = record;
        } else {
            s.step = Step.Filled;
            s.blockNumber = blockNum;
        }
    }

    /// @notice Returns whether or not a swap can be redeemed.
    /// @dev Does NOT check if the caller is the participant in the vector.
    /// @param token The token address (address(0) for native ETH)
    /// @param v The Vector containing swap parameters
    /// @return True if the swap exists and has not been redeemed or refunded
    function isRedeemable(address token, Vector calldata v)
        public
        view
        returns (bool)
    {
        (, bytes32 record, uint256 blockNum) = _retrieve(token, v);
        return blockNum != 0 && record != REFUND_RECORD && !secretValidates(record, v.secretHash);
    }

    /// @notice Initiates one or more atomic swaps.
    /// @dev For ETH swaps, msg.value must equal the sum of all vector values.
    /// For ERC20 swaps, the caller must have approved this contract to spend the total amount.
    /// @param token The token address (address(0) for native ETH)
    /// @param vectors Array of Vector structs defining each swap's parameters
    function initiate(address token, Vector[] calldata vectors)
        external
        payable
        senderIsOrigin
        nonReentrant
    {
        require(vectors.length > 0 && vectors.length <= MAX_BATCH, "bad batch size");

        uint256 total;

        for (uint256 i = 0; i < vectors.length; i++) {
            Vector calldata v = vectors[i];

            require(v.value > 0, "zero value");
            require(v.refundTimestamp > block.timestamp, "bad refund time");
            require(v.secretHash != REFUND_RECORD_HASH, "illegal hash");

            bytes32 key = contractKey(token, v);
            require(swaps[key] == bytes32(0), "already exists");

            bytes32 record = bytes32(block.number);
            require(!secretValidates(record, v.secretHash), "hash collision");

            swaps[key] = record;
            total += v.value;

            emit Initiated(key, token, v.initiator, v.value);
        }

        if (token == address(0)) {
            require(msg.value == total, "bad ETH value");
        } else {
            require(msg.value == 0, "no ETH for token swap");
            IERC20(token).safeTransferFrom(
                msg.sender,
                address(this),
                total
            );
        }
    }

    /// @notice Redeems one or more swaps by providing the secret preimage.
    /// @dev The caller must be the participant specified in each Vector.
    /// All redemptions must be for the same token. Funds are transferred to msg.sender.
    /// To prevent reentry attacks, state is updated before any transfers.
    /// Contracts cannot call this function directly (use redeemAA for account abstraction).
    /// @param token The token address (address(0) for native ETH)
    /// @param redemptions Array of Redemption structs containing the Vector and secret
    function redeem(address token, Redemption[] calldata redemptions)
        external
        senderIsOrigin
        nonReentrant
    {
        require(redemptions.length > 0 && redemptions.length <= MAX_BATCH, "bad batch size");

        uint256 total;

        for (uint256 i = 0; i < redemptions.length; i++) {
            Redemption calldata r = redemptions[i];

            require(r.v.participant == msg.sender, "not participant");

            (bytes32 key, bytes32 record, uint256 blockNum) =
                _retrieve(token, r.v);

            require(blockNum > 0 && blockNum < block.number, "not redeemable");
            require(record != REFUND_RECORD, "already refunded");
            require(!secretValidates(record, r.v.secretHash), "already redeemed");
            require(secretValidates(r.secret, r.v.secretHash), "bad secret");

            swaps[key] = r.secret;
            total += r.v.value;

            emit Redeemed(key, token, msg.sender, r.secret);
        }

        if (token == address(0)) {
            (bool ok,) = payable(msg.sender).call{value: total}("");
            require(ok, "ETH transfer failed");
        } else {
            IERC20(token).safeTransfer(msg.sender, total);
        }
    }

    /// @notice Refunds a swap after the refund timestamp has passed.
    /// @dev Can be called by anyone after the refund time has passed.
    /// Funds are always sent to v.initiator regardless of caller.
    /// Uses low-level call with no gas restrictions.
    /// @param token The token address (address(0) for native ETH)
    /// @param v The Vector containing swap parameters
    function refund(address token, Vector calldata v)
        external
        senderIsOrigin
        nonReentrant
    {
        require(block.timestamp >= v.refundTimestamp, "not expired");

        (bytes32 key, bytes32 record, uint256 blockNum) =
            _retrieve(token, v);

        require(blockNum > 0, "not initiated");
        require(record != REFUND_RECORD, "already refunded");
        require(!secretValidates(record, v.secretHash), "already redeemed");

        swaps[key] = REFUND_RECORD;

        if (token == address(0)) {
            (bool ok,) = payable(v.initiator).call{value: v.value}("");
            require(ok, "ETH refund failed");
        } else {
            IERC20(token).safeTransfer(v.initiator, v.value);
        }

        emit Refunded(key, token, v.initiator);
    }

    modifier senderIsEntryPoint() {
        require(msg.sender == entryPoint, "not entryPoint");
        _;
    }

    // _payPrefund transfers the required gas prefund to the EntryPoint.
    // Per ERC-4337 spec, the return value is intentionally ignored because
    // prefund failures should not revert the validateUserOp call - the
    // EntryPoint will handle insufficient prefund scenarios.
    function _payPrefund(uint256 missingAccountFunds) internal {
        if (missingAccountFunds > 0) {
            (bool ok,) = payable(msg.sender).call{
                value: missingAccountFunds
            }("");
            (ok); // Silence unused variable warning; see comment above.
        }
    }

    /// @notice Validates a user operation for redeeming swaps via ERC-4337 account abstraction.
    /// @dev Transfers the required gas prefund to the EntryPoint.
    /// Returns VALIDATE_SUCCESS (0) on success, or an error code (1-9) on failure.
    /// @param userOp The user operation to validate
    /// @param userOpHash The hash of the user operation for signature verification
    /// @param missingAccountFunds The amount of ETH needed for gas prefund
    /// @return validationData 0 on success, non-zero error code on failure
    function validateUserOp(
        UserOperation calldata userOp,
        bytes32 userOpHash,
        uint256 missingAccountFunds
    )
        external
        senderIsEntryPoint
        returns (uint256)
    {
        if (userOp.callData.length < 4) return VALIDATE_CALLDATA_TOO_SHORT;

        if (bytes4(userOp.callData[:4]) != this.redeemAA.selector) return VALIDATE_WRONG_SELECTOR;

        Redemption[] memory reds =
            abi.decode(userOp.callData[4:], (Redemption[]));

        if (reds.length == 0 || reds.length > MAX_BATCH) return VALIDATE_BAD_BATCH_SIZE;

        address participant;
        uint256 total;
        bytes32[] memory secrets = new bytes32[](reds.length);

        for (uint256 i = 0; i < reds.length; i++) {
            Redemption memory r = reds[i];
            secrets[i] = r.secret;

            // Verify the secret is valid before paying any prefund
            if (!secretValidates(r.secret, r.v.secretHash)) {
                return VALIDATE_INVALID_SECRET;
            }

            // redeemAA only supports ETH swaps (address(0))
            (, bytes32 record, uint256 blockNum) =
                _retrieve(address(0), r.v);

            // Must match redeemAA's checks.
            if (blockNum == 0
                || blockNum >= block.number
                || record == REFUND_RECORD
                || secretValidates(record, r.v.secretHash)
            ) {
                return VALIDATE_SWAP_NOT_REDEEMABLE;
            }

            if (i == 0) {
                participant = r.v.participant;
            } else {
                if (r.v.participant != participant) return VALIDATE_PARTICIPANT_MISMATCH;
            }

            total += r.v.value;
        }

        // Ensure callGasLimit is sufficient to prevent out-of-gas attacks
        uint256 minCallGas = MIN_CALL_GAS_BASE + (reds.length * MIN_CALL_GAS_PER_REDEMPTION);
        if (userOp.callGasLimit < minCallGas) return VALIDATE_INSUFFICIENT_CALL_GAS;

        if (missingAccountFunds > total) return VALIDATE_INSUFFICIENT_VALUE;

        if (participant != ECDSA.recover(userOpHash, userOp.signature)) {
            return VALIDATE_INVALID_SIGNATURE;
        }

        // Key by participant, total, and secrets to ensure uniqueness per UserOp.
        // This prevents collisions when different swaps share the same secrets.
        // Written after all validation checks to avoid stale entries on failure.
        bytes32 prepayKey = keccak256(abi.encode(participant, total, secrets));
        redeemPrepayments[prepayKey] = missingAccountFunds;

        _payPrefund(missingAccountFunds);
        return VALIDATE_SUCCESS;
    }

    /// @notice Redeems multiple ETH swaps via ERC-4337 account abstraction.
    /// @dev Only supports native ETH swaps. ERC20 token swaps must use the regular redeem function.
    /// Verifies that:
    /// 1. All redemptions are for the same participant
    /// 2. Each swap exists and hasn't been redeemed yet
    /// 3. The provided secret matches the secret hash
    /// After validation, records the redemption and transfers funds minus fees to participant.
    /// @param redemptions Array of Redemption structs containing the Vector and secret
    function redeemAA(Redemption[] calldata redemptions)
        external
        senderIsEntryPoint
        nonReentrant
    {
        require(redemptions.length > 0 && redemptions.length <= MAX_BATCH, "bad batch size");

        uint256 total;
        address recipient;
        bytes32[] memory secrets = new bytes32[](redemptions.length);

        for (uint256 i = 0; i < redemptions.length; i++) {
            Redemption calldata r = redemptions[i];
            secrets[i] = r.secret;

            if (i == 0) {
                recipient = r.v.participant;
            } else {
                require(r.v.participant == recipient, "participant mismatch");
            }

            (bytes32 key, bytes32 record, uint256 blockNum) =
                _retrieve(address(0), r.v);

            require(blockNum > 0 && blockNum < block.number, "not redeemable");
            require(record != REFUND_RECORD, "already refunded");
            require(!secretValidates(record, r.v.secretHash), "already redeemed");
            require(secretValidates(r.secret, r.v.secretHash), "bad secret");

            swaps[key] = r.secret;
            total += r.v.value;

            emit Redeemed(key, address(0), recipient, r.secret);
        }

        // Key by recipient, total, and secrets - must match validateUserOp
        bytes32 prepayKey = keccak256(abi.encode(recipient, total, secrets));
        uint256 fees = redeemPrepayments[prepayKey];
        delete redeemPrepayments[prepayKey];

        // Intentionally not requiring success. If the recipient is a contract
        // that reverts on ETH transfer, the swap must still finalize to prevent
        // a drain attack where validateUserOp pays prefund but redeemAA reverts,
        // allowing repeated extraction of contract funds to the EntryPoint.
        uint256 payout = total - fees;
        (bool ok,) = payable(recipient).call{value: payout}("");
        if (!ok) {
            emit RedeemAATransferFailed(recipient, payout);
        }
    }
}
