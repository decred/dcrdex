#!/usr/bin/env bash
set -ex

dir=$(pwd)
# list of all modules to test
modules="."

GV=$(go version | sed "s/^.*go\([0-9.]*\).*/\1/")
echo "Go version: $GV"

# Regenerate localized html templates and check for changes.
TMPL_STATUS=$(git status --porcelain 'client/webserver/site/src/localized_html/*.tmpl')
go generate -x ./client/webserver/site
TMPL_STATUS2=$(git status --porcelain 'client/webserver/site/src/localized_html/*.tmpl')
if [ "$TMPL_STATUS" != "$TMPL_STATUS2" ]; then
	printf "Localized HTML templates in client/webserver/site/src/localized_html need updating:\n${TMPL_STATUS2}\n"
	exit 1
fi

# For each module, run go mod tidy, build and run test.
for m in $modules
do
	cd "$dir/$m"

	# Run `go mod tidy` and fail if the git status of go.mod and/or
	# go.sum changes. Only do this for the latest Go version.
	if [[ "$GV" =~ ^1.17 ]]; then
		MOD_STATUS=$(git status --porcelain go.mod go.sum)
		go mod tidy
		UPDATED_MOD_STATUS=$(git status --porcelain go.mod go.sum)
		if [ "$UPDATED_MOD_STATUS" != "$MOD_STATUS" ]; then
			echo "$m: running 'go mod tidy' modified go.mod and/or go.sum"
		git diff --unified=0 go.mod go.sum
			exit 1
		fi
	fi

	# build and run tests
	if [ "$m" != '.' ]; then go build; fi
	env GORACE="halt_on_error=1" go test -race -short -count 1 ./...
done

cd "$dir"
dumptags=(-c -o /dev/null -tags)
go test "${dumptags[@]}" live ./client/webserver
go test "${dumptags[@]}" harness ./client/asset/dcr
go test "${dumptags[@]}" harness ./client/asset/btc/livetest
go test "${dumptags[@]}" harness ./client/asset/ltc
go test "${dumptags[@]}" harness ./client/asset/bch
go test "${dumptags[@]}" harness ./client/core
go test "${dumptags[@]}" dcrlive ./server/asset/dcr
go test "${dumptags[@]}" btclive ./server/asset/btc
go test "${dumptags[@]}" ltclive ./server/asset/ltc
go test "${dumptags[@]}" bchlive ./server/asset/bch
go test "${dumptags[@]}" pgonline ./server/db/driver/pg

# Return to initial directory.
cd "$dir"
# golangci-lint (github.com/golangci/golangci-lint) is used to run each
# static checker.

# check linters
golangci-lint run --disable-all --deadline=10m \
  --out-format=github-actions \
  --enable=goimports \
  --enable=govet \
  --enable=gosimple \
  --enable=unconvert \
  --enable=structcheck \
  --enable=ineffassign \
  --enable=asciicheck \
  --enable=rowserrcheck \
  --enable=sqlclosecheck
