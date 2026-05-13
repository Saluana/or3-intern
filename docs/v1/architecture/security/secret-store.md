# Encrypted Secret Storage

The secret store encrypts API keys, tokens, and other secrets at rest using AES-256-GCM.

## SecretManager

The `SecretManager` handles encrypting, storing, decrypting, and deleting secrets. It requires:
- A database connection (`DB`)
- An encryption key (32 bytes)

Source: `internal/security/store.go:25-28` (SecretManager struct)

## Key management

### Creating or loading a key

`LoadOrCreateKey` loads a key from a file path. If the file doesn't exist, it generates a new 32-byte random key and writes it (base64-encoded) to the file with permissions 0o600. The file's parent directory is created with permissions 0o700.

The function tries to decode the file as base64 first. If the decoded value is at least 32 bytes, the first 32 bytes are used. If the file isn't valid base64 but is at least 32 bytes raw, those raw bytes are used.

Source: `internal/security/store.go:31-58`

### Loading existing key

`LoadExistingKey` only loads an existing key file and returns an error if it doesn't exist. Used when keys must already be configured.

Source: `internal/security/store.go:61-77`

## Encryption

Secrets are encrypted with AES-256-GCM:
1. The master key is derived with HMAC-SHA256 using the label "secrets" to produce a 32-byte AES key
2. A random nonce (12 bytes for GCM) is generated
3. The plaintext is encrypted with AES-GCM Seal
4. Both ciphertext and nonce are stored

Source: `internal/security/store.go:298-325` (encryptBlob, decryptBlob, deriveKey)

## Storing secrets

`Put` encrypts the value and stores it in the database with:
- Name (the secret identifier)
- Ciphertext (encrypted bytes)
- Nonce (for decryption)
- Version marker ("v1")

Source: `internal/security/store.go:102-116`

## Retrieving secrets

`Get` fetches the record by name and decrypts the ciphertext using the stored nonce.

Source: `internal/security/store.go:118-132`

## Secret references in config

Config values can reference secrets using the `secret:` prefix. For example, `secret:brave-api-key` tells the system to resolve the value from the secret store. `ResolveConfigSecrets` walks the entire config struct using reflection and resolves all secret references. `ValidateNoSecretRefs` checks that no unresolved references remain.

Source: `internal/security/store.go:182-296` (ResolveConfigSecrets, ValidateNoSecretRefs, resolveValue, findSecretRef)

## Deleting secrets

`Delete` removes a secret by name from the database.

Source: `internal/security/store.go:134-140`

## Listing secrets

`List` returns only the secret names, never the values.

Source: `internal/security/store.go:142-148`
