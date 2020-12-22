# DCRDEX Test Harness

The harness is a collection of tmux scripts that collectively creates a
sandboxed environment for testing dex swap transactions.

## Dependencies

The dcrdex harness depends on 2 other harnesses: [DCR Test Harness](../dcr/README.md)
and [BTC Simnet Test Harness](../btc/README.md) to run.

## Using

The [DCR Test Harness](../dcr/README.md) and [BTC Simnet Test Harness](../btc/README.md)
must be running to use the dcrdex harness.

The dcrdex config file created and used by the harness sets
`pgdbname=dcrdex_simnet_test` and `rpclisten=127.0.0.1:17232`.
You can override these or set additional config opts by passing them as
arguments when executing the harness script e.g.:

```sh
./harness.sh --pgpass=dexpass
```

The harness script will drop any existing `dcrdex_simnet_test` PostgreSQL db
and create a fresh database for the session. The script will also create a
markets.json file referencing dcr and btc node config files created by the
respective node harnesses; and start a dcrdex instance listening at
`127.0.0.1:17232` or any address you specify in cli args.

The rpc cert for the dcrdex instance will be created in `~/dextest/dcrdex/rpc.cert`
with the following content:

```
-----BEGIN CERTIFICATE-----
MIICpTCCAgagAwIBAgIQZMfxMkSi24xMr4CClCODrzAKBggqhkjOPQQDBDBJMSIw
IAYDVQQKExlkY3JkZXggYXV0b2dlbmVyYXRlZCBjZXJ0MSMwIQYDVQQDExp1YnVu
dHUtcy0xdmNwdS0yZ2ItbG9uMS0wMTAeFw0yMDA2MDgxMjM4MjNaFw0zMDA2MDcx
MjM4MjNaMEkxIjAgBgNVBAoTGWRjcmRleCBhdXRvZ2VuZXJhdGVkIGNlcnQxIzAh
BgNVBAMTGnVidW50dS1zLTF2Y3B1LTJnYi1sb24xLTAxMIGbMBAGByqGSM49AgEG
BSuBBAAjA4GGAAQApXJpVD7si8yxoITESq+xaXWtEpsCWU7X+8isRDj1cFfH53K6
/XNvn3G+Yq0L22Q8pMozGukA7KuCQAAL0xnuo10AecWBN0Zo2BLHvpwKkmAs71C+
5BITJksqFxvjwyMKbo3L/5x8S/JmAWrZoepBLfQ7HcoPqLAcg0XoIgJjOyFZgc+j
gYwwgYkwDgYDVR0PAQH/BAQDAgKkMA8GA1UdEwEB/wQFMAMBAf8wZgYDVR0RBF8w
XYIadWJ1bnR1LXMtMXZjcHUtMmdiLWxvbjEtMDGCCWxvY2FsaG9zdIcEfwAAAYcQ
AAAAAAAAAAAAAAAAAAAAAYcEsj5QQYcEChAABYcQ/oAAAAAAAAAYPqf//vUPXDAK
BggqhkjOPQQDBAOBjAAwgYgCQgFMEhyTXnT8phDJAnzLbYRktg7rTAbTuQRDp1PE
jf6b2Df4DkSX7JPXvVi3NeBru+mnrOkHBUMqZd0m036aC4q/ZAJCASa+olu4Isx7
8JE3XB6kGr+s48eIFPtmq1D0gOvRr3yMHrhJe3XDNqvppcHihG0qNb0gyaiX18Cv
vF8Ti1x2vTkD
-----END CERTIFICATE-----
```

A `curl` script is created in `~/dextest/dcrdex/dexadm` for sending requests to
the DEX API e.g. `~/dextest/dcrdex/dexadm ping` or
`~/dextest/dcrdex/dexadm notifyall @path/to/file.txt`.

## Harness control

To quit the harness, run `~/dextest/dcrdex/quit` from any terminal/tmux window,
or from within the tmux window, use ctrl+c to stop the running dcrdex instance
and then `tmux kill-session` (or `exit`) to exit the harness.

## Dev Stuff

If things aren't looking right, you may need to look at the harness tmux window
to see errors. Examining the dcrdex logs to look for errors is usually a good
first debugging step.

An unfortunate issue that will pop up if you're fiddling with the script is
zombie dcrdex processes preventing the harness nodes from binding to the
specified ports. You'll have to manually hunt down the zombie PIDs and `kill`
them if this happens.

Don't forget that there may be tests that rely on the existing script's
specifics to function correctly. Changes must be tested throughout dcrdex.
