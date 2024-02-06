// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.18;

// ETHBond is an ethereum contract used to manage fidelity bonds. It allows
// users to bond funds until a certain locktime. When the locktime passes, the
// bond can be refunded.
// Bonds can also be updated. One or more bonds can be used to create a new bond
// with a longer locktime. The new bond can have a higher value than all of the
// bonds used to create it, requiring the user the send additional funds. The
// new bond can also have a lower value than the bonds used to create it, in
// which case a "change" bond is also created with a locktime equal to the
// locktime of the earliest expiring bond used to create it.
contract ETHBond {

    // Bond holds information about a single bond.
    struct Bond {
        uint value;
        uint initBlockNumber;
        uint64 locktime;
        address owner;

        // prevBondID and nextBondID are used to create a doubly-linked list
        // of bonds for an account. This is needed to be able to lookup all of
        // the bonds for an account.
        bytes32 prevBondID;
        bytes32 nextBondID;
    }

    // Account holds all the bonds for a single account.
    struct Account {
        // firstBondID is the bondID of the first bond in the doubly-linked list.
        bytes32 firstBondID;
        mapping (bytes32 => Bond) bonds;
    }

    // Mapping from accountID => Account.
    mapping(bytes32 => Account) accounts;

    // senderIsOrigin ensures that this contract cannot be used by other
    // contracts, which reduces possible attack vectors.
    modifier senderIsOrigin() {
        require(tx.origin == msg.sender, "sender != origin");
        _;
    }

    // BondInfo is used to return information about a bond.
    struct BondInfo {
        bytes32 bondID;
        uint value;
        uint initBlockNumber;
        uint64 locktime;
        address owner;
    }

    // bondsForAccount returns all the bonds for an account.
    function bondsForAccount(bytes32 accountID) external view returns (BondInfo[] memory) {
        Account storage account = accounts[accountID];

        // Arrays in memory cannot be dynamically allocated, so we need to first
        // count the number of bonds.
        uint numBonds = 0;
        bytes32 bondID = account.firstBondID;
        while (bondID != bytes32(0)) {
            numBonds++;
            bondID = account.bonds[bondID].nextBondID;
        }

        BondInfo[] memory bonds = new BondInfo[](numBonds);
        bondID = account.firstBondID;
        for (uint i = 0; i < numBonds; i++) {
            Bond storage bond = account.bonds[bondID];
            bonds[i] = BondInfo(bondID, bond.value, bond.initBlockNumber, bond.locktime, bond.owner);
            bondID = bond.nextBondID;
        }

        return bonds;
    }

    // bondData looks up a bond by accountID and bondID and returns all the information
    // about the bond. 
    function bondData(bytes32 accountID, bytes32 bondID) external view returns (uint value, uint initBlockNumber, uint64 locktime, address owner) {
        Account storage account = accounts[accountID];
        Bond storage bond = account.bonds[bondID];
        return (bond.value, bond.initBlockNumber, bond.locktime, bond.owner);
    }

    // insertBond is a helper function used to insert a new bond into the
    // bonds map and also place it at the front of the doubly-linked list.
    function insertBond(Account storage account, bytes32 bondID, uint64 locktime, uint value) internal {
        Bond storage bond = account.bonds[bondID];
        bond.value = value;
        bond.initBlockNumber = block.number;
        bond.locktime = locktime;
        bond.owner = msg.sender;

        if (account.firstBondID == bytes32(0)) {
            account.firstBondID = bondID;
        } else {
            Bond storage firstBond = account.bonds[account.firstBondID];
            bond.nextBondID = account.firstBondID;
            firstBond.prevBondID = bondID;
            account.firstBondID = bondID;
        }
    }

    // createBond creates a new bond for the accountID. The bondID must be unique.
    function createBond(bytes32 accountID, bytes32 bondID, uint64 locktime) external payable senderIsOrigin() {
        Account storage account = accounts[accountID];
        Bond storage bond = account.bonds[bondID];
        require(bond.value == 0, "bond already exists");
        require(msg.value > 0, "value must be > 0");

        insertBond(account, bondID, locktime, msg.value);
    }

    // verifyBondLocktimesAndOwner checks that the bonds are sorted by locktime,
    // the updated locktime, is greater than the locktime of the latest bond,
    // and that the owner of all the bonds is the msg.sender. It also returns the
    // total value of the bonds.
    // A separate function is created for this to avoid a stack too deep error.
    function verifyBondLocktimesAndOwner(mapping(bytes32 => Bond) storage accountBonds, bytes32[] memory bondsToUpdate, uint64 locktime) internal view returns (bool, uint, string memory){   
        uint totalValue = 0;
        uint64 previousLocktime = 0;
        for (uint i = 0; i < bondsToUpdate.length; i++) {
            bytes32 bondID = bondsToUpdate[i];
            Bond storage bond = accountBonds[bondID];
            if (bond.value == 0) {
                return (false, 0, "bond does not exist");
            }
            if (bond.owner != msg.sender) {
                return (false, 0, "bond owner != msg.sender");
            }
            if (bond.locktime < previousLocktime) {
                return (false, 0, "bonds must be sorted by locktime");
            }
            totalValue += bond.value;
            previousLocktime = bond.locktime;
        }

        Bond storage latestExpiringBond = accountBonds[bondsToUpdate[bondsToUpdate.length - 1]];
        if (latestExpiringBond.locktime >= locktime) {
            return (false, 0, "locktime cannot decrease");
        }

        return (true, totalValue, "");
    }

    // verifyBondUpdate checks if the parameters for updateBond are valid, and if not, returns
    // the reason. It also returns the total value of the bonds.
    function verifyBondUpdate(bytes32 accountID, bytes32[] memory bondsToUpdate, bytes32[] calldata newBondIDs, uint value, uint64 locktime, uint msgValue) public view returns (bool, uint, string memory) {
        mapping(bytes32 => Bond) storage accountBonds = accounts[accountID].bonds;

        (bool valid, uint totalValue, string memory reason) = verifyBondLocktimesAndOwner(accountBonds, bondsToUpdate, locktime);
        if (!valid) {
            return (false, locktime, reason);
        }

        if (totalValue > value) {
            Bond storage earliestExpiringBond = accountBonds[bondsToUpdate[0]];
            if (earliestExpiringBond.value < totalValue - value) {
                return (false, totalValue, "earliest bond is too small");
            }
            if (newBondIDs.length != 2) {
                return (false, totalValue, "incorrect number of new bonds");
            }
            if (msgValue != 0) {
                return (false, totalValue, "no funds required");
            }
        } else {
            if (newBondIDs.length != 1) {
                return (false, totalValue, "incorrect number of new bonds");
            }
            if (msgValue + totalValue != value) {
                return (false, totalValue, "insufficient funds");
            }
        }

        for (uint i = 0; i < newBondIDs.length; i++) {
            if (accountBonds[newBondIDs[i]].value != 0) {
                return (false, totalValue, "new bond already exists");
            }
        }

        return (true, totalValue, "");
    }

    // updateBond uses the funds locked in one or more bonds to create a new
    // bond with a longer locktime. If the total value of the bonds is greater
    // than the new bond, a "change" bond will be created with the remaining
    // amount of value with a lockTime of the earliest expiring bond.
    //
    // Requirements:
    // - bondsToUpdate must be sorted by locktime.
    // - The locktime of the new bond must be greater than the locktime of the
    //   latest bond.
    // - The owner of all the bonds must be the msg.sender.
    // - If a "change" bond must be created, the value of the earliest expiring
    //   must be less than the total value of the bonds minus the value of the
    //   new bond.
    // - newBondIDs must contain two IDs if a change is required, and one
    //   otherwise.
    function updateBonds(bytes32 accountID, bytes32[] calldata bondsToUpdate, bytes32[] calldata newBondIDs, uint value, uint64 locktime) external payable senderIsOrigin() {
        (bool valid, uint totalValue, string memory reason) = verifyBondUpdate(accountID, bondsToUpdate, newBondIDs, value, locktime, msg.value);
        require(valid, reason);
        
        Account storage account = accounts[accountID];

        if (totalValue > value) {
            Bond storage earliestExpiringBond = account.bonds[bondsToUpdate[0]];
            insertBond(account, newBondIDs[0], earliestExpiringBond.locktime, totalValue - value);
            insertBond(account, newBondIDs[1], locktime, value);
        } else {
            insertBond(account, newBondIDs[0], locktime, value);
        }

        for (uint i = 0; i < bondsToUpdate.length; i++) {
            removeBond(account, bondsToUpdate[i]);
        }
    }

    // bondRefundable checks if a bond is refundable.
    function bondRefundable(bytes32 accountID, bytes32 bondID) public view returns (bool) {
        Account storage account = accounts[accountID];
        Bond storage bond = account.bonds[bondID];
        return bond.value > 0 && bond.locktime < block.timestamp;
    }

    // removeBond is a helper function used to remove a bond from an account.
    // It removes the bond from the mapping and the doubly linked list.
    function removeBond(Account storage account, bytes32 bondID) internal {
        Bond storage bond = account.bonds[bondID];

        if (bond.prevBondID != bytes32(0)) {
            account.bonds[bond.prevBondID].nextBondID = bond.nextBondID;
        } else {
            account.firstBondID = bond.nextBondID;
        }

        if (bond.nextBondID != bytes32(0)) {
            account.bonds[bond.nextBondID].prevBondID = bond.prevBondID;
        }

        delete account.bonds[bondID];
    }

    // refundBond refunds a bond to its owner and deletes it from the account.
    function refundBond(bytes32 accountID, bytes32 bondID) external senderIsOrigin() {
        require(bondRefundable(accountID, bondID), "bond is not refundable");

        Account storage account = accounts[accountID];
        Bond storage bond = account.bonds[bondID];

        uint256 value = bond.value;
        address owner = bond.owner;

        removeBond(account, bondID);

        (bool ok, ) = payable(owner).call{value: value}("");
        require(ok == true, "transfer failed");
    }
}
