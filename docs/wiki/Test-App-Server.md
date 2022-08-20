# Test App Server

The client's GUI app can connect to a test app server. This allows testing of
the UI without the need to configure **dcrdex**, **dexc** or the wallets
and server backends.

First, make sure the static web page assets are rebuilt.
Navigate to `client/webserver/site` and run `npm i && npm run build`.

To run the test server, navigate to `client/webserver` and run

```sh
go test -tags live,lgpl -timeout 60m
```

In your browser, navigate to `http://localhost:54321`.
