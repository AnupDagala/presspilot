import { Runtime, DefaultLogger } from "@temporalio/worker";
export function installLogging() {
  Runtime.install({
    logger: new DefaultLogger("INFO", (entry) => {
      const allowed = [
        "taskQueue",
        "workflowId",
        "activityType",
        "attempt",
        "durationMs",
        "state",
        "namespace",
      ];
      const metadata: Record<string, unknown> = {};
      for (const key of allowed)
        if (entry.meta?.[key] !== undefined) metadata[key] = entry.meta[key];
      console.log(
        JSON.stringify({
          severity: entry.level,
          message: entry.message,
          ...metadata,
        }),
      );
    }),
  });
}
