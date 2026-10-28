/// <reference lib="webworker" />
// Service worker de la app de campo (Serwist). Alcance: /campo (lo registra
// app/campo/registrar-sw.tsx). Cachea el shell para abrir sin señal; la API (/api/*) NUNCA pasa
// por aquí: los datos sin conexión los guarda @iris/sync en IndexedDB y la cola se envía sola.
import type { PrecacheEntry, SerwistGlobalConfig } from "serwist";
import { CacheFirst, ExpirationPlugin, NetworkFirst, Serwist, StaleWhileRevalidate } from "serwist";

declare global {
    interface WorkerGlobalScope extends SerwistGlobalConfig {
        __SW_MANIFEST: (PrecacheEntry | string)[] | undefined;
    }
}

declare const self: ServiceWorkerGlobalScope;

const SHELL = "/campo";

const serwist = new Serwist({
    precacheEntries: self.__SW_MANIFEST,
    skipWaiting: true,
    clientsClaim: true,
    navigationPreload: true,
    runtimeCaching: [
        {
            // Páginas de /campo: red primero (3 s) y, sin señal, el shell precacheado.
            matcher: ({ request, url, sameOrigin }) => sameOrigin && request.mode === "navigate" && url.pathname.startsWith(SHELL),
            handler: new NetworkFirst({ cacheName: "campo-paginas", networkTimeoutSeconds: 3 }),
        },
        {
            // Estáticos con hash del build (ya precacheados; por si faltara alguno).
            matcher: ({ url, sameOrigin }) => sameOrigin && url.pathname.startsWith("/_next/static/"),
            handler: new CacheFirst({
                cacheName: "campo-estaticos",
                plugins: [new ExpirationPlugin({ maxEntries: 200, maxAgeSeconds: 30 * 24 * 60 * 60 })],
            }),
        },
        {
            // Manifest e ícono.
            matcher: ({ url, sameOrigin }) => sameOrigin && (url.pathname === "/campo/manifest.webmanifest" || url.pathname === "/icon.svg"),
            handler: new StaleWhileRevalidate({ cacheName: "campo-marca" }),
        },
    ],
    fallbacks: {
        entries: [{ url: SHELL, matcher: ({ request }) => request.destination === "document" }],
    },
});

serwist.addEventListeners();
