#!/usr/bin/env bash
# Generate descriptor JSON files for BTC testing harness using Bitcoin Core RPC.
# This script creates desc-{alpha,beta,gamma,delta}.json with correct checksums.
#
# Prerequisites:
#   - Bitcoin Core running on regtest (run harness.sh first, or start manually)
#   - bitcoin-cli available in PATH
#   - python3 available in PATH
#
# Usage: ./gen-descriptors.sh [RPC_PORT]
#   RPC_PORT defaults to 20556 (alpha node)

set -e

RPC_PORT="${1:-20556}"
RPC_USER="user"
RPC_PASS="pass"

CLI="bitcoin-cli -regtest=1 -rpcport=${RPC_PORT} -rpcuser=${RPC_USER} -rpcpassword=${RPC_PASS}"

echo "=== BTC Descriptor Generator ==="
echo "Using RPC port: ${RPC_PORT}"
echo ""

# Check if Bitcoin Core is running
if ! ${CLI} getblockchaininfo &>/dev/null; then
    echo "ERROR: Bitcoin Core not running on port ${RPC_PORT}"
    echo "Please start the harness first or run bitcoind manually:"
    echo "  bitcoind -regtest -rpcport=${RPC_PORT} -rpcuser=${RPC_USER} -rpcpassword=${RPC_PASS}"
    exit 1
fi

echo "Bitcoin Core is running. Generating descriptors..."
echo ""

# Function to generate descriptor file for a wallet
generate_wallet_descriptors() {
    local wallet_name="$1"
    local output_file="$2"
    local temp_wallet="_gen_${wallet_name}_$$"

    echo "Generating descriptors for ${wallet_name}..."

    # Create temporary descriptor wallet
    ${CLI} createwallet "${temp_wallet}" false false "" false true >/dev/null 2>&1 || {
        # Wallet might already exist, try to load it
        ${CLI} loadwallet "${temp_wallet}" >/dev/null 2>&1 || true
    }

    # Export descriptors with private keys
    local descriptors=$(${CLI} -rpcwallet="${temp_wallet}" listdescriptors true)

    # Get the first bech32 address (wpkh - BIP84)
    local address=$(${CLI} -rpcwallet="${temp_wallet}" getnewaddress "" "bech32")

    # Convert to importdescriptors format
    echo "${descriptors}" | python3 -c "
import json, sys
data = json.load(sys.stdin)
result = []
for d in data['descriptors']:
    result.append({
        'desc': d['desc'],
        'timestamp': 'now',
        'active': d['active'],
        'internal': d['internal'],
        'range': d['range'],
        'next': d.get('next', 0)
    })
print(json.dumps(result, separators=(',',':')))
" > "${output_file}"

    echo "  -> ${output_file}"
    echo "  -> Address: ${address}"

    # Unload and delete temporary wallet
    ${CLI} unloadwallet "${temp_wallet}" >/dev/null 2>&1 || true

    # Return address via global variable (bash limitation)
    GENERATED_ADDRESS="${address}"
}

# Generate descriptors for all wallets (macOS bash 3.x compatible)
generate_wallet_descriptors "alpha" "desc-alpha.json"
ALPHA_ADDR="${GENERATED_ADDRESS}"

generate_wallet_descriptors "beta" "desc-beta.json"
BETA_ADDR="${GENERATED_ADDRESS}"

generate_wallet_descriptors "gamma" "desc-gamma.json"
GAMMA_ADDR="${GENERATED_ADDRESS}"

generate_wallet_descriptors "delta" "desc-delta.json"
DELTA_ADDR="${GENERATED_ADDRESS}"

echo ""
echo "=========================================="
echo "GENERATED FILES:"
echo "=========================================="
ls -la desc-*.json
echo ""

echo "=========================================="
echo "UPDATE harness.sh WITH THESE VALUES:"
echo "=========================================="
echo ""
echo "export ALPHA_MINING_ADDR=\"${ALPHA_ADDR}\""
echo "export BETA_MINING_ADDR=\"${BETA_ADDR}\""
echo "export GAMMA_ADDRESS=\"${GAMMA_ADDR}\""
echo "export DELTA_ADDRESS=\"${DELTA_ADDR}\""
echo ""

echo "=========================================="
echo "IMPORTANT NOTES:"
echo "=========================================="
echo "1. Commit the generated desc-*.json files to the repository"
echo "2. Update harness.sh with the addresses above"
echo "3. Remove any existing harnesschain.tar.gz"
echo "4. Bump HARNESS_VER in harness.sh"
echo ""
echo "Done!"
