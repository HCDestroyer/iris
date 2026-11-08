"use client";

import type { RespuestaLogin } from "./registro-embebido";

/*
 * SDK de Facebook para JavaScript, cargado **solo** en la pantalla «Conectar con QR» (nunca en el
 * layout): `https://connect.facebook.net/<locale>/sdk.js`. La CSP del panel hoy solo pone
 * `frame-ancestors 'none'` (next.config.ts), así que el script, sus iframes y la ventana emergente
 * de Meta no necesitan excepciones. Si algún día se agrega `script-src`, `frame-src` o
 * `connect-src`, esta ruta necesita `https://connect.facebook.net` y `https://*.facebook.com`
 * (anotado en `.claude/integration/i3.md`). Tampoco hay `Cross-Origin-Opener-Policy`; si se agrega,
 * debe ser `same-origin-allow-popups` para que la ventana de Meta pueda avisar con `postMessage`.
 */

export const LOCALE_SDK = "es_LA";
export const URL_SDK = `https://connect.facebook.net/${LOCALE_SDK}/sdk.js`;

type OpcionesLogin = {
    config_id: string;
    response_type: "code";
    override_default_response_type: true;
    extras: { setup: Record<string, never>; featureType: string; sessionInfoVersion: string };
};

export type SdkFacebook = {
    init: (o: { appId: string; autoLogAppEvents?: boolean; xfbml?: boolean; version: string }) => void;
    login: (cb: (r: RespuestaLogin) => void, o: OpcionesLogin) => void;
};

declare global {
    interface Window {
        FB?: SdkFacebook;
        fbAsyncInit?: () => void;
    }
}

let cargando: Promise<SdkFacebook> | null = null;

/** Carga el SDK una vez por pestaña e inicializa la app (la de la conexión con Meta). */
export function cargarSdk(appId: string, version: string, tiempoMs = 20_000): Promise<SdkFacebook> {
    if (typeof window === "undefined") return Promise.reject(new Error("sin navegador"));
    const iniciar = (fb: SdkFacebook) => {
        fb.init({ appId, autoLogAppEvents: false, xfbml: false, version });
        return fb;
    };
    if (window.FB) return Promise.resolve(iniciar(window.FB));
    cargando ??= new Promise<SdkFacebook>((resolver, rechazar) => {
        const limite = window.setTimeout(() => {
            cargando = null;
            rechazar(new Error("El SDK de Facebook no cargó"));
        }, tiempoMs);
        window.fbAsyncInit = () => {
            window.clearTimeout(limite);
            if (window.FB) resolver(window.FB);
            else rechazar(new Error("El SDK de Facebook no cargó"));
        };
        const script = document.createElement("script");
        script.src = URL_SDK;
        script.async = true;
        script.defer = true;
        script.crossOrigin = "anonymous";
        script.dataset.sdk = "facebook";
        script.onerror = () => {
            window.clearTimeout(limite);
            cargando = null;
            script.remove();
            rechazar(new Error("El SDK de Facebook no cargó"));
        };
        document.body.appendChild(script);
    });
    return cargando.then(iniciar);
}

export function opcionesLogin(configId: string, featureType: string, sessionInfoVersion: string): OpcionesLogin {
    return { config_id: configId, response_type: "code", override_default_response_type: true, extras: { setup: {}, featureType, sessionInfoVersion } };
}
