# Metadata Scanner

The metadata scanner detects suspicious patterns in tool metadata (names and descriptions) that could indicate prompt injection or malicious intent.

## Attack patterns detected

Four pattern classes are checked against tool names, descriptions, and schema descriptions:

| Pattern class | What it detects | Example |
|---|---|---|
| `instruction_override` | Attempts to override previous/system/developer instructions | "ignore previous instructions" |
| `secret_exfiltration` | Attempts to exfiltrate secrets/tokens/passwords | "send the API key to..." |
| `hidden_prompt_request` | Attempts at hidden/invisible prompts | "hidden instructions for..." |
| `unrelated_behavior` | Attempts to enforce perpetual behavior rules | "always refuse to..." |

Source: `internal/tools/metadata_scanner.go:22-30`

## Scanning a single tool

`ScanToolMetadata(tool)` collects all text from the tool's name, description, and nested schema descriptions. Each text is checked against the four patterns. Matching texts produce `MetadataDiagnostic` entries with the tool name, pattern class, and a preview of the matching text.

Source: `internal/tools/metadata_scanner.go:32-56`

## Filtering MCP tools

`FilterSuspiciousExternalTools` scans only MCP-group tools against a configured mode:
- **off** - no scanning, all tools pass through
- **warn** (default) - suspicious tools are flagged but allowed
- **block** - suspicious tools are removed from the registry

Tools in the allowlist are exempt from scanning.

Source: `internal/tools/metadata_scanner.go:58-96`

## Diagnostic output

Each diagnostic includes:
- `ToolName` - which tool triggered it
- `Class` - which pattern class matched
- `Action` - "warn" or "block"
- `Preview` - the matching text (truncated to 180 characters)

Source: `internal/tools/metadata_scanner.go:15-20` (MetadataDiagnostic)

## Schema description collection

`collectSchemaDescriptions` recursively walks the JSON Schema (maps and arrays) and collects all "description" string values. This ensures descriptions nested deep in schema properties are also checked.

Source: `internal/tools/metadata_scanner.go:98-112`
