# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Rankanything is a server-rendered tier-list builder: create a ranking, add items, drag them into tiers, share it. Anonymous visitors get a working board immediately (an unclaimed "draft"); signing in claims it. Go 1.26 + htmx + Tailwind 4 + Postgres 18, shipped as a single binary with templates and static assets embedded (`assets/assets.go`).

## Commands

```bash
make dev            # air (Go hot reload) + tailwind --watch, together
make test           # rtk go test ./... -race, with TEST_DATABASE_URL exported
make migrate        # goose up against $DATABASE_URL
make migrate-down   # goose down one step
make sqlc           # regenerate internal/db from db/queries + db/migrations
make css            # one-shot minified tailwind build
make tidy

go run ./cmd/rankanything                    # run the server (make run is stale: it points at a nonexistent ./main.go)
go test ./internal/app -run TestHomeCreatesAnonymousDraft -race
npx prettier --write 'assets/templates/**/*.html'   # go-template-aware formatting
```

`.env` (gitignored) supplies `DATABASE_URL`; `config.Load` hard-fails if `.env` is missing or the URL is empty. `PORT` defaults to `:8001`, `APP_ENV` to `development` (production flips the Secure cookie flag). `make seed` references a `./cmd/seed` that does not exist.

## Architecture

**Request path:** `cmd/rankanything/main.go` builds the `app.App` struct (pool, queries, sessions, renderer, services) and hands `app.Routes()` to the server. Everything flows through that one struct — there is no DI container or global state.

**Layering is strict.** `internal/app` knows HTTP and templates; `internal/services` knows domain state and never imports `net/http`; `internal/db` is sqlc-generated and must not be hand-edited. Handlers translate form values into a per-operation `services.XxxRequest` struct and translate the result into a view struct from `internal/app/view.go`. When adding a service method, follow the `XxxRequest` struct convention rather than long parameter lists.

**Data layer:** `db/queries/*.sql` is the source of truth. Edit the SQL, run `make sqlc`. Schema changes are goose migrations in `db/migrations` (`-- +goose Up` / `Down`). UUIDs map to `github.com/google/uuid`, nullable columns to pointers (`emit_pointers_for_null_types`).

**Rendering (`internal/render`):** all templates are parsed once into one `template.Template`. `Page()` renders `layout.html`, which dispatches to the requested page via the `renderDynamic` func; `Partial()` renders a single named template with no layout — this is what htmx swaps in. Templates are addressed by their full path name (`"partials/tier_row.html"`), since `New(...).ParseFS` names them that way. `dict` builds ad-hoc maps for passing multiple values into a partial. `render.IsHTMXRequest(r)` distinguishes a fragment request from a full page load.

**htmx conventions:** the layout sets `hx-headers` with the session CSRF token on `<body>`, so every htmx-issued mutation carries `X-CSRF-Token` automatically. Mutating handlers respond with the smallest partial that covers the change (`partials/item_card.html`, `partials/tier_row.html`, a label swap for inline tier editing) or `Render.Empty` for deletions; out-of-band swaps are used to remove a row. Adding an endpoint usually means adding a partial, not a page.

**Auth and ownership (`internal/auth`):** sessions are scs backed by pgxstore. The session holds `user_id`, a list of draft slugs this browser created, a CSRF token, and a one-shot flash. A ranking with `user_id IS NULL` is a draft, reachable only by a session that lists its slug; an owned ranking is reachable only by its owner. That check lives in `App.RequireRankingAccess`, which parses the `{slug}` path value and stashes the `uuid.UUID` in the request context under `constants.SlugKey` — handlers on those routes read the slug from context, never from `PathValue`, and can assume access is already granted. `RequireUser` gates the account page. The global chain is Recover → RequestLog → CrossOriginProtection → session LoadAndSave → CSRF.

**Styling:** `assets/static/css/input.css` defines semantic CSS variables (`--app-background`, `--app-surface`, `--app-text-muted`, …) with a `prefers-color-scheme: dark` block, exposed to Tailwind as utilities like `bg-background`, `text-text-muted`, `border-border`. Use those tokens rather than raw palette colors. `app.css` is generated and gitignored.

## Tests

`internal/testsupport` boots the real app — real templates, real Postgres, in-memory session store — behind an `httptest.Server`. Each test gets a transaction that rolls back on cleanup, so tests are isolated and parallel-safe; migrations run once per process. Tests **skip** (not fail) when `TEST_DATABASE_URL` is unset. The `Client` helper keeps a cookie jar, does not follow redirects (assert on `Location`), scrapes the CSRF token out of returned HTML and attaches it, and offers `HTMX(method, path, form)` for fragment requests plus `FormWithBogusCSRF` for rejection tests. Handler tests live in `internal/app` and drive the app through HTTP; service tests bypass HTTP and call the service directly.
