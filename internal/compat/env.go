// Package compat centralizes the labctl→anyctl backward-compatibility shims so
// downstream consumers (the Helm chart, Ansible-managed workstation configs)
// can cut over to the anyctl binary without a lockstep edit. Today that is
// environment-variable fallbacks: an ANYCTL_* name is preferred, but the legacy
// LABCTL_* name is still honored with a one-time stderr deprecation warning.
package compat

import (
	"fmt"
	"os"
	"sync"

	"github.com/jedwards1230/anyctl/internal/brand"
)

// warnedEnv records which legacy env names have already emitted a deprecation
// warning, so Getenv warns at most once per name per process.
var warnedEnv sync.Map // oldName -> struct{}

// Getenv returns the value of newName when it is set to a non-empty string.
// Otherwise it falls back to the legacy oldName, emitting a one-time stderr
// deprecation warning the first time the legacy name is what supplies the
// value. When neither is set (or both are empty) it returns "".
//
// The preference order (new over legacy) means a consumer can set both during a
// migration window and always get the new value.
func Getenv(newName, oldName string) string {
	if v := os.Getenv(newName); v != "" {
		return v
	}
	if v := os.Getenv(oldName); v != "" {
		warnLegacyEnv(oldName, newName)
		return v
	}
	return ""
}

// LegacyEnvSet reports whether the legacy name is the one currently supplying a
// value for the pair — i.e. newName is unset/empty but oldName is set. Callers
// that only need to know "is this configured at all" should use Getenv; this is
// for diagnostics that want to distinguish the legacy path.
func LegacyEnvSet(newName, oldName string) bool {
	return os.Getenv(newName) == "" && os.Getenv(oldName) != ""
}

func warnLegacyEnv(oldName, newName string) {
	if _, loaded := warnedEnv.LoadOrStore(oldName, struct{}{}); loaded {
		return
	}
	fmt.Fprintf(os.Stderr,
		"%s: %s is deprecated; set %s instead (the legacy name still works for now)\n",
		brand.Name, oldName, newName)
}
