package tools

import (
	"strings"
	"sync"
)

const (
	ToolNameReadArtifact     = "read_artifact"
	ToolNameReadFile         = "read_file"
	ToolNameSearchFile       = "search_file"
	ToolNameWriteFile        = "write_file"
	ToolNameEditFile         = "edit_file"
	ToolNameListDir          = "list_dir"
	ToolNameMemorySetPinned  = "memory_set_pinned"
	ToolNameMemoryAddNote    = "memory_add_note"
	ToolNameMemorySearch     = "memory_search"
	ToolNameMemoryRecent     = "memory_recent"
	ToolNameMemoryGetPinned  = "memory_get_pinned"
	ToolNameSendMessage      = "send_message"
	ToolNameReadSkill        = "read_skill"
	ToolNameRunSkill         = "run_skill"
	ToolNameRunSkillScript   = "run_skill_script"
	ToolNameExec             = "exec"
	ToolNameSpawnSubagent    = "spawn_subagent"
	ToolNameWebFetch         = "web_fetch"
	ToolNameWebFetchMarkdown = "web_fetch_markdown"
	ToolNameWebSearch        = "web_search"
	ToolNameCron             = "cron"
)

type AdviceProvider interface {
	FailureAdvice(params map[string]any, errText string) []string
}

var toolAdviceProviders = struct {
	mu        sync.RWMutex
	providers map[string]AdviceProvider
}{providers: map[string]AdviceProvider{}}

func init() {
	registerBuiltInAdviceProviders(
		&ReadArtifact{},
		&ReadFile{},
		&SearchFile{},
		&WriteFile{},
		&EditFile{},
		&ListDir{},
		&MemorySetPinned{},
		&MemoryAddNote{},
		&MemorySearch{},
		&MemoryRecent{},
		&MemoryGetPinned{},
		&SendMessage{},
		&ReadSkill{},
		&RunSkill{},
		&RunSkillScript{},
		&ExecTool{},
		&SpawnSubagent{},
		&WebFetch{},
		&WebFetchMarkdown{},
		&WebSearch{},
		&CronTool{},
	)
}

func registerBuiltInAdviceProviders(builtins ...Tool) {
	for _, tool := range builtins {
		registerToolAdviceProvider(tool)
	}
}

func registerToolAdviceProvider(tool Tool) {
	if tool == nil {
		return
	}
	provider, ok := tool.(AdviceProvider)
	if !ok {
		return
	}
	name := strings.TrimSpace(tool.Name())
	if name == "" {
		return
	}
	toolAdviceProviders.mu.Lock()
	toolAdviceProviders.providers[name] = provider
	toolAdviceProviders.mu.Unlock()
}

func adviceProviderForToolName(name string) AdviceProvider {
	toolAdviceProviders.mu.RLock()
	defer toolAdviceProviders.mu.RUnlock()
	return toolAdviceProviders.providers[strings.TrimSpace(name)]
}

func metadataForTool(tool Tool, groups ...string) ToolMetadata {
	meta := ToolMetadata{Groups: normalizeGroups(groups)}
	if tool != nil {
		meta.Capabilities = []string{string(ToolCapability(tool, nil))}
	}
	return meta
}

func IsExecutionToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolNameExec, ToolNameRunSkill, ToolNameRunSkillScript:
		return true
	default:
		return false
	}
}

func IsWriteToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolNameWriteFile, ToolNameEditFile:
		return true
	default:
		return false
	}
}

func IsWebFetchToolName(name string) bool {
	switch strings.TrimSpace(name) {
	case ToolNameWebFetch, ToolNameWebFetchMarkdown:
		return true
	default:
		return false
	}
}

func IsWebToolName(name string) bool {
	return IsWebFetchToolName(name) || strings.TrimSpace(name) == ToolNameWebSearch
}

func lowerToolError(errText string) string {
	return strings.ToLower(strings.TrimSpace(errText))
}

func memoryFailureAdvice(errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"If memory is unavailable, continue the task without depending on memory persistence or retrieval."}
	if strings.Contains(lowerErr, "empty query") {
		advice = append(advice, "Provide a concrete memory query such as a project name, person, or decision.")
	}
	if strings.Contains(lowerErr, "empty text") || strings.Contains(lowerErr, "missing key/content") {
		advice = append(advice, "Provide the durable fact, note text, or pinned key/content explicitly.")
	}
	return advice
}

func skillExecutionFailureAdvice(errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use read_skill first to inspect the skill instructions and available entrypoints before executing a script."}
	if strings.Contains(lowerErr, "requires approval") || strings.Contains(lowerErr, "approval required") {
		advice = append(advice, "Wait for approval, then retry the same skill name, entrypoint/path, and timeout or resume the returned plan_id so the approval token matches the frozen plan.")
	}
	if strings.Contains(lowerErr, "skill not found") {
		advice = append(advice, "Retry with the exact installed skill name.")
	}
	if strings.Contains(lowerErr, "missing path or entrypoint") {
		advice = append(advice, "Provide either an approved entrypoint name or a bundle-relative script path.")
	}
	return advice
}

func webFetchFailureAdvice(errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use a full http:// or https:// URL. If you only know a topic, call web_search first to discover a likely URL."}
	if strings.Contains(lowerErr, "invalid url") {
		advice = append(advice, "Retry with a complete URL instead of a search query or hostname fragment.")
	}
	if strings.Contains(lowerErr, "unsupported content type") {
		advice = append(advice, "Use web_fetch with raw=true or plain web_fetch when the target is not HTML that can be converted to Markdown.")
	}
	if strings.Contains(lowerErr, "playwright") || strings.Contains(lowerErr, "browser") || strings.Contains(lowerErr, "render") {
		advice = append(advice, "Retry without render=true unless JavaScript rendering is truly required.")
	}
	return advice
}

func (*ReadArtifact) Metadata() ToolMetadata { return metadataForTool(&ReadArtifact{}, ToolGroupRead) }

func (*ReadArtifact) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use the artifact_id returned by an earlier tool result exactly as given."}
	if strings.Contains(lowerErr, "missing artifact_id") {
		advice = append(advice, "Retry with the artifact_id from the earlier read_file, web_fetch, or web_fetch_markdown result.")
	}
	if strings.Contains(lowerErr, "not found") {
		advice = append(advice, "Retry with the exact artifact_id from the prior turn, or rerun the producing tool if the artifact no longer exists.")
	}
	return advice
}

func (*ReadFile) Metadata() ToolMetadata { return metadataForTool(&ReadFile{}, ToolGroupRead) }

func (*ReadFile) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{}
	if strings.Contains(lowerErr, "path outside allowed root") {
		advice = append(advice, "Choose a file path inside the current workspace or allowed read root. Use list_dir first if you need to discover the correct path.")
	}
	if strings.Contains(lowerErr, "missing pattern") {
		advice = append(advice, "For mode=grep, provide a specific pattern such as a symbol name, config key, or exact error string.")
	}
	if strings.Contains(lowerErr, "unsupported read_file mode") {
		advice = append(advice, "Use one of the supported modes: preview, full, range, grep, or outline.")
	}
	if strings.Contains(lowerErr, "no such file") || strings.Contains(lowerErr, "not exist") {
		advice = append(advice, "Use list_dir on the parent directory to confirm the file path, then retry read_file with the exact path.")
	}
	return advice
}

func (*SearchFile) Metadata() ToolMetadata { return metadataForTool(&SearchFile{}, ToolGroupRead) }

func (*SearchFile) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use read_file mode=outline or mode=preview first if you do not yet know what pattern to search for."}
	if strings.Contains(lowerErr, "path outside allowed root") {
		advice = append(advice, "Choose a file path inside the current workspace or allowed read root.")
	}
	if strings.Contains(lowerErr, "missing pattern") {
		advice = append(advice, "Provide a non-empty pattern such as a symbol, config key, or exact error message.")
	}
	return advice
}

func (*WriteFile) Metadata() ToolMetadata { return metadataForTool(&WriteFile{}, ToolGroupWrite) }

func (*WriteFile) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use edit_file instead when you only need a localized change in an existing file."}
	if strings.Contains(lowerErr, "path outside allowed root") {
		advice = append(advice, "Write the file inside the configured writable workspace root.")
	}
	if strings.Contains(lowerErr, "no such file") || strings.Contains(lowerErr, "not exist") {
		advice = append(advice, "If you are creating a new file in a new directory, retry with mkdirs=true or choose an existing parent directory.")
	}
	return advice
}

func (*EditFile) Metadata() ToolMetadata { return metadataForTool(&EditFile{}, ToolGroupWrite) }

func (*EditFile) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use write_file only when you intend to replace the full file content."}
	if strings.Contains(lowerErr, "path outside allowed root") {
		advice = append(advice, "Edit a file inside the configured writable workspace root.")
	}
	if strings.Contains(lowerErr, "no such file") || strings.Contains(lowerErr, "not exist") {
		advice = append(advice, "Confirm the file already exists with list_dir or create it first with write_file.")
	}
	return advice
}

func (*ListDir) Metadata() ToolMetadata { return metadataForTool(&ListDir{}, ToolGroupRead) }

func (*ListDir) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{}
	if strings.Contains(lowerErr, "path is not a directory") {
		advice = append(advice, "Use read_file for files. list_dir only works on directories.")
	}
	if strings.Contains(lowerErr, "path outside allowed root") {
		advice = append(advice, "Choose a directory inside the current workspace or allowed read root.")
	}
	return advice
}

func (*MemorySetPinned) Metadata() ToolMetadata {
	return metadataForTool(&MemorySetPinned{}, ToolGroupMemory, ToolGroupRead)
}
func (*MemoryAddNote) Metadata() ToolMetadata {
	return metadataForTool(&MemoryAddNote{}, ToolGroupMemory, ToolGroupRead)
}
func (*MemorySearch) Metadata() ToolMetadata {
	return metadataForTool(&MemorySearch{}, ToolGroupMemory, ToolGroupRead)
}
func (*MemoryRecent) Metadata() ToolMetadata {
	return metadataForTool(&MemoryRecent{}, ToolGroupMemory, ToolGroupRead)
}
func (*MemoryGetPinned) Metadata() ToolMetadata {
	return metadataForTool(&MemoryGetPinned{}, ToolGroupMemory, ToolGroupRead)
}

func (*MemorySetPinned) FailureAdvice(_ map[string]any, errText string) []string {
	return memoryFailureAdvice(errText)
}
func (*MemoryAddNote) FailureAdvice(_ map[string]any, errText string) []string {
	return memoryFailureAdvice(errText)
}
func (*MemorySearch) FailureAdvice(_ map[string]any, errText string) []string {
	return memoryFailureAdvice(errText)
}
func (*MemoryRecent) FailureAdvice(_ map[string]any, errText string) []string {
	return memoryFailureAdvice(errText)
}
func (*MemoryGetPinned) FailureAdvice(_ map[string]any, errText string) []string {
	return memoryFailureAdvice(errText)
}

func (*SendMessage) Metadata() ToolMetadata {
	return metadataForTool(&SendMessage{}, ToolGroupChannels)
}

func (*SendMessage) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Only use send_message when external delivery is part of the task; otherwise answer in the current conversation."}
	if strings.Contains(lowerErr, "message requires text or media") {
		advice = append(advice, "Provide message text, media paths, or both.")
	}
	if strings.Contains(lowerErr, "reply_in_thread") {
		advice = append(advice, "Keep the current channel/recipient when reply_in_thread=true, or disable reply_in_thread when changing targets.")
	}
	if strings.Contains(lowerErr, "deliver not configured") {
		advice = append(advice, "Respond in the current conversation or use a channel that is configured for delivery.")
	}
	return advice
}

func (*ReadSkill) Metadata() ToolMetadata {
	return metadataForTool(&ReadSkill{}, ToolGroupSkills, ToolGroupRead)
}

func (*ReadSkill) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use the exact skill name from the inventory. If you only need a quick overview, prefer mode=outline or mode=preview."}
	if strings.Contains(lowerErr, "skill not found") {
		advice = append(advice, "Retry with an exact installed skill name.")
	}
	return advice
}

func (*RunSkill) Metadata() ToolMetadata {
	return metadataForTool(&RunSkill{}, ToolGroupSkills, ToolGroupExec)
}
func (*RunSkillScript) Metadata() ToolMetadata {
	return metadataForTool(&RunSkillScript{}, ToolGroupSkills, ToolGroupExec)
}

func (*RunSkill) FailureAdvice(_ map[string]any, errText string) []string {
	return skillExecutionFailureAdvice(errText)
}
func (*RunSkillScript) FailureAdvice(_ map[string]any, errText string) []string {
	return skillExecutionFailureAdvice(errText)
}

func (*ExecTool) Metadata() ToolMetadata { return metadataForTool(&ExecTool{}, ToolGroupExec) }

func (*ExecTool) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Prefer program + args over command strings, and keep the request to one direct program invocation."}
	if strings.Contains(lowerErr, "approval required") {
		advice = append(advice, "Wait for approval, then retry the exact same tool call so the approval token matches the same subject.")
		advice = append(advice, "For exec, keep the same program and args after approval; changing argv creates a different approval subject.")
	}
	if strings.Contains(lowerErr, "shell command execution disabled") || strings.Contains(lowerErr, "shell syntax is not allowed") {
		advice = append(advice, "Split shell pipelines into separate direct exec calls, or use a single program invocation with explicit args.")
	}
	if strings.Contains(lowerErr, "program not allowed") {
		advice = append(advice, "Use an allowed program, or switch to a read-only tool like read_file, search_file, or list_dir if that can answer the question.")
	}
	if strings.Contains(lowerErr, "executable file not found") || strings.Contains(lowerErr, "not found") {
		advice = append(advice, "Retry with an absolute executable path or with a different installed program name.")
	}
	if strings.Contains(lowerErr, "cwd outside allowed directory") {
		advice = append(advice, "Omit cwd to use the workspace default, or provide a cwd inside the allowed workspace root.")
	}
	return advice
}

func (*SpawnSubagent) Metadata() ToolMetadata {
	return metadataForTool(&SpawnSubagent{}, ToolGroupService)
}

func (*SpawnSubagent) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use spawn_subagent only for work that can continue independently in the background."}
	if strings.Contains(lowerErr, "disabled") {
		advice = append(advice, "Handle the work in the current turn instead of delegating it to a background subagent.")
	}
	if strings.Contains(lowerErr, "empty task") {
		advice = append(advice, "Provide a complete, self-contained task description with the expected output.")
	}
	return advice
}

func (*WebFetch) Metadata() ToolMetadata { return metadataForTool(&WebFetch{}, ToolGroupWeb) }
func (*WebFetchMarkdown) Metadata() ToolMetadata {
	return metadataForTool(&WebFetchMarkdown{}, ToolGroupWeb)
}
func (*WebSearch) Metadata() ToolMetadata { return metadataForTool(&WebSearch{}, ToolGroupWeb) }

func (*WebFetch) FailureAdvice(_ map[string]any, errText string) []string {
	return webFetchFailureAdvice(errText)
}
func (*WebFetchMarkdown) FailureAdvice(_ map[string]any, errText string) []string {
	return webFetchFailureAdvice(errText)
}

func (*WebSearch) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Keep the query specific: include the project, product, person, or exact error text you need."}
	if strings.Contains(lowerErr, "api key not configured") {
		advice = append(advice, "If you already know the target URL, use web_fetch directly instead of web_search.")
	}
	return advice
}

func (*CronTool) Metadata() ToolMetadata { return metadataForTool(&CronTool{}, ToolGroupCron) }

func (*CronTool) FailureAdvice(_ map[string]any, errText string) []string {
	lowerErr := lowerToolError(errText)
	advice := []string{"Use cron for future or recurring work, not for one-turn actions that should happen now."}
	if strings.Contains(lowerErr, "unknown action") {
		advice = append(advice, "Use one of: add, list, remove, run, or status.")
	}
	if strings.Contains(lowerErr, "missing job") {
		advice = append(advice, "For action=add, include a complete job object with schedule and payload.")
	}
	return advice
}
