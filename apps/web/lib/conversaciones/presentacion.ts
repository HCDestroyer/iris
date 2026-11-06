import { ZONA } from "../fechas";
import { textoEventoBot } from "./atencion";
import type { CanalRef, Conversacion, EventoSistema, Mensaje } from "./tipos";

/*
 * Textos y colores de la bandeja (diseño aprobado por el PM, «Conversaciones»). Todo es texto
 * plano: nada de lo que escribe un cliente se interpreta como HTML.
 */

/** Tono de la identidad Horus de cada uso de número: el punto sobre el avatar y la píldora. */
export type TonoUso = "marca" | "alerta" | "info" | "exito" | "neutro";

const USOS: Record<string, { nombre: string; tono: TonoUso }> = {
    soporte: { nombre: "Soporte", tono: "marca" },
    cobros: { nombre: "Cobros", tono: "alerta" },
    avisos: { nombre: "Avisos", tono: "info" },
    ventas: { nombre: "Ventas", tono: "exito" },
};

/** Uso con que se pinta un número: el primero que no sea «avisos» (si tiene otro), o el primero. */
export function usoPrincipal(usos: readonly string[]): string | undefined {
    return usos.find((u) => u !== "avisos") ?? usos[0];
}

export function nombreUso(uso: string): string {
    return USOS[uso]?.nombre ?? uso;
}

export function tonoDeCanal(canal: Pick<CanalRef, "usos">): TonoUso {
    const uso = usoPrincipal(canal.usos ?? []);
    return (uso && USOS[uso]?.tono) || "neutro";
}

/** Clases del punto de color del número (sobre el avatar, en filtros y administración). */
export const CLASE_PUNTO: Record<TonoUso, string> = {
    marca: "bg-primary",
    alerta: "bg-warning",
    info: "bg-info",
    exito: "bg-success",
    neutro: "bg-muted-foreground",
};

/** Clases de la píldora con el nombre del número («Soporte»). */
export const CLASE_PILDORA: Record<TonoUso, string> = {
    marca: "bg-primary/10 text-primary",
    alerta: "bg-warning/10 text-warning",
    info: "bg-info/10 text-info",
    exito: "bg-success/10 text-success",
    neutro: "bg-elevated text-muted-foreground",
};

/** Color de una etiqueta (tokens de Horus). */
export const CLASE_ETIQUETA: Record<string, string> = {
    neutro: "bg-elevated text-foreground",
    marca: "bg-primary/10 text-primary",
    info: "bg-info/10 text-info",
    exito: "bg-success/10 text-success",
    alerta: "bg-warning/10 text-warning",
    peligro: "bg-error/10 text-error",
};

/** «+502 5512 0041» (E.164 de Guatemala en grupos de 4; otros países, tal cual). */
export function formatearNumero(numero: string): string {
    const m = /^\+502(\d{4})(\d{4})$/.exec(numero);
    return m ? `+502 ${m[1]} ${m[2]}` : numero;
}

/** Título para mostrar: si es el número (contacto desconocido), en grupos legibles. */
export function tituloVisible(c: { titulo: string; numero: string }): string {
    return c.titulo === c.numero ? formatearNumero(c.numero) : c.titulo;
}

/** Iniciales para el avatar: dos letras del nombre, o «+5» si es un número. */
export function iniciales(titulo: string): string {
    const limpio = titulo.trim();
    if (/^\+?\d/.test(limpio)) {
        return limpio.replace(/\s/g, "").slice(0, 2);
    }
    const palabras = limpio.split(/\s+/).filter((p) => /\p{L}/u.test(p));
    const letras = (palabras.length > 1 ? [palabras[0], palabras[palabras.length - 1]] : [palabras[0] ?? ""]).map((p) => (p ?? "").charAt(0));
    return (letras.join("") || "?").toUpperCase().slice(0, 2);
}

const horaFmt = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, hour: "2-digit", minute: "2-digit", hourCycle: "h23" });
const diaFmt = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, day: "numeric", month: "short" });
const diaLargoFmt = new Intl.DateTimeFormat("es-GT", { timeZone: ZONA, weekday: "long", day: "numeric", month: "long" });
const fechaFmt = new Intl.DateTimeFormat("en-CA", { timeZone: ZONA, year: "numeric", month: "2-digit", day: "2-digit" });

/** Fecha local de Guatemala `AAAA-MM-DD`. */
export function diaLocal(ms: number): string {
    return fechaFmt.format(ms);
}

export function hora(iso: string): string {
    const ms = Date.parse(iso);
    return Number.isNaN(ms) ? "" : horaFmt.format(ms);
}

/** Hora en la lista: «10:41» hoy, «ayer», o «2 oct». */
export function horaLista(iso: string, ahora: number): string {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) return "";
    const hoy = diaLocal(ahora);
    const dia = diaLocal(ms);
    if (dia === hoy) return horaFmt.format(ms);
    if (dia === diaLocal(ahora - 86_400_000)) return "ayer";
    return diaFmt.format(ms).replace(".", "");
}

/** Separador de día en el hilo: «Hoy», «Ayer» o «martes 3 de noviembre». */
export function separadorDia(iso: string, ahora: number): string {
    const ms = Date.parse(iso);
    if (Number.isNaN(ms)) return "";
    const dia = diaLocal(ms);
    if (dia === diaLocal(ahora)) return "Hoy";
    if (dia === diaLocal(ahora - 86_400_000)) return "Ayer";
    return diaLargoFmt.format(ms);
}

/**
 * Ventana de 24 h de WhatsApp: «Quedan 21 h para responder», «Quedan 35 min…», o `null` si está
 * cerrada (entonces solo se pueden mandar plantillas aprobadas).
 */
export function textoVentana(ventana: Conversacion["ventana"], ahora: number): { largo: string; corto: string } | null {
    if (!ventana.abierta || !ventana.cierraEn) return null;
    const resta = Date.parse(ventana.cierraEn) - ahora;
    if (Number.isNaN(resta) || resta <= 0) return null;
    const min = Math.floor(resta / 60_000);
    if (min < 60) {
        const m = Math.max(min, 1);
        return { largo: `Quedan ${m} min para responder`, corto: `${m} min` };
    }
    const h = Math.floor(min / 60);
    return { largo: `Quedan ${h} h para responder`, corto: `${h} h` };
}

/** Texto de un evento de sistema («Andrea creó TK-004812»). */
export function textoEvento(e: EventoSistema): string {
    // Fase 1.1: bot, aviso sin atender, menú, pasada e iniciada.
    const nuevo = textoEventoBot(e);
    if (nuevo) return nuevo;
    const actor = e.actorNombre ?? "Iris";
    const ref = e.referencia ?? "";
    switch (e.tipo) {
        case "conversacion_creada":
            return e.colaNombre ? `Llegó a ${e.colaNombre}` : "Conversación nueva";
        case "asignada":
            return e.asignadoNombre ? `${actor} asignó a ${e.asignadoNombre}` : `${actor} quitó la asignación`;
        case "tomada":
            return `${actor} tomó la conversación`;
        case "transferida":
            return `${actor} la pasó a ${e.colaNombre ?? "otra cola"}`;
        case "resuelta":
            return `${actor} la resolvió`;
        case "reabierta":
            return e.actorNombre ? `${actor} la reabrió` : "El cliente volvió a escribir";
        case "etiquetada":
            return `${actor} cambió las etiquetas`;
        case "cliente_asociado":
            return e.actorNombre ? `${actor} asoció el cliente${ref ? ` ${ref}` : ""}` : "Cliente asociado por su número";
        case "cliente_confirmado":
            return `${actor} confirmó el cliente`;
        case "contacto_iris_creado":
            return `${actor} guardó el contacto${ref ? ` ${ref}` : ""}`;
        case "ticket_creado":
            return `${actor} creó ${ref || "un ticket"}`;
        case "orden_creada":
            return `${actor} creó ${ref || "una orden"}`;
        case "prospecto_creado":
            return `${actor} creó el prospecto${ref ? ` ${ref}` : ""}`;
        case "boleta_registrada":
            return `${actor} mandó la boleta a Argos`;
        case "boleta_aprobada":
            return "Caja aprobó la boleta";
        case "boleta_rechazada":
        case "boleta_fallida":
            return `Argos no aceptó la boleta${e.detalle ? `: ${e.detalle}` : ""}`;
        case "promesa_creada":
            return `${actor} registró una promesa de pago`;
        case "respondio_aviso":
            return `Respondió al aviso «${e.plantilla ?? "aviso"}»`;
        case "fuera_de_horario":
            return "Respuesta automática: fuera de horario";
        case "guardia_avisada":
            return `Se avisó a la guardia${e.asignadoNombre ? ` (${e.asignadoNombre})` : ""}`;
        case "manual_enviado":
            return `${actor} envió el manual${ref ? ` «${ref}»` : ""}`;
        default:
            return e.detalle ?? e.tipo.replace(/_/g, " ");
    }
}

/** Última línea de la fila de la lista. */
export function textoUltimo(u: Conversacion["ultimoMensaje"]): string {
    if (!u) return "Sin mensajes";
    if (u.direccion === "sistema") return u.evento ? textoEvento({ tipo: u.evento }) : (u.texto ?? "");
    const cuerpo = u.texto?.trim() || adjuntoCorto(u.tipo);
    // Fase 1.1: lo que mandó el bot o la respuesta fuera de horario no es «Tú».
    if (u.direccion === "saliente" && u.automatico === "bot") return `Bot: ${cuerpo}`;
    if (u.direccion === "saliente" && u.automatico === "fuera_de_horario") return `Automático: ${cuerpo}`;
    if (u.direccion === "saliente") return `Tú: ${cuerpo}`;
    if (u.direccion === "nota") return `Nota: ${cuerpo}`;
    return cuerpo;
}

function adjuntoCorto(tipo: string): string {
    switch (tipo) {
        case "imagen":
            return "Foto";
        case "documento":
            return "Documento";
        case "audio":
            return "Audio";
        case "video":
            return "Video";
        case "ubicacion":
            return "Ubicación";
        case "contacto":
            return "Contacto";
        case "plantilla":
            return "Plantilla";
        default:
            return "Mensaje";
    }
}

/** Estado de entrega de un saliente, en palabras. */
export function textoEntrega(m: Pick<Mensaje, "estadoEntrega" | "motivoFalla">): string {
    switch (m.estadoEntrega) {
        case "en_cola":
            return "en cola";
        case "enviado":
            return "enviado";
        case "entregado":
            return "entregado";
        case "leido":
            return "leído";
        case "fallido":
            return `no se envió${m.motivoFalla ? ` (${motivoFalla(m.motivoFalla)})` : ""}`;
        default:
            return "";
    }
}

export function motivoFalla(motivo: string): string {
    switch (motivo) {
        case "numero_invalido":
            return "número inválido";
        case "rechazado_por_canal":
            return "WhatsApp lo rechazó";
        case "fuera_de_ventana":
            return "ventana cerrada";
        case "agotado":
            return "sin respuesta de WhatsApp";
        case "sin_permiso":
            return "sin permiso de contacto";
        default:
            return motivo.replace(/_/g, " ");
    }
}

export function tamanoLegible(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1).replace(".", ",")} MB`;
}

/** Texto de una respuesta rápida con `{{nombre}}` reemplazado por el primer nombre del contacto. */
export function aplicarRespuesta(texto: string, titulo: string): string {
    const primero = /^\+?\d/.test(titulo.trim()) ? "" : (titulo.trim().split(/\s+/)[0] ?? "");
    return texto.replace(/\{\{\s*nombre\s*\}\}/g, primero).replace(/ {2,}/g, " ").replace(/ ([,.!?])/g, "$1");
}

/** Clases del punto de SLA: discreto si va en tiempo; crece de color cuando hay que actuar. */
export function puntoSla(sla: Conversacion["sla"]): { clase: string; texto: string } {
    switch (sla.estado) {
        case "vencido":
            return { clase: "bg-error", texto: "SLA vencido" };
        case "por_vencer":
            return { clase: "bg-warning", texto: "SLA por vencer" };
        case "en_tiempo":
            return { clase: "bg-success", texto: "En tiempo" };
        default:
            return { clase: "bg-border", texto: "Sin pendiente" };
    }
}
