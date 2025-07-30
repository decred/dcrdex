abigen --abi l1standardbridge.json --pkg basebridge --type L1StandardBridge --out l1standardbridge.go
abigen --abi l2standardbridge.json --pkg basebridge --type L2Standardbridge --out l2standardbridge.go
abigen --abi l2CrossDomainMessenger.json --pkg basebridge --type L2CrossDomainMessenger --out l2CrossDomainMessenger.go
