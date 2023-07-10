
# Electrum-Firo Harness Support

Harnesses to run Electrum-Firo wallet on regtest.

## 1. Firo Chain Server Test Harness

The **harness.sh** chain server harness is a collection of tmux scripts that
collectively create a sandboxed environment for testing dex swap transactions.
See Also: README_HARNESS.md

## 2. ElectrumX-Firo Test Harness

The harness is a script named **electrumx.sh** which downloads a git repo 
containing a release version of ElectrumX-Firo server.

It requires **harness.sh** Firo chain server harness running

## 3. Electrum-Firo Test Harness

The harness is a script named **electrum.sh** which downloads a git repo containg
release version of Electrum-Firo wallet client.

It requires **electrumx.sh** Firo ElectrumX-Firo server harness running

Which in turn requires **harness.sh** Firo chain server harness running

## Dependencies

The **harness.sh** script depends on [firod] and [firo-cli] to run.
https://github.com/firoorg/firo/releases binaries or build from source

The **electrumx.sh** script depends on [python3], [python3 pip] and [git] to run.
Python3 v3.6 is coded but this script was tested using python3.10. Some testing 
(minimal) was done with 3.7. Python 3.8, 3.9 would be expected to work although
untested. However using Python 3.11 there are dependencies which will not build
and are unsupported by their repective maintainers on PyPI.

Git should be latest. Pip will be downloaded and 
veriosn upgraded by the script into a virtual environment each time.

The **electrum.sh** script depends on [python3], [python3 pip] and [git] to run.
Python3 v3.6 is coded but this script was tested using python3.10. Some testing 
(minimal) was done with 3.7. Git should be latest. Pip will be re-downloaded and 
version upgraded by the script into a virtual environment each time.

_Architecture_
```
ELECTRUM WALLET CLIENT
```
```
   ELECTRUMX SERVER
```
```
  FIRO CHAIN HARNESS
```

All three scripts store data in **~/dextest/...** dirsectory tree.

## Using

You must have `firod` and `firo-cli` in `PATH` to use the harness. Use 3 tty's
and run each line below in a separate tty:

```
$  ./harness.sh
```
```
$  ./electrumx.sh
```
```
$  ./electrum.sh
```

The Electrum-Firo wallet client will have a prepared, empty but encrypted
regtest wallet.

Password is "abc"

## Dev

### Firo Chain Server

For the Firo chain server see the README_HARNESS.md in this directory

### Server
The **electrumx.sh** script first cleans part of the **~/dextest/electrum/firo/server...**
directory tree.

The script then downloads a specific commit from:
https://github.com/firoorg/electrumx-firo.git
to the ../server/electrumx-repo directory

It then creates a python virtual environment (venv) sandbox and installs the 
requesting python3 interpreter, latest version of pip and all required modules
in `setup.py` into the sandbox. 
If any need building from C source it will be done at this point. 

Server certificates are created. Then all the required environment veriables to
configure the server exported. and the electrumX daemon started

The electrumX daemon will connect to the firo chain harness node __alpha__


### Client
The **electrum.sh** script first cleans part of the **~/dextest/electrum/firo/client...**
directory tree.

The script then downloads a specific commit from:
 https://github.com/firoorg/electrum-firo.git
to the ../client/electrum-repo directory

It then creates a python virtual environment (venv) sandbox and installs the 
requesting python3 interpreter, latest version of pip and all required modules
from files in `contrib/requirements`
directory into the sandbox. If any need building from C source it will be done at
this point.

A prepared, empty but encrypted electrum wallet is copied to the electrum data
directory at **~/dextest/electrum/firo/client/wallet/regtest/wallets** directory.

The electrum client wallet will connect to the firo electrumX daemon


### Simnet Trade Testing

In seperate tty's start up:

   - btc harness
   - dcr harness
   - firo harness
   - electrumx.sh Electrum-Firo regtest server
   - electrum.sh Electrum-Firo regtest wallet
   - dcrdex harness

ensure dcrctl, firod and firo-cli are in PATH for simnet-trade-tests exec process
mining

$  ./run dcrfiroelectrum --runonce \<testcase\>

Example:

`$  ./run dcrfiroelectrum --runonce --all`


Best results by nuking everything after each run

