<a id="top"/>

**Looking to contribute? We need your help** to make DEX \#1.

Nearly all development is done in Go and JavaScript. Work is coordinated
through [the repo issues](https://github.com/decred/dcrdex/issues),
so that's the best place to start.
Before beginning work, chat with us in the
[DEX Development room](https://matrix.to/#/!EzTSRQITaqHuFBDFhM:decred.org?via=decred.org&via=matrix.org&via=zettaport.com).
The pace of development is pretty fast right now, so you'll be expected to keep
your pull requests moving through the review process.

### Prerequisites for Bison Wallet Development

1. [Go 1.24 or later](https://golang.org/doc/install) is required.
2. [Node 18 or later](https://nodejs.org/en/download/). Node.js is used to bundle resources for the browser interface. It's important to note that Bison Wallet has no external JavaScript dependencies. The client doesn't import any Node packages. We only use Node to lint and compile our own JavaScript and css resources but it's not required if building from a release branch such as `release-v0.6`.
3. At least 2 GB of available system memory.
4. (Optional) [Golangci-lint](https://golangci-lint.run/docs/welcome/install/) is recommended for linting Go code. Only required if you plan to contribute code.
5. Optional: [Docker](https://docs.docker.com/get-docker/) is recommended for containerized development and testing.

### Preparation

- [Install Node.js 18.x or later](https://nodejs.org/en/download/), which is required to lint
  and package the static web assets.

- [Install Go 1.24 or later](https://golang.org/doc/install) and verify with:

  ```sh
  go env GOROOT GOPATH
  ```
  Ensure `$GOPATH/bin` is on your `$PATH`.

- Clone the `dcrdex` repository via your terminal:

  ```sh
  git clone https://github.com/decred/dcrdex.git
  cd dcrdex
  ```

### Building from Source

- Build the web assets

    ```sh
    cd client/webserver/site
    npm clean-install && npm run build
    ```

    This will generate the bundled CSS and JavaScript files in the *dist/* folder.

    Alternatively, for development, `npm` can be made to watch for and integrate JavaScript source changes:

    ```sh
    npm run watch
    ```

- Build and Run Bison Wallet

    To build and run the Bison Wallet client, you have the option of building the `bisonw` or `bisonw-desktop` binary. First, ensure you are in the root of the git repository. If you've just built the web assets, you can return to the root with `cd ../../..`.

    **For the browser client:**

    ```sh
    cd ./client/cmd/bisonw
    ./bisonw
    ```

    Once running, open your browser and visit: `http://127.0.0.1:5758`.

    **For the desktop client**, follow the instructions in [client/cmd/bisonw-desktop/README.md](../client/cmd/bisonw-desktop/README.md).

### Running with Docker

If you prefer containerized development, follow these steps to build and run Bison Wallet using Docker.

- Install Docker

    Ensure Docker is installed on your machine. [Get Docker](https://docs.docker.com/get-docker/), open and set up it up. Then verify installation with:

    ```sh
    docker --version
    ```

- Build Docker Image

    Run this command from the root of the repository:

    ```sh
    docker build -t user/bisonw -f client/Dockerfile .
    ```

- Create a Docker Volume

    This creates a persistent data volume for the wallet:

    ```sh
    docker volume create --name=bisonw_data
    ```

- Run the Docker Container
    Start the Bison Wallet container with:

    ```sh
    docker run -d -p 127.0.0.1:5758:5758 -v bisonw_data:/root/.bisonw user/bisonw
    ```

    Then, open your browser and visit: `http://127.0.0.1:5758`

    You'll get the container ID as output. If you need to stop the container later, use:

    ```sh
    docker stop <container_id>
    ```

    To run the container again:

    ```sh
    docker container start <container_id>
    ```

    Your data will persist in the `bisonw_data` volume.

If you run into any issue during the build or run process, please reach out to us in the [DEX Development room](https://matrix.to/#/!EzTSRQITaqHuFBDFhM:decred.org?via=decred.org&via=matrix.org&via=zettaport.com) or open an issue on the [GitHub issues page](https://github.com/decred/dcrdex/issues).

### Additional Resources

Check out these wiki pages for more information.

- [Contribution Guide](Contribution-Guide)
- [DCRDEX Specification](https://github.com/decred/dcrdex/blob/master/spec/README.mediawiki)
- [Client control API](Client-control-API)
- [Integrating New Assets](Integrating-New-Assets)
- [Simnet Testing](Simnet-Testing) - Recommended for development.
- [Testnet Testing](Testnet-Testing) - Recommended for poking around.
- [Test App Server](Test-App-Server) - Useful for GUI development, or just to try everything out
without needing to create wallets or connect to a **dcrdex** server.
- [Getting Started with Fuzzing](Getting-Started-With-Fuzzing)
- [Localization and Translations](Localization-and-Translation)
- [Wiki Contribution Guide](Wiki-Contribution-Guide)

---

[⤴ Back to Top](#top)
