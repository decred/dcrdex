# NodeRelay

NodeRelay is a system for connecting to nodes on remote private machines.
This is done through a WebSockets-based reverse tunnel.

1. Run a full node on a home or otherwise private server. Coordinate credentials
for RPC requests (`rcpuser`, `rpcpassword`) with the server operator.

2. The server operator will expose the NodeRelay through an external IP address.

1. The server operator generates a **relay ID**, which can be any string without
whitespace. Each asset backend that will connect through NodeRelay will need its
own **relay ID**.

1. The server operator modifies the asset configuration to specify a relay ID
for each asset for which a noderelay is needed.

1. The server operator specifies a `noderelayaddr` in as part of the dcrdex
configuration. This is the external address at which source nodes will contact
the server.

1. Upon starting, the server will generate and store a **relayfile** to a
directory located by default at `~/.dcrdex/data/mainnet/noderelay/relay-files/`,
with a file name of e.g. `btc_0405f1069d352a0f.relayfile`. Send this file to
the private server. The **relayfile** is good until the relay ID is changed or
a new TLS key-cert pair is generated.

1. On the private server, run `sourcenode`, pointing at the relay file with
`--relayfile` and setting the node RPC port with `--port`. Specify a TLS
certificate for full node RPC with `--localcert`, if required.
    ```
    ./sourcenode --relayfile btc_0405f1069d352a0f.relayfile --port 8332
    ```
