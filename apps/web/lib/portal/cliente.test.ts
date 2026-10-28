import { describe, expect, it } from "vitest";
import { resolver } from "../api/cliente";
import { ErrorApi } from "../api/errores";
import { COOKIE_PORTAL_CSRF, crearClientePortal, esSesionTerminada } from "./cliente";

type Llamada = { metodo: string; url: string; cabeceras: Headers; credenciales: RequestCredentials };

function falso(respuesta: () => Response) {
    const llamadas: Llamada[] = [];
    const fetchFalso: typeof fetch = async (entrada) => {
        const p = entrada as Request;
        llamadas.push({ metodo: p.method, url: p.url, cabeceras: p.headers, credenciales: p.credentials });
        return respuesta();
    };
    return { llamadas, fetchFalso };
}

const json = (cuerpo: unknown, status = 200) => new Response(JSON.stringify(cuerpo), { status, headers: { "Content-Type": "application/json" } });
const noAutenticado = () =>
    new Response(JSON.stringify({ type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 }), {
        status: 401,
        headers: { "Content-Type": "application/problem+json" },
    });

const COOKIES = `iris_csrf=del-panel; ${COOKIE_PORTAL_CSRF}=del-portal; iris-empresa=10`;

describe("cliente del portal", () => {
    it("manda X-CSRF-Token desde iris_portal_csrf (no iris_csrf) y nunca X-Iris-Empresa", async () => {
        const { llamadas, fetchFalso } = falso(() => json({ ticket: {}, mensaje: {} }));
        const api = crearClientePortal({ baseUrl: "http://iris.test", fetch: fetchFalso, leerCookies: () => COOKIES });
        await api.POST("/api/v1/portal/tickets/{id}/responder", {
            params: { path: { id: 7 }, header: { "Idempotency-Key": "k-1" } },
            body: { cuerpo: "Hola", version: 2 },
        });
        const [p] = llamadas;
        expect(p?.cabeceras.get("X-CSRF-Token")).toBe("del-portal");
        expect(p?.cabeceras.get("X-Iris-Empresa")).toBeNull();
        expect(p?.cabeceras.get("Idempotency-Key")).toBe("k-1");
        expect(p?.credenciales).toBe("include");
    });

    it("no manda CSRF en lecturas ni X-Iris-Empresa", async () => {
        const { llamadas, fetchFalso } = falso(() => json({ data: [], nextCursor: null }));
        const api = crearClientePortal({ baseUrl: "http://iris.test", fetch: fetchFalso, leerCookies: () => COOKIES });
        await api.GET("/api/v1/portal/tickets", { params: { query: { estado: ["nuevo", "abierto"], limit: 20 } } });
        const [p] = llamadas;
        expect(p?.cabeceras.get("X-CSRF-Token")).toBeNull();
        expect(p?.cabeceras.get("X-Iris-Empresa")).toBeNull();
        expect(new URL(p?.url ?? "").searchParams.get("estado")).toBe("nuevo,abierto");
    });

    it("un 401 en /portal/* avisa que la sesión terminó; el 401 del canje no", async () => {
        let avisos = 0;
        const { fetchFalso } = falso(noAutenticado);
        const api = crearClientePortal({ baseUrl: "http://iris.test", fetch: fetchFalso, alSesionTerminada: () => avisos++ });
        await expect(resolver(api.GET("/api/v1/portal/tickets"))).rejects.toBeInstanceOf(ErrorApi);
        expect(avisos).toBe(1);
        await expect(resolver(api.POST("/api/v1/portal/sesion", { body: { ticket: "x".repeat(20) } }))).rejects.toMatchObject({ estado: 401 });
        expect(avisos).toBe(1);
    });

    it("esSesionTerminada", () => {
        expect(esSesionTerminada("GET", "/api/v1/portal/sesion", 401)).toBe(true);
        expect(esSesionTerminada("DELETE", "/api/v1/portal/sesion", 401)).toBe(true);
        expect(esSesionTerminada("POST", "/api/v1/portal/sesion", 401)).toBe(false);
        expect(esSesionTerminada("GET", "/api/v1/portal/tickets", 404)).toBe(false);
        expect(esSesionTerminada("GET", "/api/v1/yo", 401)).toBe(false);
    });
});
