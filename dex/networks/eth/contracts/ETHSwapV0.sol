// SPDX-License-Identifier: BlueOak-1.0.0
pragma solidity >=0.7.0 <0.9.0;
contract ETHSwap {
    enum State { Empty, Filled, Redeemed, Refunded }

    struct Swap {
        uint initBlockNumber;
        uint refundBlockTimestamp;
        bytes32 secretHash;
        bytes32 secret;
        address initiator;
        address participant;
        uint256 value;
        State state;
    }

    mapping(bytes32 => Swap) public swaps;

    constructor() {}

    modifier isRefundable(bytes32 secretHash, address refunder) {
        require(swaps[secretHash].state == State.Filled);
        require(swaps[secretHash].initiator == refunder);
        uint refundBlockTimestamp = swaps[secretHash].refundBlockTimestamp;
        require(block.timestamp >= refundBlockTimestamp);
        _;
    }

    modifier isRedeemable(bytes32 secretHash, bytes32 secret, address redeemer) {
        require(swaps[secretHash].state == State.Filled);
        require(swaps[secretHash].participant == redeemer);
        require(sha256(abi.encodePacked(secret)) == secretHash);
        _;
    }

    modifier isNotInitiated(bytes32 secretHash) {
        require(swaps[secretHash].state == State.Empty);
        _;
    }

    modifier hasNoNilValues(uint refundTime) {
        require(msg.value > 0);
        require(refundTime > 0);
        _;
    }

    modifier senderIsOrigin() {
        require(tx.origin == msg.sender);
        _;
    }

    function swap(bytes32 secretHash)
        public view returns(Swap memory)
    {
        return swaps[secretHash];
    }

    function initiate(uint refundTimestamp, bytes32 secretHash, address participant)
        public
        payable
        hasNoNilValues(refundTimestamp)
        senderIsOrigin()
        isNotInitiated(secretHash)
    {
        swaps[secretHash].initBlockNumber = block.number;
        swaps[secretHash].refundBlockTimestamp = refundTimestamp;
        swaps[secretHash].secretHash = secretHash;
        swaps[secretHash].initiator = msg.sender;
        swaps[secretHash].participant = participant;
        swaps[secretHash].value = msg.value;
        swaps[secretHash].state = State.Filled;
    }

    function redeem(bytes32 secret, bytes32 secretHash)
        public
        senderIsOrigin()
        isRedeemable(secretHash, secret, msg.sender)
    {
        payable(msg.sender).transfer(swaps[secretHash].value);
        swaps[secretHash].state = State.Redeemed;
        swaps[secretHash].secret = secret;
    }

    function refund(bytes32 secretHash)
        public
        senderIsOrigin()
        isRefundable(secretHash, msg.sender)
    {
        payable(msg.sender).transfer(swaps[secretHash].value);
        swaps[secretHash].state = State.Refunded;
    }
}
