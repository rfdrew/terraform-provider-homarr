# Changelog

All notable changes to this provider are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/spec/v2.0.0.html). While the provider is
at 0.x, a minor release may change behaviour; such changes are listed under
**Breaking and behaviour changes**.

## [0.3.1] - Unreleased

Verified against Homarr 2.5.2. No change in behaviour.

### Changed

- Built and verified against Homarr 2.5.2, previously 2.4.1. Homarr 2.5.0–2.5.2
  changed nothing on the REST surface; 0.3.0 already handles what 2.5.0 does
  differently — it checks the stored home boards on every change to the
  instance board settings, drops the creator's email from `GET /api/boards`,
  and on PostgreSQL creates users with `open_search_in_new_tab = true`, as
  SQLite already did.
- The descriptions name Homarr 2.5.0 where they said "the release after 2.4.1".
- The throwaway test instance defaults to Homarr 2.5.2, and CI runs the
  acceptance tests against Homarr 2.3.0, 2.4.1 and 2.5.2.

## [0.3.0] - 2026-10-12

Homarr 2 support. Built and verified against Homarr 2.4.1.

### Requirements

- **Homarr 2.x.** Verified against 2.4.1, and every feature works from 2.3.0.
  On 2.0–2.2 the features that need a newer release fail with an error naming
  that release, or degrade as documented: `homarr_integration_connection`
  needs 2.1.0, `homarr_root_certificate` 2.2.0, and board-settings drift
  detection, the `homarr_board_settings` data source, clearing a home board,
  user preferences other than the home boards and the search preferences,
  `header`, `homarr_current_user` and the API-key-owner check need 2.3.0.
  Homarr 1.x is no longer tested; the provider warns when Homarr reports a
  major version other than 2.
- **Terraform 1.9+.** `password_wo` needs 1.11+, import by resource identity
  1.12+, and the `homarr_board_add_item` action and list resources 1.14+.

### Breaking and behaviour changes

- `homarr_board_settings.opacity` is a whole number instead of a float. Homarr
  stores an integer, so a fractional value was truncated and would have caused a
  permanent diff; fractional values in configuration are now rejected.
- Values Homarr would reject or silently rewrite are refused at plan time:
  - `homarr_app`: leading or trailing whitespace; `""` for `description`, `href`
    or `ping_url` (use `null`); a `javascript:` `href` in any letter case; a
    `ping_url` that is not a lower-case `http://` or `https://` URL with a host.
  - `homarr_user`: a `username` that is not lowercase or has surrounding
    whitespace; an `email` Homarr would not accept; duplicate or empty
    `group_ids`.
  - `homarr_invite`: an `expiration_date` with a fractional second, a lower-case
    `T` or `Z`, or an offset not written `+hh:mm` or `-hh:mm`.
- `homarr_user`: changing `username`, or setting or changing `email`, is an
  in-place update (`~`) instead of a replacement (`-/+`). Reordering `group_ids`,
  or switching it between `null` and `[]`, no longer replaces the user; adding or
  removing a group still does, and so does removing `email`.
- `homarr_user`: importing an LDAP or OIDC account is refused, and so is a plan
  that would rename one already in state or change its email or password. Such
  accounts are still refreshed, with a warning, so a `removed` block or
  `terraform state rm` can let go of them; manage them with
  `homarr_user_preferences`.
- `homarr_user`: destroying or replacing the user that owns the provider's API
  key is refused, at plan time and in delete. Detecting it needs Homarr 2.3.0;
  the check is skipped on older releases.
- `homarr_user` and `homarr_users`: an email stored as `""`, as the UI does,
  reads as `null`.
- `homarr_user`: the new `default_search_engine_id` is owned outright, like
  `home_board_id`. After upgrading from 0.2.x, the first refresh records any
  search engine a managed user picked in the UI, and the first plan sets it to
  `null`, so applying clears it; that plan also shows the computed attributes,
  such as the other preferences, as known after apply, which makes the one real
  change easy to miss. To keep the choice, copy the value into the
  configuration, or add the attribute to `ignore_changes`, before applying.
- `homarr_user`: `password` is optional, because exactly one of `password` and
  `password_wo` must be set. Existing configurations are unaffected.
- `homarr_board` and `homarr_board_copy`: destroying a board without full access
  to it fails instead of silently dropping it from state; use
  `terraform state rm` or a `removed` block to stop managing it. A name that
  differs from another board's only in case is refused right before creating,
  and any name clash is warned about at plan time.
- `homarr_board`: the visibility error caused by the instance-wide home board is
  attached to `is_public`, and access 404s get their own message.
- `homarr_boards` is sorted by name, case-insensitively, then by exact name;
  `homarr_users` by name, then id, with unnamed users last. Index-based
  references into either list may point at a different element once.
- `homarr_users.users[*].name` can be `null`.
- The `homarr_board` data source looked up by name falls back to a
  case-insensitive match and reports the board's real spelling; several case
  variants are an ambiguity error instead of not-found.
- New computed attributes appear in state after the first refresh, without a
  diff or a state upgrade: `header` and `image` on `homarr_user` and
  `homarr_user_preferences`, and `users[*].provider_name` on `homarr_users`.
  `base_layout_id` on `homarr_board` is the exception: Homarr reports it only
  when creating a board, so it stays `null` for boards created by 0.2.x or
  imported.
- Not a provider change, but one to prepare for: the Homarr release after 2.4.1
  refuses every change to the instance board settings while either stored home
  board is private or missing — including the private onboarding board Homarr
  selects in both slots. Manage `home_board_id` and `mobile_home_board_id` of
  `homarr_server_board_settings` together; the error names the attribute to fix.

### Added

- Resource `homarr_board_copy`: creates a board by duplicating a template, with
  its tiles, layouts and appearance. A snapshot, not a sync. Import by id or
  `name=<name>`.
- Resource `homarr_user_preferences`: manages the preferences of an existing
  account — LDAP, OIDC, the onboarding admin, or one made in the UI. `user_id`
  is optional and defaults to the API key's own user. Homarr 2.3.0+.
- Action `homarr_board_add_item` (Terraform 1.14+): adds a widget or app tile,
  optionally moved in the base layout through
  `layout_id = homarr_board.<name>.base_layout_id` with `x`, `y`, `width` and
  `height`. Not idempotent; the tile is never read back or removed.
- Data sources `homarr_board_settings` (Homarr 2.3.0+), `homarr_user`,
  `homarr_current_user` (2.3.0+), `homarr_user_preferences`, `homarr_invites`,
  `homarr_server_board_settings`, `homarr_root_certificate` (2.2.0+, admin) and
  `homarr_integration_connection` (2.1.0+, full access to the integration).
- Import by resource identity (Terraform 1.12+) for `homarr_app`, `homarr_user`,
  `homarr_invite`, `homarr_board_settings`, `homarr_user_preferences` and
  `homarr_board_copy`, and list resources for `terraform query` (Terraform
  1.14+) for `homarr_app`, `homarr_user` (credentials accounts only) and
  `homarr_invite`.
- `homarr_board`: computed `base_layout_id`, known for boards created by 0.3.0
  and later; import by `name=<name>,<column_count>`; `is_home` and
  `is_mobile_home` can be set to `false` (Homarr 2.3.0+), and `is_home = false`
  undoes the home board Homarr assigns to a new board.
- `homarr_board_settings`: on Homarr 2.3.0+, drift detection, and an import by
  board id that reads back every attribute instead of only the id; every
  attribute is optional and computed. Older releases keep the write-only
  behaviour, with a warning.
- `homarr_user`: eight more preferences — `default_search_engine_id`,
  `color_scheme`, `byte_unit_system`, `first_day_of_week`, `ping_icons_enabled`,
  `enable_right_click_on_widgets`, `open_search_in_new_tab` and `ddg_bangs` —
  applied together with the account in one call (before Homarr 2.3.0, only the
  home boards and the three search preferences can be set, through older
  endpoints); the `header` layout; the profile `image`; `password_wo` and
  `password_wo_version` (Terraform 1.11+); import by `username=<name>`.
- `homarr_users`: works with any API key, reports each user's `provider_name`,
  and takes a `provider_name` filter.
- `homarr_server_board_settings`: `""` clears `home_board_id` or
  `mobile_home_board_id`.
- The provider warns when Homarr reports a major version other than 2, and
  features built on endpoints added after Homarr 2.0.0 name the release they
  need instead of failing with a misleading not-found error.

### Changed

- Built and verified against Homarr 2.4.1, previously 1.76.2. The throwaway test
  instance follows Homarr 2's claim-gated onboarding and defaults to 2.4.1;
  `HOMARR_TEST_IMAGE` picks another image.
- CI runs the acceptance tests against Homarr 2.3.0 and 2.4.1 on Terraform 1.9,
  1.12 and 1.14, plus a non-blocking run against Homarr's `dev` image.
- Listing apps falls back to `GET /api/apps/selectable` when Homarr 2 refuses
  `GET /api/apps` to a key without `app-modify-all`, so the app data sources
  work with any key.
- The documentation describes the REST surface as of Homarr 2.4.1: layouts and
  tiles are writable but unreadable, per-board settings are readable since
  2.3.0, and users can be renamed and their email changed in place.
  `homarr_app`, `homarr_invite`, `homarr_server_board_settings` and
  `homarr_board_copy` document their side effects and the permissions they need.
- [`UPSTREAM.md`](UPSTREAM.md) lists what Homarr is missing over REST, and what
  the provider would build on each addition.

### Fixed

- `homarr_user`:
  - a failed read-back after create, or a create that fails on a bad group id
    after Homarr already made the account, no longer orphans the user; it is
    recorded as tainted;
  - the new user's id is found by comparing the user list before and after the
    create, so an LDAP or OIDC account with the same name is never mistaken for
    it;
  - users Homarr stores without a name are handled;
  - on Homarr before 2.3.0 with a non-admin key, the 2.3.0 features name the
    version instead of failing with a raw 403;
  - a `null` header carried over from 0.2.x state no longer causes "inconsistent
    result after apply", LDAP or OIDC accounts in state no longer block `removed`
    blocks or destroy, and a failed header read after create no longer taints
    the user.
- `homarr_board` and `homarr_board_copy`:
  - destroy, rename and visibility changes no longer treat Homarr's "no full
    access" 404 as success or as "board gone";
  - a taken name on create, which Homarr answers with HTTP 500, is reported
    against `name`;
  - a failed read-back after create keeps the board in state, tainted, instead
    of leaking it;
  - destroying the instance home board warns that the server settings still
    point at it.
- `homarr_invite`: a failed read-back after create keeps the one-time token in
  state and taints the resource, instead of losing the token.
- `homarr_server_board_settings`: when Homarr refuses a home board as not
  public, the error names the attribute holding it, including boards that are
  only stored, not sent; the destroy warning names the home boards left
  selected.
- Corrected descriptions and examples: `homarr_users` (sort order, nullable
  names, image format), user lookups that resolved by name alone, the
  `homarr_info` example for 2.4.1, and stale claims about tRPC-only layouts,
  create-only tiles and unreadable board settings.

## [0.2.0] - 2026-10-05

Verified against Homarr 1.76.2.

### Added

- `homarr_board`: `is_home` and `is_mobile_home` are configurable, through
  `PATCH /api/boards/{id}/home` and `.../mobile-home`. Both are set-only, because
  Homarr had no REST call that clears a home board, so an explicit `false` is
  rejected at plan time.

### Changed

- Verified against Homarr 1.76.2, previously 1.73.0. The throwaway test instance
  is pinned to it and recreated when a leftover container was built from another
  image.
- `homarr_app.href` is documented for Homarr 1.74.0 and later, which reject only
  `javascript:` URIs.
- The documentation explains why board tiles are left out: REST can create one
  but not list, update or delete it.

### Fixed

- A board created but not selected as home board is recorded as tainted instead
  of leaked.
- The "cannot make a home board private" guard is documented on `is_public`,
  since it concerns the instance-wide home board.

## [0.1.0] - 2026-08-06

Initial release, verified against Homarr 1.73.0.

### Added

- Resources `homarr_app`, `homarr_board`, `homarr_board_settings` (write-only:
  Homarr had no `GET` for board settings), `homarr_user`, `homarr_invite` and
  `homarr_server_board_settings`.
- Data sources `homarr_app`, `homarr_apps`, `homarr_board`, `homarr_boards`,
  `homarr_users` and `homarr_info`.

[0.3.1]: https://github.com/rfdrew/terraform-provider-homarr/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/rfdrew/terraform-provider-homarr/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/rfdrew/terraform-provider-homarr/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/rfdrew/terraform-provider-homarr/releases/tag/v0.1.0
