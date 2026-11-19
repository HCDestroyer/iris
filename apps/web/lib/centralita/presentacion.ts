import { z } from "zod";
import { formatearFecha, haceCuanto } from "@/lib/fechas";
import { duracionCorta } from "@/lib/voz/presentacion";
import type { Llamada } from "@/lib/voz/tipos";
import { PUERTO_AMI, type Centralita, type PruebaCentralita } from "./tipos";

/* Textos y reglas de la centralita (E4a). Todo en llano: el PM no es técnico. */

export type Tono = "success" | "warning" | "error" | "info" | "neutral";

/** El estado de la conexión como lo dice la pantalla (y si pide actuar). */
export function estadoConexion(c: Pick<Centralita, "estado" | "errorCodigo" | "activa" | "desconectadaDesde">, ahora: number): { tono: Tono; texto: string; detalle?: string; actuar: boolean } {
    if (!c.activa || c.estado === "desactivada") return { tono: "neutral", texto: "Apagada", detalle: "Iris no escucha la centralita.", actuar: false };
    if (c.estado === "conectada") return { tono: "success", texto: "Conectada", actuar: false };
    if (c.estado === "desconectada") {
        const desde = c.desconectadaDesde ? ` desde las ${horaCorta(c.desconectadaDesde)}` : "";
        return {
            tono: "warning",
            texto: "Sin conexión",
            detalle: `Sin conexión${desde}. Iris vuelve a intentar sola. Mientras tanto no salen las ventanas y las llamadas de ese rato no quedan en Iris.`,
            actuar: ahora - Date.parse(c.desconectadaDesde ?? "") > 5 * 60_000,
        };
    }
    switch (c.errorCodigo) {
        case "huella_cambiada":
            return {
                tono: "error",
                texto: "Detenida",
                detalle: "El certificado de la centralita cambió. Por seguridad Iris dejó de conectarse. Si tú lo cambiaste en Issabel, compáralo y confírmalo; si no, avisa a quien administra la centralita.",
                actuar: true,
            };
        case "permisos_de_mas":
            return { tono: "error", texto: "Detenida", detalle: "El usuario de la centralita puede hacer cambios. Iris solo acepta un usuario de solo lectura (write vacío).", actuar: true };
        case "credenciales":
            return { tono: "error", texto: "No entra", detalle: "La centralita rechazó el usuario o la contraseña. Revísalos en «Editar».", actuar: true };
        default:
            return { tono: "error", texto: "Con error", detalle: "No se pudo conectar. Revisa la conexión en «Editar».", actuar: true };
    }
}

function horaCorta(iso: string): string {
    return formatearFecha(iso)?.slice(-5) ?? "";
}

/** «hace 1 min», o nada si nunca hubo señal. */
export function ultimaSenal(c: Pick<Centralita, "ultimoEventoEn" | "conectadaEn">, ahora: number): string | undefined {
    const iso = c.ultimoEventoEn ?? c.conectadaEn;
    return iso ? haceCuanto(iso, ahora) : undefined;
}

/** La huella SHA-256 como la muestra `openssl` (AB:CD:…), en mayúsculas. */
export function huellaLegible(hex: string | null | undefined): string {
    if (!hex) return "";
    return (hex.toUpperCase().match(/.{1,2}/g) ?? []).join(":");
}

/** Qué decir después de «Probar conexión». */
export function textoPrueba(p: PruebaCentralita): { tono: Tono; titulo: string; detalle: string; listo: boolean } {
    switch (p.estado) {
        case "ok":
            return { tono: "success", titulo: "Conecta bien", detalle: "La conexión va cifrada, el usuario y la contraseña son correctos y solo puede leer.", listo: true };
        case "permisos_de_mas":
            return {
                tono: "error",
                titulo: "Este usuario puede hacer cambios en la centralita",
                detalle: `Iris solo necesita leer. Usa un usuario con «write» vacío y vuelve a probar${(p.accionesDeMas ?? []).length ? ` (hoy puede: ${(p.accionesDeMas ?? []).slice(0, 4).join(", ")})` : ""}.`,
                listo: false,
            };
        case "huella_distinta":
            return {
                tono: "error",
                titulo: "El certificado no es el guardado",
                detalle: "La centralita presentó otro certificado. Compara la huella nueva con la de Issabel antes de confiar en ella.",
                listo: false,
            };
        default:
            return {
                tono: "error",
                titulo: "No se pudo conectar",
                detalle: "Revisa, en este orden: que Iris llegue a esa dirección (VPN o túnel), que el AMI cifrado esté encendido en el puerto 5039, y el usuario y la contraseña.",
                listo: false,
            };
    }
}

/** Formulario de la conexión (espejo del validador del servidor). */
export const esquemaConexion = z
    .object({
        nombre: z.string().trim().min(1, "Ponle un nombre").max(80),
        host: z
            .string()
            .trim()
            .min(1, "Escribe la dirección")
            .max(253)
            .refine((v) => !/^[a-z]+:\/\//i.test(v) && !v.includes("/") && !v.includes(":"), "Solo la IP o el nombre, sin http:// ni puerto"),
        puerto: z.number().int().min(1).max(65535),
        usuario: z.string().trim().regex(/^[A-Za-z0-9_.-]{1,64}$/, "Solo letras, números, punto, guion y guion bajo"),
        cambiarSecreto: z.boolean(),
        secreto: z.string(),
    })
    .superRefine((v, ctx) => {
        if (!v.cambiarSecreto) return;
        if (v.secreto.length < 8 || v.secreto.length > 128) ctx.addIssue({ code: "custom", path: ["secreto"], message: "Entre 8 y 128 caracteres" });
        else if (/[\r\n\0]/.test(v.secreto)) ctx.addIssue({ code: "custom", path: ["secreto"], message: "Sin saltos de línea" });
    });

export type ValoresConexion = z.infer<typeof esquemaConexion>;

export function valoresConexion(c: Centralita | null): ValoresConexion {
    return { nombre: c?.nombre ?? "", host: c?.host ?? "", puerto: c?.puerto ?? PUERTO_AMI, usuario: c?.usuario ?? "", cambiarSecreto: c === null, secreto: "" };
}

/** La contraseña guardada no viaja a otro servidor: con otro host, puerto o usuario hay que escribirla. */
export function cambiaDestino(v: Pick<ValoresConexion, "host" | "puerto" | "usuario">, c: Centralita | null): boolean {
    return c !== null && (v.host.trim().toLowerCase() !== c.host || v.puerto !== c.puerto || v.usuario.trim() !== c.usuario);
}

/** Una línea de una llamada de la centralita para la ficha y el historial. */
export function lineaLlamada(l: Llamada): { tipo: "entrante" | "saliente" | "perdida"; titulo: string; detalle: string } {
    const persona = l.usuario?.nombre;
    const ext = l.centralita?.extension ?? l.centralita?.tuExtension;
    const cuando = formatearFecha(l.sonoEn) ?? "";
    const partes = [cuando];
    if (ext) partes.push(`ext. ${ext}`);
    if (l.ticketId) partes.push(`ticket #${l.ticketId}`);
    if (l.resultado) partes.push(TEXTO_RESULTADO[l.resultado] ?? l.resultado);
    if (l.estado === "perdida") {
        const a = (l.centralita?.receptores ?? []).map((r) => r.extension);
        return { tipo: "perdida", titulo: "No contestada", detalle: [cuando, a.length ? `sonó en ${a.join(" y ")}` : ""].filter(Boolean).join(" · ") };
    }
    const dur = l.duracionSeg != null ? ` · ${duracionCorta(l.duracionSeg)}` : "";
    if (l.direccion === "saliente") return { tipo: "saliente", titulo: `${persona ?? "Alguien"} llamó${dur}`, detalle: partes.join(" · ") };
    if (l.estado === "sonando") return { tipo: "entrante", titulo: "Sonando", detalle: partes.join(" · ") };
    return { tipo: "entrante", titulo: `${persona ? `Contestó ${persona}` : "Contestada"}${dur}`, detalle: partes.join(" · ") };
}

const TEXTO_RESULTADO: Record<string, string> = { resuelto: "Resuelto", devolver: "Devolver la llamada", equivocado: "Equivocado", otro: "Otro", ticket_creado: "Ticket creado" };

/** Resultados que se eligen al anotar una llamada de la centralita (el ticket va aparte). */
export const RESULTADOS_CENTRALITA = [
    { valor: "resuelto", texto: "Resuelto" },
    { valor: "devolver", texto: "Devolver la llamada" },
    { valor: "equivocado", texto: "Equivocado" },
    { valor: "otro", texto: "Otro" },
] as const;

/** Quién llama en la ventana de la centralita: nunca un nombre inventado (el CallerIDName no se usa). */
export function quienCentralita(l: Pick<Llamada, "titulo" | "numero" | "contacto" | "centralita">): { nombre: string; conocido: boolean; varios: boolean } {
    if (l.contacto.tipo === "cliente" && l.titulo && l.titulo !== l.numero) return { nombre: l.titulo, conocido: true, varios: false };
    const varios = (l.centralita?.coincidencias ?? 0) > 1;
    if (l.numero === "") return { nombre: "Número oculto", conocido: false, varios: false };
    return { nombre: varios ? l.numero : "Número desconocido", conocido: false, varios };
}
