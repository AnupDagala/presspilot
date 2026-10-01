package main

import (
	"context"
	"strconv"
	"testing"
)

func TestPersistedProviderBudgets(t *testing.T) {
	t.Setenv("MODEL_MAX_USD_PER_MTOK", "10")
	t.Setenv("MODEL_DAILY_USD_LIMIT", "10000")
	t.Setenv("MODEL_DAILY_CALL_LIMIT", "100000")
	t.Setenv("MODEL_DAILY_TOKEN_LIMIT", "100000000")
	s, a := fixture(t)
	ctx := context.Background()
	_, _ = s.DB.Exec(ctx, "UPDATE organizations SET sandbox=false WHERE id=$1", a.Org)
	a.Sandbox = false
	v, e := s.Submit(ctx, a, "PF-1041", "Cancel", "live", "live-budget-case", Object{})
	if e != nil {
		t.Fatal(e)
	}
	cid := obj(v)["id"].(string)
	input := ToolInput{a.Org, cid, "reserve-model-test", "reserve_model_turn", ToolArgs{Config: Object{"reserve": float64(10000), "attempt": float64(1)}}}
	if _, e = s.Tool(ctx, input); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Tool(ctx, input); e != nil {
		t.Fatal("same reservation must reconcile", e)
	}
	input.Args.Config["attempt"] = float64(2)
	if _, e = s.Tool(ctx, input); category(e) != "CONFLICT" {
		t.Fatal("ambiguous delivery reattempt reused reservation", e)
	}
	input.CallID = "reserve-second-test"
	if _, e = s.Tool(ctx, input); category(e) != "LIMIT" {
		t.Fatal("token limit bypassed", e)
	}
	t.Setenv("MODEL_DAILY_CALL_LIMIT", "1")
	input.Args.Config["reserve"] = float64(1)
	if _, e = s.Tool(ctx, input); category(e) != "LIMIT" {
		t.Fatal("daily call limit bypassed", e)
	}
	t.Setenv("MODEL_DAILY_USD_LIMIT", "0.000000001")
	if _, e = s.Tool(ctx, input); category(e) != "LIMIT" {
		t.Fatal("spending bound bypassed", e)
	}
	t.Setenv("MODEL_MAX_USD_PER_MTOK", "0")
	if _, e = s.Tool(ctx, input); category(e) != "PERMANENT" {
		t.Fatal("missing pricing assumptions accepted", e)
	}
}

func TestGlobalReservationLimitAcrossOrganizations(t *testing.T) {
	t.Setenv("MODEL_MAX_USD_PER_MTOK", "10")
	t.Setenv("MODEL_DAILY_USD_LIMIT", "10000")
	t.Setenv("MODEL_DAILY_TOKEN_LIMIT", "100000000")
	s, a := fixture(t)
	_, b := fixture(t)
	ctx := context.Background()
	var current int
	if e := s.DB.QueryRow(ctx, "SELECT count(*) FROM model_turns WHERE created_at>now()-interval '24 hours'").Scan(&current); e != nil {
		t.Fatal(e)
	}
	t.Setenv("MODEL_DAILY_CALL_LIMIT", strconv.Itoa(current+1))
	inputs := []ToolInput{}
	for _, actor := range []Actor{a, b} {
		_, e := s.DB.Exec(ctx, "UPDATE organizations SET sandbox=false WHERE id=$1", actor.Org)
		if e != nil {
			t.Fatal(e)
		}
		actor.Sandbox = false
		c, e := s.Submit(ctx, actor, "PF-1041", "Cancel", "live", "global-budget-case", Object{})
		if e != nil {
			t.Fatal(e)
		}
		inputs = append(inputs, ToolInput{actor.Org, obj(c)["id"].(string), "global-reservation", "reserve_model_turn", ToolArgs{Config: Object{"reserve": float64(1)}}})
	}
	results := make(chan error, 2)
	for _, input := range inputs {
		go func(in ToolInput) { _, e := s.Tool(ctx, in); results <- e }(input)
	}
	passed, limited := 0, 0
	for range inputs {
		e := <-results
		if e == nil {
			passed++
		} else if category(e) == "LIMIT" {
			limited++
		} else {
			t.Fatal(e)
		}
	}
	if passed != 1 || limited != 1 {
		t.Fatal("cross-organization global budget race", passed, limited)
	}
}
func TestSandboxCannotReservePaidCalls(t *testing.T) {
	s, a := fixture(t)
	c := submit(t, s, a, "sandbox-budget-case")
	_, e := s.Tool(context.Background(), ToolInput{a.Org, c.ID, "sandbox-live-reserve", "reserve_model_turn", ToolArgs{Config: Object{"reserve": float64(100)}}})
	if category(e) != "FORBIDDEN" {
		t.Fatal("sandbox could reserve paid calls", e)
	}
}

func TestPersistedModelResponseRecovery(t *testing.T) {
	s, a := fixture(t)
	c := submit(t, s, a, "response-recovery-case")
	ctx := context.Background()
	config := Object{"mode": "mocked", "model": "fixture", "summary": "Recorded response", "calls": []any{}, "usage": nil}
	if _, e := s.Tool(ctx, ToolInput{a.Org, c.ID, "observation-0", "record_model_response", ToolArgs{Config: config}}); e != nil {
		t.Fatal(e)
	}
	observationID := "observation-0"
	request := ToolInput{Org: a.Org, CaseID: c.ID, CallID: "resume-attempt-2", Name: "get_model_response", Args: ToolArgs{Config: Object{"key": observationID}}}
	v, e := s.Tool(ctx, request)
	if e != nil {
		t.Fatal(e)
	}
	if digest(obj(v)["response"]) != digest(config) {
		t.Fatal("stored model response was not recovered exactly")
	}
	if _, e = s.Tool(ctx, ToolInput{a.Org, c.ID, "observation-0", "record_model_response", ToolArgs{Config: Object{"model": "changed"}}}); category(e) != "CONFLICT" {
		t.Fatal("response identity allowed replacement", e)
	}
}

func TestAdministratorReviewRecovery(t *testing.T) {
	s, a := fixture(t)
	ctx := context.Background()
	c := submit(t, s, a, "admin-review-case")
	p := propose(t, s, a, c)
	ap := a
	ap.User = "approver"
	if _, e := s.Decide(ctx, ap, p.ID, p.Binding, "approved", "admin-recovery-approval"); e != nil {
		t.Fatal(e)
	}
	for _, actor := range []Actor{a, ap} {
		if _, e := s.RequestReview(ctx, actor, c.ID, "Review needed"); category(e) != "FORBIDDEN" {
			t.Fatal("non-administrator recovery allowed", e)
		}
	}
	if _, e := s.DB.Exec(ctx, "UPDATE users SET active=false WHERE org_id=$1 AND id=$2", a.Org, a.User); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Tool(ctx, ToolInput{a.Org, c.ID, "revoked-service-escalation", "escalate_case", ToolArgs{Reason: "Review needed"}}); category(e) != "FORBIDDEN" {
		t.Fatal("revoked requester authorization bypassed", e)
	}
	admin := a
	admin.User = "administrator"
	if _, e := s.RequestReview(ctx, admin, c.ID, "Requester revoked; administrator investigation"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Execute(ctx, a.Org, p.ID); e != nil {
		t.Fatal(e)
	}
	var effects int
	var status, state string
	if e := s.DB.QueryRow(ctx, "SELECT c.status,o.state,(SELECT count(*) FROM executions WHERE org_id=$1 AND case_id=$2) FROM cases c JOIN orders o ON (o.org_id,o.id)=(c.org_id,c.order_id) WHERE c.org_id=$1 AND c.id=$2", a.Org, c.ID).Scan(&status, &state, &effects); e != nil {
		t.Fatal(e)
	}
	if status != "escalated" || state != "queued" || effects != 0 {
		t.Fatal("administrator recovery did not withdraw business action")
	}
}
func TestEscalatedCaseCannotExecuteEarlierApproval(t *testing.T) {
	s, a := fixture(t)
	ctx := context.Background()
	c := submit(t, s, a, "escalated-approval-case")
	p := propose(t, s, a, c)
	ap := a
	ap.User = "approver"
	if _, e := s.Decide(ctx, ap, p.ID, p.Binding, "approved", "escalated-case-approval"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Tool(ctx, ToolInput{a.Org, c.ID, "escalated-case-stop", "escalate_case", ToolArgs{Reason: "Contradictory evidence; review required"}}); e != nil {
		t.Fatal(e)
	}
	v, e := s.Execute(ctx, a.Org, p.ID)
	if e != nil || obj(v)["status"] != "rejected" {
		t.Fatal("escalated case executed an earlier approval", e)
	}
	var effects int
	if e = s.DB.QueryRow(ctx, "SELECT count(*) FROM executions WHERE org_id=$1 AND case_id=$2", a.Org, c.ID).Scan(&effects); e != nil || effects != 0 {
		t.Fatal("escalated case produced a business effect", e)
	}
}
