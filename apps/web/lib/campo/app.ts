import { CABECERA_APP, valorCabeceraApp, type IdentidadApp } from "@iris/sync";

/**
 * Versión de la PWA de campo: la etiqueta de la compilación (`NEXT_PUBLIC_IRIS_VERSION`, la pone el
 * build de la imagen) o `0.0.0-dev`. Va en `X-Iris-App: campo/<versión> (pwa)` para que el panel
 * muestre qué versión usa cada técnico (Administración → Actividad de la app).
 */
export const VERSION_APP_CAMPO = process.env.NEXT_PUBLIC_IRIS_VERSION?.trim() || "0.0.0-dev";

/** Identidad de la PWA para el transporte de `@iris/sync`; `pendientes` lee la cola del motor. */
export function identidadPWA(pendientes?: () => number | null): IdentidadApp {
    return { version: VERSION_APP_CAMPO, plataforma: "pwa", pendientes };
}

/** Cabeceras de campo de las llamadas sueltas (`/yo`, entrar, salir). */
export function cabecerasCampo(): Record<string, string> {
    return { "X-Iris-Canal": "campo", [CABECERA_APP]: valorCabeceraApp(VERSION_APP_CAMPO, "pwa") };
}
