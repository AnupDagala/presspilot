import { Pool } from "pg";
import { randomBytes, createHash } from "node:crypto";
import { readFile, mkdir, writeFile } from "node:fs/promises";
const database = process.env.DATABASE_URL_FILE
  ? await readFile(process.env.DATABASE_URL_FILE, "utf8")
  : process.env.DATABASE_URL;
if (!database)
  throw new Error("Set DATABASE_URL_FILE to a private connection file");
const pool = new Pool({ connectionString: database.trim() }),
  client = await pool.connect(),
  org = randomBytes(16).toString("hex"),
  token = randomBytes(32).toString("hex");
try {
  await client.query("BEGIN");
  await client.query(
    "INSERT INTO organizations(id,name,sandbox) VALUES($1,'Paper Finch private evaluation',false)",
    [org],
  );
  for (const role of ["operator", "approver", "administrator"])
    await client.query("INSERT INTO users(org_id,id,role) VALUES($1,$2,$2)", [
      org,
      role,
    ]);
  const policy = {
    version: 1,
    allowCancellation: true,
    modificationFields: ["customerReference", "finish"],
    approvalSeconds: 600,
    maxTurns: 8,
    maxTools: 16,
    maxTokens: 20000,
  };
  await client.query(
    "INSERT INTO policies(org_id,version,document) VALUES($1,1,$2)",
    [org, policy],
  );
  await client.query("INSERT INTO orders VALUES($1,'PF-1041',1,'queued',$2)", [
    org,
    {
      product: "Fictional private evaluation cards",
      quantity: 250,
      finish: "matte",
      customerReference: "Private evaluation",
    },
  ]);
  await mkdir("runtime/secrets", { recursive: true });
  // Provision separately scoped credentials without printing their values.
  for (const role of ["operator", "approver", "administrator"]) {
    const scopedToken =
      role === "operator" ? token : randomBytes(32).toString("hex");
    await client.query(
      "INSERT INTO sessions VALUES($1,$2,$3,now()+interval '1 hour')",
      [
        createHash("sha256").update(JSON.stringify(scopedToken)).digest("hex"),
        org,
        role,
      ],
    );
    await writeFile(`runtime/secrets/private-${role}-session`, scopedToken, {
      mode: 0o600,
    });
  }
  await client.query("COMMIT");
  console.log(
    "Created private evaluation organization " +
      org +
      ". One-hour role-scoped sessions saved to ignored runtime/secrets/private-<role>-session. Use the sign-in form. No provider calls made.",
  );
} catch (e) {
  await client.query("ROLLBACK");
  throw e;
} finally {
  client.release();
  await pool.end();
}
