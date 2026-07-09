// Package registry embeds the GoForge design system: dependency-free
// Svelte 5 components with a shadcn-compatible token vocabulary, copied into
// applications by `forge ui add` (never installed as an npm dependency —
// the code is yours).
package registry

import "embed"

//go:embed registry.json all:components tokens
var FS embed.FS
