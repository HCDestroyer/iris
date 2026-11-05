// Motor del chat interno de la app de campo (`contratos/chat.md` §5), compartido por la PWA y la app
// nativa. Sin DOM ni React: persiste en un `AlmacenChat`, habla por un `TransporteChat` (y un
// `Subidor` para las fotos), mide el tiempo con un `Reloj` y, si se le da, escucha el flujo SSE.
//
// - Escribir guarda primero (cola de salida con id uuid v7 y `creado_en`) y despierta el envío.
//   Sale uno a uno, en orden de creación; 201/200 reemplaza el local por el del servidor.
//   No se reintenta: 403, 404, 409, 422 (queda «no enviado», con «Reintentar»). Sí: 429 (con
//   `Retry-After`), 5xx y sin red, con espera exponencial. 401 detiene todo (hay que entrar).
// - Tiempo real solo en primer plano: SSE si hay flujo (PWA) o sondeo de `/cambios` cada 12 s.
//   Todo se aplica por id (un evento puede llegar dos veces) y el cursor se guarda. 410
//   `cursor_vencido` borra la caché (no la cola), recarga y sigue sin cursor.
// - Nunca registra el texto de un mensaje.
import { ErrorTransporte, CODIGO_SESION_AJENA } from "../errores";
import { msDeRetryAfter } from "../posiciones";
import { calcularEspera, ESPERA_POR_DEFECTO, relojSistema, type OpcionesEspera, type Reloj } from "../reloj";
import type { Subidor } from "../transporte";
import { crearGeneradorUuid7, type GeneradorUuid7 } from "../uuid7";
import {
    META_CHAT_INICIAL,
    type AbrirFlujoChat,
    type AlmacenChat,
    type ConversacionChat,
    type ConversacionChatDetalle,
    type CuerpoEnviarMensaje,
    type ErrorChat,
    type EventoChat,
    type FlujoChat,
    type LecturaMiembroChat,
    type LoteChat,
    type MensajeChat,
    type MetaChat,
    type SalidaChat,
    type TransporteChat,
    type UbicacionChat,
    type VistaPreviaChat,
} from "./tipos";

const MINUTO_MS = 60_000;
const TEXTO_MAXIMO = 4000;

export type EstadoCarga = "inactiva" | "cargando" | "lista" | "error";

/** Cómo llegan los cambios: flujo SSE, sondeo, sin red o detenido (segundo plano, sin sesión). */
export type ModoTiempoReal = "detenido" | "conectando" | "sse" | "sondeo" | "sin_conexion";

export interface ConversacionVista extends ConversacionChat {
    /** Mensajes propios en la cola (sin enviar). */
    pendientes: number;
    /** De ellos, los que el servidor no aceptó. */
    fallidos: number;
}

export type EnvioMensaje = "enviado" | "enviando" | "sin_senal" | "fallido" | "eliminando";

export interface FotoVista {
    id: string;
    /** `local`: solo está en el teléfono (se muestra la copia local). */
    estado: "local" | "pendiente" | "subiendo" | "completo" | "rechazado";
    url: string | null;
}

export interface MensajeVista {
    id: string;
    /** `null` mientras está en la cola local (va al final). */
    seq: number | null;
    autorId: number;
    autorNombre: string;
    propio: boolean;
    texto: string | null;
    ubicacion: UbicacionChat | null;
    foto: FotoVista | null;
    creadoEn: string | null;
    enviadoEn: string | null;
    eliminado: boolean;
    envio: EnvioMensaje;
    error: ErrorChat | null;
    /** Propio y otro miembro leyó hasta aquí. */
    visto: boolean;
}

export interface VistaConversacion {
    id: number;
    conversacion: ConversacionVista | null;
    mensajes: MensajeVista[];
    lecturas: LecturaMiembroChat[];
    carga: EstadoCarga;
    error: ErrorChat | null;
    hayAnteriores: boolean;
    cargandoAnteriores: boolean;
}

export interface AvisoChat {
    id: number;
    tono: "error" | "warning";
    texto: string;
}

export interface EstadoChat {
    /** Ya cargó lo guardado en el teléfono. */
    listo: boolean;
    conversaciones: ConversacionVista[];
    /** Suma de no leídos (el punto de la isla). */
    noLeidos: number;
    lista: { estado: EstadoCarga; error: ErrorChat | null };
    tiempoReal: ModoTiempoReal;
    enLinea: boolean;
    /** 401 o sesión de otra persona: no se envía ni se lee hasta volver a entrar. */
    sesionCerrada: boolean;
    /** Mensajes y eliminaciones en la cola de salida. */
    pendientes: number;
    fallidos: number;
    avisos: AvisoChat[];
}

export interface EntradaMensaje {
    texto?: string;
    ubicacion?: UbicacionChat;
    foto?: { datos: Uint8Array; mime: string; tomadoEn?: string };
}

export interface OpcionesMotorChat {
    almacen: AlmacenChat;
    transporte: TransporteChat;
    /** Alta y subida tus de las fotos (`POST /adjuntos`); sin él, las fotos no salen. */
    subidor?: Subidor | null;
    usuarioId: number;
    reloj?: Reloj;
    generarId?: GeneradorUuid7;
    sha256?: (datos: Uint8Array) => Promise<string>;
    /** Flujo SSE (PWA). Sin él se sondea `/cambios`. */
    abrirFlujo?: AbrirFlujoChat | null;
    /** Sondeo con la app abierta (12 s; el contrato sugiere 10–15 s). */
    intervaloSondeoMs?: number;
    /** Espera tras cerrarse el flujo antes de abrir otro (el `retry: 3000` del servidor). */
    reconexionMs?: number;
    espera?: Partial<OpcionesEspera>;
    /** Como el motor de sync: lanza `ErrorTransporte` si la sesión abierta no es la del dueño. */
    verificarIdentidad?: () => Promise<void>;
    /** Cada cuánto se vuelve a verificar la identidad antes de hablar con la API (60 s). */
    verificarCadaMs?: number;
    tamanoParte?: number;
    /** Mensajes guardados por conversación (los más recientes). */
    maximoPorConversacion?: number;
}

/** El teléfono no acepta lo que se pidió (antes de encolar). */
export class ErrorChatLocal extends Error {
    readonly codigo: "mensaje_vacio" | "texto_largo" | "ubicacion_invalida" | "conversacion_desconocida" | "no_eliminable" | "sin_subidor";

    constructor(codigo: ErrorChatLocal["codigo"], mensaje: string) {
        super(mensaje);
        this.name = "ErrorChatLocal";
        this.codigo = codigo;
    }
}

/** Códigos del servidor que no se arreglan reintentando el mismo envío con el mismo id. */
const CODIGOS_ID_NUEVO = new Set(["mensaje_id_reutilizado", "idempotencia_reutilizada"]);

async function sha256Sistema(datos: Uint8Array): Promise<string> {
    const resumen = await globalThis.crypto.subtle.digest("SHA-256", datos as Uint8Array<ArrayBuffer>);
    return Array.from(new Uint8Array(resumen), (x) => x.toString(16).padStart(2, "0")).join("");
}

function errorVisible(e: unknown): ErrorChat {
    if (e instanceof ErrorTransporte) {
        return { mensaje: e.problema?.detail ?? e.message, status: e.status, codigo: e.codigo };
    }
    return { mensaje: e instanceof Error ? e.message : String(e), status: null, codigo: null };
}

const esSesion = (e: unknown) => e instanceof ErrorTransporte && (e.status === 401 || e.codigo === CODIGO_SESION_AJENA || e.codigo === "dispositivo_ajeno");
const esEspera = (e: unknown) => e instanceof ErrorTransporte && (e.status === 429 || e.status === 503);

/** Vista previa de la lista a partir del estado actual del mensaje. */
export function vistaPreviaDe(m: MensajeChat): VistaPreviaChat {
    const contenido: VistaPreviaChat["contenido"] = m.eliminado ? "eliminado" : m.texto ? "texto" : m.adjunto ? "foto" : m.ubicacion ? "ubicacion" : "texto";
    return {
        id: m.id,
        autor_id: m.autor_id,
        autor_nombre: m.autor_nombre,
        contenido,
        enviado_en: m.enviado_en,
        texto: m.eliminado || !m.texto ? null : m.texto.slice(0, 140),
    };
}

const ms = (iso: string | null | undefined) => {
    const v = iso ? Date.parse(iso) : Number.NaN;
    return Number.isNaN(v) ? 0 : v;
};

type Oyente = (estado: EstadoChat) => void;

interface CargaConversacion {
    carga: EstadoCarga;
    error: ErrorChat | null;
    cursorAnteriores: string | null;
    hayAnteriores: boolean;
    cargandoAnteriores: boolean;
}

export class MotorChat {
    readonly usuarioId: number;
    private readonly almacen: AlmacenChat;
    private readonly transporte: TransporteChat;
    private readonly subidor: Subidor | null;
    private readonly reloj: Reloj;
    private readonly generarId: GeneradorUuid7;
    private readonly sha256: (datos: Uint8Array) => Promise<string>;
    private readonly abrirFlujo: AbrirFlujoChat | null;
    private readonly intervaloSondeoMs: number;
    private readonly reconexionMs: number;
    private readonly espera: OpcionesEspera;
    private readonly verificarIdentidad: (() => Promise<void>) | null;
    private readonly verificarCadaMs: number;
    private readonly tamanoParte: number;
    private readonly maximoPorConversacion: number;

    private readonly conversaciones = new Map<number, ConversacionChat>();
    private readonly mensajes = new Map<number, Map<string, MensajeChat>>();
    private readonly lecturas = new Map<number, Map<number, LecturaMiembroChat>>();
    private readonly salida = new Map<string, SalidaChat>();
    private readonly cargas = new Map<number, CargaConversacion>();
    private meta: MetaChat = structuredClone(META_CHAT_INICIAL);
    private lista: EstadoChat["lista"] = { estado: "inactiva", error: null };
    private tiempoReal: ModoTiempoReal = "detenido";
    private enLinea = true;
    private sesionCerrada = false;
    private avisos: AvisoChat[] = [];
    private siguienteAviso = 1;
    private ultimaVerificacion = Number.NEGATIVE_INFINITY;
    private bloqueoHasta = 0;
    private listo = false;

    private readonly oyentes = new Set<Oyente>();
    private instantanea: EstadoChat;
    private version = 0;
    private readonly vistas = new Map<number, { version: number; vista: VistaConversacion }>();

    private escrituras: Promise<unknown> = Promise.resolve();
    private cadenaEventos: Promise<unknown> = Promise.resolve();
    private cadenaSalida: Promise<unknown> = Promise.resolve();
    private bucleSalida: AbortController | null = null;
    private suenoSalida: AbortController | null = null;
    private salidaEnEspera = false;
    private salidaDespertada = false;
    private bucleTiempo: AbortController | null = null;
    private suenoTiempo: AbortController | null = null;
    private enPrimerPlano = true;
    private iniciado = false;
    private recargandoLista: Promise<void> | null = null;
    private readonly leyendo = new Set<number>();
    private readonly delDia = { ordenes: new Set<number>(), cuadrillas: new Set<number>() };
    private readonly abiertasDelDia = new Set<string>();
    private readonly pidiendoDetalle = new Set<number>();

    private constructor(o: OpcionesMotorChat) {
        this.usuarioId = o.usuarioId;
        this.almacen = o.almacen;
        this.transporte = o.transporte;
        this.subidor = o.subidor ?? null;
        this.reloj = o.reloj ?? relojSistema;
        this.generarId = o.generarId ?? crearGeneradorUuid7(() => this.reloj.ahora());
        this.sha256 = o.sha256 ?? sha256Sistema;
        this.abrirFlujo = o.abrirFlujo ?? null;
        this.intervaloSondeoMs = o.intervaloSondeoMs ?? 12_000;
        this.reconexionMs = o.reconexionMs ?? 3_000;
        this.espera = { ...ESPERA_POR_DEFECTO, ...o.espera };
        this.verificarIdentidad = o.verificarIdentidad ?? null;
        this.verificarCadaMs = o.verificarCadaMs ?? MINUTO_MS;
        this.tamanoParte = o.tamanoParte ?? 256 * 1024;
        this.maximoPorConversacion = Math.max(20, o.maximoPorConversacion ?? 300);
        this.instantanea = this.calcularEstado();
    }

    /** Carga la caché y la cola del teléfono. No llama a la red. */
    static async abrir(opciones: OpcionesMotorChat): Promise<MotorChat> {
        const m = new MotorChat(opciones);
        const c = await opciones.almacen.cargarChat();
        m.meta = { ...structuredClone(META_CHAT_INICIAL), ...c.meta };
        for (const conv of c.conversaciones) {
            m.conversaciones.set(conv.id, conv);
        }
        for (const msj of c.mensajes) {
            m.mapaMensajes(msj.conversacion_id).set(msj.id, msj);
        }
        for (const s of c.salida) {
            m.salida.set(s.id, s);
        }
        m.listo = true;
        m.emitir();
        return m;
    }

    // --- Suscripción ------------------------------------------------------------------------

    suscribir = (oyente: Oyente): (() => void) => {
        this.oyentes.add(oyente);
        return () => {
            this.oyentes.delete(oyente);
        };
    };

    obtenerEstado = (): EstadoChat => this.instantanea;

    /** La conversación para pintar (el mismo objeto hasta el próximo cambio). */
    obtenerConversacion(id: number): VistaConversacion {
        const cache = this.vistas.get(id);
        if (cache && cache.version === this.version) {
            return cache.vista;
        }
        const vista = this.calcularVista(id);
        this.vistas.set(id, { version: this.version, vista });
        return vista;
    }

    /** Bytes de una foto que solo está en el teléfono (para mostrar la copia local). */
    leerFotoLocal(id: string): Promise<Uint8Array | null> {
        return this.almacen.leerBytesChat(id);
    }

    /** Personas con las que se puede abrir una directa, de lo que el teléfono ya conoce. */
    contactosConocidos(): { id: number; nombre: string }[] {
        const vistos = new Map<number, string>();
        for (const c of this.conversaciones.values()) {
            if (c.otro_usuario) {
                vistos.set(c.otro_usuario.id, c.otro_usuario.nombre);
            }
            if (c.ultimo_mensaje && c.ultimo_mensaje.autor_id !== this.usuarioId) {
                vistos.set(c.ultimo_mensaje.autor_id, c.ultimo_mensaje.autor_nombre);
            }
        }
        for (const mapa of this.mensajes.values()) {
            for (const m of mapa.values()) {
                if (m.autor_id !== this.usuarioId) {
                    vistos.set(m.autor_id, m.autor_nombre);
                }
            }
        }
        for (const mapa of this.lecturas.values()) {
            for (const l of mapa.values()) {
                if (l.usuario_id !== this.usuarioId && l.nombre) {
                    vistos.set(l.usuario_id, l.nombre);
                }
            }
        }
        return [...vistos].map(([id, nombre]) => ({ id, nombre })).sort((a, b) => a.nombre.localeCompare(b.nombre, "es"));
    }

    // --- Ciclo de vida -----------------------------------------------------------------------

    /** Arranca el envío y, si la app está al frente, el tiempo real. */
    iniciar(): void {
        this.iniciado = true;
        if (!this.bucleSalida) {
            const c = new AbortController();
            this.bucleSalida = c;
            void this.correrSalida(c.signal);
        }
        this.arrancarTiempoReal();
    }

    /** Detiene todo (cerrar sesión, cambiar de empresa, desmontar). */
    detener(): void {
        this.iniciado = false;
        this.bucleSalida?.abort();
        this.bucleSalida = null;
        this.suenoSalida?.abort();
        this.pararTiempoReal();
    }

    /** La app volvió al frente: abre el flujo (o el sondeo) desde el cursor guardado. */
    primerPlano(): void {
        this.enPrimerPlano = true;
        this.arrancarTiempoReal();
        this.reintentarAhora();
    }

    /** La app pasó a segundo plano: cierra el flujo y deja de sondear. */
    segundoPlano(): void {
        this.enPrimerPlano = false;
        this.pararTiempoReal();
    }

    establecerConexion(enLinea: boolean): void {
        if (this.enLinea === enLinea) {
            return;
        }
        this.enLinea = enLinea;
        this.emitir();
        if (enLinea) {
            this.reintentarAhora();
        }
    }

    /** Volvió la señal o el técnico tocó «Reintentar»: corta las esperas (no el `Retry-After`). */
    reintentarAhora(): void {
        this.salidaEnEspera = false;
        this.salidaDespertada = true;
        this.suenoSalida?.abort();
        this.suenoTiempo?.abort();
    }

    /** Cerrar sesión conservando lo pendiente: borra la caché (no la cola de salida). */
    async olvidarCache(): Promise<void> {
        this.detener();
        await this.encadenarEscritura(() => this.almacen.borrarCacheChat());
        this.conversaciones.clear();
        this.mensajes.clear();
        this.lecturas.clear();
        this.cargas.clear();
        this.meta = { ...this.meta, cursor: null, cargadas: [] };
        this.emitir();
    }

    /** Órdenes y cuadrillas del día: se abren sus hilos (idempotente) al arrancar el feed. */
    fijarDelDia(delDia: { ordenes: readonly number[]; cuadrillas: readonly number[] }): void {
        this.delDia.ordenes = new Set(delDia.ordenes);
        this.delDia.cuadrillas = new Set(delDia.cuadrillas.filter((c) => c > 0));
        if (this.meta.cursor !== null && this.enLinea && !this.sesionCerrada) {
            void this.abrirDelDia().catch(() => undefined);
        }
    }

    // --- Lista y conversaciones --------------------------------------------------------------

    /** «Deslizar para actualizar»: pone al día el feed y vuelve a leer la lista. No lanza. */
    async recargar(): Promise<void> {
        try {
            await this.verificar(true);
            if (this.meta.cursor === null) {
                await this.arrancarFeed();
            } else {
                await this.sondearHastaAlDia();
                await this.recargarLista();
            }
        } catch (e) {
            this.alFallarLectura(e);
        }
    }

    /** Abre (o trae) la directa con esa persona. Lanza `ErrorTransporte` (422 `usuario_no_disponible`). */
    async abrirDirecta(usuarioId: number): Promise<ConversacionChat> {
        await this.verificar();
        const d = await this.transporte.abrirDirecta(usuarioId);
        await this.guardarDetalle(d);
        return d;
    }

    /** «Abrir chat de la orden» (`POST /chat/ordenes/{id}/abrir`). */
    async abrirOrden(ordenId: number): Promise<ConversacionChat> {
        await this.verificar();
        const d = await this.transporte.abrirOrden(ordenId);
        await this.guardarDetalle(d);
        return d;
    }

    /** Conversación ya conocida de una orden (sin llamar a la red). */
    conversacionDeOrden(ordenId: number): ConversacionChat | null {
        return [...this.conversaciones.values()].find((c) => c.tipo === "orden" && c.orden_id === ordenId) ?? null;
    }

    /**
     * La pantalla abre una conversación: trae su detalle («visto») y los mensajes que falten (el
     * último tramo, o desde el último `seq` guardado). No lanza: el error queda en su vista.
     */
    async cargarConversacion(id: number): Promise<void> {
        const carga = this.cargaDe(id);
        carga.carga = "cargando";
        carga.error = null;
        this.emitir();
        try {
            await this.verificar();
            const detalle = await this.transporte.verConversacion(id);
            await this.guardarDetalle(detalle);
            const locales = this.mapaMensajes(id);
            const seqs = [...locales.values()].map((m) => m.seq);
            if (this.meta.cargadas.includes(id) && seqs.length > 0) {
                // Hueco desde el último `seq` guardado (§5.5); si es largo, se empieza del final.
                const desde = Math.max(...seqs);
                const pagina = await this.transporte.listarMensajes(id, { direccion: "siguientes", desdeSeq: desde, limit: 100 });
                if (pagina.nextCursor) {
                    await this.cargarUltimoTramo(id, true);
                } else {
                    await this.guardarMensajes(id, pagina.data ?? []);
                    carga.hayAnteriores = carga.cursorAnteriores !== null || Math.min(...seqs) > 1;
                }
            } else {
                await this.cargarUltimoTramo(id, true);
            }
            carga.carga = "lista";
        } catch (e) {
            if (e instanceof ErrorTransporte && e.status === 404) {
                // Ya no es miembro (o nunca lo fue): se olvida y se avisa.
                await this.olvidarConversacion(id);
                this.avisar("error", "Ya no tienes acceso a esa conversación.");
            }
            carga.carga = "error";
            carga.error = errorVisible(e);
            this.alFallarLectura(e, false);
        }
        this.emitir();
    }

    /** Mensajes anteriores (al subir en el hilo). No lanza. */
    async cargarAnteriores(id: number): Promise<void> {
        const carga = this.cargaDe(id);
        if (carga.cargandoAnteriores || !carga.hayAnteriores) {
            return;
        }
        carga.cargandoAnteriores = true;
        this.emitir();
        try {
            await this.verificar();
            const locales = [...this.mapaMensajes(id).values()].map((m) => m.seq);
            const consulta = carga.cursorAnteriores
                ? { direccion: "anteriores" as const, cursor: carga.cursorAnteriores, limit: 30 }
                : { direccion: "anteriores" as const, ...(locales.length > 0 ? { desdeSeq: Math.min(...locales) } : {}), limit: 30 };
            const pagina = await this.transporte.listarMensajes(id, consulta);
            carga.cursorAnteriores = pagina.nextCursor;
            carga.hayAnteriores = pagina.nextCursor !== null;
            await this.guardarMensajes(id, pagina.data ?? [], false);
        } catch (e) {
            carga.error = errorVisible(e);
            this.alFallarLectura(e, false);
        }
        carga.cargandoAnteriores = false;
        this.emitir();
    }

    /**
     * Se mostró el último mensaje: `POST /leer` con el id del último recibido (nunca un pendiente).
     * Solo si hace falta (no leídos o lectura atrasada); sin señal no hace nada.
     */
    async marcarLeido(id: number): Promise<void> {
        const conv = this.conversaciones.get(id);
        if (!conv || this.leyendo.has(id) || !this.enLinea || this.sesionCerrada) {
            return;
        }
        const ultimo = this.ordenados(id).at(-1);
        if (!ultimo || ultimo.id === conv.leido_hasta_mensaje_id) {
            return;
        }
        if (conv.no_leidos === 0 && ultimo.autor_id === this.usuarioId) {
            return;
        }
        this.leyendo.add(id);
        try {
            await this.verificar();
            const r = await this.transporte.leer(id, ultimo.id);
            const actual = this.conversaciones.get(id);
            if (actual) {
                await this.guardar({ conversaciones: { poner: [{ ...actual, no_leidos: r.no_leidos, leido_hasta_mensaje_id: r.leido_hasta_mensaje_id ?? ultimo.id }] } });
            }
        } catch (e) {
            this.alFallarLectura(e, false);
        } finally {
            this.leyendo.delete(id);
            this.emitir();
        }
    }

    // --- Escribir, reintentar, eliminar ------------------------------------------------------

    /**
     * Guarda el mensaje en la cola (id uuid v7 y `creado_en` del teléfono) y despierta el envío.
     * Si la escritura falla, lanza y no queda nada.
     */
    async escribir(conversacionId: number, entrada: EntradaMensaje): Promise<SalidaChat> {
        if (!this.conversaciones.has(conversacionId)) {
            throw new ErrorChatLocal("conversacion_desconocida", "Esa conversación no está en el teléfono.");
        }
        const texto = entrada.texto?.trim() ?? "";
        if (texto.length > TEXTO_MAXIMO) {
            throw new ErrorChatLocal("texto_largo", `El mensaje pasa de ${TEXTO_MAXIMO} caracteres.`);
        }
        let ubicacion: UbicacionChat | undefined;
        if (entrada.ubicacion) {
            const { lat, lon } = entrada.ubicacion;
            if (!Number.isFinite(lat) || !Number.isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) {
                throw new ErrorChatLocal("ubicacion_invalida", "La ubicación no es válida.");
            }
            ubicacion = { lat: Math.round(lat * 1e6) / 1e6, lon: Math.round(lon * 1e6) / 1e6 };
        }
        if (!texto && !ubicacion && !entrada.foto) {
            throw new ErrorChatLocal("mensaje_vacio", "Escribe algo, agrega una foto o comparte tu ubicación.");
        }
        if (entrada.foto && !this.subidor) {
            throw new ErrorChatLocal("sin_subidor", "Este teléfono no puede enviar fotos.");
        }
        const id = this.generarId();
        const s: SalidaChat = {
            id,
            tipo: "enviar",
            conversacionId,
            secuencia: this.meta.secuencia,
            mensajeId: id,
            creadoEn: new Date(this.reloj.ahora()).toISOString(),
            estado: "pendiente",
            enviado: false,
            intentos: 0,
            error: null,
            ...(texto ? { texto } : {}),
            ...(ubicacion ? { ubicacion } : {}),
        };
        const lote: LoteChat = { meta: { secuencia: this.meta.secuencia + 1 } };
        if (entrada.foto) {
            const fotoId = this.generarId();
            s.foto = {
                id: fotoId,
                mime: entrada.foto.mime,
                tamano: entrada.foto.datos.byteLength,
                sha256: await this.sha256(entrada.foto.datos),
                fase: "por_crear",
                offset: 0,
                tomadoEn: entrada.foto.tomadoEn ?? null,
            };
            lote.bytes = { poner: [{ id: fotoId, datos: entrada.foto.datos }] };
        }
        lote.salida = { poner: [s] };
        await this.guardar(lote);
        this.emitir();
        this.despertarSalida();
        return s;
    }

    /** «Reintentar» un mensaje no enviado (con id nuevo si el servidor dijo que el id ya se usó). */
    async reintentar(id: string): Promise<void> {
        const s = this.salida.get(id);
        if (!s || s.estado !== "fallido") {
            return;
        }
        if (s.tipo === "enviar" && !s.enviado && (CODIGOS_ID_NUEVO.has(s.error?.codigo ?? "") || (s.error?.status === 409 && s.foto))) {
            // El id (o la foto) ya está en otro mensaje: sale como uno nuevo, en el mismo lugar.
            const nuevo = this.generarId();
            const lote: LoteChat = {};
            let foto = s.foto;
            if (foto) {
                const bytes = await this.almacen.leerBytesChat(foto.id);
                const fotoId = this.generarId();
                lote.bytes = { borrar: [foto.id], ...(bytes ? { poner: [{ id: fotoId, datos: bytes }] } : {}) };
                foto = { ...foto, id: fotoId, fase: "por_crear", offset: 0 };
            }
            const r: SalidaChat = { ...s, id: nuevo, mensajeId: nuevo, estado: "pendiente", error: null, intentos: 0, ...(foto ? { foto } : {}) };
            lote.salida = { borrar: [s.id], poner: [r] };
            await this.guardar(lote);
        } else {
            await this.guardar({ salida: { poner: [{ ...s, estado: "pendiente", error: null, intentos: 0 }] } });
        }
        this.emitir();
        this.reintentarAhora();
    }

    /** Descarta un mensaje de la cola (no enviado). */
    async descartar(id: string): Promise<void> {
        const s = this.salida.get(id);
        if (!s || s.enviado) {
            return;
        }
        await this.guardar({ salida: { borrar: [id] }, ...(s.foto ? { bytes: { borrar: [s.foto.id] } } : {}) });
        this.emitir();
    }

    /**
     * Elimina un mensaje propio ya enviado, dentro de los 15 min (por `enviado_en`). Se encola igual
     * sin señal; si llega tarde, el servidor responde 422 `plazo_vencido`: se avisa y queda el mensaje.
     */
    async eliminar(mensajeId: string): Promise<void> {
        const m = this.buscarMensaje(mensajeId);
        if (!m || m.autor_id !== this.usuarioId || m.eliminado) {
            throw new ErrorChatLocal("no_eliminable", "Solo puedes eliminar tus mensajes ya enviados.");
        }
        if (this.reloj.ahora() - ms(m.enviado_en) > 15 * MINUTO_MS) {
            throw new ErrorChatLocal("no_eliminable", "Ya pasaron los 15 minutos para eliminarlo.");
        }
        const clave = `eliminar:${mensajeId}`;
        if (this.salida.has(clave)) {
            return;
        }
        const s: SalidaChat = {
            id: clave,
            tipo: "eliminar",
            conversacionId: m.conversacion_id,
            secuencia: this.meta.secuencia,
            mensajeId,
            creadoEn: new Date(this.reloj.ahora()).toISOString(),
            estado: "pendiente",
            enviado: false,
            intentos: 0,
            error: null,
        };
        await this.guardar({ salida: { poner: [s] }, meta: { secuencia: this.meta.secuencia + 1 } });
        this.emitir();
        this.despertarSalida();
    }

    descartarAviso(id: number): void {
        this.avisos = this.avisos.filter((a) => a.id !== id);
        this.emitir();
    }

    // --- Envío de la cola --------------------------------------------------------------------

    /**
     * Una pasada por la cola, en orden. Devuelve los ms que hay que esperar (`Retry-After`) o `null`
     * si no queda nada que se pueda mandar. Lanza `ErrorTransporte` reintentable o de sesión.
     */
    enviarPendientes(): Promise<number | null> {
        const tarea = this.cadenaSalida.then(async () => {
            if (!this.enLinea || this.sesionCerrada) {
                return null;
            }
            for (;;) {
                const espera = this.bloqueoHasta - this.reloj.ahora();
                if (espera > 0) {
                    return espera;
                }
                const s = [...this.salida.values()].filter((x) => x.estado === "pendiente").sort((a, b) => a.secuencia - b.secuencia)[0];
                if (!s) {
                    return null;
                }
                try {
                    await this.verificar();
                    await this.procesar(s);
                } catch (e) {
                    if (e instanceof ErrorTransporte && e.status === 429) {
                        const ms429 = msDeRetryAfter(e.cabeceras?.get("Retry-After"), this.reloj.ahora()) ?? 30_000;
                        this.bloqueoHasta = this.reloj.ahora() + Math.max(1000, ms429);
                        continue;
                    }
                    if (esSesion(e)) {
                        this.sesionCerrada = true;
                        this.pararTiempoReal();
                        this.emitir();
                        throw e;
                    }
                    if (e instanceof ErrorTransporte && e.esReintentable) {
                        const actual = this.salida.get(s.id);
                        if (actual) {
                            await this.guardar({ salida: { poner: [{ ...actual, intentos: actual.intentos + 1, error: errorVisible(e) }] } });
                        }
                        this.emitir();
                        throw e;
                    }
                    await this.rechazar(s, e);
                }
            }
        });
        this.cadenaSalida = tarea.catch(() => undefined);
        return tarea;
    }

    private async procesar(inicial: SalidaChat): Promise<void> {
        let s = inicial;
        const vigente = () => this.salida.has(s.id);
        const actualizar = async (cambios: Partial<SalidaChat>) => {
            if (!vigente()) {
                return false;
            }
            s = { ...s, ...cambios };
            await this.guardar({ salida: { poner: [s] } });
            this.emitir();
            return true;
        };
        if (s.tipo === "eliminar") {
            const m = await this.transporte.eliminarMensaje(s.mensajeId);
            await this.guardar({ salida: { borrar: [s.id] }, ...this.loteMensaje(m) });
            this.emitir();
            return;
        }
        const subidor = this.subidor;
        if (s.foto && s.foto.fase === "por_crear") {
            if (!subidor) {
                throw new ErrorTransporte("http", "Este teléfono no puede subir fotos", 422);
            }
            const creado = await subidor.crear({
                id: s.foto.id,
                entidad: "chat_conversacion",
                entidadId: String(s.conversacionId),
                tipo: "foto",
                mime: s.foto.mime,
                tamano: s.foto.tamano,
                sha256: s.foto.sha256,
                ...(s.foto.tomadoEn ? { tomadoEn: s.foto.tomadoEn } : {}),
            });
            if (creado.estado === "rechazado") {
                throw new ErrorTransporte("http", creado.motivoRechazo ?? "Foto rechazada", 422, {
                    type: "urn:iris:problema:adjunto_rechazado",
                    title: "La foto fue rechazada",
                    status: 422,
                });
            }
            const fase = creado.estado === "completo" ? "subida" : "creada";
            if (!(await actualizar({ foto: { ...s.foto, fase, offset: Math.min(creado.recibido, s.foto.tamano) } }))) {
                return;
            }
        }
        if (!s.enviado) {
            const cuerpo: CuerpoEnviarMensaje = { id: s.mensajeId, creado_en: s.creadoEn };
            if (s.texto) {
                cuerpo.texto = s.texto;
            }
            if (s.ubicacion) {
                cuerpo.ubicacion = s.ubicacion;
            }
            if (s.foto) {
                cuerpo.adjunto_id = s.foto.id;
            }
            const { mensaje } = await this.transporte.enviarMensaje(s.conversacionId, cuerpo);
            if (!s.foto || s.foto.fase === "subida" || mensaje.eliminado) {
                await this.guardar({ salida: { borrar: [s.id] }, ...(s.foto ? { bytes: { borrar: [s.foto.id] } } : {}), ...this.loteMensaje(this.conFotoCompleta(mensaje, s)) });
                this.emitir();
                return;
            }
            await this.guardar(this.loteMensaje(mensaje));
            if (!(await actualizar({ enviado: true, error: null }))) {
                return;
            }
        }
        if (s.foto && s.foto.fase !== "subida" && subidor) {
            await this.subirFoto(subidor, s, actualizar);
            const m = this.buscarMensaje(s.mensajeId);
            await this.guardar({ salida: { borrar: [s.id] }, bytes: { borrar: [s.foto.id] }, ...(m ? this.loteMensaje(this.conFotoCompleta(m, s)) : {}) });
            this.emitir();
        }
    }

    /** Partes tus de la foto desde lo que el servidor confirma. */
    private async subirFoto(subidor: Subidor, s: SalidaChat, actualizar: (c: Partial<SalidaChat>) => Promise<boolean>): Promise<void> {
        let foto = s.foto;
        if (!foto) {
            return;
        }
        if (foto.offset > 0 && foto.offset < foto.tamano) {
            const offset = await subidor.consultarOffset(foto.id);
            foto = { ...foto, offset: Math.min(offset, foto.tamano) };
        }
        const bytes = foto.offset < foto.tamano ? await this.almacen.leerBytesChat(foto.id) : null;
        if (foto.offset < foto.tamano && bytes === null) {
            throw new ErrorTransporte("http", "La foto ya no está en el teléfono", 410);
        }
        for (let vueltas = 0; vueltas < 200; vueltas += 1) {
            const desde = foto.offset;
            const parte = bytes ? bytes.subarray(desde, Math.min(foto.tamano, desde + this.tamanoParte)) : new Uint8Array(0);
            let nuevo: number;
            try {
                nuevo = await subidor.enviarParte(foto.id, desde, parte);
            } catch (e) {
                const offset = e instanceof ErrorTransporte && e.codigo === "offset_distinto" ? Number(e.cabeceras?.get("Upload-Offset")) : Number.NaN;
                if (Number.isInteger(offset) && offset >= 0 && offset <= foto.tamano) {
                    foto = { ...foto, offset };
                    continue;
                }
                throw e;
            }
            foto = { ...foto, offset: Math.min(nuevo, foto.tamano) };
            if (!(await actualizar({ foto }))) {
                return;
            }
            if (nuevo >= foto.tamano) {
                return;
            }
        }
    }

    /** El servidor ya tiene los bytes: la copia en caché se marca completa (no llega otro evento). */
    private conFotoCompleta(m: MensajeChat, s: SalidaChat): MensajeChat {
        return m.adjunto && s.foto && m.adjunto.id === s.foto.id ? { ...m, adjunto: { ...m.adjunto, estado: "completo" } } : m;
    }

    /** 403, 404, 409, 422 (o una falla local): no se reintenta solo. */
    private async rechazar(s: SalidaChat, e: unknown): Promise<void> {
        const actual = this.salida.get(s.id);
        if (!actual) {
            return;
        }
        const error = errorVisible(e);
        if (actual.tipo === "eliminar") {
            await this.guardar({ salida: { borrar: [actual.id] } });
            this.avisar("error", error.codigo === "plazo_vencido" ? "No se pudo eliminar: ya pasaron los 15 minutos." : `No se pudo eliminar el mensaje: ${error.mensaje}`);
        } else if (actual.enviado) {
            // El mensaje ya salió; la foto no se pudo terminar de subir: se deja de intentar.
            await this.guardar({ salida: { borrar: [actual.id] }, ...(actual.foto ? { bytes: { borrar: [actual.foto.id] } } : {}) });
            this.avisar("warning", "Una foto del chat no se pudo subir.");
        } else {
            await this.guardar({ salida: { poner: [{ ...actual, estado: "fallido", error }] } });
        }
        this.emitir();
    }

    private despertarSalida(): void {
        if (!this.salidaEnEspera) {
            this.salidaDespertada = true;
            this.suenoSalida?.abort();
        }
    }

    private async correrSalida(senal: AbortSignal): Promise<void> {
        let intento = 0;
        while (!senal.aborted) {
            this.salidaDespertada = false;
            let espera: number;
            try {
                const resto = await this.enviarPendientes();
                intento = 0;
                espera = resto ?? 10 * MINUTO_MS;
                this.salidaEnEspera = resto !== null;
            } catch (e) {
                if (esSesion(e)) {
                    break;
                }
                espera = calcularEspera(intento, this.espera);
                intento += 1;
                this.salidaEnEspera = true;
            }
            if (senal.aborted) {
                break;
            }
            if (this.salidaDespertada && !this.salidaEnEspera) {
                continue;
            }
            const sueno = new AbortController();
            this.suenoSalida = sueno;
            const cortar = () => sueno.abort();
            senal.addEventListener("abort", cortar, { once: true });
            await this.reloj.esperar(espera, sueno.signal);
            senal.removeEventListener("abort", cortar);
            this.suenoSalida = null;
        }
        if (this.bucleSalida?.signal === senal) {
            this.bucleSalida = null;
        }
    }

    // --- Tiempo real -------------------------------------------------------------------------

    private arrancarTiempoReal(): void {
        if (!this.iniciado || !this.enPrimerPlano || this.bucleTiempo || this.sesionCerrada) {
            return;
        }
        const c = new AbortController();
        this.bucleTiempo = c;
        void this.correrTiempoReal(c.signal);
    }

    private pararTiempoReal(): void {
        this.bucleTiempo?.abort();
        this.bucleTiempo = null;
        this.suenoTiempo?.abort();
        this.fijarTiempoReal("detenido");
    }

    private fijarTiempoReal(modo: ModoTiempoReal): void {
        if (this.tiempoReal !== modo) {
            this.tiempoReal = modo;
            this.emitir();
        }
    }

    private async dormir(msEspera: number, senal: AbortSignal): Promise<void> {
        const sueno = new AbortController();
        this.suenoTiempo = sueno;
        const cortar = () => sueno.abort();
        senal.addEventListener("abort", cortar, { once: true });
        await this.reloj.esperar(msEspera, sueno.signal);
        senal.removeEventListener("abort", cortar);
        if (this.suenoTiempo === sueno) {
            this.suenoTiempo = null;
        }
    }

    private async correrTiempoReal(senal: AbortSignal): Promise<void> {
        let fallos = 0;
        let fallosFlujo = 0;
        let flujoBloqueadoHasta = 0;
        while (!senal.aborted && !this.sesionCerrada) {
            if (!this.enLinea) {
                this.fijarTiempoReal("sin_conexion");
                await this.dormir(10 * MINUTO_MS, senal);
                continue;
            }
            try {
                if (this.meta.cursor === null) {
                    this.fijarTiempoReal("conectando");
                    await this.verificar();
                    await this.arrancarFeed();
                }
                if (this.abrirFlujo && this.reloj.ahora() >= flujoBloqueadoHasta) {
                    this.fijarTiempoReal("conectando");
                    await this.verificar(true);
                    if (senal.aborted) {
                        break;
                    }
                    const r = await this.escuchar(this.abrirFlujo, senal);
                    if (senal.aborted) {
                        break;
                    }
                    // El flujo terminó: un sondeo pone al día y descubre lo que EventSource no dice
                    // (410 `cursor_vencido`, 401/403). Fallas seguidas al abrir (429, 503) → sondeo
                    // con espera antes de volver a probar el flujo.
                    fallosFlujo = r.duroMs >= 20_000 || r.eventos > 0 ? 0 : fallosFlujo + 1;
                    if (fallosFlujo >= 3) {
                        flujoBloqueadoHasta = this.reloj.ahora() + calcularEspera(fallosFlujo - 3, { ...this.espera, baseMs: MINUTO_MS, maximoMs: 10 * MINUTO_MS });
                    }
                    await this.sondearHastaAlDia();
                    fallos = 0;
                    await this.dormir(this.reconexionMs, senal);
                } else {
                    this.fijarTiempoReal("sondeo");
                    await this.verificar();
                    await this.sondearHastaAlDia();
                    fallos = 0;
                    await this.dormir(this.intervaloSondeoMs, senal);
                }
            } catch (e) {
                if (senal.aborted) {
                    break;
                }
                if (esSesion(e) || (e instanceof ErrorTransporte && e.status === 403 && !e.esDeEmpresa)) {
                    // 401/403: no se reintenta; hay que volver a entrar (o falta `chat.ver`).
                    this.sesionCerrada = esSesion(e) || this.sesionCerrada;
                    this.alFallarLectura(e, false);
                    this.bucleTiempo = null;
                    this.fijarTiempoReal("detenido");
                    return;
                }
                const retry = esEspera(e) && e instanceof ErrorTransporte ? msDeRetryAfter(e.cabeceras?.get("Retry-After"), this.reloj.ahora()) : null;
                const espera = retry ?? calcularEspera(fallos, this.espera);
                fallos += 1;
                if (e instanceof ErrorTransporte && e.status === 503) {
                    // `flujos_agotados`: el proceso no da más flujos; se sondea un rato.
                    flujoBloqueadoHasta = this.reloj.ahora() + Math.max(espera, 5 * MINUTO_MS);
                }
                this.fijarTiempoReal(this.abrirFlujo ? "conectando" : "sondeo");
                await this.dormir(Math.max(1000, espera), senal);
            }
        }
    }

    /** Escucha un flujo hasta que se cierre (o se aborte). */
    private escuchar(abrir: AbrirFlujoChat, senal: AbortSignal): Promise<{ duroMs: number; eventos: number }> {
        return new Promise((resolver) => {
            const inicio = this.reloj.ahora();
            let abierto: number | null = null;
            let eventos = 0;
            let flujo: FlujoChat | null = null;
            let terminado = false;
            const terminar = () => {
                if (terminado) {
                    return;
                }
                terminado = true;
                senal.removeEventListener("abort", terminar);
                flujo?.cerrar();
                resolver({ duroMs: abierto === null ? 0 : this.reloj.ahora() - Math.max(inicio, abierto), eventos });
            };
            senal.addEventListener("abort", terminar, { once: true });
            flujo = abrir(this.meta.cursor, {
                alAbrir: () => {
                    abierto = this.reloj.ahora();
                    this.fijarTiempoReal("sse");
                },
                alEvento: (e) => {
                    eventos += 1;
                    void this.aplicarEventos([e]).catch(() => undefined);
                },
                alCerrar: terminar,
            });
            if (terminado) {
                flujo.cerrar();
            }
        });
    }

    /** `GET /cambios` desde el cursor mientras `hayMas`. 410 → resincroniza. */
    private async sondearHastaAlDia(): Promise<void> {
        for (let vueltas = 0; vueltas < 20; vueltas += 1) {
            let r;
            try {
                r = await this.transporte.cambios(this.meta.cursor);
            } catch (e) {
                if (e instanceof ErrorTransporte && e.status === 410) {
                    await this.resincronizar();
                    return;
                }
                throw e;
            }
            await this.aplicarEventos(r.datos.data ?? [], r.datos.nextCursor);
            if (!r.datos.hayMas) {
                return;
            }
        }
    }

    /** §5.1: cabeza del feed, la lista y los hilos del día (abrir es idempotente). */
    private async arrancarFeed(): Promise<void> {
        const r = await this.transporte.cambios(null);
        await this.guardar({ meta: { cursor: r.datos.nextCursor } });
        await this.recargarLista();
        await this.abrirDelDia().catch(() => undefined);
    }

    private async abrirDelDia(): Promise<void> {
        const tareas: [string, () => Promise<ConversacionChatDetalle>][] = [
            ...[...this.delDia.cuadrillas].map((id): [string, () => Promise<ConversacionChatDetalle>] => [`c${id}`, () => this.transporte.abrirCuadrilla(id)]),
            ...[...this.delDia.ordenes].map((id): [string, () => Promise<ConversacionChatDetalle>] => [`o${id}`, () => this.transporte.abrirOrden(id)]),
        ];
        for (const [clave, abrir] of tareas) {
            if (this.abiertasDelDia.has(clave)) {
                continue;
            }
            try {
                await this.guardarDetalle(await abrir());
                this.abiertasDelDia.add(clave);
            } catch (e) {
                if (e instanceof ErrorTransporte && (e.status === 404 || e.status === 403)) {
                    // No es de él (o no tiene permiso): no se insiste en esta sesión.
                    this.abiertasDelDia.add(clave);
                } else {
                    throw e;
                }
            }
        }
        this.emitir();
    }

    /** 410 `cursor_vencido`: borra la caché (no la cola), recarga y sigue sin cursor. */
    private async resincronizar(): Promise<void> {
        await this.encadenarEscritura(() => this.almacen.borrarCacheChat());
        this.conversaciones.clear();
        this.mensajes.clear();
        this.lecturas.clear();
        this.cargas.clear();
        this.abiertasDelDia.clear();
        this.meta = { ...this.meta, cursor: null, cargadas: [] };
        this.emitir();
        await this.arrancarFeed();
    }

    private async recargarLista(): Promise<void> {
        if (this.recargandoLista) {
            return this.recargandoLista;
        }
        const tarea = (async () => {
            this.lista = { estado: "cargando", error: null };
            this.emitir();
            try {
                const todas: ConversacionChat[] = [];
                let cursor: string | undefined;
                for (let pagina = 0; pagina < 10; pagina += 1) {
                    const p = await this.transporte.listarConversaciones(cursor);
                    todas.push(...(p.data ?? []));
                    if (!p.nextCursor) {
                        break;
                    }
                    cursor = p.nextCursor;
                }
                const ids = new Set(todas.map((c) => c.id));
                const fuera = [...this.conversaciones.keys()].filter((id) => !ids.has(id));
                const mensajesFuera = fuera.flatMap((id) => [...this.mapaMensajes(id).keys()]);
                await this.guardar({
                    conversaciones: { poner: todas, borrar: fuera },
                    ...(mensajesFuera.length > 0 ? { mensajes: { borrar: mensajesFuera } } : {}),
                    ...(fuera.length > 0 ? { meta: { cargadas: this.meta.cargadas.filter((id) => !fuera.includes(id)) } } : {}),
                });
                for (const id of fuera) {
                    this.mensajes.delete(id);
                    this.lecturas.delete(id);
                }
                this.lista = { estado: "lista", error: null };
            } catch (e) {
                this.lista = { estado: "error", error: errorVisible(e) };
                throw e;
            } finally {
                this.emitir();
            }
        })();
        this.recargandoLista = tarea;
        try {
            await tarea;
        } finally {
            this.recargandoLista = null;
        }
    }

    /** Aplica eventos del feed en orden (por id: repetir no cambia nada) y guarda el cursor. */
    aplicarEventos(eventos: readonly EventoChat[], cursorFinal?: string): Promise<void> {
        const tarea = this.cadenaEventos.then(async () => {
            for (const e of eventos) {
                await this.aplicarUno(e);
            }
            if (cursorFinal !== undefined && cursorFinal !== this.meta.cursor) {
                await this.guardar({ meta: { cursor: cursorFinal } });
            }
            this.emitir();
        });
        this.cadenaEventos = tarea.catch(() => undefined);
        return tarea;
    }

    private async aplicarUno(e: EventoChat): Promise<void> {
        const lote: LoteChat = { meta: { cursor: e.id } };
        const conv = this.conversaciones.get(e.conversacion_id);
        switch (e.tipo) {
            case "mensaje_nuevo":
            case "mensaje_eliminado": {
                const m = e.mensaje;
                if (!m) {
                    break;
                }
                if (this.meta.cargadas.includes(m.conversacion_id)) {
                    lote.mensajes = { poner: [m] };
                }
                if (conv) {
                    lote.conversaciones = { poner: [this.conPrevia({ ...conv, no_leidos: e.no_leidos }, m)] };
                } else {
                    this.pedirDetalle(e.conversacion_id);
                }
                break;
            }
            case "leido": {
                const l = e.lectura;
                if (!l) {
                    break;
                }
                if (l.usuario_id === this.usuarioId) {
                    if (conv) {
                        lote.conversaciones = { poner: [{ ...conv, no_leidos: e.no_leidos, leido_hasta_mensaje_id: l.hasta_mensaje_id }] };
                    }
                } else {
                    const mapa = this.lecturas.get(e.conversacion_id) ?? new Map<number, LecturaMiembroChat>();
                    const previa = mapa.get(l.usuario_id);
                    const seq = this.mapaMensajes(e.conversacion_id).get(l.hasta_mensaje_id)?.seq ?? previa?.hasta_seq ?? 0;
                    if (!previa || seq >= previa.hasta_seq) {
                        mapa.set(l.usuario_id, { usuario_id: l.usuario_id, hasta_mensaje_id: l.hasta_mensaje_id, hasta_seq: seq, leido_en: l.leido_en, nombre: previa?.nombre ?? "" });
                        this.lecturas.set(e.conversacion_id, mapa);
                    }
                }
                break;
            }
            case "conversacion_actualizada":
                if (e.conversacion) {
                    lote.conversaciones = { poner: [{ ...e.conversacion, no_leidos: e.no_leidos }] };
                }
                break;
        }
        await this.guardar(lote);
    }

    /** Conversación nueva por un evento: se trae su detalle (una vez a la vez). */
    private pedirDetalle(id: number): void {
        if (this.pidiendoDetalle.has(id)) {
            return;
        }
        this.pidiendoDetalle.add(id);
        void this.transporte
            .verConversacion(id)
            .then((d) => this.guardarDetalle(d))
            .catch(() => undefined)
            .finally(() => {
                this.pidiendoDetalle.delete(id);
                this.emitir();
            });
    }

    // --- Internos de caché -------------------------------------------------------------------

    private conPrevia(conv: ConversacionChat, m: MensajeChat): ConversacionChat {
        const previa = conv.ultimo_mensaje;
        if (previa && previa.id !== m.id && ms(previa.enviado_en) > ms(m.enviado_en)) {
            return conv;
        }
        return { ...conv, ultimo_mensaje: vistaPreviaDe(m), ultima_actividad: ms(m.enviado_en) > ms(conv.ultima_actividad) ? m.enviado_en : conv.ultima_actividad };
    }

    /** Lote que guarda un mensaje del servidor (y su vista previa en la lista). */
    private loteMensaje(m: MensajeChat): LoteChat {
        const lote: LoteChat = {};
        if (this.meta.cargadas.includes(m.conversacion_id)) {
            lote.mensajes = { poner: [m] };
        }
        const conv = this.conversaciones.get(m.conversacion_id);
        if (conv) {
            lote.conversaciones = { poner: [this.conPrevia(conv, m)] };
        }
        return lote;
    }

    private async guardarDetalle(d: ConversacionChatDetalle): Promise<void> {
        const { lecturas, ...conv } = d;
        const mapa = new Map<number, LecturaMiembroChat>();
        for (const l of lecturas ?? []) {
            mapa.set(l.usuario_id, l);
        }
        this.lecturas.set(d.id, mapa);
        await this.guardar({ conversaciones: { poner: [conv] } });
        this.emitir();
    }

    private async cargarUltimoTramo(id: number, reemplazar: boolean): Promise<void> {
        const pagina = await this.transporte.listarMensajes(id, { direccion: "anteriores", limit: 30 });
        const carga = this.cargaDe(id);
        carga.cursorAnteriores = pagina.nextCursor;
        carga.hayAnteriores = pagina.nextCursor !== null;
        const borrar = reemplazar ? [...this.mapaMensajes(id).keys()].filter((k) => !(pagina.data ?? []).some((m) => m.id === k)) : [];
        await this.guardar({
            mensajes: { poner: pagina.data ?? [], borrar },
            meta: { cargadas: this.meta.cargadas.includes(id) ? this.meta.cargadas : [...this.meta.cargadas, id] },
        });
        if (reemplazar) {
            for (const k of borrar) {
                this.mapaMensajes(id).delete(k);
            }
        }
    }

    /** Guarda mensajes de una conversación y recorta a los más recientes. */
    private async guardarMensajes(id: number, nuevos: readonly MensajeChat[], recortar = true): Promise<void> {
        const mapa = new Map(this.mapaMensajes(id));
        for (const m of nuevos) {
            mapa.set(m.id, m);
        }
        const orden = [...mapa.values()].sort((a, b) => a.seq - b.seq);
        const sobran = recortar ? orden.slice(0, Math.max(0, orden.length - this.maximoPorConversacion)).map((m) => m.id) : [];
        if (sobran.length > 0) {
            this.cargaDe(id).hayAnteriores = true;
            this.cargaDe(id).cursorAnteriores = null;
        }
        await this.guardar({ mensajes: { poner: [...nuevos], borrar: sobran } });
    }

    private async olvidarConversacion(id: number): Promise<void> {
        const mensajes = [...this.mapaMensajes(id).keys()];
        await this.guardar({ conversaciones: { borrar: [id] }, mensajes: { borrar: mensajes }, meta: { cargadas: this.meta.cargadas.filter((x) => x !== id) } });
        this.mensajes.delete(id);
        this.lecturas.delete(id);
    }

    private buscarMensaje(id: string): MensajeChat | null {
        for (const mapa of this.mensajes.values()) {
            const m = mapa.get(id);
            if (m) {
                return m;
            }
        }
        return null;
    }

    private mapaMensajes(id: number): Map<string, MensajeChat> {
        let mapa = this.mensajes.get(id);
        if (!mapa) {
            mapa = new Map();
            this.mensajes.set(id, mapa);
        }
        return mapa;
    }

    private cargaDe(id: number): CargaConversacion {
        let c = this.cargas.get(id);
        if (!c) {
            c = { carga: "inactiva", error: null, cursorAnteriores: null, hayAnteriores: false, cargandoAnteriores: false };
            this.cargas.set(id, c);
        }
        return c;
    }

    private ordenados(id: number): MensajeChat[] {
        return [...this.mapaMensajes(id).values()].sort((a, b) => a.seq - b.seq);
    }

    private async verificar(forzar = false): Promise<void> {
        if (!this.verificarIdentidad) {
            return;
        }
        const ahora = this.reloj.ahora();
        if (!forzar && ahora - this.ultimaVerificacion < this.verificarCadaMs) {
            return;
        }
        await this.verificarIdentidad();
        this.ultimaVerificacion = this.reloj.ahora();
    }

    /** Una lectura falló: la sesión se marca cerrada con 401 (nunca se muestra el contenido). */
    private alFallarLectura(e: unknown, conLista = true): void {
        if (esSesion(e)) {
            this.sesionCerrada = true;
            this.pararTiempoReal();
        }
        if (conLista && this.lista.estado !== "error") {
            this.lista = { estado: "error", error: errorVisible(e) };
        }
        this.emitir();
    }

    private avisar(tono: AvisoChat["tono"], texto: string): void {
        this.avisos = [...this.avisos, { id: this.siguienteAviso, tono, texto }].slice(-5);
        this.siguienteAviso += 1;
    }

    private encadenarEscritura(fn: () => Promise<void>): Promise<void> {
        const tarea = this.escrituras.then(fn);
        this.escrituras = tarea.catch(() => undefined);
        return tarea;
    }

    /** Persiste primero y, solo si se guardó, aplica a la memoria. */
    private async guardar(lote: LoteChat): Promise<void> {
        await this.encadenarEscritura(() => this.almacen.escribirChat(lote));
        for (const id of lote.salida?.borrar ?? []) {
            this.salida.delete(id);
        }
        for (const s of lote.salida?.poner ?? []) {
            this.salida.set(s.id, s);
        }
        for (const id of lote.conversaciones?.borrar ?? []) {
            this.conversaciones.delete(id);
        }
        for (const c of lote.conversaciones?.poner ?? []) {
            this.conversaciones.set(c.id, c);
        }
        const borrarMensajes = new Set(lote.mensajes?.borrar ?? []);
        if (borrarMensajes.size > 0) {
            for (const mapa of this.mensajes.values()) {
                for (const id of borrarMensajes) {
                    mapa.delete(id);
                }
            }
        }
        for (const m of lote.mensajes?.poner ?? []) {
            this.mapaMensajes(m.conversacion_id).set(m.id, m);
        }
        if (lote.meta) {
            this.meta = { ...this.meta, ...lote.meta };
        }
        this.version += 1;
    }

    // --- Vistas ------------------------------------------------------------------------------

    private emitir(): void {
        this.version += 1;
        this.instantanea = this.calcularEstado();
        for (const oyente of this.oyentes) {
            oyente(this.instantanea);
        }
    }

    private conversacionVista(c: ConversacionChat): ConversacionVista {
        const suyas = [...this.salida.values()].filter((s) => s.conversacionId === c.id && s.tipo === "enviar" && !s.enviado);
        return { ...c, pendientes: suyas.length, fallidos: suyas.filter((s) => s.estado === "fallido").length };
    }

    private calcularEstado(): EstadoChat {
        const ultimaLocal = new Map<number, number>();
        for (const s of this.salida.values()) {
            if (s.tipo === "enviar" && !s.enviado) {
                ultimaLocal.set(s.conversacionId, Math.max(ultimaLocal.get(s.conversacionId) ?? 0, ms(s.creadoEn)));
            }
        }
        const conversaciones = [...this.conversaciones.values()]
            .map((c) => this.conversacionVista(c))
            .sort((a, b) => Math.max(ms(b.ultima_actividad), ultimaLocal.get(b.id) ?? 0) - Math.max(ms(a.ultima_actividad), ultimaLocal.get(a.id) ?? 0) || b.id - a.id);
        const salida = [...this.salida.values()];
        return {
            listo: this.listo,
            conversaciones,
            noLeidos: conversaciones.reduce((n, c) => n + Math.max(0, c.no_leidos), 0),
            lista: { ...this.lista },
            tiempoReal: this.tiempoReal,
            enLinea: this.enLinea,
            sesionCerrada: this.sesionCerrada,
            pendientes: salida.filter((s) => !s.enviado).length,
            fallidos: salida.filter((s) => s.estado === "fallido").length,
            avisos: [...this.avisos],
        };
    }

    private calcularVista(id: number): VistaConversacion {
        const conv = this.conversaciones.get(id);
        const carga = this.cargaDe(id);
        const lecturas = [...(this.lecturas.get(id)?.values() ?? [])];
        const otros = lecturas.filter((l) => l.usuario_id !== this.usuarioId);
        const eliminando = new Set([...this.salida.values()].filter((s) => s.tipo === "eliminar" && s.conversacionId === id).map((s) => s.mensajeId));
        const fotoPendiente = new Map([...this.salida.values()].filter((s) => s.tipo === "enviar" && s.foto && s.foto.fase !== "subida").map((s) => [s.mensajeId, s]));
        const servidor = this.ordenados(id);
        const enServidor = new Set(servidor.map((m) => m.id));
        const mensajes: MensajeVista[] = servidor.map((m) => {
            const propio = m.autor_id === this.usuarioId;
            const local = fotoPendiente.get(m.id);
            let foto: FotoVista | null = null;
            if (m.adjunto && !m.eliminado) {
                foto = local && m.adjunto.estado !== "completo" ? { id: m.adjunto.id, estado: "local", url: null } : { id: m.adjunto.id, estado: m.adjunto.estado, url: m.adjunto.estado === "completo" ? m.adjunto.url_descarga : null };
            }
            return {
                id: m.id,
                seq: m.seq,
                autorId: m.autor_id,
                autorNombre: m.autor_nombre,
                propio,
                texto: m.eliminado ? null : m.texto,
                ubicacion: m.eliminado ? null : (m.ubicacion ?? null),
                foto,
                creadoEn: m.creado_en,
                enviadoEn: m.enviado_en,
                eliminado: m.eliminado || eliminando.has(m.id),
                envio: eliminando.has(m.id) && !m.eliminado ? "eliminando" : "enviado",
                error: null,
                visto: propio && otros.some((l) => l.hasta_seq >= m.seq),
            };
        });
        const enCola = [...this.salida.values()].filter((s) => s.conversacionId === id && s.tipo === "enviar" && !s.enviado && !enServidor.has(s.mensajeId)).sort((a, b) => a.secuencia - b.secuencia);
        for (const s of enCola) {
            mensajes.push({
                id: s.mensajeId,
                seq: null,
                autorId: this.usuarioId,
                autorNombre: "",
                propio: true,
                texto: s.texto ?? null,
                ubicacion: s.ubicacion ?? null,
                foto: s.foto ? { id: s.foto.id, estado: "local", url: null } : null,
                creadoEn: s.creadoEn,
                enviadoEn: null,
                eliminado: false,
                envio: s.estado === "fallido" ? "fallido" : this.enLinea && !this.sesionCerrada ? "enviando" : "sin_senal",
                error: s.error,
                visto: false,
            });
        }
        return {
            id,
            conversacion: conv ? this.conversacionVista(conv) : null,
            mensajes,
            lecturas,
            carga: carga.carga,
            error: carga.error,
            hayAnteriores: carga.hayAnteriores,
            cargandoAnteriores: carga.cargandoAnteriores,
        };
    }
}
