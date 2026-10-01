import { mkdir, writeFile, access } from "node:fs/promises";
import { randomBytes } from "node:crypto";
await mkdir("runtime/secrets", { recursive: true });
const password = randomBytes(32).toString("hex");
const values = {
  "database-password": password,
  "worker-secret": randomBytes(32).toString("base64"),
  "database-url": `postgres://presspilot:${password}@postgres:5432/presspilot?sslmode=disable`,
  "test-database-url": `postgres://presspilot:${password}@127.0.0.1:55432/presspilot?sslmode=disable`,
};
try {
  await access("runtime/secrets/database-password");
  console.log("Local secrets already exist. No changes made.");
} catch {
  for (const [name, value] of Object.entries(values))
    await writeFile("runtime/secrets/" + name, value, { mode: 0o600 });
  console.log(
    "Created local secrets under ignored runtime/secrets. No values printed.",
  );
}
