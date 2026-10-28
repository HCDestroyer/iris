import path from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

export default defineConfig({
    resolve: {
        alias: { "@": path.dirname(fileURLToPath(import.meta.url)) },
    },
    test: {
        include: ["lib/**/*.test.ts", "app/**/*.test.ts", "componentes/**/*.test.{ts,tsx}"],
        environment: "node",
    },
});
