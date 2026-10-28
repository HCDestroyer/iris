import type { Schema } from "@iris/contratos";

type Mensaje = Schema<"Mensaje">;

export type CorreoRetenido = {
    /** `remitente_no_verificado` o `remitente_no_es_contacto` cuando se reconoce en el texto; si no, `desconocido`. */
    motivo: "remitente_no_verificado" | "remitente_no_es_contacto" | "desconocido";
    texto: string;
    explicacion: string;
};

/**
 * Correo que la API guardó como nota interna automática (soporte/README.md, «Proceso»): nota
 * (`publico = false`) del sistema por el canal `correo`. Pasa cuando el remitente no está
 * verificado (DMARC/DKIM) o no es el contacto del ticket; no cambió el estado ni recibe respuestas.
 * El motivo exacto no viaja en `Mensaje` (pedido a I2): se reconoce por el texto que escribe la API
 * («Correo de X no verificado (…)» / «Correo de X no es el contacto del ticket»).
 */
export function correoRetenido(m: Mensaje): CorreoRetenido | null {
    if (m.publico || m.autorTipo !== "sistema" || m.canal !== "correo" || !m.cuerpo.startsWith("Correo de ")) {
        return null;
    }
    const explicacion = "Se guardó como nota interna: no cambió el estado del ticket y el remitente no recibe las respuestas.";
    if (m.cuerpo.includes(" no verificado (")) {
        return { motivo: "remitente_no_verificado", texto: "Correo sin verificar", explicacion: `El servidor de correo no autenticó al remitente (DMARC/DKIM). ${explicacion}` };
    }
    if (m.cuerpo.includes(" no es el contacto del ticket")) {
        return { motivo: "remitente_no_es_contacto", texto: "Correo de otro remitente", explicacion: `El remitente no es el contacto del ticket. ${explicacion}` };
    }
    return { motivo: "desconocido", texto: "Correo retenido", explicacion: `El remitente no es el contacto verificado del ticket. ${explicacion}` };
}
