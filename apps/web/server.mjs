import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { resolve, extname, sep } from "node:path";
import { GoogleAuth } from "google-auth-library";
const root = resolve("apps/web/dist"),
  api = process.env.BUSINESS_API ?? "http://api:8080",
  auth = new GoogleAuth();
const types = {
  ".html": "text/html",
  ".js": "application/javascript",
  ".css": "text/css",
  ".json": "application/json",
  ".svg": "image/svg+xml",
  ".png": "image/png",
};
createServer(async (req, res) => {
  try {
    res.setHeader("X-Content-Type-Options", "nosniff");
    res.setHeader(
      "Content-Security-Policy",
      "default-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
    );
    res.setHeader("Referrer-Policy", "same-origin");
    const path = new URL(req.url, "http://localhost").pathname;
    if (path === "/healthz") {
      res.writeHead(200);
      res.end("ready");
      return;
    }
    if (
      path === "/graphql" ||
      ["/api/sandbox", "/api/sandbox/role", "/api/session"].includes(path)
    ) {
      if (req.method !== "POST") {
        res.writeHead(405);
        res.end();
        return;
      }
      const expected =
        (req.headers["x-forwarded-proto"] ?? "http") + "://" + req.headers.host;
      if (req.headers.origin && req.headers.origin !== expected) {
        res.writeHead(403);
        res.end("Origin rejected");
        return;
      }
      let size = 0;
      const chunks = [];
      for await (const chunk of req) {
        size += chunk.length;
        if (size > 65536) {
          res.writeHead(413);
          res.end();
          return;
        }
        chunks.push(chunk);
      }
      const headers = {
        "Content-Type": "application/json",
        Cookie: req.headers.cookie ?? "",
      };
      if (req.headers.traceparent)
        headers.traceparent = String(req.headers.traceparent);
      if (process.env.GCP_AUTH === "true") {
        const client = await auth.getIdTokenClient(api);
        const signed = await client.getRequestHeaders();
        headers["X-Serverless-Authorization"] =
          signed.get("authorization") ?? "";
      }
      const response = await fetch(api + path, {
        method: "POST",
        headers,
        body: Buffer.concat(chunks),
        signal: AbortSignal.timeout(15000),
        redirect: "error",
      });
      const session = response.headers.get("set-cookie");
      if (session) res.setHeader("Set-Cookie", session);
      res.writeHead(response.status, {
        "Content-Type": "application/json",
        "Cache-Control": "no-store",
      });
      res.end(await response.text());
      return;
    }
    if (req.method !== "GET") {
      res.writeHead(405);
      res.end();
      return;
    }
    let target = resolve(root, "." + decodeURIComponent(path));
    if (target !== root && !target.startsWith(root + sep)) {
      res.writeHead(403);
      res.end();
      return;
    }
    let content;
    try {
      content = await readFile(target);
    } catch {
      target = resolve(root, "index.html");
      content = await readFile(target);
    }
    res.writeHead(200, {
      "Content-Type": types[extname(target)] ?? "application/octet-stream",
    });
    res.end(content);
  } catch {
    res.writeHead(502, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ error: "Gateway temporarily unavailable" }));
  }
}).listen(
  Number(process.env.PORT ?? 8080),
  process.env.WEB_BIND ?? "127.0.0.1",
);
