# Error Handling

Errors are grouped into categories. Each category has a specific meaning and recovery path.

## Error Categories

- **Config errors** — bad config file, missing API key, invalid paths, unsupported provider
- **Provider errors** — AI provider API is down, rate limited, auth failed, or returned an invalid response
- **Tool errors** — a command failed to run, a file was not found, a network request timed out
- **Safety errors** — an approval request was denied, a policy was violated, a tool was blocked
- **Internal errors** — bugs in the code, resource exhaustion, unexpected states

## How Errors Are Handled

Errors are logged with context. The log includes what happened, when, and which part of the system was affected.

The user gets an actionable message when possible. For example: "Your API key is missing. Set OR3_PROVIDER_API_KEY or run `or3-intern configure`."

## Error Recovery

Some errors can be recovered automatically:
- Rate limited — wait and retry
- Network timeout — retry with backoff
- Config missing — show the configure wizard

Other errors need user action:
- Invalid API key — update the config
- Permission denied — approve the tool or change the policy
- Database corruption — restore from backup

## Fatal Errors

Fatal errors stop the runtime. These include database corruption, config validation failure, and missing required dependencies. The runtime reports the error and exits.
