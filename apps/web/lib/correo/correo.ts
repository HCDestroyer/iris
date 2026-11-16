import { z } from "zod";
import type { AjusteCorreo, CamposCorreo, CausaPrueba, EntradaCorreo, EntradaPrueba, OrigenCorreo, ResultadoPrueba, SeguridadCorreo } from "./tipos";

/*
 * Correo del sistema: el servidor de correo con que Iris manda avisos a sus usuarios
 * (contratos/correo-sistema.md). Lógica pura de la pantalla; el esquema zod es el espejo del
 * validador del servidor (§1 y §4).
 */

// --- Ámbito (§3) -------------------------------------------------------------------------------

/** `global` o el id de la empresa, como va en la ruta. */
export type Ambito = "global" | number;

/**
 * El ámbito que edita la pantalla. Quien tiene el permiso sin límite de empresa elige entre
 * «General» y «Solo esta empresa» (`?ambito=empresa`); los demás, siempre su empresa. Sin empresa
 * activa solo queda el general (y solo para quien puede).
 */
export function ambitoDe(yo: { todasLasEmpresas: boolean; empresaActiva?: { id: number } | undefined }, elegido: string | null): Ambito | null {
    const empresa = yo.empresaActiva?.id;
    if (!yo.todasLasEmpresas) return empresa ?? null;
    return elegido === "empresa" && empresa !== undefined ? empresa : "global";
}

// --- Formulario --------------------------------------------------------------------------------

export const PUERTOS = [587, 465, 25, 2525] as const;
export const SEGURIDADES = ["starttls", "tls", "ninguna"] as const satisfies readonly SeguridadCorreo[];
export const NOMBRE_SEGURIDAD: Record<SeguridadCorreo, string> = {
    starttls: "STARTTLS (lo más común)",
    tls: "TLS directo",
    ninguna: "Ninguna (solo un servidor de tu red interna)",
};

/** Nada de saltos de línea ni caracteres de control: irían a las cabeceras del correo (§4). */
const SIN_CONTROL = /^[^\u0000-\u001f\u007f]*$/;
const CORREO = /^[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"]+$/;
/** Nombre DNS o IP, sin esquema, ruta ni puerto. */
const HOST = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$|^\[[0-9A-Fa-f:.]+\]$|^[0-9A-Fa-f:]+$/;

const correo = (mensaje: string) => z.string().trim().max(254, "Hasta 254 caracteres").regex(SIN_CONTROL, "Sin saltos de línea").regex(CORREO, mensaje);

export const esquemaCorreo = z.object({
    host: z.string().trim().min(1, "Escribe el servidor").max(253, "Hasta 253 caracteres").regex(HOST, "Solo el nombre del servidor: sin «https://», sin ruta y sin puerto"),
    puerto: z.string().refine((v) => (PUERTOS as readonly number[]).includes(Number(v)), "Elige 587, 465, 25 o 2525"),
    seguridad: z.enum(SEGURIDADES),
    usuario: z.string().trim().max(254, "Hasta 254 caracteres").regex(SIN_CONTROL, "Sin saltos de línea"),
    /** Vacío = no se toca la guardada. */
    contrasena: z.string().max(1024, "Hasta 1,024 caracteres"),
    /** Borrar la contraseña guardada (`contrasena: ""`). */
    quitarContrasena: z.boolean(),
    remitenteNombre: z.string().trim().max(80, "Hasta 80 caracteres").regex(SIN_CONTROL, "Sin saltos de línea"),
    remitenteCorreo: correo("Escribe un solo correo, como avisos@tuempresa.com"),
});
export type ValoresCorreo = z.infer<typeof esquemaCorreo>;

export const esquemaPrueba = z.object({ para: correo("Escribe un solo correo de destino") });
export type ValoresPrueba = z.infer<typeof esquemaPrueba>;

export function valoresCorreo(a: AjusteCorreo): ValoresCorreo {
    return {
        host: a.host,
        // Un puerto guardado que el panel no conoce se ve tal cual y la validación pide elegir otro.
        puerto: String(a.puerto || 587),
        seguridad: (SEGURIDADES as readonly string[]).includes(a.seguridad) ? a.seguridad : "starttls",
        usuario: a.usuario,
        contrasena: "",
        quitarContrasena: false,
        remitenteNombre: a.remitenteNombre,
        remitenteCorreo: a.remitenteCorreo,
    };
}

/** Los campos que viajan en `guardar` y en `probar`. La contraseña solo va si la persona la cambió. */
export function camposDeValores(d: ValoresCorreo): CamposCorreo {
    return {
        host: d.host.trim(),
        puerto: Number(d.puerto),
        seguridad: d.seguridad,
        usuario: d.usuario.trim(),
        // Escribir una nueva gana a «quitar»; sin tocar nada, ausente = se conserva la guardada.
        ...(d.contrasena !== "" ? { contrasena: d.contrasena } : d.quitarContrasena ? { contrasena: "" } : {}),
        remitenteNombre: d.remitenteNombre.trim(),
        remitenteCorreo: d.remitenteCorreo.trim(),
    };
}

/** `guardar`: un ajuste nuevo nace encendido; uno que ya existe conserva si está activo (eso se cambia aparte). */
export function entradaCorreo(a: Pick<AjusteCorreo, "version" | "activo" | "configurado">, d: ValoresCorreo): EntradaCorreo {
    return { ...camposDeValores(d), version: a.version, activo: a.configurado ? a.activo : true };
}

export function entradaPrueba(d: ValoresCorreo, para: string): EntradaPrueba {
    return { ...camposDeValores(d), para: para.trim() };
}

export const CAMPOS_CORREO = ["host", "puerto", "seguridad", "usuario", "contrasena", "remitenteNombre", "remitenteCorreo"] as const;

// --- Atajos de proveedor -----------------------------------------------------------------------

export type Atajo = "google" | "microsoft" | "otro";
type Relleno = Pick<ValoresCorreo, "host" | "puerto" | "seguridad">;
/** Solo rellenan servidor, puerto y seguridad; todo se puede editar después. */
export const ATAJOS: readonly { id: Atajo; nombre: string; relleno: Relleno; nota: string }[] = [
    { id: "google", nombre: "Google Workspace", relleno: { host: "smtp.gmail.com", puerto: "587", seguridad: "starttls" }, nota: "Usa una «contraseña de aplicación» de la cuenta, no la de siempre." },
    { id: "microsoft", nombre: "Microsoft 365", relleno: { host: "smtp.office365.com", puerto: "587", seguridad: "starttls" }, nota: "La cuenta necesita tener activado el envío por SMTP (SMTP AUTH)." },
    { id: "otro", nombre: "Otro", relleno: { host: "", puerto: "587", seguridad: "starttls" }, nota: "Escribe los datos que te dio tu proveedor de correo." },
];

/** El atajo que coincide con lo que hay en el formulario (para marcarlo); `null` si ninguno. */
export function atajoDe(d: Pick<ValoresCorreo, "host" | "puerto" | "seguridad">): Atajo | null {
    const host = d.host.trim().toLowerCase();
    const hallado = ATAJOS.find((a) => a.id !== "otro" && a.relleno.host === host && a.relleno.puerto === d.puerto && a.relleno.seguridad === d.seguridad);
    return hallado?.id ?? (host === "" ? null : "otro");
}

// --- Textos ------------------------------------------------------------------------------------

export type TonoOrigen = "ok" | "info" | "aviso";

/**
 * La línea de arriba: por dónde salen hoy los correos de este ámbito (§2), en llano. `ninguno` no es
 * un error: los avisos siguen llegando por la campana.
 */
export function textoOrigen(a: Pick<AjusteCorreo, "origen" | "entorno">, ambito: Ambito): { tono: TonoOrigen; titulo: string; detalle: string } {
    const deEmpresa = ambito !== "global";
    switch (a.origen) {
        case "empresa":
            return { tono: "ok", titulo: "Hoy los correos salen por el correo de esta empresa", detalle: "Es el que está configurado aquí abajo." };
        case "global":
            return {
                tono: deEmpresa ? "info" : "ok",
                titulo: "Hoy los correos salen por el correo general",
                detalle: deEmpresa ? "Esta empresa no tiene uno propio encendido: usa el de toda la instalación." : "Es el que está configurado aquí abajo, para todas las empresas que no tengan uno propio.",
            };
        case "entorno":
            return {
                tono: "info",
                titulo: "Hoy los correos salen por el correo del servidor",
                detalle: `Lo dejó configurado quien instaló Iris${a.entorno?.remitenteCorreo ? ` (${a.entorno.remitenteCorreo})` : ""}. Si configuras uno aquí y lo enciendes, se usa el de aquí.`,
            };
        default:
            return { tono: "aviso", titulo: "Iris no envía correos todavía", detalle: "Los avisos llegan por la campana. Configura un correo aquí para que también lleguen al buzón." };
    }
}

const TEXTO_CAUSA: Record<CausaPrueba, string> = {
    no_conecta: "No se pudo conectar: revisa el servidor y el puerto.",
    destino_no_permitido: "Ese servidor no se puede usar desde Iris (es de una red interna o no permitida).",
    certificado: "El certificado del servidor no es válido para ese nombre.",
    seguridad: "El servidor no acepta la seguridad elegida: prueba con STARTTLS o con TLS.",
    credenciales: "El usuario o la contraseña no son correctos.",
    remitente_rechazado: "El servidor no acepta ese correo del remitente.",
    destinatario_rechazado: "El servidor no acepta esa dirección de destino.",
    tiempo_agotado: "El servidor no respondió a tiempo.",
    otro: "El servidor rechazó el envío.",
};

/** La causa de una prueba fallida, en llano. Una que el panel no conoce: el texto genérico. */
export const textoCausa = (causa: string | null | undefined): string => (causa && Object.hasOwn(TEXTO_CAUSA, causa) ? TEXTO_CAUSA[causa as CausaPrueba] : TEXTO_CAUSA.otro);

/** Qué se le dice a la persona tras una prueba: el `detalle` llano del servidor o, si falta, el texto por causa. */
export function textoResultado(r: ResultadoPrueba, para: string): { ok: boolean; titulo: string; detalle: string } {
    if (r.estado === "ok") return { ok: true, titulo: "El correo de prueba salió", detalle: r.detalle.trim() || `Revisa la bandeja de ${para}.` };
    return { ok: false, titulo: "No se pudo enviar", detalle: r.detalle.trim() || textoCausa(r.causa) };
}

/** «Inténtalo en 12 min», «Inténtalo en 40 s», «Inténtalo más tarde». */
export function textoEspera(segundos: number | undefined): string {
    if (segundos === undefined || !Number.isFinite(segundos) || segundos <= 0) return "Inténtalo más tarde.";
    if (segundos < 90) return `Inténtalo en ${Math.ceil(segundos)} s.`;
    return `Inténtalo en ${Math.ceil(segundos / 60)} min.`;
}

export const NOMBRE_ORIGEN: Record<OrigenCorreo, string> = { empresa: "esta empresa", global: "general", entorno: "servidor", ninguno: "ninguno" };
