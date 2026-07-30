// Package builtin_skills embeds first-party bundled skills shipped with or3-intern.
package builtin_skills

import "embed"

// FS contains the bundled skill tree (memory, runner, filesystem, etc.).
//
//go:embed all:*
var FS embed.FS
