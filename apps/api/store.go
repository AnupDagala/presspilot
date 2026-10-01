package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

type Store struct{ DB *pgxpool.Pool }

func (s *Store) transaction(ctx context.Context, org string, fn func(pgx.Tx) (any, error)) (any, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var found string
	err = tx.QueryRow(ctx, "SELECT id FROM organizations WHERE id=$1 AND (expires_at IS NULL OR expires_at>now()) FOR UPDATE", org).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fail("NOT_FOUND", "Organization not available")
	}
	if err != nil {
		return nil, err
	}
	result, err := fn(tx)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}
func authorize(ctx context.Context, tx pgx.Tx, a Actor, roles ...string) error {
	var role string
	if err := tx.QueryRow(ctx, "SELECT role FROM users WHERE org_id=$1 AND id=$2 AND active", a.Org, a.User).Scan(&role); err != nil {
		return fail("FORBIDDEN", "Active membership required")
	}
	if len(roles) == 0 {
		return nil
	}
	for _, r := range roles {
		if role == r {
			return nil
		}
	}
	return fail("FORBIDDEN", "Role does not permit this operation")
}
func audit(ctx context.Context, tx pgx.Tx, a Actor, c, kind string, data any) error {
	_, err := tx.Exec(ctx, "INSERT INTO audit_events(org_id,case_id,actor,kind,data) VALUES($1,NULLIF($2,''),$3,$4,$5)", a.Org, c, a.User, kind, obj(data))
	return err
}
func loadOrder(ctx context.Context, tx pgx.Tx, org, oid string) (Order, error) {
	var o Order
	err := tx.QueryRow(ctx, "SELECT id,version,state,attributes FROM orders WHERE org_id=$1 AND id=$2", org, oid).Scan(&o.ID, &o.Version, &o.State, &o.Attributes)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, fail("NOT_FOUND", "Order not found")
	}
	return o, err
}
func loadPolicy(ctx context.Context, tx pgx.Tx, org string) (Policy, error) {
	var p Policy
	var b []byte
	err := tx.QueryRow(ctx, "SELECT document FROM policies WHERE org_id=$1 ORDER BY version DESC LIMIT 1", org).Scan(&b)
	if err != nil {
		return p, err
	}
	err = json.Unmarshal(b, &p)
	return p, err
}
func loadCase(ctx context.Context, tx pgx.Tx, org, cid string) (Case, error) {
	var c Case
	err := tx.QueryRow(ctx, "SELECT id,order_id,requester_id,message,mode,status,summary,config FROM cases WHERE org_id=$1 AND id=$2", org, cid).Scan(&c.ID, &c.OrderID, &c.Requester, &c.Message, &c.Mode, &c.Status, &c.Summary, &c.Config)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, fail("NOT_FOUND", "Case not found")
	}
	return c, err
}
func loadProposal(ctx context.Context, tx pgx.Tx, org, pid string) (Proposal, error) {
	var p Proposal
	err := tx.QueryRow(ctx, "SELECT id,case_id,order_id,order_version,policy_version,action,arguments,evidence,binding,status,expires_at FROM proposals WHERE org_id=$1 AND id=$2", org, pid).Scan(&p.ID, &p.CaseID, &p.OrderID, &p.OrderVersion, &p.PolicyVersion, &p.Action, &p.Arguments, &p.Evidence, &p.Binding, &p.Status, &p.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, fail("NOT_FOUND", "Proposal not found")
	}
	return p, err
}
func once(ctx context.Context, tx pgx.Tx, org, key string, input any, fn func() (any, error)) (any, error) {
	if len(key) < 8 || len(key) > 128 {
		return nil, fail("INVALID", "Idempotency key must be 8–128 characters")
	}
	hash := digest(input)
	var prior string
	var result any
	err := tx.QueryRow(ctx, "SELECT digest,result FROM requests WHERE org_id=$1 AND key=$2", org, key).Scan(&prior, &result)
	if err == nil {
		if prior != hash {
			return nil, fail("CONFLICT", "Idempotency key was reused with different arguments")
		}
		return result, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	result, err = fn()
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, "INSERT INTO requests VALUES($1,$2,$3,$4)", org, key, hash, obj(result))
	return result, err
}
func (s *Store) Submit(ctx context.Context, a Actor, oid, message, mode, key string, config Object) (any, error) {
	return s.transaction(ctx, a.Org, func(tx pgx.Tx) (any, error) {
		if err := authorize(ctx, tx, a, "operator", "administrator"); err != nil {
			return nil, err
		}
		return once(ctx, tx, a.Org, key, Object{"type": "submit", "actor": a.User, "order": oid, "message": message, "mode": mode}, func() (any, error) {
			if len(message) == 0 || len(message) > 4000 {
				return nil, fail("INVALID", "Request must be 1–4000 bytes")
			}
			if mode != "baseline" && mode != "mocked" && mode != "live" {
				return nil, fail("INVALID", "Unknown operating mode")
			}
			if a.Sandbox && mode == "live" {
				return nil, fail("FORBIDDEN", "Public sandboxes do not permit paid provider calls")
			}
			if _, err := loadOrder(ctx, tx, a.Org, oid); err != nil {
				return nil, err
			}
			var n int
			if err := tx.QueryRow(ctx, "SELECT count(*) FROM cases WHERE org_id=$1", a.Org).Scan(&n); err != nil {
				return nil, err
			}
			if a.Sandbox && n >= 20 {
				return nil, fail("LIMIT", "Sandbox case limit reached")
			}
			c := Case{id(), oid, a.User, message, mode, "investigating", "", config}
			_, err := tx.Exec(ctx, "INSERT INTO cases(org_id,id,order_id,requester_id,message,mode,config) VALUES($1,$2,$3,$4,$5,$6,$7)", a.Org, c.ID, oid, a.User, message, mode, config)
			if err != nil {
				return nil, err
			}
			if err = audit(ctx, tx, a, c.ID, "case_submitted", Object{"mode": mode, "order": oid}); err != nil {
				return nil, err
			}
			return c, nil
		})
	})
}
func (s *Store) Decide(ctx context.Context, a Actor, pid, hash, decision, key string) (any, error) {
	return s.transaction(ctx, a.Org, func(tx pgx.Tx) (any, error) {
		if err := authorize(ctx, tx, a, "approver", "administrator"); err != nil {
			return nil, err
		}
		return once(ctx, tx, a.Org, key, Object{"type": "approval", "actor": a.User, "proposal": pid, "binding": hash, "decision": decision}, func() (any, error) {
			p, err := loadProposal(ctx, tx, a.Org, pid)
			if err != nil {
				return nil, err
			}
			if decision != "approved" && decision != "rejected" {
				return nil, fail("INVALID", "Unknown approval decision")
			}
			if p.Binding != hash || binding(a.Org, p) != hash {
				return nil, fail("CONFLICT", "Approval binding does not match the exact proposal")
			}
			if p.Status != "pending" {
				return nil, fail("CONFLICT", "Proposal no longer pending")
			}
			if time.Now().After(p.ExpiresAt) {
				return nil, fail("EXPIRED", "Approval window has expired")
			}
			_, err = tx.Exec(ctx, "INSERT INTO approvals(org_id,proposal_id,user_id,binding,decision,expires_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", a.Org, pid, a.User, hash, decision, p.ExpiresAt)
			if err != nil {
				return nil, err
			}
			var recorded string
			if err = tx.QueryRow(ctx, "SELECT decision FROM approvals WHERE org_id=$1 AND proposal_id=$2", a.Org, pid).Scan(&recorded); err != nil {
				return nil, err
			}
			if recorded != decision {
				return nil, fail("CONFLICT", "A different decision has already been recorded")
			}
			if err = audit(ctx, tx, a, p.CaseID, "approval_recorded", Object{"proposal": pid, "decision": decision, "binding": hash}); err != nil {
				return nil, err
			}
			return Object{"decision": recorded, "proposal": pid}, nil
		})
	})
}
func invalidate(ctx context.Context, tx pgx.Tx, a Actor, p Proposal, reason string) (any, error) {
	if _, err := tx.Exec(ctx, "UPDATE proposals SET status='invalidated' WHERE org_id=$1 AND id=$2", a.Org, p.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "UPDATE cases SET status='investigating',summary=$3 WHERE org_id=$1 AND id=$2", a.Org, p.CaseID, reason); err != nil {
		return nil, err
	}
	if err := audit(ctx, tx, a, p.CaseID, "proposal_invalidated", Object{"proposal": p.ID, "reason": reason}); err != nil {
		return nil, err
	}
	return Object{"status": "invalidated", "reason": reason}, nil
}
func (s *Store) Execute(ctx context.Context, org, pid string) (any, error) {
	return s.transaction(ctx, org, func(tx pgx.Tx) (any, error) {
		a := Actor{Org: org, User: "agent"}
		p, err := loadProposal(ctx, tx, org, pid)
		if err != nil {
			return nil, err
		}
		if p.Status == "executed" {
			var result Object
			err = tx.QueryRow(ctx, "SELECT result FROM executions WHERE org_id=$1 AND proposal_id=$2", org, pid).Scan(&result)
			return result, err
		}
		if p.Status != "pending" {
			return Object{"status": p.Status}, nil
		}
		var approver, hash, decision string
		var expires time.Time
		err = tx.QueryRow(ctx, "SELECT user_id,binding,decision,expires_at FROM approvals WHERE org_id=$1 AND proposal_id=$2", org, pid).Scan(&approver, &hash, &decision, &expires)
		if errors.Is(err, pgx.ErrNoRows) {
			return Object{"status": "waiting"}, nil
		}
		if err != nil {
			return nil, err
		}
		if decision == "rejected" {
			if _, err = tx.Exec(ctx, "UPDATE proposals SET status='rejected' WHERE org_id=$1 AND id=$2", org, pid); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, "UPDATE cases SET status='escalated',summary='Action rejected by approver; human review required' WHERE org_id=$1 AND id=$2", org, p.CaseID); err != nil {
				return nil, err
			}
			if err = audit(ctx, tx, a, p.CaseID, "action_rejected", Object{"proposal": pid}); err != nil {
				return nil, err
			}
			return Object{"status": "rejected"}, nil
		}
		c, err := loadCase(ctx, tx, org, p.CaseID)
		if err != nil {
			return nil, err
		}
		if err = authorize(ctx, tx, Actor{Org: org, User: approver}, "approver", "administrator"); err != nil {
			return invalidate(ctx, tx, a, p, "Approver authorization changed")
		}
		if err = authorize(ctx, tx, Actor{Org: org, User: c.Requester}, "operator", "administrator"); err != nil {
			return invalidate(ctx, tx, a, p, "Requester authorization changed")
		}
		o, err := loadOrder(ctx, tx, org, p.OrderID)
		if err != nil {
			return nil, err
		}
		policy, err := loadPolicy(ctx, tx, org)
		if err != nil {
			return nil, err
		}
		if hash != p.Binding || binding(org, p) != hash {
			return invalidate(ctx, tx, a, p, "Approval action binding changed")
		}
		if time.Now().After(expires) || time.Now().After(p.ExpiresAt) {
			return invalidate(ctx, tx, a, p, "Approval expired")
		}
		if o.Version != p.OrderVersion || policy.Version != p.PolicyVersion {
			return invalidate(ctx, tx, a, p, "Order or policy evidence changed; reassessment required")
		}
		if err = eligible(o, policy, p.Action, p.Arguments); err != nil {
			return invalidate(ctx, tx, a, p, "Action eligibility changed")
		}
		if p.Action == "cancel" {
			o.State = "cancelled"
		} else {
			for k, v := range p.Arguments {
				o.Attributes[k] = v
			}
		}
		tag, err := tx.Exec(ctx, "UPDATE orders SET version=version+1,state=$4,attributes=$5 WHERE org_id=$1 AND id=$2 AND version=$3 AND state='queued'", org, o.ID, o.Version, o.State, o.Attributes)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() != 1 {
			return nil, fail("CONFLICT", "Order transition lost a race")
		}
		o.Version++
		result := Object{"status": "executed", "order": o, "proposal": pid, "binding": hash}
		if _, err = tx.Exec(ctx, "INSERT INTO executions(org_id,id,proposal_id,case_id,binding,result) VALUES($1,$2,$3,$4,$5,$6)", org, id(), pid, p.CaseID, hash, result); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, "UPDATE proposals SET status='executed' WHERE org_id=$1 AND id=$2", org, pid); err != nil {
			return nil, err
		}
		if _, err = tx.Exec(ctx, "UPDATE cases SET status='completed',summary='Approved action executed and verified atomically' WHERE org_id=$1 AND id=$2", org, p.CaseID); err != nil {
			return nil, err
		}
		if err = audit(ctx, tx, a, p.CaseID, "action_executed", result); err != nil {
			return nil, err
		}
		return result, nil
	})
}
func (s *Store) ChangeOrder(ctx context.Context, a Actor, oid string) (any, error) {
	return s.transaction(ctx, a.Org, func(tx pgx.Tx) (any, error) {
		if err := authorize(ctx, tx, a, "administrator"); err != nil {
			return nil, err
		}
		if !a.Sandbox {
			return nil, fail("FORBIDDEN", "Simulated events require an isolated sandbox")
		}
		o, err := loadOrder(ctx, tx, a.Org, oid)
		if err != nil {
			return nil, err
		}
		if o.State != "queued" {
			return nil, fail("INELIGIBLE", "Only queued orders can enter production")
		}
		_, err = tx.Exec(ctx, "UPDATE orders SET state='production',version=version+1 WHERE org_id=$1 AND id=$2", a.Org, oid)
		if err != nil {
			return nil, err
		}
		if err = audit(ctx, tx, a, "", "simulated_production_started", Object{"order": oid, "version": o.Version + 1}); err != nil {
			return nil, err
		}
		return Object{"state": "production"}, nil
	})
}
func (s *Store) SetPolicy(ctx context.Context, a Actor, p Policy) (any, error) {
	return s.transaction(ctx, a.Org, func(tx pgx.Tx) (any, error) {
		if err := authorize(ctx, tx, a, "administrator"); err != nil {
			return nil, err
		}
		if err := validatePolicy(p); err != nil {
			return nil, err
		}
		old, err := loadPolicy(ctx, tx, a.Org)
		if err != nil {
			return nil, err
		}
		p.Version = old.Version + 1
		if _, err = tx.Exec(ctx, "INSERT INTO policies(org_id,version,document) VALUES($1,$2,$3)", a.Org, p.Version, obj(p)); err != nil {
			return nil, err
		}
		if err = audit(ctx, tx, a, "", "policy_version_created", p); err != nil {
			return nil, err
		}
		return p, nil
	})
}
func (s *Store) Snapshot(ctx context.Context, a Actor) (any, error) {
	return s.transaction(ctx, a.Org, func(tx pgx.Tx) (any, error) {
		if err := authorize(ctx, tx, a); err != nil {
			return nil, err
		}
		out := Object{"actor": obj(a)}
		queries := map[string]string{
			"orders":      "SELECT jsonb_build_object('id',id,'version',version,'state',state,'attributes',attributes) FROM orders WHERE org_id=$1 ORDER BY id LIMIT 50",
			"cases":       "SELECT jsonb_build_object('id',id,'orderId',order_id,'message',message,'mode',mode,'status',status,'summary',summary,'config',config,'createdAt',created_at) FROM cases WHERE org_id=$1 ORDER BY created_at DESC LIMIT 50",
			"proposals":   "SELECT jsonb_build_object('id',id,'caseId',case_id,'orderId',order_id,'orderVersion',order_version,'policyVersion',policy_version,'action',action,'arguments',arguments,'evidence',evidence,'binding',binding,'status',status,'expiresAt',expires_at) FROM proposals WHERE org_id=$1 ORDER BY created_at DESC LIMIT 100",
			"approvals":   "SELECT jsonb_build_object('proposalId',proposal_id,'userId',user_id,'decision',decision,'binding',binding,'expiresAt',expires_at) FROM approvals WHERE org_id=$1 LIMIT 100",
			"timeline":    "SELECT jsonb_build_object('seq',seq,'caseId',case_id,'actor',actor,'kind',kind,'data',data,'createdAt',created_at) FROM audit_events WHERE org_id=$1 ORDER BY seq DESC LIMIT 150",
			"tools":       "SELECT jsonb_build_object('caseId',case_id,'name',name,'input',input,'output',output,'createdAt',created_at) FROM tool_calls WHERE org_id=$1 ORDER BY created_at DESC LIMIT 100",
			"evaluations": "SELECT report FROM evaluation_runs WHERE org_id=$1 ORDER BY created_at DESC LIMIT 10",
		}
		for name, q := range queries {
			rows, err := tx.Query(ctx, q, a.Org)
			if err != nil {
				return nil, err
			}
			items := []Object{}
			for rows.Next() {
				var item Object
				if err = rows.Scan(&item); err != nil {
					rows.Close()
					return nil, err
				}
				items = append(items, item)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return nil, err
			}
			out[name] = items
		}
		p, err := loadPolicy(ctx, tx, a.Org)
		out["policy"] = p
		return out, err
	})
}
func (s *Store) NewSandbox(ctx context.Context) (string, Actor, error) {
	org, token := id(), id()+id()
	a := Actor{org, "operator", "operator", true}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return "", a, err
	}
	defer tx.Rollback(ctx)
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM organizations WHERE sandbox AND expires_at>now()").Scan(&count); err != nil {
		return "", a, err
	}
	if count >= 100 {
		return "", a, fail("LIMIT", "Global sandbox capacity reached")
	}
	if _, err = tx.Exec(ctx, "INSERT INTO organizations(id,name,sandbox,expires_at) VALUES($1,'Paper Finch Printworks • seeded demonstration',true,now()+interval '1 hour')", org); err != nil {
		return "", a, err
	}
	for _, r := range []string{"operator", "approver", "administrator"} {
		if _, err = tx.Exec(ctx, "INSERT INTO users(org_id,id,role) VALUES($1,$2,$2)", org, r); err != nil {
			return "", a, err
		}
	}
	p := defaultPolicy()
	if _, err = tx.Exec(ctx, "INSERT INTO policies VALUES($1,1,$2,now())", org, obj(p)); err != nil {
		return "", a, err
	}
	for i, state := range []string{"queued", "queued", "production"} {
		if _, err = tx.Exec(ctx, "INSERT INTO orders VALUES($1,$2,1,$3,$4)", org, fmt.Sprintf("PF-%d", 1041+i), state, Object{"product": "Custom print cards", "quantity": 250, "finish": "matte", "customerReference": "Launch kit"}); err != nil {
			return "", a, err
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO sessions VALUES($1,$2,$3,now()+interval '1 hour')", digest(token), org, a.User); err != nil {
		return "", a, err
	}
	return token, a, tx.Commit(ctx)
}
func (s *Store) Session(ctx context.Context, token string) (Actor, error) {
	var a Actor
	err := s.DB.QueryRow(ctx, "SELECT s.org_id,s.user_id,u.role,o.sandbox FROM sessions s JOIN users u ON (u.org_id,u.id)=(s.org_id,s.user_id) JOIN organizations o ON o.id=s.org_id WHERE digest=$1 AND s.expires_at>now() AND u.active AND (o.expires_at IS NULL OR o.expires_at>now())", digest(token)).Scan(&a.Org, &a.User, &a.Role, &a.Sandbox)
	if err != nil {
		return a, fail("UNAUTHORIZED", "Session missing or expired")
	}
	return a, nil
}
