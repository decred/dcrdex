package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/ini.v1"
)

type config struct {
	Key1 string  `ini:"key1"`
	Key2 bool    `ini:"key2"`
	Key3 int     `ini:"key3"`
	KEY4 float64 // defaults to field name i.e. `ini:"KEY4"`
	Key5 string  `ini:"-"` // ignored because of '-' ini tag
}

// defaultConfig returns config with default values.
func defaultConfig() config {
	return config{
		Key1: "default value",
		Key2: true,
		Key3: 0,
		KEY4: 3.142,
		Key5: "ignored",
	}
}

// makeConfig returns a pointer to a config with default values.
func makeConfigPtr() *config {
	c := defaultConfig()
	return &c
}

// TestConfigParsing tests the Parse() and ParseInto() functions.
func TestConfigParsing(t *testing.T) {
	var testConfig = defaultConfig()

	tempDir, err := ioutil.TempDir("", "configtest")
	if err != nil {
		t.Fatalf("error creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tempDir)
	cfgFilePath := filepath.Join(tempDir, "test.conf")
	cfgFile := ini.Empty()
	err = cfgFile.ReflectFrom(&testConfig)
	if err != nil {
		t.Fatalf("error creating temporary config file: %v", err)
	}
	err = cfgFile.SaveTo(cfgFilePath)
	if err != nil {
		t.Fatalf("error creating temporary config file: %v", err)
	}

	type expectations struct {
		parseError     bool
		optionsCount   int
		parseIntoError bool
		parsedCfg      config
	}

	type test struct {
		name      string
		cfgData   interface{}
		parsedCfg interface{}
		expect    expectations
	}

	testCount := 0
	makeOkTest := func(name, sectionHeader, secondSectionHeader string) test {
		testCount++
		value1 := fmt.Sprintf("value %d", testCount)
		value4 := 1.1 * float64(testCount)
		cfgDataString := fmt.Sprintf(`
		%v
		key1=%v
		key2=false
		%v
		key3=%v
		KEY4=%v
		key5=parsed as option, but not populated into struct
		`, sectionHeader, value1, secondSectionHeader, testCount, value4)

		return test{
			name:      name,
			cfgData:   []byte(cfgDataString),
			parsedCfg: makeConfigPtr(),
			expect: expectations{
				parseError:     false,
				optionsCount:   5,
				parseIntoError: false,
				parsedCfg: config{
					Key1: value1,
					Key2: false,
					Key3: testCount,
					KEY4: value4,
					Key5: testConfig.Key5, // should be unchanged
				},
			},
		}
	}

	// Prepare test data.
	tests := []test{
		makeOkTest("ok, with default application options header", "[Application Options]", ""),
		makeOkTest("ok, with random section header", "[Random Header]", ""),
		makeOkTest("ok, with multiple section headers", "[Application Options]", "[Random Options]"),
		makeOkTest("ok, with no section header", "", ""),
		{
			name:      "ok, with file path",
			cfgData:   cfgFilePath,
			parsedCfg: makeConfigPtr(),
			expect: expectations{
				parseError:     false,
				optionsCount:   4, // file was created from struct with only 4 valid ini fields
				parseIntoError: false,
				parsedCfg:      defaultConfig(),
			},
		},
		{
			name:      "parse error, file path, parsedCfg obj not pointer",
			cfgData:   cfgFilePath,
			parsedCfg: defaultConfig(), // not a pointer
			expect: expectations{
				parseError:     false,
				optionsCount:   4, // file was created from cfg struct with only 4 valid ini fields
				parseIntoError: true,
			},
		},
		{
			name: "parse error, []byte data, parsedCfg obj not pointer",
			cfgData: []byte(`
			key1=value 1
			key2=false
			key3=10
			`),
			parsedCfg: defaultConfig(), // not a pointer
			expect: expectations{
				parseError:     false,
				optionsCount:   3,
				parseIntoError: true,
			},
		},
		{
			name: "error, malformed section header",
			cfgData: []byte(`
			[Random Options
			key1=value 1
			key2=false
			key3=10
			`),
			parsedCfg: makeConfigPtr(),
			expect: expectations{
				parseError:     true,
				parseIntoError: true,
			},
		},
		{
			name: "error, malformed option",
			cfgData: []byte(`
			=value 1
			key2=false
			key3=10
			`),
			parsedCfg: makeConfigPtr(),
			expect: expectations{
				parseError:     true,
				parseIntoError: true,
			},
		},
	}
	// Test Parse() and ParseInto() functions.
	for _, tt := range tests {
		parsedOptions, err := Parse(tt.cfgData)
		if tt.expect.parseError && err == nil {
			t.Fatalf("%s: expected Parse() to error but got no error", tt.name)
		} else if !tt.expect.parseError && err != nil {
			t.Fatalf("%s: got unexpected Parse() error: %v", tt.name, err)
		}
		if len(parsedOptions) != tt.expect.optionsCount {
			t.Fatalf("%s: expected %d options, got %d", tt.name, tt.expect.optionsCount, len(parsedOptions))
		}

		err = ParseInto(tt.cfgData, tt.parsedCfg)
		if tt.expect.parseIntoError && err != nil {
			return
		}
		if tt.expect.parseIntoError && err == nil {
			t.Fatalf("%s: expected ParseInto() to error but got no error", tt.name)
		} else if !tt.expect.parseIntoError && err != nil {
			t.Fatalf("%s: got unexpected ParseInto() error: %v", tt.name, err)
		}

		parsedCfg, ok := tt.parsedCfg.(*config)
		if !ok {
			t.Fatalf("%s: unexpected type for parsed config", tt.name)
		}
		if parsedCfg.Key1 != tt.expect.parsedCfg.Key1 {
			t.Fatalf("%s: expected parsed cfg key1 to have '%v', got '%v'", tt.name, tt.expect.parsedCfg.Key1,
				parsedCfg.Key1)
		}
		if parsedCfg.Key2 != tt.expect.parsedCfg.Key2 {
			t.Fatalf("%s: expected parsed cfg key2 to have '%v', got '%v'", tt.name, tt.expect.parsedCfg.Key2,
				parsedCfg.Key2)
		}
		if parsedCfg.Key3 != tt.expect.parsedCfg.Key3 {
			t.Fatalf("%s: expected parsed cfg key3 to have '%v', got '%v'", tt.name, tt.expect.parsedCfg.Key3,
				parsedCfg.Key3)
		}
		if parsedCfg.KEY4 != tt.expect.parsedCfg.KEY4 {
			t.Fatalf("%s: expected parsed cfg key4 to have '%v', got '%v'", tt.name, tt.expect.parsedCfg.KEY4,
				parsedCfg.KEY4)
		}
		if parsedCfg.Key5 != tt.expect.parsedCfg.Key5 {
			t.Fatalf("%s: expected parsed cfg key5 to have '%v', got '%v'", tt.name, tt.expect.parsedCfg.Key5,
				parsedCfg.Key3)
		}
	}
}

// Test Data() function to convert map to config data (in []byte).
func DataConversion(t *testing.T) {
	m := map[string]string{
		"key1": "value1",
		"key2": "false",
		"key3": "3",
		"KEY4": "4.4",
		"key5": "parsed, but not populated into struct",
	}
	cfgData := Data(m)

	opts, err := Parse(cfgData)
	if err != nil {
		t.Fatalf("unexpected error when parsing cfg data generated from map: %v", err)
	}
	if len(opts) != len(m) {
		t.Fatalf("map-cfg-map: expected %d options, got %d", len(m), len(opts))
	}
	for k, vOriginal := range m {
		if vParsed, ok := opts[k]; !ok {
			t.Fatalf("map-cfg-map: key '%s' not found in parsed options", k)
		} else if vParsed != vOriginal {
			t.Fatalf("map-cfg-map: unexpected value for key '%s', expected '%s', got '%s'", k, vOriginal, vParsed)
		}
	}

	var cfg = makeConfigPtr()
	err = ParseInto(cfgData, cfg)
	if err != nil {
		t.Fatalf("unexpected error when parsing cfg data generated from map into obj: %v", err)
	}
	if cfg.Key1 != m["key1"] {
		t.Fatalf("map-cfg-obh: unexpected value for key 'key1', expected '%s', got '%s'", m["key1"], cfg.Key1)
	}
	if cfg.Key5 != defaultConfig().Key5 {
		t.Fatalf("map-cfg-obh: expected value for key 'key5' not to change, changed to '%s'", cfg.Key5)
	}
}
