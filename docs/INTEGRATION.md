# Shopify integration status

**Implemented, contract-tested, pending live verification.** No Shopify credentials were available. No actual merchant requests or actions were performed.

Implemented Go adapter: `Order` uses the GraphQL Admin API with explicit version `2026-10`, a fixed configured `*.myshopify.com` shop, bounded response/time, no redirects and server-held token. It requests identity, timestamps, cancellation and fulfillment facts plus `shop.plan.partnerDevelopment`. The adapter rejects a non-development store. This is a read integration; Shopify fulfillment state does not establish printing-production eligibility.

Webhook endpoint `/webhooks/shopify` verifies HMAC-SHA256 over the raw body using `X-Shopify-Hmac-Sha256`, restricts `X-Shopify-Shop-Domain`, and uses `X-Shopify-Event-Id` as a durable unique key. It persists the authenticated event before acknowledging. Repeated deliveries do not create duplicate rows. Reusing an event ID with a changed body/topic fails. Database failures return 503 for redelivery. Contract tests use actual PostgreSQL for inbox persistence.

Reconciliation mode (`RECONCILE_SHOPIFY=true`, `SHOPIFY_READ_ENABLED=true`) reads at most five pending inbox events, retrieves the current test-store order, and stores the reconciled external evidence. It leaves failed reads pending. It does not mutate internal production state from possibly stale webhook payloads. Run this bounded mode from an authorized maintenance task; no automatic external scheduler has been created.

## Exact setup prerequisites

1. Create a Shopify Partner development store and test orders with test payment data.
2. Create/install an app with the currently required `read_orders` scope. Do not request broad write scopes for the read-only adapter. Review access to protected customer data even though the adapter does not request customer PII.
3. Save the Admin API token and app webhook secret in private server secret files or Secret Manager. Configure `SHOPIFY_TEST_STORE`, `SHOPIFY_ACCESS_TOKEN_FILE`, `SHOPIFY_WEBHOOK_SECRET_FILE` and `SHOPIFY_API_VERSION=2026-10`.
4. Configure a test HTTPS webhook delivery endpoint for `orders/updated` and `orders/cancelled`. Use a dedicated authenticated ingress strategy that permits signed webhook delivery; the default private Cloud Run API is not anonymously invokable and has not been opened.
5. Deliver a signed test event twice; verify a single inbox row. Enable the bounded read-reconciliation mode, then inspect `reconciled_state` and status. Verify the development-store plan check and current order facts using actual API responses.
6. Keep merchant writes disabled. Connecting internal cancellation to Shopify would require reviewed order mapping, current `write_orders` permissions, payment/void implications, mutation/job reconciliation and another exact approval binding. These prerequisites are unresolved rather than simulated as real integration.

Sources: [Shopify webhook verification/deduplication](https://shopify.dev/docs/apps/build/webhooks/verify-deliveries), [ShopPlan development-store field](https://shopify.dev/docs/api/admin-graphql/latest/objects/ShopPlan), [order query](https://shopify.dev/docs/api/admin-graphql/latest/queries/order), and [cancellation mutation implications](https://shopify.dev/docs/api/admin-graphql/latest/mutations/orderCancel).
