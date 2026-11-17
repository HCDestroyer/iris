/*
 * Micrófono y WebRTC de la llamada (contratos/voz.md §3.3, pasos 5 y 6). El audio va directo del
 * navegador a Meta: aquí solo se arma la conexión con la oferta que dio `reclamar` y se entrega la
 * respuesta SDP para `contestar`.
 */

import { respuestaSinRedInterna } from "./sdp";

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

export type Conexion = {
    /** La respuesta SDP para `contestar`. */
    sdpRespuesta: string;
    /** Se resuelve cuando el medio conecta; se rechaza si falla. */
    conectado: Promise<void>;
    /** Avisa si el medio se cae después de conectar. */
    alCaer: (aviso: () => void) => void;
    silenciar: (silenciada: boolean) => void;
    cerrar: () => void;
};

/** Cuánto se tolera `disconnected` antes de dar la llamada por caída (suele recuperarse sola). */
const TOLERANCIA_CORTE_MS = 8000;

/** Arma el `RTCPeerConnection` con la oferta de Meta y devuelve la respuesta. */
export async function conectar(sdpOferta: string, servidores: readonly string[], microfono: MediaStream): Promise<Conexion> {
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

    try {
        pc.ontrack = (e) => {
            if (!audio) return;
            audio.srcObject = e.streams[0] ?? new MediaStream([e.track]);
            void audio.play().catch(() => undefined);
        };
        for (const pista of microfono.getAudioTracks()) pc.addTrack(pista, microfono);

        const conectado = new Promise<void>((resolver, rechazar) => {
            pc.oniceconnectionstatechange = () => {
                const estado = pc.iceConnectionState;
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

        await pc.setRemoteDescription({ type: "offer", sdp: sdpOferta });
        await pc.setLocalDescription(await pc.createAnswer());
        // No se espera a juntar candidatos: Meta (ICE-lite) aprende la dirección del navegador de sus
        // comprobaciones, y los candidatos con la red interna no deben viajar (ver `sdp.ts`).
        const local = pc.localDescription?.sdp;
        if (!local) throw new Error("medio_fallido");
        const sdpRespuesta = respuestaSinRedInterna(local);
        return {
            sdpRespuesta,
            conectado,
            alCaer: (aviso) => {
                caida = aviso;
            },
            silenciar: (silenciada) => {
                for (const pista of microfono.getAudioTracks()) pista.enabled = !silenciada;
            },
            cerrar,
        };
    } catch (e) {
        cerrar();
        throw e;
    }
}
