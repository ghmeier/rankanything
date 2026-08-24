// Package testsupport spins up a real Postgres-backed environment for tests.
//
// Set TEST_DATABASE_URL to run these; without it the suites skip so `go test
// ./...` stays green on a machine with no database.
package testsupport

import (
	"context"
	"database/sql"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/ghmeier/rankanything/db/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/ghmeier/rankanything/assets"
	"github.com/ghmeier/rankanything/internal/app"
	"github.com/ghmeier/rankanything/internal/auth"
	"github.com/ghmeier/rankanything/internal/config"
	"github.com/ghmeier/rankanything/internal/db"
	"github.com/ghmeier/rankanything/internal/email"
	"github.com/ghmeier/rankanything/internal/services"
)

// init runs migrations once per test process so parallel tests don't race on
// the goose version table.
var migrationDone sync.Once

func init() {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		return
	}

	migrationDone.Do(func() {
		migrate(dsn)
	})
}

// Pool returns a pool and a test-scoped transaction.  All queries execute
// inside the transaction which rolls back at test end — concurrent tests
// never conflict.
func Pool(t *testing.T) (*pgxpool.Pool, pgx.Tx) {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(ctx))
	t.Cleanup(pool.Close)

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	return pool, tx
}

func NewSessionManager() *scs.SessionManager {
	sm := scs.New()
	sm.Store = memstore.New()
	sm.Lifetime = time.Hour
	auth.CookieDefaults(sm, false)

	return sm
}

// SessionContext creates a context with session data initialized, suitable
// for tests that need to call auth.Session methods like LogIn or RememberDraft.
func SessionContext(t *testing.T, sm *scs.SessionManager) context.Context {
	t.Helper()
	ctx := context.Background()
	ctx, err := sm.Load(ctx, "")
	require.NoError(t, err)
	return ctx
}

// migrate brings the test database up to the current schema using the same
// embedded migration set the binary ships, so tests can never run against a
// schema the deployed app wouldn't produce.
func migrate(dsn string) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		panic(err)
	}
	defer sqlDB.Close()

	goose.SetLogger(goose.NopLogger())
	if err := migrations.Up(context.Background(), sqlDB); err != nil {
		panic(err)
	}
}

// Env is a running server plus the collaborators tests want to poke at.
type Env struct {
	T       *testing.T
	App     *app.App
	Pool    *pgxpool.Pool // kept for health checks
	Tx      pgx.Tx        // test transaction; rolls back on test exit
	Queries *db.Queries
	Server  *httptest.Server

	// EmailSink is the DevSink backing App.EmailSvc, so tests can assert on
	// what a verification or password-reset flow would have mailed without
	// touching the network.
	EmailSink *email.DevSink
}

// NewEnv builds the real app (real components, real database, in-memory
// session store) behind an httptest server.
func NewEnv(t *testing.T) *Env {
	t.Helper()

	pool, tx := Pool(t)

	queries := db.New(tx)
	sm := NewSessionManager()
	s := auth.NewSessions(sm)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// Reading APP_ENV directly (rather than requiring a full config.Load,
	// which also demands DATABASE_URL) keeps this test environment able to
	// exercise the same production gating main.go wires from config.Config.
	isProduction := config.Config{Env: os.Getenv("APP_ENV")}.IsProduction()

	// The dev sink never touches the network: tests assert on what it
	// captured (Env.EmailSink) instead of hitting Resend.
	emailSink := email.NewDevSink(logger)

	application := &app.App{
		Pool:         pool,
		Queries:      queries.WithTx(tx),
		Sessions:     s,
		Logger:       logger,
		Static:       assets.Static(),
		IsProduction: isProduction,
		UserSvc:      &services.UserService{Queries: queries.WithTx(tx), Sessions: s},
		RankingSvc:   &services.RankingsService{Queries: queries.WithTx(tx), Pool: tx},
		EmailSvc:     emailSink,
		ShareSvc:     &services.ShareService{Queries: queries.WithTx(tx), BaseURL: "https://test.rankanything.app"},
		VerificationSvc: &services.VerificationService{
			Queries:  queries.WithTx(tx),
			Sender:   emailSink,
			Sessions: s,
			BaseURL:  "https://test.rankanything.app",
		},
	}

	srv := httptest.NewServer(application.Routes())
	t.Cleanup(srv.Close)

	return &Env{T: t, App: application, Pool: pool, Tx: tx, Queries: application.Queries, Server: srv, EmailSink: emailSink}
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

// OwnerClient is a signed-in client together with the ranking (and its
// seeded draft version) it owns — the fixture nearly every ranking test in
// internal/app needs now that anonymous drafts are gone and every ranking
// requires a signed-in owner from the moment it's created.
type OwnerClient struct {
	*Client
	Ranking db.Ranking
	Draft   db.RankingVersion
}

// NewOwnerClient registers a fresh user and creates a ranking through the
// same service path GET /new uses, so its draft version comes pre-seeded
// with the default tiers exactly like the real app.
//
// It goes through the services rather than POST /register and POST /new so
// that a fixture nearly every handler test needs doesn't cost two HTTP round
// trips; the bcrypt hash alone dominated the suite's runtime. LoginUser then
// plants the session the real requests would have established.
func (e *Env) NewOwnerClient() *OwnerClient {
	e.T.Helper()
	c := e.NewClient()

	// UserService.Register logs the new user in, and scs panics on a context
	// that never went through the session middleware, so registration needs a
	// loaded session even though the one the client actually uses is the one
	// LoginUser commits below.
	ctx, err := e.App.Sessions.Load(context.Background(), "")
	require.NoError(e.T, err)

	email := "owner+" + uuid.NewString() + "@example.com"
	user, err := e.App.UserSvc.Register(ctx, services.RegisterRequest{
		Email: email, Password: OwnerPasswordHash(e.T),
	})
	require.NoError(e.T, err)

	c.LoginUser(user.ID)

	// Sending the verification email is the register handler's job, not the
	// service's, so bypassing HTTP would otherwise leave the new user in a
	// state real registration never produces.
	require.NoError(e.T, e.App.VerificationSvc.SendVerificationEmail(ctx, user.ID, user.Email))

	ranking, err := e.App.RankingSvc.CreateForUser(ctx, services.CreateForUserRequest{UserID: user.ID})
	require.NoError(e.T, err)

	draft, err := e.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	require.NoError(e.T, err)

	return &OwnerClient{Client: c, Ranking: ranking, Draft: draft}
}

// OwnerPassword is the plaintext behind every NewOwnerClient user, so a test
// that wants to sign one of them in again over HTTP can.
const OwnerPassword = "supersecret"

// OwnerPasswordHash hashes OwnerPassword at bcrypt's minimum cost. The stored
// value is still a real bcrypt hash that auth.CheckPassword accepts, but
// auth.HashPassword's DefaultCost costs tens of milliseconds per call, which
// is most of what a fixture this widely used spends.
func OwnerPasswordHash(t *testing.T) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(OwnerPassword), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

// LoginUser signs the client in as userID by committing a session directly to
// the store and putting its token in the cookie jar — the same end state
// POST /login reaches, without the request.
func (c *Client) LoginUser(userID int64) {
	c.t.Helper()

	ctx, err := c.env.App.Sessions.Load(context.Background(), "")
	require.NoError(c.t, err)

	err = c.env.App.Sessions.LogIn(ctx, userID)
	require.NoError(c.t, err)

	token, _, err := c.env.App.Sessions.Commit(ctx)
	require.NoError(c.t, err)

	u, err := url.Parse(c.env.Server.URL)
	require.NoError(c.t, err)

	c.http.Jar.SetCookies(u, []*http.Cookie{
		{
			Name:  c.env.App.Sessions.Cookie.Name,
			Value: token,
			Path:  c.env.App.Sessions.Cookie.Path,
		},
	})
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

// FormWithBogusCSRF submits a form with an intentionally invalid CSRF token,
// for testing that the server rejects it.
func (c *Client) FormWithBogusCSRF(method, path string, form url.Values) *Response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.env.Server.URL+path, strings.NewReader(form.Encode()))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "bogus")
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

// extractCSRF pulls the session's CSRF token out of the hx-headers attribute
// the layout puts on <body>. The body is unescaped first because templ
// escapes attribute values, so the JSON's quotes arrive as &#34; — a browser
// undoes that when it parses the attribute, and this has to do the same.
func extractCSRF(body string) string {
	const marker = `"X-CSRF-Token": "`
	_, after, found := strings.Cut(html.UnescapeString(body), marker)
	if !found {
		return ""
	}
	before, _, found := strings.Cut(after, `"`)
	if !found {
		return ""
	}
	return before
}
