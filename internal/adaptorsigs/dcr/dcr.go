package dcr

import "github.com/decred/dcrd/txscript/v4"

func LockRefundTxScript(kal, kaf []byte, locktime int64) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddOp(txscript.OP_IF).
		AddData(kal).
		AddOp(txscript.OP_2).
		AddOp(txscript.OP_CHECKSIGALTVERIFY).
		AddData(kaf).
		AddOp(txscript.OP_2).
		AddOp(txscript.OP_CHECKSIGALT).
		AddOp(txscript.OP_ELSE).
		AddInt64(locktime).
		AddOp(txscript.OP_CHECKSEQUENCEVERIFY).
		AddOp(txscript.OP_DROP).
		AddData(kaf).
		AddOp(txscript.OP_2).
		AddOp(txscript.OP_CHECKSIGALT).
		AddOp(txscript.OP_ENDIF).
		Script()
}

func LockTxScript(kal, kaf []byte) ([]byte, error) {
	return txscript.NewScriptBuilder().
		AddData(kal).
		AddOp(txscript.OP_2).
		AddOp(txscript.OP_CHECKSIGALTVERIFY).
		AddData(kaf).
		AddOp(txscript.OP_2).
		AddOp(txscript.OP_CHECKSIGALT).
		Script()
}
