package adminflow

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// parseBoolish interprets common config truthy/falsy representations.
func parseBoolish(value any) (bool, bool) {
	if value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		lower := strings.ToLower(strings.TrimSpace(typed))
		switch lower {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	case int:
		return typed != 0, true
	case int32:
		return typed != 0, true
	case int64:
		return typed != 0, true
	}
	return false, false
}

func boolPlanValue(value any) (bool, bool) {
	return parseBoolish(value)
}

func isTruthyValue(v any) bool {
	b, ok := parseBoolish(v)
	return ok && b
}

func stringifyPlanValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return fmt.Sprint(value)
	}
}

func planValuesEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	if leftErr == nil && rightErr == nil {
		return string(leftJSON) == string(rightJSON)
	}
	return fmt.Sprint(left) == fmt.Sprint(right)
}

func valuePresent(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []string:
		return len(typed) > 0
	case bool:
		return typed
	default:
		t := reflect.TypeOf(value)
		if t == nil {
			return false
		}
		zero := reflect.Zero(t).Interface()
		return !reflect.DeepEqual(value, zero)
	}
}
