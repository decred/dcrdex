abigen --abi child_token.json --pkg polygonbridge --type ChildERC20 --out childtoken.go
abigen --abi root_chain_manager.json --pkg polygonbridge --type RootChainManager --out rootchainmanager.go
abigen --abi erc20_predicate.json --pkg polygonbridge --type ERC20Predicate --out erc20predicate.go
abigen --abi deposit_manager.json --pkg polygonbridge --type DepositManager --out depositmanager.go
