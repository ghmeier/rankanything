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
	"github.com/ghmeier/rankanything/internal/storage"
)

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

func SessionContext(t *testing.T, sm *scs.SessionManager) context.Context {
	t.Helper()
	ctx := context.Background()
	ctx, err := sm.Load(ctx, "")
	require.NoError(t, err)
	return ctx
}

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

type Env struct {
	T       *testing.T
	App     *app.App
	Pool    *pgxpool.Pool // kept for health checks
	Tx      pgx.Tx        // test transaction; rolls back on test exit
	Queries *db.Queries
	Server  *httptest.Server

	EmailSink *email.DevSink
}

func NewEnv(t *testing.T) *Env {
	t.Helper()

	pool, tx := Pool(t)

	queries := db.New(tx)
	sm := NewSessionManager()
	s := auth.NewSessions(sm)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	isProduction := config.Config{Env: os.Getenv("APP_ENV")}.IsProduction()

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
		ShareSvc:     &services.ShareService{Queries: queries.WithTx(tx), Pool: tx, EmailSender: emailSink, BaseURL: "https://test.rankanything.app"},
		VerificationSvc: &services.VerificationService{
			Queries:  queries.WithTx(tx),
			Sender:   emailSink,
			Sessions: s,
			BaseURL:  "https://test.rankanything.app",
		},
		Storage: storage.NewMemoryStorage(),
	}

	srv := httptest.NewServer(application.Routes())
	t.Cleanup(srv.Close)

	return &Env{T: t, App: application, Pool: pool, Tx: tx, Queries: application.Queries, Server: srv, EmailSink: emailSink}
}

func (e *Env) RebuildServer() {
	e.T.Helper()
	e.Server.Close()
	e.Server = httptest.NewServer(e.App.Routes())
	e.T.Cleanup(e.Server.Close)
}

type Client struct {
	t    *testing.T
	env  *Env
	http *http.Client
	csrf string
}

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

type OwnerClient struct {
	*Client
	Ranking db.Ranking
	Draft   db.RankingVersion
}

// Bypasses HTTP to avoid bcrypt cost per fixture.
func (e *Env) NewOwnerClient() *OwnerClient {
	e.T.Helper()
	c := e.NewClient()

	// scs panics without a loaded session context.
	ctx, err := e.App.Sessions.Load(context.Background(), "")
	require.NoError(e.T, err)

	email := "owner+" + uuid.NewString() + "@example.com"
	user, err := e.App.UserSvc.Register(ctx, services.RegisterRequest{
		Email: email, Password: OwnerPasswordHash(e.T),
	})
	require.NoError(e.T, err)

	c.LoginUser(user.ID)

	require.NoError(e.T, e.App.VerificationSvc.SendVerificationEmail(ctx, user.ID, user.Email))

	ranking, err := e.App.RankingSvc.CreateForUser(ctx, services.CreateForUserRequest{UserID: user.ID})
	require.NoError(e.T, err)

	draft, err := e.Queries.ResolveLiveRankingVersion(ctx, ranking.ID)
	require.NoError(e.T, err)

	return &OwnerClient{Client: c, Ranking: ranking, Draft: draft}
}

const OwnerPassword = "supersecret"

// MinCost to keep fixtures fast.
func OwnerPasswordHash(t *testing.T) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(OwnerPassword), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

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

func (c *Client) Get(path string) *Response {
	c.t.Helper()
	req, err := http.NewRequest(http.MethodGet, c.env.Server.URL+path, nil)
	require.NoError(c.t, err)
	return c.do(req)
}

func (c *Client) Post(path string, form url.Values) *Response {
	c.t.Helper()
	return c.form(http.MethodPost, path, form)
}

func (c *Client) Put(path string, form url.Values) *Response {
	c.t.Helper()
	return c.form(http.MethodPut, path, form)
}

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

func (c *Client) FormWithBogusCSRF(method, path string, form url.Values) *Response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.env.Server.URL+path, strings.NewReader(form.Encode()))
	require.NoError(c.t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", "bogus")
	return c.do(req)
}

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

type Response struct {
	Status int
	Header http.Header
	Body   string
}

func (r *Response) Location() string { return r.Header.Get("Location") }

func (r *Response) Slug() uuid.UUID {
	loc := r.Location()
	if !strings.HasPrefix(loc, "/r/") {
		return uuid.UUID{}
	}
	return uuid.MustParse(strings.TrimPrefix(loc, "/r/"))
}

// Unescapes first because templ escapes attribute values.
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
