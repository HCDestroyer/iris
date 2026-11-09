import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import type { AjustesAsistente, EstadoAsistente, Sugerencia, UsoAsistente } from "../lib/asistente/tipos";

/*
 * Asistente de IA en la API falsa (contratos/conversaciones.md §16): estado, ajustes (con
 * `version` y 409), probar, modelos, uso, sugerencias (pedir, «Otra», usar, descartar, calificar)
 * con sus cambios por el feed (`tipo: "sugerencia"`), `sugerenciaId` al enviar y el resumen.
 */

type Problema = Schema<"Problema">;
type Permiso = Schema<"Permiso">;
type Conversacion = Schema<"ConversacionCliente">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

export const PERMISO_SUGERENCIAS: Permiso = "conversaciones.sugerencias";
export const PERMISO_CONFIGURAR: Permiso = "mensajeria.asistente_configurar";

export const TEXTO_SUGERIDO =
    "Buen día, don Carlos. Hay una falla en la red de su sector (Zona 4) desde las 7:40 a. m. y nuestro equipo ya está trabajando en ella. No necesita reiniciar su equipo. Le avisamos por aquí en cuanto se restablezca.";
export const TEXTO_OTRA = "Hola, Carlos. Ya vimos la falla en su zona y el equipo técnico la está atendiendo. Le escribimos apenas vuelva el servicio.";

const FUENTES: Sugerencia["fuentes"] = [
    { tipo: "horus", etiqueta: "Falla en Zona 4 · Horus", entidadId: "router:1:77" },
    { tipo: "argos", etiqueta: "Servicio activo, al día · Argos", entidadId: "41010" },
    { tipo: "manual", etiqueta: "Manual «Sin internet (FTTH)»", entidadId: "2" },
];
const ACCION = { tipo: "etiquetar", etiquetaId: 6, etiqueta: "Falla masiva" } as const;

export type OpcionesAsistente = {
    estado?: Partial<EstadoAsistente>;
    /** Sugerencia vigente de la conversación 41 al abrir (por defecto, `lista`); `null`, ninguna. */
    inicial?: "lista" | "generando" | "error" | "tope" | null;
    /** Cómo termina lo que se pide («Sugerir», «Otra», «Reintentar»). */
    alPedir?: "lista" | "error";
    /** `POST …/sugerencias` responde este 422. */
    pedir422?: "tope_alcanzado" | "asistente_inactivo";
    probar?: "ok" | "error";
    modelosFalla?: boolean;
    /** El primer guardado responde 409 (alguien más cambió los ajustes). */
    conflicto?: boolean;
    ajustes?: Partial<AjustesAsistente>;
    resumen?: "ok" | "error";
};

type Contexto = {
    conversaciones: Conversacion[];
    /** Publica el evento `sugerencia` en el feed. */
    emitir: (s: Sugerencia) => void;
    enviados: { ruta: string; cuerpo: unknown; idempotencia: string | undefined }[];
};

const BASE = "/api/v1/mensajeria";
const ahora = () => new Date().toISOString();

/** Sin `opciones` el asistente está apagado y sin sugerencias (las demás pruebas de la bandeja no lo ven). */
export function apiAsistente(e: Contexto, opcionesDadas?: OpcionesAsistente) {
    const opciones: OpcionesAsistente = opcionesDadas ?? { estado: { activo: false }, inicial: null };
    const estado: EstadoAsistente = { activo: true, modo: "automatico", pausado: false, ...opciones.estado };
    let ajustes: AjustesAsistente = {
        activo: estado.activo,
        proveedor: "ollama",
        url: "http://10.0.0.20:11434",
        claveGuardadaEn: null,
        modelo: "qwen3:32b",
        modo: estado.modo,
        fuentes: { manuales: true, argos: true, horus: true, historial: true },
        topeTokensMes: 5_000_000,
        precioEntradaMillon: null,
        precioSalidaMillon: null,
        version: 2,
        ...opciones.ajustes,
    };
    let conflicto = opciones.conflicto ?? false;
    const sugerencias: Sugerencia[] = [];
    const guardados: unknown[] = [];
    const pruebas: unknown[] = [];
    let n = 0;

    function nueva(conversacionId: number, datos: Partial<Sugerencia>): Sugerencia {
        n += 1;
        const t = ahora();
        const s: Sugerencia = {
            id: `0193c000-0000-7000-8000-${String(n).padStart(12, "0")}`,
            conversacionId,
            paraMensajeId: "0193b000-0000-7000-8000-000000000001",
            estado: "lista",
            texto: TEXTO_SUGERIDO,
            fuentes: structuredClone(FUENTES),
            accion: { ...ACCION },
            calificacion: null,
            origen: "automatico",
            creadaEn: t,
            actualizadaEn: t,
            ...datos,
        };
        sugerencias.push(s);
        return s;
    }

    const vigente = (id: number) => sugerencias.find((s) => s.conversacionId === id && ["generando", "lista", "error"].includes(s.estado));
    function cambiar(s: Sugerencia, datos: Partial<Sugerencia>) {
        Object.assign(s, datos, { actualizadaEn: new Date(Math.max(Date.now(), Date.parse(s.actualizadaEn) + 1)).toISOString() });
        e.emitir(structuredClone(s));
    }

    const ERROR_PROVEEDOR: Schema<"ErrorSugerencia"> = { codigo: "proveedor_no_responde", detalle: "El servidor de IA no respondió a tiempo" };
    const inicial = opciones.inicial === undefined ? "lista" : opciones.inicial;
    if (inicial === "lista") nueva(41, {});
    else if (inicial === "generando") nueva(41, { estado: "generando", texto: null, fuentes: [] });
    else if (inicial === "error" || inicial === "tope") {
        const s = nueva(41, { estado: "error", texto: null, fuentes: [], error: inicial === "tope" ? { codigo: "tope_alcanzado", detalle: "Se llegó al tope del mes" } : ERROR_PROVEEDOR });
        delete s.accion;
    }

    /** Termina de generar (como el job de River) y lo publica. */
    function terminar(s: Sugerencia, otra: boolean) {
        setTimeout(() => {
            if (s.estado !== "generando") return;
            if ((opciones.alPedir ?? "lista") === "error") cambiar(s, { estado: "error", error: ERROR_PROVEEDOR });
            else cambiar(s, { estado: "lista", texto: otra ? TEXTO_OTRA : TEXTO_SUGERIDO, fuentes: structuredClone(FUENTES), accion: { ...ACCION } });
        }, 600);
    }

    /** Llegó un mensaje del cliente: la vigente vence y, en automático, llega otra. */
    function alMensajeCliente(conversacionId: number) {
        const v = vigente(conversacionId);
        if (v) cambiar(v, { estado: "vencida" });
        if (estado.activo && !estado.pausado && estado.modo === "automatico") {
            const s = nueva(conversacionId, { estado: "generando", texto: null, fuentes: [], origen: "automatico" });
            delete s.accion;
            e.emitir(structuredClone(s));
            terminar(s, true);
        }
    }

    /** `POST …/mensajes` con `sugerenciaId`: 422 si no está `lista` ni `usada`; si sí, queda `usada`. */
    function alResponder(conversacionId: number, sugerenciaId: unknown): Problema | null {
        if (typeof sugerenciaId !== "string") return null;
        const s = sugerencias.find((x) => x.id === sugerenciaId);
        if (!s || s.conversacionId !== conversacionId || !["lista", "usada"].includes(s.estado)) return P("sugerencia_no_vigente", 422, "La sugerencia ya no está vigente");
        if (s.estado !== "usada") cambiar(s, { estado: "usada" });
        return null;
    }

    function uso(): UsoAsistente {
        return {
            mes: "2026-10",
            tokensEntrada: 1_700_000,
            tokensSalida: 400_000,
            sugerencias: 1530,
            usadas: 1040,
            utiles: 610,
            noUtiles: 85,
            resumenes: 212,
            costo: ajustes.precioEntradaMillon && ajustes.precioSalidaMillon ? "12.40" : null,
            topeTokensMes: ajustes.topeTokensMes,
            porcentaje: Math.round((2_100_000 / ajustes.topeTokensMes) * 100),
            pausado: estado.pausado,
        };
    }

    async function manejar(x: { route: Route; metodo: string; ruta: string; query: URLSearchParams; cuerpo: Record<string, unknown>; idempotencia: string | undefined; puede: (p: Permiso) => boolean }): Promise<boolean> {
        const { route, metodo, ruta, cuerpo, puede, idempotencia } = x;
        const prohibido = async () => (await problema(route, P("permiso_denegado", 403, "Sin permiso")), true);

        if (ruta === `${BASE}/asistente/estado` && metodo === "GET") {
            if (!puede(PERMISO_SUGERENCIAS)) return prohibido();
            return (await json(route, estado), true);
        }
        if (ruta.startsWith(`${BASE}/asistente/`)) {
            if (!puede(PERMISO_CONFIGURAR)) return prohibido();
            if (ruta === `${BASE}/asistente/ajustes` && metodo === "GET") return (await json(route, ajustes), true);
            if (ruta === `${BASE}/asistente/ajustes/actualizar` && metodo === "POST") {
                e.enviados.push({ ruta, cuerpo, idempotencia });
                guardados.push(cuerpo);
                if (conflicto) {
                    conflicto = false;
                    return (await problema(route, P("conflicto_version", 409, "Alguien más cambió los ajustes")), true);
                }
                if (cuerpo.version !== ajustes.version) return (await problema(route, P("conflicto_version", 409, "Cambió")), true);
                if (typeof cuerpo.url === "string" && cuerpo.url.includes("169.254.")) {
                    return (await problema(route, P("url_no_permitida", 422, "Dirección no permitida", { errors: [{ field: "body.url", message: "Esa dirección no está permitida" }] })), true);
                }
                const { clave, ...resto } = cuerpo as Record<string, unknown> & { clave?: string };
                ajustes = {
                    ...ajustes,
                    ...(resto as Partial<AjustesAsistente>),
                    claveGuardadaEn: clave === "" ? null : clave ? "2026-10-28T16:45:00Z" : ajustes.claveGuardadaEn,
                    version: ajustes.version + 1,
                };
                estado.activo = ajustes.activo;
                estado.modo = ajustes.modo;
                return (await json(route, ajustes), true);
            }
            if (ruta === `${BASE}/asistente/probar` && metodo === "POST") {
                pruebas.push(cuerpo);
                const ok = (opciones.probar ?? "ok") === "ok";
                return (await json(route, ok ? { estado: "ok", latenciaMs: 1840, tokensPorSegundo: 37.3, detalle: "" } : { estado: "error", latenciaMs: 30000, tokensPorSegundo: null, detalle: "El servidor no respondió a tiempo" }), true);
            }
            if (ruta === `${BASE}/asistente/modelos` && metodo === "GET") {
                if (opciones.modelosFalla) return (await problema(route, P("proveedor_no_responde", 502, "El proveedor no respondió", { detail: "El servidor de IA no respondió a tiempo" })), true);
                return (await json(route, { data: [{ id: "qwen3:32b", nombre: "qwen3:32b" }, { id: "llama3.1:8b", nombre: "llama3.1:8b" }, { id: "gemma3:27b", nombre: "gemma3:27b" }] }), true);
            }
            if (ruta === `${BASE}/asistente/uso` && metodo === "GET") return (await json(route, uso()), true);
            return false;
        }

        const accion = /^\/api\/v1\/mensajeria\/sugerencias\/([^/]+)\/(usar|descartar|calificar)$/.exec(ruta);
        if (accion && metodo === "POST") {
            if (!puede(PERMISO_SUGERENCIAS)) return prohibido();
            e.enviados.push({ ruta, cuerpo, idempotencia });
            const s = sugerencias.find((v) => v.id === accion[1]);
            if (!s) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
            if (accion[2] === "usar") {
                if (s.estado !== "lista" && s.estado !== "usada") return (await problema(route, P("sugerencia_no_vigente", 422, "La sugerencia ya no está vigente")), true);
                if (s.estado === "lista") cambiar(s, { estado: "usada" });
            } else if (accion[2] === "descartar") {
                if (["generando", "lista", "error"].includes(s.estado)) cambiar(s, { estado: "descartada" });
            } else {
                cambiar(s, { calificacion: cuerpo.util ? "util" : "no_util" });
            }
            return (await json(route, s), true);
        }

        const conv = /^\/api\/v1\/mensajeria\/conversaciones\/(\d+)\/(sugerencias|sugerencias\/actual|resumen)$/.exec(ruta);
        if (!conv) return false;
        if (!puede(PERMISO_SUGERENCIAS)) return prohibido();
        const id = Number(conv[1]);
        if (!e.conversaciones.some((c) => c.id === id)) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
        if (conv[2] === "sugerencias/actual" && metodo === "GET") {
            const v = vigente(id);
            return v ? (await json(route, v), true) : (await problema(route, P("sin_sugerencia", 404, "No hay sugerencia vigente")), true);
        }
        if (conv[2] === "sugerencias" && metodo === "POST") {
            e.enviados.push({ ruta, cuerpo, idempotencia });
            if (!estado.activo) return (await problema(route, P("asistente_inactivo", 422, "El asistente no está activo")), true);
            if (opciones.pedir422 || estado.pausado) {
                const codigo = opciones.pedir422 ?? "tope_alcanzado";
                return (await problema(route, P(codigo, 422, codigo === "tope_alcanzado" ? "Se llegó al tope del mes" : "El asistente no está activo")), true);
            }
            const v = vigente(id);
            if (v) cambiar(v, { estado: "descartada" });
            const s = nueva(id, { estado: "generando", texto: null, fuentes: [], origen: cuerpo.otra ? "otra" : "boton" });
            delete s.accion;
            terminar(s, Boolean(cuerpo.otra));
            return (await json(route, s, 202), true);
        }
        if (conv[2] === "resumen" && metodo === "POST") {
            e.enviados.push({ ruta, cuerpo, idempotencia });
            await new Promise((r) => setTimeout(r, 300));
            if (opciones.resumen === "error") return (await problema(route, P("proveedor_no_responde", 502, "El proveedor no respondió", { detail: "El servidor de IA no respondió a tiempo" })), true);
            return (await json(route, { texto: "Cliente sin internet desde la mañana; ya reinició el módem. Hay una falla abierta en su zona. Primera vez que escribe este mes.", generadoEn: ahora() }), true);
        }
        return false;
    }

    return {
        manejar,
        alMensajeCliente,
        alResponder,
        sugerencias,
        guardados,
        pruebas,
        estado,
        get ajustes() {
            return ajustes;
        },
    };
}

export type ApiAsistente = ReturnType<typeof apiAsistente>;
