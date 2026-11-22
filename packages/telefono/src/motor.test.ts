import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { clasificarCierre, clasificarErrorAbrir, MotorTelefono, type ConfigUa, type EventoSip, type SesionSip, type UaSip } from "./motor";
import { CIERRE_ABRIR, urlWebSocket } from "./socket";
import type { SesionTelefono } from "./tipos";

/** Un error de la API con la forma del `ErrorApi` del panel (el motor lo distingue por su forma). */
class ErrorApiPrueba extends Error {
    readonly estado: number;
    readonly codigo: string;
    readonly titulo: string;
    readonly detalle: string | undefined;
    readonly reintentarEn: number | undefined;
    constructor(d: { estado: number; codigo: string; titulo: string; detalle?: string; reintentarEn?: number | undefined }) {
        super(d.titulo);
        this.estado = d.estado;
        this.codigo = d.codigo;
        this.titulo = d.titulo;
        this.detalle = d.detalle;
        this.reintentarEn = d.reintentarEn;
    }
}

/** Como el `ErrorMicrofono` del panel. */
class ErrorMicrofono extends Error {
    constructor(readonly estado: "denegado" | "sin_microfono" | "no_disponible") {
        super(estado);
    }
}

/*
 * El motor del teléfono con un agente SIP y un WebSocket falsos: ticket por conexión, contacto con
 * el usuario SIP y `.invalid`, cierres del servidor (4001, 4003, 4502), entrante y saliente con
 * TURN relay, espera, silencio, DTMF de a un dígito y transferencia.
 */

const sesionTel = (n = 1, iceVence = "2099-01-01T00:00:00Z"): SesionTelefono => ({
    ws: "/api/v1/mensajeria/telefono/sip",
    ticket: `t${n}`,
    protocolos: ["sip", `iris-ticket.t${n}`],
    ticketVence: "2099-01-01T00:00:00Z",
    sip: { uri: "sip:204@pbx.oficina", usuario: "204", dominio: "pbx.oficina", nombre: "Ana López" },
    ice: [{ urls: ["turn:iris.prueba:3478?transport=udp"], username: `u${n}`, credential: "c" }],
    politicaIce: "relay",
    iceVence,
});

class WsFalso {
    onopen: (() => void) | null = null;
    onclose: ((e: { code: number; reason: string }) => void) | null = null;
    onmessage: ((e: { data: unknown }) => void) | null = null;
    onerror: (() => void) | null = null;
    binaryType = "";
    enviados: string[] = [];
    constructor(
        readonly url: string,
        readonly protocolos: string[],
    ) {}
    send(m: string) {
        this.enviados.push(m);
    }
    close() {}
    cerrar(code: number, reason = "") {
        this.onclose?.({ code, reason });
    }
}

class Emisor {
    private oyentes = new Map<string, ((e: EventoSip) => void)[]>();
    on(ev: string, f: (e: EventoSip) => void) {
        this.oyentes.set(ev, [...(this.oyentes.get(ev) ?? []), f]);
    }
    emitir(ev: string, e: EventoSip = {}) {
        for (const f of this.oyentes.get(ev) ?? []) f(e);
    }
}

class SesionFalsa extends Emisor implements SesionSip {
    terminada = false;
    respuestas: Parameters<SesionSip["answer"]>[0][] = [];
    tonos: string[] = [];
    refers: string[] = [];
    terminos: (Parameters<SesionSip["terminate"]>[0] | undefined)[] = [];
    espera = false;
    connection = null;
    constructor(
        readonly direction: "incoming" | "outgoing",
        readonly remote_identity: { uri: { user: string }; display_name?: string },
    ) {
        super();
    }
    answer(o: Parameters<SesionSip["answer"]>[0]) {
        this.respuestas.push(o);
    }
    terminate(o?: Parameters<SesionSip["terminate"]>[0]) {
        this.terminos.push(o);
        this.terminada = true;
        this.emitir("ended", { cause: o?.status_code === 486 ? "Rejected" : "Terminated" });
    }
    mute() {
        this.emitir("muted");
    }
    unmute() {
        this.emitir("unmuted");
    }
    hold() {
        this.espera = true;
        this.emitir("hold", { originator: "local" });
        return true;
    }
    unhold() {
        this.espera = false;
        this.emitir("unhold", { originator: "local" });
        return true;
    }
    sendDTMF(t: string) {
        this.tonos.push(t);
    }
    refer(destino: string, o?: { eventHandlers?: Record<string, (e: EventoSip) => void> }) {
        this.refers.push(destino);
        o?.eventHandlers?.requestSucceeded?.({});
    }
    isEnded() {
        return this.terminada;
    }
}

class UaFalso extends Emisor implements UaSip {
    detenido = false;
    llamadas: { destino: string; o: Parameters<UaSip["call"]>[1]; sesion: SesionFalsa }[] = [];
    constructor(readonly config: ConfigUa) {
        super();
        // Como JsSIP: «connected» al abrir el WebSocket y, si registra, «registered».
        config.socket.onconnect = () => {
            this.emitir("connected");
            if (config.registrar) this.emitir("registered");
        };
        config.socket.ondisconnect = () => undefined;
    }
    start() {
        this.config.socket.connect();
    }
    stop() {
        this.detenido = true;
        this.config.socket.disconnect();
    }
    call(destino: string, o: Parameters<UaSip["call"]>[1]) {
        const sesion = new SesionFalsa("outgoing", { uri: { user: destino } });
        this.llamadas.push({ destino, o, sesion });
        return sesion;
    }
    sonar(numero: string, llamada?: string) {
        const s = new SesionFalsa("incoming", { uri: { user: numero } });
        this.emitir("newRTCSession", { originator: "remote", session: s, request: { getHeader: (n) => (n === "X-Iris-Llamada" ? llamada : undefined) } });
        return s;
    }
}

const micro = () => ({ getTracks: () => [{ stop: vi.fn() }] }) as unknown as MediaStream;

function armar(o: { abrir?: () => Promise<SesionTelefono>; microfono?: () => Promise<MediaStream>; registrar?: boolean } = {}) {
    const sockets: WsFalso[] = [];
    const uas: UaFalso[] = [];
    let n = 0;
    const abrir = vi.fn(o.abrir ?? (() => Promise.resolve(sesionTel(++n))));
    const timbres = { sonando: 0 };
    const revisar = vi.fn();
    const motor = new MotorTelefono({
        abrir,
        crearUa: (c) => {
            const ua = new UaFalso(c);
            uas.push(ua);
            return ua;
        },
        pedirMicrofono: o.microfono ?? (() => Promise.resolve(micro())),
        reproducir: () => () => undefined,
        timbre: () => {
            timbres.sonando++;
            return { detener: () => void timbres.sonando-- };
        },
        revisar,
        crearWebSocket: (url, protocolos) => {
            const ws = new WsFalso(url, protocolos);
            sockets.push(ws);
            return ws as unknown as WebSocket;
        },
        origen: "https://iris.prueba",
        ...(o.registrar === undefined ? {} : { registrar: o.registrar }),
        azar: () => "k3j9x",
        ahora: () => Date.parse("2026-11-21T16:00:00Z"),
    });
    return { motor, sockets, uas, abrir, timbres, revisar };
}

const esperar = () => new Promise((r) => setTimeout(r, 0));

async function listo() {
    const a = armar();
    await a.motor.encender("204");
    await esperar();
    a.sockets[0]?.onopen?.();
    return a;
}

beforeEach(() => {
    vi.stubGlobal("MediaStream", function MediaStreamFalso() {});
});
afterEach(() => vi.unstubAllGlobals());

describe("conectar", () => {
    it("ticket como subprotocolo, contacto con el usuario SIP y .invalid, y listo al registrarse", async () => {
        const { motor, sockets, uas } = await listo();
        expect(sockets[0]?.url).toBe("wss://iris.prueba/api/v1/mensajeria/telefono/sip");
        expect(sockets[0]?.protocolos).toEqual(["sip", "iris-ticket.t1"]);
        expect(uas[0]?.config.contacto).toBe("sip:204@k3j9x.invalid;transport=ws");
        expect(uas[0]?.config.uri).toBe("sip:204@pbx.oficina");
        expect(motor.leer().conexion).toEqual({ fase: "listo" });
    });

    it("cada reconexión pide un ticket nuevo", async () => {
        const { motor, sockets, uas, abrir } = await listo();
        sockets[0]?.cerrar(4502, "centralita_no_responde");
        expect(motor.leer().conexion).toMatchObject({ fase: "error", error: { tipo: "sin_conexion" } });
        uas[0]?.config.socket.connect();
        await esperar();
        expect(abrir).toHaveBeenCalledTimes(2);
        expect(sockets[1]?.protocolos).toEqual(["sip", "iris-ticket.t2"]);
        sockets[1]?.onopen?.();
        expect(motor.leer().conexion).toEqual({ fase: "listo" });
    });

    it("4001: otra pestaña o dispositivo; 4003 credenciales: la contraseña. Los dos detienen el agente", async () => {
        const a = await listo();
        a.sockets[0]?.cerrar(4001, "reemplazada");
        expect(a.motor.leer().conexion).toEqual({ fase: "error", error: { tipo: "pestana" } });
        expect(a.uas[0]?.detenido).toBe(true);
        const b = await listo();
        b.sockets[0]?.cerrar(4003, "credenciales");
        expect(b.motor.leer().conexion).toEqual({ fase: "error", error: { tipo: "clave" } });
        expect(b.uas[0]?.detenido).toBe(true);
        // La contraseña rechazada: se vuelve a leer `GET /telefono` (el servidor ya la marcó sin probar).
        expect(b.revisar).toHaveBeenCalledOnce();
        const c = await listo();
        c.sockets[0]?.cerrar(4003, "sin_acceso");
        expect(c.uas[0]?.detenido).toBe(true);
        expect(c.revisar).toHaveBeenCalledOnce();
        // Revisión de #252, punto 3: nunca «Conectando…» para siempre; si `GET /telefono` lo sigue dando
        // por disponible, queda el aviso con «Reintentar».
        expect(c.motor.leer().conexion).toEqual({ fase: "error", error: { tipo: "cortado" } });
    });

    it("registrationFailed 403: igual que 4003 credenciales (detiene el agente y relee GET /telefono)", async () => {
        // Revisión de #252, punto 2: sin detener, JsSIP volvería a registrarse con la misma contraseña.
        const a = await listo();
        a.uas[0]?.emitir("registrationFailed", { response: { status_code: 403 } });
        expect(a.motor.leer().conexion).toEqual({ fase: "error", error: { tipo: "clave" } });
        expect(a.uas[0]?.detenido).toBe(true);
        expect(a.revisar).toHaveBeenCalledOnce();
        // El cierre 4003 que llega después no hace nada más.
        a.sockets[0]?.cerrar(4003, "credenciales");
        expect(a.revisar).toHaveBeenCalledOnce();
        // Otro rechazo (408, 503) no detiene: JsSIP reintenta solo.
        const b = await listo();
        b.uas[0]?.emitir("registrationFailed", { response: { status_code: 503 } });
        expect(b.uas[0]?.detenido).toBe(false);
        expect(b.revisar).not.toHaveBeenCalled();
    });

    it("abrir: motivos del servidor, 429 y la red", () => {
        const ahora = 1000;
        const e = (estado: number, codigo: string, reintentarEn?: number) => new ErrorApiPrueba({ estado, codigo, titulo: "x", reintentarEn });
        expect(clasificarErrorAbrir(e(422, "telefono_apagado"), ahora)).toEqual({ tipo: "no_disponible", motivo: "telefono_apagado" });
        expect(clasificarErrorAbrir(e(422, "registro_sin_probar"), ahora)).toEqual({ tipo: "error", error: { tipo: "clave" } });
        expect(clasificarErrorAbrir(e(429, "demasiados_intentos", 60), ahora)).toEqual({ tipo: "error", error: { tipo: "espera", hasta: 61_000 } });
        // Re-verificación de #252, M1: con varias contraseñas rechazadas espera lo que diga el servidor.
        expect(clasificarErrorAbrir(e(429, "demasiados_rechazos", 540), ahora)).toEqual({ tipo: "error", error: { tipo: "espera", hasta: 541_000, rechazos: true } });
        expect(clasificarErrorAbrir(new Error("red"), ahora)).toEqual({ tipo: "reintentar" });
        expect(clasificarCierre({ codigo: CIERRE_ABRIR, motivo: "abrir", error: e(429, "demasiados_intentos", 60) }, ahora).detener).toBe(true);
        expect(clasificarCierre({ codigo: CIERRE_ABRIR, motivo: "abrir", error: new Error("red") }, ahora)).toMatchObject({ detener: false, error: { tipo: "sin_conexion" } });
        expect(clasificarCierre({ codigo: 1006, motivo: "", error: null }, ahora)).toMatchObject({ detener: false });
        expect(urlWebSocket("/api/v1/mensajeria/telefono/sip", "http://127.0.0.1:3503")).toBe("ws://127.0.0.1:3503/api/v1/mensajeria/telefono/sip");
    });
});

describe("llamadas", () => {
    it("entrante: suena con su llamada de Iris, se contesta con el micrófono y el TURN relay", async () => {
        const { motor, uas, timbres } = await listo();
        const s = uas[0]?.sonar("+50255123480", "0199aaaa-0000-7000-8000-000000000001");
        expect(motor.leer().llamadas).toMatchObject([{ direccion: "entrante", fase: "sonando", numero: "+50255123480", llamadaId: "0199aaaa-0000-7000-8000-000000000001" }]);
        expect(timbres.sonando).toBe(1);
        const id = motor.leer().llamadas[0]?.id ?? "";
        expect(await motor.contestar(id)).toBe(true);
        expect(s?.respuestas[0]?.pcConfig).toMatchObject({ iceTransportPolicy: "relay", iceServers: [{ urls: ["turn:iris.prueba:3478?transport=udp"], username: "u1" }] });
        s?.emitir("accepted");
        expect(motor.leer().llamadas[0]).toMatchObject({ fase: "en_curso", desde: Date.parse("2026-11-21T16:00:00Z") });
        expect(timbres.sonando).toBe(0);
        motor.silenciar(id, true);
        motor.espera(id, true);
        expect(motor.leer().llamadas[0]).toMatchObject({ silenciada: true, enEspera: true });
        motor.tono(id, "4");
        motor.tono(id, "##");
        expect(s?.tonos).toEqual(["4"]);
        motor.colgar(id);
        expect(motor.leer().llamadas).toEqual([]);
    });

    it("una X-Iris-Llamada que no es un uuid no se usa", async () => {
        const { motor, uas } = await listo();
        uas[0]?.sonar("205", "<script>");
        expect(motor.leer().llamadas[0]?.llamadaId).toBeNull();
    });

    it("rechazar: 486 (la centralita la pasa a otra persona)", async () => {
        const { motor, uas } = await listo();
        const s = uas[0]?.sonar("205");
        motor.rechazar(motor.leer().llamadas[0]?.id ?? "");
        expect(s?.terminos[0]).toEqual({ status_code: 486, reason_phrase: "Busy Here" });
        expect(motor.leer().llamadas).toEqual([]);
    });

    it("sin micrófono no contesta y lo dice", async () => {
        const a = armar({ microfono: () => Promise.reject(new ErrorMicrofono("denegado")) });
        await a.motor.encender("204");
        await esperar();
        a.sockets[0]?.onopen?.();
        const s = a.uas[0]?.sonar("205");
        expect(await a.motor.contestar(a.motor.leer().llamadas[0]?.id ?? "")).toBe(false);
        expect(s?.respuestas).toEqual([]);
        expect(a.motor.leer().conexion).toEqual({ fase: "error", error: { tipo: "microfono", estado: "denegado" } });
    });

    it("contestar una segunda deja la primera en espera", async () => {
        const { motor, uas } = await listo();
        const s1 = uas[0]?.sonar("205");
        await motor.contestar(motor.leer().llamadas[0]?.id ?? "");
        s1?.emitir("accepted");
        uas[0]?.sonar("206");
        await motor.contestar(motor.leer().llamadas[1]?.id ?? "");
        expect(s1?.espera).toBe(true);
    });

    it("detenerUa cuelga, cierra la conexión RTC y suelta el micrófono aunque el WebSocket ya esté caído", async () => {
        // Revisión de #258, M1: al cerrar sesión en la app de campo no queda nada abierto.
        const pista = { stop: vi.fn() };
        const mic = { getTracks: () => [pista] } as unknown as MediaStream;
        const a = armar({ microfono: () => Promise.resolve(mic) });
        await a.motor.encender("204");
        await esperar();
        a.sockets[0]?.onopen?.();
        const id = await a.motor.llamar("sip:iris-0199aaaa-0000-7000-8000-000000000009@pbx.oficina", { numero: "+50255123480", nombre: "María", llamadaId: null, interna: false });
        expect(id).not.toBeNull();
        const sesion = a.uas[0]?.llamadas[0]?.sesion;
        if (!sesion) throw new Error("sin sesión");
        sesion.emitir("accepted");
        // El WebSocket se cae y JsSIP ya no puede mandar el BYE.
        a.sockets[0]?.cerrar(1006);
        sesion.terminate = () => {
            throw new Error("Invalid status");
        };
        const cerrar = vi.fn();
        Object.assign(sesion, { connection: { close: cerrar } });
        a.motor.detenerUa();
        expect(cerrar).toHaveBeenCalledOnce();
        expect(pista.stop).toHaveBeenCalled();
        expect(a.motor.leer().llamadas).toEqual([]);
    });

    it("solo marcar (app de campo): sin REGISTER, listo al conectar y llama igual", async () => {
        const a = armar({ registrar: false });
        await a.motor.encender("311");
        await esperar();
        expect(a.uas[0]?.config.registrar).toBe(false);
        expect(a.motor.leer().conexion).toEqual({ fase: "conectando" });
        a.sockets[0]?.onopen?.();
        expect(a.motor.leer().conexion).toEqual({ fase: "listo" });
        const id = await a.motor.llamar("sip:iris-0199aaaa-0000-7000-8000-000000000009@pbx.oficina", { numero: "+50255123480", nombre: "María", llamadaId: null, interna: false });
        expect(id).not.toBeNull();
        expect(a.uas[0]?.llamadas).toHaveLength(1);
        // El panel sigue registrando (de fábrica).
        const b = await listo();
        expect(b.uas[0]?.config.registrar).toBe(true);
    });

    it("saliente: marca el destino del servidor; credenciales TURN nuevas si vencen pronto", async () => {
        let n = 0;
        const a = armar({ abrir: () => Promise.resolve(sesionTel(++n, n === 1 ? "2026-11-21T16:00:30Z" : "2099-01-01T00:00:00Z")) });
        await a.motor.encender("204");
        await esperar();
        a.sockets[0]?.onopen?.();
        const id = await a.motor.llamar("sip:iris-0199aaaa-0000-7000-8000-000000000009@pbx.oficina", { numero: "+50255123480", nombre: "María", llamadaId: "0199aaaa-0000-7000-8000-000000000009", interna: false });
        const l = a.uas[0]?.llamadas[0];
        expect(l?.destino).toBe("sip:iris-0199aaaa-0000-7000-8000-000000000009@pbx.oficina");
        expect(l?.o.pcConfig.iceServers?.[0]).toMatchObject({ username: "u2" });
        expect(a.motor.leer().llamadas[0]).toMatchObject({ id, fase: "llamando", direccion: "saliente", nombre: "María" });
        l?.sesion.emitir("failed", { cause: "Busy" });
        expect(a.motor.leer().llamadas).toEqual([]);
    });

    it("transferir: solo extensiones o grupos (REFER a sip:<numero>@<dominio>)", async () => {
        const { motor, uas } = await listo();
        const s = uas[0]?.sonar("+50255123480");
        const id = motor.leer().llamadas[0]?.id ?? "";
        await motor.contestar(id);
        s?.emitir("accepted");
        await expect(motor.transferir(id, "0019005551234")).rejects.toThrow();
        await motor.transferir(id, "205");
        expect(s?.refers).toEqual(["sip:205@pbx.oficina"]);
    });

    it("apagar cuelga lo que haya", async () => {
        const { motor, uas } = await listo();
        const s = uas[0]?.sonar("205");
        motor.apagado("204");
        expect(s?.terminada).toBe(true);
        expect(uas[0]?.detenido).toBe(true);
        expect(motor.leer()).toMatchObject({ conexion: { fase: "apagado" }, llamadas: [] });
    });
});
