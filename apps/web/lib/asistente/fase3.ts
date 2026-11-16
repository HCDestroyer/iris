import { z } from "zod";
import type { AjustesAsistente, EntradaAjustesAsistente, FuenteSugerencia } from "./tipos";

/*
 * Fase 3 del asistente: atiende solo, categorías de mensajes, «Pruébalo» y efecto en el SLA
 * (contratos/conversaciones.md §17, en curso en `agente/i1-asistente-3`).
 *
 * FORMAS PROVISIONALES: hasta que el OpenAPI de §17 llegue a `packages/contratos`, las formas de
 * este archivo son las que el líder acordó con I1. Al mergear su rama, cada tipo de aquí pasa a ser
 * un alias de `Schema<"…">` (como `lib/asistente/tipos.ts`) y las llamadas de `api-fase3.ts` usan
 * el cliente generado. Los esquemas zod son el espejo del validador del servidor.
 */

// --- Ajustes -----------------------------------------------------------------------------------

export type ModoAutonomo = "nunca" | "sin_nadie" | "siempre_primero";

export const CLAVES_ACCION = ["responder_dudas", "avisar_fallas", "crear_ticket", "agendar_visita", "recibir_boleta", "dar_saldo", "promesa_pago", "reconexion_temporal"] as const;
export type ClaveAccion = (typeof CLAVES_ACCION)[number];
export type AccionesAsistente = Record<ClaveAccion, boolean>;

export const PRIORIDADES = ["baja", "media", "alta", "critica"] as const;
export type PrioridadTicket = (typeof PRIORIDADES)[number];
export const NOMBRE_PRIORIDAD: Record<PrioridadTicket, string> = { baja: "Baja", media: "Media", alta: "Alta", critica: "Crítica" };

export type LimitesAsistente = { prioridadMaxima: PrioridadTicket; diasAdelanteMax: number; maxTurnos: number };

/** Lo que §17 suma a `AjustesAsistente`. */
export type ExtrasAjustes = {
    modoAutonomo: ModoAutonomo;
    esperaMin: number;
    acciones: AccionesAsistente;
    limites: LimitesAsistente;
    /** `false` mientras el servidor guarda el modo pero todavía no actúa solo (entrega 3a). */
    autonomoDisponible: boolean;
};

/** Los ajustes como llegan: un servidor anterior a §17 no trae los campos nuevos. */
export type AjustesFase3 = AjustesAsistente & Partial<ExtrasAjustes>;
export type AjustesCompletos = AjustesAsistente & ExtrasAjustes;
export type EntradaAjustesFase3 = EntradaAjustesAsistente & Partial<Omit<ExtrasAjustes, "autonomoDisponible">>;

export const ACCIONES_POR_DEFECTO: AccionesAsistente = {
    responder_dudas: true,
    avisar_fallas: true,
    crear_ticket: true,
    agendar_visita: true,
    recibir_boleta: true,
    dar_saldo: true,
    promesa_pago: false,
    reconexion_temporal: false,
};
export const LIMITES_POR_DEFECTO: LimitesAsistente = { prioridadMaxima: "alta", diasAdelanteMax: 2, maxTurnos: 8 };

/** Completa los ajustes con los valores por defecto de §17 (servidor anterior: «Nunca», sin actuar solo). */
export function completar(a: AjustesFase3): AjustesCompletos {
    return {
        ...a,
        modoAutonomo: a.modoAutonomo ?? "nunca",
        esperaMin: a.esperaMin ?? 3,
        acciones: { ...ACCIONES_POR_DEFECTO, ...a.acciones },
        limites: { ...LIMITES_POR_DEFECTO, ...a.limites },
        autonomoDisponible: a.autonomoDisponible ?? false,
    };
}

/** ¿Hay un proveedor de IA conectado? (lo mismo que el servidor exige para activar, §16.1) */
export function proveedorConectado(a: Pick<AjustesAsistente, "proveedor" | "url" | "modelo" | "claveGuardadaEn">): boolean {
    if (a.proveedor === "anthropic") return a.claveGuardadaEn !== null;
    return a.url.trim() !== "" && a.modelo.trim() !== "";
}

/** Cuerpo de `POST /asistente/ajustes/actualizar` con lo guardado, para cambiar solo una parte. */
export function entradaGuardada(a: AjustesCompletos): EntradaAjustesFase3 {
    return {
        activo: a.activo,
        proveedor: a.proveedor,
        url: a.url,
        modelo: a.modelo,
        modo: a.modo,
        fuentes: { ...a.fuentes },
        topeTokensMes: a.topeTokensMes,
        precioEntradaMillon: a.precioEntradaMillon,
        precioSalidaMillon: a.precioSalidaMillon,
        version: a.version,
        modoAutonomo: a.modoAutonomo,
        esperaMin: a.esperaMin,
        acciones: { ...a.acciones },
        limites: { ...a.limites },
    };
}

export const ESPERAS_MIN = [1, 2, 3, 5, 10, 15] as const;

/** Las tres preguntas de la pantalla principal. */
export const esquemaPrincipal = z.object({
    activo: z.boolean(),
    modoAutonomo: z.enum(["nunca", "sin_nadie", "siempre_primero"]),
    esperaMin: z.number().int().min(1, "Entre 1 y 60 minutos").max(60, "Entre 1 y 60 minutos"),
    acciones: z.object({
        responder_dudas: z.boolean(),
        avisar_fallas: z.boolean(),
        crear_ticket: z.boolean(),
        agendar_visita: z.boolean(),
        recibir_boleta: z.boolean(),
        dar_saldo: z.boolean(),
        promesa_pago: z.boolean(),
        reconexion_temporal: z.boolean(),
    }),
    /** Modo por categoría (`id` como texto → modo). */
    modos: z.record(z.string(), z.enum(["responde_solo", "sugiere"])),
});
export type ValoresPrincipal = z.infer<typeof esquemaPrincipal>;

export function valoresPrincipal(a: AjustesCompletos, categorias: readonly Categoria[]): ValoresPrincipal {
    return {
        activo: a.activo,
        modoAutonomo: a.modoAutonomo,
        esperaMin: a.esperaMin,
        acciones: { ...a.acciones },
        modos: Object.fromEntries(categorias.map((c) => [String(c.id), c.modo])),
    };
}

/** Límites por acción (`/administracion/asistente/limites`). */
export const esquemaLimites = z.object({
    prioridadMaxima: z.enum(PRIORIDADES),
    diasAdelanteMax: z
        .string()
        .trim()
        .refine((v) => /^\d+$/.test(v) && Number(v) >= 1 && Number(v) <= 30, "Entre 1 y 30 días"),
    maxTurnos: z
        .string()
        .trim()
        .refine((v) => /^\d+$/.test(v) && Number(v) >= 1 && Number(v) <= 30, "Entre 1 y 30 mensajes"),
});
export type ValoresLimites = z.infer<typeof esquemaLimites>;

// --- Categorías --------------------------------------------------------------------------------

export type ModoCategoria = "responde_solo" | "sugiere";
export type CondicionCategoria = "si_hay_falla" | "solo_verificados";
/** Colores de Horus para el chip (los mismos de las etiquetas). */
export const TONOS_CATEGORIA = ["info", "peligro", "exito", "alerta", "marca", "neutro"] as const;
export type TonoCategoria = (typeof TONOS_CATEGORIA)[number];
export const NOMBRE_TONO: Record<TonoCategoria, string> = { info: "Azul", peligro: "Rojo", exito: "Verde", alerta: "Naranja", marca: "Teal", neutro: "Gris" };

export type Categoria = {
    id: number;
    nombre: string;
    descripcion: string;
    tono: string;
    colaId: number | null;
    slaPrimeraRespuestaMin: number | null;
    modo: ModoCategoria;
    condicion: CondicionCategoria | null;
    manualIds: number[];
    activa: boolean;
    version: number;
};
export type EntradaCategoria = Omit<Categoria, "id" | "activa" | "version"> & { version?: number };

/** La categoría como viaja en la conversación y en «Pruébalo». */
export type CategoriaRef = Pick<Categoria, "id" | "nombre" | "tono">;
/** Qué hizo (o espera) el asistente en la conversación: una línea corta que escribe el servidor. */
export type ActividadAsistente = { tipo: string; texto: string };

export const NOMBRE_MODO: Record<ModoCategoria, string> = { responde_solo: "Responde solo", sugiere: "Sugiere al agente" };
export const NOMBRE_CONDICION: Record<CondicionCategoria | "siempre", string> = {
    siempre: "Siempre",
    si_hay_falla: "Solo si hay una falla en la zona; si no, a la cola",
    solo_verificados: "Solo a clientes verificados",
};

/** «15 min», «2 h», «1 h 30 min». */
export function minutosLegibles(min: number): string {
    if (min < 60) return `${min} min`;
    const h = Math.floor(min / 60);
    const m = min % 60;
    return m ? `${h} h ${m} min` : `${h} h`;
}

/** «Responde solo si hay falla; si no, a la cola», «Responde solo a verificados», «Sugiere al agente». */
export function resumenModo(c: Pick<Categoria, "modo" | "condicion">): string {
    if (c.modo === "sugiere") return NOMBRE_MODO.sugiere;
    if (c.condicion === "si_hay_falla") return "Responde solo si hay falla; si no, a la cola";
    if (c.condicion === "solo_verificados") return "Responde solo a verificados";
    return NOMBRE_MODO.responde_solo;
}

export const esquemaCategoria = z.object({
    nombre: z.string().trim().min(1, "Escribe el nombre de la categoría").max(60, "Hasta 60 caracteres"),
    descripcion: z.string().trim().max(500, "Hasta 500 caracteres"),
    tono: z.enum(TONOS_CATEGORIA),
    /** `""` = sin cola propia (la del número). */
    colaId: z.string(),
    slaPrimeraRespuestaMin: z
        .string()
        .trim()
        .refine((v) => v === "" || (/^\d+$/.test(v) && Number(v) >= 1 && Number(v) <= 10080), "Entre 1 y 10,080 minutos, o vacío"),
    modo: z.enum(["responde_solo", "sugiere"]),
    condicion: z.enum(["siempre", "si_hay_falla", "solo_verificados"]),
    manualIds: z.array(z.number()).max(10, "Hasta 10 manuales"),
    activa: z.boolean(),
});
export type ValoresCategoria = z.infer<typeof esquemaCategoria>;

const esTono = (t: string): t is TonoCategoria => (TONOS_CATEGORIA as readonly string[]).includes(t);
/** El tono del chip; uno que el panel no conoce se ve neutro. */
export const tonoDe = (tono: string): TonoCategoria => (esTono(tono) ? tono : "neutro");

export function valoresCategoria(c?: Categoria): ValoresCategoria {
    return {
        nombre: c?.nombre ?? "",
        descripcion: c?.descripcion ?? "",
        tono: tonoDe(c?.tono ?? "info"),
        colaId: c?.colaId ? String(c.colaId) : "",
        slaPrimeraRespuestaMin: c?.slaPrimeraRespuestaMin ? String(c.slaPrimeraRespuestaMin) : "",
        modo: c?.modo ?? "sugiere",
        condicion: c?.condicion ?? "siempre",
        manualIds: [...(c?.manualIds ?? [])],
        activa: c?.activa ?? true,
    };
}

/** Lo guardado de una categoría como cuerpo de `…/actualizar`, para cambiar solo una parte. */
export function entradaDeCategoria(c: Categoria): EntradaCategoria {
    return {
        nombre: c.nombre,
        descripcion: c.descripcion,
        tono: c.tono,
        colaId: c.colaId,
        slaPrimeraRespuestaMin: c.slaPrimeraRespuestaMin,
        modo: c.modo,
        condicion: c.condicion,
        manualIds: [...c.manualIds],
    };
}

export function entradaCategoria(d: ValoresCategoria, version?: number): EntradaCategoria {
    return {
        nombre: d.nombre.trim(),
        descripcion: d.descripcion.trim(),
        tono: d.tono,
        colaId: d.colaId ? Number(d.colaId) : null,
        slaPrimeraRespuestaMin: d.slaPrimeraRespuestaMin ? Number(d.slaPrimeraRespuestaMin) : null,
        modo: d.modo,
        // La condición solo aplica cuando responde solo.
        condicion: d.modo === "responde_solo" && d.condicion !== "siempre" ? d.condicion : null,
        manualIds: [...d.manualIds],
        ...(version !== undefined ? { version } : {}),
    };
}

// --- «Pruébalo antes de guardar» ---------------------------------------------------------------

export type HariaAsistente = "responder_solo" | "sugerir" | "pasar_a_persona";
export type PruebaMensaje = {
    categoria: CategoriaRef | null;
    haria: HariaAsistente;
    respuesta: string | null;
    fuentes: FuenteSugerencia[];
    acciones: { tipo: string; descripcion: string }[];
};
/** Los ajustes del formulario **sin guardar** con los que se prueba. */
export type AjustesDePrueba = Pick<ExtrasAjustes, "modoAutonomo" | "esperaMin" | "acciones"> & { categorias: { id: number; modo: ModoCategoria }[] };

export const TEXTO_PRUEBA_MAX = 1000;

export const TEXTO_HARIA: Record<HariaAsistente, string> = {
    responder_solo: "Respondería solo",
    sugerir: "Le sugeriría al agente",
    pasar_a_persona: "Pasaría a una persona",
};

/** «Respondería solo, con Manual «Planes FTTH y WISP»:» (las fuentes como las nombra el servidor). */
export function textoHaria(p: Pick<PruebaMensaje, "haria" | "fuentes" | "respuesta">): string {
    const base = TEXTO_HARIA[p.haria] ?? TEXTO_HARIA.pasar_a_persona;
    const fuentes = p.fuentes.map((f) => f.etiqueta).filter(Boolean);
    const con = fuentes.length ? `, con ${fuentes.join(" y ")}` : "";
    return `${base}${con}${p.respuesta ? ":" : "."}`;
}

export function ajustesDePrueba(d: ValoresPrincipal): AjustesDePrueba {
    return {
        modoAutonomo: d.modoAutonomo,
        esperaMin: d.esperaMin,
        acciones: { ...d.acciones },
        categorias: Object.entries(d.modos).map(([id, modo]) => ({ id: Number(id), modo })),
    };
}

// --- Efecto en el SLA --------------------------------------------------------------------------

export type EfectoAsistente = {
    mes: string;
    /** Primera respuesta promedio del mes, en segundos; `null` sin conversaciones. */
    primeraRespuestaSeg: number | null;
    /** La de antes de activar el asistente, si el servidor la conoce. */
    primeraRespuestaAntesSeg: number | null;
    /** % de conversaciones resueltas por el asistente sin pasar a una persona; `null` sin datos. */
    resueltasSinPersonaPct: number | null;
};

/** «1 min 40 s», «14 min», «45 s», «2 h 5 min». */
export function duracionLegible(seg: number): string {
    const s = Math.max(0, Math.round(seg));
    if (s < 60) return `${s} s`;
    const min = Math.floor(s / 60);
    if (min < 60) {
        const resto = s % 60;
        return resto && min < 10 ? `${min} min ${resto} s` : `${min} min`;
    }
    return minutosLegibles(min);
}

// --- Bandeja -----------------------------------------------------------------------------------

/** ¿El mensaje lo escribió el asistente (y no una persona ni el bot de menú)? */
export function esDelAsistente(m: { autorTipo?: string | null | undefined; automatico?: string | null | undefined }): boolean {
    return m.autorTipo === "asistente_ia" || m.automatico === "asistente_ia";
}
