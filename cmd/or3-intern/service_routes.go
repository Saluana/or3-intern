package main

import (
	"net/http"
	"strings"
)

type serviceRouteHandler func(http.ResponseWriter, *http.Request)

type serviceRouteSpec struct {
	Path    string
	Subtree bool
	Handler serviceRouteHandler
}

func serviceRouteSpecs(server *serviceServer) []serviceRouteSpec {
	return []serviceRouteSpec{
		{Path: "/internal/v1/turns", Handler: server.handleTurns},
		{Path: "/internal/v1/subagents", Subtree: true, Handler: server.handleSubagents},
		{Path: "/internal/v1/jobs", Subtree: true, Handler: server.handleJobs},
		{Path: "/internal/v1/artifacts", Subtree: true, Handler: server.handleArtifacts},
		{Path: "/internal/v1/pairing/requests", Subtree: true, Handler: server.handlePairing},
		{Path: "/internal/v1/pairing/exchange", Handler: server.handlePairing},
		{Path: "/internal/v1/secure-connections", Subtree: true, Handler: server.handleSecureConnections},
		{Path: "/internal/v1/devices", Subtree: true, Handler: server.handleDevices},
		{Path: "/internal/v1/approvals", Subtree: true, Handler: server.handleApprovals},
		{Path: "/internal/v1/auth/capabilities", Handler: server.handleAuth},
		{Path: "/internal/v1/auth/session", Subtree: true, Handler: server.handleAuth},
		{Path: "/internal/v1/auth/passkeys", Subtree: true, Handler: server.handleAuth},
		{Path: "/internal/v1/auth/passkeys/registration", Subtree: true, Handler: server.handleAuth},
		{Path: "/internal/v1/auth/passkeys/login", Subtree: true, Handler: server.handleAuth},
		{Path: "/internal/v1/auth/step-up", Subtree: true, Handler: server.handleAuth},
		{Path: "/internal/v1/health", Handler: server.handleHealth},
		{Path: "/internal/v1/readiness", Handler: server.handleReadiness},
		{Path: "/internal/v1/capabilities", Handler: server.handleCapabilities},
		{Path: "/internal/v1/app/bootstrap", Handler: server.handleApp},
		{Path: "/internal/v1/actions", Subtree: true, Handler: server.handleActions},
		{Path: "/internal/v1/cron", Subtree: true, Handler: server.handleCron},
		{Path: "/internal/v1/embeddings", Subtree: true, Handler: server.handleEmbeddings},
		{Path: "/internal/v1/audit", Subtree: true, Handler: server.handleAudit},
		{Path: "/internal/v1/logs/stream", Handler: server.handleLogs},
		{Path: "/internal/v1/scope", Subtree: true, Handler: server.handleScope},
		{Path: "/internal/v1/configure", Subtree: true, Handler: server.handleConfigure},
		{Path: "/internal/v1/mcp/servers", Subtree: true, Handler: server.handleMCPServers},
		{Path: "/internal/v1/skills", Subtree: true, Handler: server.handleSkills},
		{Path: "/internal/v1/files", Subtree: true, Handler: server.handleFiles},
		{Path: "/internal/v1/terminal/sessions", Subtree: true, Handler: server.handleTerminal},
		{Path: "/internal/v1/agent-runners", Handler: server.handleAgentRunners},
		{Path: "/internal/v1/agent-runs", Subtree: true, Handler: server.handleAgentRuns},
		{Path: "/internal/v1/chat-runners", Handler: server.handleChatRunners},
		{Path: "/internal/v1/chat-sessions", Subtree: true, Handler: server.handleChatSessions},
		{Path: "/internal/v1/runner-chat/sessions", Subtree: true, Handler: server.handleRunnerChatSessions},
	}
}

func registerServiceRoute(mux *http.ServeMux, route serviceRouteSpec) {
	mux.Handle(route.Path, http.HandlerFunc(route.Handler))
	if route.Subtree {
		mux.Handle(strings.TrimRight(route.Path, "/")+"/", http.HandlerFunc(route.Handler))
	}
}

func handleUnknownServiceRoute(w http.ResponseWriter, _ *http.Request) {
	writeServiceJSON(w, http.StatusNotFound, map[string]any{"error": "service route not found"})
}
