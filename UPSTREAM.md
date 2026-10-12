# What Homarr's REST API is missing

This provider uses only Homarr's REST/OpenAPI surface (see "Why the surface is
small" in the [README](README.md)): a managed resource needs a create, a REST
read-back and an update or delete. This file lists, item by item, what Terraform
users would want to manage, what Homarr offers today, what upstream would have to
change, and what the provider would then add.

* **Provider:** 0.3.0.
* **Homarr:** 2.4.1, tag `v2.4.1` = `ebbd4876d` (2026-10-10). Rechecked against
  `origin/dev` @ `542727e4f` (2026-10-12); the REST surface is identical apart
  from the notes in items 8, 10 and 16.
* **References:** `v2.4.1:<path>:<line>` is the Homarr tree at the tag, `dev:` is
  `origin/dev`, `origin/<branch>:` an unmerged branch, and `t2o:` is
  `trpc-to-openapi@3.3.0/dist/esm`, the version Homarr pins. Within an item, a
  bare file name such as `board.ts:668` refers to the file cited in full earlier
  in that item; router files live under `packages/api/src/router/`.

Upstream discussion of the largest gap, board content, is in
[homarr-labs/homarr#6435](https://github.com/homarr-labs/homarr/issues/6435).

## Summary

| # | Capability | REST today | Main upstream change | Priority | Effort | Existing upstream work |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | [Board content](#1-board-content-sections-tiles-options) | write-only `PUT /content`, `POST /items`, item-layout `PATCH` | `GET /api/boards/{id}/content`; per-item `GET`/`PATCH`/`DELETE` | high | M | `origin/add-more-openapi` items router (stale); #6435 |
| 2 | [Layouts, column count](#2-board-layouts-and-column-count) | write-only `PUT /layouts`, needs the mobile id | `GET /api/boards/{id}/layouts`, or `mobileLayoutId` in create output | high | S | none |
| 3 | [Board permissions](#3-board-permissions) | none | `GET`/`PUT /api/boards/{id}/permissions[/users\|/groups]` | high | S | none |
| 4 | [Groups](#4-groups) | none | register `groupRouter`; meta and output on 9 procedures | high | M | `origin/add-more-openapi` 7ea71a57b (partial, stale) |
| 5 | [A user's group membership](#5-a-users-group-membership) | create-only `groupIds` | `groupIds` in `GET /api/users/{id}`; `PUT /api/users/{id}/groups` | high | S | none |
| 6 | [Integrations](#6-integrations) | request proxy and connection test only | CRUD, kinds, permissions; throw instead of `{error}`; `skipConnectionTest` | high | M | only #6874 and #7014 |
| 7 | [Search engines](#7-search-engines) | none | flat input schema; CRUD returning an id; `/api/settings/search` | medium | S | none |
| 8 | [API keys](#8-api-keys) | none | `GET`/`POST`/`DELETE /api/api-keys` | medium | S | none |
| 9 | [Media and icons](#9-media-and-icons) | none | JSON/base64 upload; media `GET`/`DELETE`; `GET /api/icons` | medium | M | none |
| 10 | [Other server settings](#10-other-server-settings) | `board` key only | `GET`/`PATCH /api/settings/{key}` with strict schemas | medium | M | `/api/settings/board` pattern (#6213) |
| 11 | [App tile options](#11-app-tile-options) | covered by 1 | nothing beyond 1; server-side option validation recommended | medium | S | as 1 |
| 12 | [Clearing an email](#12-clearing-a-users-email) | `PUT /api/users/profile` cannot clear | treat `email: null` as clear | low | S | none |
| 13 | [Certificates](#13-certificates-delete-list-trusted-hostnames) | `GET /{fileName}`, `POST /root` | `DELETE`, a list, trusted-hostname endpoints | medium | S | #7033, create-only by design |
| 14 | [Docker and Kubernetes](#14-docker-and-kubernetes) | none | nothing needed | low | — | none |
| 15 | [Other routers](#15-other-configuration-like-routers) | none | cron jobs, assistant, custom widgets, widget secrets, bootstrap | low–medium | S–L | none |
| 16 | [Board lifecycle details](#16-board-lifecycle-details) | works, with surprises | 409 on create, 403 vs 404, settings cleanup on delete, copy source | medium | S | none |

## 0. How a tRPC procedure becomes REST

The REST handler serves only `openApiRouter`
(`v2.4.1:apps/nextjs/src/app/api/[...trpc]/route.ts:43-51`). That router is an
allow-list (`v2.4.1:packages/api/src/open-api.ts:19-34`): the whole `app`,
`board`, `info`, `invite`, `serverSettings` and `user` routers, and two
procedures each from `certificates` and `integration`. Every item below has to
pass this checklist:

1. **`.meta({ openapi: { method, path, tags, protect: true, summary, description } })`**
   is set on the procedure.
2. **The router or procedure is registered in `open-api.ts`.** Meta alone is not
   enough since the allow-list replaced `generateOpenApiDocument(appRouter)`; the
   stale `origin/add-more-openapi` branch predates that change.
3. **`.output(zod)` is set.** Without it document generation throws "Output
   parser expects a Zod validator" (`t2o:generator/paths.mjs:58-63`).
4. **The input is a top-level `ZodObject`**, or the generator throws "Input
   parser must be a ZodObject" (`t2o:generator/paths.mjs:74-79`). `.and()`
   intersections and discriminated unions are rejected. Homarr's
   `convertIntersectionToZodObject` merges only object∧object and **drops
   refinements** (`v2.4.1:packages/api/src/schema-merger.ts:4-21`).
5. **GET and DELETE inputs travel as path or query parameters**, so every field
   is a string, number, boolean or date, and path parameters are required keys of
   the input object (`t2o:generator/schema.mjs:6-75`).
6. **Request bodies are JSON or x-www-form-urlencoded.** There is no multipart
   (`t2o:adapters/fetch.mjs:14-21`), so `zod-form-data` inputs cannot be exposed.
7. **Responses are plain `JSON.stringify`, not superjson.** A `Date` becomes an
   ISO string, as `GET /api/invites` already shows; `Map`, `Set` and `BigInt`
   would be lost.
8. **Failures throw `TRPCError`**, which maps to an HTTP status. A procedure that
   *returns* `{ error }` gives a REST client HTTP 200.
9. **Housekeeping:** the tag in `open-api.ts:76-87`, a path assertion in
   `v2.4.1:packages/api/src/test/open-api.spec.ts:7-14`, and a regenerated
   `apps/docs/public/api/open-api-schema.json`, which CI checks with
   `scripts/check-openapi-schema.mts`.

tRPC already accepts the `ApiKey` header
(`v2.4.1:apps/nextjs/src/app/api/trpc/[trpc]/route.ts:34-42`), and Homarr's docs
tell REST users to "use tRPC for advanced operations"
(`v2.4.1:apps/docs/docs/management/api/index.mdx:13-16`). REST-only is the
provider's own rule, kept because tRPC payloads are superjson-encoded and change
freely between releases.

## 1. Board content: sections, tiles, options

**Terraform users want** an authoritative `homarr_board_content` resource owning
every section and tile of a board, and/or a `homarr_board_item` resource for one
tile (`board_id`, `kind`, `options`, `title`, `css_classes`, `border_color`,
`integration_ids`, a placement per layout), plus a data source — all with drift
detection and import by board id.

**Homarr has today:**

* `board.saveBoard`, `PUT /api/boards/{id}/content`
  (`v2.4.1:packages/api/src/router/board.ts:1699-1717`). Input `boardSaveSchema`
  = `{id, sections[], items[]}` (`v2.4.1:packages/validation/src/board.ts:140-144`),
  output `z.void()`. A full replace: omitted sections and items are deleted.
  Section and item ids are client-chosen and inserted as sent (`board.ts:1745`,
  `:1781`). Needs modify access, and admin for custom widgets (`:1719-1726`).
* `board.addItem`, `POST /api/boards/items` (`board.ts:2295-2313`, input
  `validation/board.ts:177-187`): returns `{itemId}` and auto-places the item in
  every layout. The provider uses it for the `homarr_board_add_item` action.
* `board.updateItemLayout`,
  `PATCH /api/boards/{boardId}/items/{itemId}/layouts/{layoutId}`
  (`board.ts:2410-2428`): moves or resizes only.
* **No read.** Only `board.getBoardByName` (`board.ts:1208-1222`, MCP meta, no
  `openapi` meta, no `.output()`) returns `getFullBoardWithWhereAsync`
  (`board.ts:2961-3082`): columns, creator, layouts, sections including the
  per-user `collapsed` flag (`:3059`), items `{id, kind, options, advancedOptions,
  layouts[], integrationIds[]}`, and the caller's permission rows. Items go
  through a per-kind union (`board.ts:3097-3103`), sections through a schema with
  a legacy `dynamic`→`container` transform
  (`v2.4.1:packages/validation/src/shared.ts:98-102`), and options are
  superjson-parsed records whose values are JSON-safe (`board.ts:3050`,
  `:3075-3076`). The read path may write: it repairs protected layouts
  (`:3022-3031`).
* Options are **not** validated per widget kind;
  `validateWidgetConfigurationsAsync` checks integrations only
  (`board.ts:156-212`).
* No per-item read, options update or delete, and no section endpoints; the
  section router has only the per-user `changeCollapsed`
  (`v2.4.1:packages/api/src/router/section/section-router.ts:12-72`).

**Upstream must:**

1. Add **`GET /api/boards/{id}/content`**: wrap `getFullBoardWithWhereAsync` with
   `.meta({openapi:{method:"GET", path:"/api/boards/{id}/content", tags:["boards"], protect:true}})`,
   input `z.object({id: z.string()})`, the access check of `getBoardSettings`
   (modify, `board.ts:1243`) or `getBoardByName` (view), and an output
   shape-compatible with the `PUT /content` input, so read→modify→write
   round-trips:
   ```
   { id, name,
     layouts:  [{ id, name, role: "mobile"|"base"|"custom", columnCount, leftGutterColumnCount, rightGutterColumnCount, breakpoint }],
     sections: [ { id, kind: "empty", xOffset, yOffset }
               | { id, kind: "container",
                   options: { title, customCssClasses, borderColor, showLabel, collapsible, showOpenAll, scrollable },
                   layouts: [{ layoutId, parentSectionId, xOffset, yOffset, width, height }] } ],
     items:    [{ id, kind, options: {...}, advancedOptions: { title, customCssClasses, borderColor },
                  integrationIds: [...], layouts: [{ layoutId, sectionId, xOffset, yOffset, width, height }] }] }
   ```
   Export `emptySectionSchema` and `containerSectionSchema` from
   `validation/shared.ts` and build the output union without the legacy
   transform; define items as one flat `ZodObject` (`kind: z.enum(widgetKinds)`,
   `options: z.record(z.string(), z.unknown())`, plus the `sharedItemSchema`
   shape). Leave out the per-user `collapsed` flag and the permission rows.
2. Add per-tile endpoints, for a `homarr_board_item` that does not own the whole
   board: `GET /api/boards/{boardId}/items/{itemId}` (same item shape);
   `PATCH /api/boards/{boardId}/items/{itemId}` with
   `{options?, advancedOptions?, integrationIds?}`, reusing the checks in
   `board.ts:1720-1734`; `DELETE /api/boards/{boardId}/items/{itemId}`; optionally
   an explicit `sectionId` and `layouts` on `POST /api/boards/items`.
3. Optionally, containers: `POST /api/boards/{boardId}/sections`, and
   `PATCH`/`DELETE /api/boards/{boardId}/sections/{sectionId}`.

`boardRouter` is already registered whole (`open-api.ts:21`), so meta and output
are enough. There are no blockers: release-widget tokens live in `widget_secret`,
not in options (item 15), and view-level users can already read options over
tRPC and MCP.

**The provider would add:**

* `homarr_board_content`: `board_id`; computed `layout_ids` (role → id);
  `sections` (`id` optional and computed, `kind`, `title`, `css_classes`,
  `border_color`, `show_label`, `collapsible`, `show_open_all`, `scrollable`,
  `placements`); `items` (`id`, `kind`, `options_json` with semantic JSON
  equality, `title`, `css_classes`, `border_color`, `integration_ids`,
  `placements`), where a placement is keyed by layout role or id and holds
  `{x, y, width, height, section_id | parent_section_id}`. Create and update `PUT`
  the full arrays, keeping the root and gutter sections from the last read; read
  is `GET /content`; delete `PUT`s only the root empty section(s).
* `homarr_board_item`: create with `POST /items`, read with the item `GET`,
  update options with the item `PATCH` and placement with the layout `PATCH`,
  delete with `DELETE`. It does not fight UI edits to other tiles. It would
  replace the `homarr_board_add_item` action.
* A `homarr_board_content` data source.

**Priority and effort:** high — the README's main limitation. M (the read alone
is S).

**Existing upstream work:** `origin/add-more-openapi` has an items router
(`bd7a9fbd1` 2025-09-14, `56aae8885` 2025-12-10) with
`GET /api/boards/{boardId}/items`, `POST /api/items` and `GET`/`PUT`/`DELETE /api/items/{id}`
(`origin/add-more-openapi:packages/api/src/router/item/item-router.ts:66-366`),
using per-kind option schemas from `@homarr/widgets`. Stale: last commit
2025-12-19, 1,727 commits behind `dev`, never merged, predates the allow-list.

## 2. Board layouts and column count

**Terraform users want** `homarr_board.column_count` changeable in place,
`left_gutter_column_count` and `right_gutter_column_count`, computed
`base_layout_id` and `mobile_layout_id`, import by bare id instead of
`<id>,<column_count>`, and optionally `homarr_board_layout` for custom
breakpoints.

**Homarr has today:**

* `createBoard`, `POST /api/boards` (`v2.4.1:packages/api/src/router/board.ts:668-745`),
  creates a 3-column Mobile layout at breakpoint 0 (`:716-724`) and a Base layout
  with the requested `columnCount` at breakpoint 768 (`:725-734`). It returns
  `{boardId, name, layoutId}`, where `layoutId` is the **Base** layout
  (`:687`, `:741`) — the provider's `base_layout_id`. The Mobile id is never
  returned. `duplicateBoard` returns `{boardId}` only (`:765`).
* `saveLayouts`, `PUT /api/boards/{id}/layouts` (`board.ts:1282-1566`), demands
  the **complete** array — exactly one mobile (breakpoint 0) and one base layout,
  unique ids and breakpoints (`v2.4.1:packages/validation/src/board.ts:82-133`).
  New layouts must use the `custom` role (`:1314`); mobile and base cannot be
  removed (`:1317`). The output echoes the input, with server ids for new custom
  layouts (`:1368`, `:1559-1565`).
* `resetLayout`, `POST /api/boards/{boardId}/layouts/{layoutId}/reset`
  (`board.ts:1567-1646`).
* **No read.** `GET /api/boards` (`boardSummarySchema`,
  `validation/board.ts:158-175`) and `GET /api/boards/{id}/settings`
  (`validation/board.ts:77-80`) carry no layouts; only tRPC/MCP `getBoardByName`
  does.

**Upstream must** do any one of these, in order of preference:

1. `GET /api/boards/{id}/layouts` with output
   `z.array(z.object({id, name, role, columnCount, leftGutterColumnCount, rightGutterColumnCount, breakpoint}))`,
   reusing `boardLayoutSchema` without its refines (or as part of item 1's read).
2. `PATCH /api/boards/{boardId}/layouts/{layoutId}` with
   `{name?, columnCount?, leftGutterColumnCount?, rightGutterColumnCount?, breakpoint?}`;
   the server rebuilds the full array and reuses the `saveLayouts` reprojection
   (`board.ts:1300-1556`), so clients never need ids of layouts they do not
   manage.
3. Minimal fix: add `mobileLayoutId` (or all `layouts`) to the outputs of
   `createBoard` and `duplicateBoard`. This helps only boards the provider
   created or copied, not imported ones.
4. Optionally, accept gutter counts in `boardCreateSchema`
   (`validation/board.ts:146-150`).

**The provider would add:** an updatable `column_count` (`GET` layouts, change
the base layout, `PUT` layouts — or the per-layout `PATCH`); optional gutter
counts (0–3, sum below the column count, never on mobile); computed
`mobile_layout_id`; `base_layout_id` after import and on `homarr_board_copy`;
import by bare id; `column_count` in the data source; optionally
`homarr_board_layout`.

**Priority and effort:** high — today a `column_count` change destroys the
board and its content. S.

**Existing upstream work:** none. #6546 and #6213 added only the write.

## 3. Board permissions

**Terraform users want** an authoritative `homarr_board_permissions` resource
(`user_permissions` = set of `{user_id, permission}`, `group_permissions` = set of
`{group_id, permission}`), optionally per-grant resources, and a data source that
includes inherited permissions.

**Homarr has today** (none of these has `openapi` meta or an output schema):

* `board.getBoardPermissions` (`v2.4.1:packages/api/src/router/board.ts:2143-2214`)
  needs **full** access (`:2144`) and returns
  `inherited: [{permission, group:{id,name}}]`,
  `users: [{user:{id,name,image,email}, permission}]` and
  `groups: [{group:{id,name}, permission}]`.
* `saveUserBoardPermissions` (`:2215-2253`) and `saveGroupBoardPermissions`
  (`:2254-2294`) take `{entityId, permissions:[{principalId, permission}]}`
  (`v2.4.1:packages/validation/src/permissions.ts:3-15`,
  `validation/board.ts:152`); each is an authoritative replace in a transaction.
* Rows are keyed `(boardId, principal, permission)`
  (`v2.4.1:packages/db/schema/sqlite.ts:290-324`). `GET /api/boards` shows only
  the caller's own rows (`validation/board.ts:173-174`).

**Upstream must:** add meta and an output schema for
`GET /api/boards/{id}/permissions`; add `PUT /api/boards/{entityId}/permissions/users`
and `PUT /api/boards/{entityId}/permissions/groups` with body
`{permissions:[{principalId, permission}]}` (the path parameter must equal an
input key, so keep `entityId` or rename the field to `id`); optionally per-grant
`PUT`/`DELETE /api/boards/{boardId}/permissions/users/{userId}` and the same for
groups; validate principal ids, which today fail as a foreign-key 500.
`open-api.ts` needs no change.

**The provider would add:** `homarr_board_permissions` — create and update `PUT`
both lists, read is `GET`, delete `PUT`s empty lists, import by board id — and
per-grant resources if those endpoints land.

**Priority and effort:** high. S. **Existing upstream work:** none.

## 4. Groups

**Terraform users want** `homarr_group` (`name`, `permissions`,
`home_board_id`, `mobile_home_board_id`, `position`), `homarr_group_members`
(authoritative) or `homarr_group_member`, `homarr_group_order`, and `homarr_group`
/ `homarr_groups` data sources, for example to look up `everyone`.

**Homarr has today:** `v2.4.1:packages/api/src/router/group.ts`. Every procedure
needs admin; none has `openapi` meta or `.output()`; all outputs are JSON-safe.

| Procedure | Lines | Notes |
| --- | --- | --- |
| `getAll` | 24-46 | |
| `getPaginated` | 48-82 | |
| `getById` | 83-131 | `id, name, ownerId, homeBoardId, mobileHomeBoardId, position`, `members[{id,name,email,image,provider}]`, `permissions[]`, `owner` |
| `selectable` | 133-159 | any user; `id`, `name` |
| `search` | 160-177 | |
| `createGroup` | 224-241 | name 1-64 chars, not `everyone` (`validation/group.ts:8-17`); returns the id string |
| `updateGroup` | 242-257 | name only |
| `savePartialSettings` | 258-271 | `{id, settings:{homeBoardId?, mobileHomeBoardId?}}` (`validation/group.ts:21-29`) |
| `savePositions` | 272-294 | `{positions: ids[]}`, position = index + 1 |
| `savePermissions` | 295-338 | authoritative, stored as sent (`:301-303`); keys in `v2.4.1:packages/definitions/src/permissions.ts:38-47` |
| `transferOwnership` | 339-352 | |
| `deleteGroup` | 353-361 | |
| `addMember` | 362-392 | 403 unless members and the user's provider are managed locally (`:368`, `:381-386`) |
| `removeMember` | 393-404 | |

The reserved `everyone` group cannot be renamed, deleted or have its members
changed (`:420-429`); permissions and home boards are allowed. Group `position`
decides whose home board wins (`board.ts:2537-2552`).

**Upstream must** register `groupRouter` in `open-api.ts` with tag `groups` and
add meta and output to:

* `GET /api/groups` → `[{id, name, position, homeBoardId, mobileHomeBoardId, ownerId, permissions[], memberIds[]}]`
* `GET /api/groups/{id}` → the `getById` shape
* `POST /api/groups` with `{name}` → `{id}` (wrapping the returned string)
* `PATCH /api/groups/{id}` with `{name}`; `DELETE /api/groups/{id}`
* `PUT /api/groups/{groupId}/permissions` with `{permissions[]}` → echo
* `PATCH /api/groups/{id}/settings` with
  `{homeBoardId?: string|null, mobileHomeBoardId?: string|null}` — flatten the
  nested `settings` object and check that the boards exist
* `PUT /api/groups/positions` with the **complete** `{positions[]}` (a partial
  list leaves duplicate positions), or `position` in the group `PATCH`
* `PUT /api/groups/{groupId}/members` with `{userIds[]}`, authoritative and
  refusing externally managed users, plus `POST`/`DELETE /api/groups/{groupId}/members/{userId}`

**The provider would add:** `homarr_group` (create with `POST`, then `PUT`
permissions and `PATCH` settings; read with `GET /api/groups/{id}`; delete; import
by id); attachable `homarr_group_permissions` and `homarr_group_settings` that
also work on `everyone`; `homarr_group_members` for locally managed users (LDAP
and OIDC re-sync memberships at sign-in); `homarr_group_order`; `homarr_group`
and `homarr_groups` data sources.

**Priority and effort:** high. M.

**Existing upstream work:** `origin/add-more-openapi` 7ea71a57b (2025-08-31,
"feat(api): add group openapi endpoints") adds meta and output for
`GET /api/groups`, `/api/groups/paginated`, `GET`/`PUT`/`DELETE /api/groups/{id}`,
`POST /api/groups` and `POST`/`DELETE /api/groups/{groupId}/members/{userId}`. It
misses `savePermissions`, `savePartialSettings` and `savePositions`, is stale and
predates the allow-list, but is a good starting point.

## 5. A user's group membership

**Terraform users want** `homarr_user.group_ids` readable and updatable in
place. Today it is create-only, and adding or removing a group replaces the
user.

**Homarr has today:**

* Membership is written only at creation: `POST /api/users` accepts `groupIds`
  (`v2.4.1:packages/validation/src/user.ts:85-87`) and inserts them unchecked,
  after the user and outside a transaction
  (`v2.4.1:packages/api/src/router/user.ts:198-208`). Its output is `z.void()`
  (`:199`), so the new id is not returned either.
* Nothing returns membership over REST: not `GET /api/users/{userId}`
  (`user.ts:404-482`, output `:406-427`), not `GET /api/users` (`:265-302`).
* `everyone` membership is added at first sign-in
  (`v2.4.1:packages/auth/events.ts:46`, `:94-111`), not at API creation
  (`user.ts:894-905`). LDAP and non-local OIDC memberships are re-synced from the
  identity provider at every login (`events.ts:31-45`, `:113+`;
  `v2.4.1:packages/auth/providers/check-provider.ts:18-29`).

**Upstream must:** add `groupIds: z.array(z.string())` to the
`GET /api/users/{userId}` output; add `PUT /api/users/{userId}/groups` with
`{groupIds}` — admin only, authoritative, 403 when
`!isGroupMembershipManagedLocally(provider)`, `everyone` implicit — or use the
member endpoints of item 4; return `{id}` from `POST /api/users`; validate group
ids before creating the user.

**The provider would add:** `homarr_user.group_ids` optional and computed,
updated in place, read back from `GET` user, ignoring `everyone`; computed-only
for LDAP users and OIDC users whose groups are not managed locally. The id
recovery after create would go.

**Priority and effort:** high. S. **Existing upstream work:** none; PR #7078
exposes groups only to custom widgets.

## 6. Integrations

**Terraform users want** `homarr_integration` (`name`, `url`, `kind` forcing
replacement, sensitive `secrets`, `app_id`), `homarr_integration_permissions`
(users and groups with `use`, `interact` or `full`), and `homarr_integration`,
`homarr_integrations` and `homarr_integration_kinds` data sources.

**Homarr has today:**

* **REST**, two procedures (`open-api.ts:30-33`), both requiring *full* access to
  the integration — `integration-full-all`, which admins have, or a per-integration
  `full` grant — and answering 404 without it
  (`v2.4.1:packages/api/src/router/integration/integration-access.ts`):
  `POST /api/integrations/request`, a proxy (`integration-request.ts:124`),
  and `GET /api/integrations/{id}/test-connection`
  (`integration-test-stored-connection.ts:12-57`), which the provider exposes as
  `homarr_integration_connection`.
* **tRPC only** (`integration-router.ts`, no `openapi` meta):

| Procedure | Lines | Notes |
| --- | --- | --- |
| `getKinds` | 56-71 | public; `{kind, name, category, requiredSecrets: string[][]}`; no output schema |
| `all` | 72-82 | `{id, name, kind, url, permissions}` (`:765-794`) |
| `byId` | 155-224 | full access; `{id, name, kind, url, secrets:[{kind, value, updatedAt}], app}`; `value` only for public kinds such as username (`:219`; `v2.4.1:packages/definitions/src/integration.ts:11-32`) |
| `create` | 225-308 | permission `integration-create` |
| `update` | 309-414 | full access |
| `delete` | 415-438 | |
| `getIntegrationPermissions` | 439-510 | same shape as for boards |
| `saveUserIntegrationPermissions` | 511-556 | authoritative `{entityId, permissions}` (`validation/integration.ts:45`) |
| `saveGroupIntegrationPermissions` | 557-602 | as above |

* `create` (input `validation/integration.ts:9-29`: `name`, http(s)-only `url`,
  `kind`, `secrets[{kind,value}]`, a required but never read
  `attemptSearchEngineCreation`, optional `app`) always tests the connection
  first and on failure **returns** `{error}` with HTTP 200 (`:265-268`); on
  success it returns `{integration:{id,…}, appId}`.
* `update` (input `validation/integration.ts:31-43`) takes a `url` **without**
  the http(s) restriction; in `secrets[{kind, value|null}]`, `null` keeps the
  stored value and an **omitted kind is deleted** (`:389-401`); `appId` is
  nullable and `kind` immutable; it also returns `{error}` on a failed test
  (`:349-352`).

**Upstream must:**

1. Extend the `integration` sub-router in `open-api.ts:30-33` with meta and
   output for `GET /api/integrations/kinds`, `GET /api/integrations`,
   `GET /api/integrations/{id}` (non-public secret values stay `null`;
   `updatedAt` as an ISO string per secret), `POST /api/integrations` →
   `{id, appId}`, `PUT /api/integrations/{id}`, `DELETE /api/integrations/{id}`,
   `GET /api/integrations/{id}/permissions`, and
   `PUT /api/integrations/{entityId}/permissions/users` and `.../groups`.
2. Throw `TRPCError({code:"BAD_REQUEST", message:<category>})` instead of
   returning `{error}`, or declare a discriminated output, so REST callers see a
   non-2xx status.
3. Add `skipConnectionTest` (default `false`) to create and update:
   infrastructure as code provisions integrations before the target service is
   reachable.
4. Make `attemptSearchEngineCreation` optional, and apply the http(s) check to
   `update.url`.

Secret values stay write-only by design; the per-secret `updatedAt` is the only
way to detect drift.

**The provider would add:** `homarr_integration` — `name`, `url`, `kind`
(replace), `app_id`, `skip_connection_test`; a sensitive `secrets` map, or on
Terraform 1.11+ a write-only `secrets_wo` with `secrets_wo_version`; computed
`public_secrets` and `secret_updated_at`; create with `POST`, read with `GET`,
update with `PUT` always sending **every** kind (`null` for unchanged), delete;
plus `homarr_integration_permissions` and the three data sources. The
`integration_ids` of `homarr_board_add_item` could then be looked up instead of
copied from the UI's address bar.

**Priority and effort:** high. M.

**Existing upstream work:** only the two REST helpers, #6874 (request proxy,
2026-09-21) and #7014 (stored connection test, 2026-10-03). No branch exposes
CRUD.

## 7. Search engines

**Terraform users want** `homarr_search_engine` (`name`, `short`, `icon_url`,
`description`, `type` = `generic` or `fromIntegration`, `url_template`,
`integration_id`), the instance-wide default engine, and a
`homarr_search_engines` data source.

**Homarr has today:** `v2.4.1:packages/api/src/router/search-engine/search-engine-router.ts`,
no `openapi` meta: `getPaginated` (17-31), `getSelectable` (32-45), `byId`
(47-72, any user, discriminated by `type`), `getDefaultSearchEngine` (73-132,
silently clears a stale default by writing settings, `:119-129`), `catalog`
(133-154), `search` (155-175), `create` (176-190, `search-engine-create`, returns
**void**, lowercases `short` at `:183`), `update` (191-217,
`search-engine-modify-all`, `short` immutable per
`validation/search-engine.ts:32-38`), `delete` (218-229,
`search-engine-full-all`).

* The inputs are `discriminatedUnion(...).and(object)`
  (`v2.4.1:packages/validation/src/search-engine.ts:23-38`): not a `ZodObject`,
  and `convertIntersectionToZodObject` cannot merge it.
* For `fromIntegration`, `integrationId` is optional (`:11-14`), yet `byId`
  asserts it is non-null (`router:59-65`).
* The instance default is server-settings key `search`
  (`v2.4.1:packages/server-settings/src/index.ts:155-157`), written through the
  unvalidated `saveSettings` (item 10). The per-user default is already on REST
  (`user.ts:710-734` and the preferences endpoints) and managed by the user
  resources.

**Upstream must:** flatten the input to
`z.object({name, short, iconUrl, description: nullable, type: z.enum(searchEngineTypes), urlTemplate?: nullable, integrationId?: nullable}).superRefine(...)`,
requiring `integrationId` for `fromIntegration`; register `searchEngineRouter`
with tag `search-engines`; add `GET /api/search-engines` →
`[{id, name, short, iconUrl, description, type, urlTemplate, integrationId}]`,
`GET /api/search-engines/{id}`, `POST /api/search-engines` → `{id}`,
`PATCH /api/search-engines/{id}` (renaming `short` with a uniqueness check, or
keeping it immutable), `DELETE /api/search-engines/{id}`, and
`GET`/`PATCH /api/settings/search` with `{defaultSearchEngineId: string|null}`,
checked for existence.

**The provider would add:** `homarr_search_engine` (`short` forces replacement
unless renaming is allowed, normalised to lowercase; read with `GET {id}`), a
`default_search_engine_id` on a server-settings resource (item 10), and a
`homarr_search_engines` data source.

**Priority and effort:** medium. S. **Existing upstream work:** none.

## 8. API keys

**Terraform users want** `homarr_api_key` (computed `id`, sensitive `key` as
`<id>.<token>`, owner `user_id`) to issue and rotate keys for other tools, and a
`homarr_api_keys` data source.

**Homarr has today:** `v2.4.1:packages/api/src/router/apiKeys.ts`, admin only,
no `openapi` meta or output schema: `getAll` (13-35, `id` and
`user{id,name,image,email}`, never the hash); `create` (36-56) takes **no
input** — the key always belongs to the caller, with no name and no expiry — and
returns `{apiKey}` once; `delete` (57-68, `{apiKeyId}`). The table holds only
`id`, `apiKey` (hash) and `userId` (`db/schema/sqlite.ts:38-45`). On `dev`, MCP
OAuth keys share the table (`dev:packages/db/schema/sqlite.ts:41`), `getAll`
returns the OAuth columns, and `getOwnOAuth`/`revokeOwnOAuth` are added
(`dev:packages/api/src/router/apiKeys.ts:12-48`).

**Upstream must** register `apiKeysRouter` with tag `api-keys` and add
`GET /api/api-keys` → `[{id, userId, user{id,name}, oauthClientId|null}]` (never
the hash), `GET /api/api-keys/{id}`, `POST /api/api-keys` with
`{userId?, name?, expiresAt?}` → `{id, apiKey}` (a `userId` other than the
caller's needs admin; `name` and `expiresAt` need a migration), and
`DELETE /api/api-keys/{id}`. Returning the token once is acceptable, as for
invites.

**The provider would add:** `homarr_api_key` — create keeps the key in state,
read with `GET` (the key stays from state and is `null` after import), every
attribute forces replacement, delete — and a `homarr_api_keys` data source that
separates manual keys from OAuth keys.

**Priority and effort:** medium. S, or M with name and expiry. **Existing
upstream work:** none.

## 9. Media and icons

**Terraform users want** `homarr_media` to upload a local file and get
`/api/user-medias/{id}` back for app and board icons, and a `homarr_icon` data
source to find an icon URL by name for `homarr_app.icon_url`.

**Homarr has today:** `v2.4.1:packages/api/src/router/medias/media-router.ts`,
no `openapi` meta: `getPaginated` (15-54, input an intersection
`paginatedSchema.and(...)`); `uploadMedia` (55-100, `media-upload`) takes
**multipart** `zod-form-data` (`validation/media.ts:8-39`: png, jpeg, webp, gif
or svg up to 32 MiB), returns `ids[]`, and registers each upload as a local icon
with an md5 checksum (`:78-97`;
`packages/icons/src/repositories/local.icon-repository.ts:26-34`); `deleteMedia`
(101-127, the creator or `media-full-all`). No read of one media's metadata.
`icons.findIcons` (`v2.4.1:packages/api/src/router/icons.ts:8-58`) is public with
MCP meta only.

**Upstream must:** add `POST /api/medias` with JSON
`{name, contentType, contentBase64}` → `{id, url, md5}` (trpc-to-openapi cannot
take multipart); `GET /api/medias/{id}` →
`{id, name, contentType, size, createdAt, creatorId, md5}`; `GET /api/medias`
with flat query fields `search` and `includeFromAllUsers`;
`DELETE /api/medias/{id}`; `GET /api/icons` with `searchText` and
`limitPerGroup` and an output schema; and register `mediaRouter` and
`iconsRouter`.

**The provider would add:** `homarr_media` (`source` or `content_base64`;
`content_md5` forces replacement; computed `url`; read with `GET {id}`, delete
with `DELETE`) and a `homarr_icon` data source (name → url, preferring SVG).

**Priority and effort:** medium for the icon lookup, low for uploads. M.
**Existing upstream work:** none; PR #7109 is SQL performance only.

## 10. Other server settings

**Terraform users want** a `homarr_server_settings` singleton with `analytics`,
`crawling_and_indexing`, `user`, `appearance`, `branding`, `culture` and
`search` blocks, or one resource per key.

**Homarr has today:** keys and defaults in
`v2.4.1:packages/server-settings/src/index.ts:5-14`, `:127-158`.

| Key | Fields | Written today |
| --- | --- | --- |
| `analytics` | `enableGeneral`; `instanceId` and `lastSuccessfulSnapshotAt` are server-managed | strict schema (`serverSettings.ts:31`, `:158-161`) |
| `crawlingAndIndexing` | `noIndex`, `noFollow`, `noTranslate`, `noSiteLinksSearchBox` | **unvalidated** merge (`serverSettings.ts:163-167`) |
| `board` | `homeBoardId`, `mobileHomeBoardId`, `enableStatusByDefault`, `forceDisableStatus` | **already REST**, `GET`/`PATCH /api/settings/board` (`:55-131`), managed by `homarr_server_board_settings` |
| `user` | `enableGravatar` | unvalidated |
| `appearance` | `defaultColorScheme` | unvalidated |
| `branding` | `appName`, `greeting`, `logoImageUrl`, `faviconImageUrl`, `primaryColor`, `secondaryColor`, `lockPrimaryColor`, `signInBackgroundImageUrl`, `signInBackgroundOverlay`, `authBranding{…}`, `defaultRadius`, `customCss` | validated (`index.ts:42-55`; `serverSettings.ts:141-157`) |
| `culture` | `defaultLocale` | unvalidated |
| `search` | `defaultSearchEngineId` | unvalidated |

Over tRPC: `serverSettings.getAll` (`v2.4.1:packages/api/src/router/serverSettings.ts:44-46`,
admin), `saveSettings` (`:132-168`, admin, `{settingsKey, value: record}`),
`getCulture` (`:41-43`, public) and `getBranding` (`:47-54`, public, MCP meta).

On `dev`, `PATCH /api/settings/board` validates the merged result rather than
the input (`saveBoardSettingsAsync`, #7146), so a stored private or deleted home
board blocks every later change; see item 16.

**Upstream must** copy the `/api/settings/board` pattern for each key, with a
partial input and a full output: `GET`/`PATCH /api/settings/analytics` (only
`enableGeneral` writable), `.../crawling-and-indexing`, `.../user`,
`.../appearance`, `.../branding`, `.../culture` (`defaultLocale` as an enum of
supported languages) and `.../search` (engine checked for existence). Fix
`saveSettings`, which persists any key with any type. `serverSettingsRouter` is
already registered (`open-api.ts:24`), so meta and output are enough.

**The provider would add:** `homarr_server_settings` with optional nested blocks,
`null` meaning unmanaged as in `homarr_server_board_settings`, each key read with
its `GET`; delete forgets the resource or optionally resets defaults. Or one
resource per key (`homarr_server_branding`, …), plus matching data sources.

**Priority and effort:** medium (branding is the most requested). M.
**Existing upstream work:** `origin/feat/trpc-board-openapi`, merged as #6213,
set up the board-settings pattern only.

## 11. App tile options

**Terraform users want**, per tile of kind `app`: `app_id`, `open_in_new_tab`,
`show_title`, `description_display_mode`, `layout`, `ping_enabled` and the
placement.

**Homarr has today:** these are options of the `app` widget item
(`v2.4.1:packages/widgets/src/app/index.ts:16-97`), not fields of the app, whose
table holds only `id, name, description, iconUrl, href, pingUrl`
(`db/schema/sqlite.ts:428-435`): `appId`; `openInNewTab` (default `true`);
`showTitle` (default `true`); `descriptionDisplayMode` (`normal`, `tooltip` or
`hidden`, default `hidden`); `layout` (`column`, `column-reverse`, `row` or
`row-reverse`, default `column`); `pingEnabled` (default: the server's
`enableStatusByDefault`, hidden when `forceDisableStatus` is set). They are
written through `POST /api/boards/items` or `PUT /content` and never read. The
server does not validate them and applies defaults only in the UI
(`board.ts:156-212`), so stored options can be partial.

**Upstream must** do nothing beyond item 1. Recommended: validate options
server-side against the widget definition and return them with defaults filled
in, so REST round-trips are stable;
`origin/add-more-openapi:packages/api/src/router/item/item-router.ts:21-55` did
this with `reduceWidgetOptionsWithDefaultValues`, which needs the option schemas
usable outside the React package.

**The provider would add** `homarr_app_tile`, a typed wrapper over
`homarr_board_item` with `kind = "app"` and `ping_enabled` optional and
computed.

**Priority and effort:** medium. S on top of item 1. **Existing upstream
work:** as item 1.

## 12. Clearing a user's email

**Terraform users want** `homarr_user.email = null` to clear the email in place.
Setting and changing it in place already works in 0.3.0.

**Homarr has today:** `editProfile`, `PUT /api/users/profile`
(`v2.4.1:packages/api/src/router/user.ts:483-543`), rejects non-credentials users
(`:518-523`) and computes `emailDirty = input.email && user.email !== input.email`
(`:534-540`), so `null` — and `""`, which `nullableEmailSchema` turns into `null`
(`validation/email.ts:7-11`) — never clears the email. `changePassword`
(`:577-658`) also rejects non-credentials users (`:624`); `setProfileImage`
(`:210-264`) does not. The identity provider overwrites some fields at sign-in:
OIDC the name and picture (`auth/events.ts:59-80`), LDAP the name (`:47-50`).

**Upstream must** treat `undefined` as keep and `null` as clear in
`editProfile`:

```ts
const emailChanged = input.email !== undefined && input.email !== user.email;
// ...
email: emailChanged ? input.email : undefined,
emailVerified: emailChanged ? null : undefined,
```

External accounts need nothing: their name, email and password belong to the
identity provider by design. Optionally add `provider` to the `GET /api/users`
output (`user.ts:268-279`), which would save the provider one
`GET /api/users/selectable` call per auth provider in `homarr_users`.

**The provider would add:** removing `email` would update in place instead of
replacing the user, gated on the Homarr version.

**Priority and effort:** low. S. **Existing upstream work:** none.

## 13. Certificates: delete, list, trusted hostnames

**Terraform users want** a `homarr_root_certificate` resource with a real
delete, a `homarr_root_certificates` list data source, and
`homarr_trusted_hostname` (hostname + PEM → thumbprint).

**Homarr has today:** `v2.4.1:packages/api/src/router/certificates/certificate-router.ts`,
admin only.

* **REST:** `GET /api/certificates/{fileName}` (`:38-53`) returns metadata or
  `null` — the provider's `homarr_root_certificate` data source;
  `POST /api/certificates/root` (`:55-80`) is an idempotent ensure that answers
  CONFLICT when different bytes exist.
* **tRPC only:** `addCertificate` (`:85-116`, multipart, any certificate);
  `trustHostnameMismatch` (`:117-150`, `{hostname, certificate}`, stores the
  `fingerprint256` thumbprint); `removeTrustedHostname` (`:151-173`,
  `{hostname, thumbprint}`); `removeCertificate` (`:174-195`, `{fileName}`, also
  deletes the certificate's hostnames).
* **No list procedure at all.** The UI pages call core helpers directly
  (`apps/nextjs/src/app/[locale]/manage/tools/certificates/page.tsx:33`,
  `.../certificates/hostnames/page.tsx:35`;
  `packages/core/src/infrastructure/certificates/files/index.ts:12`, `:39`;
  `packages/core/src/infrastructure/certificates/hostnames/index.ts:8`).
  Hostnames are stored in `trusted_certificate_hostname`, keyed
  `(hostname, thumbprint)`.

**Upstream must** extend `open-api.ts:26-29` with
`DELETE /api/certificates/{fileName}` → `{deleted}`, `GET /api/certificates` →
`rootCertificateMetadataSchema[]` (`validation/certificates.ts:12-19`),
`GET /api/certificates/hostnames` → `[{hostname, thumbprint}]`,
`POST /api/certificates/hostnames` with `{hostname, certificate}` →
`{hostname, thumbprint}` (idempotent), and `DELETE /api/certificates/hostnames`
with `{hostname, thumbprint}` in the query, because the thumbprint contains
colons. Policy blocker: #7033 made the root-certificate API create-only on
purpose.

**The provider would add:** a `homarr_root_certificate` resource (`POST`, `GET`,
real `DELETE`; the content forces replacement), `homarr_trusted_hostname`
(create `POST`, read from the list, delete `DELETE`), and list data sources for
both.

**Priority and effort:** medium. S. **Existing upstream work:** #7033
(a17845aa1, 2026-10-05), create-only.

## 14. Docker and Kubernetes

**Terraform users want** nothing declarative here.

**Homarr has today:** `v2.4.1:packages/api/src/router/docker/docker-router.ts`,
admin only, MCP meta only: `reconcileServices` 32-42, `getServiceHealth` 43-53,
`refreshInventory` 54-67, `getContainers` 68-90, `getEndpoints` 91-100,
`startAll`/`stopAll`/`restartAll`/`removeAll` 101-153, `logs` and
`subscribeLogs` 154-240. Docker endpoints come from the `DOCKER_ENDPOINTS`
environment variable (`v2.4.1:packages/docker/src/endpoint-descriptor.ts:35-61`),
not from the database. The Kubernetes router
(`kubernetes/router/kubernetes-router.ts:5-16`) offers read-only cluster views.

**Upstream must** do nothing. An optional read-only `GET /api/docker/endpoints`
would allow a data source; container actions are imperative.

**The provider would add** nothing; configure this in the deployment, for
example with the docker or helm provider.

## 15. Other configuration-like routers

All low or medium priority, with no existing upstream work.

* **Cron jobs** (medium, S). `v2.4.1:packages/api/src/router/cron-jobs.ts:18-61`,
  admin, no `openapi` meta: `triggerJob`, `startJob`, `stopJob`,
  `updateJobInterval` (`{name, cron: z.string()}`), `disableJob`, `enableJob`,
  `getJobs` (`name, cron, preventManualExecution, preventCustomInterval,
  isEnabled`, `v2.4.1:packages/cron-jobs/src/job-manager.ts:114-137`), persisted
  in `cron_job_configuration` (`db/schema/sqlite.ts:686-690`). Upstream:
  `GET /api/cron-jobs`, `GET /api/cron-jobs/{name}`,
  `PATCH /api/cron-jobs/{name}` with `{cron?, isEnabled?}` — validating first and
  honouring `preventCustomInterval` — and optionally
  `DELETE /api/cron-jobs/{name}` to reset to the default. Provider:
  `homarr_cron_job` with `cron` optional and computed; delete resets the job.
* **Assistant (AI provider) configuration** (low, S).
  `v2.4.1:packages/api/src/router/assistant.ts:631-777`:
  `getAdminConfiguration` reports secrets only as `*Configured` booleans;
  `updateConnection` takes `{provider, baseUrl, modelDiscoveryPath, apiKey?,
  clearApiKey, customHeaders?, clearCustomHeaders}`; `clearCredentials`;
  `updateConfiguration` takes `{enabled, modelId, webSearchEnabled}`. Upstream:
  `GET`/`PUT /api/settings/assistant` with a write-only `apiKey` and
  `customHeaders`. Provider: a `homarr_assistant_settings` singleton with a
  write-only `api_key`.
* **Custom widgets** (low, L). Admin only: `custom-widget-router.ts:37-129`
  (`update` 42-69, `toggleEnabled` 74-95, `delete` 97-104),
  `management-queries.ts:31-123` (`list`, `get`), `creation-procedures.ts:33-52`
  (`create`), `transfer-procedures.ts:21-69` (`export`, `import`),
  `secret-procedures.ts:60-227` (`secretSet`, `secretClear`); tables
  `db/schema/sqlite.ts:625-665`. Upstream: REST CRUD using the export JSON as the
  resource format, plus write-only secrets. Provider: `homarr_custom_widget`
  (`definition_json`, `enabled`, write-only `secrets`).
* **Releases-widget tokens** (low, S after item 1).
  `v2.4.1:packages/api/src/router/widgets/widget-secrets.ts:21-97`: `setSecret`,
  `deleteSecret`, `getConfiguredKinds` for items of kind `releases`; stored in
  `widget_secret` (`db/schema/sqlite.ts:667-684`). Upstream:
  `PUT`/`DELETE /api/boards/items/{itemId}/secrets/{kind}` and
  `GET .../secrets` returning the configured kinds. Provider: write-only
  `secrets` on `homarr_board_item`.
* **Onboarding and bootstrap** (medium, M). `onboard-router.ts:140-…`,
  `user.initUser` (`user.ts:70`) and `group.createInitialExternalGroup`
  (`group.ts:178-223`) form a multi-step flow gated by an onboarding claim cookie
  (`trpc.ts:135-146`), so the first admin and the first API key cannot be created
  over REST; the provider's `make homarr-up` drives tRPC. Upstream: an env- or
  file-based bootstrap that seeds the admin and an API-key hash on first start —
  unauthenticated REST endpoints would be a security risk. Provider: start from a
  fresh instance with a known key.
* **Users, small items.** `POST /api/users` returns void (`user.ts:199`);
  returning `{id}` would let the provider drop its id recovery. Disabling a user
  without deleting it exists only in `origin/bot/issue-4455-disable-users`
  (4ff2c9a7f, 2026-07-06, unmerged); merged, it would become a `disabled`
  attribute on `homarr_user`.

## 16. Board lifecycle details

**Terraform users want** board create, copy, rename, visibility and delete to
fail cleanly and leave no dangling references. The provider works around each
gap below today, at the cost of extra requests and some guesswork.

**Homarr has today:**

* **Create checks no name.** `createBoard`
  (`v2.4.1:packages/api/src/router/board.ts:668-745`) never calls
  `noBoardWithSimilarNameAsync` (`board.ts:2569-2586`), which `exists` (`:288`),
  `duplicateBoard` (`:768`) and `renameBoard` (`:1074`) do. The database
  constraint is case-sensitive (`v2.4.1:packages/db/schema/sqlite.ts:265`), so an
  exact duplicate fails with a driver error that reaches REST as HTTP 500, and a
  case variant is created although the board's URL then reaches only one of the
  two.
* **"No full access" looks like "missing".** Rename, visibility and delete need
  full access (`board.ts:1072`, `:1098`, `:1135`), and
  `v2.4.1:packages/api/src/router/board/board-access.ts:67-75` answers NOT_FOUND
  to prevent snooping — even to a caller who can list and view the board.
* **Delete leaves the instance home boards dangling.** `deleteBoard`
  (`board.ts:1116-1138`) deletes only the board row; the server setting `board`
  keeps `homeBoardId` and `mobileHomeBoardId`. Onboarding also writes its private
  board into both slots (`v2.4.1:packages/api/src/router/onboard/onboard-router.ts:371-376`,
  `:1003-1006`). On `dev`, `PATCH /api/settings/board` validates the merged
  result (#7146), so either case blocks every later settings change, including
  saves from the UI.
* **A copy forgets its source.** `duplicateBoard` returns `{boardId}` only
  (`board.ts:765`) and stores no link to the source board.

**Upstream must:** call `noBoardWithSimilarNameAsync` in `createBoard`, answering
409 CONFLICT like rename and duplicate; answer 403 FORBIDDEN when the caller can
view a board but lacks the access level an operation needs, keeping 404 for
boards it cannot see; clear `homeBoardId` and `mobileHomeBoardId` in the server
settings when `deleteBoard` removes the board they point at, and let onboarding
pick a public board or none; return the layout ids from `duplicateBoard` (see
item 2) and store a `sourceBoardId`, returned by `GET /api/boards`.

**The provider would drop** its pre-create name listing, the HTTP 500 mapping,
the re-list after every 404 on rename, visibility and delete, and the destroy
warning about dangling home boards; and **add** `base_layout_id` on
`homarr_board_copy`, plus drift detection and import for `source_board_id`.

**Priority and effort:** medium. S. **Existing upstream work:** none; #7146 made
the dangling home boards more pressing.

## Upstream bugs found along the way

Worth filing independently of the provider:

1. **Clearing an email is impossible.** `editProfile` ignores `null` and `""`
   (`v2.4.1:packages/api/src/router/user.ts:534-540`).
2. **Integration update weakens URL validation.** It accepts non-http(s) URLs
   (`v2.4.1:packages/validation/src/integration.ts:35`), while create forbids them
   (`:11-14`).
3. **Integration create and update report failure as success.** They return
   `{error}` with HTTP 200 (`integration-router.ts:265-268`, `:349-352`).
4. **Dead input.** `attemptSearchEngineCreation` is required but never read
   (`validation/integration.ts:22`); `integration-router.spec.ts:507-543` asserts
   that no engine is created.
5. **Unvalidated settings writes.** `serverSettings.saveSettings` persists
   arbitrary keys and types for `crawlingAndIndexing`, `user`, `appearance`,
   `culture` and `search` (`serverSettings.ts:163-167`).
6. **Invalid cron expressions are stored.** The expression is persisted before
   it is validated, and the job is left stopped
   (`packages/cron-jobs/src/job-manager.ts:34-48`).
7. **Group home-board fallback is dead code.** `getHomeIdBoardAsync` requires
   both `groups[settingKey]` and `groups.homeBoardId` to be non-null
   (`board.ts:2547-2548`), so a group with only a mobile or only a desktop home
   board is skipped and the fallbacks at `:2555-2556` never run.
8. **Partial position lists.** `group.savePositions` accepts a partial list and
   leaves duplicate positions (`group.ts:272-294`).
9. **Search engine without integration.** A `fromIntegration` engine may be
   saved without `integrationId` (`validation/search-engine.ts:11-14`), yet `byId`
   asserts it exists (`search-engine-router.ts:59-65`).
10. **Password confirmation is not checked over REST.**
    `convertIntersectionToZodObject` drops refinements (`schema-merger.ts:4-21`),
    so `POST /api/users` never compares `confirmPassword` with `password`
    (`validation/user.ts:64-76`, `:85-87`).
11. **SQLite-only log field.** `removeTrustedHostname` logs `dbResult.changes`
    (`certificate-router.ts:159-171`), which is undefined on PostgreSQL.
12. **REST board create returns 500 for a taken name and accepts case
    variants** (item 16; `board.ts:668-745`, `db/schema/sqlite.ts:265`).
13. **User create is not atomic.** Group memberships are inserted after the user
    and outside a transaction, so a duplicate or unknown group id fails the
    request with a 500 while the user stays created (`user.ts:198-208`).
14. **Deleting a board leaves the instance home boards dangling**, which `dev`
    turns into a permanent refusal of later settings changes (item 16;
    `board.ts:1116-1138`).
