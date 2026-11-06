import { z } from "zod";
import type { BotNumero } from "./contrato-11";

/*
 * «Cuando no hay agentes» de un número (fase 1.1): espejo del validador del servidor. El bot manda
 * el saludo y, si hay menú, hasta 9 opciones numeradas (1 a 9, una tecla en WhatsApp); cada opción
 * lleva la conversación a una cola con sus etiquetas.
 */

export const MAX_OPCIONES_MENU = 9;
export const MINUTOS_SIN_CONFIRMACION = 30;
export const MINUTOS_SIN_DISPONIBLES = 3;

const minutos = (max: number) =>
    z
        .string()
        .trim()
        .min(1, "Escribe los minutos")
        .refine((v) => /^\d+$/.test(v) && Number(v) >= 1 && Number(v) <= max, `Entre 1 y ${max} minutos`);

export const esquemaOpcionMenu = z.object({
    texto: z.string().trim().min(1, "Escribe el texto de la opción").max(60, "Hasta 60 caracteres"),
    colaId: z.string().min(1, "Elige la cola"),
    etiquetaIds: z.array(z.number()).max(5, "Hasta 5 etiquetas"),
});

export const esquemaCuandoNoHayAgentes = z
    .object({
        activo: z.boolean(),
        atiende: z.enum(["bot", "agente_autonomo"]).refine((v) => v === "bot", "El agente autónomo llega en la fase 3"),
        saludo: z.string().trim().min(1, "Escribe el saludo del bot").max(1024, "Hasta 1024 caracteres"),
        conMenu: z.boolean(),
        menu: z.array(esquemaOpcionMenu).max(MAX_OPCIONES_MENU, `Hasta ${MAX_OPCIONES_MENU} opciones`),
        minutosSinConfirmacion: minutos(1440),
        minutosSinDisponibles: minutos(1440),
    })
    // El menú, si está prendido, lleva al menos una opción.
    .superRefine((v, ctx) => {
        if (v.conMenu && v.menu.length === 0) ctx.addIssue({ code: "custom", path: ["menu"], message: "Agrega al menos una opción o apaga el menú" });
    });

export type ValoresCuandoNoHayAgentes = z.infer<typeof esquemaCuandoNoHayAgentes>;

/** Lo que se manda al servidor. */
export function cuerpoCuandoNoHayAgentes(v: ValoresCuandoNoHayAgentes): BotNumero {
    return {
        activo: v.activo,
        atiende: "bot" as const,
        saludo: v.saludo.trim(),
        menu: v.conMenu ? v.menu.map((o) => ({ texto: o.texto.trim(), colaId: Number(o.colaId), etiquetaIds: o.etiquetaIds })) : [],
        minutosSinConfirmacion: Number(v.minutosSinConfirmacion),
        minutosSinDisponibles: Number(v.minutosSinDisponibles),
    };
}

/** Lo que el cliente ve en WhatsApp: el saludo y las opciones numeradas. */
export function textoBotParaCliente(saludo: string, menu: readonly { texto: string }[]): string {
    const opciones = menu.filter((o) => o.texto.trim()).map((o, i) => `${i + 1}. ${o.texto.trim()}`);
    return opciones.length ? `${saludo.trim()}\n\n${opciones.join("\n")}` : saludo.trim();
}
