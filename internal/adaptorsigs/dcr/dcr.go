package dcr

import "github.com/decred/dcrd/txscript/v4"

func LockRefundTxScript(kal, kaf []byte, locktime int64) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddOp(txscript.OP_IF).
		AddOp(txscript.OP_2).
		AddData(kal).
		AddData(kaf).
		AddOp(txscript.OP_2).
		AddOp(txscript.OP_CHECKMULTISIG).
		AddOp(txscript.OP_ELSE).
		AddInt64(locktime).
		AddOp(txscript.OP_CHECKSEQUENCEVERIFY).
		AddOp(txscript.OP_DROP).
		AddData(kaf).
		AddOp(txscript.OP_CHECKSIG).
		AddOp(txscript.OP_ENDIF).
		Script()
}

func LockTxScript(kal, kaf []byte) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddOp(txscript.OP_2).
		AddData(kal).
		AddData(kaf).
		AddOp(txscript.OP_2).
		AddOp(txscript.OP_CHECKMULTISIG).
		Script()
}

//    def verifySCLockTx(self, tx_bytes, script_out,
//                       swap_value,
//                       Kal, Kaf,
//                       feerate,
//                       check_lock_tx_inputs, vkbv=None):
//        # Verify:
//        #
//
//        # Not necessary to check the lock txn is mineable, as protocol will wait for it to confirm
//        # However by checking early we can avoid wasting time processing unmineable txns
//        # Check fee is reasonable
//
//        tx = self.loadTx(tx_bytes)
//        txid = self.getTxid(tx)
//        self._log.info('Verifying lock tx: {}.'.format(b2h(txid)))
//
//        ensure(tx.nVersion == self.txVersion(), 'Bad version')
//        ensure(tx.nLockTime == 0, 'Bad nLockTime')  # TODO match txns created by cores
//
//        script_pk = self.getScriptDest(script_out)
//        locked_n = findOutput(tx, script_pk)
//        ensure(locked_n is not None, 'Output not found in tx')
//        locked_coin = tx.vout[locked_n].nValue
//
//        # Check value
//        ensure(locked_coin == swap_value, 'Bad locked value')
//
//        # Check script
//        A, B = self.extractScriptLockScriptValues(script_out)
//        ensure(A == Kal, 'Bad script pubkey')
//        ensure(B == Kaf, 'Bad script pubkey')
//
//        if check_lock_tx_inputs:
//            # TODO: Check that inputs are unspent
//            # Verify fee rate
//            inputs_value = 0
//            add_bytes = 0
//            add_witness_bytes = getCompactSizeLen(len(tx.vin))
//            for pi in tx.vin:
//                ptx = self.rpc('getrawtransaction', [i2h(pi.prevout.hash), True])
//                prevout = ptx['vout'][pi.prevout.n]
//                inputs_value += make_int(prevout['value'])
//
//                prevout_type = prevout['scriptPubKey']['type']
//                if prevout_type == 'witness_v0_keyhash':
//                    add_witness_bytes += 107  # sig 72, pk 33 and 2 size bytes
//                    add_witness_bytes += getCompactSizeLen(107)
//                else:
//                    # Assume P2PKH, TODO more types
//                    add_bytes += 107  # OP_PUSH72 <ecdsa_signature> OP_PUSH33 <public_key>
//
//            outputs_value = 0
//            for txo in tx.vout:
//                outputs_value += txo.nValue
//            fee_paid = inputs_value - outputs_value
//            assert (fee_paid > 0)
//
//            vsize = self.getTxVSize(tx, add_bytes, add_witness_bytes)
//            fee_rate_paid = fee_paid * 1000 // vsize
//
//            self._log.info('tx amount, vsize, feerate: %ld, %ld, %ld', locked_coin, vsize, fee_rate_paid)
//
//            if not self.compareFeeRates(fee_rate_paid, feerate):
//                self._log.warning('feerate paid doesn\'t match expected: %ld, %ld', fee_rate_paid, feerate)
//                # TODO: Display warning to user
//
//        return txid, locked_n
//
//    def verifySCLockRefundTx(self, tx_bytes, lock_tx_bytes, script_out,
//                             prevout_id, prevout_n, prevout_seq, prevout_script,
//                             Kal, Kaf, csv_val_expect, swap_value, feerate, vkbv=None):
//        # Verify:
//        #   Must have only one input with correct prevout and sequence
//        #   Must have only one output to the p2wsh of the lock refund script
//        #   Output value must be locked_coin - lock tx fee
//
//        tx = self.loadTx(tx_bytes)
//        txid = self.getTxid(tx)
//        self._log.info('Verifying lock refund tx: {}.'.format(b2h(txid)))
//
//        ensure(tx.nVersion == self.txVersion(), 'Bad version')
//        ensure(tx.nLockTime == 0, 'nLockTime not 0')
//        ensure(len(tx.vin) == 1, 'tx doesn\'t have one input')
//
//        ensure(tx.vin[0].nSequence == prevout_seq, 'Bad input nSequence')
//        ensure(tx.vin[0].scriptSig == self.getScriptScriptSig(prevout_script), 'Input scriptsig mismatch')
//        ensure(tx.vin[0].prevout.hash == b2i(prevout_id) and tx.vin[0].prevout.n == prevout_n, 'Input prevout mismatch')
//
//        ensure(len(tx.vout) == 1, 'tx doesn\'t have one output')
//
//        script_pk = self.getScriptDest(script_out)
//        locked_n = findOutput(tx, script_pk)
//        ensure(locked_n is not None, 'Output not found in tx')
//        locked_coin = tx.vout[locked_n].nValue
//
//        # Check script and values
//        A, B, csv_val, C = self.extractScriptLockRefundScriptValues(script_out)
//        ensure(A == Kal, 'Bad script pubkey')
//        ensure(B == Kaf, 'Bad script pubkey')
//        ensure(csv_val == csv_val_expect, 'Bad script csv value')
//        ensure(C == Kaf, 'Bad script pubkey')
//
//        fee_paid = swap_value - locked_coin
//        assert (fee_paid > 0)
//
//        dummy_witness_stack = self.getScriptLockTxDummyWitness(prevout_script)
//        witness_bytes = self.getWitnessStackSerialisedLength(dummy_witness_stack)
//        vsize = self.getTxVSize(tx, add_witness_bytes=witness_bytes)
//        fee_rate_paid = fee_paid * 1000 // vsize
//
//        self._log.info('tx amount, vsize, feerate: %ld, %ld, %ld', locked_coin, vsize, fee_rate_paid)
//
//        if not self.compareFeeRates(fee_rate_paid, feerate):
//            raise ValueError('Bad fee rate, expected: {}'.format(feerate))
//
//        return txid, locked_coin, locked_n
//
//    def verifySCLockRefundSpendTx(self, tx_bytes, lock_refund_tx_bytes,
//                                  lock_refund_tx_id, prevout_script,
//                                  Kal,
//                                  prevout_n, prevout_value, feerate, vkbv=None):
//        # Verify:
//        #   Must have only one input with correct prevout (n is always 0) and sequence
//        #   Must have only one output sending lock refund tx value - fee to leader's address, TODO: follower shouldn't need to verify destination addr
//        tx = self.loadTx(tx_bytes)
//        txid = self.getTxid(tx)
//        self._log.info('Verifying lock refund spend tx: {}.'.format(b2h(txid)))
//
//        ensure(tx.nVersion == self.txVersion(), 'Bad version')
//        ensure(tx.nLockTime == 0, 'nLockTime not 0')
//        ensure(len(tx.vin) == 1, 'tx doesn\'t have one input')
//
//        ensure(tx.vin[0].nSequence == 0, 'Bad input nSequence')
//        ensure(tx.vin[0].scriptSig == self.getScriptScriptSig(prevout_script), 'Input scriptsig mismatch')
//        ensure(tx.vin[0].prevout.hash == b2i(lock_refund_tx_id) and tx.vin[0].prevout.n == 0, 'Input prevout mismatch')
//
//        ensure(len(tx.vout) == 1, 'tx doesn\'t have one output')
//
//        # Destination doesn't matter to the follower
//        '''
//        p2wpkh = CScript([OP_0, hash160(Kal)])
//        locked_n = findOutput(tx, p2wpkh)
//        ensure(locked_n is not None, 'Output not found in lock refund spend tx')
//        '''
//        tx_value = tx.vout[0].nValue
//
//        fee_paid = prevout_value - tx_value
//        assert (fee_paid > 0)
//
//        dummy_witness_stack = self.getScriptLockRefundSpendTxDummyWitness(prevout_script)
//        witness_bytes = self.getWitnessStackSerialisedLength(dummy_witness_stack)
//        vsize = self.getTxVSize(tx, add_witness_bytes=witness_bytes)
//        fee_rate_paid = fee_paid * 1000 // vsize
//
//        self._log.info('tx amount, vsize, feerate: %ld, %ld, %ld', tx_value, vsize, fee_rate_paid)
//
//        if not self.compareFeeRates(fee_rate_paid, feerate):
//            raise ValueError('Bad fee rate, expected: {}'.format(feerate))
//
//        return True
//
//    def verifySCLockSpendTx(self, tx_bytes,
//                            lock_tx_bytes, lock_tx_script,
//                            a_pkhash_f, feerate, vkbv=None):
//        # Verify:
//        #   Must have only one input with correct prevout (n is always 0) and sequence
//        #   Must have only one output with destination and amount
//
//        tx = self.loadTx(tx_bytes)
//        txid = self.getTxid(tx)
//        self._log.info('Verifying lock spend tx: {}.'.format(b2h(txid)))
//
//        ensure(tx.nVersion == self.txVersion(), 'Bad version')
//        ensure(tx.nLockTime == 0, 'nLockTime not 0')
//        ensure(len(tx.vin) == 1, 'tx doesn\'t have one input')
//
//        lock_tx = self.loadTx(lock_tx_bytes)
//        lock_tx_id = self.getTxid(lock_tx)
//
//        output_script = self.getScriptDest(lock_tx_script)
//        locked_n = findOutput(lock_tx, output_script)
//        ensure(locked_n is not None, 'Output not found in tx')
//        locked_coin = lock_tx.vout[locked_n].nValue
//
//        ensure(tx.vin[0].nSequence == 0, 'Bad input nSequence')
//        ensure(tx.vin[0].scriptSig == self.getScriptScriptSig(lock_tx_script), 'Input scriptsig mismatch')
//        ensure(tx.vin[0].prevout.hash == b2i(lock_tx_id) and tx.vin[0].prevout.n == locked_n, 'Input prevout mismatch')
//
//        ensure(len(tx.vout) == 1, 'tx doesn\'t have one output')
//        p2wpkh = self.getScriptForPubkeyHash(a_pkhash_f)
//        ensure(tx.vout[0].scriptPubKey == p2wpkh, 'Bad output destination')
//
//        # The value of the lock tx output should already be verified, if the fee is as expected the difference will be the correct amount
//        fee_paid = locked_coin - tx.vout[0].nValue
//        assert (fee_paid > 0)
//
//        dummy_witness_stack = self.getScriptLockTxDummyWitness(lock_tx_script)
//        witness_bytes = self.getWitnessStackSerialisedLength(dummy_witness_stack)
//        vsize = self.getTxVSize(tx, add_witness_bytes=witness_bytes)
//        fee_rate_paid = fee_paid * 1000 // vsize
//
//        self._log.info('tx amount, vsize, feerate: %ld, %ld, %ld', tx.vout[0].nValue, vsize, fee_rate_paid)
//
//        if not self.compareFeeRates(fee_rate_paid, feerate):
//            raise ValueError('Bad fee rate, expected: {}'.format(feerate))
//
//        return True
