package adminflow

// RedactedValue is a config-value representation that hides secret
// material. Use it when persisting or rendering config values in
// diagnostic logs, audit payloads, or external sharing flows so the
// raw secret bytes never leave the process.
type RedactedValue struct {
	// Value is the original value (only set when isSecret was false at
	// the call site).
	Value any `json:"value,omitempty"`
	// Present is true if a non-empty value was supplied.
	Present bool `json:"present"`
	// Redacted is true when the original value was secret-shaped and
	// has been replaced with a presence/empty summary.
	Redacted bool `json:"redacted,omitempty"`
	// Summary is the human-readable description shown to operators.
	Summary string `json:"summary,omitempty"`
}
