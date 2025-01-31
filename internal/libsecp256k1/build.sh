#!/usr/bin/env bash

rm -fr secp256k1
git clone https://github.com/tecnovert/secp256k1 -b anonswap_v0.2

cd secp256k1
./autogen.sh
./configure --enable-module-dleag --enable-experimental --enable-module-generator --enable-module-ed25519 --enable-module-recovery --enable-module-ecdsaotves
make
cd ..
