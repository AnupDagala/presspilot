package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func validWebhook(body []byte, signature, secret string) bool {
	if secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	actual, e := base64.StdEncoding.DecodeString(signature)
	return e == nil && hmac.Equal(actual, mac.Sum(nil))
}
func registerShopify(mux *http.ServeMux, s *Store) {
	mux.HandleFunc("/webhooks/shopify", func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil || r.Method != "POST" {
			reply(w, 400, Object{"error": "Invalid delivery"})
			return
		}
		if !validWebhook(body, r.Header.Get("X-Shopify-Hmac-Sha256"), secret("SHOPIFY_WEBHOOK_SECRET")) {
			reply(w, 401, Object{"error": "Signature verification failed"})
			return
		}
		shop, event, topic := r.Header.Get("X-Shopify-Shop-Domain"), r.Header.Get("X-Shopify-Event-Id"), r.Header.Get("X-Shopify-Topic")
		if shop != osShop() || !strings.HasSuffix(shop, ".myshopify.com") || len(event) < 8 || len(event) > 128 {
			reply(w, 403, Object{"error": "Unapproved test store or event identity"})
			return
		}
		var payload Object
		if e = json.Unmarshal(body, &payload); e != nil {
			reply(w, 400, Object{"error": "Malformed payload"})
			return
		}
		// Persist before acknowledgement; the adapter does not mutate merchant orders.
		_, e = s.DB.Exec(r.Context(), "INSERT INTO webhook_events(shop,event_id,topic,body) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", shop, event, topic, payload)
		if e != nil {
			reply(w, 503, Object{"error": "Event not persisted; redeliver"})
			return
		}
		reply(w, 200, Object{"persisted": true, "integration": "contract_only"})
	})
}
func osShop() string { return env("SHOPIFY_TEST_STORE", "") }
