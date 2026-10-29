import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { JORGE, LUCIA, TECNICOS, medianocheGt } from "./datos-agenda";
import type { EscenarioOrdenes } from "./datos-ordenes";

/*
 * Rutas del día en la API falsa (e2e/api-falsa.ts), tipadas con packages/contratos: listar,
 * ver (propuesta frente a la real), proponer (optimizada o con `ordenIds`) y publicar, con
 * `version` (409), los 422 que el panel muestra (`ruta_vacia`, `ruta_desactualizada`,
 * `transicion_invalida`, `validacion`) e `Idempotency-Key` obligatoria. Con el escenario del
 * despacho trae la ruta publicada de Lucía (con recorrido real) y un borrador de Jorge degradado,
 * desactualizado y con una parada oculta (sin geometría ni recorrido).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type RutaDetalle = Schema<"RutaDetalle">;
type Parada = Schema<"ParadaRuta">;
type OrdenDetalle = Schema<"OrdenDetalle">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const iso = (ms: number) => new Date(ms).toISOString().replace(".000Z", "Z");
const VIVAS: readonly OrdenDetalle["estado"][] = ["programada", "en_camino", "en_sitio", "pausada", "cerrada"];
const FIJAS: readonly OrdenDetalle["estado"][] = ["en_camino", "en_sitio", "cerrada"];

export type ContextoRutas = {
    route: Route;
    metodo: string;
    ruta: string;
    query: URLSearchParams;
    cuerpo: unknown;
    idempotencia: string | undefined;
    puede: (p: Permiso) => boolean;
    empresaId: number | null;
};

export type ApiRutas = { rutas: RutaDetalle[]; manejar: (c: ContextoRutas) => Promise<boolean> };

/** `Ruta` (lista) a partir del detalle. */
function resumen(d: RutaDetalle): Schema<"Ruta"> {
    const r: Partial<RutaDetalle> = { ...d };
    delete r.geometria;
    delete r.paradas;
    delete r.real;
    return r as Schema<"Ruta">;
}

export function apiRutas(ahora: number, ordenes: EscenarioOrdenes, empresa: number | null, despacho: boolean): ApiRutas {
    const rutas: RutaDetalle[] = [];
    let siguiente = 701;
    const hoy = new Date(medianocheGt(ahora) - 6 * 3_600_000).toISOString().slice(0, 10);

    const citasDe = (tecnicoId: number, fecha: string) => {
        const desde = Date.parse(`${fecha}T06:00:00Z`);
        return ordenes.ordenes.filter(
            (o) => o.tecnicoId === tecnicoId && o.empresaId === empresa && VIVAS.includes(o.estado) && o.programadaInicio !== null && Date.parse(o.programadaInicio) >= desde && Date.parse(o.programadaInicio) < desde + 86_400_000,
        );
    };

    function parada(o: OrdenDetalle, posicion: number): Parada {
        const estimada = o.programadaInicio;
        const llegadaReal = o.enSitioEn;
        return {
            posicion,
            ordenId: o.id,
            numero: o.numero,
            tipo: o.tipo,
            estado: o.estado,
            oculta: false,
            vigente: true,
            direccion: o.direccion,
            lat: o.lat,
            lon: o.lon,
            sinCoordenadas: o.lat === null || o.lon === null,
            duracionMin: 60,
            ventanaInicio: null,
            ventanaFin: null,
            citaInicio: o.programadaInicio,
            fija: FIJAS.includes(o.estado),
            llegadaEstimada: estimada,
            salidaEstimada: o.programadaFin,
            viajeSeg: 900,
            distanciaM: 4200,
            esperaSeg: 0,
            retrasoSeg: 0,
            fueraDeVentana: false,
            enCaminoEn: o.enCaminoEn,
            llegadaReal,
            finalizadaEn: o.finalizadaEn,
            desvioSeg: llegadaReal && estimada ? Math.round((Date.parse(llegadaReal) - Date.parse(estimada)) / 1000) : null,
        };
    }

    /** Arma la ruta como la propondría la API: fijas primero en el orden real; el resto por hora o por `ordenIds`. */
    function construir(tecnicoId: number, fecha: string, ordenIds: number[] | undefined): RutaDetalle | Problema {
        const nombre = TECNICOS.find((t) => t.usuarioId === tecnicoId)?.nombre ?? `Técnico ${tecnicoId}`;
        const citas = citasDe(tecnicoId, fecha).sort((a, b) => (a.programadaInicio ?? "").localeCompare(b.programadaInicio ?? ""));
        if (citas.length === 0) return { type: "urn:iris:problema:ruta_vacia", title: "Ruta vacía", status: 422, detail: `${nombre} no tiene citas programadas ese día` };
        const fijas = citas.filter((o) => FIJAS.includes(o.estado));
        let resto = citas.filter((o) => !FIJAS.includes(o.estado));
        if (ordenIds) {
            const sinFijas = ordenIds.filter((id) => !fijas.some((f) => f.id === id));
            if (sinFijas.length !== resto.length || !resto.every((o) => sinFijas.includes(o.id))) {
                return { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: "body.ordenIds", message: "Deben ir todas las órdenes programadas del técnico ese día" }] };
            }
            resto = sinFijas.map((id) => resto.find((o) => o.id === id) as OrdenDetalle);
        }
        const lista = [...fijas, ...resto];
        const paradas = lista.map((o, i) => parada(o, i + 1));
        const coords = lista.filter((o) => o.lat !== null && o.lon !== null).map((o) => [o.lon, o.lat]);
        const ahoraIso = iso(Date.now());
        return {
            id: 0,
            empresaId: empresa,
            tecnicoId,
            tecnicoNombre: nombre,
            fecha,
            estado: "borrador",
            modo: ordenIds ? "manual" : "optimizada",
            motor: "osrm",
            degradada: false,
            motivoMotor: null,
            origenSalida: "base",
            salidaLat: 14.62,
            salidaLon: -90.52,
            saleEn: lista[0]?.programadaInicio ?? ahoraIso,
            terminaEn: lista.at(-1)?.programadaFin ?? null,
            finTurno: iso(Date.parse(`${fecha}T06:00:00Z`) + 17 * 3_600_000),
            excedeTurno: false,
            distanciaM: paradas.reduce((n, p) => n + p.distanciaM, 0),
            viajeSeg: paradas.reduce((n, p) => n + p.viajeSeg, 0),
            esperaSeg: 0,
            totalParadas: paradas.length,
            fueraDeVentana: 0,
            desactualizada: false,
            desactualizadaEn: null,
            motivoDesactualizada: null,
            propuestaPor: 1,
            propuestaEn: ahoraIso,
            publicadaPor: null,
            publicadaEn: null,
            reemplazadaEn: null,
            reemplazadaPorId: null,
            version: 1,
            geometria: coords.length >= 2 ? { type: "LineString", coordinates: coords } : null,
            paradas,
            real: { disponible: false, motivo: "sin_posiciones", puntos: [], totalPuntos: 0, distanciaM: 0, primeraEn: null, ultimaEn: null, ordenVisitado: [] },
        };
    }

    const esProblema = (x: RutaDetalle | Problema): x is Problema => "status" in x;

    if (despacho) {
        const lucia = construir(LUCIA, hoy, undefined);
        if (!esProblema(lucia)) {
            const visitadas = (lucia.paradas ?? []).filter((p) => p.llegadaReal !== null);
            rutas.push({
                ...lucia,
                id: siguiente++,
                estado: "publicada",
                version: 2,
                publicadaPor: 1,
                publicadaEn: lucia.propuestaEn,
                real: {
                    disponible: true,
                    motivo: null,
                    puntos: [
                        { lat: 14.62, lon: -90.52, registradaEn: iso(medianocheGt(ahora) + 7.9 * 3_600_000) },
                        { lat: 14.634915, lon: -90.506882, registradaEn: iso(medianocheGt(ahora) + 8.5 * 3_600_000) },
                        { lat: 14.6011, lon: -90.5102, registradaEn: iso(medianocheGt(ahora) + 10.9 * 3_600_000) },
                    ],
                    totalPuntos: 3,
                    distanciaM: 6100,
                    primeraEn: iso(medianocheGt(ahora) + 7.9 * 3_600_000),
                    ultimaEn: iso(medianocheGt(ahora) + 10.9 * 3_600_000),
                    ordenVisitado: visitadas.map((p) => p.ordenId),
                },
            });
        }
        const jorge = construir(JORGE, hoy, undefined);
        if (!esProblema(jorge)) {
            const oculta: Parada = {
                ...(jorge.paradas?.[0] as Parada),
                posicion: 1,
                ordenId: 0,
                numero: "",
                tipo: "",
                estado: "",
                oculta: true,
                direccion: "",
                lat: null,
                lon: null,
                citaInicio: null,
                llegadaEstimada: null,
                salidaEstimada: null,
                viajeSeg: 0,
                distanciaM: 0,
                enCaminoEn: null,
                llegadaReal: null,
                finalizadaEn: null,
                desvioSeg: null,
            };
            const paradas = [oculta, ...(jorge.paradas ?? []).map((p, i) => ({ ...p, posicion: i + 2 }))];
            rutas.push({
                ...jorge,
                id: siguiente++,
                motor: "haversine",
                degradada: true,
                motivoMotor: "sin_osrm",
                desactualizada: true,
                desactualizadaEn: jorge.propuestaEn,
                motivoDesactualizada: "OT-000040 cambió de hora después de proponer la ruta.",
                geometria: null,
                paradas,
                totalParadas: paradas.length,
            });
        }
    }

    async function manejar(c: ContextoRutas): Promise<boolean> {
        const { route, metodo, ruta, query, cuerpo, puede } = c;
        if (!ruta.startsWith("/api/v1/rutas")) return false;
        const sinPermiso = () => problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 }).then(() => true);
        const sinClave = () => problema(route, { type: "urn:iris:problema:validacion", title: "Falta Idempotency-Key", status: 422 }).then(() => true);

        if (metodo === "GET" && ruta === "/api/v1/rutas") {
            if (!puede("rutas.ver")) return sinPermiso();
            const fecha = query.get("fecha") ?? hoy;
            const tecnico = query.get("tecnicoId");
            const data = rutas
                .filter((r) => r.empresaId === c.empresaId && r.fecha === fecha && r.estado !== "reemplazada")
                .filter((r) => !tecnico || r.tecnicoId === Number(tecnico))
                .map(resumen);
            await json(route, { data, nextCursor: null } satisfies Schema<"PaginaRuta">);
            return true;
        }
        if (metodo === "POST" && ruta === "/api/v1/rutas/proponer") {
            if (!puede("rutas.proponer")) return sinPermiso();
            if (!c.idempotencia) return sinClave();
            const e = cuerpo as Schema<"EntradaProponerRuta">;
            const fecha = e.fecha ?? hoy;
            const previo = rutas.find((r) => r.tecnicoId === e.tecnicoId && r.fecha === fecha && r.estado === "borrador" && r.empresaId === c.empresaId);
            if (e.version !== undefined && previo && previo.version !== e.version) {
                await problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "El borrador cambió desde que lo leíste" });
                return true;
            }
            const nueva = construir(e.tecnicoId, fecha, e.ordenIds ?? undefined);
            if (esProblema(nueva)) {
                await problema(route, nueva);
                return true;
            }
            const detalle: RutaDetalle = { ...nueva, id: previo?.id ?? siguiente++, version: (previo?.version ?? 0) + 1 };
            if (previo) rutas.splice(rutas.indexOf(previo), 1, detalle);
            else rutas.push(detalle);
            await json(route, detalle);
            return true;
        }
        const m = /^\/api\/v1\/rutas\/(\d+)(\/publicar)?$/.exec(ruta);
        const r = m ? rutas.find((x) => x.id === Number(m[1]) && x.empresaId === c.empresaId) : undefined;
        if (m && !r) {
            await problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
            return true;
        }
        if (r && metodo === "GET" && !m?.[2]) {
            if (!puede("rutas.ver")) return sinPermiso();
            await json(route, r);
            return true;
        }
        if (r && metodo === "POST" && m?.[2]) {
            if (!puede("rutas.publicar")) return sinPermiso();
            if (!c.idempotencia) return sinClave();
            if ((cuerpo as Schema<"EntradaPublicarRuta"> | undefined)?.version !== r.version) {
                await problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "La ruta cambió desde que la leíste" });
                return true;
            }
            if (r.estado !== "borrador") {
                await problema(route, { type: "urn:iris:problema:transicion_invalida", title: "Transición inválida", status: 422, detail: "Solo se publica un borrador" });
                return true;
            }
            if (r.desactualizada) {
                await problema(route, { type: "urn:iris:problema:ruta_desactualizada", title: "Ruta desactualizada", status: 422, detail: "Vuelve a proponerla antes de publicar" });
                return true;
            }
            const ahoraIso = iso(Date.now());
            for (const x of rutas) {
                if (x.tecnicoId === r.tecnicoId && x.fecha === r.fecha && x.estado === "publicada") Object.assign(x, { estado: "reemplazada", reemplazadaEn: ahoraIso, reemplazadaPorId: r.id, version: x.version + 1 });
            }
            Object.assign(r, { estado: "publicada", publicadaEn: ahoraIso, publicadaPor: 1, version: r.version + 1 } satisfies Partial<RutaDetalle>);
            await json(route, r);
            return true;
        }
        return false;
    }

    return { rutas, manejar };
}
