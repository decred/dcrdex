package config

import (
	"fmt"
	"reflect"
	"testing"
)

type configurable interface {
	expectedOptionKeysAndDesc() map[string]string
	validateTestValues(options []*Option) map[string]string
}

type tConfigurable struct {
	expectedOptions map[string]string
	randomValues    map[string]string
}

func (c tConfigurable) expectedOptionKeysAndDesc() map[string]string {
	return c.expectedOptions
}
func (c tConfigurable) validateTestValues(options []*Option) map[string]string {
	testValues := c.randomValues
	// ensure that test values are defined for all option keys and
	// that the values defined differ from the current option values.
	for _, option := range options {
		testValue, isDefined := testValues[option.Key]
		if !isDefined || testValue == fmt.Sprintf("%v", option.Value) {
			return nil
		}
	}
	return testValues
}

// TestOptions ensures that Options() returns a complete list of parsable config
// options for a specified config object. Also confirms that the returned option
// names/keys, when used to generate config data, can be properly parsed into any
// object of the same type as the config object.
func TestOptions(t *testing.T) {
	var okCfg = &struct {
		tConfigurable `ini:"-"`
		OptField1     string  `ini:"stringopt,,option 1 desc"`
		OptField2     bool    `ini:"boolopt,,option 2 desc"`
		OptField3     uint    `ini:"uintopt,,option 3 desc"`
		OptField4     float32 `ini:"floatopt,,option 4 desc"`
	}{
		tConfigurable: tConfigurable{
			expectedOptions: map[string]string{
				"stringopt": "option 1 desc",
				"boolopt":   "option 2 desc",
				"uintopt":   "option 3 desc",
				"floatopt":  "option 4 desc",
			},
			randomValues: map[string]string{
				"stringopt": "some text",
				"boolopt":   "true",
				"uintopt":   "42",
				"floatopt":  "3.142",
			},
		},
	}
	var cfgWithUnexportedField = &struct {
		tConfigurable `ini:"-"`
		OkOptField    string `ini:"okopt,,exported field"`
		nonOptField   int    `ini:"unexported,,unexported field"` // should be ignored
	}{
		tConfigurable: tConfigurable{
			expectedOptions: map[string]string{
				"okopt": "exported field",
			},
			randomValues: map[string]string{
				"okopt":      "rando",
				"unexported": "911", // specify a value to confirm that option is actually ignored
			},
		},
	}
	var dcrConfig = &struct {
		tConfigurable `ini:"-"`
		RPCUser       string `ini:"username,,Username for RPC connections"`
		RPCPass       string `ini:"password,,Password for RPC connections"`
		RPCListen     string `ini:"rpclisten,,dcrwallet interface/port for RPC connections (default port: 9109, testnet: 19109)"`
		RPCCert       string `ini:"rpccert,,Path to the dcrwallet TLS certificate file"`
	}{
		tConfigurable: tConfigurable{
			expectedOptions: map[string]string{
				"username":  "Username for RPC connections",
				"password":  "Password for RPC connections",
				"rpclisten": "dcrwallet interface/port for RPC connections (default port: 9109, testnet: 19109)",
				"rpccert":   "Path to the dcrwallet TLS certificate file",
			},
			randomValues: map[string]string{
				"username":  "dcrwallet",
				"password":  "dcrwalletpass",
				"rpclisten": "localhost",
				"rpccert":   "Path to the dcrwallet TLS certificate file",
				// below values should be ignored when parsing these values into the dcrConfig obj
				"proxy":     "127.0.0.1:9050",
				"proxyuser": "randomuser",
				"proxypass": "randompass",
			},
		},
	}

	tests := []struct {
		name   string
		cfgObj interface{}
	}{
		{
			name:   "ok",
			cfgObj: okCfg,
		},
		{
			name:   "unexported field",
			cfgObj: cfgWithUnexportedField,
		},
		{
			name:   "dcr config",
			cfgObj: dcrConfig,
		},
	}
	for _, tt := range tests {
		cfg, isConfigurable := tt.cfgObj.(configurable)
		if !isConfigurable {
			t.Fatalf("%s: cfgObj should implement configurable", tt.name)
		}

		// Read slice of parsable options from the test cfg object and confirm
		// that the expected options and expected options alone are returned.
		cfgOpts := Options(tt.cfgObj)
		expectedKeysAndDesc := cfg.expectedOptionKeysAndDesc()
		if len(cfgOpts) != len(expectedKeysAndDesc) {
			t.Fatalf("%s: expected %d options, got %d", tt.name,
				len(expectedKeysAndDesc), len(cfgOpts))
		}
		for _, opt := range cfgOpts {
			expectedDesc, isExpected := expectedKeysAndDesc[opt.Key]
			if !isExpected {
				t.Fatalf("%s: unexpected option %q extracted from config object", tt.name, opt.Key)
			}
			if expectedDesc != opt.Description {
				t.Fatalf("%s: wrong descsription for option %q, found %q, expected %q", tt.name,
					opt.Key, opt.Description, expectedDesc)
			}
		}

		// Parse test config values into the test config object to confirm that
		// the Options() function returned the complete set of parsable options.
		// It's important that test values be defined for all expected options
		// and that the values differ from the values currently set in the test
		// config object.
		// Test config values may define values for unknown/unexpected keys and
		// we'll confirm below that those values are ignored.
		tstCfgValues := cfg.validateTestValues(cfgOpts)
		if tstCfgValues == nil {
			t.Fatalf("%s: invalid test config values", tt.name)
		}
		if err := ParseInto(Data(tstCfgValues), tt.cfgObj); err != nil {
			t.Fatalf("%s: unexpected ParseInto() error: %v", tt.name, err)
		}

		// Assert that the config object is updated with tstCfgValues for all
		// expected options and contains no value for unexpected options.
		parsedValues := readCfgValues(tt.cfgObj)
		for key, value := range tstCfgValues {
			parsedValue, parsed := parsedValues[key]
			_, isExpected := expectedKeysAndDesc[key]
			if !parsed && !isExpected {
				continue
			}
			if parsed && !isExpected {
				t.Fatalf("%s: parsed config has value for unexpected option %q", tt.name, key)
			}
			if !parsed {
				t.Fatalf("%s: parsed config does not contain value for expected option %q", tt.name, key)
			} else if parsedValue != value {
				t.Fatalf("%s: unexpected value for option %q in parsed config, expected %q, got %q", tt.name,
					key, value, parsedValue)
			}
		}
	}
}

func readCfgValues(cfgObj interface{}) map[string]string {
	cfgVal := reflect.ValueOf(cfgObj)
	if reflect.TypeOf(cfgObj).Kind() == reflect.Ptr {
		cfgVal = cfgVal.Elem()
	}
	cfgType := cfgVal.Type()

	values := make(map[string]string)
	for i := 0; i < cfgType.NumField(); i++ {
		field := cfgVal.Field(i)
		if !field.CanSet() {
			continue
		}
		fieldType := cfgType.Field(i)
		key, _, _ := iniTagValues(fieldType.Tag.Get("ini"))
		if key == "-" {
			continue
		}
		if key == "" {
			key = fieldType.Name
		}
		values[key] = fmt.Sprint(field.Interface())
	}
	return values
}
