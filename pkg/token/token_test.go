package token

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSignVerify(t *testing.T) {
	tok, err := Sign("secret", TypeAuth, Claims{"sub": "user1", "email": "a@b.c"}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Verify("secret", tok, TypeAuth)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject() != "user1" || claims.String("email") != "a@b.c" {
		t.Fatalf("claims = %v", claims)
	}
}

func TestVerifyRejects(t *testing.T) {
	tok, _ := Sign("secret", TypeAuth, Claims{"sub": "u"}, time.Minute)

	if _, err := Verify("other", tok, TypeAuth); !errors.Is(err, ErrInvalid) {
		t.Fatalf("wrong secret: %v", err)
	}
	if _, err := Verify("secret", tok, TypeVerification); err == nil {
		t.Fatal("wrong type accepted")
	}
	if _, err := Verify("secret", "a.b", TypeAuth); !errors.Is(err, ErrInvalid) {
		t.Fatal("malformed accepted")
	}

	// Tampered payload must fail.
	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + parts[1][:len(parts[1])-2] + "xx" + "." + parts[2]
	if _, err := Verify("secret", tampered, TypeAuth); err == nil {
		t.Fatal("tampered accepted")
	}

	expired, _ := Sign("secret", TypeAuth, Claims{"sub": "u"}, -time.Minute)
	if _, err := Verify("secret", expired, TypeAuth); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired: %v", err)
	}
}

func TestAlgConfusion(t *testing.T) {
	// A token claiming alg=none must be rejected even with a valid-looking sig.
	tok, _ := Sign("secret", TypeAuth, Claims{"sub": "u"}, time.Minute)
	parts := strings.Split(tok, ".")
	noneHeader := b64.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	forged := noneHeader + "." + parts[1] + "."
	if _, err := Verify("secret", forged, TypeAuth); err == nil {
		t.Fatal("alg=none accepted")
	}
}
