// Package update implements Caddy-style in-place binary self-updates for
// deployed GoForge applications.
//
// The update source is a static JSON manifest (produced by `forge release`)
// served from any HTTP host or object storage:
//
//	{
//	  "name": "myapp",
//	  "channels": {
//	    "stable": [
//	      {
//	        "version": "1.4.2",
//	        "date": "2026-07-01",
//	        "notes": "Fixes ...",
//	        "artifacts": {
//	          "linux/amd64":  {"url": "https://.../myapp_1.4.2_linux_amd64",  "sha256": "...", "signature": "base64-ed25519"},
//	          "darwin/arm64": {"url": "https://.../myapp_1.4.2_darwin_arm64", "sha256": "...", "signature": "..."}
//	        }
//	      }
//	    ]
//	  }
//	}
//
// Every instance checks the manifest (manually, from the admin UI, or on a
// schedule), downloads its platform artifact, verifies the SHA-256 (and the
// ed25519 signature when a public key is configured), atomically swaps the
// running executable and restarts. Fleets of instances on different servers
// self-converge by polling the same manifest; staged rollouts happen by
// pointing groups of instances at different channels.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Artifact is one downloadable platform binary.
type Artifact struct {
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Signature string `json:"signature,omitempty"` // base64 ed25519 over the raw binary
}

// Release is one published version.
type Release struct {
	Version   string              `json:"version"`
	Date      string              `json:"date,omitempty"`
	Notes     string              `json:"notes,omitempty"`
	MinUpdate string              `json:"minUpdate,omitempty"` // refuse direct jumps from older versions
	Artifacts map[string]Artifact `json:"artifacts"`
}

// Manifest is the full update feed.
type Manifest struct {
	Name     string               `json:"name"`
	Channels map[string][]Release `json:"channels"`
}

// Checker checks and applies updates.
type Checker struct {
	ManifestURL string
	Channel     string
	Current     string
	// PublicKey (hex or base64 ed25519) enables signature verification.
	PublicKey string
	Client    *http.Client
}

// Check fetches the manifest and returns the newest applicable release
// (nil when already up to date).
func (c *Checker) Check(ctx context.Context) (*Release, error) {
	if c.ManifestURL == "" {
		return nil, fmt.Errorf("update: no manifest URL configured")
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", c.ManifestURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("update: fetch manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("update: manifest returned %d", resp.StatusCode)
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&m); err != nil {
		return nil, fmt.Errorf("update: parse manifest: %w", err)
	}
	channel := c.Channel
	if channel == "" {
		channel = "stable"
	}
	releases := m.Channels[channel]
	if len(releases) == 0 {
		return nil, fmt.Errorf("update: channel %q has no releases", channel)
	}
	var newest *Release
	for i := range releases {
		r := &releases[i]
		if newest == nil || CompareVersions(r.Version, newest.Version) > 0 {
			newest = r
		}
	}
	if CompareVersions(newest.Version, c.Current) <= 0 {
		return nil, nil
	}
	if newest.MinUpdate != "" && CompareVersions(c.Current, newest.MinUpdate) < 0 {
		return nil, fmt.Errorf("update: version %s requires updating to %s first", newest.Version, newest.MinUpdate)
	}
	return newest, nil
}

// Apply downloads, verifies and installs a release over the current binary,
// keeping a .bak of the previous one. It does not restart — call Restart.
func (c *Checker) Apply(ctx context.Context, release *Release) error {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	artifact, ok := release.Artifacts[platform]
	if !ok {
		return fmt.Errorf("update: release %s has no artifact for %s", release.Version, platform)
	}

	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, "GET", artifact.URL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("update: download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("update: download returned %d", resp.StatusCode)
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".update-*")
	if err != nil {
		return fmt.Errorf("update: temp file (need write access to %s): %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, 2<<30))
	if err != nil {
		tmp.Close()
		return fmt.Errorf("update: download: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if size == 0 {
		return fmt.Errorf("update: empty artifact")
	}

	// Integrity: SHA-256 always.
	got := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(got, artifact.SHA256) {
		return fmt.Errorf("update: sha256 mismatch (got %s, want %s)", got, artifact.SHA256)
	}
	// Authenticity: ed25519 when a public key is pinned.
	if c.PublicKey != "" {
		if err := verifySignature(tmpName, artifact.Signature, c.PublicKey); err != nil {
			return err
		}
	} else if artifact.Signature != "" {
		// Signed release but no pinned key: warn-level situation; proceed on
		// the sha256 (the manifest host is the trust root then).
		_ = artifact
	}

	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	// Atomic swap: current → .bak, tmp → current.
	bak := exe + ".bak"
	os.Remove(bak)
	if err := os.Rename(exe, bak); err != nil {
		return fmt.Errorf("update: backup current binary: %w", err)
	}
	if err := os.Rename(tmpName, exe); err != nil {
		os.Rename(bak, exe) // restore
		return fmt.Errorf("update: install new binary: %w", err)
	}
	return nil
}

// verifySignature checks an ed25519 signature over the file contents.
func verifySignature(path, sigB64, pubkey string) error {
	if sigB64 == "" {
		return fmt.Errorf("update: release is unsigned but a public key is configured")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("update: bad signature encoding: %w", err)
	}
	key, err := parsePublicKey(pubkey)
	if err != nil {
		return err
	}
	if !ed25519.Verify(key, raw, sig) {
		return fmt.Errorf("update: signature verification FAILED — refusing to install")
	}
	return nil
}

func parsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if raw, err := hex.DecodeString(s); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	if raw, err := base64.StdEncoding.DecodeString(s); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	return nil, fmt.Errorf("update: invalid ed25519 public key")
}

// Restart replaces the current process with the (updated) executable.
func Restart() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command(exe, os.Args[1:]...)
		cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
		if err := cmd.Start(); err != nil {
			return err
		}
		os.Exit(0)
	}
	return syscall.Exec(exe, os.Args, os.Environ())
}

// CompareVersions compares semver-ish strings ("1.2.3", "v1.2.3-beta.1").
// Returns -1, 0 or 1.
func CompareVersions(a, b string) int {
	pa, prea := parseVersion(a)
	pb, preb := parseVersion(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	// Equal cores: a pre-release sorts before the release.
	switch {
	case prea == preb:
		return 0
	case prea == "":
		return 1
	case preb == "":
		return -1
	case prea < preb:
		return -1
	default:
		return 1
	}
}

func parseVersion(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		if v[i] == '-' {
			pre = v[i+1:]
		}
		v = v[:i]
	}
	var out [3]int
	parts := strings.SplitN(v, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		n, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
		out[i] = n
	}
	return out, pre
}
