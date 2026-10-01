package main

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
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
		if err = tx.QueryRow(ctx, "SELECT count(*) FROM tool_calls WHERE org_id=$1 AND case_id=$2 AND name NOT IN ('get_case','record_run')", in.Org, in.CaseID).Scan(&calls); err != nil {
			return nil, err
		}
		if calls >= p.MaxTools && in.Name != "get_case" && in.Name != "escalate_case" && in.Name != "record_run" {
			return nil, fail("LIMIT", "Tool call budget exhausted")
		}
		if in.Name != "get_case" && in.Name != "get_order_history" && in.Name != "record_run" && (c.Status == "completed" || c.Status == "escalated") {
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
			rows, e := tx.Query(ctx, "SELECT kind,data FROM audit_events WHERE org_id=$1 AND (data->>'order'=$2 OR case_id=$3) ORDER BY seq DESC LIMIT 20", in.Org, o.ID, c.ID)
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
