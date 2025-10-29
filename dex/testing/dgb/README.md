## Digibyte harness

Important: After mining blocks, DigiByte node must do 2 things:
- Update Wallet DB (fast): Write transaction to wallet database
- Update UTXO Index (slow): Rebuild index of all unspent outputs

Do not use until it is ready.
