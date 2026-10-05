# Terraform Provider for Homarr

A Terraform provider for [Homarr](https://homarr.dev), managed through Homarr's
OpenAPI-compatible REST API.

Built and verified against **Homarr 1.76.2**.

## Why the surface is small

Homarr has two APIs. The REST surface under `/api/…` is documented with OpenAPI
and intended for external use; the tRPC surface under `/api/trpc/…` is an
implementation detail of the web UI. Only a handful of Homarr's routers are
exported through REST (see
[`packages/api/src/open-api.ts`](https://github.com/homarr-labs/homarr/blob/dev/packages/api/src/open-api.ts)):
`app`, `board`, `info`, `invite`, `serverSettings` and `user`, joined since 2.x by
two admin-only odds and ends — `integration` (an outbound request proxy and a
connection test) and `certificates` (2.2.0). This provider covers the first six;
the latter two are imperative calls rather than declarative state.

Consequently there is **no support for integrations, groups or board layouts**.
Those exist only over tRPC, which uses superjson-encoded payloads and changes
freely between releases; building resources on it would produce a provider that
breaks on every Homarr upgrade.

**Board items (the tiles on a board) are a special case.** Creating one *is*
available over REST — `POST /api/boards/items` adds a widget or app tile and
returns its id. But there is no endpoint to list, update, move, resize or delete
an item, so a tile can be created and then never reconciled. A Terraform resource
built on create-only semantics could only ever add tiles to a board, never
converge on a desired state, so this provider leaves the capability out. See
[homarr-labs/homarr#6435](https://github.com/homarr-labs/homarr/issues/6435) for
the upstream discussion.

## Resources and data sources

| Resource | Lifecycle | Notes |
| --- | --- | --- |
| `homarr_app` | full CRUD | Round-trips cleanly; drift detected on every attribute |
| `homarr_board` | create, read, rename, visibility, home boards, delete | `column_count` is create-only; `is_home`/`is_mobile_home` are set-only |
| `homarr_board_settings` | write-only | Homarr has no `GET` for these; drift is undetectable |
| `homarr_user` | create, read, password, home boards, delete | `group_ids` and `email` are create-only |
| `homarr_invite` | create, read, delete | `token` returned once, at creation |
| `homarr_server_board_settings` | read, update (singleton) | Instance-wide board defaults |

| Data source | Purpose |
| --- | --- |
| `homarr_app` / `homarr_apps` | Look up one app by id or name, or list them all |
| `homarr_board` / `homarr_boards` | Look up one board by id or name, or list them all |
| `homarr_users` | List users, e.g. to resolve an id without importing the account |
| `homarr_info` | Report the Homarr version |

### Known limitations

These follow from the API, not from the provider's design:

* **Board settings cannot be read back.** `PATCH /api/boards/{id}/settings` exists
  but there is no matching `GET`. `homarr_board_settings` therefore records what
  Terraform last applied. Changes made in the UI are not reverted until something
  in the configuration changes, and removing an attribute does not reset it.
* **A home board can be selected but never cleared.** `PATCH /api/boards/{id}/home`
  sets the calling user's home board; Homarr has no call that unsets it. So
  `homarr_board`'s `is_home` and `is_mobile_home` accept `true` and reject an
  explicit `false`, and leaving them out keeps them computed. They are also a
  per-user singleton, so setting one board's flag silently clears another's —
  declaring `is_home = true` on two boards makes them fight on every apply. To
  clear a slot outright, set `homarr_user.home_board_id` to `null`.
* **A board's column count is never reported.** It lives on the board's layout,
  which is tRPC-only. Changing it forces replacement, and import needs a
  composite id: `terraform import homarr_board.x <id>,<column_count>`.
* **`PATCH` on apps is a full replace.** Every field is required-but-nullable, so
  the provider always sends all of them.
* **Creating a user returns an empty body.** The provider recovers the new id by
  looking the username up immediately afterwards. Homarr also lowercases
  usernames, so state may differ in case from your configuration.
* **A user's email cannot be changed in place.** Homarr's only email-editing
  endpoint acts on the account behind the API key, so changing `email` forces
  replacement.
* **Group membership is invisible.** `group_ids` is applied at creation and then
  forces replacement, because the provider cannot read it back.
* **Invite tokens are returned once.** An imported invite has a null `token`.

## Usage

```hcl
terraform {
  required_providers {
    homarr = {
      source  = "rfdrew/homarr"
      version = "~> 0.1"
    }
  }
}

provider "homarr" {
  # Or set HOMARR_URL and HOMARR_API_KEY in the environment.
  url     = "https://homarr.example.com"
  api_key = var.homarr_api_key
}

resource "homarr_board" "infra" {
  name         = "infra"
  column_count = 12
  is_public    = true
}

resource "homarr_app" "grafana" {
  name        = "Grafana"
  description = "Metrics and dashboards"
  icon_url    = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@master/svg/grafana.svg"
  href        = "https://grafana.example.com"
  ping_url    = "https://grafana.example.com/api/health"
}
```

Full documentation for every resource and data source lives in [`docs`](docs/)
and, once published, on the Terraform Registry.

### Provider configuration

| Argument | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `url` | `HOMARR_URL` | — | Homarr origin, e.g. `https://homarr.example.com` |
| `api_key` | `HOMARR_API_KEY` | — | API key as `<id>.<token>` |
| `insecure_skip_verify` | `HOMARR_INSECURE_SKIP_VERIFY` | `false` | Skip TLS verification |
| `timeout_seconds` | `HOMARR_TIMEOUT_SECONDS` | `30` | Per-request HTTP timeout |

### Getting an API key

Create one in the Homarr UI under **Management > Tools > API**. Keys never expire
and inherit the permissions of the user that created them; `homarr_user`,
`homarr_invite` and `homarr_server_board_settings` all require an admin.

## Development

Requires Go 1.24+, Terraform 1.9+ and Docker for the acceptance tests.

```shell
make build          # build the provider binary
make test           # unit tests, no Homarr needed
make docs           # regenerate docs/ from schemas and examples
make lint           # gofmt + go vet + terraform fmt
```

### Running against a local Homarr

`make homarr-up` boots a throwaway container, completes Homarr's onboarding and
mints an admin API key. Onboarding is not part of the REST API, so the script
drives the same tRPC endpoints the UI uses.

```shell
eval "$(make homarr-up)"   # exports HOMARR_URL and HOMARR_API_KEY
make testacc               # acceptance tests against that instance
make homarr-down           # remove the container
```

> Acceptance tests create and destroy real objects. Only ever point them at a
> disposable instance.

### Manual testing with dev overrides

```shell
go build -o /tmp/tfbin/terraform-provider-homarr .

cat > /tmp/dev.tfrc <<'EOF'
provider_installation {
  dev_overrides {
    "rfdrew/homarr" = "/tmp/tfbin"
  }
  direct {}
}
EOF

export TF_CLI_CONFIG_FILE=/tmp/dev.tfrc
terraform plan   # no `terraform init` — dev overrides bypass the registry
```

## Releasing

Tag a semver release and push it; the `release` workflow runs GoReleaser and
publishes signed artefacts for the Terraform Registry.

```shell
git tag v0.1.0
git push origin v0.1.0
```

The repository needs two secrets: `GPG_PRIVATE_KEY` (ASCII-armoured, its public
half uploaded to the registry) and `PASSPHRASE`.

## License

[MPL-2.0](LICENSE)
