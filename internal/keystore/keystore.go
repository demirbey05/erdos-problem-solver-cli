package keystore

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// StoredConfig holds the encrypted API configuration.
type StoredConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key"`
}

const configDir = ".erdos-agent"
const configFile = "config.enc"

// configPath returns the full path to the encrypted config file.
func configPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot find home directory: %w", err)
	}
	return filepath.Join(home, configDir, configFile), nil
}

// deriveKey creates a deterministic encryption key from machine-specific info.
// This isn't HSM-grade security but prevents casual reading of the key file.
func deriveKey() []byte {
	hostname, _ := os.Hostname()
	uid := fmt.Sprintf("%d", os.Getuid())
	seed := fmt.Sprintf("erdos-agent:%s:%s:%s", hostname, uid, runtime.GOARCH)
	hash := sha256.Sum256([]byte(seed))
	return hash[:]
}

// Save encrypts and persists the API configuration to disk.
func Save(cfg StoredConfig) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("cannot create config directory: %w", err)
	}

	plaintext, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("cannot marshal config: %w", err)
	}

	key := deriveKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return fmt.Errorf("cannot create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("cannot create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("cannot generate nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
	if err := os.WriteFile(path, ciphertext, 0600); err != nil {
		return fmt.Errorf("cannot write config file: %w", err)
	}

	return nil
}

// Load decrypts and reads the API configuration from disk.
func Load() (StoredConfig, error) {
	var cfg StoredConfig

	path, err := configPath()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, fmt.Errorf("no stored config found: %w", err)
		}
		return cfg, fmt.Errorf("cannot read config file: %w", err)
	}

	key := deriveKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return cfg, fmt.Errorf("cannot create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return cfg, fmt.Errorf("cannot create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return cfg, fmt.Errorf("config file is corrupted")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return cfg, fmt.Errorf("cannot decrypt config (file may be corrupted): %w", err)
	}

	if err := json.Unmarshal(plaintext, &cfg); err != nil {
		return cfg, fmt.Errorf("cannot parse config: %w", err)
	}

	return cfg, nil
}

// Exists returns true if a stored config file exists.
func Exists() bool {
	path, err := configPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Clear removes the stored config file.
func Clear() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return os.Remove(path)
}
