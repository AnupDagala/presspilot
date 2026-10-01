import assert from "node:assert/strict";
import {
  decode,
  LiveProvider,
  destinations,
  ProviderError,
  type ProviderName,
  type ModelConfig,
} from "../apps/worker/src/providers";
export const envelopes: Record<ProviderName, unknown> = {
  openai: {
    model: "contract-model",
    output: [
      {
        type: "function_call",
        call_id: "test-call",
        name: "get_order",
        arguments: "{}",
      },
    ],
    usage: { input_tokens: 15, output_tokens: 8 },
  },
  anthropic: {
    model: "contract-model",
    content: [
      { type: "tool_use", id: "test-call", name: "get_order", input: {} },
    ],
    usage: { input_tokens: 15, output_tokens: 8 },
  },
  xai: {
    model: "contract-model",
    choices: [
      {
        message: {
          tool_calls: [
            {
              id: "test-call",
              type: "function",
              function: { name: "get_order", arguments: "{}" },
            },
          ],
        },
      },
    ],
    usage: { prompt_tokens: 15, completion_tokens: 8 },
  },
  openweight: {
    model: "contract-model",
    choices: [
      {
        message: {
          tool_calls: [
            {
              id: "test-call",
              type: "function",
              function: { name: "get_order", arguments: "{}" },
            },
          ],
        },
      },
    ],
    usage: { prompt_tokens: 15, completion_tokens: 8 },
  },
};
export function providerContract(provider: ProviderName) {
  const r = decode(provider, envelopes[provider], "configured-model");
  assert.equal(r.calls[0].name, "get_order");
  assert.deepEqual(r.calls[0].args, {});
  assert.deepEqual(r.usage, { input: 15, output: 8 });
  assert.equal(r.model, "contract-model");
}
export async function providerFailure(kind: string) {
  if (kind === "malformed") {
    assert.throws(
      () =>
        decode(
          "openai",
          {
            output: [
              { type: "function_call", name: "get_order", arguments: "{" },
            ],
          },
          "fixture",
        ),
      ProviderError,
    );
    return;
  }
  const priorEnabled = process.env.LIVE_MODEL_ENABLED,
    priorKey = process.env.OPENAI_API_KEY;
  process.env.LIVE_MODEL_ENABLED = "true";
  process.env.OPENAI_API_KEY = "contract-placeholder-not-a-credential";
  try {
    const config: ModelConfig = {
      provider: "openai",
      model: "contract-model",
      endpoint: destinations.openai,
      maxOutputTokens: 32,
      promptVersion: "contract-only",
    };
    const status = kind === "retryable" ? 503 : 401;
    const provider = new LiveProvider(
      config,
      async () => new Response("{}", { status }),
      false,
    );
    await assert.rejects(
      provider.turn([{ role: "user", content: "fixture" }]),
      (e: unknown) =>
        e instanceof ProviderError &&
        e.code === (kind === "retryable" ? "RETRYABLE" : "PERMANENT"),
    );
  } finally {
    if (priorEnabled === undefined) delete process.env.LIVE_MODEL_ENABLED;
    else process.env.LIVE_MODEL_ENABLED = priorEnabled;
    if (priorKey === undefined) delete process.env.OPENAI_API_KEY;
    else process.env.OPENAI_API_KEY = priorKey;
  }
}
