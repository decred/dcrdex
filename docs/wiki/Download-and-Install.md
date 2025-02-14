<a id="top"></a>

_Last updated for Bison Wallet v1.0.0._


**Contents**
- [Download](#download)
- [Bison Wallet Desktop](#bison-wallet-desktop)
- [Bison Wallet CLI](#bison-wallet-cli)
- [Building from Source](#building-from-source)
- [Docker](#docker)


# Download

The latest version of Bison Wallet can be downloaded from https://dex.decred.org.

> [!NOTE]
> We recommend you also verify that your download hash matches the hash 
in the DCRDEX releases manifest. For detailed instructions, read about 
[Verifying Binaries](https://docs.decred.org/advanced/verifying-binaries/)
 in the Decred Documentation.

You will need to visit the [releases](https://github.com/decred/dcrdex/releases) page 
to download the manifest and manifest signature:

```
bisonw-v1.0.0-manifest.txt
bisonw-v1.0.0-manifest.txt.asc
```

# Bison Wallet Desktop

``bisonw-desktop`` is the Desktop version of Bison Wallet. This version is a self-contained 
application,  making it the preferred option for new users.


<details>
<summary><b>Windows</b></summary> 

1. Download the Windows installer ``bisonw-desktop-windows-amd64-v1.0.0.msi``.

2. Double click the installer and follow the instructions.

3. The installer adds a shortcut to Bison Wallet on your Start Menu.

</details>
<details>
<summary><b>macOS</b></summary> 

1. Download the ``bisonw-desktop-darwin-amd64-v1.0.0.dmg`` file.

2. Double click the ``bisonw-desktop-darwin-amd64-v1.0.0.dmg` file to mount the disk image.

3. Drag the ``bisonw-desktop.app`` file into the link to your Applications folder within the 
disk image.

</details>
<details>
<summary><b>Linux (Debian / Ubuntu)</b></summary> 

1. Download the ```bisonw-desktop-linux-amd64-v1.0.0.deb``` file.

2. Open a terminal in the extracted folder and run the command 
```sudo dpkg -i ./bisonw-desktop-linux-amd64-v1.0.0.deb```.

3. Bison Wallet can then be launched from the applications menu.

Once the installation has completed, `Bison Wallet Desktop` can be launched from
the shortcut added to the Start/Application menu. A new window will appear once the 
application starts. 

</details>

The [Quick Configuration](Quick-Configuration) section of this wiki will guide you
through the Bison Wallet setup.

# Bison Wallet CLI

``bisonw`` is the command line version of Bison Wallet. This version provides access to several
optional parameters for more advanced users, a web browser is required to access the 
graphical user interface (GUI).

<details>
<summary><b>Windows</b></summary> 

1. Download the ``bisonw-windows-amd64-v1.0.0.zip`` file.

2. Navigate to the download location and extract ``bisonw-windows-amd64-v1.0.0.zip``.

3. The extracted files include an executable named ``bisonw``.

</details>
<details>
<summary><b>macOS</b></summary> 

1. Download the ``bisonw-darwin-amd64-v1.0.0.tar.gz`` file.

2. Navigate to the download location and extract ``bisonw-darwin-amd64-v1.0.0.tar.gz``.

3. The extracted files include an executable named ``bisonw``.

4. Open a terminal in the extracted folder and run the command ```chmod u+x bisonw``.

5. Bison Wallet can then be launched from the terminal using the command ``./bisonw``.

</details>
<details>
<summary><b>Linux</b></summary> 

1. Download the ``bisonw-linux-amd64-v1.0.0.tar.gz`` file.

2. Navigate to the download location and extract ``bisonw-linux-amd64-v1.0.0.tar.gz``.

3. The extracted files include an executable named ``bisonw``.

4. Open a terminal in the extracted folder and run the command ``chmod u+x bisonw``.

5. Bison Wallet can then be launched from the terminal using the command ``./bisonw``.

</details>

Once the installation has completed, `Bison Wallet CLI` can be launched from a terminal 
using the command ``./bisonw`` from within the folder where it was extracted. Once initial 
configuration has completed, the following message will appear in the terminal:

```
2024-10-15 10:38:04.710 [INF] WEB: Web server listening on 127.0.0.1:5758 (https = false)

        ****  OPEN IN YOUR BROWSER TO LOGIN AND TRADE  --->  http://127.0.0.1:5758  ****

```
Open any web browser to the link shown by the application. 
The [Quick Configuration](Quick-Configuration) section of this wiki will guide you
through the Bison Wallet setup.


# Building from Source

**Dependencies**

1. [Go 1.22 - 1.23](https://golang.org/doc/install)
2. (optional) [Node 18 or 20](https://docs.npmjs.com/downloading-and-installing-node-js-and-npm) 
is used to bundle resources for the browser interface. It's important to note that Bison Wallet has no 
external JavaScript dependencies. The client doesn't import any Node packages. We only use Node to lint 
and compile our own JavaScript and css resources. This build step is not required if building from a 
release branch such as `release-v1.0`.
1. At least 2 GB of available system memory.

**Building**

1. Build the web assets from `client/webserver/site/`. 
If building from the `master` branch, bundle the CSS and JavaScript with Webpack:

```sh
npm clean-install && npm run build
```

2. Build and run the client from `client/cmd/bisonw`.

```sh
go build
./bisonw
```
3. Once initial configuration has completed, the following message will appear 
in the terminal:

```
2024-10-15 10:38:04.710 [INF] WEB: Web server listening on 127.0.0.1:5758 (https = false)

        ****  OPEN IN YOUR BROWSER TO LOGIN AND TRADE  --->  http://127.0.0.1:5758  ****

```
4.Open any web browser to the link shown in the terminal. 
The [Quick Configuration](Quick-Configuration) section of this wiki will guide you
through the Bison Wallet setup.

# Docker

**Build the docker image**

```sh
docker build -t user/dcrdex -f client/Dockerfile .
```

**Create docker volume**

```sh
docker volume create --name=dcrdex_data
```

**Run image**

```sh
docker run -d --rm -p 127.0.0.1:5758:5758 -v dcrdex_data:/root/.bisonw user/dcrdex
```

Open `127.0.0.1:5758` in any browser, note that if the mapped address/port is different 
than above, this address may change. The [Quick Configuration](Quick-Configuration) section of this 
wiki will guide you through the Bison Wallet setup.

---

Next Section: [Quick Configuration](Quick-Configuration)

[Back to top ⤴](#top)
