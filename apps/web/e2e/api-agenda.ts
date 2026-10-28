import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { CUADRILLAS, escenarioAgenda, TECNICOS, TURNO, ZONAS, type EscenarioAgenda } from "./datos-agenda";
import { TIPOS_ORDEN, type EscenarioOrdenes } from "./datos-ordenes";

/*
 * Agenda y posiciones en la API falsa (e2e/api-falsa.ts): catálogos, `GET /agenda` (citas desde
 * las órdenes del escenario), `GET /tecnicos/asignables` (turno 07–17, ausencias, citas y huecos)
 * y `GET /campo/posiciones/ultimas`. `validarCita` reproduce los 422 de `agenda.ReservarCita` que
 * el panel muestra (técnico sin agenda, ausente, fuera de turno, solape).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type OrdenDetalle = Schema<"OrdenDetalle">;
type Franja = Schema<"Franja">;

const MS_MIN = 60_000;
const iso = (ms: number) => new Date(ms).toISOString().replace(".000Z", "Z");
const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const hora = (ms: number) => new Date(ms - 6 * 3_600_000).toISOString().slice(11, 16);
/** Medianoche de Guatemala del día de un instante. */
const diaDe = (ms: number) => {
    const local = new Date(ms - 6 * 3_600_000);
    return Date.UTC(local.getUTCFullYear(), local.getUTCMonth(), local.getUTCDate(), 6);
};
const VIVAS: readonly OrdenDetalle["estado"][] = ["programada", "en_camino", "en_sitio", "cerrada"];

export type ContextoAgenda = {
    route: Route;
    metodo: string;
    ruta: string;
    query: URLSearchParams;
    puede: (p: Permiso) => boolean;
    empresaId: number | null;
};

export type ApiAgenda = {
    escenario: EscenarioAgenda;
    manejar: (c: ContextoAgenda) => Promise<boolean>;
    /** 422 de la agenda al programar, reprogramar o asignar (o `undefined` si se acepta). */
    validarCita: (o: OrdenDetalle, e: { tecnicoId?: number; inicio?: string; fin?: string }, accion: string) => Problema | undefined;
};

function restar(base: Franja[], quitar: { inicio: number; fin: number }[]): Franja[] {
    let libres = base.map((f) => ({ inicio: Date.parse(f.inicio), fin: Date.parse(f.fin) }));
    for (const q of quitar) {
        libres = libres.flatMap((l) => {
            if (q.fin <= l.inicio || q.inicio >= l.fin) return [l];
            return [
                { inicio: l.inicio, fin: q.inicio },
                { inicio: q.fin, fin: l.fin },
            ].filter((x) => x.fin - x.inicio > 0);
        });
    }
    return libres.map((l) => ({ inicio: iso(l.inicio), fin: iso(l.fin) }));
}

export function apiAgenda(ahora: number, ordenes: EscenarioOrdenes, empresa: number | null, despacho: boolean): ApiAgenda {
    const esc = escenarioAgenda(ahora, ordenes, empresa, despacho);

    const cuadrillaDe = (usuarioId: number) => TECNICOS.find((t) => t.usuarioId === usuarioId)?.cuadrillaId ?? null;
    const citasDe = (tecnicoId: number, desde: number, hasta: number, excepto?: number) =>
        ordenes.ordenes.filter(
            (o) =>
                o.tecnicoId === tecnicoId &&
                o.id !== excepto &&
                VIVAS.includes(o.estado) &&
                o.programadaInicio !== null &&
                Date.parse(o.programadaInicio) < hasta &&
                Date.parse(o.programadaFin ?? o.programadaInicio) > desde,
        );
    const ausenciasDe = (tecnicoId: number, desde: number, hasta: number) => esc.ausencias.filter((a) => a.tecnicoId === tecnicoId && Date.parse(a.inicio) < hasta && Date.parse(a.fin) > desde);

    function asignable(t: Schema<"Tecnico">, dia: number, duracion: number, ordenId: number | undefined): Schema<"Asignable"> {
        const turno: Franja = { inicio: iso(dia + TURNO.desde * MS_MIN), fin: iso(dia + TURNO.hasta * MS_MIN) };
        const fin = dia + 24 * 60 * MS_MIN;
        const citas = citasDe(t.usuarioId, dia, fin, ordenId);
        const ausencias = ausenciasDe(t.usuarioId, dia, fin);
        const libres = restar(
            [turno],
            [...citas.map((c) => ({ inicio: Date.parse(c.programadaInicio ?? ""), fin: Date.parse(c.programadaFin ?? "") })), ...ausencias.map((a) => ({ inicio: Date.parse(a.inicio), fin: Date.parse(a.fin) }))],
        );
        const minutosLibres = libres.reduce((n, l) => n + (Date.parse(l.fin) - Date.parse(l.inicio)) / MS_MIN, 0);
        const ausenteTodo = ausencias.some((a) => Date.parse(a.inicio) <= Date.parse(turno.inicio) && Date.parse(a.fin) >= Date.parse(turno.fin));
        const hueco = libres.some((l) => (Date.parse(l.fin) - Date.parse(l.inicio)) / MS_MIN >= duracion);
        return {
            tecnicoId: t.id,
            usuarioId: t.usuarioId,
            nombre: t.nombre,
            cuadrillaId: t.cuadrillaId,
            habilidades: t.habilidades,
            disponible: !ausenteTodo && hueco,
            motivo: ausenteTodo ? "ausente" : hueco ? null : "sin_espacio",
            turnos: [turno],
            ausencias: ausencias.map((a) => ({ inicio: a.inicio, fin: a.fin })),
            ocupado: citas.map((c) => ({ ordenId: c.id, numero: c.numero, estado: c.estado, inicio: c.programadaInicio ?? "", fin: c.programadaFin ?? "" })),
            libres,
            minutosLibres,
            capacidad: [],
        };
    }

    function validarCita(o: OrdenDetalle, e: { tecnicoId?: number; inicio?: string; fin?: string }, accion: string): Problema | undefined {
        const tecnicoId = e.tecnicoId ?? o.tecnicoId;
        if (tecnicoId === null || tecnicoId === undefined) return undefined;
        const t = TECNICOS.find((x) => x.usuarioId === tecnicoId);
        if (!t) {
            return { type: "urn:iris:problema:tecnico_sin_agenda", title: "Técnico sin agenda", status: 422, detail: "El técnico no tiene ficha activa en la agenda", errors: [{ field: "body.tecnicoId", message: "Sin ficha de agenda" }] };
        }
        const inicioIso = e.inicio ?? (accion === "asignar" ? o.programadaInicio : null);
        if (!inicioIso) return undefined;
        const inicio = Date.parse(inicioIso);
        const duracion = TIPOS_ORDEN.find((x) => x.codigo === o.tipo)?.duracionMin ?? 60;
        const fin = e.fin ? Date.parse(e.fin) : accion === "asignar" && o.programadaFin ? Date.parse(o.programadaFin) : inicio + duracion * MS_MIN;
        const dia = diaDe(inicio);
        const ausencia = ausenciasDe(tecnicoId, inicio, fin)[0];
        if (ausencia) {
            return { type: "urn:iris:problema:tecnico_ausente", title: "Técnico ausente", status: 422, detail: `${t.nombre} está ausente (${ausencia.motivo}) en ese horario` };
        }
        if (inicio < dia + TURNO.desde * MS_MIN || fin > dia + TURNO.hasta * MS_MIN) {
            return { type: "urn:iris:problema:fuera_de_turno", title: "Fuera de turno", status: 422, detail: `La visita (${hora(inicio)}–${hora(fin)}) queda fuera del turno de ${t.nombre} (07:00–17:00)` };
        }
        const choque = citasDe(tecnicoId, inicio, fin, o.id)[0];
        if (choque) {
            return {
                type: "urn:iris:problema:solape",
                title: "Se solapa con otra cita",
                status: 422,
                detail: `Se solapa con ${choque.numero} (${hora(Date.parse(choque.programadaInicio ?? ""))}–${hora(Date.parse(choque.programadaFin ?? ""))})`,
            };
        }
        return undefined;
    }

    async function manejar(c: ContextoAgenda): Promise<boolean> {
        const { route, metodo, ruta, query, puede } = c;
        if (metodo !== "GET") return false;
        const sinPermiso = () => problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 }).then(() => true);
        const num = (k: string) => (query.get(k) ? Number(query.get(k)) : undefined);

        if (ruta === "/api/v1/zonas") {
            if (!puede("agenda.ver")) return sinPermiso();
            await json(route, { data: ZONAS, nextCursor: null } satisfies Schema<"PaginaZona">);
            return true;
        }
        if (ruta === "/api/v1/cuadrillas") {
            if (!puede("agenda.ver")) return sinPermiso();
            await json(route, { data: CUADRILLAS, nextCursor: null } satisfies Schema<"PaginaCuadrilla">);
            return true;
        }
        if (ruta === "/api/v1/tecnicos") {
            if (!puede("agenda.ver")) return sinPermiso();
            const cuadrilla = num("cuadrillaId");
            const zona = num("zonaId");
            const data = TECNICOS.filter((t) => cuadrilla === undefined || t.cuadrillaId === cuadrilla).filter(
                (t) => zona === undefined || (CUADRILLAS.find((q) => q.id === t.cuadrillaId)?.zonaIds ?? []).includes(zona),
            );
            await json(route, { data, nextCursor: null } satisfies Schema<"PaginaTecnico">);
            return true;
        }
        if (ruta === "/api/v1/tecnicos/asignables") {
            if (!puede("agenda.ver")) return sinPermiso();
            const fecha = query.get("fecha") ?? "";
            const dia = Date.parse(`${fecha}T06:00:00Z`);
            if (Number.isNaN(dia)) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "fecha", message: "AAAA-MM-DD" }] });
                return true;
            }
            const tipo = query.get("tipo");
            const duracion = TIPOS_ORDEN.find((x) => x.codigo === tipo)?.duracionMin ?? 0;
            const zona = num("zonaId");
            const lista = TECNICOS.filter((t) => zona === undefined || (CUADRILLAS.find((q) => q.id === t.cuadrillaId)?.zonaIds ?? []).includes(zona))
                .map((t) => asignable(t, dia, duracion, num("ordenId")))
                .sort((a, b) => Number(b.disponible) - Number(a.disponible));
            await json(route, { fecha, tipo: tipo || null, duracionMin: duracion, data: lista } satisfies Schema<"RespuestaAsignables">);
            return true;
        }
        if (ruta === "/api/v1/agenda") {
            if (!puede("agenda.ver")) return sinPermiso();
            const desde = Date.parse(query.get("desde") ?? "");
            const hasta = Date.parse(query.get("hasta") ?? "");
            if (Number.isNaN(desde) || Number.isNaN(hasta) || hasta <= desde || hasta - desde > 31 * 86_400_000) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, detail: "Rango de hasta 31 días" });
                return true;
            }
            const cuadrilla = num("cuadrillaId");
            const zona = num("zonaId");
            const tecnico = num("tecnicoId");
            const citas: Schema<"CitaAgenda">[] = ordenes.ordenes
                .filter((o) => o.empresaId === c.empresaId && o.tecnicoId !== null && o.programadaInicio !== null)
                .filter((o) => Date.parse(o.programadaInicio ?? "") >= desde && Date.parse(o.programadaInicio ?? "") < hasta)
                .filter((o) => cuadrilla === undefined || (o.cuadrillaId ?? cuadrillaDe(o.tecnicoId ?? 0)) === cuadrilla)
                .filter((o) => zona === undefined || o.zonaId === zona)
                .filter((o) => tecnico === undefined || o.tecnicoId === tecnico)
                .map((o) => {
                    const v = esc.ventanas.get(o.id);
                    const enVentana = v && o.programadaInicio && Date.parse(o.programadaInicio) >= Date.parse(v.inicio) && Date.parse(o.programadaInicio) <= Date.parse(v.fin);
                    return {
                        ordenId: o.id,
                        numero: o.numero,
                        tipo: o.tipo,
                        estado: o.estado,
                        ordenVersion: o.version,
                        tecnicoId: o.tecnicoId ?? 0,
                        inicio: o.programadaInicio ?? "",
                        fin: o.programadaFin ?? o.programadaInicio ?? "",
                        ventanaInicio: enVentana ? v.inicio : null,
                        ventanaFin: enVentana ? v.fin : null,
                        zonaId: o.zonaId,
                        cuadrillaId: o.cuadrillaId,
                        direccion: o.direccion,
                        enSitioEn: o.enSitioEn,
                        lat: o.lat,
                        lon: o.lon,
                        clienteId: o.clienteId,
                        servicioId: o.servicioId,
                    };
                });
            const visibles = new Set(TECNICOS.filter((t) => cuadrilla === undefined || t.cuadrillaId === cuadrilla).map((t) => t.usuarioId));
            const ausencias = esc.ausencias.filter((a) => visibles.has(a.tecnicoId) && Date.parse(a.inicio) < hasta && Date.parse(a.fin) > desde);
            await json(route, { desde: iso(desde), hasta: iso(hasta), citas, ausencias } satisfies Schema<"Calendario">);
            return true;
        }
        if (ruta === "/api/v1/campo/posiciones/ultimas") {
            if (!puede("campo.ver_posiciones")) return sinPermiso();
            const cuadrilla = num("cuadrillaId");
            // Como la API: la antigüedad se mide en cada lectura contra su reloj.
            const data = esc.posiciones
                .filter((p) => cuadrilla === undefined || p.cuadrillaId === cuadrilla)
                .map((p) => ({ ...p, antiguedadSeg: Math.max(0, Math.round((Date.now() - Date.parse(p.registradaEn)) / 1000)) }));
            await json(route, { servidorEn: iso(Date.now()), data } satisfies Schema<"UltimasPosiciones">);
            return true;
        }
        return false;
    }

    return { escenario: esc, manejar, validarCita };
}
