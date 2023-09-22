// SPDX-License-Identifier: BlueOak-1.0.0
// pragma should be as specific as possible to allow easier validation.
pragma solidity = 0.8.18;

contract MultiBalanceV0 {
    bytes4 private constant BALANCE_OF_SELECTOR = bytes4(keccak256("balanceOf(address)"));

    // balances gets the balance of the account and multiple tokens at once.
    function balances(address acct, address[] calldata tokenAddresses)
        public view
        returns (uint256[] memory)
    {

        uint256[] memory bals = new uint256[](tokenAddresses.length + 1);
        bals[0] = acct.balance;
        for (uint256 i; i < tokenAddresses.length; i++) {
            address contractAddr = tokenAddresses[i];
            bool success;
            bytes memory data;
            (success, data) = contractAddr.staticcall(abi.encodeWithSelector(BALANCE_OF_SELECTOR, acct));
            require(success, "balanceOf call failed");
            uint256 bal;
            (bal) = abi.decode(data, (uint256));
            bals[i + 1] = bal;
        }
        return bals;
    }
}
