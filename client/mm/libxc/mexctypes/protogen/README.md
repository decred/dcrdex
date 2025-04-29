# MEXC Protobuf Code Generation

This directory contains the tools and process for generating Go code from MEXC's Protocol Buffer definitions.

## Directory Structure

- `websocket-proto/`: Contains the cloned MEXC protocol buffer definitions (not committed to version control)
- `generated/`: Contains the generated Go code from protocol buffers (not committed to version control)
- `.gitignore`: Excludes the above directories from version control

The final adapted code is stored in `../protobuf.go` in the parent directory.

## Setup

1. Install Protocol Buffer compiler (protoc):
```bash
# For Ubuntu/Debian:
sudo apt-get install protobuf-compiler

# Verify installation:
protoc --version
```

2. Install Go Protocol Buffers plugin:
```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
```

3. Clone the MEXC WebSocket Proto repository:
```bash
git clone https://github.com/mexcdevelop/websocket-proto.git
```

## Generating Code

1. Modify the proto file to add the Go package option:
```go
option go_package = "github.com/decred/dcrdex/client/mm/libxc/mexctypes/protogen/generated";
```

2. Generate the Go code from the proto file:
```bash
mkdir -p generated
protoc --go_out=generated --go_opt=paths=source_relative websocket-proto/PublicLimitDepthsV3Api.proto
```

3. The generated code will be in `generated/PublicLimitDepthsV3Api.pb.go`

4. Adapt the generated code for inclusion in the mexctypes package and save to `../protobuf.go`

## Important Note

The `.proto` files should not be committed to the repository. Only the process documentation and the resulting adapted Go code should be committed. 