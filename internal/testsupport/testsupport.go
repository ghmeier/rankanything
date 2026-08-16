// Package testsupport spins up a real Postgres-backed environment for tests.
//
// Set TEST_DATABASE_URL to run these; without it the suites skip so `go test
// ./...` stays green on a machine with no database.
package testsupport

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/ghmeier/rankanything/assets"
	"github.com/ghmeier/rankanything/internal/app"
	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/render"
	"github.com/ghmeier/rankanything/internal/services"
)

// Pool returns a migrated pool with every table truncated, so each test starts
// from a known-empty database.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	migrate(t, dsn)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(ctx))
	t.Cleanup(pool.Close)

	truncate(t, pool)
	return pool
}

func migrate(t *testing.T, dsn string) {
	t.Helper()

	sqlDB, err := sql.Open("pgx", dsn)
	require.NoError(t, err)
	defer sqlDB.Close()

	goose.SetLogger(goose.NopLogger())
	require.NoError(t, goose.SetDialect("postgres"))
	require.NoError(t, goose.Up(sqlDB, migrationsDir(t)))
}

func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(file), "..", "..", "db", "migrations")
}

func truncate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE ranking_tier_items, ranking_items, ranked_items, ranking_tiers, rankings, users RESTART IDENTITY CASCADE`)
	require.NoError(t, err)
}

// Env is a running server plus the collaborators tests want to poke at.
type Env struct {
	T       *testing.T
	App     *app.App
	Pool    *pgxpool.Pool
	Queries *db.Queries
	Server  *httptest.Server
}

// NewEnv builds the real app (real templates, real database, in-memory session
// store) behind an httptest server.
func NewEnv(t *testing.T) *Env {
	t.Helper()

	pool := Pool(t)

	sm := scs.New()
	sm.Store = memstore.New()
	sm.Lifetime = time.Hour
	auth.CookieDefaults(sm, false)

	renderer, err := render.New(assets.Templates())
	require.NoError(t, err)

	queries := db.New(pool)
	application := &app.App{
		Pool:     pool,
		Queries:  queries,
		Sessions: auth.NewSessions(sm),
		Render:   renderer,
		Logger:   slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		Static:   assets.Static(),
		UserSvc:  &services.UserService{Queries: queries, Sessions: auth.NewSessions(sm)},
	}

	srv := httptest.NewServer(application.Routes())
	t.Cleanup(srv.Close)

	return &Env{T: t, App: application, Pool: pool, Queries: application.Queries, Server: srv}
}

// Client is a browser-like client: it keeps cookies and tracks the CSRF token
// the server handed out.
type Client struct {
	t    *testing.T
	env  *Env
	http *http.Client
	csrf string
}

// NewClient returns a cookie-jar client that does not follow redirects, so
// tests can assert on Location headers.
func (e *Env) NewClient() *Client {
	e.T.Helper()
	jar, err := cookiejar.New(nil)
	require.NoError(e.T, err)

	return &Client{
		t:   e.T,
		env: e,
		http: &http.Client{
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Get performs a GET and captures the CSRF token from the response body.
func (c *Client) Get(path string) *Response {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.env.Server.URL+path, nil)
	require.NoError(c.t, err)
	return c.do(req)
}

// Post submits a form, attaching the session's CSRF token automatically.
func (c *Client) Post(path string, form url.Values) *Response {
	c.t.Helper()
	return c.form(http.MethodPost, path, form)
}

// Put submits a form with PUT (used for tier edits and placements).
func (c *Client) Put(path string, form url.Values) *Response {
	c.t.Helper()
	return c.form(http.MethodPut, path, form)
}

// Delete submits a form with DELETE.
func (c *Client) Delete(path string, form url.Values) *Response {
	c.t.Helper()
	return c.form(http.MethodDelete, path, form)
}

func (c *Client) form(method, path string, form url.Values) *Response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.env.Server.URL+path, strings.NewReader(form.Encode()))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", c.CSRF())
	return c.do(req)
}

// HTMX marks the next request as an htmx swap.
func (c *Client) HTMX(method, path string, form url.Values) *Response {
	c.t.Helper()
	if form == nil {
		form = url.Values{}
	}
	req, err := http.NewRequest(method, c.env.Server.URL+path, strings.NewReader(form.Encode()))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("X-CSRF-Token", c.CSRF())
	return c.do(req)
}

// CSRF returns the token for this client's session, fetching a page if needed.
func (c *Client) CSRF() string {
	c.t.Helper()
	if c.csrf == "" {
		c.Get("/login")
	}
	require.NotEmpty(c.t, c.csrf, "no CSRF token was issued")
	return c.csrf
}

func (c *Client) do(req *http.Request) *Response {
	c.t.Helper()
	res, err := c.http.Do(req)
	require.NoError(c.t, err)
	defer res.Body.Close()

	raw, err := io.ReadAll(res.Body)
	require.NoError(c.t, err)
	body := string(raw)

	if tok := extractCSRF(body); tok != "" {
		c.csrf = tok
	}
	return &Response{Status: res.StatusCode, Header: res.Header, Body: body}
}

// Response is a flattened HTTP response for assertions.
type Response struct {
	Status int
	Header http.Header
	Body   string
}

// Location is the redirect target, if any.
func (r *Response) Location() string { return r.Header.Get("Location") }

// Slug pulls the ranking slug out of a redirect to /r/{slug}.
func (r *Response) Slug() uuid.UUID {
	loc := r.Location()
	if !strings.HasPrefix(loc, "/r/") {
		return uuid.UUID{}
	}
	return uuid.MustParse(strings.TrimPrefix(loc, "/r/"))
}

func extractCSRF(body string) string {
	const marker = `"X-CSRF-Token": "`
	_, after, found := strings.Cut(body, marker)
	if !found {
		return ""
	}
	before, _, found := strings.Cut(after, `"`)
	if !found {
		return ""
	}
	return before
}
