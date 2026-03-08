package memory

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultWorkspaceContextMaxFileBytes = 32 * 1024
	defaultWorkspaceContextMaxResults   = 6
	defaultWorkspaceContextMaxChars     = 6000
	defaultWorkspaceContextScanLimit    = 200
	workspaceContextCacheTTL            = 5 * time.Second
)

type workspaceContextCacheKey struct {
	root         string
	query        string
	maxFileBytes int
	maxResults   int
	maxChars     int
}

type workspaceContextCacheEntry struct {
	text      string
	expiresAt time.Time
}

var workspaceContextCache = struct {
	mu      sync.Mutex
	entries map[workspaceContextCacheKey]workspaceContextCacheEntry
}{entries: map[workspaceContextCacheKey]workspaceContextCacheEntry{}}

type WorkspaceContextConfig struct {
	WorkspaceDir string
	MaxFileBytes int
	MaxResults   int
	MaxChars     int
	Now          time.Time
}

type workspaceCandidate struct {
	Path    string
	Excerpt string
	Score   int
}

func BuildWorkspaceContext(cfg WorkspaceContextConfig, query string) string {
	root := strings.TrimSpace(cfg.WorkspaceDir)
	if root == "" {
		return ""
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return ""
	}
	maxFileBytes := cfg.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = defaultWorkspaceContextMaxFileBytes
	}
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultWorkspaceContextMaxResults
	}
	maxChars := cfg.MaxChars
	if maxChars <= 0 {
		maxChars = defaultWorkspaceContextMaxChars
	}
	cacheKey := workspaceContextCacheKey{
		root:         realRoot,
		query:        strings.TrimSpace(strings.ToLower(query)),
		maxFileBytes: maxFileBytes,
		maxResults:   maxResults,
		maxChars:     maxChars,
	}
	now := cfg.Now
	if now.IsZero() {
		now = time.Now()
	}
	if cached, ok := getWorkspaceContextCache(cacheKey, now); ok {
		return cached
	}

	seen := map[string]struct{}{}
	candidates := make([]workspaceCandidate, 0, maxResults)
	appendCandidate := func(candidate workspaceCandidate) {
		candidate.Path = strings.TrimSpace(candidate.Path)
		candidate.Excerpt = strings.TrimSpace(candidate.Excerpt)
		if candidate.Path == "" || candidate.Excerpt == "" {
			return
		}
		if _, exists := seen[candidate.Path]; exists {
			return
		}
		seen[candidate.Path] = struct{}{}
		candidates = append(candidates, candidate)
	}

	for _, name := range []string{"README.md", "TODO.md", "TASKS.md", "PLAN.md", "STATUS.md", "NOTES.md", "PROJECT.md"} {
		candidate, ok := workspaceFileCandidate(realRoot, filepath.Join(realRoot, name), maxFileBytes, nil)
		if ok {
			appendCandidate(candidate)
		}
	}
	for _, candidate := range recentMemoryCandidates(realRoot, now, maxFileBytes) {
		appendCandidate(candidate)
	}
	tokens := workspaceQueryTokens(query)
	if len(tokens) > 0 {
		for _, candidate := range relevantWorkspaceCandidates(realRoot, tokens, maxFileBytes, maxResults, seen) {
			appendCandidate(candidate)
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	var out strings.Builder
	out.WriteString("Startup workspace context gathered before the model call.\n")
	for i, candidate := range candidates {
		out.WriteString(fmt.Sprintf("%d) [%s] %s\n", i+1, relativeDisplayPath(realRoot, candidate.Path), workspaceOneLine(candidate.Excerpt, 320)))
	}
	text := workspaceTruncate(strings.TrimSpace(out.String()), maxChars)
	setWorkspaceContextCache(cacheKey, text, now)
	return text
}

func getWorkspaceContextCache(key workspaceContextCacheKey, now time.Time) (string, bool) {
	workspaceContextCache.mu.Lock()
	defer workspaceContextCache.mu.Unlock()
	entry, ok := workspaceContextCache.entries[key]
	if !ok {
		return "", false
	}
	if !entry.expiresAt.After(now) {
		delete(workspaceContextCache.entries, key)
		return "", false
	}
	return entry.text, true
}

func setWorkspaceContextCache(key workspaceContextCacheKey, text string, now time.Time) {
	workspaceContextCache.mu.Lock()
	defer workspaceContextCache.mu.Unlock()
	workspaceContextCache.entries[key] = workspaceContextCacheEntry{text: text, expiresAt: now.Add(workspaceContextCacheTTL)}
}

func recentMemoryCandidates(root string, now time.Time, maxFileBytes int) []workspaceCandidate {
	memoryDir := filepath.Join(root, "memory")
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		return nil
	}
	preferred := map[string]struct{}{
		now.Format("2006-01-02") + ".md":                    {},
		now.Add(-24*time.Hour).Format("2006-01-02") + ".md": {},
	}
	var selected []workspaceCandidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if _, ok := preferred[entry.Name()]; !ok {
			continue
		}
		candidate, ok := workspaceFileCandidate(root, filepath.Join(memoryDir, entry.Name()), maxFileBytes, nil)
		if ok {
			selected = append(selected, candidate)
		}
	}
	if len(selected) > 0 {
		sort.Slice(selected, func(i, j int) bool { return selected[i].Path < selected[j].Path })
		return selected
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: filepath.Join(memoryDir, entry.Name()), modTime: info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modTime.After(files[j].modTime) })
	if len(files) > 2 {
		files = files[:2]
	}
	out := make([]workspaceCandidate, 0, len(files))
	for _, file := range files {
		candidate, ok := workspaceFileCandidate(root, file.path, maxFileBytes, nil)
		if ok {
			out = append(out, candidate)
		}
	}
	return out
}

func relevantWorkspaceCandidates(root string, tokens []string, maxFileBytes, maxResults int, seen map[string]struct{}) []workspaceCandidate {
	var candidates []workspaceCandidate
	visited := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "artifacts" {
				return filepath.SkipDir
			}
			return nil
		}
		if visited >= defaultWorkspaceContextScanLimit {
			return fs.SkipAll
		}
		visited++
		if _, exists := seen[path]; exists {
			return nil
		}
		if !isWorkspaceContextFile(path) || isBootstrapWorkspaceFile(path) {
			return nil
		}
		candidate, ok := workspaceFileCandidate(root, path, maxFileBytes, tokens)
		if !ok || candidate.Score <= 0 {
			return nil
		}
		candidates = append(candidates, candidate)
		return nil
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Path < candidates[j].Path
		}
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > maxResults {
		candidates = candidates[:maxResults]
	}
	return candidates
}

func workspaceFileCandidate(root, path string, maxFileBytes int, tokens []string) (workspaceCandidate, bool) {
	resolved, ok := workspaceSafePath(root, path)
	if !ok {
		return workspaceCandidate{}, false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return workspaceCandidate{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxFileBytes)))
	if err != nil {
		return workspaceCandidate{}, false
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return workspaceCandidate{}, false
	}
	excerpt, score := workspaceExcerpt(resolved, text, tokens)
	if len(tokens) == 0 {
		score = 1
	}
	return workspaceCandidate{Path: resolved, Excerpt: excerpt, Score: score}, true
}

func workspaceSafePath(root, path string) (string, bool) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, realPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return realPath, true
}

func workspaceExcerpt(path, text string, tokens []string) (string, int) {
	one := workspaceOneLine(text, 500)
	if len(tokens) == 0 {
		return one, 0
	}
	lowerPath := strings.ToLower(path)
	lowerText := strings.ToLower(text)
	best := -1
	score := 0
	for _, token := range tokens {
		if strings.Contains(lowerPath, token) {
			score += 6
		}
		if idx := strings.Index(lowerText, token); idx >= 0 {
			score += 3
			if best < 0 || idx < best {
				best = idx
			}
		}
	}
	if best < 0 {
		return one, score
	}
	start := best - 120
	if start < 0 {
		start = 0
	}
	end := best + 220
	if end > len(text) {
		end = len(text)
	}
	excerpt := strings.TrimSpace(text[start:end])
	if start > 0 {
		excerpt = "…" + excerpt
	}
	if end < len(text) {
		excerpt += "…"
	}
	return workspaceOneLine(excerpt, 500), score
}

func workspaceQueryTokens(query string) []string {
	raw := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	})
	stop := map[string]struct{}{
		"the": {}, "and": {}, "for": {}, "with": {}, "that": {}, "this": {}, "from": {}, "into": {},
		"what": {}, "when": {}, "where": {}, "have": {}, "just": {}, "please": {}, "about": {},
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, token := range raw {
		if len(token) < 3 {
			continue
		}
		if _, blocked := stop[token]; blocked {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func isWorkspaceContextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt":
		return true
	default:
		return false
	}
}

func isBootstrapWorkspaceFile(path string) bool {
	name := strings.ToUpper(filepath.Base(path))
	switch name {
	case "SOUL.MD", "AGENTS.MD", "TOOLS.MD", "IDENTITY.MD", "MEMORY.MD", "HEARTBEAT.MD":
		return true
	default:
		return false
	}
}

func relativeDisplayPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func workspaceOneLine(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "…"
	}
	return s
}

func workspaceTruncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len(s) > max {
		return strings.TrimSpace(s[:max]) + "\n…[truncated]"
	}
	return s
}
