package providers

import "strings"

const (
	StreamTextModeDelta           = "delta"
	StreamTextModeSnapshotOrDelta = "snapshot_or_delta"
	StreamToolCallModeOpenAIIndex = "openai_indexed"
)

type ProviderProfile struct {
	Name       string
	ToolSchema ToolSchemaPolicy
	Streaming  StreamPolicy
	Retry      ProviderRetryPolicy
}

type ToolSchemaPolicy struct {
	AllowAdditionalProperties bool
	DropUnsupportedKeywords   []string
	RequireObjectRoot         bool
	MaxDescriptionRunes       int
}

type StreamPolicy struct {
	TextMode       string
	ToolCallMode   string
	RetryMalformed bool
}

type ProviderRetryPolicy struct {
	RetryEmptyStream           bool
	RetryMalformedBeforeOutput bool
	FallbackToNonStream        bool
}

func OpenAICompatibleProfile() ProviderProfile {
	return ProviderProfile{
		Name: "openai_compatible",
		ToolSchema: ToolSchemaPolicy{
			AllowAdditionalProperties: true,
			DropUnsupportedKeywords:   []string{"$schema", "examples", "default"},
			RequireObjectRoot:         true,
			MaxDescriptionRunes:       1200,
		},
		Streaming: StreamPolicy{
			TextMode:       StreamTextModeSnapshotOrDelta,
			ToolCallMode:   StreamToolCallModeOpenAIIndex,
			RetryMalformed: true,
		},
		Retry: ProviderRetryPolicy{
			RetryEmptyStream:           true,
			RetryMalformedBeforeOutput: true,
			FallbackToNonStream:        true,
		},
	}
}

func OpenRouterCompatibleProfile() ProviderProfile {
	profile := OpenAICompatibleProfile()
	profile.Name = "openrouter_compatible"
	profile.ToolSchema.AllowAdditionalProperties = false
	profile.ToolSchema.DropUnsupportedKeywords = []string{
		"$schema",
		"examples",
		"default",
		"nullable",
		"readOnly",
		"writeOnly",
		"deprecated",
		"$defs",
		"oneOf",
		"anyOf",
		"allOf",
	}
	return profile
}

func LocalCompatibleProfile() ProviderProfile {
	profile := OpenAICompatibleProfile()
	profile.Name = "local_compatible"
	profile.ToolSchema.DropUnsupportedKeywords = []string{"$schema"}
	profile.ToolSchema.MaxDescriptionRunes = 2000
	profile.Streaming.RetryMalformed = false
	profile.Retry.RetryMalformedBeforeOutput = false
	profile.Retry.FallbackToNonStream = false
	return profile
}

func SelectProviderProfile(providerName, apiBase, model string) ProviderProfile {
	combined := strings.ToLower(strings.TrimSpace(providerName + " " + apiBase + " " + model))
	switch {
	case strings.Contains(combined, "openrouter"):
		return OpenRouterCompatibleProfile()
	case strings.Contains(combined, "ollama") ||
		strings.Contains(combined, "lmstudio") ||
		strings.Contains(combined, "local"):
		return LocalCompatibleProfile()
	default:
		return OpenAICompatibleProfile()
	}
}

func (c *Client) ProviderProfile(model string) ProviderProfile {
	if c == nil {
		return OpenAICompatibleProfile()
	}
	return SelectProviderProfile("", c.APIBase, model)
}
