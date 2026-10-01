import { z } from "zod";
export type Run = {
  org: string;
  caseId: string;
  mode: "baseline" | "mocked" | "live";
};
export type Order = {
  id: string;
  version: number;
  state: string;
  attributes: Record<string, unknown>;
};
export type Policy = {
  version: number;
  allowCancellation: boolean;
  modificationFields: string[];
  approvalSeconds: number;
  maxTurns: number;
  maxTools: number;
  maxTokens: number;
};
export type Proposal = {
  id: string;
  status: string;
  expiresAt: string;
  binding: string;
};
export type CaseState = {
  case: {
    id: string;
    orderId: string;
    message: string;
    status: string;
    mode: Run["mode"];
    config: Record<string, unknown>;
  };
  proposal: Proposal | null;
  policy: Policy;
};
export const modelNames = [
  "get_order",
  "get_order_history",
  "get_applicable_policy",
  "check_action_eligibility",
  "propose_order_cancellation",
  "propose_order_modification",
  "escalate_case",
] as const;
export const toolArgs = z
  .object({
    action: z.enum(["cancel", "modify"]).optional(),
    arguments: z.record(z.string(), z.string().max(80)).optional(),
    orderVersion: z.number().int().positive().optional(),
    policyVersion: z.number().int().positive().optional(),
    reason: z.string().min(1).max(1000).optional(),
  })
  .strict();
export type ModelTool = {
  name: (typeof modelNames)[number];
  args: z.infer<typeof toolArgs>;
};
export function validateTool(v: unknown): ModelTool {
  return z
    .object({ name: z.enum(modelNames), args: toolArgs })
    .strict()
    .parse(v);
}
export const toolSchemas = modelNames.map((name) => ({
  name,
  description: (
    {
      get_order:
        "Retrieve authoritative order state and version for the case-bound order.",
      get_order_history: "Retrieve bounded order history.",
      get_applicable_policy: "Retrieve the current versioned policy.",
      check_action_eligibility:
        "Deterministically check proposed action arguments against current state and policy.",
      propose_order_cancellation:
        "Persist a cancellation proposal using retrieved orderVersion and policyVersion. No execution permission.",
      propose_order_modification:
        "Persist one permitted modification with exact arguments and retrieved versions. No execution permission.",
      escalate_case:
        "Escalate unsupported, unsafe, contradictory or insufficiently specified requests with a concise reason.",
    } as Record<string, string>
  )[name],
  parameters: {
    type: "object",
    properties: {
      action: { type: "string", enum: ["cancel", "modify"] },
      arguments: {
        type: "object",
        additionalProperties: { type: "string", maxLength: 80 },
      },
      orderVersion: { type: "integer", minimum: 1 },
      policyVersion: { type: "integer", minimum: 1 },
      reason: { type: "string", maxLength: 1000 },
    },
    additionalProperties: false,
  },
}));
