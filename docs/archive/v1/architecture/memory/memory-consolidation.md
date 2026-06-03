# Memory Consolidation

Memory consolidation is the process of summarizing old chat messages into durable, searchable memory notes. It is driven by an LLM call that extracts structured information from conversation transcripts.

Source: `internal/memory/consolidate.go`

## The Consolidator

The `Consolidator` struct (`consolidate.go:109-125`):

```go
type Consolidator struct {
    DB               *db.DB
    Provider         *providers.Client
    EmbedModel       string
    EmbedFingerprint string
    ChatModel        string
    WindowSize       int    // min messages before triggering (default: 10)
    MaxMessages      int    // max messages per pass (default: 50)
    MaxInputChars    int    // max transcript chars for LLM (default: 12000)
    CanonicalPinnedKey string  // pinned memory key (default: "long_term_memory")
}
```

## Entry Points

- **`MaybeConsolidate()`** (`consolidate.go:133`) — The main entry point. Called after every agent turn. It's a no-op when there aren't enough old messages.

- **`ArchiveAll()`** (`consolidate.go:139`) — Drains ALL unconsolidated messages in bounded passes (max 1024). Used for bulk archival.

- **`ArchiveResetWindow()`** (`consolidate.go:157`) — Archives the most recent session messages in one pass. Used for `/new` reset.

## RunOnce: Core Consolidation Loop

`RunOnce()` (`consolidate.go:212-310`) performs a single bounded consolidation pass:

1. **Set defaults** — WindowSize (10), MaxMessages (50), MaxInputChars (12000), historyMax (40).

2. **Find consolidation range** — Calls `GetConsolidationRange()` to get the last consolidated message ID and the oldest message in the active window. Messages between these IDs are candidates for consolidation.

3. **Fetch messages** — Calls `GetConsolidationMessages()` to get up to `MaxMessages` messages in the consolidation window.

4. **Build transcript** — `buildConsolidationTranscript()` (`consolidate.go:314-344`) converts messages to a text transcript. It skips `tool` role messages (noisy), skips empty content, and truncates at `MaxInputChars`.

5. **Check triggers** — Consolidation runs if:
   - `ArchiveAll` mode is active, OR
   - Message count >= `WindowSize`, OR
   - Message count >= `MaxMessages` (adaptive trigger), OR
   - Transcript length >= `MaxInputChars / 2` (adaptive trigger)

6. **LLM call** — `writeConsolidatedTranscript()` (`consolidate.go:359-436`) does the work:
   - Fetches the current canonical pinned memory
   - Calls the LLM with a system prompt and the conversation transcript
   - Parses the structured JSON output
   - Generates an embedding for the summary
   - Writes the summary note and typed sub-notes to the DB
   - Updates the canonical pinned memory
   - Advances the consolidation cursor

## Structured Output

The LLM is instructed to call the `record_consolidated_memory` tool exactly once. The tool schema requires (`consolidate.go:77-97`):

| Field | Type | Description |
|-------|------|-------------|
| `summary` | string | 3-5 sentences describing changes and decisions |
| `facts` | string[] | Stable project/user facts |
| `preferences` | string[] | How the user wants work done |
| `goals` | string[] | Active objectives |
| `procedures` | string[] | Repeatable steps, commands, runbooks |
| `decisions` | string[] | Choices that were accepted |
| `warnings` | string[] | Risks, pitfalls, failed approaches |

## Parsing the LLM Response

`parseConsolidationResponse()` (`consolidate.go:521-532`) finds the tool call in the LLM response. `parseConsolidationOutput()` (`consolidate.go:537-559`) handles:

- JSON extraction even if the model adds surrounding prose (via `extractJSON()` at line 613)
- Flexible parsing that tolerates single strings where arrays are expected
- Legacy `canonical_memory` field as a fallback preference
- Sanitization: items trimmed, capped at 400 chars each, max 10 per list

If the LLM fails to produce valid output, a retry is attempted (`consolidate.go:443-470`). If retry also fails, the cursor is advanced without a note (to avoid re-processing the same messages forever).

## Canonical Pinned Memory

`buildCanonicalPinnedText()` (`consolidate.go:659-708`) maintains a compact pinned memory string from the most durable item types (preferences and facts). It:
- Deduplicates by normalized line content
- Preserves existing items (up to 2000 chars)
- Caps the final string at 2500 chars

This pinned memory is stored in `memory_pinned` under the canonical key and included in future consolidation prompts so the LLM can avoid repeating itself.

## Written Notes

`buildExtraNotes()` (`consolidate.go:720-753`) converts the structured output into `TypedNoteInput` records. Each fact, preference, goal, procedure, decision, and warning gets its own row in `memory_notes` with the appropriate `kind` and tagged `"consolidation"`.

## Vector Profile Mismatch Handling

If writing a consolidation note fails due to vector dimension or fingerprint mismatch, `writeConsolidatedTranscript()` automatically triggers a rebuild via `RebuildMemoryVecIndexWithProfile()` and retries the write (`consolidate.go:420-427`).

## Stale Cleanup

After each successful consolidation write, `CleanupStaleMemoryNotes()` is called to mark old, never-used summary and episode notes as stale (`consolidate.go:432-434`). This uses a small batch size (20) to keep writes bounded.
