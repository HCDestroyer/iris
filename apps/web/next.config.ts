import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import withSerwistInit from "@serwist/next";
import type { NextConfig } from "next";

const directorio = path.dirname(fileURLToPath(import.meta.url));
const raiz = path.resolve(directorio, "../..");

// Manual de la app del técnico (/campo/manual): se precachea con el shell para leerlo sin señal.
// Las páginas salen del contenido generado del manual (lib/manual/contenido.generado.json).
const paginasManualCampo = (JSON.parse(readFileSync(path.join(directorio, "lib/manual/contenido.generado.json"), "utf8")) as { paginas: { clave: string }[] }).paginas
    .filter((p) => p.clave.startsWith("campo/"))
    .map((p) => `/campo/manual/${p.clave.slice("campo/".length)}`);
const revisionBuild = randomUUID();

// Service worker de la app de campo (Serwist, I4): precachea el shell de /campo y los estáticos
// del build para abrir sin señal. Solo controla /campo (alcance registrado en
// app/campo/registrar-sw.tsx); el panel y /soporte no pasan por él. En desarrollo no se genera.
const conSerwist = withSerwistInit({
    swSrc: "app/sw.ts",
    swDest: "public/sw.js",
    register: false,
    reloadOnOnline: false,
    disable: process.env.NODE_ENV === "development",
    // El shell (HTML estático de /campo) y el manual del técnico cambian en cada build: revisión nueva por build.
    additionalPrecacheEntries: ["/campo", "/campo/manual", ...paginasManualCampo].map((url) => ({ url, revision: revisionBuild })),
});

const config: NextConfig = {
    // Imagen de Docker: servidor Node autónomo (apps/web/Dockerfile).
    output: "standalone",
    outputFileTracingRoot: raiz,
    // Los paquetes del monorepo exportan fuente TS.
    transpilePackages: ["@iris/contratos", "@iris/ui", "@iris/sync", "@iris/telefono"],
    reactStrictMode: true,
    poweredByHeader: false,
    // El lint corre aparte (`pnpm lint`), con su propia configuración plana.
    eslint: { ignoreDuringBuilds: true },
    // La API se alcanza por el proxy de app/api/[...ruta]/route.ts (IRIS_API_URL leída en
    // tiempo de ejecución, no de build), así las cookies de sesión son del mismo origen.
    async headers() {
        return [
            // Nadie embebe Iris en un iframe (clickjacking): X-Frame-Options para navegadores viejos y
            // frame-ancestors de CSP para los actuales. Va en todo, también /encuesta y /api.
            {
                source: "/:path*",
                headers: [
                    { key: "X-Frame-Options", value: "DENY" },
                    { key: "Content-Security-Policy", value: "frame-ancestors 'none'" },
                    // A otro sitio (Argos, Horus, mapas) solo le llega el origen de Iris, nunca la ruta
                    // ni la consulta (ids de clientes, conversaciones, filtros).
                    { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
                    // Micrófono (teléfono, notas de voz), cámara y ubicación solo para Iris, nunca para un
                    // iframe ajeno (revisión de #252).
                    { key: "Permissions-Policy", value: "microphone=(self), camera=(self), geolocation=(self)" },
                ],
            },
            // El service worker nunca se cachea en HTTP: el navegador debe ver cada versión nueva.
            { source: "/sw.js", headers: [{ key: "Cache-Control", value: "no-cache, no-store, must-revalidate" }] },
        ];
    },
};

export default conSerwist(config);
