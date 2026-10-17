import { readFileSync } from "node:fs";
import { fileURLToPath, URL } from "node:url";

import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [react(), manualTestServer()],
  build: {
    outDir: fileURLToPath(new URL("../internal/webui/dist", import.meta.url)),
    emptyOutDir: true,
    assetsDir: "assets",
    sourcemap: false,
  },
  test: {
    environment: "jsdom",
    environmentOptions: {
      jsdom: {
        url: "https://x-insight.local/",
      },
    },
    css: true,
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    restoreMocks: true,
    coverage: {
      provider: "v8",
      reporter: ["text", "json-summary"],
      thresholds: {
        statements: 69,
        branches: 60,
        functions: 71,
        lines: 71,
      },
    },
  },
});

function manualTestServer(): Plugin {
  const manualRoot = fileURLToPath(new URL("../internal/webui/manual/zh-CN/", import.meta.url));
  return {
    name: "xinsight-manual-test-server",
    configureServer(server) {
      server.middlewares.use((request, response, next) => {
        const path = request.url?.split("?", 1)[0];
        const head = request.method === "HEAD";
        if ((request.method === "GET" || head) && (path === "/manual/zh-CN" || path === "/manual/zh-CN/")) {
          response.statusCode = 200;
          response.setHeader("Content-Type", "text/html; charset=utf-8");
          response.setHeader("Cache-Control", "no-store");
          response.end(head ? undefined : readFileSync(`${manualRoot}/index.html`));
          return;
        }
        if ((request.method === "GET" || head) && path === "/manual/zh-CN/manual.css") {
          response.statusCode = 200;
          response.setHeader("Content-Type", "text/css; charset=utf-8");
          response.setHeader("Cache-Control", "no-store");
          response.end(head ? undefined : readFileSync(`${manualRoot}/manual.css`));
          return;
        }
        if ((request.method === "GET" || head) && path === "/manual/zh-CN/manual-icon.svg") {
          response.statusCode = 200;
          response.setHeader("Content-Type", "image/svg+xml");
          response.setHeader("Cache-Control", "no-store");
          response.end(head ? undefined : readFileSync(`${manualRoot}/manual-icon.svg`));
          return;
        }
        next();
      });
    },
  };
}
