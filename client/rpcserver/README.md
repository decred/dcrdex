# rpcserver

The `rpcserver` package provides a JSON-RPC server for the Bison Wallet
(bisonw) client. It enables remote control of the wallet via HTTPS with Basic
authentication.

## Architecture

All commands are sent as JSON-RPC POST requests to a single HTTPS endpoint
(`/`). The server also exposes a WebSocket endpoint (`/ws`) for persistent
connections with the same authentication.

### Request Format

```json
{
  "type": 1,
  "route": "trade",
  "id": 1,
  "payload": {
    "appPass": "mypassword",
    "host": "dex.decred.org",
    "isLimit": true,
    "sell": true,
    "base": 42,
    "quote": 0,
    "qty": 10000000,
    "rate": 5000000,
    "tifNow": false
  }
}
```

- `type`: Always `1` (request).
- `route`: The command name (e.g., `"trade"`, `"wallets"`).
- `id`: An integer for matching responses to requests.
- `payload`: A typed JSON object specific to each route. Routes that take no
  parameters (e.g., `version`, `wallets`) use an empty or null payload.

### Response Format

```json
{
  "type": 2,
  "id": 1,
  "payload": {
    "result": "...",
    "error": null
  }
}
```

## Authentication

HTTP Basic authentication over TLS. Configure the username and password when
starting `bisonw` with `--rpcuser` and `--rpcpass`.

## Handler Pattern

Each route maps to a handler function with the signature:

```go
func handler(s *RPCServer, msg *msgjson.Message) *msgjson.ResponsePayload
```

Each handler unmarshals its own typed parameter struct from the message payload.
For example, the trade handler unmarshals a `TradeParams` struct. Routes that
take no parameters ignore the message.

## File Organization

| File | Contents |
|------|----------|
| `rpcserver.go` | Server struct, TLS setup, HTTP handling, auth middleware |
| `handlers.go` | Route constants, routes map, help messages, handler functions |
| `types.go` | Shared types (`VersionResponse`), per-route typed param structs, response types |
| `swagger.go` | Embedded OpenAPI spec, Swagger UI endpoint |
| `openapi.yaml` | OpenAPI 3.0.1 specification |

## bwctl Usage

`bwctl` is the command-line client for the RPC server.

```bash
# Get version
bwctl version

# Initialize
bwctl init

# Create a DCR wallet
bwctl newwallet 42 dcrwalletspv

# Check wallets
bwctl wallets

# Place a trade
bwctl trade dex.decred.org true true 42 0 10000000 5000000 false '{}'
```

## Route Summary

| Category | Routes |
|----------|--------|
| System | `help`, `init`, `version`, `login`, `logout` |
| Wallet | `newwallet`, `openwallet`, `closewallet`, `togglewalletstatus`, `wallets`, `rescanwallet` |
| Trading | `trade`, `multitrade`, `cancel`, `myorders`, `orderbook`, `exchanges` |
| Transactions | `withdraw`, `send`, `abandontx`, `appseed`, `deletearchivedrecords`, `notifications`, `txhistory`, `wallettx`, `withdrawbchspv` |
| DEX | `discoveracct`, `getdexconfig`, `bondassets`, `postbond`, `bondopts` |
| Market Making | `startmmbot`, `stopmmbot`, `mmstatus`, `mmavailablebalances`, `updaterunningbotcfg`, `updaterunningbotinv` |
| Staking | `stakestatus`, `setvsp`, `purchasetickets`, `setvotingprefs` |
| Bridge | `bridge`, `checkbridgeapproval`, `approvebridgecontract`, `pendingbridges`, `bridgehistory`, `supportedbridges`, `bridgefeesandlimits` |
| Multisig | `paymentmultisigpubkey`, `sendfundstomultisig`, `signmultisig`, `refundpaymentmultisig`, `viewpaymentmultisig`, `sendpaymentmultisig` |
| Peers | `walletpeers`, `addwalletpeer`, `removewalletpeer` |

## Swagger UI

When the RPC server is running, visit `https://localhost:5757/swagger` for
interactive API documentation. The raw OpenAPI spec is available at
`https://localhost:5757/openapi.yaml`.
