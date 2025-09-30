This directory contains protobuf-generated Go files for MEXC Spot v3 websocket messages.

Package name: `pb` (following Go convention for protobuf-generated code)

## Prerequisites

1. Install protoc (v21.0 or newer required for proto3 optional fields):
   ```bash
   cd /tmp
   wget https://github.com/protocolbuffers/protobuf/releases/download/v25.3/protoc-25.3-linux-x86_64.zip
   unzip protoc-25.3-linux-x86_64.zip -d protoc-25.3-linux-x86_64
   sudo cp protoc-25.3-linux-x86_64/bin/protoc /usr/local/bin/
   sudo cp -r protoc-25.3-linux-x86_64/include/* /usr/local/include/
   ```

2. Install protoc-gen-go:
   ```bash
   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
   ```

## Generation Steps

1. Clone MEXC proto files:
   ```bash
   cd /tmp
   rm -rf websocket-proto
   git clone https://github.com/mexcdevelop/websocket-proto.git
   ```

2. Generate Go code (run from dcrdex repo root):
   ```bash
   protoc \
     -I /tmp/websocket-proto \
     --experimental_allow_proto3_optional \
     --go_out=client/mm/libxc/mxctypes/pb \
     --go_opt=paths=source_relative \
     --go_opt=MPrivateAccountV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPrivateDealsV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPrivateOrdersV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicAggreBookTickerV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicAggreDealsV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicAggreDepthsV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicBookTickerBatchV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicBookTickerV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicDealsV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicIncreaseDepthsBatchV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicIncreaseDepthsV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicLimitDepthsV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicMiniTickerV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicMiniTickersV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPublicSpotKlineV3Api.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     --go_opt=MPushDataV3ApiWrapper.proto=github.com/decred/dcrdex/client/mm/libxc/mxctypes/pb \
     /tmp/websocket-proto/*.proto
   ```

## Notes

- Package name `pb` follows Go community convention for protobuf-generated code
- The `--experimental_allow_proto3_optional` flag is required because MEXC proto files use proto3 optional fields
- The `--go_opt=M<proto_file>=<go_package>` flags are needed because MEXC proto files don't include `go_package` options
- Proto source: https://github.com/mexcdevelop/websocket-proto


