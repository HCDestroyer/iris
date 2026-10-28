import { defineConfig, devices } from "@playwright/test";

// Pruebas del panel en escritorio (1280 px) y teléfono (390 px), en claro y oscuro. La API se
// simula con page.route (e2e/api-falsa.ts): no hace falta la API Go. Puerto de I3: 3503.
const puerto = Number(process.env.PORT_E2E ?? 3503);
const baseURL = `http://127.0.0.1:${puerto}`;
const escritorio = { viewport: { width: 1280, height: 800 } };
const telefono = { ...devices["Pixel 7"], viewport: { width: 390, height: 844 }, deviceScaleFactor: 2 };

export default defineConfig({
    testDir: "e2e",
    fullyParallel: true,
    forbidOnly: Boolean(process.env.CI),
    retries: process.env.CI ? 1 : 0,
    reporter: process.env.CI ? [["list"], ["html", { open: "never" }]] : "list",
    use: {
        baseURL,
        locale: "es-GT",
        timezoneId: "America/Guatemala",
        trace: "retain-on-failure",
    },
    projects: [
        { name: "escritorio-claro", use: { ...devices["Desktop Chrome"], ...escritorio, colorScheme: "light" } },
        { name: "escritorio-oscuro", use: { ...devices["Desktop Chrome"], ...escritorio, colorScheme: "dark" } },
        { name: "telefono-claro", use: { ...telefono, colorScheme: "light" } },
        { name: "telefono-oscuro", use: { ...telefono, colorScheme: "dark" } },
    ],
    webServer: {
        // Con IRIS_E2E_SIN_BUILD=1 reutiliza el build existente (p. ej. en CI tras `pnpm build`).
        command: process.env.IRIS_E2E_SIN_BUILD ? `pnpm exec next start -p ${puerto}` : `pnpm build && pnpm exec next start -p ${puerto}`,
        url: `${baseURL}/entrar`,
        reuseExistingServer: !process.env.CI,
        timeout: 180_000,
        // Ninguna petición debe llegar al proxy: si una ruta no está simulada, falla (502).
        env: { IRIS_API_URL: "http://127.0.0.1:9" },
    },
});
