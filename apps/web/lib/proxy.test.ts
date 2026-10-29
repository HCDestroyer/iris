import { describe, expect, it } from "vitest";
import { cabecerasCliente, redesConfiables, reenviar } from "./proxy";

describe("proxy /api → IRIS_API_URL", () => {
    it("reenvía método, ruta, query, cuerpo, cookies y CSRF; devuelve todas las Set-Cookie", async () => {
        let recibida: { url: string; init: RequestInit } | undefined;
        const fetchFalso: typeof fetch = async (url, init) => {
            recibida = { url: String(url), init: init ?? {} };
            const h = new Headers({ "Content-Type": "application/json", "Content-Encoding": "gzip" });
            h.append("Set-Cookie", "iris_sesion=s; Path=/; HttpOnly");
            h.append("Set-Cookie", "iris_csrf=c; Path=/");
            return new Response('{"ok":true}', { status: 200, headers: h });
        };
        const peticion = new Request("http://panel.test/api/v1/sesion?x=1", {
            method: "POST",
            headers: { Cookie: "iris_sesion=viejo", "X-CSRF-Token": "c", "Content-Type": "application/json", Host: "panel.test" },
            body: '{"correo":"a@b"}',
        });
        const r = await reenviar(peticion, fetchFalso, "http://api:8080/");
        expect(recibida?.url).toBe("http://api:8080/api/v1/sesion?x=1");
        expect(recibida?.init.method).toBe("POST");
        const h = new Headers(recibida?.init.headers);
        expect(h.get("cookie")).toBe("iris_sesion=viejo");
        expect(h.get("x-csrf-token")).toBe("c");
        expect(h.get("host")).toBeNull();
        expect(h.get("x-forwarded-host")).toBe("panel.test");
        expect(new TextDecoder().decode(recibida?.init.body as ArrayBuffer)).toBe('{"correo":"a@b"}');
        expect(r.headers.getSetCookie()).toEqual(["iris_sesion=s; Path=/; HttpOnly", "iris_csrf=c; Path=/"]);
        expect(r.headers.get("content-encoding")).toBeNull();
        expect(await r.json()).toEqual({ ok: true });
    });

    it("sin la precarga no inventa IP: no copia X-Forwarded-For, X-Real-IP ni Forwarded del navegador", async () => {
        let enviadas = new Headers();
        const fetchFalso: typeof fetch = async (_url, init) => {
            enviadas = new Headers(init?.headers);
            return new Response("{}", { status: 200 });
        };
        const peticion = new Request("http://panel.test/api/v1/yo", {
            headers: {
                "X-Forwarded-For": "10.0.0.1, 203.0.113.9",
                "X-Forwarded-Proto": "https",
                "X-Real-IP": "10.0.0.1",
                Forwarded: "for=10.0.0.1",
                "X-Iris-Empresa": "2",
            },
        });
        await reenviar(peticion, fetchFalso, "http://api");
        expect(enviadas.get("x-forwarded-for")).toBeNull();
        expect(enviadas.get("x-forwarded-proto")).toBe("http");
        expect(enviadas.get("x-real-ip")).toBeNull();
        expect(enviadas.get("forwarded")).toBeNull();
        expect(enviadas.get("x-iris-empresa")).toBe("2");
    });

    it("con la precarga: encadena el X-Forwarded-For que llegó y agrega la IP del socket; no reenvía las anotaciones", async () => {
        let enviadas = new Headers();
        const fetchFalso: typeof fetch = async (_url, init) => {
            enviadas = new Headers(init?.headers);
            return new Response("{}", { status: 200 });
        };
        const peticion = new Request("http://panel.test/api/v1/encuestas/publica/responder", {
            method: "POST",
            body: "{}",
            headers: {
                "X-Forwarded-For": "1.2.3.4",
                "X-Iris-Socket-Ip": "::ffff:198.51.100.7",
                "X-Iris-Socket-Proto": "http",
                "X-Iris-Xff-Entrante": "203.0.113.5, 10.0.0.2",
                "X-Iris-Xfp-Entrante": "https",
            },
        });
        await reenviar(peticion, fetchFalso, "http://api");
        expect(enviadas.get("x-forwarded-for")).toBe("203.0.113.5, 10.0.0.2, 198.51.100.7");
        // Quien conectó no es un proxy confiable: el protocolo es el del socket.
        expect(enviadas.get("x-forwarded-proto")).toBe("http");
        for (const c of ["x-iris-socket-ip", "x-iris-socket-proto", "x-iris-xff-entrante", "x-iris-xfp-entrante"]) {
            expect(enviadas.get(c)).toBeNull();
        }
    });

    it("X-Forwarded-Proto del proxy confiable (IRIS_WEB_PROXIES_CONFIABLES); sin XFF previo, solo el socket", () => {
        const redes = redesConfiables("10.0.0.0/8, 2001:db8::1");
        const h = (socket: string, xfp = "https") => new Headers({ "x-iris-socket-ip": socket, "x-iris-socket-proto": "http", "x-iris-xfp-entrante": xfp });
        expect(cabecerasCliente(h("10.1.2.3"), redes)).toEqual({ xff: "10.1.2.3", xfp: "https" });
        expect(cabecerasCliente(h("2001:db8::1"), redes)).toEqual({ xff: "2001:db8::1", xfp: "https" });
        expect(cabecerasCliente(h("192.0.2.1"), redes)).toEqual({ xff: "192.0.2.1", xfp: "http" });
        expect(cabecerasCliente(h("10.1.2.3", "gopher"), redes).xfp).toBe("http");
        expect(cabecerasCliente(new Headers(), redes)).toEqual({ xfp: "http" });
    });

    it("conserva Retry-After del 429", async () => {
        const fetchFalso: typeof fetch = async () =>
            new Response("{}", { status: 429, headers: { "Retry-After": "60", "Content-Type": "application/problem+json" } });
        const r = await reenviar(new Request("http://panel.test/api/v1/sesion", { method: "POST", body: "{}" }), fetchFalso, "http://api");
        expect(r.status).toBe(429);
        expect(r.headers.get("retry-after")).toBe("60");
    });

    it("sin API responde 502 problem+json", async () => {
        const r = await reenviar(
            new Request("http://panel.test/api/v1/yo"),
            async () => {
                throw new TypeError("connect ECONNREFUSED");
            },
            "http://api",
        );
        expect(r.status).toBe(502);
        expect(r.headers.get("content-type")).toBe("application/problem+json");
        expect((await r.json()).type).toBe("urn:iris:problema:api_no_disponible");
    });
});
