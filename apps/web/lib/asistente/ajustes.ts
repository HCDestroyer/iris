import { z } from "zod";
import type { AjustesAsistente, EntradaAjustesAsistente, ProveedorIA } from "./tipos";

/*
 * Formulario de «Administración › Asistente de IA», espejo de `POST /asistente/ajustes/actualizar`
 * (contratos/conversaciones.md §16.1): activar exige dirección y modelo (salvo Anthropic, que tiene
 * los suyos por defecto) y, con Anthropic, una clave; la dirección es http/https sin usuario ni
 * contraseña; el tope va de 1 000 a 1 000 000 000 tokens; los precios son decimales como texto.
 */

export const PROVEEDORES: readonly { valor: ProveedorIA; titulo: string; detalle: string; recomendado?: boolean; nube: boolean }[] = [
    {
        valor: "ollama",
        titulo: "Ollama · servidor propio",
        detalle: "Los mensajes no salen de tu red. Sin costo por uso; necesitas un servidor con tarjeta gráfica.",
        recomendado: true,
        nube: false,
    },
    {
        valor: "openai_compatible",
        titulo: "Compatible con OpenAI",
        detalle: "ChatGPT, OpenRouter, Groq, DeepSeek, Mistral, vLLM, LM Studio y otros: solo cambia la dirección.",
        nube: true,
    },
    { valor: "anthropic", titulo: "Anthropic (Claude)", detalle: "Servicio en la nube, con cobro por uso.", nube: true },
];

export const FUENTES: readonly { clave: keyof AjustesAsistente["fuentes"]; titulo: string; detalle: string }[] = [
    { clave: "manuales", titulo: "Biblioteca de manuales", detalle: "Los «Para el cliente» y los «Del equipo». Nunca cita al cliente los del equipo." },
    { clave: "argos", titulo: "Datos del cliente en Argos", detalle: "Servicio, plan y estado. Solo del cliente asociado a la conversación." },
    { clave: "horus", titulo: "Fallas de red de Horus", detalle: "Routers caídos en la zona del cliente." },
    { clave: "historial", titulo: "Historial de la conversación", detalle: "Los últimos 30 mensajes, sin notas internas." },
];

export const NUNCA: readonly string[] = [
    "Envía un mensaje sin que lo revise una persona",
    "Ve contraseñas, documentos o códigos (llegan como ***)",
    "Lee las notas internas",
    "Registra pagos, promesas ni reconexiones",
    "Promete precios o plazos que no estén en Argos o en un manual",
];

const TOPE_MIN = 1_000;
const TOPE_MAX = 1_000_000_000;
const PRECIO = /^\d{1,9}(\.\d{1,4})?$/;

/** ¿Es una dirección que el servidor acepta? (http/https, sin usuario ni contraseña) */
export function urlValida(texto: string): boolean {
    try {
        const u = new URL(texto);
        return (u.protocol === "http:" || u.protocol === "https:") && !u.username && !u.password && u.hostname !== "";
    } catch {
        return false;
    }
}

export const esquemaAjustesAsistente = z
    .object({
        activo: z.boolean(),
        proveedor: z.enum(["ollama", "openai_compatible", "anthropic"]),
        url: z.string().trim().max(500, "Hasta 500 caracteres"),
        /** Vacío con una clave guardada: se queda la guardada. */
        clave: z.string().max(500, "Hasta 500 caracteres"),
        /** Ya hay una clave guardada (no es un campo: lo llena la pantalla). */
        claveGuardada: z.boolean(),
        /** «Cambiar» la clave abierto. */
        cambiarClave: z.boolean(),
        modelo: z.string().trim().max(200, "Hasta 200 caracteres"),
        modo: z.enum(["automatico", "boton"]),
        fuentes: z.object({ manuales: z.boolean(), argos: z.boolean(), horus: z.boolean(), historial: z.boolean() }),
        topeTokensMes: z.string().trim(),
        precioEntradaMillon: z.string().trim(),
        precioSalidaMillon: z.string().trim(),
    })
    .superRefine((d, ctx) => {
        const anthropic = d.proveedor === "anthropic";
        if (d.url && !urlValida(d.url)) ctx.addIssue({ code: "custom", path: ["url"], message: "Una dirección http:// o https:// sin usuario ni contraseña" });
        if (d.activo && !anthropic && !d.url) ctx.addIssue({ code: "custom", path: ["url"], message: "Falta la dirección para activar el asistente" });
        if (d.activo && !anthropic && !d.modelo) ctx.addIssue({ code: "custom", path: ["modelo"], message: "Falta el modelo para activar el asistente" });
        const conClave = d.cambiarClave ? d.clave.trim() !== "" || d.claveGuardada : d.claveGuardada;
        if (d.activo && anthropic && !conClave) ctx.addIssue({ code: "custom", path: ["clave"], message: "Anthropic necesita una clave para activar el asistente" });
        const tope = Number(d.topeTokensMes);
        if (!/^\d+$/.test(d.topeTokensMes) || tope < TOPE_MIN || tope > TOPE_MAX) {
            ctx.addIssue({ code: "custom", path: ["topeTokensMes"], message: "Entre 1,000 y 1,000,000,000 tokens" });
        }
        for (const campo of ["precioEntradaMillon", "precioSalidaMillon"] as const) {
            if (d[campo] && !PRECIO.test(d[campo])) ctx.addIssue({ code: "custom", path: [campo], message: "Un número con hasta 4 decimales, p. ej. 3.00" });
        }
    });

export type ValoresAjustes = z.infer<typeof esquemaAjustesAsistente>;

export function valoresDe(a: AjustesAsistente): ValoresAjustes {
    return {
        activo: a.activo,
        proveedor: a.proveedor,
        url: a.url,
        clave: "",
        claveGuardada: a.claveGuardadaEn !== null,
        cambiarClave: a.claveGuardadaEn === null,
        modelo: a.modelo,
        modo: a.modo,
        fuentes: { ...a.fuentes },
        topeTokensMes: String(a.topeTokensMes),
        precioEntradaMillon: a.precioEntradaMillon ?? "",
        precioSalidaMillon: a.precioSalidaMillon ?? "",
    };
}

/**
 * Cuerpo para el servidor. La clave solo va si se escribió una nueva: ausente deja la guardada
 * (nunca se manda `""`, que la borraría).
 */
export function entradaDe(d: ValoresAjustes, version: number): EntradaAjustesAsistente {
    const clave = d.cambiarClave ? d.clave.trim() : "";
    return {
        activo: d.activo,
        proveedor: d.proveedor,
        url: d.url.trim(),
        modelo: d.modelo.trim(),
        modo: d.modo,
        fuentes: { ...d.fuentes },
        topeTokensMes: Number(d.topeTokensMes),
        precioEntradaMillon: d.precioEntradaMillon || null,
        precioSalidaMillon: d.precioSalidaMillon || null,
        version,
        ...(clave ? { clave } : {}),
    };
}

/** El proveedor manda los mensajes fuera de la red de la empresa. */
export const esNube = (p: ProveedorIA) => PROVEEDORES.find((x) => x.valor === p)?.nube ?? true;

/** Ejemplo de dirección por proveedor (texto de ayuda del campo). */
export function ejemploUrl(p: ProveedorIA): string {
    if (p === "ollama") return "http://10.0.0.20:11434";
    if (p === "anthropic") return "Vacía: https://api.anthropic.com";
    return "https://openrouter.ai/api/v1";
}
