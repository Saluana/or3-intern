package main

import (
	"or3-intern/internal/artifacts"
	"or3-intern/internal/config"
	"or3-intern/internal/db"
	"or3-intern/internal/memory"
	"or3-intern/internal/providers"
	"or3-intern/internal/security"
)

// serviceHostDeps holds process-wide dependencies used by the service.
type serviceHostDeps struct {
	DB            *db.DB
	Audit         *security.AuditLogger
	Artifacts     *artifacts.Store
	Mem           *memory.Retriever
	DocRetriever  *memory.DocRetriever
	EmbedProvider *providers.Client
}

func (s *serviceServer) serviceDB() *db.DB {
	if s == nil {
		return nil
	}
	return s.database
}

func (s *serviceServer) serviceAudit() *security.AuditLogger {
	if s == nil {
		return nil
	}
	return s.audit
}

func (s *serviceServer) serviceArtifacts() *artifacts.Store {
	if s == nil {
		return nil
	}
	return s.artifacts
}

func (s *serviceServer) serviceEmbedProvider() *providers.Client {
	if s == nil {
		return nil
	}
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return s.embedProvider
}

// configAndEmbedProviderSnapshot returns the embedding client that belongs to
// the same configuration snapshot. Embedding settings can change at runtime,
// so callers that initialize a configuration-dependent service must not mix
// the old client with the new model routing (or vice versa).
func (s *serviceServer) configAndEmbedProviderSnapshot() (config.Config, *providers.Client) {
	if s == nil {
		return config.Config{}, nil
	}
	s.configMu.RLock()
	defer s.configMu.RUnlock()
	return config.Clone(s.config), s.embedProvider
}

func (s *serviceServer) serviceMemRetriever() *memory.Retriever {
	if s == nil {
		return nil
	}
	return s.memRetriever
}

func (s *serviceServer) serviceDocRetriever() *memory.DocRetriever {
	if s == nil {
		return nil
	}
	return s.docRetriever
}

func (s *serviceServer) applyHostDeps(host serviceHostDeps) {
	if s == nil {
		return
	}
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if host.DB != nil {
		s.database = host.DB
	}
	if host.Audit != nil {
		s.audit = host.Audit
	}
	if host.Artifacts != nil {
		s.artifacts = host.Artifacts
	}
	if host.Mem != nil {
		s.memRetriever = host.Mem
	}
	if host.DocRetriever != nil {
		s.docRetriever = host.DocRetriever
	}
	if host.EmbedProvider != nil {
		s.embedProvider = host.EmbedProvider
	}
}
