# Authentication

GoForge authenticates against **auth collections** (`users` by default, plus the
system `_superusers`). Passwords are hashed with **argon2id**; every session is a
purpose-scoped **HS256 JWT** whose validity is tied to the record's `tokenKey`, so
changing a password (which rotates `tokenKey`) invalidates all existing tokens.

## Password flows

All endpoints are per-collection and rate-limited:

```
POST /api/collections/{c}/auth-with-password     {identity, password} → {token, record}
POST /api/collections/{c}/auth-refresh           (Bearer) → fresh {token, record}
POST /api/collections/{c}/request-verification   {email}
POST /api/collections/{c}/confirm-verification   {token}
POST /api/collections/{c}/request-password-reset {email}
POST /api/collections/{c}/confirm-password-reset {token, password, passwordConfirm}
POST /api/collections/{c}/request-email-change   (Bearer) {newEmail}
POST /api/collections/{c}/confirm-email-change   {token, password}
GET  /api/collections/{c}/auth-methods           → enabled methods + providers
```

Registration is just creating a record in an auth collection
(`POST /api/collections/users/records`) — governed by the collection's create
rule. The default `users` collection allows open registration; tighten it in the
admin UI. `identity` accepts any configured identity field (email by default).

Account-enumeration is avoided: verification and reset requests always return
`204` whether or not the email exists, and login failures return the same error
for wrong-password and unknown-user.

## OAuth2 / OIDC

Enable the `oauth` module and configure providers in **Settings → OAuth / SSO**.
Built-ins: Google, GitHub, Microsoft, GitLab, Discord, plus a **generic OIDC**
provider (point it at an issuer; endpoints are discovered from
`/.well-known/openid-configuration`).

```
GET /api/oauth2/{collection}/{provider}?redirect=/dashboard
    → provider consent → /api/oauth2/callback → redirect#oauthToken=<jwt>
```

The flow uses PKCE (where the provider supports it) and a signed state token with
a cookie-bound verifier. Users are linked by verified email or provisioned fresh;
the link is recorded in `_externalAuths`.

## MFA (TOTP)

Enable the `mfa` module. Standard RFC 6238 TOTP compatible with Google
Authenticator, 1Password, Authy, etc.

```
POST /api/mfa/setup     (Bearer) → {secret, otpauthURL}   # show as QR
POST /api/mfa/activate  (Bearer) {code}
POST /api/mfa/disable   (Bearer) {password}
POST /api/mfa/verify    {mfaToken, code}                   # completes login
```

When MFA is active, `auth-with-password` responds `401 {mfaRequired, mfaToken}`;
the client then calls `/api/mfa/verify` to obtain the session token.

## LDAP / Active Directory

Enable `ldap`, configure the server in **Settings → LDAP**. Users bind with their
directory credentials and are auto-provisioned as local records on first login.

```
POST /api/auth/ldap  {collection?, username, password} → {token, record}
```

## SAML 2.0

Enable `saml`. GoForge acts as a Service Provider; a signing keypair is generated
on first use.

```
GET  /api/saml/metadata   → SP metadata XML (register this with your IdP)
GET  /api/saml/login      → SP-initiated login
POST /api/saml/acs        → assertion consumer → redirect#oauthToken=<jwt>
```

Works with Okta, Entra ID, Keycloak and other standard IdPs. Users are matched or
provisioned by the email attribute (or NameID).

## Superusers

Superusers live in `_superusers`, bypass all collection rules, and manage schema
and settings. Create one from the CLI:

```sh
app superuser create you@example.com your-password
app superuser list
app superuser delete you@example.com
```

## Using tokens

Send the JWT as a bearer token:

```
Authorization: Bearer eyJhbGci...
```

The browser client (`$lib/goforge`) stores it and adds the header automatically;
for protected file access from `<img>` tags, request a short-lived file token via
`POST /api/files/token`.
