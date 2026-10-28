import { randomUUID } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import withSerwistInit from "@serwist/next";
import type { NextConfig } from "next";

const raiz = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");

// Service worker de la app de campo (Serwist, I4): precachea el shell de /campo y los estáticos
// del build para abrir sin señal. Solo controla /campo (alcance registrado en
// app/campo/registrar-sw.tsx); el panel y /soporte no pasan por él. En desarrollo no se genera.
const conSerwist = withSerwistInit({
    swSrc: "app/sw.ts",
    swDest: "public/sw.js",
    register: false,
    reloadOnOnline: false,
    disable: process.env.NODE_ENV === "development",
    // El shell (HTML estático de /campo) cambia en cada build: revisión nueva por build.
    additionalPrecacheEntries: [{ url: "/campo", revision: randomUUID() }],
});

const config: NextConfig = {
    // Imagen de Docker: servidor Node autónomo (apps/web/Dockerfile).
    output: "standalone",
    outputFileTracingRoot: raiz,
    // Los paquetes del monorepo exportan fuente TS.
    transpilePackages: ["@iris/contratos", "@iris/ui", "@iris/sync"],
    reactStrictMode: true,
    poweredByHeader: false,
    // El lint corre aparte (`pnpm lint`), con su propia configuración plana.
    eslint: { ignoreDuringBuilds: true },
    // La API se alcanza por el proxy de app/api/[...ruta]/route.ts (IRIS_API_URL leída en
    // tiempo de ejecución, no de build), así las cookies de sesión son del mismo origen.
    async headers() {
        // El service worker nunca se cachea en HTTP: el navegador debe ver cada versión nueva.
        return [{ source: "/sw.js", headers: [{ key: "Cache-Control", value: "no-cache, no-store, must-revalidate" }] }];
    },
};

export default conSerwist(config);
