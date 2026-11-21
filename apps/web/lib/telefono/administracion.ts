import { z } from "zod";
import { ErrorApi } from "@/lib/api/errores";
import type { ExtensionCentralita } from "@/lib/centralita/tipos";
import type { PruebaRegistro, PruebaTelefono, TelefonoCentralita } from "@iris/telefono";

/*
 * Administración del teléfono de Iris (contratos/voz.md §3.12, E4c; runbook centralita-issabel §9):
 * el formulario del WSS y los límites (espejo del validador del servidor), los resultados de
 * «Probar» y «Probar registro» en llano, y el estado de cada extensión.
 */

export type Tono = "success" | "warning" | "error" | "info" | "neutral";

const HOST = /^[a-z0-9]([a-z0-9.-]{0,251}[a-z0-9])?$/;

/** Un entero entre `min` y `max` escrito en un `CampoNumero` (texto). */
const entero = (min: number, max: number) =>
    z
        .string()
        .trim()
        .refine((v) => /^[0-9]{1,6}$/.test(v) && Number(v) >= min && Number(v) <= max, `De ${min} a ${max}`);

export const esquemaTelefono = z.object({
    wssPuerto: entero(1, 65535),
    wssRuta: z.string().trim().regex(/^\/[A-Za-z0-9/_.-]{0,63}$/, "Empieza con / (p. ej. /ws)"),
    sipDominio: z
        .string()
        .trim()
        .max(253)
        .refine((v) => v === "" || HOST.test(v.toLowerCase().replace(/\.$/, "")), "Un nombre de dominio o una IP, sin sip: ni puerto"),
    prefijoSalida: z.string().trim().regex(/^[0-9]{0,6}$/, "Hasta 6 dígitos"),
    llamadasPorUsuarioDia: entero(1, 300),
    llamadasSimultaneas: entero(1, 100),
    confirmoRestricciones: z.boolean(),
});

export type ValoresTelefono = z.infer<typeof esquemaTelefono>;

export function valoresTelefono(t: TelefonoCentralita | undefined | null): ValoresTelefono {
    return {
        wssPuerto: String(t?.wssPuerto ?? 8089),
        wssRuta: t?.wssRuta ?? "/ws",
        sipDominio: t?.sipDominio ?? "",
        prefijoSalida: t?.prefijoSalida ?? "",
        llamadasPorUsuarioDia: String(t?.llamadasPorUsuarioDia ?? 60),
        llamadasSimultaneas: String(t?.llamadasSimultaneas ?? 10),
        confirmoRestricciones: !!t?.restriccionesConfirmadasEn,
    };
}

/** Cambiar puerto, ruta, huella o dominio apaga el teléfono y obliga a probar otra vez (servidor). */
export function cambiaConexionTelefono(v: Pick<ValoresTelefono, "wssPuerto" | "wssRuta" | "sipDominio">, t: TelefonoCentralita | undefined | null, huella: string | null): boolean {
    if (!t) return true;
    return Number(v.wssPuerto) !== t.wssPuerto || v.wssRuta.trim() !== t.wssRuta || (v.sipDominio.trim().toLowerCase() || null) !== (t.sipDominio ?? null) || (huella !== null && huella !== t.huellaWss);
}

export function textoPruebaTelefono(p: Pick<PruebaTelefono, "estado">): { tono: Tono; titulo: string; detalle: string } {
    switch (p.estado) {
        case "ok":
            return { tono: "success", titulo: "La Issabel respondió por WSS", detalle: "Iris se conectó al puerto y la centralita contestó en SIP. Compara la huella del certificado con la de tu Issabel." };
        case "huella_distinta":
            return { tono: "error", titulo: "El certificado cambió", detalle: "La huella no es la que guardaste. Si renovaste el certificado del 8089, compara la nueva; si no, avisa a quien administra la centralita." };
        case "no_es_sip":
            return { tono: "error", titulo: "Abrió, pero no habla SIP", detalle: "Revisa el transporte transport-wss de PJSIP (pjsip reload) y la ruta (de fábrica /ws)." };
        case "no_conecta":
            return { tono: "error", titulo: "No conecta", detalle: "Revisa que el 8089 esté encendido (tlsenable en http.conf), el firewall solo con la IP de Iris y la VPN." };
    }
}

export function textoPruebaRegistro(p: Pick<PruebaRegistro, "estado">, extension: string): { tono: Tono; titulo: string; detalle: string } {
    switch (p.estado) {
        case "ok":
            return { tono: "success", titulo: `La centralita aceptó la extensión ${extension}`, detalle: "Ya le puede sonar en Iris." };
        case "credenciales":
            return {
                tono: "error",
                titulo: `La centralita no aceptó la contraseña de la extensión ${extension}`,
                detalle: `Revisa en Issabel la contraseña (Secret) de la ${extension} y escríbela otra vez aquí con «Cambiar la contraseña». Iris no vuelve a probar la misma: la Issabel bloquearía a Iris después de varios intentos fallidos.`,
            };
        case "huella_distinta":
            return { tono: "error", titulo: "El certificado de la Issabel cambió", detalle: "Prueba el teléfono en «Teléfono en Iris» y compara la huella nueva." };
        case "no_conecta":
            return { tono: "error", titulo: "No conecta con la Issabel", detalle: "Revisa el 8089, el firewall y la VPN en «Teléfono en Iris»." };
        case "sin_credencial":
            return { tono: "warning", titulo: "Falta la contraseña", detalle: "Escribe la contraseña de la extensión y guarda." };
    }
}

const enLlano = (seg: number) => (seg > 90 ? `${Math.ceil(seg / 60)} min` : `${Math.max(1, Math.round(seg))} s`);

/**
 * Por qué «Probar registro» no se pudo hacer ahora (429 del servidor, con `Retry-After`), en llano. Null
 * si el error es otro.
 */
export function esperaPruebaRegistro(e: unknown): string | null {
    if (!(e instanceof ErrorApi) || e.estado !== 429) return null;
    const espera = enLlano(e.reintentarEn ?? 60);
    switch (e.codigo) {
        case "credencial_rechazada":
            return "La centralita ya rechazó esta contraseña: escríbela otra vez con «Cambiar la contraseña».";
        case "demasiados_rechazos":
            return `La centralita rechazó varias contraseñas seguidas; para que no bloquee a Iris, espera ${espera}.`;
        default:
            return `Espera ${espera}.`;
    }
}

/** El teléfono de la empresa: un punto si está encendido; si no, lo que falta. */
export function estadoTelefonoCentralita(t: TelefonoCentralita | undefined | null): { tono: Tono; texto: string; falta: string | null } {
    if (!t) return { tono: "neutral", texto: "Sin configurar", falta: "Configura el WSS de la Issabel." };
    if (t.activo) return { tono: "success", texto: "Encendido", falta: null };
    if (!t.huellaWss) return { tono: "neutral", texto: "Sin configurar", falta: "Configura el WSS de la Issabel." };
    if (!t.restriccionesConfirmadasEn) return { tono: "warning", texto: "Apagado", falta: "Confirma que la Issabel está configurada como dice la guía." };
    if (!t.probadoEn) return { tono: "warning", texto: "Apagado", falta: "Prueba el teléfono con los datos guardados." };
    return { tono: "neutral", texto: "Apagado", falta: null };
}

/**
 * La Issabel rechazó la contraseña guardada y no se escribió otra desde entonces: no se ofrece «Probar
 * registro» ni «Reintentar», solo «Cambiar la contraseña» (el servidor responde 429 `credencial_rechazada`).
 */
export function credencialRechazada(x: Pick<ExtensionCentralita, "sipGuardadoEn" | "sipRechazadoEn">): boolean {
    if (!x.sipRechazadoEn) return false;
    return !x.sipGuardadoEn || Date.parse(x.sipGuardadoEn) <= Date.parse(x.sipRechazadoEn);
}

/** Dónde contesta cada persona y si su credencial está probada. */
export function estadoExtension(x: Pick<ExtensionCentralita, "tipo" | "sipGuardadoEn" | "sipProbadoEn" | "sipRechazadoEn">): { donde: string; tono: Tono; texto: string } {
    if (x.tipo === "escritorio") return { donde: "Teléfono de escritorio", tono: "neutral", texto: "Solo ve quién llama (sin audio en Iris)" };
    if (!x.sipGuardadoEn) return { donde: "En Iris", tono: "warning", texto: "Falta la contraseña de la extensión" };
    if (credencialRechazada(x)) return { donde: "En Iris", tono: "error", texto: "La centralita no aceptó la contraseña: hay que escribirla otra vez" };
    if (!x.sipProbadoEn) return { donde: "En Iris", tono: "error", texto: "La centralita no aceptó la contraseña o falta probar el registro" };
    return { donde: "En Iris", tono: "success", texto: "Registro probado" };
}

export const esquemaExtension = z
    .object({
        tipo: z.enum(["navegador", "escritorio"]),
        sipUsuario: z.string().trim(),
        cambiarSecreto: z.boolean(),
        sipSecreto: z.string(),
    })
    .superRefine((v, ctx) => {
        if (v.tipo !== "navegador") return;
        if (!/^[A-Za-z0-9_.-]{1,64}$/.test(v.sipUsuario)) ctx.addIssue({ code: "custom", path: ["sipUsuario"], message: "Solo letras, números, punto, guion y guion bajo" });
        if (!v.cambiarSecreto) return;
        if (v.sipSecreto.length < 8 || v.sipSecreto.length > 128) ctx.addIssue({ code: "custom", path: ["sipSecreto"], message: "Entre 8 y 128 caracteres" });
        else if (/[\r\n\0"\\]/.test(v.sipSecreto)) ctx.addIssue({ code: "custom", path: ["sipSecreto"], message: "Sin saltos de línea, comillas ni \\" });
    });

export type ValoresExtension = z.infer<typeof esquemaExtension>;
