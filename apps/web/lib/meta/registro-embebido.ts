/*
 * Registro embebido de Meta (Embedded Signup) para conectar un número de WhatsApp Business con QR
 * (contratos/conversaciones.md §15.2). Lógica pura, sin el SDK ni el DOM, para probarla con Vitest:
 *
 * - `origenDeFacebook`: solo se aceptan mensajes de `https://facebook.com`, `https://www.facebook.com`
 *   o `https://web.facebook.com` (por `event.origin`, nunca por el contenido).
 * - `leerMensaje`: interpreta `{ type: "WA_EMBEDDED_SIGNUP", event, data }`.
 * - `RegistroEmbebido`: junta el `code` del callback de `FB.login` con el `waba_id` y el
 *   `phone_number_id` del mensaje, en cualquier orden, y avisa una sola vez.
 *
 * Documentación de Meta: https://developers.facebook.com/docs/whatsapp/embedded-signup/implementation/
 */

const ORIGENES_FACEBOOK = new Set(["https://facebook.com", "https://www.facebook.com", "https://web.facebook.com"]);

export function origenDeFacebook(origen: string): boolean {
    return ORIGENES_FACEBOOK.has(origen);
}

export type MensajeRegistro =
    | { tipo: "terminado"; wabaId: string; phoneNumberId: string }
    | { tipo: "cancelado"; paso: string | null }
    | { tipo: "error"; mensaje: string | null; codigo: string | null };

const SOLO_DIGITOS = /^[0-9]{1,30}$/;

function texto(v: unknown): string | null {
    return typeof v === "string" && v.trim() ? v.trim() : typeof v === "number" ? String(v) : null;
}

/** El `data` del `message` de Meta (objeto o JSON en texto); `null` si no es del registro. */
export function leerMensaje(dato: unknown): MensajeRegistro | null {
    let d: unknown = dato;
    if (typeof d === "string") {
        try {
            d = JSON.parse(d);
        } catch {
            return null;
        }
    }
    if (!d || typeof d !== "object") return null;
    const o = d as { type?: unknown; event?: unknown; data?: Record<string, unknown> | null };
    if (o.type !== "WA_EMBEDDED_SIGNUP") return null;
    const datos = o.data ?? {};
    if (o.event === "FINISH_WHATSAPP_BUSINESS_APP_ONBOARDING" || o.event === "FINISH" || o.event === "FINISH_ONLY_WABA") {
        const wabaId = texto(datos.waba_id);
        const phoneNumberId = texto(datos.phone_number_id);
        if (!wabaId || !phoneNumberId || !SOLO_DIGITOS.test(wabaId) || !SOLO_DIGITOS.test(phoneNumberId)) {
            return { tipo: "error", mensaje: "Meta no mandó el número ni la cuenta de WhatsApp.", codigo: null };
        }
        return { tipo: "terminado", wabaId, phoneNumberId };
    }
    if (o.event === "CANCEL") return { tipo: "cancelado", paso: texto(datos.current_step) };
    if (o.event === "ERROR") return { tipo: "error", mensaje: texto(datos.error_message), codigo: texto(datos.error_code) };
    return null;
}

/** Respuesta del callback de `FB.login` (solo lo que usamos). */
export type RespuestaLogin = { authResponse?: { code?: string | null } | null; status?: string } | null | undefined;

export type ResultadoRegistro =
    | { tipo: "listo"; code: string; wabaId: string; phoneNumberId: string }
    | { tipo: "cancelado"; paso: string | null }
    | { tipo: "error"; motivo: "meta" | "sin_datos"; mensaje: string | null; codigo: string | null };

/**
 * Junta las dos mitades del registro. El `code` vive ~30 s: en cuanto están el código y los ids,
 * `alTerminar` se llama de inmediato (una sola vez). Si el login vuelve sin código, es «cancelado»
 * salvo que Meta ya haya dicho «error». Si llega el código y los ids no llegan en `esperaIdsMs`,
 * es un error (el código ya no alcanzaría).
 */
export class RegistroEmbebido {
    private code: string | null = null;
    private ids: { wabaId: string; phoneNumberId: string } | null = null;
    private terminado = false;
    private ultimoError: MensajeRegistro | null = null;
    private espera: ReturnType<typeof setTimeout> | null = null;

    constructor(
        private readonly alTerminar: (r: ResultadoRegistro) => void,
        private readonly esperaIdsMs = 10_000,
    ) {}

    get cerrado(): boolean {
        return this.terminado;
    }

    /** Un `message` de la ventana: se ignora si no es de Facebook o no es del registro. */
    alMensaje(origen: string, dato: unknown): boolean {
        if (this.terminado || !origenDeFacebook(origen)) return false;
        const m = leerMensaje(dato);
        if (!m) return false;
        if (m.tipo === "terminado") {
            this.ids = { wabaId: m.wabaId, phoneNumberId: m.phoneNumberId };
            this.intentar();
        } else if (m.tipo === "cancelado") {
            // El callback del login llega después sin código; se cierra ahí (o ya).
            this.ultimoError = m;
        } else {
            this.ultimoError = m;
        }
        return true;
    }

    alLogin(respuesta: RespuestaLogin): void {
        if (this.terminado) return;
        const code = respuesta?.authResponse?.code ?? null;
        if (code) {
            this.code = code;
            if (!this.intentar()) {
                this.espera = setTimeout(() => this.cerrar({ tipo: "error", motivo: "sin_datos", mensaje: "Meta no mandó el número ni la cuenta de WhatsApp.", codigo: null }), this.esperaIdsMs);
            }
            return;
        }
        const e = this.ultimoError;
        if (e?.tipo === "error") this.cerrar({ tipo: "error", motivo: "meta", mensaje: e.mensaje, codigo: e.codigo });
        else this.cerrar({ tipo: "cancelado", paso: e?.tipo === "cancelado" ? e.paso : null });
    }

    /** Se fue de la pantalla: no se avisa nada más. */
    abandonar(): void {
        this.terminado = true;
        if (this.espera) clearTimeout(this.espera);
    }

    private intentar(): boolean {
        if (!this.code || !this.ids) return false;
        this.cerrar({ tipo: "listo", code: this.code, ...this.ids });
        return true;
    }

    private cerrar(r: ResultadoRegistro): void {
        if (this.terminado) return;
        this.terminado = true;
        if (this.espera) clearTimeout(this.espera);
        this.alTerminar(r);
    }
}
