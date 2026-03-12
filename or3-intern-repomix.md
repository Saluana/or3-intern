This file is a merged representation of a subset of the codebase, containing files not matching ignore patterns, combined into a single document by Repomix.

# File Summary

## Purpose
This file contains a packed representation of a subset of the repository's contents that is considered the most important context.
It is designed to be easily consumable by AI systems for analysis, code review,
or other automated processes.

## File Format
The content is organized as follows:
1. This summary section
2. Repository information
3. Directory structure
4. Repository files (if enabled)
5. Multiple file entries, each consisting of:
  a. A header with the file path (## File: path/to/file)
  b. The full contents of the file in a code block

## Usage Guidelines
- This file should be treated as read-only. Any changes should be made to the
  original repository files, not this packed version.
- When processing this file, use the file path to distinguish
  between different files in the repository.
- Be aware that this file may contain sensitive information. Handle it with
  the same level of security as you would the original repository.

## Notes
- Some files may have been excluded based on .gitignore rules and Repomix's configuration
- Binary files are not included in this packed representation. Please refer to the Repository Structure section for a complete list of file paths, including binary files
- Files matching these patterns are excluded: .github, planning, nanobot-repo.md, repomix-output.md, missing.md, **/*_test.go
- Files matching patterns in .gitignore are excluded
- Files matching default ignore patterns are excluded
- Files are sorted by Git change count (files with more changes are at the bottom)

# Directory Structure
```
builtin_skills/
  cron/
    SKILL.md
cmd/
  or3-intern/
    audit_cmd.go
    doctor.go
    init.go
    main.go
    migrate.go
    secrets_cmd.go
    security_setup.go
    service_auth.go
    service_request.go
    service.go
    skills_cmd.go
docs/
  agent-runtime.md
  api-reference.md
  channels.md
  cli-reference.md
  configuration-reference.md
  getting-started.md
  mcp-tool-integrations.md
  memory-and-context.md
  README.md
  security-and-hardening.md
  skills.md
  triggers-and-automation.md
internal/
  agent/
    job_registry.go
    noop_streamer.go
    prompt.go
    runtime.go
    service_runtime_context.go
    structured_autonomy.go
    subagents.go
    tool_policy.go
  artifacts/
    attachment.go
    store.go
  bus/
    bus.go
  channels/
    cli/
      cli.go
      deliver.go
      service.go
      terminal.go
    discord/
      discord.go
    email/
      email.go
    slack/
      slack.go
    telegram/
      telegram.go
    whatsapp/
      whatsapp.go
    channels.go
    media.go
    stream.go
  clawhub/
    client.go
  config/
    config.go
  cron/
    cron.go
  db/
    db.go
    security.go
    store.go
  heartbeat/
    service.go
  mcp/
    manager.go
  memory/
    consolidate.go
    docs.go
    retrieve.go
    scheduler.go
    vector.go
    workspace_context.go
  providers/
    openai.go
  scope/
    scope.go
  security/
    network.go
    store.go
  skills/
    skills.go
  tools/
    context.go
    cron.go
    env.go
    exec.go
    files.go
    memory.go
    message.go
    registry.go
    sandbox.go
    skill_exec.go
    skill.go
    spawn.go
    tools.go
    web.go
  triggers/
    filewatch.go
    structured_tasks.go
    triggers.go
    webhook.go
.env.example
.gitignore
breakdown.md
go.mod
README.md
```

# Files

## File: builtin_skills/cron/SKILL.md
````markdown
# cron
Use the `cron` tool to add/list/remove/run/status scheduled jobs.
````

## File: cmd/or3-intern/audit_cmd.go
````go
package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"or3-intern/internal/security"
)

func runAuditCommand(ctx context.Context, audit *security.AuditLogger, args []string, stdout io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if audit == nil {
		return fmt.Errorf("audit logger not configured")
	}
	if len(args) == 0 || args[0] == "verify" {
		if err := audit.Verify(ctx); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(stdout, "[ok] audit chain verified")
		return nil
	}
	return fmt.Errorf("unknown audit subcommand: %s", args[0])
}
````

## File: cmd/or3-intern/secrets_cmd.go
````go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"or3-intern/internal/security"
)

func runSecretsCommand(ctx context.Context, mgr *security.SecretManager, audit *security.AuditLogger, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if mgr == nil {
		return fmt.Errorf("secret store not configured")
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: or3-intern secrets <set|delete|list> ...")
	}
	switch args[0] {
	case "set":
		fs := flag.NewFlagSet("secrets set", flag.ContinueOnError)
		fs.SetOutput(stderr)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() < 2 {
			return fmt.Errorf("usage: or3-intern secrets set <name> <value>")
		}
		name, value := fs.Arg(0), fs.Arg(1)
		if err := mgr.Put(ctx, name, value); err != nil {
			return err
		}
		if audit != nil {
			if err := audit.Record(ctx, "secret.set", "", "cli", map[string]any{"name": name}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(stdout, "stored\t%s\n", name)
		return nil
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: or3-intern secrets delete <name>")
		}
		if err := mgr.Delete(ctx, args[1]); err != nil {
			return err
		}
		if audit != nil {
			if err := audit.Record(ctx, "secret.delete", "", "cli", map[string]any{"name": args[1]}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(stdout, "deleted\t%s\n", args[1])
		return nil
	case "list":
		names, err := mgr.List(ctx)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			_, _ = fmt.Fprintln(stdout, "(no secrets stored)")
			return nil
		}
		for _, name := range names {
			_, _ = fmt.Fprintln(stdout, name)
		}
		return nil
	default:
		return fmt.Errorf("unknown secrets subcommand: %s", args[0])
	}
}
````

## File: cmd/or3-intern/security_setup.go
````go
package main

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/security"
)

func buildHostPolicy(cfg config.Config) security.HostPolicy {
	return security.HostPolicy{
		Enabled:       cfg.Security.Network.Enabled,
		DefaultDeny:   cfg.Security.Network.DefaultDeny,
		AllowedHosts:  append([]string{}, cfg.Security.Network.AllowedHosts...),
		AllowLoopback: cfg.Security.Network.AllowLoopback,
		AllowPrivate:  cfg.Security.Network.AllowPrivate,
	}
}

func setupSecurity(ctx context.Context, cfg config.Config, d *db.DB) (config.Config, *security.SecretManager, *security.AuditLogger, error) {
	var secretManager *security.SecretManager
	if cfg.Security.SecretStore.Enabled {
		key, err := security.LoadExistingKey(cfg.Security.SecretStore.KeyFile)
		if err != nil {
			if cfg.Security.SecretStore.Required {
				return cfg, nil, nil, err
			}
		} else {
			secretManager = &security.SecretManager{DB: d, Key: key}
		}
	}
	var auditLogger *security.AuditLogger
	if cfg.Security.Audit.Enabled {
		auditKey, err := security.LoadOrCreateKey(cfg.Security.Audit.KeyFile)
		if err != nil {
			if cfg.Security.Audit.Strict {
				return cfg, secretManager, nil, err
			}
		} else {
			auditLogger = &security.AuditLogger{DB: d, Key: auditKey, Strict: cfg.Security.Audit.Strict}
			if cfg.Security.Audit.VerifyOnStart {
				if err := auditLogger.Verify(ctx); err != nil {
					return cfg, secretManager, nil, err
				}
			}
		}
	}
	resolved, err := security.ResolveConfigSecrets(ctx, cfg, secretManager)
	if err != nil {
		return cfg, secretManager, auditLogger, err
	}
	if err := security.ValidateNoSecretRefs(resolved); err != nil {
		return cfg, secretManager, auditLogger, err
	}
	if err := validateConfiguredOutboundEndpoints(ctx, resolved, buildHostPolicy(resolved)); err != nil {
		return cfg, secretManager, auditLogger, err
	}
	return resolved, secretManager, auditLogger, nil
}

func validateConfiguredOutboundEndpoints(ctx context.Context, cfg config.Config, policy security.HostPolicy) error {
	if !policy.EnabledPolicy() {
		return nil
	}
	for _, endpoint := range []string{cfg.Provider.APIBase} {
		if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
			return err
		}
	}
	if cfg.Channels.Telegram.Enabled {
		if err := policy.ValidateEndpoint(ctx, cfg.Channels.Telegram.APIBase); err != nil {
			return err
		}
	}
	if cfg.Channels.Slack.Enabled {
		for _, endpoint := range []string{cfg.Channels.Slack.APIBase, cfg.Channels.Slack.SocketModeURL} {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" {
				continue
			}
			if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
				return err
			}
		}
	}
	if cfg.Channels.Discord.Enabled {
		for _, endpoint := range []string{cfg.Channels.Discord.APIBase, cfg.Channels.Discord.GatewayURL} {
			endpoint = strings.TrimSpace(endpoint)
			if endpoint == "" {
				continue
			}
			if err := policy.ValidateEndpoint(ctx, endpoint); err != nil {
				return err
			}
		}
	}
	if cfg.Channels.WhatsApp.Enabled {
		if err := policy.ValidateEndpoint(ctx, cfg.Channels.WhatsApp.BridgeURL); err != nil {
			return err
		}
	}
	if cfg.Channels.Email.Enabled {
		for _, host := range []string{cfg.Channels.Email.IMAPHost, cfg.Channels.Email.SMTPHost} {
			host = strings.TrimSpace(host)
			if host == "" {
				continue
			}
			if err := policy.ValidateEndpoint(ctx, host); err != nil {
				return err
			}
		}
	}
	for name, server := range cfg.Tools.MCPServers {
		if !server.Enabled || (server.Transport != "sse" && server.Transport != "streamablehttp") {
			continue
		}
		if err := policy.ValidateEndpoint(ctx, server.URL); err != nil {
			return fmt.Errorf("mcp server %s denied by network policy: %w", name, err)
		}
	}
	return nil
}
````

## File: cmd/or3-intern/service_auth.go
````go
package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const serviceTokenMaxAge = 5 * time.Minute

type serviceTokenClaims struct {
	IssuedAt int64  `json:"iat"`
	Nonce    string `json:"nonce"`
}

func serviceAuthMiddleware(secret string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := validateServiceAuthorization(secret, r.Header.Get("Authorization"), time.Now()); err != nil {
			writeServiceJSON(w, http.StatusUnauthorized, map[string]any{"error": err.Error()})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func validateServiceAuthorization(secret string, header string, now time.Time) error {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return fmt.Errorf("service secret is not configured")
	}
	scheme, token, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(token) == "" {
		return fmt.Errorf("missing bearer token")
	}
	return validateServiceBearerToken(secret, token, now)
}

func validateServiceBearerToken(secret string, token string, now time.Time) error {
	payloadPart, signaturePart, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payloadPart == "" || signaturePart == "" {
		return fmt.Errorf("invalid bearer token format")
	}
	signature, err := hex.DecodeString(signaturePart)
	if err != nil {
		return fmt.Errorf("invalid bearer token signature")
	}
	expected := signServiceToken(secret, payloadPart)
	if !hmac.Equal(signature, expected) {
		return fmt.Errorf("invalid bearer token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadPart)
	if err != nil {
		return fmt.Errorf("invalid bearer token payload")
	}
	var claims serviceTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return fmt.Errorf("invalid bearer token payload")
	}
	if claims.IssuedAt <= 0 {
		return fmt.Errorf("invalid bearer token timestamp")
	}
	issuedAt := time.Unix(claims.IssuedAt, 0)
	if issuedAt.After(now.Add(30 * time.Second)) {
		return fmt.Errorf("bearer token timestamp is in the future")
	}
	if now.Sub(issuedAt) > serviceTokenMaxAge {
		return fmt.Errorf("bearer token expired")
	}
	if strings.TrimSpace(claims.Nonce) == "" {
		return fmt.Errorf("invalid bearer token nonce")
	}
	return nil
}

func issueServiceBearerToken(secret string, now time.Time) (string, error) {
	nonce, err := randomHex(12)
	if err != nil {
		return "", err
	}
	claims := serviceTokenClaims{IssuedAt: now.Unix(), Nonce: nonce}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadPart := base64.RawURLEncoding.EncodeToString(payload)
	signature := hex.EncodeToString(signServiceToken(secret, payloadPart))
	return payloadPart + "." + signature, nil
}

func signServiceToken(secret string, payloadPart string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payloadPart))
	return mac.Sum(nil)
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", nil
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func withDetachedContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx)
}
````

## File: cmd/or3-intern/service_request.go
````go
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"or3-intern/internal/agent"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

type serviceTurnRequest struct {
	SessionKey    string
	Message       string
	AllowedTools  []string
	RestrictTools bool
	Meta          map[string]any
	ProfileName   string
}

type serviceSubagentRequest struct {
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	AllowedTools     []string
	RestrictTools    bool
	TimeoutSeconds   int
	Meta             map[string]any
	ProfileName      string
	Channel          string
	ReplyTo          string
}

type serviceToolPolicyPayload struct {
	Mode              string   `json:"mode"`
	AllowedTools      []string `json:"allowed_tools"`
	AllowedToolsCamel []string `json:"allowedTools"`
	BlockedTools      []string `json:"blocked_tools"`
	BlockedToolsCamel []string `json:"blockedTools"`
}

type serviceTurnRequestPayload struct {
	SessionKey            string                    `json:"session_key"`
	InternSessionKey      string                    `json:"intern_session_key"`
	SessionKeyCamel       string                    `json:"sessionKey"`
	InternSessionKeyCamel string                    `json:"internSessionKey"`
	Message               string                    `json:"message"`
	AllowedTools          []string                  `json:"allowed_tools"`
	AllowedToolsCamel     []string                  `json:"allowedTools"`
	ToolPolicy            *serviceToolPolicyPayload `json:"tool_policy"`
	ToolPolicyCamel       *serviceToolPolicyPayload `json:"toolPolicy"`
	Meta                  map[string]any            `json:"meta"`
	ProfileName           string                    `json:"profile_name"`
	ProfileNameCamel      string                    `json:"profileName"`
}

type serviceSubagentRequestPayload struct {
	ParentSessionKey      string                    `json:"parent_session_key"`
	ParentSessionKeyCamel string                    `json:"parentSessionKey"`
	SessionKey            string                    `json:"session_key"`
	InternSessionKey      string                    `json:"intern_session_key"`
	SessionKeyCamel       string                    `json:"sessionKey"`
	InternSessionKeyCamel string                    `json:"internSessionKey"`
	Task                  string                    `json:"task"`
	PromptSnapshot        []providers.ChatMessage   `json:"prompt_snapshot"`
	PromptSnapshotCamel   []providers.ChatMessage   `json:"promptSnapshot"`
	AllowedTools          []string                  `json:"allowed_tools"`
	AllowedToolsCamel     []string                  `json:"allowedTools"`
	ToolPolicy            *serviceToolPolicyPayload `json:"tool_policy"`
	ToolPolicyCamel       *serviceToolPolicyPayload `json:"toolPolicy"`
	TimeoutSeconds        json.Number               `json:"timeout_seconds"`
	TimeoutSecondsCamel   json.Number               `json:"timeoutSeconds"`
	Timeout               json.Number               `json:"timeout"`
	Meta                  map[string]any            `json:"meta"`
	ProfileName           string                    `json:"profile_name"`
	ProfileNameCamel      string                    `json:"profileName"`
	Channel               string                    `json:"channel"`
	ReplyTo               string                    `json:"reply_to"`
	ReplyToCamel          string                    `json:"replyTo"`
}

func decodeServiceTurnRequest(body io.Reader, registry *tools.Registry) (serviceTurnRequest, error) {
	var payload serviceTurnRequestPayload
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceTurnRequest{}, err
	}
	allowedTools, restrictTools, err := agent.ResolveServiceToolAllowlist(
		registry,
		firstToolPolicy(payload.ToolPolicy, payload.ToolPolicyCamel),
		firstStringSlice(payload.AllowedTools, payload.AllowedToolsCamel),
	)
	if err != nil {
		return serviceTurnRequest{}, err
	}
	return serviceTurnRequest{
		SessionKey:    firstNonEmptyString(payload.SessionKey, payload.InternSessionKey, payload.SessionKeyCamel, payload.InternSessionKeyCamel),
		Message:       strings.TrimSpace(payload.Message),
		AllowedTools:  allowedTools,
		RestrictTools: restrictTools,
		Meta:          cloneMapOrEmpty(payload.Meta),
		ProfileName:   firstNonEmptyString(payload.ProfileName, payload.ProfileNameCamel),
	}, nil
}

func decodeServiceSubagentRequest(body io.Reader, registry *tools.Registry) (serviceSubagentRequest, error) {
	var payload serviceSubagentRequestPayload
	if err := decodeServiceRequestBody(body, &payload); err != nil {
		return serviceSubagentRequest{}, err
	}
	allowedTools, restrictTools, err := agent.ResolveServiceToolAllowlist(
		registry,
		firstToolPolicy(payload.ToolPolicy, payload.ToolPolicyCamel),
		firstStringSlice(payload.AllowedTools, payload.AllowedToolsCamel),
	)
	if err != nil {
		return serviceSubagentRequest{}, err
	}
	timeoutSeconds, err := firstPositiveInt(payload.TimeoutSeconds, payload.TimeoutSecondsCamel, payload.Timeout)
	if err != nil {
		return serviceSubagentRequest{}, err
	}
	return serviceSubagentRequest{
		ParentSessionKey: firstNonEmptyString(
			payload.ParentSessionKey,
			payload.ParentSessionKeyCamel,
			payload.SessionKey,
			payload.InternSessionKey,
			payload.SessionKeyCamel,
			payload.InternSessionKeyCamel,
		),
		Task:           strings.TrimSpace(payload.Task),
		PromptSnapshot: firstPromptSnapshot(payload.PromptSnapshot, payload.PromptSnapshotCamel),
		AllowedTools:   allowedTools,
		RestrictTools:  restrictTools,
		TimeoutSeconds: timeoutSeconds,
		Meta:           cloneMapOrEmpty(payload.Meta),
		ProfileName:    firstNonEmptyString(payload.ProfileName, payload.ProfileNameCamel),
		Channel:        strings.TrimSpace(payload.Channel),
		ReplyTo:        firstNonEmptyString(payload.ReplyTo, payload.ReplyToCamel),
	}, nil
}

func decodeServiceRequestBody(body io.Reader, out any) error {
	decoder := json.NewDecoder(body)
	decoder.UseNumber()
	return decoder.Decode(out)
}

func firstToolPolicy(values ...*serviceToolPolicyPayload) *agent.ServiceToolPolicy {
	for _, value := range values {
		if value == nil {
			continue
		}
		return &agent.ServiceToolPolicy{
			Mode:         strings.TrimSpace(value.Mode),
			AllowedTools: firstStringSlice(value.AllowedTools, value.AllowedToolsCamel),
			BlockedTools: firstStringSlice(value.BlockedTools, value.BlockedToolsCamel),
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func firstStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		out := make([]string, len(value))
		copy(out, value)
		return out
	}
	return nil
}

func firstPromptSnapshot(values ...[]providers.ChatMessage) []providers.ChatMessage {
	for _, value := range values {
		if len(value) == 0 {
			continue
		}
		out := make([]providers.ChatMessage, len(value))
		copy(out, value)
		return out
	}
	return nil
}

func firstPositiveInt(values ...json.Number) (int, error) {
	for _, value := range values {
		raw := strings.TrimSpace(value.String())
		if raw == "" {
			continue
		}
		n, err := value.Int64()
		if err != nil {
			return 0, fmt.Errorf("invalid timeout")
		}
		if n <= 0 {
			continue
		}
		return int(n), nil
	}
	return 0, nil
}

func cloneMapOrEmpty(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func backgroundToolsRegistry(manager *agent.SubagentManager) *tools.Registry {
	if manager == nil {
		return tools.NewRegistry()
	}
	if manager.BackgroundTools != nil {
		return manager.BackgroundTools()
	}
	return tools.NewRegistry()
}
````

## File: docs/agent-runtime.md
````markdown
# Agent runtime

## Shared runtime model

`or3-intern` uses one shared runtime model across:

- `chat`
- `agent`
- `serve`
- `service`
- channel adapters
- autonomous triggers
- subagent jobs

That means turns, tool calls, memory retrieval, quotas, and session handling behave consistently no matter how a task enters the system.

## High-level flow

1. A foreground or background entrypoint receives work
2. The runtime resolves the session key and related context
3. Prompt bootstrap files and retrieved memory are injected
4. The model runs a turn
5. Tool calls execute through the shared tool registry
6. Results are persisted to SQLite-backed history and memory stores
7. Foreground callers receive the response immediately, while background jobs stream or persist status updates

## Foreground entrypoints

- `chat` for local interactive use
- `agent -m` for one-shot turns
- `service` for authenticated HTTP callers

## Background entrypoints

- `serve` for external channels
- webhook triggers
- file-watch triggers
- heartbeat turns
- cron jobs
- subagent jobs

## Streaming

The CLI `chat` command supports live token streaming from the provider. The internal service API can also stream job output over SSE for callers that send `Accept: text/event-stream`.

## Session model

Every turn runs inside a session key, such as:

- `cli:default`
- `telegram:<chat-id>`
- `slack:<channel-id>`
- `discord:<channel-id>`
- `email:<address>`
- `whatsapp:<chat-id>`
- `heartbeat:default`

Session keys are used for history isolation, memory retrieval, and optional scope linking.

## Subagents

Subagents are optional background jobs governed by the same runtime and hardening controls. Configuration includes:

- `subagents.enabled`
- `subagents.maxConcurrent`
- `subagents.maxQueued`
- `subagents.taskTimeoutSeconds`

## Related documentation

- [Memory and context](memory-and-context.md)
- [Triggers and automation](triggers-and-automation.md)
- [Internal service API reference](api-reference.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/agent/`
- `cmd/or3-intern/main.go`
- `cmd/or3-intern/service.go`
````

## File: docs/channels.md
````markdown
# Channel integrations

## Overview

`or3-intern` supports these non-CLI channels:

- Telegram
- Slack
- Discord
- Email
- WhatsApp via a local bridge

All external channels are disabled by default.

## Running channels

Use local terminal chat first:

```bash
go run ./cmd/or3-intern chat
```

Run configured external channels with:

```bash
go run ./cmd/or3-intern serve
```

`serve` starts the shared runtime plus any enabled channel workers.

## Common behavior

- inbound traffic is mapped to session keys per platform
- outbound sending uses the same shared runtime and tool loop
- `hardening.isolateChannelPeers=true` can isolate senders inside shared channels
- allowlists can limit who is allowed to talk to the runtime
- most channels support a default outbound destination for `send_message`

## Environment variables

```dotenv
OR3_TELEGRAM_TOKEN=
OR3_SLACK_APP_TOKEN=
OR3_SLACK_BOT_TOKEN=
OR3_DISCORD_TOKEN=
OR3_WHATSAPP_BRIDGE_URL=ws://127.0.0.1:3001/ws
OR3_WHATSAPP_BRIDGE_TOKEN=
OR3_EMAIL_IMAP_HOST=
OR3_EMAIL_IMAP_PORT=993
OR3_EMAIL_IMAP_USERNAME=
OR3_EMAIL_IMAP_PASSWORD=
OR3_EMAIL_SMTP_HOST=
OR3_EMAIL_SMTP_PORT=587
OR3_EMAIL_SMTP_USERNAME=
OR3_EMAIL_SMTP_PASSWORD=
OR3_EMAIL_FROM_ADDRESS=
```

## Per-channel setup

### Telegram

- enable `channels.telegram.enabled`
- set `channels.telegram.token` or `OR3_TELEGRAM_TOKEN`
- optionally set `defaultChatId`
- optionally restrict traffic with `allowedChatIds`
- polling is used; no webhook setup is required

### Slack

- enable `channels.slack.enabled`
- set `channels.slack.appToken` and `channels.slack.botToken`
- optionally set `defaultChannelId`
- optionally restrict traffic with `allowedUserIds`
- `requireMention=true` is recommended in shared spaces
- uses Socket Mode for inbound traffic and Web API for outbound messages

### Discord

- enable `channels.discord.enabled`
- set `channels.discord.token`
- optionally set `defaultChannelId`
- optionally restrict traffic with `allowedUserIds`
- `requireMention=true` is recommended in guild channels
- uses the Gateway for inbound traffic and REST for outbound messages

### WhatsApp bridge

- enable `channels.whatsApp.enabled`
- set `channels.whatsApp.bridgeUrl` or `OR3_WHATSAPP_BRIDGE_URL`
- optionally set `channels.whatsApp.bridgeToken`
- optionally set `defaultTo` and `allowedFrom`
- requires a compatible local bridge websocket service

### Email

- enable `channels.email.enabled`
- set `channels.email.consentGranted=true` only after explicit mailbox access permission
- choose either `openAccess=true` or a non-empty `allowedSenders` list
- configure IMAP (`imapHost`, `imapPort`, `imapUsername`, `imapPassword`, optional `imapMailbox`)
- configure SMTP (`smtpHost`, `smtpPort`, `smtpUsername`, `smtpPassword`, optional `fromAddress`)
- `autoReplyEnabled=false` disables automatic replies for normal inbound mail turns
- inbound mail is polled over IMAP and outbound mail reuses thread metadata when available

## Session key formats

The README documents these automatic session key patterns:

- `telegram:<chat-id>`
- `slack:<channel-id>`
- `discord:<channel-id>`
- `email:<normalized-address>`
- `whatsapp:<chat-id>`

## Related documentation

- [Configuration reference](configuration-reference.md)
- [Memory and context](memory-and-context.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/channels/telegram/`
- `internal/channels/slack/`
- `internal/channels/discord/`
- `internal/channels/email/`
- `internal/channels/whatsapp/`
````

## File: docs/cli-reference.md
````markdown
# CLI reference

## Primary commands

| Command | Purpose |
| --- | --- |
| `or3-intern init` | Guided first-run setup for config and provider settings |
| `or3-intern chat` | Interactive CLI session |
| `or3-intern serve` | Starts enabled channels, triggers, heartbeat, cron, and the shared worker runtime |
| `or3-intern service` | Starts the authenticated internal HTTP API |
| `or3-intern agent -m "..."` | Runs a one-shot foreground turn |
| `or3-intern version` | Prints the binary version |

## Operational and admin commands

| Command | Purpose |
| --- | --- |
| `or3-intern doctor [--strict]` | Audits the current config for unsafe or inconsistent settings |
| `or3-intern secrets <set|delete|list>` | Manages encrypted secret references stored in SQLite |
| `or3-intern audit [verify]` | Inspects or verifies the append-only audit chain |
| `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]` | Imports legacy session history |

## Skills commands

| Command | Purpose |
| --- | --- |
| `or3-intern skills list` | Lists discovered skills |
| `or3-intern skills list --eligible` | Lists only eligible skills |
| `or3-intern skills info <name>` | Shows metadata, permission state, and policy notes |
| `or3-intern skills check` | Validates available skills and reports policy state |
| `or3-intern skills search "query"` | Searches configured registries |
| `or3-intern skills install <slug>` | Installs a skill into the managed directory |
| `or3-intern skills update <name>` / `--all` | Updates managed installs |
| `or3-intern skills remove <name>` | Removes a managed install |

See [skills.md](skills.md) for how the loader, trust model, and quarantine policy work.

## Session scope commands

| Command | Purpose |
| --- | --- |
| `or3-intern scope link <session-key> <scope>` | Links a session to a named scope |
| `or3-intern scope list <scope>` | Lists session keys attached to a scope |
| `or3-intern scope resolve <session-key>` | Resolves the scope for a session |

Scopes let multiple session keys share one conversation history. See [memory-and-context.md](memory-and-context.md).

## Related references

- [Getting started](getting-started.md)
- [Configuration reference](configuration-reference.md)
- [Internal service API reference](api-reference.md)
````

## File: docs/configuration-reference.md
````markdown
# Configuration reference

`or3-intern` loads its primary configuration from `config.json`, usually under `~/.or3-intern/config.json` after `or3-intern init`.

## Top-level sections

| Key | Purpose |
| --- | --- |
| `dbPath`, `artifactsDir`, `workspaceDir`, `allowedDir` | Storage locations and workspace boundaries |
| `defaultSessionKey`, `session` | Session naming and cross-session identity/scope behavior |
| `identityFile`, `memoryFile` | Prompt bootstrap files |
| `provider` | Model API base, model names, keys, temperature, and timeouts |
| `tools` | Local tool behavior, proxying, timeouts, workspace restrictions, and MCP servers |
| `hardening` | Tool capability tiers, program allowlists, child environment controls, quotas, and sandboxing |
| `skills` | Managed skill loading, per-skill config, policy, and registry settings |
| `triggers` | Webhook and file-watch automation |
| `heartbeat` | Timer-driven autonomous turns |
| `cron` | Scheduled job storage and execution |
| `service` | Internal authenticated HTTP API settings |
| `channels` | Telegram, Slack, Discord, WhatsApp bridge, and Email configuration |
| `security` | Secret store, audit, access profiles, and outbound network policy |
| `docIndex` | Opt-in document indexing for prompt-time retrieval |
| `subagents` | Background job queueing and concurrency controls |

## Minimal shape

```json
{
  "provider": {},
  "tools": {},
  "hardening": {},
  "skills": {},
  "triggers": {},
  "heartbeat": {},
  "cron": {},
  "service": {},
  "channels": {},
  "security": {},
  "docIndex": {},
  "subagents": {},
  "session": {}
}
```

## Important sections

### `provider`

Controls the LLM and embedding provider settings:

- `apiBase`
- `apiKey`
- `model`
- `embedModel`
- `temperature`
- `enableVision`
- `timeoutSeconds`

### `tools`

Controls local tool execution and optional MCP registration:

- `braveApiKey`
- `webProxy`
- `execTimeoutSeconds`
- `restrictToWorkspace`
- `pathAppend`
- `mcpServers`

See [mcp-tool-integrations.md](mcp-tool-integrations.md) for the MCP-specific settings.

### `hardening`

Core runtime safety controls:

- `guardedTools`
- `privilegedTools`
- `enableExecShell`
- `execAllowedPrograms`
- `childEnvAllowlist`
- `isolateChannelPeers`
- `sandbox`
- `quotas`

See [security-and-hardening.md](security-and-hardening.md) for rollout guidance.

### `skills`

Skill loading and trust policy:

- `enableExec`
- `maxRunSeconds`
- `managedDir`
- `load`
- `entries`
- `policy`
- `clawHub`

See [skills.md](skills.md).

### `triggers`, `heartbeat`, and `cron`

These sections control autonomous execution:

- `triggers.webhook`
- `triggers.fileWatch`
- `heartbeat`
- `cron`

See [triggers-and-automation.md](triggers-and-automation.md).

### `service`

Internal service mode settings:

- `enabled`
- `listen`
- `secret`

See [api-reference.md](api-reference.md).

### `channels`

Non-CLI integrations:

- `telegram`
- `slack`
- `discord`
- `whatsApp`
- `email`

See [channels.md](channels.md).

### `security`

Phase 3 security controls:

- `secretStore`
- `audit`
- `profiles`
- `network`

See [security-and-hardening.md](security-and-hardening.md).

### `docIndex`

Opt-in file indexing and retrieval:

- `enabled`
- `roots`
- `maxFiles`
- `maxFileBytes`
- `maxChunks`
- `embedMaxBytes`
- `refreshSeconds`
- `retrieveLimit`

See [memory-and-context.md](memory-and-context.md).

## Environment overrides called out in the README

The codebase documents these direct environment overrides for service and channel setup:

- `OR3_SERVICE_ENABLED`
- `OR3_SERVICE_LISTEN`
- `OR3_SERVICE_SECRET`
- `OR3_TELEGRAM_TOKEN`
- `OR3_SLACK_APP_TOKEN`
- `OR3_SLACK_BOT_TOKEN`
- `OR3_DISCORD_TOKEN`
- `OR3_WHATSAPP_BRIDGE_URL`
- `OR3_WHATSAPP_BRIDGE_TOKEN`
- `OR3_EMAIL_IMAP_HOST`
- `OR3_EMAIL_IMAP_PORT`
- `OR3_EMAIL_IMAP_USERNAME`
- `OR3_EMAIL_IMAP_PASSWORD`
- `OR3_EMAIL_SMTP_HOST`
- `OR3_EMAIL_SMTP_PORT`
- `OR3_EMAIL_SMTP_USERNAME`
- `OR3_EMAIL_SMTP_PASSWORD`
- `OR3_EMAIL_FROM_ADDRESS`

## Related code

- `internal/config/config.go`
- `cmd/or3-intern/init.go`
````

## File: docs/getting-started.md
````markdown
# Getting started

## What `or3-intern` is

`or3-intern` is a local-first agent runtime with:

- a Go CLI
- SQLite-backed history and memory
- external channel adapters
- optional autonomous triggers
- optional internal HTTP service mode
- a hardened tool execution model

## Quick start

### 1. Initialize the runtime

```bash
go run ./cmd/or3-intern init
```

The guided setup writes provider and runtime settings to `~/.or3-intern/config.json`.

### 2. Start an interactive local session

```bash
go run ./cmd/or3-intern chat
```

Use this first when you want to confirm that provider configuration, storage, and prompts are working before enabling any external integrations.

### 3. Run external channels and automation

```bash
go run ./cmd/or3-intern serve
```

`serve` starts the shared runtime plus any enabled channels, triggers, heartbeat jobs, and other background workers.

### 4. Run internal service mode when integrating with OR3 Net

```bash
go run ./cmd/or3-intern service
```

This exposes the authenticated loopback HTTP API documented in [api-reference.md](api-reference.md).

## Important local paths

By default, runtime data lives under `~/.or3-intern/`.

Common files and directories include:

- `config.json` — primary runtime configuration
- `or3-intern.sqlite` — SQLite database for history, memory, secrets, audit data, and related state
- `artifacts/` — spilled large tool outputs and related artifacts
- `skills/` — managed skill installs
- `IDENTITY.md` — agent identity/persona injected into prompts
- `MEMORY.md` — always-available standing context
- `HEARTBEAT.md` — autonomous task list reloaded during autonomous turns

## Recommended first-run sequence

1. Run `init`
2. Confirm `chat` works with a simple question
3. Review [configuration-reference.md](configuration-reference.md)
4. Run `or3-intern doctor --strict` before exposing channels or service mode
5. Enable one advanced feature at a time: channels, skills, triggers, MCP, or service mode

## Where to go next

- Runtime behavior: [agent-runtime.md](agent-runtime.md)
- Context loading and retrieval: [memory-and-context.md](memory-and-context.md)
- External integrations: [channels.md](channels.md)
- Hardening controls: [security-and-hardening.md](security-and-hardening.md)
````

## File: docs/mcp-tool-integrations.md
````markdown
# MCP tool integrations

## Overview

MCP support is optional and disabled by default. Configured servers are registered during startup, and their tools are exposed to the model as normal local tools with stable names such as `mcp_<server>_<tool>`.

## Configuration shape

MCP servers are configured under `tools.mcpServers`.

Per-server settings include:

- `enabled`
- `transport`
- `command`
- `args`
- `env`
- `childEnvAllowlist`
- `url`
- `headers`
- `connectTimeoutSeconds`
- `toolTimeoutSeconds`
- `allowInsecureHttp`

## Supported transports

- `stdio`
- `sse`
- `streamableHttp`

## Safety notes

The README documents these safety rules:

- prefer `stdio` for local trusted servers
- plain `http://` endpoints are rejected unless `allowInsecureHttp=true`, and even then only for loopback or localhost targets
- stdio MCP servers inherit only the configured child environment allowlist plus explicit per-server `env` entries
- MCP tool calls go through the existing tool loop, timeout handling, and artifact spill behavior
- when `security.network` is enabled, MCP HTTP transports must satisfy the global trusted-host policy

## Operational notes

v1 intentionally does not include:

- live reconnect loops
- hot-add or hot-remove of MCP tools
- SQLite persistence for tool catalogs
- a separate MCP gateway service

## Related documentation

- [Configuration reference](configuration-reference.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/mcp/`
- `internal/tools/`
- `internal/config/config.go`
````

## File: docs/memory-and-context.md
````markdown
# Memory and context

## Persistent history and retrieval

`or3-intern` stores conversation history in SQLite and uses a hybrid retrieval pipeline for long-term context.

The README describes the retrieval stack as:

- pinned context
- vector similarity search
- FTS keyword search

This is meant to keep retrieval precise without needing to scan full histories on every turn.

## Context sources

A turn can draw context from several places:

- session history stored in SQLite
- `IDENTITY.md` for stable agent identity
- `MEMORY.md` for standing context and preferences
- document index excerpts from configured roots
- autonomous task context from `HEARTBEAT.md`
- scope-linked session history when session scopes are used

## Bootstrap files

Three markdown files are especially important:

- `IDENTITY.md` — the agent's role, style, and identity
- `MEMORY.md` — standing facts and preferences that should always be available
- `HEARTBEAT.md` — autonomous work instructions used only for heartbeat, cron, webhook, and file-watch turns

`HEARTBEAT.md` is reread for each autonomous turn so edits apply without restarting `serve`.

## Document index

The optional document index lets the runtime retrieve relevant excerpts from local files and inject them into the prompt.

Supported configuration keys include:

- `docIndex.enabled`
- `docIndex.roots`
- `docIndex.maxFiles`
- `docIndex.maxFileBytes`
- `docIndex.maxChunks`
- `docIndex.embedMaxBytes`
- `docIndex.refreshSeconds`
- `docIndex.retrieveLimit`

Supported file types in the README include:

- `.md`, `.txt`
- `.go`, `.py`, `.js`, `.ts`
- `.json`, `.yaml`, `.toml`
- `.sh`

## Consolidation

The top-level config also exposes message consolidation settings such as:

- `consolidationEnabled`
- `consolidationWindowSize`
- `consolidationMaxMessages`
- `consolidationMaxInputChars`
- `consolidationAsyncTimeoutSeconds`

These settings help keep long-running sessions manageable.

## Session scopes

Session scopes let multiple session keys share one conversation history.

Example use cases:

- link a Telegram chat and a Discord channel to the same project scope
- keep a shared memory thread across multiple delivery channels
- move work between channels without losing context

Commands:

```bash
or3-intern scope link telegram:12345 my-project
or3-intern scope link discord:67890 my-project
or3-intern scope list my-project
or3-intern scope resolve telegram:12345
```

## Related documentation

- [Agent runtime](agent-runtime.md)
- [CLI reference](cli-reference.md)
- [Triggers and automation](triggers-and-automation.md)

## Related code

- `internal/memory/`
- `internal/db/`
- `internal/scope/`
````

## File: docs/README.md
````markdown
# Documentation

This directory holds the detailed guides and references that were previously packed into the root README.

## Guides

- [Getting started](getting-started.md) — first-run flow, local paths, and the quickest way to get a working install
- [Agent runtime](agent-runtime.md) — how turns move through the shared runtime across CLI, service mode, channels, and automation
- [Memory and context](memory-and-context.md) — history, hybrid retrieval, bootstrap files, document indexing, and session scopes
- [Channel integrations](channels.md) — Telegram, Slack, Discord, Email, and WhatsApp bridge setup and behavior
- [Skills](skills.md) — skill loading, precedence, trust policy, and management commands
- [Triggers and automation](triggers-and-automation.md) — webhook, file-watch, heartbeat, cron, and structured task execution
- [Security and hardening](security-and-hardening.md) — phase 1/2/3 controls, doctor, secrets, audit, profiles, and network policy
- [MCP tool integrations](mcp-tool-integrations.md) — optional MCP server registration and transport safety

## References

- [Configuration reference](configuration-reference.md) — top-level config map and the major nested sections in `config.json`
- [CLI reference](cli-reference.md) — command-by-command summary for the `or3-intern` binary
- [Internal service API reference](api-reference.md) — authenticated HTTP endpoints for `or3-intern service`

## Suggested reading order

1. [Getting started](getting-started.md)
2. [Configuration reference](configuration-reference.md)
3. [Agent runtime](agent-runtime.md)
4. Any feature guides relevant to the way you plan to run the system
````

## File: docs/security-and-hardening.md
````markdown
# Security and hardening

## Overview

The README describes hardening in three layers. The defaults are meant to keep powerful tools and external exposure opt-in.

## Phase 1 defaults

Phase 1 establishes the baseline runtime posture:

- file tools stay workspace-rooted by default
- external channels stay closed unless explicitly enabled
- child processes receive a scrubbed environment allowlist
- `exec` prefers `program` plus `args`
- legacy shell execution is disabled unless `hardening.enableExecShell=true`
- tool calls are checked against capability tiers and bounded per-session quotas
- channel peers can be isolated by sender with `hardening.isolateChannelPeers=true`

## Phase 2 additions

Phase 2 adds tighter controls around autonomous execution and skill/script execution:

- skills declare permission metadata and tool allowlists
- script-capable skills default to quarantine until approved
- heartbeat, webhook, and file-watch turns carry a bounded `structured_event`
- privileged shell execution and `run_skill_script` can route through Bubblewrap
- `or3-intern doctor` audits common unsafe settings and supports `--strict`

## Phase 3 additions

Phase 3 adds stronger operational controls:

- encrypted secret references via `secret:<name>`
- HMAC-chained audit records with `or3-intern audit verify`
- named access profiles for channels and triggers
- outbound host restrictions through `security.network`

## Core config sections

### `hardening`

- `guardedTools`
- `privilegedTools`
- `enableExecShell`
- `execAllowedPrograms`
- `childEnvAllowlist`
- `isolateChannelPeers`
- `sandbox`
- `quotas`

### `security.secretStore`

- `enabled`
- `required`
- `keyFile`

### `security.audit`

- `enabled`
- `strict`
- `keyFile`
- `verifyOnStart`

### `security.profiles`

- `enabled`
- `default`
- `channels`
- `triggers`
- `profiles`

Each named profile can control:

- `maxCapability`
- `allowedTools`
- `allowedHosts`
- `writablePaths`
- `allowSubagents`

### `security.network`

- `enabled`
- `defaultDeny`
- `allowedHosts`
- `allowLoopback`
- `allowPrivate`

## Operational guidance

The README's rollout guidance recommends:

- enabling the secret store before moving secrets to `secret:<name>` references
- verifying the audit chain before enforcing verify-on-start behavior
- starting with permissive network/profile rules, then tightening after observing real usage
- keeping service mode on loopback or private networking only

## Doctor command

Use:

```bash
or3-intern doctor
or3-intern doctor --strict
```

Run this before enabling external channels, webhook listeners, privileged tools, or service mode.

## Related documentation

- [Configuration reference](configuration-reference.md)
- [Skills](skills.md)
- [MCP tool integrations](mcp-tool-integrations.md)
- [Internal service API reference](api-reference.md)

## Related code

- `internal/security/`
- `cmd/or3-intern/doctor.go`
- `cmd/or3-intern/security_setup.go`
````

## File: docs/skills.md
````markdown
# Skills

## Overview

`or3-intern` supports ClawHub/OpenClaw-compatible skill bundles with metadata, local overrides, trust policy, and quarantine controls.

## Skill locations and precedence

Skills are loaded from:

1. bundled: `builtin_skills/`
2. managed: `~/.or3-intern/skills`
3. workspace: `<workspace>/skills`

Precedence is:

`workspace > managed > bundled`

A legacy `<workspace>/workspace_skills` directory is still scanned below the new workspace root for migration safety.

## Skill metadata

Skills can ship a `skill.json` manifest for structured metadata such as summary, entrypoints, and timeouts.

Supported frontmatter keys called out in the README include:

- `name`
- `description`
- `homepage`
- `user-invocable`
- `disable-model-invocation`
- `command-dispatch`
- `command-tool`
- `command-arg-mode`

Supported metadata namespaces include:

- `metadata.openclaw`
- `metadata.clawdbot`
- `metadata.clawdis`

## Eligibility checks

The loader checks:

- OS compatibility
- required binaries
- any-of binaries
- required environment variables
- required config flags
- explicit per-skill disable flags

Ineligible skills remain inspectable through `read_skill` and `or3-intern skills info/check`.

## Per-skill configuration

The main config supports:

- `skills.enableExec`
- `skills.maxRunSeconds`
- `skills.managedDir`
- `skills.load.extraDirs`
- `skills.load.watch`
- `skills.load.watchDebounceMs`
- `skills.entries`
- `skills.policy`
- `skills.clawHub`

Per-skill config entries can include:

- `enabled`
- `apiKey`
- `env`
- `config`

## Trust model

The README's trust guidance is intentionally strict:

- treat third-party skills as untrusted input
- script-capable skills default to quarantine until explicitly approved
- origin metadata is persisted for managed installs
- install-time scanning flags obvious high-risk bundles
- `trustedOwners` and `trustedRegistries` can auto-approve known publishers
- `blockedOwners` hard-blocks known-bad publishers
- local edits after installation are treated as trust drift and re-quarantine the skill
- installer hints are informational only and are not auto-run

## Management commands

```bash
or3-intern skills list
or3-intern skills list --eligible
or3-intern skills info <name>
or3-intern skills check
or3-intern skills search "calendar"
or3-intern skills install <slug>
or3-intern skills update <name>
or3-intern skills update --all
or3-intern skills remove <name>
```

## User invocation

User-invocable skills can be triggered explicitly:

```text
/my-skill raw arguments here
```

For `command-dispatch: tool`, the runtime forwards the raw argument string directly to the selected tool. Otherwise the runtime starts a normal model turn seeded with the selected `SKILL.md`.

## Related documentation

- [CLI reference](cli-reference.md)
- [Security and hardening](security-and-hardening.md)
- [Configuration reference](configuration-reference.md)

## Related code

- `internal/skills/`
- `internal/clawhub/`
- `cmd/or3-intern/skills_cmd.go`
````

## File: docs/triggers-and-automation.md
````markdown
# Triggers and automation

## Overview

`or3-intern` supports several autonomous entrypoints:

- webhook events
- file-watch polling
- heartbeat turns
- cron jobs
- optional structured task execution

These all run through the same runtime used by CLI and service turns.

## Webhook trigger

The webhook server receives POST requests and dispatches them as agent events.

Important config keys:

- `triggers.webhook.enabled`
- `triggers.webhook.addr`
- `triggers.webhook.secret`
- `triggers.webhook.maxBodyKB`

The webhook path is fixed at `/webhook`.

## File-watch trigger

The file watcher polls configured paths for new or changed files.

Important config keys:

- `triggers.fileWatch.enabled`
- `triggers.fileWatch.paths`
- `triggers.fileWatch.pollSeconds`
- `triggers.fileWatch.debounceSeconds`

## Heartbeat

Heartbeat is a timer-driven autonomous trigger that runs inside `or3-intern serve`.

Important config keys:

- `heartbeat.enabled`
- `heartbeat.intervalMinutes`
- `heartbeat.tasksFile`
- `heartbeat.sessionKey`

Operational behavior documented in the README:

- disabled by default
- not used during `chat` or one-shot `agent` runs
- rereads `HEARTBEAT.md` on each autonomous turn
- uses its own session key for deterministic background history
- does not auto-send a normal assistant reply anywhere; explicit `send_message` is required

## Cron jobs

Cron is for schedule-specific work or per-job delivery targets.

The README highlights that job payloads can set `session_key` explicitly to override the global default session.

Use heartbeat for a standing background task list. Use cron when you need a specific schedule or delivery target.

## Structured trigger inputs

Autonomous trigger producers can attach a bounded `structured_event` object in event metadata.

The README calls out examples for:

- `heartbeat`
- `webhook`
- `filewatch`

If trigger content contains a valid `structured_tasks` envelope, the runtime validates and executes those tasks directly through the normal tool registry, quotas, and guards instead of routing them through the model.

Supported forms include:

- raw JSON with a `tasks` array
- a top-level `structured_tasks` field in a larger object
- fenced code blocks tagged `or3-tasks`, `structured-tasks`, or `autonomous-tasks`

## Related documentation

- [Agent runtime](agent-runtime.md)
- [Memory and context](memory-and-context.md)
- [Security and hardening](security-and-hardening.md)

## Related code

- `internal/triggers/`
- `internal/heartbeat/`
- `internal/cron/`
````

## File: internal/agent/noop_streamer.go
````go
package agent

import (
	"context"

	"or3-intern/internal/channels"
)

type NullStreamer struct{}

type nullStreamWriter struct{}

func (NullStreamer) BeginStream(context.Context, string, map[string]any) (channels.StreamWriter, error) {
	return nullStreamWriter{}, nil
}

func (nullStreamWriter) WriteDelta(context.Context, string) error { return nil }
func (nullStreamWriter) Close(context.Context, string) error      { return nil }
func (nullStreamWriter) Abort(context.Context) error              { return nil }
````

## File: internal/agent/tool_policy.go
````go
package agent

import (
	"fmt"
	"strings"

	"or3-intern/internal/tools"
)

type ServiceToolPolicy struct {
	Mode         string
	AllowedTools []string
	BlockedTools []string
}

func ResolveServiceToolAllowlist(base *tools.Registry, policy *ServiceToolPolicy, legacyAllowed []string) ([]string, bool, error) {
	if policy == nil {
		allowed := normalizeToolNames(legacyAllowed)
		if len(allowed) == 0 {
			return nil, false, nil
		}
		return allowed, true, nil
	}

	mode := strings.ToLower(strings.TrimSpace(policy.Mode))
	allowed := normalizeToolNames(policy.AllowedTools)
	blocked := normalizeToolNames(policy.BlockedTools)
	if mode == "" {
		return nil, false, fmt.Errorf("tool_policy mode is required")
	}

	switch mode {
	case "allow_all":
		return nil, false, nil
	case "deny_all":
		return []string{}, true, nil
	case "allow_list":
		if len(allowed) == 0 {
			return nil, false, fmt.Errorf("tool_policy allow_list requires allowed_tools")
		}
		return allowed, true, nil
	case "deny_list":
		if len(blocked) == 0 {
			return nil, false, fmt.Errorf("tool_policy deny_list requires blocked_tools")
		}
		if base == nil {
			return []string{}, true, nil
		}
		blockedSet := make(map[string]struct{}, len(blocked))
		for _, name := range blocked {
			blockedSet[name] = struct{}{}
		}
		resolved := make([]string, 0, len(base.Names()))
		for _, name := range base.Names() {
			if _, blocked := blockedSet[name]; blocked {
				continue
			}
			resolved = append(resolved, name)
		}
		return resolved, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported tool_policy mode: %s", policy.Mode)
	}
}

func normalizeToolNames(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
````

## File: internal/artifacts/attachment.go
````go
package artifacts

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"
)

const (
	KindImage = "image"
	KindAudio = "audio"
	KindVideo = "video"
	KindFile  = "file"
)

type Attachment struct {
	ArtifactID string `json:"artifact_id"`
	Filename   string `json:"filename"`
	Mime       string `json:"mime"`
	Kind       string `json:"kind"`
	SizeBytes  int64  `json:"size_bytes"`
}

type StoredArtifact struct {
	ID         string
	SessionKey string
	Mime       string
	Path       string
	SizeBytes  int64
}

func DetectKind(filename, mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mt, "image/"):
		return KindImage
	case strings.HasPrefix(mt, "audio/"):
		return KindAudio
	case strings.HasPrefix(mt, "video/"):
		return KindVideo
	}

	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".heic", ".heif":
		return KindImage
	case ".mp3", ".m4a", ".wav", ".ogg", ".oga", ".opus", ".aac", ".flac":
		return KindAudio
	case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".m4v":
		return KindVideo
	default:
		return KindFile
	}
}

func NormalizeFilename(name, mimeType string) string {
	name = strings.TrimSpace(filepath.Base(name))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	if filepath.Ext(name) == "" {
		if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
			name += exts[0]
		}
	}
	return name
}

func Marker(att Attachment) string {
	name := strings.TrimSpace(att.Filename)
	if name == "" {
		name = "attachment"
	}
	kind := strings.TrimSpace(att.Kind)
	if kind == "" {
		kind = DetectKind(name, att.Mime)
	}
	return fmt.Sprintf("[%s: %s]", kind, name)
}

func FailureMarker(kind, name, reason string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = KindFile
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "attachment"
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Sprintf("[%s: %s - unavailable]", kind, name)
	}
	return fmt.Sprintf("[%s: %s - %s]", kind, name, reason)
}
````

## File: internal/channels/cli/terminal.go
````go
package cli

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-isatty"
)

// isTTY is true when stdout is an interactive terminal.
var isTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

// ---------- ANSI helpers ----------

const (
	ansiReset     = "\033[0m"
	ansiBold      = "\033[1m"
	ansiDim       = "\033[2m"
	ansiItalic    = "\033[3m"
	ansiCyan      = "\033[36m"
	ansiYellow    = "\033[33m"
	ansiGray      = "\033[90m"
	ansiWhite     = "\033[97m"
	ansiGreen     = "\033[32m"
	ansiCursorUp  = "\033[1A" // move cursor up one line
	ansiClearLine = "\033[2K" // clear entire line
)

func style(codes, text string) string {
	if !isTTY {
		return text
	}
	return codes + text + ansiReset
}

// ---------- Banner ----------

// Banner returns the startup header shown when the CLI launches.
func Banner() string {
	if !isTTY {
		return "or3-intern CLI. Type /exit to quit.\n"
	}
	top := style(ansiDim, "╭───────────────────────────────────────────────╮")
	mid1 := style(ansiDim, "│") + "  " + style(ansiBold+ansiCyan, "or3-intern") + "                                  " + style(ansiDim, "│")
	mid2 := style(ansiDim, "│") + "  " + style(ansiGray, "Type /exit to quit · /new for new session") + "  " + style(ansiDim, "│")
	bot := style(ansiDim, "╰───────────────────────────────────────────────╯")
	return fmt.Sprintf("\n%s\n%s\n%s\n%s\n", top, mid1, mid2, bot)
}

// ---------- Prompt / separators ----------

// Prompt returns the input prompt string.
func Prompt() string {
	if !isTTY {
		return "> "
	}
	return ansiBold + ansiCyan + "❯ " + ansiReset
}

// ShowPrompt prints a blank line gap then the prompt, signalling the user
// that input is ready. Called by the Deliverer after finishing output.
func ShowPrompt() {
	if !isTTY {
		fmt.Print(Prompt())
		return
	}
	fmt.Print("\n" + Prompt())
}

// Separator returns a faint horizontal rule placed after a response block.
func Separator() string {
	if !isTTY {
		return ""
	}
	return "  " + ansiDim + strings.Repeat("─", 50) + ansiReset
}

// ---------- User message formatting ----------

// RewriteUserMessage moves the cursor up to overwrite the raw prompt line
// with a styled version of the user's message. This transforms the bare
// "❯ text" into a clearly labeled user block. No-op when not a TTY.
func RewriteUserMessage(text string) {
	if !isTTY {
		return
	}
	// Move up over the raw prompt line and replace it.
	fmt.Print(ansiCursorUp + ansiClearLine)
	fmt.Printf("  %s%s▌%s %s%s\n",
		ansiBold, ansiCyan, ansiReset,
		style(ansiBold+ansiWhite, text), ansiReset)
}

// ---------- Assistant header ----------

// AssistantHeader returns the header line printed before each response.
func AssistantHeader() string {
	if !isTTY {
		return ""
	}
	name := ansiBold + ansiGreen + "◆ or3-intern" + ansiReset
	line := ansiDim + " " + strings.Repeat("─", 38) + ansiReset
	return "\n  " + name + line + "\n"
}

// ---------- Response formatting ----------

// ResponsePrefix returns the prefix printed before the first streaming delta.
func ResponsePrefix() string {
	if !isTTY {
		return "\n"
	}
	return AssistantHeader() + "\n    "
}

// FormatResponse wraps a complete (non-streamed) response for display.
func FormatResponse(text string) string {
	if !isTTY {
		return "[cli] " + text
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return AssistantHeader() + "\n" + strings.Join(lines, "\n")
}

// ---------- Spinner ----------

// Spinner provides a braille-dot animation on stdout while the agent thinks.
// Only animates when stdout is a TTY; safe for concurrent Start/Stop.
type Spinner struct {
	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	stopped chan struct{}
}

// NewSpinner creates a ready-to-use Spinner (initially stopped).
func NewSpinner() *Spinner {
	return &Spinner{}
}

// Start begins the animation with the given label (e.g. "thinking…").
// No-op if already running or stdout is not a TTY.
func (s *Spinner) Start(label string) {
	if !isTTY {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.stopped = make(chan struct{})
	s.mu.Unlock()

	go func() {
		defer close(s.stopped)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		// First frame immediately.
		fmt.Fprintf(os.Stdout, "\r  %s%s %s%s", ansiDim, frames[0], label, ansiReset)
		for {
			select {
			case <-s.stopCh:
				// Clear the spinner line.
				fmt.Fprint(os.Stdout, "\r\033[K")
				return
			case <-ticker.C:
				i++
				fmt.Fprintf(os.Stdout, "\r  %s%s %s%s", ansiDim, frames[i%len(frames)], label, ansiReset)
			}
		}
	}()
}

// Stop halts the animation and clears the spinner line.
// Blocks until the animation goroutine exits. Safe to call when not running.
func (s *Spinner) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	stopped := s.stopped
	s.mu.Unlock()
	<-stopped
}
````

## File: internal/channels/media.go
````go
package channels

import (
	"fmt"
	"strings"
)

func ComposeMessageText(text string, markers []string) string {
	parts := make([]string, 0, len(markers)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, strings.TrimSpace(text))
	}
	for _, marker := range markers {
		marker = strings.TrimSpace(marker)
		if marker == "" {
			continue
		}
		parts = append(parts, marker)
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func MediaPaths(meta map[string]any) []string {
	if len(meta) == 0 {
		return nil
	}
	raw := meta["media_paths"]
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				continue
			}
			out = append(out, item)
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	default:
		return nil
	}
}
````

## File: internal/channels/stream.go
````go
package channels

import "context"

// StreamWriter is an optional interface for channels that can receive
// incremental text deltas (e.g., CLI live output, editable messages).
// Channels that do not implement streaming use final-only delivery.
type StreamWriter interface {
	// WriteDelta appends a text delta to the in-progress response.
	WriteDelta(ctx context.Context, text string) error
	// Close finalizes the stream with the complete text.
	Close(ctx context.Context, finalText string) error
	// Abort cancels the stream cleanly without leaving partial output.
	Abort(ctx context.Context) error
}

// StreamingChannel is an optional interface a channel can implement
// to indicate it supports incremental streaming delivery.
type StreamingChannel interface {
	// BeginStream starts a new streaming response to the given recipient.
	// meta contains channel-specific metadata (e.g., chat_id).
	// Returns a StreamWriter to write deltas, or an error.
	BeginStream(ctx context.Context, to string, meta map[string]any) (StreamWriter, error)
}
````

## File: internal/scope/scope.go
````go
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
````

## File: internal/security/store.go
````go
package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

const secretRefPrefix = "secret:"

type SecretManager struct {
	DB  *db.DB
	Key []byte
}

func LoadOrCreateKey(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("key file required")
	}
	if raw, err := os.ReadFile(path); err == nil {
		if decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); decErr == nil && len(decoded) >= 32 {
			return decoded[:32], nil
		}
		if len(raw) >= 32 {
			return raw[:32], nil
		}
		return nil, fmt.Errorf("key file too short")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func LoadExistingKey(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("key file required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); decErr == nil && len(decoded) >= 32 {
		return decoded[:32], nil
	}
	if len(raw) >= 32 {
		return raw[:32], nil
	}
	return nil, fmt.Errorf("key file too short")
}

func (m *SecretManager) ResolveRef(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), secretRefPrefix) {
		return raw, nil
	}
	if m == nil || m.DB == nil || len(m.Key) == 0 {
		return "", fmt.Errorf("secret store unavailable")
	}
	name := strings.TrimSpace(raw[len(secretRefPrefix):])
	if name == "" {
		return "", fmt.Errorf("invalid secret ref")
	}
	secret, ok, err := m.Get(ctx, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("secret not found: %s", name)
	}
	return secret, nil
}

func (m *SecretManager) Put(ctx context.Context, name, value string) error {
	if m == nil || m.DB == nil || len(m.Key) == 0 {
		return fmt.Errorf("secret store unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name required")
	}
	ciphertext, nonce, err := encryptBlob(m.Key, []byte(value))
	if err != nil {
		return err
	}
	return m.DB.PutSecret(ctx, name, ciphertext, nonce, 1, "v1")
}

func (m *SecretManager) Get(ctx context.Context, name string) (string, bool, error) {
	if m == nil || m.DB == nil || len(m.Key) == 0 {
		return "", false, fmt.Errorf("secret store unavailable")
	}
	record, ok, err := m.DB.GetSecret(ctx, name)
	if err != nil || !ok {
		return "", ok, err
	}
	plain, err := decryptBlob(m.Key, record.Ciphertext, record.Nonce)
	if err != nil {
		return "", false, fmt.Errorf("decrypt secret %s: %w", name, err)
	}
	return string(plain), true, nil
}

func (m *SecretManager) Delete(ctx context.Context, name string) error {
	if m == nil || m.DB == nil {
		return fmt.Errorf("secret store unavailable")
	}
	return m.DB.DeleteSecret(ctx, name)
}

func (m *SecretManager) List(ctx context.Context) ([]string, error) {
	if m == nil || m.DB == nil {
		return nil, fmt.Errorf("secret store unavailable")
	}
	return m.DB.ListSecretNames(ctx)
}

type AuditLogger struct {
	DB     *db.DB
	Key    []byte
	Strict bool
}

func (a *AuditLogger) Record(ctx context.Context, eventType, sessionKey, actor string, payload any) error {
	if a == nil || a.DB == nil || len(a.Key) == 0 {
		if a != nil && a.Strict {
			return fmt.Errorf("audit logger unavailable")
		}
		return nil
	}
	return a.DB.AppendAuditEvent(ctx, db.AuditEventInput{
		EventType:  eventType,
		SessionKey: strings.TrimSpace(sessionKey),
		Actor:      strings.TrimSpace(actor),
		Payload:    payload,
	}, a.Key)
}

func (a *AuditLogger) Verify(ctx context.Context) error {
	if a == nil || a.DB == nil || len(a.Key) == 0 {
		return nil
	}
	return a.DB.VerifyAuditChain(ctx, a.Key)
}

func ResolveConfigSecrets(ctx context.Context, cfg config.Config, mgr *SecretManager) (config.Config, error) {
	resolved := cfg
	if mgr == nil {
		return resolved, nil
	}
	value := reflect.ValueOf(&resolved).Elem()
	if err := resolveValue(ctx, value, mgr); err != nil {
		return cfg, err
	}
	return resolved, nil
}

func ValidateNoSecretRefs(cfg config.Config) error {
	if path, ok := findSecretRef(reflect.ValueOf(cfg), "config"); ok {
		return fmt.Errorf("unresolved secret ref at %s", path)
	}
	return nil
}

func resolveValue(ctx context.Context, value reflect.Value, mgr *SecretManager) error {
	if !value.IsValid() {
		return nil
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return resolveValue(ctx, value.Elem(), mgr)
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if !value.Field(i).CanSet() {
				continue
			}
			if err := resolveValue(ctx, value.Field(i), mgr); err != nil {
				return err
			}
		}
	case reflect.Map:
		if value.IsNil() || value.Type().Key().Kind() != reflect.String {
			return nil
		}
		for _, key := range value.MapKeys() {
			elem := value.MapIndex(key)
			if !elem.IsValid() {
				continue
			}
			if value.Type().Elem().Kind() == reflect.String {
				resolved, err := mgr.ResolveRef(ctx, elem.String())
				if err != nil {
					return err
				}
				value.SetMapIndex(key, reflect.ValueOf(resolved))
				continue
			}
			clone := reflect.New(elem.Type()).Elem()
			clone.Set(elem)
			if err := resolveValue(ctx, clone, mgr); err != nil {
				return err
			}
			value.SetMapIndex(key, clone)
		}
	case reflect.String:
		resolved, err := mgr.ResolveRef(ctx, value.String())
		if err != nil {
			return err
		}
		value.SetString(resolved)
	}
	return nil
}

func findSecretRef(value reflect.Value, path string) (string, bool) {
	if !value.IsValid() {
		return "", false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			name := field.Name
			if tag := strings.TrimSpace(strings.Split(field.Tag.Get("json"), ",")[0]); tag != "" && tag != "-" {
				name = tag
			}
			if foundPath, ok := findSecretRef(value.Field(i), path+"."+name); ok {
				return foundPath, true
			}
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			foundPath, ok := findSecretRef(value.MapIndex(key), path+"."+fmt.Sprint(key.Interface()))
			if ok {
				return foundPath, true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if foundPath, ok := findSecretRef(value.Index(i), fmt.Sprintf("%s[%d]", path, i)); ok {
				return foundPath, true
			}
		}
	case reflect.String:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value.String())), secretRefPrefix) {
			return path, true
		}
	}
	return "", false
}

func encryptBlob(master, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(deriveKey(master, "secrets"))
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	sealed := aead.Seal(nil, nonce, plaintext, nil)
	return sealed, nonce, nil
}

func decryptBlob(master, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(deriveKey(master, "secrets"))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, nil)
}

func deriveKey(master []byte, label string) []byte {
	h := hmac.New(sha256.New, master)
	_, _ = h.Write([]byte(label))
	sum := h.Sum(nil)
	return sum[:32]
}

func filepathDir(path string) string {
	return filepath.Dir(path)
}
````

## File: internal/tools/cron.go
````go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"or3-intern/internal/cron"
)

type CronTool struct {
	Base
	Svc *cron.Service
}

func (t *CronTool) Name() string { return "cron" }
func (t *CronTool) Description() string {
	return "Manage scheduled jobs: add/list/remove/run/status."
}
func (t *CronTool) Parameters() map[string]any {
	return map[string]any{"type":"object","properties":map[string]any{
		"action": map[string]any{"type":"string","enum":[]any{"add","list","remove","run","status"}},
		"job": map[string]any{"type":"object","description":"job object for add"},
		"id": map[string]any{"type":"string","description":"job id for remove/run"},
		"force": map[string]any{"type":"boolean","description":"force run"},
	},"required":[]string{"action"}}
}
func (t *CronTool) Schema() map[string]any { return t.SchemaFor(t.Name(), t.Description(), t.Parameters()) }

func (t *CronTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Svc == nil { return "", fmt.Errorf("cron service not configured") }
	act := strings.TrimSpace(fmt.Sprint(params["action"]))
	switch act {
	case "status":
		s, err := t.Svc.Status()
		if err != nil { return "", err }
		b, _ := json.MarshalIndent(s, "", "  ")
		return string(b), nil
	case "list":
		j, err := t.Svc.List()
		if err != nil { return "", err }
		b, _ := json.MarshalIndent(j, "", "  ")
		return string(b), nil
	case "remove":
		id := strings.TrimSpace(fmt.Sprint(params["id"]))
		ok, err := t.Svc.Remove(id)
		if err != nil { return "", err }
		return fmt.Sprintf("removed: %v", ok), nil
	case "run":
		id := strings.TrimSpace(fmt.Sprint(params["id"]))
		force, _ := params["force"].(bool)
		ok, err := t.Svc.RunNow(ctx, id, force)
		if err != nil { return "", err }
		return fmt.Sprintf("ran: %v", ok), nil
	case "add":
		raw, _ := params["job"].(map[string]any)
		if raw == nil { return "", fmt.Errorf("missing job") }
		b, _ := json.Marshal(raw)
		var j cron.CronJob
		if err := json.Unmarshal(b, &j); err != nil { return "", err }
		// defaults
		if j.Enabled == false && raw["enabled"] == nil { j.Enabled = true }
		if j.Payload.Kind == "" { j.Payload.Kind = "agent_turn" }
		if j.Schedule.Kind == "" { j.Schedule.Kind = cron.KindEvery; j.Schedule.EveryMS = int64((24*time.Hour).Milliseconds()) }
		if err := t.Svc.Add(j); err != nil { return "", err }
		return "ok", nil
	default:
		return "", fmt.Errorf("unknown action")
	}
}
````

## File: internal/triggers/structured_tasks.go
````go
package triggers

import (
	"encoding/json"
	"fmt"
	"strings"
)

const MetaKeyStructuredTasks = "structured_tasks"

type StructuredToolCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params,omitempty"`
}

type StructuredTaskEnvelope struct {
	Version int                  `json:"version,omitempty"`
	Tasks   []StructuredToolCall `json:"tasks"`
}

func StructuredTasksMap(env StructuredTaskEnvelope) map[string]any {
	tasks := make([]map[string]any, 0, len(env.Tasks))
	for _, task := range env.Tasks {
		tool := strings.TrimSpace(task.Tool)
		if tool == "" {
			continue
		}
		entry := map[string]any{"tool": tool}
		if len(task.Params) > 0 {
			params := make(map[string]any, len(task.Params))
			for key, value := range task.Params {
				trimmed := strings.TrimSpace(key)
				if trimmed == "" {
					continue
				}
				params[trimmed] = value
			}
			if len(params) > 0 {
				entry["params"] = params
			}
		}
		tasks = append(tasks, entry)
	}
	out := map[string]any{"tasks": tasks}
	if env.Version > 0 {
		out["version"] = env.Version
	}
	return out
}

func StructuredTasksFromMeta(meta map[string]any) (StructuredTaskEnvelope, bool) {
	if len(meta) == 0 {
		return StructuredTaskEnvelope{}, false
	}
	raw, ok := meta[MetaKeyStructuredTasks]
	if !ok || raw == nil {
		return StructuredTaskEnvelope{}, false
	}
	return normalizeStructuredTasks(raw)
}

func ParseStructuredTasksJSON(data []byte) (StructuredTaskEnvelope, bool) {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return StructuredTaskEnvelope{}, false
	}
	return normalizeStructuredTasks(raw)
}

func ParseStructuredTasksText(text string) (StructuredTaskEnvelope, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return StructuredTaskEnvelope{}, false
	}
	if env, ok := ParseStructuredTasksJSON([]byte(text)); ok {
		return env, true
	}
	block, ok := extractStructuredTasksFence(text)
	if !ok {
		return StructuredTaskEnvelope{}, false
	}
	return ParseStructuredTasksJSON([]byte(block))
}

func normalizeStructuredTasks(raw any) (StructuredTaskEnvelope, bool) {
	root, ok := raw.(map[string]any)
	if ok {
		if tasksRaw, exists := root["structured_tasks"]; exists {
			env, ok := normalizeStructuredTasks(tasksRaw)
			if !ok {
				return StructuredTaskEnvelope{}, false
			}
			if version := toInt(root["version"]); version > 0 && env.Version == 0 {
				env.Version = version
			}
			return env, true
		}
		tasksRaw, exists := root["tasks"]
		if !exists {
			return StructuredTaskEnvelope{}, false
		}
		tasks, ok := normalizeStructuredTaskList(tasksRaw)
		if !ok {
			return StructuredTaskEnvelope{}, false
		}
		version := toInt(root["version"])
		return StructuredTaskEnvelope{Version: version, Tasks: tasks}, len(tasks) > 0
	}
	if tasks, ok := normalizeStructuredTaskList(raw); ok {
		return StructuredTaskEnvelope{Tasks: tasks}, len(tasks) > 0
	}
	return StructuredTaskEnvelope{}, false
}

func normalizeStructuredTaskList(raw any) ([]StructuredToolCall, bool) {
	if typed, ok := raw.([]StructuredToolCall); ok {
		out := make([]StructuredToolCall, 0, len(typed))
		for _, task := range typed {
			tool := strings.TrimSpace(task.Tool)
			if tool == "" {
				return nil, false
			}
			out = append(out, StructuredToolCall{Tool: tool, Params: task.Params})
		}
		return out, len(out) > 0
	}
	if typed, ok := raw.([]map[string]any); ok {
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			items = append(items, item)
		}
		raw = items
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, false
	}
	out := make([]StructuredToolCall, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		tool := strings.TrimSpace(toString(entry["tool"]))
		if tool == "" {
			return nil, false
		}
		params := map[string]any{}
		if rawParams, exists := entry["params"]; exists && rawParams != nil {
			mapped, ok := rawParams.(map[string]any)
			if !ok {
				return nil, false
			}
			for key, value := range mapped {
				trimmed := strings.TrimSpace(key)
				if trimmed == "" {
					continue
				}
				params[trimmed] = value
			}
		}
		if len(params) == 0 {
			params = nil
		}
		out = append(out, StructuredToolCall{Tool: tool, Params: params})
	}
	return out, true
}

func extractStructuredTasksFence(text string) (string, bool) {
	lines := strings.Split(text, "\n")
	inside := false
	var body []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inside {
			if !strings.HasPrefix(trimmed, "```") {
				continue
			}
			info := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "```")))
			if strings.Contains(info, "or3-tasks") || strings.Contains(info, "structured-tasks") || strings.Contains(info, "autonomous-tasks") {
				inside = true
				body = body[:0]
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			payload := strings.TrimSpace(strings.Join(body, "\n"))
			if payload == "" {
				return "", false
			}
			return payload, true
		}
		body = append(body, line)
	}
	return "", false
}

func toString(raw any) string {
	if raw == nil {
		return ""
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "<nil>" {
		return ""
	}
	return text
}

func toInt(raw any) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
````

## File: breakdown.md
````markdown
## Simple Architecture Breakdown (Core Layers)

1. **Gateway** (The Central Hub / Control Plane)  
   This is the single main process you run (e.g., `openclaw gateway`).  
   - It listens for incoming messages from all your connected apps at once.  
   - It manages sessions (so conversations stay coherent across WhatsApp → Telegram → Discord).  
   - It handles routing: message comes in → Gateway wakes the right agent → agent thinks/acts → Gateway sends reply back through the original channel.  
   - It also runs background stuff like heartbeats and crons.

2. **Agent Runtime / Reasoning Loop** (The Brain in Action)  
   When there's input (your message, a scheduled cron, a webhook, or a heartbeat trigger):  
   - **Gather context** — Pulls from conversation history + persistent memory files + workspace files.  
   - **Call the LLM** — Sends the assembled prompt to your chosen model (Claude Opus, Sonnet, etc.).  
   - **Model decides** — Outputs normal text reply OR tool calls (e.g., "use browser to check site", "write file X", "run shell command").  
   - **Execute tools/skills** → Loop repeats if needed (classic ReAct/agent loop: observe → think → act → repeat).  
   - Finally streams the response back via Gateway.  
   This loop makes it feel "smart" and capable of multi-step tasks.

3. **Memory System** (What Makes It Feel Persistent)  
   Everything is file-based (super simple, no database hassle):  
   - Core files like `soul.md` (personality/vibe/boundaries), `identity.md` (who/what it is), `MEMORY.md` (short-term recall), `HEARTBEAT.md` (what to check periodically).  
   - Long-term: daily logs, project folders, thematic notes in a `memory/` dir.  
   - Agent reads these on startup/wakeup → "remembers" across restarts/sessions.  
   - Semantic search across files for pulling relevant old info without bloating context.

4. **Tools & Skills** (The Hands — What Lets It Act)  
   - Built-in tools: browser control, file ops, shell execution, voice on macOS/iOS, Canvas UI, etc.  
   - **Skills** — Community plugins (thousands in ClawHub marketplace). These are installable extensions (e.g., GitHub integration, semantic scraping, email summarizer, custom browser stealth).  
   - Agent decides which skill/tool to call based on descriptions (like function calling).  
   - Very extensible — you (or community) can write new ones in code.

5. **Proactivity / Autonomy Layer** (Why It Feels "Alive")  
   - **Heartbeat** — Agent wakes every X minutes/hours, reads `HEARTBEAT.md`, checks for pending work (new emails, calendar, mentions), acts if needed, then sleeps.  
   - **Cron jobs** — Precise scheduled tasks (e.g., "at 3 AM scrape report and notify me").  
   - **Multi-agent support** — Main agent can spawn sub-agents (specialized ones for research/coding/writing) that run in parallel/isolated sessions.  
   - Triggers: messages, schedules, webhooks, file changes → agent can start working without you prompting.

### Quick Summary Flow
- You message via WhatsApp: "Summarize my emails and draft replies."  
- Gateway receives → routes to agent session.  
- Agent assembles context (history + MEMORY.md + email skill).  
- LLM thinks → calls email tool + browser if needed → loops until done.  
- Gateway sends reply + any side effects (files written, calendar events added).  
- Later (heartbeat/cron): agent wakes independently → checks if anything new needs doing → messages you proactively.
````

## File: cmd/or3-intern/init.go
````go
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
)

type initProviderPreset struct {
	label      string
	apiBase    string
	model      string
	embedModel string
}

var initProviderPresets = map[string]initProviderPreset{
	"1": {
		label:      "OpenAI",
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
	"2": {
		label:      "OpenRouter",
		apiBase:    "https://openrouter.ai/api/v1",
		model:      "openai/gpt-4o-mini",
		embedModel: "text-embedding-3-small",
	},
	"3": {
		label:      "Custom OpenAI-compatible",
		apiBase:    "https://api.openai.com/v1",
		model:      "gpt-4.1-mini",
		embedModel: "text-embedding-3-small",
	},
}

func runInit(cfgPath string) error {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}
	return runInitWithIO(os.Stdin, os.Stdout, cfgPathOrDefault(cfgPath), cwd)
}

func runInitWithIO(in io.Reader, out io.Writer, cfgPath, cwd string) error {
	reader := bufio.NewReader(in)
	cfg := initDefaults(cwd)

	fmt.Fprintln(out, "or3-intern setup")
	fmt.Fprintln(out, "We'll create a config file and pick defaults that work well for local testing.")
	fmt.Fprintf(out, "Config file: %s\n\n", cfgPath)

	providerChoice, err := promptChoice(reader, out,
		"Choose your provider",
		[]string{"1) OpenAI", "2) OpenRouter", "3) Custom OpenAI-compatible"},
		defaultProviderChoice(cfg.Provider.APIBase),
	)
	if err != nil {
		return err
	}
	applyProviderPreset(&cfg, providerChoice)

	cfg.Provider.APIBase, err = promptString(reader, out, "API base", cfg.Provider.APIBase)
	if err != nil {
		return err
	}
	cfg.Provider.Model, err = promptString(reader, out, "Chat model", cfg.Provider.Model)
	if err != nil {
		return err
	}
	cfg.Provider.EmbedModel, err = promptString(reader, out, "Embedding model", cfg.Provider.EmbedModel)
	if err != nil {
		return err
	}

	saveKey, err := promptBool(reader, out, "Save API key in config.json (stored locally with restricted permissions; env vars are safer)?", strings.TrimSpace(cfg.Provider.APIKey) != "")
	if err != nil {
		return err
	}
	if saveKey {
		cfg.Provider.APIKey, err = promptString(reader, out, "API key", cfg.Provider.APIKey)
		if err != nil {
			return err
		}
	} else {
		cfg.Provider.APIKey = ""
	}

	cfg.DBPath, err = promptString(reader, out, "SQLite DB path", cfg.DBPath)
	if err != nil {
		return err
	}
	cfg.ArtifactsDir, err = promptString(reader, out, "Artifacts directory", cfg.ArtifactsDir)
	if err != nil {
		return err
	}

	restrictWorkspace, err := promptBool(reader, out, "Restrict file tools to the current workspace?", cfg.Tools.RestrictToWorkspace)
	if err != nil {
		return err
	}
	cfg.Tools.RestrictToWorkspace = restrictWorkspace
	if restrictWorkspace {
		cfg.WorkspaceDir = cwd
	} else if strings.TrimSpace(cfg.WorkspaceDir) == "" {
		cfg.WorkspaceDir = cwd
	}

	cfg.Tools.BraveAPIKey, err = promptString(reader, out, "Brave Search API key (optional)", cfg.Tools.BraveAPIKey)
	if err != nil {
		return err
	}

	if err := config.Save(cfgPath, cfg); err != nil {
		return err
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "Saved config to %s\n", cfgPath)
	fmt.Fprintf(out, "Provider: %s\n", initProviderPresets[providerChoice].label)
	fmt.Fprintf(out, "DB: %s\n", cfg.DBPath)
	fmt.Fprintf(out, "Artifacts: %s\n", cfg.ArtifactsDir)
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) != "" {
		fmt.Fprintf(out, "Workspace restriction: enabled (%s)\n", cfg.WorkspaceDir)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Next step:")
	fmt.Fprintln(out, "  go run ./cmd/or3-intern chat")
	return nil
}

func initDefaults(cwd string) config.Config {
	cfg := config.Default()
	config.ApplyEnvOverrides(&cfg)
	cwd = strings.TrimSpace(cwd)
	if cwd != "" {
		cfg.WorkspaceDir = cwd
		cfg.DBPath = filepath.Join(cwd, ".or3", "or3-intern.sqlite")
		cfg.ArtifactsDir = filepath.Join(cwd, ".or3", "artifacts")
		cfg.Tools.RestrictToWorkspace = true
	}
	return cfg
}

func defaultProviderChoice(apiBase string) string {
	if strings.Contains(strings.ToLower(apiBase), "openrouter.ai") {
		return "2"
	}
	return "1"
}

func applyProviderPreset(cfg *config.Config, choice string) {
	preset, ok := initProviderPresets[choice]
	if !ok || cfg == nil {
		return
	}
	cfg.Provider.APIBase = preset.apiBase
	cfg.Provider.Model = preset.model
	cfg.Provider.EmbedModel = preset.embedModel
}

func promptChoice(reader *bufio.Reader, out io.Writer, label string, options []string, defaultChoice string) (string, error) {
	fmt.Fprintln(out, label)
	for _, option := range options {
		fmt.Fprintf(out, "  %s\n", option)
	}
	for {
		answer, err := promptString(reader, out, "Selection", defaultChoice)
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		if _, ok := initProviderPresets[answer]; ok {
			return answer, nil
		}
		fmt.Fprintln(out, "Please choose 1, 2, or 3.")
	}
}

func promptBool(reader *bufio.Reader, out io.Writer, label string, defaultValue bool) (bool, error) {
	defaultText := "n"
	if defaultValue {
		defaultText = "y"
	}
	for {
		answer, err := promptString(reader, out, label+" (y/n)", defaultText)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(answer)) {
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		default:
			fmt.Fprintln(out, "Please answer y or n.")
		}
	}
}

func promptString(reader *bufio.Reader, out io.Writer, label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}
````

## File: cmd/or3-intern/migrate.go
````go
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"or3-intern/internal/db"
)

func migrateJSONL(ctx context.Context, d *db.DB, path, sessionKey string) error {
	f, err := os.Open(path)
	if err != nil { return err }
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024), 4<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		if len(line) == 0 { continue }
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			// tolerate non-json line
			if _, err := d.AppendMessage(ctx, sessionKey, "user", line, map[string]any{"migrated_line": lineNo}); err != nil {
				return fmt.Errorf("line %d: %w", lineNo, err)
			}
			continue
		}
		// detect metadata
		if lineNo == 1 {
			if _, ok := obj["messages"]; ok {
				// not expected
			}
			// store as session metadata_json if it looks like metadata
			if obj["role"] == nil && obj["content"] == nil {
				b, _ := json.Marshal(obj)
				if err := d.EnsureSession(ctx, sessionKey); err != nil {
					log.Printf("ensure session failed during migration: %v", err)
				}
				if _, err := d.SQL.ExecContext(ctx, `UPDATE sessions SET metadata_json=? WHERE key=?`, string(b), sessionKey); err != nil {
					log.Printf("session metadata update failed during migration: %v", err)
				}
				continue
			}
		}
		role := toStr(obj["role"])
		if role == "" { role = "user" }
		content := toStr(obj["content"])
		payload := obj
		delete(payload, "role")
		delete(payload, "content")
		_, err := d.AppendMessage(ctx, sessionKey, role, content, payload)
		if err != nil { return fmt.Errorf("line %d: %w", lineNo, err) }
	}
	return sc.Err()
}

func toStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(v)
	}
}
````

## File: cmd/or3-intern/service.go
````go
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/tools"
)

type serviceServer struct {
	runtime         *agent.Runtime
	subagentManager *agent.SubagentManager
	jobs            *agent.JobRegistry
}

func runServiceCommand(ctx context.Context, cfg config.Config, rt *agent.Runtime, subagentManager *agent.SubagentManager, jobs *agent.JobRegistry) error {
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		return fmt.Errorf("service secret is required")
	}
	if rt == nil {
		return fmt.Errorf("runtime not configured")
	}
	if jobs == nil {
		jobs = agent.NewJobRegistry(0, 0)
	}
	server := &serviceServer{runtime: rt, subagentManager: subagentManager, jobs: jobs}
	mux := http.NewServeMux()
	mux.Handle("/internal/v1/turns", http.HandlerFunc(server.handleTurns))
	mux.Handle("/internal/v1/subagents", http.HandlerFunc(server.handleSubagents))
	mux.Handle("/internal/v1/jobs/", http.HandlerFunc(server.handleJobs))

	httpServer := &http.Server{
		Addr:              cfg.Service.Listen,
		Handler:           serviceAuthMiddleware(cfg.Service.Secret, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("or3-intern service listening on %s", cfg.Service.Listen)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *serviceServer) handleTurns(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	req, err := decodeServiceTurnRequest(r.Body, s.runtime.Tools)
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if req.SessionKey == "" || req.Message == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "session_key and message are required"})
		return
	}
	job := s.jobs.Register("turn")
	s.jobs.Publish(job.ID, "queued", map[string]any{"status": "queued", "session_key": req.SessionKey})

	ctx, cancel := context.WithCancel(withDetachedContext(r.Context()))
	s.jobs.AttachCancel(job.ID, cancel)
	go s.runTurnJob(ctx, job.ID, req)

	if acceptsSSE(r) {
		s.streamJob(w, r, job.ID)
		return
	}
	snapshot, ok := s.jobs.Wait(r.Context(), job.ID)
	if !ok {
		writeServiceJSON(w, http.StatusGatewayTimeout, map[string]any{"error": "job timed out", "job_id": job.ID})
		return
	}
	statusCode := http.StatusOK
	if snapshot.Status == "failed" {
		statusCode = http.StatusBadGateway
	}
	writeServiceJSON(w, statusCode, buildJobResponse(snapshot))
}

func (s *serviceServer) runTurnJob(ctx context.Context, jobID string, req serviceTurnRequest) {
	s.jobs.Publish(jobID, "started", map[string]any{"status": "running", "session_key": req.SessionKey})
	observer := &serviceObserver{ConversationObserver: s.jobs.Observer(jobID)}
	runCtx := agent.ContextWithConversationObserver(ctx, observer)
	runCtx = agent.ContextWithStreamingChannel(runCtx, agent.NullStreamer{})
	if req.RestrictTools {
		filtered := tools.NewRegistry()
		if len(req.AllowedTools) > 0 {
			filtered = s.runtime.Tools.CloneFiltered(req.AllowedTools)
		}
		runCtx = agent.ContextWithToolRegistry(runCtx, filtered)
	}
	meta := cloneServiceMeta(req.Meta)
	if req.ProfileName != "" {
		meta["profile_name"] = req.ProfileName
	}
	err := s.runtime.Handle(runCtx, bus.Event{
		Type:       bus.EventUserMessage,
		SessionKey: req.SessionKey,
		Channel:    "service",
		From:       "or3-net",
		Message:    req.Message,
		Meta:       meta,
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			s.jobs.Complete(jobID, "aborted", map[string]any{"session_key": req.SessionKey, "message": "job aborted"})
			return
		}
		s.jobs.Fail(jobID, err.Error(), map[string]any{"session_key": req.SessionKey})
		return
	}
	s.jobs.Complete(jobID, "completed", map[string]any{"session_key": req.SessionKey, "final_text": observer.finalText})
}

func (s *serviceServer) handleSubagents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if s.subagentManager == nil {
		writeServiceJSON(w, http.StatusServiceUnavailable, map[string]any{"error": "subagent manager is not enabled"})
		return
	}
	req, err := decodeServiceSubagentRequest(r.Body, backgroundToolsRegistry(s.subagentManager))
	if err != nil {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid request body"})
		return
	}
	if req.ParentSessionKey == "" || req.Task == "" {
		writeServiceJSON(w, http.StatusBadRequest, map[string]any{"error": "parent_session_key and task are required"})
		return
	}
	job, err := s.subagentManager.EnqueueService(r.Context(), agent.ServiceSubagentRequest{
		ParentSessionKey: req.ParentSessionKey,
		Task:             req.Task,
		PromptSnapshot:   req.PromptSnapshot,
		AllowedTools:     req.AllowedTools,
		RestrictTools:    req.RestrictTools,
		ProfileName:      req.ProfileName,
		Channel:          req.Channel,
		ReplyTo:          req.ReplyTo,
		Meta:             req.Meta,
		Timeout:          time.Duration(req.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		statusCode := http.StatusBadGateway
		if err == db.ErrSubagentQueueFull {
			statusCode = http.StatusTooManyRequests
		}
		writeServiceJSON(w, statusCode, map[string]any{"error": err.Error()})
		return
	}
	writeServiceJSON(w, http.StatusAccepted, map[string]any{
		"job_id":            job.ID,
		"child_session_key": job.ChildSessionKey,
		"status":            db.SubagentStatusQueued,
	})
}

func (s *serviceServer) handleJobs(w http.ResponseWriter, r *http.Request) {
	relative := strings.TrimPrefix(r.URL.Path, "/internal/v1/jobs/")
	parts := strings.Split(strings.Trim(relative, "/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job route not found"})
		return
	}
	jobID := strings.TrimSpace(parts[0])
	action := strings.TrimSpace(parts[1])
	switch action {
	case "stream":
		if r.Method != http.MethodGet {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.streamJob(w, r, jobID)
	case "abort":
		if r.Method != http.MethodPost {
			writeServiceJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}
		s.abortJob(w, r, jobID)
	default:
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job action not found"})
	}
}

func (s *serviceServer) streamJob(w http.ResponseWriter, r *http.Request, jobID string) {
	snapshot, events, unsubscribe, ok := s.jobs.Subscribe(jobID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	defer unsubscribe()
	if err := beginSSE(w); err != nil {
		writeServiceJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	for _, event := range snapshot.Events {
		if err := writeSSEEvent(w, event.Type, event.Data); err != nil {
			return
		}
	}
	if isTerminalStatus(snapshot.Status) {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if err := writeSSEEvent(w, event.Type, event.Data); err != nil {
				return
			}
		}
	}
}

func (s *serviceServer) abortJob(w http.ResponseWriter, r *http.Request, jobID string) {
	if s.jobs.Cancel(jobID) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID})
		return
	}
	if s.subagentManager != nil {
		if err := s.subagentManager.Abort(r.Context(), jobID); err == nil {
			writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID})
			return
		}
	}
	snapshot, ok := s.jobs.Snapshot(jobID)
	if !ok {
		writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "job not found"})
		return
	}
	if isTerminalStatus(snapshot.Status) {
		writeServiceJSON(w, http.StatusOK, map[string]any{"ok": true, "job_id": jobID, "status": snapshot.Status})
		return
	}
	writeServiceJSON(w, http.StatusConflict, map[string]any{"error": "job is not abortable", "job_id": jobID})
}

func beginSSE(w http.ResponseWriter) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming is not supported")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()
	return nil
}

func writeSSEEvent(w http.ResponseWriter, eventType string, payload map[string]any) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming is not supported")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, encoded); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

func writeServiceJSON(w http.ResponseWriter, statusCode int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}

func acceptsSSE(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func buildJobResponse(snapshot agent.JobSnapshot) map[string]any {
	response := map[string]any{
		"job_id": snapshot.ID,
		"kind":   snapshot.Kind,
		"status": snapshot.Status,
	}
	for i := len(snapshot.Events) - 1; i >= 0; i-- {
		event := snapshot.Events[i]
		switch event.Type {
		case "completion":
			for key, value := range event.Data {
				response[key] = value
			}
			return response
		case "error":
			response["error"] = event.Data["message"]
			return response
		}
	}
	return response
}

func cloneServiceMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(meta))
	for key, value := range meta {
		out[key] = value
	}
	return out
}

func isTerminalStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "aborted", db.SubagentStatusSucceeded, db.SubagentStatusInterrupted:
		return true
	default:
		return false
	}
}

type serviceObserver struct {
	agent.ConversationObserver
	finalText string
}

func (o *serviceObserver) OnCompletion(ctx context.Context, finalText string, streamed bool) {
	o.finalText = finalText
	if o.ConversationObserver != nil {
		o.ConversationObserver.OnCompletion(ctx, finalText, streamed)
	}
}
````

## File: docs/api-reference.md
````markdown
# Internal service API reference

This page documents the authenticated HTTP API exposed by:

```bash
go run ./cmd/or3-intern service
```

## Intended use

`or3-intern service` is a loopback/private-network API intended for integrations such as OR3 Net. It uses the same runtime, tool registry, memory system, quotas, and subagent manager as the CLI and channel entrypoints.

## Service configuration

```json
{
  "service": {
    "enabled": true,
    "listen": "127.0.0.1:9100",
    "secret": "replace-with-a-long-random-shared-secret"
  }
}
```

Environment overrides documented in the README:

- `OR3_SERVICE_ENABLED`
- `OR3_SERVICE_LISTEN`
- `OR3_SERVICE_SECRET`

## Authentication

All routes require:

```http
Authorization: Bearer <signed-token>
```

The service refuses to start without `service.secret`.

Keep `service.listen` on loopback or private networking only.

## Endpoints

### `POST /internal/v1/turns`

Submits a foreground turn.

Behavior:

- returns Server-Sent Events when the request sends `Accept: text/event-stream`
- otherwise waits for completion and returns JSON

Request fields:

- canonical: `session_key`, `message`, optional `tool_policy`, `meta`, `profile_name`
- compatibility aliases also accepted: `intern_session_key`, `allowed_tools`, and the SDK camelCase forms (`sessionKey`, `internSessionKey`, `allowedTools`, `profileName`)

`tool_policy` uses the OR3 Net shape:

```json
{
  "mode": "allow_all | deny_all | allow_list | deny_list",
  "allowed_tools": ["read_file"],
  "blocked_tools": ["exec"]
}
```

### `POST /internal/v1/subagents`

Queues a background subagent job through the shared subagent manager.

Request fields:

- canonical: `parent_session_key`, `task`, optional `prompt_snapshot`, `tool_policy`, `timeout_seconds`, `meta`, `profile_name`, `channel`, `reply_to`
- compatibility aliases also accepted: `session_key`, `intern_session_key`, `allowed_tools`, `timeout`, and the SDK camelCase forms (`parentSessionKey`, `sessionKey`, `internSessionKey`, `promptSnapshot`, `allowedTools`, `timeoutSeconds`, `profileName`, `replyTo`)

### `GET /internal/v1/jobs/:jobId/stream`

Attaches to a live SSE stream for a turn or background job.

### `POST /internal/v1/jobs/:jobId/abort`

Requests cancellation of a running job when cancellation is possible.

## Operational guidance

- use strong random shared secrets
- do not expose the listener publicly without additional network controls
- run `or3-intern doctor` to catch weak secrets or risky bind addresses
- if `security.network` is enabled, remember that outbound API calls from tools/providers still have to satisfy that policy

## Related documentation

- [Security and hardening](security-and-hardening.md)
- [Agent runtime](agent-runtime.md)
- [CLI reference](cli-reference.md)

## Related code

- `cmd/or3-intern/service.go`
- `cmd/or3-intern/service_auth.go`
````

## File: internal/agent/service_runtime_context.go
````go
package agent

import (
	"context"
	"strings"

	"or3-intern/internal/channels"
	"or3-intern/internal/tools"
)

type conversationObserverContextKey struct{}
type streamingChannelContextKey struct{}
type toolRegistryContextKey struct{}

type ConversationObserver interface {
	OnTextDelta(ctx context.Context, text string)
	OnToolCall(ctx context.Context, name string, arguments string)
	OnToolResult(ctx context.Context, name string, result string, err error)
	OnCompletion(ctx context.Context, finalText string, streamed bool)
	OnError(ctx context.Context, err error)
}

func ContextWithConversationObserver(ctx context.Context, observer ConversationObserver) context.Context {
	if observer == nil {
		return ctx
	}
	return context.WithValue(ctx, conversationObserverContextKey{}, observer)
}

func conversationObserverFromContext(ctx context.Context) ConversationObserver {
	if ctx == nil {
		return nil
	}
	observer, _ := ctx.Value(conversationObserverContextKey{}).(ConversationObserver)
	return observer
}

func ContextWithStreamingChannel(ctx context.Context, streamer channels.StreamingChannel) context.Context {
	if streamer == nil {
		return ctx
	}
	return context.WithValue(ctx, streamingChannelContextKey{}, streamer)
}

func streamingChannelFromContext(ctx context.Context) channels.StreamingChannel {
	if ctx == nil {
		return nil
	}
	streamer, _ := ctx.Value(streamingChannelContextKey{}).(channels.StreamingChannel)
	return streamer
}

func ContextWithToolRegistry(ctx context.Context, reg *tools.Registry) context.Context {
	if reg == nil {
		return ctx
	}
	return context.WithValue(ctx, toolRegistryContextKey{}, reg)
}

func toolRegistryFromContext(ctx context.Context) *tools.Registry {
	if ctx == nil {
		return nil
	}
	reg, _ := ctx.Value(toolRegistryContextKey{}).(*tools.Registry)
	return reg
}

func toolRegistryWithAllowlist(base *tools.Registry, allowed []string, restrict bool) *tools.Registry {
	if base == nil {
		return tools.NewRegistry()
	}
	trimmed := make([]string, 0, len(allowed))
	for _, name := range allowed {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		trimmed = append(trimmed, name)
	}
	if !restrict {
		return base
	}
	if len(trimmed) == 0 {
		return tools.NewRegistry()
	}
	return base.CloneFiltered(trimmed)
}
````

## File: internal/agent/structured_autonomy.go
````go
package agent

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

func (r *Runtime) handleStructuredAutonomy(ctx context.Context, ev bus.Event, msgID int64) (bool, error) {
	if !isAutonomousEvent(ev.Type) || r.Tools == nil {
		return false, nil
	}
	env, ok := triggers.StructuredTasksFromMeta(ev.Meta)
	if !ok || len(env.Tasks) == 0 {
		return false, nil
	}
	replyTarget := deliveryTarget(ev)
	replyMeta := channels.ReplyMeta(ev.Meta)
	scopeKey := ev.SessionKey
	if r.DB != nil && strings.TrimSpace(ev.SessionKey) != "" {
		if resolved, err := r.DB.ResolveScopeKey(ctx, ev.SessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	toolCtx := tools.ContextWithSession(ctx, scopeKey)
	toolCtx = tools.ContextWithDelivery(toolCtx, ev.Channel, replyTarget)
	toolCtx = tools.ContextWithDeliveryMeta(toolCtx, replyMeta)
	toolCtx = r.contextWithTrustedToolAccess(toolCtx, ev)
	toolCtx = tools.ContextWithToolGuard(toolCtx, r.guardToolExecution)

	succeeded := 0
	failures := make([]string, 0)
	for index, task := range env.Tasks {
		toolName := strings.TrimSpace(task.Tool)
		params := cloneMap(task.Params)
		tool := r.Tools.Get(toolName)
		if tool == nil {
			failures = append(failures, fmt.Sprintf("#%d %s: tool not found", index+1, toolName))
			continue
		}
		if err := validateStructuredToolParams(tool, params); err != nil {
			failures = append(failures, fmt.Sprintf("#%d %s: %v", index+1, toolName, err))
			continue
		}
		out, err := r.Tools.ExecuteParams(toolCtx, toolName, params)
		payload := map[string]any{
			"tool":            toolName,
			"args":            params,
			"structured_task": true,
			"task_index":      index,
		}
		if err != nil {
			out = formatToolExecutionError(out, err)
			failures = append(failures, fmt.Sprintf("#%d %s: %v", index+1, toolName, err))
		} else {
			succeeded++
		}
		sendOut, preview, artifactID := r.boundTextResult(ctx, ev.SessionKey, out)
		if artifactID != "" {
			payload["artifact_id"] = artifactID
			payload["preview"] = preview
		}
		if _, appendErr := r.DB.AppendMessage(ctx, ev.SessionKey, "tool", sendOut, payload); appendErr != nil {
			return true, appendErr
		}
	}
	summary := structuredAutonomySummary(succeeded, len(env.Tasks), failures)
	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, summary, replyMeta, false, false)
	return true, nil
}

func structuredAutonomySummary(succeeded, total int, failures []string) string {
	base := fmt.Sprintf("structured autonomous tasks executed: %d/%d succeeded", succeeded, total)
	if len(failures) == 0 {
		return base
	}
	return base + "\nfailures:\n- " + strings.Join(failures, "\n- ")
}

func validateStructuredToolParams(tool tools.Tool, params map[string]any) error {
	if tool == nil {
		return fmt.Errorf("tool not found")
	}
	if params == nil {
		params = map[string]any{}
	}
	return validateStructuredValue(tool.Parameters(), params, "params")
}

func validateStructuredValue(schema map[string]any, value any, path string) error {
	typeName := strings.TrimSpace(fmt.Sprint(schema["type"]))
	switch typeName {
	case "", "object":
		mapped, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		for _, name := range requiredSchemaFields(schema["required"]) {
			if _, ok := mapped[name]; !ok {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
			for key := range mapped {
				if _, known := properties[key]; !known {
					return fmt.Errorf("%s.%s is not allowed", path, key)
				}
			}
		}
		for key, raw := range mapped {
			childSchema, ok := properties[key].(map[string]any)
			if !ok {
				continue
			}
			if err := validateStructuredValue(childSchema, raw, path+"."+key); err != nil {
				return err
			}
		}
		return nil
	case "array":
		items, ok := sliceItems(value)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if len(itemSchema) == 0 {
				continue
			}
			if err := validateStructuredValue(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
		return nil
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		return nil
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
		return nil
	case "integer":
		if !isIntegerValue(value) {
			return fmt.Errorf("%s must be an integer", path)
		}
		return nil
	case "number":
		if !isNumericValue(value) {
			return fmt.Errorf("%s must be a number", path)
		}
		return nil
	default:
		return nil
	}
}

func requiredSchemaFields(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		if typed, ok := raw.([]string); ok {
			return append([]string{}, typed...)
		}
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(fmt.Sprint(item))
		if name != "" && name != "<nil>" {
			out = append(out, name)
		}
	}
	return out
}

func sliceItems(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	if items, ok := value.([]any); ok {
		return items, true
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]any, rv.Len())
	for index := 0; index < rv.Len(); index++ {
		out[index] = rv.Index(index).Interface()
	}
	return out, true
}

func isNumericValue(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	default:
		return false
	}
}

func isIntegerValue(value any) bool {
	switch cast := value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float32:
		return float32(int64(cast)) == cast
	case float64:
		return float64(int64(cast)) == cast
	default:
		return false
	}
}
````

## File: internal/channels/cli/service.go
````go
package cli

import (
	"context"
	"fmt"

	"or3-intern/internal/bus"
)

type Service struct {
	Deliverer Deliverer
}

func (s Service) Name() string { return "cli" }

func (s Service) Start(ctx context.Context, eventBus *bus.Bus) error {
	_ = ctx
	_ = eventBus
	return nil
}

func (s Service) Stop(ctx context.Context) error {
	_ = ctx
	return nil
}

func (s Service) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	if len(meta) > 0 {
		if raw, ok := meta["media_paths"].([]string); ok && len(raw) > 0 {
			return fmt.Errorf("cli channel does not support media attachments")
		}
	}
	return s.Deliverer.Deliver(ctx, "cli", to, text)
}
````

## File: internal/channels/email/email.go
````go
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	xhtml "golang.org/x/net/html"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

const (
	defaultPollInterval = 30 * time.Second
	defaultNetTimeout   = 30 * time.Second
	maxFetchBatch       = 20
	maxProcessedKeys    = 4096
	dedupeMessageLimit  = 200
	lookupMessageLimit  = 50
)

type InboundMessage struct {
	UID       string
	From      string
	Subject   string
	MessageID string
	Date      time.Time
	Body      string
}

type OutboundMessage struct {
	To        string
	From      string
	Subject   string
	Text      string
	InReplyTo string
}

type threadState struct {
	Subject   string
	MessageID string
}

type Channel struct {
	Config config.EmailChannelConfig
	DB     *db.DB

	FetchMessages func(ctx context.Context) ([]InboundMessage, error)
	SendMail      func(ctx context.Context, outbound OutboundMessage) error

	mu             sync.Mutex
	running        bool
	cancel         context.CancelFunc
	threadBySender map[string]threadState
	processedKeys  map[string]struct{}
	processedOrder []string
}

func (c *Channel) Name() string { return "email" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if err := c.validate(); err != nil {
		return err
	}
	if eventBus == nil {
		return fmt.Errorf("event bus not configured")
	}
	if !c.Config.ConsentGranted {
		log.Printf("email channel disabled: consentGranted is false")
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	if c.threadBySender == nil {
		c.threadBySender = map[string]threadState{}
	}
	if c.processedKeys == nil {
		c.processedKeys = map[string]struct{}{}
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.running = true
	go c.pollLoop(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.cancel = nil
	c.running = false
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	recipient := normalizeAddress(to)
	if recipient == "" {
		recipient = normalizeAddress(c.Config.DefaultTo)
	}
	if recipient == "" {
		return fmt.Errorf("email recipient address required")
	}

	thread, _, err := c.lookupThread(ctx, recipient)
	if err != nil {
		return err
	}

	subject := c.subjectForDelivery(meta, thread.Subject)
	message := OutboundMessage{
		To:        recipient,
		From:      c.fromAddress(),
		Subject:   subject,
		Text:      text,
		InReplyTo: thread.MessageID,
	}
	return c.sendMail(ctx, message)
}

func (c *Channel) pollLoop(ctx context.Context, eventBus *bus.Bus) {
	interval := time.Duration(c.Config.PollIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = defaultPollInterval
	}
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	c.pollOnce(ctx, eventBus)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.pollOnce(ctx, eventBus)
		}
	}
}

func (c *Channel) pollOnce(ctx context.Context, eventBus *bus.Bus) {
	messages, err := c.fetchMessages(ctx)
	if err != nil {
		log.Printf("email polling error: %v", err)
		return
	}
	for _, inbound := range messages {
		sender := normalizeAddress(inbound.From)
		if sender == "" {
			continue
		}
		if c.alreadyProcessed(inbound.UID, inbound.MessageID) {
			continue
		}
		if !c.allowedSender(sender) {
			c.rememberProcessed(inbound.UID, inbound.MessageID)
			log.Printf("email sender ignored: %s", sender)
			continue
		}
		persisted, err := c.alreadyProcessedPersisted(ctx, sender, inbound.UID, inbound.MessageID)
		if err != nil {
			log.Printf("email dedupe lookup failed for %s: %v", sender, err)
		} else if persisted {
			c.rememberProcessed(inbound.UID, inbound.MessageID)
			continue
		}
		c.rememberProcessed(inbound.UID, inbound.MessageID)
		c.rememberThread(sender, inbound.Subject, inbound.MessageID)
		meta := map[string]any{
			"sender_email":       sender,
			"subject":            strings.TrimSpace(inbound.Subject),
			"message_id":         strings.TrimSpace(inbound.MessageID),
			"uid":                strings.TrimSpace(inbound.UID),
			"auto_reply_enabled": c.Config.AutoReplyEnabled,
		}
		if !inbound.Date.IsZero() {
			meta["date"] = inbound.Date.Format(time.RFC3339)
		}
		ok := eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: "email:" + sender,
			Channel:    "email",
			From:       sender,
			Message:    formatInboundMessage(sender, inbound.Subject, inbound.Date, inbound.Body),
			Meta:       meta,
		})
		if !ok {
			log.Printf("email event dropped: queue full for %s", sender)
		}
	}
}

func (c *Channel) validate() error {
	if !c.Config.Enabled {
		return nil
	}
	if !c.Config.OpenAccess && !hasNonEmpty(c.Config.AllowedSenders) {
		return fmt.Errorf("email enabled: set allowedSenders or openAccess=true")
	}
	if strings.TrimSpace(c.Config.IMAPHost) == "" || strings.TrimSpace(c.Config.IMAPUsername) == "" || strings.TrimSpace(c.Config.IMAPPassword) == "" {
		return fmt.Errorf("email requires IMAP host, username, and password")
	}
	if strings.TrimSpace(c.Config.SMTPHost) == "" || strings.TrimSpace(c.Config.SMTPUsername) == "" || strings.TrimSpace(c.Config.SMTPPassword) == "" {
		return fmt.Errorf("email requires SMTP host, username, and password")
	}
	return nil
}

func (c *Channel) fetchMessages(ctx context.Context) ([]InboundMessage, error) {
	if c.FetchMessages != nil {
		return c.FetchMessages(ctx)
	}
	return c.fetchViaIMAP(ctx)
}

func (c *Channel) sendMail(ctx context.Context, outbound OutboundMessage) error {
	if c.SendMail != nil {
		return c.SendMail(ctx, outbound)
	}
	return c.sendViaSMTP(ctx, outbound)
}

func (c *Channel) fetchViaIMAP(ctx context.Context) ([]InboundMessage, error) {
	client, stopWatch, err := c.dialIMAP(ctx)
	if err != nil {
		return nil, err
	}
	defer stopWatch()
	defer client.Close()
	if err := client.Login(c.Config.IMAPUsername, c.Config.IMAPPassword).Wait(); err != nil {
		return nil, fmt.Errorf("imap login: %w", err)
	}
	defer func() {
		if err := client.Logout().Wait(); err != nil {
			log.Printf("email logout error: %v", err)
		}
	}()

	mailbox := strings.TrimSpace(c.Config.IMAPMailbox)
	if mailbox == "" {
		mailbox = "INBOX"
	}
	if _, err := client.Select(mailbox, nil).Wait(); err != nil {
		return nil, fmt.Errorf("imap select %s: %w", mailbox, err)
	}

	criteria := &imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}
	searchData, err := client.UIDSearch(criteria, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap search: %w", err)
	}
	uids := searchData.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	if len(uids) > maxFetchBatch {
		uids = uids[len(uids)-maxFetchBatch:]
	}

	var uidSet imap.UIDSet
	uidSet.AddNum(uids...)
	bodySection := &imap.FetchItemBodySection{Peek: true}
	messages, err := client.Fetch(uidSet, &imap.FetchOptions{
		UID:         true,
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap fetch: %w", err)
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].UID < messages[j].UID })

	out := make([]InboundMessage, 0, len(messages))
	markedUIDs := make([]imap.UID, 0, len(messages))
	for _, message := range messages {
		raw := message.FindBodySection(bodySection)
		if len(raw) == 0 {
			continue
		}
		parsed, err := parseRawEmail(raw, c.Config.MaxBodyChars)
		if err != nil {
			log.Printf("email parse error for uid=%d: %v", message.UID, err)
			continue
		}
		parsed.UID = fmt.Sprintf("%d", message.UID)
		out = append(out, parsed)
		if c.Config.MarkSeen {
			markedUIDs = append(markedUIDs, message.UID)
		}
	}
	if c.Config.MarkSeen && len(markedUIDs) > 0 {
		var seenSet imap.UIDSet
		seenSet.AddNum(markedUIDs...)
		storeFlags := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}
		if err := client.Store(seenSet, storeFlags, nil).Close(); err != nil {
			log.Printf("email mark seen error: %v", err)
		}
	}
	return out, nil
}

func (c *Channel) dialIMAP(ctx context.Context) (*imapclient.Client, func(), error) {
	address := net.JoinHostPort(strings.TrimSpace(c.Config.IMAPHost), fmt.Sprintf("%d", c.Config.IMAPPort))
	dialer := &net.Dialer{Timeout: defaultNetTimeout}
	var conn net.Conn
	var err error
	if c.Config.IMAPUseSSL {
		baseConn, dialErr := dialer.DialContext(ctx, "tcp", address)
		if dialErr != nil {
			return nil, func() {}, dialErr
		}
		tlsConn := tls.Client(baseConn, &tls.Config{ServerName: strings.TrimSpace(c.Config.IMAPHost)})
		if deadline, ok := connectionDeadline(ctx); ok {
			_ = tlsConn.SetDeadline(deadline)
		}
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			_ = baseConn.Close()
			return nil, func() {}, fmt.Errorf("imap tls handshake: %w", err)
		}
		conn = tlsConn
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, func() {}, err
		}
	}
	if deadline, ok := connectionDeadline(ctx); ok {
		_ = conn.SetDeadline(deadline)
	}
	stopWatch := watchConnContext(ctx, conn)
	client := imapclient.New(conn, nil)
	if err := client.WaitGreeting(); err != nil {
		stopWatch()
		_ = client.Close()
		return nil, func() {}, fmt.Errorf("imap greeting: %w", err)
	}
	return client, stopWatch, nil
}

func (c *Channel) sendViaSMTP(ctx context.Context, outbound OutboundMessage) error {
	if err := c.validateSMTPAuthTransport(); err != nil {
		return err
	}
	messageBytes, err := buildOutboundMessage(outbound)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(strings.TrimSpace(c.Config.SMTPHost), fmt.Sprintf("%d", c.Config.SMTPPort))
	dialer := &net.Dialer{Timeout: 30 * time.Second}

	var conn net.Conn
	if c.Config.SMTPUseSSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: strings.TrimSpace(c.Config.SMTPHost)})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, strings.TrimSpace(c.Config.SMTPHost))
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if c.Config.SMTPUseTLS && !c.Config.SMTPUseSSL {
		if err := client.StartTLS(&tls.Config{ServerName: strings.TrimSpace(c.Config.SMTPHost)}); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if strings.TrimSpace(c.Config.SMTPUsername) != "" {
		auth := smtp.PlainAuth("", c.Config.SMTPUsername, c.Config.SMTPPassword, strings.TrimSpace(c.Config.SMTPHost))
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := client.Mail(outbound.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(outbound.To); err != nil {
		return fmt.Errorf("smtp rcpt to: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := writer.Write(messageBytes); err != nil {
		_ = writer.Close()
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("smtp finalize: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("smtp quit: %w", err)
	}
	return nil
}

func buildOutboundMessage(outbound OutboundMessage) ([]byte, error) {
	from := strings.TrimSpace(outbound.From)
	to := normalizeAddress(outbound.To)
	if from == "" || to == "" {
		return nil, fmt.Errorf("email from and to addresses are required")
	}
	subject := strings.TrimSpace(outbound.Subject)
	if subject == "" {
		subject = "or3-intern reply"
	}
	body := strings.ReplaceAll(outbound.Text, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")

	var buf bytes.Buffer
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		fmt.Sprintf("Message-ID: <%d.%s>", time.Now().UnixNano(), strings.ReplaceAll(strings.SplitN(from, "@", 2)[0], " ", "-")),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: quoted-printable",
	}
	if strings.TrimSpace(outbound.InReplyTo) != "" {
		headers = append(headers, "In-Reply-To: "+strings.TrimSpace(outbound.InReplyTo))
		headers = append(headers, "References: "+strings.TrimSpace(outbound.InReplyTo))
	}
	for _, headerLine := range headers {
		buf.WriteString(headerLine)
		buf.WriteString("\r\n")
	}
	buf.WriteString("\r\n")
	quoted := quotedprintable.NewWriter(&buf)
	if _, err := quoted.Write([]byte(body)); err != nil {
		return nil, err
	}
	if err := quoted.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func parseRawEmail(raw []byte, maxBodyChars int) (InboundMessage, error) {
	message, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return InboundMessage{}, err
	}
	parsed := InboundMessage{
		From:      normalizeAddress(message.Header.Get("From")),
		Subject:   decodeHeaderValue(message.Header.Get("Subject")),
		MessageID: strings.TrimSpace(message.Header.Get("Message-ID")),
	}
	if dateValue := strings.TrimSpace(message.Header.Get("Date")); dateValue != "" {
		if parsedDate, err := mail.ParseDate(dateValue); err == nil {
			parsed.Date = parsedDate
		}
	}
	parsed.Body = extractBodyText(textproto.MIMEHeader(message.Header), message.Body, maxBodyChars)
	return parsed, nil
}

func extractBodyText(header textproto.MIMEHeader, body io.Reader, maxBodyChars int) string {
	plain, htmlBodies := extractEntityBodies(header, body, maxBodyChars)
	if len(plain) > 0 {
		return truncateText(strings.Join(plain, "\n\n"), maxBodyChars)
	}
	if len(htmlBodies) > 0 {
		return truncateText(strings.Join(htmlBodies, "\n\n"), maxBodyChars)
	}
	return ""
}

func extractEntityBodies(header textproto.MIMEHeader, body io.Reader, maxBodyChars int) ([]string, []string) {
	mediaType, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	disposition, _, _ := mime.ParseMediaType(header.Get("Content-Disposition"))
	if strings.EqualFold(disposition, "attachment") {
		return nil, nil
	}

	if strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil, nil
		}
		reader := multipart.NewReader(decodeTransferEncoding(body, header.Get("Content-Transfer-Encoding")), boundary)
		plainParts := []string{}
		htmlParts := []string{}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				break
			}
			childPlain, childHTML := extractEntityBodies(part.Header, part, maxBodyChars)
			plainParts = append(plainParts, childPlain...)
			htmlParts = append(htmlParts, childHTML...)
		}
		return plainParts, htmlParts
	}

	decodedBody, err := io.ReadAll(io.LimitReader(decodeTransferEncoding(body, header.Get("Content-Transfer-Encoding")), int64(maxReadBytes(maxBodyChars))))
	if err != nil {
		return nil, nil
	}
	text := strings.TrimSpace(string(decodedBody))
	if text == "" {
		return nil, nil
	}
	switch strings.ToLower(mediaType) {
	case "text/plain":
		return []string{normalizeText(text)}, nil
	case "text/html":
		return nil, []string{normalizeText(htmlToText(text))}
	default:
		return nil, nil
	}
}

func decodeTransferEncoding(body io.Reader, encoding string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	default:
		return body
	}
}

func maxReadBytes(maxBodyChars int) int {
	if maxBodyChars <= 0 {
		return 8192
	}
	return maxBodyChars*4 + 1024
}

func htmlToText(input string) string {
	tokenizer := xhtml.NewTokenizer(strings.NewReader(input))
	segments := make([]string, 0, 16)
	appendText := func(text string) {
		text = strings.Join(strings.Fields(html.UnescapeString(text)), " ")
		if text == "" {
			return
		}
		if len(segments) > 0 {
			last := segments[len(segments)-1]
			if !strings.HasSuffix(last, "\n") && last != " " {
				segments = append(segments, " ")
			}
		}
		segments = append(segments, text)
	}
	appendBreak := func(double bool) {
		if len(segments) == 0 {
			return
		}
		want := "\n"
		if double {
			want = "\n\n"
		}
		last := segments[len(segments)-1]
		if strings.HasSuffix(last, "\n\n") || (!double && strings.HasSuffix(last, "\n")) {
			return
		}
		segments = append(segments, want)
	}
	for {
		tokenType := tokenizer.Next()
		switch tokenType {
		case xhtml.ErrorToken:
			return html.UnescapeString(strings.Join(segments, ""))
		case xhtml.TextToken:
			appendText(string(tokenizer.Text()))
		case xhtml.StartTagToken, xhtml.EndTagToken, xhtml.SelfClosingTagToken:
			name, _ := tokenizer.TagName()
			switch strings.ToLower(string(name)) {
			case "br":
				appendBreak(false)
			case "p", "div", "section", "article", "header", "footer", "aside", "li", "tr",
				"h1", "h2", "h3", "h4", "h5", "h6":
				appendBreak(true)
			}
		}
	}
}

func normalizeText(input string) string {
	text := strings.ReplaceAll(input, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	blankCount := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			blankCount++
			if blankCount > 1 {
				continue
			}
			cleaned = append(cleaned, "")
			continue
		}
		blankCount = 0
		cleaned = append(cleaned, trimmed)
	}
	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func truncateText(text string, maxBodyChars int) string {
	text = strings.TrimSpace(text)
	if maxBodyChars > 0 && len(text) > maxBodyChars {
		return strings.TrimSpace(text[:maxBodyChars]) + "…"
	}
	return text
}

func formatInboundMessage(sender, subject string, sentAt time.Time, body string) string {
	lines := []string{"From: " + sender}
	if strings.TrimSpace(subject) != "" {
		lines = append(lines, "Subject: "+strings.TrimSpace(subject))
	}
	if !sentAt.IsZero() {
		lines = append(lines, "Date: "+sentAt.Format(time.RFC1123Z))
	}
	if strings.TrimSpace(body) != "" {
		lines = append(lines, "", strings.TrimSpace(body))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func (c *Channel) allowedSender(sender string) bool {
	if c.Config.OpenAccess {
		return true
	}
	for _, allowed := range c.Config.AllowedSenders {
		if normalizeAddress(allowed) == sender {
			return true
		}
	}
	return false
}

func normalizeAddress(value string) string {
	parsed, err := mail.ParseAddress(strings.TrimSpace(value))
	if err == nil {
		return strings.ToLower(strings.TrimSpace(parsed.Address))
	}
	if strings.Contains(value, "<") && strings.Contains(value, ">") {
		start := strings.LastIndex(value, "<")
		end := strings.LastIndex(value, ">")
		if start >= 0 && end > start {
			value = value[start+1 : end]
		}
	}
	return strings.ToLower(strings.TrimSpace(value))
}

func decodeHeaderValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	decoder := new(mime.WordDecoder)
	decoded, err := decoder.DecodeHeader(value)
	if err != nil {
		return value
	}
	return strings.TrimSpace(decoded)
}

func (c *Channel) rememberProcessed(uid, messageID string) {
	keys := []string{}
	if strings.TrimSpace(uid) != "" {
		keys = append(keys, "uid:"+strings.TrimSpace(uid))
	}
	if strings.TrimSpace(messageID) != "" {
		keys = append(keys, "msgid:"+strings.TrimSpace(messageID))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.processedKeys == nil {
		c.processedKeys = map[string]struct{}{}
	}
	for _, key := range keys {
		if _, exists := c.processedKeys[key]; exists {
			continue
		}
		c.processedKeys[key] = struct{}{}
		c.processedOrder = append(c.processedOrder, key)
	}
	for len(c.processedOrder) > maxProcessedKeys {
		oldest := c.processedOrder[0]
		c.processedOrder = c.processedOrder[1:]
		delete(c.processedKeys, oldest)
	}
}

func (c *Channel) alreadyProcessed(uid, messageID string) bool {
	keys := []string{}
	if strings.TrimSpace(uid) != "" {
		keys = append(keys, "uid:"+strings.TrimSpace(uid))
	}
	if strings.TrimSpace(messageID) != "" {
		keys = append(keys, "msgid:"+strings.TrimSpace(messageID))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		if _, exists := c.processedKeys[key]; exists {
			return true
		}
	}
	return false
}

func (c *Channel) alreadyProcessedPersisted(ctx context.Context, sender, uid, messageID string) (bool, error) {
	if c.DB == nil {
		return false, nil
	}
	uid = strings.TrimSpace(uid)
	messageID = strings.TrimSpace(messageID)
	if uid == "" && messageID == "" {
		return false, nil
	}
	messages, err := c.DB.GetLastMessages(ctx, "email:"+sender, dedupeMessageLimit)
	if err != nil {
		return false, err
	}
	for _, message := range messages {
		if message.Role != "user" || strings.TrimSpace(message.PayloadJSON) == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(message.PayloadJSON), &payload); err != nil {
			continue
		}
		meta, _ := payload["meta"].(map[string]any)
		if len(meta) == 0 {
			continue
		}
		storedUID := strings.TrimSpace(fmt.Sprint(meta["uid"]))
		storedMessageID := strings.TrimSpace(fmt.Sprint(meta["message_id"]))
		if uid != "" && storedUID == uid {
			return true, nil
		}
		if messageID != "" && storedMessageID == messageID {
			return true, nil
		}
	}
	return false, nil
}

func (c *Channel) rememberThread(sender, subject, messageID string) {
	sender = normalizeAddress(sender)
	if sender == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.threadBySender == nil {
		c.threadBySender = map[string]threadState{}
	}
	state := c.threadBySender[sender]
	if strings.TrimSpace(subject) != "" {
		state.Subject = strings.TrimSpace(subject)
	}
	if strings.TrimSpace(messageID) != "" {
		state.MessageID = strings.TrimSpace(messageID)
	}
	c.threadBySender[sender] = state
}

func (c *Channel) lookupThread(ctx context.Context, recipient string) (threadState, bool, error) {
	recipient = normalizeAddress(recipient)
	c.mu.Lock()
	state, ok := c.threadBySender[recipient]
	c.mu.Unlock()
	if ok && (state.Subject != "" || state.MessageID != "") {
		return state, true, nil
	}
	if c.DB == nil {
		return threadState{}, false, nil
	}
	messages, err := c.DB.GetLastMessages(ctx, "email:"+recipient, lookupMessageLimit)
	if err != nil {
		return threadState{}, false, err
	}
	for idx := len(messages) - 1; idx >= 0; idx-- {
		message := messages[idx]
		var payload map[string]any
		if err := json.Unmarshal([]byte(message.PayloadJSON), &payload); err != nil {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(payload["channel"])) != "email" {
			continue
		}
		meta, _ := payload["meta"].(map[string]any)
		if len(meta) == 0 {
			continue
		}
		state = threadState{
			Subject:   strings.TrimSpace(fmt.Sprint(meta["subject"])),
			MessageID: strings.TrimSpace(fmt.Sprint(meta["message_id"])),
		}
		if state.Subject != "" || state.MessageID != "" {
			c.rememberThread(recipient, state.Subject, state.MessageID)
			return state, true, nil
		}
	}
	return threadState{}, false, nil
}

func (c *Channel) subjectForDelivery(meta map[string]any, base string) string {
	if override := strings.TrimSpace(fmt.Sprint(meta["subject"])); override != "" && override != "<nil>" {
		return override
	}
	base = strings.TrimSpace(base)
	if base == "" {
		return "or3-intern reply"
	}
	lower := strings.ToLower(base)
	if strings.HasPrefix(lower, "re:") {
		return base
	}
	prefix := strings.TrimSpace(c.Config.SubjectPrefix)
	if prefix == "" {
		prefix = "Re:"
	}
	if !strings.HasSuffix(prefix, ":") && !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}
	if !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}
	return prefix + base
}

func (c *Channel) fromAddress() string {
	if value := normalizeAddress(c.Config.FromAddress); value != "" {
		return value
	}
	if value := normalizeAddress(c.Config.SMTPUsername); value != "" {
		return value
	}
	return normalizeAddress(c.Config.IMAPUsername)
}

func truthyMeta(meta map[string]any, key string) bool {
	if len(meta) == 0 {
		return false
	}
	value, exists := meta[key]
	if !exists {
		return false
	}
	switch cast := value.(type) {
	case bool:
		return cast
	default:
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(value)), "true")
	}
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func (c *Channel) validateSMTPAuthTransport() error {
	if strings.TrimSpace(c.Config.SMTPUsername) == "" {
		return nil
	}
	if c.Config.SMTPUseSSL || c.Config.SMTPUseTLS {
		return nil
	}
	return fmt.Errorf("smtp auth requires TLS or SSL")
}

func connectionDeadline(ctx context.Context) (time.Time, bool) {
	if ctx == nil {
		return time.Now().Add(defaultNetTimeout), true
	}
	if deadline, ok := ctx.Deadline(); ok {
		return deadline, true
	}
	return time.Now().Add(defaultNetTimeout), true
}

func watchConnContext(ctx context.Context, conn net.Conn) func() {
	if ctx == nil || conn == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}
````

## File: internal/channels/channels.go
````go
package channels

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"or3-intern/internal/bus"
)

const (
	MetaMediaPaths       = "media_paths"
	MetaThreadTS         = "thread_ts"
	MetaReplyToMessageID = "reply_to_message_id"
	MetaMessageReference = "message_reference"
)

type Channel interface {
	Name() string
	Start(ctx context.Context, eventBus *bus.Bus) error
	Stop(ctx context.Context) error
	Deliver(ctx context.Context, to, text string, meta map[string]any) error
}

type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
	started  map[string]bool
}

func NewManager() *Manager {
	return &Manager{channels: map[string]Channel{}, started: map[string]bool{}}
}

func (m *Manager) Register(ch Channel) error {
	if ch == nil {
		return errors.New("nil channel")
	}
	name := strings.TrimSpace(strings.ToLower(ch.Name()))
	if name == "" {
		return errors.New("channel name required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.channels[name]; exists {
		return fmt.Errorf("channel already registered: %s", name)
	}
	m.channels[name] = ch
	return nil
}

func (m *Manager) Names() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.channels))
	for name := range m.channels {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) StartAll(ctx context.Context, eventBus *bus.Bus) error {
	for _, name := range m.Names() {
		if err := m.Start(ctx, name, eventBus); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) Start(ctx context.Context, name string, eventBus *bus.Bus) error {
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.started[name] {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()
	if err := ch.Start(ctx, eventBus); err != nil {
		return err
	}
	m.mu.Lock()
	m.started[name] = true
	m.mu.Unlock()
	return nil
}

func (m *Manager) StopAll(ctx context.Context) error {
	var errs []string
	for _, name := range m.Names() {
		if err := m.Stop(ctx, name); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (m *Manager) Stop(ctx context.Context, name string) error {
	ch, err := m.get(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	started := m.started[name]
	m.mu.Unlock()
	if !started {
		return nil
	}
	if err := ch.Stop(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.started, name)
	m.mu.Unlock()
	return nil
}

func (m *Manager) Deliver(ctx context.Context, channel, to, text string) error {
	return m.DeliverWithMeta(ctx, channel, to, text, nil)
}

func (m *Manager) DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error {
	if strings.TrimSpace(channel) == "" {
		channel = "cli"
	}
	ch, err := m.get(channel)
	if err != nil {
		return err
	}
	return ch.Deliver(ctx, to, text, meta)
}

func (m *Manager) get(name string) (Channel, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	m.mu.RLock()
	defer m.mu.RUnlock()
	ch := m.channels[name]
	if ch == nil {
		return nil, fmt.Errorf("channel not found: %s", name)
	}
	return ch, nil
}

func CloneMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := make(map[string]any, len(meta))
	for k, v := range meta {
		out[k] = v
	}
	return out
}

func ReplyMeta(meta map[string]any) map[string]any {
	if len(meta) == 0 {
		return nil
	}
	out := map[string]any{}
	for _, key := range []string{MetaThreadTS, MetaReplyToMessageID, MetaMessageReference} {
		if value, ok := meta[key]; ok && hasMeaningfulMetaValue(value) {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func hasMeaningfulMetaValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case int:
		return v > 0
	case int8:
		return v > 0
	case int16:
		return v > 0
	case int32:
		return v > 0
	case int64:
		return v > 0
	case uint:
		return v > 0
	case uint8:
		return v > 0
	case uint16:
		return v > 0
	case uint32:
		return v > 0
	case uint64:
		return v > 0
	default:
		text := strings.TrimSpace(fmt.Sprint(value))
		return text != "" && text != "<nil>"
	}
}
````

## File: internal/db/security.go
````go
package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type SecretRecord struct {
	Name       string
	Ciphertext []byte
	Nonce      []byte
	Version    int
	KeyVersion string
	UpdatedAt  int64
}

type AuditEvent struct {
	ID          int64
	EventType   string
	SessionKey  string
	Actor       string
	PayloadJSON string
	PrevHash    []byte
	RecordHash  []byte
	CreatedAt   int64
}

type AuditEventInput struct {
	EventType  string
	SessionKey string
	Actor      string
	Payload    any
}

func (d *DB) PutSecret(ctx context.Context, name string, ciphertext, nonce []byte, version int, keyVersion string) error {
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO secrets(name, ciphertext, nonce, version, key_version, updated_at) VALUES(?,?,?,?,?,?)
		 ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext, nonce=excluded.nonce, version=excluded.version, key_version=excluded.key_version, updated_at=excluded.updated_at`,
		strings.TrimSpace(name), ciphertext, nonce, version, strings.TrimSpace(keyVersion), NowMS())
	return err
}

func (d *DB) GetSecret(ctx context.Context, name string) (SecretRecord, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT name, ciphertext, nonce, version, key_version, updated_at FROM secrets WHERE name=?`,
		strings.TrimSpace(name))
	var record SecretRecord
	if err := row.Scan(&record.Name, &record.Ciphertext, &record.Nonce, &record.Version, &record.KeyVersion, &record.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return SecretRecord{}, false, nil
		}
		return SecretRecord{}, false, err
	}
	return record, true, nil
}

func (d *DB) DeleteSecret(ctx context.Context, name string) error {
	_, err := d.SQL.ExecContext(ctx, `DELETE FROM secrets WHERE name=?`, strings.TrimSpace(name))
	return err
}

func (d *DB) ListSecretNames(ctx context.Context) ([]string, error) {
	rows, err := d.SQL.QueryContext(ctx, `SELECT name FROM secrets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (d *DB) AppendAuditEvent(ctx context.Context, input AuditEventInput, key []byte) error {
	d.auditMu.Lock()
	defer d.auditMu.Unlock()

	payloadBytes, err := json.Marshal(input.Payload)
	if err != nil {
		return err
	}
	payload := truncateAuditPayload(string(payloadBytes))
	conn, err := d.SQL.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(context.Background(), `ROLLBACK`)
		}
	}()

	prevHash := []byte{}
	row := conn.QueryRowContext(ctx, `SELECT record_hash FROM audit_events ORDER BY id DESC LIMIT 1`)
	var previous []byte
	if err := row.Scan(&previous); err == nil {
		prevHash = append([]byte{}, previous...)
	} else if err != nil && err != sql.ErrNoRows {
		return err
	}
	createdAt := NowMS()
	recordHash := computeAuditHash(key, input.EventType, input.SessionKey, input.Actor, payload, prevHash, createdAt)
	if _, err = conn.ExecContext(ctx,
		`INSERT INTO audit_events(event_type, session_key, actor, payload_json, prev_hash, record_hash, created_at) VALUES(?,?,?,?,?,?,?)`,
		strings.TrimSpace(input.EventType), strings.TrimSpace(input.SessionKey), strings.TrimSpace(input.Actor), payload, prevHash, recordHash, createdAt); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return err
	}
	committed = true
	return nil
}

func (d *DB) VerifyAuditChain(ctx context.Context, key []byte) error {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, event_type, session_key, actor, payload_json, prev_hash, record_hash, created_at FROM audit_events ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var prev []byte
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.EventType, &event.SessionKey, &event.Actor, &event.PayloadJSON, &event.PrevHash, &event.RecordHash, &event.CreatedAt); err != nil {
			return err
		}
		if !hmac.Equal(event.PrevHash, prev) {
			return fmt.Errorf("audit chain mismatch at id=%d", event.ID)
		}
		expected := computeAuditHash(key, event.EventType, event.SessionKey, event.Actor, event.PayloadJSON, event.PrevHash, event.CreatedAt)
		if !hmac.Equal(expected, event.RecordHash) {
			return fmt.Errorf("audit hash mismatch at id=%d", event.ID)
		}
		prev = append([]byte{}, event.RecordHash...)
	}
	return rows.Err()
}

func computeAuditHash(key []byte, eventType, sessionKey, actor, payload string, prevHash []byte, createdAt int64) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(strings.TrimSpace(eventType)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(sessionKey)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(strings.TrimSpace(actor)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(payload))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(prevHash)
	_, _ = mac.Write([]byte(fmt.Sprint(createdAt)))
	return mac.Sum(nil)
}

func truncateAuditPayload(payload string) string {
	payload = strings.TrimSpace(payload)
	if len(payload) <= 2048 {
		return payload
	}
	return payload[:2048] + "...[truncated]"
}
````

## File: internal/security/network.go
````go
package security

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

var lookupIPAddr = func(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

type HostPolicy struct {
	Enabled       bool
	DefaultDeny   bool
	AllowedHosts  []string
	AllowLoopback bool
	AllowPrivate  bool
}

func (p HostPolicy) EnabledPolicy() bool {
	return p.Enabled || p.DefaultDeny || len(p.AllowedHosts) > 0
}

func (p HostPolicy) ValidateURL(ctx context.Context, target *url.URL) error {
	if target == nil {
		return fmt.Errorf("invalid url")
	}
	hostname := strings.TrimSpace(strings.ToLower(target.Hostname()))
	if hostname == "" {
		return fmt.Errorf("missing host")
	}
	return p.ValidateHost(ctx, hostname)
}

func (p HostPolicy) ValidateEndpoint(ctx context.Context, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return fmt.Errorf("invalid endpoint: %w", err)
		}
		return p.ValidateURL(ctx, u)
	}
	host, _, err := net.SplitHostPort(raw)
	if err == nil {
		return p.ValidateHost(ctx, strings.Trim(host, "[]"))
	}
	return p.ValidateHost(ctx, raw)
}

func (p HostPolicy) ValidateHost(ctx context.Context, hostname string) error {
	_, err := p.resolveHost(ctx, hostname)
	return err
}

func (p HostPolicy) resolveHost(ctx context.Context, hostname string) (resolvedHostPlan, error) {
	hostname = strings.Trim(strings.ToLower(strings.TrimSpace(hostname)), "[]")
	if hostname == "" {
		return resolvedHostPlan{}, fmt.Errorf("missing host")
	}
	if ip, err := netip.ParseAddr(hostname); err == nil {
		ip = ip.Unmap()
		if err := p.validateAddr(ip); err != nil {
			return resolvedHostPlan{}, err
		}
		return resolvedHostPlan{hostname: hostname, addrs: []netip.Addr{ip}}, nil
	}
	if p.EnabledPolicy() && !hostAllowed(hostname, p.AllowedHosts) && p.DefaultDeny {
		return resolvedHostPlan{}, fmt.Errorf("host denied by policy: %s", hostname)
	}
	addrs, err := lookupIPAddr(ctx, hostname)
	if err != nil {
		return resolvedHostPlan{}, err
	}
	if len(addrs) == 0 {
		return resolvedHostPlan{}, fmt.Errorf("host did not resolve")
	}
	approved := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok {
			return resolvedHostPlan{}, fmt.Errorf("host resolution failed")
		}
		ip = ip.Unmap()
		if err := p.validateAddr(ip); err != nil {
			return resolvedHostPlan{}, err
		}
		approved = append(approved, ip)
	}
	return resolvedHostPlan{hostname: hostname, addrs: approved}, nil
}

func (p HostPolicy) validateAddr(addr netip.Addr) error {
	if !addr.IsValid() {
		return fmt.Errorf("invalid host address")
	}
	if !p.AllowLoopback && addr.IsLoopback() {
		return fmt.Errorf("host denied by policy: loopback")
	}
	if !p.AllowPrivate && (addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified()) {
		return fmt.Errorf("host denied by policy: private address")
	}
	if addr.String() == "169.254.169.254" {
		return fmt.Errorf("host denied by policy: metadata endpoint")
	}
	return nil
}

func hostAllowed(host string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	host = strings.ToLower(strings.TrimSpace(host))
	for _, pattern := range patterns {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if pattern == host {
			return true
		}
		if strings.HasPrefix(pattern, "*.") {
			suffix := strings.TrimPrefix(pattern, "*")
			if strings.HasSuffix(host, suffix) && len(host) > len(suffix) {
				return true
			}
		}
	}
	return false
}

func WrapHTTPClient(client *http.Client, policy HostPolicy) *http.Client {
	if client == nil {
		client = &http.Client{}
	}
	cloned := *client
	prevCheckRedirect := cloned.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := policy.ValidateURL(req.Context(), req.URL); err != nil {
			return err
		}
		if prevCheckRedirect != nil {
			return prevCheckRedirect(req, via)
		}
		return nil
	}
	base := cloned.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	wrappedBase := wrapTransportWithPolicy(base, policy)
	cloned.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		plan, err := policy.resolveHost(req.Context(), req.URL.Hostname())
		if err != nil {
			return nil, err
		}
		return wrappedBase.RoundTrip(req.Clone(withResolvedHostPlan(req.Context(), plan)))
	})
	return &cloned
}

type resolvedHostPlan struct {
	hostname string
	addrs    []netip.Addr
}

type resolvedHostPlanKey struct{}

func withResolvedHostPlan(ctx context.Context, plan resolvedHostPlan) context.Context {
	return context.WithValue(ctx, resolvedHostPlanKey{}, plan)
}

func resolvedHostPlanFromContext(ctx context.Context, host string) (resolvedHostPlan, bool) {
	plan, ok := ctx.Value(resolvedHostPlanKey{}).(resolvedHostPlan)
	if !ok {
		return resolvedHostPlan{}, false
	}
	if plan.hostname != strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]") {
		return resolvedHostPlan{}, false
	}
	return plan, len(plan.addrs) > 0
}

func wrapTransportWithPolicy(base http.RoundTripper, policy HostPolicy) http.RoundTripper {
	transport, ok := base.(*http.Transport)
	if !ok {
		return roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if err := policy.ValidateURL(req.Context(), req.URL); err != nil {
				return nil, err
			}
			return base.RoundTrip(req)
		})
	}
	cloned := transport.Clone()
	baseDial := cloned.DialContext
	if baseDial == nil {
		baseDial = (&net.Dialer{}).DialContext
	}
	cloned.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return dialResolvedHost(ctx, network, addr, baseDial)
	}
	if cloned.DialTLSContext != nil {
		baseDialTLS := cloned.DialTLSContext
		cloned.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialResolvedHost(ctx, network, addr, baseDialTLS)
		}
	}
	return cloned
}

func dialResolvedHost(ctx context.Context, network, addr string, dial func(context.Context, string, string) (net.Conn, error)) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = ""
	}
	plan, ok := resolvedHostPlanFromContext(ctx, host)
	if !ok {
		return dial(ctx, network, addr)
	}
	var lastErr error
	for _, ip := range plan.addrs {
		target := ip.String()
		if port != "" {
			target = net.JoinHostPort(target, port)
		}
		conn, err := dial(ctx, network, target)
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("host did not resolve")
	}
	return nil, lastErr
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}
````

## File: internal/tools/env.go
````go
package tools

import (
	"os"
	"strings"
)

var defaultChildEnvAllowlist = []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"}

func EffectiveChildEnvAllowlist(allowlist []string) []string {
	cleaned := make([]string, 0, len(allowlist))
	seen := map[string]struct{}{}
	for _, name := range allowlist {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		cleaned = append(cleaned, name)
	}
	if len(cleaned) > 0 {
		return cleaned
	}
	return append([]string{}, defaultChildEnvAllowlist...)
}

func BuildChildEnv(base []string, allowlist []string, overlay map[string]string, pathAppend string) []string {
	values := map[string]string{}
	order := make([]string, 0, len(base)+len(overlay)+1)
	allowed := map[string]struct{}{}
	for _, name := range EffectiveChildEnvAllowlist(allowlist) {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}
	for _, raw := range base {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	for key, value := range overlay {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := values[key]; !exists {
			order = append(order, key)
		}
		values[key] = value
	}
	if pathAppend != "" {
		pathValue := values["PATH"]
		if pathValue == "" {
			pathValue = os.Getenv("PATH")
		}
		values["PATH"] = pathValue + string(os.PathListSeparator) + pathAppend
		seen := false
		for _, key := range order {
			if key == "PATH" {
				seen = true
				break
			}
		}
		if !seen {
			order = append(order, "PATH")
		}
	}
	out := make([]string, 0, len(order))
	for _, key := range order {
		out = append(out, key+"="+values[key])
	}
	return out
}
````

## File: internal/tools/sandbox.go
````go
package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type BubblewrapConfig struct {
	Enabled        bool
	BubblewrapPath string
	AllowNetwork   bool
	WritablePaths  []string
}

func commandWithSandbox(ctx context.Context, cfg BubblewrapConfig, cwd string, command []string) (*exec.Cmd, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if len(command) == 0 || strings.TrimSpace(command[0]) == "" {
		return nil, fmt.Errorf("sandbox command missing executable")
	}
	bwrap := strings.TrimSpace(cfg.BubblewrapPath)
	if bwrap == "" {
		bwrap = "bwrap"
	}
	if _, err := exec.LookPath(bwrap); err != nil {
		return nil, fmt.Errorf("bubblewrap unavailable: %w", err)
	}
	resolvedCommand, err := resolveExecutable(command[0], cwd)
	if err != nil {
		return nil, err
	}
	command = append([]string{resolvedCommand}, command[1:]...)

	args := []string{"--die-with-parent", "--new-session", "--unshare-pid", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/"}
	created := map[string]struct{}{}
	if !cfg.AllowNetwork {
		args = append(args, "--unshare-net")
	}
	for _, path := range sandboxReadOnlyPaths() {
		appendSandboxBind(&args, created, "--ro-bind", path, path)
	}
	for _, path := range cfg.WritablePaths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		if !filepath.IsAbs(clean) {
			return nil, fmt.Errorf("sandbox writable path must be absolute: %s", path)
		}
		appendSandboxBind(&args, created, "--bind", clean, clean)
	}
	if strings.TrimSpace(cwd) != "" {
		cleanCWD := filepath.Clean(cwd)
		if !filepath.IsAbs(cleanCWD) {
			return nil, fmt.Errorf("sandbox cwd must be absolute: %s", cwd)
		}
		if !sandboxPathCovered(cleanCWD, "", cfg.WritablePaths) {
			appendSandboxBind(&args, created, "--ro-bind", cleanCWD, cleanCWD)
		}
		args = append(args, "--chdir", cleanCWD)
	}
	commandDir := filepath.Dir(resolvedCommand)
	if !sandboxPathCovered(commandDir, cwd, cfg.WritablePaths) {
		appendSandboxBind(&args, created, "--ro-bind", commandDir, commandDir)
	}
	args = append(args, "--")
	args = append(args, command...)
	return exec.CommandContext(ctx, bwrap, args...), nil
}

func sandboxReadOnlyPaths() []string {
	return []string{
		"/bin",
		"/sbin",
		"/usr",
		"/lib",
		"/lib64",
		"/etc",
		"/opt",
		"/run/current-system/sw",
	}
}

func appendSandboxBind(args *[]string, created map[string]struct{}, bindFlag string, src string, dst string) {
	if _, err := os.Lstat(src); err != nil {
		return
	}
	appendSandboxParents(args, created, filepath.Dir(dst))
	*args = append(*args, bindFlag, src, dst)
}

func appendSandboxParents(args *[]string, created map[string]struct{}, dir string) {
	dir = filepath.Clean(dir)
	if dir == "." || dir == "/" {
		return
	}
	parts := strings.Split(strings.TrimPrefix(dir, "/"), "/")
	current := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = current + "/" + part
		if _, ok := created[current]; ok {
			continue
		}
		*args = append(*args, "--dir", current)
		created[current] = struct{}{}
	}
}

func sandboxPathCovered(path string, cwd string, writable []string) bool {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	coveredRoots := make([]string, 0, len(writable)+1)
	if trimmed := strings.TrimSpace(cwd); trimmed != "" {
		coveredRoots = append(coveredRoots, filepath.Clean(trimmed))
	}
	for _, root := range writable {
		if trimmed := strings.TrimSpace(root); trimmed != "" {
			coveredRoots = append(coveredRoots, filepath.Clean(trimmed))
		}
	}
	for _, root := range coveredRoots {
		if rel, err := filepath.Rel(root, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
````

## File: internal/tools/skill.go
````go
package tools

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/skills"
)

type ReadSkill struct {
	Base
	Inventory *skills.Inventory
	MaxBytes  int
}

func (t *ReadSkill) Name() string { return "read_skill" }
func (t *ReadSkill) Description() string {
	return "Read the full body of a skill by name (for ClawHub-compatible SKILL.md usage)."
}
func (t *ReadSkill) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"name":     map[string]any{"type": "string", "description": "Skill name from inventory"},
		"maxBytes": map[string]any{"type": "integer", "description": "Optional max bytes"},
	}, "required": []string{"name"}}
}
func (t *ReadSkill) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *ReadSkill) Execute(ctx context.Context, params map[string]any) (string, error) {
	_ = ctx
	if t.Inventory == nil {
		return "", fmt.Errorf("skills inventory not configured")
	}
	name := strings.TrimSpace(fmt.Sprint(params["name"]))
	if name == "" {
		return "", fmt.Errorf("missing name")
	}
	s, ok := t.Inventory.Get(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}
	maxBytes := t.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 200000
	}
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		maxBytes = int(v)
	}
	body, err := skills.LoadBody(s.Path, maxBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("# Skill: %s (%s, %s)\n\n%s", s.Name, s.Source, s.Dir, body), nil
}
````

## File: internal/tools/tools.go
````go
package tools

import (
	"context"
)

type CapabilityLevel string

const (
	CapabilitySafe       CapabilityLevel = "safe"
	CapabilityGuarded    CapabilityLevel = "guarded"
	CapabilityPrivileged CapabilityLevel = "privileged"
)

type Tool interface {
	Name() string
	Description() string
	Parameters() map[string]any
	Execute(ctx context.Context, params map[string]any) (string, error)
	Schema() map[string]any
}

type CapabilityReporter interface {
	Capability() CapabilityLevel
}

type CapabilityForParamsReporter interface {
	CapabilityForParams(params map[string]any) CapabilityLevel
}

func ToolCapability(t Tool, params map[string]any) CapabilityLevel {
	if t == nil {
		return CapabilitySafe
	}
	if dynamic, ok := t.(CapabilityForParamsReporter); ok {
		if level := dynamic.CapabilityForParams(params); level != "" {
			return level
		}
	}
	if static, ok := t.(CapabilityReporter); ok {
		if level := static.Capability(); level != "" {
			return level
		}
	}
	return CapabilitySafe
}

type Base struct{}

func (Base) SchemaFor(name, desc string, params map[string]any) map[string]any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name": name,
			"description": desc,
			"parameters": params,
		},
	}
}
````

## File: internal/triggers/triggers.go
````go
package triggers

import (
	"encoding/json"
	"strings"
)

// TriggerMeta carries metadata from trigger events.
type TriggerMeta struct {
	Source  string            // "webhook" or "filewatch"
	Path    string            // for file-change events
	Route   string            // for webhook events
	Headers map[string]string // for webhook events (limited subset)
}

const MetaKeyStructuredEvent = "structured_event"

type StructuredEvent struct {
	Type    string         `json:"type"`
	Source  string         `json:"source"`
	Trusted bool           `json:"trusted"`
	Details map[string]any `json:"details,omitempty"`
}

func StructuredEventMap(event StructuredEvent) map[string]any {
	details := map[string]any{}
	for key, value := range event.Details {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		details[trimmed] = value
	}
	return map[string]any{
		"type":    strings.TrimSpace(event.Type),
		"source":  strings.TrimSpace(event.Source),
		"trusted": event.Trusted,
		"details": details,
	}
}

func StructuredEventJSON(raw any) string {
	if raw == nil {
		return ""
	}
	b, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}
````

## File: .env.example
````
# Example environment for or3-intern
#
# This repo does NOT auto-load .env files.
# Load it in your shell before running, for example:
#   set -a; source .env; set +a
#   go run ./cmd/or3-intern chat
#
# If you use OpenRouter, set OR3_API_BASE and OR3_API_KEY.
# If you use OpenAI defaults, OPENAI_API_KEY is enough.

# --- Provider ---
# Used as the default API key unless OR3_API_KEY is set.
OPENAI_API_KEY=

# Preferred explicit provider key override.
OR3_API_KEY=

# OpenAI-compatible API base.
# OpenAI default: https://api.openai.com/v1
# OpenRouter: https://openrouter.ai/api/v1
OR3_API_BASE=https://api.openai.com/v1

# Chat model name.
# Examples:
#   gpt-4.1-mini
#   openai/gpt-4o-mini
OR3_MODEL=gpt-4.1-mini

# Embedding model used for memory retrieval.
OR3_EMBED_MODEL=text-embedding-3-small

# --- App storage ---
OR3_DB_PATH=
OR3_ARTIFACTS_DIR=

# --- Optional tool integrations ---
BRAVE_API_KEY=

# --- Optional chat channels ---
OR3_TELEGRAM_TOKEN=
OR3_SLACK_APP_TOKEN=
OR3_SLACK_BOT_TOKEN=
OR3_DISCORD_TOKEN=
OR3_WHATSAPP_BRIDGE_URL=ws://127.0.0.1:3001/ws
OR3_WHATSAPP_BRIDGE_TOKEN=
````

## File: .gitignore
````
.env
.or3/
/or3-intern
or3-intern.exe
````

## File: internal/agent/job_registry.go
````go
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	defaultJobRetention   = 2 * time.Minute
	defaultMaxTrackedJobs = 256
	defaultMaxJobEvents   = 256
	jobSubscriberBuffer   = 128
)

type JobEvent struct {
	Sequence int64          `json:"sequence"`
	Type     string         `json:"type"`
	Data     map[string]any `json:"data"`
}

type JobSnapshot struct {
	ID        string     `json:"id"`
	Kind      string     `json:"kind"`
	Status    string     `json:"status"`
	Events    []JobEvent `json:"events"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type JobRegistry struct {
	mu         sync.Mutex
	jobs       map[string]*jobEntry
	retention  time.Duration
	maxTracked int
	maxEvents  int
}

type jobEntry struct {
	id          string
	kind        string
	status      string
	events      []JobEvent
	subscribers map[int]chan JobEvent
	nextSubID   int
	nextSeq     int64
	cancel      context.CancelFunc
	done        chan struct{}
	terminal    bool
	createdAt   time.Time
	updatedAt   time.Time
}

type JobObserver struct {
	registry *JobRegistry
	jobID    string
}

func NewJobRegistry(retention time.Duration, maxTracked int) *JobRegistry {
	if retention <= 0 {
		retention = defaultJobRetention
	}
	if maxTracked <= 0 {
		maxTracked = defaultMaxTrackedJobs
	}
	return &JobRegistry{
		jobs:       map[string]*jobEntry{},
		retention:  retention,
		maxTracked: maxTracked,
		maxEvents:  defaultMaxJobEvents,
	}
}

func (r *JobRegistry) Register(kind string) JobSnapshot {
	return r.RegisterWithID(newServiceJobID(), kind)
}

func (r *JobRegistry) RegisterWithID(id string, kind string) JobSnapshot {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cleanupLocked(now)
	entry := &jobEntry{
		id:          id,
		kind:        kind,
		status:      "queued",
		subscribers: map[int]chan JobEvent{},
		done:        make(chan struct{}),
		createdAt:   now,
		updatedAt:   now,
	}
	r.jobs[id] = entry
	return snapshotForEntry(entry)
}

func (r *JobRegistry) AttachCancel(id string, cancel context.CancelFunc) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return false
	}
	entry.cancel = cancel
	entry.updatedAt = time.Now()
	return true
}

func (r *JobRegistry) Cancel(id string) bool {
	r.mu.Lock()
	entry := r.jobs[id]
	if entry == nil {
		r.mu.Unlock()
		return false
	}
	cancel := entry.cancel
	r.mu.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func (r *JobRegistry) Publish(id string, eventType string, data map[string]any) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return false
	}
	if data == nil {
		data = map[string]any{}
	}
	if _, ok := data["job_id"]; !ok {
		data["job_id"] = id
	}
	now := time.Now()
	entry.updatedAt = now
	if status, ok := data["status"].(string); ok && status != "" {
		entry.status = status
	}
	entry.nextSeq++
	event := JobEvent{Sequence: entry.nextSeq, Type: eventType, Data: cloneEventData(data)}
	entry.events = append(entry.events, event)
	if r.maxEvents > 0 && len(entry.events) > r.maxEvents {
		entry.events = append([]JobEvent(nil), entry.events[len(entry.events)-r.maxEvents:]...)
	}
	for _, ch := range entry.subscribers {
		select {
		case ch <- event:
		default:
		}
	}
	return true
}

func (r *JobRegistry) Complete(id string, status string, data map[string]any) bool {
	if data == nil {
		data = map[string]any{}
	}
	if status == "" {
		status = "completed"
	}
	data["status"] = status
	if !r.Publish(id, "completion", data) {
		return false
	}
	r.markTerminal(id, status)
	return true
}

func (r *JobRegistry) Fail(id string, message string, data map[string]any) bool {
	if data == nil {
		data = map[string]any{}
	}
	if message != "" {
		data["message"] = message
	}
	data["status"] = "failed"
	if !r.Publish(id, "error", data) {
		return false
	}
	r.markTerminal(id, "failed")
	return true
}

func (r *JobRegistry) markTerminal(id string, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil || entry.terminal {
		return
	}
	entry.status = status
	entry.terminal = true
	entry.updatedAt = time.Now()
	close(entry.done)
	for subID, ch := range entry.subscribers {
		close(ch)
		delete(entry.subscribers, subID)
	}
}

func (r *JobRegistry) Subscribe(id string) (JobSnapshot, <-chan JobEvent, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return JobSnapshot{}, nil, nil, false
	}
	snapshot := snapshotForEntry(entry)
	if entry.terminal {
		ch := make(chan JobEvent)
		close(ch)
		return snapshot, ch, func() {}, true
	}
	entry.nextSubID++
	subID := entry.nextSubID
	ch := make(chan JobEvent, jobSubscriberBuffer)
	entry.subscribers[subID] = ch
	unsubscribe := func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		current := r.jobs[id]
		if current == nil {
			return
		}
		sub, ok := current.subscribers[subID]
		if !ok {
			return
		}
		close(sub)
		delete(current.subscribers, subID)
	}
	return snapshot, ch, unsubscribe, true
}

func (r *JobRegistry) Snapshot(id string) (JobSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.jobs[id]
	if entry == nil {
		return JobSnapshot{}, false
	}
	return snapshotForEntry(entry), true
}

func (r *JobRegistry) Wait(ctx context.Context, id string) (JobSnapshot, bool) {
	r.mu.Lock()
	entry := r.jobs[id]
	if entry == nil {
		r.mu.Unlock()
		return JobSnapshot{}, false
	}
	done := entry.done
	terminal := entry.terminal
	r.mu.Unlock()
	if !terminal {
		select {
		case <-done:
		case <-ctx.Done():
			return JobSnapshot{}, false
		}
	}
	return r.Snapshot(id)
}

func (r *JobRegistry) Observer(jobID string) ConversationObserver {
	return JobObserver{registry: r, jobID: jobID}
}

func (o JobObserver) OnTextDelta(_ context.Context, text string) {
	if o.registry == nil || text == "" {
		return
	}
	o.registry.Publish(o.jobID, "text_delta", map[string]any{"content": text})
}

func (o JobObserver) OnToolCall(_ context.Context, name string, arguments string) {
	if o.registry == nil {
		return
	}
	o.registry.Publish(o.jobID, "tool_call", map[string]any{"name": name, "arguments": arguments})
}

func (o JobObserver) OnToolResult(_ context.Context, name string, result string, err error) {
	if o.registry == nil {
		return
	}
	data := map[string]any{"name": name, "result": result}
	if err != nil {
		data["error"] = err.Error()
	}
	o.registry.Publish(o.jobID, "tool_result", data)
}

func (o JobObserver) OnCompletion(_ context.Context, finalText string, streamed bool) {
	if o.registry == nil {
		return
	}
	o.registry.Publish(o.jobID, "assistant", map[string]any{"content": finalText, "streamed": streamed})
}

func (o JobObserver) OnError(_ context.Context, err error) {
	if o.registry == nil || err == nil {
		return
	}
	o.registry.Publish(o.jobID, "runtime_error", map[string]any{"message": err.Error()})
}

func (r *JobRegistry) cleanupLocked(now time.Time) {
	for id, entry := range r.jobs {
		if entry == nil {
			delete(r.jobs, id)
			continue
		}
		if entry.terminal && now.Sub(entry.updatedAt) > r.retention {
			delete(r.jobs, id)
		}
	}
	for len(r.jobs) > r.maxTracked {
		oldestID := ""
		var oldest time.Time
		for id, entry := range r.jobs {
			if entry == nil || !entry.terminal {
				continue
			}
			if oldestID == "" || entry.updatedAt.Before(oldest) {
				oldestID = id
				oldest = entry.updatedAt
			}
		}
		if oldestID == "" {
			break
		}
		delete(r.jobs, oldestID)
	}
}

func snapshotForEntry(entry *jobEntry) JobSnapshot {
	events := make([]JobEvent, len(entry.events))
	for i, event := range entry.events {
		events[i] = JobEvent{
			Sequence: event.Sequence,
			Type:     event.Type,
			Data:     cloneEventData(event.Data),
		}
	}
	return JobSnapshot{
		ID:        entry.id,
		Kind:      entry.kind,
		Status:    entry.status,
		Events:    events,
		CreatedAt: entry.createdAt,
		UpdatedAt: entry.updatedAt,
	}
}

func cloneEventData(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func newServiceJobID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "svc-job"
	}
	return "svc-" + hex.EncodeToString(raw[:])
}
````

## File: internal/channels/cli/cli.go
````go
package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"or3-intern/internal/bus"
)

// Channel reads user input from stdin and publishes messages to the bus.
type Channel struct {
	Bus        *bus.Bus
	SessionKey string
	Spinner    *Spinner // shared with Deliverer so it can be stopped on output
}

func (c *Channel) Run(ctx context.Context) error {
	if c.SessionKey == "" {
		c.SessionKey = "default"
	}
	in := bufio.NewScanner(os.Stdin)
	fmt.Print(Banner())
	ShowPrompt() // initial prompt
	for {
		// Prompt is printed either above (first iteration) or by the
		// Deliverer after finishing a response. We block on Scan here.
		if !in.Scan() {
			return nil
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			fmt.Print(Prompt())
			continue
		}
		if line == "/exit" {
			if isTTY {
				fmt.Println(style(ansiDim+ansiGray, "  Goodbye 👋"))
			}
			return nil
		}

		ok := c.Bus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: c.SessionKey,
			Channel:    "cli",
			From:       "local",
			Message:    line,
		})
		if !ok {
			fmt.Println(style(ansiYellow, "  ⚠ queue full — message dropped"))
			fmt.Print(Prompt())
		} else {
			// Restyle the raw prompt line into a labeled user message block.
			RewriteUserMessage(line)
			if c.Spinner != nil {
				c.Spinner.Start("thinking…")
			}
			// Don't print the prompt — the Deliverer will show it
			// after the response is fully rendered.
		}
	}
}
````

## File: internal/clawhub/client.go
````go
package clawhub

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	apiSearch                  = "/api/v1/search"
	apiResolve                 = "/api/v1/resolve"
	apiDownload                = "/api/v1/download"
	apiSkills                  = "/api/v1/skills"
	maxDownloadZipBytes        = 32 << 20
	maxArchiveEntries          = 512
	maxArchiveFileBytes  int64 = 4 << 20
	maxArchiveTotalBytes int64 = 64 << 20
)

type Client struct {
	SiteURL     string
	RegistryURL string
	HTTP        *http.Client
}

type SearchResult struct {
	Slug        string
	DisplayName string
	Summary     string
	Version     string
	Score       float64
	UpdatedAt   int64
}

type SkillInfo struct {
	Slug            string
	DisplayName     string
	Summary         string
	LatestVersion   string
	SelectedVersion string
	Owner           string
}

type ResolveResult struct {
	MatchVersion  string
	LatestVersion string
}

type InstallOptions struct {
	Force bool
}

type InstallResult struct {
	Path        string
	Slug        string
	Version     string
	Fingerprint string
}

type ScanFinding struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
}

func (f ScanFinding) Summary() string {
	parts := make([]string, 0, 3)
	if text := strings.TrimSpace(f.Severity); text != "" {
		parts = append(parts, text)
	}
	if text := strings.TrimSpace(f.Path); text != "" {
		parts = append(parts, text)
	}
	if text := strings.TrimSpace(f.Message); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(parts, ": ")
}

type SkillOrigin struct {
	Version          int           `json:"version"`
	Registry         string        `json:"registry"`
	Slug             string        `json:"slug"`
	Owner            string        `json:"owner,omitempty"`
	InstalledVersion string        `json:"installedVersion"`
	InstalledAt      int64         `json:"installedAt"`
	Fingerprint      string        `json:"fingerprint"`
	ScanStatus       string        `json:"scanStatus,omitempty"`
	ScanFindings     []ScanFinding `json:"scanFindings,omitempty"`
}

type InstalledSkill struct {
	Name     string
	Path     string
	Origin   SkillOrigin
	Modified bool
}

func New(siteURL, registryURL string) *Client {
	return &Client{
		SiteURL:     strings.TrimRight(siteURL, "/"),
		RegistryURL: strings.TrimRight(registryURL, "/"),
		HTTP:        &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	url := c.apiURL(apiSearch)
	url.RawQuery = queryString(map[string]string{
		"q":     strings.TrimSpace(query),
		"limit": intString(limit),
	})
	var response struct {
		Results []struct {
			Slug        string  `json:"slug"`
			DisplayName string  `json:"displayName"`
			Summary     string  `json:"summary"`
			Version     string  `json:"version"`
			Score       float64 `json:"score"`
			UpdatedAt   int64   `json:"updatedAt"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, url.String(), &response); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(response.Results))
	for _, item := range response.Results {
		results = append(results, SearchResult{
			Slug:        item.Slug,
			DisplayName: item.DisplayName,
			Summary:     item.Summary,
			Version:     item.Version,
			Score:       item.Score,
			UpdatedAt:   item.UpdatedAt,
		})
	}
	return results, nil
}

func (c *Client) Inspect(ctx context.Context, slug, version string) (SkillInfo, error) {
	slug = sanitizeSlug(slug)
	if slug == "" {
		return SkillInfo{}, fmt.Errorf("slug required")
	}
	var response struct {
		Skill *struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"displayName"`
			Summary     string `json:"summary"`
		} `json:"skill"`
		LatestVersion *struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
		Owner *struct {
			Handle      string `json:"handle"`
			DisplayName string `json:"displayName"`
		} `json:"owner"`
	}
	if err := c.getJSON(ctx, c.apiURL(apiSkills+"/"+slug).String(), &response); err != nil {
		return SkillInfo{}, err
	}
	if response.Skill == nil {
		return SkillInfo{}, fmt.Errorf("skill not found: %s", slug)
	}
	info := SkillInfo{
		Slug:        response.Skill.Slug,
		DisplayName: response.Skill.DisplayName,
		Summary:     response.Skill.Summary,
		LatestVersion: stringOr(response.LatestVersion, func(v *struct {
			Version string `json:"version"`
		}) string {
			return v.Version
		}),
		SelectedVersion: strings.TrimSpace(version),
		Owner:           ownerName(response.Owner),
	}
	if info.SelectedVersion == "" {
		info.SelectedVersion = info.LatestVersion
	}
	return info, nil
}

func (c *Client) Resolve(ctx context.Context, slug, fingerprint string) (ResolveResult, error) {
	slug = sanitizeSlug(slug)
	if slug == "" {
		return ResolveResult{}, fmt.Errorf("slug required")
	}
	url := c.apiURL(apiResolve)
	url.RawQuery = queryString(map[string]string{
		"slug":    slug,
		"version": "",
		"hash":    strings.TrimSpace(fingerprint),
	})
	var response struct {
		Match *struct {
			Version string `json:"version"`
		} `json:"match"`
		LatestVersion *struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
	}
	if err := c.getJSON(ctx, url.String(), &response); err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{
		MatchVersion: stringOr(response.Match, func(v *struct {
			Version string `json:"version"`
		}) string {
			return v.Version
		}),
		LatestVersion: stringOr(response.LatestVersion, func(v *struct {
			Version string `json:"version"`
		}) string {
			return v.Version
		}),
	}, nil
}

func (c *Client) Download(ctx context.Context, slug, version string) ([]byte, error) {
	slug = sanitizeSlug(slug)
	if slug == "" {
		return nil, fmt.Errorf("slug required")
	}
	url := c.apiURL(apiDownload)
	url.RawQuery = queryString(map[string]string{
		"slug":    slug,
		"version": strings.TrimSpace(version),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readHTTPError(resp)
	}
	reader := &io.LimitedReader{R: resp.Body, N: maxDownloadZipBytes + 1}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxDownloadZipBytes {
		return nil, fmt.Errorf("download exceeded %d bytes", maxDownloadZipBytes)
	}
	return data, nil
}

func (c *Client) Install(ctx context.Context, slug, version, destDir string, opts InstallOptions) (InstallResult, error) {
	info, err := c.Inspect(ctx, slug, version)
	if err != nil {
		return InstallResult{}, err
	}
	if strings.TrimSpace(info.SelectedVersion) == "" {
		return InstallResult{}, fmt.Errorf("could not resolve version for %s", slug)
	}
	zipBytes, err := c.Download(ctx, slug, info.SelectedVersion)
	if err != nil {
		return InstallResult{}, err
	}
	target := filepath.Join(destDir, sanitizeSlug(slug))
	if err := installZip(zipBytes, target, SkillOrigin{
		Version:          2,
		Registry:         c.RegistryURL,
		Slug:             sanitizeSlug(slug),
		Owner:            info.Owner,
		InstalledVersion: info.SelectedVersion,
		InstalledAt:      time.Now().UnixMilli(),
	}, opts); err != nil {
		return InstallResult{}, err
	}
	origin, err := ReadOrigin(target)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Path:        target,
		Slug:        origin.Slug,
		Version:     origin.InstalledVersion,
		Fingerprint: origin.Fingerprint,
	}, nil
}

func installZip(zipBytes []byte, target string, origin SkillOrigin, opts InstallOptions) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if stat, err := os.Stat(target); err == nil && stat.IsDir() {
		if !opts.Force {
			modified, checkErr := LocalEdits(target)
			if checkErr != nil {
				return checkErr
			}
			if modified {
				return fmt.Errorf("local modifications detected: %s", target)
			}
		}
	} else if err == nil {
		return fmt.Errorf("target exists and is not a directory: %s", target)
	}

	tempRoot, err := os.MkdirTemp(filepath.Dir(target), ".clawhub-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempRoot)
	tempTarget := filepath.Join(tempRoot, filepath.Base(target))
	if err := extractZipToDir(zipBytes, tempTarget); err != nil {
		return err
	}
	fingerprint, err := FingerprintDir(tempTarget)
	if err != nil {
		return err
	}
	origin.Fingerprint = fingerprint
	scanStatus, scanFindings, err := scanInstalledSkill(tempTarget)
	if err != nil {
		return err
	}
	origin.ScanStatus = scanStatus
	origin.ScanFindings = scanFindings
	if err := WriteOrigin(tempTarget, origin); err != nil {
		return err
	}

	backup := target + ".bak"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(tempTarget, target); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, target)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func extractZipToDir(zipBytes []byte, target string) error {
	if int64(len(zipBytes)) > maxDownloadZipBytes {
		return fmt.Errorf("archive exceeded %d bytes", maxDownloadZipBytes)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return err
	}
	if len(reader.File) > maxArchiveEntries {
		return fmt.Errorf("archive exceeded %d entries", maxArchiveEntries)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	var totalBytes int64
	for _, file := range reader.File {
		rel, ok := safeZipPath(file.Name)
		if !ok {
			continue
		}
		full := filepath.Join(target, rel)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(full, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o644
		}
		out, err := os.OpenFile(full, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			_ = rc.Close()
			return err
		}
		limited := &io.LimitedReader{R: rc, N: maxArchiveFileBytes + 1}
		written, copyErr := io.Copy(out, limited)
		closeErr := rc.Close()
		outCloseErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if outCloseErr != nil {
			return outCloseErr
		}
		if written > maxArchiveFileBytes {
			return fmt.Errorf("archive entry exceeded %d bytes: %s", maxArchiveFileBytes, rel)
		}
		totalBytes += written
		if totalBytes > maxArchiveTotalBytes {
			return fmt.Errorf("archive exceeded %d extracted bytes", maxArchiveTotalBytes)
		}
		if file.UncompressedSize64 > uint64(maxArchiveFileBytes) {
			return fmt.Errorf("archive entry exceeded %d bytes: %s", maxArchiveFileBytes, rel)
		}
		if err := os.Chmod(full, mode); err != nil {
			return err
		}
	}
	return nil
}

func FingerprintDir(root string) (string, error) {
	type item struct {
		path string
		sum  string
	}
	var files []item
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".clawhub" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files = append(files, item{
			path: filepath.ToSlash(rel),
			sum:  hex.EncodeToString(sum[:]),
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	h := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(h, file.path+":"+file.sum+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func LocalEdits(skillDir string) (bool, error) {
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		return false, err
	}
	current, err := FingerprintDir(skillDir)
	if err != nil {
		return false, err
	}
	return current != origin.Fingerprint, nil
}

func ListInstalled(root string) ([]InstalledSkill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]InstalledSkill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		origin, err := ReadOrigin(path)
		if err != nil {
			continue
		}
		modified, err := LocalEdits(path)
		if err != nil {
			return nil, err
		}
		out = append(out, InstalledSkill{
			Name:     entry.Name(),
			Path:     path,
			Origin:   origin,
			Modified: modified,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func ReadOrigin(skillDir string) (SkillOrigin, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, ".clawhub", "origin.json"))
	if err != nil {
		return SkillOrigin{}, err
	}
	var origin SkillOrigin
	if err := json.Unmarshal(data, &origin); err != nil {
		return SkillOrigin{}, err
	}
	return origin, nil
}

func WriteOrigin(skillDir string, origin SkillOrigin) error {
	path := filepath.Join(skillDir, ".clawhub", "origin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

const installScanReadMaxBytes = 128 * 1024

func scanInstalledSkill(root string) (string, []ScanFinding, error) {
	findings := make([]ScanFinding, 0)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".clawhub" {
				return filepath.SkipDir
			}
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		findings = append(findings, pathFindings(rel)...)
		if info.Size() <= 0 || info.Size() > installScanReadMaxBytes || !scanContentFile(rel, info.Mode()) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		findings = append(findings, contentFindings(rel, string(data))...)
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	status := "clean"
	for _, finding := range findings {
		switch strings.ToLower(strings.TrimSpace(finding.Severity)) {
		case "high":
			return "blocked", findings, nil
		case "medium":
			status = "quarantined"
		}
	}
	return status, findings, nil
}

func pathFindings(rel string) []ScanFinding {
	lower := strings.ToLower(strings.TrimSpace(rel))
	for _, name := range []string{".env", ".netrc", ".npmrc", ".pypirc", "id_rsa", "id_dsa", ".aws/credentials", ".aws/config", ".ssh/config", ".ssh/known_hosts"} {
		if lower == name || strings.HasSuffix(lower, "/"+name) {
			return []ScanFinding{{Severity: "high", Path: rel, Rule: "credential-material", Message: "bundle contains credential or host-secret material"}}
		}
	}
	return nil
}

func scanContentFile(rel string, mode os.FileMode) bool {
	ext := strings.ToLower(filepath.Ext(rel))
	if mode&0o111 != 0 {
		return true
	}
	switch ext {
	case ".sh", ".bash", ".zsh", ".command", ".ps1", ".bat", ".cmd", ".py", ".rb", ".js", ".ts":
		return true
	default:
		return false
	}
}

func contentFindings(rel, content string) []ScanFinding {
	lower := strings.ToLower(content)
	findings := make([]ScanFinding, 0, 2)
	rules := []struct {
		rule     string
		severity string
		message  string
		match    func(string) bool
	}{
		{rule: "curl-pipe-shell", severity: "medium", message: "downloads remote content directly into a shell", match: func(s string) bool { return strings.Contains(s, "curl ") && strings.Contains(s, "| sh") }},
		{rule: "wget-pipe-shell", severity: "medium", message: "downloads remote content directly into a shell", match: func(s string) bool { return strings.Contains(s, "wget ") && strings.Contains(s, "| sh") }},
		{rule: "powershell-iex", severity: "medium", message: "executes downloaded content inline", match: func(s string) bool { return strings.Contains(s, "invoke-webrequest") && strings.Contains(s, "iex") }},
		{rule: "reverse-shell", severity: "medium", message: "contains a shell/network handoff pattern", match: func(s string) bool { return strings.Contains(s, "/dev/tcp/") || strings.Contains(s, "nc -e") }},
		{rule: "osascript", severity: "medium", message: "invokes local system automation outside the declared tool model", match: func(s string) bool { return strings.Contains(s, "osascript") }},
	}
	for _, rule := range rules {
		if rule.match(lower) {
			findings = append(findings, ScanFinding{Severity: rule.severity, Path: rel, Rule: rule.rule, Message: rule.message})
		}
	}
	return findings
}

func RemoveSkill(root, name string) error {
	name = sanitizeSlug(name)
	if name == "" {
		return fmt.Errorf("skill name required")
	}
	return os.RemoveAll(filepath.Join(root, name))
}

func (c *Client) apiURL(path string) *urlBuilder {
	return newURLBuilder(c.RegistryURL, path)
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *Client) getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readHTTPError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func sanitizeSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.Contains(slug, "..") || strings.Contains(slug, "/") || strings.Contains(slug, "\\") {
		return ""
	}
	return slug
}

func safeZipPath(path string) (string, bool) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	if path == "" || strings.Contains(path, "..") {
		return "", false
	}
	return filepath.FromSlash(path), true
}

func readHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = resp.Status
	}
	return fmt.Errorf("clawhub API error: %s", text)
}

func queryString(values map[string]string) string {
	var parts []string
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, urlEncode(key)+"="+urlEncode(value))
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func intString(v int) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprint(v)
}

func ownerName(owner *struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
}) string {
	if owner == nil {
		return ""
	}
	if strings.TrimSpace(owner.Handle) != "" {
		return owner.Handle
	}
	return owner.DisplayName
}

func stringOr[T any](value *T, fn func(*T) string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fn(value))
}

type urlBuilder struct {
	base     string
	path     string
	RawQuery string
}

func newURLBuilder(base, path string) *urlBuilder {
	return &urlBuilder{
		base: strings.TrimRight(base, "/"),
		path: path,
	}
}

func (u *urlBuilder) String() string {
	if strings.TrimSpace(u.RawQuery) == "" {
		return u.base + u.path
	}
	return u.base + u.path + "?" + u.RawQuery
}

func urlEncode(s string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		" ", "%20",
		"!", "%21",
		"#", "%23",
		"$", "%24",
		"&", "%26",
		"'", "%27",
		"(", "%28",
		")", "%29",
		"+", "%2B",
		",", "%2C",
		"/", "%2F",
		":", "%3A",
		";", "%3B",
		"=", "%3D",
		"?", "%3F",
		"@", "%40",
	)
	return replacer.Replace(s)
}
````

## File: internal/heartbeat/service.go
````go
package heartbeat

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
	"or3-intern/internal/triggers"
)

const (
	DefaultChannel = "system"
	DefaultFrom    = "heartbeat"
	SeedMessage    = "Review HEARTBEAT.md and execute any active recurring tasks."

	MetaKeyHeartbeat = "heartbeat"
	MetaKeyDone      = "heartbeat_done"
)

type Service struct {
	Config       config.HeartbeatConfig
	WorkspaceDir string
	Bus          *bus.Bus

	logf func(string, ...any)

	mu        sync.Mutex
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	tickQueue chan struct{}
	inFlight  atomic.Bool
	stopping  atomic.Bool
}

func New(cfg config.HeartbeatConfig, workspaceDir string, eventBus *bus.Bus) *Service {
	return &Service{
		Config:       cfg,
		WorkspaceDir: workspaceDir,
		Bus:          eventBus,
		logf:         log.Printf,
	}
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || !s.Config.Enabled || s.Bus == nil {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	s.stopping.Store(false)

	childCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.tickQueue = make(chan struct{}, 1)

	interval := time.Duration(normalizeIntervalMinutes(s.Config.IntervalMinutes)) * time.Minute
	s.wg.Add(2)
	go s.runTicker(childCtx, interval)
	go s.runPublisher(childCtx)
}

func (s *Service) Stop() {
	if s == nil {
		return
	}
	s.stopping.Store(true)

	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.tickQueue = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	s.wg.Wait()
	s.inFlight.Store(false)
}

func (s *Service) runTicker(ctx context.Context, interval time.Duration) {
	defer s.wg.Done()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.enqueueTick("timer")
		}
	}
}

func (s *Service) runPublisher(ctx context.Context) {
	defer s.wg.Done()

	for {
		if s.stopping.Load() || ctx.Err() != nil {
			return
		}

		s.mu.Lock()
		tickQueue := s.tickQueue
		s.mu.Unlock()
		if tickQueue == nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-tickQueue:
			if s.stopping.Load() || ctx.Err() != nil {
				return
			}
			s.processTick()
		}
	}
}

func (s *Service) enqueueTick(source string) bool {
	s.mu.Lock()
	tickQueue := s.tickQueue
	s.mu.Unlock()
	if tickQueue == nil {
		return false
	}

	select {
	case tickQueue <- struct{}{}:
		return true
	default:
		s.logf("heartbeat tick dropped: pending tick already queued source=%s", source)
		return false
	}
}

func (s *Service) processTick() {
	if s.inFlight.Load() {
		s.logf("heartbeat tick skipped: previous turn still in flight")
		return
	}

	path, text, err := LoadTasksFile(s.Config.TasksFile, s.WorkspaceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.logf("heartbeat tick skipped: tasks file not found")
			return
		}
		s.logf("heartbeat tick skipped: read failed path=%q err=%v", path, err)
		return
	}
	if !HasActiveInstructions(text) {
		return
	}

	s.inFlight.Store(true)
	meta := map[string]any{
		MetaKeyHeartbeat: true,
		MetaKeyDone: func() {
			s.inFlight.Store(false)
		},
		"tasks_path": path,
		triggers.MetaKeyStructuredEvent: triggers.StructuredEventMap(triggers.StructuredEvent{
			Type:    string(bus.EventHeartbeat),
			Source:  "heartbeat",
			Trusted: true,
			Details: map[string]any{"tasks_path": path, "session_key": normalizedSessionKey(s.Config.SessionKey)},
		}),
	}
	if structuredTasks, ok := triggers.ParseStructuredTasksText(text); ok {
		meta[triggers.MetaKeyStructuredTasks] = triggers.StructuredTasksMap(structuredTasks)
	}
	ev := bus.Event{
		Type:       bus.EventHeartbeat,
		SessionKey: normalizedSessionKey(s.Config.SessionKey),
		Channel:    DefaultChannel,
		From:       DefaultFrom,
		Message:    SeedMessage,
		Meta:       meta,
	}
	if ok := s.Bus.Publish(ev); !ok {
		s.inFlight.Store(false)
		s.logf("heartbeat tick dropped: event bus full")
	}
}

func LoadTasksFile(configPath, workspaceDir string) (string, string, error) {
	var firstErr error
	for _, path := range candidatePaths(configPath, workspaceDir) {
		data, err := os.ReadFile(path)
		if err == nil {
			return path, strings.TrimSpace(string(data)), nil
		}
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return strings.TrimSpace(configPath), "", firstErr
	}
	return strings.TrimSpace(configPath), "", os.ErrNotExist
}

func HasActiveInstructions(text string) bool {
	inComment := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if inComment {
			if strings.Contains(trimmed, "-->") {
				inComment = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		return true
	}
	return false
}

func candidatePaths(configPath, workspaceDir string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 3)
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	if strings.TrimSpace(workspaceDir) != "" {
		add(filepath.Join(workspaceDir, "HEARTBEAT.md"))
		add(filepath.Join(workspaceDir, "heartbeat.md"))
	}
	add(configPath)
	return out
}

func normalizeIntervalMinutes(v int) int {
	if v <= 0 {
		return 30
	}
	if v < 1 {
		return 1
	}
	return v
}

func normalizedSessionKey(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return config.DefaultHeartbeatSessionKey
	}
	return v
}
````

## File: internal/memory/docs.go
````go
package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
)

// DocIndexConfig controls what gets indexed.
type DocIndexConfig struct {
	Roots          []string
	MaxFiles       int
	MaxFileBytes   int
	MaxChunks      int
	EmbedMaxBytes  int
	RefreshSeconds int
	RetrieveLimit  int
}

// IndexedDoc is a row from memory_docs.
type IndexedDoc struct {
	ID        int64
	ScopeKey  string
	Path      string
	Kind      string
	Title     string
	Summary   string
	Text      string
	Embedding []byte
	MTimeMS   int64
	SizeBytes int64
	Active    bool
	UpdatedAt int64
}

// DocIndexer syncs configured roots into the memory_docs table.
type DocIndexer struct {
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
	Config     DocIndexConfig
}

type indexedDocState struct {
	hash      string
	mtimeMS   int64
	sizeBytes int64
	active    bool
}

func (x *DocIndexer) defaults() DocIndexConfig {
	c := x.Config
	if c.MaxFiles <= 0 {
		c.MaxFiles = 100
	}
	if c.MaxFileBytes <= 0 {
		c.MaxFileBytes = 64 * 1024
	}
	if c.MaxChunks <= 0 {
		c.MaxChunks = 500
	}
	if c.EmbedMaxBytes <= 0 {
		c.EmbedMaxBytes = 8 * 1024
	}
	if c.RetrieveLimit <= 0 {
		c.RetrieveLimit = 5
	}
	return c
}

// SyncRoots scans all configured roots and updates memory_docs for scopeKey.
// It enforces caps on file count and file size, skips symlinks, and
// deactivates docs for files that have disappeared.
func (x *DocIndexer) SyncRoots(ctx context.Context, scopeKey string) error {
	if x == nil || x.DB == nil {
		return fmt.Errorf("doc indexer not configured")
	}
	cfg := x.defaults()
	if len(cfg.Roots) == 0 {
		return nil
	}

	seen := map[string]bool{}
	fileCount := 0
	chunkCount := 0
	existing, err := x.loadIndexedDocState(ctx, scopeKey)
	if err != nil {
		return err
	}

	for _, root := range cfg.Roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		absRoot, err = filepath.EvalSymlinks(absRoot)
		if err != nil {
			continue
		}

			err = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.Type()&os.ModeSymlink != 0 {
					return nil
				}
				if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && path != absRoot {
					return filepath.SkipDir
				}
				return nil
				}
				ext := strings.ToLower(filepath.Ext(path))
				switch ext {
				case ".md", ".txt":
				default:
					return nil
				}

				realPath, err := filepath.EvalSymlinks(path)
				if err != nil {
					return err
				}
			rel, err := filepath.Rel(absRoot, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil
			}

			if fileCount >= cfg.MaxFiles {
				return filepath.SkipAll
			}
			if chunkCount >= cfg.MaxChunks {
				return filepath.SkipAll
			}

				info, err := os.Lstat(realPath)
				if err != nil {
					return err
				}
				if info.Size() > int64(cfg.MaxFileBytes) {
					return nil
				}

			seen[realPath] = true
			fileCount++
			mtimeMS := info.ModTime().UnixMilli()
			sizeBytes := info.Size()
			if state, ok := existing[realPath]; ok && state.active && state.mtimeMS == mtimeMS && state.sizeBytes == sizeBytes {
				chunkCount++
				return nil
			}

				data, err := readDocFile(realPath, cfg.MaxFileBytes)
				if err != nil {
					return err
				}

			h := fileHash(data)
			if state, ok := existing[realPath]; ok && state.active && state.hash == h {
				chunkCount++
				return nil
			}

			kind := extKind(ext)
			title := filepath.Base(realPath)
			text := string(data)
			summary := extractSummary(text)

			var embedding []byte
			if x.Provider != nil && x.EmbedModel != "" && len(data) <= cfg.EmbedMaxBytes {
				vec, err := x.Provider.Embed(ctx, x.EmbedModel, truncateForEmbed(text, cfg.EmbedMaxBytes))
				if err == nil && len(vec) > 0 {
					embedding = PackFloat32(vec)
				}
			}

			now := db.NowMS()
				_, err = x.DB.SQL.ExecContext(ctx,
					`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, hash, mtime_ms, size_bytes, active, updated_at)
                 VALUES(?,?,?,?,?,?,?,?,?,?,1,?)
                 ON CONFLICT(scope_key, path) DO UPDATE SET
                   kind=excluded.kind, title=excluded.title, summary=excluded.summary,
                   text=excluded.text, embedding=excluded.embedding,
                   hash=excluded.hash, mtime_ms=excluded.mtime_ms,
                   size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
					scopeKey, realPath, kind, title, summary, text, nullBytes(embedding), h, mtimeMS, sizeBytes, now)
				if err != nil {
					return fmt.Errorf("upsert indexed doc %s: %w", realPath, err)
				}
				chunkCount++
				return nil
			})
			if err != nil {
				return err
			}
		}

	// deactivate docs no longer on disk
	rows, err := x.DB.SQL.QueryContext(ctx,
		`SELECT path FROM memory_docs WHERE scope_key=? AND active=1`, scopeKey)
	if err != nil {
		return err
	}
	var toDeactivate []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		if !seen[p] {
			toDeactivate = append(toDeactivate, p)
		}
	}
	rows.Close()
	for _, p := range toDeactivate {
		_, _ = x.DB.SQL.ExecContext(ctx,
			`UPDATE memory_docs SET active=0, updated_at=? WHERE scope_key=? AND path=?`,
			db.NowMS(), scopeKey, p)
	}
	return nil
}

func (x *DocIndexer) loadIndexedDocState(ctx context.Context, scopeKey string) (map[string]indexedDocState, error) {
	rows, err := x.DB.SQL.QueryContext(ctx,
		`SELECT path, hash, mtime_ms, size_bytes, active FROM memory_docs WHERE scope_key=?`, scopeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]indexedDocState{}
	for rows.Next() {
		var path, hash string
		var mtimeMS, sizeBytes int64
		var active int
		if err := rows.Scan(&path, &hash, &mtimeMS, &sizeBytes, &active); err != nil {
			return nil, err
		}
		out[path] = indexedDocState{hash: hash, mtimeMS: mtimeMS, sizeBytes: sizeBytes, active: active == 1}
	}
	return out, rows.Err()
}

func readDocFile(path string, maxBytes int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, int64(maxBytes)))
}

func (x *DocIndexer) needsUpdate(ctx context.Context, scopeKey, path, newHash string) bool {
	row := x.DB.SQL.QueryRowContext(ctx,
		`SELECT hash FROM memory_docs WHERE scope_key=? AND path=? AND active=1`, scopeKey, path)
	var existing string
	if err := row.Scan(&existing); err != nil {
		return true
	}
	return existing != newHash
}

// DocRetriever retrieves indexed docs by FTS query.
type DocRetriever struct {
	DB *db.DB
}

// RetrievedDoc is a doc excerpt returned by retrieval.
type RetrievedDoc struct {
	Path    string
	Title   string
	Excerpt string
	Score   float64
}

// RetrieveDocs queries the FTS index for docs matching query.
func (r *DocRetriever) RetrieveDocs(ctx context.Context, scopeKey, query string, topK int) ([]RetrievedDoc, error) {
	if topK <= 0 {
		topK = 5
	}
	q := normalizeFTSQuery(query)
	if q == "" {
		return nil, nil
	}
	rows, err := r.DB.SQL.QueryContext(ctx,
		`SELECT memory_docs_fts.rowid, memory_docs.path, memory_docs.title, memory_docs.text, bm25(memory_docs_fts) as rank
         FROM memory_docs_fts
         JOIN memory_docs ON memory_docs.id = memory_docs_fts.rowid
         WHERE memory_docs_fts MATCH ? AND memory_docs.scope_key=? AND memory_docs.active=1
         ORDER BY rank LIMIT ?`,
		q, scopeKey, topK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RetrievedDoc
	for rows.Next() {
		var rowid int64
		var path, title, text string
		var rank float64
		if err := rows.Scan(&rowid, &path, &title, &text, &rank); err != nil {
			return nil, err
		}
		out = append(out, RetrievedDoc{
			Path:    path,
			Title:   title,
			Excerpt: excerptText(text, 500),
			Score:   1.0 / (1.0 + rank),
		})
	}
	return out, rows.Err()
}

// UpsertDoc inserts or updates a doc in memory_docs (for direct use by tests).
func UpsertDoc(ctx context.Context, d *db.DB, scopeKey, path, kind, title, summary, text string, embedding []byte, hash string, mtimeMS, sizeBytes int64) error {
	now := db.NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_docs(scope_key, path, kind, title, summary, text, embedding, hash, mtime_ms, size_bytes, active, updated_at)
         VALUES(?,?,?,?,?,?,?,?,?,?,1,?)
         ON CONFLICT(scope_key, path) DO UPDATE SET
           kind=excluded.kind, title=excluded.title, summary=excluded.summary,
           text=excluded.text, embedding=excluded.embedding,
           hash=excluded.hash, mtime_ms=excluded.mtime_ms,
           size_bytes=excluded.size_bytes, active=1, updated_at=excluded.updated_at`,
		scopeKey, path, kind, title, summary, text, nullBytes(embedding), hash, mtimeMS, sizeBytes, now)
	return err
}

func fileHash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}

func extKind(ext string) string {
	switch ext {
	case ".md":
		return "markdown"
	case ".txt":
		return "text"
	default:
		return "text"
	}
}

func extractSummary(text string) string {
	for _, line := range strings.SplitN(text, "\n", 20) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "---") {
			continue
		}
		if len(line) > 200 {
			line = line[:200]
		}
		return line
	}
	return ""
}

func truncateForEmbed(text string, max int) string {
	if max <= 0 || len(text) <= max {
		return text
	}
	return text[:max]
}

func excerptText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars] + "…"
}

func nullBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
````

## File: internal/memory/scheduler.go
````go
package memory

import (
	"context"
	"sync"
	"time"
)

type Scheduler struct {
	timeout time.Duration
	run     func(context.Context, string)
	baseCtx context.Context

	mu       sync.Mutex
	sessions map[string]*schedulerState
}

type schedulerState struct {
	running bool
	dirty   bool
}

func NewScheduler(timeout time.Duration, run func(context.Context, string)) *Scheduler {
	return NewSchedulerWithContext(context.Background(), timeout, run)
}

func NewSchedulerWithContext(baseCtx context.Context, timeout time.Duration, run func(context.Context, string)) *Scheduler {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	return &Scheduler{
		timeout:  timeout,
		run:      run,
		baseCtx:  baseCtx,
		sessions: map[string]*schedulerState{},
	}
}

func (s *Scheduler) Trigger(sessionKey string) {
	if s == nil || s.run == nil || sessionKey == "" {
		return
	}
	s.mu.Lock()
	state, ok := s.sessions[sessionKey]
	if !ok {
		state = &schedulerState{}
		s.sessions[sessionKey] = state
	}
	if state.running {
		state.dirty = true
		s.mu.Unlock()
		return
	}
	state.running = true
	state.dirty = false
	s.mu.Unlock()

	go s.runLoop(sessionKey)
}

func (s *Scheduler) runLoop(sessionKey string) {
	for {
		base := s.baseCtx
		if base == nil {
			base = context.Background()
		}
		ctx, cancel := context.WithTimeout(base, s.timeout)
		s.run(ctx, sessionKey)
		cancel()

		s.mu.Lock()
		state := s.sessions[sessionKey]
		if state == nil {
			s.mu.Unlock()
			return
		}
		if state.dirty {
			state.dirty = false
			s.mu.Unlock()
			continue
		}
		delete(s.sessions, sessionKey)
		s.mu.Unlock()
		return
	}
}
````

## File: internal/memory/workspace_context.go
````go
package memory

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultWorkspaceContextMaxFileBytes = 32 * 1024
	defaultWorkspaceContextMaxResults   = 6
	defaultWorkspaceContextMaxChars     = 6000
	defaultWorkspaceContextScanLimit    = 200
	workspaceContextCacheTTL            = 5 * time.Second
)

type workspaceContextCacheKey struct {
	root         string
	query        string
	maxFileBytes int
	maxResults   int
	maxChars     int
}

type workspaceContextCacheEntry struct {
	text      string
	expiresAt time.Time
}

var workspaceContextCache = struct {
	mu      sync.Mutex
	entries map[workspaceContextCacheKey]workspaceContextCacheEntry
}{entries: map[workspaceContextCacheKey]workspaceContextCacheEntry{}}

type WorkspaceContextConfig struct {
	WorkspaceDir string
	MaxFileBytes int
	MaxResults   int
	MaxChars     int
	Now          time.Time
}

type workspaceCandidate struct {
	Path    string
	Excerpt string
	Score   int
	ModTime time.Time
}

func BuildWorkspaceContext(cfg WorkspaceContextConfig, query string) string {
	root := strings.TrimSpace(cfg.WorkspaceDir)
	if root == "" {
		return ""
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return ""
	}
	maxFileBytes := cfg.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = defaultWorkspaceContextMaxFileBytes
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultWorkspaceContextMaxResults
	}
	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = defaultWorkspaceContextMaxChars
	}
	cacheKey := workspaceContextCacheKey{
		root:         realRoot,
		query:        strings.TrimSpace(strings.ToLower(query)),
		maxFileBytes: maxFileBytes,
		maxResults:   maxResults,
		maxChars:     maxChars,
	}
	now := cfg.Now
	if now.IsZero() {
		now = time.Now()
	}
	if cached, ok := getWorkspaceContextCache(cacheKey, now); ok {
		return cached
	}

	seen := map[string]struct{}{}
	candidates := make([]workspaceCandidate, 0, maxResults)
	appendCandidate := func(candidate workspaceCandidate) {
		candidate.Path = strings.TrimSpace(candidate.Path)
		candidate.Excerpt = strings.TrimSpace(candidate.Excerpt)
		if candidate.Path == "" || candidate.Excerpt == "" {
			return
		}
		if _, exists := seen[candidate.Path]; exists {
			return
		}
		seen[candidate.Path] = struct{}{}
		candidates = append(candidates, candidate)
	}

	for _, name := range []string{"README.md", "TODO.md", "TASKS.md", "PLAN.md", "STATUS.md", "NOTES.md", "PROJECT.md"} {
		candidate, ok := workspaceFileCandidate(realRoot, filepath.Join(realRoot, name), maxFileBytes, nil)
		if ok {
			appendCandidate(candidate)
		}
	}
	for _, candidate := range recentMemoryCandidates(realRoot, now, maxFileBytes) {
		appendCandidate(candidate)
	}
	tokens := workspaceQueryTokens(query)
	if len(tokens) > 0 {
		for _, candidate := range relevantWorkspaceCandidates(realRoot, tokens, maxFileBytes, maxResults, seen) {
			appendCandidate(candidate)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	var out strings.Builder
	out.WriteString("Startup workspace context gathered before the model call.\n")
	for i, candidate := range candidates {
		out.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, relativeDisplayPath(realRoot, candidate.Path), workspaceOneLine(candidate.Excerpt, 320)))
	}
	text := workspaceTruncate(strings.TrimSpace(out.String()), maxChars)
	setWorkspaceContextCache(cacheKey, text, now)
	return text
}

func getWorkspaceContextCache(key workspaceContextCacheKey, now time.Time) (string, bool) {
	workspaceContextCache.mu.Lock()
	defer workspaceContextCache.mu.Unlock()
	entry, ok := workspaceContextCache.entries[key]
	if !ok {
		return "", false
	}
	if !entry.expiresAt.After(now) {
		delete(workspaceContextCache.entries, key)
		return "", false
	}
	return entry.text, true
}

func setWorkspaceContextCache(key workspaceContextCacheKey, text string, now time.Time) {
	workspaceContextCache.mu.Lock()
	defer workspaceContextCache.mu.Unlock()
	workspaceContextCache.entries[key] = workspaceContextCacheEntry{text: text, expiresAt: now.Add(workspaceContextCacheTTL)}
}

func recentMemoryCandidates(root string, now time.Time, maxFileBytes int) []workspaceCandidate {
	memoryDir := filepath.Join(root, "memory")
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		return nil
	}
	preferred := map[string]struct{}{
		now.Format("2006-01-02") + ".md":                    {},
		now.Add(-24*time.Hour).Format("2006-01-02") + ".md": {},
	}
	var selected []workspaceCandidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := preferred[entry.Name()]; !ok {
			continue
		}
		candidate, ok := workspaceFileCandidate(root, filepath.Join(memoryDir, entry.Name()), maxFileBytes, nil)
		if ok {
			selected = append(selected, candidate)
		}
	}
	if len(selected) > 0 {
		sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
		return selected
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(memoryDir, entry.Name()), modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	if len(files) > 2 {
		files = files[:2]
	}
	out := make([]workspaceCandidate, 0, len(files))
	for _, file := range files {
		candidate, ok := workspaceFileCandidate(root, file.path, maxFileBytes, nil)
		if ok {
			out = append(out, candidate)
		}
	}
	return out
}

func relevantWorkspaceCandidates(root string, tokens []string, maxFileBytes, maxResults int, seen map[string]struct{}) []workspaceCandidate {
	var candidates []workspaceCandidate
	visited := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		if visited >= defaultWorkspaceContextScanLimit {
			return fs.SkipAll
		}
		visited++
		if _, exists := seen[path]; exists {
			return nil
		}
		if !isWorkspaceContextFile(path) || isBootstrapWorkspaceFile(path) {
			return nil
		}
		candidate, ok := workspaceFileCandidate(root, path, maxFileBytes, tokens)
		if !ok || candidate.Score <= 0 {
			return nil
		}
		candidates = append(candidates, candidate)
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			if candidates[i].ModTime.Equal(candidates[j].ModTime) {
				return candidates[i].Path < candidates[j].Path
			}
			return candidates[i].ModTime.After(candidates[j].ModTime)
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	return candidates
}

func workspaceFileCandidate(root, path string, maxFileBytes int, tokens []string) (workspaceCandidate, bool) {
	resolved, ok := workspaceSafePath(root, path)
	if !ok {
		return workspaceCandidate{}, false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return workspaceCandidate{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxFileBytes)))
	if err != nil {
		return workspaceCandidate{}, false
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return workspaceCandidate{}, false
	}
	info, err := f.Stat()
	if err != nil {
		return workspaceCandidate{}, false
	}
	excerpt, score := workspaceExcerpt(resolved, text, tokens)
	if len(tokens) == 0 {
		score = 1
	}
	recencyBonus := workspaceRecencyScore(info.ModTime())
	return workspaceCandidate{Path: resolved, Excerpt: excerpt, Score: score + recencyBonus, ModTime: info.ModTime()}, true
}

func workspaceSafePath(root, path string) (string, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return realPath, true
}

func workspaceExcerpt(path, text string, tokens []string) (string, int) {
	one := workspaceOneLine(text, 500)
	if len(tokens) == 0 {
		return one, 0
	}
	lowerPath := strings.ToLower(path)
	lowerText := strings.ToLower(text)
	best := -1
	score := 0
	for _, token := range tokens {
		if strings.Contains(lowerPath, token) {
			score += 6
		}
		if idx := strings.Index(lowerText, token); idx >= 0 {
			score += 3
			if best < 0 || idx < best {
				best = idx
			}
		}
	}
	if best < 0 {
		return one, score
	}
	start := best - 120
	if start < 0 {
		start = 0
	}
	end := best + 220
	if end > len(text) {
		end = len(text)
	}
	excerpt := strings.TrimSpace(text[start:end])
	if start > 0 {
		excerpt = "…" + excerpt
	}
	if end < len(text) {
		excerpt += "…"
	}
	return workspaceOneLine(excerpt, 500), score
}

func workspaceRecencyScore(modTime time.Time) int {
	if modTime.IsZero() {
		return 0
	}
	age := time.Since(modTime)
	switch {
	case age <= 24*time.Hour:
		return 3
	case age <= 7*24*time.Hour:
		return 2
	case age <= 30*24*time.Hour:
		return 1
	default:
		return 0
	}
}

func workspaceQueryTokens(query string) []string {
	raw := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {}, "from": {}, "into": {},
		"what": {}, "when": {}, "where": {}, "have": {}, "just": {}, "please": {}, "about": {},
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		if len(token) < 3 {
			continue
		}
		if _, blocked := stop[token]; blocked {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func isWorkspaceContextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt":
		return true
	default:
		return false
	}
}

func isBootstrapWorkspaceFile(path string) bool {
	name := strings.ToUpper(filepath.Base(path))
	switch name {
	case "SOUL.MD", "AGENTS.MD", "TOOLS.MD", "IDENTITY.MD", "MEMORY.MD", "HEARTBEAT.MD":
		return true
	default:
		return false
	}
}

func relativeDisplayPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func workspaceOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

func workspaceTruncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "\n…[truncated]"
	}
	return s
}
````

## File: internal/tools/spawn.go
````go
package tools

import (
	"context"
	"fmt"
	"strings"
)

type SpawnRequest struct {
	ParentSessionKey string
	ProfileName      string
	Task             string
	Channel          string
	To               string
}

type SpawnJob struct {
	ID              string
	ChildSessionKey string
}

type SpawnEnqueuer interface {
	Enqueue(ctx context.Context, req SpawnRequest) (SpawnJob, error)
}

type SpawnSubagent struct {
	Base
	Manager        SpawnEnqueuer
	DefaultChannel string
	DefaultTo      string
}

func (t *SpawnSubagent) Capability() CapabilityLevel { return CapabilityGuarded }

func (t *SpawnSubagent) Name() string { return "spawn_subagent" }

func (t *SpawnSubagent) Description() string {
	return "Queue a longer background task and return immediately with a stable job ID."
}

func (t *SpawnSubagent) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":    map[string]any{"type": "string", "description": "Task for the background subagent"},
			"channel": map[string]any{"type": "string", "description": "Optional delivery channel override"},
			"to":      map[string]any{"type": "string", "description": "Optional recipient override"},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnSubagent) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *SpawnSubagent) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Manager == nil {
		return "", fmt.Errorf("background subagents disabled")
	}
	task := readOptionalString(params, "task")
	if task == "" {
		return "", fmt.Errorf("empty task")
	}
	channel := readOptionalString(params, "channel")
	to := readOptionalString(params, "to")
	ctxChannel, ctxTo := DeliveryFromContext(ctx)
	if channel == "" {
		channel = firstNonEmpty(ctxChannel, t.DefaultChannel)
	}
	if to == "" {
		to = firstNonEmpty(ctxTo, t.DefaultTo)
	}
	job, err := t.Manager.Enqueue(ctx, SpawnRequest{
		ParentSessionKey: SessionFromContext(ctx),
		ProfileName:      ActiveProfileFromContext(ctx).Name,
		Task:             task,
		Channel:          channel,
		To:               to,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("queued background job_id=%s", job.ID), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readOptionalString(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	v, ok := params[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}
````

## File: internal/triggers/filewatch.go
````go
package triggers

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

const structuredFileReadMaxBytes = 64 * 1024

type FileWatcher struct {
	Config     config.FileWatchConfig
	Bus        *bus.Bus
	SessionKey string

	mu     sync.Mutex
	last   map[string]fileState
	cancel context.CancelFunc
}

type fileState struct {
	mtime  time.Time
	size   int64
	lastEv time.Time // last time we published an event for this path
}

func NewFileWatcher(cfg config.FileWatchConfig, b *bus.Bus, sessionKey string) *FileWatcher {
	return &FileWatcher{
		Config:     cfg,
		Bus:        b,
		SessionKey: sessionKey,
		last:       map[string]fileState{},
	}
}

func (fw *FileWatcher) Start(ctx context.Context) {
	if !fw.Config.Enabled || len(fw.Config.Paths) == 0 {
		return
	}
	pollInterval := time.Duration(fw.Config.PollSeconds) * time.Second
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	ctx, fw.cancel = context.WithCancel(ctx)
	go fw.loop(ctx, pollInterval)
}

func (fw *FileWatcher) Stop() {
	if fw.cancel != nil {
		fw.cancel()
	}
}

func (fw *FileWatcher) loop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fw.poll(ctx)
		}
	}
}

func (fw *FileWatcher) poll(ctx context.Context) {
	debounce := time.Duration(fw.Config.DebounceSeconds) * time.Second
	if debounce <= 0 {
		debounce = 2 * time.Second
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	now := time.Now()
	for _, p := range fw.Config.Paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		absPath, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// Don't follow symlinks
		info, err := os.Lstat(absPath)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		prev, seen := fw.last[absPath]
		cur := fileState{mtime: info.ModTime(), size: info.Size()}
		if seen {
			// Check if changed
			if cur.mtime == prev.mtime && cur.size == prev.size {
				continue
			}
			// Debounce: don't republish if we published recently
			if now.Sub(prev.lastEv) < debounce {
				// update state but don't publish yet
				fw.last[absPath] = fileState{mtime: cur.mtime, size: cur.size, lastEv: prev.lastEv}
				continue
			}
		}
		cur.lastEv = now
		fw.last[absPath] = cur
		if !seen {
			// First observation - record baseline with zero lastEv so debounce
			// does not prevent the first change event from being published.
			fw.last[absPath] = fileState{mtime: cur.mtime, size: cur.size}
			continue
		}
		// Publish event
		meta := map[string]any{
			"path":  absPath,
			"size":  info.Size(),
			"mtime": info.ModTime().UnixMilli(),
			MetaKeyStructuredEvent: StructuredEventMap(StructuredEvent{
				Type:    string(bus.EventFileChange),
				Source:  "filewatch",
				Trusted: true,
				Details: map[string]any{"path": absPath, "size": info.Size(), "mtime": info.ModTime().UnixMilli()},
			}),
		}
		if info.Size() > 0 && info.Size() <= structuredFileReadMaxBytes {
			if data, readErr := os.ReadFile(absPath); readErr == nil {
				if structuredTasks, ok := ParseStructuredTasksText(string(data)); ok {
					meta[MetaKeyStructuredTasks] = StructuredTasksMap(structuredTasks)
				}
			}
		}
		ev := bus.Event{
			Type:       bus.EventFileChange,
			SessionKey: fw.SessionKey,
			Channel:    "filewatch",
			From:       absPath,
			Message:    "file changed: " + absPath,
			Meta:       meta,
		}
		if ok := fw.Bus.Publish(ev); !ok {
			log.Printf("filewatch: bus full, dropping event for %s", absPath)
		}
	}
}
````

## File: internal/triggers/webhook.go
````go
package triggers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type WebhookServer struct {
	Config     config.WebhookConfig
	Bus        *bus.Bus
	SessionKey string
	server     *http.Server
}

const structuredBodyPreviewMaxChars = 512

func NewWebhookServer(cfg config.WebhookConfig, b *bus.Bus, sessionKey string) *WebhookServer {
	return &WebhookServer{Config: cfg, Bus: b, SessionKey: sessionKey}
}

func (w *WebhookServer) Start(ctx context.Context) error {
	if !w.Config.Enabled || strings.TrimSpace(w.Config.Secret) == "" {
		return nil
	}
	addr := strings.TrimSpace(w.Config.Addr)
	if addr == "" {
		addr = "127.0.0.1:8765"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", w.handle)
	mux.HandleFunc("/webhook/", w.handle)
	w.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("webhook listen %s: %w", addr, err)
	}
	go func() {
		if err := w.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("webhook server error: %v", err)
		}
	}()
	return nil
}

func (w *WebhookServer) Stop(ctx context.Context) error {
	if w.server == nil {
		return nil
	}
	return w.server.Shutdown(ctx)
}

func (w *WebhookServer) handle(rw http.ResponseWriter, r *http.Request) {
	maxBytes := int64(w.Config.MaxBodyKB) * 1024
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		http.Error(rw, "read error", http.StatusInternalServerError)
		return
	}
	if int64(len(body)) > maxBytes {
		http.Error(rw, "request too large", http.StatusRequestEntityTooLarge)
		return
	}

	if !w.authenticate(r, body) {
		http.Error(rw, "unauthorized", http.StatusUnauthorized)
		return
	}

	route := strings.TrimPrefix(r.URL.Path, "/webhook")
	route = strings.TrimPrefix(route, "/")
	preview := strings.TrimSpace(string(body))
	if len(preview) > structuredBodyPreviewMaxChars {
		preview = preview[:structuredBodyPreviewMaxChars] + "...[truncated]"
	}

	ev := bus.Event{
		Type:       bus.EventWebhook,
		SessionKey: w.SessionKey,
		Channel:    "webhook",
		From:       r.RemoteAddr,
		Message:    string(body),
		Meta: map[string]any{
			"route":        route,
			"content_type": r.Header.Get("Content-Type"),
			"x-request-id": r.Header.Get("X-Request-ID"),
			MetaKeyStructuredEvent: StructuredEventMap(StructuredEvent{
				Type:    string(bus.EventWebhook),
				Source:  "webhook",
				Trusted: false,
				Details: map[string]any{
					"route":        route,
					"content_type": r.Header.Get("Content-Type"),
					"request_id":   r.Header.Get("X-Request-ID"),
					"remote_addr":  r.RemoteAddr,
					"body_preview": preview,
					"body_bytes":   len(body),
				},
			}),
		},
	}
	if structuredTasks, ok := ParseStructuredTasksText(string(body)); ok {
		ev.Meta[MetaKeyStructuredTasks] = StructuredTasksMap(structuredTasks)
	}
	if ok := w.Bus.Publish(ev); !ok {
		http.Error(rw, "bus full", http.StatusServiceUnavailable)
		return
	}
	rw.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(rw, "ok")
}

func (w *WebhookServer) authenticate(r *http.Request, body []byte) bool {
	secret := w.Config.Secret
	if secret == "" {
		return false
	}
	// Check HMAC-SHA256 in X-Hub-Signature-256
	sig := r.Header.Get("X-Hub-Signature-256")
	if strings.HasPrefix(sig, "sha256=") {
		sig = strings.TrimPrefix(sig, "sha256=")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := hex.EncodeToString(mac.Sum(nil))
		return hmac.Equal([]byte(sig), []byte(expected))
	}
	// Fall back to simple shared secret in X-Webhook-Secret header
	return r.Header.Get("X-Webhook-Secret") == secret
}
````

## File: internal/artifacts/store.go
````go
package artifacts

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/db"
)

type Store struct {
	Dir string
	DB  *db.DB
}

func (s *Store) Save(ctx context.Context, sessionKey, mime string, data []byte) (string, error) {
	if s.Dir == "" {
		return "", fmt.Errorf("artifacts dir not set")
	}
	if s.DB == nil {
		return "", fmt.Errorf("artifacts db not set")
	}
	if err := s.DB.EnsureSession(ctx, strings.TrimSpace(sessionKey)); err != nil {
		return "", err
	}
	_ = os.MkdirAll(s.Dir, 0o700)
	id := randID()
	path := filepath.Join(s.Dir, id)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	_, err := s.DB.SQL.ExecContext(ctx,
		`INSERT INTO artifacts(id, session_key, mime, path, size_bytes, created_at) VALUES(?,?,?,?,?,?)`,
		id, sessionKey, mime, path, len(data), time.Now().UnixMilli())
	if err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return id, nil
}

func (s *Store) SaveNamed(ctx context.Context, sessionKey, filename, mimeType string, data []byte) (Attachment, error) {
	filename = NormalizeFilename(filename, mimeType)
	id, err := s.Save(ctx, sessionKey, mimeType, data)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{
		ArtifactID: id,
		Filename:   filename,
		Mime:       strings.TrimSpace(mimeType),
		Kind:       DetectKind(filename, mimeType),
		SizeBytes:  int64(len(data)),
	}, nil
}

func (s *Store) Lookup(ctx context.Context, artifactID string) (StoredArtifact, error) {
	if s.DB == nil {
		return StoredArtifact{}, fmt.Errorf("artifacts db not set")
	}
	row := s.DB.SQL.QueryRowContext(ctx,
		`SELECT id, session_key, mime, path, size_bytes FROM artifacts WHERE id=?`,
		strings.TrimSpace(artifactID),
	)
	var stored StoredArtifact
	if err := row.Scan(&stored.ID, &stored.SessionKey, &stored.Mime, &stored.Path, &stored.SizeBytes); err != nil {
		if err == sql.ErrNoRows {
			return StoredArtifact{}, fmt.Errorf("artifact not found: %s", artifactID)
		}
		return StoredArtifact{}, err
	}
	return stored, nil
}

func randID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
````

## File: internal/bus/bus.go
````go
package bus

import (
	"context"
)

type EventType string

const (
	EventUserMessage EventType = "user_message"
	EventCron        EventType = "cron"
	EventHeartbeat   EventType = "heartbeat"
	EventSystem      EventType = "system"
	EventWebhook     EventType = "webhook"
	EventFileChange  EventType = "file_change"
)

type Event struct {
	Type       EventType
	SessionKey string
	Channel    string
	From       string
	Message    string
	Meta       map[string]any
}

type Handler func(ctx context.Context, ev Event) error

type Bus struct {
	ch chan Event
}

func New(buffer int) *Bus {
	if buffer <= 0 {
		buffer = 128
	}
	return &Bus{ch: make(chan Event, buffer)}
}

func (b *Bus) Publish(ev Event) bool {
	select {
	case b.ch <- ev:
		return true
	default:
		return false
	}
}
func (b *Bus) Channel() <-chan Event { return b.ch }
````

## File: internal/channels/cli/deliver.go
````go
package cli

import (
	"context"
	"fmt"
	"strings"

	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
)

// Deliverer handles final and streaming output to the CLI terminal.
type Deliverer struct {
	Spinner *Spinner // shared with Channel; stopped before any output
}

func (Deliverer) Name() string { return "cli" }

func (Deliverer) Start(ctx context.Context, eventBus *bus.Bus) error { return nil }

func (Deliverer) Stop(ctx context.Context) error { return nil }

func (d Deliverer) Deliver(ctx context.Context, channel, to, text string) error {
	d.stopSpinner()
	fmt.Print(FormatResponse(text))
	fmt.Println()
	fmt.Println()
	if sep := Separator(); sep != "" {
		fmt.Println(sep)
	}
	ShowPrompt()
	return nil
}

func (d Deliverer) stopSpinner() {
	if d.Spinner != nil {
		d.Spinner.Stop()
	}
}

// ──────────────────────── streaming ────────────────────────

// CLIStreamWriter renders incremental text deltas to stdout with styling.
type CLIStreamWriter struct {
	started bool
	closed  bool
	aborted bool
	spinner *Spinner
}

func (w *CLIStreamWriter) WriteDelta(ctx context.Context, text string) error {
	if w.closed || w.aborted {
		return nil
	}
	if !w.started {
		// Stop the spinner and print the response header on the first delta.
		if w.spinner != nil {
			w.spinner.Stop()
		}
		w.started = true
		fmt.Print(ResponsePrefix())
	}
	// Indent any embedded newlines so multi-line streamed text stays aligned.
	if isTTY {
		text = strings.ReplaceAll(text, "\n", "\n    ")
	}
	fmt.Print(text)
	return nil
}

func (w *CLIStreamWriter) Close(ctx context.Context, finalText string) error {
	if w.aborted {
		return nil
	}
	w.closed = true
	if w.started {
		// End the streamed block with spacing.
		fmt.Println()
		fmt.Println()
		if sep := Separator(); sep != "" {
			fmt.Println(sep)
		}
		ShowPrompt()
	} else if strings.TrimSpace(finalText) != "" {
		// Nothing was streamed — print the full response now.
		if w.spinner != nil {
			w.spinner.Stop()
		}
		fmt.Print(FormatResponse(finalText))
		fmt.Println()
		fmt.Println()
		if sep := Separator(); sep != "" {
			fmt.Println(sep)
		}
		ShowPrompt()
	}
	// If not started AND no text, do nothing (tool-call turn — spinner may keep running).
	return nil
}

func (w *CLIStreamWriter) Abort(ctx context.Context) error {
	w.aborted = true
	if w.started {
		fmt.Println()
		fmt.Println(style(ansiYellow, "  ⚠ [aborted]"))
		ShowPrompt()
	}
	// If not started, leave spinner untouched so it carries through tool-call loops.
	return nil
}

// BeginStream implements channels.StreamingChannel.
func (d Deliverer) BeginStream(ctx context.Context, to string, meta map[string]any) (channels.StreamWriter, error) {
	return &CLIStreamWriter{spinner: d.Spinner}, nil
}
````

## File: internal/channels/whatsapp/whatsapp.go
````go
package whatsapp

import (
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
)

type Channel struct {
	Config        config.WhatsAppBridgeConfig
	Dialer        *websocket.Dialer
	Artifacts     *artifacts.Store
	MaxMediaBytes int
	IsolatePeers  bool

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	closed bool
}

func (c *Channel) Name() string { return "whatsapp" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.BridgeURL) == "" {
		return fmt.Errorf("whatsapp bridge url not configured")
	}
	conn, err := c.connect(ctx)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
	c.closed = false
	c.mu.Unlock()
	go c.readLoop(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.cancel = nil
	c.closed = true
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	target := strings.TrimSpace(to)
	if target == "" {
		target = strings.TrimSpace(c.Config.DefaultTo)
	}
	if target == "" {
		return fmt.Errorf("whatsapp target required")
	}
	cmd := map[string]any{"type": "send", "to": target, "text": text}
	if mediaPaths := rootchannels.MediaPaths(meta); len(mediaPaths) > 0 {
		attachments, err := c.outboundAttachments(mediaPaths)
		if err != nil {
			return err
		}
		cmd["attachments"] = attachments
	}
	if meta != nil {
		for k, v := range meta {
			if k == "media_paths" {
				continue
			}
			cmd[k] = v
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("whatsapp bridge not connected")
	}
	return c.conn.WriteJSON(cmd)
}

func (c *Channel) connect(ctx context.Context) (*websocket.Conn, error) {
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	headers := http.Header{}
	if token := strings.TrimSpace(c.Config.BridgeToken); token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := dialer.DialContext(ctx, c.Config.BridgeURL, headers)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		var msg inboundMessage
		if err := conn.ReadJSON(&msg); err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				return
			}
		}
		if msg.Type != "message" {
			continue
		}
		if !c.allowedFrom(msg.From) {
			continue
		}
		target := strings.TrimSpace(msg.Chat)
		if target == "" {
			target = strings.TrimSpace(msg.From)
		}
		sessionKey := "whatsapp:" + target
		if c.IsolatePeers {
			sessionKey += ":" + strings.TrimSpace(msg.From)
		}
		attachments, markers := c.captureAttachments(ctx, sessionKey, msg.Attachments)
		content := rootchannels.ComposeMessageText(msg.Text, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{
			"chat_id":             target,
			"message_id":          msg.ID,
			"reply_to_message_id": msg.ID,
			"is_group":            msg.IsGroup,
		}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: sessionKey,
			Channel:    "whatsapp",
			From:       msg.From,
			Message:    content,
			Meta:       meta,
		})
	}
}

func (c *Channel) allowedFrom(from string) bool {
	if len(c.Config.AllowedFrom) == 0 {
		return c.Config.OpenAccess
	}
	for _, allowed := range c.Config.AllowedFrom {
		if strings.TrimSpace(allowed) == strings.TrimSpace(from) {
			return true
		}
	}
	return false
}

type inboundMessage struct {
	Type        string             `json:"type"`
	ID          string             `json:"id"`
	Chat        string             `json:"chat"`
	From        string             `json:"from"`
	Text        string             `json:"text"`
	IsGroup     bool               `json:"isGroup"`
	Attachments []bridgeAttachment `json:"attachments"`
}

type bridgeAttachment struct {
	Path       string `json:"path,omitempty"`
	DataBase64 string `json:"data_base64,omitempty"`
	Filename   string `json:"filename,omitempty"`
	Mime       string `json:"mime,omitempty"`
	Kind       string `json:"kind,omitempty"`
	SizeBytes  int64  `json:"size_bytes,omitempty"`
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, refs []bridgeAttachment) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(refs))
	markers := make([]string, 0, len(refs))
	for _, ref := range refs {
		filename := artifacts.NormalizeFilename(ref.Filename, ref.Mime)
		kind := strings.TrimSpace(ref.Kind)
		if kind == "" {
			kind = artifacts.DetectKind(filename, ref.Mime)
		}
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && ref.SizeBytes > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := decodeBridgeAttachment(ref, c.MaxMediaBytes)
		if err != nil {
			reason := "invalid media payload"
			if strings.Contains(err.Error(), "too large") {
				reason = "too large"
			}
			markers = append(markers, artifacts.FailureMarker(kind, filename, reason))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, ref.Mime, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) outboundAttachments(paths []string) ([]bridgeAttachment, error) {
	attachments := make([]bridgeAttachment, 0, len(paths))
	for _, mediaPath := range paths {
		info, err := os.Stat(mediaPath)
		if err != nil {
			return nil, err
		}
		if c.MaxMediaBytes == 0 {
			return nil, fmt.Errorf("media attachments disabled by config")
		}
		if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
			return nil, fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
		}
		data, err := os.ReadFile(mediaPath)
		if err != nil {
			return nil, err
		}
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(mediaPath)))
		attachments = append(attachments, bridgeAttachment{
			DataBase64: base64.StdEncoding.EncodeToString(data),
			Filename:   filepath.Base(mediaPath),
			Mime:       mimeType,
			Kind:       artifacts.DetectKind(mediaPath, mimeType),
			SizeBytes:  info.Size(),
		})
	}
	return attachments, nil
}

func decodeBridgeAttachment(ref bridgeAttachment, maxBytes int) ([]byte, error) {
	raw := strings.TrimSpace(ref.DataBase64)
	if raw == "" {
		return nil, fmt.Errorf("missing inline data")
	}
	if maxBytes > 0 && base64.StdEncoding.DecodedLen(len(raw)) > maxBytes {
		return nil, fmt.Errorf("attachment too large")
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	if maxBytes > 0 && len(data) > maxBytes {
		return nil, fmt.Errorf("attachment too large")
	}
	return data, nil
}

func BridgeURL(base string) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil {
		return ""
	}
	if u.Path == "" {
		u.Path = "/ws"
	}
	return u.String()
}

func NewTestDialer() *websocket.Dialer {
	return &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
}
````

## File: internal/cron/cron.go
````go
package cron

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

type ScheduleKind string
const (
	KindAt ScheduleKind = "at"
	KindEvery ScheduleKind = "every"
	KindCron ScheduleKind = "cron"
)

type CronSchedule struct {
	Kind ScheduleKind `json:"kind"`
	AtMS int64 `json:"at_ms,omitempty"`
	EveryMS int64 `json:"every_ms,omitempty"`
	Expr string `json:"expr,omitempty"`
	TZ string `json:"tz,omitempty"`
}

type CronPayload struct {
	Kind       string `json:"kind"` // "agent_turn"|"system_event"
	Message    string `json:"message"`
	Deliver    bool   `json:"deliver"`
	Channel    string `json:"channel,omitempty"`
	To         string `json:"to,omitempty"`
	SessionKey string `json:"session_key,omitempty"` // optional per-job session key override
}

type CronJobState struct {
	NextRunAtMS *int64 `json:"next_run_at_ms,omitempty"`
	LastRunAtMS *int64 `json:"last_run_at_ms,omitempty"`
	LastStatus string `json:"last_status,omitempty"` // ok|error|skipped
	LastError string `json:"last_error,omitempty"`
}

type CronJob struct {
	ID string `json:"id"`
	Name string `json:"name"`
	Enabled bool `json:"enabled"`
	Schedule CronSchedule `json:"schedule"`
	Payload CronPayload `json:"payload"`
	State CronJobState `json:"state"`
	CreatedAtMS int64 `json:"created_at_ms"`
	UpdatedAtMS int64 `json:"updated_at_ms"`
	DeleteAfterRun bool `json:"delete_after_run"`
}

type Store struct {
	Version int `json:"version"`
	Jobs []CronJob `json:"jobs"`
}

type Runner func(ctx context.Context, job CronJob) error

type Service struct {
	mu sync.Mutex
	path string
	runner Runner
	c *cron.Cron
	entries map[string]cron.EntryID
}

func New(path string, runner Runner) *Service {
	return &Service{
		path: path,
		runner: runner,
		entries: map[string]cron.EntryID{},
	}
}

func (s *Service) load() (Store, error) {
	var st Store
	st.Version = 1
	b, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return st, nil
		}
		return st, err
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return st, err
	}
	return st, nil
}

func (s *Service) save(st Store) error {
	if err := os.MkdirAll(filepathDir(s.path), 0o755); err != nil { return err }
	b, _ := json.MarshalIndent(st, "", "  ")
	return os.WriteFile(s.path, b, 0o644)
}

func filepathDir(p string) string {
	i := len(p)-1
	for i >= 0 && p[i] != '/' && p[i] != '\\' { i-- }
	if i <= 0 { return "." }
	return p[:i]
}

func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.c != nil { return nil }

	s.c = cron.New(cron.WithSeconds(), cron.WithParser(cron.NewParser(cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow)))
	st, err := s.load()
	if err != nil { return err }
	for _, j := range st.Jobs {
		s.armJobLocked(j)
	}
	s.c.Start()
	return nil
}

func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		ctx := s.c.Stop()
		<-ctx.Done()
		s.c = nil
		s.entries = map[string]cron.EntryID{}
	}
}

func (s *Service) Status() (map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return nil, err }
	next := int64(0)
	for _, j := range st.Jobs {
		if j.State.NextRunAtMS != nil {
			if next == 0 || *j.State.NextRunAtMS < next { next = *j.State.NextRunAtMS }
		}
	}
	var nextPtr *int64
	if next != 0 { nextPtr = &next }
	return map[string]any{"jobs": len(st.Jobs), "next_wake_at_ms": nextPtr}, nil
}

func (s *Service) List() ([]CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return nil, err }
	return st.Jobs, nil
}

func (s *Service) Add(job CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return err }
	now := time.Now().UnixMilli()
	job.CreatedAtMS = now
	job.UpdatedAtMS = now
	if job.ID == "" { job.ID = randID() }
	if job.Name == "" { job.Name = job.ID }
	st.Jobs = append(st.Jobs, job)
	if err := s.save(st); err != nil { return err }
	s.armJobLocked(job)
	return nil
}

func (s *Service) Remove(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil { return false, err }
	found := false
	n := make([]CronJob, 0, len(st.Jobs))
	for _, j := range st.Jobs {
		if j.ID == id {
			found = true
			if eid, ok := s.entries[id]; ok && s.c != nil {
				s.c.Remove(eid)
				delete(s.entries, id)
			}
			continue
		}
		n = append(n, j)
	}
	st.Jobs = n
	if err := s.save(st); err != nil { return false, err }
	return found, nil
}

func (s *Service) RunNow(ctx context.Context, id string, force bool) (bool, error) {
	s.mu.Lock()
	st, err := s.load()
	s.mu.Unlock()
	if err != nil { return false, err }
	for _, j := range st.Jobs {
		if j.ID == id {
			if !force && !j.Enabled { return false, nil }
			err := s.runner(ctx, j)
			s.mu.Lock()
			defer s.mu.Unlock()
			st2, loadErr := s.load()
			if loadErr != nil {
				return true, err
			}
			shouldDelete := false
			for i := range st2.Jobs {
				if st2.Jobs[i].ID == id {
					now := time.Now().UnixMilli()
					st2.Jobs[i].State.LastRunAtMS = &now
					if err != nil {
						st2.Jobs[i].State.LastStatus = "error"
						st2.Jobs[i].State.LastError = err.Error()
					} else {
						st2.Jobs[i].State.LastStatus = "ok"
						st2.Jobs[i].State.LastError = ""
					}
					if st2.Jobs[i].DeleteAfterRun {
						shouldDelete = true
						break
					}
					break
				}
			}
			if shouldDelete {
				next := make([]CronJob, 0, len(st2.Jobs))
				for _, jj := range st2.Jobs {
					if jj.ID == id { continue }
					next = append(next, jj)
				}
				st2.Jobs = next
				if eid, ok := s.entries[id]; ok && s.c != nil {
					s.c.Remove(eid)
					delete(s.entries, id)
				}
			}
			if saveErr := s.save(st2); saveErr != nil {
				log.Printf("cron save failed: %v", saveErr)
			}
			return true, err
		}
	}
	return false, nil
}

func (s *Service) armJobLocked(job CronJob) {
	if s.c == nil { return }
	if !job.Enabled { return }
	switch job.Schedule.Kind {
	case KindAt:
		at := time.UnixMilli(job.Schedule.AtMS)
		if time.Now().After(at) { return }
		delay := time.Until(at)
		// schedule using timer goroutine
		go func(id string, d time.Duration) {
			time.Sleep(d)
			if err := s.runner(context.Background(), job); err != nil {
				log.Printf("cron runner error: id=%s err=%v", id, err)
			}
		}(job.ID, delay)
	case KindEvery:
		sec := int64(job.Schedule.EveryMS / 1000)
		if sec <= 0 { sec = 60 }
		spec := "@every " + (time.Duration(sec) * time.Second).String()
		eid, err := s.c.AddFunc(spec, func() {
			if e := s.runner(context.Background(), job); e != nil {
				log.Printf("cron runner error: id=%s err=%v", job.ID, e)
			}
		})
		if err == nil {
			s.entries[job.ID] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", job.ID, spec, err)
		}
	case KindCron:
		spec := job.Schedule.Expr
		eid, err := s.c.AddFunc(spec, func() {
			if e := s.runner(context.Background(), job); e != nil {
				log.Printf("cron runner error: id=%s err=%v", job.ID, e)
			}
		})
		if err == nil {
			s.entries[job.ID] = eid
		} else {
			log.Printf("cron schedule add failed: id=%s spec=%s err=%v", job.ID, spec, err)
		}
	}
}

func randUint() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return binary.LittleEndian.Uint64(b[:])
}

func randID() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 10)
	for i := range b { b[i] = chars[int(randUint()%uint64(len(chars)))] }
	return string(b)
}
````

## File: internal/mcp/manager.go
````go
package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"or3-intern/internal/config"
	"or3-intern/internal/security"
	"or3-intern/internal/tools"
)

const maxResultChars = 64 * 1024

type session interface {
	Close() error
	ListTools(ctx context.Context, params *sdkmcp.ListToolsParams) (*sdkmcp.ListToolsResult, error)
	CallTool(ctx context.Context, params *sdkmcp.CallToolParams) (*sdkmcp.CallToolResult, error)
}

type connector func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error)

type Manager struct {
	servers    map[string]config.MCPServerConfig
	logf       func(string, ...any)
	connect    connector
	sessions   map[string]session
	tools      []remoteToolSpec
	hostPolicy security.HostPolicy
}

type remoteToolSpec struct {
	localName   string
	serverName  string
	remoteName  string
	description string
	parameters  map[string]any
	timeout     time.Duration
	session     session
}

type RemoteTool struct {
	tools.Base

	localName   string
	serverName  string
	remoteName  string
	description string
	parameters  map[string]any
	timeout     time.Duration
	session     session
}

func (t *RemoteTool) Capability() tools.CapabilityLevel { return tools.CapabilityGuarded }

func NewManager(servers map[string]config.MCPServerConfig) *Manager {
	cloned := make(map[string]config.MCPServerConfig, len(servers))
	for name, server := range servers {
		cloned[name] = server
	}
	mgr := &Manager{
		servers:  cloned,
		sessions: map[string]session{},
	}
	mgr.connect = func(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
		return connectSessionWithPolicy(ctx, name, cfg, mgr.hostPolicy)
	}
	return mgr
}

func (m *Manager) SetLogger(logf func(string, ...any)) {
	if m == nil {
		return
	}
	m.logf = logf
}

func (m *Manager) SetHostPolicy(policy security.HostPolicy) {
	if m == nil {
		return
	}
	m.hostPolicy = policy
}

func (m *Manager) ToolNames() []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m.tools))
	for _, spec := range m.tools {
		out = append(out, spec.localName)
	}
	sort.Strings(out)
	return out
}

func (m *Manager) Connect(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if len(m.tools) > 0 || len(m.sessions) > 0 {
		return nil
	}

	usedLocalNames := map[string]string{}
	for _, name := range enabledServerNames(m.servers) {
		cfg := m.servers[name]
		if m.hostPolicy.EnabledPolicy() && (cfg.Transport == "sse" || cfg.Transport == "streamablehttp") {
			if err := m.hostPolicy.ValidateEndpoint(ctx, cfg.URL); err != nil {
				m.logFailure(name, "host policy denied", err)
				continue
			}
		}
		sess, err := m.connect(ctx, name, cfg)
		if err != nil {
			m.logFailure(name, "connect failed", err)
			continue
		}

		remoteTools, err := listTools(ctx, sess, cfg)
		if err != nil {
			_ = sess.Close()
			m.logFailure(name, "tool discovery failed", err)
			continue
		}
		remoteTools = filterRemoteTools(name, remoteTools, m.logfSafe)
		sort.Slice(remoteTools, func(i, j int) bool {
			return strings.ToLower(remoteTools[i].Name) < strings.ToLower(remoteTools[j].Name)
		})

		added := 0
		for _, remote := range remoteTools {
			spec := newRemoteToolSpec(name, cfg, remote, sess)
			if previous, ok := usedLocalNames[spec.localName]; ok {
				m.logfSafe("mcp tool skipped: duplicate local name=%s remote=%s/%s previous=%s", spec.localName, name, remote.Name, previous)
				continue
			}
			usedLocalNames[spec.localName] = previousToolLabel(name, remote.Name)
			m.tools = append(m.tools, spec)
			added++
		}

		m.sessions[name] = sess
		m.logfSafe("mcp server connected: name=%s transport=%s tools=%d", name, cfg.Transport, added)
	}
	return nil
}

func (m *Manager) RegisterTools(reg *tools.Registry) int {
	if m == nil || reg == nil {
		return 0
	}
	for _, spec := range m.tools {
		reg.Register(spec.Tool())
	}
	return len(m.tools)
}

func (m *Manager) Close() error {
	if m == nil {
		return nil
	}
	var errs []error
	for name, sess := range m.sessions {
		if err := sess.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	m.sessions = map[string]session{}
	m.tools = nil
	return errors.Join(errs...)
}

func (m *Manager) logFailure(name, prefix string, err error) {
	if m == nil || m.logf == nil || err == nil {
		return
	}
	msg := strings.TrimSpace(err.Error())
	if len(msg) > 240 {
		msg = msg[:240] + "...[truncated]"
	}
	m.logf("mcp server unavailable: name=%s %s err=%s", name, prefix, msg)
}

func (m *Manager) logfSafe(format string, args ...any) {
	if m == nil || m.logf == nil {
		return
	}
	m.logf(format, args...)
}

func (s remoteToolSpec) Tool() tools.Tool {
	return &RemoteTool{
		localName:   s.localName,
		serverName:  s.serverName,
		remoteName:  s.remoteName,
		description: s.description,
		parameters:  cloneAnyMap(s.parameters),
		timeout:     s.timeout,
		session:     s.session,
	}
}

func (t *RemoteTool) Name() string { return t.localName }

func (t *RemoteTool) Description() string {
	if strings.TrimSpace(t.description) != "" {
		return t.description
	}
	return fmt.Sprintf("MCP tool %s from server %s.", t.remoteName, t.serverName)
}

func (t *RemoteTool) Parameters() map[string]any {
	return cloneAnyMap(t.parameters)
}

func (t *RemoteTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RemoteTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.session == nil {
		return "", fmt.Errorf("mcp %s/%s: session not connected", t.serverName, t.remoteName)
	}

	callCtx := ctx
	cancel := func() {}
	if t.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, t.timeout)
	}
	defer cancel()

	res, err := t.session.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      t.remoteName,
		Arguments: cloneAnyMap(params),
	})
	if err != nil {
		return "", fmt.Errorf("mcp %s/%s: %w", t.serverName, t.remoteName, err)
	}

	text := resultToText(res, maxResultChars)
	if res != nil && res.IsError {
		if strings.TrimSpace(text) == "" {
			text = "remote tool reported error"
		}
		return "", fmt.Errorf("mcp %s/%s: %s", t.serverName, t.remoteName, text)
	}
	return text, nil
}

func connectSession(ctx context.Context, name string, cfg config.MCPServerConfig) (session, error) {
	return connectSessionWithPolicy(ctx, name, cfg, security.HostPolicy{})
}

func connectSessionWithPolicy(ctx context.Context, _ string, cfg config.MCPServerConfig, policy security.HostPolicy) (session, error) {
	transport, err := buildTransportWithPolicy(cfg, policy)
	if err != nil {
		return nil, err
	}

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "or3-intern", Version: "v1"}, nil)
	connectCtx := ctx
	cancel := func() {}
	if cfg.ConnectTimeoutSeconds > 0 {
		connectCtx, cancel = context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds)*time.Second)
	}
	defer cancel()

	return client.Connect(connectCtx, transport, nil)
}

func buildTransport(cfg config.MCPServerConfig) (sdkmcp.Transport, error) {
	return buildTransportWithPolicy(cfg, security.HostPolicy{})
}

func buildTransportWithPolicy(cfg config.MCPServerConfig, policy security.HostPolicy) (sdkmcp.Transport, error) {
	switch cfg.Transport {
	case "stdio":
		cmd := exec.Command(cfg.Command, cfg.Args...)
		cmd.Env = tools.BuildChildEnv(os.Environ(), cfg.ChildEnvAllowlist, cfg.Env, "")
		return &sdkmcp.CommandTransport{Command: cmd}, nil
	case "sse":
		return &sdkmcp.SSEClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: buildHTTPClient(cfg, policy),
		}, nil
	case "streamablehttp":
		return &sdkmcp.StreamableClientTransport{
			Endpoint:   cfg.URL,
			HTTPClient: buildHTTPClient(cfg, policy),
			MaxRetries: -1,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %s", cfg.Transport)
	}
}

func buildHTTPClient(cfg config.MCPServerConfig, policy security.HostPolicy) *http.Client {
	timeout := time.Duration(cfg.ConnectTimeoutSeconds) * time.Second
	base := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
	}
	client := &http.Client{
		Transport: &headerRoundTripper{base: base, headers: cfg.Headers},
	}
	if policy.EnabledPolicy() {
		client.Transport = &headerRoundTripper{base: security.WrapHTTPClient(&http.Client{Transport: base}, policy).Transport, headers: cfg.Headers}
	}
	return client
}

func listTools(ctx context.Context, sess session, cfg config.MCPServerConfig) ([]*sdkmcp.Tool, error) {
	var out []*sdkmcp.Tool
	var cursor string
	for {
		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.ConnectTimeoutSeconds)*time.Second)
		res, err := sess.ListTools(reqCtx, &sdkmcp.ListToolsParams{Cursor: cursor})
		cancel()
		if err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		cursor = strings.TrimSpace(res.NextCursor)
		if cursor == "" {
			break
		}
	}
	return out, nil
}

func enabledServerNames(servers map[string]config.MCPServerConfig) []string {
	names := make([]string, 0, len(servers))
	for name, server := range servers {
		if server.Enabled {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
	return names
}

func newRemoteToolSpec(serverName string, cfg config.MCPServerConfig, remote *sdkmcp.Tool, sess session) remoteToolSpec {
	remoteName := ""
	description := ""
	var inputSchema any
	if remote != nil {
		remoteName = strings.TrimSpace(remote.Name)
		description = strings.TrimSpace(remote.Description)
		inputSchema = remote.InputSchema
	}
	return remoteToolSpec{
		localName:   localToolName(serverName, remoteName),
		serverName:  serverName,
		remoteName:  remoteName,
		description: description,
		parameters:  normalizeSchema(inputSchema),
		timeout:     time.Duration(cfg.ToolTimeoutSeconds) * time.Second,
		session:     sess,
	}
}

func filterRemoteTools(serverName string, remoteTools []*sdkmcp.Tool, logf func(string, ...any)) []*sdkmcp.Tool {
	filtered := make([]*sdkmcp.Tool, 0, len(remoteTools))
	for index, remote := range remoteTools {
		if remote == nil {
			if logf != nil {
				logf("mcp tool skipped: malformed entry server=%s index=%d reason=nil", serverName, index)
			}
			continue
		}
		if strings.TrimSpace(remote.Name) == "" {
			if logf != nil {
				logf("mcp tool skipped: malformed entry server=%s index=%d reason=missing-name", serverName, index)
			}
			continue
		}
		filtered = append(filtered, remote)
	}
	return filtered
}

func localToolName(serverName, remoteName string) string {
	return "mcp_" + sanitizeName(serverName) + "_" + sanitizeName(remoteName)
}

func sanitizeName(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return "unnamed"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unnamed"
	}
	return out
}

func normalizeSchema(schema any) map[string]any {
	if schema == nil {
		return defaultParameters()
	}
	if m, ok := schema.(map[string]any); ok && len(m) > 0 {
		return cloneAnyMap(m)
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return defaultParameters()
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || len(m) == 0 {
		return defaultParameters()
	}
	return m
}

func resultToText(res *sdkmcp.CallToolResult, limit int) string {
	if res == nil {
		return ""
	}
	var parts []string
	for _, content := range res.Content {
		if part := contentToText(content, limit); strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	if structured := structuredToText(res.StructuredContent); structured != "" {
		if len(parts) == 0 || strings.TrimSpace(parts[len(parts)-1]) != strings.TrimSpace(structured) {
			parts = append(parts, structured)
		}
	}
	return truncateResult(strings.Join(parts, "\n\n"), limit)
}

func structuredToText(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func contentToText(content sdkmcp.Content, limit int) string {
	switch block := content.(type) {
	case *sdkmcp.TextContent:
		return truncateResult(block.Text, limit)
	case *sdkmcp.ImageContent:
		return fmt.Sprintf("[image content omitted mime=%s bytes=%d]", block.MIMEType, len(block.Data))
	case *sdkmcp.AudioContent:
		return fmt.Sprintf("[audio content omitted mime=%s bytes=%d]", block.MIMEType, len(block.Data))
	case *sdkmcp.ResourceLink:
		return fmt.Sprintf("[resource link uri=%s name=%s]", block.URI, strings.TrimSpace(block.Name))
	case *sdkmcp.EmbeddedResource:
		if block.Resource == nil {
			return "[embedded resource omitted]"
		}
		if strings.TrimSpace(block.Resource.Text) != "" {
			return truncateResult(block.Resource.Text, limit)
		}
		if len(block.Resource.Blob) > 0 {
			return fmt.Sprintf("[embedded resource omitted uri=%s mime=%s bytes=%d]", block.Resource.URI, block.Resource.MIMEType, len(block.Resource.Blob))
		}
		return fmt.Sprintf("[embedded resource uri=%s]", block.Resource.URI)
	default:
		b, err := json.Marshal(content)
		if err != nil {
			return fmt.Sprintf("[unsupported MCP content %T]", content)
		}
		return truncateResult(string(b), limit)
	}
}

func truncateResult(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return strings.TrimSpace(text[:limit]) + "...[truncated]"
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func defaultParameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func mergeEnv(base []string, overrides map[string]string) []string {
	merged := make(map[string]string, len(base)+len(overrides))
	for _, raw := range base {
		key, value, ok := strings.Cut(raw, "=")
		if !ok || strings.TrimSpace(key) == "" {
			continue
		}
		merged[key] = value
	}
	for key, value := range overrides {
		if strings.TrimSpace(key) == "" {
			continue
		}
		merged[key] = value
	}
	if len(merged) == 0 {
		return []string{}
	}
	keys := make([]string, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+merged[key])
	}
	return out
}

func previousToolLabel(serverName, remoteName string) string {
	return serverName + "/" + remoteName
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (rt *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	cloned := req.Clone(req.Context())
	for key, value := range rt.headers {
		cloned.Header.Set(key, value)
	}
	return base.RoundTrip(cloned)
}
````

## File: internal/memory/consolidate.go
````go
package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

const defaultCanonicalMemoryKey = "long_term_memory"
const canonicalMemoryInputDivisor = 2

const consolidationPrompt = `You are consolidating chat memory.

Return ONLY JSON with this exact shape:
{"summary":"...", "canonical_memory":"..."}

Rules:
- summary: 3-5 concise sentences describing key facts, decisions, and context from the excerpt.
- canonical_memory: concise markdown bullet list of durable facts/preferences. Start from Existing canonical memory, keep still-true facts, and merge new stable facts.
- If no durable facts changed, canonical_memory may equal Existing canonical memory.

Existing canonical memory:
%s

Conversation excerpt:
%s`

// Consolidator rolls up conversation messages older than the active history
// window into durable memory notes (stored in memory_notes for vector/FTS
// retrieval). It is safe to call MaybeConsolidate after every agent turn;
// it is a no-op when there is nothing to consolidate.
type Consolidator struct {
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
	ChatModel  string
	// WindowSize is the minimum number of consolidatable messages required
	// before a consolidation run is triggered. Default: 10.
	WindowSize int
	// MaxMessages bounds how many messages are processed per consolidation pass.
	// Default: 50.
	MaxMessages int
	// MaxInputChars bounds transcript size passed to the LLM. Default: 12000.
	MaxInputChars int
	// CanonicalPinnedKey is the memory_pinned key used for canonical long-term memory.
	CanonicalPinnedKey string
}

type RunMode struct {
	ArchiveAll bool
}

// MaybeConsolidate checks whether there are enough old messages to warrant a
// consolidation pass and, if so, summarises them into a memory note.
func (c *Consolidator) MaybeConsolidate(ctx context.Context, sessionKey string, historyMax int) error {
	_, err := c.RunOnce(ctx, sessionKey, historyMax, RunMode{})
	return err
}

// ArchiveAll drains all unconsolidated messages in bounded passes.
func (c *Consolidator) ArchiveAll(ctx context.Context, sessionKey string, historyMax int) error {
	const maxPasses = 1024
	for i := 0; i < maxPasses; i++ {
		didWork, err := c.RunOnce(ctx, sessionKey, historyMax, RunMode{ArchiveAll: true})
		if err != nil {
			return err
		}
		if !didWork {
			return nil
		}
	}
	return fmt.Errorf("archive-all exceeded max passes")
}

// RunOnce performs a single bounded consolidation pass.
func (c *Consolidator) RunOnce(ctx context.Context, sessionKey string, historyMax int, mode RunMode) (bool, error) {
	if c.Provider == nil {
		return false, nil
	}
	windowSize := c.WindowSize
	if windowSize <= 0 {
		windowSize = 10
	}
	maxMessages := c.MaxMessages
	if maxMessages <= 0 {
		maxMessages = 50
	}
	maxInputChars := c.MaxInputChars
	if maxInputChars <= 0 {
		maxInputChars = 12000
	}
	if historyMax <= 0 {
		historyMax = 40
	}
	canonicalKey := strings.TrimSpace(c.CanonicalPinnedKey)
	if canonicalKey == "" {
		canonicalKey = defaultCanonicalMemoryKey
	}

	lastID, oldestActiveID, err := c.DB.GetConsolidationRange(ctx, sessionKey, historyMax)
	if err != nil {
		return false, fmt.Errorf("consolidation range: %w", err)
	}
	beforeID := oldestActiveID
	if mode.ArchiveAll {
		beforeID = 0
	} else if oldestActiveID == 0 || oldestActiveID <= lastID+1 {
		return false, nil
	}

	msgs, err := c.DB.GetConsolidationMessages(ctx, sessionKey, lastID, beforeID, maxMessages)
	if err != nil {
		return false, fmt.Errorf("consolidation messages: %w", err)
	}
	if len(msgs) == 0 {
		return false, nil
	}
	lastCandidateID := msgs[len(msgs)-1].ID

	// Build a plain-text conversation transcript.
	var sb strings.Builder
	var lastIncludedID int64
	for _, m := range msgs {
		// Skip tool messages – they're noisy and usually captured by the surrounding turns.
		if m.Role == "tool" {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		line := m.Role + ": " + content
		if sb.Len()+len(line)+1 > maxInputChars {
			if sb.Len() == 0 {
				remaining := maxInputChars - len(m.Role) - 3
				if remaining > 0 {
					line = m.Role + ": " + content[:remaining] + "…"
					sb.WriteString(line)
					sb.WriteString("\n")
					lastIncludedID = m.ID
				}
			}
			break
		}
		sb.WriteString(line)
		sb.WriteString("\n")
		lastIncludedID = m.ID
	}
	transcript := strings.TrimSpace(sb.String())
	memScope := sessionKey
	if memScope == "" || memScope == scope.GlobalMemoryScope {
		memScope = scope.GlobalMemoryScope
	}
	if transcript == "" {
		_, err := c.DB.WriteConsolidation(ctx, db.ConsolidationWrite{
			SessionKey:  sessionKey,
			ScopeKey:    memScope,
			CursorMsgID: lastCandidateID,
		})
		if err != nil {
			return false, fmt.Errorf("consolidation advance cursor: %w", err)
		}
		return true, nil
	}
	shouldConsolidate := mode.ArchiveAll || len(msgs) >= windowSize
	if !shouldConsolidate {
		adaptiveTriggerChars := maxInputChars / canonicalMemoryInputDivisor
		if adaptiveTriggerChars <= 0 {
			adaptiveTriggerChars = 1
		}
		if len(msgs) >= maxMessages || len(transcript) >= adaptiveTriggerChars {
			shouldConsolidate = true
		}
	}
	if !shouldConsolidate {
		return false, nil
	}

	currentCanonical, _, err := c.DB.GetPinnedValue(ctx, memScope, canonicalKey)
	if err != nil {
		return false, fmt.Errorf("consolidation get canonical memory: %w", err)
	}
	currentCanonical = trimTo(currentCanonical, maxInputChars/canonicalMemoryInputDivisor)

	model := c.ChatModel
	if model == "" {
		model = "gpt-4.1-mini"
	}
	req := providers.ChatCompletionRequest{
		Model: model,
		Messages: []providers.ChatMessage{
			{Role: "user", Content: fmt.Sprintf(consolidationPrompt, currentCanonical, transcript)},
		},
		Temperature: 0,
	}
	resp, err := c.Provider.Chat(ctx, req)
	if err != nil {
		return false, fmt.Errorf("consolidation chat: %w", err)
	}
	if len(resp.Choices) == 0 {
		return false, fmt.Errorf("consolidation: no choices returned")
	}
	summary, canonical := parseConsolidationOutput(contentToStr(resp.Choices[0].Message.Content))
	summary = trimTo(summary, maxInputChars/canonicalMemoryInputDivisor)
	canonical = trimTo(canonical, maxInputChars)
	if canonical == "" {
		canonical = currentCanonical
	}

	if summary == "" {
		w := db.ConsolidationWrite{
			SessionKey:  sessionKey,
			ScopeKey:    memScope,
			CursorMsgID: lastIncludedID,
		}
		if canonical != "" {
			w.CanonicalKey = canonicalKey
			w.CanonicalText = canonical
		}
		_, err := c.DB.WriteConsolidation(ctx, w)
		if err != nil {
			return false, fmt.Errorf("consolidation update cursor: %w", err)
		}
		log.Printf("consolidated %d messages for session %q (cursor-only)", len(msgs), sessionKey)
		return true, nil
	}

	embedModel := c.EmbedModel
	var embedding []byte
	if embedModel != "" {
		vec, embedErr := c.Provider.Embed(ctx, embedModel, summary)
		if embedErr != nil {
			log.Printf("consolidation embed failed: %v", embedErr)
			embedding = make([]byte, 0)
		} else {
			embedding = PackFloat32(vec)
		}
	} else {
		embedding = make([]byte, 0)
	}

	w := db.ConsolidationWrite{
		SessionKey:  sessionKey,
		ScopeKey:    memScope,
		NoteText:    summary,
		Embedding:   embedding,
		SourceMsgID: sql.NullInt64{Int64: lastIncludedID, Valid: true},
		NoteTags:    "consolidation",
		CursorMsgID: lastIncludedID,
	}
	if canonical != "" {
		w.CanonicalKey = canonicalKey
		w.CanonicalText = canonical
	}
	_, err = c.DB.WriteConsolidation(ctx, w)
	if err != nil {
		return false, fmt.Errorf("consolidation write: %w", err)
	}

	log.Printf("consolidated %d messages for session %q into memory note", len(msgs), sessionKey)
	return true, nil
}

// contentToStr converts a ChatMessage Content (string or other) to a plain string.
func contentToStr(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

type consolidationOutput struct {
	Summary   string `json:"summary"`
	Canonical string `json:"canonical_memory"`
}

func parseConsolidationOutput(raw string) (summary string, canonical string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	var out consolidationOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return strings.TrimSpace(out.Summary), strings.TrimSpace(out.Canonical)
	}
	return raw, ""
}

func trimTo(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max])
	}
	return s
}
````

## File: internal/memory/retrieve.go
````go
package memory

import (
	"context"
	"math"
	"sort"
	"strings"

	"or3-intern/internal/db"
)

type Retrieved struct {
	Source string // pinned|vector|fts
	ID int64
	Text string
	Score float64
}

type Retriever struct {
	DB *db.DB
	VectorWeight float64
	FTSWeight float64
	LexicalWeight float64
	RecencyWeight float64
	VectorScanLimit int
}

func NewRetriever(d *db.DB) *Retriever {
	return &Retriever{DB: d, VectorWeight: 0.55, FTSWeight: 0.25, LexicalWeight: 0.12, RecencyWeight: 0.08, VectorScanLimit: 2000}
}

func (r *Retriever) Retrieve(ctx context.Context, sessionKey, query string, queryVec []float32, vectorK, ftsK, topK int) ([]Retrieved, error) {
	vecs, err := VectorSearch(ctx, r.DB, sessionKey, queryVec, vectorK, r.VectorScanLimit)
	if err != nil { return nil, err }
	fts, _ := r.DB.SearchFTS(ctx, sessionKey, normalizeFTSQuery(query), ftsK)

	type agg struct {
		id int64
		text string
		v float64
		f float64
		createdAt int64
	}
	m := map[int64]*agg{}
	for _, c := range vecs {
		a := m[c.ID]
		if a == nil { a = &agg{id: c.ID, text: c.Text}; m[c.ID] = a }
		a.v = c.Score
		if c.CreatedAt > a.createdAt {
			a.createdAt = c.CreatedAt
		}
	}
	for _, f := range fts {
		a := m[f.ID]
		if a == nil { a = &agg{id: f.ID, text: f.Text}; m[f.ID] = a }
		// bm25 lower is better. Convert to a positive "higher is better".
		a.f = 1.0 / (1.0 + f.Rank)
		if f.CreatedAt > a.createdAt {
			a.createdAt = f.CreatedAt
		}
	}

	raw := make([]Retrieved, 0, len(m))
	tokens := retrievalTokens(query)
	newest := int64(0)
	for _, a := range m {
		if a.createdAt > newest {
			newest = a.createdAt
		}
	}
	for _, a := range m {
		lexical := lexicalOverlapScore(tokens, a.text)
		recency := recencyScore(a.createdAt, newest)
		score := (a.v * r.VectorWeight) + (a.f * r.FTSWeight) + (lexical * r.LexicalWeight) + (recency * r.RecencyWeight)
		src := "hybrid"
		if a.f > 0 && a.v == 0 { src = "fts" }
		if a.v > 0 && a.f == 0 { src = "vector" }
		raw = append(raw, Retrieved{Source: src, ID: a.id, Text: a.text, Score: score})
	}

	sort.Slice(raw, func(i, j int) bool {
		if raw[i].Score == raw[j].Score {
			return raw[i].ID > raw[j].ID
		}
		return raw[i].Score > raw[j].Score
	})
	return diversifyRetrieved(raw, topK), nil
}

func retrievalTokens(query string) []string {
	parts := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) < 3 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func lexicalOverlapScore(tokens []string, text string) float64 {
	if len(tokens) == 0 {
		return 0
	}
	lower := strings.ToLower(text)
	hits := 0
	for _, token := range tokens {
		if strings.Contains(lower, token) {
			hits++
		}
	}
	return float64(hits) / float64(len(tokens))
}

func recencyScore(createdAt, newest int64) float64 {
	if createdAt <= 0 || newest <= 0 || createdAt >= newest {
		return 1
	}
	ageHours := float64(newest-createdAt) / (1000 * 60 * 60)
	if ageHours <= 0 {
		return 1
	}
	return math.Exp(-ageHours / (24 * 14))
}

func diversifyRetrieved(items []Retrieved, topK int) []Retrieved {
	if topK <= 0 || len(items) == 0 {
		return nil
	}
	selected := make([]Retrieved, 0, min(topK, len(items)))
	seenCanonical := map[string]struct{}{}
	sourceCounts := map[string]int{}
	for _, item := range items {
		canonical := canonicalRetrievedText(item.Text)
		if canonical != "" {
			if _, ok := seenCanonical[canonical]; ok {
				continue
			}
			duplicate := false
			for _, existing := range selected {
				if similarRetrievedText(existing.Text, item.Text) {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
		}
		penalty := 1.0 / float64(sourceCounts[item.Source]+1)
		item.Score = item.Score * (0.85 + 0.15*penalty)
		selected = append(selected, item)
		if canonical != "" {
			seenCanonical[canonical] = struct{}{}
		}
		sourceCounts[item.Source]++
		if len(selected) >= topK {
			break
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Score == selected[j].Score {
			return selected[i].ID > selected[j].ID
		}
		return selected[i].Score > selected[j].Score
	})
	return selected
}

func canonicalRetrievedText(text string) string {
	text = strings.ToLower(strings.Join(strings.Fields(text), " "))
	if len(text) > 180 {
		text = text[:180]
	}
	return text
}

func similarRetrievedText(a, b string) bool {
	ac := canonicalRetrievedText(a)
	bc := canonicalRetrievedText(b)
	if ac == "" || bc == "" {
		return false
	}
	if ac == bc {
		return true
	}
	at := retrievalTokens(ac)
	bt := retrievalTokens(bc)
	if len(at) == 0 || len(bt) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, token := range at {
		set[token] = struct{}{}
	}
	shared := 0
	union := len(set)
	for _, token := range bt {
		if _, ok := set[token]; ok {
			shared++
			continue
		}
		union++
	}
	if union == 0 {
		return false
	}
	return float64(shared)/float64(union) >= 0.8
}

func normalizeFTSQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" { return "" }
	// simple: split on spaces, quote terms that contain punctuation
	parts := strings.Fields(q)
	for i, p := range parts {
		if strings.ContainsAny(p, `":*`) {
			parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		}
	}
	return strings.Join(parts, " ")
}
````

## File: internal/tools/skill_exec.go
````go
package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"or3-intern/internal/skills"
)

type RunSkillScript struct {
	Base
	Inventory         *skills.Inventory
	Enabled           bool
	Timeout           time.Duration
	ChildEnvAllowlist []string
	Sandbox           BubblewrapConfig
	OutputMaxBytes    int
}

func (t *RunSkillScript) Capability() CapabilityLevel { return CapabilityPrivileged }

func (t *RunSkillScript) Name() string { return "run_skill_script" }

func (t *RunSkillScript) Description() string {
	return "Run a skill-local script or declared entrypoint without shell interpolation."
}

func (t *RunSkillScript) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill":      map[string]any{"type": "string", "description": "Skill name from inventory"},
			"path":       map[string]any{"type": "string", "description": "Bundle-relative script path"},
			"entrypoint": map[string]any{"type": "string", "description": "Named skill.json entrypoint"},
			"args": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional argument list",
			},
			"stdin":          map[string]any{"type": "string", "description": "Optional stdin text"},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Optional timeout override"},
		},
		"required": []string{"skill"},
	}
}

func (t *RunSkillScript) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *RunSkillScript) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Inventory == nil {
		return "", fmt.Errorf("skills inventory not configured")
	}
	if !t.Enabled {
		return "", fmt.Errorf("skill execution disabled")
	}
	skillName := strings.TrimSpace(fmt.Sprint(params["skill"]))
	if skillName == "" {
		return "", fmt.Errorf("missing skill")
	}
	skill, ok := t.Inventory.Get(skillName)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", skillName)
	}
	if skill.PermissionState == "blocked" {
		return "", fmt.Errorf("skill blocked: %s", strings.Join(skill.PermissionNotes, "; "))
	}
	if skill.PermissionState != "approved" {
		return "", fmt.Errorf("skill requires approval before execution: %s", skill.Name)
	}

	cmd, err := t.commandForSkill(skill, params)
	if err != nil {
		return "", err
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		timeout = time.Duration(int(v)) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command, err := commandWithSandbox(runCtx, t.Sandbox, skill.Dir, cmd)
	if err != nil {
		return "", err
	}
	if command == nil {
		command = exec.CommandContext(runCtx, cmd[0], cmd[1:]...)
	}
	command.Dir = skill.Dir
	command.Env = BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), "")
	if stdin := strings.TrimSpace(fmt.Sprint(params["stdin"])); stdin != "" {
		command.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err = command.Run()

	out := stdout.String()
	er := stderr.String()
	max := t.OutputMaxBytes
	if max <= 0 {
		max = defaultExecOutputMaxBytes
	}
	if len(out) > max {
		out = out[:max] + "\n...[truncated]\n"
	}
	if len(er) > max {
		er = er[:max] + "\n...[truncated]\n"
	}
	if err != nil {
		return formatCommandOutput(out, er), fmt.Errorf("exec failed: %w", err)
	}
	if strings.TrimSpace(er) != "" {
		return formatCommandOutput(out, er), nil
	}
	return out, nil
}

func (t *RunSkillScript) commandForSkill(skill skills.SkillMeta, params map[string]any) ([]string, error) {
	entrypoint := strings.TrimSpace(fmt.Sprint(params["entrypoint"]))
	if entrypoint == "<nil>" {
		entrypoint = ""
	}
	if entrypoint != "" {
		for _, candidate := range skill.Entrypoints {
			if candidate.Name != entrypoint {
				continue
			}
			cmd, err := t.entrypointCommand(skill, candidate)
			if err != nil {
				return nil, err
			}
			return append(cmd, stringArgs(params["args"])...), nil
		}
		return nil, fmt.Errorf("entrypoint not found: %s", entrypoint)
	}

	relPath := strings.TrimSpace(fmt.Sprint(params["path"]))
	if relPath == "<nil>" {
		relPath = ""
	}
	if relPath == "" {
		return nil, fmt.Errorf("missing path or entrypoint")
	}
	resolved, err := t.Inventory.ResolveBundlePath(skill.Name, relPath)
	if err != nil {
		return nil, err
	}
	base, err := scriptCommand(resolved)
	if err != nil {
		return nil, err
	}
	return append(base, stringArgs(params["args"])...), nil
}

func (t *RunSkillScript) entrypointCommand(skill skills.SkillMeta, entry skills.SkillEntry) ([]string, error) {
	if len(entry.Command) == 0 {
		return nil, fmt.Errorf("entrypoint has no command: %s", entry.Name)
	}
	cmd := make([]string, 0, len(entry.Command))
	for _, token := range entry.Command {
		token = strings.ReplaceAll(token, "{baseDir}", skill.Dir)
		cmd = append(cmd, token)
	}
	if len(cmd) == 0 {
		return nil, fmt.Errorf("entrypoint has no command: %s", entry.Name)
	}
	if strings.HasPrefix(cmd[0], ".") || strings.Contains(cmd[0], string(filepath.Separator)) {
		resolved, err := t.Inventory.ResolveBundlePath(skill.Name, cmd[0])
		if err != nil {
			return nil, err
		}
		cmd[0] = resolved
	}
	return cmd, nil
}

func scriptCommand(path string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("bundle path is a directory: %s", path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh":
		return []string{"bash", path}, nil
	case ".py":
		if _, err := exec.LookPath("python3"); err == nil {
			return []string{"python3", path}, nil
		}
		if _, err := exec.LookPath("python"); err == nil {
			return []string{"python", path}, nil
		}
		return nil, fmt.Errorf("python interpreter not found")
	default:
		if info.Mode()&0o111 != 0 {
			return []string{path}, nil
		}
		return nil, fmt.Errorf("unsupported script type: %s", filepath.Ext(path))
	}
}

func stringArgs(raw any) []string {
	values, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}
````

## File: cmd/or3-intern/doctor.go
````go
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"

	"or3-intern/internal/config"
)

type doctorFinding struct {
	Level   string
	Area    string
	Message string
}

func runDoctorCommand(cfg config.Config, args []string, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	strict := fs.Bool("strict", false, "exit non-zero when warnings are found")
	if err := fs.Parse(args); err != nil {
		return err
	}
	findings := doctorFindings(cfg)
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(stdout, "[ok] configuration looks safe")
		return nil
	}
	for _, finding := range findings {
		_, _ = fmt.Fprintf(stdout, "[%s] %s: %s\n", finding.Level, finding.Area, finding.Message)
	}
	if *strict && hasDoctorWarnings(findings) {
		return fmt.Errorf("doctor found warnings")
	}
	return nil
}

func doctorFindings(cfg config.Config) []doctorFinding {
	findings := make([]doctorFinding, 0, 32)
	findings = append(findings, filesystemFindings(cfg)...)
	findings = append(findings, hardeningFindings(cfg)...)
	findings = append(findings, securityFindings(cfg)...)
	findings = append(findings, webhookFindings(cfg)...)
	findings = append(findings, serviceFindings(cfg)...)
	findings = append(findings, mcpFindings(cfg)...)
	findings = append(findings, networkFindings(cfg)...)
	findings = append(findings, profileFindings(cfg)...)
	findings = append(findings, execFindings(cfg)...)
	findings = append(findings, skillFindings(cfg)...)
	findings = append(findings, channelExposureFindings(cfg)...)
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Area == findings[j].Area {
			return findings[i].Message < findings[j].Message
		}
		return findings[i].Area < findings[j].Area
	})
	return findings
}

func filesystemFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(area, message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: area, Message: message})
	}
	if !cfg.Tools.RestrictToWorkspace {
		addWarn("filesystem", "workspace restriction is disabled")
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		addWarn("filesystem", "workspace restriction is enabled but workspaceDir is empty")
	}
	return findings
}

func hardeningFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(area, message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: area, Message: message})
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		addWarn("env", "child process environment allowlist is empty")
	}
	if !cfg.Hardening.Quotas.Enabled {
		addWarn("quotas", "tool quotas are disabled")
	}
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 || cfg.Hardening.Quotas.MaxExecCalls <= 0 || cfg.Hardening.Quotas.MaxWebCalls <= 0 || cfg.Hardening.Quotas.MaxSubagentCalls <= 0 {
		addWarn("quotas", "one or more quota limits are unset")
	}
	if cfg.Hardening.PrivilegedTools && !cfg.Hardening.Sandbox.Enabled {
		addWarn("privileged-exec", "privileged tools are enabled without Bubblewrap sandboxing")
	}
	if cfg.Hardening.Sandbox.Enabled && strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
		addWarn("privileged-exec", "Bubblewrap sandbox is enabled without a bubblewrapPath")
	}
	return findings
}

func securityFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "security", Message: message})
	}
	if !cfg.Security.Audit.Enabled {
		addWarn("audit logging is disabled")
	} else {
		if !cfg.Security.Audit.Strict {
			addWarn("audit logging is enabled but strict mode is off")
		}
		if !cfg.Security.Audit.VerifyOnStart {
			addWarn("audit logging is enabled but verifyOnStart is off")
		}
	}
	if !cfg.Security.SecretStore.Enabled {
		addWarn("secret store is disabled")
		if hasExternalIntegrations(cfg) {
			addWarn("secret store is disabled while external integrations are enabled")
		}
	} else if !cfg.Security.SecretStore.Required && hasExternalIntegrations(cfg) {
		addWarn("secret store failures are tolerated while channels, webhook, or MCP integrations are enabled")
	}
	if !cfg.Security.Profiles.Enabled {
		addWarn("access profiles are disabled")
	}
	return findings
}

func webhookFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "webhook", Message: message})
	}
	if !cfg.Triggers.Webhook.Enabled {
		return findings
	}
	if strings.TrimSpace(cfg.Triggers.Webhook.Secret) == "" {
		addWarn("webhook is enabled without a secret")
	}
	if !isLoopbackAddr(cfg.Triggers.Webhook.Addr) {
		addWarn("webhook bind address is not loopback-only")
	}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
	if !ok {
		addWarn("webhook is enabled without an effective access profile")
		if cfg.Hardening.PrivilegedTools {
			addWarn("webhook can reach privileged tools because no access profile applies")
		}
		if cfg.Hardening.GuardedTools {
			addWarn("webhook can reach guarded tools because no access profile applies")
		}
		if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
			addWarn("webhook can reach skill execution because no access profile applies")
		}
		return findings
	}
	if profileAllowsPrivileged(profile) {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with privileged capability", profileName))
	}
	if profile.AllowSubagents {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with subagents enabled", profileName))
	}
	if len(profile.WritablePaths) > 0 {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with writable paths", profileName))
	}
	if hostListTooBroad(profile.AllowedHosts) {
		addWarn(fmt.Sprintf("webhook resolves to profile %q with broad allowedHosts", profileName))
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		addWarn(fmt.Sprintf("webhook can reach exec shell mode via profile %q", profileName))
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
		addWarn(fmt.Sprintf("webhook can reach skill execution via profile %q", profileName))
	}
	return findings
}

func serviceFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "service", Message: message})
	}
	if !cfg.Service.Enabled {
		return findings
	}
	if strings.TrimSpace(cfg.Service.Secret) == "" {
		addWarn("service mode is enabled without a shared secret")
	} else if len(strings.TrimSpace(cfg.Service.Secret)) < 24 {
		addWarn("service mode is enabled with a weak shared secret")
	}
	if !isLoopbackAddr(cfg.Service.Listen) {
		addWarn("service bind address is not loopback-only")
	}
	return findings
}

func mcpFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "mcp", Message: message})
	}
	if len(cfg.Tools.MCPServers) == 0 {
		return findings
	}
	for name, server := range cfg.Tools.MCPServers {
		if !server.Enabled {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(server.Transport)) {
		case "stdio":
			if len(server.ChildEnvAllowlist) == 0 {
				addWarn(fmt.Sprintf("server %q uses stdio without a server childEnvAllowlist", name))
				if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
					addWarn(fmt.Sprintf("server %q uses stdio with no server or global child environment allowlist", name))
				}
			}
		case "sse", "streamablehttp":
			if server.AllowInsecureHTTP || isInsecureHTTPURL(server.URL) {
				addWarn(fmt.Sprintf("server %q uses insecure HTTP transport", name))
			}
			if !cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny {
				addWarn(fmt.Sprintf("server %q uses remote HTTP transport without deny-by-default network policy", name))
			}
			if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
				addWarn(fmt.Sprintf("server %q relies on a broad network allowlist", name))
			}
		}
	}
	return findings
}

func networkFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "network", Message: message})
	}
	if hostListContainsLiteralStar(cfg.Security.Network.AllowedHosts) {
		addWarn("security.network.allowedHosts contains *")
	}
	if hostListTooBroad(cfg.Security.Network.AllowedHosts) {
		addWarn("security.network.allowedHosts is broad")
	}
	if hasRemoteHTTPMCP(cfg) && (!cfg.Security.Network.Enabled || !cfg.Security.Network.DefaultDeny) {
		addWarn("remote MCP transports are enabled without a meaningful deny-by-default network posture")
	}
	return findings
}

func profileFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "profiles", Message: message})
	}
	if !cfg.Security.Profiles.Enabled {
		if hasPublicIngress(cfg) {
			addWarn("public ingress is enabled while access profiles are disabled")
		}
		if cfg.Triggers.Webhook.Enabled {
			addWarn("webhook is enabled while access profiles are disabled")
		}
		return findings
	}
	if len(cfg.Security.Profiles.Profiles) == 0 {
		addWarn("access profiles are enabled but no profiles are defined")
		return findings
	}
	if strings.TrimSpace(cfg.Security.Profiles.Default) == "" && len(cfg.Security.Profiles.Channels) == 0 && len(cfg.Security.Profiles.Triggers) == 0 {
		addWarn("access profiles are enabled but no default, channel, or trigger mapping is configured")
	}
	if hasPublicIngress(cfg) && strings.TrimSpace(cfg.Security.Profiles.Default) == "" {
		missing := false
		for _, channel := range openAccessChannelNames(cfg) {
			if _, _, ok := resolveEffectiveProfile(cfg, "", channel); !ok {
				missing = true
				break
			}
		}
		if missing {
			addWarn("one or more open-access channels have no effective access profile")
		}
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, _, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok {
			addWarn("webhook has no effective access profile")
		}
	}
	profileNames := make([]string, 0, len(cfg.Security.Profiles.Profiles))
	for name := range cfg.Security.Profiles.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	for _, name := range profileNames {
		profile := cfg.Security.Profiles.Profiles[name]
		if hostListContainsLiteralStar(profile.AllowedHosts) {
			addWarn(fmt.Sprintf("profile %q allowedHosts contains *", name))
		}
		if hostListTooBroad(profile.AllowedHosts) {
			addWarn(fmt.Sprintf("profile %q has broad allowedHosts", name))
		}
		if profileAllowsPrivileged(profile) && len(profile.AllowedTools) == 0 {
			addWarn(fmt.Sprintf("profile %q permits privileged capability without an explicit tool allowlist", name))
		}
	}
	return findings
}

func execFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "exec", Message: message})
	}
	if !cfg.Hardening.PrivilegedTools && !cfg.Hardening.GuardedTools {
		return findings
	}
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		addWarn("exec is enabled without an exec allowlist")
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		addWarn("exec-capable configuration has an empty child environment allowlist")
	}
	if cfg.Hardening.EnableExecShell {
		addWarn("exec shell command mode is enabled; prefer program + args and keep shell mode off unless strictly required")
	}
	if publicIngressCanReachExec(cfg) || webhookCanReachExec(cfg) {
		addWarn("public or webhook-facing ingress can reach privileged exec posture unless profiles deny it")
	}
	return findings
}

func skillFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	addWarn := func(message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: "skills", Message: message})
	}
	if !cfg.Skills.EnableExec {
		return findings
	}
	if !cfg.Skills.Policy.QuarantineByDefault {
		addWarn("skill execution is enabled while quarantineByDefault is false")
	}
	if len(cfg.Skills.Policy.TrustedOwners) == 0 {
		addWarn("skill execution is enabled without a trustedOwners policy for managed skills")
	}
	if len(cfg.Skills.Policy.TrustedRegistries) == 0 {
		addWarn("skill execution is enabled without a trustedRegistries policy for managed skills")
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		addWarn("skill execution is enabled with an empty child environment allowlist")
	}
	if hasPublicIngress(cfg) && publicIngressCanReachSkillExec(cfg) {
		addWarn("public ingress can reach skill execution through a permissive profile")
	}
	if cfg.Triggers.Webhook.Enabled {
		if _, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook"); !ok || (profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script")) {
			addWarn("webhook can reach skill execution through a permissive profile")
		}
	}
	return findings
}

func channelExposureFindings(cfg config.Config) []doctorFinding {
	findings := []doctorFinding{}
	add := func(area, message string) {
		findings = append(findings, doctorFinding{Level: "warn", Area: area, Message: message})
	}
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.OpenAccess {
		add("telegram", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "telegram")
	}
	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.OpenAccess {
		add("slack", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "slack")
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.OpenAccess {
		add("discord", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "discord")
	}
	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.OpenAccess {
		add("whatsapp", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "whatsapp")
	}
	if cfg.Channels.Email.Enabled && cfg.Channels.Email.OpenAccess {
		add("email", "channel is open to any sender")
		appendPublicChannelExposureWarnings(&findings, cfg, "email")
	}
	return findings
}

func appendPublicChannelExposureWarnings(findings *[]doctorFinding, cfg config.Config, channel string) {
	add := func(message string) {
		*findings = append(*findings, doctorFinding{Level: "warn", Area: channel, Message: message})
	}
	profileName, profile, ok := resolveEffectiveProfile(cfg, "", channel)
	if !ok {
		if cfg.Hardening.PrivilegedTools {
			add("open-access channel can reach privileged tools because no access profile applies")
		}
		if cfg.Hardening.GuardedTools {
			add("open-access channel can reach guarded tools because no access profile applies")
		}
		if cfg.Skills.EnableExec && cfg.Hardening.PrivilegedTools {
			add("open-access channel can reach skill execution because no access profile applies")
		}
		return
	}
	if profileAllowsPrivileged(profile) {
		add(fmt.Sprintf("open-access channel resolves to profile %q with privileged capability", profileName))
	}
	if cfg.Hardening.GuardedTools && !profileHasMeaningfulToolRestriction(profile) {
		add(fmt.Sprintf("open-access channel resolves to profile %q without a meaningful tool restriction", profileName))
	}
	if cfg.Hardening.EnableExecShell && cfg.Hardening.PrivilegedTools && profileCanReachExec(profile) {
		add(fmt.Sprintf("open-access channel can reach exec shell mode via profile %q", profileName))
	}
	if cfg.Skills.EnableExec && profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
		add(fmt.Sprintf("open-access channel can reach skill execution via profile %q", profileName))
	}
}

func hasDoctorWarnings(findings []doctorFinding) bool {
	for _, finding := range findings {
		if strings.EqualFold(finding.Level, "warn") || strings.EqualFold(finding.Level, "error") {
			return true
		}
	}
	return false
}

func isLoopbackAddr(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return true
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

func hasExternalIntegrations(cfg config.Config) bool {
	return cfg.Triggers.Webhook.Enabled || anyEnabledChannels(cfg) || anyEnabledMCPServers(cfg)
}

func anyEnabledChannels(cfg config.Config) bool {
	return cfg.Channels.Telegram.Enabled || cfg.Channels.Slack.Enabled || cfg.Channels.Discord.Enabled || cfg.Channels.WhatsApp.Enabled || cfg.Channels.Email.Enabled
}

func anyEnabledMCPServers(cfg config.Config) bool {
	for _, server := range cfg.Tools.MCPServers {
		if server.Enabled {
			return true
		}
	}
	return false
}

func hasPublicIngress(cfg config.Config) bool {
	return len(openAccessChannelNames(cfg)) > 0
}

func openAccessChannelNames(cfg config.Config) []string {
	channels := []string{}
	if cfg.Channels.Telegram.Enabled && cfg.Channels.Telegram.OpenAccess {
		channels = append(channels, "telegram")
	}
	if cfg.Channels.Slack.Enabled && cfg.Channels.Slack.OpenAccess {
		channels = append(channels, "slack")
	}
	if cfg.Channels.Discord.Enabled && cfg.Channels.Discord.OpenAccess {
		channels = append(channels, "discord")
	}
	if cfg.Channels.WhatsApp.Enabled && cfg.Channels.WhatsApp.OpenAccess {
		channels = append(channels, "whatsapp")
	}
	if cfg.Channels.Email.Enabled && cfg.Channels.Email.OpenAccess {
		channels = append(channels, "email")
	}
	return channels
}

func resolveEffectiveProfile(cfg config.Config, trigger, channel string) (string, config.AccessProfileConfig, bool) {
	if !cfg.Security.Profiles.Enabled {
		return "", config.AccessProfileConfig{}, false
	}
	if profileName := strings.TrimSpace(cfg.Security.Profiles.Triggers[strings.ToLower(strings.TrimSpace(trigger))]); profileName != "" {
		profile, ok := cfg.Security.Profiles.Profiles[profileName]
		return profileName, profile, ok
	}
	if profileName := strings.TrimSpace(cfg.Security.Profiles.Channels[strings.ToLower(strings.TrimSpace(channel))]); profileName != "" {
		profile, ok := cfg.Security.Profiles.Profiles[profileName]
		return profileName, profile, ok
	}
	profileName := strings.TrimSpace(cfg.Security.Profiles.Default)
	if profileName == "" {
		return "", config.AccessProfileConfig{}, false
	}
	profile, ok := cfg.Security.Profiles.Profiles[profileName]
	return profileName, profile, ok
}

func profileAllowsPrivileged(profile config.AccessProfileConfig) bool {
	maxCapability := strings.ToLower(strings.TrimSpace(profile.MaxCapability))
	return maxCapability == "" || maxCapability == "privileged"
}

func profileAllowsGuarded(profile config.AccessProfileConfig) bool {
	maxCapability := strings.ToLower(strings.TrimSpace(profile.MaxCapability))
	return maxCapability == "" || maxCapability == "privileged" || maxCapability == "guarded"
}

func profileAllowsTool(profile config.AccessProfileConfig, toolName string) bool {
	if len(profile.AllowedTools) == 0 {
		return true
	}
	toolName = strings.TrimSpace(toolName)
	for _, allowed := range profile.AllowedTools {
		if strings.TrimSpace(allowed) == toolName {
			return true
		}
	}
	return false
}

func profileHasMeaningfulToolRestriction(profile config.AccessProfileConfig) bool {
	return !profileAllowsGuarded(profile) || len(profile.AllowedTools) > 0
}

func profileCanReachExec(profile config.AccessProfileConfig) bool {
	return profileAllowsPrivileged(profile) && profileAllowsTool(profile, "exec")
}

func hostListContainsLiteralStar(hosts []string) bool {
	for _, host := range hosts {
		if strings.TrimSpace(host) == "*" {
			return true
		}
	}
	return false
}

func hostListTooBroad(hosts []string) bool {
	if len(hosts) > 10 {
		return true
	}
	for _, host := range hosts {
		host = strings.TrimSpace(strings.ToLower(host))
		if host == "*" {
			return true
		}
		if strings.Contains(host, "*") && !strings.HasPrefix(host, "*.") {
			return true
		}
		if strings.HasPrefix(host, "*.") {
			return true
		}
	}
	return false
}

func isInsecureHTTPURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Scheme, "http")
}

func hasRemoteHTTPMCP(cfg config.Config) bool {
	for _, server := range cfg.Tools.MCPServers {
		if !server.Enabled {
			continue
		}
		transport := strings.ToLower(strings.TrimSpace(server.Transport))
		if transport != "sse" && transport != "streamablehttp" {
			continue
		}
		u, err := url.Parse(strings.TrimSpace(server.URL))
		if err != nil {
			return true
		}
		if !isLoopbackAddr(u.Hostname()) {
			return true
		}
	}
	return false
}

func publicIngressCanReachSkillExec(cfg config.Config) bool {
	for _, channel := range openAccessChannelNames(cfg) {
		_, profile, ok := resolveEffectiveProfile(cfg, "", channel)
		if !ok {
			return cfg.Hardening.PrivilegedTools
		}
		if profileAllowsPrivileged(profile) && profileAllowsTool(profile, "run_skill_script") {
			return true
		}
	}
	return false
}

func publicIngressCanReachExec(cfg config.Config) bool {
	if !cfg.Hardening.EnableExecShell || !cfg.Hardening.PrivilegedTools {
		return false
	}
	for _, channel := range openAccessChannelNames(cfg) {
		_, profile, ok := resolveEffectiveProfile(cfg, "", channel)
		if !ok {
			return true
		}
		if profileCanReachExec(profile) {
			return true
		}
	}
	return false
}

func webhookCanReachExec(cfg config.Config) bool {
	if !cfg.Hardening.EnableExecShell || !cfg.Hardening.PrivilegedTools || !cfg.Triggers.Webhook.Enabled {
		return false
	}
	_, profile, ok := resolveEffectiveProfile(cfg, "webhook", "webhook")
	if !ok {
		return true
	}
	return profileCanReachExec(profile)
}
````

## File: cmd/or3-intern/skills_cmd.go
````go
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"or3-intern/internal/clawhub"
	"or3-intern/internal/config"
	"or3-intern/internal/mcp"
	"or3-intern/internal/skills"
)

type skillsCommandDeps struct {
	Client        *clawhub.Client
	LoadToolNames func(context.Context, config.Config) map[string]struct{}
	LoadInventory func(toolNames map[string]struct{}) skills.Inventory
	Audit         func(context.Context, string, any) error
	Stdout        io.Writer
	Stderr        io.Writer
}

func runSkillsCommand(ctx context.Context, cfg config.Config, bundledDir string, args []string, stdout, stderr io.Writer) error {
	deps := skillsCommandDeps{
		Client: newClawHubClient(cfg),
		LoadToolNames: func(ctx context.Context, cfg config.Config) map[string]struct{} {
			return loadAvailableToolNamesWithManager(ctx, cfg, nil)
		},
		LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
			return buildSkillsInventory(cfg, bundledDir, toolNames)
		},
		Stdout: stdout,
		Stderr: stderr,
	}
	return runSkillsCommandWithDeps(ctx, cfg, args, deps)
}

func runSkillsCommandWithDeps(ctx context.Context, cfg config.Config, args []string, deps skillsCommandDeps) error {
	if deps.Client == nil {
		deps.Client = newClawHubClient(cfg)
	}
	if deps.LoadToolNames == nil {
		deps.LoadToolNames = func(ctx context.Context, cfg config.Config) map[string]struct{} {
			return loadAvailableToolNamesWithManager(ctx, cfg, nil)
		}
	}
	if deps.LoadInventory == nil {
		return fmt.Errorf("skills inventory loader not configured")
	}
	if deps.Stdout == nil {
		deps.Stdout = os.Stdout
	}
	if deps.Stderr == nil {
		deps.Stderr = os.Stderr
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: or3-intern skills <list|info|check|search|install|update|remove> ...")
	}

	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("skills list", flag.ContinueOnError)
		fs.SetOutput(deps.Stderr)
		eligibleOnly := fs.Bool("eligible", false, "show only eligible skills")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		inv := deps.LoadInventory(deps.LoadToolNames(ctx, cfg))
		if len(inv.Skills) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no skills found)")
			return nil
		}
		for _, skill := range inv.Skills {
			if *eligibleOnly && !skill.Eligible {
				continue
			}
			status := "eligible"
			switch {
			case skill.ParseError != "":
				status = "parse-error"
			case skill.Disabled:
				status = "disabled"
			case !skill.Eligible:
				status = "ineligible"
			case skill.Hidden:
				status = "hidden"
			}
			permissionState := strings.TrimSpace(skill.PermissionState)
			if permissionState == "" {
				permissionState = "approved"
			}
			_, _ = fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\t%s\t%s\n", skill.Name, status, permissionState, skill.Source, skill.Dir)
		}
		return nil
	case "info":
		if len(args) < 2 {
			return fmt.Errorf("usage: or3-intern skills info <name>")
		}
		inv := deps.LoadInventory(deps.LoadToolNames(ctx, cfg))
		skill, ok := inv.Get(args[1])
		if !ok {
			return fmt.Errorf("skill not found: %s", args[1])
		}
		_, _ = fmt.Fprintf(deps.Stdout, "Name: %s\n", skill.Name)
		_, _ = fmt.Fprintf(deps.Stdout, "Description: %s\n", skill.Description)
		_, _ = fmt.Fprintf(deps.Stdout, "Source: %s\n", skill.Source)
		_, _ = fmt.Fprintf(deps.Stdout, "Location: %s\n", skill.Dir)
		if skill.Homepage != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Homepage: %s\n", skill.Homepage)
		}
		_, _ = fmt.Fprintf(deps.Stdout, "Eligible: %t\n", skill.Eligible)
		_, _ = fmt.Fprintf(deps.Stdout, "User Invocable: %t\n", skill.UserInvocable)
		if skill.Publisher != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Publisher: %s\n", skill.Publisher)
		}
		if skill.Registry != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Registry: %s\n", skill.Registry)
		}
		if skill.InstalledVersion != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Installed Version: %s\n", skill.InstalledVersion)
		}
		if skill.Modified {
			_, _ = fmt.Fprintln(deps.Stdout, "Local Modifications: true")
		}
		if strings.TrimSpace(skill.ScanStatus) != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Scan Status: %s\n", skill.ScanStatus)
			printReasons(deps.Stdout, "Scan Findings", skill.ScanFindings)
		}
		if strings.TrimSpace(skill.PermissionState) != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Permission State: %s\n", skill.PermissionState)
			_, _ = fmt.Fprintf(deps.Stdout, "Declared Permissions: %s\n", skill.Permissions.Summary())
			printReasons(deps.Stdout, "Permission Notes", skill.PermissionNotes)
		}
		if skill.Hidden {
			_, _ = fmt.Fprintln(deps.Stdout, "Model Visibility: hidden")
		}
		if skill.CommandDispatch != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Command Dispatch: %s\n", skill.CommandDispatch)
			_, _ = fmt.Fprintf(deps.Stdout, "Command Tool: %s\n", skill.CommandTool)
			_, _ = fmt.Fprintf(deps.Stdout, "Command Arg Mode: %s\n", skill.CommandArgMode)
		}
		printReasons(deps.Stdout, "Missing", skill.Missing)
		printReasons(deps.Stdout, "Unsupported", skill.Unsupported)
		if skill.ParseError != "" {
			_, _ = fmt.Fprintf(deps.Stdout, "Parse Error: %s\n", skill.ParseError)
		}
		return nil
	case "check":
		inv := deps.LoadInventory(deps.LoadToolNames(ctx, cfg))
		if len(inv.Skills) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no skills found)")
			return nil
		}
		for _, skill := range inv.Skills {
			if skill.PermissionState == "quarantined" {
				_, _ = fmt.Fprintf(deps.Stdout, "[quarantined] %s: %s\n", skill.Name, strings.Join(skill.PermissionNotes, "; "))
				continue
			}
			if skill.PermissionState == "blocked" {
				reasons := append([]string{}, skill.PermissionNotes...)
				reasons = append(reasons, skill.Missing...)
				reasons = append(reasons, skill.Unsupported...)
				if skill.ParseError != "" {
					reasons = append(reasons, skill.ParseError)
				}
				_, _ = fmt.Fprintf(deps.Stdout, "[blocked] %s: %s\n", skill.Name, strings.Join(reasons, "; "))
				continue
			}
			if skill.Eligible {
				_, _ = fmt.Fprintf(deps.Stdout, "[ok] %s\n", skill.Name)
				continue
			}
			reasons := append([]string{}, skill.Missing...)
			reasons = append(reasons, skill.Unsupported...)
			if skill.ParseError != "" {
				reasons = append(reasons, skill.ParseError)
			}
			_, _ = fmt.Fprintf(deps.Stdout, "[blocked] %s: %s\n", skill.Name, strings.Join(reasons, "; "))
		}
		return nil
	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: or3-intern skills search <query>")
		}
		results, err := deps.Client.Search(ctx, strings.Join(args[1:], " "), 10)
		if err != nil {
			return err
		}
		if len(results) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no results)")
			return nil
		}
		for _, result := range results {
			version := result.Version
			if version == "" {
				version = "latest"
			}
			_, _ = fmt.Fprintf(deps.Stdout, "%s\t%s\t%s\n", result.Slug, version, strings.TrimSpace(result.DisplayName))
		}
		return nil
	case "install":
		fs := flag.NewFlagSet("skills install", flag.ContinueOnError)
		fs.SetOutput(deps.Stderr)
		version := fs.String("version", "", "skill version")
		force := fs.Bool("force", false, "overwrite local modifications")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() < 1 {
			return fmt.Errorf("usage: or3-intern skills install <slug> [--version v]")
		}
		result, err := deps.Client.Install(ctx, fs.Arg(0), *version, resolveInstallRoot(cfg), clawhub.InstallOptions{Force: *force})
		if err != nil {
			return err
		}
		if deps.Audit != nil {
			if err := deps.Audit(ctx, "skill.install", map[string]any{"slug": result.Slug, "version": result.Version, "path": result.Path}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(deps.Stdout, "installed\t%s\t%s\t%s\n", result.Slug, result.Version, result.Path)
		return nil
	case "update":
		fs := flag.NewFlagSet("skills update", flag.ContinueOnError)
		fs.SetOutput(deps.Stderr)
		all := fs.Bool("all", false, "update all installed skills")
		version := fs.String("version", "", "target version")
		force := fs.Bool("force", false, "overwrite local modifications")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		root := resolveInstallRoot(cfg)
		installed, err := clawhub.ListInstalled(root)
		if err != nil {
			return err
		}
		targets := installed
		if !*all {
			if fs.NArg() < 1 {
				return fmt.Errorf("usage: or3-intern skills update <name>|--all")
			}
			match, matchErr := findInstalledSkill(installed, fs.Arg(0))
			if matchErr != nil {
				return matchErr
			}
			targets = []clawhub.InstalledSkill{match}
		}
		if len(targets) == 0 {
			_, _ = fmt.Fprintln(deps.Stdout, "(no installed skills)")
			return nil
		}
		for _, item := range targets {
			info, err := deps.Client.Inspect(ctx, item.Origin.Slug, *version)
			if err != nil {
				return err
			}
			targetVersion := strings.TrimSpace(*version)
			if targetVersion == "" {
				targetVersion = info.LatestVersion
			}
			if targetVersion == "" {
				return fmt.Errorf("could not resolve latest version for %s", item.Origin.Slug)
			}
			if item.Origin.InstalledVersion == targetVersion {
				_, _ = fmt.Fprintf(deps.Stdout, "up-to-date\t%s\t%s\n", item.Origin.Slug, targetVersion)
				continue
			}
			if _, err := deps.Client.Install(ctx, item.Origin.Slug, targetVersion, root, clawhub.InstallOptions{Force: *force}); err != nil {
				return err
			}
			if deps.Audit != nil {
				if err := deps.Audit(ctx, "skill.update", map[string]any{"slug": item.Origin.Slug, "version": targetVersion}); err != nil {
					return err
				}
			}
			_, _ = fmt.Fprintf(deps.Stdout, "updated\t%s\t%s\n", item.Origin.Slug, targetVersion)
		}
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: or3-intern skills remove <name>")
		}
		root := resolveInstallRoot(cfg)
		installed, err := clawhub.ListInstalled(root)
		if err != nil {
			return err
		}
		match, err := findInstalledSkill(installed, args[1])
		if err != nil {
			return err
		}
		if err := clawhub.RemoveSkill(root, match.Name); err != nil {
			return err
		}
		if deps.Audit != nil {
			if err := deps.Audit(ctx, "skill.remove", map[string]any{"name": match.Name}); err != nil {
				return err
			}
		}
		_, _ = fmt.Fprintf(deps.Stdout, "removed\t%s\n", match.Name)
		return nil
	default:
		return fmt.Errorf("unknown skills subcommand: %s", args[0])
	}
}

func buildSkillsInventory(cfg config.Config, bundledDir string, toolNames map[string]struct{}) skills.Inventory {
	approved := make(map[string]struct{}, len(cfg.Skills.Policy.Approved)*2)
	for _, name := range cfg.Skills.Policy.Approved {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		approved[trimmed] = struct{}{}
		approved[strings.ToLower(trimmed)] = struct{}{}
	}
	trustedOwners := makePolicySet(cfg.Skills.Policy.TrustedOwners)
	blockedOwners := makePolicySet(cfg.Skills.Policy.BlockedOwners)
	trustedRegistries := makePolicySet(cfg.Skills.Policy.TrustedRegistries)
	return skills.ScanWithOptions(skills.LoadOptions{
		Roots:          buildSkillRoots(cfg, bundledDir),
		Entries:        skillEntries(cfg),
		GlobalConfig:   configMap(cfg),
		Env:            envMap(),
		AvailableTools: toolNames,
		ApprovalPolicy: skills.ApprovalPolicy{QuarantineByDefault: cfg.Skills.Policy.QuarantineByDefault, ApprovedSkills: approved, TrustedOwners: trustedOwners, BlockedOwners: blockedOwners, TrustedRegistries: trustedRegistries},
	})
}

func makePolicySet(values []string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.TrimRight(strings.ToLower(strings.TrimSpace(value)), "/")
		if normalized == "" {
			continue
		}
		out[normalized] = struct{}{}
	}
	return out
}

func loadAvailableToolNames(ctx context.Context, cfg config.Config) map[string]struct{} {
	return loadAvailableToolNamesWithManager(ctx, cfg, nil)
}

func loadAvailableToolNamesWithManager(ctx context.Context, cfg config.Config, manager *mcp.Manager) map[string]struct{} {
	toolNames := availableToolNames(cfg.Cron.Enabled, cfg.Subagents.Enabled)
	if len(cfg.Tools.MCPServers) == 0 {
		return toolNames
	}
	if manager != nil {
		for _, name := range manager.ToolNames() {
			toolNames[name] = struct{}{}
		}
		return toolNames
	}
	manager = mcp.NewManager(cfg.Tools.MCPServers)
	manager.SetLogger(log.Printf)
	if err := manager.Connect(ctx); err != nil {
		log.Printf("mcp setup failed: %v", err)
		return toolNames
	}
	defer func() {
		if err := manager.Close(); err != nil {
			log.Printf("mcp close failed: %v", err)
		}
	}()
	for _, name := range manager.ToolNames() {
		toolNames[name] = struct{}{}
	}
	return toolNames
}

func buildSkillRoots(cfg config.Config, bundledDir string) []skills.Root {
	var roots []skills.Root
	for _, extra := range cfg.Skills.Load.ExtraDirs {
		if strings.TrimSpace(extra) == "" {
			continue
		}
		roots = append(roots, skills.Root{Path: extra, Source: skills.SourceExtra})
	}
	if strings.TrimSpace(bundledDir) != "" {
		roots = append(roots, skills.Root{Path: bundledDir, Source: skills.SourceBundled})
	}
	if strings.TrimSpace(cfg.Skills.ManagedDir) != "" {
		roots = append(roots, skills.Root{Path: cfg.Skills.ManagedDir, Source: skills.SourceManaged})
	}
	if strings.TrimSpace(cfg.WorkspaceDir) != "" {
		roots = append(roots,
			skills.Root{Path: filepath.Join(cfg.WorkspaceDir, "workspace_skills"), Source: skills.SourceExtra, Priority: 35},
			skills.Root{Path: filepath.Join(cfg.WorkspaceDir, "skills"), Source: skills.SourceWorkspace},
		)
	}
	return roots
}

func skillEntries(cfg config.Config) map[string]skills.EntryConfig {
	out := make(map[string]skills.EntryConfig, len(cfg.Skills.Entries))
	for key, entry := range cfg.Skills.Entries {
		out[key] = skills.EntryConfig{
			Enabled: entry.Enabled,
			APIKey:  entry.APIKey,
			Env:     entry.Env,
			Config:  entry.Config,
		}
	}
	return out
}

func configMap(cfg config.Config) map[string]any {
	buf, _ := json.Marshal(cfg)
	out := map[string]any{}
	_ = json.Unmarshal(buf, &out)
	return out
}

func envMap() map[string]string {
	out := map[string]string{}
	for _, raw := range os.Environ() {
		key, value, ok := strings.Cut(raw, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func resolveInstallRoot(cfg config.Config) string {
	installDir := strings.TrimSpace(cfg.Skills.ClawHub.InstallDir)
	if installDir == "" {
		installDir = "skills"
	}
	if filepath.IsAbs(installDir) {
		return installDir
	}
	if strings.TrimSpace(cfg.Skills.ManagedDir) != "" {
		return cfg.Skills.ManagedDir
	}
	return filepath.Join(filepath.Dir(config.DefaultPath()), installDir)
}

func availableToolNames(includeCron, includeSubagents bool) map[string]struct{} {
	names := []string{
		"exec",
		"read_file",
		"write_file",
		"edit_file",
		"list_dir",
		"web_fetch",
		"web_search",
		"memory_set_pinned",
		"memory_add_note",
		"memory_search",
		"memory_recent",
		"memory_get_pinned",
		"send_message",
		"read_skill",
		"run_skill_script",
	}
	if includeCron {
		names = append(names, "cron")
	}
	if includeSubagents {
		names = append(names, "spawn_subagent")
	}
	sort.Strings(names)
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out
}

func newClawHubClient(cfg config.Config) *clawhub.Client {
	return clawhub.New(cfg.Skills.ClawHub.SiteURL, cfg.Skills.ClawHub.RegistryURL)
}

func findInstalledSkill(installed []clawhub.InstalledSkill, raw string) (clawhub.InstalledSkill, error) {
	target := strings.TrimSpace(raw)
	for _, item := range installed {
		if item.Name == target || item.Origin.Slug == target {
			return item, nil
		}
	}
	return clawhub.InstalledSkill{}, fmt.Errorf("installed skill not found: %s", raw)
}

func printReasons(w io.Writer, label string, values []string) {
	if len(values) == 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "%s: %s\n", label, strings.Join(values, "; "))
}
````

## File: internal/agent/subagents.go
````go
package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	"or3-intern/internal/tools"
)

const (
	subagentClaimRetryDelay = 25 * time.Millisecond
	subagentFinalizeTimeout = 5 * time.Second
)

type SubagentManager struct {
	DB              *db.DB
	Runtime         *Runtime
	Deliver         Deliverer
	MaxConcurrent   int
	MaxQueued       int
	TaskTimeout     time.Duration
	BackgroundTools func() *tools.Registry
	Jobs            *JobRegistry

	mu       sync.Mutex
	started  bool
	ctx      context.Context
	cancel   context.CancelFunc
	notifyCh chan struct{}
	wg       sync.WaitGroup
}

type ServiceSubagentRequest struct {
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	AllowedTools     []string
	RestrictTools    bool
	ProfileName      string
	Channel          string
	ReplyTo          string
	Meta             map[string]any
	Timeout          time.Duration
}

type subagentJobMetadata struct {
	ProfileName    string                  `json:"profile_name,omitempty"`
	AllowedTools   []string                `json:"allowed_tools,omitempty"`
	RestrictTools  bool                    `json:"restrict_tools,omitempty"`
	PromptSnapshot []providers.ChatMessage `json:"prompt_snapshot,omitempty"`
	TimeoutSeconds int                     `json:"timeout_seconds,omitempty"`
	ServiceMeta    map[string]any          `json:"service_meta,omitempty"`
}

func (m *SubagentManager) Start(ctx context.Context) error {
	if m == nil {
		return fmt.Errorf("subagent manager is nil")
	}
	if m.DB == nil {
		return fmt.Errorf("subagent db not configured")
	}
	if m.Runtime == nil {
		return fmt.Errorf("subagent runtime not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if m.MaxConcurrent <= 0 {
		m.MaxConcurrent = 1
	}
	if m.MaxQueued <= 0 {
		m.MaxQueued = 32
	}
	if m.TaskTimeout <= 0 {
		m.TaskTimeout = 5 * time.Minute
	}
	running, err := m.DB.ListRunningSubagentJobs(ctx)
	if err != nil {
		return err
	}
	queued, err := m.DB.ListQueuedSubagentJobs(ctx)
	if err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.notifyCh = make(chan struct{}, m.MaxConcurrent)
	m.started = true
	for i := 0; i < m.MaxConcurrent; i++ {
		m.wg.Add(1)
		go m.workerLoop()
	}
	for _, job := range running {
		m.reconcileInterruptedJob(job, "subagent interrupted during restart")
	}
	if len(queued) > 0 {
		m.signalN(min(len(queued), m.MaxConcurrent))
	}
	return nil
}

func (m *SubagentManager) Stop(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	if !m.started {
		m.mu.Unlock()
		return nil
	}
	cancel := m.cancel
	m.started = false
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *SubagentManager) Enqueue(ctx context.Context, req tools.SpawnRequest) (tools.SpawnJob, error) {
	if m == nil || m.DB == nil {
		return tools.SpawnJob{}, fmt.Errorf("background subagents disabled")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return tools.SpawnJob{}, fmt.Errorf("empty task")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return tools.SpawnJob{}, fmt.Errorf("missing parent session")
	}
	jobID := newSubagentID()
	metadata := map[string]any{}
	if profileName := strings.TrimSpace(req.ProfileName); profileName != "" {
		metadata["profile_name"] = profileName
	}
	metadataJSON := mustMetadataJSON(metadata)
	job := db.SubagentJob{
		ID:               jobID,
		ParentSessionKey: parentSessionKey,
		ChildSessionKey:  childSessionKey(parentSessionKey, jobID),
		Channel:          strings.TrimSpace(req.Channel),
		ReplyTo:          strings.TrimSpace(req.To),
		Task:             task,
		Status:           db.SubagentStatusQueued,
		MetadataJSON:     metadataJSON,
	}
	if err := m.DB.EnqueueSubagentJobLimited(ctx, job, m.MaxQueued); err != nil {
		return tools.SpawnJob{}, err
	}
	if m.Jobs != nil {
		m.Jobs.RegisterWithID(job.ID, "subagent")
		m.Jobs.Publish(job.ID, "queued", map[string]any{"status": db.SubagentStatusQueued, "child_session_key": job.ChildSessionKey})
	}
	m.signal()
	return tools.SpawnJob{ID: job.ID, ChildSessionKey: job.ChildSessionKey}, nil
}

func (m *SubagentManager) EnqueueService(ctx context.Context, req ServiceSubagentRequest) (tools.SpawnJob, error) {
	if m == nil || m.DB == nil {
		return tools.SpawnJob{}, fmt.Errorf("background subagents disabled")
	}
	parentSessionKey := strings.TrimSpace(req.ParentSessionKey)
	if parentSessionKey == "" {
		return tools.SpawnJob{}, fmt.Errorf("missing parent session")
	}
	task := strings.TrimSpace(req.Task)
	if task == "" {
		return tools.SpawnJob{}, fmt.Errorf("empty task")
	}
	jobID := newSubagentID()
	metadata := subagentJobMetadata{
		ProfileName:    strings.TrimSpace(req.ProfileName),
		AllowedTools:   append([]string{}, req.AllowedTools...),
		RestrictTools:  req.RestrictTools,
		PromptSnapshot: append([]providers.ChatMessage{}, req.PromptSnapshot...),
		ServiceMeta:    cloneMap(req.Meta),
	}
	if req.Timeout > 0 {
		metadata.TimeoutSeconds = int(req.Timeout / time.Second)
	}
	job := db.SubagentJob{
		ID:               jobID,
		ParentSessionKey: parentSessionKey,
		ChildSessionKey:  childSessionKey(parentSessionKey, jobID),
		Channel:          strings.TrimSpace(req.Channel),
		ReplyTo:          strings.TrimSpace(req.ReplyTo),
		Task:             task,
		Status:           db.SubagentStatusQueued,
		MetadataJSON:     mustMetadataJSON(metadata),
	}
	if err := m.DB.EnqueueSubagentJobLimited(ctx, job, m.MaxQueued); err != nil {
		return tools.SpawnJob{}, err
	}
	if m.Jobs != nil {
		m.Jobs.RegisterWithID(job.ID, "subagent")
		m.Jobs.Publish(job.ID, "queued", map[string]any{"status": db.SubagentStatusQueued, "child_session_key": job.ChildSessionKey})
	}
	m.signal()
	return tools.SpawnJob{ID: job.ID, ChildSessionKey: job.ChildSessionKey}, nil
}

func (m *SubagentManager) workerLoop() {
	defer m.wg.Done()
	for {
		ran, err := m.runOnce()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				log.Printf("subagent worker error: %v", err)
			}
		}
		if ran {
			continue
		}
		select {
		case <-m.ctx.Done():
			return
		case <-m.notifyCh:
		case <-time.After(subagentClaimRetryDelay):
		}
	}
}

func (m *SubagentManager) runOnce() (bool, error) {
	job, err := m.DB.ClaimNextSubagentJob(m.ctx)
	if err != nil || job == nil {
		return false, err
	}
	m.executeJob(*job)
	return true, nil
}

func (m *SubagentManager) executeJob(job db.SubagentJob) {
	timeout := m.jobTimeout(job)
	runCtx, cancel := context.WithTimeout(m.ctx, timeout)
	defer cancel()
	if m.Jobs != nil {
		m.Jobs.AttachCancel(job.ID, cancel)
		m.Jobs.Publish(job.ID, "started", map[string]any{"status": db.SubagentStatusRunning, "child_session_key": job.ChildSessionKey})
	}
	result, err := m.runJob(runCtx, job)
	if err != nil {
		reason := strings.TrimSpace(err.Error())
		switch {
		case errors.Is(err, context.Canceled), errors.Is(runCtx.Err(), context.Canceled):
			m.finalizeJob(runCtx, job, db.SubagentStatusInterrupted, "", "", reasonOrDefault(reason, "subagent interrupted"), true)
		case errors.Is(err, context.DeadlineExceeded), errors.Is(runCtx.Err(), context.DeadlineExceeded):
			m.finalizeJob(runCtx, job, db.SubagentStatusFailed, "", "", reasonOrDefault(reason, "subagent timed out"), true)
		default:
			m.finalizeJob(runCtx, job, db.SubagentStatusFailed, "", "", reasonOrDefault(reason, "subagent failed"), true)
		}
		return
	}
	m.finalizeJob(runCtx, job, db.SubagentStatusSucceeded, result.Preview, result.ArtifactID, "", true)
}

func (m *SubagentManager) runJob(ctx context.Context, job db.SubagentJob) (BackgroundRunResult, error) {
	metadata := parseSubagentJobMetadata(job.MetadataJSON)
	promptSnapshot := append([]providers.ChatMessage{}, metadata.PromptSnapshot...)
	var err error
	if len(promptSnapshot) == 0 {
		promptSnapshot, err = m.Runtime.BuildPromptSnapshot(ctx, job.ParentSessionKey, job.Task)
		if err != nil {
			return BackgroundRunResult{}, err
		}
	}
	if m.Jobs != nil {
		ctx = ContextWithConversationObserver(ctx, m.Jobs.Observer(job.ID))
		ctx = ContextWithStreamingChannel(ctx, NullStreamer{})
	}
	return m.Runtime.RunBackground(ctx, BackgroundRunInput{
		SessionKey:       job.ChildSessionKey,
		ParentSessionKey: job.ParentSessionKey,
		Task:             job.Task,
		PromptSnapshot:   promptSnapshot,
		Tools:            toolRegistryWithAllowlist(m.backgroundTools(), metadata.AllowedTools, metadata.RestrictTools),
		Meta: map[string]any{
			"subagent_job_id":    job.ID,
			"parent_session_key": job.ParentSessionKey,
			"profile_name":       metadata.ProfileName,
		},
		Channel: job.Channel,
		ReplyTo: job.ReplyTo,
	})
}

func profileNameFromMetadata(raw string) string {
	return parseSubagentJobMetadata(raw).ProfileName
}

func (m *SubagentManager) backgroundTools() *tools.Registry {
	if m.BackgroundTools != nil {
		return m.BackgroundTools()
	}
	return tools.NewRegistry()
}

func (m *SubagentManager) finalizeJob(baseCtx context.Context, job db.SubagentJob, status string, preview string, artifactID string, errText string, deliver bool) {
	finalizeCtx, cancel := boundedContext(baseCtx, subagentFinalizeTimeout)
	defer cancel()
	success := status == db.SubagentStatusSucceeded
	text := formatParentSubagentSummary(job, success, preview, artifactID, errText)
	payload := map[string]any{
		"subagent_job_id": job.ID,
		"child_session":   job.ChildSessionKey,
		"status":          status,
	}
	if artifactID != "" {
		payload["artifact_id"] = artifactID
	}
	if err := m.DB.FinalizeSubagentJob(finalizeCtx, job, status, preview, artifactID, errText, text, payload); err != nil {
		log.Printf("finalize subagent failed: job=%s err=%v", job.ID, err)
		return
	}
	if m.Jobs != nil {
		if status == db.SubagentStatusSucceeded {
			m.Jobs.Complete(job.ID, status, map[string]any{"preview": preview, "artifact_id": artifactID, "child_session_key": job.ChildSessionKey})
		} else if status == db.SubagentStatusInterrupted {
			m.Jobs.Complete(job.ID, "aborted", map[string]any{"message": errText, "child_session_key": job.ChildSessionKey})
		} else {
			m.Jobs.Fail(job.ID, errText, map[string]any{"child_session_key": job.ChildSessionKey})
		}
	}
	if deliver {
		m.deliverCompletion(finalizeCtx, job, success, preview, artifactID, errText)
	}
}

func (m *SubagentManager) Abort(ctx context.Context, id string) error {
	if m == nil || m.DB == nil {
		return fmt.Errorf("background subagents disabled")
	}
	if m.Jobs != nil && m.Jobs.Cancel(id) {
		return nil
	}
	job, ok, err := m.DB.AbortQueuedSubagentJob(ctx, id, "subagent aborted before execution")
	if err != nil {
		return err
	}
	if !ok {
		stored, exists, lookupErr := m.DB.GetSubagentJob(ctx, id)
		if lookupErr != nil {
			return lookupErr
		}
		if !exists {
			return fmt.Errorf("job not found")
		}
		if stored.Status == db.SubagentStatusQueued {
			return fmt.Errorf("job is not abortable")
		}
		return fmt.Errorf("job is not abortable")
	}
	if m.Jobs != nil {
		m.Jobs.Complete(id, "aborted", map[string]any{"message": "subagent aborted before execution", "child_session_key": job.ChildSessionKey})
	}
	return nil
}

func (m *SubagentManager) jobTimeout(job db.SubagentJob) time.Duration {
	metadata := parseSubagentJobMetadata(job.MetadataJSON)
	if metadata.TimeoutSeconds > 0 {
		return time.Duration(metadata.TimeoutSeconds) * time.Second
	}
	if m.TaskTimeout <= 0 {
		return 5 * time.Minute
	}
	return m.TaskTimeout
}

func parseSubagentJobMetadata(raw string) subagentJobMetadata {
	if strings.TrimSpace(raw) == "" {
		return subagentJobMetadata{}
	}
	var metadata subagentJobMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		var legacy map[string]any
		if legacyErr := json.Unmarshal([]byte(raw), &legacy); legacyErr != nil {
			return subagentJobMetadata{}
		}
		metadata.ProfileName = strings.TrimSpace(fmt.Sprint(legacy["profile_name"]))
		return metadata
	}
	metadata.ProfileName = strings.TrimSpace(metadata.ProfileName)
	return metadata
}

func mustMetadataJSON(payload any) string {
	if payload == nil {
		return "{}"
	}
	b, err := json.Marshal(payload)
	if err != nil || len(b) == 0 {
		return "{}"
	}
	return string(b)
}

func (m *SubagentManager) reconcileInterruptedJob(job db.SubagentJob, reason string) {
	m.finalizeJob(m.ctx, job, db.SubagentStatusInterrupted, "", "", reasonOrDefault(reason, "subagent interrupted during restart"), false)
}

func (m *SubagentManager) deliverCompletion(ctx context.Context, job db.SubagentJob, success bool, preview string, artifactID string, errText string) {
	deliverer := m.Deliver
	if deliverer == nil && m.Runtime != nil {
		deliverer = m.Runtime.Deliver
	}
	if deliverer == nil || strings.TrimSpace(job.Channel) == "" || strings.TrimSpace(job.ReplyTo) == "" {
		return
	}
	text := formatDeliverySubagentSummary(job, success, preview, artifactID, errText)
	if err := deliverer.Deliver(ctx, job.Channel, job.ReplyTo, text); err != nil {
		log.Printf("subagent delivery failed: job=%s err=%v", job.ID, err)
	}
}

func (m *SubagentManager) signal() {
	m.signalN(1)
}

func (m *SubagentManager) signalN(n int) {
	if n <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.started || m.notifyCh == nil {
		return
	}
	for i := 0; i < n; i++ {
		select {
		case m.notifyCh <- struct{}{}:
		default:
			return
		}
	}
}

func boundedContext(base context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if base == nil {
		base = context.Background()
	} else {
		base = context.WithoutCancel(base)
	}
	if timeout <= 0 {
		return context.WithCancel(base)
	}
	return context.WithTimeout(base, timeout)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func childSessionKey(parentSessionKey, jobID string) string {
	return parentSessionKey + ":subagent:" + jobID
}

func newSubagentID() string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("job-%d", time.Now().UnixNano())
	}
	return "job-" + hex.EncodeToString(raw[:])
}

func reasonOrDefault(reason string, fallback string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fallback
	}
	return reason
}

func formatParentSubagentSummary(job db.SubagentJob, success bool, preview string, artifactID string, errText string) string {
	if success {
		text := fmt.Sprintf("Background job %s completed: %s", job.ID, preview)
		if artifactID != "" {
			text += fmt.Sprintf("\nartifact_id=%s", artifactID)
		}
		return text
	}
	return fmt.Sprintf("Background job %s failed: %s", job.ID, reasonOrDefault(errText, "unknown error"))
}

func formatDeliverySubagentSummary(job db.SubagentJob, success bool, preview string, artifactID string, errText string) string {
	if success {
		text := fmt.Sprintf("Background job %s finished. %s", job.ID, preview)
		if artifactID != "" {
			text += fmt.Sprintf("\nartifact_id=%s", artifactID)
		}
		return text
	}
	return fmt.Sprintf("Background job %s failed. %s", job.ID, reasonOrDefault(errText, "unknown error"))
}
````

## File: internal/channels/discord/discord.go
````go
package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.DiscordChannelConfig
	HTTP   *http.Client
	Dialer *websocket.Dialer
	Artifacts *artifacts.Store
	MaxMediaBytes int
	IsolatePeers bool

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	botID  string
}

func (c *Channel) Name() string { return "discord" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.Token) == "" {
		return fmt.Errorf("discord token not configured")
	}
	url := strings.TrimSpace(c.Config.GatewayURL)
	if url == "" {
		var resp struct{ URL string `json:"url"` }
		if err := c.getJSON(ctx, c.apiBase()+"/gateway/bot", &resp); err != nil {
			return err
		}
		url = resp.URL
	}
	if url == "" {
		return fmt.Errorf("discord gateway url missing")
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
	c.mu.Unlock()
	go c.readLoop(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nil
	c.cancel = nil
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("discord channel id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.postMultipart(ctx, channelID, text, mediaPaths, meta)
	}
	payload := map[string]any{"content": text}
	if replyID, ok := meta["message_reference"].(string); ok && replyID != "" {
		payload["message_reference"] = map[string]any{"message_id": replyID}
	}
	return c.postJSON(ctx, c.apiBase()+"/channels/"+channelID+"/messages", payload, nil)
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	var heartbeatTicker *time.Ticker
	defer func() {
		if heartbeatTicker != nil {
			heartbeatTicker.Stop()
		}
	}()
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var frame gatewayFrame
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		switch frame.Op {
		case 10:
			var hello struct { HeartbeatInterval float64 `json:"heartbeat_interval"` }
			_ = json.Unmarshal(frame.D, &hello)
			_ = conn.WriteJSON(map[string]any{"op": 2, "d": map[string]any{"token": c.Config.Token, "intents": 513, "properties": map[string]string{"$os": "linux", "$browser": "or3-intern", "$device": "or3-intern"}}})
			interval := time.Duration(int64(hello.HeartbeatInterval)) * time.Millisecond
			if interval > 0 {
				heartbeatTicker = time.NewTicker(interval)
				go func() {
					for {
						select {
						case <-ctx.Done():
							return
						case <-heartbeatTicker.C:
							_ = conn.WriteJSON(map[string]any{"op": 1, "d": nil})
						}
					}
				}()
			}
		case 0:
			switch frame.T {
			case "READY":
				var ready struct { User struct { ID string `json:"id"` } `json:"user"` }
				_ = json.Unmarshal(frame.D, &ready)
				c.botID = ready.User.ID
			case "MESSAGE_CREATE":
				var msg inboundMessage
				_ = json.Unmarshal(frame.D, &msg)
				if msg.Author.Bot {
					continue
				}
				if !c.allowedUser(msg.Author.ID) {
					continue
				}
				if c.Config.RequireMention && c.botID != "" && !mentioned(msg.Mentions, c.botID) {
					continue
				}
				clean := strings.TrimSpace(stripMention(msg.Content, c.botID))
				sessionKey := "discord:" + msg.ChannelID
				if c.IsolatePeers {
					sessionKey += ":" + msg.Author.ID
				}
				attachments, markers := c.captureAttachments(ctx, sessionKey, msg.Attachments)
				content := rootchannels.ComposeMessageText(clean, markers)
				if content == "" {
					continue
				}
				meta := map[string]any{"channel_id": msg.ChannelID, "message_reference": msg.ID, "guild_id": msg.GuildID, "is_private": strings.TrimSpace(msg.GuildID) == ""}
				if len(attachments) > 0 {
					meta["attachments"] = attachments
				}
				eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "discord", From: msg.Author.ID, Message: content, Meta: meta})
			}
		}
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://discord.com/api/v10"
	}
	return base
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("discord api error: %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) postJSON(ctx context.Context, endpoint string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api error: %s %s", resp.Status, string(body))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, refs []discordAttachment) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(refs))
	markers := make([]string, 0, len(refs))
	for _, ref := range refs {
		filename := artifacts.NormalizeFilename(ref.Filename, ref.ContentType)
		kind := artifacts.DetectKind(filename, ref.ContentType)
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && ref.Size > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := c.downloadAttachment(ctx, ref.URL)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "download failed"))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, ref.ContentType, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) downloadAttachment(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("discord attachment error: %s", resp.Status)
	}
	limit := int64(c.MaxMediaBytes)
	if limit <= 0 {
		limit = 25 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if c.MaxMediaBytes > 0 && len(data) > c.MaxMediaBytes {
		return nil, fmt.Errorf("discord attachment exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) postMultipart(ctx context.Context, channelID, text string, mediaPaths []string, meta map[string]any) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	payload := map[string]any{}
	if strings.TrimSpace(text) != "" {
		payload["content"] = text
	}
	if replyID, ok := meta["message_reference"].(string); ok && replyID != "" {
		payload["message_reference"] = map[string]any{"message_id": replyID}
	}
	payloadJSON, _ := json.Marshal(payload)
	if err := writer.WriteField("payload_json", string(payloadJSON)); err != nil {
		return err
	}
	for i, mediaPath := range mediaPaths {
		if err := c.attachFilePart(writer, i, mediaPath); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+"/channels/"+channelID+"/messages", &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+c.Config.Token)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord api error: %s %s", resp.Status, string(respBody))
	}
	return nil
}

func (c *Channel) attachFilePart(writer *multipart.Writer, index int, mediaPath string) error {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return err
	}
	if c.MaxMediaBytes == 0 {
		return fmt.Errorf("media attachments disabled by config")
	}
	if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
		return fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		return err
	}
	defer file.Close()
	part, err := writer.CreateFormFile(fmt.Sprintf("files[%d]", index), filepath.Base(mediaPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	return nil
}

func (c *Channel) allowedUser(user string) bool {
	if len(c.Config.AllowedUserIDs) == 0 {
		return c.Config.OpenAccess
	}
	for _, allowed := range c.Config.AllowedUserIDs {
		if strings.TrimSpace(allowed) == user {
			return true
		}
	}
	return false
}

func mentioned(mentions []mention, botID string) bool {
	for _, m := range mentions {
		if m.ID == botID {
			return true
		}
	}
	return false
}

func stripMention(content, botID string) string {
	if botID == "" {
		return content
	}
	content = strings.ReplaceAll(content, "<@"+botID+">", "")
	content = strings.ReplaceAll(content, "<@!"+botID+">", "")
	return content
}

type gatewayFrame struct {
	Op int             `json:"op"`
	T  string          `json:"t"`
	D  json.RawMessage `json:"d"`
}

type mention struct {
	ID string `json:"id"`
}

type inboundMessage struct {
	ID          string    `json:"id"`
	ChannelID   string    `json:"channel_id"`
	GuildID     string    `json:"guild_id"`
	Content     string    `json:"content"`
	Mentions    []mention `json:"mentions"`
	Attachments []discordAttachment `json:"attachments"`
	Author    struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	} `json:"author"`
}

type discordAttachment struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}
````

## File: internal/channels/slack/slack.go
````go
package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/config"
)

type Channel struct {
	Config        config.SlackChannelConfig
	HTTP          *http.Client
	Dialer        *websocket.Dialer
	Artifacts     *artifacts.Store
	MaxMediaBytes int
	IsolatePeers  bool

	mu     sync.Mutex
	conn   *websocket.Conn
	cancel context.CancelFunc
	botID  string
}

func (c *Channel) Name() string { return "slack" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.AppToken) == "" || strings.TrimSpace(c.Config.BotToken) == "" {
		return fmt.Errorf("slack tokens not configured")
	}
	url, err := c.openSocketURL(ctx)
	if err != nil {
		return err
	}
	dialer := c.Dialer
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}
	conn, _, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		return err
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.mu.Lock()
	c.conn = conn
	c.cancel = cancel
	c.mu.Unlock()
	go c.readLoop(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.cancel = nil
	c.conn = nil
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	channelID := strings.TrimSpace(to)
	if channelID == "" {
		channelID = strings.TrimSpace(c.Config.DefaultChannelID)
	}
	if channelID == "" {
		return fmt.Errorf("slack channel id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.uploadFiles(ctx, channelID, text, mediaPaths, meta)
	}
	payload := map[string]any{"channel": channelID, "text": text}
	if threadTS, ok := meta["thread_ts"].(string); ok && threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	return c.postJSON(ctx, c.apiBase()+"/chat.postMessage", c.Config.BotToken, payload, nil)
}

func (c *Channel) readLoop(ctx context.Context, eventBus *bus.Bus) {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var envelope socketEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if envelope.EnvelopeID != "" {
			_ = conn.WriteJSON(map[string]any{"envelope_id": envelope.EnvelopeID})
		}
		if envelope.Type == "hello" {
			continue
		}
		if envelope.Type != "events_api" || envelope.Payload.Event.Type != "message" {
			continue
		}
		ev := envelope.Payload.Event
		if ev.BotID != "" || ev.User == "" {
			continue
		}
		if !c.allowedUser(ev.User) {
			continue
		}
		if envelope.Payload.Authorizations[0].UserID != "" && c.botID == "" {
			c.botID = envelope.Payload.Authorizations[0].UserID
		}
		if c.Config.RequireMention && c.botID != "" && !strings.Contains(ev.Text, "<@"+c.botID+">") && len(ev.Files) == 0 {
			continue
		}
		clean := strings.TrimSpace(strings.ReplaceAll(ev.Text, "<@"+c.botID+">", ""))
		sessionKey := "slack:" + ev.Channel
		if c.IsolatePeers {
			sessionKey += ":" + ev.User
		}
		attachments, markers := c.captureFiles(ctx, sessionKey, ev.Files)
		content := rootchannels.ComposeMessageText(clean, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{"channel_id": ev.Channel, "thread_ts": ev.ThreadTS, "channel_type": ev.ChannelType}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{Type: bus.EventUserMessage, SessionKey: sessionKey, Channel: "slack", From: ev.User, Message: content, Meta: meta})
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (c *Channel) openSocketURL(ctx context.Context) (string, error) {
	var resp struct {
		OK  bool   `json:"ok"`
		URL string `json:"url"`
	}
	if err := c.postJSON(ctx, c.apiBase()+"/apps.connections.open", c.Config.AppToken, nil, &resp); err != nil {
		return "", err
	}
	if !resp.OK || resp.URL == "" {
		return "", fmt.Errorf("slack socket url missing")
	}
	return resp.URL, nil
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://slack.com/api"
	}
	return base
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) postJSON(ctx context.Context, endpoint, token string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) postForm(ctx context.Context, endpoint, token string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack api error: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Channel) captureFiles(ctx context.Context, sessionKey string, files []slackFile) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, len(files))
	markers := make([]string, 0, len(files))
	for _, file := range files {
		filename := artifacts.NormalizeFilename(file.Name, file.Mimetype)
		kind := artifacts.DetectKind(filename, file.Mimetype)
		if c.MaxMediaBytes == 0 {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "disabled by config"))
			continue
		}
		if c.MaxMediaBytes > 0 && file.Size > int64(c.MaxMediaBytes) {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "too large"))
			continue
		}
		if c.Artifacts == nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "storage unavailable"))
			continue
		}
		data, err := c.downloadPrivateFile(ctx, firstNonEmpty(file.URLPrivateDownload, file.URLPrivate))
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "download failed"))
			continue
		}
		att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, file.Mimetype, data)
		if err != nil {
			markers = append(markers, artifacts.FailureMarker(kind, filename, "save failed"))
			continue
		}
		attachments = append(attachments, att)
		markers = append(markers, artifacts.Marker(att))
	}
	return attachments, markers
}

func (c *Channel) downloadPrivateFile(ctx context.Context, endpoint string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Config.BotToken)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("slack file download error: %s", resp.Status)
	}
	limit := int64(c.MaxMediaBytes)
	if limit <= 0 {
		limit = 25 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if c.MaxMediaBytes > 0 && len(data) > c.MaxMediaBytes {
		return nil, fmt.Errorf("slack file exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) uploadFiles(ctx context.Context, channelID, text string, mediaPaths []string, meta map[string]any) error {
	files := make([]map[string]any, 0, len(mediaPaths))
	for _, mediaPath := range mediaPaths {
		fileID, title, err := c.uploadFile(ctx, mediaPath)
		if err != nil {
			return err
		}
		files = append(files, map[string]any{"id": fileID, "title": title})
	}
	payload := map[string]any{
		"channel_id": channelID,
		"files":      files,
	}
	if strings.TrimSpace(text) != "" {
		payload["initial_comment"] = text
	}
	if threadTS, ok := meta["thread_ts"].(string); ok && threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	var resp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := c.postJSON(ctx, c.apiBase()+"/files.completeUploadExternal", c.Config.BotToken, payload, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("slack complete upload failed: %s", resp.Error)
	}
	return nil
}

func (c *Channel) uploadFile(ctx context.Context, mediaPath string) (string, string, error) {
	info, err := os.Stat(mediaPath)
	if err != nil {
		return "", "", err
	}
	if c.MaxMediaBytes == 0 {
		return "", "", fmt.Errorf("media attachments disabled by config")
	}
	if c.MaxMediaBytes > 0 && info.Size() > int64(c.MaxMediaBytes) {
		return "", "", fmt.Errorf("media path exceeds maxMediaBytes: %s", mediaPath)
	}
	var start struct {
		OK        bool   `json:"ok"`
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
		Error     string `json:"error"`
	}
	form := url.Values{}
	form.Set("filename", filepath.Base(mediaPath))
	form.Set("length", fmt.Sprintf("%d", info.Size()))
	if err := c.postForm(ctx, c.apiBase()+"/files.getUploadURLExternal", c.Config.BotToken, form, &start); err != nil {
		return "", "", err
	}
	if !start.OK || start.UploadURL == "" || start.FileID == "" {
		return "", "", fmt.Errorf("slack upload init failed: %s", start.Error)
	}
	file, err := os.Open(mediaPath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, start.UploadURL, file)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	resp, err := c.client().Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("slack upload error: %s", resp.Status)
	}
	return start.FileID, filepath.Base(mediaPath), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *Channel) allowedUser(user string) bool {
	if len(c.Config.AllowedUserIDs) == 0 {
		return c.Config.OpenAccess
	}
	for _, allowed := range c.Config.AllowedUserIDs {
		if strings.TrimSpace(allowed) == user {
			return true
		}
	}
	return false
}

type socketEnvelope struct {
	EnvelopeID string `json:"envelope_id"`
	Type       string `json:"type"`
	Payload    struct {
		Event struct {
			Type        string      `json:"type"`
			Text        string      `json:"text"`
			User        string      `json:"user"`
			BotID       string      `json:"bot_id"`
			Channel     string      `json:"channel"`
			ChannelType string      `json:"channel_type"`
			ThreadTS    string      `json:"thread_ts"`
			Files       []slackFile `json:"files"`
		} `json:"event"`
		Authorizations []struct {
			UserID string `json:"user_id"`
		} `json:"authorizations"`
	} `json:"payload"`
}

type slackFile struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Mimetype           string `json:"mimetype"`
	Filetype           string `json:"filetype"`
	Size               int64  `json:"size"`
	URLPrivate         string `json:"url_private"`
	URLPrivateDownload string `json:"url_private_download"`
}
````

## File: internal/channels/telegram/telegram.go
````go
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/config"
)

type Channel struct {
	Config config.TelegramChannelConfig
	HTTP   *http.Client
	Artifacts *artifacts.Store
	MaxMediaBytes int
	IsolatePeers bool

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	offset  int64
}

func (c *Channel) Name() string { return "telegram" }

func (c *Channel) Start(ctx context.Context, eventBus *bus.Bus) error {
	if strings.TrimSpace(c.Config.Token) == "" {
		return fmt.Errorf("telegram token not configured")
	}
	if eventBus == nil {
		return fmt.Errorf("event bus not configured")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.running = true
	go c.poll(childCtx, eventBus)
	return nil
}

func (c *Channel) Stop(ctx context.Context) error {
	_ = ctx
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
	c.cancel = nil
	c.running = false
	return nil
}

func (c *Channel) Deliver(ctx context.Context, to, text string, meta map[string]any) error {
	chatID := strings.TrimSpace(to)
	if chatID == "" {
		chatID = strings.TrimSpace(c.Config.DefaultChatID)
	}
	if chatID == "" {
		return fmt.Errorf("telegram target chat id required")
	}
	mediaPaths := rootchannels.MediaPaths(meta)
	if len(mediaPaths) > 0 {
		return c.deliverMedia(ctx, chatID, text, mediaPaths, meta)
	}
	payload := map[string]any{"chat_id": chatID, "text": text}
	if replyID, ok := meta["reply_to_message_id"].(int64); ok && replyID > 0 {
		payload["reply_to_message_id"] = replyID
	}
	return c.postJSON(ctx, "/sendMessage", payload, nil)
}

func (c *Channel) poll(ctx context.Context, eventBus *bus.Bus) {
	interval := time.Duration(c.Config.PollSeconds) * time.Second
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.fetchUpdates(ctx, eventBus); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}

}

func (c *Channel) fetchUpdates(ctx context.Context, eventBus *bus.Bus) error {
	query := map[string]string{"timeout": "0"}
	c.mu.Lock()
	if c.offset > 0 {
		query["offset"] = strconv.FormatInt(c.offset, 10)
	}
	c.mu.Unlock()
	var updates []update
	if err := c.getJSON(ctx, "/getUpdates", query, &updates); err != nil {
		return err
	}
	for _, update := range updates {
		c.mu.Lock()
		if next := update.UpdateID + 1; next > c.offset {
			c.offset = next
		}
		c.mu.Unlock()
		msg := update.Message
		chatID := strconv.FormatInt(msg.Chat.ID, 10)
		if !c.allowedChat(chatID) {
			continue
		}
		sessionKey := "telegram:" + chatID
		if c.IsolatePeers && !strings.EqualFold(strings.TrimSpace(msg.Chat.Type), "private") {
			sessionKey = sessionKey + ":" + strconv.FormatInt(msg.From.ID, 10)
		}
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			text = strings.TrimSpace(msg.Caption)
		}
		attachments, markers := c.captureAttachments(ctx, sessionKey, msg)
		content := rootchannels.ComposeMessageText(text, markers)
		if content == "" {
			continue
		}
		meta := map[string]any{
			"chat_id":             chatID,
			"chat_type":           msg.Chat.Type,
			"message_id":          msg.MessageID,
			"reply_to_message_id": int64(msg.MessageID),
			"username":            msg.From.Username,
		}
		if msg.MediaGroupID != "" {
			meta["media_group_id"] = msg.MediaGroupID
		}
		if len(attachments) > 0 {
			meta["attachments"] = attachments
		}
		eventBus.Publish(bus.Event{
			Type:       bus.EventUserMessage,
			SessionKey: sessionKey,
			Channel:    "telegram",
			From:       strconv.FormatInt(msg.From.ID, 10),
			Message:    content,
			Meta:       meta,
		})
	}
	return nil
}

func (c *Channel) allowedChat(chatID string) bool {
	if len(c.Config.AllowedChatIDs) == 0 {
		return c.Config.OpenAccess
	}
	for _, allowed := range c.Config.AllowedChatIDs {
		if strings.TrimSpace(allowed) == chatID {
			return true
		}
	}
	return false
}

func (c *Channel) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Channel) apiBase() string {
	base := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	return base + "/bot" + c.Config.Token
}

func (c *Channel) getJSON(ctx context.Context, path string, query map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiBase()+path, nil)
	if err != nil {
		return err
	}
	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *Channel) postJSON(ctx context.Context, path string, payload any, out any) error {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(envelope.Result, out)
}

func (c *Channel) captureAttachments(ctx context.Context, sessionKey string, msg inboundMessage) ([]artifacts.Attachment, []string) {
	attachments := make([]artifacts.Attachment, 0, 4)
	markers := make([]string, 0, 4)

	// Telegram media groups are processed one update at a time in v1.
	if len(msg.Photo) > 0 {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Photo[len(msg.Photo)-1].FileID,
			FileSize: msg.Photo[len(msg.Photo)-1].FileSize,
			Filename: "photo.jpg",
			Mime:     "image/jpeg",
			Kind:     artifacts.KindImage,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Voice.FileID != "" {
		filename := "voice.ogg"
		if msg.Voice.FileUniqueID != "" {
			filename = msg.Voice.FileUniqueID + ".ogg"
		}
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Voice.FileID,
			FileSize: msg.Voice.FileSize,
			Filename: filename,
			Mime:     "audio/ogg",
			Kind:     artifacts.KindAudio,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Audio.FileID != "" {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Audio.FileID,
			FileSize: msg.Audio.FileSize,
			Filename: msg.Audio.FileName,
			Mime:     msg.Audio.MimeType,
			Kind:     artifacts.KindAudio,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	if msg.Document.FileID != "" {
		att, marker := c.captureRemoteAttachment(ctx, sessionKey, remoteAttachment{
			FileID:   msg.Document.FileID,
			FileSize: msg.Document.FileSize,
			Filename: msg.Document.FileName,
			Mime:     msg.Document.MimeType,
			Kind:     artifacts.KindFile,
		})
		if marker != "" {
			markers = append(markers, marker)
		}
		if att.ArtifactID != "" {
			attachments = append(attachments, att)
		}
	}
	return attachments, markers
}

type remoteAttachment struct {
	FileID   string
	FileSize int64
	Filename string
	Mime     string
	Kind     string
}

func (c *Channel) captureRemoteAttachment(ctx context.Context, sessionKey string, remote remoteAttachment) (artifacts.Attachment, string) {
	filename := artifacts.NormalizeFilename(remote.Filename, remote.Mime)
	if remote.Kind == "" {
		remote.Kind = artifacts.DetectKind(filename, remote.Mime)
	}
	if c.MaxMediaBytes == 0 {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "disabled by config")
	}
	if c.MaxMediaBytes > 0 && remote.FileSize > int64(c.MaxMediaBytes) {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "too large")
	}
	if c.Artifacts == nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "storage unavailable")
	}
	info, err := c.getFile(ctx, remote.FileID)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "download failed")
	}
	if c.MaxMediaBytes > 0 && info.FileSize > int64(c.MaxMediaBytes) {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "too large")
	}
	data, err := c.downloadFile(ctx, info.FilePath)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "download failed")
	}
	att, err := c.Artifacts.SaveNamed(ctx, sessionKey, filename, firstNonEmpty(remote.Mime, mime.TypeByExtension(filepath.Ext(filename))), data)
	if err != nil {
		return artifacts.Attachment{}, artifacts.FailureMarker(remote.Kind, filename, "save failed")
	}
	return att, artifacts.Marker(att)
}

func (c *Channel) getFile(ctx context.Context, fileID string) (fileInfo, error) {
	var info fileInfo
	err := c.getJSON(ctx, "/getFile", map[string]string{"file_id": fileID}, &info)
	return info, err
}

func (c *Channel) downloadFile(ctx context.Context, filePath string) ([]byte, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(c.Config.APIBase), "/")
	if endpoint == "" {
		endpoint = "https://api.telegram.org"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/file/bot"+c.Config.Token+"/"+strings.TrimLeft(filePath, "/"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram file error: %s", resp.Status)
	}
	limit := int64(c.MaxMediaBytes)
	if limit <= 0 {
		limit = 25 << 20
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if c.MaxMediaBytes > 0 && len(data) > c.MaxMediaBytes {
		return nil, fmt.Errorf("telegram file exceeds maxMediaBytes")
	}
	return data, nil
}

func (c *Channel) deliverMedia(ctx context.Context, chatID, text string, mediaPaths []string, meta map[string]any) error {
	replyID := replyToMessageID(meta)
	for i, mediaPath := range mediaPaths {
		caption := ""
		if i == 0 {
			caption = text
		}
		if err := c.sendMediaFile(ctx, chatID, mediaPath, caption, replyID); err != nil {
			return err
		}
	}
	if strings.TrimSpace(text) != "" && len(mediaPaths) == 0 {
		return c.postJSON(ctx, "/sendMessage", map[string]any{"chat_id": chatID, "text": text}, nil)
	}
	return nil
}

func (c *Channel) sendMediaFile(ctx context.Context, chatID, mediaPath, caption string, replyID int64) error {
	endpoint, fieldName, mimeType := telegramSendSpec(mediaPath)
	file, err := os.Open(mediaPath)
	if err != nil {
		return err
	}
	defer file.Close()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("chat_id", chatID); err != nil {
		return err
	}
	if replyID > 0 {
		if err := writer.WriteField("reply_to_message_id", strconv.FormatInt(replyID, 10)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(caption) != "" {
		if err := writer.WriteField("caption", caption); err != nil {
			return err
		}
	}
	part, err := writer.CreateFormFile(fieldName, filepath.Base(mediaPath))
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiBase()+endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if mimeType != "" {
		req.Header.Set("X-Or3-Media-Type", mimeType)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram api error: %s", resp.Status)
	}
	var envelope apiEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if !envelope.OK {
		return fmt.Errorf("telegram api error: %s", envelope.Description)
	}
	return nil
}

func telegramSendSpec(path string) (endpoint string, fieldName string, mimeType string) {
	mimeType = mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	switch artifacts.DetectKind(path, mimeType) {
	case artifacts.KindImage:
		return "/sendPhoto", "photo", mimeType
	case artifacts.KindAudio:
		if strings.HasSuffix(strings.ToLower(path), ".ogg") || strings.HasSuffix(strings.ToLower(path), ".opus") {
			return "/sendVoice", "voice", mimeType
		}
		return "/sendAudio", "audio", mimeType
	default:
		return "/sendDocument", "document", mimeType
	}
}

func replyToMessageID(meta map[string]any) int64 {
	switch v := meta["reply_to_message_id"].(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	default:
		return 0
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type apiEnvelope struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

type update struct {
	UpdateID int64          `json:"update_id"`
	Message  inboundMessage `json:"message"`
}

type inboundMessage struct {
	MessageID    int    `json:"message_id"`
	Text         string `json:"text"`
	Caption      string `json:"caption"`
	MediaGroupID string `json:"media_group_id"`
	Chat      struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	From struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
	} `json:"from"`
	Photo []struct {
		FileID   string `json:"file_id"`
		FileSize int64  `json:"file_size"`
	} `json:"photo"`
	Voice struct {
		FileID       string `json:"file_id"`
		FileUniqueID string `json:"file_unique_id"`
		FileSize     int64  `json:"file_size"`
	} `json:"voice"`
	Audio struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	} `json:"audio"`
	Document struct {
		FileID   string `json:"file_id"`
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
		FileSize int64  `json:"file_size"`
	} `json:"document"`
}

type fileInfo struct {
	FilePath string `json:"file_path"`
	FileSize int64  `json:"file_size"`
}
````

## File: internal/memory/vector.go
````go
package memory

import (
	"bytes"
	"container/heap"
	"context"
	"encoding/binary"
	"errors"
	"math"

	"or3-intern/internal/db"
)

func PackFloat32(vec []float32) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, vec)
	return b.Bytes()
}

func UnpackFloat32(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 { return nil, errors.New("invalid float32 blob") }
	out := make([]float32, len(blob)/4)
	if err := binary.Read(bytes.NewReader(blob), binary.LittleEndian, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func Cosine(a, b []float32) float64 {
	var dot, na, nb float64
	n := len(a)
	if len(b) < n { n = len(b) }
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 { return 0 }
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

type VecCandidate struct {
	ID        int64
	Text      string
	Score     float64
	CreatedAt int64
}

type candMinHeap []VecCandidate

func (h candMinHeap) Len() int { return len(h) }
func (h candMinHeap) Less(i, j int) bool { return h[i].Score < h[j].Score }
func (h candMinHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *candMinHeap) Push(x any) { *h = append(*h, x.(VecCandidate)) }
func (h *candMinHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func VectorSearch(ctx context.Context, d *db.DB, sessionKey string, queryVec []float32, k int, scanLimit int) ([]VecCandidate, error) {
	_ = scanLimit
	queryBlob := PackFloat32(queryVec)
	rows, err := d.SearchMemoryVectors(ctx, sessionKey, queryBlob, k)
	if err != nil {
		return nil, err
	}
	out := make([]VecCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, VecCandidate{
			ID:        row.ID,
			Text:      row.Text,
			Score:     1.0 / (1.0 + row.Distance),
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

func addVectorCandidates(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}, queryVec []float32, k int, h *candMinHeap) error {
	for rows.Next() {
		var id int64
		var text string
		var emb []byte
		var src any
		var tags string
		var created int64
		if err := rows.Scan(&id, &text, &emb, &src, &tags, &created); err != nil {
			return err
		}
		v, err := UnpackFloat32(emb)
		if err != nil {
			continue
		}
		score := Cosine(queryVec, v)
		if h.Len() < k {
			heap.Push(h, VecCandidate{ID: id, Text: text, Score: score})
		} else if (*h)[0].Score < score {
			(*h)[0] = VecCandidate{ID: id, Text: text, Score: score}
			heap.Fix(h, 0)
		}
	}
	return rows.Err()
}
````

## File: internal/tools/files.go
````go
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type FileTool struct {
	Base
	Root string // allowed root (optional)
}

const (
	defaultReadFileMaxBytes  = 200000
	defaultListDirMaxEntries = 200
)

func (t *FileTool) safePath(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("missing path")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs, err = canonicalizePath(abs)
	if err != nil {
		return "", err
	}
	if t.Root != "" {
		root, err := filepath.Abs(t.Root)
		if err != nil {
			return "", err
		}
		root, err = canonicalizeRoot(root)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("path outside allowed root")
		}
	}
	return abs, nil
}

func canonicalizeRoot(root string) (string, error) {
	if _, err := os.Stat(root); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(root)
}

func canonicalizePath(abs string) (string, error) {
	if _, err := os.Lstat(abs); err == nil {
		return filepath.EvalSymlinks(abs)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	existing := abs
	missingParts := make([]string, 0, 4)
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", os.ErrNotExist
		}
		missingParts = append(missingParts, filepath.Base(existing))
		existing = parent
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	for i := len(missingParts) - 1; i >= 0; i-- {
		realExisting = filepath.Join(realExisting, missingParts[i])
	}
	return realExisting, nil
}

type ReadFile struct{ FileTool }

func (t *ReadFile) Name() string        { return "read_file" }
func (t *ReadFile) Description() string { return "Read a UTF-8 text file." }
func (t *ReadFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":     map[string]any{"type": "string"},
		"maxBytes": map[string]any{"type": "integer", "description": "Max bytes to read (default 200000)"},
	}, "required": []string{"path"}}
}
func (t *ReadFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *ReadFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	max := defaultReadFileMaxBytes
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		max = int(v)
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(max)))
	if err != nil {
		return "", err
	}
	if len(b) > max {
		b = b[:max]
	}
	return string(b), nil
}

type WriteFile struct{ FileTool }

func (t *WriteFile) Capability() CapabilityLevel { return CapabilityGuarded }
func (t *WriteFile) Name() string                { return "write_file" }
func (t *WriteFile) Description() string         { return "Write text to a file (overwrites)." }
func (t *WriteFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path":    map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
		"mkdirs":  map[string]any{"type": "boolean"},
	}, "required": []string{"path", "content"}}
}
func (t *WriteFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *WriteFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	content := fmt.Sprint(params["content"])
	mkdirs, _ := params["mkdirs"].(bool)
	if mkdirs {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(p, []byte(content), existingFileMode(p, 0o600)); err != nil {
		return "", err
	}
	return "ok", nil
}

type EditFile struct{ FileTool }

func (t *EditFile) Capability() CapabilityLevel { return CapabilityGuarded }
func (t *EditFile) Name() string                { return "edit_file" }
func (t *EditFile) Description() string {
	return "Edit a text file by applying a list of find/replace operations."
}
func (t *EditFile) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path": map[string]any{"type": "string"},
		"edits": map[string]any{"type": "array", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"find":    map[string]any{"type": "string"},
				"replace": map[string]any{"type": "string"},
				"count":   map[string]any{"type": "integer", "description": "max replacements (0=all)"},
			},
			"required": []string{"find", "replace"},
		}},
	}, "required": []string{"path", "edits"}}
}
func (t *EditFile) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *EditFile) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	s := string(b)
	rawEdits, _ := params["edits"].([]any)
	for _, e := range rawEdits {
		m, _ := e.(map[string]any)
		find := fmt.Sprint(m["find"])
		replace := fmt.Sprint(m["replace"])
		count := 0
		if v, ok := m["count"].(float64); ok {
			count = int(v)
		}
		if count <= 0 {
			s = strings.ReplaceAll(s, find, replace)
		} else {
			s = strings.Replace(s, find, replace, count)
		}
	}
	if err := os.WriteFile(p, []byte(s), existingFileMode(p, 0)); err != nil {
		return "", err
	}
	return "ok", nil
}

type ListDir struct{ FileTool }

func (t *ListDir) Name() string        { return "list_dir" }
func (t *ListDir) Description() string { return "List directory entries." }
func (t *ListDir) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"path": map[string]any{"type": "string"},
		"max":  map[string]any{"type": "integer"},
	}, "required": []string{"path"}}
}
func (t *ListDir) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *ListDir) Execute(ctx context.Context, params map[string]any) (string, error) {
	p, err := t.safePath(fmt.Sprint(params["path"]))
	if err != nil {
		return "", err
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		return "", err
	}
	max := defaultListDirMaxEntries
	if v, ok := params["max"].(float64); ok && int(v) > 0 {
		max = int(v)
	}
	type entry struct {
		Name  string `json:"name"`
		IsDir bool   `json:"isDir"`
		Size  int64  `json:"size"`
	}
	out := []entry{}
	for _, e := range ents {
		if len(out) >= max {
			break
		}
		info, _ := e.Info()
		sz := int64(0)
		if info != nil {
			sz = info.Size()
		}
		out = append(out, entry{Name: e.Name(), IsDir: e.IsDir(), Size: sz})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

func existingFileMode(path string, defaultMode os.FileMode) os.FileMode {
	if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
		return info.Mode().Perm()
	}
	if defaultMode == 0 {
		return 0o600
	}
	return defaultMode
}
````

## File: internal/tools/memory.go
````go
package tools

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
)

type MemorySetPinned struct {
	Base
	DB *db.DB
}

func (t *MemorySetPinned) Name() string { return "memory_set_pinned" }
func (t *MemorySetPinned) Description() string {
	return "Upsert a pinned memory entry (always included in prompts)."
}
func (t *MemorySetPinned) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"key":     map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
		"scope":   map[string]any{"type": "string", "description": "Optional scope override: 'global' to share across sessions"},
	}, "required": []string{"key", "content"}}
}
func (t *MemorySetPinned) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemorySetPinned) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil {
		return "", fmt.Errorf("db not set")
	}
	key := stringParam(params, "key")
	content := stringParam(params, "content")
	if key == "" || content == "" {
		return "", fmt.Errorf("missing key/content")
	}
	if err := t.DB.UpsertPinned(ctx, memoryScopeFromParams(ctx, params), key, content); err != nil {
		return "", err
	}
	return "ok", nil
}

type MemoryAddNote struct {
	Base
	DB         *db.DB
	Provider   *providers.Client
	EmbedModel string
}

func (t *MemoryAddNote) Name() string { return "memory_add_note" }
func (t *MemoryAddNote) Description() string {
	return "Add a semantic memory note to the indexed memory store."
}
func (t *MemoryAddNote) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"text":              map[string]any{"type": "string"},
		"tags":              map[string]any{"type": "string", "description": "comma-separated tags (optional)"},
		"source_message_id": map[string]any{"type": "integer", "description": "source message id (optional)"},
		"scope":             map[string]any{"type": "string", "description": "Optional scope override: 'global' to share across sessions"},
	}, "required": []string{"text"}}
}
func (t *MemoryAddNote) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemoryAddNote) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil {
		return "", fmt.Errorf("missing deps")
	}
	text := stringParam(params, "text")
	if text == "" {
		return "", fmt.Errorf("empty text")
	}
	tags := stringParam(params, "tags")
	var src sql.NullInt64
	if v, ok := params["source_message_id"].(float64); ok && int64(v) > 0 {
		src = sql.NullInt64{Int64: int64(v), Valid: true}
	}
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, text)
	if err != nil {
		return "", err
	}
	blob := memory.PackFloat32(vec)
	id, err := t.DB.InsertMemoryNote(ctx, memoryScopeFromParams(ctx, params), text, blob, src, tags)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("ok: %d", id), nil
}

type MemorySearch struct {
	Base
	DB              *db.DB
	Provider        *providers.Client
	EmbedModel      string
	VectorK         int
	FTSK            int
	TopK            int
	VectorScanLimit int
}

func (t *MemorySearch) Name() string { return "memory_search" }
func (t *MemorySearch) Description() string {
	return "Search long-term memory (hybrid semantic + keyword) and return top results."
}
func (t *MemorySearch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"query": map[string]any{"type": "string"},
		"topK":  map[string]any{"type": "integer"},
		"scope": map[string]any{"type": "string", "description": "Optional scope override: 'global' to search only shared memory"},
	}, "required": []string{"query"}}
}
func (t *MemorySearch) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemorySearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil || t.Provider == nil {
		return "", fmt.Errorf("missing deps")
	}
	q := stringParam(params, "query")
	if q == "" {
		return "", fmt.Errorf("empty query")
	}
	topK := t.TopK
	if v, ok := params["topK"].(float64); ok && int(v) > 0 {
		topK = int(v)
	}
	vec, err := t.Provider.Embed(ctx, t.EmbedModel, q)
	if err != nil {
		return "", err
	}
	r := memory.NewRetriever(t.DB)
	r.VectorScanLimit = t.VectorScanLimit
	got, err := r.Retrieve(ctx, memoryScopeFromParams(ctx, params), q, vec, t.VectorK, t.FTSK, topK)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, m := range got {
		b.WriteString(fmt.Sprintf("%d. [%s] %.4f %s\n", i+1, m.Source, m.Score, m.Text))
	}
	return b.String(), nil
}

type MemoryRecent struct {
	Base
	DB           *db.DB
	DefaultLimit int
	MaxLimit     int
	MaxChars     int
}

func (t *MemoryRecent) Name() string { return "memory_recent" }
func (t *MemoryRecent) Description() string {
	return "Fetch recent conversation messages from the current linked session scope."
}
func (t *MemoryRecent) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"limit": map[string]any{"type": "integer"},
	}}
}
func (t *MemoryRecent) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemoryRecent) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil {
		return "", fmt.Errorf("db not set")
	}
	limit := boundedPositiveInt(params["limit"], t.DefaultLimit, t.MaxLimit)
	msgs, err := t.DB.GetLastMessagesScoped(ctx, SessionFromContext(ctx), limit)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i, msg := range msgs {
		b.WriteString(fmt.Sprintf("%d. [%s/%s] %s\n", i+1, msg.SessionKey, msg.Role, compactMemoryText(msg.Content, t.MaxChars)))
	}
	return b.String(), nil
}

type MemoryGetPinned struct {
	Base
	DB       *db.DB
	MaxChars int
}

func (t *MemoryGetPinned) Name() string { return "memory_get_pinned" }
func (t *MemoryGetPinned) Description() string {
	return "Read pinned memory entries for the current session, including shared global entries."
}
func (t *MemoryGetPinned) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"key":   map[string]any{"type": "string", "description": "Optional pinned memory key to fetch"},
		"scope": map[string]any{"type": "string", "description": "Optional scope override: 'global' to read only shared memory"},
	}}
}
func (t *MemoryGetPinned) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *MemoryGetPinned) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.DB == nil {
		return "", fmt.Errorf("db not set")
	}
	pinned, err := t.DB.GetPinned(ctx, memoryScopeFromParams(ctx, params))
	if err != nil {
		return "", err
	}
	key := stringParam(params, "key")
	if key != "" {
		value, ok := pinned[key]
		if !ok {
			return "", nil
		}
		return fmt.Sprintf("%s: %s", key, compactMemoryText(value, t.MaxChars)), nil
	}
	keys := make([]string, 0, len(pinned))
	for key := range pinned {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, key := range keys {
		b.WriteString(fmt.Sprintf("%s: %s\n", key, compactMemoryText(pinned[key], t.MaxChars)))
	}
	return b.String(), nil
}

func memoryScopeFromParams(ctx context.Context, params map[string]any) string {
	if requestedScope := stringParam(params, "scope"); scope.IsGlobalScopeRequest(requestedScope) {
		return scope.GlobalMemoryScope
	}
	return SessionFromContext(ctx)
}

func stringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	value, ok := params[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boundedPositiveInt(raw any, fallback, max int) int {
	value := fallback
	if v, ok := raw.(float64); ok && int(v) > 0 {
		value = int(v)
	}
	if max > 0 && value > max {
		return max
	}
	if value < 0 {
		return 0
	}
	return value
}

func compactMemoryText(text string, maxChars int) string {
	text = strings.Join(strings.Fields(text), " ")
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}
	if maxChars <= 3 {
		return text[:maxChars]
	}
	return text[:maxChars-3] + "..."
}
````

## File: internal/tools/message.go
````go
package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	rootchannels "or3-intern/internal/channels"
)

type DeliverFunc func(ctx context.Context, channel, to, text string, meta map[string]any) error

type SendMessage struct {
	Base
	Deliver        DeliverFunc
	DefaultChannel string
	DefaultTo      string
	AllowedRoot    string
	ArtifactsDir   string
	MaxMediaBytes  int
}

func (t *SendMessage) Capability() CapabilityLevel { return CapabilityGuarded }

func (t *SendMessage) Name() string { return "send_message" }
func (t *SendMessage) Description() string {
	return "Send a message via a configured channel (for reminders/cron or proactive messages)."
}
func (t *SendMessage) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"channel": map[string]any{"type": "string"},
		"to":      map[string]any{"type": "string"},
		"text":    map[string]any{"type": "string"},
		"reply_in_thread": map[string]any{
			"type":        "boolean",
			"description": "When true, reuse the current channel's reply/thread metadata for the outgoing message.",
		},
		"media": map[string]any{
			"type":        "array",
			"items":       map[string]any{"type": "string"},
			"description": "Optional local file paths to send as attachments.",
		},
	}, "required": []string{}}
}
func (t *SendMessage) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *SendMessage) Execute(ctx context.Context, params map[string]any) (string, error) {
	if t.Deliver == nil {
		return "", fmt.Errorf("deliver not configured")
	}
	ctxChannel, ctxTo := DeliveryFromContext(ctx)
	ch := readOptionalString(params, "channel")
	to := readOptionalString(params, "to")
	text := readOptionalString(params, "text")
	if ch == "" {
		ch = strings.TrimSpace(t.DefaultChannel)
	}
	if ch == "" {
		ch = strings.TrimSpace(ctxChannel)
	}
	if to == "" {
		to = strings.TrimSpace(t.DefaultTo)
	}
	if to == "" {
		to = strings.TrimSpace(ctxTo)
	}
	mediaPaths, err := t.validateMediaPaths(params["media"])
	if err != nil {
		return "", err
	}
	if text == "" && len(mediaPaths) == 0 {
		return "", fmt.Errorf("message requires text or media")
	}
	inheritedReplyMeta := DeliveryMetaFromContext(ctx)
	meta := map[string]any{}
	explicitTo := strings.TrimSpace(readOptionalString(params, "to")) != ""
	replyInThread, err := optionalBool(params["reply_in_thread"])
	if err != nil {
		return "", err
	}
	if replyInThread {
		if explicitTo {
			return "", fmt.Errorf("reply_in_thread requires using the current delivery target")
		}
		if strings.TrimSpace(ctxChannel) != "" && !strings.EqualFold(strings.TrimSpace(ch), strings.TrimSpace(ctxChannel)) {
			return "", fmt.Errorf("reply_in_thread requires using the current delivery channel")
		}
		for k, v := range inheritedReplyMeta {
			meta[k] = v
		}
	}
	if len(mediaPaths) > 0 || explicitTo || len(meta) > 0 {
		if len(mediaPaths) > 0 {
			meta[rootchannels.MetaMediaPaths] = mediaPaths
		}
		if explicitTo {
			meta["explicit_to"] = true
		}
	}
	if len(meta) == 0 {
		meta = nil
	}
	if err := t.Deliver(ctx, ch, to, text, meta); err != nil {
		return "", err
	}
	return "ok", nil
}

func optionalBool(raw any) (bool, error) {
	switch v := raw.(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case string:
		text := strings.TrimSpace(strings.ToLower(v))
		switch text {
		case "", "false", "0", "no":
			return false, nil
		case "true", "1", "yes":
			return true, nil
		default:
			return false, fmt.Errorf("reply_in_thread must be a boolean")
		}
	default:
		return false, fmt.Errorf("reply_in_thread must be a boolean")
	}
}

func (t *SendMessage) validateMediaPaths(raw any) ([]string, error) {
	items, err := stringSlice(raw)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, nil
	}
	roots := make([]string, 0, 2)
	if strings.TrimSpace(t.AllowedRoot) != "" {
		roots = append(roots, strings.TrimSpace(t.AllowedRoot))
	}
	if strings.TrimSpace(t.ArtifactsDir) != "" {
		roots = append(roots, strings.TrimSpace(t.ArtifactsDir))
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		p, err := filepath.Abs(strings.TrimSpace(item))
		if err != nil {
			return nil, err
		}
		p, err = canonicalizePath(p)
		if err != nil {
			return nil, err
		}
		info, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("media path is a directory: %s", item)
		}
		if t.MaxMediaBytes == 0 {
			return nil, fmt.Errorf("media attachments disabled by config")
		}
		if t.MaxMediaBytes > 0 && info.Size() > int64(t.MaxMediaBytes) {
			return nil, fmt.Errorf("media path exceeds maxMediaBytes: %s", item)
		}
		if len(roots) > 0 {
			allowed := false
			for _, root := range roots {
				ok, err := pathWithinRoot(p, root)
				if err != nil {
					return nil, err
				}
				if ok {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, fmt.Errorf("media path outside allowed roots: %s", item)
			}
		}
		out = append(out, p)
	}
	return out, nil
}

func pathWithinRoot(absPath, root string) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	root, err = canonicalizeRoot(root)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return false, err
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

func stringSlice(raw any) ([]string, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if strings.TrimSpace(item) == "" {
				continue
			}
			out = append(out, item)
		}
		return out, nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("media must be an array of strings")
	}
}
````

## File: internal/providers/openai.go
````go
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"or3-intern/internal/security"
)

type Client struct {
	APIBase    string
	APIKey     string
	HTTP       *http.Client
	HostPolicy security.HostPolicy
}

func New(apiBase, apiKey string, timeout time.Duration) *Client {
	return &Client{
		APIBase: apiBase,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: timeout},
	}
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    any        `json:"content,omitempty"` // string|null
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolDef struct {
	Type     string   `json:"type"`
	Function ToolFunc `json:"function"`
}
type ToolFunc struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Index    int    `json:"index"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
}

type ChatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Role      string     `json:"role"`
			Content   any        `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *Client) Chat(ctx context.Context, req ChatCompletionRequest) (ChatCompletionResponse, error) {
	var out ChatCompletionResponse
	b, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return out, err
	}
	r.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.do(r)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

// ChatCompletionStreamRequest is sent when stream=true.
type ChatCompletionStreamRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Tools       []ToolDef     `json:"tools,omitempty"`
	ToolChoice  any           `json:"tool_choice,omitempty"`
	Temperature float64       `json:"temperature,omitempty"`
	Stream      bool          `json:"stream"`
}

type ChatStreamDelta struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type ChatStreamChoice struct {
	Index        int             `json:"index"`
	Delta        ChatStreamDelta `json:"delta"`
	FinishReason string          `json:"finish_reason"`
}

type ChatStreamChunk struct {
	ID      string             `json:"id"`
	Choices []ChatStreamChoice `json:"choices"`
}

// ChatStream sends the request with stream:true, calls onDelta for each text
// delta, and returns the fully-accumulated ChatCompletionResponse.
func (c *Client) ChatStream(ctx context.Context, req ChatCompletionRequest, onDelta func(text string)) (ChatCompletionResponse, error) {
	streamReq := ChatCompletionStreamRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Tools:       req.Tools,
		ToolChoice:  req.ToolChoice,
		Temperature: req.Temperature,
		Stream:      true,
	}
	b, _ := json.Marshal(streamReq)
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.do(r)
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		return ChatCompletionResponse{}, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var contentBuilder strings.Builder
	var finalToolCalls []ToolCall

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var chunk ChatStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta
		if delta.Content != "" {
			contentBuilder.WriteString(delta.Content)
			if onDelta != nil {
				onDelta(delta.Content)
			}
		}
		if len(delta.ToolCalls) > 0 {
			finalToolCalls = mergeStreamToolCalls(finalToolCalls, delta.ToolCalls)
		}
	}
	if err := scanner.Err(); err != nil {
		return ChatCompletionResponse{}, err
	}

	out := ChatCompletionResponse{
		Choices: []struct {
			Message struct {
				Role      string     `json:"role"`
				Content   any        `json:"content"`
				ToolCalls []ToolCall `json:"tool_calls"`
			} `json:"message"`
		}{
			{
				Message: struct {
					Role      string     `json:"role"`
					Content   any        `json:"content"`
					ToolCalls []ToolCall `json:"tool_calls"`
				}{
					Role:      "assistant",
					Content:   contentBuilder.String(),
					ToolCalls: finalToolCalls,
				},
			},
		},
	}
	return out, nil
}

// mergeStreamToolCalls accumulates tool-call deltas arriving over SSE.
// OpenAI streaming sends each piece as {index, partial args}; we expand the
// slice to the required index and concatenate name/arguments incrementally.
func mergeStreamToolCalls(existing []ToolCall, delta []ToolCall) []ToolCall {
	for _, d := range delta {
		idx := d.Index
		for len(existing) <= idx {
			existing = append(existing, ToolCall{})
		}
		existing[idx].Function.Arguments += d.Function.Arguments
		if d.Function.Name != "" {
			existing[idx].Function.Name += d.Function.Name
		}
		if d.ID != "" {
			existing[idx].ID = d.ID
		}
		if d.Type != "" {
			existing[idx].Type = d.Type
		}
		existing[idx].Index = idx
	}
	return existing
}

type EmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}
type EmbeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (c *Client) Embed(ctx context.Context, model, input string) ([]float32, error) {
	var out EmbeddingResponse
	b, _ := json.Marshal(EmbeddingRequest{Model: model, Input: input})
	r, err := http.NewRequestWithContext(ctx, "POST", c.APIBase+"/embeddings", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("provider error %s: %s", resp.Status, string(body))
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return out.Data[0].Embedding, nil
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	if c.HostPolicy.EnabledPolicy() {
		client = security.WrapHTTPClient(client, c.HostPolicy)
	}
	return client.Do(req)
}
````

## File: internal/tools/web.go
````go
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"or3-intern/internal/security"
	"strings"
	"time"
)

type WebFetch struct {
	Base
	HTTP            *http.Client
	Timeout         time.Duration
	DefaultMaxBytes int
	HostPolicy      security.HostPolicy
}

func (t *WebFetch) Capability() CapabilityLevel { return CapabilityGuarded }

const (
	defaultWebTimeout            = 20 * time.Second
	defaultWebFetchMaxBytes      = 200000
	defaultWebFetchMaxRedirects  = 10
	defaultWebSearchMaxCount     = 10
	defaultWebSearchReadMaxBytes = 1 << 20
)

func (t *WebFetch) Name() string        { return "web_fetch" }
func (t *WebFetch) Description() string { return "Fetch a URL (GET) and return text (truncated)." }
func (t *WebFetch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"url":      map[string]any{"type": "string"},
		"maxBytes": map[string]any{"type": "integer"},
	}, "required": []string{"url"}}
}
func (t *WebFetch) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}
func (t *WebFetch) Execute(ctx context.Context, params map[string]any) (string, error) {
	profile := ActiveProfileFromContext(ctx)
	u := fmt.Sprint(params["url"])
	if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "", fmt.Errorf("invalid url")
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return "", err
	}
	if err := validateFetchURL(ctx, parsed); err != nil {
		return "", err
	}
	if err := validateURLAgainstPolicies(ctx, parsed, t.HostPolicy, profile); err != nil {
		return "", err
	}
	max := t.DefaultMaxBytes
	if max <= 0 {
		max = defaultWebFetchMaxBytes
	}
	if v, ok := params["maxBytes"].(float64); ok && int(v) > 0 {
		max = int(v)
	}
	client := t.HTTP
	if t.HTTP == nil {
		to := t.Timeout
		if to <= 0 {
			to = defaultWebTimeout
		}
		client = &http.Client{Timeout: to}
	} else {
		copyClient := *t.HTTP
		client = &copyClient
	}
	client = security.WrapHTTPClient(client, t.HostPolicy)
	prevCheckRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= defaultWebFetchMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", defaultWebFetchMaxRedirects)
		}
		if err := validateURLAgainstPolicies(req.Context(), req.URL, t.HostPolicy, profile); err != nil {
			return err
		}
		if prevCheckRedirect != nil {
			if err := prevCheckRedirect(req, via); err != nil {
				return err
			}
		}
		return validateFetchURL(req.Context(), req.URL)
	}
	r, err := http.NewRequestWithContext(ctx, "GET", parsed.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(max)))
	return fmt.Sprintf("status: %s\n\n%s", resp.Status, string(body)), nil
}

func validateFetchURL(ctx context.Context, target *url.URL) error {
	if target == nil {
		return fmt.Errorf("invalid url")
	}
	hostname := strings.TrimSpace(strings.ToLower(target.Hostname()))
	if hostname == "" {
		return fmt.Errorf("missing host")
	}
	if isBlockedFetchHostname(hostname) {
		return fmt.Errorf("blocked fetch target")
	}
	if ip, err := netip.ParseAddr(hostname); err == nil {
		if isBlockedFetchAddr(ip.Unmap()) {
			return fmt.Errorf("blocked fetch target")
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host did not resolve")
	}
	for _, addr := range addrs {
		if ip, ok := netip.AddrFromSlice(addr.IP); ok && isBlockedFetchAddr(ip.Unmap()) {
			return fmt.Errorf("blocked fetch target")
		}
	}
	return nil
}

func isBlockedFetchHostname(hostname string) bool {
	switch hostname {
	case "localhost", "ip6-localhost", "metadata.google.internal":
		return true
	default:
		return false
	}
}

func isBlockedFetchAddr(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return true
	}
	return addr.String() == "169.254.169.254"
}

type WebSearch struct {
	Base
	APIKey       string
	HTTP         *http.Client
	Timeout      time.Duration
	ReadMaxBytes int
	HostPolicy   security.HostPolicy
}

func (t *WebSearch) Capability() CapabilityLevel { return CapabilitySafe }

func (t *WebSearch) Name() string { return "web_search" }
func (t *WebSearch) Description() string {
	return "Search the web (Brave Search API) and return top results."
}
func (t *WebSearch) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"query": map[string]any{"type": "string"},
		"count": map[string]any{"type": "integer", "description": "max results (default 5)"},
	}, "required": []string{"query"}}
}
func (t *WebSearch) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *WebSearch) Execute(ctx context.Context, params map[string]any) (string, error) {
	profile := ActiveProfileFromContext(ctx)
	if strings.TrimSpace(t.APIKey) == "" {
		return "", fmt.Errorf("Brave API key not configured (set BRAVE_API_KEY)")
	}
	q := fmt.Sprint(params["query"])
	count := 5
	if v, ok := params["count"].(float64); ok && int(v) > 0 {
		count = int(v)
	}
	if count > defaultWebSearchMaxCount {
		count = defaultWebSearchMaxCount
	}
	if t.HTTP == nil {
		to := t.Timeout
		if to <= 0 {
			to = defaultWebTimeout
		}
		t.HTTP = &http.Client{Timeout: to}
	}

	endpoint := "https://api.search.brave.com/res/v1/web/search?q=" + url.QueryEscape(q) + "&count=" + fmt.Sprint(count)
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if err := validateURLAgainstPolicies(ctx, parsedEndpoint, t.HostPolicy, profile); err != nil {
		return "", err
	}
	r, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return "", err
	}
	r.Header.Set("Accept", "application/json")
	r.Header.Set("X-Subscription-Token", t.APIKey)

	resp, err := t.HTTP.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	maxRead := t.ReadMaxBytes
	if maxRead <= 0 {
		maxRead = defaultWebSearchReadMaxBytes
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxRead)))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("search error %s: %s", resp.Status, string(body))
	}

	// Reduce response to stable subset
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	out := map[string]any{"query": q, "results": []any{}}
	web, _ := raw["web"].(map[string]any)
	results, _ := web["results"].([]any)
	for _, it := range results {
		m, _ := it.(map[string]any)
		out["results"] = append(out["results"].([]any), map[string]any{
			"title":       m["title"],
			"url":         m["url"],
			"description": m["description"],
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b), nil
}

// Optional: simple text extract from HTML (very rough)
func StripHTML(s string) string {
	var b bytes.Buffer
	in := false
	for _, r := range s {
		if r == '<' {
			in = true
			continue
		}
		if r == '>' {
			in = false
			continue
		}
		if !in {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func validateURLAgainstPolicies(ctx context.Context, target *url.URL, policy security.HostPolicy, profile ActiveProfile) error {
	if policy.EnabledPolicy() {
		if err := policy.ValidateURL(ctx, target); err != nil {
			return err
		}
	}
	if strings.TrimSpace(profile.Name) == "" {
		return nil
	}
	return (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: profile.AllowedHosts}).ValidateURL(ctx, target)
}
````

## File: internal/tools/context.go
````go
package tools

import (
	"context"
	"strings"

	"or3-intern/internal/scope"
)

type sessionContextKey struct{}
type deliveryChannelContextKey struct{}
type deliveryToContextKey struct{}
type deliveryMetaContextKey struct{}
type envContextKey struct{}
type toolGuardContextKey struct{}
type activeProfileContextKey struct{}
type skillPolicyContextKey struct{}

type ToolGuard func(ctx context.Context, tool Tool, capability CapabilityLevel, params map[string]any) error

type ActiveProfile struct {
	Name           string
	MaxCapability  CapabilityLevel
	AllowedTools   map[string]struct{}
	AllowedHosts   []string
	WritablePaths  []string
	AllowSubagents bool
}

type SkillPolicy struct {
	Name           string
	AllowedTools   map[string]struct{}
	AllowExecution bool
	AllowNetwork   bool
	AllowWrite     bool
	AllowedHosts   []string
	WritablePaths  []string
}

func ContextWithSession(ctx context.Context, sessionKey string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if sessionKey == "" {
		sessionKey = scope.GlobalMemoryScope
	}
	return context.WithValue(ctx, sessionContextKey{}, sessionKey)
}

func ContextWithDelivery(ctx context.Context, channel, to string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, deliveryChannelContextKey{}, channel)
	return context.WithValue(ctx, deliveryToContextKey{}, to)
}

func ContextWithDeliveryMeta(ctx context.Context, meta map[string]any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(meta) == 0 {
		return ctx
	}
	cloned := make(map[string]any, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return context.WithValue(ctx, deliveryMetaContextKey{}, cloned)
}

func ContextWithEnv(ctx context.Context, env map[string]string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(env) == 0 {
		return ctx
	}
	copyEnv := make(map[string]string, len(env))
	for k, v := range env {
		copyEnv[k] = v
	}
	return context.WithValue(ctx, envContextKey{}, copyEnv)
}

func ContextWithToolGuard(ctx context.Context, guard ToolGuard) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if guard == nil {
		return ctx
	}
	return context.WithValue(ctx, toolGuardContextKey{}, guard)
}

func ContextWithActiveProfile(ctx context.Context, profile ActiveProfile) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(profile.Name) == "" && len(profile.AllowedTools) == 0 && len(profile.AllowedHosts) == 0 && len(profile.WritablePaths) == 0 && !profile.AllowSubagents && profile.MaxCapability == "" {
		return ctx
	}
	cloned := ActiveProfile{
		Name:           strings.TrimSpace(profile.Name),
		MaxCapability:  profile.MaxCapability,
		AllowedHosts:   append([]string{}, profile.AllowedHosts...),
		WritablePaths:  append([]string{}, profile.WritablePaths...),
		AllowSubagents: profile.AllowSubagents,
	}
	if len(profile.AllowedTools) > 0 {
		cloned.AllowedTools = make(map[string]struct{}, len(profile.AllowedTools))
		for key := range profile.AllowedTools {
			cloned.AllowedTools[key] = struct{}{}
		}
	}
	return context.WithValue(ctx, activeProfileContextKey{}, cloned)
}

func ContextWithSkillPolicy(ctx context.Context, policy SkillPolicy) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(policy.Name) == "" && len(policy.AllowedTools) == 0 && !policy.AllowExecution && !policy.AllowNetwork && !policy.AllowWrite && len(policy.AllowedHosts) == 0 && len(policy.WritablePaths) == 0 {
		return ctx
	}
	cloned := SkillPolicy{
		Name:           strings.TrimSpace(policy.Name),
		AllowExecution: policy.AllowExecution,
		AllowNetwork:   policy.AllowNetwork,
		AllowWrite:     policy.AllowWrite,
		AllowedHosts:   append([]string{}, policy.AllowedHosts...),
		WritablePaths:  append([]string{}, policy.WritablePaths...),
	}
	if len(policy.AllowedTools) > 0 {
		cloned.AllowedTools = make(map[string]struct{}, len(policy.AllowedTools))
		for key := range policy.AllowedTools {
			cloned.AllowedTools[key] = struct{}{}
		}
	}
	return context.WithValue(ctx, skillPolicyContextKey{}, cloned)
}

func SessionFromContext(ctx context.Context) string {
	if ctx == nil {
		return scope.GlobalMemoryScope
	}
	if sessionKey, ok := ctx.Value(sessionContextKey{}).(string); ok && sessionKey != "" {
		return sessionKey
	}
	return scope.GlobalMemoryScope
}

func DeliveryFromContext(ctx context.Context) (channel string, to string) {
	if ctx == nil {
		return "", ""
	}
	if v, ok := ctx.Value(deliveryChannelContextKey{}).(string); ok {
		channel = v
	}
	if v, ok := ctx.Value(deliveryToContextKey{}).(string); ok {
		to = v
	}
	return channel, to
}

func DeliveryMetaFromContext(ctx context.Context) map[string]any {
	if ctx == nil {
		return nil
	}
	meta, _ := ctx.Value(deliveryMetaContextKey{}).(map[string]any)
	if len(meta) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return cloned
}

func EnvFromContext(ctx context.Context) map[string]string {
	if ctx == nil {
		return nil
	}
	if env, ok := ctx.Value(envContextKey{}).(map[string]string); ok && len(env) > 0 {
		copyEnv := make(map[string]string, len(env))
		for k, v := range env {
			copyEnv[k] = v
		}
		return copyEnv
	}
	return nil
}

func ToolGuardFromContext(ctx context.Context) ToolGuard {
	if ctx == nil {
		return nil
	}
	guard, _ := ctx.Value(toolGuardContextKey{}).(ToolGuard)
	return guard
}

func ActiveProfileFromContext(ctx context.Context) ActiveProfile {
	if ctx == nil {
		return ActiveProfile{}
	}
	profile, _ := ctx.Value(activeProfileContextKey{}).(ActiveProfile)
	return profile
}

func SkillPolicyFromContext(ctx context.Context) SkillPolicy {
	if ctx == nil {
		return SkillPolicy{}
	}
	policy, _ := ctx.Value(skillPolicyContextKey{}).(SkillPolicy)
	return policy
}
````

## File: internal/tools/exec.go
````go
package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type ExecTool struct {
	Base
	Timeout           time.Duration
	RestrictDir       string // if non-empty, cwd must be inside
	PathAppend        string
	ChildEnvAllowlist []string
	AllowedPrograms   []string
	Sandbox           BubblewrapConfig
	EnableLegacyShell bool
	DisableShell      bool
	OutputMaxBytes    int
	BlockedPatterns   []string
}

const defaultExecOutputMaxBytes = 10000

func (t *ExecTool) Name() string { return "exec" }
func (t *ExecTool) Description() string {
	return "Run an allowed program with safety limits. Output is truncated. Legacy shell commands require explicit opt-in."
}
func (t *ExecTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"program":        map[string]any{"type": "string", "description": "Program to run"},
			"args":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Program arguments"},
			"command":        map[string]any{"type": "string", "description": "Legacy shell command to run (privileged, explicit opt-in only)"},
			"cwd":            map[string]any{"type": "string", "description": "Working directory (optional)"},
			"timeoutSeconds": map[string]any{"type": "integer", "description": "Override timeout (optional)"},
		},
		"required": []string{},
	}
}
func (t *ExecTool) Schema() map[string]any {
	return t.SchemaFor(t.Name(), t.Description(), t.Parameters())
}

func (t *ExecTool) CapabilityForParams(params map[string]any) CapabilityLevel {
	if strings.TrimSpace(fmt.Sprint(params["command"])) != "" && fmt.Sprint(params["command"]) != "<nil>" {
		return CapabilityPrivileged
	}
	return CapabilityGuarded
}

var defaultBlockedPatterns = []string{
	"rm -rf", "mkfs", "dd ", "shutdown", "reboot", "poweroff", ":(){", ">|", "chown -R /", "chmod -R 777 /",
}

func (t *ExecTool) Execute(ctx context.Context, params map[string]any) (string, error) {
	program := strings.TrimSpace(fmt.Sprint(params["program"]))
	if program == "<nil>" {
		program = ""
	}
	legacyCommand, _ := params["command"].(string)
	legacyCommand = strings.TrimSpace(legacyCommand)
	if program == "" && legacyCommand == "" {
		return "", errors.New("missing program or command")
	}
	if legacyCommand != "" {
		if !t.EnableLegacyShell || t.DisableShell {
			return "", errors.New("shell command execution disabled; use program + args or explicitly enable legacy shell mode")
		}
		lc := strings.ToLower(legacyCommand)
		patterns := t.BlockedPatterns
		if len(patterns) == 0 {
			patterns = defaultBlockedPatterns
		}
		for _, b := range patterns {
			if strings.Contains(lc, b) {
				return "", fmt.Errorf("blocked command pattern: %q", b)
			}
		}
	}
	cwd, _ := params["cwd"].(string)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	if t.RestrictDir != "" {
		abs, err := filepath.Abs(cwd)
		if err != nil {
			return "", err
		}
		abs, err = canonicalizePath(abs)
		if err != nil {
			return "", err
		}
		root, err := canonicalizeRoot(t.RestrictDir)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(root, abs)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("cwd outside allowed directory")
		}
	}
	if program != "" {
		resolvedProgram, err := resolveExecutable(program, cwd)
		if err != nil {
			return "", err
		}
		if len(t.AllowedPrograms) > 0 && !allowedProgram(program, resolvedProgram, t.AllowedPrograms) {
			return "", fmt.Errorf("program not allowed: %s", program)
		}
		program = resolvedProgram
	}

	to := t.Timeout
	if v, ok := params["timeoutSeconds"].(float64); ok && v > 0 {
		to = time.Duration(int(v)) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	var c *exec.Cmd
	var err error
	if legacyCommand != "" {
		c, err = commandWithSandbox(cctx, t.Sandbox, cwd, []string{"bash", "-lc", legacyCommand})
		if err != nil {
			return "", err
		}
		if c == nil {
			c = exec.CommandContext(cctx, "bash", "-lc", legacyCommand)
		}
	} else {
		c = exec.CommandContext(cctx, program, stringArgs(params["args"])...)
	}
	c.Dir = cwd
	c.Env = BuildChildEnv(os.Environ(), t.ChildEnvAllowlist, EnvFromContext(ctx), t.PathAppend)
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err = c.Run()
	out := stdout.String()
	er := stderr.String()
	max := t.OutputMaxBytes
	if max <= 0 {
		max = defaultExecOutputMaxBytes
	}
	if len(out) > max {
		out = out[:max] + "\n...[truncated]\n"
	}
	if len(er) > max {
		er = er[:max] + "\n...[truncated]\n"
	}
	if err != nil {
		return formatCommandOutput(out, er), fmt.Errorf("exec failed: %w", err)
	}
	if strings.TrimSpace(er) != "" {
		return formatCommandOutput(out, er), nil
	}
	return out, nil
}

func allowedProgram(program string, resolved string, allowed []string) bool {
	program = strings.TrimSpace(program)
	resolved = strings.TrimSpace(resolved)
	if program == "" || resolved == "" {
		return false
	}
	programHasPath := hasPathSeparator(program)
	for _, candidate := range allowed {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if hasPathSeparator(candidate) {
			resolvedCandidate, err := canonicalExecutablePath(candidate)
			if err == nil && resolvedCandidate == resolved {
				return true
			}
			continue
		}
		if !programHasPath && candidate == program {
			return true
		}
	}
	return false
}

func resolveExecutable(program string, cwd string) (string, error) {
	program = strings.TrimSpace(program)
	if program == "" {
		return "", fmt.Errorf("missing program")
	}
	if hasPathSeparator(program) {
		if !filepath.IsAbs(program) {
			base := strings.TrimSpace(cwd)
			if base == "" {
				var err error
				base, err = os.Getwd()
				if err != nil {
					return "", err
				}
			}
			program = filepath.Join(base, program)
		}
		return canonicalExecutablePath(program)
	}
	resolved, err := exec.LookPath(program)
	if err != nil {
		return "", err
	}
	return canonicalExecutablePath(resolved)
}

func canonicalExecutablePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return abs, nil
}

func hasPathSeparator(path string) bool {
	return strings.ContainsRune(path, filepath.Separator) || (filepath.Separator != '/' && strings.ContainsRune(path, '/'))
}

func formatCommandOutput(stdout, stderr string) string {
	return fmt.Sprintf("stdout:\n%s\n\nstderr:\n%s", stdout, stderr)
}
````

## File: internal/tools/registry.go
````go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry() *Registry {
	return &Registry{tools: map[string]Tool{}}
}

func (r *Registry) Register(t Tool)      { r.tools[t.Name()] = t }
func (r *Registry) Get(name string) Tool { return r.tools[name] }
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for k := range r.tools {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *Registry) Definitions() []map[string]any {
	names := r.Names()
	out := make([]map[string]any, 0, len(names))
	for _, name := range names {
		out = append(out, r.tools[name].Schema())
	}
	return out
}

func (r *Registry) CloneFiltered(allowed []string) *Registry {
	if r == nil {
		return NewRegistry()
	}
	if len(allowed) == 0 {
		clone := NewRegistry()
		for _, name := range r.Names() {
			clone.Register(r.tools[name])
		}
		return clone
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		allowedSet[trimmed] = struct{}{}
	}
	clone := NewRegistry()
	for _, name := range r.Names() {
		if _, ok := allowedSet[name]; !ok {
			continue
		}
		clone.Register(r.tools[name])
	}
	return clone
}

func (r *Registry) Execute(ctx context.Context, name string, argsJSON string) (string, error) {
	t := r.tools[name]
	if t == nil {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	var params map[string]any
	if argsJSON == "" {
		params = map[string]any{}
	} else {
		if err := json.Unmarshal([]byte(argsJSON), &params); err != nil {
			return "", fmt.Errorf("invalid tool args: %w", err)
		}
	}
	return r.ExecuteParams(ctx, name, params)
}

func (r *Registry) ExecuteParams(ctx context.Context, name string, params map[string]any) (string, error) {
	t := r.tools[name]
	if t == nil {
		return "", fmt.Errorf("tool '%s' not found", name)
	}
	if params == nil {
		params = map[string]any{}
	}
	if guard := ToolGuardFromContext(ctx); guard != nil {
		if err := guard(ctx, t, ToolCapability(t, params), params); err != nil {
			return "", err
		}
	}
	return t.Execute(ctx, params)
}
````

## File: go.mod
````
module or3-intern

go 1.24.0

require (
	github.com/asg017/sqlite-vec-go-bindings v0.1.6
	github.com/emersion/go-imap/v2 v2.0.0-beta.8
	github.com/gorilla/websocket v1.5.3
	github.com/mattn/go-isatty v0.0.20
	github.com/mattn/go-sqlite3 v1.14.34
	github.com/modelcontextprotocol/go-sdk v0.8.0
	github.com/robfig/cron/v3 v3.0.1
	golang.org/x/net v0.6.0
	gopkg.in/yaml.v3 v3.0.1
	modernc.org/sqlite v1.33.1
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/emersion/go-message v0.18.2 // indirect
	github.com/emersion/go-sasl v0.0.0-20241020182733-b788ff22d5a6 // indirect
	github.com/google/jsonschema-go v0.3.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/golang-lru/v2 v2.0.7 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sys v0.40.0 // indirect
	modernc.org/gc/v3 v3.0.0-20240107210532-573471604cb6 // indirect
	modernc.org/libc v1.55.3 // indirect
	modernc.org/mathutil v1.6.0 // indirect
	modernc.org/memory v1.8.0 // indirect
	modernc.org/strutil v1.2.0 // indirect
	modernc.org/token v1.1.0 // indirect
)
````

## File: internal/db/db.go
````go
package db

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
	"or3-intern/internal/scope"

	_ "modernc.org/sqlite"
)

var sqliteVecAutoOnce sync.Once

type DB struct {
	SQL     *sql.DB
	VecSQL  *sql.DB
	auditMu sync.Mutex
}

func Open(path string) (*DB, error) {
	primaryDSN := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(ON)", path)
	s, err := sql.Open("sqlite", primaryDSN)
	if err != nil {
		return nil, err
	}
	s.SetMaxOpenConns(4)
	s.SetMaxIdleConns(4)

	sqliteVecAutoOnce.Do(sqlite_vec.Auto)
	vecDSN := fmt.Sprintf("file:%s?_busy_timeout=5000&_journal=WAL&_sync=NORMAL&_fk=1", path)
	vec, err := sql.Open("sqlite3", vecDSN)
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	vec.SetMaxOpenConns(2)
	vec.SetMaxIdleConns(2)

	d := &DB{SQL: s, VecSQL: vec}
	if err := d.migrate(context.Background()); err != nil {
		_ = vec.Close()
		_ = s.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() error {
	if d == nil {
		return nil
	}
	var err error
	if d.VecSQL != nil {
		if closeErr := d.VecSQL.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if d.SQL != nil {
		if closeErr := d.SQL.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

func (d *DB) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sessions(
			key TEXT PRIMARY KEY,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}',
			last_consolidated_msg_id INTEGER NOT NULL DEFAULT 0
		);`,
		`CREATE TABLE IF NOT EXISTS messages(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			payload_json TEXT NOT NULL DEFAULT '{}',
			created_at INTEGER NOT NULL,
			FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS messages_session_id ON messages(session_key, id);`,
		`CREATE TABLE IF NOT EXISTS artifacts(
			id TEXT PRIMARY KEY,
			session_key TEXT NOT NULL,
			mime TEXT NOT NULL,
			path TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(session_key) REFERENCES sessions(key) ON DELETE CASCADE
		);`,
		`CREATE TABLE IF NOT EXISTS memory_pinned(
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(session_key, key)
		);`,
		`CREATE TABLE IF NOT EXISTS memory_notes(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			source_message_id INTEGER,
			tags TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		);`,
		// FTS5
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(text, content='memory_notes', content_rowid='id');`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_ai AFTER INSERT ON memory_notes BEGIN
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_ad AFTER DELETE ON memory_notes BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, text) VALUES('delete', old.id, old.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_notes_au AFTER UPDATE ON memory_notes BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, text) VALUES('delete', old.id, old.text);
			INSERT INTO memory_fts(rowid, text) VALUES (new.id, new.text);
		END;`,
		`CREATE TABLE IF NOT EXISTS subagent_jobs(
			id TEXT PRIMARY KEY,
			parent_session_key TEXT NOT NULL,
			child_session_key TEXT NOT NULL,
			channel TEXT NOT NULL,
			reply_to TEXT NOT NULL,
			task TEXT NOT NULL,
			status TEXT NOT NULL,
			result_preview TEXT NOT NULL DEFAULT '',
			artifact_id TEXT NOT NULL DEFAULT '',
			error_text TEXT NOT NULL DEFAULT '',
			requested_at INTEGER NOT NULL,
			started_at INTEGER NOT NULL DEFAULT 0,
			finished_at INTEGER NOT NULL DEFAULT 0,
			attempts INTEGER NOT NULL DEFAULT 0,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS subagent_jobs_status_requested_at ON subagent_jobs(status, requested_at);`,
		`CREATE INDEX IF NOT EXISTS subagent_jobs_parent_session ON subagent_jobs(parent_session_key, requested_at);`,
		`CREATE TABLE IF NOT EXISTS session_links(
			session_key TEXT PRIMARY KEY,
			scope_key TEXT NOT NULL,
			linked_at INTEGER NOT NULL,
			metadata_json TEXT NOT NULL DEFAULT '{}'
		);`,
		`CREATE INDEX IF NOT EXISTS session_links_scope_key ON session_links(scope_key);`,
		`CREATE TABLE IF NOT EXISTS memory_docs(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			scope_key TEXT NOT NULL,
			path TEXT NOT NULL,
			kind TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			text TEXT NOT NULL,
			embedding BLOB,
			hash TEXT NOT NULL,
			mtime_ms INTEGER NOT NULL,
			size_bytes INTEGER NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL,
			UNIQUE(scope_key, path)
		);`,
		`CREATE INDEX IF NOT EXISTS memory_docs_scope_path ON memory_docs(scope_key, path);`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_docs_fts USING fts5(title, summary, text, content='memory_docs', content_rowid='id');`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_ai AFTER INSERT ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(rowid, title, summary, text) VALUES (new.id, new.title, new.summary, new.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_ad AFTER DELETE ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(memory_docs_fts, rowid, title, summary, text) VALUES('delete', old.id, old.title, old.summary, old.text);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS memory_docs_au AFTER UPDATE ON memory_docs BEGIN
			INSERT INTO memory_docs_fts(memory_docs_fts, rowid, title, summary, text) VALUES('delete', old.id, old.title, old.summary, old.text);
			INSERT INTO memory_docs_fts(rowid, title, summary, text) VALUES (new.id, new.title, new.summary, new.text);
		END;`,
		`CREATE TABLE IF NOT EXISTS memory_vec_meta(
			id INTEGER PRIMARY KEY CHECK(id=1),
			dims INTEGER NOT NULL DEFAULT 0,
			updated_at INTEGER NOT NULL DEFAULT 0
		);`,
		`INSERT INTO memory_vec_meta(id, dims, updated_at)
			VALUES(1, 0, 0)
			ON CONFLICT(id) DO NOTHING;`,
		`CREATE TABLE IF NOT EXISTS secrets(
			name TEXT PRIMARY KEY,
			ciphertext BLOB NOT NULL,
			nonce BLOB NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			key_version TEXT NOT NULL DEFAULT 'v1',
			updated_at INTEGER NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS audit_events(
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			session_key TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '{}',
			prev_hash BLOB NOT NULL,
			record_hash BLOB NOT NULL,
			created_at INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS audit_events_created_at ON audit_events(created_at);`,
	}
	for _, s := range stmts {
		if _, err := d.SQL.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	if err := d.migrateMemoryPinned(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryNotesSessionColumn(ctx); err != nil {
		return err
	}
	if err := d.migrateLegacyGlobalMemoryScope(ctx); err != nil {
		return err
	}
	if err := d.ensureMemoryVecIndexForExisting(ctx); err != nil {
		return err
	}
	return nil
}

func NowMS() int64 { return time.Now().UnixMilli() }

func (d *DB) migrateMemoryPinned(ctx context.Context) error {
	hasSession, err := d.tableHasColumn(ctx, "memory_pinned", "session_key")
	if err != nil {
		return err
	}
	if hasSession {
		_, err = d.SQL.ExecContext(ctx, `CREATE UNIQUE INDEX IF NOT EXISTS memory_pinned_session_key_key ON memory_pinned(session_key, key);`)
		return err
	}
	stmts := []string{
		`ALTER TABLE memory_pinned RENAME TO memory_pinned_legacy;`,
		`CREATE TABLE memory_pinned(
			session_key TEXT NOT NULL DEFAULT '` + scope.GlobalMemoryScope + `',
			key TEXT NOT NULL,
			content TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY(session_key, key)
		);`,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at)
		 SELECT '` + scope.GlobalMemoryScope + `', key, content, updated_at FROM memory_pinned_legacy;`,
		`DROP TABLE memory_pinned_legacy;`,
		`CREATE UNIQUE INDEX IF NOT EXISTS memory_pinned_session_key_key ON memory_pinned(session_key, key);`,
	}
	for _, stmt := range stmts {
		if _, err := d.SQL.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func (d *DB) ensureMemoryNotesSessionColumn(ctx context.Context) error {
	hasSession, err := d.tableHasColumn(ctx, "memory_notes", "session_key")
	if err != nil {
		return err
	}
	if !hasSession {
		if _, err := d.SQL.ExecContext(ctx, `ALTER TABLE memory_notes ADD COLUMN session_key TEXT NOT NULL DEFAULT '`+scope.GlobalMemoryScope+`';`); err != nil {
			return err
		}
	}
	_, err = d.SQL.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS memory_notes_session_id ON memory_notes(session_key, id);`)
	return err
}

func (d *DB) migrateLegacyGlobalMemoryScope(ctx context.Context) error {
	if scope.GlobalMemoryScope == scope.GlobalScopeAlias {
		return nil
	}
	if _, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at)
		 SELECT ?, key, content, updated_at FROM memory_pinned WHERE session_key=?
		 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		scope.GlobalMemoryScope, scope.GlobalScopeAlias); err != nil {
		return err
	}
	if _, err := d.SQL.ExecContext(ctx, `DELETE FROM memory_pinned WHERE session_key=?`, scope.GlobalScopeAlias); err != nil {
		return err
	}
	_, err := d.SQL.ExecContext(ctx, `UPDATE memory_notes SET session_key=? WHERE session_key=?`, scope.GlobalMemoryScope, scope.GlobalScopeAlias)
	if err != nil {
		return err
	}
	if dims, derr := d.MemoryVectorDims(ctx); derr == nil && dims > 0 && d.VecSQL != nil {
		_, err = d.VecSQL.ExecContext(ctx, `UPDATE memory_vec SET session_key=? WHERE session_key=?`, scope.GlobalMemoryScope, scope.GlobalScopeAlias)
	}
	return err
}

func (d *DB) ensureMemoryVecIndexForExisting(ctx context.Context) error {
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	if dims == 0 {
		dims, err = d.firstMemoryVectorDim(ctx)
		if err != nil {
			return err
		}
	}
	if dims <= 0 {
		return nil
	}
	return d.initMemoryVecIndex(ctx, dims)
}

func (d *DB) MemoryVectorDims(ctx context.Context) (int, error) {
	row := d.SQL.QueryRowContext(ctx, `SELECT dims FROM memory_vec_meta WHERE id=1`)
	var dims int
	if err := row.Scan(&dims); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return dims, nil
}

func (d *DB) firstMemoryVectorDim(ctx context.Context) (int, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(length(embedding), 0)
		 FROM memory_notes
		 WHERE typeof(embedding)='blob' AND length(embedding) >= 4 AND (length(embedding) % 4)=0
		 ORDER BY id ASC
		 LIMIT 1`)
	var bytes int
	if err := row.Scan(&bytes); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	if bytes <= 0 {
		return 0, nil
	}
	return bytes / 4, nil
}

func (d *DB) EnsureMemoryVecIndexWithDim(ctx context.Context, dims int) error {
	if dims <= 0 {
		return nil
	}
	existing, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	if existing > 0 && existing != dims {
		return nil
	}
	return d.initMemoryVecIndex(ctx, dims)
}

func (d *DB) initMemoryVecIndex(ctx context.Context, dims int) error {
	if dims <= 0 {
		return nil
	}
	if d == nil || d.VecSQL == nil {
		return nil
	}
	tx, err := d.VecSQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT dims FROM memory_vec_meta WHERE id=1`).Scan(&existing); err != nil && err != sql.ErrNoRows {
		return err
	}
	if existing > 0 && existing != dims {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS memory_vec`); err != nil {
		return err
	}
	createSQL := fmt.Sprintf(`CREATE VIRTUAL TABLE memory_vec USING vec0(
			note_id integer primary key,
			session_key text partition key,
			embedding float[%d] distance_metric=cosine,
			+text text
		)`, dims)
	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_vec(note_id, session_key, embedding, text)
		 SELECT id, session_key, embedding, text
		 FROM memory_notes
		 WHERE typeof(embedding)='blob' AND length(embedding)=?`, dims*4); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO memory_vec_meta(id, dims, updated_at)
		 VALUES(1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET dims=excluded.dims, updated_at=excluded.updated_at`,
		dims, NowMS()); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) tableHasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	rows, err := d.SQL.QueryContext(ctx, `PRAGMA table_info(`+tableName+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == columnName {
			return true, nil
		}
	}
	return false, rows.Err()
}
````

## File: internal/skills/skills.go
````go
package skills

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"or3-intern/internal/clawhub"

	"gopkg.in/yaml.v3"
)

// SkillEntry describes a declared executable entrypoint from a skill manifest.
type SkillEntry struct {
	Name           string   `json:"name"`
	Command        []string `json:"command"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	AcceptsStdin   bool     `json:"acceptsStdin"`
}

type Source string

const (
	SourceExtra     Source = "extra"
	SourceBundled   Source = "bundled"
	SourceManaged   Source = "managed"
	SourceWorkspace Source = "workspace"
)

type Root struct {
	Path     string
	Source   Source
	Label    string
	Priority int
}

type EntryConfig struct {
	Enabled *bool
	APIKey  string
	Env     map[string]string
	Config  map[string]any
}

type LoadOptions struct {
	Roots          []Root
	Entries        map[string]EntryConfig
	GlobalConfig   map[string]any
	Env            map[string]string
	AvailableTools map[string]struct{}
	ApprovalPolicy ApprovalPolicy
	OS             string
}

type ApprovalPolicy struct {
	QuarantineByDefault bool
	ApprovedSkills      map[string]struct{}
	TrustedOwners       map[string]struct{}
	BlockedOwners       map[string]struct{}
	TrustedRegistries   map[string]struct{}
}

type SkillInstallSpec struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Label   string   `json:"label"`
	Bins    []string `json:"bins"`
	Formula string   `json:"formula"`
	Tap     string   `json:"tap"`
	Package string   `json:"package"`
	Module  string   `json:"module"`
	OS      []string `json:"os"`
	URL     string   `json:"url"`
}

type NixPluginSpec struct {
	Plugin  string   `json:"plugin"`
	Systems []string `json:"systems"`
}

type SkillRequirements struct {
	Bins    []string `json:"bins"`
	AnyBins []string `json:"anyBins"`
	Env     []string `json:"env"`
	Config  []string `json:"config"`
}

type SkillRuntimeMeta struct {
	Always     bool               `json:"always"`
	SkillKey   string             `json:"skillKey"`
	PrimaryEnv string             `json:"primaryEnv"`
	Emoji      string             `json:"emoji"`
	Homepage   string             `json:"homepage"`
	OS         []string           `json:"os"`
	Requires   SkillRequirements  `json:"requires"`
	Install    []SkillInstallSpec `json:"install"`
	Nix        *NixPluginSpec     `json:"nix"`
}

type SkillPermissions struct {
	Shell        bool     `json:"shell" yaml:"shell"`
	Network      bool     `json:"network" yaml:"network"`
	Write        bool     `json:"write" yaml:"write"`
	AllowedPaths []string `json:"paths" yaml:"paths"`
	AllowedHosts []string `json:"hosts" yaml:"hosts"`
}

type SkillMeta struct {
	Name        string
	Description string
	Homepage    string
	Path        string
	Dir         string
	Location    string
	Source      Source
	ModTime     time.Time
	Size        int64
	ID          string
	Summary     string
	Entrypoints []SkillEntry

	UserInvocable          bool
	DisableModelInvocation bool
	CommandDispatch        string
	CommandTool            string
	CommandArgMode         string

	Metadata        SkillRuntimeMeta
	Permissions     SkillPermissions
	AllowedTools    []string
	PermissionState string
	PermissionNotes []string
	Publisher       string
	Registry        string
	InstalledVersion string
	Modified        bool
	ScanStatus      string
	ScanFindings    []string
	Key             string
	Eligible        bool
	Disabled        bool
	Hidden          bool
	Missing         []string
	Unsupported     []string
	ParseError      string
	RuntimeEnv      map[string]string

	sourcePriority int
	rootOrder      int
}

type Inventory struct {
	Skills []SkillMeta
	byName map[string]SkillMeta
}

type skillManifest struct {
	Summary     string           `json:"summary"`
	Entrypoints []SkillEntry     `json:"entrypoints"`
	Tools       []string         `json:"tools"`
	Permissions SkillPermissions `json:"permissions"`
}

type skillFrontMatter struct {
	Name                   string           `yaml:"name"`
	Description            string           `yaml:"description"`
	Summary                string           `yaml:"summary"`
	Homepage               string           `yaml:"homepage"`
	UserInvocable          *bool            `yaml:"user-invocable"`
	DisableModelInvocation bool             `yaml:"disable-model-invocation"`
	CommandDispatch        string           `yaml:"command-dispatch"`
	CommandTool            string           `yaml:"command-tool"`
	CommandArgMode         string           `yaml:"command-arg-mode"`
	Permissions            SkillPermissions `yaml:"permissions"`
	Metadata               map[string]any   `yaml:"metadata"`
}

func defaultPriority(source Source) int {
	switch source {
	case SourceWorkspace:
		return 40
	case SourceManaged:
		return 30
	case SourceBundled:
		return 20
	default:
		return 10
	}
}

// Scan keeps the old simple API for tests and callers that only provide directories.
func Scan(dirs []string) Inventory {
	roots := make([]Root, 0, len(dirs))
	for i, dir := range dirs {
		roots = append(roots, Root{
			Path:     dir,
			Source:   SourceExtra,
			Label:    dir,
			Priority: i + 1,
		})
	}
	return ScanWithOptions(LoadOptions{Roots: roots})
}

func ScanWithOptions(opts LoadOptions) Inventory {
	if len(opts.Env) == 0 {
		opts.Env = envMap(os.Environ())
	}
	if strings.TrimSpace(opts.OS) == "" {
		opts.OS = runtime.GOOS
	}

	metaByName := map[string]SkillMeta{}
	for i, root := range opts.Roots {
		root = normalizeRoot(root)
		if strings.TrimSpace(root.Path) == "" {
			continue
		}
		absRoot, err := filepath.Abs(root.Path)
		if err != nil {
			continue
		}
		realRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			continue
		}
		scanSkillDir(metaByName, realRoot, root, i, opts)
		_ = filepath.WalkDir(realRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			if path == realRoot {
				return nil
			}
			realPath, err := filepath.EvalSymlinks(path)
			if err != nil {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(realRoot, realPath)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			if scanSkillDir(metaByName, realPath, root, i, opts) {
				return filepath.SkipDir
			}
			return nil
		})
	}

	skills := make([]SkillMeta, 0, len(metaByName))
	for _, s := range metaByName {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name == skills[j].Name {
			if skills[i].sourcePriority == skills[j].sourcePriority {
				return skills[i].Path < skills[j].Path
			}
			return skills[i].sourcePriority > skills[j].sourcePriority
		}
		return strings.ToLower(skills[i].Name) < strings.ToLower(skills[j].Name)
	})
	by := make(map[string]SkillMeta, len(skills))
	for _, s := range skills {
		by[s.Name] = s
		by[strings.ToLower(s.Name)] = s
	}
	return Inventory{Skills: skills, byName: by}
}

func normalizeRoot(root Root) Root {
	if strings.TrimSpace(root.Label) == "" {
		root.Label = string(root.Source)
	}
	if root.Priority == 0 {
		root.Priority = defaultPriority(root.Source)
	}
	return root
}

func scanSkillDir(metaByName map[string]SkillMeta, dir string, root Root, order int, opts LoadOptions) bool {
	skillPath, ok := skillFileInDir(dir)
	if !ok {
		return false
	}
	meta := loadSkill(dir, skillPath, root, order, opts)
	current, exists := metaByName[meta.Name]
	if !exists || shouldOverride(current, meta) {
		metaByName[meta.Name] = meta
	}
	return true
}

func shouldOverride(current SkillMeta, candidate SkillMeta) bool {
	if candidate.sourcePriority != current.sourcePriority {
		return candidate.sourcePriority > current.sourcePriority
	}
	if candidate.rootOrder != current.rootOrder {
		return candidate.rootOrder > current.rootOrder
	}
	return candidate.Path > current.Path
}

func skillFileInDir(dir string) (string, bool) {
	for _, name := range []string{"SKILL.md", "skill.md"} {
		path := filepath.Join(dir, name)
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		return path, true
	}
	return "", false
}

func loadSkill(dir, path string, root Root, order int, opts LoadOptions) SkillMeta {
	info, _ := os.Stat(path)
	meta := SkillMeta{
		Name:           filepath.Base(dir),
		Path:           path,
		Dir:            dir,
		Location:       dir,
		Source:         root.Source,
		ID:             hash(path),
		UserInvocable:  true,
		CommandArgMode: "raw",
		sourcePriority: root.Priority,
		rootOrder:      order,
	}
	if info != nil {
		meta.ModTime = info.ModTime()
		meta.Size = info.Size()
	}

	body, err := LoadBody(path, 0)
	if err != nil {
		meta.ParseError = err.Error()
		meta.Hidden = true
		applyApprovalPolicy(&meta, opts.ApprovalPolicy)
		return meta
	}
	fm, rawTop, err := parseFrontMatter(body)
	if err != nil {
		meta.ParseError = err.Error()
		meta.Hidden = true
		applyApprovalPolicy(&meta, opts.ApprovalPolicy)
		return meta
	}
	if strings.TrimSpace(fm.Name) != "" {
		meta.Name = strings.TrimSpace(fm.Name)
	}
	meta.Description = strings.TrimSpace(firstNonEmpty(fm.Description, fm.Summary))
	meta.Summary = meta.Description
	meta.Homepage = strings.TrimSpace(fm.Homepage)
	if fm.UserInvocable != nil {
		meta.UserInvocable = *fm.UserInvocable
	}
	meta.DisableModelInvocation = fm.DisableModelInvocation
	meta.Hidden = meta.DisableModelInvocation
	meta.CommandDispatch = strings.TrimSpace(fm.CommandDispatch)
	meta.CommandTool = strings.TrimSpace(fm.CommandTool)
	if strings.TrimSpace(fm.CommandArgMode) != "" {
		meta.CommandArgMode = strings.TrimSpace(fm.CommandArgMode)
	}
	meta.Permissions = normalizeSkillPermissions(fm.Permissions)
	declaredTools, _ := parseDeclaredTools(rawTop["tools"])
	meta.AllowedTools = declaredTools

	manifest, err := loadManifest(dir)
	if err != nil {
		meta.ParseError = err.Error()
		meta.Hidden = true
	} else if len(manifest.Entrypoints) > 0 || len(manifest.Tools) > 0 || manifest.Permissions.Requested() || strings.TrimSpace(manifest.Summary) != "" {
		meta.Entrypoints = manifest.Entrypoints
		meta.AllowedTools = mergeStringLists(meta.AllowedTools, compactStrings(manifest.Tools))
		if requested := normalizeSkillPermissions(manifest.Permissions); requested.Requested() {
			meta.Permissions = requested
		}
		if strings.TrimSpace(manifest.Summary) != "" {
			meta.Summary = strings.TrimSpace(manifest.Summary)
		}
		if meta.Description == "" {
			meta.Summary = strings.TrimSpace(manifest.Summary)
			meta.Description = meta.Summary
		}
	}

	runtimeMeta, ok := normalizeRuntimeMetadata(fm.Metadata)
	if ok {
		meta.Metadata = runtimeMeta
	}
	if meta.Homepage == "" {
		meta.Homepage = strings.TrimSpace(meta.Metadata.Homepage)
	}
	if meta.Key == "" {
		meta.Key = strings.TrimSpace(firstNonEmpty(meta.Metadata.SkillKey, meta.Name))
	}
	entry := entryConfigForSkill(opts.Entries, meta)
	meta.RuntimeEnv = buildRuntimeEnv(meta, entry, opts.Env)
	applyOriginMetadata(&meta)
	applyEligibility(&meta, rawTop, body, entry, opts)
	applyApprovalPolicy(&meta, opts.ApprovalPolicy)
	return meta
}

func applyOriginMetadata(meta *SkillMeta) {
	if meta == nil || strings.TrimSpace(meta.Dir) == "" {
		return
	}
	origin, err := clawhub.ReadOrigin(meta.Dir)
	if err != nil {
		if meta.Source == SourceManaged {
			meta.PermissionNotes = append(meta.PermissionNotes, "managed skill missing origin metadata")
		}
		return
	}
	meta.Publisher = strings.TrimSpace(origin.Owner)
	meta.Registry = strings.TrimSpace(origin.Registry)
	meta.InstalledVersion = strings.TrimSpace(origin.InstalledVersion)
	meta.ScanStatus = normalizeSupplyChainValue(origin.ScanStatus)
	for _, finding := range origin.ScanFindings {
		if summary := strings.TrimSpace(finding.Summary()); summary != "" {
			meta.ScanFindings = append(meta.ScanFindings, summary)
		}
	}
	if modified, modErr := clawhub.LocalEdits(meta.Dir); modErr == nil {
		meta.Modified = modified
	}
}

func normalizeSkillPermissions(raw SkillPermissions) SkillPermissions {
	raw.AllowedPaths = compactStrings(raw.AllowedPaths)
	raw.AllowedHosts = compactStrings(raw.AllowedHosts)
	return raw
}

func (p SkillPermissions) Requested() bool {
	return p.Shell || p.Network || p.Write || len(p.AllowedPaths) > 0 || len(p.AllowedHosts) > 0
}

func parseDeclaredTools(raw any) ([]string, bool) {
	switch value := raw.(type) {
	case nil:
		return nil, true
	case []string:
		return compactStrings(value), true
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			name, ok := item.(string)
			if !ok {
				return nil, false
			}
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			out = append(out, name)
		}
		return compactStrings(out), true
	default:
		return nil, false
	}
}

func (p SkillPermissions) Summary() string {
	parts := make([]string, 0, 5)
	if p.Shell {
		parts = append(parts, "shell")
	}
	if p.Network {
		parts = append(parts, "network")
	}
	if p.Write {
		parts = append(parts, "write")
	}
	if len(p.AllowedPaths) > 0 {
		parts = append(parts, "paths="+strings.Join(p.AllowedPaths, ","))
	}
	if len(p.AllowedHosts) > 0 {
		parts = append(parts, "hosts="+strings.Join(p.AllowedHosts, ","))
	}
	if len(parts) == 0 {
		return "(none declared)"
	}
	return strings.Join(parts, "; ")
}

func applyApprovalPolicy(meta *SkillMeta, policy ApprovalPolicy) {
	if meta == nil {
		return
	}
	if meta.ParseError != "" {
		meta.PermissionState = "blocked"
		meta.PermissionNotes = append(meta.PermissionNotes, "metadata parse failed")
		return
	}
	if ownerBlocked(meta, policy.BlockedOwners) {
		meta.PermissionState = "blocked"
		meta.PermissionNotes = append(meta.PermissionNotes, "publisher blocked by policy: "+meta.Publisher)
		return
	}
	if strings.EqualFold(meta.ScanStatus, "blocked") {
		meta.PermissionState = "blocked"
		meta.PermissionNotes = append(meta.PermissionNotes, "install-time scan blocked this bundle")
		meta.PermissionNotes = append(meta.PermissionNotes, meta.ScanFindings...)
		return
	}
	if meta.Modified {
		meta.PermissionState = "quarantined"
		meta.PermissionNotes = append(meta.PermissionNotes, "local modifications detected since install")
		return
	}
	if strings.EqualFold(meta.ScanStatus, "quarantined") {
		meta.PermissionState = "quarantined"
		meta.PermissionNotes = append(meta.PermissionNotes, "install-time scan flagged this bundle for review")
		meta.PermissionNotes = append(meta.PermissionNotes, meta.ScanFindings...)
		return
	}
	trustedPublisher := publisherTrusted(meta, policy)
	if skillApproved(meta, policy.ApprovedSkills) || trustedPublisher {
		meta.PermissionState = "approved"
		if trustedPublisher {
			meta.PermissionNotes = append(meta.PermissionNotes, "approved by trusted publisher policy")
		} else {
			meta.PermissionNotes = append(meta.PermissionNotes, "approved in config")
		}
		return
	}
	if meta.Source == SourceManaged && skillNeedsApproval(meta) {
		if strings.TrimSpace(meta.Registry) == "" || strings.TrimSpace(meta.Publisher) == "" {
			meta.PermissionState = "quarantined"
			meta.PermissionNotes = append(meta.PermissionNotes, "managed skill is missing trusted origin details")
			return
		}
		if len(policy.TrustedOwners) > 0 || len(policy.TrustedRegistries) > 0 {
			meta.PermissionState = "quarantined"
			meta.PermissionNotes = append(meta.PermissionNotes, "publisher or registry is not trusted by policy")
			return
		}
	}
	if skillNeedsApproval(meta) {
		meta.PermissionState = "quarantined"
		meta.PermissionNotes = append(meta.PermissionNotes, "operator approval required before script execution")
		return
	}
	meta.PermissionState = "approved"
}

func ownerBlocked(meta *SkillMeta, blocked map[string]struct{}) bool {
	if meta == nil || len(blocked) == 0 {
		return false
	}
	_, ok := blocked[normalizeSupplyChainValue(meta.Publisher)]
	return ok
}

func publisherTrusted(meta *SkillMeta, policy ApprovalPolicy) bool {
	if meta == nil {
		return false
	}
	anyPolicy := len(policy.TrustedOwners) > 0 || len(policy.TrustedRegistries) > 0
	if !anyPolicy {
		return false
	}
	if len(policy.TrustedOwners) > 0 {
		if _, ok := policy.TrustedOwners[normalizeSupplyChainValue(meta.Publisher)]; !ok {
			return false
		}
	}
	if len(policy.TrustedRegistries) > 0 {
		if _, ok := policy.TrustedRegistries[normalizeSupplyChainValue(meta.Registry)]; !ok {
			return false
		}
	}
	return true
}

func normalizeSupplyChainValue(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.TrimRight(value, "/")
	return value
}

func skillNeedsApproval(meta *SkillMeta) bool {
	if meta == nil {
		return false
	}
	if meta.Permissions.Requested() || len(meta.Entrypoints) > 0 {
		return true
	}
	return skillHasRunnableContent(meta.Dir)
}

func skillApproved(meta *SkillMeta, approved map[string]struct{}) bool {
	if meta == nil || len(approved) == 0 {
		return false
	}
	for _, key := range []string{meta.Name, meta.Key, strings.ToLower(meta.Name), strings.ToLower(meta.Key)} {
		if _, ok := approved[key]; ok {
			return true
		}
	}
	return false
}

func loadManifest(dir string) (skillManifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "skill.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return skillManifest{}, nil
		}
		return skillManifest{}, err
	}
	var m skillManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return skillManifest{}, fmt.Errorf("invalid skill manifest: %w", err)
	}
	return m, nil
}

func parseFrontMatter(content string) (skillFrontMatter, map[string]any, error) {
	block, ok, err := frontMatterBlock(content)
	if err != nil {
		return skillFrontMatter{}, nil, err
	}
	if !ok {
		return skillFrontMatter{}, map[string]any{}, nil
	}
	var raw map[string]any
	if err := yaml.Unmarshal([]byte(block), &raw); err != nil {
		return skillFrontMatter{}, nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	raw = toStringMap(raw)
	var fm skillFrontMatter
	if err := yaml.Unmarshal([]byte(block), &fm); err != nil {
		return skillFrontMatter{}, nil, fmt.Errorf("invalid frontmatter: %w", err)
	}
	fm.Metadata = toStringMap(fm.Metadata)
	return fm, raw, nil
}

func frontMatterBlock(content string) (string, bool, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	if !strings.HasPrefix(content, "---\n") {
		return "", false, nil
	}
	rest := content[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", false, fmt.Errorf("invalid frontmatter: missing closing delimiter")
	}
	block := rest[:end]
	return block, true, nil
}

func extractFrontMatterSummary(content string) string {
	fm, _, err := parseFrontMatter(content)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(fm.Summary, fm.Description))
}

func normalizeRuntimeMetadata(raw map[string]any) (SkillRuntimeMeta, bool) {
	if len(raw) == 0 {
		return SkillRuntimeMeta{}, false
	}
	var selected any
	for _, key := range []string{"openclaw", "clawdbot", "clawdis"} {
		if value, ok := raw[key]; ok {
			selected = value
			break
		}
	}
	if selected == nil {
		return SkillRuntimeMeta{}, false
	}
	buf, err := json.Marshal(toStringMap(selected))
	if err != nil {
		return SkillRuntimeMeta{}, false
	}
	var meta SkillRuntimeMeta
	if err := json.Unmarshal(buf, &meta); err != nil {
		return SkillRuntimeMeta{}, false
	}
	return meta, true
}

func applyEligibility(meta *SkillMeta, rawTop map[string]any, body string, entry EntryConfig, opts LoadOptions) {
	if meta == nil {
		return
	}
	if meta.ParseError != "" {
		meta.Eligible = false
		return
	}
	meta.Disabled = entry.Enabled != nil && !*entry.Enabled
	if meta.Disabled {
		meta.Missing = append(meta.Missing, "disabled in config")
	}
	meta.Unsupported = append(meta.Unsupported, detectUnsupported(*meta, rawTop, body, opts)...)
	if meta.Metadata.Always && !meta.Disabled && len(meta.Unsupported) == 0 {
		meta.Eligible = true
		return
	}
	if len(meta.Metadata.OS) > 0 && !containsFold(meta.Metadata.OS, opts.OS) {
		meta.Missing = append(meta.Missing, fmt.Sprintf("os mismatch: requires %s", strings.Join(meta.Metadata.OS, ", ")))
	}
	for _, bin := range meta.Metadata.Requires.Bins {
		if !hasBinary(bin) {
			meta.Missing = append(meta.Missing, "missing binary: "+bin)
		}
	}
	if len(meta.Metadata.Requires.AnyBins) > 0 {
		ok := false
		for _, bin := range meta.Metadata.Requires.AnyBins {
			if hasBinary(bin) {
				ok = true
				break
			}
		}
		if !ok {
			meta.Missing = append(meta.Missing, "missing any-of binary: "+strings.Join(meta.Metadata.Requires.AnyBins, ", "))
		}
	}
	for _, envName := range meta.Metadata.Requires.Env {
		if strings.TrimSpace(meta.RuntimeEnv[envName]) == "" {
			meta.Missing = append(meta.Missing, "missing env: "+envName)
		}
	}
	for _, key := range meta.Metadata.Requires.Config {
		if !configTruthy(opts.GlobalConfig, entry.Config, key) {
			meta.Missing = append(meta.Missing, "missing config: "+key)
		}
	}
	meta.Eligible = !meta.Disabled && len(meta.Missing) == 0 && len(meta.Unsupported) == 0
}

func detectUnsupported(meta SkillMeta, rawTop map[string]any, body string, opts LoadOptions) []string {
	var unsupported []string
	if _, ok := rawTop["tools"]; ok {
		if _, valid := parseDeclaredTools(rawTop["tools"]); !valid {
			unsupported = append(unsupported, "frontmatter tools must be a list of string tool names")
		}
	}
	if meta.Metadata.Nix != nil && strings.TrimSpace(meta.Metadata.Nix.Plugin) != "" {
		unsupported = append(unsupported, "requires nix plugin: "+meta.Metadata.Nix.Plugin)
	}
	for _, toolName := range meta.AllowedTools {
		if len(opts.AvailableTools) == 0 {
			continue
		}
		if _, ok := opts.AvailableTools[toolName]; !ok {
			unsupported = append(unsupported, "requires unsupported tool: "+toolName)
		}
	}
	if meta.CommandDispatch != "" && meta.CommandDispatch != "tool" {
		unsupported = append(unsupported, "unsupported command-dispatch: "+meta.CommandDispatch)
	}
	if meta.CommandDispatch == "tool" {
		if meta.CommandTool == "" {
			unsupported = append(unsupported, "command-dispatch tool requires command-tool")
		} else if len(opts.AvailableTools) > 0 {
			if _, ok := opts.AvailableTools[meta.CommandTool]; !ok {
				unsupported = append(unsupported, "requires unsupported tool: "+meta.CommandTool)
			}
		}
	}
	if strings.Contains(body, "nodes.run") {
		unsupported = append(unsupported, "requires unsupported tool: nodes.run")
	}
	return unsupported
}

func mergeStringLists(base []string, extra []string) []string {
	out := append([]string{}, compactStrings(base)...)
	seen := map[string]struct{}{}
	for _, item := range out {
		seen[item] = struct{}{}
	}
	for _, item := range compactStrings(extra) {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func buildRuntimeEnv(meta SkillMeta, entry EntryConfig, baseEnv map[string]string) map[string]string {
	out := copyMap(baseEnv)
	for k, v := range entry.Env {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(out[k]) == "" {
			out[k] = v
		}
	}
	if meta.Metadata.PrimaryEnv != "" && strings.TrimSpace(entry.APIKey) != "" && strings.TrimSpace(out[meta.Metadata.PrimaryEnv]) == "" {
		out[meta.Metadata.PrimaryEnv] = entry.APIKey
	}
	return out
}

func entryConfigForSkill(entries map[string]EntryConfig, meta SkillMeta) EntryConfig {
	if len(entries) == 0 {
		return EntryConfig{}
	}
	if entry, ok := entries[meta.Key]; ok {
		return entry
	}
	return entries[meta.Name]
}

func (inv Inventory) Get(name string) (SkillMeta, bool) {
	if strings.TrimSpace(name) == "" {
		return SkillMeta{}, false
	}
	if s, ok := inv.byName[name]; ok {
		return s, true
	}
	s, ok := inv.byName[strings.ToLower(name)]
	return s, ok
}

func (inv Inventory) Summary(max int) string {
	return summarize(inv.Skills, max)
}

func (inv Inventory) ModelSummary(max int) string {
	filtered := make([]SkillMeta, 0, len(inv.Skills))
	for _, skill := range inv.Skills {
		if !skill.Eligible || skill.Hidden {
			continue
		}
		filtered = append(filtered, skill)
	}
	if len(filtered) == 0 {
		return "(no eligible skills found)"
	}
	if max <= 0 {
		max = 50
	}
	lines := make([]string, 0, min(len(filtered), max)+1)
	for i, skill := range filtered {
		if i >= max {
			lines = append(lines, "…")
			break
		}
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = strings.TrimSpace(skill.Summary)
		}
		location := strings.TrimSpace(skill.Location)
		if location == "" {
			location = skill.Dir
		}
		lines = append(lines, fmt.Sprintf("- %s | %s | %s", skill.Name, oneLine(desc, 140), location))
	}
	return strings.Join(lines, "\n")
}

func summarize(skills []SkillMeta, max int) string {
	if max <= 0 {
		max = 50
	}
	lines := []string{}
	for i, s := range skills {
		if i >= max {
			lines = append(lines, "…")
			break
		}
		desc := strings.TrimSpace(firstNonEmpty(s.Description, s.Summary))
		if desc == "" {
			lines = append(lines, "- "+s.Name)
			continue
		}
		lines = append(lines, "- "+s.Name+": "+oneLine(desc, 140))
	}
	if len(lines) == 0 {
		return "(no skills found)"
	}
	return strings.Join(lines, "\n")
}

func (inv Inventory) RunEnv() map[string]string {
	out := map[string]string{}
	for _, skill := range inv.Skills {
		if !skill.Eligible {
			continue
		}
		for k, v := range filteredRuntimeEnv(skill.RuntimeEnv) {
			if _, exists := out[k]; !exists {
				out[k] = v
			}
		}
	}
	return out
}

func (inv Inventory) RunEnvForSkill(name string) map[string]string {
	skill, ok := inv.Get(name)
	if !ok || !skill.Eligible {
		return nil
	}
	return filteredRuntimeEnv(skill.RuntimeEnv)
}

func (inv Inventory) ResolveBundlePath(name, relPath string) (string, error) {
	skill, ok := inv.Get(name)
	if !ok {
		return "", fmt.Errorf("skill not found: %s", name)
	}
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return skill.Dir, nil
	}
	if filepath.IsAbs(relPath) {
		return "", fmt.Errorf("bundle path must be relative")
	}
	root, err := filepath.EvalSymlinks(skill.Dir)
	if err != nil {
		return "", err
	}
	full := filepath.Join(root, relPath)
	clean := filepath.Clean(full)
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", err
		}
		real = clean
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fs.ErrPermission
	}
	return real, nil
}

func LoadBody(path string, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = 200000
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fs.ErrPermission
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(b) > maxBytes {
		b = b[:maxBytes]
	}
	return string(b), nil
}

func hash(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:8])
}

func hasBinary(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := exec.LookPath(name)
	return err == nil
}

func configTruthy(global map[string]any, skill map[string]any, path string) bool {
	if truthy(lookupPath(global, path)) {
		return true
	}
	return truthy(lookupPath(skill, path))
}

func lookupPath(root map[string]any, path string) any {
	if len(root) == 0 || strings.TrimSpace(path) == "" {
		return nil
	}
	var current any = root
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}
	return current
}

func truthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case string:
		return strings.TrimSpace(val) != ""
	case float64:
		return val != 0
	case int:
		return val != 0
	case []any:
		return len(val) > 0
	case map[string]any:
		return len(val) > 0
	default:
		return true
	}
}

func envMap(values []string) map[string]string {
	out := make(map[string]string, len(values))
	for _, raw := range values {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		out[key] = value
	}
	return out
}

func toStringMap(v any) map[string]any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[k] = normalizeValue(child)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, child := range val {
			out[fmt.Sprint(k)] = normalizeValue(child)
		}
		return out
	default:
		return map[string]any{}
	}
}

func normalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any, map[any]any:
		return toStringMap(val)
	case []any:
		out := make([]any, 0, len(val))
		for _, item := range val {
			out = append(out, normalizeValue(item))
		}
		return out
	default:
		return v
	}
}

func copyMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func filteredRuntimeEnv(env map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		if strings.TrimSpace(os.Getenv(k)) != "" {
			continue
		}
		out[k] = v
	}
	return out
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func skillHasRunnableContent(dir string) bool {
	if strings.TrimSpace(dir) == "" {
		return false
	}
	found := false
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return fs.SkipAll
		}
		if path == dir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		name := strings.ToLower(strings.TrimSpace(d.Name()))
		if name == "skill.md" || name == "skill.json" {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(name))
		if info.Mode()&0o111 != 0 || ext == ".sh" || ext == ".py" {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
````

## File: internal/agent/prompt.go
````go
package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/skills"
	"or3-intern/internal/triggers"
)

const DefaultSoul = `# Soul
I am or3-intern, a personal AI assistant.
- Be clear and direct
- Prefer deterministic, bounded work
- Use tools when needed; keep outputs short
`

const DefaultAgentInstructions = `# Agent Instructions
- Use pinned memory for stable facts.
- Retrieve relevant memory snippets before answering.
- Keep constant RAM usage: last N messages + top K memories only.
- Large tool outputs must spill to artifacts.
`

const DefaultToolNotes = `# Tool Usage Notes
exec:
- Commands have a timeout
- Dangerous commands blocked
- Output truncated
cron:
- Use cron tool for scheduled reminders.
`

const (
	defaultBootstrapMaxChars      = 20000
	defaultBootstrapTotalMaxChars = 150000
	defaultPinnedOneLineMax       = 220
	defaultRetrievedOneLineMax    = 240
	defaultSkillsSummaryMax       = 80
	defaultVisionMaxImages        = 4
	defaultVisionMaxImageBytes    = 4 << 20
	defaultVisionTotalBytes       = 8 << 20
	embedCacheTTL                 = 5 * time.Minute
	embedCacheMaxEntries          = 128
)

type embedCacheKey struct {
	model string
	input string
}

type embedCacheEntry struct {
	vec       []float32
	expiresAt time.Time
	usedAt    time.Time
}

var promptEmbedCache = struct {
	mu      sync.Mutex
	entries map[embedCacheKey]embedCacheEntry
}{entries: map[embedCacheKey]embedCacheEntry{}}

type PromptParts struct {
	System  []providers.ChatMessage
	History []providers.ChatMessage
}

// BuildOptions holds options for building a prompt.
type BuildOptions struct {
	SessionKey  string
	UserMessage string
	Autonomous  bool // true for cron/webhook/file-change events
	EventMeta   map[string]any
}

type Builder struct {
	DB           *db.DB
	Artifacts    *artifacts.Store
	Skills       skills.Inventory
	Mem          *memory.Retriever
	Provider     *providers.Client
	EmbedModel   string
	EnableVision bool

	Soul                   string
	AgentInstructions      string
	ToolNotes              string
	BootstrapMaxChars      int
	BootstrapTotalMaxChars int
	SkillsSummaryMax       int

	HistoryMax int
	VectorK    int
	FTSK       int
	TopK       int

	// New fields for lightweight OpenClaw parity
	IdentityText       string               // content of IDENTITY.md
	StaticMemory       string               // content of MEMORY.md
	HeartbeatText      string               // content of HEARTBEAT.md – injected only for autonomous turns
	HeartbeatTasksFile string               // configured heartbeat file path for per-turn refresh
	DocRetriever       *memory.DocRetriever // for indexed file context
	DocRetrieveLimit   int                  // max docs to retrieve
	WorkspaceDir       string
}

// Build builds a prompt snapshot. It is a convenience wrapper around BuildWithOptions.
func (b *Builder) Build(ctx context.Context, sessionKey string, userMessage string) (PromptParts, []memory.Retrieved, error) {
	return b.BuildWithOptions(ctx, BuildOptions{SessionKey: sessionKey, UserMessage: userMessage})
}

// BuildWithOptions builds a prompt snapshot using the provided options.
func (b *Builder) BuildWithOptions(ctx context.Context, opts BuildOptions) (PromptParts, []memory.Retrieved, error) {
	scopeKey := opts.SessionKey
	if b.DB != nil && strings.TrimSpace(opts.SessionKey) != "" {
		if resolved, err := b.DB.ResolveScopeKey(ctx, opts.SessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	pinned, err := b.DB.GetPinned(ctx, scopeKey)
	if err != nil {
		return PromptParts{}, nil, err
	}
	pinnedText := formatPinned(pinned)

	// embed and retrieve
	var retrieved []memory.Retrieved
	if b.Mem != nil && b.Provider != nil && strings.TrimSpace(opts.UserMessage) != "" {
		vec, err := cachedEmbed(ctx, b.Provider, b.EmbedModel, opts.UserMessage)
		if err == nil {
			retrieved, _ = b.Mem.Retrieve(ctx, scopeKey, opts.UserMessage, vec, b.VectorK, b.FTSK, b.TopK)
		}
	}
	memText := formatRetrieved(retrieved)

	// indexed doc context
	var docContextText string
	if b.DocRetriever != nil && strings.TrimSpace(opts.UserMessage) != "" {
		limit := b.DocRetrieveLimit
		if limit <= 0 {
			limit = 5
		}
		docs, _ := b.DocRetriever.RetrieveDocs(ctx, scope.GlobalMemoryScope, opts.UserMessage, limit)
		if len(docs) > 0 {
			var sb strings.Builder
			for i, d := range docs {
				sb.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, d.Path, d.Excerpt))
			}
			docContextText = strings.TrimSpace(sb.String())
		}
	}
	workspaceContextText := memory.BuildWorkspaceContext(memory.WorkspaceContextConfig{
		WorkspaceDir: b.WorkspaceDir,
	}, opts.UserMessage)

	histRows, err := b.DB.GetLastMessagesScoped(ctx, opts.SessionKey, b.HistoryMax)
	if err != nil {
		return PromptParts{}, nil, err
	}
	visionBudget := newVisionBudget()
	hist := make([]providers.ChatMessage, 0, len(histRows))
	for _, m := range histRows {
		msg := providers.ChatMessage{Role: m.Role, Content: m.Content}
		var payload map[string]any
		if err := json.Unmarshal([]byte(m.PayloadJSON), &payload); err == nil {
			if m.Role == "assistant" {
				if raw, ok := payload["tool_calls"]; ok {
					b, _ := json.Marshal(raw)
					var tcs []providers.ToolCall
					if err := json.Unmarshal(b, &tcs); err == nil {
						msg.ToolCalls = tcs
					}
				}
			}
			if m.Role == "user" {
				msg.Content = b.buildUserContent(ctx, m.Content, attachmentsFromPayload(payload), visionBudget)
			}
		}
		hist = append(hist, msg)
	}

	heartbeat := ""
	structuredContext := ""
	structuredMax := b.BootstrapMaxChars
	if structuredMax <= 0 {
		structuredMax = defaultBootstrapMaxChars
	}
	if opts.Autonomous {
		heartbeat = b.currentHeartbeatText()
		structuredContext = formatStructuredEventContext(opts.EventMeta, structuredMax)
	}
	sysText := b.composeSystemPrompt(pinnedText, memText, b.IdentityText, b.StaticMemory, heartbeat, structuredContext, docContextText, workspaceContextText)
	sys := []providers.ChatMessage{
		{Role: "system", Content: sysText},
	}
	return PromptParts{System: sys, History: hist}, retrieved, nil
}

func (b *Builder) currentHeartbeatText() string {
	if b == nil {
		return ""
	}
	if path, text, err := heartbeat.LoadTasksFile(b.HeartbeatTasksFile, b.WorkspaceDir); err == nil && strings.TrimSpace(path) != "" {
		if heartbeat.HasActiveInstructions(text) {
			return text
		}
		return ""
	}
	return strings.TrimSpace(b.HeartbeatText)
}

func attachmentsFromPayload(payload map[string]any) []artifacts.Attachment {
	if len(payload) == 0 {
		return nil
	}
	raw := payload["attachments"]
	if raw == nil {
		if meta, ok := payload["meta"].(map[string]any); ok {
			raw = meta["attachments"]
		}
	}
	if raw == nil {
		return nil
	}
	b, _ := json.Marshal(raw)
	var atts []artifacts.Attachment
	if err := json.Unmarshal(b, &atts); err != nil {
		return nil
	}
	out := make([]artifacts.Attachment, 0, len(atts))
	for _, att := range atts {
		if strings.TrimSpace(att.ArtifactID) == "" {
			continue
		}
		if strings.TrimSpace(att.Filename) == "" {
			att.Filename = "attachment"
		}
		if strings.TrimSpace(att.Kind) == "" {
			att.Kind = artifacts.DetectKind(att.Filename, att.Mime)
		}
		out = append(out, att)
	}
	return out
}

type visionBudget struct {
	remainingImages int
	remainingBytes  int64
}

func newVisionBudget() *visionBudget {
	return &visionBudget{
		remainingImages: defaultVisionMaxImages,
		remainingBytes:  defaultVisionTotalBytes,
	}
}

func (b *Builder) buildUserContent(ctx context.Context, text string, atts []artifacts.Attachment, budget *visionBudget) any {
	if !b.EnableVision || b.Artifacts == nil || len(atts) == 0 {
		return text
	}
	parts := make([]map[string]any, 0, len(atts)+1)
	imageParts := 0
	if strings.TrimSpace(text) != "" {
		parts = append(parts, map[string]any{
			"type": "text",
			"text": text,
		})
	}
	for _, att := range atts {
		if strings.TrimSpace(att.Kind) != artifacts.KindImage && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(att.Mime)), "image/") {
			continue
		}
		part, ok := b.imagePart(ctx, att, budget)
		if !ok {
			continue
		}
		parts = append(parts, part)
		imageParts++
	}
	if imageParts == 0 {
		return text
	}
	return parts
}

func (b *Builder) imagePart(ctx context.Context, att artifacts.Attachment, budget *visionBudget) (map[string]any, bool) {
	if budget == nil || budget.remainingImages <= 0 || budget.remainingBytes <= 0 {
		return nil, false
	}
	stored, err := b.Artifacts.Lookup(ctx, att.ArtifactID)
	if err != nil {
		return nil, false
	}
	sizeBytes := stored.SizeBytes
	if sizeBytes <= 0 {
		info, err := os.Stat(stored.Path)
		if err != nil {
			return nil, false
		}
		sizeBytes = info.Size()
	}
	if sizeBytes <= 0 || sizeBytes > defaultVisionMaxImageBytes || sizeBytes > budget.remainingBytes {
		return nil, false
	}
	data, err := readCappedFile(stored.Path, defaultVisionMaxImageBytes)
	if err != nil {
		return nil, false
	}
	if int64(len(data)) > budget.remainingBytes {
		return nil, false
	}
	mimeType := strings.TrimSpace(stored.Mime)
	if mimeType == "" {
		mimeType = strings.TrimSpace(att.Mime)
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(stored.Path))
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, false
	}
	budget.remainingImages--
	budget.remainingBytes -= int64(len(data))
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data),
		},
	}, true
}

func readCappedFile(path string, maxBytes int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file exceeds vision limit")
	}
	return data, nil
}

func (b *Builder) composeSystemPrompt(pinnedText, memText, identityText, staticMemoryText, heartbeatText, structuredContextText, docContextText, workspaceContextText string) string {
	maxEach := b.BootstrapMaxChars
	if maxEach <= 0 {
		maxEach = defaultBootstrapMaxChars
	}
	maxTotal := b.BootstrapTotalMaxChars
	if maxTotal <= 0 {
		maxTotal = defaultBootstrapTotalMaxChars
	}
	skillsMax := b.SkillsSummaryMax
	if skillsMax <= 0 {
		skillsMax = defaultSkillsSummaryMax
	}

	soul := strings.TrimSpace(b.Soul)
	if soul == "" {
		soul = DefaultSoul
	}
	inst := strings.TrimSpace(b.AgentInstructions)
	if inst == "" {
		inst = DefaultAgentInstructions
	}
	notes := strings.TrimSpace(b.ToolNotes)
	if notes == "" {
		notes = DefaultToolNotes
	}

	type section struct {
		title string
		text  string
	}
	// Build sections in order, omitting optional ones when empty.
	sections := []section{
		{title: "SOUL.md", text: truncateText(soul, maxEach)},
	}
	if t := strings.TrimSpace(identityText); t != "" {
		sections = append(sections, section{title: "Identity", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "AGENTS.md", text: truncateText(inst, maxEach)})
	if t := strings.TrimSpace(staticMemoryText); t != "" {
		sections = append(sections, section{title: "Static Memory", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "TOOLS.md", text: truncateText(notes, maxEach)})
	if t := strings.TrimSpace(heartbeatText); t != "" {
		sections = append(sections, section{title: "Heartbeat", text: truncateText(t, maxEach)})
	}
	if t := strings.TrimSpace(structuredContextText); t != "" {
		sections = append(sections, section{title: "Structured Trigger Context", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "Pinned Memory", text: pinnedText})
	sections = append(sections, section{title: "Retrieved Memory", text: memText})
	if t := strings.TrimSpace(workspaceContextText); t != "" {
		sections = append(sections, section{title: "Workspace Context", text: truncateText(t, maxEach)})
	}
	if t := strings.TrimSpace(docContextText); t != "" {
		sections = append(sections, section{title: "Indexed File Context", text: truncateText(t, maxEach)})
	}
	sections = append(sections, section{title: "Skills Inventory", text: b.Skills.ModelSummary(skillsMax)})

	var out strings.Builder
	out.WriteString("# System Prompt\n")
	for _, s := range sections {
		out.WriteString("\n## ")
		out.WriteString(s.title)
		out.WriteString("\n")
		out.WriteString(strings.TrimSpace(s.text))
		out.WriteString("\n")
	}
	return truncateText(strings.TrimSpace(out.String()), maxTotal)
}

func formatStructuredEventContext(meta map[string]any, max int) string {
	if len(meta) == 0 {
		return ""
	}
	raw, ok := meta[triggers.MetaKeyStructuredEvent]
	if !ok {
		return ""
	}
	return truncateText(triggers.StructuredEventJSON(raw), max)
}

func truncateText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "\n…[truncated]"
	}
	return s
}

func formatPinned(m map[string]string) string {
	if len(m) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		v := strings.TrimSpace(m[k])
		if v == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", k, oneLine(v, defaultPinnedOneLineMax)))
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return "(none)"
	}
	return s
}

func formatRetrieved(ms []memory.Retrieved) string {
	if len(ms) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for i, m := range ms {
		b.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, m.Source, oneLine(m.Text, defaultRetrievedOneLineMax)))
	}
	return strings.TrimSpace(b.String())
}

func oneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		s = s[:max] + "…"
	}
	return s
}

func cachedEmbed(ctx context.Context, provider *providers.Client, model, input string) ([]float32, error) {
	input = strings.TrimSpace(input)
	model = strings.TrimSpace(model)
	if provider == nil {
		return nil, fmt.Errorf("provider not configured")
	}
	if model == "" || input == "" {
		return provider.Embed(ctx, model, input)
	}
	key := embedCacheKey{model: model, input: input}
	now := time.Now()
	promptEmbedCache.mu.Lock()
	if entry, ok := promptEmbedCache.entries[key]; ok && entry.expiresAt.After(now) {
		entry.usedAt = now
		promptEmbedCache.entries[key] = entry
		vec := append([]float32(nil), entry.vec...)
		promptEmbedCache.mu.Unlock()
		return vec, nil
	}
	promptEmbedCache.mu.Unlock()

	vec, err := provider.Embed(ctx, model, input)
	if err != nil {
		return nil, err
	}
	promptEmbedCache.mu.Lock()
	if len(promptEmbedCache.entries) >= embedCacheMaxEntries {
		var oldestKey embedCacheKey
		var oldest time.Time
		for k, entry := range promptEmbedCache.entries {
			if oldest.IsZero() || entry.usedAt.Before(oldest) {
				oldest = entry.usedAt
				oldestKey = k
			}
		}
		delete(promptEmbedCache.entries, oldestKey)
	}
	promptEmbedCache.entries[key] = embedCacheEntry{
		vec:       append([]float32(nil), vec...),
		expiresAt: now.Add(embedCacheTTL),
		usedAt:    now,
	}
	promptEmbedCache.mu.Unlock()
	return vec, nil
}
````

## File: internal/db/store.go
````go
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"or3-intern/internal/scope"
)

type Message struct {
	ID          int64
	SessionKey  string
	Role        string
	Content     string
	PayloadJSON string
	CreatedAt   int64
}

type ConsolidationMessage struct {
	ID      int64
	Role    string
	Content string
}

type ConsolidationWrite struct {
	SessionKey    string
	ScopeKey      string
	NoteText      string
	Embedding     []byte
	SourceMsgID   sql.NullInt64
	NoteTags      string
	CanonicalKey  string
	CanonicalText string
	CursorMsgID   int64
}

const (
	SubagentStatusQueued      = "queued"
	SubagentStatusRunning     = "running"
	SubagentStatusSucceeded   = "succeeded"
	SubagentStatusFailed      = "failed"
	SubagentStatusInterrupted = "interrupted"
)

var ErrSubagentQueueFull = errors.New("subagent queue is full")

type SubagentJob struct {
	ID               string
	ParentSessionKey string
	ChildSessionKey  string
	Channel          string
	ReplyTo          string
	Task             string
	Status           string
	ResultPreview    string
	ArtifactID       string
	ErrorText        string
	RequestedAt      int64
	StartedAt        int64
	FinishedAt       int64
	Attempts         int
	MetadataJSON     string
}

func (d *DB) EnsureSession(ctx context.Context, key string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		key, now, now)
	return err
}

func (d *DB) AppendMessage(ctx context.Context, sessionKey, role, content string, payload any) (int64, error) {
	if err := d.EnsureSession(ctx, sessionKey); err != nil {
		return 0, err
	}
	pb, _ := json.Marshal(payload)
	now := NowMS()
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := d.SQL.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey); err != nil {
		return id, err
	}
	return id, nil
}

func (d *DB) GetLastMessages(ctx context.Context, sessionKey string, limit int) ([]Message, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? ORDER BY id DESC LIMIT ?`, sessionKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// align so first is user (best-effort)
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out, rows.Err()
}

func (d *DB) GetPinned(ctx context.Context, sessionKey string) (map[string]string, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT key, content FROM memory_pinned
		 WHERE session_key IN (?, ?)
		 ORDER BY CASE WHEN session_key=? THEN 1 ELSE 0 END, key`,
		scope.GlobalMemoryScope, sessionKey, sessionKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, c string
		if err := rows.Scan(&k, &c); err != nil {
			return nil, err
		}
		out[k] = c
	}
	return out, rows.Err()
}

func (d *DB) GetPinnedValue(ctx context.Context, sessionKey, key string) (string, bool, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	row := d.SQL.QueryRowContext(ctx,
		`SELECT content FROM memory_pinned WHERE session_key=? AND key=?`,
		sessionKey, key)
	var out string
	if err := row.Scan(&out); err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, err
	}
	return out, true, nil
}

func (d *DB) UpsertPinned(ctx context.Context, sessionKey, key, content string) error {
	sessionKey = normalizeMemorySession(sessionKey)
	_, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_pinned(session_key, key, content, updated_at) VALUES(?,?,?,?)
		 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
		sessionKey, key, content, NowMS())
	return err
}

func (d *DB) InsertMemoryNote(ctx context.Context, sessionKey, text string, embedding []byte, sourceMsgID sql.NullInt64, tags string) (int64, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	if len(embedding) >= 4 && len(embedding)%4 == 0 {
		if err := d.EnsureMemoryVecIndexWithDim(ctx, len(embedding)/4); err != nil {
			return 0, err
		}
	}
	res, err := d.SQL.ExecContext(ctx,
		`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at) VALUES(?,?,?,?,?,?)`,
		sessionKey, text, embedding, sourceMsgID, tags, NowMS())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	_ = d.upsertMemoryVec(ctx, id, sessionKey, text, embedding)
	return id, nil
}

func (d *DB) upsertMemoryVec(ctx context.Context, noteID int64, sessionKey, text string, embedding []byte) error {
	if d == nil || d.VecSQL == nil {
		return nil
	}
	if len(embedding) < 4 || len(embedding)%4 != 0 {
		return nil
	}
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return err
	}
	if dims == 0 {
		if err := d.EnsureMemoryVecIndexWithDim(ctx, len(embedding)/4); err != nil {
			return err
		}
		dims, err = d.MemoryVectorDims(ctx)
		if err != nil {
			return err
		}
	}
	if dims != len(embedding)/4 {
		return nil
	}
	_, err = d.VecSQL.ExecContext(ctx,
		`INSERT OR REPLACE INTO memory_vec(note_id, session_key, embedding, text) VALUES(?,?,?,?)`,
		noteID, sessionKey, embedding, text)
	return err
}

type MemoryNoteRow struct {
	ID              int64
	Text            string
	Embedding       []byte
	SourceMessageID sql.NullInt64
	Tags            string
	CreatedAt       int64
}

func (d *DB) StreamMemoryNotes(ctx context.Context, sessionKey string) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at FROM memory_notes
		 WHERE session_key IN (?, ?)`,
		scope.GlobalMemoryScope, sessionKey)
}

func (d *DB) StreamMemoryNotesScopeLimit(ctx context.Context, sessionKey string, limit int) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	if limit <= 0 {
		return d.SQL.QueryContext(ctx,
			`SELECT id, text, embedding, source_message_id, tags, created_at
			 FROM memory_notes WHERE session_key=?`,
			sessionKey)
	}
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at
		 FROM memory_notes WHERE session_key=? ORDER BY id DESC LIMIT ?`,
		sessionKey, limit)
}

func (d *DB) StreamMemoryNotesLimit(ctx context.Context, sessionKey string, limit int) (*sql.Rows, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	if limit <= 0 {
		return d.StreamMemoryNotes(ctx, sessionKey)
	}
	return d.SQL.QueryContext(ctx,
		`SELECT id, text, embedding, source_message_id, tags, created_at
		 FROM memory_notes WHERE session_key IN (?, ?) ORDER BY id DESC LIMIT ?`,
		scope.GlobalMemoryScope, sessionKey, limit)
}

type FTSCandidate struct {
	ID        int64
	Text      string
	Rank      float64
	CreatedAt int64
}

type VecCandidateRow struct {
	ID        int64
	Text      string
	Distance  float64
	CreatedAt int64
}

func (d *DB) SearchMemoryVectors(ctx context.Context, sessionKey string, queryVec []byte, k int) ([]VecCandidateRow, error) {
	if d == nil || k <= 0 || len(queryVec) == 0 {
		return nil, nil
	}
	scopes := []string{scope.GlobalMemoryScope}
	if trimmed := strings.TrimSpace(sessionKey); trimmed != "" && trimmed != scope.GlobalMemoryScope {
		scopes = append(scopes, normalizeMemorySession(trimmed))
	}
	seen := make(map[int64]struct{}, k*len(scopes))
	out := make([]VecCandidateRow, 0, k*len(scopes))
	for _, memoryScope := range scopes {
		rows, err := d.SearchVecScope(ctx, memoryScope, queryVec, k)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			rows, err = d.SearchVecScopeFallback(ctx, memoryScope, queryVec, k)
			if err != nil {
				return nil, err
			}
		}
		for _, row := range rows {
			if _, ok := seen[row.ID]; ok {
				continue
			}
			seen[row.ID] = struct{}{}
			out = append(out, row)
		}
	}
	return out, nil
}

func (d *DB) SearchVecScope(ctx context.Context, sessionKey string, queryVec []byte, k int) ([]VecCandidateRow, error) {
	if d == nil || d.VecSQL == nil {
		return nil, nil
	}
	if k <= 0 || len(queryVec) == 0 {
		return nil, nil
	}
	dims, err := d.MemoryVectorDims(ctx)
	if err != nil {
		return nil, err
	}
	if dims == 0 || len(queryVec) != dims*4 {
		return nil, nil
	}
	rows, err := d.VecSQL.QueryContext(ctx,
		`SELECT memory_vec.note_id, memory_vec.text, distance, memory_notes.created_at
		 FROM memory_vec
		 JOIN memory_notes ON memory_notes.id = memory_vec.note_id
		 WHERE memory_vec.embedding MATCH ? AND memory_vec.k = ? AND memory_vec.session_key = ?
		 ORDER BY distance`,
		queryVec, k, normalizeMemorySession(sessionKey))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVecCandidateRows(rows)
}

func (d *DB) SearchVecScopeFallback(ctx context.Context, sessionKey string, queryVec []byte, k int) ([]VecCandidateRow, error) {
	if d == nil || d.VecSQL == nil {
		return nil, nil
	}
	if k <= 0 || len(queryVec) == 0 || len(queryVec)%4 != 0 {
		return nil, nil
	}
	rows, err := d.VecSQL.QueryContext(ctx,
		`SELECT id, text, vec_distance_cosine(embedding, ?) AS distance, created_at
		 FROM memory_notes
		 WHERE session_key=? AND typeof(embedding)='blob' AND length(embedding)=?
		 ORDER BY distance ASC
		 LIMIT ?`,
		queryVec, normalizeMemorySession(sessionKey), len(queryVec), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVecCandidateRows(rows)
}

func scanVecCandidateRows(rows *sql.Rows) ([]VecCandidateRow, error) {
	var out []VecCandidateRow
	for rows.Next() {
		var item VecCandidateRow
		var distance sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.Text, &distance, &item.CreatedAt); err != nil {
			return nil, err
		}
		if !distance.Valid {
			continue
		}
		item.Distance = distance.Float64
		out = append(out, item)
	}
	return out, rows.Err()
}

func (d *DB) SearchFTS(ctx context.Context, sessionKey, query string, k int) ([]FTSCandidate, error) {
	sessionKey = normalizeMemorySession(sessionKey)
	// bm25 lower is better; invert
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT memory_fts.rowid, memory_fts.text, bm25(memory_fts) as rank, memory_notes.created_at
		 FROM memory_fts
		 JOIN memory_notes ON memory_notes.id = memory_fts.rowid
		 WHERE memory_fts MATCH ? AND memory_notes.session_key IN (?, ?)
		 ORDER BY rank LIMIT ?`,
		query, scope.GlobalMemoryScope, sessionKey, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []FTSCandidate
	for rows.Next() {
		var id int64
		var text string
		var rank float64
		var createdAt int64
		if err := rows.Scan(&id, &text, &rank, &createdAt); err != nil {
			return nil, err
		}
		out = append(out, FTSCandidate{ID: id, Text: text, Rank: rank, CreatedAt: createdAt})
	}
	return out, rows.Err()
}

// GetConsolidationRange returns (lastConsolidatedID, oldestActiveID).
// oldestActiveID is the minimum ID among the last historyMax messages,
// or 0 if there are no messages in the session.
// Messages older than oldestActiveID (and newer than lastConsolidatedID)
// may be eligible for consolidation.
func (d *DB) GetConsolidationRange(ctx context.Context, sessionKey string, historyMax int) (lastConsolidatedID int64, oldestActiveID int64, err error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT last_consolidated_msg_id FROM sessions WHERE key=?`, sessionKey)
	if scanErr := row.Scan(&lastConsolidatedID); scanErr != nil {
		// Session row not found yet → nothing to consolidate.
		return 0, 0, nil
	}

	// Oldest ID in the active window (last historyMax messages).
	// If the total number of messages is < historyMax, MIN returns NULL → 0.
	activeRow := d.SQL.QueryRowContext(ctx,
		`SELECT COALESCE(MIN(id), 0) FROM
		 (SELECT id FROM messages WHERE session_key=? ORDER BY id DESC LIMIT ?)`,
		sessionKey, historyMax)
	if scanErr := activeRow.Scan(&oldestActiveID); scanErr != nil {
		return lastConsolidatedID, 0, scanErr
	}
	return lastConsolidatedID, oldestActiveID, nil
}

// GetMessagesForConsolidation returns messages with afterID < id < beforeID
// in chronological order. Used to build the window to summarize.
func (d *DB) GetMessagesForConsolidation(ctx context.Context, sessionKey string, afterID, beforeID int64) ([]Message, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, session_key, role, content, payload_json, created_at
		 FROM messages WHERE session_key=? AND id > ? AND id < ?
		 ORDER BY id ASC`,
		sessionKey, afterID, beforeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (d *DB) GetConsolidationMessages(ctx context.Context, sessionKey string, afterID, beforeID int64, limit int) ([]ConsolidationMessage, error) {
	if beforeID <= 0 {
		beforeID = math.MaxInt64
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, role, content
		 FROM messages WHERE session_key=? AND id > ? AND id < ?
		 ORDER BY id ASC LIMIT ?`,
		sessionKey, afterID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ConsolidationMessage, 0, limit)
	for rows.Next() {
		var m ConsolidationMessage
		if err := rows.Scan(&m.ID, &m.Role, &m.Content); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetLastConsolidatedID records the highest message ID that has been
// consolidated into memory notes for this session.
func (d *DB) SetLastConsolidatedID(ctx context.Context, sessionKey string, id int64) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=? WHERE key=?`, id, sessionKey)
	return err
}

func (d *DB) WriteConsolidation(ctx context.Context, w ConsolidationWrite) (int64, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var noteID int64
	if strings.TrimSpace(w.NoteText) != "" {
		scopeKey := normalizeMemorySession(w.ScopeKey)
		res, err := tx.ExecContext(ctx,
			`INSERT INTO memory_notes(session_key, text, embedding, source_message_id, tags, created_at) VALUES(?,?,?,?,?,?)`,
			scopeKey, w.NoteText, w.Embedding, w.SourceMsgID, w.NoteTags, NowMS())
		if err != nil {
			return 0, err
		}
		noteID, _ = res.LastInsertId()
	}
	if strings.TrimSpace(w.CanonicalKey) != "" {
		scopeKey := normalizeMemorySession(w.ScopeKey)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO memory_pinned(session_key, key, content, updated_at) VALUES(?,?,?,?)
			 ON CONFLICT(session_key, key) DO UPDATE SET content=excluded.content, updated_at=excluded.updated_at`,
			scopeKey, w.CanonicalKey, w.CanonicalText, NowMS())
		if err != nil {
			return 0, err
		}
	}
	res, err := tx.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=? WHERE key=?`, w.CursorMsgID, w.SessionKey)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, sql.ErrNoRows
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if noteID > 0 {
		_ = d.upsertMemoryVec(ctx, noteID, normalizeMemorySession(w.ScopeKey), w.NoteText, w.Embedding)
	}
	return noteID, nil
}

func (d *DB) ResetSessionHistory(ctx context.Context, sessionKey string) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM messages WHERE session_key=?`, sessionKey); err != nil {
		return err
	}
	now := NowMS()
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET last_consolidated_msg_id=0, updated_at=? WHERE key=?`,
		now, sessionKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (d *DB) EnqueueSubagentJob(ctx context.Context, job SubagentJob) error {
	return d.EnqueueSubagentJobLimited(ctx, job, 0)
}

func (d *DB) EnqueueSubagentJobLimited(ctx context.Context, job SubagentJob, maxQueued int) error {
	if job.RequestedAt == 0 {
		job.RequestedAt = NowMS()
	}
	if strings.TrimSpace(job.Status) == "" {
		job.Status = SubagentStatusQueued
	}
	if strings.TrimSpace(job.MetadataJSON) == "" {
		job.MetadataJSON = "{}"
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureSessionTx(ctx, tx, job.ParentSessionKey); err != nil {
		return err
	}
	if err := ensureSessionTx(ctx, tx, job.ChildSessionKey); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO subagent_jobs(
			id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		)
		SELECT ?,?,?,?,?,?,?,?,?,?,?,?,?,?,?
		WHERE ? <= 0 OR (SELECT COUNT(*) FROM subagent_jobs WHERE status=?) < ?`,
		job.ID,
		job.ParentSessionKey,
		job.ChildSessionKey,
		job.Channel,
		job.ReplyTo,
		job.Task,
		job.Status,
		job.ResultPreview,
		job.ArtifactID,
		job.ErrorText,
		job.RequestedAt,
		job.StartedAt,
		job.FinishedAt,
		job.Attempts,
		job.MetadataJSON,
		maxQueued,
		SubagentStatusQueued,
		maxQueued,
	)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrSubagentQueueFull
	}
	return tx.Commit()
}

func (d *DB) GetSubagentJob(ctx context.Context, id string) (SubagentJob, bool, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE id=?`, id)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return SubagentJob{}, false, nil
		}
		return SubagentJob{}, false, err
	}
	return job, true, nil
}

func (d *DB) ListQueuedSubagentJobs(ctx context.Context) ([]SubagentJob, error) {
	return d.listSubagentJobsByStatus(ctx, SubagentStatusQueued)
}

func (d *DB) ListRunningSubagentJobs(ctx context.Context) ([]SubagentJob, error) {
	return d.listSubagentJobsByStatus(ctx, SubagentStatusRunning)
}

func (d *DB) listSubagentJobsByStatus(ctx context.Context, status string) ([]SubagentJob, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE status=? ORDER BY requested_at ASC, id ASC`,
		status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SubagentJob
	for rows.Next() {
		job, err := scanSubagentJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}

func (d *DB) MarkSubagentRunning(ctx context.Context, id string) error {
	now := NowMS()
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, started_at=CASE WHEN started_at=0 THEN ? ELSE started_at END, attempts=attempts+1
		 WHERE id=? AND status=?`,
		SubagentStatusRunning, now, id, SubagentStatusQueued)
	return err
}

func (d *DB) ClaimNextSubagentJob(ctx context.Context) (*SubagentJob, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE status=? ORDER BY requested_at ASC, id ASC LIMIT 1`,
		SubagentStatusQueued)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs SET status=?, started_at=?, attempts=attempts+1 WHERE id=? AND status=?`,
		SubagentStatusRunning, now, job.ID, SubagentStatusQueued)
	if err != nil {
		return nil, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, tx.Commit()
	}
	job.Status = SubagentStatusRunning
	job.StartedAt = now
	job.Attempts++
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &job, nil
}

func (d *DB) AbortQueuedSubagentJob(ctx context.Context, id, errText string) (SubagentJob, bool, error) {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return SubagentJob{}, false, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx,
		`SELECT id, parent_session_key, child_session_key, channel, reply_to, task, status,
			result_preview, artifact_id, error_text, requested_at, started_at, finished_at, attempts, metadata_json
		 FROM subagent_jobs WHERE id=?`,
		id)
	job, err := scanSubagentJob(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return SubagentJob{}, false, nil
		}
		return SubagentJob{}, false, err
	}

	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=? AND status=?`,
		SubagentStatusInterrupted, errText, now, id, SubagentStatusQueued)
	if err != nil {
		return SubagentJob{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return SubagentJob{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return SubagentJob{}, false, err
	}
	if affected == 0 {
		return job, false, nil
	}
	job.Status = SubagentStatusInterrupted
	job.ErrorText = errText
	job.FinishedAt = now
	return job, true, nil
}

func (d *DB) MarkSubagentSucceeded(ctx context.Context, id, preview, artifactID string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, result_preview=?, artifact_id=?, error_text='', finished_at=?
		 WHERE id=?`,
		SubagentStatusSucceeded, preview, artifactID, NowMS(), id)
	return err
}

func (d *DB) MarkSubagentFailed(ctx context.Context, id, errText string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=?`,
		SubagentStatusFailed, errText, NowMS(), id)
	return err
}

func (d *DB) MarkSubagentInterrupted(ctx context.Context, id, errText string) error {
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE id=?`,
		SubagentStatusInterrupted, errText, NowMS(), id)
	return err
}

func (d *DB) MarkRunningSubagentsInterrupted(ctx context.Context, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "interrupted during restart"
	}
	_, err := d.SQL.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, error_text=?, finished_at=?
		 WHERE status=?`,
		SubagentStatusInterrupted, reason, NowMS(), SubagentStatusRunning)
	return err
}

func (d *DB) FinalizeSubagentJob(ctx context.Context, job SubagentJob, status, preview, artifactID, errText, parentSummary string, parentPayload any) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx,
		`UPDATE subagent_jobs
		 SET status=?, result_preview=?, artifact_id=?, error_text=?, finished_at=?
		 WHERE id=? AND status=?`,
		status, preview, artifactID, errText, NowMS(), job.ID, SubagentStatusRunning)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	if strings.TrimSpace(parentSummary) != "" {
		if _, err := appendMessageTx(ctx, tx, job.ParentSessionKey, "assistant", parentSummary, parentPayload); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanSubagentJob(scanner interface{ Scan(dest ...any) error }) (SubagentJob, error) {
	var job SubagentJob
	err := scanner.Scan(
		&job.ID,
		&job.ParentSessionKey,
		&job.ChildSessionKey,
		&job.Channel,
		&job.ReplyTo,
		&job.Task,
		&job.Status,
		&job.ResultPreview,
		&job.ArtifactID,
		&job.ErrorText,
		&job.RequestedAt,
		&job.StartedAt,
		&job.FinishedAt,
		&job.Attempts,
		&job.MetadataJSON,
	)
	return job, err
}

func ensureSessionTx(ctx context.Context, tx *sql.Tx, key string) error {
	now := NowMS()
	_, err := tx.ExecContext(ctx,
		`INSERT INTO sessions(key, created_at, updated_at) VALUES(?,?,?)
		 ON CONFLICT(key) DO UPDATE SET updated_at=excluded.updated_at`,
		key, now, now)
	return err
}

func appendMessageTx(ctx context.Context, tx *sql.Tx, sessionKey, role, content string, payload any) (int64, error) {
	if err := ensureSessionTx(ctx, tx, sessionKey); err != nil {
		return 0, err
	}
	pb, _ := json.Marshal(payload)
	now := NowMS()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO messages(session_key, role, content, payload_json, created_at) VALUES(?,?,?,?,?)`,
		sessionKey, role, content, string(pb), now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.ExecContext(ctx, `UPDATE sessions SET updated_at=? WHERE key=?`, now, sessionKey); err != nil {
		return id, err
	}
	return id, nil
}

func normalizeMemorySession(sessionKey string) string {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return scope.GlobalMemoryScope
	}
	return sessionKey
}

// LinkSession links a physical session key to a logical scope key.
// If scopeKey is empty, the sessionKey itself is used.
func (d *DB) LinkSession(ctx context.Context, sessionKey, scopeKey string, meta map[string]any) error {
	if strings.TrimSpace(sessionKey) == "" {
		return fmt.Errorf("sessionKey required")
	}
	if strings.TrimSpace(scopeKey) == "" {
		scopeKey = sessionKey
	}
	mb, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal metadata: %w", err)
	}
	if mb == nil {
		mb = []byte("{}")
	}
	_, err = d.SQL.ExecContext(ctx,
		`INSERT INTO session_links(session_key, scope_key, linked_at, metadata_json) VALUES(?,?,?,?)
         ON CONFLICT(session_key) DO UPDATE SET scope_key=excluded.scope_key, linked_at=excluded.linked_at, metadata_json=excluded.metadata_json`,
		sessionKey, scopeKey, NowMS(), string(mb))
	return err
}

// ResolveScopeKey returns the logical scope key for a physical session key.
// If no link exists, it returns the session key itself.
func (d *DB) ResolveScopeKey(ctx context.Context, sessionKey string) (string, error) {
	row := d.SQL.QueryRowContext(ctx,
		`SELECT scope_key FROM session_links WHERE session_key=?`, sessionKey)
	var scopeKey string
	if err := row.Scan(&scopeKey); err != nil {
		if err == sql.ErrNoRows {
			return sessionKey, nil
		}
		return sessionKey, err
	}
	return scopeKey, nil
}

// ListScopeSessions returns all physical session keys linked to the given scope key.
func (d *DB) ListScopeSessions(ctx context.Context, scopeKey string) ([]string, error) {
	rows, err := d.SQL.QueryContext(ctx,
		`SELECT session_key FROM session_links WHERE scope_key=? ORDER BY linked_at ASC`, scopeKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var sk string
		if err := rows.Scan(&sk); err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// GetLastMessagesScoped reads history for all sessions linked under the same scope
// as sessionKey, ordered by message id ascending, up to limit messages.
func (d *DB) GetLastMessagesScoped(ctx context.Context, sessionKey string, limit int) ([]Message, error) {
	scopeKey, err := d.ResolveScopeKey(ctx, sessionKey)
	if err != nil {
		return d.GetLastMessages(ctx, sessionKey, limit)
	}
	// get all sessions in scope (including the session itself)
	linked, err := d.ListScopeSessions(ctx, scopeKey)
	if err != nil || len(linked) == 0 {
		return d.GetLastMessages(ctx, sessionKey, limit)
	}
	// build IN clause; always include the physical session key itself
	allKeys := linked
	found := false
	for _, k := range linked {
		if k == sessionKey {
			found = true
			break
		}
	}
	if !found {
		allKeys = append(allKeys, sessionKey)
	}
	// build placeholders
	placeholders := make([]string, len(allKeys))
	args := make([]any, len(allKeys)+1)
	for i, k := range allKeys {
		placeholders[i] = "?"
		args[i] = k
	}
	args[len(allKeys)] = limit
	q := `SELECT id, session_key, role, content, payload_json, created_at
          FROM messages WHERE session_key IN (` + strings.Join(placeholders, ",") + `)
          ORDER BY id DESC LIMIT ?`
	rows, err := d.SQL.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionKey, &m.Role, &m.Content, &m.PayloadJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// reverse to chronological
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	// align so first is user
	for len(out) > 0 && out[0].Role != "user" {
		out = out[1:]
	}
	return out, nil
}
````

## File: README.md
````markdown
# or3-intern (v1)

`or3-intern` is a Go rewrite of nanobot with SQLite persistence, hybrid long-term memory retrieval, external channel integrations, autonomous triggers, and a hardened tool runtime.

The README now stays focused on orientation and quick start. Detailed guides and references live under [`docs/`](docs/README.md).

## Quick start

1. Run guided setup:
   ```bash
   go run ./cmd/or3-intern init
   ```
2. Start an interactive local session:
   ```bash
   go run ./cmd/or3-intern chat
   ```
3. Or run enabled external channels and automation:
   ```bash
   go run ./cmd/or3-intern serve
   ```

The `init` command can store provider settings in `~/.or3-intern/config.json`, so you only need environment variables when you prefer them.

## Core features

- Shared agent runtime for CLI, service mode, channels, and autonomous jobs
- SQLite-backed history with hybrid memory retrieval and document indexing
- External channels for Telegram, Slack, Discord, Email, and a local WhatsApp bridge
- ClawHub/OpenClaw-compatible skills with trust and quarantine controls
- Webhook, file-watch, heartbeat, and cron-based automation
- Phase-based hardening, audit, secret store, profile, and network controls
- Optional MCP tool integrations over stdio, SSE, and streamable HTTP

## Commands

- `or3-intern init` guided first-run setup
- `or3-intern chat` interactive CLI
- `or3-intern serve` run enabled external channels and automation
- `or3-intern service` run the internal authenticated HTTP API for OR3 Net
- `or3-intern agent -m "hello"` run a one-shot turn
- `or3-intern doctor [--strict]` print hardening warnings for the current config
- `or3-intern secrets <set|delete|list>` manage encrypted secret refs stored in SQLite
- `or3-intern audit [verify]` inspect or verify the append-only audit chain
- `or3-intern skills ...` list, inspect, search, install, update, check, and remove skills
- `or3-intern scope <link|list|resolve>` link multiple session keys to a shared history scope
- `or3-intern migrate-jsonl /path/to/session.jsonl [session_key]`
- `or3-intern version`

See [CLI Reference](docs/cli-reference.md) for command details.

## Documentation

- [Documentation index](docs/README.md)
- [Getting started](docs/getting-started.md)
- [Configuration reference](docs/configuration-reference.md)
- [CLI reference](docs/cli-reference.md)
- [Agent runtime](docs/agent-runtime.md)
- [Memory and context](docs/memory-and-context.md)
- [Channel integrations](docs/channels.md)
- [Skills](docs/skills.md)
- [Triggers and automation](docs/triggers-and-automation.md)
- [Security and hardening](docs/security-and-hardening.md)
- [MCP tool integrations](docs/mcp-tool-integrations.md)
- [Internal service API reference](docs/api-reference.md)

## Operational notes

- Uses SQLite with WAL plus bounded connection pools for predictable low-RAM operation.
- History is fetched with bounded queries instead of full scans.
- Hybrid retrieval combines pinned context, vector similarity, and FTS keyword search.
- External channels are disabled by default until configured.
- `or3-intern doctor` is the fastest way to audit an installation before exposing channels, triggers, or the service API.

## Dependencies

This repo uses external Go modules (including SQLite drivers, `sqlite-vec`, and the cron parser). If you are building in an offline environment, vendor the modules ahead of time.
````

## File: internal/config/config.go
````go
package config

import (
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DBPath                 string `json:"dbPath"`
	ArtifactsDir           string `json:"artifactsDir"`
	WorkspaceDir           string `json:"workspaceDir"`
	AllowedDir             string `json:"allowedDir"`
	DefaultSessionKey      string `json:"defaultSessionKey"`
	SoulFile               string `json:"soulFile"`
	AgentsFile             string `json:"agentsFile"`
	ToolsFile              string `json:"toolsFile"`
	BootstrapMaxChars      int    `json:"bootstrapMaxChars"`
	BootstrapTotalMaxChars int    `json:"bootstrapTotalMaxChars"`
	SessionCache           int    `json:"sessionCacheLimit"`
	HistoryMax             int    `json:"historyMaxMessages"`
	MaxToolBytes           int    `json:"maxToolBytes"`
	MaxMediaBytes          int    `json:"maxMediaBytes"`
	MaxToolLoops           int    `json:"maxToolLoops"`
	MemoryRetrieve         int    `json:"memoryRetrieveLimit"`
	VectorK                int    `json:"vectorSearchK"`
	FTSK                   int    `json:"ftsSearchK"`
	VectorScanLimit        int    `json:"vectorScanLimit"`
	WorkerCount            int    `json:"workerCount"`

	ConsolidationEnabled             bool            `json:"consolidationEnabled"`
	ConsolidationWindowSize          int             `json:"consolidationWindowSize"`
	ConsolidationMaxMessages         int             `json:"consolidationMaxMessages"`
	ConsolidationMaxInputChars       int             `json:"consolidationMaxInputChars"`
	ConsolidationAsyncTimeoutSeconds int             `json:"consolidationAsyncTimeoutSeconds"`
	Subagents                        SubagentsConfig `json:"subagents"`

	IdentityFile string         `json:"identityFile"`
	MemoryFile   string         `json:"memoryFile"`
	DocIndex     DocIndexConfig `json:"docIndex"`
	Skills       SkillsConfig   `json:"skills"`
	Triggers     TriggerConfig  `json:"triggers"`
	Session      SessionConfig  `json:"session"`
	Security     SecurityConfig `json:"security"`

	Provider  ProviderConfig  `json:"provider"`
	Tools     ToolsConfig     `json:"tools"`
	Hardening HardeningConfig `json:"hardening"`
	Cron      CronConfig      `json:"cron"`
	Service   ServiceConfig   `json:"service"`
	Heartbeat HeartbeatConfig `json:"heartbeat"`
	Channels  ChannelsConfig  `json:"channels"`
}

type HardeningConfig struct {
	GuardedTools        bool                 `json:"guardedTools"`
	PrivilegedTools     bool                 `json:"privilegedTools"`
	EnableExecShell     bool                 `json:"enableExecShell"`
	ExecAllowedPrograms []string             `json:"execAllowedPrograms"`
	ChildEnvAllowlist   []string             `json:"childEnvAllowlist"`
	IsolateChannelPeers bool                 `json:"isolateChannelPeers"`
	Sandbox             SandboxConfig        `json:"sandbox"`
	Quotas              HardeningQuotaConfig `json:"quotas"`
}

type SandboxConfig struct {
	Enabled        bool     `json:"enabled"`
	BubblewrapPath string   `json:"bubblewrapPath"`
	AllowNetwork   bool     `json:"allowNetwork"`
	WritablePaths  []string `json:"writablePaths"`
}

type HardeningQuotaConfig struct {
	Enabled          bool `json:"enabled"`
	MaxToolCalls     int  `json:"maxToolCalls"`
	MaxExecCalls     int  `json:"maxExecCalls"`
	MaxWebCalls      int  `json:"maxWebCalls"`
	MaxSubagentCalls int  `json:"maxSubagentCalls"`
}

type ProviderConfig struct {
	APIBase        string  `json:"apiBase"`
	APIKey         string  `json:"apiKey"`
	Model          string  `json:"model"`
	Temperature    float64 `json:"temperature"`
	EmbedModel     string  `json:"embedModel"`
	EnableVision   bool    `json:"enableVision"`
	TimeoutSeconds int     `json:"timeoutSeconds"`
}

type ToolsConfig struct {
	BraveAPIKey         string                     `json:"braveApiKey"`
	WebProxy            string                     `json:"webProxy"`
	ExecTimeoutSeconds  int                        `json:"execTimeoutSeconds"`
	RestrictToWorkspace bool                       `json:"restrictToWorkspace"`
	PathAppend          string                     `json:"pathAppend"`
	MCPServers          map[string]MCPServerConfig `json:"mcpServers"`
}

type CronConfig struct {
	Enabled   bool   `json:"enabled"`
	StorePath string `json:"storePath"`
}

const DefaultHeartbeatSessionKey = "heartbeat:default"

const (
	DefaultMCPTransport             = "stdio"
	DefaultMCPConnectTimeoutSeconds = 10
	DefaultMCPToolTimeoutSeconds    = 30
)

type MCPServerConfig struct {
	Enabled               bool              `json:"enabled"`
	Transport             string            `json:"transport"`
	Command               string            `json:"command"`
	Args                  []string          `json:"args"`
	Env                   map[string]string `json:"env"`
	ChildEnvAllowlist     []string          `json:"childEnvAllowlist"`
	URL                   string            `json:"url"`
	Headers               map[string]string `json:"headers"`
	ToolTimeoutSeconds    int               `json:"toolTimeoutSeconds"`
	ConnectTimeoutSeconds int               `json:"connectTimeoutSeconds"`
	AllowInsecureHTTP     bool              `json:"allowInsecureHttp"`
}

type HeartbeatConfig struct {
	Enabled         bool   `json:"enabled"`
	IntervalMinutes int    `json:"intervalMinutes"`
	TasksFile       string `json:"tasksFile"`
	SessionKey      string `json:"sessionKey"`
}

type ServiceConfig struct {
	Enabled bool   `json:"enabled"`
	Listen  string `json:"listen"`
	Secret  string `json:"secret"`
}

type SubagentsConfig struct {
	Enabled            bool `json:"enabled"`
	MaxConcurrent      int  `json:"maxConcurrent"`
	MaxQueued          int  `json:"maxQueued"`
	TaskTimeoutSeconds int  `json:"taskTimeoutSeconds"`
}

type TelegramChannelConfig struct {
	Enabled        bool     `json:"enabled"`
	OpenAccess     bool     `json:"openAccess"`
	Token          string   `json:"token"`
	APIBase        string   `json:"apiBase"`
	PollSeconds    int      `json:"pollSeconds"`
	DefaultChatID  string   `json:"defaultChatId"`
	AllowedChatIDs []string `json:"allowedChatIds"`
}

type SlackChannelConfig struct {
	Enabled          bool     `json:"enabled"`
	OpenAccess       bool     `json:"openAccess"`
	AppToken         string   `json:"appToken"`
	BotToken         string   `json:"botToken"`
	APIBase          string   `json:"apiBase"`
	SocketModeURL    string   `json:"socketModeUrl"`
	DefaultChannelID string   `json:"defaultChannelId"`
	AllowedUserIDs   []string `json:"allowedUserIds"`
	RequireMention   bool     `json:"requireMention"`
}

type DiscordChannelConfig struct {
	Enabled          bool     `json:"enabled"`
	OpenAccess       bool     `json:"openAccess"`
	Token            string   `json:"token"`
	APIBase          string   `json:"apiBase"`
	GatewayURL       string   `json:"gatewayUrl"`
	DefaultChannelID string   `json:"defaultChannelId"`
	AllowedUserIDs   []string `json:"allowedUserIds"`
	RequireMention   bool     `json:"requireMention"`
}

type WhatsAppBridgeConfig struct {
	Enabled     bool     `json:"enabled"`
	OpenAccess  bool     `json:"openAccess"`
	BridgeURL   string   `json:"bridgeUrl"`
	BridgeToken string   `json:"bridgeToken"`
	DefaultTo   string   `json:"defaultTo"`
	AllowedFrom []string `json:"allowedFrom"`
}

type EmailChannelConfig struct {
	Enabled             bool     `json:"enabled"`
	OpenAccess          bool     `json:"openAccess"`
	ConsentGranted      bool     `json:"consentGranted"`
	AllowedSenders      []string `json:"allowedSenders"`
	DefaultTo           string   `json:"defaultTo"`
	AutoReplyEnabled    bool     `json:"autoReplyEnabled"`
	PollIntervalSeconds int      `json:"pollIntervalSeconds"`
	MarkSeen            bool     `json:"markSeen"`
	MaxBodyChars        int      `json:"maxBodyChars"`
	SubjectPrefix       string   `json:"subjectPrefix"`
	FromAddress         string   `json:"fromAddress"`
	IMAPMailbox         string   `json:"imapMailbox"`
	IMAPHost            string   `json:"imapHost"`
	IMAPPort            int      `json:"imapPort"`
	IMAPUseSSL          bool     `json:"imapUseSSL"`
	IMAPUsername        string   `json:"imapUsername"`
	IMAPPassword        string   `json:"imapPassword"`
	SMTPHost            string   `json:"smtpHost"`
	SMTPPort            int      `json:"smtpPort"`
	SMTPUseTLS          bool     `json:"smtpUseTLS"`
	SMTPUseSSL          bool     `json:"smtpUseSSL"`
	SMTPUsername        string   `json:"smtpUsername"`
	SMTPPassword        string   `json:"smtpPassword"`
}

type ChannelsConfig struct {
	Telegram TelegramChannelConfig `json:"telegram"`
	Slack    SlackChannelConfig    `json:"slack"`
	Discord  DiscordChannelConfig  `json:"discord"`
	WhatsApp WhatsAppBridgeConfig  `json:"whatsApp"`
	Email    EmailChannelConfig    `json:"email"`
}

type DocIndexConfig struct {
	Enabled        bool     `json:"enabled"`
	Roots          []string `json:"roots"`
	MaxFiles       int      `json:"maxFiles"`
	MaxFileBytes   int      `json:"maxFileBytes"`
	MaxChunks      int      `json:"maxChunks"`
	EmbedMaxBytes  int      `json:"embedMaxBytes"`
	RefreshSeconds int      `json:"refreshSeconds"`
	RetrieveLimit  int      `json:"retrieveLimit"`
}

type SkillsConfig struct {
	EnableExec    bool                        `json:"enableExec"`
	MaxRunSeconds int                         `json:"maxRunSeconds"`
	ManagedDir    string                      `json:"managedDir"`
	Policy        SkillPolicyConfig           `json:"policy"`
	Load          SkillsLoadConfig            `json:"load"`
	Entries       map[string]SkillEntryConfig `json:"entries"`
	ClawHub       ClawHubConfig               `json:"clawHub"`
}

type SkillPolicyConfig struct {
	QuarantineByDefault bool     `json:"quarantineByDefault"`
	Approved            []string `json:"approved"`
	TrustedOwners       []string `json:"trustedOwners"`
	BlockedOwners       []string `json:"blockedOwners"`
	TrustedRegistries   []string `json:"trustedRegistries"`
}

type SkillsLoadConfig struct {
	ExtraDirs       []string `json:"extraDirs"`
	Watch           bool     `json:"watch"`
	WatchDebounceMS int      `json:"watchDebounceMs"`
}

type SkillEntryConfig struct {
	Enabled *bool             `json:"enabled,omitempty"`
	APIKey  string            `json:"apiKey"`
	Env     map[string]string `json:"env"`
	Config  map[string]any    `json:"config"`
}

type ClawHubConfig struct {
	SiteURL     string `json:"siteUrl"`
	RegistryURL string `json:"registryUrl"`
	InstallDir  string `json:"installDir"`
}

type WebhookConfig struct {
	Enabled   bool   `json:"enabled"`
	Addr      string `json:"addr"`
	Secret    string `json:"secret"`
	MaxBodyKB int    `json:"maxBodyKB"`
}

type FileWatchConfig struct {
	Enabled         bool     `json:"enabled"`
	Paths           []string `json:"paths"`
	PollSeconds     int      `json:"pollSeconds"`
	DebounceSeconds int      `json:"debounceSeconds"`
}

type TriggerConfig struct {
	Webhook   WebhookConfig   `json:"webhook"`
	FileWatch FileWatchConfig `json:"fileWatch"`
}

type SessionConfig struct {
	DirectMessagesShareDefault bool                  `json:"directMessagesShareDefault"`
	IdentityLinks              []SessionIdentityLink `json:"identityLinks"`
}

type SessionIdentityLink struct {
	Canonical string   `json:"canonical"`
	Peers     []string `json:"peers"`
}

type SecurityConfig struct {
	SecretStore SecretStoreConfig    `json:"secretStore"`
	Audit       AuditConfig          `json:"audit"`
	Profiles    AccessProfilesConfig `json:"profiles"`
	Network     NetworkPolicyConfig  `json:"network"`
}

type SecretStoreConfig struct {
	Enabled  bool   `json:"enabled"`
	Required bool   `json:"required"`
	KeyFile  string `json:"keyFile"`
}

type AuditConfig struct {
	Enabled       bool   `json:"enabled"`
	Strict        bool   `json:"strict"`
	KeyFile       string `json:"keyFile"`
	VerifyOnStart bool   `json:"verifyOnStart"`
}

type AccessProfilesConfig struct {
	Enabled  bool                           `json:"enabled"`
	Default  string                         `json:"default"`
	Channels map[string]string              `json:"channels"`
	Triggers map[string]string              `json:"triggers"`
	Profiles map[string]AccessProfileConfig `json:"profiles"`
}

type AccessProfileConfig struct {
	MaxCapability  string   `json:"maxCapability"`
	AllowedTools   []string `json:"allowedTools"`
	AllowedHosts   []string `json:"allowedHosts"`
	WritablePaths  []string `json:"writablePaths"`
	AllowSubagents bool     `json:"allowSubagents"`
}

type NetworkPolicyConfig struct {
	Enabled       bool     `json:"enabled"`
	DefaultDeny   bool     `json:"defaultDeny"`
	AllowedHosts  []string `json:"allowedHosts"`
	AllowLoopback bool     `json:"allowLoopback"`
	AllowPrivate  bool     `json:"allowPrivate"`
}

func Default() Config {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".or3-intern")
	return Config{
		DBPath:                           filepath.Join(root, "or3-intern.sqlite"),
		ArtifactsDir:                     filepath.Join(root, "artifacts"),
		WorkspaceDir:                     "",
		AllowedDir:                       "",
		DefaultSessionKey:                "cli:default",
		SoulFile:                         filepath.Join(root, "SOUL.md"),
		AgentsFile:                       filepath.Join(root, "AGENTS.md"),
		ToolsFile:                        filepath.Join(root, "TOOLS.md"),
		IdentityFile:                     filepath.Join(root, "IDENTITY.md"),
		MemoryFile:                       filepath.Join(root, "MEMORY.md"),
		BootstrapMaxChars:                20000,
		BootstrapTotalMaxChars:           150000,
		SessionCache:                     64,
		HistoryMax:                       40,
		MaxToolBytes:                     24 * 1024,
		MaxMediaBytes:                    20 * 1024 * 1024,
		MaxToolLoops:                     6,
		MemoryRetrieve:                   8,
		VectorK:                          8,
		FTSK:                             8,
		VectorScanLimit:                  2000,
		WorkerCount:                      4,
		ConsolidationEnabled:             true,
		ConsolidationWindowSize:          10,
		ConsolidationMaxMessages:         50,
		ConsolidationMaxInputChars:       12000,
		ConsolidationAsyncTimeoutSeconds: 30,
		Subagents: SubagentsConfig{
			Enabled:            false,
			MaxConcurrent:      1,
			MaxQueued:          32,
			TaskTimeoutSeconds: 300,
		},
		DocIndex: DocIndexConfig{
			Enabled:        false,
			MaxFiles:       100,
			MaxFileBytes:   64 * 1024,
			MaxChunks:      500,
			EmbedMaxBytes:  8 * 1024,
			RefreshSeconds: 300,
			RetrieveLimit:  5,
		},
		Skills: SkillsConfig{
			EnableExec:    false,
			MaxRunSeconds: 30,
			ManagedDir:    filepath.Join(root, "skills"),
			Policy: SkillPolicyConfig{
				QuarantineByDefault: true,
				Approved:            []string{},
				TrustedOwners:       []string{},
				BlockedOwners:       []string{},
				TrustedRegistries:   []string{},
			},
			Load: SkillsLoadConfig{
				Watch:           false,
				WatchDebounceMS: 250,
			},
			Entries: map[string]SkillEntryConfig{},
			ClawHub: ClawHubConfig{
				SiteURL:     "https://clawhub.ai",
				RegistryURL: "https://clawhub.ai",
				InstallDir:  "skills",
			},
		},
		Triggers: TriggerConfig{
			Webhook: WebhookConfig{
				Enabled:   false,
				Addr:      "127.0.0.1:8765",
				MaxBodyKB: 64,
			},
			FileWatch: FileWatchConfig{
				Enabled:         false,
				PollSeconds:     5,
				DebounceSeconds: 2,
			},
		},
		Session: SessionConfig{
			DirectMessagesShareDefault: false,
			IdentityLinks:              []SessionIdentityLink{},
		},
		Security: SecurityConfig{
			SecretStore: SecretStoreConfig{
				Enabled:  false,
				Required: false,
				KeyFile:  filepath.Join(root, "master.key"),
			},
			Audit: AuditConfig{
				Enabled:       false,
				Strict:        false,
				KeyFile:       filepath.Join(root, "audit.key"),
				VerifyOnStart: false,
			},
			Profiles: AccessProfilesConfig{
				Enabled:  false,
				Default:  "",
				Channels: map[string]string{},
				Triggers: map[string]string{},
				Profiles: map[string]AccessProfileConfig{},
			},
			Network: NetworkPolicyConfig{
				Enabled:       false,
				DefaultDeny:   false,
				AllowedHosts:  []string{},
				AllowLoopback: false,
				AllowPrivate:  false,
			},
		},
		Provider: ProviderConfig{
			APIBase:        "https://api.openai.com/v1",
			APIKey:         os.Getenv("OPENAI_API_KEY"),
			Model:          "gpt-4.1-mini",
			Temperature:    0,
			EmbedModel:     "text-embedding-3-small",
			TimeoutSeconds: 60,
		},
		Tools: ToolsConfig{
			BraveAPIKey:         os.Getenv("BRAVE_API_KEY"),
			WebProxy:            "",
			ExecTimeoutSeconds:  60,
			RestrictToWorkspace: true,
			PathAppend:          "",
			MCPServers:          map[string]MCPServerConfig{},
		},
		Hardening: HardeningConfig{
			GuardedTools:        false,
			PrivilegedTools:     false,
			EnableExecShell:     false,
			ExecAllowedPrograms: []string{"cat", "echo", "find", "git", "grep", "head", "ls", "pwd", "sed", "tail"},
			ChildEnvAllowlist:   []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP"},
			IsolateChannelPeers: true,
			Sandbox: SandboxConfig{
				Enabled:        false,
				BubblewrapPath: "bwrap",
				AllowNetwork:   false,
				WritablePaths:  []string{},
			},
			Quotas: HardeningQuotaConfig{
				Enabled:          true,
				MaxToolCalls:     16,
				MaxExecCalls:     2,
				MaxWebCalls:      4,
				MaxSubagentCalls: 2,
			},
		},
		Cron: CronConfig{Enabled: true, StorePath: filepath.Join(root, "cron.json")},
		Service: ServiceConfig{
			Enabled: false,
			Listen:  "127.0.0.1:9100",
			Secret:  "",
		},
		Heartbeat: HeartbeatConfig{
			Enabled:         false,
			IntervalMinutes: 30,
			TasksFile:       filepath.Join(root, "HEARTBEAT.md"),
			SessionKey:      DefaultHeartbeatSessionKey,
		},
		Channels: ChannelsConfig{
			Telegram: TelegramChannelConfig{Enabled: false, APIBase: "https://api.telegram.org", PollSeconds: 2},
			Slack:    SlackChannelConfig{Enabled: false, APIBase: "https://slack.com/api", RequireMention: true},
			Discord:  DiscordChannelConfig{Enabled: false, APIBase: "https://discord.com/api/v10", RequireMention: true},
			WhatsApp: WhatsAppBridgeConfig{Enabled: false, BridgeURL: "ws://127.0.0.1:3001/ws"},
			Email: EmailChannelConfig{
				Enabled:             false,
				ConsentGranted:      false,
				AutoReplyEnabled:    false,
				PollIntervalSeconds: 30,
				MarkSeen:            true,
				MaxBodyChars:        4000,
				SubjectPrefix:       "Re: ",
				IMAPMailbox:         "INBOX",
				IMAPPort:            993,
				IMAPUseSSL:          true,
				SMTPPort:            587,
				SMTPUseTLS:          true,
				SMTPUseSSL:          false,
			},
		},
	}
}

func DefaultPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	if v := os.Getenv("OR3_DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("OR3_ARTIFACTS_DIR"); v != "" {
		cfg.ArtifactsDir = v
	}
	if v := os.Getenv("OR3_API_BASE"); v != "" {
		cfg.Provider.APIBase = v
	}
	if v := os.Getenv("OR3_API_KEY"); v != "" {
		cfg.Provider.APIKey = v
	}
	if v := os.Getenv("OR3_MODEL"); v != "" {
		cfg.Provider.Model = v
	}
	if v := os.Getenv("OR3_EMBED_MODEL"); v != "" {
		cfg.Provider.EmbedModel = v
	}
	if v := os.Getenv("OR3_TELEGRAM_TOKEN"); v != "" {
		cfg.Channels.Telegram.Token = v
	}
	if v := os.Getenv("OR3_SLACK_APP_TOKEN"); v != "" {
		cfg.Channels.Slack.AppToken = v
	}
	if v := os.Getenv("OR3_SLACK_BOT_TOKEN"); v != "" {
		cfg.Channels.Slack.BotToken = v
	}
	if v := os.Getenv("OR3_DISCORD_TOKEN"); v != "" {
		cfg.Channels.Discord.Token = v
	}
	if v := os.Getenv("OR3_WHATSAPP_BRIDGE_URL"); v != "" {
		cfg.Channels.WhatsApp.BridgeURL = v
	}
	if v := os.Getenv("OR3_WHATSAPP_BRIDGE_TOKEN"); v != "" {
		cfg.Channels.WhatsApp.BridgeToken = v
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_HOST"); v != "" {
		cfg.Channels.Email.IMAPHost = v
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_PORT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Channels.Email.IMAPPort = parsed
		}
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_USERNAME"); v != "" {
		cfg.Channels.Email.IMAPUsername = v
	}
	if v := os.Getenv("OR3_EMAIL_IMAP_PASSWORD"); v != "" {
		cfg.Channels.Email.IMAPPassword = v
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_HOST"); v != "" {
		cfg.Channels.Email.SMTPHost = v
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_PORT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Channels.Email.SMTPPort = parsed
		}
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_USERNAME"); v != "" {
		cfg.Channels.Email.SMTPUsername = v
	}
	if v := os.Getenv("OR3_EMAIL_SMTP_PASSWORD"); v != "" {
		cfg.Channels.Email.SMTPPassword = v
	}
	if v := os.Getenv("OR3_EMAIL_FROM_ADDRESS"); v != "" {
		cfg.Channels.Email.FromAddress = v
	}
	if v := os.Getenv("OR3_SUBAGENTS_ENABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.Subagents.Enabled = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_MAX_CONCURRENT"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.MaxConcurrent = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_MAX_QUEUED"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.MaxQueued = parsed
		}
	}
	if v := os.Getenv("OR3_SUBAGENTS_TASK_TIMEOUT_SECONDS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			cfg.Subagents.TaskTimeoutSeconds = parsed
		}
	}
	if v := os.Getenv("OR3_SERVICE_ENABLED"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			cfg.Service.Enabled = parsed
		}
	}
	if v := os.Getenv("OR3_SERVICE_LISTEN"); v != "" {
		cfg.Service.Listen = v
	}
	if v := os.Getenv("OR3_SERVICE_SECRET"); v != "" {
		cfg.Service.Secret = v
	}
}

func Save(path string, cfg Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, mustJSON(cfg), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		path = DefaultPath()
	}

	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if err := Save(path, cfg); err != nil {
				return cfg, err
			}
		} else {
			return cfg, err
		}
	} else {
		if err := json.Unmarshal(b, &cfg); err != nil {
			return cfg, err
		}
	}
	ApplyEnvOverrides(&cfg)

	if cfg.Provider.TimeoutSeconds <= 0 {
		cfg.Provider.TimeoutSeconds = int((60 * time.Second).Seconds())
	}
	if cfg.DefaultSessionKey == "" {
		cfg.DefaultSessionKey = "cli:default"
	}
	if cfg.BootstrapMaxChars <= 0 {
		cfg.BootstrapMaxChars = 20000
	}
	if cfg.BootstrapTotalMaxChars <= 0 {
		cfg.BootstrapTotalMaxChars = 150000
	}
	if cfg.HistoryMax <= 0 {
		cfg.HistoryMax = 40
	}
	if cfg.MaxToolBytes <= 0 {
		cfg.MaxToolBytes = 24 * 1024
	}
	if cfg.MaxMediaBytes <= 0 {
		cfg.MaxMediaBytes = 20 * 1024 * 1024
	}
	if cfg.MaxToolLoops <= 0 {
		cfg.MaxToolLoops = 6
	}
	if cfg.VectorScanLimit <= 0 {
		cfg.VectorScanLimit = 2000
	}
	if cfg.WorkerCount <= 0 {
		cfg.WorkerCount = 4
	}
	if cfg.ConsolidationWindowSize <= 0 {
		cfg.ConsolidationWindowSize = 10
	}
	if cfg.ConsolidationMaxMessages <= 0 {
		cfg.ConsolidationMaxMessages = 50
	}
	if cfg.ConsolidationMaxInputChars <= 0 {
		cfg.ConsolidationMaxInputChars = 12000
	}
	if cfg.ConsolidationAsyncTimeoutSeconds <= 0 {
		cfg.ConsolidationAsyncTimeoutSeconds = 30
	}
	if cfg.Subagents.MaxConcurrent <= 0 {
		cfg.Subagents.MaxConcurrent = 1
	}
	if cfg.Subagents.MaxQueued <= 0 {
		cfg.Subagents.MaxQueued = 32
	}
	if cfg.Subagents.TaskTimeoutSeconds <= 0 {
		cfg.Subagents.TaskTimeoutSeconds = 300
	}
	if strings.TrimSpace(cfg.Service.Listen) == "" {
		cfg.Service.Listen = Default().Service.Listen
	}
	if cfg.Channels.Telegram.APIBase == "" {
		cfg.Channels.Telegram.APIBase = "https://api.telegram.org"
	}
	if cfg.Channels.Telegram.PollSeconds <= 0 {
		cfg.Channels.Telegram.PollSeconds = 2
	}
	if cfg.Channels.Slack.APIBase == "" {
		cfg.Channels.Slack.APIBase = "https://slack.com/api"
	}
	if cfg.Channels.Discord.APIBase == "" {
		cfg.Channels.Discord.APIBase = "https://discord.com/api/v10"
	}
	if cfg.Channels.WhatsApp.BridgeURL == "" {
		cfg.Channels.WhatsApp.BridgeURL = "ws://127.0.0.1:3001/ws"
	}
	if cfg.Channels.Email.PollIntervalSeconds <= 0 {
		cfg.Channels.Email.PollIntervalSeconds = 30
	}
	if cfg.Channels.Email.MaxBodyChars <= 0 {
		cfg.Channels.Email.MaxBodyChars = 4000
	}
	if strings.TrimSpace(cfg.Channels.Email.SubjectPrefix) == "" {
		cfg.Channels.Email.SubjectPrefix = "Re: "
	}
	if strings.TrimSpace(cfg.Channels.Email.IMAPMailbox) == "" {
		cfg.Channels.Email.IMAPMailbox = "INBOX"
	}
	if cfg.Channels.Email.IMAPPort <= 0 {
		cfg.Channels.Email.IMAPPort = 993
	}
	if cfg.Channels.Email.SMTPPort <= 0 {
		cfg.Channels.Email.SMTPPort = 587
	}
	if cfg.DocIndex.MaxFiles <= 0 {
		cfg.DocIndex.MaxFiles = 100
	}
	if cfg.DocIndex.MaxFileBytes <= 0 {
		cfg.DocIndex.MaxFileBytes = 64 * 1024
	}
	if cfg.DocIndex.MaxChunks <= 0 {
		cfg.DocIndex.MaxChunks = 500
	}
	if cfg.DocIndex.EmbedMaxBytes <= 0 {
		cfg.DocIndex.EmbedMaxBytes = 8 * 1024
	}
	if cfg.DocIndex.RefreshSeconds <= 0 {
		cfg.DocIndex.RefreshSeconds = 300
	}
	if cfg.DocIndex.RetrieveLimit <= 0 {
		cfg.DocIndex.RetrieveLimit = 5
	}
	if cfg.Skills.MaxRunSeconds <= 0 {
		cfg.Skills.MaxRunSeconds = 30
	}
	if strings.TrimSpace(cfg.Skills.ManagedDir) == "" {
		cfg.Skills.ManagedDir = filepath.Join(filepath.Dir(DefaultPath()), "skills")
	}
	if cfg.Skills.Load.WatchDebounceMS <= 0 {
		cfg.Skills.Load.WatchDebounceMS = 250
	}
	if cfg.Skills.Policy.Approved == nil {
		cfg.Skills.Policy.Approved = []string{}
	}
	if cfg.Skills.Policy.TrustedOwners == nil {
		cfg.Skills.Policy.TrustedOwners = []string{}
	}
	if cfg.Skills.Policy.BlockedOwners == nil {
		cfg.Skills.Policy.BlockedOwners = []string{}
	}
	if cfg.Skills.Policy.TrustedRegistries == nil {
		cfg.Skills.Policy.TrustedRegistries = []string{}
	}
	cfg.Skills.Policy.Approved = compactStrings(cfg.Skills.Policy.Approved)
	cfg.Skills.Policy.TrustedOwners = compactStrings(cfg.Skills.Policy.TrustedOwners)
	cfg.Skills.Policy.BlockedOwners = compactStrings(cfg.Skills.Policy.BlockedOwners)
	cfg.Skills.Policy.TrustedRegistries = compactStrings(cfg.Skills.Policy.TrustedRegistries)
	if cfg.Skills.Entries == nil {
		cfg.Skills.Entries = map[string]SkillEntryConfig{}
	}
	if cfg.Tools.MCPServers == nil {
		cfg.Tools.MCPServers = map[string]MCPServerConfig{}
	}
	if len(cfg.Hardening.ExecAllowedPrograms) == 0 {
		cfg.Hardening.ExecAllowedPrograms = append([]string{}, Default().Hardening.ExecAllowedPrograms...)
	}
	if len(cfg.Hardening.ChildEnvAllowlist) == 0 {
		cfg.Hardening.ChildEnvAllowlist = append([]string{}, Default().Hardening.ChildEnvAllowlist...)
	}
	if strings.TrimSpace(cfg.Hardening.Sandbox.BubblewrapPath) == "" {
		cfg.Hardening.Sandbox.BubblewrapPath = Default().Hardening.Sandbox.BubblewrapPath
	}
	if cfg.Hardening.Sandbox.WritablePaths == nil {
		cfg.Hardening.Sandbox.WritablePaths = []string{}
	}
	if cfg.Hardening.Quotas.MaxToolCalls <= 0 {
		cfg.Hardening.Quotas.MaxToolCalls = Default().Hardening.Quotas.MaxToolCalls
	}
	if cfg.Hardening.Quotas.MaxExecCalls <= 0 {
		cfg.Hardening.Quotas.MaxExecCalls = Default().Hardening.Quotas.MaxExecCalls
	}
	if cfg.Hardening.Quotas.MaxWebCalls <= 0 {
		cfg.Hardening.Quotas.MaxWebCalls = Default().Hardening.Quotas.MaxWebCalls
	}
	if cfg.Hardening.Quotas.MaxSubagentCalls <= 0 {
		cfg.Hardening.Quotas.MaxSubagentCalls = Default().Hardening.Quotas.MaxSubagentCalls
	}
	for name, server := range cfg.Tools.MCPServers {
		server.Transport = strings.ToLower(strings.TrimSpace(server.Transport))
		if server.Transport == "" {
			server.Transport = DefaultMCPTransport
		}
		server.Command = strings.TrimSpace(server.Command)
		server.URL = strings.TrimSpace(server.URL)
		if server.Env == nil {
			server.Env = map[string]string{}
		}
		if len(server.ChildEnvAllowlist) == 0 {
			server.ChildEnvAllowlist = append([]string{}, cfg.Hardening.ChildEnvAllowlist...)
		}
		if server.Headers == nil {
			server.Headers = map[string]string{}
		}
		if server.ToolTimeoutSeconds <= 0 {
			server.ToolTimeoutSeconds = DefaultMCPToolTimeoutSeconds
		}
		if server.ConnectTimeoutSeconds <= 0 {
			server.ConnectTimeoutSeconds = DefaultMCPConnectTimeoutSeconds
		}
		cfg.Tools.MCPServers[name] = server
	}
	if strings.TrimSpace(cfg.Skills.ClawHub.SiteURL) == "" {
		cfg.Skills.ClawHub.SiteURL = "https://clawhub.ai"
	}
	if strings.TrimSpace(cfg.Skills.ClawHub.RegistryURL) == "" {
		cfg.Skills.ClawHub.RegistryURL = "https://clawhub.ai"
	}
	if strings.TrimSpace(cfg.Skills.ClawHub.InstallDir) == "" {
		cfg.Skills.ClawHub.InstallDir = "skills"
	}
	if cfg.Triggers.Webhook.Addr == "" {
		cfg.Triggers.Webhook.Addr = "127.0.0.1:8765"
	}
	if cfg.Triggers.Webhook.MaxBodyKB <= 0 {
		cfg.Triggers.Webhook.MaxBodyKB = 64
	}
	if cfg.Triggers.FileWatch.PollSeconds <= 0 {
		cfg.Triggers.FileWatch.PollSeconds = 5
	}
	if cfg.Triggers.FileWatch.DebounceSeconds <= 0 {
		cfg.Triggers.FileWatch.DebounceSeconds = 2
	}
	if cfg.Heartbeat.IntervalMinutes <= 0 {
		cfg.Heartbeat.IntervalMinutes = 30
	}
	if cfg.Heartbeat.IntervalMinutes < 1 {
		cfg.Heartbeat.IntervalMinutes = 1
	}
	if strings.TrimSpace(cfg.Heartbeat.SessionKey) == "" {
		cfg.Heartbeat.SessionKey = DefaultHeartbeatSessionKey
	}
	if cfg.Session.IdentityLinks == nil {
		cfg.Session.IdentityLinks = []SessionIdentityLink{}
	}
	if strings.TrimSpace(cfg.Security.SecretStore.KeyFile) == "" {
		cfg.Security.SecretStore.KeyFile = Default().Security.SecretStore.KeyFile
	}
	if strings.TrimSpace(cfg.Security.Audit.KeyFile) == "" {
		cfg.Security.Audit.KeyFile = Default().Security.Audit.KeyFile
	}
	if cfg.Security.Profiles.Channels == nil {
		cfg.Security.Profiles.Channels = map[string]string{}
	}
	if cfg.Security.Profiles.Triggers == nil {
		cfg.Security.Profiles.Triggers = map[string]string{}
	}
	if cfg.Security.Profiles.Profiles == nil {
		cfg.Security.Profiles.Profiles = map[string]AccessProfileConfig{}
	}
	if cfg.Security.Network.AllowedHosts == nil {
		cfg.Security.Network.AllowedHosts = []string{}
	}
	for name, profile := range cfg.Security.Profiles.Profiles {
		profile.MaxCapability = strings.ToLower(strings.TrimSpace(profile.MaxCapability))
		profile.AllowedTools = compactStrings(profile.AllowedTools)
		profile.AllowedHosts = compactStrings(profile.AllowedHosts)
		profile.WritablePaths = compactStrings(profile.WritablePaths)
		cfg.Security.Profiles.Profiles[name] = profile
	}
	if err := validateMCPServers(cfg.Tools.MCPServers); err != nil {
		return cfg, err
	}
	if err := validateChannelAccess(cfg); err != nil {
		return cfg, err
	}
	if err := validateAccessProfiles(cfg.Security.Profiles); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func mustJSON(v any) []byte {
	b, _ := json.MarshalIndent(v, "", "  ")
	return b
}

func validateChannelAccess(cfg Config) error {
	if cfg.Channels.Telegram.Enabled && !cfg.Channels.Telegram.OpenAccess && !hasNonEmpty(cfg.Channels.Telegram.AllowedChatIDs) {
		return errors.New("telegram enabled: set channels.telegram.allowedChatIds or channels.telegram.openAccess=true")
	}
	if cfg.Channels.Slack.Enabled && !cfg.Channels.Slack.OpenAccess && !hasNonEmpty(cfg.Channels.Slack.AllowedUserIDs) {
		return errors.New("slack enabled: set channels.slack.allowedUserIds or channels.slack.openAccess=true")
	}
	if cfg.Channels.Discord.Enabled && !cfg.Channels.Discord.OpenAccess && !hasNonEmpty(cfg.Channels.Discord.AllowedUserIDs) {
		return errors.New("discord enabled: set channels.discord.allowedUserIds or channels.discord.openAccess=true")
	}
	if cfg.Channels.WhatsApp.Enabled && !cfg.Channels.WhatsApp.OpenAccess && !hasNonEmpty(cfg.Channels.WhatsApp.AllowedFrom) {
		return errors.New("whatsApp enabled: set channels.whatsApp.allowedFrom or channels.whatsApp.openAccess=true")
	}
	if cfg.Channels.Email.Enabled {
		if !cfg.Channels.Email.ConsentGranted {
			return errors.New("email enabled: set channels.email.consentGranted=true after explicit permission")
		}
		if !cfg.Channels.Email.OpenAccess && !hasNonEmpty(cfg.Channels.Email.AllowedSenders) {
			return errors.New("email enabled: set channels.email.allowedSenders or channels.email.openAccess=true")
		}
		if strings.TrimSpace(cfg.Channels.Email.IMAPHost) == "" || strings.TrimSpace(cfg.Channels.Email.IMAPUsername) == "" || strings.TrimSpace(cfg.Channels.Email.IMAPPassword) == "" {
			return errors.New("email enabled: imapHost, imapUsername, and imapPassword are required")
		}
		if strings.TrimSpace(cfg.Channels.Email.SMTPHost) == "" || strings.TrimSpace(cfg.Channels.Email.SMTPUsername) == "" || strings.TrimSpace(cfg.Channels.Email.SMTPPassword) == "" {
			return errors.New("email enabled: smtpHost, smtpUsername, and smtpPassword are required")
		}
	}
	return nil
}

func validateMCPServers(servers map[string]MCPServerConfig) error {
	for name, server := range servers {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("tools.mcpServers contains an empty server name")
		}
		if !server.Enabled {
			continue
		}
		switch server.Transport {
		case "stdio":
			if server.Command == "" {
				return errors.New("tools.mcpServers." + name + ": stdio transport requires command")
			}
		case "sse", "streamablehttp":
			if err := validateMCPHTTPURL(name, server); err != nil {
				return err
			}
		default:
			return errors.New("tools.mcpServers." + name + ": unsupported transport " + strconv.Quote(server.Transport))
		}
	}
	return nil
}

func validateMCPHTTPURL(name string, server MCPServerConfig) error {
	if server.URL == "" {
		return errors.New("tools.mcpServers." + name + ": transport " + strconv.Quote(server.Transport) + " requires url")
	}
	u, err := url.Parse(server.URL)
	if err != nil {
		return errors.New("tools.mcpServers." + name + ": invalid url")
	}
	if u.User != nil {
		return errors.New("tools.mcpServers." + name + ": url must not embed credentials")
	}
	if u.Host == "" {
		return errors.New("tools.mcpServers." + name + ": url must include host")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if !server.AllowInsecureHTTP {
			return errors.New("tools.mcpServers." + name + ": insecure http requires allowInsecureHttp=true")
		}
		if !isLoopbackHost(u.Hostname()) {
			return errors.New("tools.mcpServers." + name + ": insecure http is limited to localhost or loopback hosts")
		}
		return nil
	default:
		return errors.New("tools.mcpServers." + name + ": url scheme must be https or http")
	}
}

func validateAccessProfiles(cfg AccessProfilesConfig) error {
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.Default) != "" {
		if _, ok := cfg.Profiles[strings.TrimSpace(cfg.Default)]; !ok {
			return errors.New("security.profiles.default references unknown profile")
		}
	}
	for name, profile := range cfg.Profiles {
		name = strings.TrimSpace(name)
		if name == "" {
			return errors.New("security.profiles.profiles contains an empty profile name")
		}
		switch profile.MaxCapability {
		case "", "safe", "guarded", "privileged":
		default:
			return errors.New("security.profiles.profiles." + name + ": unsupported maxCapability")
		}
	}
	for channel, profileName := range cfg.Channels {
		if strings.TrimSpace(channel) == "" {
			return errors.New("security.profiles.channels contains an empty channel name")
		}
		if _, ok := cfg.Profiles[strings.TrimSpace(profileName)]; !ok {
			return errors.New("security.profiles.channels." + channel + " references unknown profile")
		}
	}
	for trigger, profileName := range cfg.Triggers {
		if strings.TrimSpace(trigger) == "" {
			return errors.New("security.profiles.triggers contains an empty trigger name")
		}
		if _, ok := cfg.Profiles[strings.TrimSpace(profileName)]; !ok {
			return errors.New("security.profiles.triggers." + trigger + " references unknown profile")
		}
	}
	return nil
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func hasNonEmpty(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}
````

## File: internal/agent/runtime.go
````go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	"or3-intern/internal/channels"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/security"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

const commandNewSession = "/new"

const maxTrackedQuotaSessions = 1024

type trustedToolAccessContextKey struct{}

type Deliverer interface {
	Deliver(ctx context.Context, channel, to, text string) error
}

type MetaDeliverer interface {
	DeliverWithMeta(ctx context.Context, channel, to, text string, meta map[string]any) error
}

type sessionLock struct {
	mu   sync.Mutex
	refs int
}

type Runtime struct {
	DB               *db.DB
	Provider         *providers.Client
	Model            string
	Temperature      float64
	Tools            *tools.Registry
	Hardening        config.HardeningConfig
	AccessProfiles   config.AccessProfilesConfig
	Builder          *Builder
	Artifacts        *artifacts.Store
	MaxToolBytes     int
	MaxToolLoops     int
	ToolPreviewBytes int
	Audit            *security.AuditLogger

	Deliver  Deliverer
	Streamer channels.StreamingChannel

	Consolidator           *memory.Consolidator
	ConsolidationScheduler *memory.Scheduler
	DefaultScopeKey        string
	LinkDirectMessages     bool
	IdentityScopeMap       map[string]string

	locksMu sync.Mutex
	locks   map[string]*sessionLock
	quotaMu sync.Mutex
	quotas  map[string]*sessionQuotaState
}

type sessionQuotaState struct {
	ToolCalls     int
	ExecCalls     int
	WebCalls      int
	SubagentCalls int
	LastSeen      time.Time
}

type BackgroundRunInput struct {
	SessionKey       string
	ParentSessionKey string
	Task             string
	PromptSnapshot   []providers.ChatMessage
	Tools            *tools.Registry
	Meta             map[string]any
	Channel          string
	ReplyTo          string
}

type BackgroundRunResult struct {
	FinalText  string
	Preview    string
	ArtifactID string
}

func (r *Runtime) lockFor(key string) *sync.Mutex {
	return &r.getSessionLock(key).mu
}

func (r *Runtime) acquireSessionLock(key string) *sessionLock {
	r.locksMu.Lock()
	if r.locks == nil {
		r.locks = map[string]*sessionLock{}
	}
	entry := r.locks[key]
	if entry == nil {
		entry = &sessionLock{}
		r.locks[key] = entry
	}
	entry.refs++
	r.locksMu.Unlock()
	return entry
}

func (r *Runtime) releaseSessionLock(key string, entry *sessionLock) {
	if r == nil || entry == nil {
		return
	}
	r.locksMu.Lock()
	if entry.refs > 0 {
		entry.refs--
	}
	if entry.refs == 0 {
		if current := r.locks[key]; current == entry {
			delete(r.locks, key)
		}
	}
	r.locksMu.Unlock()
}

func (r *Runtime) getSessionLock(key string) *sessionLock {
	r.locksMu.Lock()
	defer r.locksMu.Unlock()
	if r.locks == nil {
		r.locks = map[string]*sessionLock{}
	}
	entry := r.locks[key]
	if entry == nil {
		entry = &sessionLock{}
		r.locks[key] = entry
	}
	return entry
}

func (r *Runtime) Handle(ctx context.Context, ev bus.Event) error {
	ctx = r.contextWithEventProfile(ctx, ev)
	entry := r.acquireSessionLock(ev.SessionKey)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		r.releaseSessionLock(ev.SessionKey, entry)
	}()
	switch ev.Type {
	case bus.EventUserMessage, bus.EventCron, bus.EventHeartbeat, bus.EventSystem, bus.EventWebhook, bus.EventFileChange:
		return r.turn(ctx, ev)
	default:
		return nil
	}
}

func (r *Runtime) turn(ctx context.Context, ev bus.Event) error {
	defer releaseEvent(ev)

	if ev.Type == bus.EventUserMessage && strings.EqualFold(strings.TrimSpace(ev.Message), commandNewSession) {
		return r.handleNewSession(ctx, ev)
	}
	r.ensureSessionScope(ctx, ev)

	// persist user message
	msgID, err := r.DB.AppendMessage(ctx, ev.SessionKey, "user", ev.Message, map[string]any{
		"channel": ev.Channel, "from": ev.From, "meta": ev.Meta,
	})
	if err != nil {
		return err
	}
	if handled, err := r.handleExplicitSkillInvocation(ctx, ev, msgID); handled || err != nil {
		return err
	}
	if handled, err := r.handleStructuredAutonomy(ctx, ev, msgID); handled || err != nil {
		return err
	}

	// build prompt
	if r.Builder == nil {
		return fmt.Errorf("runtime builder not configured")
	}
	isAutonomous := isAutonomousEvent(ev.Type)
	messages, err := r.BuildPromptSnapshotWithOptions(ctx, BuildOptions{
		SessionKey:  ev.SessionKey,
		UserMessage: ev.Message,
		Autonomous:  isAutonomous,
		EventMeta:   cloneMap(ev.Meta),
	})
	if err != nil {
		return err
	}

	replyTarget := deliveryTarget(ev)
	replyMeta := channels.ReplyMeta(ev.Meta)
	finalText, streamed, err := r.executeConversation(ctx, ev.Type, ev.SessionKey, messages, r.effectiveTools(ctx, r.Tools), ev.Channel, replyTarget, replyMeta)
	if err != nil {
		return err
	}

	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, finalText, replyMeta, streamed, shouldAutoDeliver(ev))

	// best-effort rolling consolidation of old messages into memory notes
	if r.Consolidator != nil && r.Builder != nil && r.ConsolidationScheduler != nil {
		r.ConsolidationScheduler.Trigger(ev.SessionKey)
	} else if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.MaybeConsolidate(ctx, ev.SessionKey, historyMax); err != nil {
			log.Printf("consolidation failed: session=%s err=%v", ev.SessionKey, err)
		}
	}

	return nil
}

func (r *Runtime) ensureSessionScope(ctx context.Context, ev bus.Event) {
	if r == nil || r.DB == nil || strings.TrimSpace(ev.SessionKey) == "" {
		return
	}
	scopeKey, ok := r.scopeKeyForEvent(ev)
	if !ok {
		return
	}
	scopeKey = strings.TrimSpace(scopeKey)
	if scopeKey == "" || scopeKey == ev.SessionKey {
		return
	}
	meta := map[string]any{"auto": true, "channel": ev.Channel}
	_ = r.DB.LinkSession(ctx, ev.SessionKey, scopeKey, meta)
}

func (r *Runtime) scopeKeyForEvent(ev bus.Event) (string, bool) {
	if r == nil {
		return "", false
	}
	if scopeKey := strings.TrimSpace(r.IdentityScopeMap[ev.SessionKey]); scopeKey != "" {
		return scopeKey, true
	}
	if r.LinkDirectMessages && isDirectMessageEvent(ev) {
		scopeKey := strings.TrimSpace(r.DefaultScopeKey)
		if scopeKey == "" {
			scopeKey = ev.SessionKey
		}
		return scopeKey, true
	}
	return "", false
}

func isDirectMessageEvent(ev bus.Event) bool {
	if len(ev.Meta) == 0 {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(ev.Channel)) {
	case "telegram":
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(ev.Meta["chat_type"])), "private")
	case "slack":
		return strings.EqualFold(strings.TrimSpace(fmt.Sprint(ev.Meta["channel_type"])), "im")
	case "discord":
		if v, ok := ev.Meta["is_private"].(bool); ok {
			return v
		}
		return strings.TrimSpace(fmt.Sprint(ev.Meta["guild_id"])) == ""
	case "whatsapp":
		if v, ok := ev.Meta["is_group"].(bool); ok {
			return !v
		}
	case "email":
		return true
	}
	return false
}

func (r *Runtime) handleExplicitSkillInvocation(ctx context.Context, ev bus.Event, msgID int64) (bool, error) {
	if ev.Type != bus.EventUserMessage || r.Builder == nil {
		return false, nil
	}
	commandName, rawArgs, ok := parseSkillCommand(ev.Message)
	if !ok || commandName == "new" {
		return false, nil
	}
	replyTarget := deliveryTarget(ev)
	replyMeta := channels.ReplyMeta(ev.Meta)
	skill, found := r.Builder.Skills.Get(commandName)
	if !found {
		return false, nil
	}
	if !skill.UserInvocable {
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, "skill is not user-invocable: "+skill.Name, replyMeta, false, shouldAutoDeliver(ev))
		return true, nil
	}
	if !skill.Eligible {
		reasons := append([]string{}, skill.Missing...)
		reasons = append(reasons, skill.Unsupported...)
		if skill.ParseError != "" {
			reasons = append(reasons, skill.ParseError)
		}
		message := "skill unavailable: " + skill.Name
		if len(reasons) > 0 {
			message += " (" + strings.Join(reasons, "; ") + ")"
		}
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, message, replyMeta, false, shouldAutoDeliver(ev))
		return true, nil
	}
	if skill.CommandDispatch == "tool" {
		text := r.dispatchExplicitSkillTool(ctx, ev, skill, commandName, rawArgs)
		r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, text, replyMeta, false, shouldAutoDeliver(ev))
		return true, nil
	}

	promptInput := strings.TrimSpace(rawArgs)
	if promptInput == "" {
		promptInput = skill.Name
	}
	messages, err := r.BuildPromptSnapshotWithOptions(ctx, BuildOptions{
		SessionKey:  ev.SessionKey,
		UserMessage: promptInput,
		EventMeta:   cloneMap(ev.Meta),
	})
	if err != nil {
		return true, err
	}
	body, err := skills.LoadBody(skill.Path, 200000)
	if err != nil {
		return true, err
	}
	seed := fmt.Sprintf("Explicit skill requested: %s\nLocation: %s\nSource: %s\n\n%s", skill.Name, skill.Dir, skill.Source, body)
	seeded := make([]providers.ChatMessage, 0, len(messages)+1)
	if len(messages) > 0 {
		seeded = append(seeded, messages[0])
		seeded = append(seeded, providers.ChatMessage{Role: "system", Content: seed})
		seeded = append(seeded, messages[1:]...)
	} else {
		seeded = append(seeded, providers.ChatMessage{Role: "system", Content: seed})
		seeded = append(seeded, providers.ChatMessage{Role: "user", Content: promptInput})
	}
	runCtx := tools.ContextWithEnv(ctx, r.skillRunEnvFor(skill.Name))
	runCtx = tools.ContextWithSkillPolicy(runCtx, skillPolicyForSkill(skill))
	finalText, streamed, err := r.executeConversation(runCtx, ev.Type, ev.SessionKey, seeded, r.effectiveTools(runCtx, r.Tools), ev.Channel, replyTarget, replyMeta)
	if err != nil {
		return true, err
	}
	r.persistAssistantReply(ctx, ev.SessionKey, msgID, ev.Channel, replyTarget, finalText, replyMeta, streamed, shouldAutoDeliver(ev))
	return true, nil
}

func (r *Runtime) dispatchExplicitSkillTool(ctx context.Context, ev bus.Event, skill skills.SkillMeta, commandName, rawArgs string) string {
	if r.Tools == nil {
		return "tool registry not configured"
	}
	scopeKey := ev.SessionKey
	if r.DB != nil && strings.TrimSpace(ev.SessionKey) != "" {
		if resolved, err := r.DB.ResolveScopeKey(ctx, ev.SessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	toolCtx := tools.ContextWithSession(ctx, scopeKey)
	toolCtx = tools.ContextWithDelivery(toolCtx, ev.Channel, deliveryTarget(ev))
	toolCtx = tools.ContextWithDeliveryMeta(toolCtx, channels.ReplyMeta(ev.Meta))
	toolCtx = tools.ContextWithEnv(toolCtx, r.skillRunEnvFor(skill.Name))
	toolCtx = tools.ContextWithSkillPolicy(toolCtx, skillPolicyForSkill(skill))
	toolCtx = r.contextWithTrustedToolAccess(toolCtx, ev)
	toolCtx = tools.ContextWithToolGuard(toolCtx, r.guardToolExecution)
	params := map[string]any{
		"command":     rawArgs,
		"commandName": commandName,
		"skillName":   skill.Name,
	}
	out, err := r.Tools.ExecuteParams(toolCtx, skill.CommandTool, params)
	if err != nil {
		out = formatToolExecutionError(out, err)
	}
	payload := map[string]any{
		"tool":        skill.CommandTool,
		"skill":       skill.Name,
		"commandName": commandName,
		"args":        rawArgs,
	}
	sendOut, preview, artifactID := r.boundTextResult(ctx, ev.SessionKey, out)
	if artifactID != "" {
		payload["artifact_id"] = artifactID
		payload["preview"] = preview
	}
	if _, err := r.DB.AppendMessage(ctx, ev.SessionKey, "tool", sendOut, payload); err != nil {
		log.Printf("append tool message failed: %v", err)
	}
	return out
}

func parseSkillCommand(message string) (commandName string, rawArgs string, ok bool) {
	message = strings.TrimSpace(message)
	if !strings.HasPrefix(message, "/") || len(message) < 2 {
		return "", "", false
	}
	body := strings.TrimPrefix(message, "/")
	commandName, rawArgs, _ = strings.Cut(body, " ")
	commandName = strings.TrimSpace(commandName)
	rawArgs = strings.TrimSpace(rawArgs)
	if commandName == "" {
		return "", "", false
	}
	return commandName, rawArgs, true
}

func (r *Runtime) BuildPromptSnapshot(ctx context.Context, sessionKey string, userMessage string) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.Build(ctx, sessionKey, userMessage)
	if err != nil {
		return nil, err
	}
	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: userMessage})
	}
	return messages, nil
}

func (r *Runtime) BuildPromptSnapshotWithOptions(ctx context.Context, opts BuildOptions) ([]providers.ChatMessage, error) {
	if r.Builder == nil {
		return nil, fmt.Errorf("runtime builder not configured")
	}
	pp, _, err := r.Builder.BuildWithOptions(ctx, opts)
	if err != nil {
		return nil, err
	}
	messages := append([]providers.ChatMessage{}, pp.System...)
	messages = append(messages, pp.History...)
	if len(pp.History) == 0 || pp.History[len(pp.History)-1].Role != "user" {
		messages = append(messages, providers.ChatMessage{Role: "user", Content: opts.UserMessage})
	}
	return messages, nil
}

func (r *Runtime) RunBackground(ctx context.Context, input BackgroundRunInput) (BackgroundRunResult, error) {
	ctx = r.contextWithProfileName(ctx, strings.TrimSpace(fmt.Sprint(input.Meta["profile_name"])))
	entry := r.acquireSessionLock(input.SessionKey)
	entry.mu.Lock()
	defer func() {
		entry.mu.Unlock()
		r.releaseSessionLock(input.SessionKey, entry)
	}()

	if strings.TrimSpace(input.SessionKey) == "" {
		return BackgroundRunResult{}, fmt.Errorf("background session key required")
	}
	if len(input.PromptSnapshot) == 0 {
		return BackgroundRunResult{}, fmt.Errorf("background prompt snapshot required")
	}
	if _, err := r.DB.AppendMessage(ctx, input.SessionKey, "user", input.Task, input.Meta); err != nil {
		return BackgroundRunResult{}, err
	}
	reg := input.Tools
	if reg == nil {
		reg = r.effectiveTools(ctx, r.Tools)
	}
	finalText, _, err := r.executeConversation(ctx, bus.EventSystem, input.SessionKey, append([]providers.ChatMessage{}, input.PromptSnapshot...), reg, input.Channel, input.ReplyTo, nil)
	if err != nil {
		return BackgroundRunResult{}, err
	}
	storedText, preview, artifactID := r.boundTextResult(ctx, input.SessionKey, finalText)
	payload := cloneMap(input.Meta)
	if input.ParentSessionKey != "" {
		payload["parent_session_key"] = input.ParentSessionKey
	}
	if artifactID != "" {
		payload["artifact_id"] = artifactID
		payload["preview"] = preview
	}
	if _, err := r.DB.AppendMessage(ctx, input.SessionKey, "assistant", storedText, payload); err != nil {
		log.Printf("append background assistant(final) failed: %v", err)
	}
	return BackgroundRunResult{FinalText: finalText, Preview: preview, ArtifactID: artifactID}, nil
}

func (r *Runtime) handleNewSession(ctx context.Context, ev bus.Event) error {
	replyTarget := deliveryTarget(ev)
	if r.Consolidator != nil && r.Builder != nil {
		historyMax := r.Builder.HistoryMax
		if historyMax <= 0 {
			historyMax = 40
		}
		if err := r.Consolidator.ArchiveAll(ctx, ev.SessionKey, historyMax); err != nil {
			msg := "Memory archival failed, session not cleared. Please try again."
			if r.Deliver != nil {
				if derr := r.deliver(ctx, ev.Channel, replyTarget, msg, ev.Meta); derr != nil {
					log.Printf("deliver failed: %v", derr)
				}
			}
			return nil
		}
	}
	if err := r.DB.ResetSessionHistory(ctx, ev.SessionKey); err != nil {
		msg := "New session failed. Please try again."
		if r.Deliver != nil {
			if derr := r.deliver(ctx, ev.Channel, replyTarget, msg, ev.Meta); derr != nil {
				log.Printf("deliver failed: %v", derr)
			}
		}
		return nil
	}
	if r.Deliver != nil {
		if err := r.deliver(ctx, ev.Channel, replyTarget, "New session started.", ev.Meta); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}
	return nil
}

func contentToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (r *Runtime) executeConversation(ctx context.Context, eventType bus.EventType, sessionKey string, messages []providers.ChatMessage, reg *tools.Registry, channel string, replyTo string, replyMeta map[string]any) (string, bool, error) {
	if reg == nil {
		reg = tools.NewRegistry()
	}
	observer := conversationObserverFromContext(ctx)
	scopeKey := sessionKey
	if r.DB != nil && strings.TrimSpace(sessionKey) != "" {
		if resolved, err := r.DB.ResolveScopeKey(ctx, sessionKey); err == nil && strings.TrimSpace(resolved) != "" {
			scopeKey = resolved
		}
	}
	maxLoops := r.MaxToolLoops
	if maxLoops <= 0 {
		maxLoops = 6
	}
	for loop := 0; loop < maxLoops; loop++ {
		req := providers.ChatCompletionRequest{
			Model:       r.Model,
			Messages:    messages,
			Tools:       toToolDefs(reg),
			Temperature: r.Temperature,
		}

		var resp providers.ChatCompletionResponse
		var err error
		var sw channels.StreamWriter // lazily created on first text delta
		var swOnce sync.Once
		streamer := r.streamerForContext(ctx)
		if streamer != nil {
			resp, err = r.Provider.ChatStream(ctx, req, func(text string) {
				if observer != nil {
					observer.OnTextDelta(ctx, text)
				}
				swOnce.Do(func() {
					streamMeta := channels.CloneMeta(replyMeta)
					if streamMeta == nil {
						streamMeta = map[string]any{}
					}
					streamMeta["channel"] = channel
					w, beginErr := streamer.BeginStream(ctx, replyTo, streamMeta)
					if beginErr == nil {
						sw = w
					}
				})
				if sw != nil {
					_ = sw.WriteDelta(ctx, text)
				}
			})
		} else {
			resp, err = r.Provider.Chat(ctx, req)
		}
		if err != nil {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			if observer != nil {
				observer.OnError(ctx, err)
			}
			return "", false, err
		}
		if len(resp.Choices) == 0 {
			if sw != nil {
				_ = sw.Abort(ctx)
			}
			err = fmt.Errorf("no choices")
			if observer != nil {
				observer.OnError(ctx, err)
			}
			return "", false, err
		}
		msg := resp.Choices[0].Message
		if len(msg.ToolCalls) == 0 {
			finalText := strings.TrimSpace(contentToString(msg.Content))
			messages = append(messages, providers.ChatMessage{Role: "assistant", Content: finalText})
			if sw != nil {
				_ = sw.Close(ctx, finalText)
				if observer != nil {
					observer.OnCompletion(ctx, finalText, true)
				}
				return finalText, true, nil
			}
			if observer != nil {
				observer.OnCompletion(ctx, finalText, false)
			}
			return finalText, false, nil
		}

		// Tool-call turn: close any partial stream that showed text.
		if sw != nil {
			_ = sw.Close(ctx, contentToString(msg.Content))
		}

		messages = append(messages, providers.ChatMessage{Role: "assistant", Content: msg.Content, ToolCalls: msg.ToolCalls})
		if _, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", contentToString(msg.Content), map[string]any{"tool_calls": msg.ToolCalls}); err != nil {
			log.Printf("append assistant(tool_calls) failed: %v", err)
		}

		for _, tc := range msg.ToolCalls {
			if observer != nil {
				observer.OnToolCall(ctx, tc.Function.Name, tc.Function.Arguments)
			}
			toolCtx := tools.ContextWithSession(ctx, scopeKey)
			toolCtx = tools.ContextWithDelivery(toolCtx, channel, replyTo)
			toolCtx = tools.ContextWithDeliveryMeta(toolCtx, replyMeta)
			toolCtx = r.contextWithTrustedToolAccess(toolCtx, bus.Event{Type: eventType, SessionKey: sessionKey, Channel: channel})
			toolCtx = tools.ContextWithToolGuard(toolCtx, r.guardToolExecution)
			out, err := reg.Execute(toolCtx, tc.Function.Name, tc.Function.Arguments)
			if observer != nil {
				observer.OnToolResult(ctx, tc.Function.Name, out, err)
			}
			if err != nil {
				out = formatToolExecutionError(out, err)
			}

			payload := map[string]any{
				"tool": tc.Function.Name,
				"args": json.RawMessage([]byte(tc.Function.Arguments)),
			}
			sendOut, preview, artifactID := r.boundTextResult(ctx, sessionKey, out)
			if artifactID != "" {
				payload["artifact_id"] = artifactID
				payload["preview"] = preview
			}
			if _, err := r.DB.AppendMessage(ctx, sessionKey, "tool", sendOut, payload); err != nil {
				log.Printf("append tool message failed: %v", err)
			}
			messages = append(messages, providers.ChatMessage{Role: "tool", ToolCallID: tc.ID, Content: sendOut})
		}
	}
	return "(no response)", false, nil
}

func (r *Runtime) effectiveTools(ctx context.Context, fallback *tools.Registry) *tools.Registry {
	if reg := toolRegistryFromContext(ctx); reg != nil {
		return reg
	}
	if fallback == nil {
		return tools.NewRegistry()
	}
	return fallback
}

func (r *Runtime) streamerForContext(ctx context.Context) channels.StreamingChannel {
	if streamer := streamingChannelFromContext(ctx); streamer != nil {
		return streamer
	}
	return r.Streamer
}

func (r *Runtime) guardToolExecution(ctx context.Context, tool tools.Tool, capability tools.CapabilityLevel, params map[string]any) error {
	if tool == nil {
		return nil
	}
	profile := tools.ActiveProfileFromContext(ctx)
	if tool.Name() == "send_message" && trustedToolAccessFromContext(ctx) {
		capability = tools.CapabilitySafe
	}
	if err := r.enforceProfile(ctx, profile, tool, capability, params); err != nil {
		return err
	}
	if err := r.enforceSkillPolicy(ctx, tool, params); err != nil {
		return err
	}
	if capability == tools.CapabilityGuarded && !r.Hardening.GuardedTools {
		return fmt.Errorf("tool requires guarded access: %s", tool.Name())
	}
	if capability == tools.CapabilityPrivileged && !r.Hardening.PrivilegedTools {
		return fmt.Errorf("tool requires privileged access: %s", tool.Name())
	}
	if r.Audit != nil && (capability == tools.CapabilityPrivileged || tool.Name() == "spawn_subagent") {
		if err := r.Audit.Record(ctx, "tool.execute", tools.SessionFromContext(ctx), profileActor(profile), map[string]any{
			"tool":       tool.Name(),
			"capability": capability,
			"profile":    profile.Name,
			"summary":    summarizeToolParams(tool.Name(), params),
		}); err != nil {
			return err
		}
	}
	if !r.Hardening.Quotas.Enabled {
		return nil
	}
	state := r.sessionQuotaState(tools.SessionFromContext(ctx))
	if err := quotaIncrement(&state.ToolCalls, r.Hardening.Quotas.MaxToolCalls, "tool calls", tool.Name()); err != nil {
		return err
	}
	switch tool.Name() {
	case "exec", "run_skill_script":
		if err := quotaIncrement(&state.ExecCalls, r.Hardening.Quotas.MaxExecCalls, "exec calls", tool.Name()); err != nil {
			return err
		}
	case "web_fetch", "web_search":
		if err := quotaIncrement(&state.WebCalls, r.Hardening.Quotas.MaxWebCalls, "web calls", tool.Name()); err != nil {
			return err
		}
	case "spawn_subagent":
		if err := quotaIncrement(&state.SubagentCalls, r.Hardening.Quotas.MaxSubagentCalls, "subagent calls", tool.Name()); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) enforceSkillPolicy(ctx context.Context, tool tools.Tool, params map[string]any) error {
	policy := tools.SkillPolicyFromContext(ctx)
	if tool == nil || strings.TrimSpace(policy.Name) == "" {
		return nil
	}
	if len(policy.AllowedTools) > 0 {
		if _, ok := policy.AllowedTools[tool.Name()]; !ok {
			return fmt.Errorf("tool denied by skill policy: %s", tool.Name())
		}
	}
	switch tool.Name() {
	case "exec", "run_skill_script":
		if !policy.AllowExecution {
			return fmt.Errorf("execution denied by skill policy: %s", tool.Name())
		}
		if cwd := strings.TrimSpace(fmt.Sprint(params["cwd"])); cwd != "" && cwd != "<nil>" && len(policy.WritablePaths) > 0 {
			if err := validateProfileWritablePath(policy.WritablePaths, cwd); err != nil {
				return err
			}
		}
	case "write_file", "edit_file":
		if !policy.AllowWrite {
			return fmt.Errorf("write denied by skill policy: %s", tool.Name())
		}
		if len(policy.WritablePaths) > 0 {
			if err := validateProfileWritablePath(policy.WritablePaths, fmt.Sprint(params["path"])); err != nil {
				return err
			}
		}
	case "web_fetch":
		if !policy.AllowNetwork {
			return fmt.Errorf("network denied by skill policy: %s", tool.Name())
		}
		if len(policy.AllowedHosts) > 0 {
			parsed, err := url.Parse(strings.TrimSpace(fmt.Sprint(params["url"])))
			if err != nil {
				return err
			}
			if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: policy.AllowedHosts}).ValidateURL(ctx, parsed); err != nil {
				return err
			}
		}
	case "web_search":
		if !policy.AllowNetwork {
			return fmt.Errorf("network denied by skill policy: %s", tool.Name())
		}
		if len(policy.AllowedHosts) > 0 {
			if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: policy.AllowedHosts}).ValidateHost(ctx, "api.search.brave.com"); err != nil {
				return err
			}
		}
	}
	return nil
}

func skillPolicyForSkill(skill skills.SkillMeta) tools.SkillPolicy {
	allowed := map[string]struct{}{}
	for _, toolName := range skill.AllowedTools {
		toolName = strings.TrimSpace(toolName)
		if toolName == "" {
			continue
		}
		allowed[toolName] = struct{}{}
	}
	if commandTool := strings.TrimSpace(skill.CommandTool); commandTool != "" {
		allowed[commandTool] = struct{}{}
	}
	return tools.SkillPolicy{
		Name:           skill.Name,
		AllowedTools:   allowed,
		AllowExecution: skill.Permissions.Shell || (strings.EqualFold(skill.CommandDispatch, "tool") && (strings.EqualFold(skill.CommandTool, "exec") || strings.EqualFold(skill.CommandTool, "run_skill_script"))),
		AllowNetwork:   skill.Permissions.Network,
		AllowWrite:     skill.Permissions.Write,
		AllowedHosts:   append([]string{}, skill.Permissions.AllowedHosts...),
		WritablePaths:  append([]string{}, skill.Permissions.AllowedPaths...),
	}
}

func (r *Runtime) contextWithEventProfile(ctx context.Context, ev bus.Event) context.Context {
	if r == nil {
		return ctx
	}
	return r.contextWithProfileName(ctx, r.profileNameForEvent(ev))
}

func (r *Runtime) contextWithProfileName(ctx context.Context, name string) context.Context {
	profile, ok := r.resolveProfile(name)
	if !ok {
		return ctx
	}
	return tools.ContextWithActiveProfile(ctx, profile)
}

func (r *Runtime) profileNameForEvent(ev bus.Event) string {
	if len(ev.Meta) > 0 {
		if profileName := strings.TrimSpace(fmt.Sprint(ev.Meta["profile_name"])); profileName != "" && profileName != "<nil>" {
			return profileName
		}
	}
	if !r.AccessProfiles.Enabled {
		return ""
	}
	triggerKey := strings.ToLower(strings.TrimSpace(string(ev.Type)))
	if profileName := strings.TrimSpace(r.AccessProfiles.Triggers[triggerKey]); profileName != "" {
		return profileName
	}
	if profileName := strings.TrimSpace(r.AccessProfiles.Channels[strings.ToLower(strings.TrimSpace(ev.Channel))]); profileName != "" {
		return profileName
	}
	return strings.TrimSpace(r.AccessProfiles.Default)
}

func (r *Runtime) resolveProfile(name string) (tools.ActiveProfile, bool) {
	name = strings.TrimSpace(name)
	if !r.AccessProfiles.Enabled || name == "" {
		return tools.ActiveProfile{}, false
	}
	profileCfg, ok := r.AccessProfiles.Profiles[name]
	if !ok {
		return tools.ActiveProfile{}, false
	}
	allowed := map[string]struct{}{}
	for _, toolName := range profileCfg.AllowedTools {
		allowed[strings.TrimSpace(toolName)] = struct{}{}
	}
	maxCapability := tools.CapabilityPrivileged
	switch strings.ToLower(strings.TrimSpace(profileCfg.MaxCapability)) {
	case "safe":
		maxCapability = tools.CapabilitySafe
	case "guarded":
		maxCapability = tools.CapabilityGuarded
	case "privileged", "":
		maxCapability = tools.CapabilityPrivileged
	}
	return tools.ActiveProfile{
		Name:           name,
		MaxCapability:  maxCapability,
		AllowedTools:   allowed,
		AllowedHosts:   append([]string{}, profileCfg.AllowedHosts...),
		WritablePaths:  append([]string{}, profileCfg.WritablePaths...),
		AllowSubagents: profileCfg.AllowSubagents,
	}, true
}

func (r *Runtime) enforceProfile(ctx context.Context, profile tools.ActiveProfile, tool tools.Tool, capability tools.CapabilityLevel, params map[string]any) error {
	if strings.TrimSpace(profile.Name) == "" {
		return nil
	}
	if capabilityRank(capability) > capabilityRank(profile.MaxCapability) {
		return fmt.Errorf("tool exceeds profile capability: %s", tool.Name())
	}
	if len(profile.AllowedTools) > 0 {
		if _, ok := profile.AllowedTools[tool.Name()]; !ok {
			return fmt.Errorf("tool denied by profile: %s", tool.Name())
		}
	}
	if tool.Name() == "spawn_subagent" && !profile.AllowSubagents {
		return fmt.Errorf("subagents denied by profile")
	}
	switch tool.Name() {
	case "write_file", "edit_file":
		if len(profile.WritablePaths) == 0 {
			return fmt.Errorf("path denied by profile")
		}
		if err := validateProfileWritablePath(profile.WritablePaths, fmt.Sprint(params["path"])); err != nil {
			return err
		}
	case "exec":
		if cwd := strings.TrimSpace(fmt.Sprint(params["cwd"])); cwd != "" && cwd != "<nil>" {
			if len(profile.WritablePaths) == 0 {
				return fmt.Errorf("path denied by profile")
			}
			if err := validateProfileWritablePath(profile.WritablePaths, cwd); err != nil {
				return err
			}
		}
	}
	switch tool.Name() {
	case "web_fetch":
		parsed, err := url.Parse(strings.TrimSpace(fmt.Sprint(params["url"])))
		if err != nil {
			return err
		}
		if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: profile.AllowedHosts}).ValidateURL(ctx, parsed); err != nil {
			return err
		}
	case "web_search":
		if err := (security.HostPolicy{Enabled: true, DefaultDeny: true, AllowedHosts: profile.AllowedHosts}).ValidateHost(ctx, "api.search.brave.com"); err != nil {
			return err
		}
	}
	return nil
}

func capabilityRank(level tools.CapabilityLevel) int {
	switch level {
	case tools.CapabilitySafe:
		return 0
	case tools.CapabilityGuarded:
		return 1
	case tools.CapabilityPrivileged:
		return 2
	default:
		return 2
	}
}

func validateProfileWritablePath(allowed []string, path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "<nil>" {
		return nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	for _, root := range allowed {
		rootPath, rootErr := filepath.Abs(root)
		if rootErr != nil {
			continue
		}
		rel, relErr := filepath.Rel(rootPath, absPath)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil
		}
	}
	return fmt.Errorf("path denied by profile")
}

func profileActor(profile tools.ActiveProfile) string {
	if strings.TrimSpace(profile.Name) == "" {
		return "runtime"
	}
	return "profile:" + profile.Name
}

func summarizeToolParams(toolName string, params map[string]any) map[string]any {
	summary := map[string]any{"tool": toolName}
	switch toolName {
	case "exec":
		summary["program"] = strings.TrimSpace(fmt.Sprint(params["program"]))
		summary["cwd"] = strings.TrimSpace(fmt.Sprint(params["cwd"]))
	case "run_skill_script":
		summary["skill"] = strings.TrimSpace(fmt.Sprint(params["skill"]))
		summary["entrypoint"] = strings.TrimSpace(fmt.Sprint(params["entrypoint"]))
	case "spawn_subagent":
		summary["task"] = previewText(strings.TrimSpace(fmt.Sprint(params["task"])), 120)
	case "web_fetch":
		summary["url"] = strings.TrimSpace(fmt.Sprint(params["url"]))
	}
	return summary
}

func formatToolExecutionError(out string, err error) string {
	if err == nil {
		return out
	}
	if strings.TrimSpace(out) == "" {
		return "tool error: " + err.Error()
	}
	return out + "\n\nerror: " + err.Error()
}

func (r *Runtime) sessionQuotaState(sessionKey string) *sessionQuotaState {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		sessionKey = scope.GlobalMemoryScope
	}
	r.quotaMu.Lock()
	defer r.quotaMu.Unlock()
	if r.quotas == nil {
		r.quotas = map[string]*sessionQuotaState{}
	}
	r.evictQuotaStateLocked()
	state := r.quotas[sessionKey]
	if state == nil {
		state = &sessionQuotaState{}
		r.quotas[sessionKey] = state
	}
	state.LastSeen = time.Now()
	return state
}

func (r *Runtime) contextWithTrustedToolAccess(ctx context.Context, ev bus.Event) context.Context {
	if !isTrustedToolEvent(ev.Type) {
		return ctx
	}
	return context.WithValue(ctx, trustedToolAccessContextKey{}, true)
}

func trustedToolAccessFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	trusted, _ := ctx.Value(trustedToolAccessContextKey{}).(bool)
	return trusted
}

func isTrustedToolEvent(eventType bus.EventType) bool {
	switch eventType {
	case bus.EventHeartbeat, bus.EventCron:
		return true
	default:
		return false
	}
}

func (r *Runtime) evictQuotaStateLocked() {
	if len(r.quotas) < maxTrackedQuotaSessions {
		return
	}
	oldestKey := ""
	var oldestTime time.Time
	for key, state := range r.quotas {
		if state == nil {
			delete(r.quotas, key)
			continue
		}
		if oldestKey == "" || state.LastSeen.Before(oldestTime) {
			oldestKey = key
			oldestTime = state.LastSeen
		}
	}
	if oldestKey != "" {
		delete(r.quotas, oldestKey)
	}
}

func quotaIncrement(current *int, limit int, label string, toolName string) error {
	if current == nil || limit <= 0 {
		return nil
	}
	if *current >= limit {
		return fmt.Errorf("quota exceeded for %s while executing %s", label, toolName)
	}
	*current = *current + 1
	return nil
}

func (r *Runtime) skillRunEnvFor(name string) map[string]string {
	if r.Builder == nil {
		return nil
	}
	return r.Builder.Skills.RunEnvForSkill(name)
}

func (r *Runtime) persistAssistantReply(ctx context.Context, sessionKey string, msgID int64, channel, replyTarget, finalText string, replyMeta map[string]any, streamed bool, autoDeliver bool) {
	if strings.TrimSpace(finalText) == "" {
		finalText = "(no response)"
	}
	if _, err := r.DB.AppendMessage(ctx, sessionKey, "assistant", finalText, map[string]any{"in_reply_to": msgID}); err != nil {
		log.Printf("append assistant(final) failed: %v", err)
	}
	if autoDeliver && !streamed && r.Deliver != nil {
		if err := r.deliver(ctx, channel, replyTarget, finalText, replyMeta); err != nil {
			log.Printf("deliver failed: %v", err)
		}
	}
}

func (r *Runtime) deliver(ctx context.Context, channel, to, text string, meta map[string]any) error {
	if r.Deliver == nil {
		return nil
	}
	if withMeta, ok := r.Deliver.(MetaDeliverer); ok {
		return withMeta.DeliverWithMeta(ctx, channel, to, text, channels.ReplyMeta(meta))
	}
	return r.Deliver.Deliver(ctx, channel, to, text)
}

func (r *Runtime) boundTextResult(ctx context.Context, sessionKey string, text string) (stored string, preview string, artifactID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "(no response)", "(no response)", ""
	}
	preview = previewText(text, r.toolPreviewBytes())
	if r.MaxToolBytes > 0 && len(text) > r.MaxToolBytes && r.Artifacts != nil {
		id, err := r.Artifacts.Save(ctx, sessionKey, "text/plain", []byte(text))
		if err != nil {
			log.Printf("artifact save failed: %v", err)
			return text, preview, ""
		}
		return fmt.Sprintf("artifact_id=%s\npreview:\n%s", id, preview), preview, id
	}
	return text, preview, ""
}

func (r *Runtime) toolPreviewBytes() int {
	if r.ToolPreviewBytes <= 0 {
		return 500
	}
	return r.ToolPreviewBytes
}

func previewText(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(no response)"
	}
	if max > 0 && len(s) > max {
		return s[:max] + "…[preview]"
	}
	return s
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func deliveryTarget(ev bus.Event) string {
	if len(ev.Meta) > 0 {
		for _, key := range []string{"chat_id", "channel_id"} {
			if target := strings.TrimSpace(fmt.Sprint(ev.Meta[key])); target != "" && target != "<nil>" {
				return target
			}
		}
	}
	return strings.TrimSpace(ev.From)
}

func isAutonomousEvent(eventType bus.EventType) bool {
	switch eventType {
	case bus.EventCron, bus.EventHeartbeat, bus.EventWebhook, bus.EventFileChange:
		return true
	default:
		return false
	}
}

func shouldAutoDeliver(ev bus.Event) bool {
	if ev.Type == bus.EventHeartbeat {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(ev.Channel), "email") {
		if len(ev.Meta) == 0 {
			return true
		}
		value, ok := ev.Meta["auto_reply_enabled"]
		if !ok {
			return true
		}
		switch cast := value.(type) {
		case bool:
			return cast
		default:
			return strings.EqualFold(strings.TrimSpace(fmt.Sprint(cast)), "true")
		}
	}
	return true
}

func releaseEvent(ev bus.Event) {
	if len(ev.Meta) == 0 {
		return
	}
	done, ok := ev.Meta[heartbeat.MetaKeyDone].(func())
	if !ok || done == nil {
		return
	}
	done()
}

func toToolDefs(reg *tools.Registry) []providers.ToolDef {
	if reg == nil {
		return nil
	}
	raw := reg.Definitions()
	out := make([]providers.ToolDef, 0, len(raw))
	for _, d := range raw {
		fn, _ := d["function"].(map[string]any)
		td := providers.ToolDef{
			Type: "function",
			Function: providers.ToolFunc{
				Name:        fmt.Sprint(fn["name"]),
				Description: fmt.Sprint(fn["description"]),
				Parameters:  fn["parameters"],
			},
		}
		out = append(out, td)
	}
	return out
}

// Cron runner helper: turns a job into a bus event message
func CronRunner(b *bus.Bus, defaultSessionKey string) cron.Runner {
	return func(ctx context.Context, job cron.CronJob) error {
		_ = ctx
		msg := job.Payload.Message
		if strings.TrimSpace(msg) == "" {
			msg = "cron job: " + job.Name
		}
		// prefer per-job session key over the default
		sessionKey := job.Payload.SessionKey
		if strings.TrimSpace(sessionKey) == "" {
			sessionKey = defaultSessionKey
		}
		ev := bus.Event{Type: bus.EventCron, SessionKey: sessionKey, Channel: job.Payload.Channel, From: job.Payload.To, Message: msg, Meta: map[string]any{"job_id": job.ID}}
		if ok := b.Publish(ev); !ok {
			return fmt.Errorf("event bus full")
		}
		return nil
	}
}

func WithTimeout(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		sec = 60
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}
````

## File: cmd/or3-intern/main.go
````go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/bus"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/channels/cli"
	"or3-intern/internal/channels/discord"
	"or3-intern/internal/channels/email"
	"or3-intern/internal/channels/slack"
	"or3-intern/internal/channels/telegram"
	"or3-intern/internal/channels/whatsapp"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/heartbeat"
	"or3-intern/internal/mcp"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/scope"
	"or3-intern/internal/security"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
	"or3-intern/internal/triggers"
)

const (
	schedulerMaxConsolidationPasses = 3
	gracefulShutdownTimeout         = 5 * time.Second
)

func main() {
	var cfgPath string
	flag.StringVar(&cfgPath, "config", "", "path to config.json")
	flag.Parse()

	args := flag.Args()
	cmd := "chat"
	if len(args) > 0 {
		cmd = args[0]
	}
	if cmd == "init" {
		if err := runInit(cfgPath); err != nil {
			fmt.Fprintln(os.Stderr, "init error:", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(1)
	}
	if cmd == "doctor" {
		if err := runDoctorCommand(cfg, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "doctor error:", err)
			os.Exit(1)
		}
		return
	}
	if cfg.Tools.RestrictToWorkspace && strings.TrimSpace(cfg.WorkspaceDir) == "" {
		if cwd, err := os.Getwd(); err == nil {
			cfg.WorkspaceDir = cwd
		}
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir db dir error:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.ArtifactsDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir artifacts dir error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.SoulFile, agent.DefaultSoul); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap soul file error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.AgentsFile, agent.DefaultAgentInstructions); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap agents file error:", err)
		os.Exit(1)
	}
	if err := ensureFileIfMissing(cfg.ToolsFile, agent.DefaultToolNotes); err != nil {
		fmt.Fprintln(os.Stderr, "bootstrap tools file error:", err)
		os.Exit(1)
	}
	// Bootstrap IDENTITY.md and MEMORY.md (silent fallback if missing)
	if cfg.IdentityFile != "" {
		_ = ensureFileIfMissing(cfg.IdentityFile, "# Identity\n")
	}
	if cfg.MemoryFile != "" {
		_ = ensureFileIfMissing(cfg.MemoryFile, "# Static Memory\n")
	}

	d, err := db.Open(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "db error:", err)
		os.Exit(1)
	}
	defer d.Close()

	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	cfg, secretManager, auditLogger, err := setupSecurity(ctx, cfg, d)
	if err != nil {
		fmt.Fprintln(os.Stderr, "security error:", err)
		os.Exit(1)
	}
	if cmd == "secrets" {
		if secretManager == nil && cfg.Security.SecretStore.Enabled {
			key, keyErr := security.LoadOrCreateKey(cfg.Security.SecretStore.KeyFile)
			if keyErr != nil {
				fmt.Fprintln(os.Stderr, "secret key error:", keyErr)
				os.Exit(1)
			}
			secretManager = &security.SecretManager{DB: d, Key: key}
		}
		if err := runSecretsCommand(ctx, secretManager, auditLogger, args[1:], os.Stdout, os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "secrets error:", err)
			os.Exit(1)
		}
		return
	}
	if cmd == "audit" {
		if err := runAuditCommand(ctx, auditLogger, args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "audit error:", err)
			os.Exit(1)
		}
		return
	}
	timeout := time.Duration(cfg.Provider.TimeoutSeconds) * time.Second
	prov := providers.New(cfg.Provider.APIBase, cfg.Provider.APIKey, timeout)
	prov.HostPolicy = buildHostPolicy(cfg)
	art := &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}

	b := bus.New(256)
	spinner := cli.NewSpinner()
	del := cli.Deliverer{Spinner: spinner}
	channelManager, err := buildChannelManager(cfg, del, art, cfg.MaxMediaBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "channel config error:", err)
		os.Exit(1)
	}

	var mcpManager *mcp.Manager
	if len(cfg.Tools.MCPServers) > 0 {
		mcpManager = mcp.NewManager(cfg.Tools.MCPServers)
		mcpManager.SetLogger(log.Printf)
		mcpManager.SetHostPolicy(buildHostPolicy(cfg))
		if err := mcpManager.Connect(ctx); err != nil {
			log.Printf("mcp setup failed: %v", err)
		}
	}

	// skills
	builtin := filepath.Join(filepath.Dir(cfgPathOrDefault(cfgPath)), "builtin_skills")
	toolNames := loadAvailableToolNamesWithManager(ctx, cfg, mcpManager)
	inv := buildSkillsInventory(cfg, builtin, toolNames)
	var cronSvc *cron.Service
	var subagentManager *agent.SubagentManager
	enableSubagents := subagentsEnabledForCommand(cmd, cfg)
	buildRuntimeTools := func() *tools.Registry {
		return buildToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, subagentManager, mcpManager)
	}
	buildBackgroundTools := func() *tools.Registry {
		return buildBackgroundToolRegistry(cfg, d, prov, channelManager, &inv, cronSvc, mcpManager)
	}

	ret := memory.NewRetriever(d)
	ret.VectorScanLimit = cfg.VectorScanLimit

	var docIndexer *memory.DocIndexer
	var docRetriever *memory.DocRetriever
	if cfg.DocIndex.Enabled && len(cfg.DocIndex.Roots) > 0 {
		docIndexer = &memory.DocIndexer{
			DB:         d,
			Provider:   prov,
			EmbedModel: cfg.Provider.EmbedModel,
			Config: memory.DocIndexConfig{
				Roots:          cfg.DocIndex.Roots,
				MaxFiles:       cfg.DocIndex.MaxFiles,
				MaxFileBytes:   cfg.DocIndex.MaxFileBytes,
				MaxChunks:      cfg.DocIndex.MaxChunks,
				EmbedMaxBytes:  cfg.DocIndex.EmbedMaxBytes,
				RefreshSeconds: cfg.DocIndex.RefreshSeconds,
				RetrieveLimit:  cfg.DocIndex.RetrieveLimit,
			},
		}
		docRetriever = &memory.DocRetriever{DB: d}
		// Initial sync in background (don't block startup)
		go func() {
			syncCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := docIndexer.SyncRoots(syncCtx, scope.GlobalMemoryScope); err != nil {
				log.Printf("doc index sync failed: %v", err)
			}
		}()
	}
	if docIndexer != nil && cfg.DocIndex.RefreshSeconds > 0 {
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.DocIndex.RefreshSeconds) * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				syncCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				if err := docIndexer.SyncRoots(syncCtx, scope.GlobalMemoryScope); err != nil {
					log.Printf("doc index refresh failed: %v", err)
				}
				cancel()
			}
		}()
	}

	rt := &agent.Runtime{
		DB:          d,
		Provider:    prov,
		Model:       cfg.Provider.Model,
		Temperature: cfg.Provider.Temperature,
		Tools:       buildRuntimeTools(),
		Builder: &agent.Builder{
			DB:                     d,
			Artifacts:              art,
			Skills:                 inv,
			Mem:                    ret,
			Provider:               prov,
			EmbedModel:             cfg.Provider.EmbedModel,
			EnableVision:           cfg.Provider.EnableVision,
			Soul:                   loadBootstrapFile(cfg.SoulFile, cfg.WorkspaceDir, "SOUL.md", agent.DefaultSoul),
			AgentInstructions:      loadBootstrapFile(cfg.AgentsFile, cfg.WorkspaceDir, "AGENTS.md", agent.DefaultAgentInstructions),
			ToolNotes:              loadBootstrapFile(cfg.ToolsFile, cfg.WorkspaceDir, "TOOLS.md", agent.DefaultToolNotes),
			IdentityText:           loadBootstrapFile(cfg.IdentityFile, cfg.WorkspaceDir, "IDENTITY.md", ""),
			StaticMemory:           loadBootstrapFile(cfg.MemoryFile, cfg.WorkspaceDir, "MEMORY.md", ""),
			HeartbeatTasksFile:     cfg.Heartbeat.TasksFile,
			BootstrapMaxChars:      cfg.BootstrapMaxChars,
			BootstrapTotalMaxChars: cfg.BootstrapTotalMaxChars,
			HistoryMax:             cfg.HistoryMax,
			VectorK:                cfg.VectorK,
			FTSK:                   cfg.FTSK,
			TopK:                   cfg.MemoryRetrieve,
			DocRetriever:           docRetriever,
			DocRetrieveLimit:       cfg.DocIndex.RetrieveLimit,
			WorkspaceDir:           cfg.WorkspaceDir,
		},
		Artifacts:          art,
		MaxToolBytes:       cfg.MaxToolBytes,
		MaxToolLoops:       cfg.MaxToolLoops,
		Deliver:            delivererFunc(channelManager.Deliver),
		DefaultScopeKey:    cfg.DefaultSessionKey,
		LinkDirectMessages: cfg.Session.DirectMessagesShareDefault,
		IdentityScopeMap:   buildIdentityScopeMap(cfg),
		Hardening:          cfg.Hardening,
		AccessProfiles:     cfg.Security.Profiles,
		Audit:              auditLogger,
	}
	var serviceJobs *agent.JobRegistry
	if cmd == "service" {
		serviceJobs = agent.NewJobRegistry(0, 0)
		rt.Deliver = nil
		rt.Streamer = nil
	}

	// cron service + tool
	if cfg.Cron.Enabled {
		cronSvc = cron.New(cfg.Cron.StorePath, agent.CronRunner(b, cfg.DefaultSessionKey))
		if err := cronSvc.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "cron start error:", err)
			os.Exit(1)
		}
		rt.Tools = buildRuntimeTools()
	}

	if enableSubagents {
		subagentManager = &agent.SubagentManager{
			DB:            d,
			Runtime:       rt,
			Deliver:       delivererFunc(channelManager.Deliver),
			MaxConcurrent: cfg.Subagents.MaxConcurrent,
			MaxQueued:     cfg.Subagents.MaxQueued,
			TaskTimeout:   time.Duration(cfg.Subagents.TaskTimeoutSeconds) * time.Second,
			Jobs:          serviceJobs,
			BackgroundTools: func() *tools.Registry {
				return buildBackgroundTools()
			},
		}
		if cmd == "service" {
			subagentManager.Deliver = nil
		}
		if err := subagentManager.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "subagent manager error:", err)
			os.Exit(1)
		}
		rt.Tools = buildRuntimeTools()
	}
	if cfg.ConsolidationEnabled {
		rt.Consolidator = &memory.Consolidator{
			DB:                 d,
			Provider:           prov,
			EmbedModel:         cfg.Provider.EmbedModel,
			ChatModel:          cfg.Provider.Model,
			WindowSize:         cfg.ConsolidationWindowSize,
			MaxMessages:        cfg.ConsolidationMaxMessages,
			MaxInputChars:      cfg.ConsolidationMaxInputChars,
			CanonicalPinnedKey: "long_term_memory",
		}
		rt.ConsolidationScheduler = memory.NewSchedulerWithContext(
			ctx,
			time.Duration(cfg.ConsolidationAsyncTimeoutSeconds)*time.Second,
			func(runCtx context.Context, sessionKey string) {
				historyMax := cfg.HistoryMax
				if historyMax <= 0 {
					historyMax = 40
				}
				for i := 0; i < schedulerMaxConsolidationPasses; i++ {
					didWork, err := rt.Consolidator.RunOnce(runCtx, sessionKey, historyMax, memory.RunMode{})
					if err != nil {
						log.Printf("consolidation failed: session=%s err=%v", sessionKey, err)
						return
					}
					if !didWork {
						return
					}
				}
			},
		)
	}

	var heartbeatSvc *heartbeat.Service
	switch cmd {
	case "chat":
		rt.Streamer = del
		_ = channelManager.Start(ctx, "cli", b)
		runWorkers(ctx, b, rt, cfg.WorkerCount)
		ch := &cli.Channel{Bus: b, SessionKey: cfg.DefaultSessionKey, Spinner: spinner}
		if err := ch.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "cli error:", err)
		}
	case "serve":
		runWorkers(ctx, b, rt, cfg.WorkerCount)
		if err := channelManager.StartAll(ctx, b); err != nil {
			fmt.Fprintln(os.Stderr, "channel start error:", err)
			os.Exit(1)
		}
		// start webhook server if configured
		webhookSrv := triggers.NewWebhookServer(cfg.Triggers.Webhook, b, cfg.DefaultSessionKey)
		if err := webhookSrv.Start(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "webhook start error:", err)
			os.Exit(1)
		}
		defer webhookSrv.Stop(context.Background())
		// start file watcher if configured
		fileWatcher := triggers.NewFileWatcher(cfg.Triggers.FileWatch, b, cfg.DefaultSessionKey)
		fileWatcher.Start(ctx)
		defer fileWatcher.Stop()
		heartbeatSvc = heartbeatServiceForCommand(cmd, cfg, b)
		if heartbeatSvc != nil {
			heartbeatSvc.Start(ctx)
		}
		fmt.Println("or3-intern serve: channels running. Ctrl+C to stop.")
		<-ctx.Done()
	case "service":
		runWorkers(ctx, b, rt, cfg.WorkerCount)
		if err := runServiceCommand(ctx, cfg, rt, subagentManager, serviceJobs); err != nil {
			fmt.Fprintln(os.Stderr, "service error:", err)
			os.Exit(1)
		}
	case "agent":
		// one-shot: or3-intern agent -m "hello"
		fs := flag.NewFlagSet("agent", flag.ExitOnError)
		var msg string
		var session string
		fs.StringVar(&msg, "m", "", "message")
		fs.StringVar(&session, "s", cfg.DefaultSessionKey, "session key")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(msg) == "" {
			fmt.Fprintln(os.Stderr, "missing -m message")
			os.Exit(2)
		}
		if err := rt.Handle(ctx, bus.Event{Type: bus.EventUserMessage, SessionKey: session, Channel: "cli", From: "local", Message: msg}); err != nil {
			fmt.Fprintln(os.Stderr, "agent error:", err)
			os.Exit(1)
		}
	case "migrate-jsonl":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: or3-intern migrate-jsonl <jsonl_path> [session_key]")
			os.Exit(2)
		}
		sessionKey := "migrated:default"
		if len(args) >= 3 {
			sessionKey = args[2]
		}
		if err := migrateJSONL(ctx, d, args[1], sessionKey); err != nil {
			fmt.Fprintln(os.Stderr, "migration error:", err)
			os.Exit(1)
		}
		fmt.Println("ok")
	case "version":
		fmt.Println("or3-intern v1")
	case "skills":
		deps := skillsCommandDeps{
			Client: newClawHubClient(cfg),
			LoadToolNames: func(ctx context.Context, cfg config.Config) map[string]struct{} {
				return loadAvailableToolNamesWithManager(ctx, cfg, nil)
			},
			LoadInventory: func(toolNames map[string]struct{}) skills.Inventory {
				return buildSkillsInventory(cfg, builtin, toolNames)
			},
			Audit: func(ctx context.Context, eventType string, payload any) error {
				if auditLogger == nil {
					return nil
				}
				return auditLogger.Record(ctx, eventType, "", "cli", payload)
			},
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		}
		if err := runSkillsCommandWithDeps(ctx, cfg, args[1:], deps); err != nil {
			fmt.Fprintln(os.Stderr, "skills error:", err)
			os.Exit(1)
		}
	case "scope":
		// or3-intern scope link <session-key> <scope-key>
		// or3-intern scope list <scope-key>
		fs := flag.NewFlagSet("scope", flag.ExitOnError)
		_ = fs.Parse(args[1:])
		scopeArgs := fs.Args()
		if len(scopeArgs) < 1 {
			fmt.Fprintln(os.Stderr, "usage: or3-intern scope <link|list> ...")
			os.Exit(2)
		}
		switch scopeArgs[0] {
		case "link":
			if len(scopeArgs) < 3 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope link <session-key> <scope-key>")
				os.Exit(2)
			}
			if err := d.LinkSession(ctx, scopeArgs[1], scopeArgs[2], nil); err != nil {
				fmt.Fprintln(os.Stderr, "scope link error:", err)
				os.Exit(1)
			}
			fmt.Printf("Linked session %q -> scope %q\n", scopeArgs[1], scopeArgs[2])
		case "list":
			if len(scopeArgs) < 2 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope list <scope-key>")
				os.Exit(2)
			}
			sessions, err := d.ListScopeSessions(ctx, scopeArgs[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "scope list error:", err)
				os.Exit(1)
			}
			if len(sessions) == 0 {
				fmt.Println("(no sessions linked to scope)")
			} else {
				for _, s := range sessions {
					fmt.Println(s)
				}
			}
		case "resolve":
			if len(scopeArgs) < 2 {
				fmt.Fprintln(os.Stderr, "usage: or3-intern scope resolve <session-key>")
				os.Exit(2)
			}
			scopeKey, err := d.ResolveScopeKey(ctx, scopeArgs[1])
			if err != nil {
				fmt.Fprintln(os.Stderr, "scope resolve error:", err)
				os.Exit(1)
			}
			fmt.Println(scopeKey)
		default:
			fmt.Fprintln(os.Stderr, "unknown scope subcommand:", scopeArgs[0])
			os.Exit(2)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		os.Exit(2)
	}

	if heartbeatSvc != nil {
		heartbeatSvc.Stop()
	}
	if mcpManager != nil {
		if err := mcpManager.Close(); err != nil {
			log.Printf("mcp shutdown failed: %v", err)
		}
	}
	if cronSvc != nil {
		cronSvc.Stop()
	}
	if subagentManager != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
		if err := subagentManager.Stop(shutdownCtx); err != nil {
			log.Printf("subagent manager stop failed: %v", err)
		}
		cancel()
	}
	_ = channelManager.StopAll(context.Background())
}

func subagentsEnabledForCommand(cmd string, cfg config.Config) bool {
	if !cfg.Subagents.Enabled {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(cmd)) {
	case "service", "chat", "serve":
		return true
	default:
		return false
	}
}

func buildIdentityScopeMap(cfg config.Config) map[string]string {
	out := map[string]string{}
	for _, link := range cfg.Session.IdentityLinks {
		canonical := strings.TrimSpace(link.Canonical)
		if canonical == "" {
			continue
		}
		for _, peer := range link.Peers {
			peer = strings.TrimSpace(peer)
			if peer == "" {
				continue
			}
			out[peer] = canonical
		}
	}
	return out
}

type delivererFunc func(ctx context.Context, channel, to, text string) error

func (f delivererFunc) Deliver(ctx context.Context, channel, to, text string) error {
	return f(ctx, channel, to, text)
}

type mcpToolRegistrar interface {
	RegisterTools(reg *tools.Registry) int
}

func buildToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer, mcpRegistrar mcpToolRegistrar) *tools.Registry {
	return buildToolRegistryWithOptions(cfg, d, prov, channelManager, inv, cronSvc, spawnManager, mcpRegistrar, true)
}

func buildBackgroundToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, mcpRegistrar mcpToolRegistrar) *tools.Registry {
	return buildToolRegistryWithOptions(cfg, d, prov, channelManager, inv, cronSvc, nil, mcpRegistrar, false)
}

func buildToolRegistryWithOptions(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer, mcpRegistrar mcpToolRegistrar, includeSendMessage bool) *tools.Registry {
	reg := tools.NewRegistry()
	fileRoot := allowedRoot(cfg)
	sandboxCfg := tools.BubblewrapConfig{Enabled: cfg.Hardening.Sandbox.Enabled, BubblewrapPath: cfg.Hardening.Sandbox.BubblewrapPath, AllowNetwork: cfg.Hardening.Sandbox.AllowNetwork, WritablePaths: append([]string{}, cfg.Hardening.Sandbox.WritablePaths...)}
	hostPolicy := buildHostPolicy(cfg)
	reg.Register(&tools.ExecTool{Timeout: time.Duration(cfg.Tools.ExecTimeoutSeconds) * time.Second, RestrictDir: fileRoot, PathAppend: cfg.Tools.PathAppend, AllowedPrograms: append([]string{}, cfg.Hardening.ExecAllowedPrograms...), ChildEnvAllowlist: append([]string{}, cfg.Hardening.ChildEnvAllowlist...), Sandbox: sandboxCfg, EnableLegacyShell: cfg.Hardening.EnableExecShell})
	reg.Register(&tools.ReadFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.EditFile{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.ListDir{FileTool: tools.FileTool{Root: fileRoot}})
	reg.Register(&tools.WebFetch{HostPolicy: hostPolicy})
	reg.Register(&tools.WebSearch{APIKey: cfg.Tools.BraveAPIKey, HostPolicy: hostPolicy})
	reg.Register(&tools.MemorySetPinned{DB: d})
	reg.Register(&tools.MemoryAddNote{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel})
	reg.Register(&tools.MemorySearch{DB: d, Provider: prov, EmbedModel: cfg.Provider.EmbedModel, VectorK: cfg.VectorK, FTSK: cfg.FTSK, TopK: cfg.MemoryRetrieve, VectorScanLimit: cfg.VectorScanLimit})
	reg.Register(&tools.MemoryRecent{DB: d, DefaultLimit: 10, MaxLimit: cfg.HistoryMax, MaxChars: 240})
	reg.Register(&tools.MemoryGetPinned{DB: d, MaxChars: 400})
	if includeSendMessage {
		reg.Register(&tools.SendMessage{
			Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
				if channelManager == nil {
					return fmt.Errorf("channel manager not configured")
				}
				return channelManager.DeliverWithMeta(ctx, channel, to, text, meta)
			},
			AllowedRoot:   fileRoot,
			ArtifactsDir:  cfg.ArtifactsDir,
			MaxMediaBytes: cfg.MaxMediaBytes,
		})
	}
	if inv != nil {
		reg.Register(&tools.ReadSkill{Inventory: inv})
		reg.Register(&tools.RunSkillScript{Inventory: inv, Enabled: cfg.Skills.EnableExec, Timeout: time.Duration(cfg.Skills.MaxRunSeconds) * time.Second, ChildEnvAllowlist: append([]string{}, cfg.Hardening.ChildEnvAllowlist...), Sandbox: sandboxCfg})
	}
	if cronSvc != nil {
		reg.Register(&tools.CronTool{Svc: cronSvc})
	}
	if spawnManager != nil {
		reg.Register(&tools.SpawnSubagent{Manager: spawnManager})
	}
	if mcpRegistrar != nil {
		mcpRegistrar.RegisterTools(reg)
	}
	return reg
}

func buildChannelManager(cfg config.Config, cliDeliverer cli.Deliverer, art *artifacts.Store, maxMediaBytes int) (*rootchannels.Manager, error) {
	mgr := rootchannels.NewManager()
	if err := mgr.Register(cli.Service{Deliverer: cliDeliverer}); err != nil {
		return nil, err
	}
	if cfg.Channels.Telegram.Enabled {
		if err := mgr.Register(&telegram.Channel{Config: cfg.Channels.Telegram, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Slack.Enabled {
		if err := mgr.Register(&slack.Channel{Config: cfg.Channels.Slack, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Discord.Enabled {
		if err := mgr.Register(&discord.Channel{Config: cfg.Channels.Discord, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.WhatsApp.Enabled {
		cfg.Channels.WhatsApp.BridgeURL = whatsapp.BridgeURL(cfg.Channels.WhatsApp.BridgeURL)
		if err := mgr.Register(&whatsapp.Channel{Config: cfg.Channels.WhatsApp, Artifacts: art, MaxMediaBytes: maxMediaBytes, IsolatePeers: cfg.Hardening.IsolateChannelPeers}); err != nil {
			return nil, err
		}
	}
	if cfg.Channels.Email.Enabled {
		var database *db.DB
		if art != nil {
			database = art.DB
		}
		if err := mgr.Register(&email.Channel{Config: cfg.Channels.Email, DB: database}); err != nil {
			return nil, err
		}
	}
	return mgr, nil
}

func cfgPathOrDefault(p string) string {
	if p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".or3-intern", "config.json")
}

func allowedRoot(cfg config.Config) string {
	if cfg.Tools.RestrictToWorkspace {
		if cfg.WorkspaceDir != "" {
			return cfg.WorkspaceDir
		}
	}
	if cfg.AllowedDir != "" {
		return cfg.AllowedDir
	}
	return ""
}

func heartbeatServiceForCommand(cmd string, cfg config.Config, eventBus *bus.Bus) *heartbeat.Service {
	if cmd != "serve" || !cfg.Heartbeat.Enabled {
		return nil
	}
	return heartbeat.New(cfg.Heartbeat, cfg.WorkspaceDir, eventBus)
}

func runWorkers(ctx context.Context, b *bus.Bus, rt *agent.Runtime, n int) {
	if n <= 0 {
		n = 4
	}
	for i := 0; i < n; i++ {
		go func() {
			for ev := range b.Channel() {
				cctx, cancel := agent.WithTimeout(ctx, 120)
				if err := rt.Handle(cctx, ev); err != nil {
					log.Printf("handle event failed: type=%s session=%s err=%v", ev.Type, ev.SessionKey, err)
				}
				cancel()
			}
		}()
	}
}

func loadBootstrapFile(configPath, workspaceDir, baseName, fallback string) string {
	paths := []string{}
	if strings.TrimSpace(workspaceDir) != "" {
		paths = append(paths,
			filepath.Join(workspaceDir, baseName),
			filepath.Join(workspaceDir, strings.ToLower(baseName)),
		)
	}
	if strings.TrimSpace(configPath) != "" {
		paths = append(paths, configPath)
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return fallback
}

func ensureFileIfMissing(path, content string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644)
}
````
