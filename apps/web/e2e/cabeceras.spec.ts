import { expect, test } from "@playwright/test";
import { ADMIN, conSesion, simularApi } from "./api-falsa";

/*
 * Cabeceras de seguridad de next.config.ts: nadie embebe Iris en un iframe (clickjacking), en el
 * panel, la encuesta pública, la entrada y el proxy de /api; el service worker sigue sin caché.
 */

const sinIframe = (h: Record<string, string>) => {
    expect(h["x-frame-options"]).toBe("DENY");
    expect(h["content-security-policy"]).toContain("frame-ancestors 'none'");
};

test("X-Frame-Options y frame-ancestors en /, /encuesta, /entrar y /api", async ({ page, context, baseURL, request }) => {
    await simularApi(page);
    await conSesion(context, baseURL ?? "", ADMIN.correo);
    const panel = await page.goto("/");
    sinIframe(panel?.headers() ?? {});

    for (const ruta of ["/encuesta", "/entrar"]) {
        const r = await request.get(ruta);
        expect(r.status()).toBe(200);
        sinIframe(r.headers());
    }
    // Sin sesión, / redirige a /entrar: la redirección también lleva las cabeceras.
    const redireccion = await request.get("/", { maxRedirects: 0 });
    expect(redireccion.status()).toBe(307);
    sinIframe(redireccion.headers());
    // El proxy de /api (aquí sin API detrás: 502) también.
    sinIframe((await request.get("/api/v1/yo")).headers());

    const sw = await request.get("/sw.js");
    expect(sw.headers()["cache-control"]).toBe("no-cache, no-store, must-revalidate");
    sinIframe(sw.headers());
});
