// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.6;

interface ERC20 {
    function transfer(address recipient, uint256 amount) external returns (bool);
    function transferFrom(address sender, address recipient, uint256 amount) external returns (bool);
}


contract ERC20Swap {
    bytes4 private constant TRANSFER_FROM_SELECTOR = bytes4(keccak256("transferFrom(address,address,uint256)"));
	bytes4 private constant TRANSFER_SELECTOR = bytes4(keccak256("transfer(address,uint256)"));

    // State is a type that hold's a contract's state. Empty is the uninitiated
    // or null value.
    enum State { Empty, Filled, Redeemed, Refunded }

    // Swap holds information related to one side of a single swap. The order of
    // the struct fields is important to efficiently pack the struct into as few
    // 256-bit slots as possible to reduce gas cost. In particular, the 160-bit
    // address can pack with the 8-bit State.
    struct Swap {
        bytes32 secret;
        uint256 value;
        uint initBlockNumber;
        uint refundBlockTimestamp;
        address initiator;
        address participant;
        address token;
        State state;
    }

    // Swaps is a map of swap secret hashes to swaps. It can be read by anyone
    // for free.
    mapping(bytes32 => Swap) swaps;

    // constructor is empty. This contract has no connection to the original
    // sender after deployed. It can only be interacted with by users
    // initiating, redeeming, and refunding swaps.
    constructor() {}

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors. There is some
    // conversation in the eth community about removing tx.origin, which would
    // make this check impossible.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender);
        _;
    }

    // swap returns a single swap from the swaps map.
    function swap(bytes32 secretHash)
        public view returns(Swap memory)
    {
        return swaps[secretHash];
    }

    // Initiation is used to specify the information needed to initiatite a swap.
    // The ERC20 token address is provided separately to the initiate function.
    struct Initiation {
        uint refundTimestamp;
        bytes32 secretHash;
        address participant;
        uint value;
    }

    // initiate initiates an array of swaps all for the same ERC20 token.
    // It checks that all of the swaps have a non zero redemptionTimestamp
    // and value, and that none of the secret hashes have ever been used
    // previously. Once initiated, each swap's state is set to Filled. The
    // tokens equal to the sum of each swap's value are now in the custody of
    // the contract and can only be retrieved through redeem or refund.
    //
    // It is important to note that this calls an external contract which has no
    // restrictions on gas used. This has the potential to open the contract up
    // to a reentry attack.
    //
    // This is a writing function and requires gas. Failure or success should
    // be guaged by querying the swap and checking state after being mined. Gas
    // is expended either way.
    function initiate(Initiation[] calldata initiations, address token)
        public
        senderIsOrigin()
    {
        uint initVal = 0;
        for (uint i = 0; i < initiations.length; i++) {
            Initiation calldata initiation = initiations[i];
            Swap storage swapToUpdate = swaps[initiation.secretHash];

            require(initiation.value > 0);
            require(initiation.refundTimestamp > 0);
            require(swapToUpdate.state == State.Empty);

            swapToUpdate.initBlockNumber = block.number;
            swapToUpdate.refundBlockTimestamp = initiation.refundTimestamp;
            swapToUpdate.initiator = msg.sender;
            swapToUpdate.participant = initiation.participant;
            swapToUpdate.value = initiation.value;
            swapToUpdate.state = State.Filled;
            swapToUpdate.token = token;

            initVal += initiation.value;
        }

        ERC20 tokenContract = ERC20(token);
        tokenContract.transferFrom(msg.sender, address(this), initVal);
    }

    // Redemption is used to specify the information needed to redeem a swap.
    struct Redemption {
        bytes32 secret;
        bytes32 secretHash;
    }

    // isRedeemable returns whether or not a swap identified by secretHash
    // can be redeemed using secret.
    function isRedeemable(bytes32 secretHash, bytes32 secret)
        public
        view
        returns (bool)
    {
        Swap storage swapToRedeem = swaps[secretHash];
        return swapToRedeem.state == State.Filled &&
               swapToRedeem.participant == msg.sender &&
               sha256(abi.encodePacked(secret)) == secretHash;
    }

    // redeem redeems a contract. It checks that the sender is not a contract,
    // and that the secret hash hashes to secretHash. The ERC20 tokens are tranfered
    // from the contract to the sender.
    //
    // It is important to note that this calls an external contract which has no
    // restrictions on gas used. This has the potential to open the contract up
    // to a reentry attack.
    //
    // Any throw at any point in this function will revert the state to what it
    // was originally, to Initiated.
    //
    // This is a writing function and requires gas. Failure or success should
    // be guaged by querying the swap and checking state after being mined. Gas
    // is expended either way.
    function redeem(Redemption[] calldata redemptions)
        public
        senderIsOrigin()
    {
        uint amountToRedeem = 0;
        address token;
        for (uint i = 0; i < redemptions.length; i++) {
            Redemption calldata redemption = redemptions[i];
            Swap storage swapToRedeem = swaps[redemption.secretHash];

            if (i == 0) {
                token = swapToRedeem.token;
            } else {
                require(token == swapToRedeem.token);
            }

            require(swapToRedeem.state == State.Filled);
            require(swapToRedeem.participant == msg.sender);
            require(sha256(abi.encodePacked(redemption.secret)) == redemption.secretHash);

            swapToRedeem.state = State.Redeemed;
            swapToRedeem.secret = redemption.secret;
            amountToRedeem += swapToRedeem.value;
        }

        ERC20 tokenContract = ERC20(token);
        tokenContract.transfer(msg.sender, amountToRedeem);
    }


    // isRefundable checks that a swap can be refunded. The requirements are
    // the initiator is msg.sender, the state is Filled, and the block
    // timestamp be after the swap's stored refundBlockTimestamp.
    function isRefundable(bytes32 secretHash) public view returns (bool) {
        Swap storage swapToCheck = swaps[secretHash];
        return swapToCheck.state == State.Filled &&
               swapToCheck.initiator == msg.sender &&
               block.timestamp >= swapToCheck.refundBlockTimestamp;
    }

    // refund refunds a contract. It checks that the sender is not a contract,
    // and that the refund time has passed. An amount of ERC20 tokens equal to
    // swap.value is tranfered from the contract to the sender.
    //
    //
    // It is important to note that this calls an external contract which has no
    // restrictions on gas used. This has the potential to open the contract up
    // to a reentry attack.
    //
    // Any throw at any point in this function will revert the state to what it
    // was originally, to Initiated.
    //
    // This is a writing function and requires gas. Failure or success should
    // be guaged by querying the swap and checking state after being mined. Gas
    // is expended either way.
    function refund(bytes32 secretHash)
        public
        senderIsOrigin()
    {
        require(isRefundable(secretHash));
        Swap storage swapToRefund = swaps[secretHash];
        swapToRefund.state = State.Refunded;

        ERC20 tokenContract = ERC20(swapToRefund.token);
        tokenContract.transfer(msg.sender, swapToRefund.value);
    }
}
