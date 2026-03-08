package clawhub

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	apiSearch   = "/api/v1/search"
	apiResolve  = "/api/v1/resolve"
	apiDownload = "/api/v1/download"
	apiSkills   = "/api/v1/skills"
)

type Client struct {
	SiteURL     string
	RegistryURL string
	HTTP        *http.Client
}

type SearchResult struct {
	Slug        string
	DisplayName string
	Summary     string
	Version     string
	Score       float64
	UpdatedAt   int64
}

type SkillInfo struct {
	Slug            string
	DisplayName     string
	Summary         string
	LatestVersion   string
	SelectedVersion string
	Owner           string
}

type ResolveResult struct {
	MatchVersion  string
	LatestVersion string
}

type InstallOptions struct {
	Force bool
}

type InstallResult struct {
	Path        string
	Slug        string
	Version     string
	Fingerprint string
}

type SkillOrigin struct {
	Version          int    `json:"version"`
	Registry         string `json:"registry"`
	Slug             string `json:"slug"`
	InstalledVersion string `json:"installedVersion"`
	InstalledAt      int64  `json:"installedAt"`
	Fingerprint      string `json:"fingerprint"`
}

type InstalledSkill struct {
	Name     string
	Path     string
	Origin   SkillOrigin
	Modified bool
}

func New(siteURL, registryURL string) *Client {
	return &Client{
		SiteURL:     strings.TrimRight(siteURL, "/"),
		RegistryURL: strings.TrimRight(registryURL, "/"),
		HTTP:        &http.Client{Timeout: 15 * time.Second},
	}
}

func (c *Client) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	url := c.apiURL(apiSearch)
	url.RawQuery = queryString(map[string]string{
		"q":     strings.TrimSpace(query),
		"limit": intString(limit),
	})
	var response struct {
		Results []struct {
			Slug        string  `json:"slug"`
			DisplayName string  `json:"displayName"`
			Summary     string  `json:"summary"`
			Version     string  `json:"version"`
			Score       float64 `json:"score"`
			UpdatedAt   int64   `json:"updatedAt"`
		} `json:"results"`
	}
	if err := c.getJSON(ctx, url.String(), &response); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(response.Results))
	for _, item := range response.Results {
		results = append(results, SearchResult{
			Slug:        item.Slug,
			DisplayName: item.DisplayName,
			Summary:     item.Summary,
			Version:     item.Version,
			Score:       item.Score,
			UpdatedAt:   item.UpdatedAt,
		})
	}
	return results, nil
}

func (c *Client) Inspect(ctx context.Context, slug, version string) (SkillInfo, error) {
	slug = sanitizeSlug(slug)
	if slug == "" {
		return SkillInfo{}, fmt.Errorf("slug required")
	}
	var response struct {
		Skill *struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"displayName"`
			Summary     string `json:"summary"`
		} `json:"skill"`
		LatestVersion *struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
		Owner *struct {
			Handle      string `json:"handle"`
			DisplayName string `json:"displayName"`
		} `json:"owner"`
	}
	if err := c.getJSON(ctx, c.apiURL(apiSkills+"/"+slug).String(), &response); err != nil {
		return SkillInfo{}, err
	}
	if response.Skill == nil {
		return SkillInfo{}, fmt.Errorf("skill not found: %s", slug)
	}
	info := SkillInfo{
		Slug:        response.Skill.Slug,
		DisplayName: response.Skill.DisplayName,
		Summary:     response.Skill.Summary,
		LatestVersion: stringOr(response.LatestVersion, func(v *struct {
			Version string `json:"version"`
		}) string {
			return v.Version
		}),
		SelectedVersion: strings.TrimSpace(version),
		Owner:           ownerName(response.Owner),
	}
	if info.SelectedVersion == "" {
		info.SelectedVersion = info.LatestVersion
	}
	return info, nil
}

func (c *Client) Resolve(ctx context.Context, slug, fingerprint string) (ResolveResult, error) {
	slug = sanitizeSlug(slug)
	if slug == "" {
		return ResolveResult{}, fmt.Errorf("slug required")
	}
	url := c.apiURL(apiResolve)
	url.RawQuery = queryString(map[string]string{
		"slug":    slug,
		"version": "",
		"hash":    strings.TrimSpace(fingerprint),
	})
	var response struct {
		Match *struct {
			Version string `json:"version"`
		} `json:"match"`
		LatestVersion *struct {
			Version string `json:"version"`
		} `json:"latestVersion"`
	}
	if err := c.getJSON(ctx, url.String(), &response); err != nil {
		return ResolveResult{}, err
	}
	return ResolveResult{
		MatchVersion: stringOr(response.Match, func(v *struct {
			Version string `json:"version"`
		}) string {
			return v.Version
		}),
		LatestVersion: stringOr(response.LatestVersion, func(v *struct {
			Version string `json:"version"`
		}) string {
			return v.Version
		}),
	}, nil
}

func (c *Client) Download(ctx context.Context, slug, version string) ([]byte, error) {
	slug = sanitizeSlug(slug)
	if slug == "" {
		return nil, fmt.Errorf("slug required")
	}
	url := c.apiURL(apiDownload)
	url.RawQuery = queryString(map[string]string{
		"slug":    slug,
		"version": strings.TrimSpace(version),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, readHTTPError(resp)
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) Install(ctx context.Context, slug, version, destDir string, opts InstallOptions) (InstallResult, error) {
	info, err := c.Inspect(ctx, slug, version)
	if err != nil {
		return InstallResult{}, err
	}
	if strings.TrimSpace(info.SelectedVersion) == "" {
		return InstallResult{}, fmt.Errorf("could not resolve version for %s", slug)
	}
	zipBytes, err := c.Download(ctx, slug, info.SelectedVersion)
	if err != nil {
		return InstallResult{}, err
	}
	target := filepath.Join(destDir, sanitizeSlug(slug))
	if err := installZip(zipBytes, target, SkillOrigin{
		Version:          1,
		Registry:         c.RegistryURL,
		Slug:             sanitizeSlug(slug),
		InstalledVersion: info.SelectedVersion,
		InstalledAt:      time.Now().UnixMilli(),
	}, opts); err != nil {
		return InstallResult{}, err
	}
	origin, err := ReadOrigin(target)
	if err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Path:        target,
		Slug:        origin.Slug,
		Version:     origin.InstalledVersion,
		Fingerprint: origin.Fingerprint,
	}, nil
}

func installZip(zipBytes []byte, target string, origin SkillOrigin, opts InstallOptions) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if stat, err := os.Stat(target); err == nil && stat.IsDir() {
		if !opts.Force {
			modified, checkErr := LocalEdits(target)
			if checkErr != nil {
				return checkErr
			}
			if modified {
				return fmt.Errorf("local modifications detected: %s", target)
			}
		}
	} else if err == nil {
		return fmt.Errorf("target exists and is not a directory: %s", target)
	}

	tempRoot, err := os.MkdirTemp(filepath.Dir(target), ".clawhub-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempRoot)
	tempTarget := filepath.Join(tempRoot, filepath.Base(target))
	if err := extractZipToDir(zipBytes, tempTarget); err != nil {
		return err
	}
	fingerprint, err := FingerprintDir(tempTarget)
	if err != nil {
		return err
	}
	origin.Fingerprint = fingerprint
	if err := WriteOrigin(tempTarget, origin); err != nil {
		return err
	}

	backup := target + ".bak"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, backup); err != nil {
			return err
		}
	}
	if err := os.Rename(tempTarget, target); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = os.Rename(backup, target)
		}
		return err
	}
	_ = os.RemoveAll(backup)
	return nil
}

func extractZipToDir(zipBytes []byte, target string) error {
	reader, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	for _, file := range reader.File {
		rel, ok := safeZipPath(file.Name)
		if !ok {
			continue
		}
		full := filepath.Join(target, rel)
		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(full, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			return readErr
		}
		mode := file.Mode()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(full, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func FingerprintDir(root string) (string, error) {
	type item struct {
		path string
		sum  string
	}
	var files []item
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".clawhub" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		files = append(files, item{
			path: filepath.ToSlash(rel),
			sum:  hex.EncodeToString(sum[:]),
		})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	h := sha256.New()
	for _, file := range files {
		_, _ = io.WriteString(h, file.path+":"+file.sum+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func LocalEdits(skillDir string) (bool, error) {
	origin, err := ReadOrigin(skillDir)
	if err != nil {
		return false, err
	}
	current, err := FingerprintDir(skillDir)
	if err != nil {
		return false, err
	}
	return current != origin.Fingerprint, nil
}

func ListInstalled(root string) ([]InstalledSkill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]InstalledSkill, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		path := filepath.Join(root, entry.Name())
		origin, err := ReadOrigin(path)
		if err != nil {
			continue
		}
		modified, err := LocalEdits(path)
		if err != nil {
			return nil, err
		}
		out = append(out, InstalledSkill{
			Name:     entry.Name(),
			Path:     path,
			Origin:   origin,
			Modified: modified,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func ReadOrigin(skillDir string) (SkillOrigin, error) {
	data, err := os.ReadFile(filepath.Join(skillDir, ".clawhub", "origin.json"))
	if err != nil {
		return SkillOrigin{}, err
	}
	var origin SkillOrigin
	if err := json.Unmarshal(data, &origin); err != nil {
		return SkillOrigin{}, err
	}
	return origin, nil
}

func WriteOrigin(skillDir string, origin SkillOrigin) error {
	path := filepath.Join(skillDir, ".clawhub", "origin.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(origin, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func RemoveSkill(root, name string) error {
	name = sanitizeSlug(name)
	if name == "" {
		return fmt.Errorf("skill name required")
	}
	return os.RemoveAll(filepath.Join(root, name))
}

func (c *Client) apiURL(path string) *urlBuilder {
	return newURLBuilder(c.RegistryURL, path)
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (c *Client) getJSON(ctx context.Context, rawURL string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return readHTTPError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(dest)
}

func sanitizeSlug(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.Contains(slug, "..") || strings.Contains(slug, "/") || strings.Contains(slug, "\\") {
		return ""
	}
	return slug
}

func safeZipPath(path string) (string, bool) {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	if path == "" || strings.Contains(path, "..") {
		return "", false
	}
	return filepath.FromSlash(path), true
}

func readHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	text := strings.TrimSpace(string(body))
	if text == "" {
		text = resp.Status
	}
	return fmt.Errorf("clawhub API error: %s", text)
}

func queryString(values map[string]string) string {
	var parts []string
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, urlEncode(key)+"="+urlEncode(value))
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func intString(v int) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprint(v)
}

func ownerName(owner *struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
}) string {
	if owner == nil {
		return ""
	}
	if strings.TrimSpace(owner.Handle) != "" {
		return owner.Handle
	}
	return owner.DisplayName
}

func stringOr[T any](value *T, fn func(*T) string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fn(value))
}

type urlBuilder struct {
	base     string
	path     string
	RawQuery string
}

func newURLBuilder(base, path string) *urlBuilder {
	return &urlBuilder{
		base: strings.TrimRight(base, "/"),
		path: path,
	}
}

func (u *urlBuilder) String() string {
	if strings.TrimSpace(u.RawQuery) == "" {
		return u.base + u.path
	}
	return u.base + u.path + "?" + u.RawQuery
}

func urlEncode(s string) string {
	replacer := strings.NewReplacer(
		"%", "%25",
		" ", "%20",
		"!", "%21",
		"#", "%23",
		"$", "%24",
		"&", "%26",
		"'", "%27",
		"(", "%28",
		")", "%29",
		"+", "%2B",
		",", "%2C",
		"/", "%2F",
		":", "%3A",
		";", "%3B",
		"=", "%3D",
		"?", "%3F",
		"@", "%40",
	)
	return replacer.Replace(s)
}
