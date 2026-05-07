package agent

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"or3-intern/internal/tools"
)

type ToolArgumentValidator struct{}

type ToolArgumentValidationResult struct {
	Params        map[string]any
	ArgumentsJSON string
	Coercions     []ToolArgumentCoercion
	Errors        []ToolArgumentError
}

type ToolArgumentCoercion struct {
	Path string `json:"path"`
	From string `json:"from"`
	To   string `json:"to"`
}

type ToolArgumentError struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (v ToolArgumentValidator) ValidateAndCoerce(tool tools.Tool, argsJSON string) ToolArgumentValidationResult {
	result := ToolArgumentValidationResult{}
	raw := strings.TrimSpace(argsJSON)
	if raw == "" {
		raw = "{}"
	}
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		result.Errors = append(result.Errors, ToolArgumentError{Path: "$", Code: "malformed_json", Message: err.Error()})
		return result
	}
	params, ok := decoded.(map[string]any)
	if !ok {
		result.Errors = append(result.Errors, ToolArgumentError{Path: "$", Code: "expected_object", Message: "tool arguments must be a JSON object"})
		return result
	}
	schema := map[string]any{}
	if tool != nil {
		schema = tool.Parameters()
	}
	coerced, errors, coercions := validateSchemaValue("$", params, schema)
	if len(errors) > 0 {
		result.Errors = append(result.Errors, errors...)
		return result
	}
	result.Params, _ = coerced.(map[string]any)
	if result.Params == nil {
		result.Params = map[string]any{}
	}
	result.Coercions = coercions
	b, err := json.Marshal(result.Params)
	if err != nil {
		result.Errors = append(result.Errors, ToolArgumentError{Path: "$", Code: "encode_failed", Message: err.Error()})
		return result
	}
	result.ArgumentsJSON = string(b)
	return result
}

func formatToolValidationError(call NormalizedToolCall, validation ToolArgumentValidationResult) string {
	payload := map[string]any{
		"kind":         "tool_result",
		"ok":           false,
		"status":       "validation_failed",
		"public_code":  "tool_argument_validation_failed",
		"tool":         call.Name,
		"tool_call_id": call.ID,
		"errors":       validation.Errors,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return `{"kind":"tool_result","ok":false,"status":"validation_failed","public_code":"tool_argument_validation_failed"}`
	}
	return string(b)
}

func validateSchemaValue(path string, value any, schema map[string]any) (any, []ToolArgumentError, []ToolArgumentCoercion) {
	if schema == nil {
		return value, nil, nil
	}
	typ := schemaType(schema)
	if typ == "" {
		return value, nil, nil
	}
	switch typ {
	case "object":
		obj, ok := value.(map[string]any)
		if !ok {
			if text, ok := value.(string); ok {
				var parsed map[string]any
				if err := json.Unmarshal([]byte(text), &parsed); err == nil {
					return validateObject(path, parsed, schema, []ToolArgumentCoercion{{Path: path, From: "string", To: "object"}})
				}
			}
			return nil, []ToolArgumentError{{Path: path, Code: "expected_object", Message: "expected object"}}, nil
		}
		return validateObject(path, obj, schema, nil)
	case "array":
		arr, ok := value.([]any)
		var coercions []ToolArgumentCoercion
		if !ok {
			itemSchema, _ := schema["items"].(map[string]any)
			if itemSchema != nil && isScalarType(schemaType(itemSchema)) {
				coerced, errs, itemCoercions := validateSchemaValue(path+"[0]", value, itemSchema)
				if len(errs) == 0 {
					arr = []any{coerced}
					coercions = append(coercions, ToolArgumentCoercion{Path: path, From: scalarTypeName(value), To: "array"})
					coercions = append(coercions, itemCoercions...)
					ok = true
				}
			}
		}
		if !ok {
			return nil, []ToolArgumentError{{Path: path, Code: "expected_array", Message: "expected array"}}, nil
		}
		itemSchema, _ := schema["items"].(map[string]any)
		out := make([]any, 0, len(arr))
		var errors []ToolArgumentError
		for i, item := range arr {
			next, errs, itemCoercions := validateSchemaValue(fmt.Sprintf("%s[%d]", path, i), item, itemSchema)
			errors = append(errors, errs...)
			coercions = append(coercions, itemCoercions...)
			out = append(out, next)
		}
		if len(errors) > 0 {
			return nil, errors, coercions
		}
		return out, nil, coercions
	case "string":
		text, ok := value.(string)
		if !ok {
			return nil, []ToolArgumentError{{Path: path, Code: "expected_string", Message: "expected string"}}, nil
		}
		if err := validateEnum(path, text, schema); err != nil {
			return nil, []ToolArgumentError{*err}, nil
		}
		return text, nil, nil
	case "number":
		n, coercion, ok := coerceNumber(value, false)
		if !ok {
			return nil, []ToolArgumentError{{Path: path, Code: "expected_number", Message: "expected number"}}, nil
		}
		if err := validateEnum(path, n, schema); err != nil {
			return nil, []ToolArgumentError{*err}, nil
		}
		if coercion {
			return n, nil, []ToolArgumentCoercion{{Path: path, From: "string", To: "number"}}
		}
		return n, nil, nil
	case "integer":
		n, coercion, ok := coerceNumber(value, true)
		if !ok {
			return nil, []ToolArgumentError{{Path: path, Code: "expected_integer", Message: "expected integer"}}, nil
		}
		if err := validateEnum(path, n, schema); err != nil {
			return nil, []ToolArgumentError{*err}, nil
		}
		if coercion {
			return n, nil, []ToolArgumentCoercion{{Path: path, From: "string", To: "integer"}}
		}
		return n, nil, nil
	case "boolean":
		b, ok := value.(bool)
		if !ok {
			if text, textOK := value.(string); textOK {
				switch strings.ToLower(strings.TrimSpace(text)) {
				case "true":
					return true, nil, []ToolArgumentCoercion{{Path: path, From: "string", To: "boolean"}}
				case "false":
					return false, nil, []ToolArgumentCoercion{{Path: path, From: "string", To: "boolean"}}
				}
			}
			return nil, []ToolArgumentError{{Path: path, Code: "expected_boolean", Message: "expected boolean"}}, nil
		}
		if err := validateEnum(path, b, schema); err != nil {
			return nil, []ToolArgumentError{*err}, nil
		}
		return b, nil, nil
	default:
		return value, nil, nil
	}
}

func validateObject(path string, obj map[string]any, schema map[string]any, initial []ToolArgumentCoercion) (any, []ToolArgumentError, []ToolArgumentCoercion) {
	properties, _ := schema["properties"].(map[string]any)
	required := stringSet(schema["required"])
	out := make(map[string]any, len(obj))
	var errors []ToolArgumentError
	coercions := append([]ToolArgumentCoercion{}, initial...)
	for field := range required {
		if _, ok := obj[field]; !ok {
			errors = append(errors, ToolArgumentError{Path: path + "." + field, Code: "required", Message: "required field is missing"})
		}
	}
	for field, value := range obj {
		propSchema, _ := properties[field].(map[string]any)
		if propSchema == nil {
			out[field] = value
			continue
		}
		next, errs, fieldCoercions := validateSchemaValue(path+"."+field, value, propSchema)
		errors = append(errors, errs...)
		coercions = append(coercions, fieldCoercions...)
		out[field] = next
	}
	if len(errors) > 0 {
		return nil, errors, coercions
	}
	return out, nil, coercions
}

func schemaType(schema map[string]any) string {
	switch typ := schema["type"].(type) {
	case string:
		return strings.ToLower(strings.TrimSpace(typ))
	case []any:
		for _, item := range typ {
			if text, ok := item.(string); ok && text != "null" {
				return strings.ToLower(strings.TrimSpace(text))
			}
		}
	}
	return ""
}

func isScalarType(typ string) bool {
	return typ == "string" || typ == "number" || typ == "integer" || typ == "boolean"
}

func scalarTypeName(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case float64, float32, int, int64, int32:
		return "number"
	case bool:
		return "boolean"
	default:
		return "value"
	}
}

func coerceNumber(value any, integer bool) (any, bool, bool) {
	switch typed := value.(type) {
	case float64:
		if integer {
			if math.Trunc(typed) != typed {
				return nil, false, false
			}
		}
		return typed, false, true
	case string:
		text := strings.TrimSpace(typed)
		if integer {
			n, err := strconv.ParseInt(text, 10, 64)
			return float64(n), true, err == nil
		}
		n, err := strconv.ParseFloat(text, 64)
		return n, true, err == nil
	default:
		return nil, false, false
	}
}

func validateEnum(path string, value any, schema map[string]any) *ToolArgumentError {
	raw := enumValues(schema["enum"])
	if len(raw) == 0 {
		return nil
	}
	valueJSON, _ := json.Marshal(value)
	for _, allowed := range raw {
		allowedJSON, _ := json.Marshal(allowed)
		if string(valueJSON) == string(allowedJSON) {
			return nil
		}
	}
	return &ToolArgumentError{Path: path, Code: "enum", Message: "value is not in enum"}
}

func enumValues(raw any) []any {
	switch values := raw.(type) {
	case []any:
		return values
	case []string:
		out := make([]any, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out
	default:
		return nil
	}
}

func stringSet(raw any) map[string]struct{} {
	out := map[string]struct{}{}
	switch typed := raw.(type) {
	case []any:
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out[text] = struct{}{}
			}
		}
	case []string:
		for _, text := range typed {
			if strings.TrimSpace(text) != "" {
				out[text] = struct{}{}
			}
		}
	}
	return out
}
