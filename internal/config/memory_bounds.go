package config

import "fmt"

// Bounds for memory and document-index settings. Values outside these ranges are
// clamped on load and rejected by config edit APIs.
const (
	MinMemoryRetrieveLimit            = 1
	MaxMemoryRetrieveLimit            = 64
	MinVectorSearchK                  = 1
	MaxVectorSearchK                  = 64
	MinFTSSearchK                     = 1
	MaxFTSSearchK                     = 64
	MinVectorScanLimit                = 100
	MaxVectorScanLimit                = 10000
	MinHistoryMaxMessages             = 1
	MaxHistoryMaxMessages             = 500
	MinConsolidationWindowSize        = 1
	MaxConsolidationWindowSize        = 200
	MinConsolidationMaxMessages       = 1
	MaxConsolidationMaxMessages       = 500
	MinConsolidationMaxInputChars     = 1000
	MaxConsolidationMaxInputChars     = 200000
	MinDocIndexMaxFiles               = 1
	MaxDocIndexMaxFiles               = 5000
	MinDocIndexMaxFileBytes           = 1024
	MaxDocIndexMaxFileBytes           = 4 * 1024 * 1024
	MinDocIndexMaxChunks              = 1
	MaxDocIndexMaxChunks              = 20000
	MinDocIndexEmbedMaxBytes          = 0
	MaxDocIndexEmbedMaxBytes          = 64 * 1024
	MinDocIndexRetrieveLimit          = 1
	MaxDocIndexRetrieveLimit          = 50
)

func clampMemoryAndDocConfig(cfg *Config) {
	cfg.MemoryRetrieve = clampInt(cfg.MemoryRetrieve, MinMemoryRetrieveLimit, MaxMemoryRetrieveLimit, defaultMemoryRetrieveLimit)
	cfg.VectorK = clampInt(cfg.VectorK, MinVectorSearchK, MaxVectorSearchK, defaultVectorSearchK)
	cfg.FTSK = clampInt(cfg.FTSK, MinFTSSearchK, MaxFTSSearchK, defaultFTSSearchK)
	cfg.VectorScanLimit = clampInt(cfg.VectorScanLimit, MinVectorScanLimit, MaxVectorScanLimit, defaultVectorScanLimit)
	cfg.HistoryMax = clampInt(cfg.HistoryMax, MinHistoryMaxMessages, MaxHistoryMaxMessages, defaultHistoryMaxMessages)
	cfg.ConsolidationWindowSize = clampInt(cfg.ConsolidationWindowSize, MinConsolidationWindowSize, MaxConsolidationWindowSize, defaultConsolidationWindowSize)
	cfg.ConsolidationMaxMessages = clampInt(cfg.ConsolidationMaxMessages, MinConsolidationMaxMessages, MaxConsolidationMaxMessages, defaultConsolidationMaxMessages)
	cfg.ConsolidationMaxInputChars = clampInt(cfg.ConsolidationMaxInputChars, MinConsolidationMaxInputChars, MaxConsolidationMaxInputChars, defaultConsolidationMaxInputChars)
	cfg.DocIndex.MaxFiles = clampInt(cfg.DocIndex.MaxFiles, MinDocIndexMaxFiles, MaxDocIndexMaxFiles, 100)
	cfg.DocIndex.MaxFileBytes = clampInt(cfg.DocIndex.MaxFileBytes, MinDocIndexMaxFileBytes, MaxDocIndexMaxFileBytes, 64*1024)
	cfg.DocIndex.MaxChunks = clampInt(cfg.DocIndex.MaxChunks, MinDocIndexMaxChunks, MaxDocIndexMaxChunks, 500)
	cfg.DocIndex.EmbedMaxBytes = clampInt(cfg.DocIndex.EmbedMaxBytes, MinDocIndexEmbedMaxBytes, MaxDocIndexEmbedMaxBytes, 8*1024)
	cfg.DocIndex.RetrieveLimit = clampInt(cfg.DocIndex.RetrieveLimit, MinDocIndexRetrieveLimit, MaxDocIndexRetrieveLimit, 5)
}

func clampInt(value, min, max, fallback int) int {
	if value <= 0 {
		return fallback
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// ValidateMemoryIntField returns an error when value is outside the supported
// range for a memory or doc-index numeric setting.
func ValidateMemoryIntField(field string, value int) error {
	type bounds struct {
		min, max int
	}
	limits := map[string]bounds{
		"runtime_memory_retrieve":            {MinMemoryRetrieveLimit, MaxMemoryRetrieveLimit},
		"runtime_vector_k":                   {MinVectorSearchK, MaxVectorSearchK},
		"runtime_fts_k":                      {MinFTSSearchK, MaxFTSSearchK},
		"runtime_vector_scan_limit":          {MinVectorScanLimit, MaxVectorScanLimit},
		"runtime_history_max":                {MinHistoryMaxMessages, MaxHistoryMaxMessages},
		"runtime_consolidation_window":       {MinConsolidationWindowSize, MaxConsolidationWindowSize},
		"runtime_consolidation_max_messages": {MinConsolidationMaxMessages, MaxConsolidationMaxMessages},
		"runtime_consolidation_max_input_chars": {MinConsolidationMaxInputChars, MaxConsolidationMaxInputChars},
		"docindex_max_files":                 {MinDocIndexMaxFiles, MaxDocIndexMaxFiles},
		"docindex_max_file_bytes":            {MinDocIndexMaxFileBytes, MaxDocIndexMaxFileBytes},
		"docindex_max_chunks":                {MinDocIndexMaxChunks, MaxDocIndexMaxChunks},
		"docindex_embed_max_bytes":           {MinDocIndexEmbedMaxBytes, MaxDocIndexEmbedMaxBytes},
		"docindex_retrieve_limit":            {MinDocIndexRetrieveLimit, MaxDocIndexRetrieveLimit},
	}
	limit, ok := limits[field]
	if !ok {
		return nil
	}
	if value < limit.min || value > limit.max {
		return fmt.Errorf("%s must be between %d and %d", field, limit.min, limit.max)
	}
	return nil
}
