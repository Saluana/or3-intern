package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"or3-intern/internal/adminflow"
)

const (
	doctorToolNameDocsIndex   = "doctor_docs_index"
	doctorToolNameDocsSection = "doctor_docs_section"
)

var (
	doctorDocsIndexMu     sync.RWMutex
	doctorDocsIndexByRoot = map[string]*doctorDocsCorpus{}
)

type doctorDocsSection struct {
	Heading string
	Level   int
	Body    string
}

type doctorDocsPage struct {
	RelPath   string
	Title     string
	Category  string
	Content   string
	Sections  []doctorDocsSection
	Headings  []string
}

type doctorDocsCorpus struct {
	root  string
	pages []doctorDocsPage
}

func doctorDocsAliasBoost(relPath, query string) int {
	lowerPath := strings.ToLower(relPath)
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if lowerQuery == "" {
		return 0
	}
	type alias struct {
		terms []string
		paths []string
	}
	aliases := []alias{
		{terms: []string{"health", "readiness", "doctor"}, paths: []string{
			"user-guide/cli/health.md",
			"user-guide/cli/doctor.md",
			"user-guide/app-integration/readiness-health.md",
			"architecture/diagnostics/health-checks.md",
		}},
		{terms: []string{"approval", "approve", "deny", "quota"}, paths: []string{
			"user-guide/workflows/approval-workflow.md",
			"user-guide/app-integration/approvals.md",
			"architecture/service-api/approvals-endpoints.md",
			"architecture/security/approval-system.md",
		}},
		{terms: []string{"pair", "connect", "app", "bootstrap", "mobile"}, paths: []string{
			"user-guide/app-integration/or3-app-connection-guide.md",
			"user-guide/app-integration/bootstrap.md",
			"user-guide/app-integration/overview.md",
		}},
		{terms: []string{"runner", "external agent", "agent cli"}, paths: []string{
			"user-guide/app-integration/runner-chat.md",
			"user-guide/workflows/external-agent-runner-workflow.md",
			"architecture/external-agent-cli/runner-registry.md",
		}},
		{terms: []string{"provider", "api key", "model", "openai"}, paths: []string{
			"getting-started/configuration-basics.md",
			"reference/config-reference.md",
		}},
		{terms: []string{"config", "setting", "configure"}, paths: []string{
			"reference/config-reference.md",
			"architecture/config/overview.md",
			"user-guide/cli/configure.md",
		}},
		{terms: []string{"tool", "exec", "mcp", "skill"}, paths: []string{
			"reference/tool-reference.md",
			"architecture/tools/overview.md",
			"architecture/tools/registry.md",
		}},
		{terms: []string{"service", "api", "internal"}, paths: []string{
			"architecture/service-api/overview.md",
			"architecture/service-api/routing.md",
			"getting-started/running-service-mode.md",
		}},
		{terms: []string{"secure", "pairing", "webauthn", "passkey"}, paths: []string{
			"architecture/security/secure-connections/secure-connections-api.md",
			"user-guide/cli/pairing.md",
		}},
	}
	for _, item := range aliases {
		matched := false
		for _, term := range item.terms {
			if strings.Contains(lowerQuery, term) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, path := range item.paths {
			if strings.Contains(lowerPath, strings.ToLower(path)) {
				return 40
			}
		}
	}
	return 0
}

func doctorDocsCategory(relPath string) string {
	trimmed := strings.TrimPrefix(filepath.ToSlash(relPath), "docs/v1/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		return "root"
	}
	if parts[0] == trimmed {
		return "root"
	}
	return parts[0]
}

func parseDoctorDocsSections(content string) []doctorDocsSection {
	lines := strings.Split(content, "\n")
	sections := []doctorDocsSection{}
	var current *doctorDocsSection
	flush := func() {
		if current == nil {
			return
		}
		current.Body = strings.TrimSpace(current.Body)
		if current.Heading != "" || current.Body != "" {
			sections = append(sections, *current)
		}
		current = nil
	}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			level := 0
			for _, r := range trimmed {
				if r == '#' {
					level++
					continue
				}
				break
			}
			if level >= 2 && level <= 4 {
				flush()
				heading := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
				current = &doctorDocsSection{Heading: heading, Level: level}
				continue
			}
		}
		if current != nil {
			if current.Body != "" {
				current.Body += "\n"
			}
			current.Body += line
		}
	}
	flush()
	return sections
}

func loadDoctorDocsCorpus(ctx context.Context, docsDir string) (*doctorDocsCorpus, error) {
	docsDir = strings.TrimSpace(docsDir)
	if docsDir == "" {
		return nil, fmt.Errorf("docs directory is required")
	}
	doctorDocsIndexMu.RLock()
	if cached, ok := doctorDocsIndexByRoot[docsDir]; ok {
		doctorDocsIndexMu.RUnlock()
		return cached, nil
	}
	doctorDocsIndexMu.RUnlock()

	pages := []doctorDocsPage{}
	walkErr := filepath.WalkDir(docsDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 256*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		rel, _ := filepath.Rel(docsDir, path)
		rel = filepath.ToSlash(filepath.Join("docs", "v1", rel))
		sections := parseDoctorDocsSections(content)
		headings := make([]string, 0, len(sections))
		for _, section := range sections {
			if section.Heading != "" {
				headings = append(headings, section.Heading)
			}
		}
		pages = append(pages, doctorDocsPage{
			RelPath:  rel,
			Title:    doctorDocsTitle(content, rel),
			Category: doctorDocsCategory(rel),
			Content:  content,
			Sections: sections,
			Headings: headings,
		})
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].RelPath < pages[j].RelPath
	})
	corpus := &doctorDocsCorpus{root: docsDir, pages: pages}
	doctorDocsIndexMu.Lock()
	doctorDocsIndexByRoot[docsDir] = corpus
	doctorDocsIndexMu.Unlock()
	return corpus, nil
}

func (c *doctorDocsCorpus) search(query string, limit int) []map[string]any {
	if c == nil || limit <= 0 {
		return nil
	}
	terms := doctorSearchTerms(query)
	type scored struct {
		score  int
		result map[string]any
	}
	matches := []scored{}
	for _, page := range c.pages {
		score := doctorDocsScore(page.RelPath, page.Title, page.Content, terms, query)
		score += doctorDocsAliasBoost(page.RelPath, query)
		if score == 0 {
			continue
		}
		headings := page.Headings
		if len(headings) > 6 {
			headings = headings[:6]
		}
		matches = append(matches, scored{
			score: score,
			result: map[string]any{
				"path":     page.RelPath,
				"title":    adminflow.SanitizeForAI(page.Title),
				"category": page.Category,
				"snippet":  doctorDocsSnippet(page.Content, terms),
				"headings": headings,
				"score":    score,
			},
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return fmt.Sprint(matches[i].result["path"]) < fmt.Sprint(matches[j].result["path"])
		}
		return matches[i].score > matches[j].score
	})
	results := make([]map[string]any, 0, minInt(limit, len(matches)))
	for i, item := range matches {
		if i == limit {
			break
		}
		results = append(results, item.result)
	}
	return results
}

func (c *doctorDocsCorpus) indexSummary() map[string]any {
	if c == nil {
		return map[string]any{"categories": []map[string]any{}, "total_pages": 0}
	}
	type catAgg struct {
		count  int
		titles []string
	}
	byCat := map[string]*catAgg{}
	for _, page := range c.pages {
		agg := byCat[page.Category]
		if agg == nil {
			agg = &catAgg{}
			byCat[page.Category] = agg
		}
		agg.count++
		if len(agg.titles) < 12 {
			agg.titles = append(agg.titles, page.Title)
		}
	}
	cats := make([]string, 0, len(byCat))
	for name := range byCat {
		cats = append(cats, name)
	}
	sort.Strings(cats)
	items := make([]map[string]any, 0, len(cats))
	for _, name := range cats {
		agg := byCat[name]
		items = append(items, map[string]any{
			"name":        name,
			"page_count":  agg.count,
			"sample_docs": agg.titles,
		})
	}
	return map[string]any{
		"total_pages": len(c.pages),
		"categories":  items,
		"hint":        "Use doctor_docs_search for targeted lookup, then doctor_docs_section to read one section before answering.",
	}
}

func (c *doctorDocsCorpus) readSection(relPath, heading string, maxChars int) (map[string]any, error) {
	if c == nil {
		return nil, fmt.Errorf("documentation index unavailable")
	}
	relPath = strings.TrimSpace(filepath.ToSlash(relPath))
	heading = strings.TrimSpace(heading)
	if relPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	if maxChars <= 0 {
		maxChars = 4000
	}
	if maxChars > 12000 {
		maxChars = 12000
	}
	var page *doctorDocsPage
	for i := range c.pages {
		if c.pages[i].RelPath == relPath {
			page = &c.pages[i]
			break
		}
	}
	if page == nil {
		return nil, fmt.Errorf("documentation path not found: %s", relPath)
	}
	body := ""
	selectedHeading := heading
	if heading == "" {
		body = strings.TrimSpace(page.Content)
		selectedHeading = page.Title
	} else {
		lowerHeading := strings.ToLower(heading)
		for _, section := range page.Sections {
			if strings.EqualFold(strings.TrimSpace(section.Heading), heading) ||
				strings.Contains(strings.ToLower(section.Heading), lowerHeading) {
				body = section.Body
				selectedHeading = section.Heading
				break
			}
		}
		if body == "" {
			return nil, fmt.Errorf("section %q not found in %s", heading, relPath)
		}
	}
	body = adminflow.SanitizeForAI(body)
	truncated := false
	if len(body) > maxChars {
		body = strings.TrimSpace(body[:maxChars]) + "..."
		truncated = true
	}
	return map[string]any{
		"path":             page.RelPath,
		"title":            adminflow.SanitizeForAI(page.Title),
		"heading":          adminflow.SanitizeForAI(selectedHeading),
		"content":          body,
		"truncated":        truncated,
		"available_headings": page.Headings,
	}, nil
}

func (s *serviceServer) loadDoctorDocsCorpus(ctx context.Context) (*doctorDocsCorpus, error) {
	docsDir, err := doctorDocsV1Dir(s.configPath)
	if err != nil {
		return nil, err
	}
	return loadDoctorDocsCorpus(ctx, docsDir)
}

func (s *serviceServer) executeDoctorDocsIndexTool(ctx context.Context, params map[string]any) (string, error) {
	_ = params
	corpus, err := s.loadDoctorDocsCorpus(ctx)
	if err != nil {
		return encodeDoctorToolResult(doctorToolNameDocsIndex, false, "OR3 v1 documentation is not available on this host.", map[string]any{"error": err.Error()}), nil
	}
	stats := corpus.indexSummary()
	summary := fmt.Sprintf("Indexed %d OR3 v1 documentation pages across %d categories.", stats["total_pages"], len(stats["categories"].([]map[string]any)))
	return encodeDoctorToolResult(doctorToolNameDocsIndex, true, summary, stats), nil
}

func (s *serviceServer) executeDoctorDocsSectionTool(ctx context.Context, params map[string]any) (string, error) {
	path := strings.TrimSpace(doctorToolString(params, "path"))
	heading := strings.TrimSpace(doctorToolString(params, "heading"))
	maxChars := doctorToolInt(params, "max_chars", 4000)
	corpus, err := s.loadDoctorDocsCorpus(ctx)
	if err != nil {
		return encodeDoctorToolResult(doctorToolNameDocsSection, false, "OR3 v1 documentation is not available on this host.", map[string]any{"path": path, "error": err.Error()}), nil
	}
	section, err := corpus.readSection(path, heading, maxChars)
	if err != nil {
		return encodeDoctorToolResult(doctorToolNameDocsSection, false, err.Error(), map[string]any{"path": path, "heading": heading, "error": err.Error()}), nil
	}
	summary := fmt.Sprintf("Read documentation section from %s.", path)
	if heading != "" {
		summary = fmt.Sprintf("Read section %q from %s.", heading, path)
	}
	return encodeDoctorToolResult(doctorToolNameDocsSection, true, summary, section), nil
}

// doctorSearchTermsTokenize exported for tests in same package.
func doctorSearchTermsTokenize(query string) []string {
	return doctorSearchTerms(query)
}

func doctorDocsHeadingMatchScore(heading, query string) int {
	terms := doctorSearchTerms(query)
	if len(terms) == 0 {
		return 0
	}
	lower := strings.ToLower(heading)
	score := 0
	for _, term := range terms {
		if strings.Contains(lower, term) {
			score += 4
		}
	}
	return score
}
