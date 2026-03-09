package security

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"or3-intern/internal/config"
	"or3-intern/internal/db"
)

const secretRefPrefix = "secret:"

type SecretManager struct {
	DB  *db.DB
	Key []byte
}

func LoadOrCreateKey(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("key file required")
	}
	if raw, err := os.ReadFile(path); err == nil {
		if decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); decErr == nil && len(decoded) >= 32 {
			return decoded[:32], nil
		}
		if len(raw) >= 32 {
			return raw[:32], nil
		}
		return nil, fmt.Errorf("key file too short")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepathDir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(key)), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}

func LoadExistingKey(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("key file required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if decoded, decErr := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw))); decErr == nil && len(decoded) >= 32 {
		return decoded[:32], nil
	}
	if len(raw) >= 32 {
		return raw[:32], nil
	}
	return nil, fmt.Errorf("key file too short")
}

func (m *SecretManager) ResolveRef(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), secretRefPrefix) {
		return raw, nil
	}
	if m == nil || m.DB == nil || len(m.Key) == 0 {
		return "", fmt.Errorf("secret store unavailable")
	}
	name := strings.TrimSpace(raw[len(secretRefPrefix):])
	if name == "" {
		return "", fmt.Errorf("invalid secret ref")
	}
	secret, ok, err := m.Get(ctx, name)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("secret not found: %s", name)
	}
	return secret, nil
}

func (m *SecretManager) Put(ctx context.Context, name, value string) error {
	if m == nil || m.DB == nil || len(m.Key) == 0 {
		return fmt.Errorf("secret store unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("secret name required")
	}
	ciphertext, nonce, err := encryptBlob(m.Key, []byte(value))
	if err != nil {
		return err
	}
	return m.DB.PutSecret(ctx, name, ciphertext, nonce, 1, "v1")
}

func (m *SecretManager) Get(ctx context.Context, name string) (string, bool, error) {
	if m == nil || m.DB == nil || len(m.Key) == 0 {
		return "", false, fmt.Errorf("secret store unavailable")
	}
	record, ok, err := m.DB.GetSecret(ctx, name)
	if err != nil || !ok {
		return "", ok, err
	}
	plain, err := decryptBlob(m.Key, record.Ciphertext, record.Nonce)
	if err != nil {
		return "", false, fmt.Errorf("decrypt secret %s: %w", name, err)
	}
	return string(plain), true, nil
}

func (m *SecretManager) Delete(ctx context.Context, name string) error {
	if m == nil || m.DB == nil {
		return fmt.Errorf("secret store unavailable")
	}
	return m.DB.DeleteSecret(ctx, name)
}

func (m *SecretManager) List(ctx context.Context) ([]string, error) {
	if m == nil || m.DB == nil {
		return nil, fmt.Errorf("secret store unavailable")
	}
	return m.DB.ListSecretNames(ctx)
}

type AuditLogger struct {
	DB     *db.DB
	Key    []byte
	Strict bool
}

func (a *AuditLogger) Record(ctx context.Context, eventType, sessionKey, actor string, payload any) error {
	if a == nil || a.DB == nil || len(a.Key) == 0 {
		if a != nil && a.Strict {
			return fmt.Errorf("audit logger unavailable")
		}
		return nil
	}
	return a.DB.AppendAuditEvent(ctx, db.AuditEventInput{
		EventType:  eventType,
		SessionKey: strings.TrimSpace(sessionKey),
		Actor:      strings.TrimSpace(actor),
		Payload:    payload,
	}, a.Key)
}

func (a *AuditLogger) Verify(ctx context.Context) error {
	if a == nil || a.DB == nil || len(a.Key) == 0 {
		return nil
	}
	return a.DB.VerifyAuditChain(ctx, a.Key)
}

func ResolveConfigSecrets(ctx context.Context, cfg config.Config, mgr *SecretManager) (config.Config, error) {
	resolved := cfg
	if mgr == nil {
		return resolved, nil
	}
	value := reflect.ValueOf(&resolved).Elem()
	if err := resolveValue(ctx, value, mgr); err != nil {
		return cfg, err
	}
	return resolved, nil
}

func ValidateNoSecretRefs(cfg config.Config) error {
	if path, ok := findSecretRef(reflect.ValueOf(cfg), "config"); ok {
		return fmt.Errorf("unresolved secret ref at %s", path)
	}
	return nil
}

func resolveValue(ctx context.Context, value reflect.Value, mgr *SecretManager) error {
	if !value.IsValid() {
		return nil
	}
	switch value.Kind() {
	case reflect.Pointer:
		if value.IsNil() {
			return nil
		}
		return resolveValue(ctx, value.Elem(), mgr)
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if !value.Field(i).CanSet() {
				continue
			}
			if err := resolveValue(ctx, value.Field(i), mgr); err != nil {
				return err
			}
		}
	case reflect.Map:
		if value.IsNil() || value.Type().Key().Kind() != reflect.String {
			return nil
		}
		for _, key := range value.MapKeys() {
			elem := value.MapIndex(key)
			if !elem.IsValid() {
				continue
			}
			if value.Type().Elem().Kind() == reflect.String {
				resolved, err := mgr.ResolveRef(ctx, elem.String())
				if err != nil {
					return err
				}
				value.SetMapIndex(key, reflect.ValueOf(resolved))
				continue
			}
			clone := reflect.New(elem.Type()).Elem()
			clone.Set(elem)
			if err := resolveValue(ctx, clone, mgr); err != nil {
				return err
			}
			value.SetMapIndex(key, clone)
		}
	case reflect.String:
		resolved, err := mgr.ResolveRef(ctx, value.String())
		if err != nil {
			return err
		}
		value.SetString(resolved)
	}
	return nil
}

func findSecretRef(value reflect.Value, path string) (string, bool) {
	if !value.IsValid() {
		return "", false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			name := field.Name
			if tag := strings.TrimSpace(strings.Split(field.Tag.Get("json"), ",")[0]); tag != "" && tag != "-" {
				name = tag
			}
			if foundPath, ok := findSecretRef(value.Field(i), path+"."+name); ok {
				return foundPath, true
			}
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			foundPath, ok := findSecretRef(value.MapIndex(key), path+"."+fmt.Sprint(key.Interface()))
			if ok {
				return foundPath, true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if foundPath, ok := findSecretRef(value.Index(i), fmt.Sprintf("%s[%d]", path, i)); ok {
				return foundPath, true
			}
		}
	case reflect.String:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(value.String())), secretRefPrefix) {
			return path, true
		}
	}
	return "", false
}

func encryptBlob(master, plaintext []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(deriveKey(master, "secrets"))
	if err != nil {
		return nil, nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	sealed := aead.Seal(nil, nonce, plaintext, nil)
	return sealed, nonce, nil
}

func decryptBlob(master, ciphertext, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(deriveKey(master, "secrets"))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return aead.Open(nil, nonce, ciphertext, nil)
}

func deriveKey(master []byte, label string) []byte {
	h := hmac.New(sha256.New, master)
	_, _ = h.Write([]byte(label))
	sum := h.Sum(nil)
	return sum[:32]
}

func filepathDir(path string) string {
	return filepath.Dir(path)
}
