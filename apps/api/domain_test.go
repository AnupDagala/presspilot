package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Store, Actor) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL required: actual PostgreSQL integration test")
	}
	db, e := pgxpool.New(context.Background(), url)
	if e != nil {
		t.Fatal(e)
	}
	migration, _ := files.ReadFile("migrations/001.sql")
	if _, e = db.Exec(context.Background(), string(migration)); e != nil {
		t.Fatal(e)
	}
	s := &Store{db}
	_, a, e := s.NewSandbox(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "UPDATE organizations SET expires_at=now() WHERE id=$1", a.Org)
		db.Close()
	})
	return s, a
}
func submit(t *testing.T, s *Store, a Actor, key string) Case {
	t.Helper()
	v, e := s.Submit(context.Background(), a, "PF-1041", "Please cancel my order.", "baseline", key, Object{})
	if e != nil {
		t.Fatal(e)
	}
	m := obj(v)
	return Case{ID: m["id"].(string), OrderID: "PF-1041", Requester: a.User}
}
func propose(t *testing.T, s *Store, a Actor, c Case) Proposal {
	t.Helper()
	ctx := context.Background()
	for _, name := range []string{"get_order", "get_applicable_policy"} {
		_, e := s.Tool(ctx, ToolInput{a.Org, c.ID, "test-" + name, name, ToolArgs{}})
		if e != nil {
			t.Fatal(e)
		}
	}
	v, e := s.Tool(ctx, ToolInput{a.Org, c.ID, "test-proposal", "propose_order_cancellation", ToolArgs{OrderVersion: 1, PolicyVersion: 1}})
	if e != nil {
		t.Fatal(e)
	}
	m := obj(v)
	return Proposal{ID: m["id"].(string), CaseID: c.ID, OrderID: c.OrderID, Binding: m["binding"].(string)}
}
func approve(t *testing.T, s *Store, a Actor, p Proposal) {
	t.Helper()
	a.User = "approver"
	_, e := s.Decide(context.Background(), a, p.ID, p.Binding, "approved", "approval-"+p.ID)
	if e != nil {
		t.Fatal(e)
	}
}
func TestDomainPolicy(t *testing.T) {
	p := defaultPolicy()
	o := Order{ID: "A", Version: 1, State: "queued", Attributes: Object{}}
	if eligible(o, p, "cancel", Object{}) != nil {
		t.Fatal("queued cancellation should be eligible")
	}
	o.State = "production"
	if eligible(o, p, "cancel", Object{}) == nil {
		t.Fatal("production cancellation allowed")
	}
	o.State = "queued"
	for _, args := range []Object{{"quantity": "5"}, {"finish": "metallic"}, {"finish": "gloss", "customerReference": "x"}} {
		if eligible(o, p, "modify", args) == nil {
			t.Fatal("forbidden modification allowed")
		}
	}
	if eligible(o, p, "modify", Object{"finish": "gloss"}) != nil {
		t.Fatal("permitted finish rejected")
	}
}
func TestIdempotencyAndAtomicEffect(t *testing.T) {
	s, a := fixture(t)
	c := submit(t, s, a, "repeat-request")
	again := submit(t, s, a, "repeat-request")
	if again.ID != c.ID {
		t.Fatal("duplicate case")
	}
	if _, e := s.Submit(context.Background(), a, "PF-1041", "change this", "baseline", "repeat-request", Object{}); category(e) != "CONFLICT" {
		t.Fatal("changed key arguments accepted", e)
	}
	p := propose(t, s, a, c)
	approve(t, s, a, p)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Execute(context.Background(), a.Org, p.ID); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var n, version int
	var state string
	ctx := context.Background()
	_ = s.DB.QueryRow(ctx, "SELECT count(*) FROM executions WHERE org_id=$1", a.Org).Scan(&n)
	_ = s.DB.QueryRow(ctx, "SELECT version,state FROM orders WHERE org_id=$1 AND id='PF-1041'", a.Org).Scan(&version, &state)
	if n != 1 || version != 2 || state != "cancelled" {
		t.Fatalf("effects=%d version=%d state=%s", n, version, state)
	}
}
func TestConcurrentApprovals(t *testing.T) {
	s, a := fixture(t)
	p := propose(t, s, a, submit(t, s, a, "parallel-submit"))
	a.User = "approver"
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.Decide(context.Background(), a, p.ID, p.Binding, "approved", id())
			if e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	var n int
	_ = s.DB.QueryRow(context.Background(), "SELECT count(*) FROM approvals WHERE org_id=$1", a.Org).Scan(&n)
	if n != 1 {
		t.Fatalf("approval rows=%d", n)
	}
}
func TestOrderChangeInvalidatesApproval(t *testing.T) {
	s, a := fixture(t)
	p := propose(t, s, a, submit(t, s, a, "stale-order-case"))
	admin := a
	admin.User = "administrator"
	if _, e := s.ChangeOrder(context.Background(), admin, "PF-1041"); e != nil {
		t.Fatal(e)
	}
	approve(t, s, a, p)
	v, e := s.Execute(context.Background(), a.Org, p.ID)
	if e != nil || obj(v)["status"] != "invalidated" {
		t.Fatal(v, e)
	}
	var n int
	_ = s.DB.QueryRow(context.Background(), "SELECT count(*) FROM executions WHERE org_id=$1", a.Org).Scan(&n)
	if n != 0 {
		t.Fatal("stale approval executed")
	}
}
func TestPolicyChangeInvalidatesApproval(t *testing.T) {
	s, a := fixture(t)
	p := propose(t, s, a, submit(t, s, a, "stale-policy-case"))
	approve(t, s, a, p)
	admin := a
	admin.User = "administrator"
	policy := defaultPolicy()
	policy.AllowCancellation = false
	if _, e := s.SetPolicy(context.Background(), admin, policy); e != nil {
		t.Fatal(e)
	}
	v, e := s.Execute(context.Background(), a.Org, p.ID)
	if e != nil || obj(v)["status"] != "invalidated" {
		t.Fatal(v, e)
	}
}
func TestAuthorizationBoundaries(t *testing.T) {
	s, a := fixture(t)
	p := propose(t, s, a, submit(t, s, a, "security-case"))
	if _, e := s.Decide(context.Background(), a, p.ID, p.Binding, "approved", "operator-approval"); category(e) != "FORBIDDEN" {
		t.Fatal("operator granted approval", e)
	}
	_, b, e := s.NewSandbox(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	b.User = "approver"
	if _, e = s.Decide(context.Background(), b, p.ID, p.Binding, "approved", "cross-org-approval"); category(e) != "NOT_FOUND" {
		t.Fatal("cross-org access", e)
	}
	if _, e = s.Tool(context.Background(), ToolInput{b.Org, p.CaseID, "cross-org-tool", "get_order", ToolArgs{}}); category(e) != "NOT_FOUND" {
		t.Fatal("cross-org tool", e)
	}
	a.User = "approver"
	if _, e = s.Decide(context.Background(), a, p.ID, "wrong-binding", "approved", "tampered-approval"); category(e) != "CONFLICT" {
		t.Fatal("changed binding accepted", e)
	}
	_, _ = s.DB.Exec(context.Background(), "UPDATE organizations SET expires_at=now() WHERE id=$1", b.Org)
}
func TestExpiredRejectedAndRevokedApproval(t *testing.T) {
	for _, scenario := range []string{"expired", "rejected", "revoked"} {
		t.Run(scenario, func(t *testing.T) {
			s, a := fixture(t)
			p := propose(t, s, a, submit(t, s, a, "approval-scenario"))
			ap := a
			ap.User = "approver"
			decision := "approved"
			if scenario == "rejected" {
				decision = "rejected"
			}
			if _, e := s.Decide(context.Background(), ap, p.ID, p.Binding, decision, "scenario-approval"); e != nil {
				t.Fatal(e)
			}
			if scenario == "expired" {
				_, _ = s.DB.Exec(context.Background(), "UPDATE approvals SET expires_at=$2 WHERE org_id=$1", a.Org, time.Now().Add(-time.Second))
			}
			if scenario == "revoked" {
				_, _ = s.DB.Exec(context.Background(), "UPDATE users SET active=false WHERE org_id=$1 AND id='approver'", a.Org)
			}
			v, e := s.Execute(context.Background(), a.Org, p.ID)
			if e != nil || obj(v)["status"] == "executed" {
				t.Fatal(v, e)
			}
		})
	}
}
func TestEvidenceAndToolPermissions(t *testing.T) {
	s, a := fixture(t)
	c := submit(t, s, a, "tools-security-case")
	if _, e := s.Tool(context.Background(), ToolInput{a.Org, c.ID, "missing-evidence", "propose_order_cancellation", ToolArgs{OrderVersion: 1, PolicyVersion: 1}}); category(e) != "INSUFFICIENT_EVIDENCE" {
		t.Fatal("unsupported evidence accepted", e)
	}
	for _, name := range []string{"sql", "shell", "grant_permission", "refund"} {
		if _, e := s.Tool(context.Background(), ToolInput{a.Org, c.ID, "forbidden-" + name, name, ToolArgs{}}); category(e) != "FORBIDDEN" {
			t.Fatal("tool permissions bypassed", name, e)
		}
	}
}
func TestWebhookSignature(t *testing.T) {
	if validWebhook([]byte("{}"), "invalid", "test-secret") {
		t.Fatal("invalid signature accepted")
	}
	if validWebhook([]byte("{}"), "", "") {
		t.Fatal("missing secret accepted")
	}
}
