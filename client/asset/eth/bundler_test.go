package eth

import "testing"

func TestEstimateBundlerGasResultTotalGas(t *testing.T) {
	// Test case 1: Normal values
	result := &estimateBundlerGasResult{
		PreVerificationGas:   "0x5208",  // 21000 in decimal
		VerificationGasLimit: "0x186a0", // 100000 in decimal
		CallGasLimit:         "0x7530",  // 30000 in decimal
	}

	expected := uint64(21000 + 100000 + 30000) // 151000
	actual := result.totalGas()

	if actual != expected {
		t.Errorf("Expected total gas %d, got %d", expected, actual)
	}

	// Test case 2: Zero values
	resultZero := &estimateBundlerGasResult{
		PreVerificationGas:   "0x0",
		VerificationGasLimit: "0x0",
		CallGasLimit:         "0x0",
	}

	expectedZero := uint64(0)
	actualZero := resultZero.totalGas()

	if actualZero != expectedZero {
		t.Errorf("Expected total gas %d, got %d", expectedZero, actualZero)
	}
}
