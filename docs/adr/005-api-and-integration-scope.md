# ADR 005 · Compact API and Shopify scope

Accepted. GraphQL uses typed mutation inputs and a bounded JSON snapshot result in v1. Domain models and internal tool inputs are separately typed. This keeps the first implementation compact, but gives up GraphQL field-level projection/code generation for snapshots; fully typed result objects are an extension.

Shopify integration begins with signed inbox ingestion and bounded read/reconciliation from verified development stores. Shopify fulfillment status is not authoritative printing-production status. External cancellation, refund and merchant-write mapping remain disabled until permissions, payment implications and cross-system reconciliation are verified. No external exactly-once claim is made.
