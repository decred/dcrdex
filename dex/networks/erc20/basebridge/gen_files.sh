abigen --abi erc20.json --pkg basebridge --type ERC20 --out erc20.go
abigen --abi l1standardbridge.json --pkg basebridge --type L1StandardBridge --out l1standardbridge.go
abigen --abi l2OutputOracle.json --pkg basebridge --type L2OutputOracle --out l2OutputOracle.go
abigen --abi l2standardbridge.json --pkg basebridge --type L2Standardbridge --out l2standardbridge.go
abigen --abi l2ToL1MessagePasser.json --pkg basebridge --type L2ToL1MessagePasser --out l2ToL1MessagePasser.go
abigen --abi optimismPortal.json --pkg basebridge --type OptimismPortal --out optimismPortal.go
abigen --abi l2CrossDomainMessenger.json --pkg basebridge --type L2CrossDomainMessenger --out l2CrossDomainMessenger.go
