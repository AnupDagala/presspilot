import { execFileSync } from "node:child_process";
import { readFile } from "node:fs/promises";
const files = execFileSync(
  "git",
  ["ls-files", "--cached", "--others", "--exclude-standard", "-z"],
  { encoding: "utf8" },
)
  .split("\0")
  .filter(Boolean);
const patterns = [
  /-----BEGIN (?:RSA |EC |OPENSSH )?PRIVATE KEY-----/,
  /\bsk-(?:ant-)?[A-Za-z0-9_-]{24,}\b/,
  /\bxai-[A-Za-z0-9_-]{24,}\b/,
  /postgres(?:ql)?:\/\/[^:\s]+:[^@\s]{8,}@/,
];
const failures = [];
let checked = 0;
for (const file of files) {
  if (/\.(png|zip|woff2?)$/.test(file)) continue;
  let value;
  try {
    value = await readFile(file, "utf8");
  } catch {
    continue;
  }
  checked++;
  const source = value.replace(/\$\{[^}]+\}/g, "x");
  if (patterns.some((p) => p.test(source))) failures.push(file);
}
if (failures.length) {
  console.error("Possible secrets detected in: " + failures.join(", "));
  process.exitCode = 1;
} else
  console.log(
    `Source secret scan passed (${checked} files). Runtime secret directories are ignored. This is a targeted scan, not a comprehensive secret detector.`,
  );
