import { crearCliente, type Schema } from "@iris/contratos";
import { describe, expect, it } from "vitest";
import { ErrorTransporte } from "./errores";
import { cabecerasApp, crearSubidorApi, crearTransporteApi, crearTransportePosicionesApi, valorCabeceraApp } from "./http";

interface Pedido {
    url: string;
    metodo: string;
    cabeceras: Headers;
    cuerpo: ArrayBuffer;
}

/** fetch falso con la forma de la API (respuestas tipadas con los esquemas del OpenAPI). */
function fetchFalso(responder: (p: Pedido) => Response): { fetch: typeof fetch; pedidos: Pedido[] } {
    const pedidos: Pedido[] = [];
    const fn: typeof fetch = async (entrada) => {
        const req = entrada as Request;
        const p: Pedido = { url: req.url, metodo: req.method, cabeceras: req.headers, cuerpo: await req.arrayBuffer() };
        pedidos.push(p);
        return responder(p);
    };
    return { fetch: fn, pedidos };
}

const json = (cuerpo: unknown, init: ResponseInit = {}) =>
    new Response(JSON.stringify(cuerpo), { ...init, headers: { "Content-Type": "application/json", ...init.headers } });

const problema = (codigo: string, status: number, extra: Partial<Schema<"Problema">> = {}): Schema<"Problema"> => ({
    type: `urn:iris:problema:${codigo}`,
    title: codigo,
    status,
    ...extra,
});

const respuestaLote: Schema<"RespuestaLote"> = {
    resultados: [{ id: "0190f000-0000-7000-8000-000000000001", estado: "aplicada", orden: { version: 3, estado: "en_camino" } }],
    cursor: "c9",
    servidorEn: "2026-10-28T15:00:02Z",
    desfaseMs: 2000,
};

function clienteCon(f: typeof fetch) {
    return crearCliente({ baseUrl: "https://iris.test", fetch: f });
}

describe("crearTransporteApi", () => {
    it("envía el lote con canal campo y toma la hora de servidorEn", async () => {
        const { fetch, pedidos } = fetchFalso(() => json(respuestaLote, { headers: { Date: "Wed, 28 Oct 2026 14:00:00 GMT" } }));
        const t = crearTransporteApi(clienteCon(fetch));
        const peticion: Schema<"EntradaLote"> = {
            dispositivoId: "d1",
            plataforma: "pwa",
            enviadoEn: "2026-10-28T15:00:00Z",
            cursor: "c9",
            operaciones: [
                { id: "0190f000-0000-7000-8000-000000000001", tipo: "orden.iniciar_camino", ordenId: 12, versionBase: 2, ocurridoEn: "2026-10-28T14:59:00Z", datos: {} },
            ],
        };
        const r = await t.enviarLote(peticion);
        expect(r).toEqual({ datos: respuestaLote, horaServidor: Date.parse("2026-10-28T15:00:02Z") });
        expect(pedidos[0]?.url).toBe("https://iris.test/api/v1/sync/lote");
        expect(pedidos[0]?.metodo).toBe("POST");
        expect(pedidos[0]?.cabeceras.get("X-Iris-Canal")).toBe("campo");
        expect(JSON.parse(new TextDecoder().decode(pedidos[0]?.cuerpo))).toEqual(peticion);
    });

    it("pide paquete y cambios con la query codificada; cambios toma la hora de la cabecera Date", async () => {
        const cambios: Schema<"Cambios"> = { cambios: [], cursor: "a+b/c", hayMas: false };
        const { fetch, pedidos } = fetchFalso(() => json(cambios, { headers: { Date: "Wed, 28 Oct 2026 15:00:00 GMT" } }));
        const t = crearTransporteApi(clienteCon(fetch));
        const r = await t.obtenerCambios("a+b/c");
        await t.obtenerPaquete("2026-10-28").catch(() => undefined);
        expect(r.horaServidor).toBe(Date.parse("2026-10-28T15:00:00Z"));
        expect(pedidos.map((p) => p.url)).toEqual([
            "https://iris.test/api/v1/campo/cambios?cursor=a%2Bb%2Fc",
            "https://iris.test/api/v1/campo/paquete?fecha=2026-10-28",
        ]);
    });

    it("convierte un problem+json en ErrorTransporte con su código", async () => {
        const p = problema("no_autenticado", 401);
        const { fetch } = fetchFalso(() => new Response(JSON.stringify(p), { status: 401, headers: { "Content-Type": "application/problem+json" } }));
        const error = await crearTransporteApi(clienteCon(fetch))
            .obtenerPaquete("2026-10-28")
            .catch((e: unknown) => e);
        expect(error).toBeInstanceOf(ErrorTransporte);
        expect(error).toMatchObject({ tipo: "http", status: 401, problema: p, codigo: "no_autenticado", esDeSesion: true, esReintentable: false });
    });

    it("distingue los problemas de empresa de los de sesión", async () => {
        const { fetch } = fetchFalso(() => json(problema("empresa_no_permitida", 403), { status: 403 }));
        const error = await crearTransporteApi(clienteCon(fetch))
            .obtenerCambios("c")
            .catch((e: unknown) => e);
        expect(error).toMatchObject({ esDeEmpresa: true, esDeSesion: false });
    });

    it("una falla de red es reintentable", async () => {
        const fetch: typeof globalThis.fetch = () => Promise.reject(new TypeError("Failed to fetch"));
        const error = await crearTransporteApi(clienteCon(fetch))
            .obtenerCambios("c")
            .catch((e: unknown) => e);
        expect(error).toMatchObject({ tipo: "red", esReintentable: true });
    });
});

describe("crearSubidorApi", () => {
    const adjunto: Schema<"Adjunto"> = {
        id: "0190f000-0000-7000-8000-0000000000aa",
        entidad: "orden",
        entidadId: "12",
        tipo: "foto",
        claveChecklist: "fachada",
        mime: "image/jpeg",
        tamano: 8,
        sha256: "a".repeat(64),
        recibido: 0,
        estado: "pendiente",
        motivoRechazo: null,
        lat: null,
        lon: null,
        tomadoEn: null,
        subidoPor: 5,
        creadoEn: "2026-10-28T15:00:00Z",
        completadoEn: null,
        urlSubida: "/api/v1/adjuntos/0190f000-0000-7000-8000-0000000000aa/subida",
        urlDescarga: "/api/v1/adjuntos/0190f000-0000-7000-8000-0000000000aa",
        version: 1,
    };

    it("da de alta con Idempotency-Key y sube con HEAD y PATCH de tus 1.0 (bytes crudos)", async () => {
        const { fetch, pedidos } = fetchFalso((p) => {
            if (p.metodo === "POST") {
                return json(adjunto, { status: 201 });
            }
            if (p.metodo === "HEAD") {
                return new Response(null, { status: 200, headers: { "Upload-Offset": "4", "Upload-Length": "8" } });
            }
            return new Response(null, { status: 204, headers: { "Upload-Offset": "8" } });
        });
        const s = crearSubidorApi(clienteCon(fetch));
        const creado = await s.crear({
            id: adjunto.id,
            entidad: "orden",
            entidadId: "12",
            tipo: "foto",
            mime: "image/jpeg",
            tamano: 8,
            sha256: "a".repeat(64),
            claveChecklist: "fachada",
        });
        expect(creado).toEqual(adjunto);
        expect(pedidos[0]?.cabeceras.get("Idempotency-Key")).toBe(adjunto.id);
        expect(await s.consultarOffset(adjunto.id)).toBe(4);
        expect(pedidos[1]?.url).toBe(`https://iris.test${adjunto.urlSubida}`);
        expect(pedidos[1]?.cabeceras.get("Tus-Resumable")).toBe("1.0.0");
        expect(await s.enviarParte(adjunto.id, 4, Uint8Array.from([5, 6, 7, 8]))).toBe(8);
        const patch = pedidos[2];
        expect(patch?.metodo).toBe("PATCH");
        expect(patch?.cabeceras.get("Upload-Offset")).toBe("4");
        expect(patch?.cabeceras.get("Content-Type")).toBe("application/offset+octet-stream");
        expect(patch?.cabeceras.get("X-Iris-Canal")).toBe("campo");
        expect(new Uint8Array(patch?.cuerpo ?? new ArrayBuffer(0))).toEqual(Uint8Array.from([5, 6, 7, 8]));
    });

    it("409 offset_distinto trae el Upload-Offset del servidor en el error", async () => {
        const { fetch } = fetchFalso(() =>
            json(problema("offset_distinto", 409), { status: 409, headers: { "Upload-Offset": "6", "Content-Type": "application/problem+json" } }),
        );
        const error = await crearSubidorApi(clienteCon(fetch))
            .enviarParte(adjunto.id, 4, new Uint8Array(2))
            .catch((e: unknown) => e);
        expect(error).toMatchObject({ codigo: "offset_distinto", status: 409 });
        expect((error as ErrorTransporte).cabeceras?.get("Upload-Offset")).toBe("6");
    });

    it("falla si la respuesta no trae Upload-Offset", async () => {
        const { fetch } = fetchFalso(() => new Response(null, { status: 200 }));
        await expect(crearSubidorApi(clienteCon(fetch)).consultarOffset(adjunto.id)).rejects.toBeInstanceOf(ErrorTransporte);
    });
});

describe("crearTransportePosicionesApi", () => {
    const entrada: Schema<"EntradaPosiciones"> = {
        dispositivoId: "d1",
        enviadoEn: "2026-10-28T15:00:00Z",
        puntos: [{ lat: 14.6, lon: -90.5, precision: 10, registradaEn: "2026-10-28T14:59:00Z" }],
    };

    it("envía el lote de puntos con canal campo y toma la hora de servidorEn", async () => {
        const respuesta: Schema<"RespuestaPosiciones"> = {
            aceptadas: 1,
            duplicadas: 0,
            descartadas: 0,
            motivos: {},
            servidorEn: "2026-10-28T15:00:01Z",
            desfaseMs: 1000,
        };
        const { fetch, pedidos } = fetchFalso(() => json(respuesta));
        const r = await crearTransportePosicionesApi(clienteCon(fetch)).enviarPosiciones(entrada);
        expect(r).toEqual({ datos: respuesta, horaServidor: Date.parse("2026-10-28T15:00:01Z") });
        expect(pedidos[0]?.url).toBe("https://iris.test/api/v1/campo/posiciones");
        expect(pedidos[0]?.metodo).toBe("POST");
        expect(pedidos[0]?.cabeceras.get("X-Iris-Canal")).toBe("campo");
        expect(JSON.parse(new TextDecoder().decode(pedidos[0]?.cuerpo))).toEqual(entrada);
    });

    it("un 429 llega con su Retry-After; un 403 requiere_sesion_campo es de sesión", async () => {
        const { fetch } = fetchFalso(() =>
            json(problema("demasiadas_posiciones", 429), { status: 429, headers: { "Content-Type": "application/problem+json", "Retry-After": "17" } }),
        );
        const e429 = await crearTransportePosicionesApi(clienteCon(fetch))
            .enviarPosiciones(entrada)
            .catch((e: unknown) => e);
        expect(e429).toBeInstanceOf(ErrorTransporte);
        expect(e429).toMatchObject({ status: 429, codigo: "demasiadas_posiciones", esReintentable: true });
        expect((e429 as ErrorTransporte).cabeceras?.get("Retry-After")).toBe("17");

        const { fetch: f403 } = fetchFalso(() => json(problema("requiere_sesion_campo", 403), { status: 403, headers: { "Content-Type": "application/problem+json" } }));
        const e403 = await crearTransportePosicionesApi(clienteCon(f403))
            .enviarPosiciones(entrada)
            .catch((e: unknown) => e);
        expect(e403).toMatchObject({ status: 403, codigo: "requiere_sesion_campo", esDeSesion: true });
    });
});

describe("X-Iris-App", () => {
    it("arma campo/<versión> (<plataforma>) con valores limpios", () => {
        expect(valorCabeceraApp("1.4.0", "android")).toBe("campo/1.4.0 (android)");
        expect(valorCabeceraApp(" v1.4.0 beta ", "PWA")).toBe("campo/v1.4.0beta (pwa)");
        expect(valorCabeceraApp("", "")).toBe("campo/0 (desconocida)");
        expect(valorCabeceraApp("-1", "i/os")).toBe("campo/1 (ios)");
    });

    it("informa los pendientes solo si son un entero válido", () => {
        expect(cabecerasApp()).toEqual({});
        expect(cabecerasApp({ version: "1", plataforma: "pwa", pendientes: () => 4 })).toEqual({
            "X-Iris-App": "campo/1 (pwa)",
            "X-Iris-Pendientes": "4",
        });
        expect(cabecerasApp({ version: "1", plataforma: "pwa", pendientes: () => null })).toEqual({ "X-Iris-App": "campo/1 (pwa)" });
        expect(cabecerasApp({ version: "1", plataforma: "pwa", pendientes: () => -1 })).toEqual({ "X-Iris-App": "campo/1 (pwa)" });
    });

    it("el lote, el paquete, las posiciones y las subidas llevan la app y los pendientes del momento", async () => {
        const { fetch, pedidos } = fetchFalso(() => json(respuestaLote));
        let pendientes = 3;
        const app = { version: "2.1.0", plataforma: "android", pendientes: () => pendientes };
        const cliente = clienteCon(fetch);
        await crearTransporteApi(cliente, app).enviarLote({ dispositivoId: "d1", operaciones: [] });
        pendientes = 0;
        await crearTransporteApi(cliente, app).obtenerPaquete("2026-10-28").catch(() => undefined);
        await crearTransportePosicionesApi(cliente, app).enviarPosiciones({ puntos: [] }).catch(() => undefined);
        await crearSubidorApi(cliente, app).consultarOffset("0190f000-0000-7000-8000-000000000001").catch(() => undefined);
        expect(pedidos.map((p) => p.cabeceras.get("X-Iris-App"))).toEqual(Array(4).fill("campo/2.1.0 (android)"));
        expect(pedidos.map((p) => p.cabeceras.get("X-Iris-Pendientes"))).toEqual(["3", "0", "0", "0"]);
        expect(pedidos.every((p) => p.cabeceras.get("X-Iris-Canal") === "campo")).toBe(true);
    });

    it("sin app no manda la cabecera", async () => {
        const { fetch, pedidos } = fetchFalso(() => json(respuestaLote));
        await crearTransporteApi(clienteCon(fetch)).enviarLote({ dispositivoId: "d1", operaciones: [] });
        expect(pedidos[0]?.cabeceras.get("X-Iris-App")).toBeNull();
    });
});
