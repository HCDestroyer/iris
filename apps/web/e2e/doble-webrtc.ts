import type { BrowserContext, Page } from "@playwright/test";

/*
 * Dobles del micrófono y de `RTCPeerConnection` para las pruebas de voz: no hay Meta al otro lado,
 * así que la conexión «conecta» (o no) según lo que pida la prueba. Se instalan antes de cargar la
 * página (`addInitScript`) y se manejan desde la prueba con `window.__voz`.
 */

export type ControlVoz = {
    /** Qué hace `getUserMedia`: da un micrófono, lo niega, no hay ninguno, o no responde nunca. */
    microfono: "ok" | "denegado" | "sin_microfono" | "colgado";
    /** Lo que dice `navigator.permissions.query({ name: "microphone" })`. */
    permiso: "prompt" | "granted" | "denied";
    /** Qué hace el medio tras la respuesta SDP: conecta, no conecta nunca o falla. */
    medio: "conecta" | "nunca" | "falla";
    /** Veces que se pidió el micrófono. */
    pedidos: number;
    /** Conexiones creadas (abiertas o no). */
    conexiones: { cerrada: boolean; oferta: string | null }[];
    /** ¿Las pistas del micrófono en uso están activas (no silenciadas)? `null` sin micrófono. */
    pistasActivas: () => boolean | null;
    /** ¿Quedó algún micrófono abierto? */
    microfonoAbierto: () => boolean;
    /** Tira el medio de la conexión abierta (red caída). */
    cortar: () => void;
};

declare global {
    interface Window {
        __voz: ControlVoz;
    }
}

function instalar(inicial: Partial<Pick<ControlVoz, "microfono" | "permiso" | "medio">>) {
    type Pista = { kind: string; enabled: boolean; readyState: "live" | "ended"; stop: () => void };
    const flujos: { pistas: Pista[] }[] = [];
    const abiertas: Doble[] = [];
    const control: ControlVoz = {
        microfono: inicial.microfono ?? "ok",
        permiso: inicial.permiso ?? "prompt",
        medio: inicial.medio ?? "conecta",
        pedidos: 0,
        conexiones: [],
        pistasActivas: () => {
            const vivas = flujos.flatMap((f) => f.pistas).filter((p) => p.readyState === "live");
            return vivas.length === 0 ? null : vivas.every((p) => p.enabled);
        },
        microfonoAbierto: () => flujos.some((f) => f.pistas.some((p) => p.readyState === "live")),
        cortar: () => {
            for (const c of abiertas) c.cambiar("failed");
        },
    };
    window.__voz = control;

    const error = (nombre: string) => new DOMException(nombre, nombre);
    const dispositivos = {
        getUserMedia: () => {
            control.pedidos += 1;
            if (control.microfono === "denegado") {
                control.permiso = "denied";
                return Promise.reject(error("NotAllowedError"));
            }
            if (control.microfono === "sin_microfono") return Promise.reject(error("NotFoundError"));
            if (control.microfono === "colgado") return new Promise(() => undefined);
            control.permiso = "granted";
            const pista: Pista = {
                kind: "audio",
                enabled: true,
                readyState: "live",
                stop() {
                    this.readyState = "ended";
                },
            };
            const flujo = { pistas: [pista], getTracks: () => [pista], getAudioTracks: () => [pista] };
            flujos.push(flujo);
            return Promise.resolve(flujo);
        },
    };
    Object.defineProperty(navigator, "mediaDevices", { configurable: true, value: dispositivos });
    Object.defineProperty(navigator, "permissions", {
        configurable: true,
        value: { query: (d: { name: string }) => (d.name === "microphone" ? Promise.resolve({ state: control.permiso }) : Promise.reject(new TypeError("no"))) },
    });

    // Como un navegador real con el micrófono permitido: trae la IP privada del equipo.
    const RESPUESTA = [
        "v=0",
        "o=- 2 2 IN IP4 127.0.0.1",
        "s=-",
        "t=0 0",
        "m=audio 54321 UDP/TLS/RTP/SAVPF 111",
        "c=IN IP4 192.168.1.23",
        "a=rtcp:9 IN IP4 192.168.1.23",
        "a=candidate:1 1 udp 2122260223 192.168.1.23 54321 typ host generation 0",
        "a=candidate:3 1 udp 1686052607 190.56.10.20 54321 typ srflx raddr 192.168.1.23 rport 54321 generation 0",
        "a=ice-ufrag:abcd",
        "a=ice-pwd:0123456789abcdefghijklmn",
        "a=fingerprint:sha-256 AA:BB:CC:DD",
        "a=setup:active",
        "a=mid:0",
        "a=sendrecv",
        "a=rtcp-mux",
        "a=rtpmap:111 opus/48000/2",
        "",
    ].join("\r\n");
    class Doble {
        iceConnectionState = "new";
        iceGatheringState = "complete";
        localDescription: { type: string; sdp: string } | null = null;
        oniceconnectionstatechange: (() => void) | null = null;
        onicegatheringstatechange: (() => void) | null = null;
        ontrack: (() => void) | null = null;
        registro = { cerrada: false, oferta: null as string | null };
        constructor() {
            control.conexiones.push(this.registro);
            abiertas.push(this);
        }
        addTrack() {
            return {};
        }
        setRemoteDescription(d: { sdp: string }) {
            this.registro.oferta = d.sdp;
            return Promise.resolve();
        }
        createAnswer() {
            return Promise.resolve({ type: "answer", sdp: RESPUESTA });
        }
        setLocalDescription(d: { type: string; sdp: string }) {
            this.localDescription = d;
            if (control.medio === "conecta") setTimeout(() => this.cambiar("connected"), 30);
            if (control.medio === "falla") setTimeout(() => this.cambiar("failed"), 30);
            return Promise.resolve();
        }
        cambiar(estado: string) {
            if (this.registro.cerrada) return;
            this.iceConnectionState = estado;
            this.oniceconnectionstatechange?.();
        }
        close() {
            this.registro.cerrada = true;
            this.iceConnectionState = "closed";
        }
    }
    Object.defineProperty(window, "RTCPeerConnection", { configurable: true, writable: true, value: Doble });
}

/** Instala los dobles en todas las páginas del contexto (antes de cargarlas). */
export async function instalarDobleVoz(destino: Page | BrowserContext, inicial: Partial<Pick<ControlVoz, "microfono" | "permiso" | "medio">> = {}) {
    await destino.addInitScript(instalar, inicial);
}

/** Cambia el comportamiento de los dobles en una página ya cargada. */
export async function ajustarVoz(page: Page, cambio: Partial<Pick<ControlVoz, "microfono" | "permiso" | "medio">>) {
    await page.evaluate((c) => Object.assign(window.__voz, c), cambio);
}
