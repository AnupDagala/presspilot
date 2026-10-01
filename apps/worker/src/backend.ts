import { readFileSync } from "node:fs";
import { ApplicationFailure, Context } from "@temporalio/activity";
import type { Run } from "./contracts";
import { cloudHeaders } from "./cloud-auth";
export function secret(name: string): string {
  const path = process.env[name + "_FILE"];
  return path ? readFileSync(path, "utf8").trim() : (process.env[name] ?? "");
}
export async function call<T>(
  run: Run,
  callId: string,
  name: string,
  args: Record<string, unknown> = {},
): Promise<T> {
  const api = process.env.BUSINESS_API ?? "http://127.0.0.1:8080";
  let cancellation: AbortSignal | undefined;
  try {
    cancellation = Context.current().cancellationSignal;
  } catch {
    /* Offline contract/reconciliation calls have no activity context. */
  }
  const response = await fetch(api + "/internal/tools", {
    method: "POST",
    headers: {
      ...(await cloudHeaders(api)),
      "Content-Type": "application/json",
      Authorization: "Bearer " + secret("WORKER_SECRET"),
    },
    body: JSON.stringify({ ...run, mode: undefined, callId, name, args }),
    signal: cancellation
      ? AbortSignal.any([cancellation, AbortSignal.timeout(10000)])
      : AbortSignal.timeout(10000),
  });
  const text = await response.text();
  if (text.length > 32768)
    throw ApplicationFailure.nonRetryable(
      "Backend output exceeded limit",
      "LIMIT",
    );
  const out = JSON.parse(text);
  if (!response.ok) {
    if (response.status >= 500)
      throw ApplicationFailure.retryable(
        "Backend temporarily unavailable",
        "RETRYABLE",
      );
    throw ApplicationFailure.nonRetryable(
      String(out.error ?? "Tool rejected"),
      String(out.code ?? "PERMANENT"),
    );
  }
  return out as T;
}
