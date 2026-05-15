# Service Hardening Measurements

Measured on 2026-03-12 with:

```sh
go test ./internal/db ./internal/memory -run '^$' -bench 'Benchmark(HistoryLoad|ScopedRetrieval|HybridRetrieval|FTSRetrieval|DocIndexSync)' -benchmem -count=1
```

Environment:

- `goos=darwin`
- `goarch=arm64`
- `cpu=Apple M4 Pro`

## Current benchmark snapshot

| Benchmark | Latency | Memory | Allocs |
| --- | ---: | ---: | ---: |
| `BenchmarkHistoryLoad` | `23.8 us/op` | `10.7 KB/op` | `304 allocs/op` |
| `BenchmarkScopedRetrieval` | `22.7 us/op` | `7.4 KB/op` | `338 allocs/op` |
| `BenchmarkHybridRetrieval` | `190.1 us/op` | `22.2 KB/op` | `439 allocs/op` |
| `BenchmarkFTSRetrieval` | `123.3 us/op` | `3.7 KB/op` | `104 allocs/op` |
| `BenchmarkDocIndexSync` | `229.0 us/op` | `63.2 KB/op` | `799 allocs/op` |

## Practical budgets

- Last-N history load: keep steady-state reads below `50 us/op` and `16 KB/op`.
- Scoped retrieval streaming: keep steady-state reads below `50 us/op` and `12 KB/op`.
- Hybrid memory search: keep common-case retrieval below `250 us/op` and `32 KB/op`.
- FTS-only search: keep query latency below `150 us/op` and `8 KB/op`.
- Document indexing refresh: keep per-change sync passes below `300 us/op` and `80 KB/op` without embed API calls.

## Notes

- These numbers cover local SQLite and in-process retrieval/indexing only; they do not include provider embedding latency.
- `BenchmarkDocIndexSync` measures steady-state refresh after a single on-disk document change, which is the operational path `serve` uses after the initial sync.
- Re-run the benchmark command above after schema, retrieval, or indexing changes and update this note when the budget envelope changes materially.
