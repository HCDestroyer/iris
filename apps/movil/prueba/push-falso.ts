// Dobles de las notificaciones push para las pruebas: las preferencias de la cuenta con las formas
// del contrato, el `data` de un push de chat y un `ServicioPush` que guarda lo que se le pide.
import type { RespuestaPush } from "../src/push/acciones";
import type { PreferenciasPush } from "../src/push/api";
import type { MensajePush } from "../src/push/mensaje";
import type { PermisoPush, ServicioPush } from "../src/push/servicio";

export function preferencias(extra: Partial<PreferenciasPush> = {}): PreferenciasPush {
    return {
        version: 3,
        tipos: { chat: true, mencion: true, orden: true, bandeja: true },
        contenido: "completo",
        noMolestar: { activo: false, desde: "21:00", hasta: "06:00", urgenteIgual: true },
        zonaHoraria: "America/Guatemala",
        push: { estado: "configurado", texto: "Notificaciones push: activas" },
        bandejaDisponible: false,
        ...extra,
    };
}

/** El `data` de un push de chat como lo arma el servidor (todo texto). */
export function dataChat(extra: Record<string, string> = {}, datos: Record<string, unknown> = {}): Record<string, string> {
    return {
        v: "1",
        id: "8812",
        tipo: "chat",
        canal: "chat",
        usuarioId: "5",
        empresaId: "",
        titulo: "Cuadrilla Norte",
        cuerpo: "Carlos Ruiz: ya llegué a la casa",
        publicoTitulo: "Nuevo mensaje",
        grupo: "chat-12",
        etiqueta: "chat-0193a",
        enlace: "iris://chat/12",
        acciones: "responder,leido",
        ocurridoEn: "2026-11-06T20:32:10Z",
        datos: JSON.stringify({
            conversacionId: 12,
            conversacionTipo: "cuadrilla",
            mensajeId: "0193a",
            seq: 41,
            remitente: { id: 9, nombre: "Carlos Ruiz", iniciales: "CR" },
            contenido: "texto",
            texto: "ya llegué a la casa",
            ...datos,
        }),
        ...extra,
    };
}

export class ServicioPushFalso implements ServicioPush {
    estadoPermiso: PermisoPush;
    /** Lo que devuelve el diálogo del sistema. */
    respuestaDialogo: PermisoPush = "concedido";
    dialogos = 0;
    ajustesAbiertos = 0;
    tokenActual: string | null = "t".repeat(150);
    presentadas: MensajePush[] = [];
    gruposQuitados: string[] = [];
    private recibir: ((d: Record<string, unknown>) => void) | null = null;
    private responder: ((r: RespuestaPush) => void) | null = null;
    private oyenteToken: ((t: string) => void) | null = null;
    /** La respuesta que abrió la app cerrada (se entrega al suscribirse). */
    pendiente: RespuestaPush | null = null;

    constructor(permiso: PermisoPush = "concedido") {
        this.estadoPermiso = permiso;
    }

    async preparar() {}
    async permiso() {
        return this.estadoPermiso;
    }
    async pedirPermiso() {
        this.dialogos += 1;
        this.estadoPermiso = this.respuestaDialogo;
        return this.estadoPermiso;
    }
    async token() {
        return this.estadoPermiso === "concedido" ? this.tokenActual : null;
    }
    alCambiarToken(fn: (t: string) => void) {
        this.oyenteToken = fn;
        return () => {
            this.oyenteToken = null;
        };
    }
    alRecibir(fn: (d: Record<string, unknown>) => void) {
        this.recibir = fn;
        return () => {
            this.recibir = null;
        };
    }
    alResponder(fn: (r: RespuestaPush) => void) {
        this.responder = fn;
        if (this.pendiente) {
            const r = this.pendiente;
            this.pendiente = null;
            fn(r);
        }
        return () => {
            this.responder = null;
        };
    }
    async presentar(m: MensajePush) {
        this.presentadas.push(m);
    }
    async quitarGrupo(g: string) {
        this.gruposQuitados.push(g);
    }
    todasQuitadas = 0;
    async quitarTodas() {
        this.todasQuitadas += 1;
    }
    async abrirAjustes() {
        this.ajustesAbiertos += 1;
    }

    /** FCM entrega un push con la app al frente. */
    llega(data: Record<string, unknown>) {
        this.recibir?.(data);
    }
    /** El técnico toca la notificación o un botón. */
    toca(r: RespuestaPush) {
        this.responder?.(r);
    }
    rotarToken(t: string) {
        this.oyenteToken?.(t);
    }
}
