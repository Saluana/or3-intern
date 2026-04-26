# Evaluation Notes: Token-Efficient Context Packets

Implemented Phase 12 evaluation coverage uses deterministic fixtures in `internal/agent` rather than external services.

- Fixtures cover coding, planning, debugging, long-running tasks, repeated memories, stale memories, large tool logs, channel sessions, and workspace retrieval.
- Mode comparison tests assert poor ≤ balanced ≤ quality packet size while protected soul, agent instructions, tool policy, and pinned memory remain present.
- Legacy regression coverage checks stable-prefix byte parity for unchanged inputs.
- The cache-prefix measurement harness reports stable-prefix bytes, total input bytes, and cache-hit-eligible percentage.
- Benchmarks cover packet construction against a large synthetic fixture; defaults remain quality-leaning, so users must opt into lower-budget modes.
