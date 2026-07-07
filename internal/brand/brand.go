// Package brand is the single source of the tool's identity. A rename edits
// this file (plus the go.mod module path and doc/comment sweeps) and nothing
// else in the Go source.
package brand

const (
	Name                = "anyctl"              // binary + root command name
	LegacyName          = "labctl"              // argv[0] alias detection + deprecation notice
	EnvPrefix           = "ANYCTL_"             // env var namespace (preferred)
	LegacyEnvPrefix     = "LABCTL_"             // honored fallback (see internal/compat)
	ConfigDirName       = "anyctl"              // dir under $XDG_CONFIG_HOME / ~/.config
	LegacyConfigDirName = "labctl"              // read-fallback config dir
	Repo                = "jedwards1230/anyctl" // self-update release source + schema $id host path

	// Pinned identities — deliberately DO NOT follow Name. Changing any of
	// these is a BREAKING change for external consumers; do it consciously.

	// FederationName is the MCP server name, the ui://<name>/* resource-URI
	// host, and the read-result StructuredContent metadata key. The
	// ContextForge gateway registration and saved claude.ai tool allowlists
	// key on this exact value — it must not change with a binary rename.
	// NOTE: the matching `json:"labctl"` struct tag in internal/mcpserver
	// cannot reference this constant (Go struct tags must be static string
	// literals); that tag stays a literal and carries a comment pointing here.
	FederationName = "labctl"

	// TelemetryPrefix is the OTEL attribute namespace ("anyctl.service" etc.).
	// Grafana dashboards key on it, so it is pinned independently of Name.
	TelemetryPrefix = "anyctl."
)
