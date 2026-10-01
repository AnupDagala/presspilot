# ADR 004 · Continuous workers and cost

Accepted. Web and Go API deploy as private Cloud Run services; Temporal polling uses a Cloud Run worker pool with one manually allocated instance. The API outbox dispatcher has a minimum instance and continuously allocated CPU. Temporal Cloud provides durable infrastructure separately from Cloud SQL business storage.

The configuration intentionally incurs persistent worker, dispatcher, database and Temporal costs. It avoids pretending that request-scoped CPU can safely power a long-lived worker. A VM or GKE self-hosted Temporal deployment may reduce managed-service cost at the expense of substantial maintenance. No infrastructure has been applied.
