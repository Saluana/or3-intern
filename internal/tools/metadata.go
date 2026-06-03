package tools

const (
	ToolGroupService = "service"
)

// ToolMetadata describes doctor/service action grouping for provider schemas.
type ToolMetadata struct {
	Groups       []string
	Capabilities []string
	Hidden       bool
}
