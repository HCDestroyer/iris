import { esErrorDeApi, esErrorMicrofono } from "./errores";
import { motivoFin } from "./presentacion";
import { CIERRE_ABRIR, SocketIris, type CierreSocket } from "./socket";
import type { ConexionTelefono, ErrorTelefono, EstadoTelefono, LlamadaTelefono, MotivoTelefono, SesionTelefono } from "./tipos";

/*
 * El teléfono de esta pestaña (contratos/voz.md §3.12, E4c): una sola fuente de verdad para la barra
 * superior, la ventana de la entrante, la barra y el panel de la llamada. Envuelve el agente SIP
 * (JsSIP, `jssip.ts`) detrás de `UaSip`/`SesionSip` para probarlo sin navegador.
 *
 * Reglas que vienen del puente de Iris:
 * - Cada conexión pide un ticket nuevo (`abrir`); el `Contact` es `sip:<usuario>@<azar>.invalid;transport=ws`.
 * - El audio va siempre por el TURN de Iris (`iceTransportPolicy: relay`); las credenciales TURN
 *   vencen (`iceVence`): antes de contestar o llamar, si vencen pronto, se piden otras.
 * - `4001` (otra pestaña o dispositivo tomó el teléfono) y `4003` (sin acceso; `credenciales` si la
 *   Issabel rechazó la contraseña) detienen el agente: reintentar solo empeoraría (fail2ban).
 * - El DTMF va por INFO, de a un dígito.
 */

// El agente SIP, lo mínimo que usa el motor ---------------------------------------------------

export type EventoSip = {
    cause?: string;
    originator?: "local" | "remote" | "system";
    session?: SesionSip;
    request?: { getHeader: (nombre: string) => string | undefined };
    peerconnection?: RTCPeerConnection;
    candidate?: RTCIceCandidate;
    ready?: () => void;
    response?: { status_code?: number };
};

export type SesionSip = {
    readonly direction: "incoming" | "outgoing";
    readonly remote_identity: { uri: { user: string }; display_name?: string };
    readonly connection?: RTCPeerConnection | null;
    answer: (o: { mediaStream?: MediaStream; pcConfig?: RTCConfiguration; mediaConstraints?: MediaStreamConstraints }) => void;
    terminate: (o?: { status_code?: number; reason_phrase?: string }) => void;
    mute: (o?: { audio?: boolean }) => void;
    unmute: (o?: { audio?: boolean }) => void;
    hold: () => boolean;
    unhold: () => boolean;
    sendDTMF: (tonos: string, o?: { transportType?: "INFO" | "RFC2833"; duration?: number }) => void;
    refer: (destino: string, o?: { eventHandlers?: Record<string, (e: EventoSip) => void> }) => void;
    isEnded: () => boolean;
    on: (evento: string, f: (e: EventoSip) => void) => void;
};

export type OpcionesLlamadaSip = { mediaStream?: MediaStream; pcConfig: RTCConfiguration; mediaConstraints: MediaStreamConstraints };

export type UaSip = {
    start: () => void;
    stop: () => void;
    on: (evento: "connected" | "disconnected" | "registered" | "unregistered" | "registrationFailed" | "newRTCSession", f: (e: EventoSip) => void) => void;
    call: (destino: string, o: OpcionesLlamadaSip) => SesionSip;
};

/** `registrar: false`: solo marca (app de campo, primera etapa): sin REGISTER ni entrantes. */
export type ConfigUa = { socket: SocketIris; uri: string; contacto: string; nombre: string; registrar: boolean };

export type DependenciasMotor = {
    abrir: () => Promise<SesionTelefono>;
    crearUa: (c: ConfigUa) => UaSip;
    pedirMicrofono: () => Promise<MediaStream>;
    /** Pone a sonar el audio de la llamada; devuelve cómo pararlo. */
    reproducir: (s: MediaStream) => () => void;
    timbre: () => { detener: () => void };
    /** El servidor cortó por algo que dice `GET /telefono` (4003 sin motivo propio). */
    revisar: () => void;
    /**
     * `false`: solo marca (la app de campo, `dispositivo: app`; contratos/voz.md §3.12): sin REGISTER
     * (el puente lo rechaza) ni entrantes; está listo al conectar el WebSocket. De fábrica, `true`.
     */
    registrar?: boolean;
    ahora?: () => number;
    /** Para las pruebas: el WebSocket y la parte azarosa del Contact. */
    crearWebSocket?: (url: string, protocolos: string[]) => WebSocket;
    origen?: string;
    azar?: () => string;
};

// Clasificación de errores ------------------------------------------------------------------------

export type ResultadoAbrir = { tipo: "no_disponible"; motivo: MotivoTelefono } | { tipo: "error"; error: ErrorTelefono } | { tipo: "reintentar" };

const MOTIVOS: readonly MotivoTelefono[] = ["sin_centralita", "sin_extension", "extension_escritorio", "sin_credencial", "registro_sin_probar", "telefono_apagado", "turn_no_configurado"];

/** Qué hacer con un error de `abrir`. */
export function clasificarErrorAbrir(e: unknown, ahora: number): ResultadoAbrir {
    if (esErrorDeApi(e)) {
        if (e.codigo === "registro_sin_probar") return { tipo: "error", error: { tipo: "clave" } };
        const m = MOTIVOS.find((x) => x === e.codigo);
        if (m) return { tipo: "no_disponible", motivo: m };
        if (e.estado === 429) {
            const hasta = ahora + (e.reintentarEn ?? 60) * 1000;
            return { tipo: "error", error: e.codigo === "demasiados_rechazos" ? { tipo: "espera", hasta, rechazos: true } : { tipo: "espera", hasta } };
        }
        if (e.estado === 401 || e.estado === 403 || e.estado === 404) return { tipo: "no_disponible", motivo: "sin_extension" };
    }
    return { tipo: "reintentar" };
}

/** Qué hacer con un cierre del WebSocket. */
export function clasificarCierre(c: CierreSocket, ahora: number): { detener: boolean; error: ErrorTelefono | null; revisar: boolean } {
    if (c.codigo === CIERRE_ABRIR) {
        const r = clasificarErrorAbrir(c.error, ahora);
        if (r.tipo === "error") return { detener: true, error: r.error, revisar: false };
        if (r.tipo === "no_disponible") return { detener: true, error: null, revisar: true };
        return { detener: false, error: { tipo: "sin_conexion", desde: ahora }, revisar: false };
    }
    if (c.codigo === 4001) return { detener: true, error: { tipo: "pestana" }, revisar: false };
    if (c.codigo === 4003) {
        if (c.motivo === "credenciales") return { detener: true, error: { tipo: "clave" }, revisar: false };
        return { detener: true, error: null, revisar: true };
    }
    return { detener: false, error: { tipo: "sin_conexion", desde: ahora }, revisar: false };
}

// El motor ---------------------------------------------------------------------------------------

type Interna = {
    datos: LlamadaTelefono;
    sesion: SesionSip;
    parar: (() => void) | null;
    microfono: MediaStream | null;
};

/** Cuánto antes del vencimiento del TURN se piden credenciales nuevas. */
const MARGEN_ICE_MS = 90_000;
/** Cuánto se espera más candidatos relay después del primero (no hace falta esperar a todos). */
const ESPERA_ICE_MS = 400;

const azarContacto = () => Array.from(crypto.getRandomValues(new Uint8Array(6)), (b) => b.toString(36).padStart(2, "0")).join("").slice(0, 10);

export class MotorTelefono {
    private estado: EstadoTelefono = { conexion: { fase: "apagado" }, llamadas: [], extension: null };
    private oyentes = new Set<() => void>();
    private ua: UaSip | null = null;
    private sesionTel: SesionTelefono | null = null;
    private llamadas = new Map<string, Interna>();
    private timbre: { detener: () => void } | null = null;
    private registrado = false;
    private generacion = 0;
    private siguienteId = 1;
    private readonly d: DependenciasMotor;
    private readonly ahora: () => number;

    constructor(d: DependenciasMotor) {
        this.d = d;
        this.ahora = d.ahora ?? Date.now;
    }

    // Estado para React (useSyncExternalStore).
    suscribir = (f: () => void): (() => void) => {
        this.oyentes.add(f);
        return () => void this.oyentes.delete(f);
    };
    leer = (): EstadoTelefono => this.estado;

    private publicar(cambio: Partial<EstadoTelefono>) {
        this.estado = { ...this.estado, ...cambio };
        for (const f of this.oyentes) f();
    }

    private conexion(c: ConexionTelefono) {
        this.publicar({ conexion: c });
    }

    private publicarLlamadas() {
        const lista = [...this.llamadas.values()].map((l) => l.datos);
        this.publicar({ llamadas: lista });
        const suena = lista.some((l) => l.fase === "sonando");
        if (suena && !this.timbre) this.timbre = this.d.timbre();
        if (!suena && this.timbre) {
            this.timbre.detener();
            this.timbre = null;
        }
    }

    /** El teléfono no está disponible para esta persona (lo dijo `GET /telefono`). */
    noDisponible(motivo: MotivoTelefono | null) {
        this.detenerUa();
        this.conexion({ fase: "no_disponible", motivo });
    }

    /** Disponible pero apagado en este navegador. */
    apagado(extension: string | null) {
        this.detenerUa();
        this.publicar({ conexion: { fase: "apagado" }, extension });
    }

    /** La Issabel rechazó la contraseña de la extensión (`registro_sin_probar` o 4003 `credenciales`). */
    credencialRechazada(extension: string | null) {
        this.detenerUa();
        this.publicar({ conexion: { fase: "error", error: { tipo: "clave" } }, extension });
    }

    /** Otra pestaña lo tiene. */
    enOtraPestana() {
        this.detenerUa();
        this.conexion({ fase: "error", error: { tipo: "pestana" } });
    }

    microfonoBloqueado(estado: "denegado" | "sin_microfono" | "no_disponible") {
        this.conexion({ fase: "error", error: { tipo: "microfono", estado } });
    }

    get encendido(): boolean {
        return this.ua !== null;
    }

    /** Pide un ticket, abre el WebSocket y se registra. */
    async encender(extension: string | null): Promise<void> {
        this.detenerUa();
        const gen = ++this.generacion;
        this.publicar({ conexion: { fase: "conectando" }, extension });
        let sesion: SesionTelefono;
        try {
            sesion = await this.d.abrir();
        } catch (e) {
            if (gen !== this.generacion) return;
            const r = clasificarErrorAbrir(e, this.ahora());
            if (r.tipo === "no_disponible") this.conexion({ fase: "no_disponible", motivo: r.motivo });
            else if (r.tipo === "error") this.conexion({ fase: "error", error: r.error });
            else this.conexion({ fase: "error", error: { tipo: "sin_conexion", desde: this.ahora() } });
            return;
        }
        if (gen !== this.generacion) return;
        this.sesionTel = sesion;
        const socket = new SocketIris({
            primera: sesion,
            abrir: () => this.d.abrir(),
            alSesion: (s) => {
                this.sesionTel = s;
            },
            alCerrar: (c) => this.alCerrar(c, gen),
            crear: this.d.crearWebSocket,
            origen: this.d.origen,
        });
        const contacto = `sip:${sesion.sip.usuario}@${(this.d.azar ?? azarContacto)()}.invalid;transport=ws`;
        const registrar = this.d.registrar ?? true;
        const ua = this.d.crearUa({ socket, uri: sesion.sip.uri, contacto, nombre: sesion.sip.nombre, registrar });
        this.ua = ua;
        this.registrado = false;
        if (!registrar) {
            // Solo marca: listo con el WebSocket abierto (no hay registro que esperar).
            ua.on("connected", () => {
                if (gen !== this.generacion) return;
                this.registrado = true;
                if (this.estado.conexion.fase !== "error" || this.estado.conexion.error.tipo === "sin_conexion") this.conexion({ fase: "listo" });
            });
            ua.on("disconnected", () => {
                if (gen === this.generacion) this.registrado = false;
            });
        }
        ua.on("registered", () => {
            if (gen !== this.generacion) return;
            this.registrado = true;
            if (this.estado.conexion.fase !== "error" || this.estado.conexion.error.tipo === "sin_conexion") this.conexion({ fase: "listo" });
        });
        ua.on("unregistered", () => {
            if (gen === this.generacion) this.registrado = false;
        });
        ua.on("registrationFailed", (e) => {
            if (gen !== this.generacion) return;
            this.registrado = false;
            // Un 403 del puente: la Issabel rechazó la credencial. Igual que 4003 `credenciales`: se
            // detiene (si no, JsSIP volvería a registrarse con la misma contraseña y fail2ban bloquearía la
            // IP de Iris) y se relee `GET /telefono`.
            if (e.response?.status_code === 403) {
                this.credencialRechazada(this.estado.extension);
                this.d.revisar();
            }
        });
        ua.on("newRTCSession", (e) => {
            if (gen !== this.generacion || !e.session) return;
            if (e.originator === "remote") this.entrante(e.session, e.request?.getHeader("X-Iris-Llamada") ?? null);
        });
        ua.start();
    }

    private alCerrar(c: CierreSocket, gen: number) {
        if (gen !== this.generacion) return;
        this.registrado = false;
        const r = clasificarCierre(c, this.ahora());
        if (r.detener) this.detenerUa();
        if (r.error) {
            // Si ya había un error de conexión, se queda su hora.
            const antes = this.estado.conexion;
            const mismo = antes.fase === "error" && antes.error.tipo === "sin_conexion" && r.error.tipo === "sin_conexion";
            if (!mismo) this.conexion({ fase: "error", error: r.error });
        } else if (r.revisar) {
            // Si `GET /telefono` dice que ya no está disponible, el proveedor lo cambia a «no disponible»;
            // si sigue disponible, queda este aviso con «Reintentar» (nunca «Conectando…» para siempre).
            this.conexion({ fase: "error", error: { tipo: "cortado" } });
        }
        // También con la contraseña rechazada: el servidor ya la marcó «sin probar» y lo dice `GET /telefono`.
        if (r.revisar || r.error?.tipo === "clave") this.d.revisar();
    }

    /** Suelta el teléfono (al apagar, al perder la pestaña o al salir). */
    detenerUa() {
        this.generacion++;
        for (const l of this.llamadas.values()) {
            try {
                if (!l.sesion.isEnded()) l.sesion.terminate();
            } catch {
                // Ya terminada, o JsSIP no pudo (el WebSocket ya se cayó).
            }
            // Aunque JsSIP no haya podido terminarla: la conexión RTC se cierra igual (sin audio colgado).
            try {
                l.sesion.connection?.close();
            } catch {
                // Ya cerrada.
            }
            this.limpiar(l);
        }
        this.llamadas.clear();
        this.publicarLlamadas();
        const ua = this.ua;
        this.ua = null;
        this.registrado = false;
        if (ua) {
            try {
                ua.stop();
            } catch {
                // Nada que soltar.
            }
        }
    }

    /** Vuelve a intentar: el micrófono si ese era el problema; si no, otra conexión. */
    async reintentar(): Promise<void> {
        const c = this.estado.conexion;
        if (c.fase === "error" && c.error.tipo === "microfono" && this.ua) {
            try {
                const m = await this.d.pedirMicrofono();
                for (const t of m.getTracks()) t.stop();
                this.conexion(this.registrado ? { fase: "listo" } : { fase: "conectando" });
            } catch (e) {
                if (esErrorMicrofono(e)) this.microfonoBloqueado(e.estado);
            }
            return;
        }
        await this.encender(this.estado.extension);
    }

    // Llamadas ---------------------------------------------------------------------------------

    private async pcConfig(): Promise<RTCConfiguration> {
        let s = this.sesionTel;
        if (s && Date.parse(s.iceVence) - this.ahora() < MARGEN_ICE_MS) {
            // Solo para el TURN (decisión de la revisión de #252, punto 4): no hay un endpoint aparte
            // porque /abrir ya da el TURN con su tope y su auditoría. El ticket nuevo no se usa: vale 30 s
            // y un solo uso, y vence solo. Pasa solo al contestar o llamar con el TURN a menos de 90 s de
            // vencer (dura de 10 a 20 min), así que a lo más una vez cada ~10 min: 1 de las 6 por minuto
            // de /abrir (contratos/voz.md §3.12, `POST /telefono/abrir`).
            try {
                s = await this.d.abrir();
                this.sesionTel = s;
            } catch {
                // Se intenta con las que hay.
            }
        }
        const servidores = (s?.ice ?? []).map((i) => ({ urls: i.urls ?? [], username: i.username, credential: i.credential }));
        return { iceServers: servidores, iceTransportPolicy: "relay", bundlePolicy: "max-bundle", rtcpMuxPolicy: "require" };
    }

    private async microfono(): Promise<MediaStream | null> {
        try {
            return await this.d.pedirMicrofono();
        } catch (e) {
            this.microfonoBloqueado(esErrorMicrofono(e) ? e.estado : "no_disponible");
            return null;
        }
    }

    private nueva(sesion: SesionSip, datos: Omit<LlamadaTelefono, "id" | "desde" | "silenciada" | "enEspera" | "motivoFin" | "causa">): Interna {
        const id = `t${this.siguienteId++}`;
        const l: Interna = { datos: { ...datos, id, desde: null, silenciada: false, enEspera: false, motivoFin: null, causa: null }, sesion, parar: null, microfono: null };
        this.llamadas.set(id, l);
        this.vigilar(l);
        this.publicarLlamadas();
        return l;
    }

    private cambiar(l: Interna, cambio: Partial<LlamadaTelefono>) {
        l.datos = { ...l.datos, ...cambio };
        this.publicarLlamadas();
    }

    private vigilar(l: Interna) {
        const s = l.sesion;
        let listoIce: ReturnType<typeof setTimeout> | null = null;
        s.on("peerconnection", (e) => {
            e.peerconnection?.addEventListener("track", (t: RTCTrackEvent) => {
                const stream = t.streams[0] ?? new MediaStream([t.track]);
                l.parar?.();
                l.parar = this.d.reproducir(stream);
            });
        });
        s.on("icecandidate", (e) => {
            // Con el primer candidato relay basta (el puente solo deja pasar relay): no se espera a todos.
            if (e.candidate?.type === "relay" && e.ready && listoIce === null) {
                const listo = e.ready;
                listoIce = globalThis.setTimeout(listo, ESPERA_ICE_MS);
            }
        });
        s.on("accepted", () => {
            if (l.datos.fase !== "en_curso") this.cambiar(l, { fase: "en_curso", desde: this.ahora() });
        });
        s.on("confirmed", () => {
            if (l.datos.fase !== "en_curso") this.cambiar(l, { fase: "en_curso", desde: this.ahora() });
        });
        s.on("hold", (e) => {
            if (e.originator === "local") this.cambiar(l, { enEspera: true });
        });
        s.on("unhold", (e) => {
            if (e.originator === "local") this.cambiar(l, { enEspera: false });
        });
        s.on("muted", () => this.cambiar(l, { silenciada: true }));
        s.on("unmuted", () => this.cambiar(l, { silenciada: false }));
        const fin = (e: EventoSip) => {
            if (listoIce !== null) globalThis.clearTimeout(listoIce);
            this.cambiar(l, { fase: "terminada", motivoFin: motivoFin(e.cause, l.datos.direccion), causa: e.cause ?? null });
            this.limpiar(l);
            this.llamadas.delete(l.datos.id);
            this.publicarLlamadas();
        };
        s.on("ended", fin);
        s.on("failed", fin);
    }

    private limpiar(l: Interna) {
        l.parar?.();
        l.parar = null;
        for (const t of l.microfono?.getTracks() ?? []) t.stop();
        l.microfono = null;
    }

    private entrante(sesion: SesionSip, llamadaId: string | null) {
        const numero = sesion.remote_identity.uri.user;
        const nombre = sesion.remote_identity.display_name?.trim() || numero;
        this.nueva(sesion, { direccion: "entrante", numero, llamadaId: llamadaId && /^[0-9a-f-]{36}$/i.test(llamadaId) ? llamadaId : null, nombre, fase: "sonando", interna: false });
    }

    /** Contesta (las demás en curso quedan en espera). `false` si no se pudo (micrófono). */
    async contestar(id: string): Promise<boolean> {
        const l = this.llamadas.get(id);
        if (!l || l.datos.fase !== "sonando") return false;
        const mic = await this.microfono();
        if (!mic) return false;
        if (!this.llamadas.has(id) || l.sesion.isEnded()) {
            for (const t of mic.getTracks()) t.stop();
            return false;
        }
        for (const otra of this.llamadas.values()) {
            if (otra !== l && otra.datos.fase === "en_curso" && !otra.datos.enEspera) otra.sesion.hold();
        }
        l.microfono = mic;
        l.sesion.answer({ mediaStream: mic, pcConfig: await this.pcConfig() });
        return true;
    }

    rechazar(id: string) {
        this.llamadas.get(id)?.sesion.terminate({ status_code: 486, reason_phrase: "Busy Here" });
    }

    colgar(id: string) {
        const l = this.llamadas.get(id);
        if (!l) return;
        try {
            l.sesion.terminate();
        } catch {
            // Ya terminada: JsSIP avisa con «ended».
        }
    }

    /** Marca el destino que dio `POST /telefono/llamar`. */
    async llamar(destino: string, datos: { numero: string; nombre: string; llamadaId: string | null; interna: boolean }): Promise<string | null> {
        if (!this.ua || !this.registrado) return null;
        const mic = await this.microfono();
        if (!mic || !this.ua) {
            for (const t of mic?.getTracks() ?? []) t.stop();
            return null;
        }
        for (const otra of this.llamadas.values()) {
            if (otra.datos.fase === "en_curso" && !otra.datos.enEspera) otra.sesion.hold();
        }
        let sesion: SesionSip;
        try {
            sesion = this.ua.call(destino, { mediaStream: mic, pcConfig: await this.pcConfig(), mediaConstraints: { audio: true, video: false } });
        } catch {
            // JsSIP no pudo marcar (el WebSocket se cerró justo ahora): sin llamada y sin micrófono abierto.
            for (const t of mic.getTracks()) t.stop();
            return null;
        }
        const l = this.nueva(sesion, { direccion: "saliente", fase: "llamando", ...datos });
        l.microfono = mic;
        return l.datos.id;
    }

    silenciar(id: string, si: boolean) {
        const l = this.llamadas.get(id);
        if (!l) return;
        if (si) l.sesion.mute({ audio: true });
        else l.sesion.unmute({ audio: true });
    }

    espera(id: string, si: boolean) {
        const l = this.llamadas.get(id);
        if (!l) return;
        if (si) l.sesion.hold();
        else {
            for (const otra of this.llamadas.values()) {
                if (otra !== l && otra.datos.fase === "en_curso" && !otra.datos.enEspera) otra.sesion.hold();
            }
            l.sesion.unhold();
        }
    }

    /** Un dígito por INFO (el puente no deja pasar «##» ni «*2» seguidos). */
    tono(id: string, digito: string) {
        if (!/^[0-9*#]$/.test(digito)) return;
        this.llamadas.get(id)?.sesion.sendDTMF(digito, { transportType: "INFO", duration: 160 });
    }

    /** Transferencia ciega a una extensión o grupo de la lista (REFER). Se resuelve con el 202. */
    transferir(id: string, numero: string): Promise<void> {
        const l = this.llamadas.get(id);
        const dominio = this.sesionTel?.sip.dominio;
        if (!l || !dominio || !/^[0-9]{2,6}$/.test(numero)) return Promise.reject(new Error("transferir"));
        return new Promise((resolve, reject) => {
            l.sesion.refer(`sip:${numero}@${dominio}`, {
                eventHandlers: {
                    requestSucceeded: () => {
                        resolve();
                        // La Issabel ya tiene la llamada: esta pierna se cuelga si ella no lo hace.
                        globalThis.setTimeout(() => this.colgar(id), 4000);
                    },
                    requestFailed: () => reject(new Error("transferir")),
                },
            });
        });
    }
}
