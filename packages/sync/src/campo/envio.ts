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
 * Contadores de la barra inferior: en «Pendientes», lo que falta por enviar o decidir; en «Por
 * recibir», los traslados por recibir (0 mientras no se hayan leído: nunca un total inventado).
 */
export function contadoresPestanas(
    estado: EstadoEnvio & { recepciones: readonly OperacionTrasladoLocal[] },
    traslados: readonly Traslado[] | null,
): Record<Pestana, number> {
    const r = resumenEnvio(estado);
    return {
        hoy: 0,
        recibir: traslados ? porRecibir(traslados, estado.recepciones).length : 0,
        pendientes: r.porEnviar + Math.max(0, r.archivos) + r.rechazos,
        yo: 0,
    };
}
