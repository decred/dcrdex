## Multisig Payment

Funds can be locked in a multisig script that has a refund path with a time lock.

### Alice and Bob example

We have two users, Bob and Alice that will collaborate on sending funds. We have two more users Tim and Sue that we wish to send funds to. Currently Bob has the funds.

Alice sends an address pubkey from their wallet to Bob. Tim and Sue send Bob p2pkh addresses and amounts they expect.

Bob inputs all this into a csv file and feeds that to the new tool. It sends funds, one tx to a script with two paths, a 2 of 2 multisig requiring Bob and Alice signatures, and a refund path that goes back to Bob. Bob signs his side of the txn and sends the half signed txn and redeem scripts to Alice in csv form.

Alice reviews the tx and if acceptable, feeds it through her tool to sign the other half and send.

### With RPC Server

The rpc server must be enabled when starting bisonw.

The file at /dcrdex/client/core/multisigexample.csv should be copied and the data filled in according to comments.

The user gathers pubkeys, addresses, and amounts from all parties. Pubkeys must be derived with the rpc command `paymentmultisigpubkey` so that core can find them later.

The user then uses the rpc command `sendfundstomultisig` to send funds to the multisig. If it succeeds funds are now locked in the script's output. The csv file is updated in place with a spending tx at the bottom. This spending tx has all the data needed to spend or refund the tx.

The user then signs that tx with `signmultisig`

The user then sends that entire file with the hex at bottom to the next signer. They can review with `viewpaymentmultisig`. If satisfied they then sign with `signmultisig` and then send with `sendpaymentmultisig`

At any time after the locktime, the initial sender can use `refundpaymentmultisig` to return funds to their account. Can only be refunded by the original sender with the same wallet they used to create.
