// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.15;

// ETHSwap creates a contract to be deployed on an ethereum network. In
// order to save on gas fees, a separate ERC20Swap contract is deployed
// for each ERC20 token. After deployed, it keeps a map of swaps that
// facilitates atomic swapping of ERC20 tokens with other crypto currencies
// that support time locks. 
//
// It accomplishes this by holding tokens acquired during a swap initiation
// until conditions are met. Prior to initiating a swap, the initiator must
// approve the ERC20Swap contract to be able to spend the initiator's tokens.
// When calling initiate, the necessary tokens for swaps are transferred to
// the swap contract. At this point the funds belong to the contract, and
// cannot be accessed by anyone else, not even the contract's deployer. The
// initiator sets a secret hash, a blocktime the funds will be accessible should
// they not be redeemed, and a participant who can redeem before or after the
// locktime. The participant can redeem at any time after the initiation
// transaction is mined if they have the secret that hashes to the secret hash.
// Otherwise, the initiator can refund funds any time after the locktime.
//
// This contract has no limits on gas used for any transactions.
//
// This contract cannot be used by other contracts or by a third party mediating
// the swap or multisig wallets.
contract ERC20Swap {
    bytes4 private constant TRANSFER_FROM_SELECTOR = bytes4(keccak256("transferFrom(address,address,uint256)"));
    bytes4 private constant TRANSFER_SELECTOR = bytes4(keccak256("transfer(address,uint256)"));
    
    address public immutable token_address;

    // State is a type that hold's a contract's state. Empty is the uninitiated
    // or null value.
    enum State { Empty, Filled, Redeemed, Refunded }

    struct Record {
        State state;
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

    // Contract is the information necessary for initialization and redemption
    // or refund. The Contract itself is not stored on-chain. Instead, a key
    // unique to the Contract is generated from the Contract data and keys
    // the swap record.
    struct Contract {
        bytes32 secretHash;
        address initiator;
        uint64 refundTimestamp;
        address participant;
        uint64 value;
    }

    // contractKey generates a key hash which commits to the contract data. The
    // generated hash is used as a key in the swaps map.
    function contractKey(Contract calldata c) public pure returns (bytes32) {
        return sha256(bytes.concat(c.secretHash, bytes20(c.initiator), bytes20(c.participant), bytes8(c.value), bytes8(c.refundTimestamp)));
    }

    // Redemption is the information necessary to redeem a Contract. Since we
    // don't store the Contract itself, it must be provided as part of the
    // redemption.
    struct Redemption {
        Contract c;
        bytes32 secret;
    }

    function secretValidates(bytes32 secret, bytes32 secretHash) public pure returns (bool) {
        return sha256(bytes.concat(secret)) == secretHash;
    }

    constructor(address token) {
        token_address = token;
    }

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender, "sender != origin");
        _;
    }

    // retrieveRecord retrieves the current swap record for the contract.
    function retrieveRecord(Contract calldata c)
        private view returns (bytes32, bytes32, uint256)
    {
        bytes32 k = contractKey(c);
        bytes32 record = swaps[k];
        return (k, record, uint256(record));
    }

    // state returns the current state of the swap.
    function state(Contract calldata c)
        public view returns(Record memory)
    {
        (, bytes32 record, uint256 blockNum) = retrieveRecord(c);
        Record memory r;
        if (blockNum == 0) {
            r.state = State.Empty;
        } else if (record == RefundRecord) {
            r.state = State.Refunded;
        } else if (secretValidates(record, c.secretHash)) {
            r.state = State.Redeemed;
            r.secret = record;
        } else {
            r.state = State.Filled;
            r.blockNumber =  blockNum;
        }
        return r;
    }

     // initiate initiates an array of Contracts.
    function initiate(Contract[] calldata contracts)
        public
        payable
        senderIsOrigin()
    {
        uint initVal = 0;
        for (uint i = 0; i < contracts.length; i++) {
            Contract calldata c = contracts[i];

            require(c.value > 0, "0 val");
            require(c.refundTimestamp > 0, "0 refundTimestamp");

            bytes32 k = contractKey(c);
            bytes32 record = swaps[k];
            require(record == bytes32(0), "swap not empty");

            record = bytes32(block.number);
            require(!secretValidates(record, c.secretHash), "hash collision");
            
            swaps[k] = record;

            initVal += c.value * 1 gwei;
        }

        bool success;
        bytes memory data;
        (success, data) = token_address.call(abi.encodeWithSelector(TRANSFER_FROM_SELECTOR, msg.sender, address(this), initVal));
        require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer from failed');
    }

    // isRedeemable returns whether or not a swap identified by secretHash
    // can be redeemed using secret.
    function isRedeemable(Contract calldata c)
        public
        view
        returns (bool)
    {
        (, bytes32 record, uint256 blockNum) = retrieveRecord(c);
        return blockNum != 0 && blockNum <= block.number && !secretValidates(record, c.secretHash);
    }

    // redeem redeems a Contract. It checks that the sender is not a contract,
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

            require(r.c.participant == msg.sender, "not authed");

            (bytes32 k, bytes32 record, uint256 blockNum) = retrieveRecord(r.c);

            // To be redeemable, the record needs to represent a valid block
            // number.
            require(blockNum > 0 && blockNum < block.number, "unfilled swap");

            // Can't already be redeemed.
            require(!secretValidates(record, r.c.secretHash), "already redeemed");

            // Are they presenting the correct secret?
            require(secretValidates(r.secret, r.c.secretHash), "invalid secret");

            swaps[k] = r.secret;
            amountToRedeem += r.c.value * 1 gwei;
        }

        bool success;
        bytes memory data;
        (success, data) = token_address.call(abi.encodeWithSelector(TRANSFER_SELECTOR, msg.sender, amountToRedeem));
        require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer failed');
    }


    // refund refunds a Contract. It checks that the sender is not a contract
    // and that the refund time has passed. msg.value is transfered from the
    // contract to the sender = Contract.participant.
    //
    // It is important to note that this also uses call.value which comes with
    // no restrictions on gas used. See redeem for more info.
    function refund(Contract calldata c)
        public
        senderIsOrigin()
    {
        // Is this contract even in a refundable state?
        require(block.timestamp >= c.refundTimestamp, "locktime not expired");

        // Retrieve the record.
        (bytes32 k, bytes32 record, uint256 blockNum) = retrieveRecord(c);

        // Is this swap initialized?
        require(blockNum > 0 && blockNum < block.number, "swap not active");

        // Is it already redeemed?
        require(!secretValidates(record, c.secretHash), "swap already redeemed");

        // Is it already refunded?
        require(record != RefundRecord, "swap already refunded");

        swaps[k] = RefundRecord;

        bool success;
        bytes memory data;
        (success, data) = token_address.call(abi.encodeWithSelector(TRANSFER_SELECTOR, msg.sender, c.value));
        require(success && (data.length == 0 || abi.decode(data, (bool))), 'transfer failed');
    }
}
