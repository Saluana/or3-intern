package configmeta

import (
	"path"
	"strings"
	"sync"
)

// registry is the global config metadata registry.
var (
	registry            = make(map[string]ConfigFieldMetadata)
	registryByPath      = make(map[string]ConfigFieldMetadata)
	registryPatterns    []ConfigFieldMetadata
	registryPathPattern []ConfigFieldMetadata
	registryMu          sync.RWMutex
)

// Register adds metadata for a config field to the registry.
func Register(meta ConfigFieldMetadata) {
	registryMu.Lock()
	defer registryMu.Unlock()
	key := meta.Section + "." + meta.Key
	if hasWildcard(key) {
		registryPatterns = append(registryPatterns, meta)
	} else {
		registry[key] = meta
	}
	if meta.Path != "" {
		if hasWildcard(meta.Path) {
			registryPathPattern = append(registryPathPattern, meta)
		} else {
			registryByPath[meta.Path] = meta
		}
	}
}

// Get returns metadata for a config field by section and key.
func Get(section, key string) (ConfigFieldMetadata, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	meta, ok := registry[section+"."+key]
	if ok {
		return meta, true
	}
	return findPatternMatch(registryPatterns, section+"."+key, func(meta ConfigFieldMetadata) string {
		return meta.Section + "." + meta.Key
	})
}

// GetByPath returns metadata for a config field by its full path.
func GetByPath(path string) (ConfigFieldMetadata, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	meta, ok := registryByPath[path]
	if ok {
		return meta, true
	}
	return findPatternMatch(registryPathPattern, path, func(meta ConfigFieldMetadata) string {
		return meta.Path
	})
}

// List returns all registered fields.
func List() []ConfigFieldMetadata {
	registryMu.RLock()
	defer registryMu.RUnlock()
	result := make([]ConfigFieldMetadata, 0, len(registry)+len(registryPatterns))
	for _, meta := range registry {
		result = append(result, meta)
	}
	result = append(result, registryPatterns...)
	return result
}

// ListBySection returns all fields in a section.
func ListBySection(section string) []ConfigFieldMetadata {
	registryMu.RLock()
	defer registryMu.RUnlock()
	result := []ConfigFieldMetadata{}
	for _, meta := range registry {
		if meta.Section == section {
			result = append(result, meta)
		}
	}
	for _, meta := range registryPatterns {
		if meta.Section == section {
			result = append(result, meta)
		}
	}
	return result
}

// Clear removes all registered metadata (for testing).
func Clear() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]ConfigFieldMetadata)
	registryByPath = make(map[string]ConfigFieldMetadata)
	registryPatterns = nil
	registryPathPattern = nil
}

func hasWildcard(value string) bool {
	return strings.Contains(value, "*")
}

func findPatternMatch(items []ConfigFieldMetadata, target string, pattern func(ConfigFieldMetadata) string) (ConfigFieldMetadata, bool) {
	best := ConfigFieldMetadata{}
	bestLen := -1
	for _, item := range items {
		candidate := pattern(item)
		matched, err := path.Match(candidate, target)
		if err != nil || !matched {
			continue
		}
		if len(candidate) > bestLen {
			best = item
			bestLen = len(candidate)
		}
	}
	if bestLen < 0 {
		return ConfigFieldMetadata{}, false
	}
	return best, true
}
