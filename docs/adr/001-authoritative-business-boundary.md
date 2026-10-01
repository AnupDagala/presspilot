# ADR 001 · Authoritative business boundary

Accepted. Go and PostgreSQL enforce all consequential actions. The model can interpret, choose narrow tools and explain evidence, but cannot execute unrestricted operations. Canonical JSON binds exact evidence and arguments across in-memory structs and persisted JSONB. The executor rechecks authorization and policy and commits the final versioned transition atomically.

An organization-row lock makes concurrency easy to audit for this bounded demo. It serializes unrelated orders in one organization; finer order/policy locking can replace it when throughput demands justify the additional lock-order complexity.
