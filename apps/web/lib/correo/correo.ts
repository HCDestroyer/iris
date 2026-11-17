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
const sinControl = (v: string) => ![...v].some((c) => c.charCodeAt(0) < 32 || c.charCodeAt(0) === 127);
const CORREO = /^[^\s@<>,;"]+@[^\s@<>,;"]+\.[^\s@<>,;"]+$/;
/** Nombre DNS o IP, sin esquema, ruta ni puerto. */
const HOST = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?$|^\[[0-9A-Fa-f:.]+\]$|^[0-9A-Fa-f:]+$/;

const correo = (mensaje: string) => z.string().trim().max(254, "Hasta 254 caracteres").refine(sinControl, "Sin saltos de línea").regex(CORREO, mensaje);

export const esquemaCorreo = z
    .object({
        host: z.string().trim().min(1, "Escribe el servidor").max(253, "Hasta 253 caracteres").regex(HOST, "Solo el nombre del servidor: sin «https://», sin ruta y sin puerto"),
        puerto: z.string().refine((v) => (PUERTOS as readonly number[]).includes(Number(v)), "Elige 587, 465, 25 o 2525"),
        seguridad: z.enum(SEGURIDADES),
        usuario: z.string().trim().max(254, "Hasta 254 caracteres").refine(sinControl, "Sin saltos de línea"),
        /** Vacío = no se toca la guardada. */
        contrasena: z.string().max(1024, "Hasta 1,024 caracteres"),
        /** Borrar la contraseña guardada (`contrasena: ""`). */
        quitarContrasena: z.boolean(),
        remitenteNombre: z.string().trim().max(80, "Hasta 80 caracteres").refine(sinControl, "Sin saltos de línea"),
        remitenteCorreo: correo("Escribe un solo correo, como avisos@tuempresa.com"),
    })
    .superRefine((v, ctx) => {
        // Sin cifrado no viajan credenciales (422 `sin_cifrado_sin_credenciales`).
        if (v.seguridad === "ninguna" && (v.usuario.trim() !== "" || v.contrasena !== "")) {
            ctx.addIssue({ code: "custom", path: [v.usuario.trim() !== "" ? "usuario" : "contrasena"], message: TEXTO_REGLA.sin_cifrado_sin_credenciales.mensaje });
        }
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
        usuario: a.usuario ?? "",
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

/**
 * ¿Falta escribir la contraseña? Espejo de `contrasena_requerida`: con usuario hace falta una, y la
 * guardada no se manda a otro servidor: si cambió el servidor, el puerto o el usuario hay que
 * escribirla otra vez. `null` si no falta.
 */
export function faltaContrasena(a: Pick<AjusteCorreo, "tieneContrasena" | "host" | "puerto" | "usuario">, d: ValoresCorreo): string | null {
    if (d.usuario.trim() === "" || d.contrasena !== "") return null;
    if (!a.tieneContrasena || d.quitarContrasena) return "Con usuario hace falta la contraseña: escríbela.";
    const cambio = d.host.trim().toLowerCase() !== a.host.trim().toLowerCase() || Number(d.puerto) !== a.puerto || d.usuario.trim() !== (a.usuario ?? "").trim();
    return cambio ? TEXTO_CONTRASENA_OTRA_VEZ : null;
}

/** Guardar sin usuario borra la contraseña guardada (sin usuario no hay con qué entrar): se avisa antes. */
export const seBorraLaContrasena = (a: Pick<AjusteCorreo, "tieneContrasena">, d: Pick<ValoresCorreo, "usuario">): boolean => a.tieneContrasena && d.usuario.trim() === "";

export const TEXTO_CONTRASENA_OTRA_VEZ = "Cambiaste el servidor, el puerto o el usuario: escribe la contraseña otra vez.";

type CampoRegla = "host" | "seguridad" | "usuario" | "contrasena";
/** Las reglas del servidor (422) que el panel explica con sus palabras; `campo: null` = aviso general. */
const TEXTO_REGLA = {
    contrasena_requerida: { campo: "contrasena", mensaje: TEXTO_CONTRASENA_OTRA_VEZ },
    destino_no_permitido: { campo: "host", mensaje: "Ese servidor no se puede usar desde Iris (es de una red interna o no permitida)." },
    sin_cifrado_solo_red_interna: { campo: "seguridad", mensaje: "Sin cifrado solo vale para un servidor de tu red interna: elige STARTTLS o TLS." },
    sin_cifrado_solo_global: { campo: "seguridad", mensaje: "«Sin cifrado» solo se puede usar en el correo general: elige STARTTLS o TLS." },
    sin_cifrado_sin_credenciales: { campo: "usuario", mensaje: "Sin cifrado no se mandan usuario ni contraseña: elige STARTTLS o TLS, o deja el usuario vacío." },
    cifrado_no_configurado: { campo: null, mensaje: "Al servidor de Iris le falta su clave de cifrado, así que no puede guardar la contraseña. Avisa a quien administra la instalación." },
} as const satisfies Record<string, { campo: CampoRegla | null; mensaje: string }>;

/**
 * Una regla de negocio del servidor, en llano y con el campo donde va. `null` si el código no es
 * de los que el panel conoce (entonces valen los `errors[]` del 422 tal cual).
 */
export function reglaDeCorreo(codigo: string, a: Pick<AjusteCorreo, "tieneContrasena">, campoDelServidor?: string): { campo: CampoRegla | null; mensaje: string } | null {
    if (!Object.hasOwn(TEXTO_REGLA, codigo)) return null;
    const regla = TEXTO_REGLA[codigo as keyof typeof TEXTO_REGLA];
    if (codigo === "contrasena_requerida" && !a.tieneContrasena) return { campo: "contrasena", mensaje: "Con usuario hace falta la contraseña: escríbela." };
    // `sin_cifrado_sin_credenciales` puede venir por el usuario o por la contraseña: manda el servidor.
    const campo = regla.campo !== null && (campoDelServidor === "usuario" || campoDelServidor === "contrasena") && codigo === "sin_cifrado_sin_credenciales" ? campoDelServidor : regla.campo;
    return { campo, mensaje: regla.mensaje };
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
export function textoOrigen(a: Pick<AjusteCorreo, "origen" | "origenDetalle">, ambito: Ambito): { tono: TonoOrigen; titulo: string; detalle: string } {
    const deEmpresa = ambito !== "global";
    // El detalle es la frase que arma el servidor (`origenDetalle`), tal cual; la de aquí es solo respaldo.
    const con = (tono: TonoOrigen, titulo: string, respaldo: string) => ({ tono, titulo, detalle: a.origenDetalle?.trim() || respaldo });
    switch (a.origen) {
        case "empresa":
            return con("ok", "Hoy los correos salen por el correo de esta empresa", "Es el que está configurado aquí abajo.");
        case "global":
            return con(deEmpresa ? "info" : "ok", "Hoy los correos salen por el correo general", deEmpresa ? "Esta empresa no tiene uno propio encendido: usa el de toda la instalación." : "Es el que está configurado aquí abajo.");
        case "entorno":
            return con("info", "Hoy los correos salen por el correo del servidor", "Lo dejó configurado quien instaló Iris. Si configuras uno aquí y lo enciendes, se usa el de aquí.");
        default:
            return con("aviso", "Iris no envía correos todavía", "Los avisos llegan por la campana. Configura un correo aquí para que también lleguen al buzón.");
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

/** Qué se le dice a la persona tras una prueba: el `detalle` llano del servidor, tal cual; si falta, el texto por causa. */
export function textoResultado(r: ResultadoPrueba, para: string): { ok: boolean; titulo: string; detalle: string } {
    if (r.estado === "ok") return { ok: true, titulo: "El correo de prueba salió", detalle: r.detalle.trim() || `Revisa la bandeja de ${para}.` };
    return { ok: false, titulo: "No se pudo enviar", detalle: r.detalle.trim() || textoCausa(r.causa) };
}

/**
 * «Sin cifrado» solo se ofrece en el correo general (contrato 1.1.0: en una empresa, 422
 * `sin_cifrado_solo_global`). Si una empresa ya lo tenía guardado se sigue viendo, para poder cambiarlo.
 */
export function seguridadesDe(ambito: Ambito, guardada: SeguridadCorreo): readonly SeguridadCorreo[] {
    return SEGURIDADES.filter((s) => s !== "ninguna" || ambito === "global" || guardada === "ninguna");
}

/**
 * Qué decir con un 429 al guardar o al probar: `demasiados_rechazos` (muchos intentos con servidores
 * que Iris no puede usar) o el tope de pruebas por hora; siempre con cuánto esperar.
 */
export function textoDemasiados(codigo: string, reintentarEn: number | undefined): string {
    const que = codigo === "demasiados_rechazos" ? "Demasiados intentos con servidores no permitidos." : "Ya se hicieron muchas pruebas seguidas.";
    return `${que} ${textoEspera(reintentarEn)}`;
}

/** «Inténtalo en 12 min», «Inténtalo en 40 s», «Inténtalo más tarde». */
export function textoEspera(segundos: number | undefined): string {
    if (segundos === undefined || !Number.isFinite(segundos) || segundos <= 0) return "Inténtalo más tarde.";
    if (segundos < 90) return `Inténtalo en ${Math.ceil(segundos)} s.`;
    return `Inténtalo en ${Math.ceil(segundos / 60)} min.`;
}

export const NOMBRE_ORIGEN: Record<OrigenCorreo, string> = { empresa: "esta empresa", global: "general", entorno: "servidor", ninguno: "ninguno" };
