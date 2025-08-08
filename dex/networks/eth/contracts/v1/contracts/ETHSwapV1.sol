// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.18;

import "@account-abstraction/contracts/interfaces/IAccount.sol";
import "@account-abstraction/contracts/core/EntryPoint.sol";
import "@openzeppelin/contracts/utils/cryptography/ECDSA.sol";

// ETHSwap creates a contract to be deployed on an ethereum network. After
// deployed, it keeps a record of the state of a contract and enables
// redemption and refund of the contract when conditions are met.
//
// ETHSwap accomplishes this by holding funds sent to ETHSwap until certain
// conditions are met. An initiator sends a tx with the Vector(s) to fund and
// the requisite value to transfer to ETHSwap. At
// this point the funds belong to the contract, and cannot be accessed by
// anyone else, not even the contract's deployer. The swap Vector specifies
// the conditions necessary for refund and redeem.
//
// ETHSwap has no limits on gas used for any transactions.
//
// ETHSwap cannot be used by other contracts or by a third party mediating
// the swap or multisig wallets.
//
// This code should be verifiable as resulting in a certain on-chain contract
// by compiling with the correct version of solidity and comparing the
// resulting byte code to the data in the original transaction.
contract ETHSwap is IAccount {
    bytes4 private constant TRANSFER_FROM_SELECTOR = bytes4(keccak256("transferFrom(address,address,uint256)"));
    bytes4 private constant TRANSFER_SELECTOR = bytes4(keccak256("transfer(address,uint256)"));
    // Step is a type that hold's a contract's current step. Empty is the
    // uninitiated or null value.
    enum Step { Empty, Filled, Redeemed, Refunded }

    struct Status {
        Step step;
        bytes32 secret;
        uint256 blockNumber;
    }

    bytes32 constant RefundRecord = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF;
    bytes32 constant RefundRecordHash = 0xAF9613760F72635FBDB44A5A0A63C39F12AF30F950A6EE5C971BE188E89C4051;

    address payable public entryPoint;

    // swaps is a map of contract hashes to the "swap record". The swap record
    // has the following interpretation.
    //   if (record == bytes32(0x00)): contract is uninitiated
    //   else if (uint256(record) < block.number && sha256(record) != contract.secretHash):
    //      contract is initiated and redeemable by the participant with the secret.
    //   else if (sha256(record) == contract.secretHash): contract has been redeemed
    //   else if (record == RefundRecord): contract has been refunded
    //   else: invalid record. Should be impossible by construction
    mapping(bytes32 => bytes32) public swaps;

    mapping(bytes32 => uint256) public redeemPrepayments;

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

    // contractKey generates a key hash which commits to the contract data. The
    // generated hash is used as a key in the swaps map.
    function contractKey(address token, Vector memory v) public pure returns (bytes32) {
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

    // Redemption is the information necessary to redeem a Vector. Since we
    // don't store the Vector itself, it must be provided as part of the
    // redemption.
    struct Redemption {
        Vector v;
        bytes32 secret;
    }

    function secretValidates(bytes32 secret, bytes32 secretHash) public pure returns (bool) {
        return sha256(bytes.concat(secret)) == secretHash;
    }

    constructor(address payable _entryPoint) {
        entryPoint = _entryPoint;
    }

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender, "sender != origin");
        _;
    }

    // retrieveStatus retrieves the current swap record for the contract.
    function retrieveStatus(address token, Vector memory v)
        private view returns (bytes32, bytes32, uint256)
    {
        bytes32 k = contractKey(token, v);
        bytes32 record = swaps[k];
        return (k, record, uint256(record));
    }

    // status returns the current state of the swap.
    function status(address token, Vector calldata v)
        public view returns(Status memory)
    {
        (, bytes32 record, uint256 blockNum) = retrieveStatus(token, v);
        Status memory r;
        if (blockNum == 0) {
            r.step = Step.Empty;
        } else if (record == RefundRecord) {
            r.step = Step.Refunded;
        } else if (secretValidates(record, v.secretHash)) {
            r.step = Step.Redeemed;
            r.secret = record;
        } else {
            r.step = Step.Filled;
            r.blockNumber =  blockNum;
        }
        return r;
    }

    // initiate initiates an array of Vectors.
    function initiate(address token, Vector[] calldata contracts)
        public
        payable
        senderIsOrigin()
    {
        uint initVal = 0;
        for (uint i = 0; i < contracts.length; i++) {
            Vector calldata v = contracts[i];

            require(v.value > 0, "0 val");
            require(v.refundTimestamp > 0, "0 refundTimestamp");
            require(v.secretHash != RefundRecordHash, "illegal secret hash (refund record hash)");

            bytes32 k = contractKey(token, v);
            bytes32 record = swaps[k];
            require(record == bytes32(0), "swap not empty");

            record = bytes32(block.number);
            require(!secretValidates(record, v.secretHash), "hash collision");

            swaps[k] = record;

            initVal += v.value;
        }

        if (token == address(0)) {
            require(initVal == msg.value, "bad val");
        } else {
            bool success;
            bytes memory data;
            (success, data) = token.call(abi.encodeWithSelector(TRANSFER_FROM_SELECTOR, msg.sender, address(this), initVal));
            require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer from failed');
        }
    }

    // isRedeemable returns whether or not a swap identified by vector
    // can be redeemed using secret. isRedeemable DOES NOT check if the caller
    // is the participant in the vector.
    function isRedeemable(address token, Vector calldata v)
        public
        view
        returns (bool)
    {
        (, bytes32 record, uint256 blockNum) = retrieveStatus(token, v);
        return blockNum != 0 && !secretValidates(record, v.secretHash);
    }

    // redeem redeems a Vector. It checks that the sender is not a contract,
    // and that the secret hashes to secretHash. msg.value is tranfered
    // from ETHSwap to the sender.
    //
    // To prevent reentry attack, it is very important to check the state of the
    // contract first, and change the state before proceeding to send. That way,
    // the nested attacking function will throw upon trying to call redeem a
    // second time. Currently, reentry is also not possible because contracts
    // cannot use this contract.
    function redeem(address token, Redemption[] calldata redemptions)
        public
        senderIsOrigin()
    {
        uint amountToRedeem = 0;
        for (uint i = 0; i < redemptions.length; i++) {
            Redemption calldata r = redemptions[i];

            require(r.v.participant == msg.sender, "not authed");

            (bytes32 k, bytes32 record, uint256 blockNum) = retrieveStatus(token, r.v);

            // To be redeemable, the record needs to represent a valid block
            // number.
            require(blockNum > 0 && blockNum < block.number, "unfilled swap");

            // Can't already be redeemed.
            require(!secretValidates(record, r.v.secretHash), "already redeemed");

            // Are they presenting the correct secret?
            require(secretValidates(r.secret, r.v.secretHash), "invalid secret");

            swaps[k] = r.secret;
            amountToRedeem += r.v.value;
        }

         if (token == address(0)) {
            (bool ok, ) = payable(msg.sender).call{value: amountToRedeem}("");
            require(ok == true, "transfer failed");
         } else {
            bool success;
            bytes memory data;
            (success, data) = token.call(abi.encodeWithSelector(TRANSFER_SELECTOR, msg.sender, amountToRedeem));
            require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer failed');
         }
    }

    // refund refunds a Vector. It checks that the sender is not a contract
    // and that the refund time has passed. msg.value is transfered from the
    // contract to the sender = Vector.participant.
    //
    // It is important to note that this also uses call.value which comes with
    // no restrictions on gas used. See redeem for more info.
    function refund(address token, Vector calldata v)
        public
        senderIsOrigin()
    {
        // Is this contract even in a refundable state?
        require(block.timestamp >= v.refundTimestamp, "locktime not expired");

        // Retrieve the record.
        (bytes32 k, bytes32 record, uint256 blockNum) = retrieveStatus(token, v);

        // Is this swap initialized?
        // This check also guarantees that the swap has not already been
        // refunded i.e. record != RefundRecord, since RefundRecord is certainly
        // greater than block.number.
        require(blockNum > 0 && blockNum <= block.number, "swap not active");

        // Is it already redeemed?
        require(!secretValidates(record, v.secretHash), "swap already redeemed");

        swaps[k] = RefundRecord;

        if (token == address(0)) {
            (bool ok, ) = payable(v.initiator).call{value: v.value}("");
            require(ok == true, "transfer failed");
        } else {
            bool success;
            bytes memory data;
            (success, data) = token.call(abi.encodeWithSelector(TRANSFER_SELECTOR, v.initiator, v.value));
            require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer failed');
        }
    }

    modifier senderIsEntryPoint() {
        require(entryPoint == msg.sender, "sender != entryPoint");
        _;
    }

    function _payPrefund(uint256 missingAccountFunds) internal {
        if (missingAccountFunds != 0) {
            (bool success, ) = payable(msg.sender).call{
                value: missingAccountFunds,
                gas: type(uint256).max
            }("");
            (success);
            //ignore failure (its EntryPoint's job to verify, not account.)
        }
    }

    // validateUserOp validates a user operation for redeeming swaps via account abstraction, 
    // and transfers the amount required for gas fees to the entrypoint.
    function validateUserOp(UserOperation calldata userOp, bytes32 userOpHash, uint256 missingAccountFunds)
        external
        senderIsEntryPoint()
        returns (uint256 validationData) {

        if (userOp.callData.length < 4) {
            return 1;
        }

        // 0x919835a4 is the function selector for redeemAA.
        if (bytes4(userOp.callData[0:4]) != bytes4(0x919835a4)) {
            return 2;
        }

        (Redemption[] memory redemptions) = abi.decode(userOp.callData[4:], (Redemption[]));

        address participant;
        uint256 amountToRedeem;
        for (uint i = 0; i < redemptions.length; i++) {
            Redemption memory redemption = redemptions[i];
            (, bytes32 record, uint256 blockNum) = retrieveStatus(address(0), redemption.v);
            if (blockNum == 0 || secretValidates(record, redemption.v.secretHash)) {
                return 3;
            }
            if (i == 0) {
                participant = redemption.v.participant;
                redeemPrepayments[redemption.v.secretHash] = missingAccountFunds;
            } else if (redemption.v.participant != participant) {
                return 4;
            }
            amountToRedeem += redemption.v.value;
        }

        if (missingAccountFunds > amountToRedeem) {
            return 5;
        }

        if (participant != ECDSA.recover(userOpHash, userOp.signature)) {
            return 6;
        }

        _payPrefund(missingAccountFunds);

        return 0;
    }

    // redeemAA redeems multiple swaps via account abstraction. It verifies that:
    // 1. All redemptions are for the same participant
    // 2. Each swap exists and hasn't been redeemed yet
    // 3. The provided secret matches the secret hash
    // After validation, it records the redemption and transfers the funds minus fees
    // to the participant.
    function redeemAA(Redemption[] calldata redemptions)
        public
        senderIsEntryPoint() {

        uint amountToRedeem = 0;
        address recipient;

        for (uint i = 0; i < redemptions.length; i++) {
            Redemption calldata r = redemptions[i];

            if (i == 0) {
                recipient = r.v.participant;
            } else {
                require(r.v.participant == recipient, "bad participant");
            }

            (bytes32 k, bytes32 record, uint256 blockNum) = retrieveStatus(address(0), r.v);

            // To be redeemable, the record needs to represent a valid block
            // number.
            require(blockNum > 0 && blockNum < block.number, "unfilled swap");

            // Can't already be redeemed.
            require(!secretValidates(record, r.v.secretHash), "already redeemed");

            // Are they presenting the correct secret?
            require(secretValidates(r.secret, r.v.secretHash), "invalid secret");

            swaps[k] = r.secret;
            amountToRedeem += r.v.value;
        }

        uint256 fees = redeemPrepayments[redemptions[0].v.secretHash];
        delete redeemPrepayments[redemptions[0].v.secretHash];

        (bool ok, ) = payable(recipient).call{value: amountToRedeem - fees}("");
        require(ok == true, "transfer failed");
    }
}