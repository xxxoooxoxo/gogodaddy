# gogodaddy

Unofficial GoDaddy CLI for agents.

A small Go CLI for GoDaddy DNS automation. It is built for agent-driven use:
mutations require explicit `--apply`, there is no broad delete-all command, and
single-record delete refuses ambiguous matches.

The CLI uses the GoDaddy Domains API from:

https://developer.godaddy.com/swagger/swagger_domains.json

GoDaddy authenticates requests with:

```sh
Authorization: sso-key API_KEY:API_SECRET
```

## Install locally

From this directory:

```sh
go install ./cmd/gogodaddy
```

From GitHub:

```sh
go install github.com/xxxoooxoxo/gogodaddy/cmd/gogodaddy@latest
```

## Auth

Save a global session:

```sh
gogodaddy auth login --api-key "$GODADDY_API_KEY" --api-secret "$GODADDY_API_SECRET"
```

By default the session targets production, `https://api.godaddy.com`. For OTE:

```sh
gogodaddy auth login --env ote --api-key "$GODADDY_API_KEY" --api-secret "$GODADDY_API_SECRET"
```

The session file is stored under the user's config directory as
`gogodaddy/session.json` with `0600` permissions. Override it with:

```sh
gogodaddy --config /path/to/session.json auth status
```

Environment variables can also be used without saving a session:

```sh
GODADDY_API_KEY=... GODADDY_API_SECRET=... gogodaddy auth status
```

## Add DNS Records

Dry run:

```sh
gogodaddy records add --domain example.com --type A --name www --data 192.0.2.10 --ttl 600
```

Apply:

```sh
gogodaddy records add --domain example.com --type A --name www --data 192.0.2.10 --ttl 600 --apply
```

This uses GoDaddy's additive `PATCH /v1/domains/{domain}/records` endpoint.

## List DNS Records

The swagger exposes record lookup by type and name:

```sh
gogodaddy --json records list --domain example.com --type A --name www
```

## Delete One DNS Record

There is no raw delete-all command. GoDaddy's direct DELETE endpoint removes all
records for a type/name, so this CLI first fetches that type/name set and requires
a selector that matches exactly one record.

Dry run:

```sh
gogodaddy records delete --domain example.com --type TXT --name _acme-challenge --data token-value
```

Apply:

```sh
gogodaddy records delete --domain example.com --type TXT --name _acme-challenge --data token-value --apply
```

If multiple records match `--data`, add selectors such as `--ttl`, `--priority`,
`--port`, `--weight`, `--protocol`, or `--service`.

When more than one record exists for the same type/name, deletion is implemented
by replacing that type/name set with the one selected record removed. When the
selected record is the only record in that type/name set, the CLI calls GoDaddy's
DELETE endpoint only after confirming the fetched set contains exactly one record.
