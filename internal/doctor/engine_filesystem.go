package doctor

import (
	"os"
	"path/filepath"
	"strings"

	"or3-intern/internal/config"
)

func filesystemFindings(cfg config.Config, opts Options) []Finding {
	findings := []Finding{}
	if !cfg.Tools.RestrictToWorkspace {
		findings = append(findings, Finding{
			ID:       "filesystem.workspace_restriction_disabled",
			Area:     "filesystem",
			Severity: SeverityWarn,
			Summary:  "workspace restriction is disabled",
			Detail:   "File tools are not bounded to a workspace directory.",
			FixMode:  FixModeManual,
			FixHint:  "Enable workspace restriction or explicitly scope writable paths.",
		})
	}
	workspaceDir := strings.TrimSpace(cfg.WorkspaceDir)
	if cfg.Tools.RestrictToWorkspace && workspaceDir == "" {
		findings = append(findings, Finding{
			ID:       "filesystem.workspace_dir_empty",
			Area:     "filesystem",
			Severity: severityFor(opts.Mode, SeverityError, false),
			Summary:  "workspace restriction is enabled but workspaceDir is empty",
			Detail:   "Restricted file tools need a concrete workspace root.",
			FixMode:  FixModeManual,
			FixHint:  "Set tools.restrictToWorkspace=false or configure workspaceDir.",
		})
	}
	if cfg.Tools.RestrictToWorkspace && workspaceDir != "" {
		if _, err := os.Stat(workspaceDir); err != nil {
			findings = append(findings, Finding{
				ID:       "filesystem.workspace_dir_missing",
				Area:     "filesystem",
				Severity: severityFor(opts.Mode, SeverityWarn, false),
				Summary:  "workspaceDir does not exist on disk",
				Detail:   workspaceDir,
				FixMode:  FixModeManual,
				FixHint:  "Create the workspace directory or point workspaceDir at an existing path.",
			})
		}
	}
	dbDir := filepath.Dir(strings.TrimSpace(cfg.DBPath))
	if dbDir != "" {
		if _, err := os.Stat(dbDir); err != nil {
			findings = append(findings, Finding{
				ID:       "filesystem.db_parent_missing",
				Area:     "filesystem",
				Severity: SeverityWarn,
				Summary:  "database parent directory does not exist",
				Detail:   dbDir,
				FixMode:  FixModeAutomatic,
				FixHint:  "Create the database directory.",
			})
		}
	}
	artifactsDir := strings.TrimSpace(cfg.ArtifactsDir)
	if artifactsDir == "" {
		findings = append(findings, Finding{
			ID:       "filesystem.artifacts_dir_empty",
			Area:     "filesystem",
			Severity: severityFor(opts.Mode, SeverityError, false),
			Summary:  "artifacts directory is empty",
			Detail:   "Artifacts are needed for channels, media, and runtime outputs.",
			FixMode:  FixModeManual,
			FixHint:  "Set artifactsDir to an existing or creatable directory.",
		})
	} else if _, err := os.Stat(artifactsDir); err != nil {
		findings = append(findings, Finding{
			ID:       "filesystem.artifacts_dir_missing",
			Area:     "filesystem",
			Severity: SeverityWarn,
			Summary:  "artifacts directory does not exist",
			Detail:   artifactsDir,
			FixMode:  FixModeAutomatic,
			FixHint:  "Create the artifacts directory.",
		})
	}
	return findings
}
