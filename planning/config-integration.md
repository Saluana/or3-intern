Audit Report: Config & Integration Consistency
🔴 HIGH — Potential Runtime Failures

1. Silent JSON marshal errors in provider client (4 sites)
   internal/providers/openai.go:219,379,483,530 — b, \_ := json.Marshal(req) discards marshal errors. If marshaling fails (extremely unlikely for these struct types, but possible with map[string]any content), an empty []byte or null body is sent to the provider API, resulting in opaque HTTP errors. Same pattern in 52 other files across the codebase (agentcli/manager.go, channels, tools, etc.).
2. Stream scanner buffer overflow is not retryable
   internal/providers/openai.go:412-413 — Scanner buffer is 1MB. If a single SSE line exceeds 1MB (bufio.ErrTooLong), the error is returned as-is at line 437. It's NOT a ProviderStreamError, so IsTransientError won't flag it, the stream → non-stream fallback won't trigger, and the stream will fail permanently even though the provider might accept a non-streaming retry.
3. OR3 runner has empty Binary in AllRunners but no special handling in detection
   internal/agentcli/runners.go:115-133 — RunnerOR3 has Binary: "" with all support flags false. In DetectAll/Detect, this will produce a "missing" status since no binary exists, but the Manager's Enqueue skips detection for OR3 (agentcli/manager.go:191). However the RunnerRegistry.Spec(RunnerOR3) still returns the spec that has empty Binary. If an adapter is somehow registered for OR3 and BuildCommand is called, the resulting CommandSpec.Binary would be empty.
   🟡 MEDIUM — Consistency & Integration Gaps
4. Two unrelated "ProviderProfile" types with different purposes

- config.ProviderProfileConfig (internal/config/types.go:274) — config-layer: API keys, models, timeout, vision
- providers.ProviderProfile (internal/providers/profile.go:11) — runtime-layer: schema sanitization, streaming policy, retry policy
  The runtime profile is selected SOLELY via SelectProviderProfile("", c.APIBase, model) (profile.go:104-109) which only uses the APIBase URL heuristic — the config's provider Name key (e.g., "openai", "openrouter") is NEVER passed in. This means you could name a provider "openrouter" but have it point to api.openai.com and get OpenAI profile behavior, or vice versa. The profile selection should use the config-level provider key.

5. ContextManagerConfig.Provider stores APIBase, not provider key
   internal/config/types.go:203 — Provider string in ContextManagerConfig is documented as a provider reference. But syncLegacyProviderFromRouting (routing.go:176-178) sets it to profile.APIBase (a URL like https://api.openai.com/v1), not the provider key. Meanwhile, ModelRoleConfig.Primary.Provider stores the key. This is a type confusion.
6. ExecTool registration gate misses the DisableShell check
   cmd/or3-intern/main.go:950-951 — ExecTool is registered with EnableLegacyShell: cfg.Hardening.EnableExecShell but shouldRegisterExecTool only checks cfg.Tools.EnableExec, allowlist length, and profile restrictions. It does NOT check cfg.Hardening.DisableShell. Actually, looking at ExecTool.Execute() (exec.go:157), it checks t.DisableShell separately, but the field DisableShell on the struct is never set from config — it's always the zero value false. The config field HardeningConfig.EnableExecShell controls whether the legacy command field works, but there's no separate DisableShell config field to populate ExecTool.DisableShell.
7. readinessStateFromIssues counts disabled MCP servers
   internal/config/readiness.go:186 — isAdvancedCustomReadiness uses len(cfg.Tools.MCPServers) > 0 which counts ALL server entries (including disabled ones). The hasRemoteHTTPMCPServers function (used for security checks) correctly filters for Enabled. This means a config with only disabled MCP servers will incorrectly be classified as ReadinessAdvancedCustom.
8. LocalCompatibleProfile defined but unreachable via heuristic
   internal/providers/profile.go:79-88 — LocalCompatibleProfile() is only selected when the combined APIBase+name+model string contains "ollama", "lmstudio", or "local". If someone runs a local OpenAI-compatible server (e.g., http://localhost:8080/v1) without those substrings, it gets OpenAICompatibleProfile which enables stream retry and fallback — potentially causing slow error recovery for local servers.
9. Duplicate validation between config and runtime for AgentCLI mode/isolation

- Config validation: validateAgentCLIConfig (config/validate.go:594) — checked at load time
- Runtime validation: ValidateRunPolicy (agentcli/registry.go:285) — checked at enqueue time
- The runtime check adds allowSandboxAuto flag constraint but the config check catches the mode+isolation mismatch. If the config passes validation but someone calls Enqueue with different mode/isolation, the runtime check applies. But there's an inconsistency: runtime allows IsolationSandboxWrite for review mode, while config validation doesn't express this constraint.

10. Stream textDelta overlap computation is O(n²) worst-case per chunk
    internal/providers/stream_assembler.go:168-174 — suffixPrefixOverlap scans up to len(accumulated_content) bytes for every snapshot-style chunk, looking for suffix/prefix overlap. For long first responses (e.g., 16KB of text), this runs per-SSE-chunk. In practice SSE chunks are small, but a provider that sends large snapshots could cause noticeable CPU usage.
    🟢 LOW — Code Quality & Maintenance
11. Dotenv file re-parsed on every Load() call
    internal/config/dotenv.go:12 — LoadDotEnv() is called at the top of Load() with no caching. Since os.Setenv persists, the re-parsing is redundant after the first load in a process. In short-lived CLI invocations this is fine, but in the long-running service mode, any internal config reload triggers redundant file I/O.
12. Config.IntegrationWarnings field with json:"-" tag
    internal/config/types.go:147 — IntegrationWarnings []IntegrationQuarantine \json:"-"\` is set by QuarantineInvalidOptionalIntegrations` but never persisted. If a user fixes a quarantined integration and reloads, the warnings are regenerated fresh (which is correct). But there's no way to inspect these warnings through the service API — they exist only in-memory and are silently consumed.
13. b, \_ := json.Marshal(...) pattern at internal/agentcli/manager.go:237 and manager.go:388,445
    These discard errors too. If map[string]any marshal fails (e.g., a channel, func, or infinite value sneaks in), metaJSON will be empty string or invalid JSON silently.
14. Adapter event normalization has significant copy-paste duplication
    internal/agentcli/chat_adapters.go — All four adapters (OpenCode, Codex, Claude, Gemini) have nearly identical NormalizeChatEvent implementations (lines 185-269), each with if raw.Type == "structured" / if raw.Type == "output" blocks. The extract/generic path is identical. Consider a shared normalizeRunnerChatEvent with per-runner structured parsers.
15. OpenCodeAdapter.BuildChatCommand ignores ContinuationMode
    internal/agentcli/chat_adapters.go:45-73 — OpenCodeAdapter.BuildChatCommand doesn't switch on ContinuationMode. It checks for NativeSessionRef to add --session flag, then uses replay prompt as fallback. But unlike Codex/Claude/Gemini, it doesn't bifurcate between ContinuationNative and ContinuationReplay. If ContinuationMode == ContinuationNative but NativeSessionRef is empty, it silently falls back to replay — correct behavior but no diagnostic.

---

Summary: The most actionable item is #4 (disconnected provider profiles) — the runtime schema/streaming profile should respect the config-level provider identity, not just the APIBase URL substring heuristic. Item #2 (scanner buffer overflow) is a real failure mode for providers that send very long single-line JSON events. Item #7 (readiness miscounts disabled MCP servers) is a minor UX bug. The b, \_ := json.Marshal(...) pattern (#1) is pervasive but low-risk since most types can't fail to marshal.
