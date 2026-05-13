package main

import (
	"net/http"
	"strings"
)

func writeServiceRequestWarnings(w http.ResponseWriter, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	w.Header().Set("X-Or3-Request-Warning", strings.Join(warnings, "; "))
}
