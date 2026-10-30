import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";
import { config, middleware } from "./middleware";

const pedir = (ruta: string, cookies = "") => new NextRequest(`http://iris.test${ruta}`, { headers: cookies ? { cookie: cookies } : {} });
const coincide = (ruta: string) => config.matcher.some((m) => new RegExp(`^${m}$`).test(ruta));

describe("middleware", () => {
    it("el panel sin iris_sesion va a /entrar", () => {
        expect(middleware(pedir("/tickets")).headers.get("location")).toBe("http://iris.test/entrar?siguiente=%2Ftickets");
    });
    it("/soporte no exige iris_sesion: con iris_portal pasa", () => {
        expect(middleware(pedir("/soporte", "iris_portal=p")).headers.get("location")).toBeNull();
        expect(middleware(pedir("/soporte/12", "iris_portal=p")).headers.get("location")).toBeNull();
    });
    it("/soporte sin iris_portal va a la entrada del portal (aunque haya sesión del panel)", () => {
        expect(middleware(pedir("/soporte/nuevo", "iris_sesion=s")).headers.get("location")).toBe("http://iris.test/soporte/entrar");
    });
    it("/soporte/entrar y la API no pasan por el middleware", () => {
        expect(coincide("/soporte/entrar")).toBe(false);
        expect(coincide("/api/v1/portal/sesion")).toBe(false);
        expect(coincide("/soporte")).toBe(true);
        expect(coincide("/soporte/3")).toBe(true);
        expect(coincide("/tickets")).toBe(true);
    });
    it("la encuesta pública no pasa por el middleware; la lista del panel sí", () => {
        expect(coincide("/encuesta")).toBe(false);
        expect(coincide("/encuestas")).toBe(true);
    });
    it("el autologin desde Argos no pasa por el middleware", () => {
        expect(coincide("/sso/entrar")).toBe(false);
        expect(coincide("/sso")).toBe(true);
    });
    it("/campo, su manifest y el service worker no pasan por el middleware (abren sin señal)", () => {
        expect(coincide("/campo")).toBe(false);
        expect(coincide("/campo/manifest.webmanifest")).toBe(false);
        expect(coincide("/sw.js")).toBe(false);
        expect(coincide("/campos")).toBe(true);
    });
});
