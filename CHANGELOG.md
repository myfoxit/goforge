# Changelog

All notable changes to GoForge are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/); versions follow semver.

## [Unreleased]

### Added — initial release

- **Dynamic collections** (base / auth / view) with runtime DDL sync across
  SQLite, PostgreSQL and MySQL behind one dialect abstraction.
- **Rules engine** — an access/filter expression language compiled to
  parameterized SQL, with `@request.*` placeholders and single-hop relations.
- **REST API** — records CRUD with filtering, sorting, pagination, relation
  expansion; collections admin; settings; file serving with thumbnails; realtime
  SSE subscriptions; request logs.
- **Auth** — password flows (register/verify/login/reset/email-change) with
  argon2id + purpose-scoped JWTs; OAuth2/OIDC (Google, GitHub, Microsoft, GitLab,
  Discord, generic OIDC); TOTP MFA; LDAP; SAML 2.0.
- **Permissions** — roles/RBAC and route guards over the rules engine.
- **MCP server** — every collection as typed AI tools, schema-building tools for
  admin keys, scoped API keys; one-click connect snippets.
- **Mail** — SMTP, sendmail, Resend, SES adapters, runtime-switchable; templated
  transactional emails.
- **Storage** — local filesystem and S3-compatible backends (SigV4, no SDK) with
  on-demand image thumbnails.
- **Operational modules** — Caddy-style signed self-update, cron jobs, Prometheus
  metrics, signed outgoing webhooks, multi-tenant orgs with invites, backups.
- **Embedded admin dashboard** at `/_/` — collections, records, schema editor,
  settings, API keys/MCP, logs.
- **Design system** — 23 dependency-free Svelte 5 components with shadcn-style
  tokens (light/dark), vendored into apps with a hash-aware lockfile.
- **`forge` CLI** — init (interactive module picker), add/remove, ui add/update,
  dev, build, release (cross-compile + signed manifest), module scaffolding.
- **SvelteKit frontend template** with a typed API client and realtime demo.
