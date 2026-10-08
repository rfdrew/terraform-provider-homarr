# Terraform Provider for Homarr

A Terraform provider for [Homarr](https://homarr.dev), managed through Homarr's
OpenAPI-compatible REST API.

Built and verified against **Homarr 2.3.0**.

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

**Board items (the tiles on a board) are a special case.** Homarr 2.3.0 added
`PUT /api/boards/{id}/content`, which writes a board's sections and items
declaratively in one idempotent call — but still offers no way to read them
back. A Terraform resource needs a read to refresh and detect drift, and the
write deletes any section or item the payload omits, so a stale state file
would wipe the board rather than converge on it. The capability is therefore
still left out. See
[homarr-labs/homarr#6435](https://github.com/homarr-labs/homarr/issues/6435) for
the upstream discussion.

## Resources and data sources

| Resource | Lifecycle | Notes |
| --- | --- | --- |
| `homarr_app` | full CRUD | Round-trips cleanly; drift detected on every attribute |
| `homarr_board` | create, read, rename, visibility, home boards, delete | `column_count` is create-only; clearing a home board needs 2.3.0 |
| `homarr_board_settings` | full CRUD | Readable since Homarr 2.3.0; reading needs *modify* access |
| `homarr_user` | create, read, password, preferences, delete | `group_ids` and `email` are create-only |
| `homarr_user_preferences` | read, update (attaches to an existing user) | For LDAP/OIDC and UI-made accounts |
| `homarr_invite` | create, read, delete | `token` returned once, at creation |
| `homarr_server_board_settings` | read, update (singleton) | Instance-wide board defaults |

| Data source | Purpose |
| --- | --- |
| `homarr_app` / `homarr_apps` | Look up one app by id or name, or list them all |
| `homarr_board` / `homarr_boards` | Look up one board by id or name, or list them all |
| `homarr_board_settings` | Read one board's appearance settings |
| `homarr_users` | List users, e.g. to resolve an id without importing the account |
| `homarr_info` | Report the Homarr version |

### Known limitations

These follow from the API, not from the provider's design:

* **A board's column count is never reported.** It lives on the board's layout.
  Homarr 2.3.0 added `PUT /api/boards/{id}/layouts`, but nothing reads layouts
  back and the endpoint is unusable from a REST-only client anyway — it demands
  the full layout array including the mobile layout's id, which no REST call
  returns. Changing `column_count` still forces replacement, and import still
  needs a composite id: `terraform import homarr_board.x <id>,<column_count>`.
* **Board tiles are still unmanageable.** `PUT /api/boards/{id}/content` can
  write a whole board declaratively, but no endpoint reads sections or items
  back, so a resource could never converge — and because omitted sections are
  deleted, a stale state file would wipe the board.
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
* **Listing apps needs the `app-modify-all` permission** since Homarr 2.0.0,
  because the full catalogue exposes internal URLs. The provider falls back to
  `GET /api/apps/selectable`, which any authenticated key may call and which
  returns the same fields, so a scoped key keeps working.
* **Groups, integrations, search engines and API keys have no REST surface** and
  cannot be managed here. Certificates have create and read but no delete.

### Requires Homarr 2.3.0

Most of the provider works against much older releases, but these need 2.3.0,
which added `GET /api/boards/{id}/settings` and the `/api/users/preferences`
pair:

* drift detection for `homarr_board_settings` and the `homarr_board_settings`
  data source — against an older Homarr the resource degrades to its previous
  write-only behaviour and says so in a warning;
* setting `is_home` or `is_mobile_home` to `false`, which clears a home board;
* the preference attributes on `homarr_user`, and `homarr_user_preferences`.

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
