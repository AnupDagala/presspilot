import { readFile, writeFile, mkdir } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { resolve } from "node:path";
import assert from "node:assert/strict";
import { Client, Connection } from "@temporalio/client";
import { Worker, NativeConnection } from "@temporalio/worker";
import { ApplicationFailure } from "@temporalio/activity";
import * as activities from "../apps/worker/src/activities";
import { Session, db, waitFor } from "./helpers";
import { providerContract, providerFailure } from "./provider-fixtures";
import type { ProviderName } from "../apps/worker/src/providers";
import { installLogging } from "../apps/worker/src/logging";
installLogging();
type Scenario = {
  id: string;
  split: string;
  request?: string;
  order?: string;
  state?: string;
  event?: string;
  driver?: string;
  provider?: ProviderName;
  failure?: string;
  expected: {
    status?: string;
    state?: string;
    action?: string;
    arguments?: Record<string, string>;
    invalidated?: boolean;
    denied?: boolean;
    attempts?: number;
    contract?: boolean;
  };
};
const scenarios: Scenario[] = JSON.parse(
  await readFile("evaluations/scenarios.json", "utf8"),
);
assert.equal(scenarios.length, 60);
const mode = process.env.EVAL_MODE ?? "baseline";
if (!["baseline", "mocked", "live"].includes(mode))
  throw new Error("Unknown evaluation mode");
let selected = scenarios,
  trials = 1;
if (mode === "live") {
  if (process.env.LIVE_EVAL_ENABLED !== "true")
    throw new Error(
      "Paid evaluation requires explicit LIVE_EVAL_ENABLED=true and server-side spending limits",
    );
  const ids = (process.env.LIVE_EVAL_CASE_IDS ?? "").split(",").filter(Boolean);
  trials = Number(process.env.LIVE_EVAL_TRIALS ?? 1);
  if (
    !ids.length ||
    ids.length > 3 ||
    !Number.isInteger(trials) ||
    trials < 1 ||
    trials > 3
  )
    throw new Error(
      "Select one to three live case IDs and one to three trials",
    );
  selected = scenarios.filter((s) => ids.includes(s.id) && !s.driver);
  if (selected.length !== ids.length)
    throw new Error(
      "Live selection must contain unique business scenario IDs without fault-injection drivers",
    );
}
const connection = await Connection.connect({
    address: process.env.TEMPORAL_ADDRESS ?? "127.0.0.1:7233",
  }),
  client = new Client({ connection });
const core = await NativeConnection.connect({
  address: process.env.TEMPORAL_ADDRESS ?? "127.0.0.1:7233",
});
const results: {
  id: string;
  split: string;
  category: string;
  passed: boolean;
  latencyMs: number;
  error?: string;
  attempts?: number;
  run?: Record<string, any>;
}[] = [];
async function workflowTrial(task: Scenario) {
  const session = await Session.create(
    mode === "live" || process.env.EVAL_PRIVATE_FIXTURES === "true",
  );
  let worker: Worker | undefined, workerRun: Promise<void> | undefined;
  let attempts = 0;
  let other: Session | undefined;
  try {
    const oid = task.order ?? "PF-1041",
      key = randomUUID();
    if (task.state)
      await db.query("UPDATE orders SET state=$3 WHERE org_id=$1 AND id=$2", [
        session.org,
        oid,
        task.state,
      ]);
    if (task.event === "policy_before") {
      await session.role("administrator");
      const snap = await session.snapshot();
      await session.gql("mutation($p:JSON!){updatePolicy(policy:$p)}", {
        p: { ...snap.policy, allowCancellation: false },
      });
      await session.role("operator");
    }
    let c: Record<string, any>;
    if (task.driver) {
      // The test driver suppresses the application's dispatcher before starting a real Temporal run.
      const cid = randomUUID();
      await db.query(
        "INSERT INTO cases(org_id,id,order_id,requester_id,message,mode,workflow_started) VALUES($1,$2,$3,'operator',$4,'baseline',true)",
        [session.org, cid, oid, task.request],
      );
      c = { id: cid };
      const queue = "eval-" + cid;
      const wrapped = {
        ...activities,
        investigate: async (
          ...args: Parameters<typeof activities.investigate>
        ) => {
          attempts++;
          if (task.driver === "permanent")
            throw ApplicationFailure.nonRetryable(
              "Injected permanent tool failure",
              "PERMANENT",
            );
          if (
            task.driver === "exhausted" ||
            (task.driver === "retryable" && attempts < 3)
          )
            throw ApplicationFailure.retryable(
              "Injected retryable tool failure",
              "RETRYABLE",
            );
          return activities.investigate(...args);
        },
      };
      const make = () =>
        Worker.create({
          connection: core,
          taskQueue: queue,
          workflowsPath: resolve("apps/worker/dist/workflows.js"),
          activities: wrapped,
          maxConcurrentActivityTaskExecutions: 2,
        });
      worker = await make();
      workerRun = worker.run();
      await client.workflow.start("resolveCase", {
        workflowId: "evaluation-" + cid,
        taskQueue: queue,
        args: [{ org: session.org, caseId: cid, mode: "baseline" }],
        workflowExecutionTimeout: "2 minutes",
      });
      if (task.driver === "restart") {
        await waitFor(
          () => session.snapshot(),
          (s) =>
            s.cases.find((x: any) => x.id === cid)?.status ===
            "awaiting_approval",
        );
        worker.shutdown();
        await workerRun;
        worker = await make();
        workerRun = worker.run();
      }
    } else {
      c = await session.submit(
        task.request!,
        oid,
        process.env.EVAL_MODE ?? "baseline",
        key,
      );
      if (task.event === "duplicate_submit") {
        const again = await session.submit(
          task.request!,
          oid,
          process.env.EVAL_MODE ?? "baseline",
          key,
        );
        assert.equal(again.id, c.id);
      }
    }
    const ready = await waitFor(
      () => session.snapshot(),
      (s) =>
        ["awaiting_approval", "escalated"].includes(
          s.cases.find((x: any) => x.id === c.id)?.status,
        ),
    );
    const p = ready.proposals.find((x: any) => x.caseId === c.id);
    let denied = false;
    if (
      p &&
      ready.cases.find((x: any) => x.id === c.id).status === "awaiting_approval"
    ) {
      if (task.event === "production_after") {
        await session.role("administrator");
        if (mode === "live" || process.env.EVAL_PRIVATE_FIXTURES === "true") {
          // Test-owned fictional fixtures use a trusted simulated production event.
          // Private operational APIs deliberately do not expose sandbox simulation controls.
          await db.query(
            "UPDATE orders SET state='production',version=version+1 WHERE org_id=$1 AND id=$2 AND state='queued'",
            [session.org, oid],
          );
          await db.query(
            "INSERT INTO audit_events(org_id,case_id,actor,kind,data) VALUES($1,$2,'evaluation_fixture','simulated_production_started',$3)",
            [session.org, c.id, { orderId: oid, simulated: true }],
          );
        } else
          await session.gql(
            "mutation($id:ID!){simulateProduction(orderId:$id)}",
            { id: oid },
          );
      }
      if (task.event === "policy_after") {
        await session.role("administrator");
        await session.gql("mutation($p:JSON!){updatePolicy(policy:$p)}", {
          p: { ...ready.policy, allowCancellation: false },
        });
      }
      if (task.event === "operator_approve") {
        await assert.rejects(session.approve(p), /FORBIDDEN/);
        denied = true;
      }
      if (task.event === "cross_org") {
        other = await Session.create(
          mode === "live" || process.env.EVAL_PRIVATE_FIXTURES === "true",
        );
        await other.role("approver");
        await assert.rejects(other.approve(p), /NOT_FOUND/);
        denied = true;
      }
      await session.role("approver");
      if (task.event === "tamper_binding") {
        await assert.rejects(
          session.approve(p, "approved", "not-the-bound-action"),
          /CONFLICT/,
        );
        denied = true;
      }
      if (task.event === "expire")
        await db.query(
          "UPDATE proposals SET expires_at=now()-interval '1 second' WHERE org_id=$1 AND id=$2",
          [session.org, p.id],
        );
      else if (task.event === "parallel_approve")
        await Promise.all(Array.from({ length: 8 }, () => session.approve(p)));
      else
        await session.approve(
          p,
          task.event === "reject" ? "rejected" : "approved",
        );
      if (task.event === "revoke_approver") {
        await session.role("administrator");
        await db.query(
          "UPDATE users SET active=false WHERE org_id=$1 AND id='approver'",
          [session.org],
        );
      }
    }
    await waitFor(
      () => session.snapshot(),
      (s) =>
        ["completed", "escalated"].includes(
          s.cases.find((x: any) => x.id === c.id)?.status,
        ),
    );
    if (task.event === "repeat_execute" && p) {
      const run = { org: session.org, caseId: c.id, mode: "baseline" as const };
      await Promise.all(
        Array.from({ length: 4 }, () => activities.execute(run, p.id)),
      );
    }
    const row = (
      await db.query(
        "SELECT c.status,c.mode,c.config,o.state,o.version,o.attributes FROM cases c JOIN orders o ON (o.org_id,o.id)=(c.org_id,c.order_id) WHERE c.org_id=$1 AND c.id=$2",
        [session.org, c.id],
      )
    ).rows[0];
    assert.equal(row.status, task.expected.status);
    assert.equal(row.state, task.expected.state);
    const effects = (
      await db.query(
        "SELECT e.*,p.action,p.arguments,p.evidence,p.order_version,p.policy_version,a.binding AS approval_binding FROM executions e JOIN proposals p ON (p.org_id,p.id)=(e.org_id,e.proposal_id) JOIN approvals a ON (a.org_id,a.proposal_id)=(p.org_id,p.id) WHERE e.org_id=$1 AND e.case_id=$2",
        [session.org, c.id],
      )
    ).rows;
    if (task.expected.action) {
      assert.equal(effects.length, 1);
      const effect = effects[0];
      assert.equal(effect.action, task.expected.action);
      assert.deepEqual(effect.arguments, task.expected.arguments ?? {});
      assert.equal(effect.binding, effect.approval_binding);
      assert.equal(effect.evidence.order.version, effect.order_version);
      assert.equal(effect.evidence.policy.version, effect.policy_version);
      assert.equal(effect.evidence.order.state, "queued");
      assert.equal(row.version, effect.order_version + 1);
      for (const [k, v] of Object.entries(task.expected.arguments ?? {}))
        assert.equal(row.attributes[k], v);
    } else assert.equal(effects.length, 0);
    if (task.expected.invalidated) {
      const invalid = (
        await db.query(
          "SELECT count(*)::int AS n FROM proposals WHERE org_id=$1 AND case_id=$2 AND status='invalidated'",
          [session.org, c.id],
        )
      ).rows[0];
      assert.ok(invalid.n >= 1);
    }
    if (task.expected.denied) assert.ok(denied);
    if (task.expected.attempts) assert.equal(attempts, task.expected.attempts);
    const reservations = (
      await db.query(
        "SELECT reserved,used,configuration FROM model_turns WHERE org_id=$1 AND case_id=$2 ORDER BY created_at",
        [session.org, c.id],
      )
    ).rows;
    return {
      attempts,
      run: { mode: row.mode, configuration: row.config, reservations },
    };
  } finally {
    if (worker) {
      worker.shutdown();
      await workerRun;
    }
    if (other) await other.expire();
    await session.expire();
  }
}
try {
  for (const task of Array.from({ length: trials }, () => selected).flat()) {
    const start = Date.now();
    try {
      let attempts: number | undefined;
      let run: Record<string, any> | undefined;
      if (task.driver === "provider") providerContract(task.provider!);
      else if (task.driver === "provider_error")
        await providerFailure(task.failure!);
      else ({ attempts, run } = await workflowTrial(task));
      results.push({
        id: task.id,
        split: task.split,
        category: task.driver?.startsWith("provider")
          ? "recorded_provider_contract"
          : "business_workflow",
        passed: true,
        latencyMs: Date.now() - start,
        attempts,
        run,
      });
      console.log(`PASS ${task.id}`);
    } catch (e) {
      const error = e instanceof Error ? e.message : "Trial failed";
      results.push({
        id: task.id,
        split: task.split,
        category: task.driver?.startsWith("provider")
          ? "recorded_provider_contract"
          : "business_workflow",
        passed: false,
        latencyMs: Date.now() - start,
        error,
      });
      console.log(`FAIL ${task.id}: ${error}`);
    }
  }
  const sorted = results.map((r) => r.latencyMs).sort((a, b) => a - b);
  const report = {
    mode,
    datasetSize: selected.length,
    sourceDatasetSize: 60,
    split: {
      development: selected.filter((s) => s.split === "development").length,
      held_out: selected.filter((s) => s.split === "held_out").length,
    },
    trials,
    passed: results.filter((r) => r.passed).length,
    failed: results.filter((r) => !r.passed).length,
    latencyMs: {
      p50: sorted[Math.floor(sorted.length * 0.5)],
      p95: sorted[Math.floor(sorted.length * 0.95)],
    },
    configuration: {
      temporalAddress: process.env.TEMPORAL_ADDRESS ?? "127.0.0.1:7233",
      postgres: "actual local PostgreSQL",
      baseline: "baseline-1",
      paidProviderCalls:
        mode === "live"
          ? "See persisted model-turn reservations and usage per private trial"
          : 0,
      networkProviderCalls:
        mode === "live" ? "Optional bounded execution; no count inferred" : 0,
      syntheticConfiguration:
        process.env.EVAL_MODE === "mocked"
          ? {
              provider: "mocked",
              model: "recorded-fixture-v1",
              endpoint: "mock://recorded",
              maxOutputTokens: 0,
              promptVersion: "mocked-contract-1",
            }
          : null,
      liveVerified:
        mode === "live" &&
        results.some((r) =>
          r.run?.reservations.some((t: any) => t.used !== null),
        ),
      usage:
        mode === "live"
          ? results.map((r) => ({
              scenario: r.id,
              observed: r.run?.configuration.usage ?? null,
              reservations: r.run?.reservations ?? [],
            }))
          : null,
      cost:
        mode === "live"
          ? "Estimated from recorded usage and operator-supplied pricing bounds; not billing verification"
          : "No paid calls. Runtime infrastructure costs excluded.",
      measuredAt: new Date().toISOString(),
      workflowCount: selected.filter((s) => !s.driver?.startsWith("provider"))
        .length,
      recordedProviderContracts: selected.filter((s) =>
        s.driver?.startsWith("provider"),
      ).length,
      recoveryDriverMode:
        "baseline; injected activity failures and worker lifecycle checks",
      recoveryHarness:
        "real Temporal worker shutdown/restart and bounded activity failures",
    },
    results,
  };
  const directory =
    mode === "live" ? "runtime/private-evaluations" : "evaluations";
  await mkdir(directory, { recursive: true });
  await writeFile(
    `${directory}/results-${report.mode}.json`,
    JSON.stringify(report, null, 2),
  );
  if (mode !== "live")
    await writeFile(
      `apps/web/public/evaluation-${report.mode}.json`,
      JSON.stringify(report, null, 2),
    );
  if (report.mode === "baseline")
    await writeFile(
      "apps/web/public/evaluation-report.json",
      JSON.stringify(report, null, 2),
    );
  console.log(
    JSON.stringify({
      passed: report.passed,
      failed: report.failed,
      latencyMs: report.latencyMs,
    }),
  );
  if (report.failed) process.exitCode = 1;
} finally {
  await connection.close();
  await core.close();
  await db.end();
}
