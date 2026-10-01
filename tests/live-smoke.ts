import { readFile, mkdir, writeFile } from "node:fs/promises";
import { Session, db, waitFor } from "./helpers";
if (process.env.LIVE_TEST_ENABLED !== "true")
  throw new Error(
    "Optional paid live smoke requires explicit LIVE_TEST_ENABLED=true, private session and approved server-side limits",
  );
const path = process.env.PRIVATE_SESSION_TOKEN_FILE;
if (!path) throw new Error("Protected PRIVATE_SESSION_TOKEN_FILE is required");
const api = process.env.TEST_API ?? "http://127.0.0.1:8080",
  token = (await readFile(path, "utf8")).trim();
const response = await fetch(api + "/api/session", {
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ accessToken: token }),
});
const actor = (await response.json()).actor;
if (!response.ok || !actor || actor.Sandbox !== false)
  throw new Error(
    "A private authenticated evaluation organization is required",
  );
const session = new Session(
    response.headers.get("set-cookie")!.split(";")[0],
    actor.Org,
  ),
  start = Date.now();
try {
  const c = await session.submit(
    "Please cancel my order.",
    process.env.LIVE_TEST_ORDER ?? "PF-1041",
    "live",
  );
  const snapshot = await waitFor(
    () => session.snapshot(),
    (s) =>
      ["awaiting_approval", "escalated"].includes(
        s.cases.find((x: any) => x.id === c.id)?.status,
      ),
    60000,
  );
  const result = snapshot.cases.find((x: any) => x.id === c.id);
  await mkdir("runtime", { recursive: true });
  await writeFile(
    "runtime/private-live-smoke.json",
    JSON.stringify(
      {
        mode: "live",
        trialCount: 1,
        status: result.status,
        configuration: result.config,
        latencyMs: Date.now() - start,
        executed: false,
        note: "One bounded investigation; operator cannot approve. This is not a repeated-trial benchmark.",
      },
      null,
      2,
    ),
  );
  console.log(
    "Live investigation report saved privately under ignored runtime/. No credentials or private case contents printed.",
  );
} finally {
  await db.end();
}
