# Paper Finch Printworks demonstration policy

This is a fictional printing business and an explicitly authored demonstration policy. It is not affiliated with Sticker Mule or any merchant. No public merchant policy has been imported as the complete operating specification.

1. An order is eligible for cancellation only while `state=queued`, cancellation is enabled in the current policy, no additional cancellation arguments are present, and an active approver or administrator approves the exact proposal before expiry.
2. A queued order may change **one** permitted attribute per case. Initial attributes are `finish` (`matte` or `gloss`) and `customerReference` (nonempty text, at most 80 bytes). Quantity, price, shipping, payment and artwork are not permitted.
3. After production starts, automatic cancellation/modification is not permitted. Explain that production restricts the action, retain evidence and escalate to human review. The system does not invent refunds or alternate fulfillment promises.
4. Missing values, unclear intent, multiple consequential actions, contradictory instructions and unsupported changes escalate. The baseline is intentionally conservative; it does not claim broad natural-language understanding.
5. Approval lifetime defaults to 600 seconds. Runtime policy bounds are 1–12 model turns, 3–24 business tool calls, 256–20,000 tokens and 5–3,600 approval seconds. Code always rejects unsupported fields, even if a policy administrator tries to enable them.
6. Every policy edit creates a new immutable version. Every order mutation increments its version. Either version changing invalidates the old proposal at execution. Approval never permits substituting arguments.
7. Orders transition from queued to cancelled only through the privileged validated executor. Simulated queued-to-production events are administrator-only sandbox features. Refunds, shipment changes, artwork analysis and fleet management are extension points, not implemented workflows.

Roles: operators submit and inspect; approvers approve/reject and inspect; administrators submit, inspect, approve and configure supported policies/limits. All roles are scoped to active organization membership. Public sandbox role switching exists solely to demonstrate these controls.
