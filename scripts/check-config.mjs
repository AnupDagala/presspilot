import { readFile } from "node:fs/promises";
import assert from "node:assert/strict";
import YAML from "yaml";
const compose = YAML.parse(await readFile("compose.yml", "utf8"));
assert.ok(compose.services.postgres.volumes[0].startsWith("business-data:"));
assert.ok(
  compose.services.temporal.volumes[0].startsWith("temporal-infrastructure:"),
);
assert.equal(compose.services.worker.environment.LIVE_MODEL_ENABLED, "false");
for (const service of Object.values(compose.services))
  for (const port of service.ports ?? [])
    assert.ok(
      port.startsWith("127.0.0.1:"),
      "Local containers must bind loopback",
    );
console.log(
  "Compose YAML and isolation checks passed. Docker engine execution is a separate verification.",
);
