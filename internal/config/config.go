// Package config resolves ACC's on-disk locations and connection settings.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	DefaultAddr = "127.0.0.1:7317"
	tokenFile   = "token"
)

// Home is ACC's state directory: $ACC_HOME or ~/.acc.
func Home() string {
	if h := os.Getenv("ACC_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".acc"
	}
	return filepath.Join(home, ".acc")
}

func DBPath() string   { return filepath.Join(Home(), "acc.db") }
func SpoolDir() string { return filepath.Join(Home(), "spool") }

// ServerURL is the daemon clients talk to: $ACC_SERVER or the local default.
func ServerURL() string {
	if s := os.Getenv("ACC_SERVER"); s != "" {
		return strings.TrimRight(s, "/")
	}
	return "http://" + DefaultAddr
}

// Token returns the local API token. Hosted mode will replace this with
// OIDC-derived machine tokens stored in the OS keychain.
func Token() (string, error) {
	if t := os.Getenv("ACC_TOKEN"); t != "" {
		return t, nil
	}
	b, err := os.ReadFile(filepath.Join(Home(), tokenFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// EnsureToken returns the local token, creating a random one (mode 0600) if absent.
func EnsureToken() (string, error) {
	if t, err := Token(); err == nil && t != "" {
		return t, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return newSecret(tokenFile)
}

// RemoteKeyPath holds the key other devices sign in with when the daemon
// listens beyond loopback (acc serve --addr 0.0.0.0:7317). It is separate
// from the local token so it can be shared and rotated (delete the file)
// without touching the agents' hooks.
func RemoteKeyPath() string { return filepath.Join(Home(), "remote_key") }

// EnsureRemoteKey returns $ACC_REMOTE_KEY, else the key in RemoteKeyPath,
// creating a random one if absent.
func EnsureRemoteKey() (string, error) {
	if k := strings.TrimSpace(os.Getenv("ACC_REMOTE_KEY")); k != "" {
		return k, nil
	}
	b, err := os.ReadFile(RemoteKeyPath())
	if k := strings.TrimSpace(string(b)); err == nil && k != "" {
		return k, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return newSecret(filepath.Base(RemoteKeyPath()))
}

func newSecret(name string) (string, error) {
	if err := os.MkdirAll(Home(), 0o700); err != nil {
		return "", err
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	t := hex.EncodeToString(buf)
	return t, os.WriteFile(filepath.Join(Home(), name), []byte(t+"\n"), 0o600)
}
