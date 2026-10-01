# PressPilot · Saero

**Local alpha.** PressPilot is a commerce exception-resolution project developed under **Saero**, an independent software and applied AI studio. It investigates support requests for fictional Paper Finch Printworks orders, retrieves authoritative evidence, proposes permitted actions, waits for approval, and verifies business results.

This repository demonstrates implementation and local verification. Live-agent reliability, production deployment, customer adoption, and measured business savings have not been established.

## Workflows and outdated approvals

- Cancel a queued order before production starts.
- Modify one permitted attribute (`finish` or `customerReference`) before production starts.
- Investigate requests after production starts and escalate with evidence.

**Try the flagship scenario:** submit a cancellation as Operator, inspect the proposal, switch the labelled sandbox role to Administrator and select **Simulate production start**, then approve as Approver. The proposal is invalidated, the workflow retrieves fresh evidence, and the case escalates. The order stays in production. In a fresh sandbox, an unchanged queued-order cancellation executes after approval.

Each proposal binds organization, order identity/version, policy version, exact action/arguments and evidence. Approval has an identity and expiry and applies only to that proposal. The backend revalidates requester/approver authorization, policy and order state immediately before an atomic PostgreSQL transition. Idempotency bindings and unique execution constraints prevent repeated effects. Customer text and model output cannot grant permissions. Requester protections remain enforced during recovery.

Demo sessions are isolated, bounded and expire after one hour. Seeded data and production events are explicitly simulated. Sandbox role switching is denied for private organizations; public sessions cannot make paid model calls. Refunds, shipment changes, artwork analysis and fleet management are outside this alpha.

## Architecture

| Boundary                              | Responsibility                                                                                         |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------ |
| Go / GraphQL (`apps/api`)             | Tenant isolation, authorization, domain rules, narrow tools, atomic execution, audit and webhook inbox |
| TypeScript / Temporal (`apps/worker`) | Durable workflows, approval waits, bounded retries, baseline and provider adapters                     |
| React / TypeScript (`apps/web`)       | Case queue, evidence, approvals, timelines, policies and evaluation reports; Node web gateway          |
| PostgreSQL                            | Authoritative orders, policies, proposals, approvals, executions and audit                             |
| Temporal                              | Execution history, timers and recovery; model/network calls occur only in activities                   |

The worker has no database credentials or arbitrary SQL, shell or general network tool. Application PostgreSQL and Temporal infrastructure storage are separate. Read [architecture/data flow](docs/ARCHITECTURE.md), [decision records](docs/adr/001-authoritative-business-boundary.md), [demonstration policy](docs/POLICY.md), and [SECURITY.md / threat model](SECURITY.md).

## Local setup

Run commands from the repository root. Native verification used Node **24.11.0**, Go **1.27.1**, PostgreSQL **18.6**, and Temporal CLI **1.9.1**. Dependencies are pinned in lockfiles. Both setup paths serve the console at **http://localhost:5173** and Temporal UI at **http://localhost:8233**.

### Docker Compose

Requires Docker Engine with Compose v2, Node 24.11+ and Git:

```sh
npm ci
node scripts/init-dev.mjs
# Linux/macOS: containers must match the owner of private secret files.
export LOCAL_UID=$(id -u) LOCAL_GID=$(id -g)
docker compose config --quiet
docker compose up -d --build
```

On Windows, omit `export`. The generator preserves existing credentials and writes private files under ignored `runtime/secrets` without printing values. Ports bind loopback. `docker compose down` stops services while retaining volumes; keep the volumes when their state is needed. Temporal's SQLite-backed development server is real and durable, but is not the production deployment.

The original native verification machine had no Docker engine. **Docker execution was subsequently verified in [Ubuntu CI run 36904644437](https://github.com/AnupDagala/presspilot/actions/runs/36904644437)** at commit `4100d86`: all source/infrastructure jobs, actual PostgreSQL/Temporal baseline evaluations, mocked authorization fixtures, hard worker recovery and browser tests passed. [Verification reconciliation](docs/VERIFICATION.md) distinguishes that successful commit from an older failed run that was rerun later. Compare a run's tested SHA with HEAD; a workflow definition or rerun of old code is not verification of a new commit.

### Native Windows

Download the pinned runtimes from official distributions and verify their published checksums. Put the full distributions in ignored `.tools`, exposing `go/bin/go.exe`, `pgsql/bin/pg_ctl.exe`, and `temporal/temporal.exe` below that directory.

```powershell
./scripts/start-local.ps1 -RuntimeRoot ./.tools -StateRoot ./runtime/native
# Stop tracked processes, preserving data and workflow history:
./scripts/stop-local.ps1 -RuntimeRoot ./.tools -StateRoot ./runtime/native
```

The startup script installs dependencies, builds the application and starts hidden loopback-only services. It refuses to replace occupied service ports. Native PostgreSQL uses loopback-only trust authentication; Compose uses generated passwords. Neither is a cloud identity configuration. Native verification also exercised the built Node web gateway on a separate loopback port.

## Modes and configuration

**Baseline** uses conservative deterministic interpretation and business tools. **Mocked** uses synthetic/recorded fixtures, with no live inference. **Live** runs a bounded tool-using model in a private environment; it is disabled by default and unverified. The console displays configuration and observed usage, never private reasoning.

Compose configures database/worker secret files, the Temporal address and sandbox flag. Native setup creates equivalent state under `runtime/native`. [.env.example](.env.example) documents safe defaults without credentials; processes do not load it automatically.

Live execution requires a private organization, `SANDBOX_ENABLED=false`, `LIVE_MODEL_ENABLED=true`, explicit `MODEL_PROVIDER`/`MODEL_ID`, server-only credentials and configured spending limits/price assumptions. Endpoints are allowlisted; private addresses and redirects are rejected. OpenAI, Claude, Grok and an approved open-weight inference adapter are implemented and contract-tested. Follow [provider configuration](docs/PROVIDERS.md) and [Shopify integration status](docs/INTEGRATION.md). Keep secrets out of source, command arguments and public reports.

## Actual verification

Recorded native checks used actual PostgreSQL and Temporal:

- **60/60 baseline** and **60/60 mocked** tasks: 40 development / 20 held-out, one trial each. Each suite includes 53 business/security/workflow tasks and seven recorded provider contracts.
- **18 Go test functions**, three subtests, **12 unit/contract tests**, and **six browser tests** passed.
- Baseline and mocked worker process-kill/restart checks each produced one order retrieval and one business effect. Private authorization and isolated PostgreSQL dump/restore checks passed.
- Builds, formatting, linting, type checks, dependency/secret scans, Compose isolation checks and Terraform validation passed locally.

**The 60/60 baseline and mocked results are not live-model benchmarks.** No paid calls occurred; synthetic responses do not establish live reliability. Baseline p50/p95 was 1,109/12,268 ms; mocked p50/p95 was 1,126/8,103 ms. Measurements include approval polling and other verification on the same host, not load testing.

Supporting evidence: [evaluation methodology](evaluations/SPEC.md), [scenario dataset](evaluations/scenarios.json), [baseline report](evaluations/results-baseline.json), [mocked report](evaluations/results-mocked.json), [verification record](docs/VERIFICATION.md), and [machine-readable results](evaluations/verification.json).

```sh
npm run format:check
npm run lint
npm run typecheck
npm run build
npm test
npm run config:check
npm run scan
```

For Compose tests set `TEST_DATABASE_URL_FILE=runtime/secrets/test-database-url` and `WORKER_SECRET_FILE=runtime/secrets/worker-secret`. Run [the evaluation harness](tests/evaluate.ts) with `npm run eval`; use `EVAL_MODE=mocked EVAL_PRIVATE_FIXTURES=true` for mocked private fixtures. From `apps/api`, set the database file path to `../../runtime/secrets/test-database-url` before `go test -v ./...` and `go vet ./...`. Tests explicitly skip without a database; skips do not prove invariants.

Native runners instead use the non-secret URL `postgres://presspilot@127.0.0.1:55432/presspilot?sslmode=disable` in `TEST_DATABASE_URL` and the worker secret file under `runtime/native`. Do not use Compose credentials for the native database. Run [process-crash recovery](tests/recovery-process.ts) with `npx tsx tests/recovery-process.ts`, then with `RECOVERY_MODE=mocked`. Run [browser flows](tests/browser/console.spec.ts) with `npx playwright install chromium` and `npm run test:browser`; `PP_BROWSER_CHANNEL=chrome` uses installed Chrome.

[GitHub Actions](.github/workflows/verify.yml) checks source and provisions actual integration dependencies without provider secrets. Paid tests are absent from push/PR CI; optional bounded live trials require explicit local configuration. No passing badge is included without a verified workflow run.

## Remaining milestones

[Demonstration guide](docs/DEMO.md), [known limitations](docs/LIMITATIONS.md), and [deployment/recovery runbook](docs/RUNBOOK.md) describe the next steps.

Live provider inference, actual Shopify development-store integration, and cloud deployment remain unverified. Docker execution passed on the Ubuntu CI runner; the original Windows machine still has no Docker engine. Shopify signature/inbox/read/reconciliation contracts are implemented; merchant writes are disabled. Terraform prepares private Cloud Run web/API, a continuous Temporal worker pool, Cloud SQL, Secret Manager references, traces and alerts. No cloud resources or merchant actions were performed.

Next: keep container verification passing for each changed commit; run bounded live trials with approved credentials/budget; verify a Shopify development store; then review cloud deployment, IAM, restore/replay compatibility and alert delivery with explicit authorization. Private identity onboarding, retention cleanup, multi-version worker rollout and tracing across worker/provider activities require further work before operational use.
