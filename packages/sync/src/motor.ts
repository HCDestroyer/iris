// Motor de sincronización de la app de campo (`contratos/sync.md`). Núcleo sin DOM: persiste en
// un `Almacen`, habla con la API por un `Transporte` y un `Subidor`, y mide el tiempo con un
// `Reloj`, todos inyectados. Pensado para React (`useSyncExternalStore(motor.suscribir,
// motor.obtenerEstado)`) sin depender de React.
import type { Almacen, LoteEscritura } from "./almacen";
import { aplicarOperacion, bodegaDeOrden, descontarExistencias, ESTADOS_TERMINALES, TIPOS_EVIDENCIA } from "./aplicar";
import type {
    DatosPorTipo,
    Existencias,
    OperacionCable,
    OrdenCampo,
    Problema,
    ResultadoOperacion,
    RutaCampo,
    TipoAdjunto,
    TipoOperacion,
} from "./contrato";
import { codigoProblema, ErrorSync, ErrorTransporte } from "./errores";
import type { AdjuntoLocal, MetaSync, OperacionLocal, OrdenLocal } from "./modelo";
import { calcularEspera, ESPERA_POR_DEFECTO, relojSistema, type OpcionesEspera, type Reloj } from "./reloj";
import type { RespuestaApi, Subidor, Transporte } from "./transporte";
import { crearGeneradorUuid7 } from "./uuid7";

export const DIA_MS = 24 * 60 * 60 * 1000;

/** Rechazos que dicen algo de la orden entera: no bloquean la cola, la API juzga cada operación. */
export const CODIGOS_TERMINALES: ReadonlySet<string> = new Set(["orden_cancelada", "orden_reasignada"]);

/** Rechazo que solo repite uno anterior de la misma orden en el lote: vuelve a la cola, detrás. */
export const CODIGO_BLOQUEADA = "bloqueada_por_rechazo";

/** Tope de la API para `POST /sync/lote` (4 MiB) con margen para el sobre. */
const BYTES_LOTE = 3.5 * 1024 * 1024;

export interface OpcionesMotor {
    almacen: Almacen;
    transporte: Transporte;
    /** Sin subidor los adjuntos quedan en cola y `orden.cerrar` espera. */
    subidor?: Subidor;
    reloj?: Reloj;
    /** Generador de UUID v7 (por defecto, el propio con el reloj del motor). */
    generarId?: () => string;
    espera?: Partial<OpcionesEspera>;
    /** Operaciones por lote (100; la API acepta hasta 200). */
    tamanoLote?: number;
    /** Bytes de JSON por lote (3,5 MiB; la API acepta hasta 4 MiB). */
    bytesLote?: number;
    /** `plataforma` del lote (`pwa`, `android`, `ios`…). */
    plataforma?: string;
    /** Bytes por PATCH de tus (256 KiB). */
    tamanoParte?: number;
    /** Caducidad de los datos sin sincronizar (7 días). */
    caducidadMs?: number;
    /** Cada cuánto el bucle pide cambios con la app abierta (60 s). */
    intervaloMs?: number;
    sha256?: (datos: Uint8Array) => Promise<string>;
}

/**
 * - `sin_sesion`: 401/403; hay que volver a entrar.
 * - `sin_empresa`: la empresa activa ya no vale (403 `empresa_no_permitida`/`sin_empresa`, 422
 *   `empresa_requerida`); hay que elegir otra.
 */
export type Conexion = "desconocida" | "en_linea" | "sin_conexion" | "sin_sesion" | "sin_empresa";

export type Aviso = "material_excede_existencia";

export interface ErrorVisible {
    mensaje: string;
    status: number | null;
    problema: Problema | null;
}

/** Orden tal como la ve el técnico: lo último del servidor más su cola aplicada. */
export interface OrdenVista extends OrdenCampo {
    reasignada: boolean;
    /** Operaciones suyas por enviar. */
    pendientes: number;
    rechazos: number;
    /** Un rechazo o una espera de adjuntos retiene la cola de esta orden. */
    bloqueada: boolean;
    esperandoAdjuntos: boolean;
    subidasPendientes: number;
}

export interface ResumenSubidas {
    /** Por crear, subiendo o rechazadas (todo lo que no llegó). */
    pendientes: number;
    bytesPendientes: number;
    conError: number;
    /** Rechazadas por la API: esperan «Descartar» o «Rehacer». */
    rechazadas: number;
}

export interface EstadoSync {
    conexion: Conexion;
    sincronizando: boolean;
    /** Operaciones por enviar (incluye las retenidas detrás de un rechazo). */
    pendientes: number;
    /** Las mismas, en el orden de la cola (para mostrarlas). */
    cola: readonly OperacionLocal[];
    /** Rechazadas: el técnico decide `descartar` o `rehacer`. */
    rechazos: readonly OperacionLocal[];
    /** `orden.cerrar` que reintentará sola cuando terminen sus subidas. */
    esperandoAdjuntos: readonly OperacionLocal[];
    subidas: ResumenSubidas;
    adjuntos: readonly AdjuntoLocal[];
    ordenes: readonly OrdenVista[];
    /** Existencias de las bodegas del técnico menos lo usado en la cola (`null` sin paquete). */
    existencias: Existencias | null;
    ruta: RutaCampo | null;
    fecha: string | null;
    ultimaSincronizacion: number | null;
    /** Pasaron 7 días sin sincronizar: se borró lo del servidor y hay que bajar el paquete. */
    caducado: boolean;
    /** Hora del próximo reintento tras una falla (espera exponencial). */
    proximoIntento: number | null;
    ultimoError: ErrorVisible | null;
    desfaseMs: number;
}

export interface ResultadoRegistro {
    operacion: OperacionLocal;
    avisos: Aviso[];
}

export interface EntradaAdjunto {
    ordenId: number;
    tipo: TipoAdjunto;
    datos: Uint8Array;
    mime: string;
    /** Cuenta para el cierre (foto de un ítem obligatorio, firma si la plantilla la pide). */
    obligatorio?: boolean;
    /** Ítem foto del checklist: `itemId` para `adjunto.registrar`, `codigo` para el alta (`claveChecklist`). */
    item?: { itemId: number; codigo: string };
    lat?: number;
    lon?: number;
    /** Hora de captura sin corregir (EXIF o reloj del teléfono). */
    tomadoEn?: string;
    descripcion?: string;
}

type Oyente = (estado: EstadoSync) => void;

async function sha256Sistema(datos: Uint8Array): Promise<string> {
    const resumen = await globalThis.crypto.subtle.digest("SHA-256", datos as Uint8Array<ArrayBuffer>);
    return Array.from(new Uint8Array(resumen), (x) => x.toString(16).padStart(2, "0")).join("");
}

/** Adjunto local al que apunta una operación (alta pendiente o rechazada la retienen). */
function adjuntoDeOp(op: OperacionLocal): string | null {
    switch (op.tipo) {
        case "adjunto.registrar":
            return op.datos.adjuntoId;
        case "firma.capturar":
            return op.datos.adjuntoId ?? null;
        case "checklist.marcar":
            return typeof op.datos.valor === "string" ? op.datos.valor : null;
        default:
            return null;
    }
}

/** La misma operación apuntando a otro adjunto (al rehacer un adjunto rechazado). */
function cambiarAdjunto(op: OperacionLocal, de: string, a: string): OperacionLocal {
    switch (op.tipo) {
        case "adjunto.registrar":
            return op.datos.adjuntoId === de ? { ...op, datos: { ...op.datos, adjuntoId: a } } : op;
        case "firma.capturar":
            return op.datos.adjuntoId === de ? { ...op, datos: { ...op.datos, adjuntoId: a } } : op;
        case "checklist.marcar":
            return op.datos.valor === de ? { ...op, datos: { ...op.datos, valor: a } } : op;
        default:
            return op;
    }
}

function aCable(op: OperacionLocal): OperacionCable {
    return {
        id: op.id,
        tipo: op.tipo,
        ordenId: op.ordenId,
        versionBase: op.versionBase,
        ocurridoEn: op.ocurridoEn,
        datos: op.datos,
    } as OperacionCable;
}

/**
 * Lo que pide `faltan_adjuntos`: `errors[].field` sin el prefijo `adjuntos.` (claves de I1:
 * `firma`, `foto:<código del ítem>`; también se acepta el id del adjunto).
 */
export function faltantesDe(problema: Problema | null): string[] {
    return (problema?.errors ?? []).map((e) => e.field.replace(/^adjuntos\./, ""));
}

/** ¿Este adjunto local cubre la clave faltante `firma`, `foto:<código>` o su id? */
export function cubreFaltante(adjunto: Pick<AdjuntoLocal, "id" | "tipo" | "claveChecklist">, faltante: string): boolean {
    if (faltante === adjunto.id) {
        return true;
    }
    const [tipo, clave] = faltante.split(":", 2);
    if (tipo !== adjunto.tipo) {
        return false;
    }
    return clave === undefined || clave === adjunto.claveChecklist;
}

export class MotorSync {
    private readonly almacen: Almacen;
    private readonly transporte: Transporte;
    private readonly subidor: Subidor | null;
    private readonly reloj: Reloj;
    private readonly generarId: () => string;
    private readonly espera: OpcionesEspera;
    private readonly tamanoLote: number;
    private readonly bytesLote: number;
    private readonly plataforma: string;
    private readonly tamanoParte: number;
    private readonly caducidadMs: number;
    private readonly intervaloMs: number;
    private readonly sha256: (datos: Uint8Array) => Promise<string>;

    private readonly ops = new Map<string, OperacionLocal>();
    private readonly ordenes = new Map<number, OrdenLocal>();
    private readonly adjuntos = new Map<string, AdjuntoLocal>();
    private meta: MetaSync;
    private readonly enVuelo = new Set<string>();
    /** Desfase y último contacto medidos en memoria, a guardar con la próxima escritura. */
    private metaSucia = false;

    private conexion: Conexion = "desconocida";
    private sincronizando = 0;
    private proximoIntento: number | null = null;
    private ultimoError: ErrorVisible | null = null;
    private cadena: Promise<unknown> = Promise.resolve();
    private readonly oyentes = new Set<Oyente>();
    private instantanea: EstadoSync;

    private bucle: AbortController | null = null;
    private sueno: AbortController | null = null;
    private enEspera = false;

    private constructor(opciones: OpcionesMotor, meta: MetaSync) {
        this.almacen = opciones.almacen;
        this.transporte = opciones.transporte;
        this.subidor = opciones.subidor ?? null;
        this.reloj = opciones.reloj ?? relojSistema;
        const reloj = this.reloj;
        this.generarId = opciones.generarId ?? crearGeneradorUuid7(() => reloj.ahora());
        this.espera = { ...ESPERA_POR_DEFECTO, ...opciones.espera };
        this.tamanoLote = opciones.tamanoLote ?? 100;
        this.bytesLote = opciones.bytesLote ?? BYTES_LOTE;
        this.plataforma = opciones.plataforma ?? "pwa";
        this.tamanoParte = opciones.tamanoParte ?? 256 * 1024;
        this.caducidadMs = opciones.caducidadMs ?? 7 * DIA_MS;
        this.intervaloMs = opciones.intervaloMs ?? 60_000;
        this.sha256 = opciones.sha256 ?? sha256Sistema;
        this.meta = meta;
        this.instantanea = this.calcularEstado();
    }

    /** Carga lo guardado en el teléfono y aplica la caducidad. No llama a la red. */
    static async abrir(opciones: OpcionesMotor): Promise<MotorSync> {
        const contenido = await opciones.almacen.cargar();
        const motor = new MotorSync(opciones, contenido.meta);
        for (const op of contenido.operaciones) {
            motor.ops.set(op.id, op);
        }
        for (const orden of contenido.ordenes) {
            motor.ordenes.set(orden.id, orden);
        }
        for (const adjunto of contenido.adjuntos) {
            motor.adjuntos.set(adjunto.id, adjunto);
        }
        if (motor.meta.dispositivoId === null) {
            await motor.guardar({ meta: { dispositivoId: motor.generarId() } });
        }
        await motor.verificarCaducidad();
        motor.emitir();
        return motor;
    }

    // --- Suscripción ------------------------------------------------------------------------

    /** Devuelve la función para desuscribirse (firma de `useSyncExternalStore`). */
    suscribir = (oyente: Oyente): (() => void) => {
        this.oyentes.add(oyente);
        return () => {
            this.oyentes.delete(oyente);
        };
    };

    /** Instantánea inmutable: cambia de identidad solo cuando algo cambió. */
    obtenerEstado = (): EstadoSync => this.instantanea;

    obtenerOrden(id: number): OrdenVista | null {
        return this.instantanea.ordenes.find((o) => o.id === id) ?? null;
    }

    get dispositivoId(): string {
        return this.meta.dispositivoId ?? "";
    }

    // --- Acciones del técnico ---------------------------------------------------------------

    /**
     * Encola una acción: primero la guarda en el almacén (si falla, lanza y no queda nada),
     * luego la aplica a la vista y despierta el envío.
     */
    async registrar<T extends TipoOperacion>(tipo: T, ordenId: number, datos: DatosPorTipo[T]): Promise<ResultadoRegistro> {
        const vista = this.validarAccion(tipo, ordenId);
        const avisos: Aviso[] = [];
        if (tipo === "material.usar") {
            const { productoId, cantidad } = datos as DatosPorTipo["material.usar"];
            const bodega = bodegaDeOrden(this.instantanea.existencias, vista.bodegaId);
            const disponible = bodega?.lineas?.find((e) => e.productoId === productoId)?.cantidad ?? 0;
            if (cantidad > disponible) {
                avisos.push("material_excede_existencia");
            }
        }
        const secuencia = this.meta.secuencia + 1;
        const operacion = {
            id: this.generarId(),
            tipo,
            ordenId,
            versionBase: vista.version,
            ocurridoEn: this.ocurridoAhora(),
            datos,
            secuencia,
            estado: "pendiente",
            error: null,
            intentos: 0,
        } as OperacionLocal;
        await this.guardar({ operaciones: { poner: [operacion] }, meta: { secuencia } });
        this.emitir();
        this.despertar();
        return { operacion, avisos };
    }

    /**
     * Guarda una foto, firma o documento y encola su `adjunto.registrar`, todo en una escritura.
     * La subida corre en `sincronizar`; `orden.cerrar` espera a los `obligatorio`.
     */
    async agregarAdjunto(entrada: EntradaAdjunto): Promise<{ adjunto: AdjuntoLocal; operacion: OperacionLocal }> {
        const vista = this.validarAccion("adjunto.registrar", entrada.ordenId);
        const id = this.generarId();
        const adjunto: AdjuntoLocal = {
            id,
            ordenId: entrada.ordenId,
            tipo: entrada.tipo,
            claveChecklist: entrada.item?.codigo ?? null,
            obligatorio: entrada.obligatorio ?? false,
            mime: entrada.mime,
            tamano: entrada.datos.byteLength,
            sha256: await this.sha256(entrada.datos),
            lat: entrada.lat ?? null,
            lon: entrada.lon ?? null,
            tomadoEn: entrada.tomadoEn ?? null,
            estado: "por_crear",
            offset: 0,
            intentos: 0,
            ultimoError: null,
            problema: null,
        };
        const datos: DatosPorTipo["adjunto.registrar"] = { adjuntoId: id, tipo: entrada.tipo };
        if (entrada.item) {
            datos.itemId = entrada.item.itemId;
        }
        if (entrada.descripcion) {
            datos.descripcion = entrada.descripcion;
        }
        const secuencia = this.meta.secuencia + 1;
        const operacion: OperacionLocal = {
            id: this.generarId(),
            tipo: "adjunto.registrar",
            ordenId: entrada.ordenId,
            versionBase: vista.version,
            ocurridoEn: this.ocurridoAhora(),
            datos,
            secuencia,
            estado: "pendiente",
            error: null,
            intentos: 0,
        };
        await this.guardar({
            adjuntos: { poner: [adjunto] },
            bytes: { poner: [{ id, datos: entrada.datos }] },
            operaciones: { poner: [operacion] },
            meta: { secuencia },
        });
        this.emitir();
        this.despertar();
        return { adjunto, operacion };
    }

    /** El técnico descarta una operación rechazada (o en espera de adjuntos). */
    async descartar(id: string): Promise<void> {
        const op = this.opDecidible(id);
        await this.guardar({ operaciones: { borrar: [op.id] }, ...this.borrarAdjuntoDe([op]) });
        this.emitir();
        this.despertar();
    }

    /** Descarta todo lo que queda por enviar de una orden (p. ej. reasignada o cancelada). */
    async descartarOrden(ordenId: number): Promise<number> {
        const ops = [...this.ops.values()].filter((op) => op.ordenId === ordenId && !this.enVuelo.has(op.id));
        if (ops.length === 0) {
            return 0;
        }
        const lote: LoteEscritura = { operaciones: { borrar: ops.map((o) => o.id) }, ...this.borrarAdjuntoDe(ops) };
        const orden = this.ordenes.get(ordenId);
        const quedan = [...this.ops.values()].some((op) => op.ordenId === ordenId && this.enVuelo.has(op.id));
        if (orden?.reasignada && !quedan) {
            lote.ordenes = { borrar: [ordenId] };
        }
        await this.guardar(lote);
        this.emitir();
        this.despertar();
        return ops.length;
    }

    /**
     * Rehace una operación rechazada en su mismo lugar de la cola: id nuevo, `versionBase` actual
     * y, si se pasan, datos corregidos. Conserva `ocurridoEn` (cuándo lo hizo el técnico).
     */
    async rehacer<T extends TipoOperacion>(id: string, datos?: DatosPorTipo[T]): Promise<OperacionLocal> {
        const op = this.opDecidible(id);
        const orden = this.ordenes.get(op.ordenId);
        const nueva = {
            ...op,
            id: this.generarId(),
            datos: datos ?? op.datos,
            versionBase: orden?.base.version ?? op.versionBase,
            estado: "pendiente",
            error: null,
            intentos: 0,
        } as OperacionLocal;
        await this.guardar({ operaciones: { borrar: [op.id], poner: [nueva] } });
        this.emitir();
        this.despertar();
        return nueva;
    }

    /**
     * Descarta un adjunto rechazado por la API: borra sus bytes y lo que queda por enviar que lo
     * use (su `adjunto.registrar`, la marca del checklist o la firma que lo citan).
     */
    async descartarAdjunto(id: string): Promise<void> {
        const adjunto = this.adjuntoRechazado(id);
        const ops = [...this.ops.values()].filter((op) => adjuntoDeOp(op) === id && !this.enVuelo.has(op.id));
        await this.guardar({
            adjuntos: { borrar: [adjunto.id] },
            bytes: { borrar: [adjunto.id] },
            operaciones: { borrar: ops.map((o) => o.id) },
        });
        this.emitir();
        this.despertar();
    }

    /**
     * Rehace un adjunto rechazado con un id nuevo (la API pide otro id) y los mismos bytes. Lo que
     * queda en la cola apuntando al viejo pasa al nuevo; si su `adjunto.registrar` ya se aplicó,
     * se encola otro.
     */
    async rehacerAdjunto(id: string): Promise<AdjuntoLocal> {
        const previo = this.adjuntoRechazado(id);
        const bytes = await this.almacen.leerBytes(id);
        if (bytes === null) {
            throw new ErrorSync("adjunto_desconocido", "Los bytes del adjunto ya no están en el teléfono");
        }
        const nuevoId = this.generarId();
        const nuevo: AdjuntoLocal = { ...previo, id: nuevoId, estado: "por_crear", offset: 0, intentos: 0, ultimoError: null, problema: null };
        const reescritas = [...this.ops.values()]
            .filter((op) => adjuntoDeOp(op) === id && !this.enVuelo.has(op.id))
            .map((op) => cambiarAdjunto({ ...op, estado: "pendiente", error: null } as OperacionLocal, id, nuevoId));
        const lote: LoteEscritura = {
            adjuntos: { borrar: [id], poner: [nuevo] },
            bytes: { borrar: [id], poner: [{ id: nuevoId, datos: bytes }] },
            operaciones: { poner: reescritas },
        };
        if (!reescritas.some((op) => op.tipo === "adjunto.registrar")) {
            const orden = this.ordenes.get(previo.ordenId);
            const item = orden?.base.checklist?.find((i) => i.codigo === previo.claveChecklist);
            const datos: DatosPorTipo["adjunto.registrar"] = { adjuntoId: nuevoId, tipo: previo.tipo };
            if (item) {
                datos.itemId = item.itemId;
            }
            const secuencia = this.meta.secuencia + 1;
            reescritas.push({
                id: this.generarId(),
                tipo: "adjunto.registrar",
                ordenId: previo.ordenId,
                versionBase: orden?.base.version ?? 0,
                ocurridoEn: this.ocurridoAhora(),
                datos,
                secuencia,
                estado: "pendiente",
                error: null,
                intentos: 0,
            });
            lote.meta = { secuencia };
        }
        await this.guardar(lote);
        this.emitir();
        this.despertar();
        return nuevo;
    }

    // --- Red ----------------------------------------------------------------------------------

    /** Baja el paquete del día (`GET /campo/paquete`) y reemplaza lo del servidor. */
    cargarPaquete(fecha: string): Promise<void> {
        return this.serializar(async () => {
            const { datos: paquete } = await this.llamar(() => this.transporte.obtenerPaquete(fecha));
            const ordenes = paquete.ordenes ?? [];
            const enPaquete = new Set(ordenes.map((o) => o.id));
            const conOps = this.ordenesConOps();
            const borrar = [...this.ordenes.keys()].filter((id) => !enPaquete.has(id) && !conOps.has(id));
            await this.guardar({
                ordenes: { poner: ordenes.map((base) => ({ id: base.id, base, reasignada: false })), borrar },
                meta: {
                    cursor: paquete.cursor,
                    fecha: paquete.fecha,
                    existencias: paquete.existencias,
                    ruta: paquete.ruta ?? null,
                },
            });
        });
    }

    /**
     * Una pasada completa: caducidad, subidas, lotes y cambios. Lanza `ErrorTransporte` si la red
     * falla (lo ya confirmado queda guardado). Las llamadas se encadenan, nunca corren a la vez.
     */
    sincronizar(): Promise<void> {
        return this.serializar(async () => {
            await this.verificarCaducidad();
            // Primero lo que no depende de archivos (el estado llega rápido aunque la señal sea
            // mala), luego las subidas, y otra vuelta para lo que ellas liberaron. Lo que el
            // servidor no contestó no se repite en la misma pasada.
            const sinRespuesta = new Set<string>();
            await this.enviarPendientes(sinRespuesta);
            const errorSubida = await this.subirAdjuntos();
            await this.enviarPendientes(sinRespuesta);
            await this.traerCambios();
            if (errorSubida) {
                throw errorSubida;
            }
        });
    }

    /** Arranca el bucle: sincroniza, espera `intervaloMs` y, tras una falla, espera exponencial. */
    iniciar(): void {
        if (this.bucle) {
            return;
        }
        const control = new AbortController();
        this.bucle = control;
        void this.correrBucle(control.signal);
    }

    detener(): void {
        this.bucle?.abort();
        this.bucle = null;
        this.sueno?.abort();
    }

    /** El adaptador avisa de `online`/`offline`. Al volver la señal, reintenta enseguida. */
    establecerConexion(enLinea: boolean): void {
        if (!enLinea) {
            this.actualizarConexion("sin_conexion");
            return;
        }
        if (this.conexion === "sin_conexion") {
            this.actualizarConexion("desconocida");
        }
        this.reintentarAhora();
    }

    /** «Reintentar» del técnico (o volver a entrar tras perder la sesión): corta la espera. */
    reintentarAhora(): void {
        this.enEspera = false;
        this.sueno?.abort();
    }

    // --- Internos: bucle ----------------------------------------------------------------------

    /** Una acción nueva despierta al bucle, salvo que esté esperando por una falla. */
    private despertar(): void {
        if (!this.enEspera) {
            this.sueno?.abort();
        }
    }

    private async correrBucle(senal: AbortSignal): Promise<void> {
        let intento = 0;
        while (!senal.aborted) {
            let espera = this.intervaloMs;
            try {
                await this.sincronizar();
                intento = 0;
                this.enEspera = false;
                this.proximoIntento = null;
            } catch (e) {
                if (!(e instanceof ErrorTransporte)) {
                    this.ultimoError = { mensaje: e instanceof Error ? e.message : String(e), status: null, problema: null };
                }
                if (e instanceof ErrorTransporte && (e.esDeSesion || e.esDeEmpresa)) {
                    // Sin sesión no sirve insistir: espera a `reintentarAhora` (o al intervalo).
                    espera = this.intervaloMs;
                } else {
                    espera = calcularEspera(intento, this.espera);
                    intento += 1;
                }
                this.enEspera = true;
                this.proximoIntento = this.reloj.ahora() + espera;
            }
            this.emitir();
            if (senal.aborted) {
                break;
            }
            const sueno = new AbortController();
            this.sueno = sueno;
            await this.reloj.esperar(espera, sueno.signal);
            this.sueno = null;
        }
    }

    private serializar<T>(tarea: () => Promise<T>): Promise<T> {
        const resultado = this.cadena.then(async () => {
            this.sincronizando += 1;
            this.emitir();
            try {
                return await tarea();
            } finally {
                this.sincronizando -= 1;
                this.emitir();
            }
        });
        this.cadena = resultado.catch(() => undefined);
        return resultado;
    }

    /** Llama a la API, mide el desfase y actualiza la conexión. */
    private async llamar<T>(fn: () => Promise<RespuestaApi<T>>): Promise<RespuestaApi<T>> {
        const inicio = this.reloj.ahora();
        try {
            const respuesta = await fn();
            const fin = this.reloj.ahora();
            if (respuesta.horaServidor !== null) {
                // Punto medio del viaje: el servidor respondió más o menos a la mitad.
                this.meta = { ...this.meta, desfaseMs: Math.round(respuesta.horaServidor - (inicio + fin) / 2) };
            }
            this.meta = { ...this.meta, ultimaSincronizacion: fin };
            this.metaSucia = true;
            this.ultimoError = null;
            this.actualizarConexion("en_linea");
            return respuesta;
        } catch (e) {
            if (e instanceof ErrorTransporte) {
                this.ultimoError = { mensaje: e.message, status: e.status, problema: e.problema };
                this.actualizarConexion(
                    e.tipo === "red" ? "sin_conexion" : e.esDeEmpresa ? "sin_empresa" : e.esDeSesion ? "sin_sesion" : "en_linea",
                );
            }
            throw e;
        }
    }

    private actualizarConexion(conexion: Conexion): void {
        if (this.conexion !== conexion) {
            this.conexion = conexion;
            this.emitir();
        }
    }

    // --- Internos: caducidad --------------------------------------------------------------

    private async verificarCaducidad(): Promise<void> {
        const ultima = this.meta.ultimaSincronizacion;
        if (ultima === null || this.meta.fecha === null || this.reloj.ahora() - ultima <= this.caducidadMs) {
            return;
        }
        // Se borra lo que vino del servidor; el trabajo del técnico (cola y adjuntos) nunca.
        const conOps = this.ordenesConOps();
        await this.guardar({
            ordenes: { borrar: [...this.ordenes.keys()].filter((id) => !conOps.has(id)) },
            meta: { cursor: null, fecha: null, existencias: null, ruta: null },
        });
    }

    // --- Internos: adjuntos -------------------------------------------------------------------

    /**
     * Sube lo pendiente. Sin red, sin sesión o sin empresa corta la pasada; otro error de un
     * archivo queda en ese adjunto (visible) y siguen los demás. Devuelve el primer error
     * reintentable, para que el bucle espere antes de la próxima pasada.
     */
    private async subirAdjuntos(): Promise<ErrorTransporte | null> {
        if (!this.subidor) {
            return null;
        }
        let reintentable: ErrorTransporte | null = null;
        const porSubir = [...this.adjuntos.values()].filter((a) => a.estado === "por_crear" || a.estado === "subiendo");
        for (const adjunto of porSubir) {
            try {
                await this.subirUno(this.subidor, adjunto);
            } catch (e) {
                if (!(e instanceof ErrorTransporte) || e.tipo === "red" || e.esDeSesion || e.esDeEmpresa) {
                    throw e;
                }
                const actual = this.adjuntos.get(adjunto.id);
                if (e.esReintentable) {
                    // 429 `demasiadas_subidas`, 5xx (`almacen_no_disponible`…): temporal.
                    reintentable ??= e;
                    if (actual) {
                        await this.guardar({ adjuntos: { poner: [{ ...actual, intentos: actual.intentos + 1, ultimoError: e.message }] } });
                    }
                } else if (actual) {
                    // Definitivo (`mime_no_permitido`, `tamano_excedido`, `sha256_distinto`, 410
                    // `adjunto_rechazado`, 404…): el técnico lo descarta o lo rehace con otro id.
                    await this.guardar({
                        adjuntos: {
                            poner: [{ ...actual, estado: "rechazado", intentos: actual.intentos + 1, ultimoError: e.message, problema: e.problema }],
                        },
                    });
                }
                this.emitir();
            }
        }
        return reintentable;
    }

    private async subirUno(subidor: Subidor, inicial: AdjuntoLocal): Promise<void> {
        let adjunto = inicial;
        const llamarSubida = <T>(fn: () => Promise<T>) =>
            this.llamar(async () => ({ datos: await fn(), horaServidor: null })).then((r) => r.datos);
        const actualizar = async (cambios: Partial<AdjuntoLocal>) => {
            adjunto = { ...adjunto, ...cambios };
            await this.guardar({ adjuntos: { poner: [adjunto] } });
            this.emitir();
        };
        if (adjunto.estado === "por_crear") {
            // El alta es idempotente por id: repetida devuelve lo recibido para retomar.
            const creado = await llamarSubida(() => subidor.crear(this.entradaAlta(adjunto)));
            await actualizar({ estado: "subiendo", offset: Math.min(creado.recibido, adjunto.tamano) });
            if (creado.estado === "completo") {
                await this.terminarSubida(adjunto);
                return;
            }
            if (creado.estado === "rechazado") {
                throw new ErrorTransporte("http", creado.motivoRechazo ?? "Adjunto rechazado", 410);
            }
        } else {
            // Reanudación tras un corte: lo que cuenta es lo que el servidor confirma (HEAD).
            const offset = await llamarSubida(() => subidor.consultarOffset(adjunto.id));
            if (offset !== adjunto.offset) {
                await actualizar({ offset: Math.min(offset, adjunto.tamano) });
            }
        }
        const bytes = adjunto.offset < adjunto.tamano ? await this.almacen.leerBytes(adjunto.id) : null;
        if (adjunto.offset < adjunto.tamano && bytes === null) {
            throw new ErrorTransporte("http", "Los bytes del adjunto ya no están en el teléfono", 410);
        }
        for (let vueltas = 0; ; vueltas += 1) {
            const desde = adjunto.offset;
            // Con todo enviado, un PATCH vacío en `tamano` confirma el cierre (y repite el de un 503).
            const parte = bytes ? bytes.subarray(desde, Math.min(adjunto.tamano, desde + this.tamanoParte)) : new Uint8Array(0);
            let nuevo: number;
            try {
                nuevo = await llamarSubida(() => subidor.enviarParte(adjunto.id, desde, parte));
            } catch (e) {
                const offset = e instanceof ErrorTransporte && e.codigo === "offset_distinto" ? Number(e.cabeceras?.get("Upload-Offset")) : Number.NaN;
                if (Number.isInteger(offset) && offset >= 0 && offset <= adjunto.tamano && vueltas < 50) {
                    // El servidor tiene otra cantidad (p. ej. una parte llegó y la respuesta no): sigue desde ahí.
                    await actualizar({ offset });
                    continue;
                }
                throw e;
            }
            if (nuevo < desde || nuevo > adjunto.tamano || (nuevo === desde && desde < adjunto.tamano)) {
                throw new ErrorTransporte("http", `Upload-Offset inesperado (${nuevo})`, 409);
            }
            await actualizar({ offset: nuevo, ultimoError: null });
            if (nuevo >= adjunto.tamano) {
                break;
            }
        }
        await this.terminarSubida(adjunto);
    }

    private async terminarSubida(adjunto: AdjuntoLocal): Promise<void> {
        const listo: AdjuntoLocal = { ...adjunto, estado: "subido", offset: adjunto.tamano, ultimoError: null, problema: null };
        await this.guardar({ adjuntos: { poner: [listo] }, bytes: { borrar: [adjunto.id] } });
        await this.liberarEsperas(adjunto.ordenId);
        this.emitir();
    }

    /** Alta del adjunto (I1): entidad `orden`, `claveChecklist` = código del ítem foto. */
    private entradaAlta(adjunto: AdjuntoLocal): Parameters<Subidor["crear"]>[0] {
        const entrada: Parameters<Subidor["crear"]>[0] = {
            id: adjunto.id,
            entidad: "orden",
            entidadId: String(adjunto.ordenId),
            tipo: adjunto.tipo,
            mime: adjunto.mime,
            tamano: adjunto.tamano,
            sha256: adjunto.sha256,
        };
        if (adjunto.claveChecklist !== null) {
            entrada.claveChecklist = adjunto.claveChecklist;
        }
        if (adjunto.lat !== null && adjunto.lon !== null) {
            entrada.lat = adjunto.lat;
            entrada.lon = adjunto.lon;
        }
        const tomado = adjunto.tomadoEn === null ? Number.NaN : Date.parse(adjunto.tomadoEn);
        if (!Number.isNaN(tomado)) {
            // `tomadoEn` va corregido con el desfase medido (la API no lo corrige).
            entrada.tomadoEn = new Date(tomado + this.meta.desfaseMs).toISOString();
        }
        return entrada;
    }

    /** ¿Queda por subir algún adjunto de la orden que cubra lo que falta? */
    private faltaSubir(ordenId: number, faltantes: readonly string[]): boolean {
        return [...this.adjuntos.values()].some(
            (a) =>
                a.ordenId === ordenId &&
                (a.estado === "por_crear" || a.estado === "subiendo") &&
                (faltantes.length === 0 || faltantes.some((f) => cubreFaltante(a, f))),
        );
    }

    /** `orden.cerrar` en espera vuelve a la cola (con id nuevo) cuando ya no falta nada que subir. */
    private async liberarEsperas(ordenId: number): Promise<void> {
        const listas = [...this.ops.values()].filter(
            (op) => op.ordenId === ordenId && op.estado === "esperando_adjuntos" && !this.faltaSubir(ordenId, faltantesDe(op.error)),
        );
        if (listas.length > 0) {
            await this.guardar({
                operaciones: { borrar: listas.map((o) => o.id), poner: listas.map((o) => this.conIdNuevo(o)) },
            });
        }
    }

    private borrarAdjuntoDe(ops: readonly OperacionLocal[]): LoteEscritura {
        const ids = ops
            .map((op) => (op.tipo === "adjunto.registrar" ? this.adjuntos.get(op.datos.adjuntoId) : undefined))
            .filter((a): a is AdjuntoLocal => a !== undefined && a.estado !== "subido")
            .map((a) => a.id);
        return ids.length > 0 ? { adjuntos: { borrar: ids }, bytes: { borrar: ids } } : {};
    }

    private adjuntoRechazado(id: string): AdjuntoLocal {
        const adjunto = this.adjuntos.get(id);
        if (!adjunto) {
            throw new ErrorSync("adjunto_desconocido", "El adjunto ya no está en el teléfono");
        }
        if (adjunto.estado !== "rechazado") {
            throw new ErrorSync("adjunto_no_rechazado", "Solo se descarta o rehace un adjunto rechazado");
        }
        return adjunto;
    }

    // --- Internos: lote -------------------------------------------------------------------

    private ordenadas(): OperacionLocal[] {
        return [...this.ops.values()].sort((a, b) => a.secuencia - b.secuencia);
    }

    /** Una operación que aún no puede salir retiene a las siguientes de su orden. */
    private retenida(op: OperacionLocal): boolean {
        const adjuntoId = adjuntoDeOp(op);
        const adjunto = adjuntoId === null ? undefined : this.adjuntos.get(adjuntoId);
        if (adjunto && (adjunto.estado === "por_crear" || adjunto.estado === "rechazado")) {
            // La API no conoce el id hasta `POST /adjuntos` (registrar, foto del checklist, firma).
            return true;
        }
        if (op.tipo === "orden.cerrar") {
            return [...this.adjuntos.values()].some((a) => a.ordenId === op.ordenId && a.obligatorio && a.estado !== "subido");
        }
        return false;
    }

    /** Órdenes cuya cola está detenida por un rechazo (no terminal) o una espera de adjuntos. */
    private ordenesBloqueadas(): Set<number> {
        const bloqueadas = new Set<number>();
        for (const op of this.ops.values()) {
            if (op.estado === "esperando_adjuntos" || (op.estado === "rechazada" && !CODIGOS_TERMINALES.has(codigoProblema(op.error) ?? ""))) {
                bloqueadas.add(op.ordenId);
            }
        }
        return bloqueadas;
    }

    private seleccionarLote(sinRespuesta: ReadonlySet<string>): OperacionLocal[] {
        const lote: OperacionLocal[] = [];
        const detenidas = new Set<number>();
        let bytes = 0;
        for (const op of this.ordenadas()) {
            if (op.estado !== "pendiente") {
                // Lo posterior a un rechazo de la misma orden no sale hasta que el técnico decida.
                if (op.estado === "esperando_adjuntos" || !CODIGOS_TERMINALES.has(codigoProblema(op.error) ?? "")) {
                    detenidas.add(op.ordenId);
                }
                continue;
            }
            if (detenidas.has(op.ordenId)) {
                continue;
            }
            if (sinRespuesta.has(op.id) || this.retenida(op)) {
                detenidas.add(op.ordenId);
                continue;
            }
            const tamano = JSON.stringify(op.datos).length + 300;
            if (lote.length > 0 && bytes + tamano > this.bytesLote) {
                break;
            }
            bytes += tamano;
            lote.push(op);
            if (lote.length >= this.tamanoLote) {
                break;
            }
        }
        return lote;
    }

    private async enviarPendientes(sinRespuesta: Set<string>): Promise<void> {
        for (;;) {
            const lote = this.seleccionarLote(sinRespuesta);
            if (lote.length === 0) {
                return;
            }
            for (const op of lote) {
                this.enVuelo.add(op.id);
            }
            let avance: boolean;
            try {
                const datos = await this.enviarLote(lote);
                const resultados = datos.resultados ?? [];
                avance = await this.procesarResultados(lote, resultados);
                const contestadas = new Set(resultados.map((r) => r.id));
                for (const op of lote) {
                    if (!contestadas.has(op.id)) {
                        sinRespuesta.add(op.id);
                    }
                }
            } catch (e) {
                await this.contarIntento(lote);
                throw e;
            } finally {
                for (const op of lote) {
                    this.enVuelo.delete(op.id);
                }
            }
            if (!avance) {
                return;
            }
        }
    }

    /**
     * `POST /sync/lote` con `enviadoEn` del mismo reloj que `ocurridoEn` (la API mide el desfase y
     * corrige cada hora). Un 409 `dispositivo_ajeno` (el id es de otro usuario) genera otro
     * dispositivo y repite una vez.
     */
    private async enviarLote(lote: readonly OperacionLocal[]) {
        for (let intento = 0; ; intento += 1) {
            const peticion = {
                dispositivoId: this.dispositivoId,
                plataforma: this.plataforma,
                enviadoEn: new Date(this.reloj.ahora()).toISOString(),
                operaciones: lote.map(aCable),
                ...(this.meta.cursor !== null ? { cursor: this.meta.cursor } : {}),
            };
            try {
                return (await this.llamar(() => this.transporte.enviarLote(peticion))).datos;
            } catch (e) {
                if (intento === 0 && e instanceof ErrorTransporte && e.codigo === "dispositivo_ajeno") {
                    await this.guardar({ meta: { dispositivoId: this.generarId() } });
                    continue;
                }
                throw e;
            }
        }
    }

    private async contarIntento(lote: readonly OperacionLocal[]): Promise<void> {
        const poner = lote
            .map((op) => this.ops.get(op.id))
            .filter((op): op is OperacionLocal => op !== undefined)
            .map((op) => ({ ...op, intentos: op.intentos + 1 }));
        await this.guardar({ operaciones: { poner } }).catch(() => undefined);
    }

    private conIdNuevo(op: OperacionLocal): OperacionLocal {
        return { ...op, id: this.generarId(), estado: "pendiente", error: null, intentos: 0 };
    }

    /**
     * Aplica los resultados del lote en una sola escritura. Devuelve si hubo avance (para seguir
     * con el siguiente lote).
     */
    private async procesarResultados(lote: readonly OperacionLocal[], resultados: readonly ResultadoOperacion[]): Promise<boolean> {
        const porId = new Map(resultados.map((r) => [r.id, r]));
        const ordenes = new Map<number, OrdenLocal>();
        const leerOrden = (id: number) => ordenes.get(id) ?? this.ordenes.get(id);
        const borrar: string[] = [];
        const poner: OperacionLocal[] = [];
        let existencias = this.meta.existencias;
        /** Primer rechazo (no terminal) de cada orden en este lote. */
        const causas = new Set<number>();
        let avance = false;

        for (const op of lote) {
            const r = porId.get(op.id);
            if (!r) {
                poner.push({ ...op, intentos: op.intentos + 1 });
                continue;
            }
            avance = true;
            const orden = leerOrden(op.ordenId);
            if (r.estado === "aplicada" || r.estado === "duplicada") {
                borrar.push(op.id);
                if (orden) {
                    let base = aplicarOperacion(orden.base, op);
                    if (r.orden) {
                        base = { ...base, version: r.orden.version, estado: r.orden.estado };
                    }
                    ordenes.set(orden.id, { ...orden, base });
                }
                if (op.tipo === "material.usar") {
                    existencias = descontarExistencias(existencias, [op], (id) => leerOrden(id)?.base.bodegaId ?? null);
                }
                continue;
            }
            // Rechazada.
            const error = r.error ?? null;
            const codigo = codigoProblema(error) ?? "";
            if (orden) {
                let actual: OrdenLocal = orden;
                if (r.orden) {
                    actual = { ...actual, base: { ...actual.base, version: r.orden.version, estado: r.orden.estado } };
                }
                if (codigo === "orden_reasignada") {
                    actual = { ...actual, reasignada: true };
                } else if (codigo === "orden_cancelada" && !r.orden) {
                    actual = { ...actual, base: { ...actual.base, estado: "cancelada" } };
                }
                ordenes.set(actual.id, actual);
            }
            borrar.push(op.id);
            if (codigo === CODIGO_BLOQUEADA || (causas.has(op.ordenId) && !CODIGOS_TERMINALES.has(codigo))) {
                // Consecuencia del rechazo anterior de su orden: vuelve a la cola, detrás de él,
                // con id nuevo (la API guarda el resultado por id).
                poner.push(this.conIdNuevo(op));
                continue;
            }
            if (!CODIGOS_TERMINALES.has(codigo)) {
                causas.add(op.ordenId);
            }
            const espera =
                codigo === "faltan_adjuntos" && op.tipo === "orden.cerrar" && this.faltaSubir(op.ordenId, faltantesDe(error));
            poner.push({ ...op, estado: espera ? "esperando_adjuntos" : "rechazada", error });
        }

        const borrarFinal = borrar.filter((id) => !poner.some((p) => p.id === id));
        await this.guardar({
            operaciones: { poner, borrar: borrarFinal },
            ordenes: { poner: [...ordenes.values()] },
            ...(existencias !== this.meta.existencias ? { meta: { existencias } } : {}),
        });
        this.emitir();
        return avance;
    }

    // --- Internos: cambios ------------------------------------------------------------------

    private async traerCambios(): Promise<void> {
        let cursor = this.meta.cursor;
        for (let pagina = 0; cursor !== null && pagina < 50; pagina += 1) {
            const actual: string = cursor;
            const { datos } = await this.llamar(() => this.transporte.obtenerCambios(actual));
            const conOps = this.ordenesConOps();
            const poner = new Map<number, OrdenLocal>();
            const borrar = new Set<number>();
            for (const cambio of datos.cambios ?? []) {
                const local = poner.get(cambio.ordenId) ?? this.ordenes.get(cambio.ordenId);
                if (cambio.tipo === "reasignada") {
                    if (conOps.has(cambio.ordenId) && local) {
                        poner.set(local.id, { ...local, reasignada: true });
                    } else {
                        poner.delete(cambio.ordenId);
                        borrar.add(cambio.ordenId);
                    }
                    continue;
                }
                if (cambio.orden) {
                    borrar.delete(cambio.ordenId);
                    poner.set(cambio.ordenId, { id: cambio.ordenId, base: cambio.orden, reasignada: false });
                } else if (cambio.tipo === "cancelada" && local) {
                    poner.set(local.id, { ...local, base: { ...local.base, estado: "cancelada" } });
                }
            }
            await this.guardar({
                ordenes: { poner: [...poner.values()], borrar: [...borrar] },
                meta: { cursor: datos.cursor },
            });
            this.emitir();
            if (!datos.hayMas || datos.cursor === actual) {
                return;
            }
            cursor = datos.cursor;
        }
    }

    // --- Internos: estado -----------------------------------------------------------------

    private ordenesConOps(): Set<number> {
        return new Set([...this.ops.values()].map((op) => op.ordenId));
    }

    /** Hora del teléfono sin corregir: la API la corrige con el desfase de `enviadoEn` (I2). */
    private ocurridoAhora(): string {
        return new Date(this.reloj.ahora()).toISOString();
    }

    private validarAccion(tipo: TipoOperacion, ordenId: number): OrdenVista {
        const vista = this.obtenerOrden(ordenId);
        if (!vista) {
            throw new ErrorSync("orden_desconocida", "La orden no está en el teléfono");
        }
        if (vista.reasignada) {
            throw new ErrorSync("orden_reasignada", "La orden se asignó a otro técnico");
        }
        if (vista.estado === "cancelada" && !TIPOS_EVIDENCIA.has(tipo)) {
            throw new ErrorSync("orden_cancelada", "La orden fue cancelada: solo se aceptan notas y adjuntos");
        }
        if (ESTADOS_TERMINALES.has(vista.estado) && vista.estado !== "cancelada" && !TIPOS_EVIDENCIA.has(tipo)) {
            throw new ErrorSync("orden_terminada", "La orden ya está cerrada");
        }
        return vista;
    }

    private opDecidible(id: string): OperacionLocal {
        const op = this.ops.get(id);
        if (!op) {
            throw new ErrorSync("operacion_desconocida", "La operación ya no está en la cola");
        }
        if (op.estado === "pendiente") {
            throw new ErrorSync("operacion_no_rechazada", "Solo se descarta o rehace una operación rechazada");
        }
        return op;
    }

    /** Persiste primero y, solo si se guardó, aplica a la memoria. */
    private async guardar(entrada: LoteEscritura): Promise<void> {
        const lote: LoteEscritura = this.metaSucia
            ? {
                  ...entrada,
                  meta: { desfaseMs: this.meta.desfaseMs, ultimaSincronizacion: this.meta.ultimaSincronizacion, ...entrada.meta },
              }
            : entrada;
        await this.almacen.escribir(lote);
        this.metaSucia = false;
        for (const id of lote.operaciones?.borrar ?? []) {
            this.ops.delete(id);
        }
        for (const op of lote.operaciones?.poner ?? []) {
            this.ops.set(op.id, op);
        }
        for (const id of lote.ordenes?.borrar ?? []) {
            this.ordenes.delete(id);
        }
        for (const orden of lote.ordenes?.poner ?? []) {
            this.ordenes.set(orden.id, orden);
        }
        for (const id of lote.adjuntos?.borrar ?? []) {
            this.adjuntos.delete(id);
        }
        for (const adjunto of lote.adjuntos?.poner ?? []) {
            this.adjuntos.set(adjunto.id, adjunto);
        }
        if (lote.meta) {
            this.meta = { ...this.meta, ...lote.meta };
        }
    }

    private emitir(): void {
        this.instantanea = this.calcularEstado();
        for (const oyente of this.oyentes) {
            oyente(this.instantanea);
        }
    }

    private calcularEstado(): EstadoSync {
        const ops = this.ordenadas();
        const pendientes = ops.filter((op) => op.estado === "pendiente");
        const bloqueadas = this.ordenesBloqueadas();
        const adjuntos = [...this.adjuntos.values()];
        const porSubir = adjuntos.filter((a) => a.estado !== "subido");
        const posiciones = new Map((this.meta.ruta?.ordenIds ?? []).map((id, i) => [id, i]));
        const ordenes: OrdenVista[] = [...this.ordenes.values()].map((orden) => {
            const suyas = ops.filter((op) => op.ordenId === orden.id);
            const base = suyas.filter((op) => op.estado === "pendiente").reduce(aplicarOperacion, orden.base);
            return {
                ...base,
                reasignada: orden.reasignada,
                pendientes: suyas.filter((op) => op.estado === "pendiente").length,
                rechazos: suyas.filter((op) => op.estado === "rechazada").length,
                bloqueada: bloqueadas.has(orden.id),
                esperandoAdjuntos: suyas.some((op) => op.estado === "esperando_adjuntos"),
                subidasPendientes: porSubir.filter((a) => a.ordenId === orden.id).length,
            };
        });
        const clave = (o: OrdenVista) => posiciones.get(o.id) ?? Number.MAX_SAFE_INTEGER;
        // Orden de ruta si hay ruta publicada; si no, por cita (inicio programado) y número.
        const cita = (o: OrdenVista) => o.programadaInicio ?? "9999";
        ordenes.sort((a, b) => clave(a) - clave(b) || cita(a).localeCompare(cita(b)) || a.numero.localeCompare(b.numero));
        const bodegaDe = (id: number) => this.ordenes.get(id)?.base.bodegaId ?? null;
        return {
            conexion: this.conexion,
            sincronizando: this.sincronizando > 0,
            pendientes: pendientes.length,
            cola: pendientes,
            rechazos: ops.filter((op) => op.estado === "rechazada"),
            esperandoAdjuntos: ops.filter((op) => op.estado === "esperando_adjuntos"),
            subidas: {
                pendientes: porSubir.length,
                bytesPendientes: porSubir.reduce((s, a) => s + (a.tamano - a.offset), 0),
                conError: porSubir.filter((a) => a.ultimoError !== null).length,
                rechazadas: adjuntos.filter((a) => a.estado === "rechazado").length,
            },
            adjuntos,
            ordenes,
            existencias: descontarExistencias(this.meta.existencias, pendientes, bodegaDe),
            ruta: this.meta.ruta,
            fecha: this.meta.fecha,
            ultimaSincronizacion: this.meta.ultimaSincronizacion,
            caducado: this.meta.fecha === null && this.meta.ultimaSincronizacion !== null,
            proximoIntento: this.proximoIntento,
            ultimoError: this.ultimoError,
            desfaseMs: this.meta.desfaseMs,
        };
    }
}
