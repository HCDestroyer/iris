import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import {
    Ban,
    CalendarClock,
    CircleCheck,
    CircleDashed,
    CirclePause,
    CircleX,
    Hammer,
    Lock,
    MapPinCheck,
    PackageMinus,
    PlugZap,
    Repeat,
    Router,
    TriangleAlert,
    Truck,
    Wrench,
    Clock,
    LoaderCircle,
    type LucideIcon,
} from "lucide-react";

export type Orden = Schema<"Orden">;
export type OrdenDetalle = Schema<"OrdenDetalle">;
export type EstadoOrden = Orden["estado"];
export type TipoOrden = Orden["tipo"];
export type Pendiente = Schema<"Pendiente">;
export type EstadoPendiente = Pendiente["estado"];

type Presentacion = { tono: Tono; icono: LucideIcon; texto: string };

/*
 * Estados de la orden (skill horus-identity, estados.md): el flujo es `neutral`/`info`; solo el
 * resultado lleva urgencia (cerrada = success, fallida = error). Siempre icono + texto + color.
 */
export const ESTADOS_ORDEN: Record<EstadoOrden, Presentacion> = {
    pendiente: { tono: "neutral", icono: CircleDashed, texto: "Pendiente" },
    programada: { tono: "neutral", icono: CalendarClock, texto: "Programada" },
    en_camino: { tono: "info", icono: Truck, texto: "En camino" },
    en_sitio: { tono: "info", icono: MapPinCheck, texto: "En sitio" },
    pausada: { tono: "warning", icono: CirclePause, texto: "Pausada" },
    cerrada: { tono: "success", icono: CircleCheck, texto: "Cerrada" },
    fallida: { tono: "error", icono: CircleX, texto: "Fallida" },
    cancelada: { tono: "neutral", icono: Ban, texto: "Cancelada" },
};

export const ORDEN_ESTADOS_ORDEN: readonly EstadoOrden[] = ["pendiente", "programada", "en_camino", "en_sitio", "cerrada", "fallida", "cancelada"];
export const ESTADOS_ACTIVOS_ORDEN: readonly EstadoOrden[] = ["pendiente", "programada", "en_camino", "en_sitio"];
export const ESTADOS_FINALES: readonly EstadoOrden[] = ["cerrada", "fallida", "cancelada"];

export const TIPOS_ORDEN: Record<TipoOrden, { icono: LucideIcon; texto: string }> = {
    instalacion: { icono: Router, texto: "Instalación" },
    visita: { icono: Wrench, texto: "Visita técnica" },
    retiro: { icono: PackageMinus, texto: "Retiro" },
    cambio_equipo: { icono: Repeat, texto: "Cambio de equipo" },
    mantenimiento: { icono: Hammer, texto: "Mantenimiento" },
};

export const ORDEN_TIPOS: readonly TipoOrden[] = ["instalacion", "visita", "retiro", "cambio_equipo", "mantenimiento"];

/** Columnas del tablero: los tres estados finales comparten la última. */
export type Columna = { id: string; titulo: string; estados: readonly EstadoOrden[] };
export const COLUMNAS: readonly Columna[] = [
    { id: "pendiente", titulo: "Pendiente", estados: ["pendiente"] },
    { id: "programada", titulo: "Programada", estados: ["programada"] },
    { id: "en_camino", titulo: "En camino", estados: ["en_camino"] },
    { id: "en_sitio", titulo: "En sitio", estados: ["en_sitio"] },
    { id: "finalizada", titulo: "Cerrada, fallida o cancelada", estados: ESTADOS_FINALES },
];

export function columnaDe(estado: EstadoOrden): Columna {
    return COLUMNAS.find((c) => c.estados.includes(estado)) ?? (COLUMNAS[0] as Columna);
}

export const RESULTADOS: Record<NonNullable<Orden["resultado"]>, string> = {
    exitosa: "Exitosa",
    parcial: "Parcial",
    fallida: "Fallida",
    cancelada: "Cancelada",
};

export const MOTIVOS_FALLA: Record<Schema<"EntradaFallarOrden">["motivoFalla"], string> = {
    cliente_ausente: "Cliente ausente",
    sin_acceso: "Sin acceso al lugar",
    sin_cobertura: "Sin cobertura",
    falta_material: "Falta material",
    equipo_defectuoso: "Equipo defectuoso",
    clima: "Clima",
    cliente_rechaza: "El cliente rechaza el trabajo",
    otro: "Otro motivo",
};

export function textoMotivoFalla(codigo: string | null): string | undefined {
    if (!codigo) return undefined;
    return codigo in MOTIVOS_FALLA ? MOTIVOS_FALLA[codigo as keyof typeof MOTIVOS_FALLA] : codigo;
}

/*
 * Pendientes hacia Argos (I1): ¿requiere acción ya? (falló o agotó) → error; ¿conviene mirarlo?
 * (bloqueado) → warning; en cola o en curso → info; hecho → success.
 */
export const ESTADOS_PENDIENTE: Record<EstadoPendiente, Presentacion> = {
    pendiente: { tono: "info", icono: Clock, texto: "En cola" },
    en_curso: { tono: "info", icono: LoaderCircle, texto: "Enviando" },
    hecho: { tono: "success", icono: CircleCheck, texto: "Hecho" },
    fallido_negocio: { tono: "error", icono: CircleX, texto: "Argos lo rechazó" },
    agotado: { tono: "error", icono: PlugZap, texto: "Sin respuesta de Argos" },
    bloqueado: { tono: "warning", icono: Lock, texto: "Bloqueado" },
};

/** Estados que cuentan como «pendiente en Argos» (todo menos `hecho`). */
export const ESTADOS_PENDIENTE_ABIERTOS: readonly EstadoPendiente[] = ["fallido_negocio", "agotado", "bloqueado", "en_curso", "pendiente"];

/** De más a menos urgente: la marca del tablero muestra el peor. */
const GRAVEDAD: Record<EstadoPendiente, number> = { fallido_negocio: 5, agotado: 4, bloqueado: 3, en_curso: 2, pendiente: 1, hecho: 0 };

export const PRESENTACION_CONFLICTO: Presentacion = { tono: "warning", icono: TriangleAlert, texto: "Conflicto en Argos" };

export const OPERACIONES_PENDIENTE: Record<string, string> = {
    activar_instalacion: "Activar la instalación",
    asignar_comodato: "Asignar equipo en comodato",
    consumo: "Descontar materiales",
    devolucion: "Devolver equipo a bodega",
    perdida: "Registrar equipo perdido",
    retiro: "Retirar el servicio",
    cargo: "Generar cargo",
};

export function textoOperacion(operacion: string): string {
    return OPERACIONES_PENDIENTE[operacion] ?? operacion.replaceAll("_", " ");
}

/** Resumen de los pendientes no `hecho` de una orden: el peor estado y cuántos son. */
export type ResumenPendientes = { peor: EstadoPendiente; cuantos: number; conflicto: boolean };

export function resumirPendientes(pendientes: readonly Pendiente[]): ResumenPendientes | undefined {
    const abiertos = pendientes.filter((p) => p.estado !== "hecho");
    const conflicto = pendientes.some((p) => p.conflicto !== null);
    if (abiertos.length === 0) {
        return conflicto ? { peor: "hecho", cuantos: 0, conflicto } : undefined;
    }
    const peor = abiertos.reduce((a, b) => (GRAVEDAD[b.estado] > GRAVEDAD[a.estado] ? b : a)).estado;
    return { peor, cuantos: abiertos.length, conflicto };
}

/** Estado corto para la marca del tablero (tras «Pendiente en Argos ·»). */
const ESTADO_EN_MARCA: Record<EstadoPendiente, string> = {
    pendiente: "en cola",
    en_curso: "enviando",
    hecho: "hecho",
    fallido_negocio: "rechazado",
    agotado: "sin respuesta",
    bloqueado: "bloqueado",
};

/** Texto de la marca «Pendiente en Argos» con su estado. */
export function textoMarcaPendiente(r: ResumenPendientes): string {
    if (r.cuantos === 0) {
        return PRESENTACION_CONFLICTO.texto;
    }
    const estado = ESTADO_EN_MARCA[r.peor];
    return r.cuantos === 1 ? `Pendiente en Argos · ${estado}` : `${r.cuantos} pendientes en Argos · ${estado}`;
}

/** ¿Se puede reintentar a mano? (API: desde fallido_negocio, agotado o pendiente, con datos completos). */
export function reintentable(p: Pendiente): boolean {
    return (p.estado === "fallido_negocio" || p.estado === "agotado" || p.estado === "pendiente") && !p.datosIncompletos;
}

/** Último error en una línea (el detail de Argos puede ser largo). */
export function resumirError(error: string | null, max = 160): string | undefined {
    if (!error) return undefined;
    const limpio = error.replace(/\s+/g, " ").trim();
    if (!limpio) return undefined;
    const bloqueo = /^depende_de_(\d+)_fallido$/.exec(limpio);
    if (bloqueo) {
        return `Espera al pendiente ${bloqueo[1]}, que falló`;
    }
    return limpio.length > max ? `${limpio.slice(0, max - 1).trimEnd()}…` : limpio;
}

/** Código de equipo (MAC normalizada) legible. */
export function textoEquipo(e: { serie: string | null; mac: string | null; modelo: string }): string {
    return [e.modelo, e.serie ? `S/N ${e.serie}` : null, e.mac].filter(Boolean).join(" · ") || "Equipo sin datos";
}

export const CONDICIONES: Record<"buena" | "danada" | "perdido", Presentacion> = {
    buena: { tono: "success", icono: CircleCheck, texto: "Buena" },
    danada: { tono: "warning", icono: TriangleAlert, texto: "Dañada" },
    perdido: { tono: "error", icono: CircleX, texto: "Perdido" },
};
