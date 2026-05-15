# Security Store

The security store manages encrypted secrets and a hash-chained audit log. Secrets are encrypted at rest with authenticated encryption. Audit events form a tamper-evident chain using HMAC-SHA256.

Source: `internal/db/security.go`

## Secrets

### SecretRecord (`security.go:13-20`)

```go
type SecretRecord struct {
    Name       string   // secret name (primary key)
    Ciphertext []byte   // encrypted data
    Nonce      []byte   // AEAD nonce
    Version    int      // secret version
    KeyVersion string   // which encryption key was used
    UpdatedAt  int64
}
```

### Secret Operations

| Function | Purpose |
|----------|---------|
| `PutSecret()` | Upserts a secret. Uses `ON CONFLICT(name) DO UPDATE` |
| `GetSecret()` | Retrieves by name. Returns `(record, found, error)` |
| `DeleteSecret()` | Deletes by name |
| `ListSecretNames()` | Lists all secret names ordered alphabetically |

The encryption/decryption happens at a higher layer — the store only persists the ciphertext, nonce, and key version metadata.

## Audit Log

The audit log uses an HMAC chain to detect tampering. Each event hashes the previous event's hash into its own record hash, forming a linked chain.

### AuditEvent (`security.go:22-31`)

```go
type AuditEvent struct {
    ID          int64
    EventType   string
    SessionKey  string
    Actor       string
    PayloadJSON string
    PrevHash    []byte    // hash of the previous event
    RecordHash  []byte    // HMAC of this event's data
    CreatedAt   int64
}
```

### AuditEventInput (`security.go:33-38`)

```go
type AuditEventInput struct {
    EventType  string
    SessionKey string
    Actor      string
    Payload    any      // marshaled to JSON
}
```

### Audit Operations

| Function | Purpose |
|----------|---------|
| `AppendAuditEvent()` | Appends a new event to the chain. Reads the previous event's hash, computes HMAC over all fields including the previous hash, and inserts. Uses `auditMu` mutex for serialization and `BEGIN IMMEDIATE` for transactional safety |
| `VerifyAuditChain()` | Walks the entire audit log and verifies every hash. Returns an error at the first mismatch |
| `CountAuditEvents()` | Returns the total count of audit events |
| `LatestAuditEventSummary()` | Returns the most recent event (id, type, actor, created_at) |

### Hash Computation

`computeAuditHash()` (`security.go:190-203`) creates the HMAC:

```go
func computeAuditHash(key []byte, eventType, sessionKey, actor, payload string, 
                      prevHash []byte, createdAt int64) []byte {
    mac := hmac.New(sha256.New, key)
    // Writes each field separated by null bytes, plus the previous hash and timestamp
}
```

Fields are written in order with null byte separators: eventType, sessionKey, actor, payload, prevHash, createdAt.

### Payload Truncation

`truncateAuditPayload()` (`security.go:205-211`) caps payloads at 2048 characters to keep the audit log bounded.

### Concurrency

The `auditMu` mutex on the `DB` struct serializes audit writes. `AppendAuditEvent()` locks this mutex and uses `d.SQL.Conn(ctx)` to get a dedicated connection, then runs `BEGIN IMMEDIATE` to prevent concurrent writes from interleaving in the chain.
