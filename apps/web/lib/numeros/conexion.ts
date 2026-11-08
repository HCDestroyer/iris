import type { Tono } from "@iris/ui";
import { formatearNumero } from "@/lib/conversaciones/presentacion";
import type { CalidadNumero, Canal, Conexion, EstadoPrueba, PruebaCanal } from "./tipos";

/*
 * Textos de la conexión de un número (contratos/conversaciones.md §1.1). La columna «Conexión» es
 * `canal.conexion`, calculada en el servidor: aquí solo se le pone nombre, tono e icono, nunca se
 * recalcula.
 */

/** Nombre del icono de Lucide de cada estado (lo dibuja `EstadoConexion`). */
export type IconoConexion = "punto" | "reloj" | "x" | "aviso" | "apagado" | "pendiente";

export type PresentacionConexion = { texto: string; tono: Tono; icono: IconoConexion; /** Hace falta tocar las credenciales. */ credenciales: boolean };

const CONEXION: Record<Conexion, PresentacionConexion> = {
    conectado: { texto: "Conectado", tono: "success", icono: "punto", credenciales: false },
    webhook_sin_verificar: { texto: "Webhook sin verificar", tono: "warning", icono: "reloj", credenciales: false },
    token_invalido: { texto: "Token vencido", tono: "error", icono: "x", credenciales: true },
    numero_no_coincide: { texto: "Número no coincide", tono: "error", icono: "x", credenciales: true },
    sin_permiso: { texto: "Sin permiso en Meta", tono: "error", icono: "x", credenciales: true },
    error_meta: { texto: "Meta no respondió", tono: "warning", icono: "aviso", credenciales: false },
    sin_probar: { texto: "Sin probar", tono: "neutral", icono: "pendiente", credenciales: false },
    desactivado: { texto: "Desactivado", tono: "neutral", icono: "apagado", credenciales: false },
};

export function presentarConexion(conexion: Conexion): PresentacionConexion {
    return CONEXION[conexion] ?? CONEXION.sin_probar;
}

/**
 * URL para pegar en Meta. Sin `IRIS_BASE_URL` el servidor manda solo la ruta
 * (`webhookUrlCompleta: false`) y el panel antepone su propio origen.
 */
export function webhookUrlDe(canal: Pick<Canal, "webhookUrl" | "webhookUrlCompleta" | "webhookRuta">, origen: string): string {
    if (canal.webhookUrlCompleta && canal.webhookUrl) return canal.webhookUrl;
    const ruta = canal.webhookUrl || canal.webhookRuta;
    return `${origen.replace(/\/+$/, "")}${ruta.startsWith("/") ? "" : "/"}${ruta}`;
}

const CALIDAD: Record<CalidadNumero, string> = { alta: "calidad alta", media: "calidad media", baja: "calidad baja", desconocida: "calidad sin dato" };

/** Acción principal de la tarjeta del resultado. */
export type AccionPrueba = "lista" | "volver_a_comprobar" | "cambiar_token" | "corregir_datos";

export type LineaPrueba = { titulo: string; texto: string; tono: Tono; icono: "check" | "x" | "reloj" | "info" };

export type PresentacionPrueba = {
    titulo: string;
    texto: string;
    tono: Tono;
    accion: AccionPrueba;
    credenciales: LineaPrueba;
    webhook: LineaPrueba;
};

const fmtHora = new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", hour: "2-digit", minute: "2-digit", hour12: false });
const fmtFecha = new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", day: "numeric", month: "short", year: "numeric" });

/** «hoy a las 10:42» o «el 3 nov. 2026, 14:20» (hora de Guatemala). */
export function cuando(iso: string, ahora: number = Date.now()): string {
    const fecha = new Date(iso);
    const hora = fmtHora.format(fecha);
    if (fmtFecha.format(fecha) === fmtFecha.format(new Date(ahora))) return `hoy a las ${hora}`;
    return `el ${fmtFecha.format(fecha)}, ${hora}`;
}

/** «3 nov. 2026» (para «guardado el …»). */
export function fechaCorta(iso: string): string {
    return fmtFecha.format(new Date(iso));
}

const LINEA_CREDENCIALES: Record<Exclude<EstadoPrueba, "conectado">, Omit<LineaPrueba, "titulo">> = {
    token_invalido: { texto: "El token venció o no tiene permiso sobre este número. Genera uno permanente (caducidad «Nunca») y cámbialo.", tono: "error", icono: "x" },
    numero_no_coincide: { texto: "El Phone number ID o el número no son los de Meta. Revisa los dos en WhatsApp → Configuración de la API.", tono: "error", icono: "x" },
    sin_permiso: { texto: "Al token le falta whatsapp_business_messaging o whatsapp_business_management, o el usuario del sistema no tiene asignada la cuenta de WhatsApp.", tono: "error", icono: "x" },
    error_meta: { texto: "Meta no respondió bien. Suele pasar solo; vuelve a comprobar en un momento.", tono: "warning", icono: "info" },
};

/** Encabezado: lo que pasó (el `detalle` del servidor o, sin él, `porDefecto`) y qué significa. */
const ENCABEZADO: Record<Exclude<EstadoPrueba, "conectado">, { titulo: string; porDefecto: string; consecuencia: string; accion: AccionPrueba; tono: Tono }> = {
    token_invalido: { titulo: "Token inválido o vencido", porDefecto: "Meta rechazó el token.", consecuencia: "Los mensajes no van a salir por este número.", accion: "cambiar_token", tono: "error" },
    numero_no_coincide: { titulo: "El número no coincide", porDefecto: "Meta tiene otro número para ese Phone number ID.", consecuencia: "Revisa el número y el Phone number ID.", accion: "corregir_datos", tono: "error" },
    sin_permiso: { titulo: "Al token le faltan permisos", porDefecto: "Meta reconoce el token, pero no deja usarlo con este número.", consecuencia: "Los mensajes no van a salir por este número.", accion: "cambiar_token", tono: "error" },
    error_meta: { titulo: "Meta no respondió", porDefecto: "No se pudo comprobar ahora.", consecuencia: "Las credenciales quedaron guardadas.", accion: "volver_a_comprobar", tono: "warning" },
};

/** Tarjeta «Probar conexión»: encabezado, acción y las dos líneas (credenciales y webhook). */
export function presentarPrueba(p: PruebaCanal, numero: string, ahora: number = Date.now()): PresentacionPrueba {
    if (p.estado !== "conectado") {
        const { porDefecto, consecuencia, ...e } = ENCABEZADO[p.estado];
        const linea = LINEA_CREDENCIALES[p.estado];
        const metaDice = p.estado === "numero_no_coincide" && p.numeroMeta ? ` Meta dice: ${p.numeroMeta}.` : "";
        return {
            ...e,
            texto: `${p.detalle?.trim() || porDefecto} ${consecuencia}`,
            credenciales: { titulo: "Credenciales de Meta", ...linea, texto: `${linea.texto}${metaDice}` },
            webhook: { titulo: "Webhook", texto: "Sin probar: primero las credenciales.", tono: "neutral", icono: "info" },
        };
    }
    const partes = [`Número ${formatearNumero(p.numeroMeta ?? numero)}`];
    if (p.nombreVerificado) partes.push(`«${p.nombreVerificado}»`);
    const credenciales: LineaPrueba = {
        titulo: "Credenciales de Meta",
        texto: `${partes.join(" ")}${p.calidad ? `, ${CALIDAD[p.calidad]}` : ""}.`,
        tono: "success",
        icono: "check",
    };
    if (!p.webhookVerificado || !p.webhookVerificadoEn) {
        return {
            titulo: "Webhook sin verificar todavía",
            texto: "Las credenciales están bien, pero Meta aún no llamó a la URL del webhook.",
            tono: "warning",
            accion: "volver_a_comprobar",
            credenciales,
            webhook: { titulo: "Webhook", texto: "Pega la URL y el token en Meta, pulsa «Verificar y guardar» y vuelve a comprobar.", tono: "warning", icono: "reloj" },
        };
    }
    return {
        titulo: "Conectado",
        texto: "Meta aceptó las credenciales y verificó el webhook.",
        tono: "success",
        accion: "lista",
        credenciales,
        webhook: { titulo: "Webhook", texto: `Meta lo verificó ${cuando(p.webhookVerificadoEn, ahora)}.`, tono: "success", icono: "check" },
    };
}

/** Última actividad para la tarjeta del webhook en «Credenciales». */
export function textoUltimoMensaje(iso: string | null, ahora: number = Date.now()): string {
    if (!iso) return "Todavía no llega ningún mensaje de Meta a este número.";
    const min = Math.max(0, Math.round((ahora - Date.parse(iso)) / 60_000));
    if (min < 1) return "Último mensaje recibido hace menos de un minuto.";
    if (min < 60) return `Último mensaje recibido hace ${min} ${min === 1 ? "minuto" : "minutos"}.`;
    return `Último mensaje recibido ${cuando(iso, ahora)}.`;
}
