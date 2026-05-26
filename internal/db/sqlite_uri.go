package db

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// sqliteFileURI builds a SQLite file DSN with a safely escaped absolute path.
func sqliteFileURI(path string, query string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slashPath := filepath.ToSlash(abs)
	if !strings.HasPrefix(slashPath, "/") {
		// Windows drive paths: file:///C:/...
		slashPath = "/" + slashPath
	}
	escaped := (&url.URL{Path: slashPath}).EscapedPath()
	if escaped == "" {
		escaped = slashPath
	}
	if query != "" {
		return fmt.Sprintf("file:%s?%s", escaped, query)
	}
	return "file:" + escaped
}
