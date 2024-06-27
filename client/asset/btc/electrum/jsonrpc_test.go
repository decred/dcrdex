// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

import (
	"fmt"
	"testing"
)

func Test_floatString_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		arg     []byte
		wantErr bool
	}{
		{
			name: "float64",
			arg:  []byte{49, 50, 46, 50, 51}, // 12.23
		},
		{
			name: "string float64",
			arg:  []byte{34, 49, 50, 46, 50, 51, 34}, // "12.23"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var fs floatString
			if err := fs.UnmarshalJSON(tt.arg); (err != nil) != tt.wantErr {
				t.Errorf("%s: error = %v, wantErr %v", tt.name, err, tt.wantErr)
			}

			fsStr := fmt.Sprint(fs)
			fsByte := []byte(fsStr)
			if string(fsByte) != fsStr {
				t.Errorf("%s: expected %v got %v", tt.name, string(fsByte), fsStr)
			}
		})
	}
}
