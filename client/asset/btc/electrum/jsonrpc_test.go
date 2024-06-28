// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package electrum

import (
	"encoding/json"
	"fmt"
	"testing"
)

func Test_floatString_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		arg     any
		wantErr bool
	}{
		{
			name: "float",
			arg:  12.23,
		},
		{
			name: "string float",
			arg:  "12.23",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argByte, err := json.Marshal(tt.arg)
			if err != nil {
				t.Errorf("%s: json.Marshal error: %v", tt.name, err)
			}

			var fs floatString
			if err := fs.UnmarshalJSON(argByte); (err != nil) != tt.wantErr {
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
