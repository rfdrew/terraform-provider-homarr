# Terraform Provider for Homarr

A Terraform provider for [Homarr](https://homarr.dev), managed through Homarr's
OpenAPI-compatible REST API.

## Versions

**Provider 0.3.0** — built and verified against **Homarr 2.4.1**. Every feature
works from Homarr **2.3.0**. Homarr 2.0–2.2 work too, but the features listed
under [Minimum Homarr versions](#minimum-homarr-versions) are unavailable or
degraded there. The provider warns when Homarr reports a major version other
than 2.

| Provider | Released | Verified against | Supported Homarr | Terraform |
| --- | --- | --- | --- | --- |
| 0.3.0 | unreleased | Homarr 2.4.1 | 2.3.0 and newer 2.x; 2.0–2.2 with the exceptions below | 1.9+ (see below) |
| 0.2.0 | 2026-10-05 | Homarr 1.76.2 | 1.x; not tested against 2.x | 1.9+ |
| 0.1.0 | 2026-08-06 | Homarr 1.73.0 | 1.x; not tested against 2.x | 1.9+ |

Homarr 2.4.1 changed nothing on the REST surface since 2.3.0, so both behave
identically; CI runs the acceptance tests against each. On Homarr 2, use 0.3.0.

Terraform 1.9 is enough for everything except:

* `password_wo` on `homarr_user` — Terraform 1.11+ (write-only attributes);
* importing by resource identity — Terraform 1.12+;
* the `homarr_board_add_item` action and the list resources used by
  `terraform query` — Terraform 1.14+.

Changes between releases are listed in [`CHANGELOG.md`](CHANGELOG.md).

## Why the surface is small

Homarr has two APIs. The REST surface under `/api/…` is documented with OpenAPI
and intended for external use; the tRPC surface under `/api/trpc/…` is an
implementation detail of the web UI. Only a handful of Homarr's routers are
exported through REST (see
[`packages/api/src/open-api.ts`](https://github.com/homarr-labs/homarr/blob/dev/packages/api/src/open-api.ts)):
`app`, `board`, `info`, `invite`, `serverSettings` and `user`, plus two
procedures each from `integration` — an outbound request proxy and a connection
test of a saved integration, both needing *full* access to that integration —
and `certificates` — reading and adding a trusted root CA, admin only. This
provider manages the first six and reads the connection test and the
certificate metadata as data sources; the request proxy is an imperative call
with no state to manage.

Consequently there is **no support for managing integrations, groups, search
engines or API keys**. Those can only be managed over tRPC, which uses
superjson-encoded payloads and changes freely between releases; building
resources on it would produce a provider that breaks on every Homarr upgrade.

**Board layouts and tiles are a special case: REST can write them but not read
them.** `PUT /api/boards/{id}/layouts` (2.3.0) rewrites a board's layouts, but
only with the mobile layout's id, which no REST response contains. Tiles can be
added (`POST /api/boards/items`), moved or resized
(`PATCH /api/boards/{boardId}/items/{itemId}/layouts/{layoutId}`), and a whole
board rewritten in one idempotent call (`PUT /api/boards/{id}/content`) — but no
endpoint reads sections, tiles or layouts back, or deletes a single tile. A
Terraform resource needs a read to refresh and detect drift, and the content
write deletes any section or tile the payload omits, so a stale state file would
wipe the board rather than converge on it. Tiles are therefore placed by the
`homarr_board_add_item` action, which fires once and remembers nothing, and
`homarr_board_copy` reproduces a board designed in the UI. See
[homarr-labs/homarr#6435](https://github.com/homarr-labs/homarr/issues/6435) for
the upstream discussion, and [`UPSTREAM.md`](UPSTREAM.md) for what Homarr would
need to add.

## Resources, data sources and actions

| Resource | Lifecycle | Notes |
| --- | --- | --- |
| `homarr_app` | full CRUD | Round-trips cleanly; drift detected on every attribute |
| `homarr_board` | create, read, rename, visibility, home boards, delete | `column_count` is create-only; import by id or `name=<name>`, plus the column count; `base_layout_id` from create; clearing a home board needs 2.3.0 |
| `homarr_board_copy` | copy (create), read, rename, visibility, home boards, delete | Copies a template board with its tiles; a snapshot, not a sync; a new `source_board_id` replaces the copy |
| `homarr_board_settings` | full CRUD | Readable since Homarr 2.3.0; reading needs *modify* access |
| `homarr_user` | create, read, rename, email, password (incl. write-only), image, preferences incl. header, delete | Credentials accounts only; `group_ids` is create-only; removing `email` forces replacement; import by id or `username=<name>` |
| `homarr_user_preferences` | read, update (attaches to an existing user) | For LDAP/OIDC and UI-made accounts; `user_id` is optional and defaults to the API key's own user; also sets the profile `image` |
| `homarr_invite` | create, read, delete | `token` returned once, at creation; an invite works once |
| `homarr_server_board_settings` | read, update (singleton) | Instance-wide board defaults |

| Data source | Purpose |
| --- | --- |
| `homarr_app` / `homarr_apps` | Look up one app by id or name, or list them all |
| `homarr_board` / `homarr_boards` | Look up one board by id or name (exact match first, then case-insensitive), or list them all |
| `homarr_board_settings` | Read one board's appearance settings |
| `homarr_user` | Look up one user by id or username; `provider_name` picks between accounts of different auth providers that share a name. Reports any account, LDAP and OIDC included; non-admin keys get only the summary (id, username, provider, email, image) for other users |
| `homarr_current_user` | The user the API key belongs to; any key |
| `homarr_users` | List users with any key; each has `provider_name`, and an optional `provider_name` argument filters the list |
| `homarr_user_preferences` | Read a user's preferences; the API key's own when `user_id` is omitted |
| `homarr_invites` | List open invites with an `expired` flag; no tokens. Needs an admin key and the credentials provider |
| `homarr_server_board_settings` | Read the instance board defaults without managing them. Needs an admin key |
| `homarr_root_certificate` | Inspect a trusted custom root CA by file name. Needs an admin key |
| `homarr_integration_connection` | Live connection test of a saved integration, for `check` blocks. Needs full access to the integration |
| `homarr_info` | Report the Homarr version |

| Action | Purpose |
| --- | --- |
| `homarr_board_add_item` | Terraform 1.14+. Adds a widget or app tile, optionally at a position in the base layout (`layout_id = homarr_board.x.base_layout_id`, with `x`, `y`, `width` and `height`). **Not idempotent**: every invocation adds another tile, and the tile is never read back or removed. Trigger it on `after_create` |

### Import by identity and `terraform query`

`homarr_app`, `homarr_user` and `homarr_invite` also have list resources, so
`terraform query` (Terraform 1.14+) can find existing objects and generate the
`import` blocks for them; the `homarr_user` list holds credentials accounts only.
Those three, together with `homarr_board_settings`, `homarr_user_preferences`
and `homarr_board_copy`, carry a resource identity — `id`, or `board_id` and
`user_id` for the two that attach to an existing object — so an `import` block
can name them with `identity` instead of an import id (Terraform 1.12+).
`homarr_board` has none, because importing a board also needs its column count.
Older Terraform releases ignore both and keep importing by id.

### Known limitations

These follow from the API, not from the provider's design. [`UPSTREAM.md`](UPSTREAM.md)
lists, item by item, what Homarr would have to add to lift them.

* **A board's column count is never reported.** It lives on the board's layouts,
  and nothing reads layouts back over REST. `PUT /api/boards/{id}/layouts`
  (2.3.0) can write them, but demands the full layout list including the mobile
  layout's id, which no REST call returns. Changing `column_count` therefore
  forces replacement, and import still needs a composite id:
  `terraform import homarr_board.x <id>,<column_count>`. `base_layout_id` comes
  only from the create response, so it is null after an import, and
  `homarr_board_copy` has none.
* **Board tiles can be written but never read.** Tiles can be added (the
  `homarr_board_add_item` action), moved or resized, and a whole board rewritten
  with `PUT /api/boards/{id}/content`, but no endpoint lists sections or tiles or
  deletes a single tile. Terraform cannot see, change or remove a tile once
  placed — and because the content write deletes omitted sections, a resource
  built on it would wipe the board from a stale state file.
* **Board names are unique case-insensitively, but REST create does not check.**
  An exact duplicate fails with HTTP 500, which the provider reports as "Board
  name already taken"; a name differing only in case would be accepted, so the
  provider refuses it right before creating — also between boards created in
  the same apply, whose creates, copies and renames it serialises. While
  planning it only warns, because a plan cannot see a board renamed or
  destroyed earlier in the same apply.
* **Renaming, changing visibility and deleting a board need full access.**
  Homarr answers 404 to anyone else. The provider re-lists the board to tell "no
  access" from "gone", so destroying a board without full access fails instead
  of silently dropping it from state.
* **Deleting a board does not clear the instance home boards.**
  `homarr_server_board_settings` then points at a missing board, and the
  provider warns on destroy. The release after 2.4.1 refuses every later change
  to those settings while a stored home board is private or missing, so clear
  the home boards before destroying the board they point at.
* **`PATCH` on apps is a full replace.** Every field is required-but-nullable, so
  the provider always sends all of them.
* **Creating a user returns an empty body.** The provider recovers the new id by
  comparing the user list before and after the create.
* **A user's email can be set or changed in place but never cleared.**
  `PUT /api/users/profile` ignores an empty email, so removing `email` replaces
  the user.
* **LDAP and OIDC accounts cannot be managed by `homarr_user`.** Homarr answers
  403 to renames and to email or password changes for them, so importing one is
  refused. One imported by 0.2.x is still read, with a warning, so it can be let
  go with a `removed` block or `terraform state rm`. Use
  `homarr_user_preferences` for such accounts.
* **Group membership is invisible.** `group_ids` is applied at creation; adding
  or removing a group forces replacement, because the provider cannot read
  memberships back. Reordering the list no longer replaces the user. After an
  import the provider knows of no memberships, so a configured `group_ids`
  plans a replacement, which deletes and recreates the account: leave
  `group_ids` out of imported users.
* **The user that owns the provider's API key cannot be destroyed or replaced.**
  Homarr deletes a user's API keys along with the account, which would cut
  Terraform off in the middle of the apply, so the provider refuses.
* **Header board shortcuts must be viewable by the target user.** Deleting a
  board does not remove its shortcut.
* **Profile pictures are `data:` URLs.** `image` must be a base64 `data:` URL
  (png, jpeg, gif or webp) of at most 350,000 characters, and is kept in state in
  full. For OIDC accounts, Homarr replaces any picture that is not a `data:` URL
  at every sign-in.
* **Invite tokens are returned once.** An imported invite has a null `token`.
* **Listing apps needs the `app-modify-all` permission** since Homarr 2.0.0,
  because the full catalogue exposes internal URLs. The provider falls back to
  `GET /api/apps/selectable`, which any authenticated key may call and which
  returns the same fields, so a scoped key keeps working. `homarr_users` falls
  back to `GET /api/users/selectable` the same way.
* **Integrations can be tested but neither listed nor managed.** Integration ids
  have to be copied from the integration's edit page in the UI.
* **Certificates can be added and read, but not listed or deleted.** The read is
  exposed as the `homarr_root_certificate` data source; there is no resource,
  because it could never be destroyed.
* **Groups, search engines, API keys, media and the remaining server settings
  have no REST surface** and cannot be managed here.

### Minimum Homarr versions

Everything not listed here uses endpoints present since Homarr 2.0.0 —
including `homarr_board_copy`, the `homarr_board_add_item` action, in-place
renames and email changes, and `image`. On an older release the features below
are unavailable or degrade as described, and the error or warning names the
release they need.

**2.1.0**, which added `GET /api/integrations/{id}/test-connection`:

* the `homarr_integration_connection` data source.

**2.2.0**, which added `GET /api/certificates/{fileName}`:

* the `homarr_root_certificate` data source.

**2.3.0**, which added `GET /api/boards/{id}/settings` and the
`/api/users/preferences` pair:

* drift detection for `homarr_board_settings` — against an older Homarr the
  resource degrades to its previous write-only behaviour and says so in a
  warning;
* the `homarr_board_settings` data source;
* setting `is_home` or `is_mobile_home` to `false` on `homarr_board` or
  `homarr_board_copy`, which clears a home board;
* setting the preference attributes on `homarr_user` — they are reported on
  older releases too, and the home boards and the search preferences
  (`default_search_engine_id`, `open_search_in_new_tab`, `ddg_bangs`) fall back
  to older endpoints, so a default search engine picked in the UI can still be
  cleared — and `homarr_user_preferences`;
* the `header` attribute on `homarr_user` and `homarr_user_preferences`, and in
  the user data sources — null on older releases, with a warning where it
  applies;
* the `homarr_current_user` data source, and the `homarr_user_preferences` data
  source without `user_id` — with `user_id` it falls back to
  `GET /api/users/{id}` on older releases, with `header` null;
* the check that refuses to destroy or replace the API key's own user — skipped
  on older releases.

## Usage

```hcl
terraform {
  required_providers {
    homarr = {
      source  = "rfdrew/homarr"
      version = "~> 0.3"
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

Full documentation for every resource, data source and action lives in
[`docs`](docs/) and, once published, on the Terraform Registry.

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
`homarr_invite` and `homarr_server_board_settings` all require an admin, as do
the `homarr_invites`, `homarr_server_board_settings` and
`homarr_root_certificate` data sources. `homarr_users`, `homarr_current_user`,
and username lookups in `homarr_user` work with any key.

## Development

Requires Go 1.26+, Terraform 1.9+ (1.14+ to run every acceptance test) and Docker
for the acceptance tests. `make docs` downloads Terraform 1.14.0 rather than
using the one on `PATH`, because older releases report no actions or list
resources and their pages would silently disappear from `docs/`; set
`TERRAFORM_DOCS_VERSION` to pick another exact release.

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

The container runs Homarr 2.4.1 by default. Set `HOMARR_TEST_IMAGE` to test
another release; the script replaces a leftover container built from a different
image.

```shell
eval "$(make homarr-up)"   # exports HOMARR_URL and HOMARR_API_KEY
make testacc               # acceptance tests against that instance
make homarr-down           # remove the container

# The oldest fully supported release, or the unreleased development build:
eval "$(HOMARR_TEST_IMAGE=ghcr.io/homarr-labs/homarr:v2.3.0 make homarr-up)"
eval "$(HOMARR_TEST_IMAGE=ghcr.io/homarr-labs/homarr:dev make homarr-up)"
```

CI runs the acceptance tests against Homarr 2.3.0 and 2.4.1 on several Terraform
releases, plus a non-blocking run against the `dev` image to catch upcoming
changes early.

A few acceptance tests need more than the throwaway instance offers:
`TestAccIntegrationConnectionDataSource` runs only when
`HOMARR_TEST_INTEGRATION_ID` names a saved integration, and
`TestAccRootCertificateDataSource_found` leaves a self-signed test CA behind,
because REST cannot delete one.

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

In [`CHANGELOG.md`](CHANGELOG.md), replace `Unreleased` in the newest heading
with the release date and point its compare link at the new tag instead of
`HEAD`; put the same date in the Released column of the
[version table](#versions) at the top of this README. Then tag a semver release
and push it; the `release` workflow runs GoReleaser and publishes signed
artefacts for the Terraform Registry.

```shell
git tag v0.3.0
git push origin v0.3.0
```

The repository needs two secrets: `GPG_PRIVATE_KEY` (ASCII-armoured, its public
half uploaded to the registry) and `PASSPHRASE`.

## License

[MPL-2.0](LICENSE)
