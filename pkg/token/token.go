// Package token implements compact HS256 JWTs with zero external
// dependencies. GoForge issues short-lived, purpose-scoped tokens
// (auth, verification, password reset, email change, file access, mfa).
package token

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Token purposes. A token issued for one purpose is never accepted for another.
const (
	TypeAuth          = "auth"
	TypeAdmin         = "admin"
	TypeVerification  = "verification"
	TypePasswordReset = "passwordReset"
	TypeEmailChange   = "emailChange"
	TypeFile          = "file"
	TypeMFA           = "mfa"
	TypeInvite        = "invite"
)

var (
	ErrInvalid = errors.New("token: invalid")
	ErrExpired = errors.New("token: expired")
)

// Claims is a minimal JWT claims map. Standard keys: sub, exp, iat, type.
type Claims map[string]any

// Subject returns the "sub" claim.
func (c Claims) Subject() string { s, _ := c["sub"].(string); return s }

// Type returns the "type" claim.
func (c Claims) Type() string { s, _ := c["type"].(string); return s }

// String returns a string claim by key.
func (c Claims) String(key string) string { s, _ := c[key].(string); return s }

var b64 = base64.RawURLEncoding

// Sign creates an HS256 JWT for the given claims, setting iat/exp/type.
func Sign(secret string, typ string, claims Claims, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("token: empty secret")
	}
	if claims == nil {
		claims = Claims{}
	}
	now := time.Now()
	claims["iat"] = now.Unix()
	claims["exp"] = now.Add(ttl).Unix()
	claims["type"] = typ

	header, _ := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	return signing + "." + b64.EncodeToString(sign(secret, signing)), nil
}

// Verify parses and validates a token: signature, expiry and expected type.
func Verify(secret, raw, expectType string) (Claims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, ErrInvalid
	}
	signing := parts[0] + "." + parts[1]
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, sign(secret, signing)) {
		return nil, ErrInvalid
	}
	var header struct {
		Alg string `json:"alg"`
	}
	hb, err := b64.DecodeString(parts[0])
	if err != nil || json.Unmarshal(hb, &header) != nil || header.Alg != "HS256" {
		return nil, ErrInvalid
	}
	pb, err := b64.DecodeString(parts[1])
	if err != nil {
		return nil, ErrInvalid
	}
	var claims Claims
	if err := json.Unmarshal(pb, &claims); err != nil {
		return nil, ErrInvalid
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return nil, ErrInvalid
	}
	if time.Now().Unix() > int64(exp) {
		return nil, ErrExpired
	}
	if expectType != "" && claims.Type() != expectType {
		return nil, fmt.Errorf("%w: unexpected type %q", ErrInvalid, claims.Type())
	}
	return claims, nil
}

func sign(secret, data string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
