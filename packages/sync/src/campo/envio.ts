// Barra de estado discreta y contadores de la barra inferior (fase 4): con señal y todo enviado es
// solo un punto junto a «Iris Campo»; crece con texto y una acción solo cuando hace falta (algo por
// enviar, sin señal con pendientes, un rechazo, la sesión venció o la empresa ya no vale).
// Compartido con la app nativa.
import type { Traslado } from "../contrato";
import type { OperacionTrasladoLocal } from "../modelo";
import type { Conexion, EstadoSync } from "../motor";
import type { TonoEstado } from "./textos";
import { porRecibir } from "./traslados";
import type { Pestana } from "./vista";

/** Conexión en palabras y su tono (el icono lo pone cada app). */
export const CONEXION_TEXTO: Record<Conexion, { tono: TonoEstado; texto: string }> = {
    en_linea: { tono: "success", texto: "En línea" },
    desconocida: { tono: "neutral", texto: "Conectando…" },
    sin_conexion: { tono: "warning", texto: "Sin conexión" },
    sin_sesion: { tono: "error", texto: "Sesión vencida" },
    sin_empresa: { tono: "error", texto: "Empresa no válida" },
};

export interface ResumenEnvio {
    /** Operaciones por enviar (incluye las que esperan archivos). */
    porEnviar: number;
    /** Archivos por subir que no fueron rechazados. */
    archivos: number;
    /** Operaciones y archivos rechazados: piden decidir. */
    rechazos: number;
    /** La barra se abre: hay algo por enviar, un rechazo, la sesión venció o la empresa no vale. */
    abierta: boolean;
    /** Texto del estado (también en el punto, para lectores de pantalla y pruebas). */
    texto: string;
}

type EstadoEnvio = Pick<EstadoSync, "conexion" | "sincronizando" | "pendientes" | "rechazos" | "esperandoAdjuntos" | "subidas">;

/** Qué mostrar: un punto si todo va bien; la barra con texto y acción solo cuando hace falta. */
export function resumenEnvio(estado: EstadoEnvio): ResumenEnvio {
    const rechazos = estado.rechazos.length + estado.subidas.rechazadas;
    const porEnviar = estado.pendientes + estado.esperandoAdjuntos.length;
    const archivos = estado.subidas.pendientes - estado.subidas.rechazadas;
    const bloqueo = estado.conexion === "sin_sesion" || estado.conexion === "sin_empresa";
    const abierta = bloqueo || rechazos > 0 || porEnviar > 0 || archivos > 0;
    const enviando = estado.sincronizando && estado.conexion !== "sin_conexion" && !bloqueo && (porEnviar > 0 || archivos > 0);
    return { porEnviar, archivos, rechazos, abierta, texto: enviando ? "Sincronizando…" : CONEXION_TEXTO[estado.conexion].texto };
}

/** Etiqueta del punto de estado: con la barra cerrada dice «…, todo enviado». */
export function etiquetaPunto(conexion: Conexion, r: ResumenEnvio): string {
    if (r.abierta) {
        return r.texto;
    }
    return conexion === "en_linea" ? "En línea, todo enviado" : `${CONEXION_TEXTO[conexion].texto}, todo enviado`;
}

/** Partes del detalle de la barra: «N pendientes por enviar», «N archivos por subir», «N rechazados». */
export function partesEnvio(r: Pick<ResumenEnvio, "porEnviar" | "archivos" | "rechazos">): { pendientes: string[]; rechazos: string | null } {
    const pendientes = [
        r.porEnviar > 0 ? `${r.porEnviar} ${r.porEnviar === 1 ? "pendiente" : "pendientes"} por enviar` : null,
        r.archivos > 0 ? `${r.archivos} ${r.archivos === 1 ? "archivo" : "archivos"} por subir` : null,
    ].filter((p): p is string => p !== null);
    return { pendientes, rechazos: r.rechazos > 0 ? `${r.rechazos} ${r.rechazos === 1 ? "rechazado" : "rechazados"}` : null };
}

/**
 * Contadores de la isla de menú: en «Chat», los mensajes sin leer (el punto). «Por recibir» ya no
 * está en la isla (su contador va en el mosaico de Hoy) y lo que falta por enviar va en los
 * indicadores de estado (`indicadoresEstado`), no en un botón.
 */
export function contadoresPestanas(noLeidosChat: number): Record<Pestana, number> {
    return { hoy: 0, chat: Math.max(0, noLeidosChat), agenda: 0, yo: 0 };
}

/** Traslados por recibir (0 mientras no se hayan leído: nunca un total inventado). */
export function cuentaPorRecibir(estado: { recepciones: readonly OperacionTrasladoLocal[] }, traslados: readonly Traslado[] | null): number {
    return traslados ? porRecibir(traslados, estado.recepciones).length : 0;
}

/** Lo que falta por enviar o decidir (operaciones, archivos y rechazos), para «Por enviar». */
export function totalPorEnviar(estado: EstadoEnvio): number {
    const r = resumenEnvio(estado);
    return r.porEnviar + Math.max(0, r.archivos) + r.rechazos;
}

/** Qué hace la píldora al tocarla. */
export type AccionAvisoIsla = "pendientes" | "entrar" | "empresa" | null;

export interface AvisoIsla {
    tono: TonoEstado;
    /** «Sin conexión», «Sincronizando…», «Sesión vencida»… (el de `resumenEnvio`). */
    estado: string;
    /** «2 pendientes por enviar · 1 rechazado» (vacío si no hay nada por enviar). */
    detalle: string;
    /** Hay un rechazo: pide decidir (en la píldora va con el icono de alerta y «Revisar»). */
    rechazo: boolean;
    accion: AccionAvisoIsla;
    /** Nombre accesible de la píldora completa. */
    etiqueta: string;
}

/**
 * Píldora de estado encima de la isla (la barra de estado discreta de la fase 4, en otra forma):
 * `null` con todo enviado (queda el punto del encabezado). Sale con algo por enviar, un rechazo, la
 * sesión vencida o la empresa no válida. El tono es el de la conexión; un rechazo lo vuelve error.
 */
export function avisoIsla(estado: EstadoEnvio): AvisoIsla | null {
    const r = resumenEnvio(estado);
    if (!r.abierta) {
        return null;
    }
    const { pendientes, rechazos } = partesEnvio(r);
    const detalle = [...pendientes, ...(rechazos ? [rechazos] : [])].join(" · ");
    const bloqueo = estado.conexion === "sin_sesion" ? "entrar" : estado.conexion === "sin_empresa" ? "empresa" : null;
    const accion: AccionAvisoIsla = detalle ? "pendientes" : bloqueo;
    const tono: TonoEstado = r.rechazos > 0 ? "error" : CONEXION_TEXTO[estado.conexion].tono === "success" ? "info" : CONEXION_TEXTO[estado.conexion].tono;
    const etiqueta = [r.texto, detalle].filter(Boolean).join(": ") + (accion === "pendientes" ? ". Ver detalle" : "");
    return { tono, estado: r.texto, detalle, rechazo: r.rechazos > 0, accion, etiqueta };
}

/** Los dos indicadores del encabezado (y las secciones de la hoja de estado). */
export type IdIndicador = "sincronizacion" | "ubicacion";

/**
 * La ubicación en vivo, en la forma que la usan los indicadores (cada app la arma con su rastreador):
 * `activa` (GPS encendido en turno), `apagada` (fuera de turno o fuera de curso: está bien),
 * `apagada_en_turno` (debería ir y no va: sin permiso, sin señal GPS o pausada por el técnico con
 * una orden en curso) o `sin_gps` (el teléfono no tiene).
 */
export type EstadoUbicacionIndicador = "activa" | "apagada" | "apagada_en_turno" | "sin_gps";

/**
 * La nube de sincronización (conexión y envíos juntos):
 * - `al_dia`: en línea y todo enviado (check, discreto);
 * - `pendientes`: en línea con algo por enviar (flecha y el número, en teal);
 * - `sin_senal`: sin señal y nada pendiente (nube tachada en naranja);
 * - `sin_senal_pendientes`: sin señal con pendientes (nube tachada en naranja con el número);
 * - `bloqueada`: la sesión venció o la empresa ya no vale (error: hay que entrar o elegir).
 * Un rechazo vuelve el tono error (hay que decidir).
 */
export type EstadoNube = "al_dia" | "pendientes" | "sin_senal" | "sin_senal_pendientes" | "bloqueada";

export interface Indicador {
    id: IdIndicador;
    /** El estado del dibujo: la nube (`EstadoNube`) o la ubicación (`EstadoUbicacionIndicador`). */
    forma: EstadoNube | EstadoUbicacionIndicador;
    /** `marca` = el teal de la marca; el resto, los tonos de estado. */
    tono: TonoEstado | "marca";
    /** Lo que suma cuando hay que actuar: un punto o un número; `null` = discreto. */
    marca: "punto" | number | null;
    /** Hay que hacer algo: el indicador resalta. */
    actuar: boolean;
    /** Texto corto del estado («Sin señal · 2 por enviar», «GPS activo»). */
    texto: string;
    /** Nombre accesible: «Sin señal, 2 por enviar, toca para ver el estado». */
    etiqueta: string;
}

/** Estado de la nube de sincronización (función pura, igual en la PWA y la app nativa). */
export function estadoSincronizacion(estado: EstadoEnvio): Indicador {
    const r = resumenEnvio(estado);
    const total = r.porEnviar + Math.max(0, r.archivos) + r.rechazos;
    const sinSenal = estado.conexion === "sin_conexion";
    const bloqueada = estado.conexion === "sin_sesion" || estado.conexion === "sin_empresa";
    const forma: EstadoNube = bloqueada ? "bloqueada" : sinSenal ? (total > 0 ? "sin_senal_pendientes" : "sin_senal") : total > 0 ? "pendientes" : "al_dia";
    const porEnviar = total > 0 ? `${total} por enviar` : null;
    const base = bloqueada ? CONEXION_TEXTO[estado.conexion].texto : sinSenal ? "Sin señal" : forma === "pendientes" ? (estado.sincronizando ? "Enviando" : "En línea") : "Todo enviado";
    const texto = [base, porEnviar].filter(Boolean).join(" · ");
    const tono: Indicador["tono"] = bloqueada || r.rechazos > 0 ? "error" : sinSenal ? "warning" : forma === "pendientes" ? "marca" : "success";
    return {
        id: "sincronizacion",
        forma,
        tono,
        marca: total > 0 ? total : bloqueada ? "punto" : null,
        actuar: forma !== "al_dia",
        texto,
        etiqueta: `${[base, porEnviar].filter(Boolean).join(", ")}, toca para ver el estado`,
    };
}

const TEXTO_UBICACION: Record<EstadoUbicacionIndicador, { tono: Indicador["tono"]; texto: string; actuar: boolean }> = {
    activa: { tono: "marca", texto: "GPS activo", actuar: false },
    apagada: { tono: "neutral", texto: "GPS apagado", actuar: false },
    apagada_en_turno: { tono: "warning", texto: "GPS apagado en turno", actuar: true },
    sin_gps: { tono: "neutral", texto: "Sin GPS", actuar: false },
};

/** El indicador de GPS: teal encendido, gris apagado, naranja con un punto si está apagado en turno. */
export function estadoGps(ubicacion: EstadoUbicacionIndicador): Indicador {
    const u = TEXTO_UBICACION[ubicacion];
    return { id: "ubicacion", forma: ubicacion, tono: u.tono, marca: u.actuar ? "punto" : null, actuar: u.actuar, texto: u.texto, etiqueta: `${u.texto}, toca para ver el estado` };
}

/** Los indicadores del encabezado, en orden: sincronización y GPS (luego va buscar). */
export function indicadoresEstado(estado: EstadoEnvio, ubicacion: EstadoUbicacionIndicador): Indicador[] {
    return [estadoSincronizacion(estado), estadoGps(ubicacion)];
}
