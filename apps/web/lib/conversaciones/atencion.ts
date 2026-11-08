import { ErrorApi } from "@/lib/api/errores";
import type { Agente, EventoSistema, MotivoDestinatario, OrigenDisponibilidad } from "./tipos";

/**
 * 409 `conversacion_en_otra_empresa` (§15.3): en un número compartido, otra empresa ya atiende a
 * este contacto. Sale al iniciar, al reabrir y al responder una conversación resuelta o con
 * plantilla. Reintentar no sirve: se muestra su texto, sin «Reintentar».
 */
export const EN_OTRA_EMPRESA = {
    titulo: "La atiende otra empresa que usa este número",
    texto: "Este contacto tiene una conversación abierta en otra empresa que comparte este número. Hasta que la resuelvan, aquí no se puede reabrir ni responder.",
} as const;

export const esEnOtraEmpresa = (e: unknown): boolean => e instanceof ErrorApi && e.codigo === "conversacion_en_otra_empresa";

/*
 * «¿Alguien la atiende?», disponibilidad y bot (fase 1.1, contrato §14): textos y cálculos puros.
 */

/** Minutos que faltan para que conteste el bot (redondeado hacia arriba), o null si no aplica. */
export function minutosParaBot(botContestaEn: string | null | undefined, ahora: number): number | null {
    if (!botContestaEn) return null;
    const ms = Date.parse(botContestaEn) - ahora;
    if (Number.isNaN(ms)) return null;
    return Math.max(0, Math.ceil(ms / 60_000));
}

/**
 * «El bot contestará en 12 min» (o «Bot en 12 min»). El barrido del servidor corre cada minuto:
 * pasada la hora y sin respuesta del bot, «en un momento» (nunca un número negativo).
 */
export function textoCuentaRegresiva(minutos: number, corto = false): string {
    if (minutos <= 0) return corto ? "Bot en un momento" : "El bot contestará en un momento";
    return corto ? `Bot en ${minutos} min` : `El bot contestará en ${minutos} min`;
}

/** Fracción ya transcurrida de la espera (0 a 1) para la barrita del aviso. */
export function avanceEspera(esperaDesde: string | null | undefined, botContestaEn: string | null | undefined, ahora: number): number {
    const a = esperaDesde ? Date.parse(esperaDesde) : NaN;
    const b = botContestaEn ? Date.parse(botContestaEn) : NaN;
    if (Number.isNaN(a) || Number.isNaN(b) || b <= a) return 0;
    return Math.min(1, Math.max(0, (ahora - a) / (b - a)));
}

/** «Espera hace 18 min». */
export function textoEsperaDesde(esperaDesde: string | null | undefined, ahora: number): string | null {
    const a = esperaDesde ? Date.parse(esperaDesde) : NaN;
    if (Number.isNaN(a)) return null;
    const min = Math.max(0, Math.floor((ahora - a) / 60_000));
    return min < 1 ? "Espera desde hace un momento" : `Espera hace ${min} min`;
}

export const estaDisponible = (a: Pick<Agente, "disponibilidad">) => a.disponibilidad?.estado === "disponible";

/** ¿Supervisa esta cola? (global con `supervisa` o de la cola). */
export function supervisaCola(a: Pick<Agente, "supervisa" | "colasSupervisadas"> | undefined, colaId: number | null | undefined): boolean {
    if (!a) return false;
    return a.supervisa || (colaId !== null && colaId !== undefined && (a.colasSupervisadas ?? []).some((c) => c.id === colaId));
}

/** Personas de una cola (miembros, supervisores de la cola y globales), con los disponibles primero. */
export function personasDeCola(agentes: readonly Agente[], colaId: number | null | undefined): Agente[] {
    const lista = colaId ? agentes.filter((a) => supervisaCola(a, colaId) || (a.colas ?? []).some((c) => c.id === colaId)) : [...agentes];
    return lista.sort((x, y) => Number(estaDisponible(y)) - Number(estaDisponible(x)) || x.nombre.localeCompare(y.nombre, "es"));
}

export function textoDisponibles(n: number, cola: string): string {
    if (n === 0) return `Nadie disponible en ${cola}`;
    return n === 1 ? `1 disponible en ${cola}` : `${n} disponibles en ${cola}`;
}

/** Segunda línea de alguien «No disponible». */
export function textoMotivoNoDisponible(origen: OrigenDisponibilidad | undefined): string {
    switch (origen) {
        case "horario":
            return "Fuera de su horario";
        case "inactividad":
            return "Sin actividad";
        case "cierre_sesion":
            return "Cerró sesión";
        default:
            return "No disponible";
    }
}

/** «3 conversaciones abiertas», «Supervisora · 5 abiertas». */
export function textoCarga(a: { abiertas?: number }, supervisor: boolean): string | null {
    const n = a.abiertas;
    if (n === undefined) return supervisor ? "Supervisa la cola" : null;
    const abiertas = supervisor ? `${n} ${n === 1 ? "abierta" : "abiertas"}` : `${n} ${n === 1 ? "conversación abierta" : "conversaciones abiertas"}`;
    return supervisor ? `Supervisa · ${abiertas}` : abiertas;
}

/** Texto de los eventos de sistema de la fase 1.1 (los demás, `textoEvento`). Null si no es uno de ellos. */
export function textoEventoBot(e: EventoSistema): string | null {
    switch (e.tipo) {
        case "bot_atendio":
            return e.detalle === "sin_disponibles" ? "Sin agentes disponibles: contestó el bot" : `Nadie la tomó en ${e.minutos ?? 30} min: contestó el bot`;
        case "aviso_sin_atender": {
            const n = e.disponibles ?? 0;
            return `Se avisó a ${n === 1 ? "1 agente" : `${n} agentes`}${e.colaNombre ? ` de ${e.colaNombre}` : ""}`;
        }
        case "pasada":
            return `${e.actorNombre ?? "Iris"} la pasó a ${e.asignadoNombre ?? "otra persona"}${e.detalle === "no_disponible" ? " (no disponible)" : ""}`;
        case "pasada_a_empresa":
            // §15.3: `detalle` = empresa dueña; sin `actorNombre` (quien la recibe no ve a sus agentes).
            return `Pasada por ${e.detalle ?? "otra empresa"}`;
        case "menu_elegido":
            return `Eligió «${e.detalle ?? "una opción"}» en el menú${e.colaNombre ? `: pasó a ${e.colaNombre}` : ""}`;
        case "iniciada":
            return `${e.actorNombre ?? "Iris"} inició la conversación${e.plantilla ? ` con «${e.plantilla}»` : ""}`;
        default:
            return null;
    }
}

/** ¿Es un evento del bot? (icono y color de información). */
export const esEventoBot = (tipo: string) => tipo === "bot_atendio" || tipo === "aviso_sin_atender" || tipo === "menu_elegido";

/** Por qué no se le puede escribir a un destinatario («No acepta mensajes: se dio de baja»). */
export function textoMotivoDestinatario(motivo: MotivoDestinatario | null): string {
    switch (motivo) {
        case "baja":
            return "No acepta mensajes: se dio de baja";
        case "sin_permiso":
            return "No acepta mensajes: sin permiso de contacto";
        case "sin_contacto":
            return "Sin número de WhatsApp en Argos";
        case "cliente_fuera_de_empresa":
            return "No es cliente de esta empresa";
        case "conversacion_de_otro":
            return "Ya tiene una conversación con otra persona";
        case "conversacion_de_otra_cola":
            return "La atiende otra cola";
        case "conversacion_en_otra_empresa":
            return "La atiende otra empresa que usa este número";
        case "numero_no_permitido":
            return "Ese número de salida no es de tus colas";
        default:
            return "Acepta mensajes";
    }
}
