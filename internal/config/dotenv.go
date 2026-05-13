package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// LoadDotEnv loads simple KEY=value entries from local .env files before
// normal environment overrides are applied. Existing environment variables win.
func LoadDotEnv() {
	if disabledByEnv("OR3_LOAD_DOTENV") {
		return
	}
	seen := map[string]struct{}{}
	for _, path := range dotenvCandidates() {
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		_ = loadDotEnvFile(path)
	}
}

func dotenvCandidates() []string {
	var paths []string
	if cwd, err := os.Getwd(); err == nil && strings.TrimSpace(cwd) != "" {
		paths = append(paths, filepath.Join(cwd, ".env"))
		if parent := filepath.Dir(cwd); parent != cwd {
			paths = append(paths, filepath.Join(parent, ".env"))
		}
	}
	return paths
}

func loadDotEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return scanner.Err()
}

func parseDotEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !ok || key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		quote := value[0]
		if (quote == '"' || quote == '\'') && value[len(value)-1] == quote {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, true
}

func disabledByEnv(key string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return value == "0" || value == "false" || value == "no" || value == "off"
}
