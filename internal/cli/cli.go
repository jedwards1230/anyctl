// Package cli wires the cobra command tree from the loaded manifests. Every
// service is registered under the `svc` parent command (never at the root, so a
// user-defined service can't collide with a built-in); each named command and
// generic verb becomes a leaf. The CLI re-reads manifests just-in-time per
// invocation (no daemon).
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/jedwards1230/anyctl/internal/agentsafety"
	"github.com/jedwards1230/anyctl/internal/brand"
	"github.com/jedwards1230/anyctl/internal/command"
	"github.com/jedwards1230/anyctl/internal/engine"
	"github.com/jedwards1230/anyctl/internal/manifest"
	"github.com/jedwards1230/anyctl/internal/output"
	"github.com/jedwards1230/anyctl/internal/telemetry"
	"github.com/jedwards1230/anyctl/internal/transport"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Version is set at build time via -ldflags.
var Version = "dev"

type globalFlags struct {
	configDir  string
	filter     string
	raw        bool
	query      string
	limit      int
	output     string
	endpoint   string
	dryRun     bool
	verbose    bool
	jsonErrors bool
	yes        bool
}

type runner struct {
	flags   globalFlags
	stdout  io.Writer
	stderr  io.Writer
	config  manifest.Config
	loaded  *manifest.Loaded
	loadErr error
	tracer  trace.Tracer

	curService string
	curCommand string
	runner     interface{} // reserved for test secret-runner injection
}

// Run builds the command tree and executes it, returning a process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	// Optional OpenTelemetry — no-op unless OTEL_* env configures an endpoint.
	tracer, shutdown := telemetry.Start(context.Background(), Version)
	defer shutdown()

	r := &runner{stdout: stdout, stderr: stderr, tracer: tracer}
	root := r.newRoot()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.Execute(); err != nil {
		// Cobra's "unknown command" / "unknown flag" errors are usage errors (exit 2).
		if isUnknownCommandError(err) {
			// When the config dir failed to load, no service commands were
			// registered, so a service invocation degrades to "unknown
			// command". Surface the real load diagnostic (and its exit code)
			// instead of the misleading cobra message.
			if r.loadErr != nil {
				err = r.loadErr
			} else {
				err = agentsafety.NewUsageError(err.Error())
			}
		}
		return reportError(stderr, err, r.flags.jsonErrors, r.curService, r.curCommand)
	}
	return agentsafety.ExitOK
}

// ex renders a cobra Example block: each line is prefixed with the two-space
// indent cobra help uses plus the binary name, and the lines are joined with
// newlines (no trailing newline). Routing the command name through brand.Name
// keeps every example in lockstep with the tool's identity.
func ex(lines ...string) string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = "  " + brand.Name + " " + l
	}
	return strings.Join(out, "\n")
}

func (r *runner) newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           brand.Name,
		Short:         "Manifest-driven CLI for homelab service APIs",
		Long:          fmt.Sprintf("%s executes one HTTP/RPC call against a service described by a YAML manifest.\nAdding a service is a manifest edit, never a recompile.", brand.Name),
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       Version,
	}
	pf := root.PersistentFlags()
	pf.StringVar(&r.flags.configDir, "config-dir", "", fmt.Sprintf("config dir (default: $XDG_CONFIG_HOME/%s or ~/.config/%s)", brand.ConfigDirName, brand.ConfigDirName))
	pf.StringVar(&r.flags.filter, "filter", "", "jq filter over the response (overrides the command default)")
	pf.BoolVar(&r.flags.raw, "raw", false, "print the raw response, no jq filtering")
	pf.StringVar(&r.flags.query, "query", "", "extra query string appended to the request")
	pf.IntVar(&r.flags.limit, "limit", 0, "bound the item count (adds ?limit=N)")
	pf.StringVarP(&r.flags.output, "output", "o", "", "output mode: json|raw|scalar")
	pf.StringVar(&r.flags.endpoint, "endpoint", "", "target a named endpoint")
	pf.BoolVar(&r.flags.dryRun, "dry-run", false, "resolve and print the request, send nothing")
	pf.BoolVarP(&r.flags.verbose, "verbose", "v", false, "request/response diagnostics to stderr (secrets redacted)")
	pf.BoolVar(&r.flags.jsonErrors, "json-errors", false, "emit errors as a JSON envelope")
	pf.BoolVarP(&r.flags.yes, "yes", "y", false, "clear a step's confirm: gate (manifests opt in; otherwise the binary gates nothing)")

	// Load manifests for dynamic registration. A load error still lets builtins
	// like `lint` run, so report it lazily rather than aborting here.
	loaded, loadErr := manifest.Load(configDirFromArgs(r.flags.configDir))
	if loaded != nil {
		r.config = loaded.Config
		r.loaded = loaded
	}
	r.loadErr = loadErr

	r.addBuiltins(root, loaded, loadErr)
	root.AddCommand(r.newSvcCmd(loaded, loadErr))
	return root
}

// newSvcCmd builds the `svc` parent command. Every manifest-derived service
// command lives under it (never at the root), so a user-defined service can
// never collide with a built-in. With no service given it lists the configured
// services — the same content as `anyctl list`.
func (r *runner) newSvcCmd(loaded *manifest.Loaded, loadErr error) *cobra.Command {
	svcCmd := &cobra.Command{
		Use:     "svc <service> [command]",
		Aliases: []string{"s"},
		Short:   "run a configured service's API commands",
		Long: "Run a configured service's API commands.\n\n" +
			"Each service is a manifest under services/; built-ins (init, lint, list,\n" +
			fmt.Sprintf("doctor, catalog, mcp, version, self-update) live at the top level. Bare `%s svc`\n", brand.Name) +
			fmt.Sprintf("lists the configured services (same as `%s list`).", brand.Name),
		Example: ex(
			"svc                      # list configured services",
			"svc radarr list          # a named command",
			"svc tdarr get /api/v2/status   # generic verb passthrough",
			"s radarr list            # `s` is an alias for `svc`",
		),
		// RunE handles bare `anyctl svc` (list services) and an unknown service
		// argument (usage error). A known service routes to its own subcommand.
		RunE: func(cmd *cobra.Command, args []string) error {
			// A failed config load registered no services, so any service
			// invocation lands here. Surface the real load diagnostic (and its
			// exit code) instead of a misleading "unknown service".
			if loadErr != nil {
				return loadErr
			}
			if len(args) > 0 {
				return agentsafety.NewUsageError(fmt.Sprintf("unknown service %q", args[0]))
			}
			return r.listServices(loaded, loadErr)
		},
	}
	if loaded != nil {
		// Every selector — bare names and every installed-catalog's qualified
		// "<catalog>:<service>" form — gets a runnable cobra subcommand, so both
		// `anyctl svc foo` and `anyctl svc cat:foo` dispatch.
		for _, name := range loaded.SortedServiceNames() {
			svcCmd.AddCommand(r.newServiceCmd(name, loaded.Services[name]))
		}
		// A bare name more than one installed catalog defines (with no local
		// override) has no entry in loaded.Services — register a stub so
		// `anyctl svc <name>` and `anyctl svc <name> <cmd>` both surface the
		// "qualify it" diagnostic instead of "unknown service/command".
		for _, name := range sortedKeys(loaded.Ambiguous) {
			svcCmd.AddCommand(r.newAmbiguousServiceCmd(loaded, name))
		}
	}
	return svcCmd
}

// newAmbiguousServiceCmd builds a stub `svc <name>` subcommand for a bare name
// that more than one installed catalog defines. It accepts (and ignores) any
// trailing args — so a subcommand invocation lands here too — and always
// returns the ambiguity error from Loaded.Lookup.
func (r *runner) newAmbiguousServiceCmd(loaded *manifest.Loaded, name string) *cobra.Command {
	return &cobra.Command{
		Use:                name,
		Short:              "ambiguous: defined by multiple installed catalogs; qualify as <catalog>:" + name,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			r.curService = name
			if len(args) > 0 {
				r.curCommand = args[0]
			}
			_, err := loaded.Lookup(name)
			return err
		},
	}
}

// sortedKeys returns the sorted keys of an ambiguity map (bare service name →
// defining catalogs), for deterministic cobra registration order.
func sortedKeys(m map[string][]string) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *runner) newServiceCmd(selector string, svc *manifest.Service) *cobra.Command {
	cmds := command.FromManifest(svc)
	sc := &cobra.Command{
		Use:   selector,
		Short: svc.Description,
		Long:  serviceHelp(svc, cmds),
		// RunE is invoked when cobra cannot find a matching subcommand (e.g.
		// "anyctl svc radarr bogus-cmd"). Any argument here is an unknown command,
		// so return agentsafety.NewUsageError (exit 2) instead of printing help and exiting 0.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return agentsafety.NewUsageError(fmt.Sprintf("unknown command %q for %q", args[0], cmd.CommandPath()))
			}
			return cmd.Help()
		},
	}

	// Named commands.
	for _, id := range command.SortedIDs(cmds) {
		c := cmds[id]
		sc.AddCommand(&cobra.Command{
			Use:                id,
			Short:              c.Help,
			DisableFlagParsing: false,
			RunE: func(cmd *cobra.Command, args []string) error {
				return r.execNamed(selector, svc, c, args)
			},
		})
	}

	// Generic verbs (skip any that a named command already defines).
	for verb := range command.HTTPVerbs {
		if _, taken := cmds[verb]; taken {
			continue
		}
		v := verb
		sc.AddCommand(&cobra.Command{
			Use:   v + " <path> [body|query]",
			Short: "generic " + v + " passthrough",
			Args:  cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return r.execVerb(selector, svc, v, args)
			},
		})
	}
	if svc.Transport == "jsonrpc-ws" {
		sc.AddCommand(&cobra.Command{
			Use:   "call <method> [json-params]",
			Short: "generic jsonrpc passthrough",
			Args:  cobra.ArbitraryArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				return r.execVerb(selector, svc, "call", args)
			},
		})
	}
	return sc
}

func (r *runner) execNamed(selector string, svc *manifest.Service, c *command.Command, args []string) error {
	r.curService, r.curCommand = selector, c.ID
	return r.dispatch(svc, c, args)
}

func (r *runner) execVerb(selector string, svc *manifest.Service, verb string, args []string) error {
	r.curService, r.curCommand = selector, verb
	c, err := command.Verb(svc.Transport, verb, args)
	if err != nil {
		return agentsafety.NewUsageError(err.Error())
	}
	// For verbs, positional args beyond the path are consumed by the synthesizer;
	// pass none as templating args.
	return r.dispatch(svc, c, nil)
}

func (r *runner) dispatch(svc *manifest.Service, c *command.Command, args []string) error {
	ctx, span := r.startSpan(svc, c)
	defer span.End()

	res, err := engine.Execute(ctx, engine.Request{
		Config:  r.config,
		Service: svc,
		Command: c,
		Args:    args,
		Flags: engine.Flags{
			Filter:   r.flags.filter,
			Raw:      r.flags.raw,
			Query:    r.flags.query,
			Limit:    r.flags.limit,
			Output:   r.flags.output,
			Endpoint: r.flags.endpoint,
			DryRun:   r.flags.dryRun,
			Verbose:  r.flags.verbose,
			Yes:      r.flags.yes,
		},
		Runner: r.secretRunner(),
	}, r.stderr)
	if err != nil {
		recordSpanError(span, err)
		return err
	}
	if res.DryRunMsg != "" {
		_, _ = fmt.Fprint(r.stdout, res.DryRunMsg)
		span.SetStatus(codes.Ok, "")
		return nil
	}
	if err := output.Render(res.Body, res.Output, output.Options{
		Filter:        r.flags.filter,
		Raw:           r.flags.raw,
		Mode:          r.flags.output,
		DefaultMode:   r.config.Defaults.Output,
		ResponseCodec: res.ResponseCodec,
	}, r.stdout); err != nil {
		recordSpanError(span, err)
		return agentsafety.NewDecodeError(err)
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// startSpan opens a span for one command execution. With tracing disabled the
// tracer is a no-op, so this is free.
func (r *runner) startSpan(svc *manifest.Service, c *command.Command) (context.Context, trace.Span) {
	ctx, span := r.tracer.Start(context.Background(), svc.Name+" "+c.ID)
	attrs := []attribute.KeyValue{
		attribute.String(brand.TelemetryPrefix+"service", svc.Name),
		attribute.String(brand.TelemetryPrefix+"command", c.ID),
		attribute.Bool(brand.TelemetryPrefix+"write", c.Write),
	}
	if svc.Transport == "jsonrpc-ws" {
		attrs = append(attrs, attribute.String("rpc.method", c.Method))
	} else if c.Method != "" {
		attrs = append(attrs, attribute.String("http.request.method", c.Method))
	}
	span.SetAttributes(attrs...)
	return ctx, span
}

// recordSpanError marks the span failed and attaches an HTTP/RPC status when known.
func recordSpanError(span trace.Span, err error) {
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	var he *transport.HTTPError
	if errors.As(err, &he) {
		span.SetAttributes(attribute.Int("http.response.status_code", he.Status))
	}
	var re *transport.RPCError
	if errors.As(err, &re) {
		span.SetAttributes(attribute.Int("rpc.grpc_status_code", re.Code))
	}
}

// secretRunner returns nil (real op) unless a test injected one.
func (r *runner) secretRunner() func(argv []string) (string, error) {
	if r.runner == nil {
		return nil
	}
	return r.runner.(func(argv []string) (string, error))
}

// isUnknownCommandError reports whether the cobra error is an "unknown command"
// or "unknown flag" message. These are usage errors (exit 2), not general errors (exit 1).
func isUnknownCommandError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unknown command") ||
		strings.Contains(msg, "unknown flag") ||
		strings.Contains(msg, "unknown shorthand flag")
}

// configDirFromArgs peeks at --config-dir before full parse so dynamic
// registration uses the right dir. Falls back to the resolved default.
func configDirFromArgs(flagVal string) string {
	// Pre-parse persistent flags to honor --config-dir during registration.
	if flagVal != "" {
		return flagVal
	}
	for i, a := range os.Args {
		if a == "--config-dir" && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
		if len(a) > len("--config-dir=") && a[:len("--config-dir=")] == "--config-dir=" {
			return a[len("--config-dir="):]
		}
	}
	return manifest.ConfigDir()
}
