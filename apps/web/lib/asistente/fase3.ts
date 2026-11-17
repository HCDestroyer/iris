import type { Schema } from "@iris/contratos";
import { z } from "zod";
import type { AjustesAsistente, EntradaAjustesAsistente } from "./tipos";

/*
 * Fase 3 del asistente: atiende solo, categorías de mensajes, «Pruébalo» y efecto en el SLA
 * (contratos/conversaciones.md §17). Los tipos son alias de `packages/contratos`; los esquemas zod
 * son el espejo del validador del servidor.
 *
 * Entregas del servidor: en 3a guarda todo pero **todavía no actúa solo** (`autonomoDisponible:
 * false`, `modoAutonomo` solo admite `nunca`); en 3b actúa (mensajes con `autorTipo:
 * "asistente_ia"` y `asistente.estado` en la conversación).
 */

// --- Ajustes (§17.1) ---------------------------------------------------------------------------

export type ModoAutonomo = NonNullable<AjustesAsistente["modoAutonomo"]>;
export type PerfilAsistente = Schema<"PerfilAsistente">;
export type AccionesAsistente = Schema<"AccionesAsistente">;
export type LimitesPerfil = Schema<"LimitesAsistente">;
export type AccionesCola = Schema<"AccionesCola">;

/** Las ocho acciones que se prenden y se apagan (`pasar_a_persona` es siempre `true`). */
export const CLAVES_ACCION = ["responder_dudas", "avisar_fallas", "crear_ticket", "agendar_visita", "recibir_boleta", "dar_saldo", "promesa_pago", "reconexion_temporal"] as const;
export type ClaveAccion = (typeof CLAVES_ACCION)[number];
export const NOMBRE_ACCION: Record<ClaveAccion, string> = {
    responder_dudas: "Responder dudas",
    avisar_fallas: "Avisar de fallas en la zona",
    crear_ticket: "Crear tickets",
    agendar_visita: "Agendar visitas",
    recibir_boleta: "Recibir boletas de pago",
    dar_saldo: "Dar el saldo",
    promesa_pago: "Promesas de pago",
    reconexion_temporal: "Reconexión temporal",
};

export const PRIORIDADES = ["baja", "media", "alta", "critica"] as const;
export type PrioridadTicket = (typeof PRIORIDADES)[number];
export const NOMBRE_PRIORIDAD: Record<PrioridadTicket, string> = { baja: "Baja", media: "Media", alta: "Alta", critica: "Crítica" };

/**
 * El perfil de fábrica (§17.8.2): solo dudas, fallas y tickets. Lo que toca dinero o el servicio
 * (saldo, boletas, promesas, reconexión) y las visitas vienen apagadas: cada empresa las enciende.
 */
export const PERFIL_POR_DEFECTO: PerfilAsistente = {
    codigo: "defecto",
    nombre: "General",
    acciones: {
        responder_dudas: true,
        avisar_fallas: true,
        dar_saldo: false,
        crear_ticket: true,
        agendar_visita: false,
        recibir_boleta: false,
        promesa_pago: false,
        reconexion_temporal: false,
        pasar_a_persona: true,
    },
    // §17.10: de fábrica pasa de 2 a 7 días (acorta `visitas.diasAdelante`).
    limites: { prioridadMaxima: "alta", diasAdelanteMax: 7 },
};

/** Los ajustes con lo de §17 completo: un servidor anterior no lo manda (queda en «Nunca», sin actuar solo). */
export type AjustesCompletos = AjustesAsistente & {
    autonomoDisponible: boolean;
    modoAutonomo: ModoAutonomo;
    esperaMin: number;
    maxTurnos: number;
};

export function completar(a: AjustesAsistente): AjustesCompletos {
    return {
        ...a,
        autonomoDisponible: a.autonomoDisponible ?? false,
        modoAutonomo: a.modoAutonomo ?? "nunca",
        esperaMin: a.esperaMin ?? 3,
        maxTurnos: a.maxTurnos ?? 8,
    };
}

/**
 * ¿Para dar el saldo hay que verificar la identidad (DPI o NIT)? Ausente = sí (§17.8.2). Es lo único
 * que una empresa puede bajar a «basta con su número».
 */
export const saldoExigeIdentidad = (p: Pick<PerfilAsistente, "exigeVerificacionFuerte">): boolean => p.exigeVerificacionFuerte?.dar_saldo ?? true;

/**
 * `exigeVerificacionFuerte` para guardar: «con solo su número» vale únicamente con «Dar el saldo»
 * encendido. Al apagarlo vuelve a exigir identidad, para que encenderlo más adelante no herede en
 * silencio la opción menos cuidadosa.
 */
export function exigenciaDeSaldo(d: { acciones: { dar_saldo: boolean }; saldoConNumero: boolean }): NonNullable<PerfilAsistente["exigeVerificacionFuerte"]> {
    return { dar_saldo: !(d.acciones.dar_saldo && d.saldoConNumero) };
}

/** ¿El servidor manda lo avanzado de 3b (§17.8.2)? Uno anterior no: ni se muestra ni se guarda. */
export const conAvanzado = (a: Pick<AjustesAsistente, "presupuestoAutonomoPct" | "verificacionFuerteMin">): boolean =>
    a.presupuestoAutonomoPct !== undefined && a.verificacionFuerteMin !== undefined;

/** El perfil «defecto» (el primero): el que edita la pantalla simple. */
export function perfilDefecto(a: Pick<AjustesAsistente, "perfiles">): PerfilAsistente {
    return a.perfiles?.[0] ?? PERFIL_POR_DEFECTO;
}

/** Los perfiles con el «defecto» cambiado; los demás (por horario) viajan igual. */
export function conPerfilDefecto(a: Pick<AjustesAsistente, "perfiles">, cambio: Partial<Pick<PerfilAsistente, "acciones" | "limites" | "exigeVerificacionFuerte">>): PerfilAsistente[] {
    const [, ...resto] = a.perfiles ?? [];
    return [{ ...perfilDefecto(a), ...cambio }, ...resto];
}

/** ¿Hay un proveedor de IA conectado? (lo mismo que el servidor exige para activar, §16.1) */
export function proveedorConectado(a: Pick<AjustesAsistente, "proveedor" | "url" | "modelo" | "claveGuardadaEn">): boolean {
    if (a.proveedor === "anthropic") return a.claveGuardadaEn !== null;
    // El Servidor de IA no lleva dirección ni clave (la API guarda `url: ""`): basta el modelo (voz.md §4.11).
    if (a.proveedor === "servidor_ia") return a.modelo.trim() !== "";
    return a.url.trim() !== "" && a.modelo.trim() !== "";
}

/**
 * Cuerpo de `POST /asistente/ajustes/actualizar` con lo guardado, para cambiar solo una parte. Lo
 * de §17 es opcional (ausente = se deja como está): aquí no va, lo pone quien lo cambia.
 */
export function entradaGuardada(a: AjustesAsistente): EntradaAjustesAsistente {
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
    };
}

export const ESPERAS_MIN = [1, 2, 3, 5, 10, 15] as const;

const acciones = z.object({
    responder_dudas: z.boolean(),
    avisar_fallas: z.boolean(),
    crear_ticket: z.boolean(),
    agendar_visita: z.boolean(),
    recibir_boleta: z.boolean(),
    dar_saldo: z.boolean(),
    promesa_pago: z.boolean(),
    reconexion_temporal: z.boolean(),
});

/** Las tres preguntas de la pantalla principal. */
export const esquemaPrincipal = z.object({
    activo: z.boolean(),
    modoAutonomo: z.enum(["nunca", "sin_nadie", "siempre_primero"]),
    esperaMin: z.number().int().min(1, "Entre 1 y 60 minutos").max(60, "Entre 1 y 60 minutos"),
    acciones,
    /** Dar el saldo con solo el número del cliente (`exigeVerificacionFuerte.dar_saldo: false`). */
    saldoConNumero: z.boolean(),
    /** Modo por categoría (`id` como texto → modo). */
    modos: z.record(z.string(), z.enum(["responde_solo", "sugiere"])),
});
export type ValoresPrincipal = z.infer<typeof esquemaPrincipal>;

const soloAcciones = (a: AccionesAsistente): ValoresPrincipal["acciones"] => Object.fromEntries(CLAVES_ACCION.map((c) => [c, a[c]])) as ValoresPrincipal["acciones"];

export function valoresPrincipal(a: AjustesAsistente, categorias: readonly Categoria[]): ValoresPrincipal {
    const c = completar(a);
    return {
        activo: a.activo,
        modoAutonomo: c.modoAutonomo,
        esperaMin: c.esperaMin,
        acciones: soloAcciones(perfilDefecto(a).acciones),
        saldoConNumero: perfilDefecto(a).acciones.dar_saldo && !saldoExigeIdentidad(perfilDefecto(a)),
        modos: Object.fromEntries(categorias.map((x) => [String(x.id), modoDe(x)])),
    };
}

/** ¿Cambió algo de los ajustes (no de las categorías) en la pantalla principal? */
export function cambianAjustes(a: AjustesAsistente, d: ValoresPrincipal): boolean {
    const perfil = perfilDefecto(a);
    const guardadas = perfil.acciones;
    const c = completar(a);
    return (
        a.activo !== d.activo ||
        c.modoAutonomo !== d.modoAutonomo ||
        c.esperaMin !== d.esperaMin ||
        saldoExigeIdentidad(perfil) !== exigenciaDeSaldo(d).dar_saldo ||
        CLAVES_ACCION.some((x) => guardadas[x] !== d.acciones[x])
    );
}

/** Lo que la pantalla principal manda al guardar: lo de la conexión igual y lo suyo cambiado. */
export function entradaPrincipal(a: AjustesAsistente, d: ValoresPrincipal): EntradaAjustesAsistente {
    return {
        ...entradaGuardada(a),
        activo: d.activo,
        modoAutonomo: d.modoAutonomo,
        esperaMin: d.esperaMin,
        // Lo demás de `exigeVerificacionFuerte` (p. ej. `agendar_visita`, §17.10) queda como estaba.
        perfiles: conPerfilDefecto(a, { acciones: { ...d.acciones, pasar_a_persona: true }, exigeVerificacionFuerte: { ...perfilDefecto(a).exigeVerificacionFuerte, ...exigenciaDeSaldo(d) } }),
    };
}

export const entero = (min: number, max: number, mensaje: string) =>
    z
        .string()
        .trim()
        .refine((v) => /^\d+$/.test(v) && Number(v) >= min && Number(v) <= max, mensaje);

/** Límites (`/administracion/asistente/limites`): los del perfil «defecto», `maxTurnos` y lo apagado por cola. */
export const esquemaLimites = z.object({
    prioridadMaxima: z.enum(PRIORIDADES),
    diasAdelanteMax: entero(1, 14, "Entre 1 y 14 días"),
    maxTurnos: entero(2, 20, "Entre 2 y 20 mensajes"),
    /** Avanzado (§17.8.2): parte del tope del mes que puede gastar atendiendo solo. */
    presupuestoAutonomoPct: entero(1, 100, "Entre 1 y 100 %"),
    /** Avanzado (§17.8.2): minutos que vale la verificación de identidad. */
    verificacionFuerteMin: entero(5, 1440, "Entre 5 y 1,440 minutos (un día)"),
    /** Acciones apagadas por cola (`colaId` como texto → claves). */
    porCola: z.record(z.string(), z.array(z.enum(CLAVES_ACCION))),
});
export type ValoresLimites = z.infer<typeof esquemaLimites>;

const esAccion = (c: string): c is ClaveAccion => (CLAVES_ACCION as readonly string[]).includes(c);

export function valoresLimites(a: AjustesAsistente): ValoresLimites {
    const p = perfilDefecto(a);
    return {
        prioridadMaxima: p.limites.prioridadMaxima,
        diasAdelanteMax: String(p.limites.diasAdelanteMax),
        maxTurnos: String(completar(a).maxTurnos),
        // Solo para que el formulario valide: si el servidor no los manda no se muestran ni viajan.
        presupuestoAutonomoPct: String(a.presupuestoAutonomoPct ?? 70),
        verificacionFuerteMin: String(a.verificacionFuerteMin ?? 60),
        porCola: Object.fromEntries((a.porCola ?? []).map((c) => [String(c.colaId), (c.accionesApagadas ?? []).filter(esAccion)])),
    };
}

/**
 * `porCola` para guardar: lo marcado en el formulario más lo que el servidor ya tenía y el panel no
 * conoce (una acción nueva del servidor, o una cola que la pantalla no muestra): la lista reemplaza
 * a la guardada, así que lo desconocido se conserva tal cual. Solo viajan las colas con algo apagado.
 */
export function porColaDe(a: Pick<AjustesAsistente, "porCola">, marcadas: ValoresLimites["porCola"]): AccionesCola[] {
    const guardadas = new Map((a.porCola ?? []).map((c) => [String(c.colaId), c.accionesApagadas ?? []]));
    const colas = [...new Set([...guardadas.keys(), ...Object.keys(marcadas)])].sort((x, y) => Number(x) - Number(y));
    return colas
        .map((colaId) => {
            const desconocidas = (guardadas.get(colaId) ?? []).filter((c) => !esAccion(c));
            // Una cola que el formulario no trae queda como estaba.
            const conocidas = marcadas[colaId] ?? (guardadas.get(colaId) ?? []).filter(esAccion);
            return { colaId: Number(colaId), accionesApagadas: [...conocidas, ...desconocidas] };
        })
        .filter((c) => c.accionesApagadas.length > 0);
}

export function entradaLimites(a: AjustesAsistente, d: ValoresLimites): EntradaAjustesAsistente {
    return {
        ...entradaGuardada(a),
        maxTurnos: Number(d.maxTurnos),
        // Un servidor que no manda lo avanzado no recibe valores inventados (ausente = se deja como está).
        ...(conAvanzado(a) ? { presupuestoAutonomoPct: Number(d.presupuestoAutonomoPct), verificacionFuerteMin: Number(d.verificacionFuerteMin) } : {}),
        perfiles: conPerfilDefecto(a, { limites: { prioridadMaxima: d.prioridadMaxima, diasAdelanteMax: Number(d.diasAdelanteMax) } }),
        porCola: porColaDe(a, d.porCola),
    };
}

// --- Categorías (§17.2) ------------------------------------------------------------------------

export type Categoria = Schema<"CategoriaMensajes">;
export type EntradaCategoria = Schema<"CamposCategoriaMensajes">;
/** La categoría como viaja en la conversación y en «Pruébalo». */
export type CategoriaRef = Pick<Schema<"CategoriaMensajesRef">, "nombre" | "tono"> & { id: number | null };
export type ModoCategoria = NonNullable<Categoria["modo"]>;
/**
 * El modo de la categoría. Sin `mensajeria.asistente_configurar` el servidor manda solo la ficha
 * (`id`, `nombre`, `tono`, `activa`, `esOtro`) y `modo` llega ausente: se lee como «Sugiere».
 */
export const modoDe = (c: Pick<Categoria, "modo">): ModoCategoria => c.modo ?? "sugiere";
export type CondicionCategoria = NonNullable<Categoria["condicion"]>;
export type TonoCategoria = Categoria["tono"];
/** Qué hizo (o espera) el asistente en la conversación: una línea corta que escribe el servidor. */
export type ActividadAsistente = Schema<"ActividadAsistente">;

export const TONOS_CATEGORIA = ["info", "error", "ok", "aviso", "neutro"] as const satisfies readonly TonoCategoria[];
export const NOMBRE_TONO: Record<TonoCategoria, string> = { info: "Azul", error: "Rojo", ok: "Verde", aviso: "Naranja", neutro: "Gris" };
/** El tono de la categoría en los colores de Horus (los de las etiquetas: `CLASE_ETIQUETA`). */
export const COLOR_DE_TONO: Record<TonoCategoria, string> = { info: "info", error: "peligro", ok: "exito", aviso: "alerta", neutro: "neutro" };

const esTono = (t: string): t is TonoCategoria => (TONOS_CATEGORIA as readonly string[]).includes(t);
/** El tono del chip; uno que el panel no conoce se ve neutro. */
export const tonoDe = (tono: string): TonoCategoria => (esTono(tono) ? tono : "neutro");

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
    if (modoDe(c) === "sugiere") return NOMBRE_MODO.sugiere;
    if (c.condicion === "si_hay_falla") return "Responde solo si hay falla; si no, a la cola";
    if (c.condicion === "solo_verificados") return "Responde solo a verificados";
    return NOMBRE_MODO.responde_solo;
}

export const esquemaCategoria = z.object({
    nombre: z.string().trim().min(1, "Escribe el nombre de la categoría").max(40, "Hasta 40 caracteres"),
    descripcion: z.string().trim().max(300, "Hasta 300 caracteres"),
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

export function valoresCategoria(c?: Categoria): ValoresCategoria {
    return {
        nombre: c?.nombre ?? "",
        descripcion: c?.descripcion ?? "",
        tono: tonoDe(c?.tono ?? "info"),
        colaId: c?.colaId ? String(c.colaId) : "",
        slaPrimeraRespuestaMin: c?.slaPrimeraRespuestaMin ? String(c.slaPrimeraRespuestaMin) : "",
        modo: c ? modoDe(c) : "sugiere",
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
        modo: modoDe(c),
        condicion: c.condicion,
        manualIds: [...(c.manualIds ?? [])],
    };
}

export function entradaCategoria(d: ValoresCategoria): EntradaCategoria {
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
    };
}

// --- «Pruébalo antes de guardar» (§17.5) -------------------------------------------------------

export type PruebaMensaje = Schema<"MensajeProbado">;
export type HariaAsistente = PruebaMensaje["haria"];
/** Los ajustes del formulario **sin guardar** con los que se prueba. */
export type AjustesDePrueba = Schema<"AjustesPrueba">;

export const TEXTO_PRUEBA_MAX = 1000;

export const TEXTO_HARIA: Record<HariaAsistente, string> = {
    responde_solo: "Respondería solo",
    sugiere: "Le sugeriría al agente",
    pasa_a_persona: "Pasaría a una persona",
};

const TEXTO_MOTIVO: Record<NonNullable<PruebaMensaje["motivo"]>, string> = {
    modo_nunca: "Porque en la pregunta 1 está en «Nunca».",
    categoria_sugiere: "Porque este tipo de mensaje está en «Sugiere al agente».",
    sin_falla: "En la prueba no hay un cliente: no se sabe si hay una falla en su zona.",
    sin_verificar: "En la prueba no hay un cliente verificado.",
    pide_persona: "Porque el cliente pide hablar con una persona.",
    enojo: "Porque el cliente está molesto.",
    sin_respuesta: "Porque no encontró con qué responder.",
};

/** Por qué haría eso, en palabras; `null` si el servidor no da un motivo. */
export const textoMotivo = (p: Pick<PruebaMensaje, "motivo">): string | null => (p.motivo ? (TEXTO_MOTIVO[p.motivo] ?? null) : null);

/** «Respondería solo, con Manual «Planes FTTH y WISP»:» (las fuentes como las nombra el servidor). */
export function textoHaria(p: Pick<PruebaMensaje, "haria" | "fuentes" | "respuesta">): string {
    const base = TEXTO_HARIA[p.haria] ?? TEXTO_HARIA.pasa_a_persona;
    const fuentes = (p.fuentes ?? []).map((f) => f.etiqueta).filter(Boolean);
    const con = fuentes.length ? `, con ${fuentes.join(" y ")}` : "";
    return `${base}${con}${p.respuesta ? ":" : "."}`;
}

/** Lo que hay en el formulario (sin guardar) como `ajustes` de `probar-mensaje`. */
export function ajustesDePrueba(d: ValoresPrincipal, a: AjustesAsistente, categorias: readonly Categoria[]): AjustesDePrueba {
    return {
        modoAutonomo: d.modoAutonomo,
        perfil: { acciones: { ...d.acciones, pasar_a_persona: true }, limites: { ...perfilDefecto(a).limites }, exigeVerificacionFuerte: { ...perfilDefecto(a).exigeVerificacionFuerte, ...exigenciaDeSaldo(d) } },
        categorias: categorias
            .filter((c) => c.activa)
            .map((c) => {
                const modo = d.modos[String(c.id)] ?? modoDe(c);
                return { id: c.id, nombre: c.nombre, descripcion: c.descripcion, tono: c.tono, modo, condicion: modo === "responde_solo" ? c.condicion : null, manualIds: [...(c.manualIds ?? [])] };
            }),
    };
}

// --- Efecto en el SLA (§17.6) ------------------------------------------------------------------

export type EfectoAsistente = Schema<"EfectoAsistente">;

/** % de las atendidas que el asistente resolvió sin pasar a una persona; `null` sin conversaciones. */
export function porcentajeSinPersona(e: Pick<EfectoAsistente, "resueltasSinPersona" | "totalAtendidas">): number | null {
    return e.totalAtendidas > 0 ? Math.round((e.resueltasSinPersona / e.totalAtendidas) * 100) : null;
}

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

// --- Bandeja (§17.4) ---------------------------------------------------------------------------

/** ¿El mensaje lo escribió el asistente (y no una persona ni el bot de menú)? (3b) */
export function esDelAsistente(m: { autorTipo?: string | null | undefined; automatico?: string | null | undefined }): boolean {
    return m.autorTipo === "asistente_ia" || m.automatico === "asistente";
}

/** ¿El asistente dejó la conversación en la cola, esperando a una persona? (3b) */
export function asistenteEspera(c: { asistente?: { estado: string } | undefined; estado: string }): boolean {
    return c.asistente?.estado === "espera_persona" && c.estado !== "resuelta";
}

/** ¿El asistente está atendiendo solo la conversación? (3b: `asistente.estado`) */
export function asistenteAtiende(c: { asistente?: { estado: string } | undefined; estado: string }): boolean {
    return c.asistente?.estado === "atendiendo" && c.estado !== "resuelta";
}
