import { spawn, type ChildProcess } from "node:child_process";
import { randomUUID } from "node:crypto";
import { resolve } from "node:path";
import { once } from "node:events";
import { writeFile } from "node:fs/promises";
import assert from "node:assert/strict";
import { Connection, Client } from "@temporalio/client";
import { Session, db, waitFor } from "./helpers";
const session = await Session.create(),
  cid = randomUUID(),
  queue = "process-recovery-" + cid;
let child: ChildProcess | undefined;
const mode = process.env.RECOVERY_MODE === "mocked" ? "mocked" : "baseline";
const connection = await Connection.connect({
    address: process.env.TEMPORAL_ADDRESS ?? "127.0.0.1:7233",
  }),
  client = new Client({ connection });
const launch = () =>
  spawn(process.execPath, [resolve("apps/worker/dist/worker.js")], {
    env: {
      ...process.env,
      PP_TASK_QUEUE: queue,
      WORKER_HEALTH_PORT: "9091",
      WORKER_HEALTH_BIND: "127.0.0.1",
    },
    stdio: "ignore",
    windowsHide: true,
  });
try {
  await db.query(
    "INSERT INTO cases(org_id,id,order_id,requester_id,message,mode,workflow_started) VALUES($1,$2,'PF-1041','operator','Cancel my order',$3,true)",
    [session.org, cid, mode],
  );
  child = launch();
  await client.workflow.start("resolveCase", {
    workflowId: "process-recovery-" + cid,
    taskQueue: queue,
    args: [{ org: session.org, caseId: cid, mode }],
    workflowExecutionTimeout: "2 minutes",
  });
  const state = await waitFor(
    () => session.snapshot(),
    (s) =>
      s.cases.find((c: any) => c.id === cid)?.status === "awaiting_approval",
    40000,
  );
  const exit = once(child, "exit");
  child.kill("SIGKILL");
  await exit;
  child = undefined;
  await session.role("approver");
  await session.approve(state.proposals.find((p: any) => p.caseId === cid));
  child = launch();
  await waitFor(
    () => session.snapshot(),
    (s) => s.cases.find((c: any) => c.id === cid)?.status === "completed",
    40000,
  );
  const effects = (
    await db.query(
      "SELECT count(*)::int AS n FROM executions WHERE org_id=$1 AND case_id=$2",
      [session.org, cid],
    )
  ).rows[0].n;
  const retrievals = (
    await db.query(
      "SELECT count(*)::int AS n FROM tool_calls WHERE org_id=$1 AND case_id=$2 AND name='get_order'",
      [session.org, cid],
    )
  ).rows[0].n;
  assert.equal(effects, 1);
  assert.equal(retrievals, 1);
  const result = {
    test: "worker process terminated during approval wait; approval submitted while down; replacement process replayed and completed",
    passed: true,
    mode,
    businessEffects: effects,
    orderRetrievals: retrievals,
    paidModelCalls: 0,
    measuredAt: new Date().toISOString(),
  };
  await writeFile(
    `evaluations/recovery-process-${mode}.json`,
    JSON.stringify(result, null, 2),
  );
  console.log(JSON.stringify(result));
} finally {
  if (child) {
    const exit = once(child, "exit");
    child.kill("SIGKILL");
    await exit;
  }
  await session.expire();
  await connection.close();
  await db.end();
}
