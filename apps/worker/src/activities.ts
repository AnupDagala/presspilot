import { Context } from "@temporalio/activity";
import { call } from "./backend";
import { interpret } from "./baseline";
import { modelInvestigate } from "./agent";
import type { Run, CaseState, Order, Policy, Proposal } from "./contracts";
export async function inspect(run: Run, tick: number): Promise<CaseState> {
  return call(run, `inspect-${tick}`, "get_case");
}
export async function investigate(
  run: Run,
  revision: number,
): Promise<{ status: string; proposal?: Proposal }> {
  const prefix = `revision-${revision}`;
  Context.current().heartbeat("retrieving evidence");
  const state = await call<CaseState>(run, prefix + "-case", "get_case");
  if (["completed", "escalated"].includes(state.case.status))
    return { status: state.case.status };
  if (run.mode !== "baseline") return modelInvestigate(run, revision, state);
  const order = await call<Order>(run, prefix + "-order", "get_order");
  const policy = await call<Policy>(
    run,
    prefix + "-policy",
    "get_applicable_policy",
  );
  await call(run, prefix + "-config", "record_run", {
    config: {
      mode: "baseline",
      provider: null,
      model: null,
      promptVersion: "baseline-1",
      modelCalls: 0,
      usage: null,
      policyVersion: policy.version,
    },
  });
  const intent = interpret(state.case.message);
  const escalate = async (reason: string) => {
    await call(run, prefix + "-escalate", "escalate_case", { reason });
    return { status: "escalated" };
  };
  if ("escalate" in intent) return escalate(intent.escalate);
  const eligibility = await call<{ eligible: boolean; reason: string }>(
    run,
    prefix + "-eligibility",
    "check_action_eligibility",
    intent,
  );
  if (!eligibility.eligible) return escalate(eligibility.reason);
  const proposal = await call<Proposal>(
    run,
    prefix + "-propose",
    intent.action === "cancel"
      ? "propose_order_cancellation"
      : "propose_order_modification",
    {
      arguments: intent.arguments,
      orderVersion: order.version,
      policyVersion: policy.version,
    },
  );
  return { status: "awaiting_approval", proposal };
}
export async function execute(
  run: Run,
  proposalId: string,
): Promise<{ status: string }> {
  return call(run, "execute-" + proposalId, "execute_approved_action", {
    proposalId,
  });
}
export async function escalateFailure(run: Run, reason: string): Promise<void> {
  await call(run, "workflow-terminal-escalation", "escalate_case", {
    reason: reason.slice(0, 1000),
  });
}
