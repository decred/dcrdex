package config

import (
	"reflect"
	"strings"
)

// Option models a typical ini config option.
type Option struct {
	Key         string      `json:"key"`
	DisplayName string      `json:"displayname"`
	Description string      `json:"description"`
	Value       interface{} `json:"value"`
}

// Options returns a slice of config options that can be used to parse config
// data into any object of the same type as the specified config object.
// The type of the specified config object should be a struct that annotates
// config fields with an `ini:"key,display name,description"` tag.
// Config fields must be exported. Unexported fields and fields with `ini:"-"`
// are ignored. Exported fields without an ini attribute use the field name as
// the option name.
func Options(cfgObj interface{}) []*Option {
	cfgVal := reflect.ValueOf(cfgObj)
	if reflect.TypeOf(cfgObj).Kind() == reflect.Ptr {
		cfgVal = cfgVal.Elem()
	}
	cfgType := cfgVal.Type()

	options := make([]*Option, 0)
	for i := 0; i < cfgType.NumField(); i++ {
		field := cfgVal.Field(i)
		if !field.CanSet() {
			continue
		}

		fieldType := cfgType.Field(i)
		key, displayName, desc := iniTagValues(fieldType.Tag.Get("ini"))
		if key == "-" {
			continue
		}

		if key == "" {
			key = fieldType.Name
		}
		options = append(options, &Option{
			Key:         key,
			DisplayName: displayName,
			Description: desc,
			Value:       field.Interface(),
		})
	}

	return options
}

func iniTagValues(iniTag string) (key, displayName, description string) {
	iniTagValues := strings.SplitN(iniTag, ",", 3)
	key = iniTagValues[0]
	if len(iniTagValues) > 1 {
		displayName = strings.TrimSpace(iniTagValues[1])
	}
	if len(iniTagValues) > 2 {
		description = strings.TrimSpace(iniTagValues[2])
	}
	return key, displayName, description
}
