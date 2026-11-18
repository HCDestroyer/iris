import type { Schema } from "@iris/contratos";

/*
 * Por qué no se envió un mensaje, en llano (contratos/conversaciones.md §1.1 y mensajes). Cuando
 * WhatsApp rechaza el token o los permisos del número, el agente tiene que saber que no es cosa
 * suya ni del cliente, y quien administra los números, a dónde ir a arreglarlo.
 */

type CodigoFalla = Schema<"CodigoFalla">;
type MensajeFallido = { estadoEntrega?: string | null | undefined; motivoFalla?: string | null | undefined; codigoFalla?: CodigoFalla | undefined };

export type ExplicacionFalla = {
    texto: string;
    /** Lo arregla quien administra los números: se le ofrece ir al número. */
    revisarNumero: boolean;
    /** «Código de WhatsApp: 190 (HTTP 401), subcódigo 463», si el servidor lo mandó. */
    codigo: string | null;
};

/** El código de Meta, discreto. Solo llega a quien administra los números; cada parte puede faltar. */
export function textoCodigoFalla(c: CodigoFalla | null | undefined): string | null {
    if (!c) return null;
    const partes: string[] = [];
    if (c.codigo !== null) partes.push(String(c.codigo));
    if (c.status !== null) partes.push(c.codigo !== null ? `(HTTP ${c.status})` : `HTTP ${c.status}`);
    if (partes.length === 0 && c.subcodigo === null) return null;
    const principal = partes.length > 0 ? `Código de WhatsApp: ${partes.join(" ")}` : "Código de WhatsApp";
    return c.subcodigo !== null ? `${principal}${partes.length > 0 ? "," : ":"} subcódigo ${c.subcodigo}` : principal;
}

/**
 * La explicación de un mensaje fallido que merece más que el pie de la burbuja; `null` si con el
 * pie basta (número inválido, ventana cerrada…). `administra`: tiene `mensajeria.configurar`.
 */
export function explicarFalla(m: MensajeFallido, administra: boolean): ExplicacionFalla | null {
    if (m.estadoEntrega !== "fallido") return null;
    const codigo = textoCodigoFalla(m.codigoFalla);
    switch (m.motivoFalla) {
        case "token_invalido":
            return {
                texto: administra
                    ? "No se envió: el token de WhatsApp de este número venció o no sirve. Revísalo en Números de WhatsApp."
                    : "No se envió: WhatsApp rechazó la conexión de este número. Avisa a un administrador.",
                revisarNumero: administra,
                codigo,
            };
        case "sin_permiso_meta":
            return {
                texto: administra
                    ? "No se envió: al token de WhatsApp de este número le falta un permiso. Revísalo en Números de WhatsApp."
                    : "No se envió: a la conexión de este número le falta un permiso en WhatsApp. Avisa a un administrador.",
                revisarNumero: administra,
                codigo,
            };
        case "rechazado_por_canal":
            // Depende del destinatario o de la cuenta, no del token: no hay nada que revisar en el número.
            return { texto: "WhatsApp no aceptó el mensaje.", revisarNumero: false, codigo };
        default:
            // Otro motivo: el pie ya lo dice; solo se añade el código si vino.
            return codigo ? { texto: "", revisarNumero: false, codigo } : null;
    }
}
