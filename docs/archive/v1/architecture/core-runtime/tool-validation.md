# Tool Validation

Tool validation (`internal/agent/tool_validation.go`) checks every tool call before it runs. This prevents bad input from reaching the tools.

## What Gets Checked

- **Argument types** — strings are strings, numbers are numbers, booleans are booleans
- **Required arguments** — all required arguments are present
- **Argument ranges** — values are within allowed ranges (e.g., max file size)
- **Security** — no injection attempts, no path traversal, no command injection

## Validation Process

1. The tool call arrives with arguments
2. The validator checks each argument against the tool's schema
3. If validation passes, the tool runs
4. If validation fails, the tool is rejected with a clear message

## Security Checks

Validation also checks for misuse. It looks for:
- Path traversal attempts (e.g., `../../../etc/passwd`)
- Command injection in tool arguments
- Excessively large inputs that could cause problems
- Unusual encoding or escape sequences

## Rejected Calls

When a tool call is rejected, the agent gets an error message. It can try again with corrected arguments or ask the user for help.
