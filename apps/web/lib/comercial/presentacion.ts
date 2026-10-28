import type { Schema } from "@iris/contratos";
import type { Tono } from "@iris/ui";
import {
    Ban,
    CircleCheck,
    CircleDashed,
    CircleQuestionMark,
    CircleX,
    Clock,
    FilePenLine,
    FileText,
    HardHat,
    LoaderCircle,
    Lock,
    MessageSquare,
    NotebookPen,
    Phone,
    PhoneCall,
    PlugZap,
    Radar,
    Trophy,
    UserPlus,
    Users,
    type LucideIcon,
} from "lucide-react";

export type Prospecto = Schema<"Prospecto">;
export type ProspectoDetalle = Schema<"ProspectoDetalle">;
export type EstadoProspecto = Prospecto["estado"];
export type OrigenProspecto = Prospecto["origen"];
export type Factibilidad = Schema<"Factibilidad">;
export type ResultadoFactibilidad = Factibilidad["resultado"];
export type Cotizacion = Schema<"Cotizacion">;
export type LineaCotizacion = Schema<"LineaCotizacion">;
export type TipoLinea = LineaCotizacion["tipo"];
export type Contrato = Schema<"Contrato">;
export type Documento = Schema<"DocumentoPDF">;
export type Alta = Schema<"Alta">;
export type PendienteArgos = Schema<"PendienteArgos">;
export type Actividad = Schema<"Actividad">;
export type TipoActividad = Actividad["tipo"];
export type EventoProspecto = Schema<"EventoProspecto">;
export type Cobertura = Schema<"Cobertura">;
export type PlanesDisponibles = Schema<"PlanesDisponibles">;
export type Acciones = Schema<"AccionesDisponible">;

export type Presentacion = { tono: Tono; icono: LucideIcon; texto: string };

/*
 * Estados del embudo (horus-identity): el avance es neutral/info; solo el desenlace urge
 * (ganado = success, perdido = error). Siempre icono + texto + color.
 */
export const ESTADOS_PROSPECTO: Record<EstadoProspecto, Presentacion> = {
    nuevo: { tono: "neutral", icono: UserPlus, texto: "Nuevo" },
    contactado: { tono: "neutral", icono: PhoneCall, texto: "Contactado" },
    factibilidad: { tono: "info", icono: Radar, texto: "Factibilidad" },
    cotizado: { tono: "info", icono: FileText, texto: "Cotizado" },
    contrato: { tono: "info", icono: FilePenLine, texto: "Contrato" },
    ganado: { tono: "success", icono: Trophy, texto: "Ganado" },
    perdido: { tono: "error", icono: CircleX, texto: "Perdido" },
};

/** Orden de las columnas del embudo. */
export const ORDEN_ESTADOS: readonly EstadoProspecto[] = ["nuevo", "contactado", "factibilidad", "cotizado", "contrato", "ganado", "perdido"];
/** Pasos del embudo (sin `perdido`, que es una salida). */
export const PASOS_EMBUDO: readonly EstadoProspecto[] = ["nuevo", "contactado", "factibilidad", "cotizado", "contrato", "ganado"];

export const ORIGENES: Record<OrigenProspecto, string> = {
    panel: "Panel",
    web: "Sitio web",
    referido: "Referido",
    argos: "Argos",
    otro: "Otro",
};
/** Orígenes que una persona puede elegir al crear (`argos` es solo de la integración). */
export const ORIGENES_ELEGIBLES: readonly OrigenProspecto[] = ["panel", "web", "referido", "otro"];

export const RESULTADOS_FACTIBILIDAD: Record<ResultadoFactibilidad, Presentacion> = {
    viable: { tono: "success", icono: CircleCheck, texto: "Viable" },
    requiere_obra: { tono: "warning", icono: HardHat, texto: "Requiere obra" },
    no_viable: { tono: "error", icono: CircleX, texto: "No viable" },
    indeterminado: { tono: "neutral", icono: CircleQuestionMark, texto: "Indeterminado" },
};

/** Por qué la factibilidad quedó indeterminada (o no hay cobertura que dibujar). */
const MOTIVOS: Record<string, string> = {
    sin_conector: "Iris no está conectado con Argos, así que no conoce los routers de la red.",
    conector_apagado: "La conexión con Argos está apagada: no hay datos de la red actualizados.",
    sin_coordenadas: "El prospecto no tiene coordenadas: agrega su ubicación para medir la distancia.",
    sin_empresa: "El prospecto no tiene empresa: no se sabe de qué red es.",
    sin_instantanea: "Todavía no se leyeron los routers de Argos para esta empresa.",
    sin_routers_con_coordenadas: "Ningún router de la empresa tiene coordenadas en Argos.",
    planta_no_disponible: "El servicio de planta externa no respondió.",
    argos_no_disponible: "Argos no respondió.",
};

export function textoMotivo(motivo: string | null | undefined): string {
    if (!motivo) return "Sin datos suficientes.";
    return MOTIVOS[motivo] ?? motivo.replaceAll("_", " ");
}

/** Metros legibles: «850 m» o «1.2 km». */
export function textoDistancia(m: number | null | undefined): string | undefined {
    if (m === null || m === undefined || !Number.isFinite(m)) return undefined;
    if (m < 1000) return `${Math.round(m)} m`;
    return `${(m / 1000).toLocaleString("es-GT", { maximumFractionDigits: 1 })} km`;
}

export const TIPOS_LINEA: Record<TipoLinea, string> = {
    plan: "Plan",
    instalacion: "Instalación",
    equipo: "Equipo",
    descuento: "Descuento",
    otro: "Otro",
};

export const TIPOS_ACTIVIDAD: Record<TipoActividad, { icono: LucideIcon; texto: string }> = {
    llamada: { icono: Phone, texto: "Llamada" },
    visita: { icono: Users, texto: "Visita" },
    nota: { icono: NotebookPen, texto: "Nota" },
    mensaje: { icono: MessageSquare, texto: "Mensaje" },
};

export const ESTADOS_COTIZACION: Record<Cotizacion["estado"], Presentacion> = {
    vigente: { tono: "info", icono: Clock, texto: "Vigente" },
    aceptada: { tono: "success", icono: CircleCheck, texto: "Aceptada" },
    reemplazada: { tono: "neutral", icono: Ban, texto: "Reemplazada" },
};
export const COTIZACION_VENCIDA: Presentacion = { tono: "warning", icono: Clock, texto: "Vencida" };

export const ESTADOS_CONTRATO: Record<Contrato["estado"], Presentacion> = {
    borrador: { tono: "neutral", icono: CircleDashed, texto: "Borrador" },
    firmado: { tono: "success", icono: FilePenLine, texto: "Firmado" },
    anulado: { tono: "neutral", icono: Ban, texto: "Anulado" },
};

export const ESTADOS_ALTA: Record<Alta["estado"], Presentacion> = {
    esperando_cliente: { tono: "info", icono: LoaderCircle, texto: "Creando el cliente en Argos" },
    esperando_servicio: { tono: "info", icono: LoaderCircle, texto: "Creando el servicio en Argos" },
    completa: { tono: "success", icono: CircleCheck, texto: "Alta completa" },
};

/** Estados de un pendiente del alta (texto libre en el contrato: lo desconocido se muestra tal cual). */
const ESTADOS_PENDIENTE: Record<string, Presentacion> = {
    pendiente: { tono: "info", icono: Clock, texto: "En cola" },
    en_curso: { tono: "info", icono: LoaderCircle, texto: "Enviando" },
    hecho: { tono: "success", icono: CircleCheck, texto: "Hecho" },
    fallido_negocio: { tono: "error", icono: CircleX, texto: "Argos lo rechazó" },
    agotado: { tono: "error", icono: PlugZap, texto: "Sin respuesta de Argos" },
    bloqueado: { tono: "warning", icono: Lock, texto: "Bloqueado" },
};

export function presentacionPendiente(estado: string): Presentacion {
    return ESTADOS_PENDIENTE[estado] ?? { tono: "neutral", icono: CircleQuestionMark, texto: estado.replaceAll("_", " ") };
}

/** Un pendiente que el usuario puede reintentar (`integraciones.reintentar`). */
export function pendienteReintentable(estado: string): boolean {
    return estado === "fallido_negocio" || estado === "agotado";
}

export const OPERACIONES_ALTA: Record<string, string> = {
    crear_cliente: "Crear el cliente en Argos",
    crear_servicio: "Crear el servicio en Argos",
};

/** Etiquetas de los eventos del historial (lo desconocido, legible). */
const EVENTOS: Record<string, string> = {
    creado: "Prospecto creado",
    estado: "Cambio de estado",
    asignado: "Vendedor asignado",
    factibilidad: "Factibilidad evaluada",
    cotizacion: "Cotización creada",
    cotizacion_aceptada: "Cotización aceptada",
    contrato: "Contrato generado",
    contrato_firmado: "Contrato firmado",
    ganado: "Ganado",
    alta: "Alta en Argos",
};

export function textoEvento(tipo: string): string {
    return EVENTOS[tipo] ?? tipo.replaceAll("_", " ").replace(/^./, (c) => c.toUpperCase());
}

/** Texto de un estado del embudo (o del historial: también resultados de factibilidad). */
export function textoEstadoCualquiera(valor: string | null): string | undefined {
    if (!valor) return undefined;
    if (valor in ESTADOS_PROSPECTO) return ESTADOS_PROSPECTO[valor as EstadoProspecto].texto;
    if (valor in RESULTADOS_FACTIBILIDAD) return RESULTADOS_FACTIBILIDAD[valor as ResultadoFactibilidad].texto;
    return valor.replaceAll("_", " ");
}

/**
 * Monto de la API (texto decimal, «250.00») con su moneda. Solo da formato a lo que calculó la
 * API: el panel nunca suma montos.
 */
export function textoMonto(monto: string, moneda = "GTQ"): string {
    const n = Number(monto);
    if (!Number.isFinite(n)) return `${moneda} ${monto}`;
    try {
        return new Intl.NumberFormat("es-GT", { style: "currency", currency: moneda, minimumFractionDigits: 2 }).format(n);
    } catch {
        return `${moneda} ${n.toFixed(2)}`;
    }
}

export type Requisito = { clave: "factibilidad" | "cotizacion" | "contrato"; texto: string; cumplido: boolean; detalle?: string };

/**
 * Qué pide la API para ganar (README de comercial): contrato firmado, su cotización aceptada y la
 * última factibilidad viable o con obra. Solo explica: el botón lo habilita `acciones.ganar`.
 */
export function requisitosGanar(d: Pick<ProspectoDetalle, "factibilidad" | "cotizacion" | "contrato">): Requisito[] {
    const f = d.factibilidad;
    const factible = f !== undefined && (f.resultado === "viable" || f.resultado === "requiere_obra");
    return [
        {
            clave: "factibilidad",
            texto: "Factibilidad viable o con obra confirmada",
            cumplido: factible,
            ...(f ? { detalle: RESULTADOS_FACTIBILIDAD[f.resultado].texto } : { detalle: "Sin evaluar" }),
        },
        {
            clave: "cotizacion",
            texto: "Cotización aceptada por el cliente",
            cumplido: d.cotizacion?.estado === "aceptada",
            detalle: d.cotizacion ? (d.cotizacion.vencida && d.cotizacion.estado === "vigente" ? "Vencida" : ESTADOS_COTIZACION[d.cotizacion.estado].texto) : "Sin cotización",
        },
        {
            clave: "contrato",
            texto: "Contrato firmado",
            cumplido: d.contrato?.estado === "firmado",
            detalle: d.contrato ? ESTADOS_CONTRATO[d.contrato.estado].texto : "Sin contrato",
        },
    ];
}
