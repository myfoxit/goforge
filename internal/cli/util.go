package cli

import (
	"context"
	"os"
	"strings"
)

func bgContext() context.Context { return context.Background() }

func trimSpace(s string) string { return strings.TrimSpace(s) }

func mkdirAll(path string) error { return os.MkdirAll(path, 0o755) }
