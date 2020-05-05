// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package config

import (
	"bytes"
	"fmt"

	"gopkg.in/go-ini/ini.v1"
)

// Parse returns a collection of all key-value options in the provided config
// file path or []byte data.
func Parse(cfgPathOrData interface{}) (map[string]string, error) {
	cfgFile, err := ini.Load(cfgPathOrData)
	if err != nil {
		return nil, err
	}
	options := make(map[string]string)
	for _, section := range cfgFile.Sections() {
		for _, key := range section.Keys() {
			options[key.Name()] = key.String()
		}
	}
	return options, nil
}

// ParseInto parses config options from the provided config file path or []byte
// data into the specified interface.
func ParseInto(cfgPathOrData, obj interface{}) error {
	cfgFile, err := ini.Load(cfgPathOrData)
	if err != nil {
		return err
	}
	for _, section := range cfgFile.Sections() {
		err := section.MapTo(obj)
		if err != nil {
			return err
		}
	}
	return nil
}

// Data generates a config []byte data from a settings map.
func Data(settings map[string]string) []byte {
	var buffer bytes.Buffer
	for key, value := range settings {
		buffer.WriteString(fmt.Sprintf("%s=%s\n", key, value))
	}
	return buffer.Bytes()
}

// Unmapify parses config options from the provided settings map into the
// specified interface.
func Unmapify(settings map[string]string, obj interface{}) error {
	cfgData := Data(settings)
	return ParseInto(cfgData, obj)
}
