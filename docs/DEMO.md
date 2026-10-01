# Demonstration script

1. Open http://localhost:5173. Introduce PressPilot as a working exception-resolution agent developed under Saero, using fictional Paper Finch Printworks orders.
2. Start the isolated demo. Explain the one-hour lifetime, seeded data and explicit baseline operating mode. Show the queue and roles.
3. Submit `Please cancel my order.` for PF-1041. Inspect the request, current queued state, immutable order/policy evidence, exact proposal binding, expiry and audit timeline. Operator approval is disabled and rejected by the backend.
4. Switch demo role to Administrator; simulate production start. Order version increments and state becomes production. Switch to Approver and approve the exact proposal.
5. Show `approval recorded`, `proposal invalidated`, new tool evidence and `case escalated`. The order remains in production and no execution record is created. Approval could not override changed evidence.
6. Create a fresh sandbox/browser session. Submit the same cancellation without changing evidence. Approve and show a single atomic transition to cancelled, version increment and execution audit event.
7. In another session, request `Change finish to gloss`. Inspect the exact modification arguments and approve. The order remains queued with a new version and gloss finish.
8. Submit a request after production, an ambiguous request, or an instruction to bypass policy. Show escalation without effect. Reject a permitted proposal and show that rejection also leaves the order unchanged.
9. Open Evaluations. Explain the development/held-out split and separate baseline, mocked and live modes. The measured report grades PostgreSQL effects; it does not estimate model intelligence. Show failure/recovery evidence files and the hard worker-process restart check.
10. Explain the architecture: Go owns authorization and transitions; TypeScript activities select narrow tools; Temporal owns durable execution; PostgreSQL owns business records. Provider and Shopify live runs are unverified. GCP configuration is validated preparation, not a commercial deployment.

Factual resume bullets:

- Built PressPilot under Saero, a Go/GraphQL, TypeScript/React and PostgreSQL application for investigating printing-commerce cancellation and modification exceptions.
- Implemented Temporal workflows and evidence-bound approvals with transactional version, policy and authorization revalidation, preventing stale approvals and repeated requests from producing duplicate order effects.
- Built a 60-scenario evaluation suite with database-state grading, security/concurrency checks, provider contract fixtures and durable worker-recovery tests, plus containerized GCP and Shopify integration preparation.
