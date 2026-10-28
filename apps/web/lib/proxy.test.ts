import { describe, expect, it } from "vitest";
import { reenviar } from "./proxy";

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
