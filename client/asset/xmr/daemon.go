package xmr

func (r *xmrRpc) getBlockHeightFast() (uint64, error) {
	if r.isReScanning() {
		return 0, errRescanning
	}
	bhfResp, err := r.daemon.DaemonGetBlockCount(r.ctx)
	if err != nil {
		return 0, err
	}
	return bhfResp.Count, nil
}

// getFeeRate gives an estimation on fees (atoms) per byte.
func (r *xmrRpc) getFeeRate() (uint64, error) {
	if r.isReScanning() {
		return 0, errRescanning
	}
	feeResp, err := r.daemon.DaemonGetFeeEstimate(r.ctx)
	if err != nil {
		r.log.Errorf("getFeeRate - %v", err)
		return 0, err
	}
	return feeResp.Fee, nil
}
