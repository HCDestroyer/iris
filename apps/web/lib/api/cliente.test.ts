import type { Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { crearClienteIris, leerCookie, resolver } from "./cliente";
import { ErrorApi } from "./errores";

type Llamada = { metodo: string; url: string; cabeceras: Headers; credenciales: RequestCredentials };

function falso(respuesta: () => Response) {
    const llamadas: Llamada[] = [];
    const fetchFalso: typeof fetch = async (entrada) => {
        const peticion = entrada as Request;
        llamadas.push({
            metodo: peticion.method,
            url: peticion.url,
            cabeceras: peticion.headers,
            credenciales: peticion.credentials,
        });
        return respuesta();
    };
    return { llamadas, fetchFalso };
}

const problema = (p: Schema<"Problema">, cabeceras: Record<string, string> = {}) =>
    new Response(JSON.stringify(p), {
        status: p.status,
        headers: { "Content-Type": "application/problem+json", ...cabeceras },
    });

const yo: Schema<"Yo"> = {
    actor: "usuario",
    permisos: ["usuarios.ver"],
    empresas: [],
    modoEmpresa: "sin_conector",
    todasLasEmpresas: false,
    hayDatosSinEmpresa: false,
    usuario: {
        id: 1,
        nombre: "Ana",
        correo: "ana@iris.test",
        tipo: "persona",
        activo: true,
        mfaActivo: false,
        roles: [{ rol: "admin" }],
        empresas: [],
        version: 1,
        creadoEn: "2026-10-01T00:00:00Z",
        actualizadoEn: "2026-10-01T00:00:00Z",
    },
};

describe("leerCookie", () => {
    it("lee el valor (decodificado) entre varias cookies", () => {
        expect(leerCookie("iris_csrf", "a=1; iris_csrf=abc%3D%3D; b=2")).toBe("abc==");
        expect(leerCookie("iris_csrf", "a=1")).toBeUndefined();
        expect(leerCookie("iris_csrf", "")).toBeUndefined();
    });
});

describe("cliente: CSRF y cookies", () => {
    it("añade X-CSRF-Token desde iris_csrf en métodos que cambian estado", async () => {
        const { llamadas, fetchFalso } = falso(() => new Response(null, { status: 204 }));
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso, leerCookies: () => "iris_csrf=tok123" });
        await api.DELETE("/api/v1/sesion");
        await api.POST("/api/v1/sesion/mfa", { body: { codigo: "123456" } });
        expect(llamadas.map((l) => l.metodo)).toEqual(["DELETE", "POST"]);
        for (const l of llamadas) {
            expect(l.cabeceras.get("X-CSRF-Token")).toBe("tok123");
            expect(l.credenciales).toBe("include");
        }
    });

    it("no manda X-CSRF-Token en GET", async () => {
        const { llamadas, fetchFalso } = falso(() => Response.json(yo));
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso, leerCookies: () => "iris_csrf=tok123" });
        const datos = await resolver(api.GET("/api/v1/yo"));
        expect(datos.usuario.nombre).toBe("Ana");
        expect(llamadas[0]?.cabeceras.has("X-CSRF-Token")).toBe(false);
        expect(llamadas[0]?.url).toBe("http://panel.test/api/v1/yo");
    });

    it("sin cookie iris_csrf no inventa la cabecera", async () => {
        const { llamadas, fetchFalso } = falso(() => new Response(null, { status: 204 }));
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso, leerCookies: () => "" });
        await api.DELETE("/api/v1/sesion");
        expect(llamadas[0]?.cabeceras.has("X-CSRF-Token")).toBe(false);
    });
});

describe("resolver: errores problem+json", () => {
    it("429 demasiados_intentos con Retry-After", async () => {
        const { fetchFalso } = falso(() =>
            problema(
                {
                    type: "urn:iris:problema:demasiados_intentos",
                    title: "Demasiados intentos",
                    status: 429,
                    detail: "Espera antes de volver a intentar",
                },
                { "Retry-After": "540" },
            ),
        );
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso });
        const error = await resolver(api.POST("/api/v1/sesion", { body: { correo: "a@b.c", contrasena: "x" } })).catch((e: unknown) => e);
        expect(error).toBeInstanceOf(ErrorApi);
        const e = error as ErrorApi;
        expect(e.estado).toBe(429);
        expect(e.codigo).toBe("demasiados_intentos");
        expect(e.reintentarEn).toBe(540);
    });

    it("401 mfa_bloqueado y 422 con errores por campo", async () => {
        let respuesta = problema({ type: "urn:iris:problema:mfa_bloqueado", title: "Demasiados códigos inválidos", status: 401 });
        const { fetchFalso } = falso(() => respuesta);
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso });
        const e1 = (await resolver(api.POST("/api/v1/sesion/mfa", { body: { codigo: "000000" } })).catch((e: unknown) => e)) as ErrorApi;
        expect(e1.codigo).toBe("mfa_bloqueado");
        expect(e1.estado).toBe(401);

        respuesta = problema({
            type: "urn:iris:problema:validacion",
            title: "Datos inválidos",
            status: 422,
            errors: [{ field: "body.correo", message: "correo inválido" }],
        });
        const e2 = (await resolver(api.POST("/api/v1/sesion", { body: { correo: "x", contrasena: "y" } })).catch((e: unknown) => e)) as ErrorApi;
        expect(e2.estado).toBe(422);
        expect(e2.errorDeCampo("correo")).toBe("correo inválido");
        expect(e2.errorDeCampo("contrasena")).toBeUndefined();
    });

    it("una respuesta que no es problem+json conserva el estado HTTP", async () => {
        const { fetchFalso } = falso(() => new Response("<html>bad gateway</html>", { status: 502, headers: { "Content-Type": "text/html" } }));
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso });
        const e = (await resolver(api.GET("/api/v1/yo")).catch((x: unknown) => x)) as ErrorApi;
        expect(e.estado).toBe(502);
        expect(e.codigo).toBe("http_502");
    });

    it("un fallo de red es sin_conexion", async () => {
        const api = crearClienteIris({
            baseUrl: "http://panel.test",
            fetch: async () => {
                throw new TypeError("fetch failed");
            },
        });
        const e = (await resolver(api.GET("/api/v1/yo")).catch((x: unknown) => x)) as ErrorApi;
        expect(e).toBeInstanceOf(ErrorApi);
        expect(e.codigo).toBe("sin_conexion");
        expect(e.estado).toBe(0);
    });
});

describe("cliente: empresa activa", () => {
    it("manda X-Iris-Empresa en toda petición si hay empresa elegida", async () => {
        const { llamadas, fetchFalso } = falso(() => Response.json(yo));
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso, leerCookies: () => "iris_csrf=t", leerEmpresa: () => 20 });
        await api.GET("/api/v1/yo");
        await api.DELETE("/api/v1/sesion");
        expect(llamadas.map((l) => l.cabeceras.get("X-Iris-Empresa"))).toEqual(["20", "20"]);
    });

    it("sin empresa elegida no manda la cabecera", async () => {
        const { llamadas, fetchFalso } = falso(() => Response.json(yo));
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso, leerEmpresa: () => undefined });
        await api.GET("/api/v1/yo");
        expect(llamadas[0]?.cabeceras.has("X-Iris-Empresa")).toBe(false);
    });

    it("avisa de los 422/403 de empresa y no de otros errores", async () => {
        const vistos: string[] = [];
        let respuesta = () => problema({ type: "urn:iris:problema:empresa_requerida", title: "Elige una empresa", status: 422 });
        const { fetchFalso } = falso(() => respuesta());
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso, leerEmpresa: () => undefined, alProblemaEmpresa: (p) => vistos.push(p) });
        const e = (await resolver(api.GET("/api/v1/tickets")).catch((x: unknown) => x)) as ErrorApi;
        expect(e.codigo).toBe("empresa_requerida");
        respuesta = () => problema({ type: "urn:iris:problema:empresa_no_permitida", title: "Empresa no permitida", status: 403 });
        await resolver(api.GET("/api/v1/yo")).catch(() => undefined);
        respuesta = () => problema({ type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
        await resolver(api.GET("/api/v1/yo")).catch(() => undefined);
        expect(vistos).toEqual(["empresa_requerida", "empresa_no_permitida"]);
    });

    it("los arreglos de la query van separados por coma", async () => {
        const { llamadas, fetchFalso } = falso(() => Response.json({ data: [], nextCursor: null }));
        const api = crearClienteIris({ baseUrl: "http://panel.test", fetch: fetchFalso, leerEmpresa: () => undefined });
        await api.GET("/api/v1/tickets", { params: { query: { estado: ["nuevo", "abierto"], prioridad: ["alta"] } } });
        const url = new URL(llamadas[0]?.url ?? "");
        expect(url.searchParams.getAll("estado")).toEqual(["nuevo,abierto"]);
        expect(url.searchParams.get("prioridad")).toBe("alta");
    });
});
