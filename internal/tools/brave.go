package tools

import "strings"

const braveSearchHost = "api.search.brave.com"

// BraveSearchConfigured reports whether a Brave Search API key is configured.
func BraveSearchConfigured(apiKey string) bool {
	return strings.TrimSpace(apiKey) != ""
}

// AppendBraveSearchHostIfMissing adds the Brave Search API host to allowed hosts.
func AppendBraveSearchHostIfMissing(allowedHosts []string) []string {
	for _, host := range allowedHosts {
		if strings.EqualFold(strings.TrimSpace(host), braveSearchHost) {
			return allowedHosts
		}
	}
	return append(append([]string{}, allowedHosts...), braveSearchHost)
}
