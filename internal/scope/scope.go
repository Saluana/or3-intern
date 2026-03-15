// Package scope defines shared session-scope identifiers and helpers.
package scope

import "strings"

const (
	// GlobalMemoryScope is the canonical scope key used for shared global memory.
	GlobalMemoryScope = "__or3_global__"
	// GlobalScopeAlias is the user-facing alias for the global scope.
	GlobalScopeAlias = "global"
)

// IsGlobalScopeRequest reports whether v refers to the global memory scope.
func IsGlobalScopeRequest(v string) bool {
	v = strings.TrimSpace(v)
	return strings.EqualFold(v, GlobalScopeAlias) || v == GlobalMemoryScope
}
