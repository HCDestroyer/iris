/*
 * Micrófono y WebRTC de la llamada (contratos/voz.md §3.3, pasos 5 y 6). El audio va directo del
 * navegador a Meta: aquí solo se arma la conexión con la oferta que dio `reclamar` y se entrega la
 * respuesta SDP para `contestar`.
 */

export type EstadoMicrofono = "concedido" | "denegado" | "sin_microfono" | "no_disponible";

/** ¿Este navegador puede contestar llamadas? (contexto seguro, micrófono y WebRTC) */
export function navegadorAdmiteLlamadas(): boolean {
    return typeof navigator !== "undefined" && !!navigator.mediaDevices?.getUserMedia && typeof RTCPeerConnection !== "undefined";
}

export class ErrorMicrofono extends Error {
    constructor(readonly estado: Exclude<EstadoMicrofono, "concedido">) {
        super(estado);
    }
}

/** Pide el micrófono. Lanza `ErrorMicrofono` con el motivo en llano. */
export async function pedirMicrofono(): Promise<MediaStream> {
    if (!navegadorAdmiteLlamadas()) throw new ErrorMicrofono("no_disponible");
    try {
        return await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true }, video: false });
    } catch (e) {
        const nombre = e instanceof DOMException ? e.name : "";
        if (nombre === "NotAllowedError" || nombre === "SecurityError") throw new ErrorMicrofono("denegado");
        if (nombre === "NotFoundError" || nombre === "OverconstrainedError" || nombre === "NotReadableError") throw new ErrorMicrofono("sin_microfono");
        throw new ErrorMicrofono("no_disponible");
    }
}

export function soltarMicrofono(stream: MediaStream | null): void {
    for (const pista of stream?.getTracks() ?? []) pista.stop();
}

/** Lo que el navegador ya decidió sobre el micrófono, sin preguntar (o `null` si no lo dice). */
export async function permisoMicrofono(): Promise<"granted" | "denied" | "prompt" | null> {
    try {
        const estado = await navigator.permissions.query({ name: "microphone" as PermissionName });
        return estado.state;
    } catch {
        return null;
    }
}

/** Lo común de una conexión de audio, entrante o saliente. */
export type Medio = {
    /** Se resuelve cuando el medio conecta; se rechaza si falla. */
    conectado: Promise<void>;
    /** Avisa si el medio se cae después de conectar. */
    alCaer: (aviso: () => void) => void;
    silenciar: (silenciada: boolean) => void;
    cerrar: () => void;
};

export type Conexion = Medio & {
    /** La respuesta SDP para `contestar`. */
    sdpRespuesta: string;
};

/** Una llamada saliente: el navegador ofrece y la respuesta de Meta llega después. */
export type Oferta = Medio & {
    /** La oferta SDP para `llamar`. */
    sdpOferta: string;
    /** Aplica la respuesta SDP de Meta (`GET /llamadas/{id}/respuesta-sdp`). Una sola vez. */
    aplicarRespuesta: (sdp: string) => Promise<void>;
};

/** Cuánto se tolera `disconnected` antes de dar la llamada por caída (suele recuperarse sola). */
const TOLERANCIA_CORTE_MS = 8000;

/** `RTCPeerConnection` con el micrófono, el audio remoto y la vigilancia del medio. */
function montar(servidores: readonly string[], microfono: MediaStream): { pc: RTCPeerConnection; medio: Medio } {
    const pc = new RTCPeerConnection({ iceServers: servidores.length ? [{ urls: [...servidores] }] : [] });
    const audio = typeof Audio === "undefined" ? null : new Audio();
    if (audio) audio.autoplay = true;
    let cerrada = false;
    let caida: (() => void) | null = null;
    let conecto = false;
    let corte = 0;

    const cerrar = () => {
        if (cerrada) return;
        cerrada = true;
        window.clearTimeout(corte);
        pc.oniceconnectionstatechange = null;
        pc.onicecandidate = null;
        pc.ontrack = null;
        try {
            pc.close();
        } catch {
            // Ya estaba cerrada.
        }
        if (audio) {
            audio.pause();
            audio.srcObject = null;
        }
        soltarMicrofono(microfono);
    };

    pc.ontrack = (e) => {
        if (!audio) return;
        audio.srcObject = e.streams[0] ?? new MediaStream([e.track]);
        void audio.play().catch(() => undefined);
    };
    for (const pista of microfono.getAudioTracks()) pc.addTrack(pista, microfono);

    // Diagnóstico en la consola del navegador: estados ICE y tipos de candidato locales (host, srflx,
    // relay), nunca direcciones ni el SDP. Junto con chrome://webrtc-internals dice si fue la red.
    const tipos: Record<string, number> = {};
    pc.onicecandidate = (e) => {
        const tipo = e.candidate?.type;
        if (tipo) tipos[tipo] = (tipos[tipo] ?? 0) + 1;
    };
    const conectado = new Promise<void>((resolver, rechazar) => {
        pc.oniceconnectionstatechange = () => {
            const estado = pc.iceConnectionState;
            console.info("[voz] ICE:", estado, "candidatos locales:", { ...tipos });
            if (estado === "connected" || estado === "completed") {
                window.clearTimeout(corte);
                conecto = true;
                resolver();
            } else if (estado === "failed" || estado === "closed") {
                window.clearTimeout(corte);
                if (conecto) caida?.();
                else rechazar(new Error("medio_fallido"));
            } else if (estado === "disconnected" && conecto) {
                window.clearTimeout(corte);
                corte = window.setTimeout(() => !cerrada && caida?.(), TOLERANCIA_CORTE_MS);
            }
        };
    });
    // Si nadie espera `conectado` (se cerró antes), que no quede un rechazo sin atender.
    conectado.catch(() => undefined);

    return {
        pc,
        medio: {
            conectado,
            alCaer: (aviso) => {
                caida = aviso;
            },
            silenciar: (silenciada) => {
                for (const pista of microfono.getAudioTracks()) pista.enabled = !silenciada;
            },
            cerrar,
        },
    };
}

/** Entrante: arma la conexión con la oferta de Meta y devuelve la respuesta. */
export async function conectar(sdpOferta: string, servidores: readonly string[], microfono: MediaStream): Promise<Conexion> {
    const { pc, medio } = montar(servidores, microfono);
    try {
        await pc.setRemoteDescription({ type: "offer", sdp: sdpOferta });
        await pc.setLocalDescription(await pc.createAnswer());
        // La respuesta del navegador va tal cual: el servidor la sanea antes de mandarla a Meta
        // (voz.md, revisión de seguridad de E1). No se espera a juntar candidatos: Meta es ICE-lite.
        const sdpRespuesta = pc.localDescription?.sdp;
        if (!sdpRespuesta) throw new Error("medio_fallido");
        return { ...medio, sdpRespuesta };
    } catch (e) {
        medio.cerrar();
        throw e;
    }
}

/**
 * Saliente (E1b): el navegador crea la oferta, que va a `POST /conversaciones/{id}/llamar` (el
 * servidor la sanea); la respuesta de Meta se aplica cuando el feed avisa `llamada_respuesta`.
 */
export async function ofrecer(microfono: MediaStream): Promise<Oferta> {
    const { pc, medio } = montar([], microfono);
    try {
        await pc.setLocalDescription(await pc.createOffer());
        const sdpOferta = pc.localDescription?.sdp;
        if (!sdpOferta) throw new Error("medio_fallido");
        let aplicada = false;
        return {
            ...medio,
            sdpOferta,
            aplicarRespuesta: async (sdp) => {
                if (aplicada) return;
                aplicada = true;
                await pc.setRemoteDescription({ type: "answer", sdp });
            },
        };
    } catch (e) {
        medio.cerrar();
        throw e;
    }
}
