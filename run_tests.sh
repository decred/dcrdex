#!/bin/bash

set -ex

dir=$(pwd)
# list of all modules to test
modules=". client/cmd/dexcctl"

# Test each module separately.
for m in $modules
do
	cd $dir/$m
	# run tests
	env GORACE="halt_on_error=1" go test -race -short ./...
done

cd $dir
dumptags="-c -o /dev/null -tags"
go test $dumptags live ./client/webserver
go test $dumptags harness ./client/asset/dcr
go test $dumptags harness ./client/asset/btc
go test $dumptags dcrlive ./server/asset/dcr
go test $dumptags btclive ./server/asset/btc
go test $dumptags pgonline ./server/db/driver/pg

for path in server/cmd/dcrdex client/cmd/dexc client/cmd/dexcctl
do
	cd $dir/$path
	go build -race
done

# Return to initial directory.
cd $dir
# golangci-lint (github.com/golangci/golangci-lint) is used to run each
# static checker.

# check linters
golangci-lint run --disable-all --deadline=10m \
  --enable=goimports \
  --enable=govet \
  --enable=gosimple \
  --enable=unconvert \
  --enable=structcheck \
  --enable=ineffassign
