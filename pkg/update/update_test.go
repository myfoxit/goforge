package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.10.0", "1.9.0", 1},
		{"1.0.0-beta.1", "1.0.0", -1},
		{"1.0.0", "1.0.0-rc.1", 1},
		{"1.0.0-beta.1", "1.0.0-beta.2", -1},
		{"0.1.0-dev", "0.1.0", -1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func manifestServer(t *testing.T, binary []byte, version string, signWith ed25519.PrivateKey) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Write(binary)
	})
	var srv *httptest.Server
	srv = httptest.NewServer(mux)
	sum := sha256.Sum256(binary)
	artifact := Artifact{URL: srv.URL + "/bin", SHA256: hex.EncodeToString(sum[:])}
	if signWith != nil {
		artifact.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signWith, binary))
	}
	manifest := Manifest{
		Name: "testapp",
		Channels: map[string][]Release{
			"stable": {{
				Version: version,
				Artifacts: map[string]Artifact{
					platformKey(): artifact,
				},
			}},
		},
	}
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(manifest)
	})
	return srv
}

func platformKey() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func TestCheckFindsNewerVersion(t *testing.T) {
	srv := manifestServer(t, []byte("new-binary"), "2.0.0", nil)
	defer srv.Close()

	c := &Checker{ManifestURL: srv.URL + "/manifest.json", Current: "1.0.0"}
	release, err := c.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if release == nil || release.Version != "2.0.0" {
		t.Fatalf("release = %+v", release)
	}

	// Same version → up to date.
	c.Current = "2.0.0"
	release, err = c.Check(context.Background())
	if err != nil || release != nil {
		t.Fatalf("up-to-date check = %v %v", release, err)
	}

	// Unknown channel errors.
	c.Channel = "nightly"
	if _, err := c.Check(context.Background()); err == nil {
		t.Fatal("unknown channel accepted")
	}
}

func TestSignatureVerification(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	binary := []byte("signed-binary-content")

	srv := manifestServer(t, binary, "2.0.0", priv)
	defer srv.Close()

	// Correct key verifies.
	sig := ed25519.Sign(priv, binary)
	tmp := t.TempDir() + "/bin"
	writeFile(t, tmp, binary)
	if err := verifySignature(tmp, base64.StdEncoding.EncodeToString(sig), hex.EncodeToString(pub)); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	// Wrong key fails.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if err := verifySignature(tmp, base64.StdEncoding.EncodeToString(sig), hex.EncodeToString(otherPub)); err == nil {
		t.Fatal("wrong key accepted")
	}

	// Tampered binary fails.
	writeFile(t, tmp, []byte("tampered"))
	if err := verifySignature(tmp, base64.StdEncoding.EncodeToString(sig), hex.EncodeToString(pub)); err == nil {
		t.Fatal("tampered binary accepted")
	}

	// Missing signature with pinned key fails.
	if err := verifySignature(tmp, "", hex.EncodeToString(pub)); err == nil {
		t.Fatal("unsigned accepted with pinned key")
	}
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
