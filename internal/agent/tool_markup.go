package agent

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"or3-intern/internal/providers"
)

var toolMarkupPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?is)<tool_call>[\s\S]*?</tool_call>`),
	regexp.MustCompile(`(?is)<tool_call[\s\S]*$`),
	regexp.MustCompile(`(?i)</?tool_call>`),
	regexp.MustCompile(`(?i)<function=[^>]*>`),
	regexp.MustCompile(`(?is)<function=[\s\S]*$`),
	regexp.MustCompile(`(?i)<parameter=[^>]*>`),
	regexp.MustCompile(`(?is)<parameter[\s\S]*$`),
	regexp.MustCompile(`(?is)<\s*[|｜]\s*DSML\s*[|｜]\s*tool_calls\s*>[\s\S]*?<\s*/\s*[|｜]\s*DSML\s*[|｜]\s*tool_calls\s*>`),
	regexp.MustCompile(`(?is)<\s*[|｜]\s*DSML\s*[|｜]\s*tool_calls[\s\S]*$`),
	regexp.MustCompile(`(?is)<\s*/?\s*[|｜]\s*DSML\s*[|｜]\s*(?:invoke|parameter)[^>]*>`),
}

var (
	toolMarkupBlockPattern            = regexp.MustCompile(`(?is)<tool_call\b[^>]*>(.*?)</tool_call>`)
	toolMarkupFunctionPattern         = regexp.MustCompile(`(?is)<function=([^>\s]+)>\s*`)
	toolMarkupParameterPattern        = regexp.MustCompile(`(?is)<parameter=([^>\s]+)>\s*`)
	toolMarkupClosingPattern          = regexp.MustCompile(`(?is)</(?:parameter|function)>\s*$`)
	toolMarkupNameElementPattern      = regexp.MustCompile(`(?is)<name>\s*(.*?)\s*</name>`)
	toolMarkupArgumentsElementPattern = regexp.MustCompile(`(?is)<arguments>\s*(.*?)\s*</arguments>`)
	dsmlToolCallsBlockPattern         = regexp.MustCompile(`(?is)<\s*[|｜]\s*DSML\s*[|｜]\s*tool_calls\s*>(.*?)<\s*/\s*[|｜]\s*DSML\s*[|｜]\s*tool_calls\s*>`)
	dsmlInvokePattern                 = regexp.MustCompile(`(?is)<\s*[|｜]\s*DSML\s*[|｜]\s*invoke\b([^>]*)>(.*?)<\s*/\s*[|｜]\s*DSML\s*[|｜]\s*invoke\s*>`)
	dsmlParameterPattern              = regexp.MustCompile(`(?is)<\s*[|｜]\s*DSML\s*[|｜]\s*parameter\b([^>]*)>(.*?)<\s*/\s*[|｜]\s*DSML\s*[|｜]\s*parameter\s*>`)
	markupNameAttrPattern             = regexp.MustCompile(`(?is)\bname\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

func sanitizeToolTurnContent(text string) string {
	cleaned := text
	for _, pattern := range toolMarkupPatterns {
		cleaned = pattern.ReplaceAllString(cleaned, "")
	}
	return strings.TrimSpace(cleaned)
}

func parseToolMarkupCalls(text string, idPrefix string) []providers.ToolCall {
	matches := toolMarkupBlockPattern.FindAllStringSubmatch(text, -1)
	out := make([]providers.ToolCall, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		block := match[1]
		name, args, ok := parseToolMarkupBlock(block)
		if !ok {
			continue
		}
		index := len(out)
		tc := providers.ToolCall{
			ID:    fmt.Sprintf("%s_%d", idPrefix, index+1),
			Index: index,
			Type:  "function",
		}
		tc.Function.Name = name
		tc.Function.Arguments = args
		out = append(out, tc)
	}
	for _, match := range dsmlToolCallsBlockPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		out = append(out, parseDSMLToolMarkupCalls(match[1], idPrefix, len(out))...)
	}
	return out
}

func parseToolMarkupBlock(block string) (string, string, bool) {
	block = strings.TrimSpace(html.UnescapeString(block))
	if block == "" {
		return "", "", false
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(block), &object); err == nil {
		name := strings.TrimSpace(fmt.Sprint(object["name"]))
		if name == "" {
			return "", "", false
		}
		args := object["arguments"]
		if args == nil {
			args = map[string]any{}
		}
		encoded, err := json.Marshal(args)
		if err != nil {
			return "", "", false
		}
		return name, string(encoded), true
	}
	if nameMatch := toolMarkupNameElementPattern.FindStringSubmatch(block); len(nameMatch) >= 2 {
		name := strings.TrimSpace(html.UnescapeString(nameMatch[1]))
		if name == "" {
			return "", "", false
		}
		args := "{}"
		if argMatch := toolMarkupArgumentsElementPattern.FindStringSubmatch(block); len(argMatch) >= 2 {
			rawArgs := strings.TrimSpace(html.UnescapeString(argMatch[1]))
			if rawArgs != "" {
				var decoded any
				if err := json.Unmarshal([]byte(rawArgs), &decoded); err == nil {
					if encoded, err := json.Marshal(decoded); err == nil {
						args = string(encoded)
					}
				} else {
					params := parseToolMarkupParams(rawArgs)
					if len(params) > 0 {
						if encoded, err := json.Marshal(params); err == nil {
							args = string(encoded)
						}
					} else if encoded, err := json.Marshal(map[string]any{"value": rawArgs}); err == nil {
						args = string(encoded)
					}
				}
			}
		}
		return name, args, true
	}
	functionMatch := toolMarkupFunctionPattern.FindStringSubmatch(block)
	if len(functionMatch) < 2 {
		return "", "", false
	}
	name := strings.TrimSpace(html.UnescapeString(functionMatch[1]))
	if name == "" {
		return "", "", false
	}
	params := parseToolMarkupParams(block)
	args, err := json.Marshal(params)
	if err != nil {
		return "", "", false
	}
	return name, string(args), true
}

func parseDSMLToolMarkupCalls(block string, idPrefix string, offset int) []providers.ToolCall {
	matches := dsmlInvokePattern.FindAllStringSubmatch(block, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]providers.ToolCall, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := markupNameAttr(match[1])
		if name == "" {
			continue
		}
		params := parseDSMLToolMarkupParams(match[2])
		args, err := json.Marshal(params)
		if err != nil {
			continue
		}
		index := offset + len(out)
		tc := providers.ToolCall{
			ID:    fmt.Sprintf("%s_%d", idPrefix, index+1),
			Index: index,
			Type:  "function",
		}
		tc.Function.Name = name
		tc.Function.Arguments = string(args)
		out = append(out, tc)
	}
	return out
}

func parseDSMLToolMarkupParams(block string) map[string]any {
	params := map[string]any{}
	matches := dsmlParameterPattern.FindAllStringSubmatch(block, -1)
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		name := markupNameAttr(match[1])
		if name == "" {
			continue
		}
		params[name] = parseToolMarkupParamValue(html.UnescapeString(match[2]))
	}
	return params
}

func markupNameAttr(attrs string) string {
	match := markupNameAttrPattern.FindStringSubmatch(attrs)
	if len(match) < 3 {
		return ""
	}
	name := match[1]
	if name == "" {
		name = match[2]
	}
	return strings.TrimSpace(html.UnescapeString(name))
}

func parseToolMarkupParams(block string) map[string]any {
	params := map[string]any{}
	matches := toolMarkupParameterPattern.FindAllStringSubmatchIndex(block, -1)
	for i, match := range matches {
		if len(match) < 4 {
			continue
		}
		name := strings.TrimSpace(html.UnescapeString(block[match[2]:match[3]]))
		if name == "" {
			continue
		}
		start := match[1]
		end := len(block)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		value := strings.TrimSpace(block[start:end])
		value = strings.TrimSpace(toolMarkupClosingPattern.ReplaceAllString(value, ""))
		params[name] = parseToolMarkupParamValue(html.UnescapeString(value))
	}
	return params
}

func parseToolMarkupParamValue(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	first := value[0]
	if !strings.ContainsRune(`{["-0123456789tfn`, rune(first)) {
		return value
	}
	var parsed any
	if err := json.Unmarshal([]byte(value), &parsed); err == nil {
		return parsed
	}
	return value
}
