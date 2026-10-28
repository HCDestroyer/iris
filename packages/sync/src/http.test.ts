import { describe, expect, it } from "vitest";
import { ErrorTransporte } from "./errores";
import { crearSubidorTus, crearTransporteHttp } from "./http";

interface Pedido {
    url: string;
    metodo: string;
    cabeceras: Headers;
    cuerpo: unknown;
}

function fetchFalso(responder: (p: Pedido) => Response): { fetch: typeof fetch; pedidos: Pedido[] } {
    const pedidos: Pedido[] = [];
    const fn: typeof fetch = (entrada, init) => {
        const p: Pedido = {
            url: String(entrada),
            metodo: init?.method ?? "GET",
            cabeceras: new Headers(init?.headers),
            cuerpo: init?.body,
        };
        pedidos.push(p);
        return Promise.resolve(responder(p));
    };
    return { fetch: fn, pedidos };
}

describe("crearTransporteHttp", () => {
    it("envía el lote con canal campo y cabeceras propias, y lee la hora del servidor", async () => {
        const { fetch, pedidos } = fetchFalso(
            () => new Response(JSON.stringify({ resultados: [] }), { headers: { Date: "Wed, 28 Oct 2026 15:00:00 GMT" } }),
        );
        const t = crearTransporteHttp({ baseUrl: "https://iris.test", fetch, cabeceras: () => ({ "X-CSRF-Token": "abc" }) });
        const r = await t.enviarLote({ dispositivoId: "d", operaciones: [] });
        expect(r).toEqual({ datos: { resultados: [] }, horaServidor: Date.parse("2026-10-28T15:00:00Z") });
        expect(pedidos[0]?.url).toBe("https://iris.test/api/v1/sync/lote");
        expect(pedidos[0]?.metodo).toBe("POST");
        expect(pedidos[0]?.cabeceras.get("X-Iris-Canal")).toBe("campo");
        expect(pedidos[0]?.cabeceras.get("X-CSRF-Token")).toBe("abc");
        expect(JSON.parse(String(pedidos[0]?.cuerpo))).toEqual({ dispositivoId: "d", operaciones: [] });
    });

    it("codifica fecha y cursor en la query", async () => {
        const { fetch, pedidos } = fetchFalso(() => new Response("{}"));
        const t = crearTransporteHttp({ fetch });
        await t.obtenerPaquete("2026-10-28");
        await t.obtenerCambios("a+b/c");
        expect(pedidos.map((p) => p.url)).toEqual(["/api/v1/campo/paquete?fecha=2026-10-28", "/api/v1/campo/cambios?cursor=a%2Bb%2Fc"]);
    });

    it("convierte errores HTTP en ErrorTransporte con el problema RFC 9457", async () => {
        const problema = { type: "urn:iris:problema:no_autenticado", title: "Sin sesión", status: 401 };
        const { fetch } = fetchFalso(() => new Response(JSON.stringify(problema), { status: 401 }));
        const error = await crearTransporteHttp({ fetch })
            .obtenerPaquete("2026-10-28")
            .catch((e: unknown) => e);
        expect(error).toBeInstanceOf(ErrorTransporte);
        expect(error).toMatchObject({ tipo: "http", status: 401, problema, esDeSesion: true, esReintentable: false });
    });

    it("una falla de red es reintentable", async () => {
        const fetch: typeof globalThis.fetch = () => Promise.reject(new TypeError("Failed to fetch"));
        const error = await crearTransporteHttp({ fetch })
            .obtenerCambios("c")
            .catch((e: unknown) => e);
        expect(error).toMatchObject({ tipo: "red", esReintentable: true });
    });
});

describe("crearSubidorTus", () => {
    it("crea con POST /adjuntos y sube con HEAD y PATCH de tus 1.0", async () => {
        const { fetch, pedidos } = fetchFalso((p) => {
            if (p.metodo === "POST") {
                return new Response(JSON.stringify({ id: "f1", urlSubida: "https://subidas.test/f1" }), { status: 201 });
            }
            if (p.metodo === "HEAD") {
                return new Response(null, { status: 200, headers: { "Upload-Offset": "4" } });
            }
            return new Response(null, { status: 204, headers: { "Upload-Offset": "8" } });
        });
        const s = crearSubidorTus({ fetch });
        const creado = await s.crear({ id: "f1", ordenId: "A", tipo: "foto", tamano: 8, sha256: "x" });
        expect(creado.urlSubida).toBe("https://subidas.test/f1");
        expect(pedidos[0]?.cabeceras.get("Idempotency-Key")).toBe("f1");
        expect(await s.consultarOffset(creado.urlSubida)).toBe(4);
        expect(pedidos[1]?.cabeceras.get("Tus-Resumable")).toBe("1.0.0");
        expect(await s.enviarParte(creado.urlSubida, 4, new Uint8Array(4))).toBe(8);
        expect(pedidos[2]?.url).toBe("https://subidas.test/f1");
        expect(pedidos[2]?.cabeceras.get("Upload-Offset")).toBe("4");
        expect(pedidos[2]?.cabeceras.get("Content-Type")).toBe("application/offset+octet-stream");
    });

    it("falla si la respuesta no trae Upload-Offset", async () => {
        const { fetch } = fetchFalso(() => new Response(null, { status: 200 }));
        await expect(crearSubidorTus({ fetch }).consultarOffset("/tus/x")).rejects.toBeInstanceOf(ErrorTransporte);
    });
});
