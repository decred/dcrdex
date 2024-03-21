// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.18;

// ETHBond is a contract that allows users to post fidelity bonds. In order to
// save on gas costs, the actual bonds are not stored in the contract, but rather
// a commitment to the bond is stored, which is a hash of the user's address and
// the current bonds they have posted. The actual values of the bonds should be
// stored off chain, but they can be retrieved by checking the event logs of the
// contract. The first bond for an account must be posted by calling createBond.
// Any further updates to the bonds for an account, including adding, extending,
// and refunding bonds should be done by calling updateBonds.
contract ETHBond {
    event BondCreated(bytes32 indexed acctID, uint indexed index, Bond bond);
    event BondUpdated(bytes32 indexed acctID, uint indexed index, Bond[] oldBonds, Bond[] newBonds);

    struct Bond {
        uint256 value;
        uint64 locktime;
    }

    // bondCommit is a mapping from account ID to a list of commitments.
    // A commitment is the hash of the account owner's address and the
    // bonds currently posted for their account. Ideally this would be
    // a mapping(bytes32 => bytes32) with only one commitment, but this
    // would allow a troublemaker to post a bond blocking the actual
    // account owner from doing so.
    mapping(bytes32 => bytes32[]) public bondCommits;

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender, "sender != origin");
        _;
    }

    // hashBonds creates a hash that commits to the sender and the provided bonds.
    function hashBonds(Bond[] memory bonds, address sender) public pure returns (bytes32) {
        return keccak256(abi.encode(sender, bonds));
    }

    // createBond creates a new bond for the provided account ID. This should
    // be used to create the initial bond for an account. The index of the
    // commitment should be retrieved from the BondCreated event, and then used
    // when calling updateBonds.
    function createBond(bytes32 acctID, Bond calldata bond) external payable senderIsOrigin() {
        require(msg.value == bond.value, "insufficient funds");
        Bond[] memory bonds = new Bond[](1);
        bonds[0] = bond;
        bondCommits[acctID].push(hashBonds(bonds, msg.sender));
        emit BondCreated(acctID, bondCommits[acctID].length - 1, bond);
    }

    // updateBond updates the bonds for the provided account ID and index. The
    // bonds currently stored for this commitment must be passed in as oldBonds,
    // and the new bonds should be passed in as newBonds.
    // - The bonds must be sorted by decreasing locktime.
    // - The value of oldBonds that are reused in newBonds must be locked for
    //   the same amount of time or longer.
    // - If the total value of new bonds is greater than the total value of the
    //   old bonds, the difference must be sent as a payment to this contract.
    // - If the total value of new bonds is less than the total value of the old
    //   bonds, the difference will be refunded to the sender, but only if the
    //   locktimes of the bonds to be refunded has passed.
    function updateBonds(bytes32 acctID, Bond[] calldata oldBonds, Bond[] calldata newBonds, uint index) external payable senderIsOrigin() {
        emit BondUpdated(acctID, index, oldBonds, newBonds);

        bytes32 commit = bondCommits[acctID][index];
        require(oldBonds.length > 0, "need old bonds");
        require(hashBonds(oldBonds, msg.sender) == commit, "incorrect commit");

        // Quick path for pure refund.
        if (newBonds.length == 0) {
            uint256 refund = 0;
            for (uint i = 0; i < oldBonds.length; i++) {
                require(oldBonds[i].locktime < block.timestamp, "cannot refund unexpired bond");
                refund += oldBonds[i].value;
            }
            bondCommits[acctID][index] = bytes32(0);
            (bool ok, ) = payable(msg.sender).call{value: refund}("");
            require(ok == true, "full refund failed");
            return;
        }

        uint64 stamp = newBonds[0].locktime;
        uint256 newFunds = 0;
        uint256 totalBondsValue = 0;
        uint256 oldBondsIndex = 0;

        Bond[] memory bondReserves = oldBonds;

        // Fund new bonds.
        for (uint i = 0; i < newBonds.length; i++) {
            Bond memory newBond = newBonds[i];
            require(newBond.locktime <= stamp, "bonds not sorted");
            stamp = newBond.locktime;
            uint256 toFund = newBond.value;
            totalBondsValue += toFund;

            while (oldBondsIndex < bondReserves.length) {
                require(bondReserves[oldBondsIndex].locktime <= newBond.locktime, "illegal lock time shortening");
                if (bondReserves[oldBondsIndex].value >= toFund) {
                    bondReserves[oldBondsIndex].value -= toFund;
                    toFund = 0;
                    break;
                } else {
                    toFund -= bondReserves[oldBondsIndex].value;
                    bondReserves[oldBondsIndex].value = 0;
                    oldBondsIndex++;
                }
            }

            newFunds += toFund;
        }

        require (msg.value == newFunds, "incorrect amount");

        bondCommits[acctID][index] = hashBonds(newBonds, msg.sender);

        uint256 unextended = 0;
        for (; oldBondsIndex < bondReserves.length; oldBondsIndex++) {
            if (bondReserves[oldBondsIndex].value > 0) {
                require(bondReserves[oldBondsIndex].locktime < block.timestamp, "unextended bond is not old enough to refund");
                unextended += bondReserves[oldBondsIndex].value;
            }
        }

        if (unextended > 0) {
            (bool ok, ) = payable(msg.sender).call{value: unextended}("");
            require(ok == true, "refund failed");
        }
    }
}
