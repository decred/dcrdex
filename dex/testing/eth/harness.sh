#!/bin/sh
# Tmux script that sets up a simnet harness.
set -ex

SESSION="eth-harness"

CHAIN_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-38.123221057Z--9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS="9ebba10a6136607688ca4f27fab70e23938cd027"
CHAIN_ADDRESS_JSON='{"address":"9ebba10a6136607688ca4f27fab70e23938cd027","crypto":{"cipher":"aes-128-ctr","ciphertext":"dcfbe17de6f315c732855111b782496d76b2d703169afddaaa69e1bc9e02ec51","cipherparams":{"iv":"907e5e050649d1c5c0be782ec7db5cf1"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"060f4e16d601069a6bccae0693a15cd72090baf1ab20e408c89883117d4f7c51"},"mac":"b9ca7dad75a04b77dc7751a814c051f32752603334e4bb4046caf927196a5579"},"id":"74805e39-6a2f-46eb-8125-70c41d12c6d9","version":3}'

ALPHA_ADDRESS="18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-28T08-47-02.993754951Z--18d65fb8d60c1199bb1ad381be47aa692b482605"
ALPHA_ADDRESS_JSON='{"address":"18d65fb8d60c1199bb1ad381be47aa692b482605","crypto":{"cipher":"aes-128-ctr","ciphertext":"927bc2432492fc4bbe9acfe0042f5cd2cef25aff251ac1fb2f420ee85e3b6ee4","cipherparams":{"iv":"89e7333535aed5284abd52f841d30c95"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"6fe29ea59d166989be533da62d79802a6b0cef26a9766fa363c7a4bb4c263b5f"},"mac":"c7e2b6c4538c373b2c4e0be7b343db618d39cc68fa872909059357ff36743ca0"},"id":"0e2b9cef-d659-4a26-8739-879129ed0b63","version":3}'
ALPHA_NODE_KEY="71d810d39333296b518c846a3e49eca55f998fd7994998bb3e5048567f2f073c"
# ALPHA_ENODE="897c84f6e4f18195413c1d02927e6a4093f5e7574b52bdec6f20844c4f1f6dd3f16036a9e600bd8681ab50fd8dd144df4a6ba9dd8722bb578a86aaa8222c964f"
ALPHA_NODE_PORT="30302"

BETA_ADDRESS="4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON_FILE_NAME="UTC--2021-01-27T08-20-58.179642501Z--4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06"
BETA_ADDRESS_JSON='{"address":"4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06","crypto":{"cipher":"aes-128-ctr","ciphertext":"c5672bb829df9e209ca8ce18dbdd1fed69c603d639e06ab09127b672a609c121","cipherparams":{"iv":"24460eb2934c8b61cee3ad0aa7b843c0"},"kdf":"scrypt","kdfparams":{"dklen":32,"n":262144,"p":1,"r":8,"salt":"1f85da881994ca7b4a23f0698da70500a4b79f97a4450b83b129ebf3b4c28f50"},"mac":"1ecea707f1bffa1f6f944cb47e83118d8179e8a5005b83c88610b7e8692a1197"},"id":"56633762-6fb1-4cbf-8396-3a2e4661f7d4","version":3}'
BETA_NODE_KEY="0f3f23a0f14202da009bd59a96457098acea901986629e54d5be1eea32fc404a"
BETA_ENODE="b1d3e358ee5c9b268e911f2cab47bc12d0e65c80a6d2b453fece34facc9ac3caed14aa3bc7578166bb08c5bc9719e5a2267ae14e0b42da393f4d86f6d5829061"
BETA_NODE_PORT="30303"

# PASSWORD is the password used to unlock all accounts/wallets/addresses.
PASSWORD="abc"

export NODES_ROOT=~/dextest/eth
export GENESIS_JSON_FILE_LOCATION="${NODES_ROOT}/genesis.json"
export MINE_JS="${NODES_ROOT}/mine.js"

if [ -d "${NODES_ROOT}" ]; then
  rm -R "${NODES_ROOT}"
fi

mkdir -p "${NODES_ROOT}/alpha"
mkdir -p "${NODES_ROOT}/beta"
mkdir -p "${NODES_ROOT}/harness-ctl"

echo "Writing ctl scripts"
################################################################################
# Control Scripts
################################################################################

# Write genesis json. ".*Block" fields represent block height where certain
# protocols take effect. "clique" is our proof of authority scheme. One block
# can be mined per second with the a signature belonging to the address in
# "extradata". The addresses in the "alloc" field are allocated "balance".
cat > "${NODES_ROOT}/genesis.json" <<EOF
{
  "config": {
    "chainId": 42,
    "homesteadBlock": 0,
    "eip150Block": 0,
    "eip155Block": 0,
    "eip158Block": 0,
    "byzantiumBlock": 0,
    "constantinopleBlock": 0,
    "petersburgBlock": 0,
    "clique": {
      "period": 1,
      "epoch": 30000
    }
  },
  "difficulty": "1",
  "gasLimit": "8000000",
  "extradata": "0x00000000000000000000000000000000000000000000000000000000000000009ebba10a6136607688ca4f27fab70e23938cd0270000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
  "alloc": {
    "18d65fb8d60c1199bb1ad381be47aa692b482605": {
        "balance": "1000000000000"
    },
    "4f8ef3892b65ed7fc356ff473a2ef2ae5ec27a06": {
        "balance": "1000000000000"
    }
  }
}
EOF

# Write mining javascript.
# NOTE: This sometimes mines more than one block. It is a race. This returns
# the number of blocks mined within the lifespan of the function, but one more
# MAY be mined after returning.
cat > "${MINE_JS}" <<EOF
function mine() {
  blkN = eth.blockNumber;
  miner.start();
  miner.stop();
  admin.sleep(1.1);
  return eth.blockNumber - blkN;
}
EOF

# Add wallet script.
HARNESS_DIR=$(dirname "$0")
cp "${HARNESS_DIR}/create-node.sh" "${NODES_ROOT}/harness-ctl/create-node"

# Reorg script
# TODO: Make this.

# Shutdown script
cat > "${NODES_ROOT}/harness-ctl/quit" <<EOF
#!/bin/sh
tmux send-keys -t $SESSION:1 C-c
tmux send-keys -t $SESSION:2 C-c
tmux kill-session
EOF
chmod +x "${NODES_ROOT}/harness-ctl/quit"

################################################################################
# Start harness
################################################################################

echo "Starting harness"
tmux new-session -d -s $SESSION
tmux rename-window -t $SESSION:0 'harness-ctl'
tmux send-keys -t $SESSION:0 "set +o history" C-m
tmux send-keys -t $SESSION:0 "cd ${NODES_ROOT}/harness-ctl" C-m

################################################################################
# Eth nodes
################################################################################

echo "Starting simnet alpha node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:1" "alpha" "$ALPHA_NODE_PORT " \
	"$CHAIN_ADDRESS" "$PASSWORD" "$CHAIN_ADDRESS_JSON " \
	"$CHAIN_ADDRESS_JSON_FILE_NAME" "$ALPHA_ADDRESS" "$PASSWORD" \
	"$ALPHA_ADDRESS_JSON " "$ALPHA_ADDRESS_JSON_FILE_NAME" "$ALPHA_NODE_KEY"

echo "Starting simnet beta node"
"${HARNESS_DIR}/create-node.sh" "$SESSION:2" "beta" "$BETA_NODE_PORT " \
	"$CHAIN_ADDRESS" "$PASSWORD" "$CHAIN_ADDRESS_JSON " \
	"$CHAIN_ADDRESS_JSON_FILE_NAME" "$BETA_ADDRESS" "$PASSWORD" \
	"$BETA_ADDRESS_JSON " "$BETA_ADDRESS_JSON_FILE_NAME" "$BETA_NODE_KEY"

# NOTE: This will cause beta to connect automatically to alpha.
echo "Connecting nodes"
"${NODES_ROOT}/harness-ctl/alpha" "attach --exec admin.addPeer('enode://${BETA_ENODE}@127.0.0.1:$BETA_NODE_PORT')"

# Reenable history and attach to the control session.
tmux select-window -t $SESSION:0
tmux send-keys -t $SESSION:0 "set -o history" C-m
tmux attach-session -t $SESSION
