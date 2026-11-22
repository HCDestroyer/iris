import type { Page, WebSocketRoute } from "@playwright/test";

/*
 * La Issabel detrás del puente de Iris, para las pruebas del teléfono (E4c): atiende el WebSocket
 * `/api/v1/mensajeria/telefono/sip` con `page.routeWebSocket` y habla SIP de verdad con JsSIP
 * (REGISTER, INVITE en los dos sentidos, ACK, BYE, CANCEL, INFO, REFER y re-INVITE de espera). No
 * hay audio: el navegador corre con el micrófono falso de Chromium y sin servidores TURN.
 */

export type OpcionesSip = {
    /** Qué pasa con el REGISTER: ok, la Issabel rechaza la contraseña (el puente corta con 4003) o no hay centralita (4502). */
    registro?: "ok" | "credenciales" | "sin_conexion";
};

type Mensaje = { primera: string; cabeceras: [string, string][]; cuerpo: string };

const CRLF = "\r\n";
const azar = () => Math.random().toString(36).slice(2, 12);

function leer(texto: string): Mensaje {
    const [cab = "", ...resto] = texto.split(CRLF + CRLF);
    const [primera = "", ...lineas] = cab.split(CRLF);
    const cabeceras = lineas.filter((l) => l.includes(":")).map((l) => {
        const i = l.indexOf(":");
        return [l.slice(0, i).trim(), l.slice(i + 1).trim()] as [string, string];
    });
    return { primera, cabeceras, cuerpo: resto.join(CRLF + CRLF) };
}

const LARGAS: Record<string, string> = { v: "via", f: "from", t: "to", i: "call-id", m: "contact", l: "content-length", c: "content-type" };
const nombre = (n: string) => LARGAS[n.toLowerCase()] ?? n.toLowerCase();
const una = (m: Mensaje, n: string) => m.cabeceras.find(([k]) => nombre(k) === n.toLowerCase())?.[1] ?? "";
const todas = (m: Mensaje, n: string) => m.cabeceras.filter(([k]) => nombre(k) === n.toLowerCase()).map(([, v]) => v);
const metodo = (m: Mensaje) => (m.primera.startsWith("SIP/2.0") ? null : (m.primera.split(" ")[0] ?? ""));

function armar(primera: string, cabeceras: [string, string][], cuerpo = ""): string {
    return [primera, ...cabeceras.map(([k, v]) => `${k}: ${v}`), `Content-Length: ${new TextEncoder().encode(cuerpo).length}`, "", cuerpo].join(CRLF);
}

const HUELLA = Array.from({ length: 32 }, (_, i) => ((i * 37 + 11) % 256).toString(16).padStart(2, "0").toUpperCase()).join(":");

/** Una oferta de Asterisk con `webrtc=yes` (DTLS-SRTP, ICE, rtcp-mux), como la que reenvía el puente. */
function ofertaIssabel(direccion = "sendrecv"): string {
    return [
        "v=0",
        "o=- 1000 1000 IN IP4 10.20.0.5",
        "s=Asterisk",
        "c=IN IP4 10.20.0.5",
        "t=0 0",
        "a=group:BUNDLE 0",
        "m=audio 10000 UDP/TLS/RTP/SAVPF 0 101",
        "a=mid:0",
        "a=setup:actpass",
        `a=fingerprint:sha-256 ${HUELLA}`,
        "a=ice-ufrag:abcd1234",
        "a=ice-pwd:abcdefghijklmnopqrstuvwx",
        "a=rtcp-mux",
        "a=rtpmap:0 PCMU/8000",
        "a=rtpmap:101 telephone-event/8000",
        "a=fmtp:101 0-16",
        `a=${direccion}`,
        "a=candidate:1 1 udp 2130706431 10.20.0.5 10000 typ host",
        "",
    ].join(CRLF);
}

/** La respuesta de la Issabel a una oferta del navegador (mismo `mid`, PCMU). */
function respuestaA(oferta: string): string {
    const mid = /a=mid:(\S+)/.exec(oferta)?.[1] ?? "0";
    const envia = /a=sendonly/.test(oferta) ? "recvonly" : /a=inactive/.test(oferta) ? "inactive" : "sendrecv";
    return [
        "v=0",
        "o=- 2000 2000 IN IP4 10.20.0.5",
        "s=Asterisk",
        "c=IN IP4 10.20.0.5",
        "t=0 0",
        `a=group:BUNDLE ${mid}`,
        "m=audio 10002 UDP/TLS/RTP/SAVPF 0",
        `a=mid:${mid}`,
        "a=setup:active",
        `a=fingerprint:sha-256 ${HUELLA}`,
        "a=ice-ufrag:efgh5678",
        "a=ice-pwd:zyxwvutsrqponmlkjihgfedc",
        "a=rtcp-mux",
        "a=rtpmap:0 PCMU/8000",
        `a=${envia}`,
        "a=candidate:1 1 udp 2130706431 10.20.0.5 10002 typ host",
        "",
    ].join(CRLF);
}

type Dialogo = { callId: string; from: string; to: string; contactoNavegador: string; cseq: number; via: string };

export class SipFalso {
    /** Todo lo que mandó el navegador, en texto. */
    readonly recibidos: string[] = [];
    readonly conexiones: string[] = [];
    private ws: WebSocketRoute | null = null;
    private readonly opciones: OpcionesSip;
    /** Salientes del navegador esperando que «contesten». */
    private pendientes = new Map<string, Mensaje>();
    private dialogos = new Map<string, Dialogo>();

    constructor(opciones: OpcionesSip) {
        this.opciones = opciones;
    }

    async instalar(page: Page) {
        await page.routeWebSocket(/\/api\/v1\/mensajeria\/telefono\/sip/, (ws) => {
            this.ws = ws;
            this.conexiones.push(ws.url());
            if (this.opciones.registro === "sin_conexion") {
                setTimeout(() => void ws.close({ code: 4502, reason: "centralita_no_responde" }), 50);
                return;
            }
            ws.onMessage((m) => {
                if (typeof m === "string") this.recibir(m);
            });
        });
    }

    private enviar(texto: string) {
        this.ws?.send(texto);
    }

    /** Responde a una petición del navegador copiando Via, From, To (con etiqueta), Call-ID y CSeq. */
    private responder(m: Mensaje, codigo: number, razon: string, extra: [string, string][] = [], cuerpo = "", etiqueta = "pbx" + azar()) {
        const to = una(m, "to");
        const cab: [string, string][] = [
            ...todas(m, "via").map((v) => ["Via", v] as [string, string]),
            ["From", una(m, "from")],
            ["To", /;tag=/.test(to) || codigo === 100 ? to : `${to};tag=${etiqueta}`],
            ["Call-ID", una(m, "call-id")],
            ["CSeq", una(m, "cseq")],
            ...extra,
        ];
        this.enviar(armar(`SIP/2.0 ${codigo} ${razon}`, cab, cuerpo));
    }

    private recibir(texto: string) {
        if (texto.trim() === "") return;
        this.recibidos.push(texto);
        const m = leer(texto);
        const met = metodo(m);
        if (met === null) return this.respuestaDelNavegador(m);
        switch (met) {
            case "REGISTER":
                if (this.opciones.registro === "credenciales") {
                    // El puente responde 403 al navegador y corta (contrato §3.12, revisión I3).
                    this.responder(m, 403, "Forbidden");
                    setTimeout(() => void this.ws?.close({ code: 4003, reason: "credenciales" }), 30);
                    return;
                }
                return this.responder(m, 200, "OK", [
                    ["Contact", `${una(m, "contact").replace(/;expires=\d+/, "")};expires=300`],
                    ["Expires", "300"],
                ]);
            case "INVITE": {
                const callId = una(m, "call-id");
                if (/;tag=/.test(una(m, "to"))) {
                    // re-INVITE (espera o volver): 200 con la respuesta que pide su dirección.
                    return this.responder(m, 200, "OK", [["Contact", "<sip:asterisk@10.20.0.5:8089;transport=ws>"], ["Content-Type", "application/sdp"]], respuestaA(m.cuerpo), this.etiquetaDe(callId));
                }
                this.responder(m, 100, "Trying");
                this.responder(m, 180, "Ringing", [["Contact", "<sip:asterisk@10.20.0.5:8089;transport=ws>"]], "", this.etiquetaDe(callId));
                this.pendientes.set(callId, m);
                return;
            }
            case "CANCEL": {
                this.responder(m, 200, "OK");
                const inv = this.pendientes.get(una(m, "call-id"));
                if (inv) {
                    this.pendientes.delete(una(m, "call-id"));
                    this.responder(inv, 487, "Request Terminated", [], "", this.etiquetaDe(una(m, "call-id")));
                }
                return;
            }
            case "BYE":
                this.dialogos.delete(una(m, "call-id"));
                return this.responder(m, 200, "OK");
            case "INFO":
            case "OPTIONS":
                return this.responder(m, 200, "OK");
            case "REFER":
                return this.responder(m, 202, "Accepted");
            default:
                return;
        }
    }

    private etiquetas = new Map<string, string>();
    private etiquetaDe(callId: string): string {
        let t = this.etiquetas.get(callId);
        if (!t) {
            t = "pbx" + azar();
            this.etiquetas.set(callId, t);
        }
        return t;
    }

    /** La respuesta del navegador a un INVITE de la Issabel: con 2xx, la Issabel manda el ACK. */
    private respuestaDelNavegador(m: Mensaje) {
        const codigo = Number(m.primera.split(" ")[1]);
        const callId = una(m, "call-id");
        const d = this.dialogos.get(callId);
        if (!d || !/INVITE/.test(una(m, "cseq")) || codigo < 200) return;
        const contacto = /<([^>]+)>/.exec(una(m, "contact"))?.[1] ?? d.contactoNavegador;
        const ack: [string, string][] = [
            ["Via", `SIP/2.0/WSS 10.20.0.5:8089;branch=z9hG4bK${azar()}`],
            ["Max-Forwards", "70"],
            ["From", d.from],
            ["To", una(m, "to")],
            ["Call-ID", callId],
            ["CSeq", `${d.cseq} ACK`],
        ];
        this.enviar(armar(`ACK ${contacto} SIP/2.0`, ack));
        d.to = una(m, "to");
        d.contactoNavegador = contacto;
        if (codigo >= 300) this.dialogos.delete(callId);
    }

    /** Suena una entrante en la extensión 204 (con `X-Iris-Llamada` si el AMI ya creó la llamada). */
    llamar(numero: string, llamadaId?: string): string {
        const callId = `pbx-${azar()}@10.20.0.5`;
        const from = `<sip:${numero}@pbx.oficina>;tag=pbx${azar()}`;
        const to = "<sip:204@pbx.oficina>";
        const via = `SIP/2.0/WSS 10.20.0.5:8089;branch=z9hG4bK${azar()}`;
        this.dialogos.set(callId, { callId, from, to, contactoNavegador: "sip:204@navegador.invalid;transport=ws", cseq: 1, via });
        const sdp = ofertaIssabel();
        const cab: [string, string][] = [
            ["Via", via],
            ["Max-Forwards", "70"],
            ["From", from],
            ["To", to],
            ["Call-ID", callId],
            ["CSeq", "1 INVITE"],
            ["Contact", "<sip:asterisk@10.20.0.5:8089;transport=ws>"],
            ["Allow", "OPTIONS, INVITE, ACK, BYE, CANCEL, UPDATE, PRACK, REFER, INFO"],
            ["Supported", "timer, replaces"],
            ...(llamadaId ? ([["X-Iris-Llamada", llamadaId]] as [string, string][]) : []),
            ["Content-Type", "application/sdp"],
        ];
        this.enviar(armar("INVITE sip:204@navegador.invalid;transport=ws SIP/2.0", cab, sdp));
        return callId;
    }

    /** El otro lado cuelga la llamada en curso (BYE de la Issabel). */
    colgar(callId: string) {
        const d = this.dialogos.get(callId);
        if (!d) return;
        d.cseq += 1;
        this.enviar(
            armar(`BYE ${d.contactoNavegador} SIP/2.0`, [
                ["Via", `SIP/2.0/WSS 10.20.0.5:8089;branch=z9hG4bK${azar()}`],
                ["Max-Forwards", "70"],
                ["From", d.from],
                ["To", d.to],
                ["Call-ID", callId],
                ["CSeq", `${d.cseq} BYE`],
            ]),
        );
        this.dialogos.delete(callId);
    }

    /** Contesta la saliente que el navegador marcó (200 con la respuesta SDP). */
    contestar(): boolean {
        const [callId, inv] = [...this.pendientes.entries()].at(-1) ?? [];
        if (!callId || !inv) return false;
        this.pendientes.delete(callId);
        this.responder(inv, 200, "OK", [["Contact", "<sip:asterisk@10.20.0.5:8089;transport=ws>"], ["Content-Type", "application/sdp"]], respuestaA(inv.cuerpo), this.etiquetaDe(callId));
        return true;
    }

    /** La saliente pendiente termina sin contestar (486 ocupado, 480 no disponible…). */
    rechazarSaliente(codigo: number, razon: string): boolean {
        const [callId, inv] = [...this.pendientes.entries()].at(-1) ?? [];
        if (!callId || !inv) return false;
        this.pendientes.delete(callId);
        this.responder(inv, codigo, razon, [], "", this.etiquetaDe(callId));
        return true;
    }

    /** ¿Hay una saliente sonando (INVITE sin respuesta final)? */
    sonando(): boolean {
        return this.pendientes.size > 0;
    }

    /** La primera línea de cada mensaje recibido (para revisar el orden). */
    lineas(): string[] {
        return this.recibidos.map((t) => t.split(CRLF)[0] ?? "");
    }

    /** Los mensajes recibidos de un método, o las respuestas con un código. */
    de(que: string): string[] {
        return this.recibidos.filter((t) => (t.split(CRLF)[0] ?? "").startsWith(que) || (t.startsWith("SIP/2.0 ") && t.split(" ")[1] === que));
    }

    cerrar(codigo: number, motivo: string) {
        void this.ws?.close({ code: codigo, reason: motivo });
    }
}

export async function simularSip(page: Page, opciones: OpcionesSip = {}): Promise<SipFalso> {
    const s = new SipFalso(opciones);
    await s.instalar(page);
    return s;
}
