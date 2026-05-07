package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"or3-intern/internal/config"
)

func TestProbeFindings_DoesNotCreateDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.sqlite")
	cfg := config.Default()
	cfg.DBPath = path

	findings := probeFindings(cfg, Options{Probe: true})
	if len(findings) != 1 {
		t.Fatalf("expected one probe finding, got %#v", findings)
	}
	if findings[0].ID != "probe.sqlite_open_failed" {
		t.Fatalf("expected sqlite probe failure, got %#v", findings)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected probe to avoid creating %q, stat err=%v", path, err)
	}
}

func TestCollectChannels_ReturnsAllFiveChannels(t *testing.T) {
	cfg := config.Default()
	channels := collectChannels(cfg)
	if len(channels) != 5 {
		t.Fatalf("expected 5 channels, got %d", len(channels))
	}
	names := make(map[string]bool)
	for _, ch := range channels {
		names[ch.Name] = true
	}
	for _, name := range []string{"telegram", "slack", "discord", "whatsapp", "email"} {
		if !names[name] {
			t.Errorf("expected channel %q in snapshot", name)
		}
	}
}

func TestOpenAccessChannelNames_EmptyByDefault(t *testing.T) {
	cfg := config.Default()
	names := openAccessChannelNames(cfg)
	if len(names) != 0 {
		t.Fatalf("expected no open access channels by default, got %v", names)
	}
}

func TestOpenAccessChannelNames_ReportsEnabled(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.OpenAccess = true
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.OpenAccess = true
	names := openAccessChannelNames(cfg)
	if len(names) != 2 {
		t.Fatalf("expected 2 open access channels, got %v", names)
	}
}

func TestChannelExposureFindings_PerChannel(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Telegram.Enabled = true
	cfg.Channels.Telegram.OpenAccess = true
	cfg.Channels.Discord.Enabled = true
	cfg.Channels.Discord.OpenAccess = true
	opts := Options{Mode: ModeAdvisory}
	findings := channelExposureFindings(cfg, opts)
	if len(findings) < 2 {
		t.Fatalf("expected at least 2 findings for open channels, got %d", len(findings))
	}
	areas := make(map[string]bool)
	for _, f := range findings {
		areas[f.Area] = true
	}
	if !areas["telegram"] {
		t.Error("expected telegram channel finding")
	}
	if !areas["discord"] {
		t.Error("expected discord channel finding")
	}
}

func TestChannelIngressFindings_InvalidIngress(t *testing.T) {
	cfg := config.Default()
	cfg.Channels.Slack.Enabled = true
	cfg.Channels.Slack.InboundPolicy = "" // no policy means requires allowlist or open access
	opts := Options{Mode: ModeStartupServe}
	findings := channelIngressFindings(cfg, opts)
	if len(findings) != 1 {
		t.Fatalf("expected 1 invalid ingress finding, got %d", len(findings))
	}
	if findings[0].ID != "channels.invalid_ingress" {
		t.Fatalf("expected channels.invalid_ingress, got %s", findings[0].ID)
	}
}
