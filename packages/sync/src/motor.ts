// Motor de sincronización de la app de campo (`contratos/sync.md`). Núcleo sin DOM: persiste en
// un `Almacen`, habla con la API por un `Transporte` y un `Subidor`, y mide el tiempo con un
// `Reloj`, todos inyectados. Pensado para React (`useSyncExternalStore(motor.suscribir,
// motor.obtenerEstado)`) sin depender de React.
import type { Almacen, LoteEscritura } from "./almacen";
import { aplicarOperacion, descontarExistencias, ESTADOS_TERMINALES, TIPOS_EVIDENCIA } from "./aplicar";
import type {
    DatosPorTipo,
    Existencia,
    OperacionCable,
    OrdenCampo,
    Problema,
    ResultadoOperacion,
    RutaPublicada,
    TipoAdjunto,
    TipoOperacion,
} from "./contrato-sync";
import { codigoProblema, ErrorSync, ErrorTransporte } from "./errores";
import type { AdjuntoLocal, MetaSync, OperacionLocal, OrdenLocal } from "./modelo";
import { calcularEspera, ESPERA_POR_DEFECTO, relojSistema, type OpcionesEspera, type Reloj } from "./reloj";
import type { RespuestaApi, Subidor, Transporte } from "./transporte";
import { crearGeneradorUuid7 } from "./uuid7";

export const DIA_MS = 24 * 60 * 60 * 1000;

/** Rechazos que dicen algo de la orden entera: no bloquean la cola, la API juzga cada operación. */
export const CODIGOS_TERMINALES: ReadonlySet<string> = new Set(["orden_cancelada", "orden_reasignada"]);

export interface OpcionesMotor {
    almacen: Almacen;
    transporte: Transporte;
    /** Sin subidor los adjuntos quedan en cola y `orden.cerrar` espera. */
    subidor?: Subidor;
    reloj?: Reloj;
    /** Generador de UUID v7 (por defecto, el propio con el reloj del motor). */
    generarId?: () => string;
    espera?: Partial<OpcionesEspera>;
    /** Operaciones por lote (100). */
    tamanoLote?: number;
    /** Bytes por PATCH de tus (256 KiB). */
    tamanoParte?: number;
    /** Caducidad de los datos sin sincronizar (7 días). */
    caducidadMs?: number;
    /** Cada cuánto el bucle pide cambios con la app abierta (60 s). */
    intervaloMs?: number;
    sha256?: (datos: Uint8Array) => Promise<string>;
}

export type Conexion = "desconocida" | "en_linea" | "sin_conexion" | "sin_sesion";

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
    pendientes: number;
    bytesPendientes: number;
    conError: number;
}

export interface EstadoSync {
    conexion: Conexion;
    sincronizando: boolean;
    /** Operaciones por enviar (incluye las retenidas detrás de un rechazo). */
    pendientes: number;
    /** Rechazadas: el técnico decide `descartar` o `rehacer`. */
    rechazos: readonly OperacionLocal[];
    /** `orden.cerrar` que reintentará sola cuando terminen sus subidas. */
    esperandoAdjuntos: readonly OperacionLocal[];
    subidas: ResumenSubidas;
    adjuntos: readonly AdjuntoLocal[];
    ordenes: readonly OrdenVista[];
    existencias: readonly Existencia[];
    ruta: RutaPublicada | null;
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
    ordenId: string;
    tipo: TipoAdjunto;
    datos: Uint8Array;
    mime: string;
    obligatorio?: boolean;
    checklistItemId?: string;
}

type Oyente = (estado: EstadoSync) => void;

async function sha256Sistema(datos: Uint8Array): Promise<string> {
    const resumen = await globalThis.crypto.subtle.digest("SHA-256", datos as Uint8Array<ArrayBuffer>);
    return Array.from(new Uint8Array(resumen), (x) => x.toString(16).padStart(2, "0")).join("");
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

/** Lo que pide `faltan_adjuntos`: `errors[].field` (`adjuntos.<id|tipo|itemId>` o el valor solo). */
export function faltantesDe(problema: Problema | null): string[] {
    return (problema?.errors ?? []).map((e) => e.field.replace(/^adjuntos\./, ""));
}

export class MotorSync {
    private readonly almacen: Almacen;
    private readonly transporte: Transporte;
    private readonly subidor: Subidor | null;
    private readonly reloj: Reloj;
    private readonly generarId: () => string;
    private readonly espera: OpcionesEspera;
    private readonly tamanoLote: number;
    private readonly tamanoParte: number;
    private readonly caducidadMs: number;
    private readonly intervaloMs: number;
    private readonly sha256: (datos: Uint8Array) => Promise<string>;

    private readonly ops = new Map<string, OperacionLocal>();
    private readonly ordenes = new Map<string, OrdenLocal>();
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

    obtenerOrden(id: string): OrdenVista | null {
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
    async registrar<T extends TipoOperacion>(tipo: T, ordenId: string, datos: DatosPorTipo[T]): Promise<ResultadoRegistro> {
        const vista = this.validarAccion(tipo, ordenId);
        const avisos: Aviso[] = [];
        if (tipo === "material.usar") {
            const { productoId, cantidad } = datos as DatosPorTipo["material.usar"];
            const disponible = this.instantanea.existencias.find((e) => e.productoId === productoId)?.cantidad ?? 0;
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
            checklistItemId: entrada.checklistItemId ?? null,
            obligatorio: entrada.obligatorio ?? false,
            mime: entrada.mime,
            tamano: entrada.datos.byteLength,
            sha256: await this.sha256(entrada.datos),
            estado: "por_crear",
            urlSubida: null,
            offset: 0,
            intentos: 0,
            ultimoError: null,
        };
        const datos: DatosPorTipo["adjunto.registrar"] = { adjuntoId: id, tipo: entrada.tipo };
        if (entrada.checklistItemId !== undefined) {
            datos.checklistItemId = entrada.checklistItemId;
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
    async descartarOrden(ordenId: string): Promise<number> {
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

    // --- Red ----------------------------------------------------------------------------------

    /** Baja el paquete del día (`GET /campo/paquete`) y reemplaza lo del servidor. */
    cargarPaquete(fecha: string): Promise<void> {
        return this.serializar(async () => {
            const { datos: paquete } = await this.llamar(() => this.transporte.obtenerPaquete(fecha));
            const enPaquete = new Set(paquete.ordenes.map((o) => o.id));
            const conOps = this.ordenesConOps();
            const borrar = [...this.ordenes.keys()].filter((id) => !enPaquete.has(id) && !conOps.has(id));
            await this.guardar({
                ordenes: { poner: paquete.ordenes.map((base) => ({ id: base.id, base, reasignada: false })), borrar },
                meta: {
                    cursor: paquete.cursor,
                    fecha: paquete.fecha,
                    existencias: paquete.existencias,
                    ruta: paquete.ruta,
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
                if (e instanceof ErrorTransporte && e.esDeSesion) {
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
                this.actualizarConexion(e.tipo === "red" ? "sin_conexion" : e.esDeSesion ? "sin_sesion" : "en_linea");
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
            meta: { cursor: null, fecha: null, existencias: [], ruta: null },
        });
    }

    // --- Internos: adjuntos -------------------------------------------------------------------

    /**
     * Sube lo pendiente. Sin red o sin sesión corta la pasada; otro error de un archivo queda en
     * ese adjunto (visible) y siguen los demás. Devuelve el primer error reintentable, para que el
     * bucle espere antes de la próxima pasada.
     */
    private async subirAdjuntos(): Promise<ErrorTransporte | null> {
        if (!this.subidor) {
            return null;
        }
        let reintentable: ErrorTransporte | null = null;
        const porSubir = [...this.adjuntos.values()].filter((a) => a.estado !== "subido");
        for (const adjunto of porSubir) {
            try {
                await this.subirUno(this.subidor, adjunto);
            } catch (e) {
                if (!(e instanceof ErrorTransporte) || e.tipo === "red" || e.esDeSesion) {
                    throw e;
                }
                if (e.esReintentable) {
                    reintentable ??= e;
                }
                const actual = this.adjuntos.get(adjunto.id);
                if (actual) {
                    await this.guardar({
                        adjuntos: { poner: [{ ...actual, intentos: actual.intentos + 1, ultimoError: e.message }] },
                    });
                    this.emitir();
                }
            }
        }
        return reintentable;
    }

    private async subirUno(subidor: Subidor, inicial: AdjuntoLocal): Promise<void> {
        let adjunto = inicial;
        const llamarSubida = <T>(fn: () => Promise<T>) =>
            this.llamar(async () => ({ datos: await fn(), horaServidor: null })).then((r) => r.datos);
        if (adjunto.estado === "por_crear" || adjunto.urlSubida === null) {
            const creado = await llamarSubida(() =>
                subidor.crear({
                    id: adjunto.id,
                    ordenId: adjunto.ordenId,
                    tipo: adjunto.tipo,
                    tamano: adjunto.tamano,
                    sha256: adjunto.sha256,
                }),
            );
            adjunto = { ...adjunto, estado: "subiendo", urlSubida: creado.urlSubida, offset: 0 };
            await this.guardar({ adjuntos: { poner: [adjunto] } });
            this.emitir();
        } else {
            // Reanudación tras un corte: lo que cuenta es lo que el servidor confirma (HEAD).
            const url = adjunto.urlSubida;
            const offset = await llamarSubida(() => subidor.consultarOffset(url));
            if (offset !== adjunto.offset) {
                adjunto = { ...adjunto, offset };
                await this.guardar({ adjuntos: { poner: [adjunto] } });
            }
        }
        const url = adjunto.urlSubida ?? "";
        if (adjunto.offset < adjunto.tamano) {
            const bytes = await this.almacen.leerBytes(adjunto.id);
            if (bytes === null) {
                throw new ErrorTransporte("http", "Los bytes del adjunto ya no están en el teléfono", 410);
            }
            while (adjunto.offset < adjunto.tamano) {
                const desde = adjunto.offset;
                const parte = bytes.subarray(desde, Math.min(adjunto.tamano, desde + this.tamanoParte));
                const nuevo = await llamarSubida(() => subidor.enviarParte(url, desde, parte));
                if (nuevo <= desde || nuevo > adjunto.tamano) {
                    throw new ErrorTransporte("http", `Upload-Offset inesperado (${nuevo})`, 409);
                }
                adjunto = { ...adjunto, offset: nuevo, ultimoError: null };
                await this.guardar({ adjuntos: { poner: [adjunto] } });
                this.emitir();
            }
        }
        adjunto = { ...adjunto, estado: "subido", ultimoError: null };
        await this.guardar({ adjuntos: { poner: [adjunto] }, bytes: { borrar: [adjunto.id] } });
        await this.liberarEsperas(adjunto.ordenId);
        this.emitir();
    }

    /** ¿Queda por subir algún adjunto de la orden que coincida con lo que falta? */
    private faltaSubir(ordenId: string, faltantes: readonly string[]): boolean {
        return [...this.adjuntos.values()].some(
            (a) =>
                a.ordenId === ordenId &&
                a.estado !== "subido" &&
                (faltantes.length === 0 ||
                    faltantes.some((f) => f === a.id || f === a.tipo || (a.checklistItemId !== null && f === a.checklistItemId))),
        );
    }

    /** `orden.cerrar` en espera vuelve a la cola (con id nuevo) cuando ya no falta nada que subir. */
    private async liberarEsperas(ordenId: string): Promise<void> {
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

    // --- Internos: lote -------------------------------------------------------------------

    private ordenadas(): OperacionLocal[] {
        return [...this.ops.values()].sort((a, b) => a.secuencia - b.secuencia);
    }

    /** Una operación que aún no puede salir retiene a las siguientes de su orden. */
    private retenida(op: OperacionLocal): boolean {
        if (op.tipo === "adjunto.registrar") {
            // La API no conoce el id hasta `POST /adjuntos`.
            return this.adjuntos.get(op.datos.adjuntoId)?.estado === "por_crear";
        }
        if (op.tipo === "orden.cerrar") {
            return [...this.adjuntos.values()].some((a) => a.ordenId === op.ordenId && a.obligatorio && a.estado !== "subido");
        }
        return false;
    }

    /** Órdenes cuya cola está detenida por un rechazo (no terminal) o una espera de adjuntos. */
    private ordenesBloqueadas(): Set<string> {
        const bloqueadas = new Set<string>();
        for (const op of this.ops.values()) {
            if (op.estado === "esperando_adjuntos" || (op.estado === "rechazada" && !CODIGOS_TERMINALES.has(codigoProblema(op.error) ?? ""))) {
                bloqueadas.add(op.ordenId);
            }
        }
        return bloqueadas;
    }

    private seleccionarLote(sinRespuesta: ReadonlySet<string>): OperacionLocal[] {
        const lote: OperacionLocal[] = [];
        const detenidas = new Set<string>();
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
                const { datos } = await this.llamar(() =>
                    this.transporte.enviarLote({ dispositivoId: this.dispositivoId, operaciones: lote.map(aCable) }),
                );
                avance = await this.procesarResultados(lote, datos.resultados);
                const contestadas = new Set(datos.resultados.map((r) => r.id));
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
        const ordenes = new Map<string, OrdenLocal>();
        const leerOrden = (id: string) => ordenes.get(id) ?? this.ordenes.get(id);
        const borrar: string[] = [];
        const poner: OperacionLocal[] = [];
        let existencias = this.meta.existencias;
        /** Primer rechazo (no terminal) de cada orden en este lote. */
        const causas = new Set<string>();
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
                    existencias = descontarExistencias(existencias, [op]);
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
            if (causas.has(op.ordenId) && !CODIGOS_TERMINALES.has(codigo)) {
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
            const poner = new Map<string, OrdenLocal>();
            const borrar = new Set<string>();
            for (const cambio of datos.cambios) {
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

    private ordenesConOps(): Set<string> {
        return new Set([...this.ops.values()].map((op) => op.ordenId));
    }

    private ocurridoAhora(): string {
        return new Date(this.reloj.ahora() + this.meta.desfaseMs).toISOString();
    }

    private validarAccion(tipo: TipoOperacion, ordenId: string): OrdenVista {
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
        const posiciones = new Map((this.meta.ruta?.paradas ?? []).map((p) => [p.ordenId, p.posicion]));
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
        ordenes.sort((a, b) => clave(a) - clave(b) || (a.inicio ?? "").localeCompare(b.inicio ?? "") || a.numero.localeCompare(b.numero));
        return {
            conexion: this.conexion,
            sincronizando: this.sincronizando > 0,
            pendientes: pendientes.length,
            rechazos: ops.filter((op) => op.estado === "rechazada"),
            esperandoAdjuntos: ops.filter((op) => op.estado === "esperando_adjuntos"),
            subidas: {
                pendientes: porSubir.length,
                bytesPendientes: porSubir.reduce((s, a) => s + (a.tamano - a.offset), 0),
                conError: porSubir.filter((a) => a.ultimoError !== null).length,
            },
            adjuntos,
            ordenes,
            existencias: descontarExistencias(this.meta.existencias, pendientes),
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
