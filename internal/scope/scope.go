package scope

import "strings"

const (
	GlobalMemoryScope = "__or3_global__"
	GlobalScopeAlias  = "global"
)

func IsGlobalScopeRequest(v string) bool {
	v = strings.TrimSpace(v)
	return strings.EqualFold(v, GlobalScopeAlias) || v == GlobalMemoryScope
}
