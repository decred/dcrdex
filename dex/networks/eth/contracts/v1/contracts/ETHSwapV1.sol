// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.23;

import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import "@openzeppelin/contracts/utils/cryptography/EIP712.sol";
import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

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
/// the swap or multisig wallets (except via EIP-712 signed redemptions).
///
/// TOKEN COMPATIBILITY: This contract is NOT compatible with fee-on-transfer
/// tokens or rebasing tokens. Using such tokens will result in accounting
/// mismatches and potential loss of funds. Only use standard ERC20 tokens
/// where transfer(amount) results in exactly amount being received.
///
/// This code should be verifiable as resulting in a certain on-chain contract
/// by compiling with the correct version of solidity and comparing the
/// resulting byte code to the data in the original transaction.
contract ETHSwap is EIP712, ReentrancyGuard {
    using SafeERC20 for IERC20;

    uint256 public constant MAX_BATCH = 20;

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

    // ZERO_SECRET_HASH is sha256(bytes32(0)). Swaps with this secretHash
    // are unredeemable because redeem rejects zero secrets.
    bytes32 internal constant ZERO_SECRET_HASH =
        0x66687AADF862BD776C8FC18B8E9F8E20089714856EE233B3902A591D0D5F2925;

    // EIP-712 type hash for signed redemptions.
    bytes32 public constant REDEEM_TYPEHASH = keccak256(
        "Redeem(address feeRecipient,bytes32 redemptionsHash,uint256 relayerFee,uint256 nonce,uint256 deadline)"
    );

    // Sequential nonces per participant for signed redeems.
    //
    // A swap cannot be redeemed twice, but a participant can still sign
    // multiple redeemWithSignature messages over time. The nonce makes each
    // signed redeem usable only once and prevents old signed messages from
    // staying valid until the deadline.
    //
    // It also gives the wallet and relay a simple way to identify and dedupe
    // signed redeem requests: participant + nonce.
    mapping(address => uint256) public nonces;

    // swaps is a map of contract hashes to the "swap record". The swap record
    // has the following interpretation.
    //   if (record == bytes32(0x00)): contract is uninitiated
    //   else if (uint256(record) < block.number && sha256(record) != contract.secretHash):
    //      contract is initiated and redeemable by the participant with the secret.
    //   else if (sha256(record) == contract.secretHash): contract has been redeemed
    //   else if (record == REFUND_RECORD): contract has been refunded
    //   else: invalid record. Should be impossible by construction
    mapping(bytes32 => bytes32) public swaps;

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

    /// @notice Constructs the ETHSwap contract with EIP-712 domain.
    constructor() EIP712("ETHSwap", "1") {}

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    //
    // NOTE: This uses tx.origin which prevents smart contract wallets from
    // using initiate/redeem/refund directly.
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
            require(v.initiator == msg.sender, "bad initiator");
            require(v.participant != address(0), "zero addr");
            require(v.refundTimestamp > block.timestamp, "bad refund time");
            require(v.secretHash != bytes32(0), "zero hash");
            require(v.secretHash != REFUND_RECORD_HASH, "illegal hash");
            require(v.secretHash != ZERO_SECRET_HASH, "zero secret hash");

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
            uint256 balBefore = IERC20(token).balanceOf(address(this));
            IERC20(token).safeTransferFrom(
                msg.sender,
                address(this),
                total
            );
            require(
                IERC20(token).balanceOf(address(this)) - balBefore == total,
                "fee-on-transfer not supported"
            );
        }
    }

    /// @notice Redeems one or more swaps by providing the secret preimage.
    /// @dev The caller must be the participant specified in each Vector.
    /// Requires at least one redemption.
    /// All redemptions must be for the same token. Funds are transferred to msg.sender.
    /// To prevent reentry attacks, state is updated before any transfers.
    /// Contracts cannot call this function directly (use redeemWithSignature for relayed redemptions).
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
            require(r.secret != bytes32(0), "zero secret");
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

    // _verifySignedRedeem validates the EIP-712 signature and nonce for a
    // signed redemption. Returns the participant address.
    function _verifySignedRedeem(
        Redemption[] calldata redemptions,
        address feeRecipient,
        uint256 relayerFee,
        uint256 nonce,
        uint256 deadline,
        bytes calldata signature
    ) internal returns (address participant) {
        require(block.timestamp <= deadline, "expired");
        require(redemptions.length > 0 && redemptions.length <= MAX_BATCH, "bad batch size");

        participant = redemptions[0].v.participant;

        // Check and increment nonce.
        require(nonces[participant] == nonce, "bad nonce");
        nonces[participant] = nonce + 1;

        // Compute the redemptions hash: keccak256(abi.encode(redemptions)).
        bytes32 redemptionsHash = keccak256(abi.encode(redemptions));

        // Build and verify EIP-712 signature.
        bytes32 structHash = keccak256(abi.encode(
            REDEEM_TYPEHASH,
            feeRecipient,
            redemptionsHash,
            relayerFee,
            nonce,
            deadline
        ));
        address recovered = ECDSA.recover(_hashTypedDataV4(structHash), signature);
        require(recovered == participant, "bad signature");
    }

    // _executeETHRedemptions processes the swap state changes for native ETH
    // swaps only and returns the total value. Token swaps use a different
    // contract key (keyed by token address), so they cannot be redeemed
    // through this path.
    function _executeETHRedemptions(
        Redemption[] calldata redemptions,
        address participant
    ) internal returns (uint256 total) {
        for (uint256 i = 0; i < redemptions.length; i++) {
            Redemption calldata r = redemptions[i];

            require(r.v.participant == participant, "participant mismatch");

            (bytes32 key, bytes32 record, uint256 blockNum) =
                _retrieve(address(0), r.v);

            require(blockNum > 0 && blockNum < block.number, "not redeemable");
            require(record != REFUND_RECORD, "already refunded");
            require(!secretValidates(record, r.v.secretHash), "already redeemed");
            require(r.secret != bytes32(0), "zero secret");
            require(secretValidates(r.secret, r.v.secretHash), "bad secret");

            swaps[key] = r.secret;
            total += r.v.value;

            emit Redeemed(key, address(0), participant, r.secret);
        }
    }

    /// @notice Redeems swaps using an EIP-712 signature from the participant.
    /// @dev Allows a third-party relayer to submit redemptions on behalf of the
    /// participant. The participant signs the redemption parameters, the relayer
    /// fee, and the fee recipient address. When feeRecipient is nonzero, the
    /// contract enforces feeRecipient == msg.sender to prevent MEV front-running.
    /// When feeRecipient is zero, any relayer may submit and receive the signed fee.
    /// Only supports native ETH swaps.
    /// @param redemptions Array of Redemption structs containing the Vector and secret
    /// @param feeRecipient Address that receives the relayer fee (signed, must equal msg.sender or zero)
    /// @param relayerFee Amount to pay the relayer from redeemed funds (signed)
    /// @param nonce Sequential nonce for replay protection (signed)
    /// @param deadline Timestamp after which the signature expires (signed)
    /// @param signature EIP-712 signature from the participant
    function redeemWithSignature(
        Redemption[] calldata redemptions,
        address feeRecipient,
        uint256 relayerFee,
        uint256 nonce,
        uint256 deadline,
        bytes calldata signature
    ) external nonReentrant {
        address participant = _verifySignedRedeem(
            redemptions, feeRecipient, relayerFee, nonce, deadline, signature
        );

        uint256 total = _executeETHRedemptions(redemptions, participant);

        require(relayerFee <= total / 2, "fee exceeds half");

        // Enforce feeRecipient == msg.sender (or use msg.sender if zero).
        if (feeRecipient != address(0)) {
            require(feeRecipient == msg.sender, "feeRecipient must be msg.sender");
        } else {
            feeRecipient = msg.sender;
        }

        // Transfer funds: relayer fee to feeRecipient, remainder to participant.
        uint256 remainder = total - relayerFee;

        if (relayerFee > 0) {
            (bool ok,) = payable(feeRecipient).call{value: relayerFee}("");
            require(ok, "fee transfer failed");
        }
        if (remainder > 0) {
            (bool ok,) = payable(participant).call{value: remainder}("");
            require(ok, "ETH transfer failed");
        }
    }
}
