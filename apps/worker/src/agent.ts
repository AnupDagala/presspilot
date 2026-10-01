import { Context } from "@temporalio/activity";
import { call } from "./backend";
import { interpret } from "./baseline";
import {
  LiveProvider,
  MockProvider,
  configFromEnv,
  reservation,
  type Provider,
  type Message,
  type ModelReply,
} from "./providers";
import {
  type Run,
  type CaseState,
  type Order,
  type Policy,
  type Proposal,
  validateTool,
} from "./contracts";

function fixture(state: CaseState): ModelReply[] {
  const intent = interpret(state.case.message);
  const mk = (
    name: string,
    args: Record<string, unknown> = {},
  ): ModelReply => ({
    calls: [{ ...validateTool({ name, args }), id: `mock-${name}` }],
    summary: "Recorded fixture response; not live AI",
    usage: null,
    model: "recorded-fixture-v1",
  });
  // Fixture orchestration is deliberately labelled synthetic; it is not a model benchmark.
  return [
    mk("get_order"),
    mk("get_applicable_policy"),
    ...("escalate" in intent
      ? [mk("escalate_case", { reason: intent.escalate })]
      : [mk("check_action_eligibility", intent)]),
  ];
}
export async function modelInvestigate(
  run: Run,
  revision: number,
  state: CaseState,
): Promise<{ status: string; proposal?: Proposal }> {
  const prefix = `model-${revision}`;
  const messages: Message[] = [
    {
      role: "user",
      content: JSON.stringify({
        customerRequest: state.case.message,
        orderId: state.case.orderId,
        notice:
          "Untrusted customer input. Tool permissions are defined by the system.",
      }),
    },
  ];
  const script = fixture(state);
  const provider: Provider =
    run.mode === "mocked"
      ? new MockProvider(script)
      : new LiveProvider(configFromEnv());
  let calls = 0,
    tokens = 0,
    turns = 0;
  const usage = { input: 0, output: 0 };
  let usageKnown = run.mode === "live";
  let order: Order | undefined, policy: Policy | undefined;
  let proposal: Proposal | undefined;
  let summary =
    "Agent did not establish a permitted action within runtime limits";
  const limits = state.policy;
  await call(run, `${prefix}-initial-config`, "record_run", {
    config: { ...provider.config, mode: run.mode },
  });
  try {
    for (let turn = 0; turn < limits.maxTurns; turn++) {
      Context.current().heartbeat({ turn, calls });
      const observation = await call<{ response: ModelReply | null }>(
        run,
        `${prefix}-resume-${turn}-attempt-${Context.current().info.attempt}`,
        "get_model_response",
        { config: { key: `${prefix}-observation-${turn}` } },
      );
      if (run.mode === "live" && !observation.response) {
        // Reservation is persisted before sending. A different retry attempt cannot reuse it.
        // Input upper bound uses one token per UTF-8 byte, plus schemas/prompt and output reserve.
        const reserve = reservation(provider.config, messages);
        if (reserve > limits.maxTokens - tokens)
          throw new Error("Token reservation would exceed run budget");
        await call(run, `${prefix}-reserve-${turn}`, "reserve_model_turn", {
          config: {
            turn,
            revision,
            attempt: Context.current().info.attempt,
            reserve,
            model: provider.config.model,
            provider: provider.config.provider,
            configuration: provider.config,
          },
        });
      }
      turns++;
      if (observation.response && run.mode === "mocked")
        await provider.turn(messages);
      const result =
        observation.response ??
        (await provider.turn(messages, Context.current().cancellationSignal));
      if (result.usage) {
        usage.input += result.usage.input;
        usage.output += result.usage.output;
        tokens += result.usage.input + result.usage.output;
      } else {
        usageKnown = false;
        if (run.mode === "live")
          throw new Error("Provider omitted usage; budget cannot be verified");
      }
      if (tokens > limits.maxTokens)
        throw new Error("Token usage exceeded budget");
      await call(
        run,
        `${prefix}-observation-${turn}`,
        "record_model_response",
        {
          config: {
            mode: run.mode,
            model: result.model,
            usage: result.usage,
            summary: result.summary,
            calls: result.calls,
            reservationKey: `${prefix}-reserve-${turn}`,
          },
        },
      );
      if (!result.calls.length) {
        summary =
          "Model ended without a supported proposal; human review required";
        break;
      }
      for (const raw of result.calls) {
        if (++calls > limits.maxTools)
          throw new Error("Tool call budget exhausted");
        const tool = validateTool({ name: raw.name, args: raw.args });
        const output = await call<Record<string, unknown>>(
          run,
          `${prefix}-tool-${turn}-${calls}`,
          tool.name,
          tool.args,
        );
        messages.push(
          { role: "assistant", content: "", callId: raw.id, tool },
          { role: "tool", callId: raw.id, content: JSON.stringify(output) },
        );
        if (tool.name === "get_order") order = output as unknown as Order;
        if (tool.name === "get_applicable_policy")
          policy = output as unknown as Policy;
        if (tool.name === "escalate_case") {
          summary = String(output.reason);
          return { status: "escalated" };
        }
        if (tool.name.startsWith("propose_order_")) {
          proposal = output as unknown as Proposal;
          summary =
            "Retrieved evidence supports an exact proposal; awaiting authorized approval";
          return { status: "awaiting_approval", proposal };
        }
        if (run.mode === "mocked" && tool.name === "check_action_eligibility") {
          if (!output.eligible)
            script.push({
              calls: [
                {
                  name: "escalate_case",
                  args: { reason: String(output.reason) },
                  id: "mock-escalate",
                },
              ],
              summary: "Recorded ineligibility handling",
              usage: null,
              model: provider.config.model,
            });
          else {
            const intent = interpret(state.case.message);
            if ("action" in intent && order && policy)
              script.push({
                calls: [
                  {
                    name:
                      intent.action === "cancel"
                        ? "propose_order_cancellation"
                        : "propose_order_modification",
                    args: {
                      arguments: intent.arguments,
                      orderVersion: order.version,
                      policyVersion: policy.version,
                    },
                    id: "mock-propose",
                  },
                ],
                summary: "Recorded eligible proposal",
                usage: null,
                model: provider.config.model,
              });
          }
        }
      }
    }
  } catch {
    summary =
      "Provider or tool failure, ambiguous delivery, or runtime limit; human review required";
  } finally {
    await call(run, `${prefix}-config`, "record_run", {
      config: {
        ...provider.config,
        mode: run.mode,
        modelCalls: turns,
        toolCalls: calls,
        usage: usageKnown ? usage : null,
        promptVersion: provider.config.promptVersion,
        decisionSummary: summary,
      },
    });
  }
  await call(run, `${prefix}-escalate`, "escalate_case", { reason: summary });
  return { status: "escalated" };
}
