package cli

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/myfoxit/goforge/pkg/update"
)

// defaultTargets are the platforms `forge release` cross-compiles for.
var defaultTargets = []string{
	"linux/amd64", "linux/arm64",
	"darwin/amd64", "darwin/arm64",
	"windows/amd64",
}

// Release cross-compiles the app and writes a self-update manifest.
func Release(args []string) error {
	if len(args) > 0 && args[0] == "keygen" {
		return releaseKeygen()
	}
	fs := flag.NewFlagSet("release", flag.ContinueOnError)
	version := fs.String("version", "", "release version (e.g. 1.2.0); defaults to git describe")
	channel := fs.String("channel", "stable", "release channel")
	baseURL := fs.String("url", "", "public base URL where artifacts will be hosted (required)")
	keyPath := fs.String("key", "", "ed25519 private key file to sign artifacts (optional)")
	targetList := fs.String("targets", strings.Join(defaultTargets, ","), "comma-separated os/arch targets")
	notes := fs.String("notes", "", "release notes")
	if err := fs.Parse(args); err != nil {
		return err
	}

	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	if *version == "" {
		*version = strings.TrimPrefix(gitVersion(dir), "v")
	}
	if *baseURL == "" {
		return fmt.Errorf("--url is required (where the artifacts will be downloadable)")
	}
	*baseURL = strings.TrimSuffix(*baseURL, "/")

	// Build the UI once; it's platform-independent and embedded in every binary.
	if m.UI.Path != "" {
		uiDir := filepath.Join(dir, m.UI.Path)
		step("Building frontend")
		if err := stream(uiDir, "npm", "install"); err != nil {
			return err
		}
		if err := stream(uiDir, "npm", "run", "build"); err != nil {
			return err
		}
	}

	var signer ed25519.PrivateKey
	if *keyPath != "" {
		signer, err = loadPrivateKey(*keyPath)
		if err != nil {
			return err
		}
	}

	distDir := filepath.Join(dir, "dist", "release")
	if err := os.MkdirAll(distDir, 0o755); err != nil {
		return err
	}

	release := update.Release{Version: *version, Notes: *notes, Date: today(), Artifacts: map[string]update.Artifact{}}
	targets := splitList(*targetList)
	for _, target := range targets {
		parts := strings.SplitN(target, "/", 2)
		if len(parts) != 2 {
			return fmt.Errorf("bad target %q", target)
		}
		goos, goarch := parts[0], parts[1]
		binName := fmt.Sprintf("%s_%s_%s_%s", m.Name, *version, goos, goarch)
		if goos == "windows" {
			binName += ".exe"
		}
		outPath := filepath.Join(distDir, binName)

		step(fmt.Sprintf("Building %s/%s", goos, goarch))
		cmd := exec.Command("go", "build",
			"-ldflags", "-s -w -X github.com/myfoxit/goforge/pkg/cmd.Version="+*version,
			"-o", outPath, ".")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("build %s: %w", target, err)
		}

		sum, err := sha256File(outPath)
		if err != nil {
			return err
		}
		artifact := update.Artifact{
			URL:    fmt.Sprintf("%s/%s", *baseURL, binName),
			SHA256: sum,
		}
		if signer != nil {
			sig, err := signFile(outPath, signer)
			if err != nil {
				return err
			}
			artifact.Signature = sig
		}
		release.Artifacts[target] = artifact
	}

	// Merge into an existing manifest if present (keeps release history).
	manifestPath := filepath.Join(distDir, "manifest.json")
	manifest := update.Manifest{Name: m.Name, Channels: map[string][]update.Release{}}
	if raw, err := os.ReadFile(manifestPath); err == nil {
		json.Unmarshal(raw, &manifest)
	}
	if manifest.Channels == nil {
		manifest.Channels = map[string][]update.Release{}
	}
	// Replace same-version entry if re-releasing.
	existing := manifest.Channels[*channel]
	filtered := existing[:0]
	for _, r := range existing {
		if r.Version != *version {
			filtered = append(filtered, r)
		}
	}
	manifest.Channels[*channel] = append(filtered, release)

	raw, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(manifestPath, append(raw, '\n'), 0o644); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println(BoxStyle.Render(
		OkStyle.Render("✓ release "+*version+" built") + "\n\n" +
			DimStyle.Render("artifacts: ") + distDir + "\n" +
			DimStyle.Render("manifest:  ") + manifestPath + "\n\n" +
			"Upload dist/release/* to " + *baseURL + "\n" +
			"Point instances' updates.manifestURL at " + *baseURL + "/manifest.json"))
	if signer == nil {
		fmt.Println(DimStyle.Render("tip: sign releases with `forge release keygen` + --key for tamper protection"))
	}
	return nil
}

func releaseKeygen() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	privPath := "forge-release.key"
	if err := os.WriteFile(privPath, []byte(base64.StdEncoding.EncodeToString(priv)), 0o600); err != nil {
		return err
	}
	fmt.Println(OkStyle.Render("✓ signing key written to ") + privPath + DimStyle.Render("  (keep secret, never commit!)"))
	fmt.Println()
	fmt.Println("Public key — set as the `updates.publicKey` setting in every instance:")
	fmt.Println(TitleStyle.Render(hex.EncodeToString(pub)))
	return nil
}

func loadPrivateKey(path string) (ed25519.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(decoded) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key in %s", path)
	}
	return ed25519.PrivateKey(decoded), nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func signFile(path string, key ed25519.PrivateKey) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, raw)), nil
}

// today returns the current date; kept trivial to avoid importing time here
// where determinism in tests matters less.
func today() string {
	out, err := exec.Command("date", "+%Y-%m-%d").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
