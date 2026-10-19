// Package localstate keeps what ptah-console local remembers between runs:
// which kubeconfig credential plugins the person agreed to run. It never
// holds a credential, a token or a kubeconfig.
//
// The file lives under the user's config directory, in a directory only the
// user can enter, and is written whole or not at all. A file anyone else can
// read or write is refused rather than trusted.
package localstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"
)

// Version is the state file's schema version.
const Version = 1

// State is the whole file.
type State struct {
	Version            int             `json:"version"`
	TrustedExecPlugins []TrustedPlugin `json:"trustedExecPlugins,omitempty"`
}

// TrustedPlugin is one credential plugin the person agreed to run, bound to
// where it was configured and to exactly what it runs.
type TrustedPlugin struct {
	Kubeconfig string `json:"kubeconfig"`
	Context    string `json:"context"`
	// Digest covers the command, its arguments and its environment. A changed
	// plugin is a new question.
	Digest    string    `json:"digest"`
	TrustedAt time.Time `json:"trustedAt"`
}

// Trusts reports whether the plugin was agreed to as it is now.
func (s State) Trusts(plugin TrustedPlugin) bool {
	return slices.ContainsFunc(s.TrustedExecPlugins, func(trusted TrustedPlugin) bool {
		return trusted.Kubeconfig == plugin.Kubeconfig && trusted.Context == plugin.Context && trusted.Digest == plugin.Digest
	})
}

// Trust records the plugin, replacing an earlier answer for the same
// kubeconfig and context.
func (s *State) Trust(plugin TrustedPlugin) {
	s.TrustedExecPlugins = slices.DeleteFunc(s.TrustedExecPlugins, func(trusted TrustedPlugin) bool {
		return trusted.Kubeconfig == plugin.Kubeconfig && trusted.Context == plugin.Context
	})
	s.TrustedExecPlugins = append(s.TrustedExecPlugins, plugin)
}

// DefaultPath is state.json under the user's config directory.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("localstate: find the config directory: %w", err)
	}
	return filepath.Join(dir, "ptah-console", "state.json"), nil
}

// Load reads the state. A missing file is an empty state.
func Load(path string) (State, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return State{Version: Version}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("localstate: read %s: %w", path, err)
	}
	if err := private(path); err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("localstate: %s is not a state file: %w", path, err)
	}
	if state.Version != Version {
		return State{}, fmt.Errorf("localstate: %s has schema version %d and this build reads %d", path, state.Version, Version)
	}
	return state, nil
}

// Save writes the state whole: to a temporary file beside it, then renamed
// over it, so a crash leaves the old file or the new one and never half of
// either.
func Save(path string, state State) error {
	state.Version = Version
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("localstate: encode: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("localstate: create %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("localstate: restrict %s: %w", dir, err)
	}
	temp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("localstate: create a temporary file in %s: %w", dir, err)
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("localstate: restrict %s: %w", name, err)
	}
	if _, err := temp.Write(append(data, '\n')); err != nil {
		_ = temp.Close()
		return fmt.Errorf("localstate: write %s: %w", name, err)
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return fmt.Errorf("localstate: sync %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("localstate: close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("localstate: replace %s: %w", path, err)
	}
	return nil
}

// private refuses a file another user can read or write. Windows keeps
// access in ACLs that the mode bits do not show, so the check is Unix-only.
func private(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("localstate: stat %s: %w", path, err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("localstate: %s has mode %04o; only its owner may read it (chmod 600)", path, info.Mode().Perm())
	}
	return nil
}
