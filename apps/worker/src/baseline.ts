export type Intent =
  | { action: "cancel" | "modify"; arguments: Record<string, string> }
  | { escalate: string };
// Conservative deterministic baseline; this is never labelled as live AI.
export function interpret(message: string): Intent {
  const m = message.trim().toLowerCase();
  if (
    /ignore|system prompt|administrator|admin role|sql|execute|bypass|permission|refund|ship|artwork|address|quantity|price|discount/.test(
      m,
    )
  )
    return {
      escalate:
        "Request includes unsupported changes or untrusted instructions; human review required",
    };
  if (
    /maybe|might|if |or |not cancel|don't cancel|do not cancel|uncertain/.test(
      m,
    )
  )
    return {
      escalate:
        "Request intent is ambiguous; obtain explicit customer instruction",
    };
  const cancel = /\bcancel\b/.test(m);
  const finish = /\b(matte|gloss)\b/.exec(m);
  const ref = /customer reference to ["']?([^"'\n]+)["']?/i.exec(message);
  if (cancel && (finish || ref))
    return { escalate: "Multiple consequential actions require clarification" };
  if (cancel) return { action: "cancel", arguments: {} };
  if (/change|modify|set|update/.test(m) && finish)
    return { action: "modify", arguments: { finish: finish[1] } };
  if (ref && /change|modify|set|update/.test(m))
    return {
      action: "modify",
      arguments: { customerReference: ref[1].trim() },
    };
  return {
    escalate:
      "Missing or unsupported intent or attribute value; human review required",
  };
}
