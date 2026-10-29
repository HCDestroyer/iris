import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";

/*
 * Encuestas de satisfacción en la API falsa (e2e/api-falsa.ts), tipadas con packages/contratos y
 * fieles a internal/soporte/encuestas.go: lista con cursor y filtros (`encuestas.ver`), y la
 * página pública con el token en el cuerpo (404 inválido, 409 ya respondida con otra respuesta,
 * 410 vencida o anulada, 422 validación, 429 con `Retry-After`) y el enlace aparte
 * (`POST /encuestas/{id}/enlace`, `encuestas.compartir`; 409 por correo, 410, 422 sin clave). Las rutas públicas ignoran la sesión del panel; se anota
 * si llegaron cookies o `X-CSRF-Token` para comprobar que la página no los manda.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Encuesta = Schema<"Encuesta">;
type Publica = Schema<"EncuestaPublica">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema, cabeceras: Record<string, string> = {}) =>
    route.fulfill({ status: p.status, contentType: "application/problem+json", headers: cabeceras, body: JSON.stringify(p) });

/** Tokens del escenario (43 caracteres en base64url, como los de la API). */
const tok = (semilla: string) => semilla.padEnd(43, "x");
export const TOKENS = {
    pendiente: tok("PendienteTK000001"),
    respondida: tok("RespondidaTK000002"),
    vencida: tok("VencidaOT000003"),
    anulada: tok("AnuladaTK000006"),
    limite: tok("LimiteGlobal"),
    falla: tok("FallaServidor"),
} as const;

const base = "http://localhost/encuesta#";

function encuesta(d: Partial<Encuesta> & Pick<Encuesta, "id" | "entidad" | "numero" | "estado">): Encuesta {
    return {
        ticketId: null,
        ordenId: null,
        canal: "enlace",
        expiraEn: "2026-11-04T18:00:00Z",
        calificacion: null,
        comentario: null,
        respondidaEn: null,
        alerta: false,
        enlaceDisponible: true,
        creadaEn: "2026-10-28T18:00:00Z",
        ...d,
    };
}

function escenario(): Encuesta[] {
    const fijas: Encuesta[] = [
        encuesta({ id: 1, entidad: "ticket", ticketId: 1, numero: "TK-000101", estado: "pendiente", canal: "enlace", creadaEn: "2026-10-28T20:00:00Z" }),
        encuesta({
            id: 2,
            entidad: "orden",
            ordenId: 1,
            numero: "OT-000001",
            estado: "respondida",
            canal: "correo",
            calificacion: 2,
            alerta: true,
            comentario: "El técnico llegó tarde y no avisó.",
            respondidaEn: "2026-10-28T19:30:00Z",
            creadaEn: "2026-10-28T19:00:00Z",
        }),
        encuesta({ id: 3, entidad: "ticket", ticketId: 2, numero: "TK-000102", estado: "pendiente", canal: "correo", creadaEn: "2026-10-28T18:30:00Z" }),
        encuesta({ id: 4, entidad: "ticket", ticketId: 4, numero: "TK-000104", estado: "pendiente", canal: "enlace", enlaceDisponible: false, creadaEn: "2026-10-28T18:10:00Z" }),
        encuesta({ id: 5, entidad: "ticket", ticketId: 5, numero: "TK-000105", estado: "respondida", calificacion: 5, comentario: "Muy amables, gracias.", respondidaEn: "2026-10-27T15:00:00Z", creadaEn: "2026-10-27T14:00:00Z" }),
        encuesta({ id: 7, entidad: "ticket", ticketId: 6, numero: "TK-000106", estado: "anulada", creadaEn: "2026-10-16T14:00:00Z" }),
        encuesta({ id: 6, entidad: "orden", ordenId: 2, numero: "OT-000002", estado: "vencida", creadaEn: "2026-10-15T14:00:00Z", expiraEn: "2026-10-22T14:00:00Z" }),
    ];
    // Relleno para «Cargar más»: respondidas de días anteriores.
    const relleno = Array.from({ length: 30 }, (_, i) =>
        encuesta({
            id: 100 + i,
            entidad: i % 3 ? "ticket" : "orden",
            ...(i % 3 ? { ticketId: 200 + i } : { ordenId: 200 + i }),
            numero: i % 3 ? `TK-${String(200 + i).padStart(6, "0")}` : `OT-${String(200 + i).padStart(6, "0")}`,
            estado: "respondida",
            calificacion: (i % 5) + 1,
            alerta: (i % 5) + 1 <= 2,
            respondidaEn: new Date(Date.UTC(2026, 9, 20, 15) - i * 3600_000).toISOString(),
            creadaEn: new Date(Date.UTC(2026, 9, 20, 14) - i * 3600_000).toISOString(),
        }),
    );
    return [...fijas, ...relleno];
}

export type ApiEncuestas = {
    encuestas: Encuesta[];
    /** Peticiones a las rutas públicas que llegaron con cookie o `X-CSRF-Token` (deben ser 0). */
    publicasConSesion: string[];
    /** Encuestas cuyo enlace se pidió (`POST /encuestas/{id}/enlace`). */
    enlacesPedidos: number[];
    /** Respuestas recibidas en la página pública. */
    respuestas: { calificacion: number; comentario?: string }[];
    manejarPublica: (c: { route: Route; metodo: string; ruta: string; cabeceras: Record<string, string>; cuerpo: unknown }) => Promise<boolean>;
    manejar: (c: { route: Route; metodo: string; ruta: string; query: URLSearchParams; puede: (p: Permiso) => boolean }) => Promise<boolean>;
};

const PUBLICA = /^\/api\/v1\/encuestas\/publica\/(consultar|responder)$/;
const ENLACE = /^\/api\/v1\/encuestas\/(\d+)\/enlace$/;

export function apiEncuestas(): ApiEncuestas {
    const encuestas = escenario();
    const publicasConSesion: string[] = [];
    const respuestas: ApiEncuestas["respuestas"] = [];
    const enlacesPedidos: number[] = [];
    // Estado público por token.
    const publicas = new Map<string, Publica>([
        [TOKENS.pendiente, { entidad: "ticket", numero: "TK-000101", fecha: "2026-10-28T20:00:00Z", empresa: "Fibra Express", estado: "pendiente", expiraEn: "2026-11-04T20:00:00Z" }],
        [TOKENS.respondida, { entidad: "ticket", numero: "TK-000105", fecha: "2026-10-27T14:00:00Z", empresa: null, estado: "respondida", expiraEn: "2026-11-03T14:00:00Z", calificacion: 5 }],
        [TOKENS.vencida, { entidad: "orden", numero: "OT-000002", fecha: "2026-10-15T14:00:00Z", empresa: null, estado: "vencida", expiraEn: "2026-10-22T14:00:00Z" }],
    ]);

    return {
        encuestas,
        publicasConSesion,
        respuestas,
        enlacesPedidos,
        async manejarPublica({ route, metodo, ruta, cabeceras, cuerpo }) {
            const m = PUBLICA.exec(ruta);
            if (!m) return false;
            if (cabeceras.cookie || cabeceras["x-csrf-token"] || cabeceras["x-iris-empresa"]) publicasConSesion.push(`${metodo} ${ruta}`);
            if (metodo !== "POST") return false;
            const token = typeof (cuerpo as { token?: unknown } | undefined)?.token === "string" ? (cuerpo as { token: string }).token : "";
            if (token === TOKENS.limite) {
                await problema(route, { type: "urn:iris:problema:demasiados_intentos", title: "Demasiados intentos", status: 429 }, { "Retry-After": "120" });
                return true;
            }
            if (token === TOKENS.falla) {
                await problema(route, { type: "urn:iris:problema:error_interno", title: "Error interno", status: 500 });
                return true;
            }
            if (token === TOKENS.anulada) {
                await problema(route, { type: "urn:iris:problema:encuesta_anulada", title: "Encuesta anulada", status: 410 });
                return true;
            }
            const p = publicas.get(token);
            if (!p) {
                await problema(route, { type: "urn:iris:problema:encuesta_no_encontrada", title: "Encuesta no encontrada", status: 404 });
                return true;
            }
            if (m[1] === "consultar") {
                await json(route, p);
                return true;
            }
            const c = (cuerpo ?? {}) as { calificacion?: unknown; comentario?: unknown };
            const calificacion = typeof c.calificacion === "number" ? c.calificacion : NaN;
            const comentario = typeof c.comentario === "string" ? c.comentario : undefined;
            if (!Number.isInteger(calificacion) || calificacion < 1 || calificacion > 5 || (comentario?.length ?? 0) > 1000) {
                await problema(route, {
                    type: "urn:iris:problema:validacion",
                    title: "Datos inválidos",
                    status: 422,
                    errors: [{ field: "body.calificacion", message: "La calificación va de 1 a 5" }],
                });
                return true;
            }
            if (p.estado === "vencida") {
                await problema(route, { type: "urn:iris:problema:encuesta_vencida", title: "Encuesta vencida", status: 410 });
                return true;
            }
            if (p.estado === "respondida") {
                if (p.calificacion === calificacion) {
                    await json(route, p);
                } else {
                    await problema(route, { type: "urn:iris:problema:encuesta_respondida", title: "Encuesta ya respondida", status: 409 });
                }
                return true;
            }
            respuestas.push({ calificacion, ...(comentario ? { comentario } : {}) });
            p.estado = "respondida";
            p.calificacion = calificacion;
            const e = encuestas.find((x) => x.numero === p.numero);
            if (e) {
                e.estado = "respondida";
                e.calificacion = calificacion;
                e.comentario = comentario ?? null;
                e.alerta = calificacion <= 2;
                e.respondidaEn = new Date().toISOString();
            }
            await json(route, p);
            return true;
        },
        async manejar({ route, metodo, ruta, query, puede }) {
            const enlace = ENLACE.exec(ruta);
            if (enlace && metodo === "POST") {
                if (!puede("encuestas.compartir")) {
                    await problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
                    return true;
                }
                const e = encuestas.find((x) => x.id === Number(enlace[1]));
                const p = e ? [...publicas.entries()].find(([, v]) => v.numero === e.numero) : undefined;
                if (!e) await problema(route, { type: "urn:iris:problema:no_encontrado", title: "No encontrado", status: 404 });
                else if (e.canal === "correo") await problema(route, { type: "urn:iris:problema:encuesta_por_correo", title: "Enviada por correo", status: 409 });
                else if (e.estado === "respondida") await problema(route, { type: "urn:iris:problema:encuesta_respondida", title: "Encuesta ya respondida", status: 409 });
                else if (e.estado === "vencida") await problema(route, { type: "urn:iris:problema:encuesta_vencida", title: "Encuesta vencida", status: 410 });
                else if (e.estado === "anulada") await problema(route, { type: "urn:iris:problema:encuesta_anulada", title: "Encuesta anulada", status: 410 });
                else if (!e.enlaceDisponible || !p) await problema(route, { type: "urn:iris:problema:enlace_no_disponible", title: "Enlace no disponible", status: 422 });
                else {
                    enlacesPedidos.push(e.id);
                    await route.fulfill({ status: 200, contentType: "application/json", headers: { "Cache-Control": "no-store" }, body: JSON.stringify({ enlace: `${base}${p[0]}`, expiraEn: e.expiraEn } satisfies Schema<"EnlaceEncuesta">) });
                }
                return true;
            }
            if (ruta !== "/api/v1/encuestas" || metodo !== "GET") return false;
            if (!puede("encuestas.ver")) {
                await problema(route, { type: "urn:iris:problema:prohibido", title: "Sin permiso", status: 403 });
                return true;
            }
            if (query.getAll("calificacion").length > 1) {
                await problema(route, { type: "urn:iris:problema:validacion", title: "Datos inválidos", status: 422, detail: "calificacion repetida" });
                return true;
            }
            const calif = query.get("calificacion")?.split(",").map(Number);
            const desde = query.get("desde");
            const hasta = query.get("hasta");
            const dia = (iso: string) => new Date(Date.parse(iso) - 6 * 3600_000).toISOString().slice(0, 10);
            const lista = encuestas
                .filter((e) => !query.get("ticketId") || e.ticketId === Number(query.get("ticketId")))
                .filter((e) => !query.get("ordenId") || e.ordenId === Number(query.get("ordenId")))
                .filter((e) => !query.get("estado") || e.estado === query.get("estado"))
                .filter((e) => !query.get("entidad") || e.entidad === query.get("entidad"))
                .filter((e) => !calif || (e.calificacion !== null && calif.includes(e.calificacion)))
                .filter((e) => !desde || dia(e.creadaEn) >= desde)
                .filter((e) => !hasta || dia(e.creadaEn) <= hasta)
                .sort((a, b) => b.creadaEn.localeCompare(a.creadaEn));
            const limite = Math.min(200, Math.max(1, Number(query.get("limit") ?? 25)));
            const inicio = Number(query.get("cursor") ?? 0);
            await json(route, {
                data: lista.slice(inicio, inicio + limite),
                nextCursor: inicio + limite < lista.length ? String(inicio + limite) : null,
            } satisfies Schema<"PaginaEncuesta">);
            return true;
        },
    };
}

export const REPORTE_SATISFACCION = (desde: string, hasta: string, entidad: string | null): Schema<"ReporteSatisfaccion"> => {
    const fila = (e: "ticket" | "orden" | undefined, k: number): Schema<"FilaSatisfaccion"> => ({
        ...(e ? { entidad: e } : {}),
        enviadas: 50 * k,
        respondidas: 30 * k,
        pendientes: 12 * k,
        vencidas: 8 * k,
        porcentajeRespondidas: 60,
        promedio: 4.1,
        alertas: 4 * k,
        distribucion: [
            { calificacion: 5, cantidad: 14 * k },
            { calificacion: 4, cantidad: 9 * k },
            { calificacion: 3, cantidad: 3 * k },
            { calificacion: 2, cantidad: 3 * k },
            { calificacion: 1, cantidad: 1 * k },
        ],
    });
    const porEntidad = [fila("ticket", 1), fila("orden", 1)].filter((f) => !entidad || f.entidad === entidad);
    return {
        periodo: { desde, hasta, inicio: `${desde}T06:00:00Z`, fin: `${hasta}T06:00:00Z`, zonaHoraria: "America/Guatemala" },
        total: fila(undefined, entidad ? 1 : 2),
        porEntidad,
    };
};
