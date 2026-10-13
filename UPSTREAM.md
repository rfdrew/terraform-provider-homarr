# What Homarr's REST API is missing

This provider uses only Homarr's REST/OpenAPI surface (see "Why the surface is
small" in the [README](README.md)): a managed resource needs a create, a REST
read-back and an update or delete. This file lists, item by item, what Terraform
users would want to manage, what Homarr offers today, what upstream would have to
change, and what the provider would then add.

* **Provider:** 0.3.1.
* **Homarr:** 2.5.2, tag `v2.5.2` = `7eb9a417a` (2026-10-13). Rechecked against
  `origin/dev` @ `2655561ec` (2026-10-13); the REST surface is identical.
* **References:** `v2.5.2:<path>:<line>` is the Homarr tree at the tag,
  `origin/<branch>:` is an unmerged upstream branch, and `t2o:` is
  `trpc-to-openapi@3.3.0/dist/esm`, the version Homarr pins and patches
  (`v2.5.2:patches/trpc-to-openapi.patch`).
  [#7174](https://github.com/homarr-labs/homarr/pull/7174) is cited by procedure
  and endpoint names, never by line, because it is open and its lines will move.
  Within an item, a bare file name such as `board.ts:667` refers to the file
  cited in full earlier in that item; router files live under
  `packages/api/src/router/`.

Upstream discussion of the largest gap, board content, is in
[homarr-labs/homarr#6435](https://github.com/homarr-labs/homarr/issues/6435).

## Summary

| # | Capability | REST today | Main upstream change | Priority | Effort | Existing upstream work |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | [Board content](#1-board-content-sections-tiles-options) | write-only `PUT /content` (it updates only placements that already exist), `POST /items`, item-layout `PATCH` | `GET /api/boards/{id}/content`; per-item `GET`/`PATCH`/`DELETE` | high | M | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open); `origin/add-more-openapi` items router (stale); #6435 |
| 2 | [Layouts, column count](#2-board-layouts-and-column-count) | write-only `PUT /layouts` (rewrites every row), needs the mobile id | `GET /api/boards/{id}/layouts`, or `mobileLayoutId` in create output | high | S | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open): layouts `GET`, per-layout `PATCH`, `mobileLayoutId` and gutters on create |
| 3 | [Board permissions](#3-board-permissions) | none | `GET`/`PUT /api/boards/{id}/permissions[/users\|/groups]`; validate principals | high | S | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open) |
| 4 | [Groups](#4-groups) | none | register `groupRouter`; meta and output on 10 procedures plus an authoritative members `PUT` | high | M | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open): all 11 endpoints; `origin/add-more-openapi` 7ea71a57b (stale) |
| 5 | [A user's group membership](#5-a-users-group-membership) | create-only `groupIds`, no id returned | `groupIds` in `GET /api/users/{id}`; `PUT /api/users/{id}/groups`; `{id}` from create | high | S | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open); #7078 widgets only |
| 6 | [Integrations](#6-integrations) | request proxy and connection test only | CRUD, kinds, permissions; throw instead of `{error}`; `skipConnectionTest`; 4xx before writing | high | M | #6874, #7014 (released); [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open) |
| 7 | [Search engines](#7-search-engines) | none | flat input schema; CRUD returning an id (409 on duplicate `short`); `/api/settings/search` | medium | S | none |
| 8 | [API keys](#8-api-keys) | none | `GET`/`POST`/`DELETE /api/api-keys` (+ `GET {id}`), with `oauthClientId` to tell OAuth keys apart | medium | S (M with name and expiry) | none (#7146 only added MCP OAuth keys to the same table) |
| 9 | [Media and icons](#9-media-and-icons) | none | JSON/base64 upload; media `GET`/`DELETE`; `GET /api/icons` | medium | M | none |
| 10 | [Other server settings](#10-other-server-settings) | `board` key only | `GET`/`PATCH /api/settings/{key}` with strict schemas | medium | M | `/api/settings/board` pattern (#6213); [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open), `board` key only: changed home-board ids checked, slots cleared on board delete; app tiles stop following `enableStatusByDefault` (item 11) |
| 11 | [App tile options](#11-app-tile-options) | covered by 1; options are stored unvalidated and often partial | nothing beyond 1; server-side option validation recommended | medium | S | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open), `app` only |
| 12 | [Clearing an email](#12-clearing-a-users-email) | `PUT /api/users/profile` cannot clear | treat `email: null` as clear | low | S | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open) |
| 13 | [Certificates](#13-certificates-delete-list-trusted-hostnames) | `GET /{fileName}`, `POST /root` | `DELETE`, a list, trusted-hostname endpoints | medium | S | #7033, create-only by design |
| 14 | [Docker and Kubernetes](#14-docker-and-kubernetes) | none | nothing needed | low | — | none |
| 15 | [Other routers](#15-other-configuration-like-routers) | none | cron jobs, assistant, custom widgets, widget secrets, bootstrap | low–medium | S–L | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open) for `POST /api/users` → `{id}` only; unmerged disable-users branch |
| 16 | [Board lifecycle details](#16-board-lifecycle-details) | works, with surprises; since 2.5.0 a deleted home board blocks settings saves | 409 on create, 403 vs 404, settings cleanup on delete, copy source | medium | S | [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open) proposes all four |

## Open upstream PR #7174

[#7174](https://github.com/homarr-labs/homarr/pull/7174), "feat(api): extend the
REST API for boards, users, groups and integrations", is open against `dev`
(head `936634deb`, base `2655561ec`); it is not merged and in no release. One PR
covers several items, so everything it adds is described below as proposed, not
as what Homarr has today, and every provider feature that relies on it is gated
on the Homarr release that ships it; older releases keep today's workarounds.

| Item | What #7174 delivers | Still missing |
| --- | --- | --- |
| [1](#1-board-content-sections-tiles-options) | `GET /api/boards/{id}/content` in the `PUT /content` shape; `GET`/`PATCH`/`DELETE /api/boards/{boardId}/items/{itemId}` | section endpoints; placement on `POST /items`; a `PUT` that inserts and deletes placements of existing entries |
| [2](#2-board-layouts-and-column-count) | `GET /api/boards/{id}/layouts`; `PATCH /api/boards/{boardId}/layouts/{layoutId}`; only changed layout rows written; `mobileLayoutId` and gutters on create | adding or removing one custom layout still needs the full-array `PUT` |
| [3](#3-board-permissions) | meta and output on `GET /api/boards/{id}/permissions` and both `PUT`s; 400 for a repeated principal, 404 for an unknown one | optional per-grant endpoints; the existence check is not locked |
| [4](#4-groups) | `groupRouter` registered: 11 admin-only endpoints, including an authoritative members `PUT`; complete positions list | per-group position; `transferOwnership`; the `LIKE` name check; bug 7 |
| [5](#5-a-users-group-membership) | `groupIds` in `GET /api/users/{userId}`; `PUT /api/users/{userId}/groups`; `{id}` and atomic memberships on `POST /api/users` | a check-then-write race on group ids |
| [6](#6-integrations) | the whole `integrationRouter`: kinds, list, get, create, update, delete, permissions; `BAD_REQUEST` with the failure category; `skipConnectionTest`; 4xx before writing | non-transactional create and update; optional secret kinds not listed by kinds |
| [11](#11-app-tile-options) | `app` options validated and completed with defaults on `POST /items` and `saveItemOptions`, and for new or changed items on `PUT /content` and the item `PATCH` | other widget kinds; tiles from onboarding, the seed, board duplication or unchanged older tiles stay partial |
| [12](#12-clearing-a-users-email) | `email: null` or `""` clears; `provider` in `GET /api/users` | nothing but a release |
| [16](#16-board-lifecycle-details) | 409 on create; 403 for a visible board; home-board slots cleared on delete, only changed ids validated; `sourceBoardId` and layout ids from duplicate | the name check is still check-then-insert; onboarding still sets the `everyone` group's home board; `sourceBoardId` is `null` when the source is hidden |

Of the [bugs](#upstream-bugs-found-along-the-way) below, it proposes fixes for
1, 2, 3, 4, 8, 12, 13, 14, 16 and 19. Item 10 has no row, but #7174 changes its
`board` key: only changed home-board ids are checked, a deleted board's slots
are cleared (item 16), and the app tiles it completes store `pingEnabled`
instead of following `enableStatusByDefault` (item 11). The `{id}` from
`POST /api/users` (item 5) also serves item 15. Items 7, 8, 9, 13 and 14 are
untouched.

## 0. How a tRPC procedure becomes REST

The REST handler serves only `openApiRouter`
(`v2.5.2:apps/nextjs/src/app/api/[...trpc]/route.ts:43-51`) and authenticates
only with the `ApiKey` header, never with a session cookie (`route.ts:16`,
`:28-36`). That router is an allow-list
(`v2.5.2:packages/api/src/open-api.ts:19-34`): the whole `app`, `board`, `info`,
`invite`, `serverSettings` and `user` routers, and two procedures each from
`certificates` and `integration`.

Every item below has to pass this checklist:

1. **`.meta({ openapi: { method, path, tags, protect: true, summary, description } })`**
   is set on the procedure; inside a registered router, procedures without it
   are skipped (`t2o:utils/procedure.mjs:34-50`). `protect` only shapes the
   document (`t2o:generator/paths.mjs:89-90`); access is enforced by the
   procedure builder (`publicProcedure`, `protectedProcedure`,
   `permissionRequiredProcedure`, `v2.5.2:packages/api/src/trpc.ts:135-196`).
   Optional authentication cannot be expressed, so `open-api.ts:51-56` rewrites
   `security` by hand for `GET /api/boards` and `GET /api/apps/{id}`.
2. **The router or procedure is registered in `open-api.ts`.** Meta alone is not
   enough since the allow-list replaced `generateOpenApiDocument(appRouter)`
   (#5579); the stale `origin/add-more-openapi` branch predates that change. The
   handler takes the first registered path pattern that matches, ignoring case
   (`t2o:adapters/node-http/procedures.mjs:20-31`, `t2o:utils/path.mjs:7-10`), so
   a static path must be declared before a parameterised sibling
   ([#7174](https://github.com/homarr-labs/homarr/pull/7174) declares
   `GET /api/integrations/kinds` before `GET /api/integrations/{id}`).
3. **`.output(zod)` is set.** Without it document generation throws "Output
   parser expects a Zod validator" (`t2o:generator/paths.mjs:58-63`).
4. **The input is a top-level `ZodObject`**, or the generator throws "Input
   parser must be a ZodObject" (`t2o:generator/paths.mjs:74-79`). `.and()`
   intersections and discriminated unions are rejected. Homarr's
   `convertIntersectionToZodObject` merges only object∧object and **drops
   refinements**, at runtime too
   (`v2.5.2:packages/api/src/schema-merger.ts:4-21`; bug 10). t2o itself leaves
   root refinements out of the documented request body only
   (`t2o:generator/schema.mjs:94-108`), which `open-api.ts:63-75` patches back
   for `PATCH /api/users/preferences`.
5. **GET and DELETE inputs travel as path or query parameters**, so every field
   is a string, number, boolean, bigint or date, or an array of one (the query
   key repeated), and path parameters are required keys of the input object
   (`t2o:generator/schema.mjs:6-75`, `t2o:adapters/node-http/core.mjs:72-93`).
   Homarr patches t2o to always coerce non-string values
   (`v2.5.2:patches/trpc-to-openapi.patch:7-8`).
6. **Request bodies are JSON.** `POST`, `PUT` and `PATCH` need
   `Content-Type: application/json`, otherwise the handler answers 415
   (`core.mjs:52-61`); the REST route sets that header when it is missing
   (`route.ts:38-41`). The fetch adapter would parse x-www-form-urlencoded
   (`t2o:adapters/fetch.mjs:21-26`), but that check rejects it, and there is no
   multipart, so `zod-form-data` inputs such as `certificate.addCertificate`
   (`v2.5.2:packages/api/src/router/certificates/certificate-router.ts:85-91`)
   and `mediaUploadSchema` (`v2.5.2:packages/validation/src/media.ts:8`) cannot
   be exposed.
7. **Responses are plain `JSON.stringify`, not superjson** (`core.mjs:18-27`). A
   `Date` becomes an ISO string, as `GET /api/invites` already shows
   (`v2.5.2:packages/api/src/router/invite.ts:14-33`); `Map` and `Set` become
   `{}`, and a `BigInt` makes the response a 500.
8. **Failures throw `TRPCError`**, whose code maps to an HTTP status
   (`t2o:adapters/node-http/errors.mjs:2-24`) and a body
   `{message, code, data, issues}` (`core.mjs:141-162`): a zod input error is a
   400 "Input validation failed" with `issues`, and `data` carries the
   `zodError` and `error` fields of Homarr's `errorFormatter` (`trpc.ts:81-88`).
   A procedure that *returns* `{ error }` gives a REST client HTTP 200.
9. **Housekeeping:** the tag in `open-api.ts:76-87`, a path assertion in
   `v2.5.2:packages/api/src/test/open-api.spec.ts:7-15`, and a regenerated
   `apps/docs/public/api/open-api-schema.json` (`scripts:update-openapi`,
   `v2.5.2:package.json:299`), which CI compares with the generated document
   (`v2.5.2:scripts/check-openapi-schema.mts:25-40`, run as `check:openapi` by
   `v2.5.2:.github/workflows/ci.yml:157-158`).

[#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes to register
`groupRouter` and the whole `integrationRouter` instead of the two-procedure
`integration` sub-router (`integration.request` and `integration.testConnection`
keep their paths and operation ids), to add `GET /api/integrations/kinds` to the
optional-authentication override (point 1), and a `groups` tag. Its REST route
also passes `isOpenApiRequest: true` into the tRPC context, which integration
create and update use to keep connection-test details out of REST error bodies
(item [6](#6-integrations)).

tRPC already accepts the `ApiKey` header
(`v2.5.2:apps/nextjs/src/app/api/trpc/[trpc]/route.ts:34-42`), and Homarr's docs
tell REST users to "Use tRPC for advanced operations"
(`v2.5.2:apps/docs/docs/management/api/index.mdx:13-16`);
[#7174](https://github.com/homarr-labs/homarr/pull/7174) leaves that page
unchanged. REST-only is the provider's own rule, kept because tRPC payloads are
superjson-encoded (`trpc.ts:76-80`) and change freely between releases.

## 1. Board content: sections, tiles, options

**Terraform users want** an authoritative `homarr_board_content` resource owning
every section and tile of a board, and/or a `homarr_board_item` resource for one
tile (`board_id`, `kind`, `options`, `title`, `css_classes`, `border_color`,
`integration_ids`, a placement per layout), plus a data source — all with drift
detection and import by board id.

**Homarr has today:**

* `board.saveBoard`, `PUT /api/boards/{id}/content`
  (`v2.5.2:packages/api/src/router/board.ts:1703-1721`). Input `boardSaveSchema`
  = `{id, sections[], items[]}` (`v2.5.2:packages/validation/src/board.ts:140-144`),
  output `z.void()`. Needs modify access (`board.ts:1723`), admin to add or
  reconfigure Custom Widgets (`:1727-1731`), and checks a changed timetable base
  URL (`:1733-1739`). It replaces the *set* of sections and items: omitted ones
  are deleted (`:1749-1750`, `:1927-1936`). Ids are client-chosen and inserted
  as sent (`:1758`, `:1792`); they are table-wide primary keys
  (`v2.5.2:packages/db/schema/sqlite.ts:393`, `:423`), so a client must generate
  globally unique ones. Existing entries are **not** fully replaced:
  * placements of a new item or container are inserted as sent
    (`board.ts:1768-1786`, `:1799-1813`);
  * for an existing item or container it only `UPDATE`s the placement rows that
    match `(id, layoutId)` (`:1856-1872`, `:1888-1904` on PostgreSQL;
    `:2055-2067`, `:2084-2098` on SQLite). A placement for a layout the entry
    has no row in is silently ignored, and an omitted placement is kept;
  * an existing section's `kind` is never updated (`:1876-1884`, `:2071-2080`),
    while an existing item's `kind`, `options` and `advancedOptions` are
    overwritten (`:1846-1854`);
  * deleting a section deletes the item placements inside it (cascade,
    `sqlite.ts:350-352`), and a later `PUT` cannot re-create them for an
    existing item.
* `board.addItem`, `POST /api/boards/items` (`board.ts:2288-2403`, input
  `validation/board.ts:176-186`): returns `{itemId}` and places the item at the
  first free cell of the topmost main root section, in every layout
  (`board.ts:2338-2385`); it takes no section or position. The provider uses it
  for the `homarr_board_add_item` action.
* `board.updateItemLayout`,
  `PATCH /api/boards/{boardId}/items/{itemId}/layouts/{layoutId}`
  (`board.ts:2404-2499`): moves or resizes an existing placement inside its
  section, answering 400 out of bounds and 409 on overlap (`:2462-2479`). The
  action uses it to position the new tile.
* **No read.** Only `board.getBoardByName` (`board.ts:1210-1224`, MCP meta, no
  `openapi` meta, no `.output()`) returns `getFullBoardWithWhereAsync`
  (`board.ts:2955-3074`): the board row, creator, layouts sorted by breakpoint
  (`:3031-3037`), sections including the per-user `collapsed` flag (`:3052`),
  items `{id, kind, options, advancedOptions, layouts[], integrationIds[]}`
  (`:3055-3072`), and the caller's permission rows (`:2996-3004`). Sections,
  items, placements and integration ids come in no defined order: the query has
  no `orderBy` (`:2977-2994`). Items go through a per-kind union (`:3090-3096`);
  one that fails to parse is logged and skipped (`:3100-3108`), and since
  `saveBoard` diffs against this read (`:1725`, `:1741-1750`), `PUT` neither
  updates nor deletes it. A section that fails to parse makes the read throw
  (`:3110-3117`). Sections go through a schema with a legacy
  `dynamic`→`container` transform
  (`v2.5.2:packages/validation/src/shared.ts:98-102`); options are
  superjson-parsed records (`board.ts:3043`, `:3068-3069`). The read path may
  write: it repairs protected layouts (`:3015-3025`).
* Options are **not** validated per widget kind;
  `validateWidgetConfigurationsAsync` checks integrations only
  (`board.ts:156-211`), and the Custom Widget and timetable checks above are the
  only option checks.
* No per-item read, options update or delete, and no section endpoints; the
  section router has only the per-user `changeCollapsed`
  (`v2.5.2:packages/api/src/router/section/section-router.ts:12-72`).

**Upstream must:**

1. Add **`GET /api/boards/{id}/content`**: wrap `getFullBoardWithWhereAsync` with
   `.meta({openapi:{method:"GET", path:"/api/boards/{id}/content", tags:["boards"], protect:true}})`,
   input `z.object({id: z.string()})`, the access check of `getBoardSettings`
   (modify, `board.ts:1245`) or `getBoardByName` (view, `:1221`), and an output
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
   `validation/shared.ts` (`:50-55`, `:81-96`) and build the output union
   without the legacy transform; define items as one flat `ZodObject`
   (`kind: z.enum(widgetKinds)`, `options: z.record(z.string(), z.unknown())`,
   plus the `sharedItemSchema` shape). Leave out the per-user `collapsed` flag
   and the permission rows.
2. Add per-tile endpoints, for a `homarr_board_item` that does not own the whole
   board: `GET /api/boards/{boardId}/items/{itemId}` (same item shape);
   `PATCH /api/boards/{boardId}/items/{itemId}` with
   `{options?, advancedOptions?, integrationIds?}`, reusing the checks in
   `board.ts:1726-1739`; `DELETE /api/boards/{boardId}/items/{itemId}`; optionally
   an explicit `sectionId` and `layouts` on `POST /api/boards/items`.
3. Optionally, containers: `POST /api/boards/{boardId}/sections`, and
   `PATCH`/`DELETE /api/boards/{boardId}/sections/{sectionId}`.
4. Optionally, make `PUT /content` authoritative for the placements of existing
   items and containers too — insert missing ones and delete omitted ones, or
   reject them with 400 — instead of ignoring them.

`boardRouter` is already registered whole
(`v2.5.2:packages/api/src/open-api.ts:21`), so meta and output are enough. There
are no blockers: release-widget tokens live in `widget_secret`, not in options
(item [15](#15-other-configuration-like-routers)), and view-level users can
already read options over tRPC and MCP (`board.ts:1210-1224`).

**The provider would add:**

* `homarr_board_content`: `board_id`; computed `layout_ids` (role → id);
  `sections` (`id` optional and computed, `kind`, `title`, `css_classes`,
  `border_color`, `show_label`, `collapsible`, `show_open_all`, `scrollable`,
  `placements`); `items` (`id`, `kind`, `options_json` with semantic JSON
  equality, `title`, `css_classes`, `border_color`, `integration_ids`,
  `placements`), where a placement is keyed by layout role or id and holds
  `{x, y, width, height, section_id | parent_section_id}`. Create and update `PUT`
  the full arrays, keeping the root and gutter sections from the last read; read
  is `GET /content`; delete `PUT`s only the root empty section(s). Because of how
  `PUT` treats existing entries, the provider generates globally unique ids,
  sends a placement in every layout when it creates an item or container, and
  replaces an entry (new id) when its section `kind` or the set of layouts it is
  placed in changes. It matches entries by id, never by position.
* `homarr_board_item`: create with `POST /items` and position with the layout
  `PATCH`, read with the item `GET`, update options with the item `PATCH` and
  placement with the layout `PATCH`, delete with `DELETE`. Until `POST /items`
  takes a section, such a tile can live only in the main root section. It does
  not fight UI edits to other tiles. It would replace the
  `homarr_board_add_item` action.
* A `homarr_board_content` data source.

**Priority and effort:** high — the README's main limitation. M (the read alone
is S).

**Existing upstream work:**

* [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open, not merged or
  released) proposes points 1 and 2, except the optional placement on
  `POST /items`.
  Decisions that matter to a REST client:
  * `board.getBoardContent`, `GET /api/boards/{id}/content`: view access behind
    login (`protectedProcedure`; an API key counts, but anonymous reads of
    public boards do not, unlike `getBoardByName`). Output `boardContentSchema`
    = `{id, name, layouts, sections, items}` as sketched above: sections are
    `emptySectionSchema | containerSectionSchema` without `collapsed` and without
    the legacy transform, items one flat `boardItemSchema`, layouts
    `boardLayoutFieldsSchema` with `role`. `name` and `layouts` are ignored by
    `PUT`, so GET→PUT changes nothing. It reuses `getFullBoardWithWhereAsync`,
    so layout repair, skipped unparseable items and the missing order are as
    above (clients key by id). It leaves `PUT`'s placement handling unchanged.
  * `board.getItem`, `board.updateItem`, `board.deleteItem`:
    `GET` (view), `PATCH` and `DELETE` (modify)
    `/api/boards/{boardId}/items/{itemId}`. An unknown item or one of another
    board answers 404 `Item not found`; `GET` and `PATCH` do so for an
    unparseable item too, while `DELETE` removes it. A caller who can view but
    not modify the board gets 403 (item [16](#16-board-lifecycle-details)).
  * `updateItem` takes `{options?, advancedOptions?, integrationIds?}`
    (`updateBoardItemSchema`, at most 32 unique integration ids). Each supplied
    field replaces the stored value — `options` whole, unlike the tRPC-only
    `saveItemOptions`, which merges; `advancedOptions` whole, with omitted
    sub-fields reset to their defaults. Omitted fields are not written; kind and
    placements cannot change. It runs `saveBoard`'s checks (integration
    existence, use access and kind support, Custom Widget admin, timetable base
    URL only when `options` is sent) and returns the item read back.
  * `deleteItem` removes placements, integration links and widget secrets by
    cascade.
  * `app` options are validated and completed with defaults on `POST /items`,
    and for new or changed items on `PUT /content` and the item `PATCH` (item
    [11](#11-app-tile-options)).

  Left open: point 3 (section endpoints), explicit placement on `POST /items`,
  point 4, and MCP meta for the new procedures (MCP keeps `getBoardByName`).
* `origin/add-more-openapi` has an items router (`bd7a9fbd1` 2025-08-31,
  `56aae8885` 2025-12-10) with `GET /api/boards/{boardId}/items`,
  `POST /api/items` and `GET`/`PUT`/`DELETE /api/items/{id}`
  (`origin/add-more-openapi:packages/api/src/router/item/item-router.ts:66-366`),
  using per-kind option schemas from `@homarr/widgets`. Stale: last commit
  2025-12-19, 1,742 commits behind `v2.5.2`, never merged, predates the
  allow-list.
* Discussion: [#6435](https://github.com/homarr-labs/homarr/issues/6435).

## 2. Board layouts and column count

**Terraform users want** `homarr_board.column_count` changeable in place,
`left_gutter_column_count` and `right_gutter_column_count`, computed
`base_layout_id` and `mobile_layout_id`, import by bare id instead of
`<id>,<column_count>`, and optionally `homarr_board_layout` for custom
breakpoints.

**Homarr has today:**

* `createBoard`, `POST /api/boards` (`v2.5.2:packages/api/src/router/board.ts:667-745`),
  creates a 3-column Mobile layout at breakpoint 0 (`:716-725`) and a Base layout
  with the requested `columnCount` and no gutters at breakpoint 768 (`:726-735`).
  It returns `{boardId, name, layoutId}`, where `layoutId` is the **Base** layout
  (`:687`, `:744`) — the provider's `base_layout_id`. The Mobile id is never
  returned, and `boardCreateSchema` takes no gutter counts
  (`v2.5.2:packages/validation/src/board.ts:146-150`). `duplicateBoard` returns
  `{boardId}` only (`board.ts:766`, `:1052`).
* `saveLayouts`, `PUT /api/boards/{id}/layouts` (`board.ts:1284-1569`), demands
  the **complete** array — exactly one mobile (breakpoint 0) and one base layout,
  unique ids and breakpoints, 0–3 gutter columns per side that leave at least
  one column and none on mobile (`validation/board.ts:30`, `:82-133`). New
  layouts must use the `custom` role (`board.ts:1316-1318`); mobile and base
  cannot be removed (`:1319-1321`) and roles cannot change (`:1322-1329`); a
  custom layout left out of the array is deleted (`:1314`, `:1496-1498`). Every
  submitted layout row is rewritten, changed or not (`:1401`, `:1483-1495`), so a
  `PUT` built from an older read undoes concurrent changes to other layouts. The
  output echoes the input sorted by breakpoint, with server ids for new custom
  layouts (`:1369-1371`, `:1562-1568`).
* `resetLayout`, `POST /api/boards/{boardId}/layouts/{layoutId}/reset`
  (`board.ts:1570-1650`).
* **No read.** `GET /api/boards` (`boardSummarySchema`,
  `validation/board.ts:158-174`) and `GET /api/boards/{id}/settings`
  (`validation/board.ts:77-80`) carry no layouts; only tRPC/MCP `getBoardByName`
  (`board.ts:1210-1224`) does.

**Upstream must** do any one of these, in order of preference:

1. `GET /api/boards/{id}/layouts` with output
   `z.array(z.object({id, name, role, columnCount, leftGutterColumnCount, rightGutterColumnCount, breakpoint}))`,
   reusing `boardLayoutSchema` without its refines (or as part of item 1's read).
2. `PATCH /api/boards/{boardId}/layouts/{layoutId}` with
   `{name?, columnCount?, leftGutterColumnCount?, rightGutterColumnCount?, breakpoint?}`;
   the server rebuilds the full array and reuses the `saveLayouts` reprojection
   (`board.ts:1305-1560`), so clients never need ids of layouts they do not
   manage.
3. Minimal fix: add `mobileLayoutId` (or all `layouts`) to the outputs of
   `createBoard` and `duplicateBoard`. This helps only boards the provider
   created or copied, not imported ones.
4. Optionally, accept gutter counts in `boardCreateSchema`
   (`validation/board.ts:146-150`).

[#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes all four; see
existing upstream work.

**The provider would add:** an updatable `column_count` (`GET` layouts, then the
base layout's `PATCH`; with only the full-array `PUT`, a read–modify–write);
optional gutter counts (0–3, sum below the column count, never on mobile), sent
at create where Homarr accepts them; computed `mobile_layout_id`;
`base_layout_id` and `mobile_layout_id` after import and on
`homarr_board_copy`; drift detection for all of these; import by bare id;
`column_count` in the `homarr_board` data source; optionally
`homarr_board_layout` for custom layouts. All of it is gated on a Homarr release
that ships the endpoints; older releases keep the `<id>,<column_count>` import
and the replacement on a `column_count` change.

**Priority and effort:** high — today a `column_count` change destroys the
board and its content. S.

**Existing upstream work:** [#7174](https://github.com/homarr-labs/homarr/pull/7174)
(open, not merged or released) covers 1–4. Decisions that matter to a REST
client:

* `board.getBoardLayouts`, `GET /api/boards/{id}/layouts`: the same access as
  the content read (item 1). It returns every layout through a new
  `boardLayoutFieldsSchema` — `boardLayoutSchema` without its refines — ordered
  by breakpoint, repairing a missing Mobile or Base layout first, as the
  full-board read does. `GET /api/boards/{id}/content` (item 1) carries the same
  layouts.
* `board.updateLayout`, `PATCH /api/boards/{boardId}/layouts/{layoutId}`: modify
  access, the body above, role fixed; it returns the updated layout. The body of
  `saveLayouts` moves into a shared helper, `saveBoardLayoutsAsync`, which merges
  the `PATCH` into the same read it saves against, validates the merged array
  with `responsiveBoardLayoutsSchema` (400 "Input validation failed" with the
  broken rule in `issues`, where tRPC callers get the zod message; 404 for an
  unknown or foreign layout id) and reprojects items and
  sections as `saveLayouts` does. The helper writes only rows whose name, column
  count, gutters or breakpoint changed — for `PUT` too — so concurrent saves of
  different layouts no longer overwrite each other. Saves of the same layout can
  still interleave: there is no row lock.
* `createBoard` also returns `mobileLayoutId`; `layoutId` stays the Base layout.
  It accepts optional `leftGutterColumnCount` and `rightGutterColumnCount` (0–3)
  for the Base layout, answers 400 when they leave no column, and creates the
  side canvases.
* `duplicateBoard` returns `{boardId, baseLayoutId, mobileLayoutId}` (item 16);
  the keys differ from `createBoard`'s `layoutId`.

It leaves open adding or removing a single custom layout. That still takes the
full-array `PUT`, which deletes every custom layout missing from the array, so
`homarr_board_layout` would read, modify and write the array under a per-board
lock in the provider. Before #7174, #6546 added the layout write and reset and
#6213 the duplicate, both without a read.

## 3. Board permissions

**Terraform users want** an authoritative `homarr_board_permissions` resource
(`user_permissions` = set of `{user_id, permission}`, `group_permissions` = set of
`{group_id, permission}`), optionally per-grant resources, and a data source that
includes inherited permissions.

**Homarr has today** (none of these has `openapi` meta or an output schema):

* `board.getBoardPermissions` (`v2.5.2:packages/api/src/router/board.ts:2136-2207`)
  needs **full** access (`:2137`) and returns
  `inherited: [{permission, group:{id,name}}]`,
  `users: [{user:{id,name,image,email}, permission}]` and
  `groups: [{group:{id,name}, permission}]`. `inherited` lists every group whose
  global permission is a `board-*-all` permission or implies one, `admin`
  included (`:2139-2155`; `v2.5.2:packages/definitions/src/permissions.ts:54-83`).
  The creator's implicit full access and a public board's view access are not
  rows, so neither is listed
  (`v2.5.2:packages/auth/permissions/board-permissions.ts:24-46`).
* `saveUserBoardPermissions` (`board.ts:2208-2246`) and
  `saveGroupBoardPermissions` (`:2247-2287`) take
  `{entityId, permissions:[{principalId, permission}]}`
  (`v2.5.2:packages/validation/src/permissions.ts:3-15`,
  `validation/board.ts:152`); each is an authoritative replace in a transaction.
  An unknown principal fails on the foreign key with a 500.
* Rows are keyed `(boardId, principal, permission)`
  (`v2.5.2:packages/db/schema/sqlite.ts:294-328`): a principal listed twice with
  the same permission fails on the primary key with a 500, and one listed with
  two permissions is stored as two rows. `GET /api/boards` shows only the
  caller's own rows (`board.ts:377-382`, `validation/board.ts:172-173`).
* A caller without full access gets 404 "Board not found", even one who can view
  the board (`v2.5.2:packages/api/src/router/board/board-access.ts:16-75`).

**Upstream must:** add meta and an output schema for
`GET /api/boards/{id}/permissions`; add `PUT /api/boards/{entityId}/permissions/users`
and `PUT /api/boards/{entityId}/permissions/groups` with body
`{permissions:[{principalId, permission}]}` (the path parameter must equal an
input key, `t2o:generator/schema.mjs:9-16`, so keep `entityId` or rename the
field to `id`); optionally per-grant
`PUT`/`DELETE /api/boards/{boardId}/permissions/users/{userId}` and the same for
groups; validate principal ids and reject a principal listed twice before
writing. `open-api.ts` needs no change: the board router is allow-listed whole
(`v2.5.2:packages/api/src/open-api.ts:19-34`).
[#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes all of this
except the per-grant endpoints; see existing upstream work.

**The provider would add:** `homarr_board_permissions` — create and update `PUT`
both lists, read is `GET` (the `PUT`s return no body), delete `PUT`s empty lists,
import by board id — with at most one permission per principal; a data source
that also returns `inherited`; per-grant resources if those endpoints land.

**Priority and effort:** high. S.

**Existing upstream work:** [#7174](https://github.com/homarr-labs/homarr/pull/7174)
(open, not merged or released) adds `openapi` meta (tag `boards`) and an explicit
output to the three procedures. Decisions that matter to a REST client:

* `GET /api/boards/{id}/permissions` (`board.getBoardPermissions`, full access)
  validates today's shape through a new `boardPermissionsSchema`; user `name`,
  `image` and `email` are nullable, and `inherited[].permission` can be any group
  permission key.
* `PUT /api/boards/{entityId}/permissions/users` and `…/groups` keep today's
  input, so the path parameter stays `entityId`. Each still replaces every
  explicit grant of its kind in one transaction and answers 200 with an empty
  body.
* After the full-access check and before writing, a principal listed twice
  answers 400 "Principal IDs must be unique" — even with different permissions,
  so a principal holds one permission — and an unknown id answers 404 "User not
  found" or "Group not found". Nothing is written, so existing grants stay; a
  caller without full access cannot probe ids.
* With the access change of item 16, a caller who can view the board but lacks
  full access gets 403 instead of 404.

It leaves open the per-grant endpoints and an unlocked existence check: a
principal deleted between the check and the insert still fails on the foreign
key with a 500.

## 4. Groups

**Terraform users want** `homarr_group` (`name`, `permissions`,
`home_board_id`, `mobile_home_board_id`, `position`), `homarr_group_members`
(authoritative) or `homarr_group_member`, `homarr_group_order`, and `homarr_group`
/ `homarr_groups` data sources, for example to look up `everyone`.

**Homarr has today:** `v2.5.2:packages/api/src/router/group.ts`, unchanged since
2.4.1. It is not in the allow-list
(`v2.5.2:packages/api/src/open-api.ts:19-34`), so no group endpoint exists over
REST. Every procedure below except `selectable` needs admin; none has `openapi`
meta or `.output()`; all outputs are JSON-safe.

| Procedure | Lines | Notes |
| --- | --- | --- |
| `getAll` | 24-46 | `members[{id,name,email,image}]`, no permissions |
| `getPaginated` | 48-82 | |
| `getById` | 83-131 | `id, name, ownerId, homeBoardId, mobileHomeBoardId, position`, `members[{id,name,email,image,provider}]`, `permissions[]`, `owner` or null |
| `selectable` | 133-159 | any user; `id`, `name` |
| `search` | 160-177 | |
| `createGroup` | 224-241 | position = max + 1, owner = caller; returns the id string (`group.ts:240`); name trimmed, 1-64 chars, not `everyone` (`v2.5.2:packages/validation/src/group.ts:8-17`) |
| `updateGroup` | 242-257 | name only |
| `savePartialSettings` | 258-271 | `{id, settings:{homeBoardId?, mobileHomeBoardId?}}` (`validation/group.ts:21-29`); `settings: {}` is a 500, drizzle's "No values to set" (`group.ts:264-270`); an unknown board is a foreign-key 500 (`v2.5.2:packages/db/schema/sqlite.ts:157-162`) |
| `savePositions` | 272-294 | `{positions: ids[]}`, position = index + 1 (`group.ts:276`); the list is not checked, so a partial list leaves duplicate positions and unknown ids are ignored (bug 8) |
| `savePermissions` | 295-338 | authoritative, deduplicated, stored without implied permissions (`group.ts:301-303`); keys in `v2.5.2:packages/definitions/src/permissions.ts:38-47` |
| `transferOwnership` | 339-352 | |
| `deleteGroup` | 353-361 | |
| `addMember` | 362-392 | 403 unless members and the user's provider are managed locally (`group.ts:368`, `:381-386`); re-adding a member is a primary-key 500 (`sqlite.ts:144-148`) |
| `removeMember` | 393-404 | checks only `canManageGroupMembersLocally` (`group.ts:399`); an unknown user or a non-member is a no-op, and an externally synchronised member can be removed until the next sign-in re-adds it |

Create and rename use the new name as an SQL `LIKE` pattern
(`group.ts:407-418`): any existing name it matches answers 409, and `_` and `%`
in it act as wildcards.

The reserved `everyone` group cannot be renamed, deleted, transferred or have its
members changed (`group.ts:420-429`); permissions and home boards are allowed.
It is seeded with a random id and position -1
(`v2.5.2:packages/db/migrations/seed.ts:305-321`), and every user is added to it
at sign-in (`v2.5.2:packages/auth/events.ts:47`, `:95-112`). The groups page
leaves it out of the order
(`v2.5.2:apps/nextjs/src/app/[locale]/manage/users/groups/page.tsx:25-26`, `:51`),
but `savePositions` accepts it.

LDAP users, and OIDC users unless `AUTH_OIDC_GROUPS_LOCAL_MANAGEMENT` is set, have
their memberships re-synced by group name at every sign-in, `everyone` excepted
(`events.ts:33-46`, `:114-192`); since 2.5.0 (#7089) a single-string OIDC groups
claim is synced too (`events.ts:34-35`). `isGroupMembershipManagedLocally`
(`v2.5.2:packages/auth/providers/check-provider.ts:18-29`) encodes this rule.

Group `position` decides whose home board wins: `getHomeIdBoardAsync` takes the
user's lowest-position group other than `everyone`
(`v2.5.2:packages/api/src/router/board.ts:2530-2550`). It requires both the
device's home board and `homeBoardId` (`:2541-2542`), so on mobile a group with
only one of the two is skipped and the group `homeBoardId` fallback (`:2550`) is
dead code (bug 7). Deleting a board clears group home boards through
`ON DELETE SET NULL` (`sqlite.ts:157-162`).

**Upstream must** register `groupRouter` in `open-api.ts` with tag `groups` and
add meta and output to:

* `GET /api/groups` → `[{id, name, position, homeBoardId, mobileHomeBoardId, ownerId, permissions[], memberIds[]}]`
* `GET /api/groups/{id}` → the `getById` shape, plus whether each membership is
  managed locally
* `POST /api/groups` with `{name}` → `{id}` (wrapping the returned string)
* `PATCH /api/groups/{id}` with `{name}`; `DELETE /api/groups/{id}`
* `PUT /api/groups/{groupId}/permissions` with `{permissions[]}` → echo
* `PATCH /api/groups/{id}/settings` with
  `{homeBoardId?: string|null, mobileHomeBoardId?: string|null}` — flatten the
  nested `settings` object, check that the boards exist and accept an empty body
* `PUT /api/groups/positions` with the **complete** `{positions[]}` (a partial
  list leaves duplicate positions), or `position` in the group `PATCH`
* `PUT /api/groups/{groupId}/members` with `{userIds[]}`, authoritative and
  refusing externally managed users, plus idempotent
  `POST`/`DELETE /api/groups/{groupId}/members/{userId}`

[#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes all of these;
see existing upstream work.

**The provider would add:** `homarr_group` (create with `POST`, then `PUT`
permissions and `PATCH` settings; read with `GET /api/groups/{id}`; update in
place; delete; import by id; `position` and `owner_id` computed); attachable
`homarr_group_permissions` and `homarr_group_settings` that also work on
`everyone`, whose id differs per instance; `homarr_group_members` for locally
managed users, which reads `isMembershipManagedLocally` and keeps externally
synchronised members in every `PUT`, or per-user `homarr_group_member`;
`homarr_group_order`, which owns the order of every group except `everyone`;
`homarr_group` and `homarr_groups` data sources. A membership belongs either here
or in `homarr_user.group_ids` (item 5), not both.

**Priority and effort:** high. M.

**Existing upstream work:** [#7174](https://github.com/homarr-labs/homarr/pull/7174)
(open, not merged or released) registers `groupRouter` with tag `groups` and
exposes 10 procedures plus a new `saveMembers` as 11 admin-only endpoints that
meet every point above. Decisions that matter to a REST client:

* `GET /api/groups` includes `everyone` and returns `permissions[]` and
  `members[{id, name, email, image}]` objects rather than `memberIds[]`.
* `GET /api/groups/{id}` returns the `getById` shape, with each member's
  `provider` and a new `isMembershipManagedLocally` flag.
* `POST /api/groups` returns `{id}` and `PATCH /api/groups/{id}/settings` takes the
  flat body; both change the tRPC contract.
* Settings answer 404 for an unknown board, treat an empty body as a no-op and are
  allowed on `everyone`; whether the group's members can view the board is not
  checked.
* `PUT /api/groups/positions` requires every group except `everyone` exactly once
  (400 for a duplicate or missing group, 404 for an unknown one, 403 for
  `everyone`), which fixes bug 8.
* `PUT /api/groups/{groupId}/permissions` returns the stored, deduplicated list as
  `{permissions}`.
* `PUT /api/groups/{groupId}/members` replaces members in one transaction and
  returns the stored `{userIds}`. It answers 404 for an unknown user and 403 for
  `everyone` or if it would add or remove an externally synchronised member, so
  those members must stay listed.
* `POST /api/groups/{groupId}/members/{userId}` is idempotent instead of a
  primary-key 500. `DELETE` now refuses externally managed (403) and unknown
  (404) users, so a stale synchronised membership can no longer be ended through
  the API.

It leaves open a per-group position (only the whole order can be written), the
unexposed `getPaginated`, `selectable`, `search` and `transferOwnership` (the
owner stays read-only), the `LIKE` name check and bug 7. `origin/add-more-openapi`
7ea71a57b (2025-08-31, meta for part of the router) is stale history that #7174
supersedes.

## 5. A user's group membership

**Terraform users want** `homarr_user.group_ids` readable and updatable in
place. Today it is create-only: adding or removing a group replaces the user, and
an imported user with `group_ids` configured plans a replacement.

**Homarr has today:**

* Membership is written only at creation: `POST /api/users` accepts `groupIds`
  (`v2.5.2:packages/validation/src/user.ts:85-87`) and inserts them unchecked,
  after the user and outside a transaction
  (`v2.5.2:packages/api/src/router/user.ts:213-223`), so a duplicate or unknown
  id fails with a 500 once the user exists
  (`v2.5.2:packages/db/schema/sqlite.ts:134-149`). Its output is `z.void()`
  (`user.ts:214`), so the new id is not returned either.
* Nothing returns membership over REST: not `GET /api/users/{userId}`
  (`user.ts:419-497`, output `:421-443`), not `GET /api/users` (`:280-316`). The
  group router is not allow-listed (`v2.5.2:packages/api/src/open-api.ts:19-34`;
  item 4), so the `everyone` group's id cannot be read either.
* `everyone` membership is added at sign-in when missing
  (`v2.5.2:packages/auth/events.ts:47`, `:95-112`), not at API creation
  (`user.ts:909-920`). LDAP and non-local OIDC memberships are re-synced from the
  identity provider at every login (`events.ts:33-46`, `:114+`;
  `v2.5.2:packages/auth/providers/check-provider.ts:18-29`); since 2.5.0 (#7089)
  also when the OIDC groups claim is a single string (`events.ts:34-35`).

**Upstream must:** add `groupIds: z.array(z.string())` to the
`GET /api/users/{userId}` output; add `PUT /api/users/{userId}/groups` with
`{groupIds}` — admin only, authoritative, 403 when
`!isGroupMembershipManagedLocally(provider)`, `everyone` implicit — or use the
member endpoints of item 4; return `{id}` from `POST /api/users`; validate group
ids before creating the user, and insert the user and its memberships in one
transaction. [#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes
all of this; see existing upstream work.

**The provider would add:** `homarr_user.group_ids` optional and computed,
updated in place with `PUT`, read back from `GET` user, ignoring `everyone`
(whose id `GET /api/groups` of item 4 returns). `homarr_user` manages only
credentials users, whose memberships are always local (`check-provider.ts:20-21`),
so a 403 reaches it only when no enabled provider allows local member management
or for an LDAP or OIDC account imported by an earlier provider version; it would
be reported as an error. The id recovery after create, the tainting after a
failed create and the import caveat would go.

**Priority and effort:** high. S.

**Existing upstream work:** [#7174](https://github.com/homarr-labs/homarr/pull/7174)
(open, not merged or released) meets every point above. Decisions that matter to
a REST client:

* `GET /api/users/{userId}` gains `groupIds`, the stored memberships, so
  `everyone` is included once the user has signed in or was given it at creation.
  Access is unchanged (self or admin).
* `PUT /api/users/{userId}/groups` (`user.setGroups`, admin) takes `{groupIds}`,
  replaces the memberships in one transaction and returns `{groupIds}` as stored
  afterwards. An existing `everyone` membership is kept and an `everyone` id in
  the body is ignored, so a client can send back what it read; a missing one is
  not added. Repeated ids are harmless.
* It answers 403 when no enabled provider allows local member management, or
  when the user is LDAP, or OIDC without `AUTH_OIDC_GROUPS_LOCAL_MANAGEMENT` —
  the checks of `group.addMember`
  (`v2.5.2:packages/api/src/router/group.ts:368`, `:381-386`) — and 404 for an
  unknown user or group; a rejected request changes nothing.
* `POST /api/users` returns `{id}`, de-duplicates `groupIds` and checks that they
  exist before writing (404, no user created), then inserts the user and its
  memberships in one transaction. It still accepts and stores an `everyone` id.
* `GET /api/users` gains `provider` but not `groupIds`; `GET /api/groups` (item 4)
  lists `everyone` with its id.

It leaves open a race: groups are checked before the transaction, so a group
deleted in between still fails on the foreign key. PR #7078 exposes groups only
to custom widgets.

## 6. Integrations

**Terraform users want** `homarr_integration` (`name`, `url`, `kind` forcing
replacement, sensitive `secrets`, `app_id`), `homarr_integration_permissions`
(users and groups with `use`, `interact` or `full`), and `homarr_integration`,
`homarr_integrations` and `homarr_integration_kinds` data sources.

**Homarr has today:**

* **REST**, two procedures (`v2.5.2:packages/api/src/open-api.ts:30-33`), both
  requiring *full* access to the integration — `integration-full-all`, which
  admins have, or a per-integration `full` grant
  (`v2.5.2:packages/auth/permissions/integration-permissions.ts:14-29`):
  `POST /api/integrations/request`, a proxy
  (`v2.5.2:packages/api/src/router/integration/integration-request.ts:124-151`)
  that answers 403 without that access
  (`v2.5.2:packages/api/src/router/integration/integration-http.ts:41-46`,
  `:74-75`), and `GET /api/integrations/{id}/test-connection`
  (`v2.5.2:packages/api/src/router/integration/integration-test-stored-connection.ts:12-57`),
  which answers 404 as for a missing integration
  (`v2.5.2:packages/api/src/router/integration/integration-access.ts:87-96`)
  and which the provider exposes as `homarr_integration_connection`.
* **tRPC only**
  (`v2.5.2:packages/api/src/router/integration/integration-router.ts`, no
  `openapi` meta):

| Procedure | Lines | Notes |
| --- | --- | --- |
| `getKinds` | 56-71 | public; `{kind, name, category, requiredSecrets: string[][]}`; no output schema |
| `all` | 72-82 | integrations the caller can use, as `{id, name, kind, url, permissions}` (`:750-756`, `:767-799`) |
| `byId` | 155-224 | full access; `{id, name, kind, url, secrets:[{kind, value, updatedAt}], app}`; `value` only for public kinds such as username (`:219`; `v2.5.2:packages/definitions/src/integration.ts:11-32`) |
| `create` | 225-309 | permission `integration-create`, plus `app-create` for a new app (`integration-router.ts:243-248`) |
| `update` | 310-415 | full access |
| `delete` | 416-443 | full access |
| `getIntegrationPermissions` | 444-515 | full access; `{inherited, users, groups}`, the same shape as for boards |
| `saveUserIntegrationPermissions` | 516-561 | authoritative `{entityId, permissions:[{principalId, permission}]}` (`v2.5.2:packages/validation/src/integration.ts:45`) |
| `saveGroupIntegrationPermissions` | 562-607 | as above |

* `create` (input `validation/integration.ts:9-29`: `name`, http(s)-only `url`,
  `kind`, `secrets[{kind,value}]`, a required but never read
  `attemptSearchEngineCreation`, optional `app` as `{id}` or a new app) always
  tests the connection first and on failure **returns** `{error}` with HTTP 200
  (`integration-router.ts:265-270`); on success it returns
  `{integration:{id, name, kind, url}, appId}` (`:300-308`). Its writes are
  separate statements without a transaction (`:272-291`): an unknown app id
  fails on the foreign key, and a repeated secret kind fails on the
  `(integrationId, kind)` key after the integration row is written
  (`v2.5.2:packages/db/schema/sqlite.ts:203`, `:222-225`), both as 500.
* `update` (input `validation/integration.ts:31-43`) takes a `url` **without**
  the http(s) restriction and accepts empty secret values; in
  `secrets[{kind, value|null}]`, `null` keeps the stored value and an **omitted
  kind is deleted** (`integration-router.ts:390-403`); `appId` is nullable and
  unchecked, and `kind` immutable. It tests the connection with every stored
  secret, including those it is about to delete (`:331-339`), so removing a
  required secret passes the test and leaves a broken integration; it also
  returns `{error}` on a failed test (`:349-354`). A sent value never compares
  equal to the stored one, because `encryptSecret` uses a fresh IV
  (`v2.5.2:packages/common/src/encryption.ts:12-18`), so every sent value is
  rewritten (`integration-router.ts:365-388`) and bumps that secret's
  `updatedAt` (`sqlite.ts:215-217`).
* The permission saves replace all rows in a transaction, but an unknown user
  or group fails on the foreign key and a principal listed twice with the same
  level fails on the primary key, both as 500; listed with two different levels,
  it is stored twice (`sqlite.ts:231-265`; bug 16).

**Upstream must:**

1. Extend the `integration` sub-router in `open-api.ts:30-33` with meta and
   output for `GET /api/integrations/kinds`, `GET /api/integrations`,
   `GET /api/integrations/{id}` (non-public secret values stay `null`;
   `updatedAt` as an ISO string per secret), `POST /api/integrations` returning
   the new id and `appId`, `PUT /api/integrations/{id}`,
   `DELETE /api/integrations/{id}`, `GET /api/integrations/{id}/permissions`,
   and `PUT /api/integrations/{entityId}/permissions/users` and `.../groups`.
2. Throw `TRPCError({code:"BAD_REQUEST", message:<category>})` instead of
   returning `{error}`, or declare a discriminated output, so REST callers see a
   non-2xx status.
3. Add `skipConnectionTest` (default `false`) to create and update:
   infrastructure as code provisions integrations before the target service is
   reachable.
4. Make `attemptSearchEngineCreation` optional, and apply the http(s) check to
   `update.url`.
5. Reject invalid input with a 4xx before writing — repeated secret kinds,
   unknown app ids, repeated or unknown principals — and test an update only
   with the secrets it keeps.

Secret values stay write-only by design; the per-secret `updatedAt`, which
changes only when a value is sent, is the only way to detect drift.

**The provider would add:** `homarr_integration` — `name`, `url`, `kind`
(replace), `app_id`, `skip_connection_test`; a sensitive `secrets` map, or on
Terraform 1.11+ a write-only `secrets_wo` with `secrets_wo_version`; computed
`public_secrets` and `secret_updated_at`; create with `POST`, read with `GET`,
update with `PUT` always sending **every** kind (`null` for unchanged, so
`updatedAt` moves only on a real change), delete; a failed connection test
reported with its category; plus `homarr_integration_permissions` (`inherited`
read-only) and the three data sources. The `integration_ids` of
`homarr_board_add_item` could then be looked up instead of copied from the UI's
address bar.

**Priority and effort:** high. M.

**Existing upstream work:** released are only the two REST helpers, #6874
(request proxy, 2026-09-21) and #7014 (stored connection test, 2026-10-03).
[#7174](https://github.com/homarr-labs/homarr/pull/7174) (open, not merged or
released) registers the whole `integrationRouter`, whose procedures without
`openapi` meta stay off REST, and meets points 1–5:

* **Endpoints (1).** All nine, at the paths above, with output schemas.
  `GET /api/integrations/kinds` is public with optional `ApiKey`.
  `GET /api/integrations` lists only integrations the caller can use, with
  `hasUseAccess`, `hasInteractAccess` and `hasFullAccess` flags.
  `GET /api/integrations/{id}` needs full access (404 otherwise) and returns
  `value` only for public kinds plus `updatedAt` per secret. `POST` keeps the
  nested `{integration:{id, name, kind, url}, appId}` response, not a flat
  `{id, appId}`, because the UI completion modal and MCP read it; the provider
  takes `integration.id`. `PUT` documents the secret rules above (a value
  replaces, `null` keeps, an omitted stored kind is deleted; `kind` cannot
  change) and returns nothing.
* **Failed tests (2).** create and update throw `BAD_REQUEST` whose message is
  the category (`authorization`, `certificate`, `request`, `statusCode`,
  `parse` or `unknown`). trpc-to-openapi spreads the tRPC error shape, including
  `data.error` (`v2.5.2:packages/api/src/trpc.ts:81-88`), into the REST body
  (`t2o:adapters/node-http/core.mjs:154-161`), so the detailed error is attached
  only when a new `isOpenApiRequest` context flag, set by the REST route, is
  absent: REST bodies carry only the category, as on `test-connection`, and the
  forms keep their detailed error card. The tRPC contract changes with it; the
  UI, MCP and assistant callers are updated.
* **`skipConnectionTest` (3)** skips only the network test: the secrets sent on
  create, or kept on update, must still cover one `requiredSecrets` option, else
  400 `No secret defined for this integration`.
* **Validation (4, 5).** `attemptSearchEngineCreation` is removed rather than
  made optional; zod strips the key when old callers still send it. update
  requires http(s) URLs and non-empty secret values, and tests with the kept
  secrets only, so removing a required secret is a 400. Repeated secret kinds
  return 400 and an unknown app 404 on create and update; a repeated principal
  returns 400 and an unknown user or group 404 on the permission saves, all
  before any write.

Left open: create and update still write in separate statements without a
transaction, so a failure after the validation above can leave a partial row;
`requiredSecrets` lists only the `secretKinds` options, not optional kinds such
as Wazuh's `wazuhDashboardUrl` (`definitions/integration.ts:988`), and secret
kinds outside a kind's options are stored unchecked, so the provider cannot
validate `secrets` completely from `GET /api/integrations/kinds`.

## 7. Search engines

**Terraform users want** `homarr_search_engine` (`name`, `short`, `icon_url`,
`description`, `type` = `generic` or `fromIntegration`, `url_template`,
`integration_id`), the instance-wide default engine, and a
`homarr_search_engines` data source.

**Homarr has today:** `v2.5.2:packages/api/src/router/search-engine/search-engine-router.ts`,
no `openapi` meta: `getPaginated` (17-31), `getSelectable` (32-45), `byId`
(47-72, any signed-in user, discriminated by `type`), `getDefaultSearchEngine`
(73-132, silently clears a stale default by writing settings, `:119-129`),
`catalog` (133-154), `search` (155-175), `create` (176-190,
`search-engine-create`, returns **void**, lowercases `short` at `:183`), `update`
(191-217, `search-engine-modify-all`, `short` immutable per
`v2.5.2:packages/validation/src/search-engine.ts:32-38`), `delete` (218-229,
`search-engine-full-all`; clears the users' defaults at
`search-engine-router.ts:222-227`, not the instance default).

* The inputs are `discriminatedUnion(...).and(object)`
  (`search-engine.ts:23-38`): not a `ZodObject`,
  and `convertIntersectionToZodObject`
  (`v2.5.2:packages/api/src/schema-merger.ts:4-21`) merges only two objects.
* For `fromIntegration`, `integrationId` is optional (`search-engine.ts:11-14`),
  yet `byId` asserts it is non-null (`search-engine-router.ts:59-65`).
* `short` is a unique column (`v2.5.2:packages/db/schema/sqlite.ts:562`), and
  `create` does not check it first, so a duplicate fails as a 500. Deleting an
  integration deletes its `fromIntegration` engines (`sqlite.ts:566`).
* The instance default is server-settings key `search`
  (`v2.5.2:packages/server-settings/src/index.ts:155-157`), written through the
  unvalidated generic branch of `saveSettings`
  (`v2.5.2:packages/api/src/router/serverSettings.ts:178-182`; item 10). The
  per-user default is already on REST (`PATCH /api/users/search-preferences`,
  `v2.5.2:packages/api/src/router/user.ts:750-770`, and
  `GET`/`PATCH /api/users/preferences`, `user.ts:399-418`, `:674-704`) and
  managed by the user resources.

**Upstream must:** flatten the input to
`z.object({name, short, iconUrl, description: nullable, type: z.enum(searchEngineTypes), urlTemplate?: nullable, integrationId?: nullable}).superRefine(...)`,
requiring `integrationId` for `fromIntegration`; register `searchEngineRouter`
with tag `search-engines`; add `GET /api/search-engines` →
`[{id, name, short, iconUrl, description, type, urlTemplate, integrationId}]`,
`GET /api/search-engines/{id}`, `POST /api/search-engines` → `{id}` (409 for a
duplicate `short`), `PATCH /api/search-engines/{id}` (renaming `short` with a
uniqueness check, or keeping it immutable), `DELETE /api/search-engines/{id}`,
and `GET`/`PATCH /api/settings/search` with `{defaultSearchEngineId: string|null}`,
checked for existence.

**The provider would add:** `homarr_search_engine` (`short` forces replacement
unless renaming is allowed, normalised to lowercase; read with `GET {id}`), a
`default_search_engine_id` on a server-settings resource (item 10), and a
`homarr_search_engines` data source.

**Priority and effort:** medium. S. **Existing upstream work:** none;
[#7174](https://github.com/homarr-labs/homarr/pull/7174) leaves search engines
out.

## 8. API keys

**Terraform users want** `homarr_api_key` (computed `id`, sensitive `key` as
`<id>.<token>`, owner `user_id`) to issue and rotate keys for other tools, and a
`homarr_api_keys` data source.

**Homarr has today:** `v2.5.2:packages/api/src/router/apiKeys.ts`, registered
for tRPC only (`v2.5.2:packages/api/src/root.ts:25`), with no `openapi` meta or
output schema:

* `getAll` (41-60, admin) returns every key, manual and OAuth, as `id`, the
  OAuth columns and `user{id,name,image,email}`, never the hash.
* `create` (61-81, admin) takes **no input**: the key always belongs to the
  caller, with no name and no expiry, and `{apiKey}` is returned once.
* `delete` (82-93, admin, `{apiKeyId}`) deletes any key, OAuth keys included,
  and succeeds silently for an unknown id.
* Since 2.5.0 (#7146), MCP OAuth access tokens are rows of the same table, with
  `oauthClientId`, `oauthClientName`, `oauthRedirectUri` and `oauthIssuedAt`
  (`v2.5.2:packages/db/schema/sqlite.ts:38-50`), inserted by the token grant
  (`v2.5.2:apps/nextjs/src/app/api/mcp/oauth/_grant.ts:270-281`); a manual key
  has `oauthClientId = null`. Any signed-in user can list and revoke their own
  OAuth keys with `getOwnOAuth` and `revokeOwnOAuth` (`apiKeys.ts:21-40`).
* The table has no name, expiry, or creation time for manual keys, and a user's
  keys are deleted with the user (`sqlite.ts:45-49`).
* A key authenticates as its owner, with all of the owner's permissions
  (`v2.5.2:packages/auth/api-key/get-api-key-session.ts:40-75`,
  `v2.5.2:packages/auth/callbacks.ts:23-36`), so every key `create` mints
  today is an admin's key.

**Upstream must** register `apiKeysRouter` with tag `api-keys` and add
`GET /api/api-keys` →
`[{id, userId, user{id,name}, oauthClientId|null, oauthClientName|null, oauthIssuedAt|null}]`
(never the hash), `GET /api/api-keys/{id}` (404 when missing),
`POST /api/api-keys` with `{userId?, name?, expiresAt?}` → `{id, apiKey}` (a
`userId` other than the caller's needs admin; `name` and `expiresAt` need a
migration, and `expiresAt` a check in `getSessionFromApiKeyAsync`), and
`DELETE /api/api-keys/{id}`, answering 404 for an unknown id. Returning the token
once is acceptable, as for invites.

**The provider would add:** `homarr_api_key` — create keeps the key in state,
read with `GET` (the key stays from state and is `null` after import), every
attribute forces replacement, delete; it would refuse to delete the key the
provider itself authenticates with, as it already refuses for that key's user —
and a `homarr_api_keys` data source that separates manual keys from OAuth keys
by `oauthClientId`.

**Priority and effort:** medium. S, or M with name and expiry.

**Existing upstream work:** none.
[#7174](https://github.com/homarr-labs/homarr/pull/7174) does not touch API keys.

## 9. Media and icons

**Terraform users want** `homarr_media` to upload a local file and get
`/api/user-medias/{id}` back for app and board icons, and a `homarr_icon` data
source to find an icon URL by name for `homarr_app.icon_url`.

**Homarr has today:**

* `v2.5.2:packages/api/src/router/medias/media-router.ts`, registered for tRPC
  only (`v2.5.2:packages/api/src/root.ts:26`), with no `openapi` meta:
  `getPaginated` (15-54) takes an intersection `paginatedSchema.and(...)` and
  lists the caller's own media unless the caller has `media-view-all` and sets
  `includeFromAllUsers` (`media-router.ts:22`); `uploadMedia` (55-100,
  `media-upload`) takes **multipart** `zod-form-data`
  (`v2.5.2:packages/validation/src/media.ts:6-39`:
  png, jpeg, webp, gif or svg up to 32 MiB), returns `ids[]`, and registers each
  upload as a local icon with an md5 checksum (`media-router.ts:78-97`;
  `v2.5.2:packages/icons/src/repositories/local.icon-repository.ts:26-36`);
  `deleteMedia` (101-127) needs the creator or `media-full-all`, answers
  NOT_FOUND for an unknown id and also removes the icon row.
* No read of one media's metadata. The Next.js route `GET /api/user-medias/{id}`,
  outside the OpenAPI document, serves only the bytes, with a SHA-256 `ETag` of
  what it serves; SVGs are sanitised first
  (`v2.5.2:apps/nextjs/src/app/api/user-medias/[id]/route.ts:51-94`, `:65-79`).
* `icons.findIcons` (`v2.5.2:packages/api/src/router/icons.ts:8-58`) is public,
  with MCP meta only and no output schema. It lists the local repository first,
  prefers SVGs (`:21-28`, `:43-48`) and returns
  `{icons: [{id, slug, icons[{id, name, url}]}], countIcons}`. Its input is
  already flat (`searchText?`, `limitPerGroup` 1–500, default 12;
  `v2.5.2:packages/validation/src/icons.ts:3-6`).

**Upstream must:** add `POST /api/medias` with JSON
`{name, contentType, contentBase64}` → `{id, url, md5}` (no multipart, point 6
of section 0), keeping the type and size checks; `GET /api/medias/{id}` →
`{id, name, contentType, size, createdAt, creatorId, md5}`; `GET /api/medias`
with flat query fields `search`, `includeFromAllUsers`, `page` and `pageSize`
instead of the intersection; `DELETE /api/medias/{id}`; `GET /api/icons` with
`searchText` and `limitPerGroup` and an output schema; and register
`mediaRouter` and `iconsRouter` in `open-api.ts`.

**The provider would add:** `homarr_media` (`source` or `content_base64`;
`content_md5` forces replacement; computed `url`; read with `GET {id}`, delete
with `DELETE`) and a `homarr_icon` data source (name → url, preferring SVG as
`findIcons` already does).

**Priority and effort:** medium for the icon lookup, low for uploads. M.

**Existing upstream work:** none; PR #7109 is SQL performance only, and
[#7174](https://github.com/homarr-labs/homarr/pull/7174) does not touch media or
icons.

## 10. Other server settings

**Terraform users want** a `homarr_server_settings` singleton with `analytics`,
`crawling_and_indexing`, `user`, `appearance`, `branding`, `culture` and
`search` blocks, or one resource per key.

**Homarr has today:** keys and defaults in
`v2.5.2:packages/server-settings/src/index.ts:5-14`, `:127-158`; the router is
`v2.5.2:packages/api/src/router/serverSettings.ts`.

| Key | Fields | Written today |
| --- | --- | --- |
| `analytics` | `enableGeneral`; `instanceId` and `lastSuccessfulSnapshotAt` are server-managed | strict schema (`serverSettings.ts:32`, `:173-177`) |
| `crawlingAndIndexing` | `noIndex`, `noFollow`, `noTranslate`, `noSiteLinksSearchBox` | **unvalidated** merge (`serverSettings.ts:178-182`) |
| `board` | `homeBoardId`, `mobileHomeBoardId`, `enableStatusByDefault`, `forceDisableStatus` | **already REST**, `GET`/`PATCH /api/settings/board` (`:97-142`), managed by `homarr_server_board_settings`; since 2.5.0 (#7146) `saveSettings` sends this key through the same check (`:152-155`) |
| `user` | `enableGravatar` | unvalidated |
| `appearance` | `defaultColorScheme` (`auto`, `light`, `dark`) | unvalidated |
| `branding` | `appName`, `greeting`, `logoImageUrl`, `faviconImageUrl`, `primaryColor`, `secondaryColor`, `lockPrimaryColor`, `signInBackgroundImageUrl`, `signInBackgroundOverlay`, `authBranding{…}`, `defaultRadius`, `customCss` | validated (`index.ts:42-55`; `serverSettings.ts:156-172`) |
| `culture` | `defaultLocale` | unvalidated; an unsupported locale is stored, and routing falls back to `en` (`v2.5.2:apps/nextjs/src/proxy.ts:62-67`) |
| `search` | `defaultSearchEngineId` | unvalidated; a deleted engine stays until the next default lookup clears it (item 7) |

Over tRPC: `serverSettings.getAll` (`serverSettings.ts:86-88`, admin),
`saveSettings` (`:143-183`, admin, `{settingsKey, value: record}`, merging any
field of any type into the five unvalidated keys), `getCulture` (`:83-85`,
public) and `getBranding` (`:89-96`, public, MCP meta).

Since 2.5.0 (#7146), `PATCH /api/settings/board` validates the merged result
rather than the input (`saveBoardSettingsAsync`, `:42-80`): both stored home
boards must be public. Onboarding writes its board, private when onboarding
creates it, into both slots
(`v2.5.2:packages/api/src/router/onboard/onboard-router.ts:376`, `:1003-1007`),
and `deleteBoard` leaves the slots as they are
(`v2.5.2:packages/api/src/router/board.ts:1117-1140`), so either one makes every
later change fail with 400, from REST and from the UI; see item 16. The board
input is not strict: unknown keys are dropped without an error
(`serverSettings.ts:24-31`).

[#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes, for the
`board` key only: `saveBoardSettingsAsync` checks only the home board ids that
change; `deleteBoard` clears the slots that point at the deleted board, in the
same transaction; onboarding stores its board only when it is public. The input,
output and status codes of `PATCH /api/settings/board` stay; the 400 "must
reference public boards" then concerns changed ids only. Separately, it stores
`pingEnabled` on new or changed app tiles, defaulting to
`enableStatusByDefault`, so a later change of that setting no longer reaches
them; today such tiles follow the setting when rendered
(`v2.5.2:packages/widgets/src/app/index.ts:88`; item 11).

**Upstream must** copy the `/api/settings/board` pattern for each key, with a
partial, strict input and a full output: `GET`/`PATCH /api/settings/analytics`
(only `enableGeneral` writable), `.../crawling-and-indexing`, `.../user`,
`.../appearance` (`defaultColorScheme` as an enum), `.../branding`,
`.../culture` (`defaultLocale` as an enum of supported languages) and
`.../search` (engine checked for existence, and cleared when the engine is
deleted, as #7174 does for home boards). Fix `saveSettings`, which persists any
field with any type for the unvalidated keys. `serverSettingsRouter` is already
registered (`v2.5.2:packages/api/src/open-api.ts:24`), so meta and output are
enough.

**The provider would add:** `homarr_server_settings` with optional nested blocks,
`null` meaning unmanaged as in `homarr_server_board_settings`, each key read with
its `GET`; delete forgets the resource or optionally resets defaults. Or one
resource per key (`homarr_server_branding`, …), plus matching data sources. With
#7174, `homarr_server_board_settings` would no longer need both home boards
managed together on the Homarr releases that carry it.

**Priority and effort:** medium (branding is the most requested). M.

**Existing upstream work:** `origin/feat/trpc-board-openapi`, merged as #6213,
set up the board-settings pattern only.
[#7174](https://github.com/homarr-labs/homarr/pull/7174) (open, not merged or
released) changes the `board` key as above (the home-board check, the clean-up
on board delete, onboarding's write) and makes the app tiles it completes stop
following `enableStatusByDefault`. Every other key is left as it is.

## 11. App tile options

**Terraform users want**, per tile of kind `app`: `app_id`, `open_in_new_tab`,
`show_title`, `description_display_mode`, `layout`, `ping_enabled` and the
placement.

**Homarr has today:** these are options of the `app` widget item
(`v2.5.2:packages/widgets/src/app/index.ts:20-98`), not fields of the app, whose
table holds only `id, name, description, iconUrl, href, pingUrl`
(`v2.5.2:packages/db/schema/sqlite.ts:432-439`): `appId` (default `""`,
`v2.5.2:packages/widgets/src/options.ts:313-317`); `openInNewTab` (default
`true`); `showTitle` (default `true`); `descriptionDisplayMode` (`normal`,
`tooltip` or `hidden`, default `hidden`); `layout` (`column`, `column-reverse`,
`row` or `row-reverse`, default `column`); `pingEnabled` (default: the server's
`enableStatusByDefault`, hidden when `forceDisableStatus` is set,
`index.ts:88-95`). They are written through `POST /api/boards/items` or
`PUT /content` and never read over REST. The server stores them as submitted,
checking neither values nor that the app exists
(`v2.5.2:packages/api/src/router/board.ts:156-211`); defaults are filled in only
when the UI renders a tile (`v2.5.2:packages/widgets/src/manifest.ts:144-155`,
called from `v2.5.2:apps/nextjs/src/components/board/items/item-content.tsx:203`,
`:475`). Stored options are therefore often partial: the board's "Add app"
action stores `{appId}`
(`v2.5.2:apps/nextjs/src/components/board/use-board-add-actions.ts:30`),
onboarding `{appId, openInNewTab, showTitle}`
(`v2.5.2:packages/api/src/router/onboard/onboard-router.ts:894`), and the seed
the same, with or without `pingEnabled: false`
(`v2.5.2:packages/db/migrations/seed.ts:933`,
`v2.5.2:packages/db/queries/widget-placement.ts:148`). A tile without
`pingEnabled` follows later changes to `enableStatusByDefault`. The tRPC-only
`widget.options.saveItemOptions` merges options, also unvalidated
(`v2.5.2:packages/api/src/router/widgets/options.ts:28-64`).

**Upstream must** do nothing beyond item [1](#1-board-content-sections-tiles-options).
Recommended: validate options server-side against the widget definition and
return them with defaults filled in, so REST round-trips are stable. The
definitions cannot simply be imported by `@homarr/api`: `@homarr/widgets`
depends on it (`v2.5.2:packages/widgets/package.json:48`) and they sit next to
React code. `origin/add-more-openapi:packages/api/src/router/item/item-router.ts:17-38`
imported them by relative path, with `enableStatusByDefault: true` hard-coded,
and filled defaults with `reduceWidgetOptionsWithDefaultValues` (`:105`,
`:145`).

**The provider would add** `homarr_app_tile`, a typed wrapper over
`homarr_board_item` with `kind = "app"`, `app_id` required, and the other five
options optional and computed: tiles written by onboarding, the seed or before
the server fills defaults keep partial options until they are next changed.

**Priority and effort:** medium. S on top of item 1.

**Existing upstream work:** [#7174](https://github.com/homarr-labs/homarr/pull/7174)
(open, not merged or released) proposes the recommended validation, for `app`
only. Decisions that matter to a REST client:

* The schema is a hand-kept copy, `widgetOptionsSchemas` in
  `@homarr/validation/widgets/options` (the pattern of
  `widgetIntegrationSupport`), checked against the real definition for both
  values of `enableStatusByDefault` by a test in
  `packages/widgets/src/manifest.spec.ts`. Every other kind, Custom Widgets
  included, is still stored as submitted.
* `normalizeWidgetOptionsAsync` runs on `POST /api/boards/items` and tRPC
  `saveItemOptions`; `normalizeChangedWidgetOptionsAsync` on `PUT /content` and
  the item `PATCH`. Missing or `null` options get their defaults, `pingEnabled`
  from `enableStatusByDefault` at write time. Unknown keys are dropped, not
  rejected; a strict object, which would turn typos into 400s, is left as an
  open question. Invalid values answer 400
  `Invalid options for app item <id>: <path>: <message>`, without the id on
  `POST /items`. `appId` still defaults to `""` and is not checked against
  existing apps.
* `PUT /content` and the item `PATCH` complete only items that are new or whose
  kind or options changed (deep equality), so unchanged legacy tiles keep their
  partial options; onboarding and the seed still write partial options, and
  `duplicateBoard` copies stored options as they are.
* Changed contract: tiles from the UI's "Add app" action store `pingEnabled`
  when the board is saved instead of following later changes to
  `enableStatusByDefault`; `forceDisableStatus` still overrides at render time.

## 12. Clearing a user's email

**Terraform users want** `homarr_user.email = null` to clear the email in place.
Setting and changing it in place already works in 0.3.1; removing it replaces the
user.

**Homarr has today:** `editProfile`, `PUT /api/users/profile`
(`v2.5.2:packages/api/src/router/user.ts:498-558`), rejects non-credentials users
(`:533-538`) and computes `emailDirty = input.email && user.email !== input.email`
(`:549-556`), so `null` — and `""`, which `nullableEmailSchema` turns into `null`
(`v2.5.2:packages/validation/src/email.ts:7-11`,
`v2.5.2:packages/validation/src/user.ts:122-126`) — never clears the email, and
the call still succeeds. `changePassword` (`user.ts:592-673`) also rejects
non-credentials users (`:639-644`); `setProfileImage` (`:225-279`) does not. The
identity provider overwrites some fields at sign-in: OIDC the name and, unless an
uploaded data URL is stored, the picture (`v2.5.2:packages/auth/events.ts:58-83`),
LDAP the name (`:49-56`).

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
output (`user.ts:283-293`), which would save the provider one
`GET /api/users/selectable` call per auth provider in the `homarr_user` and
`homarr_users` data sources.
[#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes both; see
existing upstream work.

**The provider would add:** removing `email` would send `email: null` and update
in place instead of replacing the user, gated on the Homarr version; the user
data sources would read `provider` from `GET /api/users`.

**Priority and effort:** low. S.

**Existing upstream work:** [#7174](https://github.com/homarr-labs/homarr/pull/7174)
(open, not merged or released) makes exactly this change to `editProfile`: an
omitted `email` is kept, `null` or `""` clears it, a different address replaces
it, and both reset `emailVerified`; the same address changes nothing. Input
schema, access rules and status codes stay, and the endpoint description now
documents omitted versus `null`, which the schema cannot express. A client that
sent `null` or `""` to mean "keep" would now clear the email; 0.3.1 omits a null
email and rejects `""` in configuration, so it is unaffected. The PR also adds
`provider` to `GET /api/users`. Nothing is left open but the release.

## 13. Certificates: delete, list, trusted hostnames

**Terraform users want** a `homarr_root_certificate` resource with a real
delete, a `homarr_root_certificates` list data source, and
`homarr_trusted_hostname` (hostname + PEM → thumbprint).

**Homarr has today:** `v2.5.2:packages/api/src/router/certificates/certificate-router.ts`,
admin only.

* **REST:** `GET /api/certificates/{fileName}` (`certificate-router.ts:38-53`)
  returns metadata or `null` — the provider's `homarr_root_certificate` data
  source — and answers 400 for a file that is not a single self-signed root CA
  (`v2.5.2:packages/core/src/infrastructure/certificates/files/root-certificate.ts:29-53`,
  `:110-117`). `POST /api/certificates/root` (`certificate-router.ts:55-80`) is an
  idempotent ensure that answers CONFLICT when different bytes exist
  (`root-certificate.ts:129-135`).
* **tRPC only** (`certificate-router.ts`): `addCertificate` (`:85-116`,
  multipart, any certificate); `trustHostnameMismatch` (`:117-150`,
  `{hostname, certificate}`, stores the `fingerprint256` thumbprint);
  `removeTrustedHostname` (`:151-173`, `{hostname, thumbprint}`);
  `removeCertificate` (`:174-195`, `{fileName}`, also deletes the certificate's
  hostnames).
* **Not REST-ready as they are.** `trustHostnameMismatch` inserts without a
  conflict check (`certificate-router.ts:138-142`), so repeating it fails on the
  `(hostname, thumbprint)` primary key of `trusted_certificate_hostname` with a
  500 (`v2.5.2:packages/core/src/infrastructure/certificates/hostnames/db/sqlite.ts:3-15`).
  `removeCertificate` on a missing file throws a plain `Error`, also a 500
  (`v2.5.2:packages/core/src/infrastructure/certificates/files/index.ts:45-48`).
  `addCertificate` overwrites a file of the same name (`files/index.ts:73`), so
  the UI can replace what the create-only REST call refuses to.
* **No list procedure at all.** The UI pages call core helpers directly
  (`v2.5.2:apps/nextjs/src/app/[locale]/manage/tools/certificates/page.tsx:33`,
  `.../certificates/hostnames/page.tsx:35`; `files/index.ts:12-32`;
  `v2.5.2:packages/core/src/infrastructure/certificates/hostnames/index.ts:8-10`).
  The folder can also hold chains and leaf certificates added through the UI,
  which the metadata read rejects (`root-certificate.ts:41-53`).

**Upstream must** extend `v2.5.2:packages/api/src/open-api.ts:26-29` with
`DELETE /api/certificates/{fileName}` → `{deleted}` (no 500 for a missing file),
`GET /api/certificates` → `rootCertificateMetadataSchema[]`
(`v2.5.2:packages/validation/src/certificates.ts:12-19`) that skips or flags
files that are not a single root CA instead of failing,
`GET /api/certificates/hostnames` → `[{hostname, thumbprint}]`,
`POST /api/certificates/hostnames` with `{hostname, certificate}` →
`{hostname, thumbprint}` (idempotent), and `DELETE /api/certificates/hostnames`
with `{hostname, thumbprint}` in the query, because the thumbprint contains
colons. Policy blocker: #7033 shipped the REST surface as create-only, and the
API documentation promises CONFLICT "without replacement"
(`v2.5.2:apps/docs/docs/management/api/index.mdx:75-76`); a REST delete needs a
maintainer decision.

**The provider would add:** a `homarr_root_certificate` resource (`POST`, `GET`,
real `DELETE`; the content forces replacement), `homarr_trusted_hostname`
(create `POST`, read from the list, delete `DELETE`), and list data sources for
both.

**Priority and effort:** medium. S.

**Existing upstream work:** #7033 (a17845aa1, 2026-10-05, released in 2.2.0),
create-only. [#7174](https://github.com/homarr-labs/homarr/pull/7174) leaves the
`certificates` block of `open-api.ts` as it is.

## 14. Docker and Kubernetes

**Terraform users want** nothing declarative here.

**Homarr has today:** `v2.5.2:packages/api/src/router/docker/docker-router.ts:31-246`,
admin only, MCP meta only: `reconcileServices` 32-42, `getServiceHealth` 43-53,
`refreshInventory` 54-68, `getContainers` 69-91, `getEndpoints` 92-101,
`startAll`/`stopAll`/`restartAll`/`removeAll` 102-158, `logs` 159-189 and
`subscribeLogs` 190-245. Docker endpoints come from environment variables —
`DOCKER_ENDPOINTS` (JSON descriptors), else `DOCKER_SOCKET_PATHS` and
`DOCKER_HOSTNAMES`/`DOCKER_PORTS`, else `/var/run/docker.sock`
(`v2.5.2:packages/docker/src/singleton.ts:52-100`,
`v2.5.2:packages/docker/src/env.ts:7-15`) — not from the database. The
Kubernetes router (`v2.5.2:packages/api/src/router/kubernetes/router/kubernetes-router.ts:5-16`)
offers read-only cluster views: every procedure is an admin query (for example
`kubernetes/router/pods.ts:15-27`), unavailable unless `ENABLE_KUBERNETES` is
set (`v2.5.2:packages/api/src/middlewares/kubernetes.ts:7-17`), with the cluster
taken from `KUBECONFIG` or the in-cluster service account
(`kubernetes/kubernetes-client.ts:164-172`).

**Upstream must** do nothing. An optional read-only `GET /api/docker/endpoints`
(`getEndpoints` with an output schema) would allow a data source; container
actions are imperative.

**The provider would add** nothing; configure this in the deployment, for
example with the docker or helm provider.

**Priority and effort:** low. Nothing to build.

**Existing upstream work:** none.
[#7174](https://github.com/homarr-labs/homarr/pull/7174) does not touch either
router.

## 15. Other configuration-like routers

All low or medium priority.
[#7174](https://github.com/homarr-labs/homarr/pull/7174) touches only the users
point at the end; the rest has no upstream work.

* **Cron jobs** (medium, S). `v2.5.2:packages/api/src/router/cron-jobs.ts:17-83`,
  admin, no `openapi` meta: `triggerJob`, `startJob`, `stopJob`, `disableJob`
  and `enableJob` take a bare `z.enum` of job names, not an object (point 4 of
  the checklist in section 0); `updateJobInterval` (`:36-46`) takes
  `{name, cron: z.string()}`; `getJobs` (`:59-61`) returns `name`, `cron`,
  `preventManualExecution`, `preventCustomInterval` and `isEnabled` for the jobs
  that are not hidden (`v2.5.2:packages/cron-jobs/src/job-manager.ts:114-137`),
  persisted in
  `cron_job_configuration` (`v2.5.2:packages/db/schema/sqlite.ts:690-694`).
  `updateIntervalAsync` refuses a custom expression for a
  `preventCustomInterval` job with a plain `Error`, a 500
  (`job-manager.ts:30-32`), and stores the expression and stops the task before
  `new Cron` parses it (`:34-48`), so an invalid expression leaves the job
  stopped, and `createTask` throws on it again at the next start
  (`v2.5.2:packages/cron-jobs-core/src/creator.ts:77-90`). Upstream:
  `GET /api/cron-jobs`, `GET /api/cron-jobs/{name}`,
  `PATCH /api/cron-jobs/{name}` with `{cron?, isEnabled?}` — parsing the
  expression before writing, and answering 400 for it and for
  `preventCustomInterval` — and optionally `DELETE /api/cron-jobs/{name}` to
  reset to the default. Provider: `homarr_cron_job` with `cron` optional and
  computed; delete resets the job.
* **Assistant (AI provider) configuration** (low, S).
  `v2.5.2:packages/api/src/router/assistant.ts:631-777`, admin:
  `getAdminConfiguration` (`:631-645`) reports secrets only as
  `apiKeyConfigured` and `customHeadersConfigured`; `updateConnection`
  (`:647-719`) takes `{provider, baseUrl, modelDiscoveryPath, apiKey?,
  clearApiKey, customHeaders?, clearCustomHeaders}`, drops stored credentials
  that are not sent again when the provider or base URL changes, and resets
  `modelId` and `enabled` when the connection changes (`:664-695`);
  `clearCredentials` (`:721-734`); `updateConfiguration` (`:744-777`) takes
  `{enabled, modelId, webSearchEnabled}` and may store a resolved model id
  instead of the one sent (`:767-772`). Since 2.5.0 (#7135) the migration seed
  inserts an enabled `homarr`-provider row when none exists
  (`v2.5.2:packages/db/migrations/seed.ts:77-96`), so the singleton always
  exists. Upstream: `GET`/`PUT /api/settings/assistant` taking connection and
  configuration in one body, with a write-only `apiKey` and `customHeaders`, and
  returning the stored `modelId`. Provider: a `homarr_assistant_settings`
  singleton with a write-only `api_key`.
* **Custom widgets** (low, L). Admin only:
  `v2.5.2:packages/api/src/router/custom-widget/custom-widget-router.ts:37-129`
  (`update` 42-69, `toggleEnabled` 74-95, `delete` 97-104, `duplicate` 106-122),
  `custom-widget/management-queries.ts:31-49` (`list`) and `:78-102` (`get`,
  secrets only as `hasValue`), `custom-widget/creation-procedures.ts:33-52`
  (`create`), `custom-widget/transfer-procedures.ts:21-30` (`export`) and
  `:58-67` (`import`), `custom-widget/secret-procedures.ts:60-79` (`secretSet`)
  and `:222-234` (`secretClear`); tables `db/schema/sqlite.ts:629-669`. `export`
  strips integration bindings
  (`v2.5.2:packages/custom-widgets/src/core/import.ts:181-192`), so `get`, not
  `export`, is the faithful read. The definition JSON Schema is served at
  `GET /api/custom-widgets/schema`, but only to a signed-in admin session, not
  to API keys
  (`v2.5.2:apps/nextjs/src/app/api/custom-widgets/[resource]/route.ts:19-56`,
  `.../custom-widgets/admin.ts:14-25`). Upstream: REST CRUD with the `import`
  input (`customWidgetDefinitionSchema`,
  `v2.5.2:packages/custom-widgets/src/core/custom-jsx-schema.ts:333`) as the
  body and `get` as the read, plus write-only secrets. Provider:
  `homarr_custom_widget` (`definition_json`, `enabled`, write-only `secrets`).
* **Releases-widget tokens** (low, S once item endpoints exist).
  `v2.5.2:packages/api/src/router/widgets/widget-secrets.ts:21-97`: `setSecret`
  (`:22-55`, replaces), `deleteSecret` (`:57-72`), `getConfiguredKinds`
  (`:74-96`), for items of kind `releases` on a board the caller may modify
  (`:14-19`); the kinds are the release providers
  (`v2.5.2:packages/definitions/src/release-provider.ts:46`). Tokens are stored
  encrypted in `widget_secret`, keyed `(itemId, kind)` and deleted with the item
  (`db/schema/sqlite.ts:671-688`). Upstream:
  `PUT`/`DELETE /api/boards/{boardId}/items/{itemId}/secrets/{kind}` and
  `GET .../secrets` returning the configured kinds, next to the item endpoints
  proposed in [#7174](https://github.com/homarr-labs/homarr/pull/7174) (item 1),
  whose item `DELETE` already removes the tokens through that cascade. Provider:
  write-only `secrets` on `homarr_board_item`.
* **Onboarding and bootstrap** (medium, M). The first admin is created by
  `user.initUser` (`v2.5.2:packages/api/src/router/user.ts:70-129`) behind an
  onboarding claim cookie from `POST /api/onboarding/claim`
  (`v2.5.2:apps/nextjs/src/app/api/onboarding/claim/route.ts:13-53`, checked at
  `v2.5.2:packages/api/src/trpc.ts:137-146`). Claim-only access ends once a user
  exists (`v2.5.2:packages/api/src/onboarding-claim.ts:69-76`), so
  `group.createInitialExternalGroup`
  (`v2.5.2:packages/api/src/router/group.ts:178-223`, only with LDAP or OIDC),
  `onboard.completeSetup` (`onboard/onboard-router.ts:318`) and the first
  `apiKeys.create` (item 8) need a signed-in session. None of this is REST; the
  provider's `make homarr-up` drives it over tRPC. Since 2.5.0 (#7146)
  `initUser` moves the onboarding step only away from `user`, in the same
  transaction, and answers CONFLICT on a repeat (`user.ts:95-128`). Upstream:
  an env- or file-based bootstrap that seeds the admin and an API-key hash on
  first start — unauthenticated REST endpoints would be a security risk.
  Provider: start from a fresh instance with a known key.
* **Users, small items.** `POST /api/users` returns nothing (`user.ts:214`) and
  writes `groupIds` after the user, outside a transaction (`:219-223`).
  [#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes returning
  `{id}`, with the group ids validated first and the user and its memberships
  inserted in one transaction (item 5); that would let the provider drop its id
  recovery. Disabling a user without deleting it exists only in the unmerged
  `origin/bot/issue-4455-disable-users` (4ff2c9a7f, 2026-07-06); merged, it
  would become a `disabled` attribute on `homarr_user`.

## 16. Board lifecycle details

**Terraform users want** board create, copy, rename, visibility and delete to
fail cleanly and leave no dangling references. The provider works around each
gap below today, at the cost of extra requests and some guesswork.

**Homarr has today:**

* **Create checks no name.** `createBoard`
  (`v2.5.2:packages/api/src/router/board.ts:667-745`) never calls
  `noBoardWithSimilarNameAsync` (`board.ts:2563-2581`), which `exists` (`:288`),
  `duplicateBoard` (`:769`) and `renameBoard` (`:1075`) do. The database
  constraint is case-sensitive (`v2.5.2:packages/db/schema/sqlite.ts:269`,
  `v2.5.2:packages/db/schema/postgresql.ts:284`), so an exact duplicate fails
  with a driver error that reaches REST as HTTP 500
  (`t2o:adapters/node-http/errors.mjs:70-89`, `t2o:adapters/node-http/core.mjs:152`),
  and a case variant is created although the board's URL then reaches only one
  of the two (`getBoardByName` matches on `UPPER(name)`, `board.ts:1220`).
* **"No full access" looks like "missing".** Rename, visibility and delete need
  full access (`board.ts:1073`, `:1099`, `:1137`), and
  `v2.5.2:packages/api/src/router/board/board-access.ts:63-75` answers NOT_FOUND
  to prevent snooping — even to a caller who can list and view the board
  (`v2.5.2:packages/auth/permissions/board-permissions.ts:40-45`). Modify-level
  calls such as settings, layouts and content (`board.ts:1245`, `:1303`,
  `:1723`) answer a view-only caller the same way.
* **Delete leaves the instance home boards dangling.** `deleteBoard`
  (`board.ts:1117-1140`) deletes only the board row (`:1139`). User and group
  home boards are foreign keys with `ON DELETE SET NULL` (`sqlite.ts:60-65`,
  `:157-162`), but the server setting `board` is a SuperJSON value
  (`sqlite.ts:473-476`) and keeps `homeBoardId` and `mobileHomeBoardId`;
  visitors who fall back to it (`board.ts:2515-2561`) get 404. Onboarding's
  `completeSetup` also writes its target board into both slots whatever its
  visibility, and a board it creates itself is private
  (`v2.5.2:packages/api/src/router/onboard/onboard-router.ts:329-333`, `:376`,
  `:1003-1007`). Since 2.5.0 (#7146), `PATCH /api/settings/board` and the UI's
  `saveSettings` validate the merged result and require both stored ids to name
  public boards (`v2.5.2:packages/api/src/router/serverSettings.ts:42-80`,
  `:140-142`, `:152-155`), so either case blocks every later settings change,
  even one to `enableStatusByDefault` alone, until the stale slots are replaced
  or cleared.
* **A copy forgets its source.** `duplicateBoard` returns `{boardId}` only
  (`board.ts:766`, `:1052`), and the `board` table has no column linking a copy
  to its source (`sqlite.ts:267-292`).

**Upstream must:** call `noBoardWithSimilarNameAsync` in `createBoard`, answering
409 CONFLICT like rename and duplicate; answer 403 FORBIDDEN when the caller can
view a board but lacks the access level an operation needs, keeping 404 for
boards it cannot see; clear `homeBoardId` and `mobileHomeBoardId` in the server
settings when `deleteBoard` removes the board they point at, let onboarding
pick a public board or none, and stop an id already stored from blocking
unrelated settings changes; return the layout ids from `duplicateBoard` (see
item 2) and store a `sourceBoardId`, returned by `GET /api/boards`.
[#7174](https://github.com/homarr-labs/homarr/pull/7174) proposes all of this;
see existing upstream work.

**The provider would drop** its pre-create name listing, the HTTP 500 mapping,
the re-list after every 404 on rename, visibility and delete, and the destroy
warning about dangling home boards with the matching caveats on
`homarr_server_board_settings`; and **add** `base_layout_id` and
`mobile_layout_id` on `homarr_board_copy`, `source_board_id` on the import of a
copy, and a computed `source_board_id` on the `homarr_boards` data source. As
#7174 proposes it, a `source_board_id` read back is computed-only: `null` also
means the source was deleted or is hidden from the key, so it never counts as
drift and never forces a new copy. The provider keeps serialising board names
within a run, because the server check is still check-then-insert. All of it is
gated on a Homarr release that ships the change.

**Priority and effort:** medium. S.

**Existing upstream work:** since 2.5.0, #7146 has made the dangling home boards
pressing. [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open, not
merged or released) proposes every point above. Decisions that matter to a REST
client:

* `createBoard` runs `noBoardWithSimilarNameAsync` first and answers 409 "Board
  with similar name already exists".
* `throwIfActionForbiddenAsync` answers 403 "Permission denied" when the caller
  can see the board (view or change access) but lacks modify or full access; 404
  stays for missing or invisible boards, and view checks never answer 403. The
  change sits in the shared helper, so it applies to every modify and full
  check, tRPC-only widget procedures included.
* `deleteBoard` clears the instance slots that point at the board in the same
  transaction as the delete, reading the setting inside it (`SELECT … FOR
  UPDATE` on PostgreSQL); the other slot and settings stay as stored.
  Onboarding sets the instance slots only to a public target board and
  otherwise keeps the stored values. Board-settings saves validate only the
  home-board ids that change, so an instance already holding a deleted or
  private id can save again; a new private or missing id is still refused, and
  `null` always clears.
* `duplicateBoard` returns `{boardId, baseLayoutId, mobileLayoutId}` and stores
  `sourceBoardId`, a nullable self-reference with `ON DELETE SET NULL`
  (migrations SQLite 0050, PostgreSQL 0019). It names the direct source, so a
  copy of a copy points at the intermediate copy. `GET /api/boards` returns it
  only when the caller can access the source too; it is `null` otherwise, for
  boards not made by copying, for copies made before the migration and once the
  source is deleted. Full-board reads (`getBoardByName`, `getHomeBoard`) leave
  it out.

It leaves open the name race: like rename and duplicate, create checks and then
inserts without a transaction, so two concurrent creates can still both pass the
check (bug 17). Onboarding still makes its board the `everyone` group's home
board (bug 18), ids stored before the fix stay until replaced, and the delete's
OpenAPI description does not mention the clean-up.

## Upstream bugs found along the way

Worth filing independently of the provider:

1. **Clearing an email is impossible.** `editProfile` ignores `null` and `""`
   (`v2.5.2:packages/api/src/router/user.ts:549-555`). Fix proposed in
   [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open).
2. **Integration update weakens URL validation.** It accepts non-http(s) URLs
   (`v2.5.2:packages/validation/src/integration.ts:35`), while create forbids
   them (`:11-14`). Fix proposed in
   [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open).
3. **Integration create and update report failure as success.** They return
   `{error}` with HTTP 200
   (`v2.5.2:packages/api/src/router/integration/integration-router.ts:265-270`,
   `:349-354`). Fix proposed in
   [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open).
4. **Dead input.** `attemptSearchEngineCreation` is required but never read
   (`validation/integration.ts:22`);
   `v2.5.2:packages/api/src/router/test/integration/integration-router.spec.ts:507-543`
   asserts that no engine is created. Fix proposed in
   [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open).
5. **Unvalidated settings writes.** `serverSettings.saveSettings` persists
   arbitrary keys and types for `crawlingAndIndexing`, `user`, `appearance`,
   `culture` and `search`
   (`v2.5.2:packages/api/src/router/serverSettings.ts:145-150`, `:178-182`).
   Only `board` (validated since 2.5.0, #7146; `:152-155`), `branding` and
   `analytics` (`:156-177`) are parsed.
6. **Invalid cron expressions are stored.** The expression is persisted before
   it is validated, and the job is left stopped
   (`v2.5.2:packages/cron-jobs/src/job-manager.ts:34-48`).
7. **Group home-board fallback is dead code.** `getHomeIdBoardAsync` requires
   both `groups[settingKey]` and `groups.homeBoardId` to be non-null
   (`v2.5.2:packages/api/src/router/board.ts:2541-2542`), so on a mobile device
   a group with only a mobile or only a desktop home board is skipped, and the
   desktop fallback at `:2550` never runs.
8. **Partial position lists.** `group.savePositions` accepts a partial list and
   leaves duplicate positions
   (`v2.5.2:packages/api/src/router/group.ts:272-294`). Fix proposed in
   [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open).
9. **Search engine without integration.** A `fromIntegration` engine may be
   saved without `integrationId`
   (`v2.5.2:packages/validation/src/search-engine.ts:11-14`), yet `byId` asserts
   it exists
   (`v2.5.2:packages/api/src/router/search-engine/search-engine-router.ts:59-65`).
10. **Password confirmation is not checked by the server.**
    `convertIntersectionToZodObject` drops refinements
    (`v2.5.2:packages/api/src/schema-merger.ts:4-21`), so neither
    `POST /api/users` nor `PATCH /api/users/{userId}/changePassword` compares
    `confirmPassword` with `password` (`user.ts:213`, `:593`;
    `v2.5.2:packages/validation/src/user.ts:64-76`, `:85-87`, `:136-140`).
11. **SQLite-only log field.** `removeTrustedHostname` logs `dbResult.changes`
    (`v2.5.2:packages/api/src/router/certificates/certificate-router.ts:159-171`),
    which is undefined on PostgreSQL.
12. **REST board create returns 500 for a taken name and accepts case variants**
    (item 16; `board.ts:667-745`, `v2.5.2:packages/db/schema/sqlite.ts:269`).
    Fix proposed in [#7174](https://github.com/homarr-labs/homarr/pull/7174)
    (open). Concurrent requests can still race past the check (bug 17).
13. **User create is not atomic.** Group memberships are inserted after the user
    and outside a transaction, so a duplicate or unknown group id fails the
    request with a 500 while the user stays created
    (`v2.5.2:packages/api/src/router/user.ts:219-223`). Fix proposed in
    [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open).
14. **Deleting a board leaves the instance home boards dangling**, and since
    2.5.0 (#7146) every later save of the board settings that keeps the dangling
    id is refused with 400 (item 16; `board.ts:1117-1140`,
    `serverSettings.ts:48-69`). Fix proposed in
    [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open). The delete
    clears the matching slots in its transaction, and saves validate only the
    home-board ids that change.
15. **On SQLite, deleting an app linked to an integration fails.**
    `v2.5.2:packages/db/migrations/sqlite/0034_add_app_reference_to_integration.sql:1`
    adds `integration.app_id REFERENCES app(id)` without the
    `ON DELETE set null` that the schema declares (`db/schema/sqlite.ts:203`)
    and PostgreSQL has
    (`v2.5.2:packages/db/migrations/postgresql/0002_add_app_reference_to_integration.sql:2`).
    drizzle-kit leaves `ON DELETE` out of SQLite `ALTER TABLE … ADD`, and no
    later migration rebuilds `integration` the way
    `0023_fix_on_delete_actions.sql` rebuilt `user`. The connection keeps
    better-sqlite3's default of enforced foreign keys
    (`v2.5.2:packages/core/src/infrastructure/db/drivers/sqlite.ts:7-10`), so
    `DELETE /api/apps/{id}` (`v2.5.2:packages/api/src/router/app.ts:305-311`)
    fails with `FOREIGN KEY constraint failed`, HTTP 500, instead of unlinking.
    Not fixed by [#7174](https://github.com/homarr-labs/homarr/pull/7174), which
    adds the clause by hand only to its own new migration.
16. **Integration permission saves answer 500 for bad principals.**
    `saveUserIntegrationPermissions` and `saveGroupIntegrationPermissions`
    (`integration-router.ts:516-561`, `:562-607`) insert the list as given, so
    an unknown user or group id (foreign key) or a principal listed twice with
    the same permission (primary key, `db/schema/sqlite.ts:231-265`) rolls the
    replace back with a driver error, HTTP 500 (item 6; boards behave the same,
    item 3). Fix proposed in
    [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open). Repeated
    principal ids then answer 400 and unknown ones 404.
17. **Board names are checked, then written.** `noBoardWithSimilarNameAsync`
    (`board.ts:2563-2581`) reads all names outside any transaction before
    `duplicateBoard` (`:769`, insert `:970-1050`) and `renameBoard`
    (`:1075-1077`) write, and the unique index is case-sensitive
    (`db/schema/sqlite.ts:269`, `v2.5.2:packages/db/schema/postgresql.ts:284`).
    Two concurrent requests for `Media` and `media` both succeed; for the exact
    same name the second fails with a driver error, HTTP 500. Not fixed by
    [#7174](https://github.com/homarr-labs/homarr/pull/7174), which puts the
    same check in front of `createBoard`.
18. **The `everyone` group's home board is private.** Onboarding sets both
    home boards of the `everyone` group to its target board
    (`v2.5.2:packages/api/src/router/onboard/onboard-router.ts:1094-1097`,
    `:1183-1187`). On a fresh install that is the seeded `dashboard`, which the
    seed creates private and without a creator and already sets as that group's
    desktop home board (`v2.5.2:packages/db/migrations/seed.ts:488-501`,
    `:528-533`); a board that onboarding creates itself is private too
    (`onboard-router.ts:337-344`, `:1048`; `db/schema/sqlite.ts:270`). No board
    permission is granted, so a signed-in user without `board-view-all` and
    without an own or other group home board resolves to it
    (`board.ts:2552-2557`), and `getHomeBoard` answers NOT_FOUND (`:1187-1206`).
    Not fixed by [#7174](https://github.com/homarr-labs/homarr/pull/7174), which
    only keeps a private board out of the instance home boards.
19. **Integration router specs fail under `CI=true`.** `bun run test` sets it
    (`v2.5.2:package.json:302`), `createRedisConnection` then returns `null`,
    and since #6545 `requireRedisConnection` throws on that
    (`v2.5.2:packages/redis/src/lib/connection.ts:10-21`). `update` and `delete`
    call `invalidateIntegrationCacheAsync` (`integration-router.ts:414`, `:442`;
    `v2.5.2:packages/redis/src/lib/channel.ts:622-628`), and the spec does not
    mock `@homarr/redis`, so both success tests fail
    (`integration-router.spec.ts:697`, `:824`). Fix proposed in
    [#7174](https://github.com/homarr-labs/homarr/pull/7174) (open), which mocks
    the call in the spec.
