// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.18;

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
contract ETHSwap {
    // Step is a type that hold's a contract's current step. Empty is the
    // uninitiated or null value.
    enum Step { Empty, Filled, Redeemed, Refunded }

    struct Status {
        Step step;
        bytes32 secret;
        uint256 blockNumber;
    }

    bytes32 constant RefundRecord = 0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF;

    // swaps is a map of contract hashes to the "swap record". The swap record
    // has the following interpretation.
    //   if (record == bytes32(0x00)): contract is uninitiated
    //   else if (uint256(record) < block.number && sha256(record) != contract.secretHash):
    //      contract is initiated and redeemable by the participant with the secret.
    //   else if (sha256(record) == contract.secretHash): contract has been redeemed
    //   else if (record == RefundRecord): contract has been refunded
    //   else: invalid record. Should be impossible by construction
    mapping(bytes32 => bytes32) public swaps;

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
    function contractKey(Vector calldata v) public pure returns (bytes32) {
        return sha256(bytes.concat(v.secretHash, bytes20(v.initiator), bytes20(v.participant), bytes32(v.value), bytes8(v.refundTimestamp)));
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

    // constructor is empty. This contract has no connection to the original
    // sender after deployed. It can only be interacted with by users
    // initiating, redeeming, and refunding swaps.
    constructor() {}

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender, "sender != origin");
        _;
    }

    // retrieveStatus retrieves the current swap record for the contract.
    function retrieveStatus(Vector calldata v)
        private view returns (bytes32, bytes32, uint256)
    {
        bytes32 k = contractKey(v);
        bytes32 record = swaps[k];
        return (k, record, uint256(record));
    }

    // status returns the current state of the swap.
    function status(Vector calldata v)
        public view returns(Status memory)
    {
        (, bytes32 record, uint256 blockNum) = retrieveStatus(v);
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
    function initiate(Vector[] calldata contracts)
        public
        payable
        senderIsOrigin()
    {
        uint initVal = 0;
        for (uint i = 0; i < contracts.length; i++) {
            Vector calldata v = contracts[i];

            require(v.value > 0, "0 val");
            require(v.refundTimestamp > 0, "0 refundTimestamp");

            bytes32 k = contractKey(v);
            bytes32 record = swaps[k];
            require(record == bytes32(0), "swap not empty");

            record = bytes32(block.number);
            require(!secretValidates(record, v.secretHash), "hash collision");

            swaps[k] = record;

            initVal += v.value;
        }

        require(initVal == msg.value, "bad val");
    }

    // isRedeemable returns whether or not a swap identified by secretHash
    // can be redeemed using secret. isRedeemable DOES NOT check if the caller
    // is the participant in the vector.
    function isRedeemable(Vector calldata v)
        public
        view
        returns (bool)
    {
        (, bytes32 record, uint256 blockNum) = retrieveStatus(v);
        return blockNum != 0 && !secretValidates(record, v.secretHash);
    }

    // redeem redeems a Vector. It checks that the sender is not a contract,
    // and that the secret hash hashes to secretHash. msg.value is tranfered
    // from ETHSwap to the sender.
    //
    // To prevent reentry attack, it is very important to check the state of the
    // contract first, and change the state before proceeding to send. That way,
    // the nested attacking function will throw upon trying to call redeem a
    // second time. Currently, reentry is also not possible because contracts
    // cannot use this contract.
    function redeem(Redemption[] calldata redemptions)
        public
        senderIsOrigin()
    {
        uint amountToRedeem = 0;
        for (uint i = 0; i < redemptions.length; i++) {
            Redemption calldata r = redemptions[i];

            require(r.v.participant == msg.sender, "not authed");

            (bytes32 k, bytes32 record, uint256 blockNum) = retrieveStatus(r.v);

            // To be redeemable, the record needs to represent a valid block
            // number.
            require(blockNum > 0 && blockNum < block.number, "unfilled swap");

            // Can't already be redeemed.
            require(!secretValidates(record, r.v.secretHash), "already redeemed");

            // Are they presenting the correct secret?
            require(secretValidates(r.secret, r.v.secretHash), "invalid secret");

            swaps[k] = r.secret;


            // DRAFT TODO: NOOOOOO! This doesn't account for decimals. This is
            // WRONG for e.g. USDC.
            amountToRedeem += r.v.value;
        }

        (bool ok, ) = payable(msg.sender).call{value: amountToRedeem}("");
        require(ok == true, "transfer failed");
    }

    // refund refunds a Vector. It checks that the sender is not a contract
    // and that the refund time has passed. msg.value is transfered from the
    // contract to the sender = Vector.participant.
    //
    // It is important to note that this also uses call.value which comes with
    // no restrictions on gas used. See redeem for more info.
    function refund(Vector calldata v)
        public
        senderIsOrigin()
    {
        // Is this contract even in a refundable state?
        require(block.timestamp >= v.refundTimestamp, "locktime not expired");

        // Retrieve the record.
        (bytes32 k, bytes32 record, uint256 blockNum) = retrieveStatus(v);

        // Is this swap initialized?
        require(blockNum > 0 && blockNum <= block.number, "swap not active");

        // Is it already redeemed?
        require(!secretValidates(record, v.secretHash), "swap already redeemed");

        // Is it already refunded?
        require(record != RefundRecord, "swap already refunded");

        swaps[k] = RefundRecord;

        (bool ok, ) = payable(v.initiator).call{value: v.value}("");
        require(ok == true, "transfer failed");
    }
}
