package app

import (
	"path/filepath"

	"github.com/downmygo/downmygo/internal/engine"
)

func uniqueIn(dir, name string, exists func(string) bool) string {
	return engine.UniqueName(name, func(n string) bool { return exists(filepath.Join(dir, n)) })
}
