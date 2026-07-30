package runners

// RunnerMemoryDebug records which memory sources were included in a runner turn.
// Values are flags only; raw memory text must never be stored here.
type RunnerMemoryDebug struct {
	PassiveCompiled   bool `json:"passive_compiled"`
	NativeRefresh     bool `json:"native_refresh"`
	PinnedNonEmpty    bool `json:"pinned_non_empty"`
	RetrievedNonEmpty bool `json:"retrieved_non_empty"`
	DigestNonEmpty    bool `json:"digest_non_empty"`
	DocsNonEmpty      bool `json:"docs_non_empty"`
}
