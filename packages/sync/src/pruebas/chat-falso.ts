// Servidor de chat falso en memoria para probar `MotorChat` (y las pantallas): conversaciones,
// mensajes con `seq`, idempotencia por id, eliminar con plazo, lecturas, feed con cursor y un
// flujo SSE manual. Las fallas se programan con `fallar`.
import { ErrorTransporte } from "../errores";
import type { Adjunto, EntradaRegistrarAdjunto } from "../contrato";
import type { Subidor } from "../transporte";
import type {
    AbrirFlujoChat,
    ConsultaMensajes,
    ConversacionChat,
    ConversacionChatDetalle,
    CuerpoEnviarMensaje,
    EventoChat,
    FlujoChat,
    LecturaMiembroChat,
    MensajeChat,
    OyentesFlujo,
    PaginaPersonaChat,
    TransporteChat,
} from "../chat/tipos";

export type MetodoChat = keyof TransporteChat | "crearAdjunto" | "enviarParte";

const problema = (codigo: string, status: number) => ({ type: `urn:iris:problema:${codigo}`, title: codigo, status });

export function errorHttp(status: number, codigo: string, cabeceras?: Record<string, string>): ErrorTransporte {
    return new ErrorTransporte("http", codigo, status, problema(codigo, status), cabeceras ? new Headers(cabeceras) : null);
}

export const errorRed = () => new ErrorTransporte("red", "Sin conexión");

interface FlujoAbierto {
    desde: string | null;
    oyentes: OyentesFlujo;
    cerrado: boolean;
}

export class ServidorChatFalso implements TransporteChat {
    readonly yo: number;
    /** Nombre del dueño (autor de lo que envía) y de otras personas (directas nuevas). */
    nombreYo = "Yo";
    readonly nombres = new Map<number, string>();
    ahora = () => Date.parse("2026-11-05T17:00:00Z");
    readonly conversaciones = new Map<number, ConversacionChat>();
    readonly mensajes = new Map<number, MensajeChat[]>();
    readonly lecturas = new Map<number, LecturaMiembroChat[]>();
    readonly eventos: EventoChat[] = [];
    /** Cursor más viejo que se conserva (antes: 410). */
    retencion = 0;
    readonly llamadas: { metodo: MetodoChat; args: unknown[] }[] = [];
    private readonly fallas: { metodo: MetodoChat; error: ErrorTransporte; veces: number }[] = [];
    readonly flujos: FlujoAbierto[] = [];
    readonly adjuntos = new Map<string, { entrada: EntradaRegistrarAdjunto; recibido: number }>();
    private siguienteConv = 100;

    constructor(yo: number) {
        this.yo = yo;
    }

    /** La próxima (o las próximas `veces`) llamada a `metodo` falla con `error`. */
    fallar(metodo: MetodoChat, error: ErrorTransporte, veces = 1): void {
        this.fallas.push({ metodo, error, veces });
    }

    llamadasA(metodo: MetodoChat): unknown[][] {
        return this.llamadas.filter((l) => l.metodo === metodo).map((l) => l.args);
    }

    private registrar(metodo: MetodoChat, args: unknown[]): void {
        this.llamadas.push({ metodo, args });
        const i = this.fallas.findIndex((f) => f.metodo === metodo);
        const f = this.fallas[i];
        if (f) {
            f.veces -= 1;
            if (f.veces <= 0) {
                this.fallas.splice(i, 1);
            }
            throw f.error;
        }
    }

    crearConversacion(datos: Partial<ConversacionChat> & Pick<ConversacionChat, "tipo" | "titulo">): ConversacionChat {
        const id = datos.id ?? this.siguienteConv++;
        const c: ConversacionChat = {
            id,
            empresa_id: 1,
            cuadrilla_id: null,
            orden_id: null,
            orden_estado: null,
            orden_tipo: null,
            menciones: 0,
            solo_lectura: false,
            no_leidos: 0,
            leido_hasta_mensaje_id: null,
            ultima_actividad: new Date(this.ahora()).toISOString(),
            creada_en: new Date(this.ahora()).toISOString(),
            ...datos,
        };
        this.conversaciones.set(id, c);
        this.mensajes.set(id, this.mensajes.get(id) ?? []);
        return c;
    }

    /** Mensaje de otra persona que llega al servidor (y al feed). */
    recibir(conversacionId: number, autor: { id: number; nombre: string }, texto: string): MensajeChat {
        const lista = this.mensajes.get(conversacionId) ?? [];
        const m: MensajeChat = {
            id: `0193${String(lista.length + 1).padStart(4, "0")}-0000-7000-8000-${String(conversacionId).padStart(12, "0")}`,
            conversacion_id: conversacionId,
            seq: lista.length + 1,
            autor_id: autor.id,
            autor_nombre: autor.nombre,
            texto,
            creado_en: new Date(this.ahora()).toISOString(),
            enviado_en: new Date(this.ahora()).toISOString(),
            eliminado: false,
            eliminado_en: null,
            menciones: [],
        };
        lista.push(m);
        this.mensajes.set(conversacionId, lista);
        const c = this.conversaciones.get(conversacionId);
        if (c) {
            c.no_leidos += autor.id === this.yo ? 0 : 1;
            c.ultima_actividad = m.enviado_en;
            c.ultimo_mensaje = { id: m.id, autor_id: m.autor_id, autor_nombre: m.autor_nombre, contenido: "texto", texto, enviado_en: m.enviado_en };
        }
        this.emitir({ tipo: "mensaje_nuevo", menciones: 0, conversacion_id: conversacionId, no_leidos: c?.no_leidos ?? 0, mensaje: structuredClone(m) });
        return m;
    }

    /** Otro miembro leyó hasta ese mensaje. */
    leyo(conversacionId: number, usuario: { id: number; nombre: string }, mensajeId: string): void {
        const m = this.mensajes.get(conversacionId)?.find((x) => x.id === mensajeId);
        const l: LecturaMiembroChat = { usuario_id: usuario.id, nombre: usuario.nombre, hasta_mensaje_id: mensajeId, hasta_seq: m?.seq ?? 0, leido_en: new Date(this.ahora()).toISOString() };
        this.lecturas.set(conversacionId, [...(this.lecturas.get(conversacionId) ?? []).filter((x) => x.usuario_id !== usuario.id), l]);
        this.emitir({
            tipo: "leido",
            conversacion_id: conversacionId,
            no_leidos: this.conversaciones.get(conversacionId)?.no_leidos ?? 0,
            menciones: 0,
            lectura: { usuario_id: usuario.id, hasta_mensaje_id: mensajeId, leido_en: l.leido_en },
        });
    }

    emitir(e: Omit<EventoChat, "id" | "ocurrido_en">): EventoChat {
        const evento: EventoChat = { ...e, id: String(this.eventos.length + 1), ocurrido_en: new Date(this.ahora()).toISOString() } as EventoChat;
        this.eventos.push(evento);
        for (const f of this.flujos) {
            if (!f.cerrado) {
                f.oyentes.alEvento(structuredClone(evento));
            }
        }
        return evento;
    }

    private detalle(id: number): ConversacionChatDetalle {
        const c = this.conversaciones.get(id);
        if (!c) {
            throw errorHttp(404, "no_encontrado");
        }
        return { ...structuredClone(c), lecturas: structuredClone(this.lecturas.get(id) ?? []) };
    }

    // --- TransporteChat ----------------------------------------------------------------------

    listarConversaciones(cursor?: string) {
        this.registrar("listarConversaciones", [cursor]);
        const data = [...this.conversaciones.values()].sort((a, b) => Date.parse(b.ultima_actividad) - Date.parse(a.ultima_actividad)).map((c) => structuredClone(c));
        return Promise.resolve({ data, nextCursor: null });
    }

    verConversacion(id: number) {
        this.registrar("verConversacion", [id]);
        return Promise.resolve(this.detalle(id));
    }

    /**
     * `GET /chat/personas`: las de `nombres` (personas de la empresa con `chat.ver`), sin el dueño,
     * por nombre; `q` filtra sin distinguir mayúsculas. Páginas de `tamanoPersonas`.
     */
    tamanoPersonas = 50;
    personas(q: string, cursor?: string): Promise<PaginaPersonaChat> {
        this.registrar("personas", [q, cursor]);
        const filtro = q.trim().toLocaleLowerCase("es");
        const todas = [...this.nombres.entries()]
            .filter(([id, nombre]) => id !== this.yo && (!filtro || nombre.toLocaleLowerCase("es").includes(filtro)))
            .sort((a, b) => a[1].localeCompare(b[1], "es") || a[0] - b[0])
            .map(([id, nombre]) => ({ id, nombre, conversacion_id: [...this.conversaciones.values()].find((c) => c.tipo === "directa" && c.otro_usuario?.id === id)?.id ?? null }));
        const desde = cursor ? Number(cursor) : 0;
        const data = todas.slice(desde, desde + this.tamanoPersonas);
        const siguiente = desde + this.tamanoPersonas;
        return Promise.resolve({ data, nextCursor: siguiente < todas.length ? String(siguiente) : null });
    }

    abrirDirecta(usuarioId: number) {
        this.registrar("abrirDirecta", [usuarioId]);
        const existe = [...this.conversaciones.values()].find((c) => c.tipo === "directa" && c.otro_usuario?.id === usuarioId);
        const nombre = this.nombres.get(usuarioId) ?? `Usuario ${usuarioId}`;
        const c = existe ?? this.crearConversacion({ tipo: "directa", titulo: nombre, otro_usuario: { id: usuarioId, nombre } });
        return Promise.resolve(this.detalle(c.id));
    }

    abrirCuadrilla(cuadrillaId: number) {
        this.registrar("abrirCuadrilla", [cuadrillaId]);
        const existe = [...this.conversaciones.values()].find((c) => c.tipo === "cuadrilla" && c.cuadrilla_id === cuadrillaId);
        const c = existe ?? this.crearConversacion({ tipo: "cuadrilla", titulo: `Cuadrilla ${cuadrillaId}`, cuadrilla_id: cuadrillaId });
        return Promise.resolve(this.detalle(c.id));
    }

    abrirOrden(ordenId: number) {
        this.registrar("abrirOrden", [ordenId]);
        const existe = [...this.conversaciones.values()].find((c) => c.tipo === "orden" && c.orden_id === ordenId);
        const c = existe ?? this.crearConversacion({ tipo: "orden", titulo: `OT-${String(ordenId).padStart(6, "0")}`, orden_id: ordenId, orden_estado: "en_sitio" });
        return Promise.resolve(this.detalle(c.id));
    }

    listarMensajes(conversacionId: number, c: ConsultaMensajes) {
        this.registrar("listarMensajes", [conversacionId, c]);
        if (!this.conversaciones.has(conversacionId)) {
            return Promise.reject(errorHttp(404, "no_encontrado"));
        }
        const todos = this.mensajes.get(conversacionId) ?? [];
        const limite = c.limit ?? 30;
        if (c.direccion === "siguientes") {
            const desde = c.cursor ? Number(c.cursor) : (c.desdeSeq ?? 0);
            const tramo = todos.filter((m) => m.seq > desde).slice(0, limite);
            const ultimo = tramo.at(-1);
            const mas = ultimo !== undefined && todos.some((m) => m.seq > ultimo.seq);
            return Promise.resolve({ data: structuredClone(tramo), nextCursor: mas && ultimo ? String(ultimo.seq) : null });
        }
        const hasta = c.cursor ? Number(c.cursor) : (c.desdeSeq ?? Number.POSITIVE_INFINITY);
        const previos = todos.filter((m) => m.seq < hasta);
        const tramo = previos.slice(-limite);
        const primero = tramo[0];
        return Promise.resolve({ data: structuredClone(tramo), nextCursor: primero && primero.seq > 1 ? String(primero.seq) : null });
    }

    enviarMensaje(conversacionId: number, cuerpo: CuerpoEnviarMensaje) {
        this.registrar("enviarMensaje", [conversacionId, structuredClone(cuerpo)]);
        const lista = this.mensajes.get(conversacionId);
        if (!lista) {
            return Promise.reject(errorHttp(404, "no_encontrado"));
        }
        const previo = lista.find((m) => m.id === cuerpo.id);
        if (previo) {
            if (!previo.eliminado && (previo.texto ?? undefined) !== cuerpo.texto) {
                return Promise.reject(errorHttp(409, "mensaje_id_reutilizado"));
            }
            return Promise.resolve({ mensaje: structuredClone(previo), reenvio: true });
        }
        const m: MensajeChat = {
            id: cuerpo.id,
            conversacion_id: conversacionId,
            seq: lista.length + 1,
            autor_id: this.yo,
            autor_nombre: this.nombreYo,
            texto: cuerpo.texto ?? null,
            creado_en: cuerpo.creado_en ?? null,
            enviado_en: new Date(this.ahora()).toISOString(),
            eliminado: false,
            eliminado_en: null,
            menciones: [],
            ...(cuerpo.ubicacion ? { ubicacion: cuerpo.ubicacion } : {}),
            ...(cuerpo.adjunto_id ? { adjunto: { id: cuerpo.adjunto_id, estado: "subiendo" as const, url_descarga: `/api/v1/adjuntos/${cuerpo.adjunto_id}` } } : {}),
        };
        lista.push(m);
        this.emitir({ tipo: "mensaje_nuevo", menciones: 0, conversacion_id: conversacionId, no_leidos: this.conversaciones.get(conversacionId)?.no_leidos ?? 0, mensaje: structuredClone(m) });
        return Promise.resolve({ mensaje: structuredClone(m), reenvio: false });
    }

    eliminarMensaje(mensajeId: string) {
        this.registrar("eliminarMensaje", [mensajeId]);
        for (const lista of this.mensajes.values()) {
            const m = lista.find((x) => x.id === mensajeId);
            if (m) {
                if (this.ahora() - Date.parse(m.enviado_en) > 15 * 60_000) {
                    return Promise.reject(errorHttp(422, "plazo_vencido"));
                }
                Object.assign(m, { eliminado: true, eliminado_en: new Date(this.ahora()).toISOString(), texto: null });
                delete m.adjunto;
                delete m.ubicacion;
                this.emitir({ tipo: "mensaje_eliminado", menciones: 0, conversacion_id: m.conversacion_id, no_leidos: 0, mensaje: structuredClone(m) });
                return Promise.resolve(structuredClone(m));
            }
        }
        return Promise.reject(errorHttp(404, "no_encontrado"));
    }

    leer(conversacionId: number, hasta: string) {
        this.registrar("leer", [conversacionId, hasta]);
        const c = this.conversaciones.get(conversacionId);
        if (c) {
            c.no_leidos = 0;
            c.leido_hasta_mensaje_id = hasta;
        }
        return Promise.resolve({ no_leidos: 0, leido_hasta_mensaje_id: hasta });
    }

    cambios(cursor: string | null) {
        this.registrar("cambios", [cursor]);
        const cabeza = String(this.eventos.length);
        if (cursor === null) {
            return Promise.resolve({ datos: { data: [], nextCursor: cabeza, hayMas: false }, horaServidor: null });
        }
        if (Number(cursor) < this.retencion) {
            return Promise.reject(errorHttp(410, "cursor_vencido"));
        }
        const data = this.eventos.filter((e) => Number(e.id) > Number(cursor)).map((e) => structuredClone(e));
        return Promise.resolve({ datos: { data, nextCursor: data.at(-1)?.id ?? cursor, hayMas: false }, horaServidor: null });
    }

    // --- Flujo SSE manual --------------------------------------------------------------------

    /** `AbrirFlujoChat`: reenvía lo que haya después de `desde` y luego cada evento nuevo. */
    readonly abrirFlujo: AbrirFlujoChat = (desde, oyentes): FlujoChat => {
        this.llamadas.push({ metodo: "cambios", args: ["flujo", desde] });
        const f: FlujoAbierto = { desde, oyentes, cerrado: false };
        this.flujos.push(f);
        queueMicrotask(() => {
            if (f.cerrado) {
                return;
            }
            oyentes.alAbrir();
            if (desde !== null) {
                for (const e of this.eventos.filter((x) => Number(x.id) > Number(desde))) {
                    oyentes.alEvento(structuredClone(e));
                }
            }
        });
        return {
            cerrar: () => {
                f.cerrado = true;
            },
        };
    };

    /** El servidor cierra los flujos abiertos (30 min o un corte). */
    cortarFlujos(): void {
        for (const f of this.flujos) {
            if (!f.cerrado) {
                f.cerrado = true;
                f.oyentes.alCerrar();
            }
        }
    }

    get flujosAbiertos(): number {
        return this.flujos.filter((f) => !f.cerrado).length;
    }

    // --- Subidor (fotos) ---------------------------------------------------------------------

    readonly subidor: Subidor = {
        crear: (entrada) => {
            this.registrar("crearAdjunto", [structuredClone(entrada)]);
            const previo = this.adjuntos.get(entrada.id);
            const a = previo ?? { entrada, recibido: 0 };
            this.adjuntos.set(entrada.id, a);
            return Promise.resolve(this.adjunto(a));
        },
        consultarOffset: (id) => Promise.resolve(this.adjuntos.get(id)?.recibido ?? 0),
        enviarParte: (id, offset, parte) => {
            this.registrar("enviarParte", [id, offset, parte.byteLength]);
            const a = this.adjuntos.get(id);
            if (!a) {
                return Promise.reject(errorHttp(404, "no_encontrado"));
            }
            a.recibido = Math.min(a.entrada.tamano, offset + parte.byteLength);
            return Promise.resolve(a.recibido);
        },
    };

    private adjunto(a: { entrada: EntradaRegistrarAdjunto; recibido: number }): Adjunto {
        const e = a.entrada;
        return {
            id: e.id,
            entidad: e.entidad,
            entidadId: e.entidadId,
            tipo: e.tipo,
            claveChecklist: null,
            mime: e.mime,
            tamano: e.tamano,
            sha256: e.sha256,
            recibido: a.recibido,
            estado: a.recibido === e.tamano ? "completo" : "pendiente",
            motivoRechazo: null,
            lat: null,
            lon: null,
            tomadoEn: null,
            subidoPor: this.yo,
            creadoEn: new Date(this.ahora()).toISOString(),
            completadoEn: null,
            urlSubida: `/api/v1/adjuntos/${e.id}/subida`,
            urlDescarga: `/api/v1/adjuntos/${e.id}`,
            version: 1,
        };
    }
}
