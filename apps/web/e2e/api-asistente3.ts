import type { Schema } from "@iris/contratos";
import type { Route } from "@playwright/test";
import { ACCIONES_POR_DEFECTO, LIMITES_POR_DEFECTO, type Categoria, type CategoriaRef, type EfectoAsistente, type ExtrasAjustes, type PruebaMensaje } from "../lib/asistente/fase3";
import type { Conversacion, Mensaje } from "../lib/conversaciones/tipos";

/*
 * Fase 3 del asistente en la API falsa (contratos/conversaciones.md §17, formas provisionales de
 * `lib/asistente/fase3.ts`): categorías (lista, crear, actualizar, activar y desactivar),
 * `probar-mensaje`, `efecto`, y en la bandeja: catálogo, categoría y actividad del asistente en
 * cada conversación, filtro `categoriaId`, conteos, `categorizar`, mensajes del asistente
 * (`autorTipo: "asistente_ia"`) y la franja «El asistente está atendiendo».
 */

type Problema = Schema<"Problema">;

const json = (route: Route, cuerpo: unknown, status = 200) => route.fulfill({ status, contentType: "application/json", body: JSON.stringify(cuerpo) });
const problema = (route: Route, p: Problema) => route.fulfill({ status: p.status, contentType: "application/problem+json", body: JSON.stringify(p) });
const P = (codigo: string, status: number, title: string, extra: Partial<Problema> = {}): Problema => ({ type: `urn:iris:problema:${codigo}`, title, status, ...extra });

export type OpcionesFase3 = {
    /** El servidor ya actúa solo (entrega 3b). Por defecto `false`: guarda el modo pero no actúa. */
    autonomoDisponible?: boolean;
    ajustes?: Partial<Omit<ExtrasAjustes, "autonomoDisponible">>;
    /** La bandeja trae categorías, actividad del asistente y mensajes del asistente. */
    bandeja?: boolean;
    /** Sin categorías creadas. */
    sinCategorias?: boolean;
    /** `POST /asistente/probar-mensaje`: el primer pedido falla (502) o responde 429 con `Retry-After: 5`. */
    probar?: "ok" | "error" | "429";
    /** `GET /asistente/efecto`: con el «antes», sin él, sin datos o con error (el primero). */
    efecto?: "ok" | "sin_antes" | "sin_datos" | "error";
    /** El primer guardado de una categoría responde 409. */
    conflictoCategoria?: boolean;
    /** `…/categorizar` responde 409 la primera vez. */
    conflictoCategorizar?: boolean;
};

export const EXTRAS_POR_DEFECTO: ExtrasAjustes = { modoAutonomo: "nunca", esperaMin: 3, acciones: { ...ACCIONES_POR_DEFECTO }, limites: { ...LIMITES_POR_DEFECTO }, autonomoDisponible: false };

export const extrasDe = (o: OpcionesFase3 | undefined): ExtrasAjustes => ({ ...EXTRAS_POR_DEFECTO, ...o?.ajustes, autonomoDisponible: o?.autonomoDisponible ?? false });

const categoria = (c: Pick<Categoria, "id" | "nombre" | "tono"> & Partial<Categoria>): Categoria => ({
    descripcion: "",
    colaId: 2,
    slaPrimeraRespuestaMin: 60,
    modo: "sugiere",
    condicion: null,
    manualIds: [],
    activa: true,
    version: 1,
    ...c,
});

export const CATEGORIAS: Categoria[] = [
    categoria({ id: 1, nombre: "Planes y precios", tono: "info", descripcion: "Pregunta por planes, precios o cobertura.", colaId: 4, slaPrimeraRespuestaMin: 15, modo: "responde_solo", manualIds: [2] }),
    categoria({ id: 2, nombre: "Sin internet", tono: "peligro", descripcion: "No tiene servicio o está lento.", colaId: 2, slaPrimeraRespuestaMin: 10, modo: "responde_solo", condicion: "si_hay_falla", manualIds: [1] }),
    categoria({ id: 3, nombre: "Pagos y saldo", tono: "exito", descripcion: "Pagos, boletas y saldo.", colaId: 3, slaPrimeraRespuestaMin: 30, modo: "responde_solo", condicion: "solo_verificados" }),
    categoria({ id: 4, nombre: "Cambio de plan", tono: "info", colaId: 4, slaPrimeraRespuestaMin: 120 }),
    categoria({ id: 5, nombre: "Mudanza o traslado", tono: "alerta", colaId: 2, slaPrimeraRespuestaMin: 240 }),
    categoria({ id: 6, nombre: "Otro", tono: "neutro", colaId: 2, slaPrimeraRespuestaMin: 60 }),
];

export const RESPUESTA_PRUEBA = "¡Hola! Soy el asistente virtual de Red Sur. El plan de 100 Mb cuesta Q 299 al mes, con instalación gratis este mes. ¿Quiere que le agende la instalación?";

const ref = (c: Categoria): CategoriaRef => ({ id: c.id, nombre: c.nombre, tono: c.tono });

/** Lo que la bandeja de prueba trae de la fase 3, por conversación. */
const EN_BANDEJA: Record<number, { categoriaId: number; actividad?: { tipo: string; texto: string }; atiende?: boolean }> = {
    41: { categoriaId: 2, actividad: { tipo: "respondio", texto: "Asistente respondió: falla en Zona 4" } },
    42: { categoriaId: 3, actividad: { tipo: "boleta", texto: "Boleta enviada a Argos" } },
    43: { categoriaId: 1, actividad: { tipo: "atendiendo", texto: "Asistente respondió con el manual" }, atiende: true },
    45: { categoriaId: 2 },
    46: { categoriaId: 5, actividad: { tipo: "espera_persona", texto: "Espera a una persona · vence en 3 h 50 min" } },
};

type Contexto = {
    conversaciones: Conversacion[];
    mensajes: Map<number, Mensaje[]>;
    enviados: { ruta: string; cuerpo: unknown; idempotencia: string | undefined }[];
};

const BASE = "/api/v1/mensajeria";

export function apiFase3(e: Contexto, opciones: OpcionesFase3 | undefined) {
    const categorias: Categoria[] = opciones?.sinCategorias ? [] : CATEGORIAS.map((c) => structuredClone(c));
    const pruebas: unknown[] = [];
    let probar = opciones?.probar ?? "ok";
    let efecto = opciones?.efecto ?? "ok";
    let conflictoCategoria = opciones?.conflictoCategoria ?? false;
    let conflictoCategorizar = opciones?.conflictoCategorizar ?? false;
    const bandeja = opciones?.bandeja ?? false;

    if (bandeja) {
        for (const c of e.conversaciones) {
            const d = EN_BANDEJA[c.id];
            const cat = d ? categorias.find((x) => x.id === d.categoriaId) : undefined;
            if (!d || !cat) continue;
            c.categoria = ref(cat);
            if (d.actividad) c.actividadAsistente = d.actividad;
            if (d.atiende) c.asistenteAtiende = true;
        }
        // 43: el asistente contestó solo; 46: dejó una nota al pasar a una persona.
        const de43 = e.mensajes.get(43);
        if (de43) {
            de43.push({
                id: "0193a100-0000-7000-8000-000000000043",
                conversacionId: 43,
                seq: 9001,
                direccion: "saliente",
                tipo: "texto",
                texto: "¡Hola! Soy el asistente virtual de Red Sur. Sí tenemos fibra en San Lucas. ¿Quiere que le cuente los planes?",
                estadoEntrega: "entregado",
                motivoFalla: null,
                menciones: [],
                creadoEn: "2026-10-28T16:21:00Z",
                enviadoEn: null,
                entregadoEn: null,
                leidoEn: null,
                automatico: null,
                autorTipo: "asistente_ia",
            });
        }
        const de46 = e.mensajes.get(46);
        if (de46) {
            de46.push({
                id: "0193a100-0000-7000-8000-000000000046",
                conversacionId: 46,
                seq: 9002,
                direccion: "nota",
                tipo: "texto",
                texto: "El cliente quiere mudarse a Mixco el otro mes. No sé si hay cobertura en esa dirección: la dejo en la cola de Soporte.",
                estadoEntrega: null,
                motivoFalla: null,
                menciones: [],
                creadoEn: "2026-10-28T15:53:00Z",
                enviadoEn: null,
                entregadoEn: null,
                leidoEn: null,
                automatico: null,
                autorTipo: "asistente_ia",
            });
        }
    }

    /** Catálogo de la bandeja (`GET /conversaciones/categorias`): las activas, o ninguna sin la fase 3. */
    const catalogo = (): CategoriaRef[] => (bandeja ? categorias.filter((c) => c.activa).map(ref) : []);

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
            c.categoria = null;
            return null;
        }
        const cat = categorias.find((x) => x.id === cuerpo.categoriaId && x.activa);
        if (!cat) return P("categoria_no_valida", 422, "La categoría no existe o está inactiva");
        c.categoria = ref(cat);
        return null;
    }

    function prueba(texto: string, ajustes: Record<string, unknown> | undefined): PruebaMensaje {
        const t = texto.toLowerCase();
        const cat = categorias.find((c) => (t.includes("plan") || t.includes("cuesta") ? c.id === 1 : t.includes("internet") ? c.id === 2 : t.includes("pag") || t.includes("boleta") ? c.id === 3 : c.id === 6));
        const modos = (ajustes?.categorias as { id: number; modo: string }[] | undefined) ?? [];
        const modo = modos.find((m) => m.id === cat?.id)?.modo ?? cat?.modo ?? "sugiere";
        if (t.includes("persona") || t.includes("humano")) return { categoria: cat ? ref(cat) : null, haria: "pasar_a_persona", respuesta: null, fuentes: [], acciones: [{ tipo: "pasar_a_persona", descripcion: "Dejaría la conversación en la cola de Soporte con una nota para el agente" }] };
        const solo = modo === "responde_solo" && ajustes?.modoAutonomo !== "nunca";
        const boleta = cat?.id === 3;
        return {
            categoria: cat ? ref(cat) : null,
            haria: solo ? "responder_solo" : "sugerir",
            respuesta: boleta ? "Con gusto. Envíeme la foto de su boleta y la paso a caja para que la revisen." : RESPUESTA_PRUEBA,
            fuentes: boleta ? [] : [{ tipo: "manual", etiqueta: "Manual «Planes FTTH y WISP»", entidadId: "2" }],
            acciones: boleta && (ajustes?.acciones as Record<string, boolean> | undefined)?.recibir_boleta ? [{ tipo: "recibir_boleta", descripcion: "Recibiría la boleta y la mandaría a Argos para que caja la apruebe" }] : [],
        };
    }

    function efectoDelMes(): EfectoAsistente {
        if (efecto === "sin_datos") return { mes: "2026-10", primeraRespuestaSeg: null, primeraRespuestaAntesSeg: null, resueltasSinPersonaPct: null };
        return { mes: "2026-10", primeraRespuestaSeg: 100, primeraRespuestaAntesSeg: efecto === "sin_antes" ? null : 840, resueltasSinPersonaPct: 41 };
    }

    /** Rutas bajo `/asistente/` (ya con `mensajeria.asistente_configurar` comprobado). */
    async function manejar(x: { route: Route; metodo: string; ruta: string; cuerpo: Record<string, unknown>; idempotencia: string | undefined }): Promise<boolean> {
        const { route, metodo, ruta, cuerpo, idempotencia } = x;
        if (ruta === `${BASE}/asistente/categorias` && metodo === "GET") return (await json(route, { data: categorias, nextCursor: null }), true);
        if (ruta === `${BASE}/asistente/categorias` && metodo === "POST") {
            e.enviados.push({ ruta, cuerpo, idempotencia });
            if (categorias.some((c) => c.nombre.toLowerCase() === String(cuerpo.nombre).toLowerCase())) {
                return (await problema(route, P("validacion", 422, "Datos inválidos", { errors: [{ field: "body.nombre", message: "Ya hay una categoría con ese nombre" }] })), true);
            }
            const nueva = categoria({ ...(cuerpo as unknown as Categoria), id: Math.max(0, ...categorias.map((c) => c.id)) + 1, activa: true, version: 1 });
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
            if (m[2] === "actualizar") Object.assign(c, cuerpo);
            else c.activa = m[2] === "activar";
            c.version += 1;
            return (await json(route, c), true);
        }
        if (ruta === `${BASE}/asistente/probar-mensaje` && metodo === "POST") {
            pruebas.push(cuerpo);
            await new Promise((r) => setTimeout(r, 250));
            if (probar === "429") {
                probar = "ok";
                return (await route.fulfill({ status: 429, headers: { "Content-Type": "application/problem+json", "Retry-After": "5" }, body: JSON.stringify(P("demasiados_pedidos", 429, "Demasiados pedidos")) }), true);
            }
            if (probar === "error") {
                probar = "ok";
                return (await problema(route, P("proveedor_no_responde", 502, "El proveedor no respondió", { detail: "El servidor de IA no respondió a tiempo" })), true);
            }
            return (await json(route, prueba(String(cuerpo.texto ?? ""), cuerpo.ajustes as Record<string, unknown> | undefined)), true);
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

    return { manejar, categorias, catalogo, porCategoria, categorizar, pruebas, bandeja };
}

export type ApiFase3 = ReturnType<typeof apiFase3>;
