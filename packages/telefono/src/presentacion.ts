import { esErrorDeApi } from "./errores";
import type { ConexionTelefono, ErrorTelefono, MotivoTelefono } from "./tipos";

/*
 * Textos del teléfono de Iris en llano (maqueta aprobada de E4c, 21 nov 2026): el punto del
 * teléfono, los avisos de error con lo que hay que hacer, lo que se puede marcar en el teclado y por
 * qué terminó una llamada.
 */

// Lo que se marca en el teclado ------------------------------------------------------------------

export type Marcado =
    /** 8 dígitos de Guatemala (fijos y móviles: empiezan por 2 a 7). */
    | { tipo: "externo"; nacional: string }
    /** Una extensión o un grupo de la centralita (2 a 6 cifras). */
    | { tipo: "interno"; numero: string }
    | { tipo: "incompleto" }
    | { tipo: "invalido"; motivo: string };

/**
 * Lo mismo que acepta el servidor (`nacionalGT` y las extensiones de 2 a 6 cifras): el teclado
 * avisa antes de marcar, pero quien decide es el servidor (422 `destino_no_permitido`).
 */
export function clasificarMarcado(crudo: string): Marcado {
    const s = crudo.trim();
    if (s === "") return { tipo: "incompleto" };
    if (/[^0-9+\s()-]/.test(s)) return { tipo: "invalido", motivo: "Solo números" };
    const digitos = s.replace(/\D/g, "");
    if (s.startsWith("+") && !digitos.startsWith("502")) return { tipo: "invalido", motivo: "Solo números de Guatemala" };
    const nacional = digitos.length === 11 && digitos.startsWith("502") ? digitos.slice(3) : digitos;
    if (!s.startsWith("+") && digitos.length >= 2 && digitos.length <= 6) return { tipo: "interno", numero: digitos };
    if (nacional.length < 8) return { tipo: "incompleto" };
    if (nacional.length !== 8) return { tipo: "invalido", motivo: "Solo números de Guatemala (8 dígitos) o extensiones del equipo" };
    if (nacional.charAt(0) < "2" || nacional.charAt(0) > "7") return { tipo: "invalido", motivo: "Ese número no es un teléfono de Guatemala" };
    return { tipo: "externo", nacional };
}

/** «5512 3480» (los 8 dígitos de Guatemala en dos grupos). */
export function nacionalLegible(nacional: string): string {
    return nacional.length === 8 ? `${nacional.slice(0, 4)} ${nacional.slice(4)}` : nacional;
}

/** «+502 5512 3480» si es un número de Guatemala; si no, tal cual. */
export function numeroLegible(numero: string): string {
    const d = numero.replace(/\D/g, "");
    if (d.length === 11 && d.startsWith("502")) return `+502 ${nacionalLegible(d.slice(3))}`;
    if (d.length === 8 && /^[2-7]/.test(d)) return `+502 ${nacionalLegible(d)}`;
    return numero;
}

// El punto del teléfono -------------------------------------------------------------------------

export type PuntoTelefono = {
    /** Un punto (verde o gris) o un aviso (chip) cuando hay que actuar. */
    forma: "punto" | "aviso";
    tono: "success" | "neutral" | "warning" | "danger";
    texto: string;
    /** Texto corto del chip en el teléfono (390 px). */
    corto: string;
};

export function puntoTelefono(c: ConexionTelefono): PuntoTelefono | null {
    switch (c.fase) {
        case "no_disponible":
            return null;
        case "apagado":
            return { forma: "punto", tono: "neutral", texto: "Teléfono apagado en este navegador", corto: "Apagado" };
        case "conectando":
            return { forma: "punto", tono: "neutral", texto: "Conectando el teléfono…", corto: "Conectando" };
        case "listo":
            return { forma: "punto", tono: "success", texto: "Teléfono listo", corto: "Listo" };
        case "error":
            return puntoError(c.error);
    }
}

function puntoError(e: ErrorTelefono): PuntoTelefono {
    switch (e.tipo) {
        case "sin_conexion":
            return { forma: "aviso", tono: "danger", texto: "Sin conexión con la centralita", corto: "Sin conexión" };
        case "clave":
            return { forma: "aviso", tono: "danger", texto: "Teléfono apagado: la centralita no aceptó la contraseña", corto: "Teléfono apagado" };
        case "microfono":
            return { forma: "aviso", tono: "danger", texto: "Micrófono bloqueado", corto: "Micrófono" };
        case "pestana":
            return { forma: "aviso", tono: "warning", texto: "El teléfono está en otra pestaña", corto: "En otra pestaña" };
        case "espera":
            return e.rechazos
                ? { forma: "aviso", tono: "warning", texto: "El teléfono espera unos minutos", corto: "En unos minutos" }
                : { forma: "aviso", tono: "warning", texto: "El teléfono se vuelve a conectar en un minuto", corto: "En un minuto" };
        case "cortado":
            return { forma: "aviso", tono: "danger", texto: "Teléfono desconectado", corto: "Desconectado" };
    }
}

// Avisos de error --------------------------------------------------------------------------------

export type AvisoTelefono = {
    tono: "danger" | "warning";
    titulo: string;
    detalle: string;
    /** Pasos numerados (micrófono). */
    pasos?: string[];
    accion: "reintentar" | "usar_aqui" | null;
};

const hora = (ms: number) => new Intl.DateTimeFormat("es-GT", { hour: "2-digit", minute: "2-digit", hour12: false, timeZone: "America/Guatemala" }).format(ms);

export function avisoError(e: ErrorTelefono, extension: string | null): AvisoTelefono {
    const tuExt = extension ? `tu extensión ${extension}` : "tu extensión";
    switch (e.tipo) {
        case "sin_conexion":
            return {
                tono: "danger",
                titulo: "No hay conexión con la centralita",
                detalle: `Desde las ${hora(e.desde)} las llamadas de ${tuExt} no suenan en Iris. Iris lo intenta sola cada 30 segundos. Si sigue así, avisa a quien administra la centralita.`,
                accion: "reintentar",
            };
        case "clave":
            return {
                tono: "danger",
                titulo: "La centralita no aceptó la contraseña de tu extensión",
                detalle: `${tuExt.charAt(0).toUpperCase()}${tuExt.slice(1)} no puede sonar en Iris. No es algo que puedas arreglar tú: un administrador tiene que escribirla otra vez en Administración › Centralita.`,
                accion: "reintentar",
            };
        case "microfono":
            if (e.estado === "sin_microfono") {
                return {
                    tono: "danger",
                    titulo: "No encontramos un micrófono",
                    detalle: "Conecta tus audífonos con micrófono (o revisa que otra aplicación no lo esté usando) y toca «Reintentar».",
                    accion: "reintentar",
                };
            }
            if (e.estado === "no_disponible") {
                return {
                    tono: "danger",
                    titulo: "Este navegador no puede usar el teléfono",
                    detalle: "Abre Iris en Chrome, Edge, Firefox o Safari actualizados.",
                    accion: null,
                };
            }
            return {
                tono: "danger",
                titulo: "Iris no tiene permiso para usar tu micrófono",
                detalle: "Sin micrófono no puedes contestar. Para darle permiso:",
                pasos: ["Toca el candado junto a la dirección, arriba.", "En «Micrófono», elige Permitir.", "Vuelve aquí y toca «Reintentar»."],
                accion: "reintentar",
            };
        case "pestana":
            return {
                tono: "warning",
                titulo: "El teléfono está abierto en otra pestaña de Iris",
                detalle: "Solo una pestaña puede sonar y contestar a la vez, para que no suene doble. Tus llamadas suenan en la otra.",
                accion: "usar_aqui",
            };
        case "espera":
            if (e.rechazos) {
                return {
                    tono: "warning",
                    titulo: "La centralita rechazó varias contraseñas seguidas",
                    detalle: `Para que la centralita no bloquee a Iris, el teléfono espera. Iris lo vuelve a conectar sola a las ${hora(e.hasta)}.`,
                    accion: null,
                };
            }
            return {
                tono: "warning",
                titulo: "El teléfono se abrió muchas veces seguidas",
                detalle: `Iris lo vuelve a conectar sola a las ${hora(e.hasta)}.`,
                accion: null,
            };
        case "cortado":
            return {
                tono: "danger",
                titulo: "Iris desconectó el teléfono",
                detalle: `Las llamadas de ${tuExt} no suenan en Iris. Puede que hayan cambiado la centralita o tus permisos. Toca «Reintentar»; si vuelve a pasar, avisa a quien administra la centralita.`,
                accion: "reintentar",
            };
    }
}

/** Por qué el teléfono no está disponible para esta persona (popover y Administración). */
export function textoMotivo(m: MotivoTelefono | null): string {
    switch (m) {
        case "sin_centralita":
            return "Tu empresa no tiene una centralita conectada.";
        case "sin_extension":
            return "No tienes una extensión de la centralita.";
        case "extension_escritorio":
            return "Tu extensión suena en tu teléfono de escritorio.";
        case "sin_credencial":
            return "Falta la contraseña de tu extensión: la escribe un administrador.";
        case "registro_sin_probar":
            return "La centralita no aceptó la contraseña de tu extensión: un administrador tiene que escribirla otra vez y probarla.";
        case "telefono_apagado":
            return "El teléfono de Iris está apagado en tu empresa.";
        case "turn_no_configurado":
            return "El servidor de Iris no tiene el relevo de audio configurado.";
        default:
            return "El teléfono de Iris no está disponible.";
    }
}

// Al llamar --------------------------------------------------------------------------------------

/** Los errores de `POST /telefono/llamar` en llano. */
export function textoErrorLlamar(e: unknown): { titulo: string; detalle?: string } {
    if (!esErrorDeApi(e)) return { titulo: "No se pudo llamar", detalle: "Revisa tu conexión e intenta otra vez." };
    switch (e.codigo) {
        case "destino_no_permitido":
            return { titulo: "Ese número no se puede marcar", detalle: "Solo números de Guatemala (8 dígitos) y extensiones o grupos del equipo." };
        case "sin_contacto":
            return { titulo: "No tiene ese teléfono", detalle: "En Argos no hay un número de ese tipo para este cliente." };
        case "limite_llamadas":
            return { titulo: "Llegaste al límite de llamadas de hoy", detalle: "Tu empresa pone un tope de llamadas por persona y día. Mañana vuelves a tener." };
        case "llamadas_simultaneas":
            return { titulo: "Hay muchas llamadas a la vez en tu empresa", detalle: "Espera a que termine alguna e intenta otra vez." };
        case "sin_permiso":
        case "prohibido":
            return { titulo: "No tienes permiso para llamar con tu extensión" };
        default:
            return { titulo: e.titulo || "No se pudo llamar", detalle: e.detalle };
    }
}

/**
 * Por qué terminó una llamada (la `cause` de JsSIP) en llano, o `null` si fue un colgar normal.
 * Las causas: https://jssip.net/documentation/3.x/api/causes/
 */
export function motivoFin(causa: string | undefined, direccion: "entrante" | "saliente"): string | null {
    switch (causa) {
        case undefined:
        case "Terminated":
        case "Canceled":
        case "Rejected":
        case "Bye":
            return null;
        case "Busy":
            return "Ocupado";
        case "Unavailable":
        case "No Answer":
        case "Request Timeout":
            return direccion === "saliente" ? "No contestó" : null;
        case "Not Found":
        case "Address Incomplete":
            return "Ese número no existe";
        case "User Denied Media Access":
            return "Iris no pudo usar tu micrófono";
        case "Connection Error":
        case "SIP Failure Code":
        case "Internal Error":
        case "Bad Media Description":
        case "RTP Timeout":
        case "WebRTC Error":
            return "Se cortó la llamada (problema de conexión)";
        case "Authentication Error":
        case "Missing SDP":
        case "Incompatible SDP":
        case "Redirected":
            return "La centralita no dejó hacer la llamada";
        default:
            return "Se cortó la llamada";
    }
}

/** 4:12 o 1:02:03. */
export function reloj(segundos: number): string {
    const s = Math.max(0, Math.floor(segundos));
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const r = String(s % 60).padStart(2, "0");
    return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${r}` : `${String(m).padStart(2, "0")}:${r}`;
}
