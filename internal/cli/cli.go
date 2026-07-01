package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/xxxoooxoxo/gogodaddy/internal/config"
	"github.com/xxxoooxoxo/gogodaddy/internal/godaddy"
)

// delegateAccessURL is GoDaddy's web page for granting or accepting delegate
// access to an account. API calls then act on a delegated account by passing
// that account's shopper id via --shopper-id (the X-Shopper-Id header).
const delegateAccessURL = "https://account.godaddy.com/access"

// version is the CLI version, overridable at build time via
// -ldflags "-X github.com/xxxoooxoxo/gogodaddy/internal/cli.version=x.y.z".
var version = "0.1.0"

type globalOptions struct {
	configPath string
	json       bool
	baseURL    string
	shopperID  string
	timeout    time.Duration
	version    bool
}

type optionalInt struct {
	value int
	set   bool
}

func (i *optionalInt) String() string {
	if !i.set {
		return ""
	}
	return fmt.Sprint(i.value)
}

func (i *optionalInt) Set(value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("expected integer: %w", err)
	}
	i.value = parsed
	i.set = true
	return nil
}

func (i optionalInt) Ptr() *int {
	if !i.set {
		return nil
	}
	return &i.value
}

func Main(args []string, stdout, stderr io.Writer) int {
	opts, rest, err := parseGlobal(args, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 2
	}
	if opts.version {
		return writeOutput(stdout, opts.json, map[string]any{"version": version}, "gogodaddy "+version+"\n")
	}
	if len(rest) == 0 {
		printUsage(stdout)
		return 2
	}

	switch rest[0] {
	case "auth":
		return runAuth(opts, rest[1:], stdout, stderr)
	case "records", "record", "dns":
		return runRecords(opts, rest[1:], stdout, stderr)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", rest[0])
		printUsage(stderr)
		return 2
	}
}

func parseGlobal(args []string, stderr io.Writer) (globalOptions, []string, error) {
	opts := globalOptions{timeout: 30 * time.Second}
	defaultConfigPath, err := config.DefaultPath()
	if err != nil {
		return opts, nil, err
	}

	fs := flag.NewFlagSet("gogodaddy", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.configPath, "config", defaultConfigPath, "path to the global session file")
	fs.BoolVar(&opts.json, "json", false, "write JSON output")
	fs.StringVar(&opts.baseURL, "base-url", "", "override API base URL for this command")
	fs.StringVar(&opts.shopperID, "shopper-id", "", "override X-Shopper-Id for reseller calls")
	fs.DurationVar(&opts.timeout, "timeout", opts.timeout, "HTTP timeout")
	fs.BoolVar(&opts.version, "version", false, "print version and exit")
	fs.BoolVar(&opts.version, "V", false, "print version and exit")
	fs.Usage = func() {
		printUsage(stderr)
	}
	if err := fs.Parse(args); err != nil {
		return opts, nil, err
	}
	return opts, fs.Args(), nil
}

func runAuth(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printAuthUsage(stdout)
		return 2
	}

	switch args[0] {
	case "login":
		return runAuthLogin(opts, args[1:], stdout, stderr)
	case "status":
		return runAuthStatus(opts, args[1:], stdout, stderr)
	case "delegate":
		return runAuthDelegate(opts, args[1:], stdout, stderr)
	case "logout":
		return runAuthLogout(opts, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown auth command %q\n\n", args[0])
		printAuthUsage(stderr)
		return 2
	}
}

func runAuthLogin(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gogodaddy auth login", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apiKey := fs.String("api-key", os.Getenv("GODADDY_API_KEY"), "GoDaddy API key")
	apiSecret := fs.String("api-secret", os.Getenv("GODADDY_API_SECRET"), "GoDaddy API secret")
	environment := fs.String("env", envOrDefault("GODADDY_ENV", config.ProductionEnv), "environment: production or ote")
	baseURL := fs.String("base-url", "", "custom API base URL")
	shopperID := fs.String("shopper-id", os.Getenv("GODADDY_SHOPPER_ID"), "optional X-Shopper-Id")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *apiKey == "" || *apiSecret == "" {
		return writeError(stderr, opts.json, "missing_flag", "missing --api-key or --api-secret; flags can also come from GODADDY_API_KEY and GODADDY_API_SECRET", 2)
	}
	resolvedBaseURL, err := config.ResolveBaseURL(*environment, *baseURL)
	if err != nil {
		return writeError(stderr, opts.json, "invalid_environment", err.Error(), 2)
	}

	session := config.Session{
		APIKey:      *apiKey,
		APISecret:   *apiSecret,
		Environment: *environment,
		BaseURL:     resolvedBaseURL,
		ShopperID:   *shopperID,
	}
	if err := config.Save(opts.configPath, session); err != nil {
		return writeError(stderr, opts.json, "runtime_error", err.Error(), 1)
	}

	return writeOutput(stdout, opts.json, map[string]any{
		"status":      "saved",
		"path":        opts.configPath,
		"environment": session.Environment,
		"base_url":    session.BaseURL,
		"api_key":     config.Redact(session.APIKey),
		"shopper_id":  session.ShopperID,
	}, fmt.Sprintf("Saved GoDaddy session to %s (%s).\n", opts.configPath, session.BaseURL))
}

func runAuthStatus(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gogodaddy auth status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	session, source, err := loadSessionOrEnv(opts)
	if err != nil {
		if errors.Is(err, config.ErrNoSession) {
			return writeOutput(stdout, opts.json, map[string]any{
				"authenticated": false,
				"path":          opts.configPath,
			}, "No saved GoDaddy session. Run `gogodaddy auth login --api-key ... --api-secret ...`.\n")
		}
		return writeError(stderr, opts.json, "runtime_error", err.Error(), 1)
	}

	return writeOutput(stdout, opts.json, map[string]any{
		"authenticated": true,
		"source":        source,
		"path":          opts.configPath,
		"environment":   session.Environment,
		"base_url":      session.BaseURL,
		"api_key":       config.Redact(session.APIKey),
		"shopper_id":    session.ShopperID,
	}, fmt.Sprintf("Authenticated via %s as %s against %s.\n", source, config.Redact(session.APIKey), session.BaseURL))
}

func runAuthLogout(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gogodaddy auth logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if err := config.Delete(opts.configPath); err != nil {
		return writeError(stderr, opts.json, "runtime_error", err.Error(), 1)
	}
	return writeOutput(stdout, opts.json, map[string]any{
		"status": "deleted",
		"path":   opts.configPath,
	}, fmt.Sprintf("Deleted GoDaddy session at %s.\n", opts.configPath))
}

func runAuthDelegate(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gogodaddy auth delegate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	// Surface whichever shopper id a real call would use, if any: the global
	// --shopper-id flag wins, otherwise fall back to the saved session or env.
	shopperID := opts.shopperID
	source := "flag"
	if shopperID == "" {
		source = ""
		if session, src, err := loadSessionOrEnv(opts); err == nil {
			shopperID = session.ShopperID
			if shopperID != "" {
				source = src
			}
		}
	}

	var text strings.Builder
	fmt.Fprintf(&text, "Delegate Access\n")
	fmt.Fprintf(&text, "Grant access to your account, or accept access to someone else's, here:\n  %s\n\n", delegateAccessURL)
	fmt.Fprintf(&text, "Once access is granted, act on the delegated account by passing its shopper id:\n")
	fmt.Fprintf(&text, "  gogodaddy --shopper-id OWNER_SHOPPER_ID records list --domain example.com\n")
	fmt.Fprintf(&text, "  gogodaddy auth login --shopper-id OWNER_SHOPPER_ID --api-key KEY --api-secret SECRET\n\n")
	if shopperID != "" {
		fmt.Fprintf(&text, "Current X-Shopper-Id: %s (from %s)\n", shopperID, source)
	} else {
		fmt.Fprintf(&text, "Current X-Shopper-Id: not set (calls act as the API key's own account)\n")
	}

	return writeOutput(stdout, opts.json, map[string]any{
		"delegate_access_url": delegateAccessURL,
		"shopper_id":          shopperID,
		"shopper_id_source":   source,
		"next_step":           "grant or accept delegate access at the URL, then pass --shopper-id OWNER_SHOPPER_ID",
	}, text.String())
}

func runRecords(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printRecordsUsage(stdout)
		return 2
	}

	switch args[0] {
	case "add":
		return runRecordsAdd(opts, args[1:], stdout, stderr)
	case "list", "get":
		return runRecordsList(opts, args[1:], stdout, stderr)
	case "delete", "rm", "remove":
		return runRecordsDelete(opts, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown records command %q\n\n", args[0])
		printRecordsUsage(stderr)
		return 2
	}
}

func runRecordsAdd(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gogodaddy records add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	domain := fs.String("domain", "", "domain to modify")
	recordType := fs.String("type", "", "DNS record type")
	name := fs.String("name", "", "DNS record name, for example @, www, or _acme-challenge")
	data := fs.String("data", "", "DNS record data")
	apply := fs.Bool("apply", false, "send the mutation; without this flag the command is a dry run")
	var ttl, priority, port, weight optionalInt
	fs.Var(&ttl, "ttl", "record TTL")
	fs.Var(&priority, "priority", "record priority for MX and SRV")
	fs.Var(&port, "port", "service port for SRV")
	fs.Var(&weight, "weight", "record weight for SRV")
	protocol := fs.String("protocol", "", "service protocol for SRV")
	service := fs.String("service", "", "service type for SRV")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	record := godaddy.DNSRecord{
		Type:     strings.ToUpper(*recordType),
		Name:     *name,
		Data:     *data,
		TTL:      ttl.Ptr(),
		Priority: priority.Ptr(),
		Port:     port.Ptr(),
		Weight:   weight.Ptr(),
		Protocol: *protocol,
		Service:  *service,
	}
	if *domain == "" {
		return writeError(stderr, opts.json, "missing_flag", "--domain is required", 2)
	}
	if err := godaddy.ValidateRecord(record, true); err != nil {
		return writeError(stderr, opts.json, "invalid_record", err.Error(), 2)
	}

	if !*apply {
		nextCommand := buildAddCommand(*domain, record)
		return writeOutput(stdout, opts.json, map[string]any{
			"dry_run":      true,
			"action":       "add",
			"method":       "PATCH",
			"path":         "/v1/domains/" + *domain + "/records",
			"records":      []godaddy.DNSRecord{record},
			"next_command": nextCommand,
		}, "Dry run only. Run this to apply:\n  "+nextCommand+"\n")
	}

	client, err := clientFromSession(opts)
	if err != nil {
		code, msg := clientErrorCodeMessage(err)
		return writeError(stderr, opts.json, code, msg, 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	if err := client.AddRecords(ctx, *domain, []godaddy.DNSRecord{record}); err != nil {
		return writeError(stderr, opts.json, "api_error", err.Error(), 1)
	}

	return writeOutput(stdout, opts.json, map[string]any{
		"status": "added",
		"domain": *domain,
		"record": record,
	}, "Added DNS record.\n")
}

func runRecordsList(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gogodaddy records list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	domain := fs.String("domain", "", "domain to query")
	recordType := fs.String("type", "", "DNS record type")
	name := fs.String("name", "", "DNS record name")
	offset := fs.Int("offset", 0, "pagination offset")
	limit := fs.Int("limit", 0, "pagination limit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *domain == "" {
		return writeError(stderr, opts.json, "missing_flag", "--domain is required", 2)
	}
	if err := godaddy.ValidateTypeName(strings.ToUpper(*recordType), *name); err != nil {
		return writeError(stderr, opts.json, "invalid_record", err.Error(), 2)
	}

	client, err := clientFromSession(opts)
	if err != nil {
		code, msg := clientErrorCodeMessage(err)
		return writeError(stderr, opts.json, code, msg, 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	records, err := client.GetRecords(ctx, *domain, strings.ToUpper(*recordType), *name, *offset, *limit)
	if err != nil {
		return writeError(stderr, opts.json, "api_error", err.Error(), 1)
	}

	if opts.json {
		if records == nil {
			records = []godaddy.DNSRecord{}
		}
		return writeJSON(stdout, map[string]any{
			"domain":  *domain,
			"type":    strings.ToUpper(*recordType),
			"name":    *name,
			"count":   len(records),
			"offset":  *offset,
			"limit":   *limit,
			"records": records,
		})
	}
	printRecords(stdout, records)
	return 0
}

func runRecordsDelete(opts globalOptions, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gogodaddy records delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	domain := fs.String("domain", "", "domain to modify")
	recordType := fs.String("type", "", "DNS record type")
	name := fs.String("name", "", "DNS record name")
	data := fs.String("data", "", "DNS record data to delete")
	apply := fs.Bool("apply", false, "send the mutation; without this flag the command only prints the plan")
	var ttl, priority, port, weight optionalInt
	fs.Var(&ttl, "ttl", "match TTL")
	fs.Var(&priority, "priority", "match priority")
	fs.Var(&port, "port", "match SRV port")
	fs.Var(&weight, "weight", "match SRV weight")
	protocol := fs.String("protocol", "", "match SRV protocol")
	service := fs.String("service", "", "match SRV service")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	upperType := strings.ToUpper(*recordType)
	if *domain == "" {
		return writeError(stderr, opts.json, "missing_flag", "--domain is required", 2)
	}
	if err := godaddy.ValidateTypeName(upperType, *name); err != nil {
		return writeError(stderr, opts.json, "invalid_record", err.Error(), 2)
	}

	selector := godaddy.DeleteSelector{
		Data:     *data,
		TTL:      ttl.Ptr(),
		Priority: priority.Ptr(),
		Port:     port.Ptr(),
		Weight:   weight.Ptr(),
		Protocol: optionalString(*protocol),
		Service:  optionalString(*service),
	}
	if selector.Data == "" {
		return writeError(stderr, opts.json, "missing_flag", "delete requires --data so one record can be selected", 2)
	}

	client, err := clientFromSession(opts)
	if err != nil {
		code, msg := clientErrorCodeMessage(err)
		return writeError(stderr, opts.json, code, msg, 1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	existing, err := client.GetRecords(ctx, *domain, upperType, *name, 0, 0)
	if err != nil {
		return writeError(stderr, opts.json, "api_error", err.Error(), 1)
	}
	plan, err := godaddy.PlanDeleteOne(existing, selector)
	if err != nil {
		return writeError(stderr, opts.json, "no_single_match", err.Error(), 1)
	}

	if !*apply {
		nextCommand := buildDeleteCommand(*domain, upperType, *name, selector)
		return writeOutput(stdout, opts.json, map[string]any{
			"dry_run":           true,
			"action":            "delete_one",
			"domain":            *domain,
			"type":              upperType,
			"name":              *name,
			"record":            plan.Record,
			"use_direct_delete": plan.UseDirectDelete,
			"remaining_count":   len(plan.Remaining),
			"next_command":      nextCommand,
		}, "Dry run only. Run this to apply:\n  "+nextCommand+"\n")
	}

	if plan.UseDirectDelete {
		if err := client.DeleteRecordsByTypeName(ctx, *domain, upperType, *name); err != nil {
			return writeError(stderr, opts.json, "api_error", err.Error(), 1)
		}
	} else {
		if err := client.ReplaceRecordsByTypeName(ctx, *domain, upperType, *name, plan.Remaining); err != nil {
			return writeError(stderr, opts.json, "api_error", err.Error(), 1)
		}
	}

	return writeOutput(stdout, opts.json, map[string]any{
		"status": "deleted",
		"domain": *domain,
		"type":   upperType,
		"name":   *name,
		"record": plan.Record,
	}, "Deleted exactly one DNS record.\n")
}

func clientFromSession(opts globalOptions) (*godaddy.Client, error) {
	session, _, err := loadSessionOrEnv(opts)
	if err != nil {
		return nil, err
	}
	baseURL := session.BaseURL
	if opts.baseURL != "" {
		baseURL = opts.baseURL
	}
	shopperID := session.ShopperID
	if opts.shopperID != "" {
		shopperID = opts.shopperID
	}
	return godaddy.NewClient(baseURL, session.APIKey, session.APISecret, shopperID, &http.Client{Timeout: opts.timeout}), nil
}

func loadSessionOrEnv(opts globalOptions) (config.Session, string, error) {
	session, err := config.Load(opts.configPath)
	if err == nil {
		return session, "session", nil
	}
	if !errors.Is(err, config.ErrNoSession) {
		return config.Session{}, "", err
	}

	apiKey := os.Getenv("GODADDY_API_KEY")
	apiSecret := os.Getenv("GODADDY_API_SECRET")
	if apiKey == "" || apiSecret == "" {
		return config.Session{}, "", config.ErrNoSession
	}
	environment := envOrDefault("GODADDY_ENV", config.ProductionEnv)
	baseURL, err := config.ResolveBaseURL(environment, os.Getenv("GODADDY_BASE_URL"))
	if err != nil {
		return config.Session{}, "", err
	}
	return config.Session{
		APIKey:      apiKey,
		APISecret:   apiSecret,
		Environment: environment,
		BaseURL:     baseURL,
		ShopperID:   os.Getenv("GODADDY_SHOPPER_ID"),
	}, "environment", nil
}

func writeOutput(stdout io.Writer, asJSON bool, payload any, text string) int {
	if asJSON {
		return writeJSON(stdout, payload)
	}
	fmt.Fprint(stdout, text)
	return 0
}

func writeJSON(stdout io.Writer, payload any) int {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		return 1
	}
	return 0
}

// writeError reports a failure to stderr and returns exitCode so callers can
// `return writeError(...)`. Under --json it emits a structured object with a
// stable error code so an agent can branch without string-matching; otherwise
// it prints the human-readable message.
func writeError(stderr io.Writer, asJSON bool, code, message string, exitCode int) int {
	if asJSON {
		encoder := json.NewEncoder(stderr)
		encoder.SetIndent("", "  ")
		_ = encoder.Encode(map[string]any{
			"error":     code,
			"message":   message,
			"exit_code": exitCode,
		})
		return exitCode
	}
	fmt.Fprintln(stderr, message)
	return exitCode
}

// clientErrorCodeMessage maps a session/credential load failure to a stable
// error code and an actionable message.
func clientErrorCodeMessage(err error) (string, string) {
	if errors.Is(err, config.ErrNoSession) {
		return "missing_credentials", "no GoDaddy credentials; run `gogodaddy auth login` or set GODADDY_API_KEY and GODADDY_API_SECRET"
	}
	return "runtime_error", err.Error()
}

// buildAddCommand reconstructs a verbatim, replayable `records add ... --apply`
// command from the parsed record so an agent can copy it to execute the dry run.
func buildAddCommand(domain string, r godaddy.DNSRecord) string {
	parts := []string{"gogodaddy records add", "--domain", shellQuote(domain), "--type", r.Type, "--name", shellQuote(r.Name), "--data", shellQuote(r.Data)}
	if r.TTL != nil {
		parts = append(parts, "--ttl", fmt.Sprint(*r.TTL))
	}
	if r.Priority != nil {
		parts = append(parts, "--priority", fmt.Sprint(*r.Priority))
	}
	if r.Port != nil {
		parts = append(parts, "--port", fmt.Sprint(*r.Port))
	}
	if r.Weight != nil {
		parts = append(parts, "--weight", fmt.Sprint(*r.Weight))
	}
	if r.Protocol != "" {
		parts = append(parts, "--protocol", shellQuote(r.Protocol))
	}
	if r.Service != "" {
		parts = append(parts, "--service", shellQuote(r.Service))
	}
	return strings.Join(append(parts, "--apply"), " ")
}

// buildDeleteCommand reconstructs a verbatim, replayable `records delete ... --apply`
// command from the parsed selector.
func buildDeleteCommand(domain, recordType, name string, sel godaddy.DeleteSelector) string {
	parts := []string{"gogodaddy records delete", "--domain", shellQuote(domain), "--type", recordType, "--name", shellQuote(name), "--data", shellQuote(sel.Data)}
	if sel.TTL != nil {
		parts = append(parts, "--ttl", fmt.Sprint(*sel.TTL))
	}
	if sel.Priority != nil {
		parts = append(parts, "--priority", fmt.Sprint(*sel.Priority))
	}
	if sel.Port != nil {
		parts = append(parts, "--port", fmt.Sprint(*sel.Port))
	}
	if sel.Weight != nil {
		parts = append(parts, "--weight", fmt.Sprint(*sel.Weight))
	}
	if sel.Protocol != nil {
		parts = append(parts, "--protocol", shellQuote(*sel.Protocol))
	}
	if sel.Service != nil {
		parts = append(parts, "--service", shellQuote(*sel.Service))
	}
	return strings.Join(append(parts, "--apply"), " ")
}

// shellQuote single-quotes a value only when it contains characters outside a
// shell-safe set, so reconstructed commands are safe to paste.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	for _, r := range s {
		safe := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' || r == '@' || r == '/' || r == ':'
		if !safe {
			return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
	}
	return s
}

func printRecords(stdout io.Writer, records []godaddy.DNSRecord) {
	if len(records) == 0 {
		fmt.Fprintln(stdout, "No records found.")
		return
	}
	for _, record := range records {
		parts := []string{record.Type, record.Name, record.Data}
		if record.TTL != nil {
			parts = append(parts, "ttl="+fmt.Sprint(*record.TTL))
		}
		if record.Priority != nil {
			parts = append(parts, "priority="+fmt.Sprint(*record.Priority))
		}
		if record.Port != nil {
			parts = append(parts, "port="+fmt.Sprint(*record.Port))
		}
		if record.Weight != nil {
			parts = append(parts, "weight="+fmt.Sprint(*record.Weight))
		}
		if record.Protocol != "" {
			parts = append(parts, "protocol="+record.Protocol)
		}
		if record.Service != "" {
			parts = append(parts, "service="+record.Service)
		}
		fmt.Fprintln(stdout, strings.Join(parts, "\t"))
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `GoDaddy DNS CLI

Usage:
  gogodaddy [global flags] auth login --api-key KEY --api-secret SECRET
  gogodaddy [global flags] records add --domain DOMAIN --type TYPE --name NAME --data VALUE --apply
  gogodaddy [global flags] records list --domain DOMAIN --type TYPE --name NAME
  gogodaddy [global flags] records delete --domain DOMAIN --type TYPE --name NAME --data VALUE --apply

Global flags:
  --config PATH       global session file path
  --json              write JSON output
  --base-url URL      override API base URL for this command
  --shopper-id ID     set X-Shopper-Id for reseller calls
  --timeout DURATION  HTTP timeout, default 30s
  --version, -V       print version and exit

Commands:
  auth       Manage the saved GoDaddy API session and delegate access
  records    Add, list, and safely delete DNS records

Output:
  --json works on every command: data on stdout, diagnostics on stderr.

Exit codes:
  0  success
  1  runtime or API error (inspect stderr; a retry may help)
  2  usage error (fix flags, then retry)

See AGENTS.md for the full agent guide (auth, safety model, JSON shapes).
`)
}

func printAuthUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  gogodaddy auth login --api-key KEY --api-secret SECRET [--env production|ote]
  gogodaddy auth status
  gogodaddy auth delegate
  gogodaddy auth logout

Delegate access:
  auth delegate prints the GoDaddy delegate-access page and shows how to act on a
  delegated account with --shopper-id OWNER_SHOPPER_ID (the X-Shopper-Id header).
`)
}

func printRecordsUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  gogodaddy records add --domain DOMAIN --type TYPE --name NAME --data VALUE [--ttl N] --apply
  gogodaddy records list --domain DOMAIN --type TYPE --name NAME
  gogodaddy records delete --domain DOMAIN --type TYPE --name NAME --data VALUE [selectors] --apply

Examples:
  gogodaddy --json records list --domain example.com --type A --name www
  gogodaddy records add --domain example.com --type A --name www --data 192.0.2.10 --ttl 600          # dry run
  gogodaddy records add --domain example.com --type A --name www --data 192.0.2.10 --ttl 600 --apply  # execute
  gogodaddy records delete --domain example.com --type TXT --name _acme-challenge --data token         # dry run

Safeguards:
  add and delete are a safe dry run without --apply; no network mutation occurs.
  --json is a global flag (place it before the subcommand) and works on every command.
  add uses GoDaddy's additive PATCH endpoint.
  delete has no broad delete-all mode. It fetches one type/name set, requires --data,
  refuses ambiguous matches, and mutates only after --apply is present.
`)
}
