import { z } from "zod";
import {
    CLAVES_ACCION,
    entero,
    entradaGuardada,
    exigenciaDeSaldo,
    PERFIL_POR_DEFECTO,
    PRIORIDADES,
    saldoExigeIdentidad,
    type ClaveAccion,
    type PerfilAsistente,
} from "./fase3";
import type { AjustesAsistente, EntradaAjustesAsistente } from "./tipos";

/*
 * Fase 3b del asistente: atiende solo (contratos/conversaciones.md §17.8, §17.8.1 y §17.8.2).
 * Aquí va lo que el panel suma en 3b: las acciones de dinero y su verificación, los perfiles por
 * horario y la verificación del cliente en la bandeja. Tipos de `packages/contratos`; los esquemas
 * zod son el espejo del validador del servidor.
 */

// --- Acciones de dinero y verificación (§17.8.2) ------------------------------------------------

/** Las que tocan dinero o el servicio: apagadas de fábrica; cada empresa las enciende. */
export const ACCIONES_DINERO = ["recibir_boleta", "dar_saldo", "promesa_pago", "reconexion_temporal"] as const satisfies readonly ClaveAccion[];
export type AccionDinero = (typeof ACCIONES_DINERO)[number];
export const esDeDinero = (c: ClaveAccion): c is AccionDinero => (ACCIONES_DINERO as readonly string[]).includes(c);
/** Las demás, en el orden de la pantalla. */
export const ACCIONES_COMUNES: readonly ClaveAccion[] = CLAVES_ACCION.filter((c) => !esDeDinero(c));

/**
 * Qué le pide al cliente antes de cada acción de dinero, en llano. `recibir_boleta` acepta el
 * número; `dar_saldo` pide identidad salvo que la empresa lo baje; promesa y reconexión, siempre.
 */
export function requisitoDe(accion: AccionDinero, saldoConNumero: boolean): string {
    switch (accion) {
        case "recibir_boleta":
            return "Basta con que escriba desde su número. Caja la aprueba o la rechaza.";
        case "dar_saldo":
            return saldoConNumero ? "Basta con que escriba desde su número." : "Antes le pide su DPI o NIT.";
        case "promesa_pago":
            return "Antes le pide su DPI o NIT, siempre. Con las reglas de Argos.";
        case "reconexion_temporal":
            return "Antes le pide su DPI o NIT, siempre. Solo fuera de horario y con una boleta ya registrada.";
    }
}

// --- Perfiles por horario (§17.1) ---------------------------------------------------------------

export const MAX_PERFILES = 10;
/** Valor reservado de `franjas[].perfil`: en esa franja el asistente no actúa solo. */
export const SOLO_SUGERIR = "solo_sugerir";
const CODIGO = /^[a-z][a-z0-9_]{1,29}$/;

/** Todos los perfiles, con el «defecto» siempre primero (un servidor anterior no los manda). */
export function perfilesDe(a: Pick<AjustesAsistente, "perfiles">): PerfilAsistente[] {
    const lista = a.perfiles ?? [];
    return lista.length > 0 ? lista : [PERFIL_POR_DEFECTO];
}

export const esDefecto = (p: Pick<PerfilAsistente, "codigo">): boolean => p.codigo === PERFIL_POR_DEFECTO.codigo;

/**
 * El código de un perfil nuevo, sacado de su nombre («Noche y feriados» → `noche_y_feriados`): la
 * persona no lo escribe. Único entre los de la empresa y nunca uno reservado.
 */
export function codigoDePerfil(nombre: string, usados: readonly string[]): string {
    const base =
        nombre
            .normalize("NFD")
            .replace(/[̀-ͯ]/g, "")
            .toLowerCase()
            .replace(/[^a-z0-9]+/g, "_")
            .replace(/^[^a-z]+/, "")
            .replace(/_+$/, "")
            .slice(0, 26) || "perfil";
    const candidato = base.length < 2 ? `${base}_perfil` : base;
    const ocupados = new Set([...usados, SOLO_SUGERIR, PERFIL_POR_DEFECTO.codigo]);
    if (!ocupados.has(candidato) && CODIGO.test(candidato)) return candidato;
    for (let n = 2; n < 100; n++) {
        const otro = `${candidato}_${n}`;
        if (!ocupados.has(otro)) return otro;
    }
    return `${candidato}_x`;
}

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

export const esquemaPerfil = z.object({
    nombre: z.string().trim().min(1, "Escribe el nombre del perfil").max(40, "Hasta 40 caracteres"),
    acciones,
    saldoConNumero: z.boolean(),
    prioridadMaxima: z.enum(PRIORIDADES),
    diasAdelanteMax: entero(1, 14, "Entre 1 y 14 días"),
});
export type ValoresPerfil = z.infer<typeof esquemaPerfil>;

/** Un perfil nuevo empieza como el de fábrica: lo de dinero apagado. */
export function valoresPerfil(p?: PerfilAsistente): ValoresPerfil {
    const base = p ?? PERFIL_POR_DEFECTO;
    return {
        nombre: p?.nombre ?? "",
        acciones: Object.fromEntries(CLAVES_ACCION.map((c) => [c, base.acciones[c]])) as ValoresPerfil["acciones"],
        saldoConNumero: base.acciones.dar_saldo && !saldoExigeIdentidad(base),
        prioridadMaxima: base.limites.prioridadMaxima,
        diasAdelanteMax: String(base.limites.diasAdelanteMax),
    };
}

export function perfilDeValores(d: ValoresPerfil, codigo: string, guardado?: Pick<PerfilAsistente, "exigeVerificacionFuerte">): PerfilAsistente {
    return {
        codigo,
        nombre: d.nombre.trim(),
        acciones: { ...d.acciones, pasar_a_persona: true },
        // Lo que este formulario no edita (`agendar_visita`, §17.10) se conserva del perfil guardado.
        exigeVerificacionFuerte: { ...guardado?.exigeVerificacionFuerte, ...exigenciaDeSaldo(d) },
        limites: { prioridadMaxima: d.prioridadMaxima, diasAdelanteMax: Number(d.diasAdelanteMax) },
    };
}

/** La lista con el perfil cambiado (mismo código) o agregado al final. */
export function perfilesCon(a: Pick<AjustesAsistente, "perfiles">, perfil: PerfilAsistente): PerfilAsistente[] {
    const lista = perfilesDe(a);
    return lista.some((p) => p.codigo === perfil.codigo) ? lista.map((p) => (p.codigo === perfil.codigo ? perfil : p)) : [...lista, perfil];
}

/** La lista sin ese perfil. El «defecto» no se quita. */
export function perfilesSin(a: Pick<AjustesAsistente, "perfiles">, codigo: string): PerfilAsistente[] {
    return perfilesDe(a).filter((p) => esDefecto(p) || p.codigo !== codigo);
}

/** Cuerpo de `…/ajustes/actualizar` que cambia solo los perfiles (la lista reemplaza a la guardada). */
export function entradaPerfiles(a: AjustesAsistente, perfiles: PerfilAsistente[]): EntradaAjustesAsistente {
    return { ...entradaGuardada(a), perfiles };
}

/** «Responde dudas, avisa de fallas y crea tickets» → cuántas acciones tiene encendidas, en corto. */
export function resumenPerfil(p: PerfilAsistente): string {
    const encendidas = CLAVES_ACCION.filter((c) => p.acciones[c]);
    const dinero = encendidas.filter(esDeDinero).length;
    if (encendidas.length === 0) return "Solo pasa a una persona";
    const total = encendidas.length === 1 ? "1 acción encendida" : `${encendidas.length} acciones encendidas`;
    return dinero > 0 ? `${total} · ${dinero} de dinero` : `${total} · nada de dinero`;
}

export type OpcionPerfil = { valor: string; texto: string };

/**
 * Las opciones de «¿Qué hace el asistente en esta franja?» (horarios, §10.1): `""` = el perfil
 * general (sin `perfil`), `solo_sugerir` y los demás perfiles de la empresa. Un código guardado que
 * ya no existe se muestra como tal: el servidor lo trata como `solo_sugerir` (§17.8.2).
 */
export function opcionesDePerfil(perfiles: readonly Pick<PerfilAsistente, "codigo" | "nombre">[], guardado: string | null | undefined): OpcionPerfil[] {
    const general = perfiles.find(esDefecto)?.nombre ?? PERFIL_POR_DEFECTO.nombre;
    const lista: OpcionPerfil[] = [
        { valor: "", texto: `Lo de siempre («${general}»)` },
        { valor: SOLO_SUGERIR, texto: "Solo sugiere: no atiende solo" },
        ...perfiles.filter((p) => !esDefecto(p)).map((p) => ({ valor: p.codigo, texto: `Perfil «${p.nombre}»` })),
    ];
    if (guardado && guardado !== PERFIL_POR_DEFECTO.codigo && !lista.some((o) => o.valor === guardado)) lista.push({ valor: guardado, texto: "Un perfil que ya no existe (solo sugiere)" });
    return lista;
}

/** El valor del selector para lo guardado en la franja (`null`, ausente o `defecto` = lo de siempre). */
export const valorDePerfil = (perfil: string | null | undefined): string => (!perfil || perfil === PERFIL_POR_DEFECTO.codigo ? "" : perfil);

// --- Verificación del cliente en la bandeja (§17.8.2) --------------------------------------------

export type NivelVerificacion = "numero" | "fuerte";

/**
 * El nivel que dice el servidor en `contacto.verificacion`, y nada más: el panel nunca lo deduce
 * (puede bajar sola de `fuerte` a `numero` al vencer, y `confirmar-cliente` sobre una conversación
 * resuelta no da `fuerte`). `undefined` = el servidor no lo manda: no se muestra nada.
 */
export function nivelVerificacion(contacto: { verificacion?: string | null | undefined }): NivelVerificacion | null | undefined {
    if (contacto.verificacion === undefined) return undefined;
    return contacto.verificacion === "fuerte" || contacto.verificacion === "numero" ? contacto.verificacion : null;
}

export const TEXTO_VERIFICACION: Record<NivelVerificacion | "ninguna", { corto: string; largo: string }> = {
    fuerte: { corto: "Identidad verificada", largo: "Se verificó su identidad en esta conversación. Vale por un tiempo y después vuelve a «Su número coincide»." },
    numero: { corto: "Su número coincide", largo: "Escribe desde un número registrado a su nombre. Para promesas y reconexión hace falta verificar su identidad." },
    ninguna: { corto: "Sin verificar", largo: "Todavía no se sabe si es quien dice ser." },
};

/**
 * Qué hacer cuando falla la relectura periódica de una conversación con la identidad verificada:
 * sin sesión o sin permiso (401, 403) no tiene caso insistir; cualquier otra falla (red, 5xx) se
 * deja pasar y se prueba en el siguiente tic, con los datos que ya están en pantalla.
 */
export function trasFallarRelectura(error: unknown): "detener" | "reintentar" {
    const estado = typeof error === "object" && error !== null && "estado" in error ? (error as { estado: unknown }).estado : undefined;
    return estado === 401 || estado === 403 ? "detener" : "reintentar";
}

/**
 * Texto del mensaje de sistema `reconexion_pedida`: llega cuando Argos ya decidió (`detalle`:
 * aceptada o rechazada). Otro valor no se interpreta.
 */
export function textoReconexion(actor: string, estado: string | undefined): string {
    // Lo que decidió Argos va primero: en el teléfono la línea se corta por el final.
    if (estado === "aceptada") return `Reconexión temporal: Argos la aceptó (la pidió ${actor})`;
    if (estado === "rechazada") return `Reconexión temporal: Argos no la aceptó (la pidió ${actor})`;
    return `Reconexión temporal pedida por ${actor}`;
}

/** Resultados de un intento de verificar la identidad (`evento.detalle`, lista cerrada, §17.8.2). */
export type ResultadoIdentidad = "coincide" | "no_coincide" | "no_disponible" | "demasiados_intentos" | "supervisor";
const RESULTADOS_IDENTIDAD: readonly string[] = ["coincide", "no_coincide", "no_disponible", "demasiados_intentos", "supervisor"];

/** El resultado si es uno de los que el panel conoce; cualquier otro, `null` (se pinta neutro). */
export const resultadoIdentidad = (detalle: string | undefined): ResultadoIdentidad | null => (detalle && RESULTADOS_IDENTIDAD.includes(detalle) ? (detalle as ResultadoIdentidad) : null);

/** ¿El intento dejó la identidad verificada? Solo entonces el mensaje lleva el escudo con la marca. */
export const identidadQuedoVerificada = (detalle: string | undefined): boolean => detalle === "coincide" || detalle === "supervisor";

/**
 * Texto fijo del mensaje de sistema `identidad_verificada`, por resultado. Nunca el documento. Un
 * resultado que el panel no conoce no afirma nada.
 */
export function textoIdentidad(actor: string, detalle: string | undefined): string {
    switch (resultadoIdentidad(detalle)) {
        case "coincide":
            return `${actor} verificó la identidad del cliente`;
        case "supervisor":
            return `${actor} confirmó la identidad del cliente`;
        case "no_coincide":
            return "Identidad sin verificar: el documento no coincide";
        case "no_disponible":
            return "Identidad sin verificar: Argos no pudo comprobarla";
        case "demasiados_intentos":
            return "Identidad sin verificar: demasiados intentos";
        default:
            return "Intento de verificar la identidad";
    }
}

const MOTIVO_PASO: Record<string, string> = {
    cliente_lo_pidio: "el cliente pidió hablar con una persona",
    enojo: "el cliente está molesto",
    sin_respuesta: "no tenía con qué responder",
    herramienta_fallo: "una acción no se pudo completar",
    max_turnos: "llegó al máximo de mensajes sin resolver",
    tope: "se llegó al tope de uso del mes",
    asistente_apagado: "se apagó el modo de atender solo",
};

/**
 * Por qué el asistente pasó la conversación a una persona (`asistente.motivoPaso`, lista cerrada),
 * en llano y listo para ir después de «porque». Un motivo que el panel no conoce, o ninguno: `null`.
 */
export const textoMotivoPaso = (motivo: string | null | undefined): string | null => (motivo && Object.hasOwn(MOTIVO_PASO, motivo) ? (MOTIVO_PASO[motivo] ?? null) : null);

/**
 * Milisegundos que faltan para que venza la identidad verificada (`contacto.verificacionVenceEn`);
 * `null` si el nivel no es `fuerte` o el servidor no manda la fecha (queda la relectura de 30 s).
 */
export function faltaParaVencer(contacto: { verificacion?: string | null | undefined; verificacionVenceEn?: string | null | undefined }, ahora: number): number | null {
    if (contacto.verificacion !== "fuerte" || !contacto.verificacionVenceEn) return null;
    const vence = Date.parse(contacto.verificacionVenceEn);
    return Number.isFinite(vence) ? Math.max(0, vence - ahora) : null;
}
