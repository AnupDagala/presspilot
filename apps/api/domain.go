package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Object = map[string]any
type Actor struct {
	Org     string
	User    string
	Role    string
	Sandbox bool
}
type Order struct {
	ID         string `json:"id"`
	Version    int    `json:"version"`
	State      string `json:"state"`
	Attributes Object `json:"attributes"`
}
type Policy struct {
	Version            int      `json:"version"`
	AllowCancellation  bool     `json:"allowCancellation"`
	ModificationFields []string `json:"modificationFields"`
	ApprovalSeconds    int      `json:"approvalSeconds"`
	MaxTurns           int      `json:"maxTurns"`
	MaxTools           int      `json:"maxTools"`
	MaxTokens          int      `json:"maxTokens"`
}
type Case struct {
	ID        string `json:"id"`
	OrderID   string `json:"orderId"`
	Requester string `json:"requester"`
	Message   string `json:"message"`
	Mode      string `json:"mode"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	Config    Object `json:"config"`
}
type Proposal struct {
	ID            string    `json:"id"`
	CaseID        string    `json:"caseId"`
	OrderID       string    `json:"orderId"`
	OrderVersion  int       `json:"orderVersion"`
	PolicyVersion int       `json:"policyVersion"`
	Action        string    `json:"action"`
	Arguments     Object    `json:"arguments"`
	Evidence      Object    `json:"evidence"`
	Binding       string    `json:"binding"`
	Status        string    `json:"status"`
	ExpiresAt     time.Time `json:"expiresAt"`
}
type Fault struct {
	Code    string
	Message string
}

func (f *Fault) Error() string              { return f.Code + ": " + f.Message }
func (f *Fault) Extensions() map[string]any { return Object{"code": f.Code} }
func fail(code, msg string) error           { return &Fault{code, msg} }
func category(err error) string {
	var f *Fault
	if errors.As(err, &f) {
		return f.Code
	}
	return "RETRYABLE"
}
func id() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func digest(v any) string {
	b, _ := json.Marshal(v)
	// Canonicalize nested structs and JSONB objects to identical key ordering.
	var canonical any
	_ = json.Unmarshal(b, &canonical)
	b, _ = json.Marshal(canonical)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func obj(v any) Object {
	b, _ := json.Marshal(v)
	var out Object
	_ = json.Unmarshal(b, &out)
	return out
}
func defaultPolicy() Policy {
	return Policy{1, true, []string{"customerReference", "finish"}, 600, 8, 16, 12000}
}
func validatePolicy(p Policy) error {
	if p.ApprovalSeconds < 5 || p.ApprovalSeconds > 3600 || p.MaxTurns < 1 || p.MaxTurns > 12 || p.MaxTools < 3 || p.MaxTools > 24 || p.MaxTokens < 256 || p.MaxTokens > 20000 {
		return fail("INVALID", "Policy limits exceed supported bounds")
	}
	for _, field := range p.ModificationFields {
		if field != "finish" && field != "customerReference" {
			return fail("INVALID", "Unsupported modification field")
		}
	}
	return nil
}
func eligible(o Order, p Policy, action string, args Object) error {
	if o.State != "queued" {
		return fail("INELIGIBLE", "Production has started or the order is no longer queued; human review required")
	}
	switch action {
	case "cancel":
		if !p.AllowCancellation || len(args) != 0 {
			return fail("INELIGIBLE", "Cancellation is disabled or arguments are unsupported")
		}
	case "modify":
		if len(args) != 1 {
			return fail("INELIGIBLE", "Exactly one permitted attribute must be supplied")
		}
		for k, v := range args {
			permitted := false
			for _, field := range p.ModificationFields {
				if field == k {
					permitted = true
				}
			}
			value, ok := v.(string)
			if !permitted || !ok || len(strings.TrimSpace(value)) == 0 || len(value) > 80 {
				return fail("INELIGIBLE", "Attribute or value is outside demonstration policy")
			}
			if k == "finish" && value != "matte" && value != "gloss" {
				return fail("INELIGIBLE", "Finish must be matte or gloss")
			}
		}
	default:
		return fail("FORBIDDEN", "Unsupported business action")
	}
	return nil
}
func binding(org string, p Proposal) string {
	return digest(Object{"org": org, "order": p.OrderID, "orderVersion": p.OrderVersion, "policyVersion": p.PolicyVersion, "action": p.Action, "arguments": p.Arguments, "evidence": p.Evidence, "case": p.CaseID})
}
