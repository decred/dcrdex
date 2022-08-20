# BTC Wallet Recovery and Rescanning

## Reinitializing the Native BTC Wallet

If the native BTC wallet is corrupted, the dexc.log may have a message containing "connectWallet: wallet not found", such as:

> [ERR] CORE: Unable to connect to **btc** wallet (start and sync wallets BEFORE starting dex!): **connectWallet: wallet not found**

Note **btc** in the above message, and **wallet not found**.  This may also say something about db corruption or similar errors, which may also be addressed as follows.

In this case, the Wallets page (/wallets) will still show the wallet since it was previously configured, just that it is not synchronized and cannot be unlocked:

![wallets view](https://user-images.githubusercontent.com/9373513/149597739-06318c29-f6be-4837-bb6f-2055971be8cf.png)

This may be solvable by simply clicking the gears to open the settings box, and then click Submit:

![wallet settings](https://user-images.githubusercontent.com/9373513/149597817-abbcbf5f-d05f-4b48-996f-485030d2a74c.png)

The dexc.log should say:

> [INF] CORE: Initializing a built-in btc wallet

The wallet will begin re-synchronizing.  This may take several minutes.

If this process results in errors, it may be necessary to go to the folder containing the wallet data and **delete the wallet.db so it may be regenerated** from your application seed.  First SHUTDOWN DEX. Next, for mainnet, on Linux, this would be in the folder `~/.dexc/mainnet/assetdb/btc/mainnet/`.  There may be a wallet.db file in this folder, which you should rename wallet.db.bak.  Now startup and login to DEX.  Go to the Wallets page to see wallet status, which should re-synchronize.  Check notifications and logs for any errors.

## Rescanning the wallet

There will be a Rescan button added to the Wallets page, but for now, you can force the wallet to rescan in one of two ways.  First, SHUTDOWN DEX.  Then do one of the following:

1. Delete the wallet.db file in the "assetdb/btc" folder as described at the end of the previous section.  Start back up and log in. Follow the instructions in [the Reinitializing the Native BTC Wallet section](#reinitializing-the-native-btc-wallet) to reinitialize the wallet and begin a resync.
2. OR (**safest option**) use the `dropwtxmgr` tool on that wallet.db file.  See the instructions in <https://github.com/btcsuite/btcwallet/blob/master/docs/force_rescans.md>.  You will build that tool as described there and then run it on DEX's btc wallet.db. For example `dropwtxmgr --db ~/.dexc/mainnet/assetdb/btc/mainnet/wallet.db`.  When you start DEX and login again, it will begin to rescan the wallet.

## Full reinitialize

In addition to reinitializing or rescanning the BTC wallet.db file, you may also remove all of the chain data files to force resynchronization of all blockchain data used by the neutrino service that powers the wallet.  To do this, delete all of the files in `~/.dexc/mainnet/assetdb/btc/mainnet/` (for mainnet), including neutrino.db, reg_filter_headers.bin, block_headers.bin, and wallet.db.  You may keep peers.json to help with bootstrapping when you restart, but it may be deleted too.

Next, startup DEX and go to the Wallet page. Finally, click the gears icon for the Bitcoin wallet, and click Submit after ensuring it is currently showing the Native wallet option as in the [first section's screenshot](#reinitializing-the-native-btc-wallet).
