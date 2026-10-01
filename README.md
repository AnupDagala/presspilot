# PressPilot · Saero

PressPilot investigates printing-commerce support exceptions, proposes an evidence-bound action, waits for an authorized person, and verifies the final order transition in PostgreSQL. Developed under **Saero**, an independent software and applied AI studio. This is a functioning portfolio application with fictional data, not a production-proven or customer-deployed service.

## Local application

Open **http://localhost:5173** when the local services are running. Start an isolated one-hour demo session. Three seeded orders belong only to that session. Public demo sessions cannot run paid models. Role switching is a labelled sandbox feature and is rejected for private organizations.

The verified production web gateway currently runs at **http://localhost:5180** on the implementation machine; the reproducible development setup uses port 5173.

Requirements for the reproducible container path: Docker Compose v2, Node 24.11+, Git. Runtime versions and dependencies are pinned in source and lockfiles.

```sh
npm ci
node scripts/init-dev.mjs
docker compose config --quiet
docker compose up -d --build
```

The secret generator writes private files into ignored `runtime/secrets`; it never prints credential values. PostgreSQL business data and Temporal development-server infrastructure use separate volumes. All published local ports bind loopback. Temporal's local UI is http://localhost:8233. The development server is a real Temporal server with durable SQLite storage; it is not the production Temporal deployment.

**Verification on the implementation machine:** Docker was unavailable. The application was built and exercised with portable Go, actual PostgreSQL 18.6, and Temporal CLI 1.9.1 / server 1.32.0. Docker image execution is pending; YAML/isolation validation and container build instructions are provided. See [verification evidence](docs/VERIFICATION.md) for exact results and remaining gaps.

For native Windows operation use `scripts/start-local.ps1` with paths to official portable Go, PostgreSQL, Temporal runtimes. Node dependencies are installed by `npm ci`. The script stores state and private service credentials under an ignored runtime directory and starts hidden, loopback-only services. Existing state is preserved. On this machine the downloaded runtimes and state are under the parent workspace's `work` directory.

## Demonstrate the safety boundary

1. Start an isolated demo. As Operator, submit `Please cancel my order.` for `PF-1041`.
2. Inspect the order and policy evidence, versions, proposal hash, and approval expiry.
3. Switch the labelled demo role to Administrator. Select **Simulate production start**.
4. Switch to Approver. Select **Approve exact action**.
5. The timeline shows approval, proposal invalidation, fresh evidence retrieval, and escalation. The order stays in production. No cancellation executes.
6. In a fresh session, approve an unchanged queued-order cancellation and verify the order becomes cancelled exactly once.

[Full demonstration script](docs/DEMO.md) also covers modification, rejection, policy changes, evaluations, and recovery.

## Verification commands

```sh
npm run build
npm run typecheck
npm run lint
npm test
npm run format:check
npm run config:check
npm run scan
npm audit --audit-level=high
```

Run Go integration tests with `TEST_DATABASE_URL_FILE` pointing to the **host** connection file `runtime/secrets/test-database-url`. From `apps/api`, use `../../runtime/secrets/test-database-url`. Then run `go test -v ./...`, `go vet ./...`, and `govulncheck ./...`. Without a test database, integration tests explicitly skip; skipped tests are not evidence of passing database invariants.

Set `WORKER_SECRET_FILE=runtime/secrets/worker-secret` and `TEST_DATABASE_URL_FILE=runtime/secrets/test-database-url`, then run `npm run eval` while the API and worker are up. The harness grades actual PostgreSQL effects and runs real Temporal retry and recovery scenarios. `EVAL_MODE=mocked` runs the synthetic provider workflow separately. `tsx tests/recovery-process.ts` forcibly terminates a separate worker process while approval is pending and verifies recovery without duplicated retrievals or effects. Browser tests require an installed Playwright browser; `PP_BROWSER_CHANNEL=chrome` uses installed Chrome.

## Boundaries and operating modes

- `apps/api`: Go, GraphQL, authorization, narrow internal tool gateway, business rules, transactions, audit, outbox reconciliation, webhook ingestion and Shopify read adapter.
- `apps/worker`: TypeScript, Temporal orchestration, deterministic baseline, synthetic recorded responses, bounded live tool loop, four provider adapters.
- `apps/web`: React operations console; production Node gateway forwards same-origin authenticated requests to the private API.
- PostgreSQL owns operational facts. Temporal owns execution history and durable timers. The worker has no database credentials.

**Baseline** is conservative deterministic interpretation and business tooling. **Mocked** is a recorded/synthetic fixture sequence, not live AI. **Live** uses configured provider/model IDs and is disabled unless explicitly configured in a private environment. No live calls have been verified. The console exposes run configuration and observed usage, never private reasoning.

[Provider configuration](docs/PROVIDERS.md), [architecture](docs/ARCHITECTURE.md), [demonstration policy](docs/POLICY.md), [threat model](SECURITY.md), [evaluation specification](evaluations/SPEC.md), [Shopify status](docs/INTEGRATION.md), and [deployment/recovery](docs/RUNBOOK.md) describe the implementation and limits.

## Deployment status

No cloud resources were provisioned and no site was publicly deployed. `infra/` prepares private Cloud Run web/API services, a continuous worker pool, Cloud SQL, Secret Manager references, logging and alerts. The worker uses Temporal Cloud TLS/API-key configuration. The API dispatcher has continuously allocated CPU and a minimum instance; it does not depend on processing surviving an HTTP request. [Google documents worker pools as continuous background runtimes](https://docs.cloud.google.com/run/docs/deploy-worker-pools).

The infrastructure configuration contains secret containers and references only. Populate secret versions separately from protected files. Obtain explicit authorization before applying paid infrastructure or changing public access. [Deployment preparation and costs](docs/RUNBOOK.md) includes migration, backup, restore, rollback, and readiness procedures.
