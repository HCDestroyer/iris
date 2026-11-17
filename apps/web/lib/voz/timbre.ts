/*
 * Timbre de la llamada entrante, hecho con WebAudio (sin archivo de sonido): dos tonos cortos cada
 * 3 s. Si el navegador no deja sonar sin un gesto previo (pestaña recién abierta), el aviso se ve
 * igual; `data-timbre` del aviso dice si está sonando.
 */

export type Timbre = { detener: () => void };

type ConWebkit = Window & { webkitAudioContext?: typeof AudioContext };

export function sonarTimbre(): Timbre {
    if (typeof window === "undefined") return { detener: () => undefined };
    const Contexto = window.AudioContext ?? (window as ConWebkit).webkitAudioContext;
    if (!Contexto) return { detener: () => undefined };
    let contexto: AudioContext;
    try {
        contexto = new Contexto();
    } catch {
        return { detener: () => undefined };
    }
    void contexto.resume().catch(() => undefined);

    function tono(inicio: number, frecuencia: number) {
        const oscilador = contexto.createOscillator();
        const volumen = contexto.createGain();
        oscilador.type = "sine";
        oscilador.frequency.value = frecuencia;
        volumen.gain.setValueAtTime(0, inicio);
        volumen.gain.linearRampToValueAtTime(0.16, inicio + 0.03);
        volumen.gain.linearRampToValueAtTime(0, inicio + 0.38);
        oscilador.connect(volumen).connect(contexto.destination);
        oscilador.start(inicio);
        oscilador.stop(inicio + 0.4);
    }
    function ciclo() {
        if (contexto.state !== "running") return;
        const t = contexto.currentTime + 0.02;
        tono(t, 660);
        tono(t + 0.45, 880);
    }
    ciclo();
    const id = window.setInterval(ciclo, 3000);
    return {
        detener: () => {
            window.clearInterval(id);
            void contexto.close().catch(() => undefined);
        },
    };
}
