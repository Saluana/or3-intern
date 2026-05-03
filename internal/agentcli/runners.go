// Package agentcli defines the typed runner registry, detection, and adapter
// contracts for external agent CLI delegation.
package agentcli

import (
	"context"
	"encoding/json"
)

// RunnerID uniquely identifies an external agent CLI runner.
type RunnerID string

const (
	RunnerOpenCode RunnerID = "opencode"
	RunnerCodex    RunnerID = "codex"
	RunnerClaude   RunnerID = "claude"
	RunnerGemini   RunnerID = "gemini"
	RunnerOR3      RunnerID = "or3-intern"
)

// RunnerMode describes the autonomy level requested for a run.
type RunnerMode string

const (
	RunnerModeReview      RunnerMode = "review"
	RunnerModeSafeEdit    RunnerMode = "safe_edit"
	RunnerModeSandboxAuto RunnerMode = "sandbox_auto"
)

// RunIsolation describes the filesystem/sandbox boundary for a run.
type RunIsolation string

const (
	IsolationHostReadOnly       RunIsolation = "host_readonly"
	IsolationHostWorkspaceWrite RunIsolation = "host_workspace_write"
	IsolationSandboxWrite       RunIsolation = "sandbox_workspace_write"
	IsolationSandboxDangerous   RunIsolation = "sandbox_dangerous"
)

// RunnerStatus describes detection readiness of a runner binary.
type RunnerStatus string

const (
	RunnerStatusAvailable          RunnerStatus = "available"
	RunnerStatusMissing            RunnerStatus = "missing"
	RunnerStatusNotExecutable      RunnerStatus = "not_executable"
	RunnerStatusAuthMissing        RunnerStatus = "auth_missing"
	RunnerStatusAuthUnknown        RunnerStatus = "auth_unknown"
	RunnerStatusUnsupportedVersion RunnerStatus = "unsupported_version"
	RunnerStatusDisabledByConfig   RunnerStatus = "disabled_by_config"
	RunnerStatusError              RunnerStatus = "error"
)

// AuthStatus describes CLI auth readiness.
type AuthStatus string

const (
	AuthReady   AuthStatus = "ready"
	AuthMissing AuthStatus = "missing"
	AuthUnknown AuthStatus = "unknown"
)

// OutputMode describes how the CLI output is formatted.
type OutputMode string

const (
	OutputPlain OutputMode = "plain"
	OutputJSONL OutputMode = "jsonl"
	OutputJSON  OutputMode = "json"
)

// RunnerSupports declares which features an adapter supports.
type RunnerSupports struct {
	StructuredOutput    bool `json:"structuredOutput"`
	StreamingJSON       bool `json:"streamingJson"`
	ModelFlag           bool `json:"modelFlag"`
	PermissionsMode     bool `json:"permissionsMode"`
	SafeSandboxFlag     bool `json:"safeSandboxFlag"`
	DangerousBypassFlag bool `json:"dangerousBypassFlag"`
	StdinPrompt         bool `json:"stdinPrompt"`
}

// SmallCommandSpec describes a small probe command (version or auth).
type SmallCommandSpec struct {
	Args    []string
	Timeout int
}

// RunnerSpec describes a runner: binary name, detection commands, and capabilities.
type RunnerSpec struct {
	ID          RunnerID       `json:"id"`
	DisplayName string         `json:"displayName"`
	Binary      string         `json:"binary"`
	VersionArgs []string       `json:"versionArgs"`
	AuthCheck   *SmallCommandSpec `json:"authCheck,omitempty"`
	Supports    RunnerSupports `json:"supports"`
}

// RunnerInfo is the detection result returned by the API.
type RunnerInfo struct {
	ID                 string         `json:"id"`
	DisplayName        string         `json:"display_name"`
	BinaryName         string         `json:"binary_name"`
	BinaryPath         string         `json:"binary_path,omitempty"`
	Version            string         `json:"version,omitempty"`
	Status             RunnerStatus   `json:"status"`
	DisabledReason     string         `json:"disabled_reason,omitempty"`
	AuthStatus         AuthStatus     `json:"auth_status"`
	Supports           RunnerSupports `json:"supports"`
	DefaultArgsPreview []string       `json:"default_args_preview"`
}

// AgentRunRequest is the validated input for starting a CLI run.
type AgentRunRequest struct {
	ParentSessionKey string
	RunnerID         string
	Task             string
	TimeoutSeconds   int
	Cwd              string
	Model            string
	Mode             string
	Isolation        string
	MaxTurns         int
	Meta             map[string]any
}

// CommandSpec is the executable command built by an adapter.
type CommandSpec struct {
	Binary        string     `json:"binary"`
	Args          []string   `json:"args"`
	Env           []string   `json:"env,omitempty"`
	Cwd           string     `json:"cwd,omitempty"`
	PromptOnStdin bool       `json:"promptOnStdin"`
	Stdin         []byte     `json:"-"`
	OutputMode    OutputMode `json:"outputMode"`
	ArgvPreview   []string   `json:"argvPreview"`
}

// AgentRunEvent is a persisted and published normalized event.
type AgentRunEvent struct {
	Type       string          `json:"type"`
	TS         string          `json:"ts"`
	Seq        int64           `json:"seq,omitempty"`
	JobID      string          `json:"job_id,omitempty"`
	RunnerID   string          `json:"runner_id,omitempty"`
	Stream     string          `json:"stream,omitempty"`
	Chunk      string          `json:"chunk,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	ExitCode   *int            `json:"exit_code,omitempty"`
	Status     string          `json:"status,omitempty"`
	Message    string          `json:"message,omitempty"`
	DurationMS int64           `json:"duration_ms,omitempty"`
}

// RunnerAdapter builds commands for a specific external CLI tool.
type RunnerAdapter interface {
	ID() RunnerID
	DisplayName() string
	Spec() RunnerSpec
	Detect(ctx context.Context, opts DetectOptions) RunnerInfo
	BuildCommand(req AgentRunRequest) (CommandSpec, error)
}

// DetectOptions control detection behavior.
type DetectOptions struct {
	WorkDir        string
	Env            []string
	DisabledRunners []string
}