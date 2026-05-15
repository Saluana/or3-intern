# Compatibility Helpers

The compatibility layer keeps old client field names working while preserving canonical v1 request shapes.

## Package

`internal/compat`

## Helpers

| Helper | Purpose |
| --- | --- |
| `FirstString(canonical, aliases...)` | Return the trimmed canonical value when present, otherwise the first non-empty alias |
| `FirstStringSlice(canonical, aliases...)` | Return a copy of the canonical slice when present, otherwise a copy of the first non-empty alias slice |

Canonical fields intentionally win when both canonical and alias fields are present.

## Service API Usage

Service request decoding accepts snake_case canonical fields and selected camelCase or legacy aliases for app compatibility. Examples include:

- `session_key` before `sessionKey` or `session_id`
- `parent_session_key` before aliases
- `approval_token` before approval-token aliases
- explicit `allowed_tools` / `restrict_tools` before tool-policy aliases

When a request sends conflicting canonical and alias values, the decoder can add `X-Or3-Request-Warning`. New clients should send snake_case only.

## Why This Exists

The service API is used by app releases that may not update in lockstep with the CLI binary. Compatibility helpers let OR3 Intern keep accepting older payloads while making the current contract clear and testable.
