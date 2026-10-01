package main

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/json"
	graphql "github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed migrations/*.sql schema.graphql
var files embed.FS

type JSON struct{ Value any }

func (JSON) ImplementsGraphQLType(name string) bool { return name == "JSON" }
func (j *JSON) UnmarshalGraphQL(v any) error        { j.Value = v; return nil }
func (j JSON) MarshalJSON() ([]byte, error)         { return json.Marshal(j.Value) }

type actorKey struct{}
type Resolver struct{ Store *Store }

func current(ctx context.Context) Actor { return ctx.Value(actorKey{}).(Actor) }
func (r *Resolver) Snapshot(ctx context.Context) (JSON, error) {
	v, e := r.Store.Snapshot(ctx, current(ctx))
	return JSON{v}, e
}

type SubmitInput struct {
	OrderID        graphql.ID
	Message        string
	Mode           string
	IdempotencyKey string
}

func (r *Resolver) SubmitCase(ctx context.Context, args struct{ Input SubmitInput }) (JSON, error) {
	i := args.Input
	v, e := r.Store.Submit(ctx, current(ctx), string(i.OrderID), i.Message, i.Mode, i.IdempotencyKey, Object{"mode": i.Mode})
	return JSON{v}, e
}

type DecisionInput struct {
	ProposalID     graphql.ID
	Binding        string
	Decision       string
	IdempotencyKey string
}

func (r *Resolver) DecideProposal(ctx context.Context, args struct{ Input DecisionInput }) (JSON, error) {
	i := args.Input
	v, e := r.Store.Decide(ctx, current(ctx), string(i.ProposalID), i.Binding, i.Decision, i.IdempotencyKey)
	return JSON{v}, e
}
func (r *Resolver) SimulateProduction(ctx context.Context, args struct{ OrderID graphql.ID }) (JSON, error) {
	v, e := r.Store.ChangeOrder(ctx, current(ctx), string(args.OrderID))
	return JSON{v}, e
}
func (r *Resolver) UpdatePolicy(ctx context.Context, args struct{ Policy JSON }) (JSON, error) {
	b, _ := json.Marshal(args.Policy.Value)
	var p Policy
	if e := json.Unmarshal(b, &p); e != nil {
		return JSON{}, fail("INVALID", "Malformed policy")
	}
	v, e := r.Store.SetPolicy(ctx, current(ctx), p)
	return JSON{v}, e
}
func (r *Resolver) RequestCaseReview(ctx context.Context, args struct {
	CaseID graphql.ID
	Reason string
}) (JSON, error) {
	v, e := r.Store.RequestReview(ctx, current(ctx), string(args.CaseID), args.Reason)
	return JSON{v}, e
}
func env(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
func secret(k string) string {
	if path := os.Getenv(k + "_FILE"); path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			panic("secret file unavailable")
		}
		return strings.TrimSpace(string(b))
	}
	return os.Getenv(k)
}
func cookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: "presspilot_session", Value: token, Path: "/", HttpOnly: true, Secure: os.Getenv("COOKIE_SECURE") == "true", SameSite: http.SameSiteStrictMode, MaxAge: 3600})
}
func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	traces, traceErr := setupTracing(ctx)
	if traceErr != nil {
		panic("tracing configuration unavailable")
	}
	defer func() {
		shutdown, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = traces.Shutdown(shutdown)
	}()
	db, e := pgxpool.New(ctx, secret("DATABASE_URL"))
	if e != nil {
		panic("database configuration invalid")
	}
	defer db.Close()
	if e = db.Ping(ctx); e != nil {
		panic("database unavailable")
	}
	migration, _ := files.ReadFile("migrations/001.sql")
	if env("AUTO_MIGRATE", "true") == "true" || os.Getenv("MIGRATE_ONLY") == "true" {
		if _, e = db.Exec(ctx, string(migration)); e != nil {
			panic(e)
		}
	}
	if os.Getenv("MIGRATE_ONLY") == "true" {
		return
	}
	store := &Store{db}
	if os.Getenv("RECONCILE_SHOPIFY") == "true" {
		if e = reconcileShopify(ctx, store, ShopifyClient{Shop: osShop(), Token: secret("SHOPIFY_ACCESS_TOKEN"), Version: env("SHOPIFY_API_VERSION", "2026-10")}); e != nil {
			slog.Error("shopify_reconciliation_failed", "code", category(e))
			os.Exit(1)
		}
		return
	}
	sdl, _ := files.ReadFile("schema.graphql")
	schema := graphql.MustParseSchema(string(sdl), &Resolver{store}, graphql.MaxDepth(8), graphql.MaxQueryLength(10000), graphql.MaxParallelism(4))
	workerSecret := secret("WORKER_SECRET")
	if len(workerSecret) < 32 {
		panic("WORKER_SECRET must contain at least 32 characters")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			reply(w, 405, Object{"error": "POST required"})
			return
		}
		var in struct {
			AccessToken string `json:"accessToken"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || len(in.AccessToken) > 256 {
			reply(w, 400, Object{"error": "Invalid session input"})
			return
		}
		a, err := store.Session(r.Context(), in.AccessToken)
		if err != nil {
			reply(w, 401, Object{"error": "Session unavailable or expired"})
			return
		}
		cookie(w, in.AccessToken)
		reply(w, 200, Object{"actor": a})
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := db.Ping(r.Context()); e != nil {
			reply(w, 503, Object{"status": "database_unavailable"})
			return
		}
		reply(w, 200, Object{"status": "ready"})
	})
	mux.HandleFunc("/api/sandbox", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || os.Getenv("SANDBOX_ENABLED") != "true" {
			reply(w, 404, Object{"error": "Sandbox disabled"})
			return
		}
		token, a, e := store.NewSandbox(r.Context())
		if e != nil {
			reply(w, 429, Object{"error": e.Error()})
			return
		}
		cookie(w, token)
		reply(w, 201, Object{"actor": a, "seeded": true, "expiresInSeconds": 3600})
	})
	mux.HandleFunc("/api/sandbox/role", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || os.Getenv("SANDBOX_ENABLED") != "true" {
			reply(w, 404, Object{"error": "Sandbox disabled"})
			return
		}
		c, e := r.Cookie("presspilot_session")
		if e != nil {
			reply(w, 401, Object{"error": "Sign in required"})
			return
		}
		a, e := store.Session(r.Context(), c.Value)
		if e != nil || !a.Sandbox {
			reply(w, 403, Object{"error": "Sandbox session required"})
			return
		}
		var input struct {
			Role string `json:"role"`
		}
		if e = json.NewDecoder(r.Body).Decode(&input); e != nil {
			reply(w, 400, Object{"error": "Invalid body"})
			return
		}
		if input.Role != "operator" && input.Role != "approver" && input.Role != "administrator" {
			reply(w, 400, Object{"error": "Invalid role"})
			return
		}
		if _, e = db.Exec(r.Context(), "UPDATE sessions SET user_id=$2 WHERE digest=$1", digest(c.Value), input.Role); e != nil {
			reply(w, 503, Object{"error": "Session update failed"})
			return
		}
		reply(w, 200, Object{"role": input.Role})
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			reply(w, 405, Object{"error": "POST required"})
			return
		}
		c, e := r.Cookie("presspilot_session")
		if e != nil {
			reply(w, 401, Object{"error": "Session required"})
			return
		}
		a, e := store.Session(r.Context(), c.Value)
		if e != nil {
			reply(w, 401, Object{"error": e.Error()})
			return
		}
		(&relay.Handler{Schema: schema}).ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
	mux.HandleFunc("/internal/tools", func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if r.Method != "POST" || subtle.ConstantTimeCompare([]byte(bearer), []byte(workerSecret)) != 1 {
			reply(w, 403, Object{"code": "FORBIDDEN", "error": "Worker authentication required"})
			return
		}
		var in ToolInput
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if e = decoder.Decode(&in); e != nil || len(in.CallID) < 8 || len(in.CallID) > 128 {
			reply(w, 400, Object{"code": "INVALID", "error": "Invalid typed tool request"})
			return
		}
		v, err := store.Tool(r.Context(), in)
		if err != nil {
			code := category(err)
			if code == "RETRYABLE" {
				slog.Error("tool_backend_failure", "tool", in.Name, "code", code)
			}
			status := 422
			message := err.Error()
			if code == "RETRYABLE" {
				status = 503
				message = "Backend unavailable; retry within activity bounds"
			}
			reply(w, status, Object{"code": code, "error": message})
			return
		}
		reply(w, 200, v)
	})
	registerShopify(mux, store)
	go dispatch(ctx, store)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		trace := id()
		w.Header().Set("X-Request-ID", trace)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if origin := r.Header.Get("Origin"); origin != "" && origin != env("WEB_ORIGIN", "http://localhost:5173") {
			reply(w, 403, Object{"error": "Origin not allowed"})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 65536)
		mux.ServeHTTP(w, r)
		spanContext := oteltrace.SpanContextFromContext(r.Context())
		slog.Info("http_request", "request_id", trace, "method", r.Method, "path", r.URL.Path, "trace_id", spanContext.TraceID().String(), "span_id", spanContext.SpanID().String(), "duration_ms", time.Since(start).Milliseconds())
	})
	server := &http.Server{Addr: env("API_BIND", "127.0.0.1") + ":" + env("PORT", "8080"), Handler: tracedHTTP(traces, handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second}
	go func() {
		<-ctx.Done()
		shutdown, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = server.Shutdown(shutdown)
	}()
	slog.Info("api_started", "port", env("PORT", "8080"))
	if e = server.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		panic(e)
	}
}
func dispatch(ctx context.Context, s *Store) {
	var temporal client.Client
	defer func() {
		if temporal != nil {
			temporal.Close()
		}
	}()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if temporal == nil {
			options := client.Options{HostPort: env("TEMPORAL_ADDRESS", "127.0.0.1:7233"), Namespace: env("TEMPORAL_NAMESPACE", "default")}
			if os.Getenv("TEMPORAL_TLS") == "true" {
				options.ConnectionOptions.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
			}
			if key := secret("TEMPORAL_API_KEY"); key != "" {
				options.Credentials = client.NewAPIKeyStaticCredentials(key)
			}
			c, e := client.Dial(options)
			if e != nil {
				slog.Warn("temporal_unavailable")
				continue
			}
			temporal = c
		}
		rows, e := s.DB.Query(ctx, "SELECT org_id,id,mode FROM cases WHERE NOT workflow_started AND status='investigating' AND EXISTS(SELECT 1 FROM organizations o WHERE o.id=cases.org_id AND (o.expires_at IS NULL OR o.expires_at>now())) ORDER BY created_at LIMIT 10")
		if e != nil {
			continue
		}
		type item struct{ Org, ID, Mode string }
		items := []item{}
		for rows.Next() {
			var v item
			if e = rows.Scan(&v.Org, &v.ID, &v.Mode); e == nil {
				items = append(items, v)
			}
		}
		rows.Close()
		for _, v := range items {
			_, e = temporal.ExecuteWorkflow(ctx, client.StartWorkflowOptions{ID: "case-" + v.Org + "-" + v.ID, TaskQueue: "presspilot", WorkflowExecutionTimeout: 65 * time.Minute, WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE}, "resolveCase", Object{"org": v.Org, "caseId": v.ID, "mode": v.Mode})
			if e == nil || strings.Contains(e.Error(), "already") {
				_, _ = s.DB.Exec(ctx, "UPDATE cases SET workflow_started=true WHERE org_id=$1 AND id=$2", v.Org, v.ID)
			} else {
				slog.Warn("workflow_dispatch_failed")
			}
		}
		// Expiry is a business transition even if the worker is down.
		_, _ = s.DB.Exec(ctx, "WITH expired AS (UPDATE cases SET status='escalated',summary='Approval window expired; human review required' WHERE status='awaiting_approval' AND EXISTS(SELECT 1 FROM proposals p WHERE p.org_id=cases.org_id AND p.case_id=cases.id AND p.status='pending' AND p.expires_at<now()) RETURNING org_id,id) INSERT INTO audit_events(org_id,case_id,actor,kind,data) SELECT org_id,id,'dispatcher','approval_expired','{}'::jsonb FROM expired")
	}
}

// Keep pgx referenced here for build-time driver compatibility checks.
var _ = pgx.ErrNoRows
