CREATE TABLE IF NOT EXISTS organizations (
 id text PRIMARY KEY, name text NOT NULL, sandbox boolean NOT NULL DEFAULT false,
 expires_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS users (
 org_id text NOT NULL REFERENCES organizations(id), id text NOT NULL,
 role text NOT NULL CHECK(role IN ('operator','approver','administrator')),
 active boolean NOT NULL DEFAULT true, PRIMARY KEY(org_id,id)
);
CREATE TABLE IF NOT EXISTS sessions (
 digest text PRIMARY KEY, org_id text NOT NULL, user_id text NOT NULL, expires_at timestamptz NOT NULL,
 FOREIGN KEY(org_id,user_id) REFERENCES users(org_id,id)
);
CREATE TABLE IF NOT EXISTS orders (
 org_id text NOT NULL REFERENCES organizations(id), id text NOT NULL,
 version integer NOT NULL CHECK(version>0), state text NOT NULL CHECK(state IN ('queued','production','cancelled','shipped')),
 attributes jsonb NOT NULL, PRIMARY KEY(org_id,id)
);
CREATE TABLE IF NOT EXISTS policies (
 org_id text NOT NULL REFERENCES organizations(id), version integer NOT NULL,
 document jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(org_id,version)
);
CREATE TABLE IF NOT EXISTS cases (
 org_id text NOT NULL, id text NOT NULL, order_id text NOT NULL, requester_id text NOT NULL,
 message text NOT NULL CHECK(length(message)<=4000), mode text NOT NULL CHECK(mode IN ('baseline','mocked','live')),
 status text NOT NULL DEFAULT 'investigating', summary text NOT NULL DEFAULT '',
 workflow_started boolean NOT NULL DEFAULT false, config jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(org_id,id),
 FOREIGN KEY(org_id,order_id) REFERENCES orders(org_id,id), FOREIGN KEY(org_id,requester_id) REFERENCES users(org_id,id)
);
CREATE TABLE IF NOT EXISTS proposals (
 org_id text NOT NULL, id text NOT NULL, case_id text NOT NULL, order_id text NOT NULL,
 order_version integer NOT NULL, policy_version integer NOT NULL, action text NOT NULL,
 arguments jsonb NOT NULL, evidence jsonb NOT NULL, binding text NOT NULL,
 status text NOT NULL DEFAULT 'pending', expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(org_id,id),
 UNIQUE(org_id,case_id,binding), FOREIGN KEY(org_id,case_id) REFERENCES cases(org_id,id),
 FOREIGN KEY(org_id,order_id) REFERENCES orders(org_id,id), FOREIGN KEY(org_id,policy_version) REFERENCES policies(org_id,version)
);
CREATE TABLE IF NOT EXISTS approvals (
 org_id text NOT NULL, proposal_id text NOT NULL, user_id text NOT NULL,
 binding text NOT NULL, decision text NOT NULL CHECK(decision IN ('approved','rejected')),
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(org_id,proposal_id),
 FOREIGN KEY(org_id,proposal_id) REFERENCES proposals(org_id,id), FOREIGN KEY(org_id,user_id) REFERENCES users(org_id,id)
);
CREATE TABLE IF NOT EXISTS executions (
 org_id text NOT NULL, id text NOT NULL, proposal_id text NOT NULL, case_id text NOT NULL,
 binding text NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(org_id,id),
 UNIQUE(org_id,proposal_id), UNIQUE(org_id,case_id), FOREIGN KEY(org_id,proposal_id) REFERENCES proposals(org_id,id)
);
CREATE TABLE IF NOT EXISTS requests (
 org_id text NOT NULL REFERENCES organizations(id), key text NOT NULL, digest text NOT NULL, result jsonb NOT NULL,
 PRIMARY KEY(org_id,key)
);
CREATE TABLE IF NOT EXISTS audit_events (
 seq bigserial PRIMARY KEY, org_id text NOT NULL REFERENCES organizations(id), case_id text,
 actor text NOT NULL, kind text NOT NULL, data jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS audit_org ON audit_events(org_id,seq);
CREATE TABLE IF NOT EXISTS tool_calls (
 org_id text NOT NULL, case_id text NOT NULL, call_id text NOT NULL, name text NOT NULL,
 input jsonb NOT NULL, output jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(org_id,case_id,call_id), FOREIGN KEY(org_id,case_id) REFERENCES cases(org_id,id)
);
CREATE TABLE IF NOT EXISTS webhook_events (
 shop text NOT NULL, event_id text NOT NULL, topic text NOT NULL, body jsonb NOT NULL,
 status text NOT NULL DEFAULT 'received', created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(shop,event_id)
);
CREATE TABLE IF NOT EXISTS evaluation_runs (
 org_id text NOT NULL REFERENCES organizations(id), id text NOT NULL, report jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(org_id,id)
);
