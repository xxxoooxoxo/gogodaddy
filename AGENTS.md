<!--
Managed agent guide for gogodaddy. Safe for an agent to read top-to-bottom before
running anything. Hand-maintained — keep it faithful to internal/cli/cli.go behavior.
-->

# AGENTS.md

## Overview

`gogodaddy` is an unofficial GoDaddy DNS CLI (Go), built to be driven by AI agents.
Mutations are **dry-run by default** and require an explicit `--apply`. There is no
delete-all: single-record delete refuses ambiguous matches. Adds are additive.

## Install and verify

```sh
go install github.com/xxxoooxoxo/gogodaddy/cmd/gogodaddy@latest
export PATH="$(go env GOPATH)/bin:$PATH"   # if `which gogodaddy` is empty
gogodaddy --help
```

Run from a checkout without installing:

```sh
go run ./cmd/gogodaddy --help
gogodaddy --version   # or -V; add --json for {"version":"..."}
```

## Authentication

Credentials are a GoDaddy API key + secret, sent as `Authorization: sso-key KEY:SECRET`.
Prefer environment variables — they avoid writing the secret to disk:

| Variable | Purpose |
|---|---|
| `GODADDY_API_KEY` | API key (required) |
| `GODADDY_API_SECRET` | API secret (required) |
| `GODADDY_ENV` | `production` (default) or `ote` |
| `GODADDY_BASE_URL` | override the base URL |
| `GODADDY_SHOPPER_ID` | `X-Shopper-Id` for delegate / reseller calls |
| `GODADDY_CLI_CONFIG` | session file path |

Or save a session (writes `session.json`, mode `0600`, under the user config dir):

```sh
gogodaddy auth login --api-key "$GODADDY_API_KEY" --api-secret "$GODADDY_API_SECRET"
gogodaddy auth status
```

Resolution precedence: **saved session > env vars**; `--base-url`, `--shopper-id`, and
`--config` flags override. Secrets are redacted in all output — never print them. A secret
passed via `--api-key/--api-secret` is visible in `ps` and shell history; prefer env vars.

## Environments

Production `https://api.godaddy.com` is the default; OTE is `https://api.ote-godaddy.com`
(`--env ote` / `GODADDY_ENV=ote`). Do not mix production credentials with OTE, or vice versa.

## Safety model — read before any mutation

- `records add` and `records delete` are **dry-run by default**: they print the plan and
  make **no network mutation**. Re-run the same command with `--apply` to execute.
- `records list` and every `auth` command are read-only.
- `delete` has no delete-all. It requires `--data`, fetches a single type/name set, and
  refuses to act unless **exactly one** record matches. Disambiguate with
  `--ttl/--priority/--port/--weight/--protocol/--service`.
- `add` uses GoDaddy's **additive** PATCH, so re-running the same add creates a duplicate.
  It is **not idempotent** — after a timeout, `records list` first instead of blindly retrying.

## JSON output contract

`--json` is a **global** flag (place it before the subcommand) and works on every command.
Data goes to **stdout**; diagnostics and errors go to **stderr**. Current shapes:

| Command | stdout under `--json` |
|---|---|
| `auth login` | `{status, path, environment, base_url, api_key, shopper_id}` |
| `auth status` | `{authenticated, source, path, environment, base_url, api_key, shopper_id}` |
| `auth delegate` | `{delegate_access_url, shopper_id, shopper_id_source, next_step}` |
| `records list` | `{domain, type, name, count, offset, limit, records:[{type,name,data,ttl,...}]}` |
| `records add` / `delete` (dry run) | `{dry_run:true, action, ..., next_command:"<runnable cmd>"}` |
| `records add` / `delete` (`--apply`) | `{status:"added"|"deleted", domain, ...record}` |

On a dry run, `next_command` is a verbatim, paste-ready command (ends in `--apply`) that
executes exactly the planned change — copy it instead of reconstructing flags.

### Errors

Under `--json`, a failure prints a structured object to **stderr** and the process exits non-zero:

```json
{"error": "missing_flag", "message": "--domain is required", "exit_code": 2}
```

Stable `error` codes: `missing_flag`, `invalid_record`, `invalid_environment`,
`missing_credentials`, `api_error`, `no_single_match`, `runtime_error`. Branch on `error`,
not on the message text.

## Exit codes

| Code | Meaning | Agent action |
|---|---|---|
| 0 | success | continue |
| 1 | runtime / API error | inspect stderr; a retry may help (network, 5xx, 429) |
| 2 | usage error (bad or missing flags) | fix arguments, then retry |

## Command examples

```sh
# read-only list
gogodaddy --json records list --domain example.com --type A --name www

# add: dry run, then apply
gogodaddy records add --domain example.com --type A --name www --data 192.0.2.10 --ttl 600
gogodaddy records add --domain example.com --type A --name www --data 192.0.2.10 --ttl 600 --apply

# delete exactly one record: dry run, then apply
gogodaddy records delete --domain example.com --type TXT --name _acme-challenge --data token
gogodaddy records delete --domain example.com --type TXT --name _acme-challenge --data token --apply
```

## Delegate / reseller access

A standard API key reaches only its own account. To manage another account, a human must
first grant access at https://account.godaddy.com/access, then calls target that account
via its shopper id:

```sh
gogodaddy --shopper-id OWNER_SHOPPER_ID records list --domain example.com
```

This is a one-time **human** action — surface it to the user; do not loop retrying a `403`.
Run `gogodaddy auth delegate` for the link and the exact next step.

## Never do

- Never print the API key or secret.
- Never `--apply` before reviewing the dry-run plan.
- Never broad-delete a type/name set via raw `curl`; use the guarded `records delete`.
- Never drop `--data` on delete, or work around the ambiguous-match refusal.
- Never mix production and OTE credentials.

## Verify changes to this repo

```sh
go vet ./...
go test ./...   # tests live in internal/cli, internal/config, internal/godaddy
```
