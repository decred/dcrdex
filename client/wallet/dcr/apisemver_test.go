package dcr

import (
	"testing"

	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
)

func TestCheckSemVer(t *testing.T) {
	tests := []struct {
		expected *dcrdtypes.VersionResult
		provided *dcrdtypes.VersionResult
		wantErr  bool
	}{
		{
			expected: &dcrdtypes.VersionResult{
				VersionString: "1.5.1",
				Major:         1,
				Minor:         5,
				Patch:         1,
			},
			provided: &dcrdtypes.VersionResult{
				VersionString: "1.5.1",
				Major:         1,
				Minor:         5,
				Patch:         1,
			},
			wantErr: false,
		},
		{
			expected: &dcrdtypes.VersionResult{
				VersionString: "1.5.1",
				Major:         1,
				Minor:         5,
				Patch:         1,
			},
			provided: &dcrdtypes.VersionResult{
				VersionString: "1.5.2",
				Major:         1,
				Minor:         5,
				Patch:         2,
			},
			wantErr: false,
		},
		{
			expected: &dcrdtypes.VersionResult{
				VersionString: "1.5.1",
				Major:         1,
				Minor:         5,
				Patch:         1,
			},
			provided: &dcrdtypes.VersionResult{
				VersionString: "1.5.0",
				Major:         1,
				Minor:         5,
				Patch:         0,
			},
			wantErr: true,
		},
		{
			expected: &dcrdtypes.VersionResult{
				VersionString: "1.5.1",
				Major:         1,
				Minor:         5,
				Patch:         1,
			},
			provided: &dcrdtypes.VersionResult{
				VersionString: "1.4.1",
				Major:         1,
				Minor:         4,
				Patch:         1,
			},
			wantErr: true,
		},
		{
			expected: &dcrdtypes.VersionResult{
				VersionString: "1.5.1",
				Major:         1,
				Minor:         5,
				Patch:         1,
			},
			provided: &dcrdtypes.VersionResult{
				VersionString: "0.4.1",
				Major:         0,
				Minor:         4,
				Patch:         1,
			},
			wantErr: true,
		},
		{
			expected: &dcrdtypes.VersionResult{
				VersionString: "1.5.1",
				Major:         1,
				Minor:         5,
				Patch:         1,
			},
			provided: &dcrdtypes.VersionResult{
				VersionString: "2.0.0",
				Major:         2,
				Minor:         0,
				Patch:         0,
			},
			wantErr: false,
		},
	}

	for idx, tc := range tests {
		err := checkSemVer("dcrd", tc.expected, tc.provided)
		if (err != nil) != tc.wantErr {
			t.Errorf("[checkSemVer] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}
	}
}

func TestCheckVersionInfo(t *testing.T) {
	tests := []struct {
		info    map[string]dcrdtypes.VersionResult
		wantErr bool
	}{
		{
			info: map[string]dcrdtypes.VersionResult{
				"dcrd": dcrdtypes.VersionResult{
					VersionString: "1.5.0",
					Major:         1,
					Minor:         5,
					Patch:         0,
				},
				"dcrdjsonrpcapi": dcrdtypes.VersionResult{
					VersionString: "6.1.0",
					Major:         6,
					Minor:         1,
					Patch:         0,
				},
				"dcrwalletjsonrpcapi": dcrdtypes.VersionResult{
					VersionString: "6.2.0",
					Major:         6,
					Minor:         2,
					Patch:         0,
				},
			},
			wantErr: false,
		},
		{
			info: map[string]dcrdtypes.VersionResult{
				"wrongid": dcrdtypes.VersionResult{
					VersionString: "1.5.0",
					Major:         1,
					Minor:         5,
					Patch:         0,
				},
				"dcrdjsonrpcapi": dcrdtypes.VersionResult{
					VersionString: "6.1.0",
					Major:         6,
					Minor:         1,
					Patch:         0,
				},
				"dcrwalletjsonrpcapi": dcrdtypes.VersionResult{
					VersionString: "6.2.0",
					Major:         6,
					Minor:         2,
					Patch:         0,
				},
			},
			wantErr: true,
		},
		{
			info: map[string]dcrdtypes.VersionResult{
				"dcrd": dcrdtypes.VersionResult{
					VersionString: "1.4.0",
					Major:         1,
					Minor:         4,
					Patch:         0,
				},
				"dcrdjsonrpcapi": dcrdtypes.VersionResult{
					VersionString: "6.1.0",
					Major:         6,
					Minor:         1,
					Patch:         0,
				},
				"dcrwalletjsonrpcapi": dcrdtypes.VersionResult{
					VersionString: "6.2.0",
					Major:         6,
					Minor:         2,
					Patch:         0,
				},
			},
			wantErr: true,
		},
		{
			info: map[string]dcrdtypes.VersionResult{
				"dcrd": dcrdtypes.VersionResult{
					VersionString: "1.5.0",
					Major:         1,
					Minor:         5,
					Patch:         0,
				},
				"dcrwalletjsonrpcapi": dcrdtypes.VersionResult{
					VersionString: "6.2.0",
					Major:         6,
					Minor:         2,
					Patch:         0,
				},
			},
			wantErr: true,
		},
	}

	for idx, tc := range tests {
		err := checkVersionInfo(tc.info, "dcrd", "dcrdjsonrpcapi", "dcrwalletjsonrpcapi")
		if (err != nil) != tc.wantErr {
			t.Errorf("[checkVersionInfo] #%d: error: %v, wantErr: %v",
				idx+1, err, tc.wantErr)
		}
	}
}
