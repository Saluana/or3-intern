package adminflow

import (
	"reflect"
	"strings"

	"or3-intern/internal/config"
)

// resolveConfigPathValue reads a value from config using a dotted JSON path.
func resolveConfigPathValue(current config.Config, path string) (any, bool) {
	segments := strings.Split(strings.TrimSpace(path), ".")
	if len(segments) == 0 {
		return nil, false
	}
	value := reflect.ValueOf(current)
	for _, segment := range segments {
		for value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return nil, false
			}
			value = value.Elem()
		}
		switch value.Kind() {
		case reflect.Struct:
			found := false
			typ := value.Type()
			for i := 0; i < value.NumField(); i++ {
				field := typ.Field(i)
				tag := strings.Split(field.Tag.Get("json"), ",")[0]
				if tag == segment {
					value = value.Field(i)
					found = true
					break
				}
			}
			if !found {
				return nil, false
			}
		case reflect.Map:
			key := reflect.ValueOf(segment)
			item := value.MapIndex(key)
			if !item.IsValid() {
				return nil, false
			}
			value = item
		default:
			return nil, false
		}
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, true
		}
		value = value.Elem()
	}
	return value.Interface(), true
}
