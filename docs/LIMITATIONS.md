# Known limitations and verification boundaries

- No live model credentials, paid trials, real provider usage or model benchmark claims. The mocked mode is a synthetic tool sequence rather than a replay of a paid model run.
- No Shopify credentials or live API verification. Signed ingestion and read/reconciliation adapters are contract-tested. Merchant write operations, payment refunds/void handling and external action mapping are intentionally disabled/pending.
- No paid GCP resources, public access changes or deployed customer service. Terraform validation is local preparation. Docker container execution passed in Ubuntu GitHub Actions at the commit recorded in `evaluations/ci-verification.json`; it remains unavailable on the native Windows machine. Cloud Run/SQL/Secret Manager runtime behavior remains unverified.
- Private authentication uses operator-provisioned, expiring opaque sessions. SSO/OIDC, account recovery, invitation workflows and private role management are extension work. Sandbox role switching never grants access to private organizations.
- GraphQL result snapshots are bounded JSON rather than fully typed field-projected result schemas. Mutation inputs and backend/tool domain contracts are typed.
- Organization-level transaction locking favors straightforward correctness over parallel throughput. Approval polling has up to five seconds of intentional latency and creates bounded workflow history.
- Current policy supports one attribute change per case. The deterministic baseline is intentionally narrow and escalates unsupported/ambiguous instructions.
- The evaluation dataset is authored by the project builder and is small; held-out splits are regression coverage, not statistically independent industry benchmarking. Subjective explanations are not automatically graded.
- Temporal multi-version worker deployment, cloud trace export, tracing across the worker/provider, alert delivery, production restoration drills and independent penetration review are not verified. Backend HTTP tracing is implemented and locally tested.
- Expired sandbox records are inaccessible and no longer count against concurrent capacity, but physical retention cleanup is not scheduled. Add an authorized retention job before exposing a sustained public demo.
- Local native PostgreSQL uses loopback-only trust authentication. Compose uses generated private passwords. Neither local arrangement is the production Cloud SQL identity design.
