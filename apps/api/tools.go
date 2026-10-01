package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"math"
	"strconv"
	"time"
)

type ToolInput struct {
	Org    string   `json:"org"`
	CaseID string   `json:"caseId"`
	CallID string   `json:"callId"`
	Name   string   `json:"name"`
	Args   ToolArgs `json:"args"`
}
type ToolArgs struct {
	Action        string `json:"action,omitempty"`
	Arguments     Object `json:"arguments,omitempty"`
	OrderVersion  int    `json:"orderVersion,omitempty"`
	PolicyVersion int    `json:"policyVersion,omitempty"`
	Reason        string `json:"reason,omitempty"`
	ProposalID    string `json:"proposalId,omitempty"`
	Config        Object `json:"config,omitempty"`
}

func (s *Store) Tool(ctx context.Context, in ToolInput) (any, error) {
	if in.Name == "execute_approved_action" {
		var boundCase string
		if err := s.DB.QueryRow(ctx, "SELECT case_id FROM proposals WHERE org_id=$1 AND id=$2", in.Org, in.Args.ProposalID).Scan(&boundCase); err != nil || boundCase != in.CaseID {
			return nil, fail("NOT_FOUND", "Case-bound proposal not found")
		}
		return s.Execute(ctx, in.Org, in.Args.ProposalID)
	}
	return s.transaction(ctx, in.Org, func(tx pgx.Tx) (any, error) {
		c, err := loadCase(ctx, tx, in.Org, in.CaseID)
		if err != nil {
			return nil, err
		}
		a := Actor{Org: in.Org, User: c.Requester}
		if err = authorize(ctx, tx, a, "operator", "administrator"); err != nil {
			return nil, err
		}
		var priorIn Object
		var priorOut Object
		err = tx.QueryRow(ctx, "SELECT input,output FROM tool_calls WHERE org_id=$1 AND case_id=$2 AND call_id=$3", in.Org, in.CaseID, in.CallID).Scan(&priorIn, &priorOut)
		if err == nil {
			if digest(priorIn) != digest(obj(in)) {
				return nil, fail("CONFLICT", "Tool call identity reused with different arguments")
			}
			return priorOut, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		p, err := loadPolicy(ctx, tx, in.Org)
		if err != nil {
			return nil, err
		}
		var calls int
		control := in.Name == "get_case" || in.Name == "record_run" || in.Name == "record_model_response" || in.Name == "reserve_model_turn" || in.Name == "get_model_response"
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM tool_calls WHERE org_id=$1 AND case_id=$2 AND name NOT IN ('get_case','record_run','record_model_response','reserve_model_turn','get_model_response')", in.Org, in.CaseID).Scan(&calls); err != nil {
			return nil, err
		}
		if calls >= p.MaxTools && !control && in.Name != "escalate_case" {
			return nil, fail("LIMIT", "Tool call budget exhausted")
		}
		if !control && in.Name != "get_order_history" && (c.Status == "completed" || c.Status == "escalated") {
			return nil, fail("CONFLICT", "Case already terminal")
		}
		o, err := loadOrder(ctx, tx, in.Org, c.OrderID)
		if err != nil {
			return nil, err
		}
		var result any
		switch in.Name {
		case "get_case":
			var latest *Proposal
			var pid string
			err = tx.QueryRow(ctx, "SELECT id FROM proposals WHERE org_id=$1 AND case_id=$2 ORDER BY created_at DESC LIMIT 1", in.Org, c.ID).Scan(&pid)
			if err == nil {
				proposal, e := loadProposal(ctx, tx, in.Org, pid)
				if e != nil {
					return nil, e
				}
				latest = &proposal
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			result = Object{"case": c, "proposal": latest, "policy": p}
		case "get_order":
			result = o
		case "get_applicable_policy":
			result = p
		case "get_order_history":
			rows, e := tx.Query(ctx, "SELECT kind,data FROM audit_events WHERE org_id=$1 AND (data->>'order'=$2 OR data->'order'->>'id'=$2 OR data->>'orderId'=$2 OR case_id=$3) ORDER BY seq DESC LIMIT 20", in.Org, o.ID, c.ID)
			if e != nil {
				return nil, e
			}
			items := []Object{}
			for rows.Next() {
				var kind string
				var data Object
				if e = rows.Scan(&kind, &data); e != nil {
					rows.Close()
					return nil, e
				}
				items = append(items, Object{"kind": kind, "data": data})
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, e
			}
			result = Object{"events": items}
		case "check_action_eligibility":
			e := eligible(o, p, in.Args.Action, in.Args.Arguments)
			reason := "Action permitted before production with explicit approval"
			if e != nil {
				reason = e.Error()
			}
			result = Object{"eligible": e == nil, "reason": reason, "orderVersion": o.Version, "policyVersion": p.Version}
		case "propose_order_cancellation", "propose_order_modification":
			action := "cancel"
			if in.Name == "propose_order_modification" {
				action = "modify"
			}
			args := in.Args.Arguments
			if args == nil {
				args = Object{}
			}
			if err = eligible(o, p, action, args); err != nil {
				return nil, err
			}
			if in.Args.OrderVersion != o.Version || in.Args.PolicyVersion != p.Version {
				return nil, fail("STALE", "Retrieved evidence changed before proposal")
			}
			var evidenceCount int
			if err = tx.QueryRow(ctx, "SELECT count(DISTINCT name) FROM tool_calls WHERE org_id=$1 AND case_id=$2 AND ((name='get_order' AND (output->>'version')::integer=$3) OR (name='get_applicable_policy' AND (output->>'version')::integer=$4))", in.Org, c.ID, o.Version, p.Version).Scan(&evidenceCount); err != nil {
				return nil, err
			}
			if evidenceCount != 2 {
				return nil, fail("INSUFFICIENT_EVIDENCE", "Retrieve the current order and policy before proposing")
			}
			proposal := Proposal{id(), c.ID, o.ID, o.Version, p.Version, action, args, Object{"order": o, "policy": p, "sources": []string{"get_order", "get_applicable_policy"}}, "", "pending", time.Now().UTC().Add(time.Duration(p.ApprovalSeconds) * time.Second)}
			proposal.Binding = binding(in.Org, proposal)
			_, err = tx.Exec(ctx, "INSERT INTO proposals(org_id,id,case_id,order_id,order_version,policy_version,action,arguments,evidence,binding,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(org_id,case_id,binding) DO NOTHING", in.Org, proposal.ID, c.ID, o.ID, o.Version, p.Version, action, args, proposal.Evidence, proposal.Binding, proposal.ExpiresAt)
			if err != nil {
				return nil, err
			}
			if err = tx.QueryRow(ctx, "SELECT id FROM proposals WHERE org_id=$1 AND case_id=$2 AND binding=$3", in.Org, c.ID, proposal.Binding).Scan(&proposal.ID); err != nil {
				return nil, err
			}
			proposal, err = loadProposal(ctx, tx, in.Org, proposal.ID)
			if err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, "UPDATE cases SET status='awaiting_approval',summary='Evidence supports the proposed action. Waiting for authorized approval.' WHERE org_id=$1 AND id=$2", in.Org, c.ID); err != nil {
				return nil, err
			}
			if err = audit(ctx, tx, Actor{Org: in.Org, User: "agent"}, c.ID, "action_proposed", proposal); err != nil {
				return nil, err
			}
			result = proposal
		case "escalate_case":
			if len(in.Args.Reason) == 0 || len(in.Args.Reason) > 1000 {
				return nil, fail("INVALID", "Escalation requires a bounded reason")
			}
			if _, err = tx.Exec(ctx, "UPDATE cases SET status='escalated',summary=$3 WHERE org_id=$1 AND id=$2 AND status<>'completed'", in.Org, c.ID, in.Args.Reason); err != nil {
				return nil, err
			}
			if err = audit(ctx, tx, Actor{Org: in.Org, User: "agent"}, c.ID, "case_escalated", Object{"reason": in.Args.Reason}); err != nil {
				return nil, err
			}
			result = Object{"status": "escalated", "reason": in.Args.Reason}
		case "reserve_model_turn":
			var sandbox bool
			if err = tx.QueryRow(ctx, "SELECT sandbox FROM organizations WHERE id=$1", in.Org).Scan(&sandbox); err != nil {
				return nil, err
			}
			if c.Mode != "live" || sandbox {
				return nil, fail("FORBIDDEN", "Only private live cases may reserve provider calls")
			}
			amount, ok := in.Args.Config["reserve"].(float64)
			if !ok || amount < 1 || amount > 20000 || amount != float64(int(amount)) {
				return nil, fail("INVALID", "Invalid token reservation")
			}
			var turns, charged, dailyTurns, dailyTokens int
			if err = tx.QueryRow(ctx, "SELECT count(*),COALESCE(sum(COALESCE(used,reserved)),0) FROM model_turns WHERE org_id=$1 AND case_id=$2", in.Org, c.ID).Scan(&turns, &charged); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(73032)"); err != nil {
				return nil, err
			}
			if err = tx.QueryRow(ctx, "SELECT count(*),COALESCE(sum(COALESCE(used,reserved)),0) FROM model_turns WHERE created_at>now()-interval '24 hours'").Scan(&dailyTurns, &dailyTokens); err != nil {
				return nil, err
			}
			dayCalls, _ := strconv.Atoi(env("MODEL_DAILY_CALL_LIMIT", "20"))
			dayTokens, _ := strconv.Atoi(env("MODEL_DAILY_TOKEN_LIMIT", "100000"))
			maxRate, rateErr := strconv.ParseFloat(env("MODEL_MAX_USD_PER_MTOK", "0"), 64)
			usdLimit, limitErr := strconv.ParseFloat(env("MODEL_DAILY_USD_LIMIT", "0"), 64)
			if rateErr != nil || limitErr != nil || math.IsNaN(maxRate) || math.IsInf(maxRate, 0) || math.IsNaN(usdLimit) || math.IsInf(usdLimit, 0) || maxRate <= 0 || usdLimit <= 0 {
				return nil, fail("PERMANENT", "Configure an explicit provider pricing upper bound and daily spending limit before live execution")
			}
			if float64(dailyTokens+int(amount))*maxRate/1000000 > usdLimit {
				return nil, fail("LIMIT", "Daily estimated provider spending bound exhausted")
			}
			if turns >= p.MaxTurns || charged+int(amount) > p.MaxTokens || dailyTurns >= dayCalls || dailyTokens+int(amount) > dayTokens {
				return nil, fail("LIMIT", "Persisted model call or token budget exhausted")
			}
			pricedConfig := Object{"request": in.Args.Config, "maxUSDPerMillionTokens": maxRate, "dailyUSDLimit": usdLimit}
			if _, err = tx.Exec(ctx, "INSERT INTO model_turns(org_id,case_id,turn_key,reserved,configuration) VALUES($1,$2,$3,$4,$5)", in.Org, c.ID, in.CallID, int(amount), pricedConfig); err != nil {
				return nil, err
			}
			result = Object{"reserved": amount, "key": in.CallID}
		case "record_model_response":
			if c.Mode == "live" {
				usage, ok := in.Args.Config["usage"].(map[string]any)
				if !ok {
					return nil, fail("INVALID", "Live provider usage must be reported")
				}
				input, iok := usage["input"].(float64)
				output, ook := usage["output"].(float64)
				key, kok := in.Args.Config["reservationKey"].(string)
				if !iok || !ook || !kok || input < 0 || output < 0 || input+output > 200000 {
					return nil, fail("INVALID", "Invalid provider usage")
				}
				if _, err = tx.Exec(ctx, "UPDATE model_turns SET used=$4 WHERE org_id=$1 AND case_id=$2 AND turn_key=$3", in.Org, c.ID, key, int(input+output)); err != nil {
					return nil, err
				}
			}
			result = Object{"recorded": true}
		case "get_model_response":
			key, ok := in.Args.Config["key"].(string)
			if !ok || len(key) > 128 {
				return nil, fail("INVALID", "Bounded observation key required")
			}
			var observation Object
			err = tx.QueryRow(ctx, "SELECT input->'args'->'config' FROM tool_calls WHERE org_id=$1 AND case_id=$2 AND call_id=$3 AND name='record_model_response'", in.Org, c.ID, key).Scan(&observation)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			result = Object{"response": observation}
		case "record_run":
			if _, err = tx.Exec(ctx, "UPDATE cases SET config=$3 WHERE org_id=$1 AND id=$2", in.Org, c.ID, in.Args.Config); err != nil {
				return nil, err
			}
			result = Object{"recorded": true}
		default:
			return nil, fail("FORBIDDEN", "Tool is not in the allowlist")
		}
		out := obj(result)
		if _, err = tx.Exec(ctx, "INSERT INTO tool_calls(org_id,case_id,call_id,name,input,output) VALUES($1,$2,$3,$4,$5,$6)", in.Org, c.ID, in.CallID, in.Name, obj(in), out); err != nil {
			return nil, err
		}
		return out, nil
	})
}
