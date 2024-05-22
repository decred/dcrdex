# walletpair.sh

This is a tmux script for a 2-wallet pair that developers can use to perform manual testing on mainnet and testnet. The script aims to solve a number of problems.

- Testing by self-trading with a single wallet has historically failed to detect some bugs.
- Keeping your personal mainnet wallet on a release branch is advisable, so we don't want to use our primary wallet for testing.
- For testing on mainnet, we want to use a testing server that has ultra-low lot limits and bond size, and we only want to run the nodes/markets that we're interested in testing. It's not usually a server suitable for public use.

**walletpair.sh** will create two bisonw instances, and open browser windows to
them. Wallet data is persisted through restarts, so you can send mainnet funds
without risk of loss. That said, this is for testing, so only send what you can
lose in case of massive catastrophe.

Use `--testnet` and `--simnet` flags to use respective testing networks.
