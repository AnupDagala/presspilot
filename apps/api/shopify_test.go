package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeShopify func(*http.Request) (*http.Response, error)

func (f fakeShopify) Do(r *http.Request) (*http.Response, error) { return f(r) }
func TestShopifyOrderContract(t *testing.T) {
	fixture := `{"data":{"shop":{"plan":{"partnerDevelopment":true}},"order":{"id":"gid://shopify/Order/1","name":"#1001","updatedAt":"2026-10-01T00:00:00Z","cancelledAt":null,"displayFulfillmentStatus":"UNFULFILLED"}}}`
	c := ShopifyClient{Shop: "fixture-test.myshopify.com", Token: "contract-placeholder", Version: "2026-10", HTTP: fakeShopify(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "fixture-test.myshopify.com" || r.URL.Path != "/admin/api/2026-10/graphql.json" {
			t.Fatal("unexpected endpoint")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(fixture))}, nil
	})}
	if _, e := c.Order(context.Background(), "gid://shopify/Order/1"); e != nil {
		t.Fatal(e)
	}
	fixture = strings.ReplaceAll(fixture, `"partnerDevelopment":true`, `"partnerDevelopment":false`)
	if _, e := c.Order(context.Background(), "gid://shopify/Order/1"); category(e) != "FORBIDDEN" {
		t.Fatal("merchant store accepted", e)
	}
	c.Shop = "127.0.0.1"
	if _, e := c.Order(context.Background(), "gid://shopify/Order/1"); category(e) != "PERMANENT" {
		t.Fatal("unapproved endpoint accepted", e)
	}
}
func TestWebhookPersistsBeforeAckAndDeduplicates(t *testing.T) {
	s, a := fixture(t)
	_ = a
	t.Setenv("SHOPIFY_WEBHOOK_SECRET", "contract-placeholder")
	t.Setenv("SHOPIFY_TEST_STORE", "fixture-test.myshopify.com")
	mux := http.NewServeMux()
	registerShopify(mux, s)
	send := func(body string, status int) {
		mac := hmac.New(sha256.New, []byte("contract-placeholder"))
		mac.Write([]byte(body))
		r := httptest.NewRequest("POST", "/webhooks/shopify", strings.NewReader(body))
		r.Header.Set("X-Shopify-Hmac-Sha256", base64.StdEncoding.EncodeToString(mac.Sum(nil)))
		r.Header.Set("X-Shopify-Shop-Domain", "fixture-test.myshopify.com")
		r.Header.Set("X-Shopify-Event-Id", "test-event-id")
		r.Header.Set("X-Shopify-Topic", "orders/updated")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	_, _ = s.DB.Exec(context.Background(), "DELETE FROM webhook_events WHERE shop='fixture-test.myshopify.com'")
	send(`{"id":1}`, 200)
	send(`{"id":1}`, 200)
	send(`{"id":2}`, 409)
	var n int
	_ = s.DB.QueryRow(context.Background(), "SELECT count(*) FROM webhook_events WHERE shop='fixture-test.myshopify.com'").Scan(&n)
	if n != 1 {
		t.Fatal("duplicate persisted", n)
	}
}
