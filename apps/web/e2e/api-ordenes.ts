import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { escenarioOrdenes, PNG, soloOrden, TIPOS_ORDEN, type EscenarioOrdenes } from "./datos-ordenes";

/*
 * Órdenes, adjuntos y pendientes hacia Argos en la API falsa (e2e/api-falsa.ts). Reproduce las
 * reglas que importan al panel: alcance (`ordenes.ver` / `ordenes.ver_propias`), transiciones
 * del README de campo (422 `transicion_invalida`), `version` (409), `Idempotency-Key`
 * obligatoria, descarga autenticada de adjuntos y reintento de pendientes.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Orden = Schema<"OrdenDetalle">;

export type ContextoOrdenes = {
    route: Route;
    metodo: string;
    ruta: string;
    query: URLSearchParams;
    cuerpo: unknown;
    idempotencia: string | undefined;
    actual: Schema<"Usuario">;
    puede: (p: Permiso) => boolean;
    empresaId: number | null;
    /** Tickets visibles (para crear desde un ticket). */
    tickets: readonly Schema<"Ticket">[];
};

export type ApiOrdenes = {
    escenario: EscenarioOrdenes;
    /** ¿Atendió la petición? */
    manejar: (c: ContextoOrdenes) => Promise<boolean>;
};

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const prohibido = (route: Route) => problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
const noEncontrado = (route: Route) => problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });

function pagina<T>(lista: readonly T[], q: URLSearchParams, porDefecto = 50): { data: T[]; nextCursor: string | null } {
    const desde = Number(q.get("cursor")?.replace("c:", "") ?? 0);
    const limite = Number(q.get("limit") ?? porDefecto);
    const corte = lista.slice(desde, desde + limite + 1);
    return { data: corte.slice(0, limite), nextCursor: corte.length > limite ? `c:${desde + limite}` : null };
}

const TRANSICIONES: Record<string, readonly Orden["estado"][]> = {
    asignar: ["pendiente", "programada", "en_camino"],
    programar: ["pendiente"],
    reprogramar: ["programada", "en_camino", "fallida"],
    cancelar: ["pendiente", "programada", "en_camino", "en_sitio"],
    reabrir: ["fallida", "cancelada"],
};

export function apiOrdenes(empresas: readonly number[]): ApiOrdenes {
    const esc = escenarioOrdenes(Date.now(), empresas);
    let siguiente = 1000;
    let siguienteEvento = 5000;

    async function manejar(c: ContextoOrdenes): Promise<boolean> {
        const { route, metodo, ruta, query, cuerpo, actual, puede, empresaId } = c;
        const veTodas = puede("ordenes.ver");
        const visibles = esc.ordenes.filter((o) => o.empresaId === empresaId && (veTodas || (puede("ordenes.ver_propias") && o.tecnicoId === actual.id)));

        if (metodo === "GET" && ruta === "/api/v1/tipos-orden") {
            await json(route, { data: TIPOS_ORDEN, nextCursor: null } satisfies Schema<"PaginaTipoOrden">);
            return true;
        }

        if (ruta === "/api/v1/ordenes" && metodo === "GET") {
            if (!veTodas && !puede("ordenes.ver_propias")) return prohibido(route).then(() => true);
            if (query.getAll("estado").length > 1 || query.getAll("tipo").length > 1) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, detail: "estado repetido" });
                return true;
            }
            const estados = query.get("estado")?.split(",");
            const tipos = query.get("tipo")?.split(",");
            const texto = query.get("q")?.toLowerCase();
            const desde = query.get("desde");
            const hasta = query.get("hasta");
            const lista = visibles
                .filter((o) => !estados || estados.includes(o.estado))
                .filter((o) => !tipos || tipos.includes(o.tipo))
                .filter((o) => !query.get("tecnicoId") || o.tecnicoId === Number(query.get("tecnicoId")))
                .filter((o) => query.get("sinTecnico") !== "true" || o.tecnicoId === null)
                .filter((o) => !query.get("ticketId") || o.ticketId === Number(query.get("ticketId")))
                .filter((o) => !query.get("zonaId") || o.zonaId === Number(query.get("zonaId")))
                .filter((o) => !desde || (o.programadaInicio !== null && o.programadaInicio >= desde))
                .filter((o) => !hasta || (o.programadaInicio !== null && o.programadaInicio < hasta))
                .filter((o) => !texto || o.numero.toLowerCase().includes(texto) || o.direccion.toLowerCase().includes(texto))
                // La API ordena por id desc; aquí ascendente para que los casos salgan en la primera página.
                .sort((a, b) => a.id - b.id)
                .map(soloOrden);
            await json(route, pagina(lista, query) satisfies Schema<"PaginaOrden">);
            return true;
        }

        if (ruta === "/api/v1/ordenes" && metodo === "POST") {
            if (!puede("ordenes.crear")) return prohibido(route).then(() => true);
            if (!c.idempotencia) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
                return true;
            }
            const e = cuerpo as Schema<"EntradaCrearOrden">;
            const ticket = e.ticketId !== undefined ? c.tickets.find((t) => t.id === e.ticketId) : undefined;
            if (e.ticketId !== undefined && !ticket) {
                await problema(route, { type: "urn:iris:problema:ticket_invalido", title: "Ticket inválido", status: 422, detail: "El ticket no existe o no lo puedes ver" });
                return true;
            }
            if (!ticket && e.clienteId === undefined && !e.direccion) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "body.direccion", message: "Obligatoria sin instantánea de Argos" }] });
                return true;
            }
            const id = siguiente++;
            const ahora = new Date().toISOString();
            const base = esc.ordenes[1];
            if (!base) return noEncontrado(route).then(() => true);
            const nueva: Orden = {
                ...base,
                id,
                numero: `OT-${String(id).padStart(6, "0")}`,
                tipo: e.tipo,
                tipoId: TIPOS_ORDEN.find((t) => t.codigo === e.tipo)?.id ?? 1,
                plantillaId: e.plantillaId ?? null,
                estado: "pendiente",
                empresaId,
                ticketId: ticket?.id ?? null,
                clienteId: ticket?.clienteId ?? e.clienteId ?? null,
                servicioId: ticket?.servicioId ?? e.servicioId ?? null,
                zonaId: ticket?.zonaId ?? e.zonaId ?? null,
                tecnicoId: null,
                direccion: e.direccion ?? (ticket ? "Dirección del servicio en Argos" : ""),
                lat: e.lat ?? null,
                lon: e.lon ?? null,
                contacto: ticket ? { nombre: ticket.contacto.nombre, telefono: ticket.contacto.telefono } : { nombre: e.contacto?.nombre ?? null, telefono: e.contacto?.telefono ?? null },
                descripcion: e.descripcion ?? "",
                programadaInicio: null,
                programadaFin: null,
                version: 1,
                creadoEn: ahora,
                actualizadoEn: ahora,
                checklist: [],
                notas: [],
                materiales: [],
                equipos: [],
                adjuntos: [],
                requiereFirma: false,
            };
            delete nueva.firma;
            esc.ordenes.push(nueva);
            esc.eventos.push({ id: siguienteEvento++, ordenId: id, tipo: "creada", de: null, a: "pendiente", motivo: null, datos: {}, actorTipo: "usuario", actorId: actual.id, actorNombre: actual.nombre, canal: "panel", ocurridoEn: ahora, creadoEn: ahora });
            await json(route, soloOrden(nueva), 201);
            return true;
        }

        const deOrden = /^\/api\/v1\/ordenes\/(\d+)(?:\/([a-z-]+))?$/.exec(ruta);
        if (deOrden) {
            if (!veTodas && !puede("ordenes.ver_propias")) return prohibido(route).then(() => true);
            const o = visibles.find((x) => x.id === Number(deOrden[1]));
            if (!o) return noEncontrado(route).then(() => true);
            const sub = deOrden[2];
            if (metodo === "GET" && !sub) {
                await json(route, o);
                return true;
            }
            if (metodo === "GET" && sub === "eventos") {
                const data: Schema<"EventoOrden">[] = esc.eventos
                    .filter((e) => e.ordenId === o.id)
                    .map((e) => {
                        const copia: Partial<typeof e> = { ...e };
                        delete copia.ordenId;
                        return copia as Schema<"EventoOrden">;
                    });
                await json(route, { data, nextCursor: null } satisfies Schema<"PaginaEventoOrden">);
                return true;
            }
            if (metodo === "POST" && sub && sub in TRANSICIONES) {
                if (!puede(`ordenes.${sub}` as Permiso)) return prohibido(route).then(() => true);
                if (!c.idempotencia) {
                    await problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
                    return true;
                }
                const e = (cuerpo ?? {}) as { version?: number; tecnicoId?: number; inicio?: string; motivo?: string };
                if (e.version !== o.version) {
                    await problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "La orden cambió desde que la leíste" });
                    return true;
                }
                if (!TRANSICIONES[sub]?.includes(o.estado)) {
                    await problema(route, { type: "urn:iris:problema:transicion_invalida", title: "Transición inválida", status: 422, detail: `No se puede ${sub} una orden ${o.estado}` });
                    return true;
                }
                if ((sub === "cancelar" || sub === "reabrir" || sub === "reprogramar") && (!e.motivo || e.motivo.length < 3)) {
                    await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "body.motivo", message: "Obligatorio" }] });
                    return true;
                }
                if (e.tecnicoId !== undefined && e.tecnicoId !== 3) {
                    await problema(route, { type: "urn:iris:problema:tecnico_fuera_de_empresa", title: "Técnico fuera de la empresa", status: 422, errors: [{ field: "body.tecnicoId", message: "El técnico no opera en esta empresa" }] });
                    return true;
                }
                const ahora = new Date().toISOString();
                const antes = o.estado;
                if (sub === "asignar") {
                    o.tecnicoId = e.tecnicoId ?? null;
                    if (o.tecnicoId === null) o.estado = "pendiente";
                } else if (sub === "programar" || sub === "reprogramar") {
                    if (e.tecnicoId === undefined && o.tecnicoId === null) {
                        await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "body.tecnicoId", message: "Obligatorio" }] });
                        return true;
                    }
                    o.tecnicoId = e.tecnicoId ?? o.tecnicoId;
                    o.programadaInicio = e.inicio ?? null;
                    const dur = TIPOS_ORDEN.find((t) => t.codigo === o.tipo)?.duracionMin ?? 60;
                    o.programadaFin = e.inicio ? new Date(Date.parse(e.inicio) + dur * 60_000).toISOString() : null;
                    o.estado = "programada";
                    o.resultado = null;
                    o.motivoFalla = null;
                    if (sub === "reprogramar") o.motivo = e.motivo ?? null;
                    esc.eventos.push({ id: siguienteEvento++, ordenId: o.id, tipo: "programada", de: null, a: null, motivo: e.motivo ?? null, datos: { tecnico_id: o.tecnicoId, inicio: o.programadaInicio, fin: o.programadaFin, reprogramada: sub === "reprogramar" }, actorTipo: "usuario", actorId: actual.id, actorNombre: actual.nombre, canal: "panel", ocurridoEn: ahora, creadoEn: ahora });
                } else if (sub === "cancelar") {
                    o.estado = "cancelada";
                    o.resultado = "cancelada";
                    o.motivo = e.motivo ?? null;
                    o.finalizadaEn = ahora;
                } else if (sub === "reabrir") {
                    o.estado = "pendiente";
                    o.resultado = null;
                    o.motivo = e.motivo ?? null;
                    o.finalizadaEn = null;
                }
                if (antes !== o.estado) {
                    esc.eventos.push({ id: siguienteEvento++, ordenId: o.id, tipo: "estado", de: antes, a: o.estado, motivo: e.motivo ?? null, datos: {}, actorTipo: "usuario", actorId: actual.id, actorNombre: actual.nombre, canal: "panel", ocurridoEn: ahora, creadoEn: ahora });
                }
                o.version++;
                o.actualizadoEn = ahora;
                await json(route, soloOrden(o));
                return true;
            }
            return noEncontrado(route).then(() => true);
        }

        if (ruta === "/api/v1/adjuntos" && metodo === "GET") {
            if (!puede("adjuntos.ver")) return prohibido(route).then(() => true);
            const ordenId = Number(query.get("entidadId"));
            if (query.get("entidad") !== "orden" || !visibles.some((o) => o.id === ordenId)) return noEncontrado(route).then(() => true);
            await json(route, { data: esc.adjuntos.filter((a) => a.entidadId === String(ordenId)), nextCursor: null } satisfies Schema<"PaginaAdjunto">);
            return true;
        }
        const deAdjunto = /^\/api\/v1\/adjuntos\/([0-9a-f-]{36})$/.exec(ruta);
        if (deAdjunto && metodo === "GET") {
            if (!puede("adjuntos.ver")) return prohibido(route).then(() => true);
            const a = esc.adjuntos.find((x) => x.id === deAdjunto[1] && visibles.some((o) => String(o.id) === x.entidadId));
            if (!a) return noEncontrado(route).then(() => true);
            if (a.estado !== "completo") {
                await problema(route, { type: "urn:iris:problema:adjunto_incompleto", title: "Adjunto incompleto", status: 409 });
                return true;
            }
            const datos = a.tipo === "firma" ? PNG.firma : a.id.endsWith("1") ? PNG.fotoA : PNG.fotoB;
            await route.fulfill({
                status: 200,
                contentType: a.mime,
                headers: { "Content-Disposition": "inline", "X-Content-Type-Options": "nosniff", "Content-Security-Policy": "default-src 'none'; sandbox" },
                body: Buffer.from(datos, "base64"),
            });
            return true;
        }

        if (ruta === "/api/v1/integraciones/pendientes" && metodo === "GET") {
            if (!puede("integraciones.ver_pendientes")) return prohibido(route).then(() => true);
            const lista = esc.pendientes
                .filter((p) => esc.ordenes.some((o) => String(o.id) === p.entidadId && o.empresaId === empresaId))
                .filter((p) => !query.get("entidad") || p.entidad === query.get("entidad"))
                .filter((p) => !query.get("entidadId") || p.entidadId === query.get("entidadId"))
                .filter((p) => !query.get("estado") || p.estado === query.get("estado"))
                .filter((p) => query.get("conflicto") !== "true" || p.conflicto !== null)
                .sort((a, b) => b.id - a.id);
            await json(route, pagina(lista, query) satisfies Schema<"PaginaPendiente">);
            return true;
        }
        const reintento = /^\/api\/v1\/integraciones\/pendientes\/(\d+)\/reintentar$/.exec(ruta);
        if (reintento && metodo === "POST") {
            if (!puede("integraciones.reintentar")) return prohibido(route).then(() => true);
            const p = esc.pendientes.find((x) => x.id === Number(reintento[1]));
            if (!p) return noEncontrado(route).then(() => true);
            if (!c.idempotencia) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 });
                return true;
            }
            if ((cuerpo as { version?: number } | undefined)?.version !== p.version) {
                await problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "El pendiente cambió" });
                return true;
            }
            if (!(p.estado === "fallido_negocio" || p.estado === "agotado" || p.estado === "pendiente")) {
                await problema(route, { type: "urn:iris:problema:pendiente_no_reintentable", title: "No se puede reintentar", status: 422, detail: "Está hecho, en curso o bloqueado" });
                return true;
            }
            Object.assign(p, { estado: "pendiente", intentos: 0, proximoIntento: new Date().toISOString(), version: p.version + 1 } satisfies Partial<Schema<"Pendiente">>);
            await json(route, p);
            return true;
        }
        return false;
    }

    return { escenario: esc, manejar };
}
