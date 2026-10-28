import type { Schema } from "@iris/contratos";
import type { BrowserContext, Page, Route } from "@playwright/test";
import { CSRF } from "./api-falsa";
import { PERMISOS_TECNICO, TECNICO, existenciasDelTecnico, ordenesDelDia } from "./datos-campo";

/*
 * API falsa de la app de campo para Playwright, con la forma exacta de I2 (paquete, cambios,
 * lote) e I1 (alta de adjuntos y tus 1.0), tipada con packages/contratos. Lleva su propio estado
 * de servidor: versiones, transiciones, requisitos del cierre (cierre_incompleto y
 * faltan_adjuntos con las claves `adjuntos.firma` / `adjuntos.foto:<código>`) y subidas.
 */

type OrdenCampo = Schema<"OrdenCampo">;
type OperacionLote = Schema<"OperacionLote">;
type ResultadoOperacion = Schema<"ResultadoOperacion">;
type Problema = Schema<"Problema">;

interface Subida {
    entrada: Schema<"EntradaRegistrar">;
    recibido: number;
}

export interface OpcionesCampo {
    empresas?: Schema<"EmpresaResumen">[];
    ordenes?: OrdenCampo[];
    /** Requisitos de fotos del servidor (código de ítem) además de los del checklist del paquete. */
    fotosExigidas?: string[];
}

export interface ApiCampo {
    /** Sin red: toda petición a la API falla como `internetdisconnected` (junto con setOffline). */
    sinRed: boolean;
    /** Las PATCH de tus fallan (la subida queda a medias) mientras sea true. */
    pausarSubidas: boolean;
    lotes: Schema<"EntradaLote">[];
    resultados: ResultadoOperacion[];
    peticiones: { metodo: string; ruta: string; canal: string | undefined; csrf: string | undefined; empresa: string | undefined }[];
    ordenes: Map<number, OrdenCampo>;
    subidas: Map<string, Subida>;
    /** Rechaza la próxima operación que cumpla `cuando` con este problema. */
    rechazar: (cuando: (op: OperacionLote) => boolean, problema: Problema) => void;
    /** Cambio del lado del servidor que llega por GET /campo/cambios. */
    cambiar: (cambio: Schema<"CambioOrden">) => void;
}

const ESTADO_TRAS: Partial<Record<OperacionLote["tipo"], OrdenCampo["estado"]>> = {
    "orden.iniciar_camino": "en_camino",
    "orden.llegar": "en_sitio",
    "orden.cerrar": "cerrada",
    "orden.fallar": "fallida",
};
const DESDE: Partial<Record<OperacionLote["tipo"], OrdenCampo["estado"][]>> = {
    "orden.iniciar_camino": ["programada"],
    "orden.llegar": ["programada", "en_camino"],
    "orden.cerrar": ["en_sitio"],
    "orden.fallar": ["programada", "en_camino", "en_sitio"],
    "checklist.marcar": ["en_camino", "en_sitio"],
    "material.usar": ["en_camino", "en_sitio"],
    "equipo.instalar": ["en_sitio"],
    "equipo.retirar": ["en_sitio"],
    "firma.capturar": ["en_sitio"],
};

const problema = (codigo: string, status: number, title: string, errors?: Problema["errors"]): Problema => ({
    type: `urn:iris:problema:${codigo}`,
    title,
    status,
    ...(errors ? { errors } : {}),
});

/** Con sesión de técnico ya iniciada (cookies como las deja la API). */
export async function conSesionTecnico(context: BrowserContext, baseURL: string) {
    await context.addCookies([
        { name: "iris_sesion", value: `s:${TECNICO.correo}`, url: baseURL, httpOnly: true, sameSite: "Lax" },
        { name: "iris_csrf", value: CSRF, url: baseURL, sameSite: "Lax" },
    ]);
}

export async function simularApiCampo(page: Page, opciones: OpcionesCampo = {}): Promise<ApiCampo> {
    const ordenesIniciales = opciones.ordenes ?? ordenesDelDia();
    const empresas = opciones.empresas ?? [];
    const pendientesRechazo: { cuando: (op: OperacionLote) => boolean; problema: Problema }[] = [];
    const cambiosPendientes: Schema<"CambioOrden">[] = [];
    let cursor = 1;
    const api: ApiCampo = {
        sinRed: false,
        pausarSubidas: false,
        lotes: [],
        resultados: [],
        peticiones: [],
        ordenes: new Map(ordenesIniciales.map((o) => [o.id, structuredClone(o)])),
        subidas: new Map(),
        rechazar: (cuando, p) => pendientesRechazo.push({ cuando, problema: p }),
        cambiar: (c) => cambiosPendientes.push(c),
    };

    const completo = (ordenId: number, tipo: string, clave?: string) =>
        [...api.subidas.values()].some(
            (s) =>
                s.entrada.entidadId === String(ordenId) &&
                s.entrada.tipo === tipo &&
                (clave === undefined || s.entrada.claveChecklist === clave) &&
                s.recibido === s.entrada.tamano,
        );

    function aplicar(op: OperacionLote, rechazadas: Set<number>): ResultadoOperacion {
        const o = api.ordenes.get(op.ordenId);
        if (!o) {
            return { id: op.id, estado: "rechazada", error: problema("no_encontrado", 404, "No encontrada") };
        }
        const conOrden = (r: ResultadoOperacion): ResultadoOperacion => ({ ...r, orden: { version: o.version, estado: o.estado } });
        if (rechazadas.has(op.ordenId)) {
            return conOrden({ id: op.id, estado: "rechazada", error: problema("bloqueada_por_rechazo", 409, "Bloqueada por un rechazo anterior") });
        }
        const i = pendientesRechazo.findIndex((r) => r.cuando(op));
        if (i >= 0) {
            const [r] = pendientesRechazo.splice(i, 1);
            rechazadas.add(op.ordenId);
            return conOrden({ id: op.id, estado: "rechazada", error: r?.problema ?? problema("validacion", 422, "Inválido") });
        }
        const desde = DESDE[op.tipo];
        if (desde && !desde.includes(o.estado)) {
            rechazadas.add(op.ordenId);
            return conOrden({ id: op.id, estado: "rechazada", error: problema("transicion_invalida", 422, "Transición inválida") });
        }
        const d = (op.datos ?? {}) as Record<string, unknown>;
        const cuando = op.ocurridoEn ?? new Date().toISOString();
        if (op.tipo === "orden.cerrar") {
            const faltan: { field: string; message: string }[] = [];
            for (const it of o.checklist ?? []) {
                if (it.obligatorio && it.valor === null) {
                    faltan.push({ field: `checklist.${it.codigo}`, message: "Falta" });
                }
            }
            if (o.requiereFirma && !o.firma) {
                faltan.push({ field: "firma", message: "Falta la firma" });
            }
            if (o.tipo === "instalacion" && !(o.equipos ?? []).some((e) => e.accion === "instalar")) {
                faltan.push({ field: "equipos.instalar", message: "Falta el equipo" });
            }
            if (faltan.length > 0) {
                rechazadas.add(op.ordenId);
                return conOrden({ id: op.id, estado: "rechazada", error: problema("cierre_incompleto", 422, "Cierre incompleto", faltan) });
            }
            const fotos = new Set([
                ...(o.checklist ?? []).filter((it) => it.tipo === "foto" && it.obligatorio).map((it) => it.codigo),
                ...(opciones.fotosExigidas ?? []),
            ]);
            const faltanAdj = [
                ...[...fotos].filter((c) => !completo(o.id, "foto", c)).map((c) => ({ field: `adjuntos.foto:${c}`, message: "Falta la foto" })),
                ...(o.requiereFirma && !completo(o.id, "firma") ? [{ field: "adjuntos.firma", message: "Falta la firma" }] : []),
            ];
            if (faltanAdj.length > 0) {
                rechazadas.add(op.ordenId);
                return conOrden({ id: op.id, estado: "rechazada", error: problema("faltan_adjuntos", 422, "Faltan adjuntos", faltanAdj) });
            }
        }
        switch (op.tipo) {
            case "checklist.marcar":
                o.checklist = (o.checklist ?? []).map((it) =>
                    it.itemId === d.itemId ? { ...it, valor: d.valor as never, ocurridoEn: cuando, marcadoPor: TECNICO.id } : it,
                );
                break;
            case "nota.agregar":
                o.notas = [...(o.notas ?? []), { id: op.id, texto: String(d.texto), ocurridoEn: cuando, autorId: TECNICO.id, autorNombre: TECNICO.nombre, canal: "campo" }];
                break;
            case "material.usar":
                o.materiales = [
                    ...(o.materiales ?? []),
                    { id: op.id, productoId: Number(d.productoId), cantidad: Number(d.cantidad), descripcion: String(d.descripcion ?? ""), bodegaId: o.bodegaId, excedeExistencia: false, ocurridoEn: cuando },
                ];
                break;
            case "equipo.instalar":
            case "equipo.retirar":
                o.equipos = [
                    ...(o.equipos ?? []),
                    {
                        id: op.id,
                        accion: op.tipo === "equipo.instalar" ? "instalar" : "retirar",
                        serie: (d.serie as string | undefined) ?? null,
                        mac: (d.mac as string | undefined) ?? null,
                        modelo: String(d.modelo ?? ""),
                        unidadId: (d.unidadId as number | undefined) ?? null,
                        condicion: (d.condicion as "buena" | undefined) ?? null,
                        ocurridoEn: cuando,
                        validado: false,
                    },
                ];
                break;
            case "firma.capturar":
                o.firma = { nombre: String(d.nombre), documento: String(d.documento ?? ""), adjuntoId: (d.adjuntoId as string | undefined) ?? null, ocurridoEn: cuando };
                break;
            case "adjunto.registrar":
                if (!api.subidas.has(String(d.adjuntoId))) {
                    rechazadas.add(op.ordenId);
                    return conOrden({ id: op.id, estado: "rechazada", error: problema("no_encontrado", 404, "Adjunto no encontrado") });
                }
                o.adjuntos = [
                    ...(o.adjuntos ?? []),
                    { id: String(d.adjuntoId), tipo: d.tipo as "foto", itemId: (d.itemId as number | undefined) ?? null, descripcion: "", ocurridoEn: cuando },
                ];
                break;
            default:
                break;
        }
        const destino = ESTADO_TRAS[op.tipo];
        if (destino) {
            o.estado = destino;
        }
        o.version += 1;
        return conOrden({ id: op.id, estado: "aplicada" });
    }

    const json = (route: Route, cuerpo: unknown, status = 200, cabeceras: Record<string, string> = {}) =>
        route.fulfill({ status, contentType: "application/json", headers: { Date: new Date().toUTCString(), ...cabeceras }, body: JSON.stringify(cuerpo) });
    const error = (route: Route, p: Problema, cabeceras: Record<string, string> = {}) =>
        route.fulfill({ status: p.status, contentType: "application/problem+json", headers: cabeceras, body: JSON.stringify(p) });

    await page.route("**/api/v1/**", async (route) => {
        const req = route.request();
        const url = new URL(req.url());
        const ruta = url.pathname;
        const metodo = req.method();
        const h = await req.allHeaders();
        api.peticiones.push({ metodo, ruta, canal: h["x-iris-canal"], csrf: h["x-csrf-token"], empresa: h["x-iris-empresa"] });
        if (api.sinRed) {
            return route.abort("internetdisconnected");
        }
        if (!/(?:^|;\s*)iris_sesion=/.test(h.cookie ?? "")) {
            return error(route, problema("no_autenticado", 401, "Sin sesión"));
        }
        if (metodo !== "GET" && metodo !== "HEAD" && h["x-csrf-token"] !== CSRF) {
            return error(route, problema("csrf_invalido", 403, "CSRF"));
        }

        if (ruta === "/api/v1/yo" && metodo === "GET") {
            const empresa = Number(h["x-iris-empresa"]);
            const activa = empresas.find((e) => e.id === empresa) ?? (empresas.length === 1 ? empresas[0] : undefined);
            const yo: Schema<"Yo"> = {
                actor: "usuario",
                usuario: TECNICO,
                permisos: PERMISOS_TECNICO,
                empresas,
                modoEmpresa: empresas.length === 0 ? "sin_conector" : activa ? "activa" : "por_elegir",
                todasLasEmpresas: false,
                hayDatosSinEmpresa: false,
                ...(activa ? { empresaActiva: activa } : {}),
            };
            return json(route, yo);
        }
        if (ruta === "/api/v1/campo/paquete") {
            const paquete: Schema<"Paquete"> = {
                fecha: url.searchParams.get("fecha") ?? new Date().toISOString().slice(0, 10),
                servidorEn: new Date().toISOString(),
                cursor: String(cursor),
                ordenes: [...api.ordenes.values()].filter((o) => !["cerrada", "fallida", "cancelada"].includes(o.estado)).map((o) => structuredClone(o)),
                existencias: existenciasDelTecnico(),
            };
            return json(route, paquete);
        }
        if (ruta === "/api/v1/campo/cambios") {
            const cambios = cambiosPendientes.splice(0);
            if (cambios.length > 0) {
                cursor += 1;
            }
            const r: Schema<"Cambios"> = { cambios, cursor: String(cursor), hayMas: false };
            return json(route, r);
        }
        if (ruta === "/api/v1/sync/lote" && metodo === "POST") {
            const entrada = req.postDataJSON() as Schema<"EntradaLote">;
            api.lotes.push(entrada);
            const rechazadas = new Set<number>();
            const resultados = (entrada.operaciones ?? []).map((op) => aplicar(op, rechazadas));
            api.resultados.push(...resultados);
            const r: Schema<"RespuestaLote"> = { resultados, cursor: entrada.cursor ?? String(cursor), servidorEn: new Date().toISOString(), desfaseMs: 0 };
            return json(route, r);
        }
        if (ruta === "/api/v1/adjuntos" && metodo === "POST") {
            const e = req.postDataJSON() as Schema<"EntradaRegistrar">;
            const previa = api.subidas.get(e.id);
            if (!previa) {
                api.subidas.set(e.id, { entrada: e, recibido: 0 });
            }
            const s = api.subidas.get(e.id) as Subida;
            const adjunto: Schema<"Adjunto"> = {
                id: e.id,
                entidad: e.entidad,
                entidadId: e.entidadId,
                tipo: e.tipo,
                claveChecklist: e.claveChecklist ?? null,
                mime: e.mime,
                tamano: e.tamano,
                sha256: e.sha256,
                recibido: s.recibido,
                estado: s.recibido === e.tamano ? "completo" : s.recibido > 0 ? "subiendo" : "pendiente",
                motivoRechazo: null,
                lat: e.lat ?? null,
                lon: e.lon ?? null,
                tomadoEn: e.tomadoEn ?? null,
                subidoPor: TECNICO.id,
                creadoEn: new Date().toISOString(),
                completadoEn: null,
                urlSubida: `/api/v1/adjuntos/${e.id}/subida`,
                urlDescarga: `/api/v1/adjuntos/${e.id}`,
                version: 1,
            };
            return json(route, adjunto, previa ? 200 : 201, { Location: adjunto.urlSubida });
        }
        const subida = /^\/api\/v1\/adjuntos\/([^/]+)\/subida$/.exec(ruta);
        if (subida) {
            const s = api.subidas.get(subida[1] ?? "");
            if (!s) {
                return error(route, problema("no_encontrado", 404, "No existe"));
            }
            if (metodo === "HEAD") {
                return route.fulfill({ status: 200, headers: { "Upload-Offset": String(s.recibido), "Upload-Length": String(s.entrada.tamano), "Tus-Resumable": "1.0.0" } });
            }
            if (api.pausarSubidas) {
                return route.abort("connectionreset");
            }
            const desde = Number(h["upload-offset"]);
            if (desde !== s.recibido) {
                return error(route, problema("offset_distinto", 409, "Offset distinto"), { "Upload-Offset": String(s.recibido) });
            }
            const cuerpo = req.postDataBuffer();
            s.recibido = Math.min(s.entrada.tamano, s.recibido + (cuerpo?.byteLength ?? 0));
            return route.fulfill({ status: 204, headers: { "Upload-Offset": String(s.recibido), "Tus-Resumable": "1.0.0" } });
        }
        if (ruta === "/api/v1/sesion" && metodo === "DELETE") {
            return route.fulfill({ status: 204 });
        }
        return error(route, problema("no_simulado", 501, `Sin simular: ${metodo} ${ruta}`));
    });
    return api;
}
