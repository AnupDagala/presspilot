# PressPilot implementation plan

1. Inspect instructions, preserve workspace, initialize dedicated repository. Complete: no instructions or pre-existing code; initialized outputs/presspilot.
2. Build and verify the cancellation slice against PostgreSQL and Temporal, including stale approval invalidation. Complete: actual PostgreSQL 18.6 / Temporal 1.32.0; Go integration tests and 4 browser flows passed. Fixed evidence SQL typing and canonical JSON bindings.
3. Extend modifications, roles, bounded tool agent, four provider adapters and 60 normative evaluation scenarios. Complete locally: baseline and mocked workflows, private authorization fixtures and optional bounded live evaluator; live inference remains unverified.
4. Verify browser flows, concurrency, security, durable recovery, builds, dependencies and secrets. Complete locally: actual PostgreSQL/Temporal; both 60-case suites; process-kill recovery; production gateway browser flows; private sign-in; local database restore; formatting/type/lint/build/vulnerability and redacted secret checks. Results live in evaluations/.
5. Add Shopify contracts and GCP deployment configuration. Complete preparation: signed durable inbox, read/reconciliation contracts, private Cloud Run/API/worker pool, Cloud SQL/Secret Manager, tracing, alerts and recovery runbook. Docker execution, Shopify/provider live calls, cloud export/deployment and alert delivery require external access and remain unverified.
6. Record measured results, demonstration instructions, source commit, limitations and resume bullets. Complete documentation; final commit identifies this local milestone. No production/customer-use claim.

No paid provisioning, public deployment, or merchant actions are authorized.

Public portfolio publication: account verified as AnupDagala, source/history scans passed, and reviewed source published to the public AnupDagala/presspilot repository. Full Docker CI passed for commit 4100d86 after correcting Temporal persistence ownership/readiness. On 2 October, reconciled an older failed run's second attempt against that success, recorded per-step evidence, and updated native-versus-CI verification boundaries. New documentation commits trigger the complete workflow; their result must be checked against pushed HEAD. Cloud provisioning and merchant actions remain outside scope.
