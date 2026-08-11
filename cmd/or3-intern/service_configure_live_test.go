package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"or3-intern/internal/app"
	"or3-intern/internal/approval"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/jobs"
)

func TestApplyLiveConfigRefreshesRunnerRuntime(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := config.Default()
	cfg.Runners.Default = "opencode"
	jobs := jobs.NewRegistry(time.Minute, 16)
	srv := &serviceServer{
		config:   cfg,
		database: database,
		jobs:     jobs,
		appSvc:   app.NewServiceAppWithRunnerTurns(cfg, jobs, nil, nil, nil),
	}
	srv.applyLiveConfig(cfg)
	if srv.runnerManager == nil || srv.chatManager == nil || srv.turnOrchestrator == nil {
		t.Fatalf("expected live runner runtime, got manager=%v chat=%v orchestrator=%v", srv.runnerManager, srv.chatManager, srv.turnOrchestrator)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = srv.runnerManager.Stop(stopCtx)
	})

	next := cfg
	next.Runners.Default = "codex"
	next.Runners.Disabled = []string{"gemini"}
	srv.applyLiveConfig(next)
	if got := srv.runnerManager.Cfg.Default; got != "codex" {
		t.Fatalf("expected default runner refreshed, got %q", got)
	}
	if len(srv.runnerManager.Cfg.Disabled) != 1 || srv.runnerManager.Cfg.Disabled[0] != "gemini" {
		t.Fatalf("expected disabled runners refreshed, got %#v", srv.runnerManager.Cfg.Disabled)
	}
}

func TestApplyLiveConfigRefreshesEmbeddingDependencies(t *testing.T) {
	database, err := db.Open(filepath.Join(t.TempDir(), "service.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	cfg := config.Default()
	cfg.Providers["embed-old"] = config.ProviderProfileConfig{
		APIBase:           "https://embed-old.example/v1",
		DefaultEmbedModel: "embed-old",
		TimeoutSeconds:    30,
	}
	cfg.ModelRouting.Embeddings.Primary = config.ModelRef{Provider: "embed-old", Model: "embed-old"}
	jobRegistry := jobs.NewRegistry(time.Minute, 16)
	srv := &serviceServer{
		config:        cfg,
		database:      database,
		jobs:          jobRegistry,
		runnerManager: buildRuntimeRunnerManager(cfg, database, jobRegistry),
	}
	srv.applyLiveConfig(cfg)

	next := config.Clone(cfg)
	next.Providers["embed-new"] = config.ProviderProfileConfig{
		APIBase:           "https://embed-new.example/v1",
		DefaultEmbedModel: "embed-new",
		TimeoutSeconds:    30,
	}
	next.ModelRouting.Embeddings.Primary = config.ModelRef{Provider: "embed-new", Model: "embed-new"}
	srv.applyLiveConfig(next)

	embedProvider := srv.serviceEmbedProvider()
	if embedProvider == nil || embedProvider.APIBase != "https://embed-new.example/v1" {
		t.Fatalf("expected embedding provider to refresh, got %#v", embedProvider)
	}
	memorySvc := srv.memoryService()
	if memorySvc == nil || memorySvc.Provider == nil || memorySvc.Provider.APIBase != "https://embed-new.example/v1" {
		t.Fatalf("expected memory service provider to refresh, got %#v", memorySvc)
	}
	if memorySvc.EmbedModel != "embed-new" {
		t.Fatalf("expected memory embedding model to refresh, got %q", memorySvc.EmbedModel)
	}
	if memorySvc.EmbedFingerprint != currentEmbedFingerprint(next) {
		t.Fatalf("expected memory embedding fingerprint %q, got %q", currentEmbedFingerprint(next), memorySvc.EmbedFingerprint)
	}
	control := srv.control()
	if control == nil || control.Provider == nil || control.Provider.APIBase != "https://embed-new.example/v1" {
		t.Fatalf("expected control-plane embedding provider to refresh, got %#v", control)
	}
}

func TestServiceConfigUpdatesAreSerializedWithConcurrentReads(t *testing.T) {
	clearConfigEnvForTest(t)
	cfg := config.Default()
	cfgPath := filepath.Join(t.TempDir(), "or3-intern.json")
	globalSkills := filepath.Join(t.TempDir(), "skills")
	demoSkill := filepath.Join(globalSkills, "demo")
	if err := os.MkdirAll(demoSkill, 0o755); err != nil {
		t.Fatalf("create demo skill: %v", err)
	}
	if err := os.WriteFile(filepath.Join(demoSkill, "SKILL.md"), []byte("---\nname: demo\ndescription: Concurrent settings test\n---\n# Demo\n"), 0o644); err != nil {
		t.Fatalf("write demo skill: %v", err)
	}
	cfg.Skills.Load.GlobalDir = globalSkills
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	server := &serviceServer{
		config:        cfg,
		configPath:    cfgPath,
		runnerManager: buildRuntimeRunnerManager(cfg, nil, nil),
	}
	handler := serviceBoundaryMiddleware(server, newServiceMux(server))

	request := func(method, path, body string) error {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req = req.WithContext(context.WithValue(req.Context(), serviceAuthContextKey{}, serviceAuthIdentity{Actor: "ops", Role: approval.RoleOperator}))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return fmt.Errorf("%s %s: expected 200, got %d (%s)", method, path, rec.Code, rec.Body.String())
		}
		return nil
	}

	readers := []string{
		"/internal/v1/configure/fields?section=provider",
		"/internal/v1/configure/providers",
		"/internal/v1/skills",
	}
	writers := []struct {
		path string
		body string
	}{
		{path: "/internal/v1/configure/apply", body: `{"changes":[{"section":"provider","field":"provider_temperature","op":"set","value":"0.4"}]}`},
		{path: "/internal/v1/configure/providers", body: `{"key":"concurrency-test","label":"Concurrent provider","apiBase":"https://example.test/v1","defaultDimensions":0}`},
		{path: "/internal/v1/skills/demo/settings", body: `{"env":{"DEMO_API_KEY":"runtime-key"},"config":{"demo.enabled":true}}`},
	}

	start := make(chan struct{})
	errs := make(chan error, len(readers)*16+len(writers))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		for _, path := range readers {
			path := path
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if err := request(http.MethodGet, path, ""); err != nil {
					errs <- err
				}
			}()
		}
	}
	for _, write := range writers {
		write := write
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := request(http.MethodPost, write.path, write.body); err != nil {
				errs <- err
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if t.Failed() {
		return
	}

	loaded, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if loaded.Provider.Temperature != 0.4 {
		t.Fatalf("lost provider temperature update: %v", loaded.Provider.Temperature)
	}
	if profile := loaded.Providers["concurrency-test"]; profile.APIBase != "https://example.test/v1" {
		t.Fatalf("lost provider map update: %#v", profile)
	}
	entry := loaded.Skills.Entries["demo"]
	if entry.Env["DEMO_API_KEY"] != "runtime-key" || entry.Config["demo.enabled"] != true {
		t.Fatalf("lost skill map update: %#v", entry)
	}
}
