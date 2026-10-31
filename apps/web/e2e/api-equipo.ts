import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { CUADRILLAS, TECNICOS, ZONAS, type EscenarioAgenda } from "./datos-agenda";

/*
 * Técnicos (fichas, turnos y ausencias), cuadrillas y zonas en la API falsa, con estado propio
 * por prueba (copias de los fixtures de datos-agenda). Reproduce lo que el panel usa de
 * apps/api/internal/agenda: alta con candidatos, edición que reemplaza la ficha, activar y
 * desactivar con `version` (409 si cambió), turnos sin cruces y ausencias que devuelven las citas
 * en conflicto. Las ausencias se suman al escenario de la agenda (las ve el calendario).
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type TecnicoDetalle = Schema<"TecnicoDetalle">;
type Cuadrilla = Schema<"Cuadrilla">;
type Zona = Schema<"Zona">;
type Ausencia = Schema<"Ausencia">;
type OrdenDetalle = Schema<"OrdenDetalle">;

const ahora = "2026-10-28T15:00:00Z";
const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const prohibido = (route: Route) => problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 }).then(() => true);
const noEncontrado = (route: Route) => problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 }).then(() => true);
const conflicto = (route: Route) =>
    problema(route, { type: "urn:iris:problema:conflicto_version", title: "Conflicto de versión", status: 409, detail: "Cambió desde que lo leíste" }).then(() => true);
const validacion = (route: Route, field: string, message: string) =>
    problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, errors: [{ field: `body.${field}`, message }] }).then(() => true);

/** Usuarios con el rol de técnico aún sin ficha. */
export const CANDIDATOS: Schema<"CandidatoTecnico">[] = [
    { usuarioId: 14, nombre: "Pablo Xol", correo: "pablo@iris.test" },
    { usuarioId: 15, nombre: "Marta Cux", correo: "marta@iris.test" },
];

export type ApiEquipo = {
    tecnicos: TecnicoDetalle[];
    cuadrillas: Cuadrilla[];
    zonas: Zona[];
    /** Usuarios sin ficha que `GET /tecnicos/candidatos` ofrece (se pueden agregar en la prueba). */
    candidatos: Schema<"CandidatoTecnico">[];
    /** Otra persona cambió la ficha: sube su versión (la próxima acción con la vieja da 409). */
    cambiarVersionTecnico: (fichaId: number) => void;
    manejar: (c: { route: Route; metodo: string; ruta: string; query: URLSearchParams; cuerpo: unknown; puede: (p: Permiso) => boolean }) => Promise<boolean>;
};

export function apiEquipo(agenda: EscenarioAgenda, ordenes: OrdenDetalle[]): ApiEquipo {
    const zonas: Zona[] = ZONAS.map((z) => ({ ...z }));
    const cuadrillas: Cuadrilla[] = CUADRILLAS.map((c) => ({ ...c, zonaIds: [...(c.zonaIds ?? [])] }));
    const turnosBase = [1, 2, 3, 4, 5, 6, 7].map((dia) => ({ dia, inicio: "07:00", fin: "17:00" }));
    const tecnicos: TecnicoDetalle[] = TECNICOS.map((t) => ({
        ...t,
        habilidades: [...(t.habilidades ?? [])],
        turnos: turnosBase.map((x) => ({ ...x })),
        capacidad: [],
        ausencias: agenda.ausencias
            .filter((a) => a.tecnicoId === t.usuarioId)
            .map((a) => ({ id: a.id, tecnicoId: t.id, inicio: a.inicio, fin: a.fin, motivo: a.motivo as Ausencia["motivo"], detalle: "", anuladaEn: null, version: 1, creadoEn: ahora })),
    }));
    const candidatos = CANDIDATOS.map((c) => ({ ...c }));
    let siguiente = 900;

    const tocar = <T extends { version: number; actualizadoEn: string }>(x: T) => {
        x.version++;
        x.actualizadoEn = new Date().toISOString();
    };

    async function manejar({ route, metodo, ruta, query, cuerpo, puede }: Parameters<ApiEquipo["manejar"]>[0]): Promise<boolean> {
        const b = (cuerpo ?? {}) as Record<string, unknown>;
        const partes = ruta.split("/").slice(3); // ["tecnicos", "21", "editar"]
        const [recurso, idTexto, accion, subId, subAccion] = partes;
        const id = Number(idTexto);

        // --- Zonas -----------------------------------------------------------------------
        if (recurso === "zonas") {
            if (metodo === "GET" && partes.length === 1) {
                if (!puede("agenda.ver")) return prohibido(route);
                await json(route, { data: zonas.filter((z) => query.get("todas") === "true" || z.activa), nextCursor: null } satisfies Schema<"PaginaZona">);
                return true;
            }
            if (metodo !== "POST") return false;
            if (partes.length === 1) {
                if (!puede("agenda.crear_zona")) return prohibido(route);
                const nombre = String(b.nombre ?? "").trim();
                if (zonas.some((z) => z.nombre.toLowerCase() === nombre.toLowerCase())) return validacion(route, "nombre", "Ya hay una zona con ese nombre");
                const z: Zona = { id: 1_000_000_000_000 + siguiente++, nombre, origen: "local", activa: true, empresaId: null, poligono: null, sincronizadoEn: null, version: 1, creadoEn: ahora, actualizadoEn: ahora };
                zonas.push(z);
                await json(route, z, 201);
                return true;
            }
            const z = zonas.find((x) => x.id === id);
            if (!z) return noEncontrado(route);
            if (b.version !== z.version) return conflicto(route);
            if (accion === "editar") {
                if (!puede("agenda.editar_zona")) return prohibido(route);
                z.nombre = String(b.nombre ?? z.nombre).trim();
            } else if (accion === "activar" || accion === "desactivar") {
                if (!puede(accion === "activar" ? "agenda.activar_zona" : "agenda.desactivar_zona")) return prohibido(route);
                z.activa = accion === "activar";
            } else return false;
            tocar(z);
            await json(route, z);
            return true;
        }

        // --- Cuadrillas ------------------------------------------------------------------
        if (recurso === "cuadrillas") {
            if (metodo === "GET" && partes.length === 1) {
                if (!puede("agenda.ver")) return prohibido(route);
                await json(route, { data: cuadrillas.filter((c) => query.get("todas") === "true" || c.activa), nextCursor: null } satisfies Schema<"PaginaCuadrilla">);
                return true;
            }
            if (metodo !== "POST") return false;
            const zonaIds = Array.isArray(b.zonaIds) ? (b.zonaIds as number[]) : [];
            if (zonaIds.some((zid) => !zonas.some((z) => z.id === zid))) return validacion(route, "zonaIds", "Zona inexistente");
            if (partes.length === 1) {
                if (!puede("agenda.crear_cuadrilla")) return prohibido(route);
                const nombre = String(b.nombre ?? "").trim();
                if (cuadrillas.some((c) => c.nombre.toLowerCase() === nombre.toLowerCase())) return validacion(route, "nombre", "Ya hay una cuadrilla con ese nombre");
                const c: Cuadrilla = { id: siguiente++, nombre, activa: true, empresaId: null, zonaIds, capacidad: [], version: 1, creadoEn: ahora, actualizadoEn: ahora };
                cuadrillas.push(c);
                await json(route, c, 201);
                return true;
            }
            const c = cuadrillas.find((x) => x.id === id);
            if (!c) return noEncontrado(route);
            if (b.version !== c.version) return conflicto(route);
            if (accion === "editar") {
                if (!puede("agenda.editar_cuadrilla")) return prohibido(route);
                c.nombre = String(b.nombre ?? c.nombre).trim();
                c.zonaIds = zonaIds;
            } else if (accion === "activar" || accion === "desactivar") {
                if (!puede(accion === "activar" ? "agenda.activar_cuadrilla" : "agenda.desactivar_cuadrilla")) return prohibido(route);
                c.activa = accion === "activar";
            } else return false;
            tocar(c);
            await json(route, c);
            return true;
        }

        if (recurso !== "tecnicos") return false;

        // --- Técnicos: lecturas ----------------------------------------------------------
        if (metodo === "GET") {
            if (!puede("agenda.ver")) return prohibido(route);
            if (partes.length === 1) {
                const num = (k: string) => (query.get(k) ? Number(query.get(k)) : undefined);
                const cuadrilla = num("cuadrillaId");
                const zona = num("zonaId");
                const usuario = num("usuarioId");
                const activo = query.get("activo");
                const q = (query.get("q") ?? "").toLowerCase();
                const data = tecnicos
                    .filter((t) => cuadrilla === undefined || t.cuadrillaId === cuadrilla)
                    .filter((t) => zona === undefined || (cuadrillas.find((c) => c.id === t.cuadrillaId)?.zonaIds ?? []).includes(zona))
                    .filter((t) => usuario === undefined || t.usuarioId === usuario)
                    .filter((t) => !activo || String(t.activo) === activo)
                    .filter((t) => !q || t.nombre.toLowerCase().includes(q) || t.correo.toLowerCase().includes(q));
                await json(route, { data, nextCursor: null } satisfies Schema<"PaginaTecnico">);
                return true;
            }
            if (idTexto === "candidatos") {
                if (!puede("agenda.crear_tecnico")) return prohibido(route);
                const q = (query.get("q") ?? "").toLowerCase();
                const data = candidatos.filter((c) => !q || c.nombre.toLowerCase().includes(q) || c.correo.toLowerCase().includes(q));
                await json(route, { data, nextCursor: null } satisfies Schema<"PaginaCandidatoTecnico">);
                return true;
            }
            if (idTexto === "asignables") return false;
            const t = tecnicos.find((x) => x.id === id);
            if (!t || partes.length !== 2) return noEncontrado(route);
            await json(route, { ...t, ausencias: (t.ausencias ?? []).filter((a) => a.anuladaEn === null) });
            return true;
        }
        if (metodo !== "POST") return false;

        // --- Técnicos: alta --------------------------------------------------------------
        if (partes.length === 1) {
            if (!puede("agenda.crear_tecnico")) return prohibido(route);
            const c = candidatos.find((x) => x.usuarioId === b.usuarioId);
            if (!c) return validacion(route, "usuarioId", "El usuario no existe o ya tiene ficha");
            if (typeof b.telefono === "string" && (b.telefono.match(/\d/g) ?? []).length < 7) return validacion(route, "telefono", "El teléfono necesita al menos 7 dígitos");
            const t: TecnicoDetalle = {
                id: siguiente++,
                usuarioId: c.usuarioId,
                nombre: c.nombre,
                correo: c.correo,
                cuadrillaId: typeof b.cuadrillaId === "number" ? b.cuadrillaId : null,
                bodegaId: typeof b.bodegaId === "number" ? b.bodegaId : null,
                habilidades: Array.isArray(b.habilidades) ? (b.habilidades as string[]) : [],
                baseLat: null,
                baseLon: null,
                telefono: typeof b.telefono === "string" ? b.telefono : "",
                activo: true,
                version: 1,
                creadoEn: ahora,
                actualizadoEn: ahora,
                turnos: [],
                capacidad: [],
                ausencias: [],
            };
            tecnicos.push(t);
            candidatos.splice(candidatos.indexOf(c), 1);
            await json(route, t, 201);
            return true;
        }

        const t = tecnicos.find((x) => x.id === id);
        if (!t) return noEncontrado(route);

        // --- Ausencias -------------------------------------------------------------------
        if (accion === "ausencias" && subId === undefined) {
            if (!puede("agenda.registrar_ausencia")) return prohibido(route);
            const inicio = String(b.inicio ?? "");
            const fin = String(b.fin ?? "");
            if (!(Date.parse(fin) > Date.parse(inicio))) return validacion(route, "fin", "El fin debe ser posterior al inicio");
            const a: Ausencia = { id: siguiente++, tecnicoId: t.id, inicio, fin, motivo: b.motivo as Ausencia["motivo"], detalle: String(b.detalle ?? ""), anuladaEn: null, version: 1, creadoEn: ahora };
            t.ausencias = [...(t.ausencias ?? []), a];
            agenda.ausencias.push({ id: a.id, tecnicoId: t.usuarioId, fichaId: t.id, inicio, fin, motivo: a.motivo });
            const vivas: readonly OrdenDetalle["estado"][] = ["programada", "en_camino", "en_sitio", "pausada"];
            const citasEnConflicto = ordenes
                .filter((o) => o.tecnicoId === t.usuarioId && vivas.includes(o.estado) && o.programadaInicio && o.programadaFin)
                .filter((o) => Date.parse(o.programadaInicio ?? "") < Date.parse(fin) && Date.parse(o.programadaFin ?? "") > Date.parse(inicio))
                .map((o) => ({ ordenId: o.id, numero: o.numero, estado: o.estado, inicio: o.programadaInicio ?? "", fin: o.programadaFin ?? "" }));
            await json(route, { ausencia: a, citasEnConflicto } satisfies Schema<"RespuestaAusencia">, 201);
            return true;
        }
        if (accion === "ausencias" && subAccion === "anular") {
            if (!puede("agenda.anular_ausencia")) return prohibido(route);
            const a = (t.ausencias ?? []).find((x) => x.id === Number(subId));
            if (!a) return noEncontrado(route);
            if (b.version !== a.version) return conflicto(route);
            a.anuladaEn = new Date().toISOString();
            a.version++;
            const i = agenda.ausencias.findIndex((x) => x.id === a.id);
            if (i >= 0) agenda.ausencias.splice(i, 1);
            await json(route, a);
            return true;
        }

        // --- Ficha -----------------------------------------------------------------------
        if (b.version !== t.version) return conflicto(route);
        if (accion === "editar") {
            if (!puede("agenda.editar_tecnico")) return prohibido(route);
            t.cuadrillaId = typeof b.cuadrillaId === "number" ? b.cuadrillaId : null;
            t.bodegaId = typeof b.bodegaId === "number" ? b.bodegaId : null;
            t.habilidades = Array.isArray(b.habilidades) ? (b.habilidades as string[]) : [];
            t.telefono = typeof b.telefono === "string" ? b.telefono : "";
            t.baseLat = typeof b.baseLat === "number" ? b.baseLat : null;
            t.baseLon = typeof b.baseLon === "number" ? b.baseLon : null;
        } else if (accion === "activar" || accion === "desactivar") {
            if (!puede(accion === "activar" ? "agenda.activar_tecnico" : "agenda.desactivar_tecnico")) return prohibido(route);
            t.activo = accion === "activar";
        } else if (accion === "turnos") {
            if (!puede("agenda.fijar_turnos")) return prohibido(route);
            t.turnos = Array.isArray(b.turnos) ? (b.turnos as Schema<"Turno">[]) : [];
        } else return false;
        tocar(t);
        await json(route, t);
        return true;
    }

    return {
        tecnicos,
        cuadrillas,
        zonas,
        candidatos,
        cambiarVersionTecnico: (fichaId) => {
            const t = tecnicos.find((x) => x.id === fichaId);
            if (t) t.version++;
        },
        manejar,
    };
}
