import type { ConfigUa, UaSip } from "./motor";

/*
 * El agente SIP de verdad: JsSIP 3.13 (MIT; https://jssip.net). Se carga solo cuando la persona
 * enciende el teléfono (import dinámico: no pesa en el resto del panel).
 *
 * Configuración que pide el puente de Iris (contratos/voz.md §3.12): el socket propio con el ticket
 * como subprotocolo, `contact_uri` con su usuario SIP y host `.invalid`, registro de 5 minutos (el
 * puente acepta hasta 600 s), sin contraseña (la pone el servidor) y reintentos de 5 a 30 s.
 */

export async function cargarJssip(): Promise<(c: ConfigUa) => UaSip> {
    const JsSIP = await import("jssip");
    JsSIP.debug.disable();
    return (c) => {
        const ua = new JsSIP.UA({
            sockets: c.socket as unknown as import("jssip").Socket,
            uri: c.uri,
            contact_uri: c.contacto,
            display_name: c.nombre || undefined,
            register: true,
            register_expires: 300,
            session_timers: false,
            no_answer_timeout: 90,
            connection_recovery_min_interval: 5,
            connection_recovery_max_interval: 30,
            user_agent: "Iris",
        });
        return ua as unknown as UaSip;
    };
}
