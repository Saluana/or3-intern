# Embeddings

`embeddings` inspects or rebuilds persisted embedding state after provider or embedding-model changes.

## Supported subcommands

| Command | Description |
| --- | --- |
| `status` | Show stored vector dimensions, stored fingerprint, current fingerprint, and mismatch state |
| `rebuild memory` | Regenerate long-term memory embeddings |
| `rebuild docs` | Regenerate indexed document embeddings |
| `rebuild all` | Rebuild both memory and indexed docs |

## Examples

```bash
or3-intern embeddings status
or3-intern embeddings rebuild memory
or3-intern embeddings rebuild all
```

## When to use it

- after changing embedding providers or embedding models
- after importing memory-heavy data and wanting a clean rebuild
- when bootstrap or status reports an embedding fingerprint mismatch

If document indexing is disabled, `docs` rebuild work may be skipped.
