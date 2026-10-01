import {
  envelopes,
  providerContract,
  providerFailure,
} from "./provider-fixtures";
import assert from "node:assert/strict";
import { test } from "node:test";
import {
  decode,
  LiveProvider,
  destinations,
  privateIP,
  validateDestination,
  ProviderError,
  type ProviderName,
} from "../apps/worker/src/providers";
import { validateTool } from "../apps/worker/src/contracts";
for (const provider of Object.keys(destinations) as ProviderName[])
  test(`${provider} recorded response contract`, () =>
    providerContract(provider));
test("provider tool names and unknown arguments cannot grant permissions", () => {
  assert.throws(() =>
    validateTool({ name: "execute_approved_action", args: {} }),
  );
  assert.throws(() =>
    validateTool({ name: "get_order", args: { org: "another-org" } }),
  );
  assert.throws(
    () =>
      decode(
        "openai",
        { output: [{ type: "function_call", name: "shell", arguments: "{}" }] },
        "fixture",
      ),
    ProviderError,
  );
});
test("inference endpoint allowlist rejects SSRF and redirects", () => {
  for (const u of [
    "http://127.0.0.1",
    "https://169.254.169.254/",
    "https://api.openai.com.evil.test/v1/responses",
    "https://api.openai.com/v1/responses?redirect=internal",
  ]) {
    assert.throws(() => validateDestination("openai", u));
  }
  for (const a of [
    "127.0.0.1",
    "10.0.1.1",
    "169.254.169.254",
    "172.16.0.1",
    "192.168.1.1",
    "::1",
    "fd00::1",
    "::ffff:127.0.0.1",
  ]) {
    assert.equal(privateIP(a), true);
  }
  assert.equal(privateIP("8.8.8.8"), false);
});
for (const kind of ["malformed", "retryable", "permanent"])
  test(`provider ${kind} failure contract`, () => providerFailure(kind));
test("all adapters emit provider-specific tool request envelopes", async () => {
  const prior = process.env.LIVE_MODEL_ENABLED;
  process.env.LIVE_MODEL_ENABLED = "true";
  const keys = {
    openai: "OPENAI_API_KEY",
    anthropic: "ANTHROPIC_API_KEY",
    xai: "XAI_API_KEY",
    openweight: "INFERENCE_API_KEY",
  };
  try {
    for (const provider of Object.keys(destinations) as ProviderName[]) {
      const key = keys[provider],
        old = process.env[key];
      process.env[key] = "contract-placeholder";
      let sent: Record<string, any> = {};
      try {
        const client = new LiveProvider(
          {
            provider,
            model: "contract-model",
            endpoint: destinations[provider],
            maxOutputTokens: 32,
            promptVersion: "contract",
          },
          async (_url, init) => {
            sent = JSON.parse(String(init?.body));
            assert.equal(init?.redirect, "error");
            return new Response(JSON.stringify(envelopes[provider]), {
              status: 200,
            });
          },
          false,
        );
        await client.turn([
          { role: "user", content: "Cancel" },
          {
            role: "assistant",
            content: "",
            callId: "a",
            tool: { name: "get_order", args: {} },
          },
          { role: "tool", content: "{}", callId: "a" },
        ]);
        assert.equal(sent.model, "contract-model");
        assert.ok(sent.tools.length > 0);
        if (provider === "openai") assert.equal(sent.store, false);
        if (provider === "anthropic") assert.ok(sent.tools[0].input_schema);
      } finally {
        if (old === undefined) delete process.env[key];
        else process.env[key] = old;
      }
    }
  } finally {
    if (prior === undefined) delete process.env.LIVE_MODEL_ENABLED;
    else process.env.LIVE_MODEL_ENABLED = prior;
  }
});
