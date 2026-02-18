// SPDX-License-Identifier: BlueOak-1.0.0
pragma solidity = 0.8.23;

import "@openzeppelin/contracts/token/ERC20/ERC20.sol";

// TestCaller is a contract used in tests to verify that senderIsOrigin
// rejects calls from contracts.
contract TestCaller {
    function callContract(address target, bytes calldata data) external payable {
        (bool ok, bytes memory ret) = target.call{value: msg.value}(data);
        if (!ok) {
            assembly {
                revert(add(ret, 32), mload(ret))
            }
        }
    }
}

// RevertingRecipient is a contract that reverts when receiving ETH.
// Used to test that redeemAA finalizes even when the recipient can't accept ETH.
contract RevertingRecipient {
    receive() external payable {
        revert("no ETH accepted");
    }
}

// MockERC20 is a simple ERC20 token for testing token swap paths.
contract MockERC20 is ERC20 {
    constructor() ERC20("MockToken", "MTK") {
        _mint(msg.sender, 1000000 * 10 ** 18);
    }

    function mint(address to, uint256 amount) external {
        _mint(to, amount);
    }
}
