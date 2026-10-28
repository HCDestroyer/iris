import path from "node:path";
import { fileURLToPath } from "node:url";
import type { NextConfig } from "next";

const raiz = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

const config: NextConfig = {
    // Imagen de Docker: servidor Node autónomo (apps/web/Dockerfile).
    output: "standalone",
    outputFileTracingRoot: raiz,
    // Los paquetes del monorepo exportan fuente TS.
    transpilePackages: ["@iris/contratos", "@iris/ui"],
    reactStrictMode: true,
    poweredByHeader: false,
    // El lint corre aparte (`pnpm lint`), con su propia configuración plana.
    eslint: { ignoreDuringBuilds: true },
    // La API se alcanza por el proxy de app/api/[...ruta]/route.ts (IRIS_API_URL leída en
    // tiempo de ejecución, no de build), así las cookies de sesión son del mismo origen.
};

export default config;
