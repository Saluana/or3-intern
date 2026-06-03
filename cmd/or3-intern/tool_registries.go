package main

import (
	"context"
	"fmt"
	"time"

	"or3-intern/internal/agent"
	"or3-intern/internal/approval"
	"or3-intern/internal/artifacts"
	"or3-intern/internal/config"
	"or3-intern/internal/cron"
	"or3-intern/internal/db"
	"or3-intern/internal/providers"
	rootchannels "or3-intern/internal/channels"
	"or3-intern/internal/skills"
	"or3-intern/internal/tools"
)

// toolRegistryProfile selects which tools are registered for a registry build.
type toolRegistryProfile int

const (
	// toolRegistryLegacyModelCallable retains the pre-runner-first model tool surface.
	toolRegistryLegacyModelCallable toolRegistryProfile = iota
	// toolRegistryPlatformInternal keeps OR3 service/memory/artifact helpers without model-callable work tools.
	toolRegistryPlatformInternal
	// toolRegistryDoctorRuntime is an empty base; doctor admin tools are registered separately on the service runtime.
	toolRegistryDoctorRuntime
)

func toolRegistryProfileFor(cfg config.Config, background bool) toolRegistryProfile {
	if !cfg.AgentCLI.Enabled {
		return toolRegistryLegacyModelCallable
	}
	if background {
		return toolRegistryPlatformInternal
	}
	return toolRegistryDoctorRuntime
}

func buildToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer, mcpRegistrar mcpToolRegistrar, approvalBroker *approval.Broker) *tools.Registry {
	return buildToolRegistryWithOptions(cfg, d, prov, channelManager, inv, cronSvc, spawnManager, mcpRegistrar, approvalBroker, true, toolRegistryProfileFor(cfg, false))
}

func buildBackgroundToolRegistry(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, mcpRegistrar mcpToolRegistrar, approvalBroker *approval.Broker) *tools.Registry {
	return buildToolRegistryWithOptions(cfg, d, prov, channelManager, inv, cronSvc, nil, mcpRegistrar, approvalBroker, false, toolRegistryProfileFor(cfg, true))
}

func buildToolRegistryWithOptions(cfg config.Config, d *db.DB, prov *providers.Client, channelManager *rootchannels.Manager, inv *skills.Inventory, cronSvc *cron.Service, spawnManager tools.SpawnEnqueuer, mcpRegistrar mcpToolRegistrar, approvalBroker *approval.Broker, includeSendMessage bool, profile toolRegistryProfile) *tools.Registry {
	reg := tools.NewRegistry()
	if profile == toolRegistryDoctorRuntime {
		return reg
	}

	fileWriteRoot := allowedRoot(cfg)
	fileReadRoot := allowedReadRoot(cfg)
	sandboxCfg := tools.BubblewrapConfig{Enabled: cfg.Hardening.Sandbox.Enabled, BubblewrapPath: cfg.Hardening.Sandbox.BubblewrapPath, AllowNetwork: cfg.Hardening.Sandbox.AllowNetwork, WritablePaths: append([]string{}, cfg.Hardening.Sandbox.WritablePaths...)}
	hostPolicy := buildHostPolicy(cfg)

	modelCallable := profile == toolRegistryLegacyModelCallable
	if modelCallable {
		if shouldRegisterExecTool(cfg) {
			reg.Register(&tools.ExecTool{Timeout: time.Duration(cfg.Tools.ExecTimeoutSeconds) * time.Second, RestrictDir: fileWriteRoot, PathAppend: cfg.Tools.PathAppend, AllowedPrograms: append([]string{}, cfg.Hardening.ExecAllowedPrograms...), ChildEnvAllowlist: append([]string{}, cfg.Hardening.ChildEnvAllowlist...), Sandbox: sandboxCfg, EnableLegacyShell: cfg.Hardening.EnableExecShell, ApprovalBroker: approvalBroker})
		}
		reg.Register(&tools.ReadFile{FileTool: tools.FileTool{Root: fileReadRoot, WriteRoot: fileWriteRoot}})
		reg.Register(&tools.SearchFile{FileTool: tools.FileTool{Root: fileReadRoot, WriteRoot: fileWriteRoot}})
		reg.Register(&tools.WriteFile{FileTool: tools.FileTool{Root: fileReadRoot, WriteRoot: fileWriteRoot}})
		reg.Register(&tools.EditFile{FileTool: tools.FileTool{Root: fileReadRoot, WriteRoot: fileWriteRoot}})
		reg.Register(&tools.DeleteFile{FileTool: tools.FileTool{Root: fileReadRoot, WriteRoot: fileWriteRoot}})
		reg.Register(&tools.ListDir{FileTool: tools.FileTool{Root: fileReadRoot, WriteRoot: fileWriteRoot}})
		reg.Register(&tools.WebFetch{HostPolicy: hostPolicy, Store: &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}})
		reg.Register(&tools.WebFetchMarkdown{HostPolicy: hostPolicy, Store: &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}})
		reg.Register(&tools.WebSearch{APIKey: cfg.Tools.BraveAPIKey, HostPolicy: hostPolicy})
		planBase := agent.NewPlanToolBase(d)
		reg.Register(&agent.CreatePlanTool{PlanToolBase: planBase})
		reg.Register(&agent.UpdatePlanTool{PlanToolBase: planBase})
		reg.Register(&agent.CompletePlanTaskTool{PlanToolBase: planBase})
		reg.Register(&agent.RemovePlanTool{PlanToolBase: planBase})
		if inv != nil {
			reg.Register(&tools.ReadSkill{Inventory: inv})
			if cfg.Skills.EnableExec {
				reg.Register(&tools.RunSkill{RunSkillScript: tools.RunSkillScript{Inventory: inv, Enabled: true, Timeout: time.Duration(cfg.Skills.MaxRunSeconds) * time.Second, ChildEnvAllowlist: append([]string{}, cfg.Hardening.ChildEnvAllowlist...), Sandbox: sandboxCfg, ApprovalBroker: approvalBroker, DB: d}})
				reg.Register(&tools.RunSkillScript{Inventory: inv, Enabled: true, Timeout: time.Duration(cfg.Skills.MaxRunSeconds) * time.Second, ChildEnvAllowlist: append([]string{}, cfg.Hardening.ChildEnvAllowlist...), Sandbox: sandboxCfg, ApprovalBroker: approvalBroker, DB: d})
			}
		}
		if spawnManager != nil {
			reg.Register(&tools.SpawnSubagent{Manager: spawnManager})
		}
		if mcpRegistrar != nil {
			mcpRegistrar.RegisterTools(reg)
		}
	}

	reg.Register(&tools.ReadArtifact{Store: &artifacts.Store{Dir: cfg.ArtifactsDir, DB: d}, MaxReadBytes: int64(cfg.MaxToolBytes)})
	registerMemoryTools(reg, cfg, d, prov)

	if includeSendMessage {
		reg.Register(&tools.SendMessage{
			Deliver: func(ctx context.Context, channel, to, text string, meta map[string]any) error {
				if channelManager == nil {
					return fmt.Errorf("channel manager not configured")
				}
				return channelManager.DeliverWithMeta(ctx, channel, to, text, meta)
			},
			AllowedRoot:    allowedRoot(cfg),
			ArtifactsDir:   cfg.ArtifactsDir,
			MaxMediaBytes:  cfg.MaxMediaBytes,
			ApprovalBroker: approvalBroker,
		})
	}
	if cronSvc != nil {
		reg.Register(&tools.CronTool{Svc: cronSvc})
	}
	return reg
}

func registerMemoryTools(reg *tools.Registry, cfg config.Config, d *db.DB, prov *providers.Client) {
	reg.Register(&tools.MemorySetPinned{DB: d})
	embedRole := cfg.ModelRole(config.ModelRoleEmbeddings)
	reg.Register(&tools.MemoryAddNote{DB: d, Provider: prov, EmbedModel: embedRole.Primary.Model, EmbedFingerprint: currentEmbedFingerprint(cfg)})
	reg.Register(&tools.MemorySearch{DB: d, Provider: prov, EmbedModel: embedRole.Primary.Model, EmbedFingerprint: currentEmbedFingerprint(cfg), VectorK: cfg.VectorK, FTSK: cfg.FTSK, TopK: cfg.MemoryRetrieve, VectorScanLimit: cfg.VectorScanLimit})
	reg.Register(&tools.MemoryRecent{DB: d, DefaultLimit: 10, MaxLimit: cfg.HistoryMax, MaxChars: 240})
	reg.Register(&tools.MemoryGetPinned{DB: d, MaxChars: 400})
}
