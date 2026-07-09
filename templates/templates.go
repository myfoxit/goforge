// Package templates embeds the `forge init` application scaffold.
// Files ending in .tmpl are rendered with text/template; everything else is
// copied verbatim.
package templates

import "embed"

//go:embed all:app
var FS embed.FS

// AppRoot is the scaffold root inside FS.
const AppRoot = "app"
