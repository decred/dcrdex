solc --abi --bin --optimize --overwrite --optimize-runs 1000000 --base-path . --include-path node_modules --output-dir build-entrypoint node_modules/@account-abstraction/contracts/core/EntryPoint.sol
abigen --abi=build-entrypoint/EntryPoint.abi --bin=build-entrypoint/EntryPoint.bin --pkg=entrypoint --out=build-entrypoint/entrypoint.go
cp build-entrypoint/entrypoint.go ../../../../../client/asset/eth/entrypoint-v6.go
cp build-entrypoint/EntryPoint.bin .