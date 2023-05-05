#!/usr/bin/env bash
set -ex

dir=$(pwd)
# list of all modules to test
modules=". /dex/testing/loadbot /client/cmd/dexc-desktop"

GV=$(go version | sed "s/^.*go\([0-9.]*\).*/\1/")
echo "Go version: $GV"

# Ensure html templates pass localization.
go generate -x ./client/webserver/site # no -write

# For each module, run go mod tidy, build and run test.
for m in $modules
do
	cd "$dir/$m"

	# Run `go mod tidy` and fail if the git status of go.mod and/or
	# go.sum changes. Only do this for the latest Go version.
	if [[ "$GV" =~ ^1.20 ]]; then
		MOD_STATUS=$(git status --porcelain go.mod go.sum)
		go mod tidy
		UPDATED_MOD_STATUS=$(git status --porcelain go.mod go.sum)
		if [ "$UPDATED_MOD_STATUS" != "$MOD_STATUS" ]; then
			echo "$m: running 'go mod tidy' modified go.mod and/or go.sum"
		git diff --unified=0 go.mod go.sum
			exit 1
		fi
	fi

	# run tests
	env GORACE="halt_on_error=1" go test -race -short -count 1 ./...
done

cd "$dir"

# Print missing Core notification translations.
go run ./client/core/localetest/main.go

# -race in go tests above requires cgo, but disable it for the compile tests below
export CGO_ENABLED=0
go build ./...
go build -tags harness -o /dev/null ./client/cmd/simnet-trade-tests
go build -tags systray -o /dev/null ./client/cmd/dexc

go test -c -o /dev/null -tags live ./client/webserver
go test -c -o /dev/null -tags harness ./client/asset/dcr
go test -c -o /dev/null -tags electrumlive ./client/asset/btc
go test -c -o /dev/null -tags harness ./client/asset/btc/livetest
go test -c -o /dev/null -tags harness ./client/asset/ltc
go test -c -o /dev/null -tags harness ./client/asset/bch
go test -c -o /dev/null -tags harness ./client/asset/eth
go test -c -o /dev/null -tags rpclive ./client/asset/eth
go test -c -o /dev/null -tags harness ./client/asset/zec
go test -c -o /dev/null -tags dcrlive ./server/asset/dcr
go test -c -o /dev/null -tags btclive ./server/asset/btc
go test -c -o /dev/null -tags ltclive ./server/asset/ltc
go test -c -o /dev/null -tags bchlive ./server/asset/bch
go test -c -o /dev/null -tags dogelive ./server/asset/doge
go test -c -o /dev/null -tags zeclive ./server/asset/zec
go test -c -o /dev/null -tags harness ./server/asset/eth
go test -c -o /dev/null -tags pgonline ./server/db/driver/pg

# Return to initial directory.
cd "$dir"
# golangci-lint (github.com/golangci/golangci-lint) is used to run each
# static checker.

# check linters
golangci-lint run --disable-all --deadline=10m \
  --out-format=github-actions,colored-line-number \
  --enable=gofmt \
  --enable=goimports \
  --enable=govet \
  --enable=gosimple \
  --enable=unconvert \
  --enable=structcheck \
  --enable=ineffassign \
  --enable=asciicheck \
  --enable=rowserrcheck \
  --enable=sqlclosecheck \
  --enable=makezero
