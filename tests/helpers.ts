import { randomUUID, randomBytes, createHash } from "node:crypto";
import { Pool } from "pg";
import { readFileSync } from "node:fs";
const database = process.env.TEST_DATABASE_URL_FILE
  ? readFileSync(process.env.TEST_DATABASE_URL_FILE, "utf8").trim()
  : (process.env.TEST_DATABASE_URL ??
    "postgres://presspilot@127.0.0.1:55432/presspilot?sslmode=disable");
export const db = new Pool({ connectionString: database, max: 4 });
const api = process.env.TEST_API ?? "http://127.0.0.1:8080";
export class Session {
  constructor(
    public cookie: string,
    public org: string,
    private roleCookies?: Record<string, string>,
  ) {}
  static async create(privateFixture = false) {
    if (privateFixture) {
      const org = randomUUID(),
        client = await db.connect(),
        cookies: Record<string, string> = {};
      try {
        await client.query("BEGIN");
        await client.query(
          "INSERT INTO organizations(id,name,sandbox,expires_at) VALUES($1,'Private fictional evaluation',false,now()+interval '1 hour')",
          [org],
        );
        for (const role of ["operator", "approver", "administrator"]) {
          const token = randomBytes(32).toString("hex");
          cookies[role] = "presspilot_session=" + token;
          await client.query(
            "INSERT INTO users(org_id,id,role) VALUES($1,$2,$2)",
            [org, role],
          );
          await client.query(
            "INSERT INTO sessions VALUES($1,$2,$3,now()+interval '1 hour')",
            [
              createHash("sha256").update(JSON.stringify(token)).digest("hex"),
              org,
              role,
            ],
          );
        }
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
        for (const [oid, state] of [
          ["PF-1041", "queued"],
          ["PF-1042", "queued"],
          ["PF-1043", "production"],
        ])
          await client.query("INSERT INTO orders VALUES($1,$2,1,$3,$4)", [
            org,
            oid,
            state,
            {
              product: "Fictional evaluation cards",
              quantity: 250,
              finish: "matte",
              customerReference: "Launch kit",
            },
          ]);
        await client.query("COMMIT");
        return new Session(cookies.operator, org, cookies);
      } catch (e) {
        await client.query("ROLLBACK");
        throw e;
      } finally {
        client.release();
      }
    }
    const r = await fetch(api + "/api/sandbox", { method: "POST" });
    const data = await r.json();
    if (!r.ok) throw new Error(data.error);
    return new Session(
      r.headers.get("set-cookie")!.split(";")[0],
      data.actor.Org,
    );
  }
  async role(role: string) {
    if (this.roleCookies) {
      if (!this.roleCookies[role]) throw new Error("Unknown evaluation role");
      this.cookie = this.roleCookies[role];
      return;
    }
    const r = await fetch(api + "/api/sandbox/role", {
      method: "POST",
      headers: { Cookie: this.cookie, "Content-Type": "application/json" },
      body: JSON.stringify({ role }),
    });
    if (!r.ok) throw new Error("Role switch failed");
  }
  async gql(
    query: string,
    variables: Record<string, unknown> = {},
  ): Promise<Record<string, any>> {
    const r = await fetch(api + "/graphql", {
      method: "POST",
      headers: { Cookie: this.cookie, "Content-Type": "application/json" },
      body: JSON.stringify({ query, variables }),
    });
    const data = await r.json();
    if (!r.ok || data.errors)
      throw new Error(data.errors?.[0]?.message ?? data.error);
    return data.data;
  }
  async submit(
    message: string,
    orderId = "PF-1041",
    mode = "baseline",
    key = randomUUID(),
  ): Promise<Record<string, any>> {
    return (
      await this.gql(
        "mutation($input:SubmitCaseInput!){submitCase(input:$input)}",
        { input: { orderId, message, mode, idempotencyKey: key } },
      )
    ).submitCase;
  }
  async snapshot() {
    return (await this.gql("{snapshot}")).snapshot;
  }
  async approve(
    p: Record<string, any>,
    decision = "approved",
    binding = p.binding,
  ) {
    return this.gql(
      "mutation($input:DecisionInput!){decideProposal(input:$input)}",
      {
        input: {
          proposalId: p.id,
          decision,
          binding,
          idempotencyKey: randomUUID(),
        },
      },
    );
  }
  async expire() {
    await db.query("UPDATE organizations SET expires_at=now() WHERE id=$1", [
      this.org,
    ]);
  }
}
export async function waitFor<T>(
  fn: () => Promise<T>,
  predicate: (v: T) => boolean,
  timeout = 30000,
): Promise<T> {
  const deadline = Date.now() + timeout;
  while (true) {
    const value = await fn();
    if (predicate(value)) return value;
    if (Date.now() > deadline)
      throw new Error("Timed out waiting for verified state");
    await new Promise((r) => setTimeout(r, 250));
  }
}
