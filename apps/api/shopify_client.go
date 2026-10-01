package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}
type ShopifyClient struct {
	Shop    string
	Token   string
	Version string
	HTTP    Doer
}

func (c ShopifyClient) query(ctx context.Context, query string, variables Object) (Object, error) {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.myshopify\.com$`).MatchString(c.Shop) || c.Token == "" {
		return nil, fail("PERMANENT", "Approved development-store configuration required")
	}
	version := c.Version
	if version == "" {
		version = "2026-10"
	}
	if !regexp.MustCompile(`^202[6-9]-(01|04|07|10)$`).MatchString(version) {
		return nil, fail("PERMANENT", "Unsupported configured Shopify API version")
	}
	b, _ := json.Marshal(Object{"query": query, "variables": variables})
	req, err := http.NewRequestWithContext(ctx, "POST", "https://"+c.Shop+"/admin/api/"+version+"/graphql.json", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Shopify-Access-Token", c.Token)
	transport := c.HTTP
	if transport == nil {
		transport = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, err := transport.Do(req)
	if err != nil {
		return nil, fail("RETRYABLE", "Shopify read failed; state remains unreconciled")
	}
	defer response.Body.Close()
	if response.StatusCode == 429 || response.StatusCode >= 500 {
		return nil, fail("RETRYABLE", "Shopify rate limited or unavailable")
	}
	if response.StatusCode != 200 {
		return nil, fail("PERMANENT", "Shopify authorization or request rejected")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 32769))
	if err != nil || len(data) > 32768 {
		return nil, fail("PERMANENT", "Shopify response exceeded bounded contract")
	}
	var envelope struct {
		Data   Object   `json:"data"`
		Errors []Object `json:"errors"`
	}
	if json.Unmarshal(data, &envelope) != nil || envelope.Data == nil || len(envelope.Errors) > 0 {
		return nil, fail("PERMANENT", "Shopify GraphQL response invalid or rejected")
	}
	return envelope.Data, nil
}
func (c ShopifyClient) Order(ctx context.Context, gid string) (Object, error) {
	if !regexp.MustCompile(`^gid://shopify/Order/[0-9]+$`).MatchString(gid) {
		return nil, fail("INVALID", "Exact Shopify order identity required")
	}
	data, err := c.query(ctx, `query Evidence($id:ID!){shop{plan{partnerDevelopment}} order(id:$id){id name updatedAt cancelledAt displayFulfillmentStatus}}`, Object{"id": gid})
	if err != nil {
		return nil, err
	}
	shop, ok := data["shop"].(map[string]any)
	if !ok {
		return nil, fail("PERMANENT", "Development-store evidence missing")
	}
	plan, ok := shop["plan"].(map[string]any)
	if !ok || plan["partnerDevelopment"] != true {
		return nil, fail("FORBIDDEN", "Only verified partner development stores are supported")
	}
	order, ok := data["order"].(map[string]any)
	if !ok || order["id"] != gid {
		return nil, fail("NOT_FOUND", "Requested development-store order unavailable")
	}
	return order, nil
}
func reconcileShopify(ctx context.Context, s *Store, c ShopifyClient) error {
	if env("SHOPIFY_READ_ENABLED", "false") != "true" {
		return fail("FORBIDDEN", "Enable bounded test-store read reconciliation explicitly")
	}
	rows, err := s.DB.Query(ctx, "SELECT event_id,body FROM webhook_events WHERE shop=$1 AND status='received' ORDER BY created_at LIMIT 5", c.Shop)
	if err != nil {
		return err
	}
	type event struct {
		ID   string
		Body Object
	}
	events := []event{}
	for rows.Next() {
		var e event
		if err = rows.Scan(&e.ID, &e.Body); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, event := range events {
		gid, _ := event.Body["admin_graphql_api_id"].(string)
		if gid == "" {
			if n, ok := event.Body["id"].(float64); ok {
				gid = "gid://shopify/Order/" + strconv.FormatInt(int64(n), 10)
			}
		}
		evidence, e := c.Order(ctx, gid)
		if e != nil {
			return e
		}
		if _, e = s.DB.Exec(ctx, "UPDATE webhook_events SET status='reconciled',reconciled_state=$3 WHERE shop=$1 AND event_id=$2", c.Shop, event.ID, evidence); e != nil {
			return e
		}
	}
	return nil
}
