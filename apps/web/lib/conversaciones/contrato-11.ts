import type { Schema } from "@iris/contratos";

/*
 * PROVISIONAL (fase 1.1 de la bandeja, contratos/conversaciones.md §14). Mientras I1 no publique en
 * `packages/contratos` los tipos regenerados, aquí está la forma del texto del contrato. Al llegar
 * los tipos reales este archivo se borra y `tipos.ts` pasa a alias de `Schema<…>`.
 */

export type EstadoDisponibilidad = "disponible" | "no_disponible";
export type OrigenDisponibilidad = "manual" | "cierre_sesion" | "inactividad" | "horario";

/** `GET /conversaciones/disponibilidad` (§14.5): la mía. */
export type Disponibilidad = {
    estado: EstadoDisponibilidad;
    origen: OrigenDisponibilidad;
    desde: string | null;
    /** Cierre más tardío de las franjas abiertas al ponerme disponible (o al último latido). */
    horarioHasta: string | null;
    minutosInactividad: number;
    version: number;
};

/** `GET /conversaciones/agentes` (§14.1, §14.5). */
export type AgenteDisponible = Schema<"AgenteConversaciones"> & {
    colasSupervisadas: Schema<"ColaRef">[];
    disponibilidad: { estado: EstadoDisponibilidad; origen: OrigenDisponibilidad; desde: string | null };
    /** Conversaciones activas que tiene. */
    abiertas: number;
};

/** Cola con supervisores, disponibles y los minutos del bot (null = los del número). */
export type ColaConSupervisores = Schema<"ColaConversaciones"> & {
    supervisores: Schema<"UsuarioRef">[];
    disponibles: number[];
    botSinConfirmacionMin: number | null;
    botSinDisponiblesMin: number | null;
};

export type OpcionMenuBot = { texto: string; colaId: number; etiquetaIds: number[] };

/** Número: «Cuando no hay agentes» (§14.6). */
export type BotNumero = {
    activo: boolean;
    atiende: "bot" | "agente_autonomo";
    minutosSinConfirmacion: number;
    minutosSinDisponibles: number;
    saludo: string;
    menu: OpcionMenuBot[];
};

export type NumeroConBot = Schema<"NumeroConversaciones"> & { bot?: BotNumero; iniciar?: boolean };

/** Ajustes de la bandeja de la empresa (§14.5). */
export type AjustesBandeja = { minutosInactividad: number; version: number };

/** Espera de una conversación sin asignar (§14.6). */
export type Espera = {
    disponibles: Schema<"UsuarioRef">[];
    totalDisponibles: number;
    botContestaEn: string | null;
    botMotivo: "sin_confirmacion" | "sin_disponibles" | null;
    botAtendio: boolean;
};

export type ConversacionConEspera = Schema<"ConversacionCliente"> & { espera?: Espera };

export type MiembroEquipo = { id: number; nombre: string; rol: "miembro" | "supervisor"; disponible: boolean };

export type DetalleConEquipo = Schema<"ConversacionClienteDetalle"> & { espera?: Espera; supervisores?: Schema<"UsuarioRef">[]; equipo?: MiembroEquipo[] };

export type MensajeAutomatico = Schema<"MensajeConversacion"> & { automatico?: "bot" | "fuera_de_horario" | null };

export type EventoSistemaBot = Schema<"EventoSistema"> & { minutos?: number; disponibles?: number };

export type MotivoDestinatario = "sin_contacto" | "sin_permiso" | "baja" | "cliente_fuera_de_empresa" | "conversacion_de_otro" | "numero_no_permitido";

/**
 * `GET /conversaciones/destinatarios?q=&canalId=&limit=` (clientes de Argos y contactos de Iris
 * juntos) y `GET /conversaciones/destinatario` (§14.3).
 */
export type Destinatario = {
    tipo: "cliente" | "contacto_iris";
    id: number;
    nombre: string;
    codigo: string | null;
    /** Texto secundario (tipo de contacto, plan…). */
    detalle: string | null;
    numero: string | null;
    puede: boolean;
    motivo: MotivoDestinatario | null;
    /** Si ya hay una conversación de ese número en ese canal. */
    conversacionId: number | null;
};

/** Respuesta de `POST /conversaciones/iniciar`. */
export type ConversacionIniciada = { conversacion: DetalleConEquipo; mensaje: Schema<"MensajeConversacion">; nueva: boolean };

/** Respuesta de `POST /conversaciones/{id}/pasar`. */
export type ConversacionPasada = { conversacion: DetalleConEquipo; destino: Schema<"UsuarioRef">; destinoDisponible: boolean };

/** `GET /conversaciones/canales/{id}/plantillas` (pedido a I1): las aprobadas en ese número. */
export type PlantillaCanal = {
    codigo: string;
    nombre: string;
    categoria?: string | null;
    variables: { nombre: string; formato: string; obligatoria: boolean }[];
    vistaPrevia: string;
};
