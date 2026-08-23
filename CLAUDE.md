# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

Rankanything is a server-rendered tier-list builder: create a ranking, add items, drag them into tiers, share it. Every ranking has a signed-in owner from creation — there is no anonymous draft. A ranking's tiers and items belong to a `RankingVersion` (a draft has no `published_at`); `/r/{uuid}` addresses the live version, `/r/{uuid}/v/{short}` pins one. Go 1.26 + htmx + Tailwind 4 + Postgres 18, shipped as a single binary: markup is templ components compiled into the binary, static assets are embedded (`assets/assets.go`).

## Commands

```bash
make dev            # air (Go hot reload) + tailwind --watch, together
make test           # go test ./... -race, with TEST_DATABASE_URL loaded from .env
make migrate        # goose up against $DATABASE_URL
make migrate-down   # goose down one step
make templ          # regenerate *_templ.go from .templ sources; the generated files are committed, so run this and commit the result after any .templ change, or CI's drift check fails
make sqlc           # regenerate internal/db from db/queries + db/migrations
make css            # one-shot minified tailwind build
make tidy

go run ./cmd/rankanything                    # run the server (make run is stale: it points at a nonexistent ./main.go)
go test ./internal/app -run TestOwnedRankingsAreNotReadableByOthers -race
```

`.env` (gitignored) supplies `DATABASE_URL`; `config.Load` hard-fails if `.env` is missing or the URL is empty. The Makefile loads it with `-include`, so a checkout without one (CI) can still run `make css` and `make templ` — but note that an unset `TEST_DATABASE_URL` makes every database test *skip* and `make test` exit 0. `PORT` defaults to `:8001`, `APP_ENV` to `development` (production flips the Secure cookie flag). `make seed` references a `./cmd/seed` that does not exist.

The MVP rewrote `db/migrations` from scratch and reused numbers 00003 and 00004 for different tables, so a database created against the prototype cannot be migrated forward — goose reports those versions as applied and then collides on `00005_rankings.sql`. Point at a fresh database rather than trying to reconcile an old one.

## Architecture

**Request path:** `cmd/rankanything/main.go` builds the `app.App` struct (pool, queries, sessions, services) and hands `app.Routes()` to the server. Everything flows through that one struct — there is no DI container or global state.

**Layering is strict.** `internal/app` knows HTTP and components; `internal/services` knows domain state and never imports `net/http`; `internal/db` is sqlc-generated and must not be hand-edited. Handlers translate form values into a per-operation `services.XxxRequest` struct and translate the result into a view struct from `internal/app/view.go`. When adding a service method, follow the `XxxRequest` struct convention rather than long parameter lists.

**Data layer:** `db/queries/*.sql` is the source of truth. Edit the SQL, run `make sqlc`. Schema changes are goose migrations in `db/migrations` (`-- +goose Up` / `Down`). UUIDs map to `github.com/google/uuid`, nullable columns to pointers (`emit_pointers_for_null_types`).

**Rendering (`internal/ui`):** every page and fragment is a templ component, type-checked at compile time — there is no template cache, no runtime template lookup by name, and no `html/template` anywhere. A page component wraps its content in `Layout`; the same component tree also exposes the smaller pieces htmx swaps in, so a handler renders either the whole page or just the fragment from one source. `internal/app/view.go` holds the three helpers the handlers share: `renderComponent` (status code then render, since `templ.Component.Render` has no notion of one), `isHTMXRequest` (fragment or full page), and `empty` (status, no body).

Generated `*_templ.go` files **are committed**. Run `make templ` after changing a `.templ` and commit the result, or CI's drift check fails.

One templ gotcha worth knowing: a component that writes its own `class` (like `Button`) will emit a duplicate attribute if you also pass one through `Attrs`, and the browser keeps the first. `ButtonClass` is exported so you can build the class string yourself in that case.

**htmx conventions:** the layout sets `hx-headers` with the session CSRF token on `<body>`, so every htmx-issued mutation carries `X-CSRF-Token` automatically. Mutating handlers respond with the smallest component that covers the change (`ItemCard`, `TierRow`, a label swap for inline tier editing) or `empty` for deletions; out-of-band swaps are used to remove a row. Adding an endpoint usually means adding a component, not a page. htmx only swaps 2xx by default — the auth pages render `AuthErrorSwapScript` to opt 401 and 422 back in, since a rejected sign-in or registration answers with the form that carries the message.

**Auth and ownership (`internal/auth`):** sessions are scs backed by pgxstore. The session holds `user_id`, a CSRF token, and a one-shot flash. A ranking is reachable only by its owner — that check lives in `App.RequireRankingAccess`, which parses the `{uuid}` path value, confirms `rankings.user_id` matches the session's user, and stashes the ranking's `uuid.UUID` in the request context under `constants.RankingUUIDKey`. It also resolves which version the request addresses (the live version for `/r/{uuid}`, or the one pinned by `/r/{uuid}/v/{short}`) and stashes that `db.RankingVersion` under `constants.RankingVersionKey`. Handlers on those routes read both from context, never from `PathValue`, and can assume access is already granted. `RequireUser` gates the account page. `Routes()` is split into `registerAuthRoutes`, `registerRankingRoutes`, and `registerPublicRoutes` (one file each), called from the middleware chain: Recover → RequestLog → CrossOriginProtection → session LoadAndSave → CSRF.

**Styling:** `assets/static/css/input.css` defines semantic CSS variables (`--app-background`, `--app-surface`, `--app-text-muted`, …) with a `prefers-color-scheme: dark` block, exposed to Tailwind as utilities like `bg-background`, `text-text-muted`, `border-border`. Use those tokens rather than raw palette colors. `app.css` is generated and gitignored.

## Tests

`internal/testsupport` boots the real app — real components, real Postgres, in-memory session store — behind an `httptest.Server`. Each test gets a transaction that rolls back on cleanup, so tests are isolated and parallel-safe; migrations run once per process. Tests **skip** (not fail) when `TEST_DATABASE_URL` is unset. The `Client` helper keeps a cookie jar, does not follow redirects (assert on `Location`), scrapes the CSRF token out of returned HTML and attaches it (unescaping first, since templ escapes the `hx-headers` attribute the token rides in), and offers `HTMX(method, path, form)` for fragment requests plus `FormWithBogusCSRF` for rejection tests. `Env.NewOwnerClient()` registers a fresh user and creates a ranking for them (draft version seeded with the default tiers, same as `POST /new`) — the fixture most ranking tests need now that every ranking requires a signed-in owner. Handler tests live in `internal/app` and drive the app through HTTP; service tests bypass HTTP and call the service directly.
