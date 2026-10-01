import { execFileSync } from "node:child_process";
import { readFileSync, existsSync } from "node:fs";
const tracked = execFileSync("git", ["ls-files", "-z"], { encoding: "utf8" })
  .split("\0")
  .filter(Boolean);
const unsafe = tracked.filter(
  (file) =>
    /(^|\/)(runtime|node_modules|dist|test-results|playwright-report|private-data|backups|\.tools|\.terraform)\//.test(
      file,
    ) ||
    /\.(dump|backup|sqlite3?|db|pem|key|tfplan)$/.test(file) ||
    /\.tfstate(?:\.|$)|\.tfvars(?:\.json)?$/.test(file) ||
    (/(^|\/)\.env(?:\.|$)/.test(file) && file !== ".env.example"),
);
if (unsafe.length)
  throw new Error("Unsafe tracked publication paths: " + unsafe.join(", "));
const documents = tracked.filter((file) => /\.md$/.test(file));
for (const file of documents) {
  const text = readFileSync(file, "utf8");
  if (
    /[A-Za-z]:[\\/](?:Users|Documents|Program Files)[\\/]|\/(?:Users|home)\/[^\s/]+\//.test(
      text,
    )
  )
    throw new Error("Machine-specific path in " + file);
}
const readme = readFileSync("README.md", "utf8");
for (const match of readme.matchAll(/\]\(([^)]+)\)/g)) {
  const target = match[1];
  if (!/^[a-z]+:|^#/.test(target) && !existsSync(target.split("#")[0]))
    throw new Error("Broken README link: " + target);
}
const samples = [
  ".env",
  ".env.production",
  "runtime/secrets/example",
  "private-data/customer.json",
  "backups/database.dump",
  "infra/example.tfstate",
  "infra/private.tfvars",
  ".tools/go/bin/go.exe",
  "service-account-private.json",
];
for (const file of samples)
  execFileSync("git", ["check-ignore", "--quiet", file]);
console.log(
  `Publication hygiene passed: ${tracked.length} tracked paths and ${documents.length} documents; credential/state exclusions verified. Secret scanning is a separate check.`,
);
