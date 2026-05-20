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

type globalOptions struct {
	configPath string
	json       bool
	baseURL    string
	shopperID  string
	timeout    time.Duration
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
		fmt.Fprintln(stderr, "missing --api-key or --api-secret; flags can also come from GODADDY_API_KEY and GODADDY_API_SECRET")
		return 2
	}
	resolvedBaseURL, err := config.ResolveBaseURL(*environment, *baseURL)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	session := config.Session{
		APIKey:      *apiKey,
		APISecret:   *apiSecret,
		Environment: *environment,
		BaseURL:     resolvedBaseURL,
		ShopperID:   *shopperID,
	}
	if err := config.Save(opts.configPath, session); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
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
		fmt.Fprintln(stderr, err)
		return 1
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
		fmt.Fprintln(stderr, err)
		return 1
	}
	return writeOutput(stdout, opts.json, map[string]any{
		"status": "deleted",
		"path":   opts.configPath,
	}, fmt.Sprintf("Deleted GoDaddy session at %s.\n", opts.configPath))
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
		fmt.Fprintln(stderr, "--domain is required")
		return 2
	}
	if err := godaddy.ValidateRecord(record, true); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	if !*apply {
		return writeOutput(stdout, opts.json, map[string]any{
			"dry_run": true,
			"action":  "add",
			"method":  "PATCH",
			"path":    "/v1/domains/" + *domain + "/records",
			"records": []godaddy.DNSRecord{record},
			"apply":   "rerun with --apply to add this record",
		}, "Dry run only. Rerun with --apply to add the DNS record.\n")
	}

	client, err := clientFromSession(opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	if err := client.AddRecords(ctx, *domain, []godaddy.DNSRecord{record}); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
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
		fmt.Fprintln(stderr, "--domain is required")
		return 2
	}
	if err := godaddy.ValidateTypeName(strings.ToUpper(*recordType), *name); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	client, err := clientFromSession(opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()
	records, err := client.GetRecords(ctx, *domain, strings.ToUpper(*recordType), *name, *offset, *limit)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if opts.json {
		return writeJSON(stdout, records)
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
		fmt.Fprintln(stderr, "--domain is required")
		return 2
	}
	if err := godaddy.ValidateTypeName(upperType, *name); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
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
		fmt.Fprintln(stderr, "delete requires --data so one record can be selected")
		return 2
	}

	client, err := clientFromSession(opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	existing, err := client.GetRecords(ctx, *domain, upperType, *name, 0, 0)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := godaddy.PlanDeleteOne(existing, selector)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if !*apply {
		return writeOutput(stdout, opts.json, map[string]any{
			"dry_run":           true,
			"action":            "delete_one",
			"domain":            *domain,
			"type":              upperType,
			"name":              *name,
			"record":            plan.Record,
			"use_direct_delete": plan.UseDirectDelete,
			"remaining_count":   len(plan.Remaining),
			"apply":             "rerun with --apply to delete exactly this record",
		}, "Dry run only. Rerun with --apply to delete exactly one matched DNS record.\n")
	}

	if plan.UseDirectDelete {
		if err := client.DeleteRecordsByTypeName(ctx, *domain, upperType, *name); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	} else {
		if err := client.ReplaceRecordsByTypeName(ctx, *domain, upperType, *name, plan.Remaining); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
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

Commands:
  auth       Manage the saved GoDaddy API session
  records    Add, list, and safely delete DNS records
`)
}

func printAuthUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  gogodaddy auth login --api-key KEY --api-secret SECRET [--env production|ote]
  gogodaddy auth status
  gogodaddy auth logout
`)
}

func printRecordsUsage(w io.Writer) {
	fmt.Fprint(w, `Usage:
  gogodaddy records add --domain DOMAIN --type TYPE --name NAME --data VALUE [--ttl N] --apply
  gogodaddy records list --domain DOMAIN --type TYPE --name NAME
  gogodaddy records delete --domain DOMAIN --type TYPE --name NAME --data VALUE [selectors] --apply

Safeguards:
  add uses GoDaddy's additive PATCH endpoint.
  delete has no broad delete-all mode. It fetches one type/name set, requires --data,
  refuses ambiguous matches, and mutates only after --apply is present.
`)
}
