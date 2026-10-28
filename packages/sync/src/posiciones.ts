// Ubicación en vivo del técnico: cola local de puntos GPS y envío en lote a
// `POST /api/v1/campo/posiciones` (I2). Igual que el motor, sin DOM: persiste en un
// `AlmacenPosiciones`, habla por un `TransportePosiciones` y mide el tiempo con un `Reloj`. Quien
// toma los puntos (la PWA con `navigator.geolocation`, Expo con su GPS) llama `agregar`.
//
// Reglas (de la API, `i2.md` «Para I4»):
// - Reenviar es seguro: un punto con la misma hora es `duplicada`. Lo que la API contesta con 200
//   (aceptado, duplicado o descartado por `vieja`/`futura`/`frecuencia`) sale de la cola.
// - 6 lotes por minuto por técnico; 429 `demasiadas_posiciones` con `Retry-After`: se espera y no
//   se descarta nada. El motor también se limita solo para no llegar al 429.
// - Un punto de más de 24 h se descarta en la API: se borra antes de enviarlo.
// - 401/403 (`requiere_sesion_campo`, sin permiso, sesión de otra persona) es definitivo: el bucle
//   se detiene con la cola intacta hasta que el dueño vuelva a entrar.
import type { EntradaPosiciones, PuntoPosicion, RespuestaPosiciones } from "./contrato";
import { ErrorTransporte } from "./errores";
import type { ErrorVisible } from "./motor";
import { calcularEspera, ESPERA_POR_DEFECTO, relojSistema, type OpcionesEspera, type Reloj } from "./reloj";
import type { RespuestaApi } from "./transporte";

const MINUTO_MS = 60_000;
const DIA_MS = 24 * 60 * MINUTO_MS;

/** Un punto en la cola del teléfono: el del cable más su hora en ms (la clave). */
export interface PosicionLocal extends PuntoPosicion {
    ms: number;
}

export interface CambiosPosiciones {
    poner?: PosicionLocal[];
    borrar?: number[];
}

/** Persistencia de la cola de posiciones (en la misma base del usuario y la empresa que la cola de sync). */
export interface AlmacenPosiciones {
    cargarPosiciones(): Promise<PosicionLocal[]>;
    /** Atómica: o todo o nada. */
    escribirPosiciones(cambios: CambiosPosiciones): Promise<void>;
}

/** `POST /api/v1/campo/posiciones`. Toda falla se lanza como `ErrorTransporte`. */
export interface TransportePosiciones {
    enviarPosiciones(entrada: EntradaPosiciones): Promise<RespuestaApi<RespuestaPosiciones>>;
}

export interface OpcionesColaPosiciones {
    almacen: AlmacenPosiciones;
    transporte: TransportePosiciones;
    reloj?: Reloj;
    /** El mismo del motor de sync (`MotorSync.dispositivoId`). */
    dispositivoId?: string;
    /** Puntos guardados como máximo; al pasarse se descartan los más viejos (1500 ≈ 25 h a uno por minuto). */
    tope?: number;
    /** Puntos por lote (500, el máximo de la API). */
    tamanoLote?: number;
    /** Antigüedad máxima de un punto (24 h, la de la API). */
    antiguedadMaxMs?: number;
    /** Lotes por minuto (6, el límite de la API). */
    lotesPorMinuto?: number;
    /** Espera tras un 429 sin `Retry-After` legible (30 s). */
    esperaSinRetryAfterMs?: number;
    /** Espera exponencial tras una falla de red o 5xx. */
    espera?: Partial<OpcionesEspera>;
    /** Como en el motor: lanza `ErrorTransporte` si la sesión abierta no es la del dueño de la cola. */
    verificarIdentidad?: () => Promise<void>;
}

export interface DescartadasLocales {
    /** Por pasar el tope de la cola (las más viejas). */
    tope: number;
    /** Por tener más de 24 h sin enviarse. */
    vieja: number;
    /** El lote volvió con 422: nunca se aceptaría; se borra y se muestra el error. */
    invalida: number;
}

export interface EstadoPosiciones {
    /** Puntos guardados por enviar. */
    pendientes: number;
    enviando: boolean;
    /** El último punto que se guardó (enviado o no). */
    ultimaPosicion: PosicionLocal | null;
    /** Hora del teléfono del último lote que la API aceptó. */
    ultimoEnvio: number | null;
    ultimaRespuesta: RespuestaPosiciones | null;
    /** No se envía antes de esta hora: `Retry-After` de un 429 o el límite de lotes por minuto. */
    esperaHasta: number | null;
    /** Próximo reintento tras una falla de red o del servidor. */
    proximoIntento: number | null;
    ultimoError: ErrorVisible | null;
    /** Sin sesión o con la de otra persona: no se envía hasta que el dueño vuelva a entrar. */
    detenida: boolean;
    /** Lo que el teléfono descartó desde que se abrió la cola. */
    descartadas: DescartadasLocales;
}

type Oyente = (estado: EstadoPosiciones) => void;

const redondear = (valor: number, decimales: number) => Math.round(valor * 10 ** decimales) / 10 ** decimales;
const finito = (valor: unknown): valor is number => typeof valor === "number" && Number.isFinite(valor);

/**
 * Deja el punto como lo acepta la API (`PuntoPosicion`): lat/lon en rango con 6 decimales, hora
 * ISO en UTC y los opcionales solo si son válidos (el rumbo del navegador es `NaN` quieto). Un punto
 * sin coordenadas u hora válidas, o con más de 100 km de imprecisión, no sirve: `null`.
 */
export function normalizarPunto(punto: PuntoPosicion): PuntoPosicion | null {
    const { lat, lon, precision, velocidad, rumbo } = punto;
    const ms = Date.parse(punto.registradaEn);
    if (!finito(lat) || !finito(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180 || Number.isNaN(ms)) {
        return null;
    }
    if (finito(precision) && precision > 100_000) {
        return null;
    }
    const limpio: PuntoPosicion = { lat: redondear(lat, 6), lon: redondear(lon, 6), registradaEn: new Date(ms).toISOString() };
    if (finito(precision) && precision >= 0) {
        limpio.precision = redondear(precision, 1);
    }
    if (finito(velocidad) && velocidad >= 0 && velocidad <= 150) {
        limpio.velocidad = redondear(velocidad, 2);
    }
    if (finito(rumbo) && rumbo >= 0 && rumbo <= 360) {
        limpio.rumbo = redondear(rumbo, 1);
    }
    return limpio;
}

/** `Retry-After` en segundos o como fecha HTTP → ms de espera (`null` si no se entiende). */
export function msDeRetryAfter(valor: string | null | undefined, ahora: number): number | null {
    const texto = valor?.trim() ?? "";
    if (/^\d+$/.test(texto)) {
        return Number(texto) * 1000;
    }
    const fecha = texto ? Date.parse(texto) : Number.NaN;
    return Number.isNaN(fecha) ? null : Math.max(0, fecha - ahora);
}

function aCable(local: PosicionLocal): PuntoPosicion {
    const punto: PuntoPosicion = { lat: local.lat, lon: local.lon, registradaEn: local.registradaEn };
    if (local.precision !== undefined) {
        punto.precision = local.precision;
    }
    if (local.velocidad !== undefined) {
        punto.velocidad = local.velocidad;
    }
    if (local.rumbo !== undefined) {
        punto.rumbo = local.rumbo;
    }
    return punto;
}

/**
 * Cola de posiciones del técnico. `agregar` guarda primero (si falla, lanza y no queda nada) y
 * despierta el envío; `iniciar` corre el bucle que envía en lotes respetando el límite, el
 * `Retry-After` y la espera exponencial. Para React: `suscribir` / `obtenerEstado`.
 */
export class ColaPosiciones {
    private readonly almacen: AlmacenPosiciones;
    private readonly transporte: TransportePosiciones;
    private readonly reloj: Reloj;
    private readonly dispositivoId: string | undefined;
    private readonly tope: number;
    private readonly tamanoLote: number;
    private readonly antiguedadMaxMs: number;
    private readonly lotesPorMinuto: number;
    private readonly esperaSinRetryAfterMs: number;
    private readonly espera: OpcionesEspera;
    private readonly verificarIdentidad: (() => Promise<void>) | null;

    private readonly puntos = new Map<number, PosicionLocal>();
    /** Horas de los últimos lotes enviados (ventana de un minuto). */
    private envios: number[] = [];
    /** `Retry-After` del último 429. */
    private bloqueoHasta = 0;
    private enviando = 0;
    private ultimaPosicion: PosicionLocal | null = null;
    private ultimoEnvio: number | null = null;
    private ultimaRespuesta: RespuestaPosiciones | null = null;
    private esperaHasta: number | null = null;
    private proximoIntento: number | null = null;
    private ultimoError: ErrorVisible | null = null;
    private detenida = false;
    private readonly descartadas: DescartadasLocales = { tope: 0, vieja: 0, invalida: 0 };

    private cadena: Promise<unknown> = Promise.resolve();
    private readonly oyentes = new Set<Oyente>();
    private instantanea: EstadoPosiciones;
    private bucle: AbortController | null = null;
    private sueno: AbortController | null = null;
    /** Esperando por una falla, un 429 o el límite: un punto nuevo no despierta el envío. */
    private enEspera = false;

    private constructor(opciones: OpcionesColaPosiciones) {
        this.almacen = opciones.almacen;
        this.transporte = opciones.transporte;
        this.reloj = opciones.reloj ?? relojSistema;
        this.dispositivoId = opciones.dispositivoId || undefined;
        this.tope = Math.max(1, opciones.tope ?? 1500);
        this.tamanoLote = Math.min(500, Math.max(1, opciones.tamanoLote ?? 500));
        this.antiguedadMaxMs = opciones.antiguedadMaxMs ?? DIA_MS;
        this.lotesPorMinuto = Math.max(1, opciones.lotesPorMinuto ?? 6);
        this.esperaSinRetryAfterMs = opciones.esperaSinRetryAfterMs ?? 30_000;
        this.espera = { ...ESPERA_POR_DEFECTO, ...opciones.espera };
        this.verificarIdentidad = opciones.verificarIdentidad ?? null;
        this.instantanea = this.calcularEstado();
    }

    /** Carga lo guardado, descarta lo de más de 24 h o lo que pase el tope. No llama a la red. */
    static async abrir(opciones: OpcionesColaPosiciones): Promise<ColaPosiciones> {
        const cola = new ColaPosiciones(opciones);
        for (const punto of await opciones.almacen.cargarPosiciones()) {
            cola.puntos.set(punto.ms, punto);
        }
        cola.ultimaPosicion = cola.ordenados().at(-1) ?? null;
        const borrar = cola.sobrantes();
        if (borrar.length > 0) {
            await cola.escribir({ borrar });
        }
        cola.emitir();
        return cola;
    }

    // --- Suscripción ------------------------------------------------------------------------

    suscribir = (oyente: Oyente): (() => void) => {
        this.oyentes.add(oyente);
        return () => {
            this.oyentes.delete(oyente);
        };
    };

    obtenerEstado = (): EstadoPosiciones => this.instantanea;

    // --- Puntos -------------------------------------------------------------------------------

    /**
     * Guarda un punto (normalizado) y despierta el envío. Devuelve `false` si no sirve, si ya
     * estaba (misma hora) o si tiene más de 24 h.
     */
    async agregar(punto: PuntoPosicion): Promise<boolean> {
        const limpio = normalizarPunto(punto);
        if (!limpio) {
            return false;
        }
        const ms = Date.parse(limpio.registradaEn);
        if (this.puntos.has(ms) || ms < this.reloj.ahora() - this.antiguedadMaxMs) {
            return false;
        }
        const local: PosicionLocal = { ...limpio, ms };
        this.puntos.set(ms, local);
        const borrar = this.sobrantes();
        this.puntos.delete(ms);
        if (borrar.includes(ms)) {
            // Más vieja que todo lo que cabe en el tope: no entra (ya contada en `sobrantes`).
            const otras = borrar.filter((x) => x !== ms);
            if (otras.length > 0) {
                await this.escribir({ borrar: otras });
            }
            this.emitir();
            return false;
        }
        await this.escribir({ poner: [local], borrar });
        if (!this.ultimaPosicion || ms > this.ultimaPosicion.ms) {
            this.ultimaPosicion = local;
        }
        this.emitir();
        this.despertar();
        return true;
    }

    // --- Envío --------------------------------------------------------------------------------

    /**
     * Una pasada: envía lotes mientras haya puntos y el límite lo permita. Devuelve los ms que hay
     * que esperar para seguir (`Retry-After` o límite por minuto) o `null` si no queda nada. Lanza
     * `ErrorTransporte` si la red o la API fallan (lo confirmado ya salió de la cola).
     */
    enviar(): Promise<number | null> {
        return this.serializar(async () => {
            const viejas = this.sobrantes();
            if (viejas.length > 0) {
                await this.escribir({ borrar: viejas });
            }
            while (this.puntos.size > 0) {
                const ahora = this.reloj.ahora();
                const espera = this.esperaPorLimite(ahora);
                if (espera > 0) {
                    this.esperaHasta = ahora + espera;
                    return espera;
                }
                this.esperaHasta = null;
                const lote = this.ordenados().slice(0, this.tamanoLote);
                try {
                    await this.verificarIdentidad?.();
                    this.envios.push(this.reloj.ahora());
                    const entrada: EntradaPosiciones = { enviadoEn: new Date(this.reloj.ahora()).toISOString(), puntos: lote.map(aCable) };
                    if (this.dispositivoId) {
                        entrada.dispositivoId = this.dispositivoId;
                    }
                    const { datos } = await this.transporte.enviarPosiciones(entrada);
                    await this.escribir({ borrar: lote.map((p) => p.ms) });
                    this.ultimaRespuesta = datos;
                    this.ultimoEnvio = this.reloj.ahora();
                    this.ultimoError = null;
                    this.emitir();
                } catch (e) {
                    if (e instanceof ErrorTransporte && e.status === 429) {
                        // Se respeta `Retry-After`; los puntos se quedan.
                        const ms = msDeRetryAfter(e.cabeceras?.get("Retry-After"), this.reloj.ahora()) ?? this.esperaSinRetryAfterMs;
                        this.bloqueoHasta = this.reloj.ahora() + Math.max(1000, ms);
                        continue;
                    }
                    if (e instanceof ErrorTransporte && e.status === 422) {
                        // Un lote inválido nunca se aceptaría y trabaría la cola: se borra, a la vista.
                        await this.escribir({ borrar: lote.map((p) => p.ms) });
                        this.descartadas.invalida += lote.length;
                        this.ultimoError = { mensaje: e.message, status: e.status, problema: e.problema };
                        this.emitir();
                        continue;
                    }
                    this.ultimoError =
                        e instanceof ErrorTransporte
                            ? { mensaje: e.message, status: e.status, problema: e.problema }
                            : { mensaje: e instanceof Error ? e.message : String(e), status: null, problema: null };
                    if (e instanceof ErrorTransporte && e.esDeSesion) {
                        this.detenida = true;
                    }
                    throw e;
                }
            }
            this.esperaHasta = null;
            return null;
        });
    }

    /** Arranca el bucle de envío (con la app abierta). */
    iniciar(): void {
        if (this.bucle) {
            return;
        }
        this.detenida = false;
        const control = new AbortController();
        this.bucle = control;
        void this.correrBucle(control.signal);
    }

    detener(): void {
        this.bucle?.abort();
        this.bucle = null;
        this.sueno?.abort();
    }

    /** Volvió la señal o el técnico pidió reintentar: corta la espera exponencial (no el `Retry-After`). */
    reintentarAhora(): void {
        this.enEspera = false;
        this.proximoIntento = null;
        this.sueno?.abort();
    }

    // --- Internos -----------------------------------------------------------------------------

    private despertar(): void {
        if (!this.enEspera) {
            this.sueno?.abort();
        }
    }

    private async correrBucle(senal: AbortSignal): Promise<void> {
        let intento = 0;
        while (!senal.aborted) {
            let espera: number;
            try {
                const resto = await this.enviar();
                intento = 0;
                this.proximoIntento = null;
                // Sin puntos se duerme hasta que llegue uno (`agregar` despierta).
                espera = resto ?? 10 * MINUTO_MS;
                this.enEspera = resto !== null;
            } catch (e) {
                if (e instanceof ErrorTransporte && e.esDeSesion) {
                    this.bucle = null;
                    this.emitir();
                    break;
                }
                espera = calcularEspera(intento, this.espera);
                intento += 1;
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

    private esperaPorLimite(ahora: number): number {
        this.envios = this.envios.filter((t) => t > ahora - MINUTO_MS);
        const porLimite = this.envios.length >= this.lotesPorMinuto ? (this.envios[0] ?? ahora) + MINUTO_MS - ahora : 0;
        return Math.max(0, this.bloqueoHasta - ahora, porLimite);
    }

    private ordenados(): PosicionLocal[] {
        return [...this.puntos.values()].sort((a, b) => a.ms - b.ms);
    }

    /** Claves a borrar: más de 24 h y lo que pase el tope (las más viejas). Las cuenta. */
    private sobrantes(): number[] {
        const limite = this.reloj.ahora() - this.antiguedadMaxMs;
        const ordenados = this.ordenados();
        const viejas = ordenados.filter((p) => p.ms < limite).map((p) => p.ms);
        const vigentes = ordenados.length - viejas.length;
        const exceso = ordenados
            .filter((p) => p.ms >= limite)
            .slice(0, Math.max(0, vigentes - this.tope))
            .map((p) => p.ms);
        this.descartadas.vieja += viejas.length;
        this.descartadas.tope += exceso.length;
        return [...viejas, ...exceso];
    }

    /** Persiste primero y, solo si se guardó, aplica a la memoria. */
    private async escribir(cambios: CambiosPosiciones): Promise<void> {
        await this.almacen.escribirPosiciones(cambios);
        for (const ms of cambios.borrar ?? []) {
            this.puntos.delete(ms);
        }
        for (const punto of cambios.poner ?? []) {
            this.puntos.set(punto.ms, punto);
        }
    }

    private serializar<T>(tarea: () => Promise<T>): Promise<T> {
        const resultado = this.cadena.then(async () => {
            this.enviando += 1;
            this.emitir();
            try {
                return await tarea();
            } finally {
                this.enviando -= 1;
                this.emitir();
            }
        });
        this.cadena = resultado.catch(() => undefined);
        return resultado;
    }

    private emitir(): void {
        this.instantanea = this.calcularEstado();
        for (const oyente of this.oyentes) {
            oyente(this.instantanea);
        }
    }

    private calcularEstado(): EstadoPosiciones {
        return {
            pendientes: this.puntos.size,
            enviando: this.enviando > 0,
            ultimaPosicion: this.ultimaPosicion,
            ultimoEnvio: this.ultimoEnvio,
            ultimaRespuesta: this.ultimaRespuesta,
            esperaHasta: this.esperaHasta,
            proximoIntento: this.proximoIntento,
            ultimoError: this.ultimoError,
            detenida: this.detenida,
            descartadas: { ...this.descartadas },
        };
    }
}
