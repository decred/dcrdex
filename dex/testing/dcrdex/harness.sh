#!/usr/bin/env bash
# Tmux script that configures and runs dcrdex.

set -e

# Setup test data dir for dcrdex.
TEST_ROOT=~/dextest
DCRDEX_DATA_DIR=${TEST_ROOT}/dcrdex
rm -rf "${DCRDEX_DATA_DIR}"
mkdir -p "${DCRDEX_DATA_DIR}"

# Rebuild dcrdex with the required simnet locktime settings.
HARNESS_DIR=$(
  cd $(dirname "$0")
  pwd
)

# Build script
cat > "${DCRDEX_DATA_DIR}/build" <<EOF
#!/usr/bin/env bash
cd ${HARNESS_DIR}/../../../server/cmd/dcrdex/
go build -o ${DCRDEX_DATA_DIR}/dcrdex -ldflags \
    "-X 'decred.org/dcrdex/dex.testLockTimeTaker=3m' \
    -X 'decred.org/dcrdex/dex.testLockTimeMaker=6m'"
EOF
chmod +x "${DCRDEX_DATA_DIR}/build"

# Alternate lock times build script as a harness control
cat > "${DCRDEX_DATA_DIR}/build-lock" <<EOF
#!/usr/bin/env bash
cd ${HARNESS_DIR}/../../../server/cmd/dcrdex/
go build -o ${DCRDEX_DATA_DIR}/dcrdex -ldflags \
    "-X 'decred.org/dcrdex/dex.testLockTimeTaker=\${1:-1m}' \
    -X 'decred.org/dcrdex/dex.testLockTimeMaker=\${2:-2m}'"
EOF
chmod +x "${DCRDEX_DATA_DIR}/build-lock"

"${DCRDEX_DATA_DIR}/build"

cd "${DCRDEX_DATA_DIR}"

# Drop and re-create the test db.
TEST_DB=dcrdex_simnet_test
sudo -u postgres -H psql -c "DROP DATABASE IF EXISTS ${TEST_DB}" \
-c "CREATE DATABASE ${TEST_DB} OWNER dcrdex"

# Write dcrdex.conf. The regfeexpub comes from the alpha>server_fees account.
cat << EOF >> ./dcrdex.conf
pgdbname=${TEST_DB}
simnet=1
rpclisten=127.0.0.1:17273
debuglevel=trace
loglocal=true
signingkeypass=keypass
adminsrvon=1
adminsrvpass=adminpass
adminsrvaddr=127.0.0.1:16542
bcasttimeout=1m
# freecancels=1
maxepochcancels=128
httpprof=1
noderelayaddr=127.0.0.1:17539
EOF

# Set the postgres user pass if provided.
if [ -n "${PG_PASS}" ]; then
echo pgpass="${PG_PASS}" >> ./dcrdex.conf
fi

# Write rpc.cert and rpc.key.
cat > "./rpc.cert" <<EOF
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
EOF
cat > "./rpc.key" <<EOF
-----BEGIN EC PRIVATE KEY-----
MIHcAgEBBEIADTDRCsp8om9OhJa+m46FZ5IhgLAno1Rp6B0i2lqESL5x9vV/upiV
TbNzCeFqEY5/Ujra9f8ZovqMlrIQmNOaZFmgBwYFK4EEACOhgYkDgYYABAClcmlU
PuyLzLGghMRKr7Fpda0SmwJZTtf7yKxEOPVwV8fncrr9c2+fcb5irQvbZDykyjMa
6QDsq4JAAAvTGe6jXQB5xYE3RmjYEse+nAqSYCzvUL7kEhMmSyoXG+PDIwpujcv/
nHxL8mYBatmh6kEt9Dsdyg+osByDRegiAmM7IVmBzw==
-----END EC PRIVATE KEY-----
EOF

# DEX admin script
cat > "${DCRDEX_DATA_DIR}/dexadm" <<EOF
#!/usr/bin/env bash
if [[ "\$#" -eq "2" ]]; then
    curl --cacert ${DCRDEX_DATA_DIR}/rpc.cert --basic -u u:adminpass --header "Content-Type: text/plain" --data-binary "\$2" https://127.0.0.1:16542/api/\$1
else
    curl --cacert ${DCRDEX_DATA_DIR}/rpc.cert --basic -u u:adminpass https://127.0.0.1:16542/api/\$1
fi
EOF
chmod +x "${DCRDEX_DATA_DIR}/dexadm"

SESSION="dcrdex-harness"

export SHELL=$(which bash)

# Shutdown script
cat > "${DCRDEX_DATA_DIR}/quit" <<EOF
#!/usr/bin/env bash
tmux send-keys -t $SESSION:0 C-c
if [ -n "${NODERELAY}" ] ; then
  tmux send-keys -t $SESSION:1 C-c
  tmux wait-for donenoderelay
fi
tmux wait-for donedex
tmux kill-session -t $SESSION
EOF
chmod +x "${DCRDEX_DATA_DIR}/quit"

cat > "${DCRDEX_DATA_DIR}/run" <<EOF
#!/usr/bin/env bash
${HARNESS_DIR}/genmarkets.sh
${DCRDEX_DATA_DIR}/dcrdex --appdata=$(pwd) \$*; tmux wait-for -S donedex
EOF
chmod +x "${DCRDEX_DATA_DIR}/run"

echo "Starting dcrdex"
tmux new-session -d -s $SESSION $SHELL
tmux rename-window -t $SESSION:0 'dcrdex'

if [ -n "${NODERELAY}" ]; then
    tmux send-keys -t $SESSION:0 "export NODERELAY=1" C-m

    # These match the values in genmarkets.sh
    BTC_NODERELAY_ID="btc_a21afba3"
    DCR_NODERELAY_ID="dcr_a21afba3"

    SOURCENODE_DIR=$(realpath "${HARNESS_DIR}/../../../server/noderelay/cmd/sourcenode/")
    cd ${SOURCENODE_DIR}
    go build -o ${DCRDEX_DATA_DIR}/sourcenode
    cd "${DCRDEX_DATA_DIR}"

    RPC_PORT="20556"
    RELAYFILE="${DCRDEX_DATA_DIR}/data/simnet/noderelay/relay-files/${BTC_NODERELAY_ID}.relayfile"

    tmux new-window -t $SESSION:1 -n 'sourcenode_btc' $SHELL
    # dcrdex needs to write the relayfiles.
    tmux send-keys -t $SESSION:1 "sleep 4" C-m
    tmux send-keys -t $SESSION:1 "./sourcenode --port ${RPC_PORT} --relayfile ${RELAYFILE}; tmux wait-for -S donenoderelay" C-m

    # Decred
    RPC_PORT="19561"
    RELAYFILE="${DCRDEX_DATA_DIR}/data/simnet/noderelay/relay-files/${DCR_NODERELAY_ID}.relayfile"
    DCRD_CERT="${TEST_ROOT}/dcr/alpha/rpc.cert"

    tmux new-window -t $SESSION:2 -n 'sourcenode_dcr' $SHELL
    tmux send-keys -t $SESSION:2 "sleep 4" C-m
    tmux send-keys -t $SESSION:2 "./sourcenode --port ${RPC_PORT} --relayfile ${RELAYFILE} --localcert ${DCRD_CERT}; tmux wait-for -S donenoderelay" C-m
fi

tmux send-keys -t $SESSION:0 "${DCRDEX_DATA_DIR}/run" C-m
tmux select-window -t $SESSION:0
tmux attach-session -t $SESSION
