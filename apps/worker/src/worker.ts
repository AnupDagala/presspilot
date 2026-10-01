import { Worker, NativeConnection } from "@temporalio/worker";
import * as activities from "./activities";
import { secret } from "./backend";
import { createServer } from "node:http";
import { installLogging } from "./logging";
installLogging();
async function main() {
  const connection = await NativeConnection.connect({
    address: process.env.TEMPORAL_ADDRESS ?? "127.0.0.1:7233",
    tls: process.env.TEMPORAL_TLS === "true",
    apiKey: secret("TEMPORAL_API_KEY") || undefined,
  });
  const worker = await Worker.create({
    connection,
    namespace: process.env.TEMPORAL_NAMESPACE ?? "default",
    taskQueue: process.env.PP_TASK_QUEUE ?? "presspilot",
    workflowsPath: require.resolve("./workflows"),
    activities,
    maxConcurrentActivityTaskExecutions: 2,
    maxConcurrentWorkflowTaskExecutions: 4,
    shutdownGraceTime: "10 seconds",
  });
  const health = createServer((_req, res) => {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(
      JSON.stringify({
        status: "polling",
        taskQueue: process.env.PP_TASK_QUEUE ?? "presspilot",
      }),
    );
  }).listen(
    Number(process.env.WORKER_HEALTH_PORT ?? 9090),
    process.env.WORKER_HEALTH_BIND ?? "127.0.0.1",
  );
  process.on("SIGINT", () => worker.shutdown());
  process.on("SIGTERM", () => worker.shutdown());
  try {
    await worker.run();
  } finally {
    health.close();
    await connection.close();
  }
}
main().catch(() => {
  console.error(
    JSON.stringify({
      event: "worker_failed",
      message: "Worker startup or runtime failed; consult Temporal task status",
    }),
  );
  process.exitCode = 1;
});
