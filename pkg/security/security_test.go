package security

import (
	"strings"
	"testing"
)

func TestRandomID(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := RandomID(15)
		if len(id) != 15 {
			t.Fatalf("len = %d, want 15", len(id))
		}
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}
	if got := RandomID(0); len(got) != 15 {
		t.Fatalf("default length = %d, want 15", len(got))
	}
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("unexpected format: %s", hash)
	}
	if !VerifyPassword(hash, "s3cret-pass") {
		t.Fatal("correct password rejected")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("wrong password accepted")
	}
	if VerifyPassword("garbage", "x") {
		t.Fatal("garbage hash accepted")
	}
	if _, err := HashPassword(""); err == nil {
		t.Fatal("empty password should error")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	secret := "master-secret"
	ct, err := Encrypt("hello world", secret)
	if err != nil {
		t.Fatal(err)
	}
	if ct == "hello world" {
		t.Fatal("not encrypted")
	}
	pt, err := Decrypt(ct, secret)
	if err != nil {
		t.Fatal(err)
	}
	if pt != "hello world" {
		t.Fatalf("roundtrip = %q", pt)
	}
	if _, err := Decrypt(ct, "wrong-secret"); err == nil {
		t.Fatal("wrong key should fail")
	}
	if _, err := Decrypt("!!!", secret); err == nil {
		t.Fatal("bad input should fail")
	}
}

func TestHashToken(t *testing.T) {
	a, b := HashToken("tok1"), HashToken("tok1")
	if a != b {
		t.Fatal("not deterministic")
	}
	if HashToken("tok2") == a {
		t.Fatal("collision")
	}
}
