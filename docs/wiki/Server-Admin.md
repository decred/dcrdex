# Server Settings

## Exchange Settings

### Admin HTTP JSON API

See <https://github.com/decred/dcrdex/blob/6693bc57283d4cf5b451778091aa1c1b20cb9187/server/admin/server.go#L145>

### Markets JSON Settings File

```text
{
    "markets" (array): Array of market objects.
    [
        {
            "base" (string): The coin ticker shorthand followed by network. i.e. DCR_testnet
            "quote" (string): The coin ticker shorthand followed by network. i.e. BTC_testnet
            "epochDuration" (int): The length of one epoch in milliseconds
            "marketBuyBuffer" (float): A coefficient that when multiplied by the market's lot size specifies the minimum required amount for a market buy order
        },...
    ],
    "assets" (object): Map of coin ticker shorthand followed by network of the base asset to an asset object.
    {
        "[TICKER_network]": {
            "bip44symbol" (string): The coin ticker. i.e. dcr
            "network" (string): The network the coin daemon is running on. i.e. testnet
            "lotSize" (int): The amount of basic units of a coin in one lot
            "rateStep" (int): The price rate increment in basic units of this coin
            "maxFeeRate" (int): The maximum fee rate for swap transactions
            "swapConf" (int): The minimum confirmations before acting on a swap transaction
            "configPath" (string): The path to the coin daemon's config file or ipc file in the case of Ethereum
        },...
    }
}
```
