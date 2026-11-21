import type { SesionTelefono } from "./tipos";

/*
 * El WebSocket SIP del teléfono para JsSIP (interfaz `JsSIP.Socket`), con el ticket de un uso como
 * subprotocolo (`Sec-WebSocket-Protocol: sip, iris-ticket.<ticket>`, contratos/voz.md §3.12): el
 * `WebSocketInterface` de JsSIP solo pide `sip`. Cada conexión (también las de reconexión de JsSIP)
 * pide un ticket nuevo con `abrir`: un ticket vale 30 s y una sola vez.
 *
 * Los cierres del servidor llegan con su código (`4001` reemplazada, `4003` sin acceso, `4008`
 * límite, `4502` la centralita no responde) para que el motor decida si JsSIP sigue reintentando.
 */

/** Códigos propios de cuando no se llegó a abrir el WebSocket (falló `abrir`). */
export const CIERRE_ABRIR = 4900;

export type CierreSocket = { codigo: number; motivo: string; error: unknown };

export type OpcionesSocket = {
    /** La sesión que ya se pidió (la primera conexión la usa sin volver a pedir). */
    primera: SesionTelefono;
    /** `POST /telefono/abrir`. */
    abrir: () => Promise<SesionTelefono>;
    /** Cada sesión nueva (para el TURN de las llamadas siguientes). */
    alSesion: (s: SesionTelefono) => void;
    /** Antes de avisar a JsSIP, el motor sabe por qué se cerró. */
    alCerrar: (c: CierreSocket) => void;
    /** Para las pruebas: cómo se crea el WebSocket. */
    crear?: (url: string, protocolos: string[]) => WebSocket;
    /** El origen (https://iris…); de fábrica, el de la página. */
    origen?: string;
};

export function urlWebSocket(ruta: string, origen: string): string {
    const u = new URL(ruta, origen);
    u.protocol = u.protocol === "https:" ? "wss:" : "ws:";
    return u.toString();
}

export class SocketIris {
    via_transport = "WSS";
    readonly url: string;
    readonly sip_uri: string;
    onconnect: () => void = () => undefined;
    ondisconnect: (error: boolean, code?: number, reason?: string) => void = () => undefined;
    ondata: (data: string) => void = () => undefined;

    private ws: WebSocket | null = null;
    private pendiente: SesionTelefono | null;
    private intento = 0;
    private estado: "cerrado" | "conectando" | "abierto" = "cerrado";
    private readonly o: OpcionesSocket;
    private readonly origen: string;

    constructor(o: OpcionesSocket) {
        this.o = o;
        this.origen = o.origen ?? window.location.origin;
        this.pendiente = o.primera;
        this.url = urlWebSocket(o.primera.ws, this.origen);
        this.sip_uri = `sip:${o.primera.sip.dominio};transport=ws`;
    }

    connect(): void {
        if (this.estado !== "cerrado") return;
        this.estado = "conectando";
        const intento = ++this.intento;
        const usar = this.pendiente;
        this.pendiente = null;
        const sesion = usar ? Promise.resolve(usar) : this.o.abrir();
        sesion.then(
            (s) => {
                if (intento !== this.intento || this.estado !== "conectando") return;
                if (!usar) this.o.alSesion(s);
                this.abrirWs(s, intento);
            },
            (error: unknown) => {
                if (intento !== this.intento || this.estado !== "conectando") return;
                this.estado = "cerrado";
                this.o.alCerrar({ codigo: CIERRE_ABRIR, motivo: "abrir", error });
                this.ondisconnect(true, CIERRE_ABRIR, "abrir");
            },
        );
    }

    private abrirWs(s: SesionTelefono, intento: number) {
        let ws: WebSocket;
        try {
            const url = urlWebSocket(s.ws, this.origen);
            const protocolos = s.protocolos ?? ["sip"];
            ws = this.o.crear ? this.o.crear(url, protocolos) : new WebSocket(url, protocolos);
        } catch (error) {
            this.estado = "cerrado";
            this.o.alCerrar({ codigo: CIERRE_ABRIR, motivo: "websocket", error });
            this.ondisconnect(true, CIERRE_ABRIR, "websocket");
            return;
        }
        this.ws = ws;
        ws.binaryType = "arraybuffer";
        ws.onopen = () => {
            if (intento !== this.intento) return;
            this.estado = "abierto";
            this.onconnect();
        };
        ws.onmessage = (e: MessageEvent) => {
            if (intento !== this.intento) return;
            // El servidor solo manda texto; un frame binario no es SIP.
            if (typeof e.data === "string") this.ondata(e.data);
        };
        ws.onclose = (e: CloseEvent) => {
            if (intento !== this.intento) return;
            this.ws = null;
            this.estado = "cerrado";
            this.o.alCerrar({ codigo: e.code, motivo: e.reason, error: null });
            this.ondisconnect(e.code !== 1000, e.code, e.reason);
        };
        ws.onerror = () => undefined;
    }

    disconnect(): void {
        this.intento++;
        this.estado = "cerrado";
        const ws = this.ws;
        this.ws = null;
        if (ws) {
            ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
            try {
                ws.close(1000);
            } catch {
                // Ya cerrado.
            }
        }
    }

    send(mensaje: string | ArrayBufferLike | Blob | ArrayBufferView): boolean {
        if (!this.ws || this.estado !== "abierto" || typeof mensaje !== "string") return false;
        try {
            this.ws.send(mensaje);
            return true;
        } catch {
            return false;
        }
    }

    isConnected(): boolean {
        return this.estado === "abierto";
    }

    isConnecting(): boolean {
        return this.estado === "conectando";
    }
}
