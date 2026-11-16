import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { PERFIL_POR_DEFECTO, type Categoria, type EfectoAsistente, type PerfilAsistente, type PruebaMensaje } from "../lib/asistente/fase3";
import type { AjustesAsistente } from "../lib/asistente/tipos";
import type { Conversacion, Mensaje } from "../lib/conversaciones/tipos";

/*
 * Fase 3 del asistente en la API falsa (contratos/conversaciones.md §17), tipada con
 * packages/contratos: categorías (lista, crear, actualizar, activar y desactivar),
 * `probar-mensaje`, `efecto`, y en la bandeja: categoría y actividad del asistente en cada
 * conversación, filtro `categoriaId`, conteos, `categorizar` y, como en la entrega 3b, mensajes del
 * asistente (`autorTipo: "asistente_ia"`) y `asistente.estado: "atendiendo"`.
 */

type Problema = Schema<"Problema">;
type CategoriaRef = Schema<"CategoriaMensajesRef">;
type AjustesPrueba = Schema<"AjustesPrueba">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

type ExtrasAjustes = Pick<AjustesAsistente, "autonomoDisponible" | "modoAutonomo" | "esperaMin" | "maxTurnos" | "perfiles" | "porCola">;

export type OpcionesFase3 = {
    /** El servidor ya actúa solo (entrega 3b). Por defecto `false`: guarda todo pero `modoAutonomo` solo admite `nunca`. */
    autonomoDisponible?: boolean;
    ajustes?: Partial<Omit<ExtrasAjustes, "autonomoDisponible">>;
    /** La bandeja trae categorías, actividad del asistente y (como en 3b) mensajes del asistente. */
    bandeja?: boolean;
    /** Sin categorías (un servidor recién instalado las siembra; aquí, ninguna). */
    sinCategorias?: boolean;
    /** `POST /asistente/probar-mensaje`: el primer pedido falla (502) o responde 429 con `Retry-After: 5`. */
    probar?: "ok" | "error" | "429";
    /** `GET /asistente/efecto`: con el «antes», sin él, sin datos o con error (el primero). */
    efecto?: "ok" | "sin_antes" | "sin_datos" | "error";
    /** El primer guardado de una categoría responde 409 `conflicto_version`. */
    conflictoCategoria?: boolean;
    /** `…/categorizar` responde 409 la primera vez. */
    conflictoCategorizar?: boolean;
};

/** Un segundo perfil, por horario (lo edita la pantalla de horarios; la simple lo conserva). */
export const PERFIL_NOCHE: PerfilAsistente = { codigo: "noche", nombre: "Noche", acciones: { ...PERFIL_POR_DEFECTO.acciones, promesa_pago: true }, limites: { prioridadMaxima: "media", diasAdelanteMax: 1 } };

export const extrasDe = (o: OpcionesFase3 | undefined): ExtrasAjustes => ({
    modoAutonomo: "nunca",
    esperaMin: 3,
    maxTurnos: 8,
    perfiles: [structuredClone(PERFIL_POR_DEFECTO), structuredClone(PERFIL_NOCHE)],
    porCola: [{ colaId: 4, accionesApagadas: ["promesa_pago"] }],
    ...o?.ajustes,
    autonomoDisponible: o?.autonomoDisponible ?? false,
});

const categoria = (c: Pick<Categoria, "id" | "nombre" | "tono"> & Partial<Categoria>): Categoria => ({
    descripcion: "",
    colaId: 2,
    slaPrimeraRespuestaMin: 60,
    modo: "sugiere",
    condicion: null,
    manualIds: [],
    esOtro: false,
    activa: true,
    orden: c.id,
    version: 1,
    ...c,
});

export const CATEGORIAS: Categoria[] = [
    categoria({ id: 1, nombre: "Planes y precios", tono: "info", descripcion: "Pregunta por planes, precios o cobertura.", colaId: 4, slaPrimeraRespuestaMin: 15, modo: "responde_solo", manualIds: [2] }),
    categoria({ id: 2, nombre: "Sin internet", tono: "error", descripcion: "No navega, luz roja, lento, cortes.", colaId: 2, slaPrimeraRespuestaMin: 10, modo: "responde_solo", condicion: "si_hay_falla", manualIds: [1] }),
    categoria({ id: 3, nombre: "Pagos y saldo", tono: "ok", descripcion: "Pagos, boletas y saldo.", colaId: 3, slaPrimeraRespuestaMin: 30, modo: "responde_solo", condicion: "solo_verificados" }),
    categoria({ id: 4, nombre: "Cambio de plan", tono: "info", colaId: 4, slaPrimeraRespuestaMin: 120 }),
    categoria({ id: 5, nombre: "Mudanza o traslado", tono: "aviso", colaId: 2, slaPrimeraRespuestaMin: 240 }),
    categoria({ id: 6, nombre: "Otro", tono: "neutro", colaId: null, slaPrimeraRespuestaMin: null, esOtro: true }),
];

export const RESPUESTA_PRUEBA = "¡Hola! Soy el asistente virtual de Red Sur. El plan de 100 Mb cuesta Q 299 al mes, con instalación gratis este mes. ¿Quiere que le agende la instalación?";

const ref = (c: Categoria): CategoriaRef => ({ id: c.id, nombre: c.nombre, tono: c.tono });

type Actividad = NonNullable<Conversacion["actividadAsistente"]>;
const EN = "2026-10-28T16:40:00Z";

/** Lo que la bandeja de prueba trae de la fase 3, por conversación. */
const EN_BANDEJA: Record<number, { categoriaId: number; actividad?: Pick<Actividad, "tipo" | "texto">; atiende?: boolean }> = {
    41: { categoriaId: 2, actividad: { tipo: "sugirio", texto: "Sugerencia lista para el agente" } },
    42: { categoriaId: 3, actividad: { tipo: "boleta_enviada", texto: "Boleta enviada a Argos" } },
    43: { categoriaId: 1, actividad: { tipo: "respondio_manual", texto: "Respondió con el manual" }, atiende: true },
    45: { categoriaId: 2 },
    46: { categoriaId: 5, actividad: { tipo: "espera_persona", texto: "Espera a una persona" } },
};

type Contexto = {
    conversaciones: Conversacion[];
    mensajes: Map<number, Mensaje[]>;
    enviados: { ruta: string; cuerpo: unknown; idempotencia: string | undefined }[];
};

const BASE = "/api/v1/mensajeria";

export function apiFase3(e: Contexto, opciones: OpcionesFase3 | undefined) {
    // Sin la opción `fase3`: sin categorías, y las demás pruebas de la bandeja no cambian.
    const categorias: Categoria[] = opciones === undefined || opciones.sinCategorias ? [] : CATEGORIAS.map((c) => structuredClone(c));
    const pruebas: { texto: string; ajustes?: AjustesPrueba }[] = [];
    let probar = opciones?.probar ?? "ok";
    let efecto = opciones?.efecto ?? "ok";
    let conflictoCategoria = opciones?.conflictoCategoria ?? false;
    let conflictoCategorizar = opciones?.conflictoCategorizar ?? false;
    const bandeja = opciones?.bandeja ?? false;

    const delAsistente = (conversacionId: number, seq: number, direccion: "saliente" | "nota", texto: string, creadoEn: string): Mensaje => ({
        id: `0193a100-0000-7000-8000-0000000000${conversacionId}`,
        conversacionId,
        seq,
        direccion,
        tipo: "texto",
        texto,
        estadoEntrega: direccion === "nota" ? null : "entregado",
        motivoFalla: null,
        menciones: [],
        creadoEn,
        enviadoEn: null,
        entregadoEn: null,
        leidoEn: null,
        automatico: "asistente",
        autorTipo: "asistente_ia",
    });

    if (bandeja) {
        for (const c of e.conversaciones) {
            const d = EN_BANDEJA[c.id];
            const cat = d ? categorias.find((x) => x.id === d.categoriaId) : undefined;
            if (!d || !cat) continue;
            c.categoria = ref(cat);
            c.categoriaConfianza = 0.92;
            c.categoriaOrigen = "asistente";
            if (d.actividad) c.actividadAsistente = { ...d.actividad, en: EN };
            if (d.atiende) c.asistente = { estado: "atendiendo", turnos: 1 };
        }
        // 43: el asistente contestó solo; 46: dejó una nota al pasar a una persona.
        e.mensajes.get(43)?.push(delAsistente(43, 9001, "saliente", "¡Hola! Soy el asistente virtual de Red Sur. Sí tenemos fibra en San Lucas. ¿Quiere que le cuente los planes?", "2026-10-28T16:21:00Z"));
        e.mensajes.get(46)?.push(delAsistente(46, 9002, "nota", "El cliente quiere mudarse a Mixco el otro mes. No sé si hay cobertura en esa dirección: la dejo en la cola de Soporte.", "2026-10-28T15:53:00Z"));
    }

    function porCategoria(activas: readonly Conversacion[]): { id: number; total: number }[] {
        const m = new Map<number, number>();
        for (const c of activas) if (c.categoria) m.set(c.categoria.id, (m.get(c.categoria.id) ?? 0) + 1);
        return [...m].map(([id, total]) => ({ id, total }));
    }

    /** `POST /conversaciones/{id}/categorizar`: devuelve el problema o cambia la conversación. */
    function categorizar(c: Conversacion, cuerpo: Record<string, unknown>): Problema | null {
        if (conflictoCategorizar) {
            conflictoCategorizar = false;
            return P("conflicto_version", 409, "Alguien más cambió la conversación");
        }
        if (cuerpo.categoriaId === null) {
            delete c.categoria;
            c.categoriaOrigen = null;
            c.categoriaConfianza = null;
            return null;
        }
        const cat = categorias.find((x) => x.id === cuerpo.categoriaId && x.activa);
        if (!cat) return P("categoria_inactiva", 422, "La categoría no existe o está inactiva");
        c.categoria = ref(cat);
        c.categoriaOrigen = "agente";
        c.categoriaConfianza = null;
        return null;
    }

    function prueba(texto: string, ajustes: AjustesPrueba | undefined): PruebaMensaje {
        const t = texto.toLowerCase();
        const lista = ajustes?.categorias ?? categorias;
        const nombre = t.includes("plan") || t.includes("cuesta") ? "Planes y precios" : t.includes("internet") ? "Sin internet" : t.includes("pag") || t.includes("boleta") ? "Pagos y saldo" : "Otro";
        const cat = lista.find((c) => c.nombre === nombre);
        const categoriaProbada = { id: cat?.id ?? null, nombre: cat?.nombre ?? "Otro", tono: cat?.tono ?? ("neutro" as const) };
        const base = { categoria: categoriaProbada, confianza: 0.94, fuentes: [], acciones: [] };
        if (t.includes("persona") || t.includes("humano")) return { ...base, haria: "pasa_a_persona", motivo: "pide_persona", respuesta: null };
        const cuerpo = {
            respuesta: RESPUESTA_PRUEBA,
            fuentes: [{ tipo: "manual" as const, etiqueta: "Manual «Planes FTTH y WISP»", entidadId: "2" }],
            // Solo describe acciones encendidas en el perfil que se prueba.
            acciones: ajustes?.perfil?.acciones.agendar_visita === false ? [] : [{ tipo: "agendar_visita" as const, descripcion: "Propondría hasta 3 espacios para agendar la visita" }],
        };
        if (ajustes?.modoAutonomo === "nunca") return { ...base, ...cuerpo, haria: "sugiere", motivo: "modo_nunca" };
        if (cat?.modo !== "responde_solo") return { ...base, ...cuerpo, haria: "sugiere", motivo: "categoria_sugiere" };
        // En la prueba no hay cliente: ni Argos ni Horus (§17.5).
        if (cat.condicion === "si_hay_falla") return { ...base, haria: "pasa_a_persona", motivo: "sin_falla", respuesta: null };
        if (cat.condicion === "solo_verificados") return { ...base, haria: "pasa_a_persona", motivo: "sin_verificar", respuesta: null };
        return { ...base, ...cuerpo, haria: "responde_solo", motivo: null };
    }

    function efectoDelMes(): EfectoAsistente {
        if (efecto === "sin_datos") return { mes: "2026-10", primeraRespuestaPromedioSeg: null, primeraRespuestaPromedioAntesSeg: null, resueltasSinPersona: 0, totalAtendidas: 0, porCategoria: [] };
        return {
            mes: "2026-10",
            primeraRespuestaPromedioSeg: 100,
            primeraRespuestaPromedioAntesSeg: efecto === "sin_antes" ? null : 840,
            resueltasSinPersona: 41,
            totalAtendidas: 100,
            porCategoria: [{ categoriaId: 2, nombre: "Sin internet", tono: "error", total: 40, resueltasSinPersona: 22, primeraRespuestaPromedioSeg: 80 }],
        };
    }

    /** `GET /asistente/categorias` (`conversaciones.ver`): las activas por `orden`, o todas con `incluirInactivas`. */
    const listar = (query: URLSearchParams): Categoria[] => categorias.filter((c) => c.activa || query.get("incluirInactivas") === "true").sort((a, b) => a.orden - b.orden);

    /** Escrituras, `probar-mensaje` y `efecto` (ya con `mensajeria.asistente_configurar` comprobado). */
    async function manejar(x: { route: Route; metodo: string; ruta: string; cuerpo: Record<string, unknown>; idempotencia: string | undefined }): Promise<boolean> {
        const { route, metodo, ruta, cuerpo, idempotencia } = x;
        const repetida = (nombre: unknown, salvo?: number) => categorias.some((c) => c.id !== salvo && c.nombre.toLowerCase() === String(nombre).trim().toLowerCase());
        const duplicada = () => problema(route, P("categoria_duplicada", 409, "Ya hay una categoría con ese nombre"));
        if (ruta === `${BASE}/asistente/categorias` && metodo === "POST") {
            e.enviados.push({ ruta, cuerpo, idempotencia });
            if (repetida(cuerpo.nombre)) return (await duplicada(), true);
            const id = Math.max(0, ...categorias.map((c) => c.id)) + 1;
            const nueva = categoria({ ...(cuerpo as unknown as Categoria), id, orden: id, esOtro: false, activa: true, version: 1 });
            categorias.push(nueva);
            return (await json(route, nueva, 201), true);
        }
        const m = /^\/api\/v1\/mensajeria\/asistente\/categorias\/(\d+)\/(actualizar|activar|desactivar)$/.exec(ruta);
        if (m && metodo === "POST") {
            e.enviados.push({ ruta, cuerpo, idempotencia });
            const c = categorias.find((v) => v.id === Number(m[1]));
            if (!c) return (await problema(route, P("no_encontrado", 404, "No existe")), true);
            if (conflictoCategoria || cuerpo.version !== c.version) {
                conflictoCategoria = false;
                return (await problema(route, P("conflicto_version", 409, "Alguien más cambió la categoría")), true);
            }
            if (m[2] === "actualizar") {
                if (repetida(cuerpo.nombre, c.id)) return (await duplicada(), true);
                Object.assign(c, cuerpo);
            } else if (m[2] === "desactivar" && c.esOtro) {
                return (await problema(route, P("categoria_otro", 422, "«Otro» no se desactiva")), true);
            } else c.activa = m[2] === "activar";
            c.version += 1;
            return (await json(route, c), true);
        }
        if (ruta === `${BASE}/asistente/probar-mensaje` && metodo === "POST") {
            pruebas.push(cuerpo as unknown as { texto: string; ajustes?: AjustesPrueba });
            await new Promise((r) => setTimeout(r, 250));
            if (probar === "429") {
                probar = "ok";
                return (await route.fulfill({ status: 429, headers: { "Content-Type": "application/problem+json", "Retry-After": "5" }, body: JSON.stringify(P("demasiados_pedidos", 429, "Demasiados pedidos")) }), true);
            }
            if (probar === "error") {
                probar = "ok";
                return (await problema(route, P("proveedor_no_responde", 502, "El proveedor no respondió", { detail: "El servidor de IA no respondió a tiempo" })), true);
            }
            return (await json(route, prueba(String(cuerpo.texto ?? ""), cuerpo.ajustes as AjustesPrueba | undefined)), true);
        }
        if (ruta === `${BASE}/asistente/efecto` && metodo === "GET") {
            if (efecto === "error") {
                efecto = "ok";
                return (await problema(route, P("error_interno", 500, "Error del servidor")), true);
            }
            return (await json(route, efectoDelMes()), true);
        }
        return false;
    }

    return { manejar, listar, categorias, porCategoria, categorizar, pruebas, bandeja };
}

export type ApiFase3 = ReturnType<typeof apiFase3>;
