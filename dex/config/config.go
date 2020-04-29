// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package config

import (
	"bytes"
	"fmt"

	"gopkg.in/go-ini/ini.v1"
)

// OptionsMapToINIData generates a config []byte data from settings.
func OptionsMapToINIData(options map[string]string) []byte {
	var buffer bytes.Buffer
	for key, value := range options {
		buffer.WriteString(fmt.Sprintf("%s=%s\n", key, value))
	}
	return buffer.Bytes()
}

// Options returns a collection of all key-value options in provided config
// file path or []byte data.
func Options(cfgPathOrData interface{}) (map[string]string, error) {
	cfgFile, err := ini.Load(cfgPathOrData)
	if err != nil {
		return nil, err
	}
	return options(cfgFile), nil
}

func options(cfgFile *ini.File) map[string]string {
	options := make(map[string]string)
	for _, section := range cfgFile.Sections() {
		for _, key := range section.Keys() {
			options[key.Name()] = key.String()
		}
	}
	return options
}

// Parse parses config options from the provided config file path or []byte
// data into the specified struct object.
// If the config has section headers, the config options are first read into
// a map, then converted to []byte before being parsed. Otherwise the struct
// object would not be modified with any data from the config file.
func Parse(cfgPathOrData, obj interface{}) error {
	cfgFile, err := ini.Load(cfgPathOrData)
	if err != nil {
		return err
	}

	cfgSections := cfgFile.Sections()
	if len(cfgSections) > 1 || cfgSections[0].Name() != ini.DefaultSection {
		// config file or data has non-default section headers, remove sections
		// by extracting all config options and regenerating the config data.
		cfgOptions := options(cfgFile)
		cfgPathOrData = OptionsMapToINIData(cfgOptions)
		return Parse(cfgPathOrData, obj)
	}

	err = cfgFile.MapTo(obj)
	return err
}
