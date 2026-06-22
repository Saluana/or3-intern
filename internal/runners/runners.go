// Package runners defines the typed runner registry, detection, and adapter
// contracts for external runner delegation.
package runners

import (
	"context"
	"encoding/json"
)

// RunnerID uniquely identifies an external runner runner.
type RunnerID string

const (
	RunnerOpenCode RunnerID = "opencode"
	RunnerCodex    RunnerID = "codex"
	RunnerClaude   RunnerID = "claude"
	RunnerGemini   RunnerID = "gemini"
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

// RunnerRuntimeKind describes how a runner turn will be executed.
type RunnerRuntimeKind string

const (
	RuntimeCLI    RunnerRuntimeKind = "cli"
	RuntimeNative RunnerRuntimeKind = "native"
)

// RunnerRuntimeMode controls whether native runtime backends are attempted.
type RunnerRuntimeMode string

const (
	RuntimeModeAuto   RunnerRuntimeMode = "auto"
	RuntimeModeNative RunnerRuntimeMode = "native"
	RuntimeModeCLI    RunnerRuntimeMode = "cli"
)

// RunnerRuntimeOwnership records whether OR3 owns a helper process.
type RunnerRuntimeOwnership string

const (
	RuntimeOwnershipNone     RunnerRuntimeOwnership = "none"
	RuntimeOwnershipManaged  RunnerRuntimeOwnership = "managed"
	RuntimeOwnershipExternal RunnerRuntimeOwnership = "external"
	RuntimeOwnershipUnknown  RunnerRuntimeOwnership = "unknown"
)

// RunnerRuntimeState is safe, user-facing readiness for a native runtime.
type RunnerRuntimeState string

const (
	RuntimeStateUnavailable RunnerRuntimeState = "unavailable"
	RuntimeStateReady       RunnerRuntimeState = "ready"
	RuntimeStateStarting    RunnerRuntimeState = "starting"
	RuntimeStateError       RunnerRuntimeState = "error"
	RuntimeStateFallback    RunnerRuntimeState = "fallback"
)

// RunnerModelInfo is model metadata exposed to the app selector.
type RunnerModelInfo struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name,omitempty"`
	Provider         string   `json:"provider,omitempty"`
	ProviderName     string   `json:"provider_name,omitempty"`
	Default          bool     `json:"default,omitempty"`
	Reasoning        []string `json:"reasoning,omitempty"`
	ReasoningDefault string   `json:"reasoning_default,omitempty"`
	// Options are per-model option descriptors (reasoning, fast mode, etc).
	Options []RunnerModelOption `json:"options,omitempty"`
	// Capabilities describes what the model/runner supports for this entry.
	Capabilities RunnerModelCapabilities `json:"capabilities,omitempty"`
}

// RunnerModelOption describes a configurable option for a model entry.
type RunnerModelOption struct {
	ID           string                   `json:"id"`
	Label        string                   `json:"label,omitempty"`
	Description  string                   `json:"description,omitempty"`
	Type         string                   `json:"type"`
	Values       []RunnerModelOptionValue `json:"values,omitempty"`
	DefaultValue string                   `json:"default_value,omitempty"`
	CurrentValue string                   `json:"current_value,omitempty"`
}

// RunnerModelOptionValue describes one selectable value for a select option.
type RunnerModelOptionValue struct {
	Value       string `json:"value"`
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// RunnerModelCapabilities lists feature flags the runner advertises for a model.
type RunnerModelCapabilities struct {
	ReasoningEffort bool `json:"reasoning_effort,omitempty"`
	FastMode        bool `json:"fast_mode,omitempty"`
	Variants        bool `json:"variants,omitempty"`
	Agents          bool `json:"agents,omitempty"`
	ToolUse         bool `json:"tool_use,omitempty"`
}

// RunnerProviderInfo is a single provider entry from server discovery.
type RunnerProviderInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
	// Source identifies how the provider was discovered (server, cli, static).
	Source string `json:"source,omitempty"`
}

// RunnerAgentInfo describes a runner-defined agent/persona entry.
type RunnerAgentInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Default     bool   `json:"default,omitempty"`
	BuiltIn     bool   `json:"built_in,omitempty"`
	Mode        string `json:"mode,omitempty"`
}

// RunnerNativeHealth is a small snapshot of native backend readiness.
type RunnerNativeHealth struct {
	Reachable      bool   `json:"reachable"`
	Endpoint       string `json:"endpoint,omitempty"`
	LatencyMS      int64  `json:"latency_ms,omitempty"`
	ServerVersion  string `json:"server_version,omitempty"`
	LastCheckedAt  int64  `json:"last_checked_at,omitempty"`
	Detail         string `json:"detail,omitempty"`
	AuthStatus     string `json:"auth_status,omitempty"`
	AuthDetail     string `json:"auth_detail,omitempty"`
	StartedAt      int64  `json:"started_at,omitempty"`
	IdleTimeoutSec int    `json:"idle_timeout_seconds,omitempty"`
}

// RunnerRuntimeInfo is discovery/status metadata for native-first backends.
type RunnerRuntimeInfo struct {
	Kind           RunnerRuntimeKind      `json:"kind"`
	Mode           RunnerRuntimeMode      `json:"mode"`
	State          RunnerRuntimeState     `json:"state"`
	Ownership      RunnerRuntimeOwnership `json:"ownership"`
	Endpoint       string                 `json:"endpoint,omitempty"`
	Message        string                 `json:"message,omitempty"`
	Fallback       bool                   `json:"fallback"`
	FallbackReason string                 `json:"fallback_reason,omitempty"`
	NextAction     string                 `json:"next_action,omitempty"`
	Version        string                 `json:"version,omitempty"`
	AuthStatus     AuthStatus             `json:"auth_status,omitempty"`
	AuthDetail     string                 `json:"auth_detail,omitempty"`
	Models         []RunnerModelInfo      `json:"models,omitempty"`
	DefaultModel   string                 `json:"default_model,omitempty"`
	Providers      []RunnerProviderInfo   `json:"providers,omitempty"`
	Agents         []RunnerAgentInfo      `json:"agents,omitempty"`
	Health         *RunnerNativeHealth    `json:"health,omitempty"`
	Refs           RunnerRuntimeRefs      `json:"refs,omitempty"`
	Options        []RunnerRuntimeOption  `json:"options,omitempty"`
	Skills         []string               `json:"skills,omitempty"`
}

// RunnerRuntimeRefs are caller-supplied references to a live runtime (e.g. an
// app-server session id, a managed server PID, or a request id awaiting reply).
type RunnerRuntimeRefs struct {
	SessionID     string `json:"session_id,omitempty"`
	ThreadID      string `json:"thread_id,omitempty"`
	TurnID        string `json:"turn_id,omitempty"`
	ProcessID     int    `json:"process_id,omitempty"`
	ServerID      string `json:"server_id,omitempty"`
	LastRequestID string `json:"last_request_id,omitempty"`
}

// RunnerRuntimeOption describes a runtime-level option (e.g. fast mode).
type RunnerRuntimeOption struct {
	ID           string                   `json:"id"`
	Label        string                   `json:"label,omitempty"`
	Description  string                   `json:"description,omitempty"`
	Type         string                   `json:"type"`
	Values       []RunnerModelOptionValue `json:"values,omitempty"`
	DefaultValue string                   `json:"default_value,omitempty"`
	CurrentValue string                   `json:"current_value,omitempty"`
}

// OutputMode describes how the CLI output is formatted.
type OutputMode string

const (
	OutputPlain OutputMode = "plain"
	OutputJSONL OutputMode = "jsonl"
	OutputJSON  OutputMode = "json"
)

// RunnerChatCapabilities declares which chat-specific behaviors a runner
// adapter supports. Defaults are conservative (everything off).
type RunnerChatCapabilities struct {
	// ChatSelectable means the runner can be picked as the active chat
	// transport in the composer.
	ChatSelectable bool `json:"chatSelectable"`
	// ChatReplay means the adapter supports building a bounded transcript
	// replay prompt and executing one chat turn at a time.
	ChatReplay bool `json:"chatReplay"`
	// ChatNativeSession means the adapter exposes a stable session reference
	// and a specific-session resume command that has been verified by tests.
	ChatNativeSession bool `json:"chatNativeSession"`
	// ChatResume means the adapter can resume a specific session by ID
	// (not "continue latest", which is process-global and unsafe for
	// concurrent chats).
	ChatResume bool `json:"chatResume"`
	// ChatSessionRefExtractable means the adapter can deterministically
	// extract a stable session ref from runner output.
	ChatSessionRefExtractable bool `json:"chatSessionRefExtractable"`
	// StreamToolEvents means the adapter normalizes tool_call/tool_result
	// events into the runner chat event stream.
	StreamToolEvents bool `json:"streamToolEvents"`
	// SupportsNativeFork means the adapter exposes a specific-session,
	// specific-state fork primitive. Replay-mode fork is the universal
	// fallback when this is false.
	SupportsNativeFork bool `json:"supportsNativeFork"`
}

// RunnerSupports declares which features an adapter supports.
type RunnerSupports struct {
	StructuredOutput    bool                   `json:"structuredOutput"`
	StreamingJSON       bool                   `json:"streamingJson"`
	ModelFlag           bool                   `json:"modelFlag"`
	PermissionsMode     bool                   `json:"permissionsMode"`
	SafeSandboxFlag     bool                   `json:"safeSandboxFlag"`
	DangerousBypassFlag bool                   `json:"dangerousBypassFlag"`
	StdinPrompt         bool                   `json:"stdinPrompt"`
	Chat                RunnerChatCapabilities `json:"chat"`
}

// SmallCommandSpec describes a small probe command (version or auth).
type SmallCommandSpec struct {
	Args    []string
	Timeout int
}

// RunnerSpec describes a runner: binary name, detection commands, and capabilities.
type RunnerSpec struct {
	ID          RunnerID          `json:"id"`
	DisplayName string            `json:"displayName"`
	Binary      string            `json:"binary"`
	VersionArgs []string          `json:"versionArgs"`
	AuthCheck   *SmallCommandSpec `json:"authCheck,omitempty"`
	Supports    RunnerSupports    `json:"supports"`
}

// RunnerInfo is the detection result returned by the API.
type RunnerInfo struct {
	ID                 string            `json:"id"`
	DisplayName        string            `json:"display_name"`
	BinaryName         string            `json:"binary_name"`
	BinaryPath         string            `json:"binary_path,omitempty"`
	Version            string            `json:"version,omitempty"`
	Status             RunnerStatus      `json:"status"`
	DisabledReason     string            `json:"disabled_reason,omitempty"`
	AuthStatus         AuthStatus        `json:"auth_status"`
	Supports           RunnerSupports    `json:"supports"`
	DefaultArgsPreview []string          `json:"default_args_preview"`
	Runtime            RunnerRuntimeInfo `json:"runtime,omitempty"`
}

// RunnerRunRequest is the validated input for starting a runner run.
type RunnerRunRequest struct {
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
	// NoTimeout disables the runner execution deadline. Durable runner-chat
	// turns use this because they are explicitly abortable and may remain
	// productive for much longer than the configured batch-run timeout.
	NoTimeout bool
}

// CommandSpec is the executable command built by an adapter.
type CommandSpec struct {
	RunnerID      RunnerID   `json:"runnerId,omitempty"`
	Binary        string     `json:"binary"`
	Args          []string   `json:"args"`
	Env           []string   `json:"env,omitempty"`
	Cwd           string     `json:"cwd,omitempty"`
	PromptOnStdin bool       `json:"promptOnStdin"`
	Stdin         []byte     `json:"-"`
	OutputMode    OutputMode `json:"outputMode"`
	ArgvPreview   []string   `json:"argvPreview"`
}

// RunnerRunEvent is a persisted and published normalized event.
type RunnerRunEvent struct {
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

// RunnerAdapter builds commands for a specific external runner tool.
type RunnerAdapter interface {
	ID() RunnerID
	DisplayName() string
	Spec() RunnerSpec
	Detect(ctx context.Context, opts DetectOptions) RunnerInfo
	BuildCommand(req RunnerRunRequest) (CommandSpec, error)
}

// DetectOptions control detection behavior.
type DetectOptions struct {
	WorkDir         string
	Env             []string
	DisabledRunners []string
}

// ContinuationMode is how a chat turn continues prior conversation context.
type ContinuationMode string

const (
	// ContinuationReplay rebuilds a bounded transcript prompt from prior
	// completed turns. Universally supported.
	ContinuationReplay ContinuationMode = "replay"
	// ContinuationNative resumes a runner-native session via its own
	// session reference. Only safe when an adapter advertises
	// ChatNativeSession + ChatResume.
	ContinuationNative ContinuationMode = "native"
)

// RunnerChatTurn is a previously persisted chat turn used for replay-prompt
// reconstruction. Field shapes match the runner_chat_turns columns the
// chat manager writes.
type RunnerChatTurn struct {
	ID          string
	Sequence    int64
	UserText    string
	FinalText   string
	Status      string
	RequestedAt int64
	CompletedAt int64
}

// RunnerChatEvent is a normalized event emitted from a chat turn.
type RunnerChatEvent struct {
	Type    string          `json:"type"`
	Seq     int64           `json:"seq,omitempty"`
	Stream  string          `json:"stream,omitempty"`
	Text    string          `json:"text,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RunnerChatCommandRequest is the validated input to chat command building.
type RunnerChatCommandRequest struct {
	SessionID        string
	TurnID           string
	NativeSessionRef string
	ContinuationMode ContinuationMode
	ReplayPrompt     string
	UserMessage      string
	MemoryRefresh    string
	Model            string
	Mode             string
	Isolation        string
	MaxTurns         int
	Cwd              string
	TimeoutSeconds   int
	Meta             map[string]any
}

// RunnerChatAdapter is an optional capability for chat-aware runners. The
// existing one-shot RunnerAdapter contract is preserved.
type RunnerChatAdapter interface {
	RunnerAdapter
	// BuildChatCommand builds an executable command for a single chat turn,
	// either via replay prompt or native resume.
	BuildChatCommand(req RunnerChatCommandRequest) (CommandSpec, error)
	// NormalizeChatEvent converts a raw runner event into zero or more
	// normalized chat events. Unknown shapes may be returned as a single
	// runner_output event so they remain visible for diagnostics.
	NormalizeChatEvent(raw RunnerRunEvent) []RunnerChatEvent
}

// NativeRunnerChatAdapter is implemented by adapters that can extract a
// stable native session ref from runner output.
type NativeRunnerChatAdapter interface {
	RunnerChatAdapter
	// ExtractNativeSessionRef inspects an event/output and returns a stable
	// session ref when one is found. Returning ("", false) means no ref
	// was extracted from this input.
	ExtractNativeSessionRef(event RunnerRunEvent) (string, bool)
}

// NativeRequestKind classifies a native request the runtime is asking the
// host to resolve (approval, question, free-form input, ...).
type NativeRequestKind string

const (
	NativeRequestApproval NativeRequestKind = "approval"
	NativeRequestQuestion NativeRequestKind = "question"
	NativeRequestInput    NativeRequestKind = "input"
	NativeRequestUnknown  NativeRequestKind = "unknown"
)

// NativeRequestRef is a runtime-stable handle to a pending native request.
// The runtime can be told to "respond" to this ref (approve/reject) so the
// active turn can continue without a process restart.
type NativeRequestRef struct {
	RunnerID   RunnerID          `json:"runner_id"`
	Kind       NativeRequestKind `json:"kind"`
	RequestID  string            `json:"request_id"`
	ThreadID   string            `json:"thread_id,omitempty"`
	TurnID     string            `json:"turn_id,omitempty"`
	SessionID  string            `json:"session_id,omitempty"`
	Method     string            `json:"method,omitempty"`
	Summary    string            `json:"summary,omitempty"`
	IssuedAt   int64             `json:"issued_at,omitempty"`
	RawPayload json.RawMessage   `json:"raw_payload,omitempty"`
}

// NativeRequestDecision is the user's response to a NativeRequestRef.
type NativeRequestDecision struct {
	Decision    string          `json:"decision"`
	Message     string          `json:"message,omitempty"`
	AlwaysAllow bool            `json:"always_allow,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

// NativeRequestResponder is implemented by runtimes that can accept
// responses to a pending native request and resume the active turn.
type NativeRequestResponder interface {
	NativeRunnerRuntime
	// RespondToNativeRequest resolves a pending native request and
	// resumes the underlying turn. Returning a non-nil error indicates
	// the runtime could not resume (e.g. session is dead); callers should
	// fall back to the approval-token retry path.
	RespondToNativeRequest(ctx context.Context, ref NativeRequestRef, decision NativeRequestDecision) error
}

// NativeTurnContinuer is implemented by native runtimes that can keep
// waiting for turn completion after the manager pauses for approval.
type NativeTurnContinuer interface {
	NativeRunnerRuntime
	ContinuePendingTurn(ctx context.Context, req NativeRuntimeExecuteRequest) (ProcessOutput, error)
}
