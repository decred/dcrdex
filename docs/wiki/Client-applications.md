## Client Applications and the Core Package

### Applications

There are two client applications.

#### dexc

The **browser-based GUI** (a.k.a. "the app") offers a familiar exchange
experience in your browser. The app is really just a one-client web server that
you run and connect to on the same machine. The market view allows you to see
the market's order book in sorted lists or as a depth chart. You can place your
order and monitor its status in the same market view. The GUI application is
managed by the **dexc** utility in *client/cmd/dexc*.

Most users will only need to use this application.

See the README for instructions on obtaining and starting **dexc**.

#### dexcctl

The **dexcctl** utility enables trading via CLI. Commands are parsed and
issued to **Core** for execution. **dexcctl** also requires **dexc**.

### Core client Go language package

For developers, the `decred.org/dcrdex/client/core` Go language package provides
the `Core` client type, which offers an intuitive programmer interface, with
methods for creating wallets, registering DEX accounts, viewing markets, and
performing trades.
