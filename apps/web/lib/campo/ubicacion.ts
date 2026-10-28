// Ubicación en vivo del técnico en la PWA: toma un punto con `navigator.geolocation` cada minuto
// mientras la app está abierta y el técnico está en turno, y lo guarda en la cola de posiciones de
// `@iris/sync` (IndexedDB del usuario y la empresa), que lo envía en lote a `POST /campo/posiciones`.
//
// «En turno»: lo empieza el técnico («Iniciar turno») o empieza solo con «En camino» / «Llegué»
// (si ya aceptó la explicación y hay permiso, y con aviso). Termina cuando no le quedan órdenes
// abiertas del día (cerró, falló, se canceló o se reasignó la última), cuando lo termina él o, como
// red de seguridad, al cambiar el día en Guatemala: la app no conoce su turno de la agenda. La
// explicación se muestra la primera vez por usuario aunque el permiso ya esté dado, y el navegador
// pregunta solo después de ella; si se niega, la app sigue funcionando y queda «Sin permiso».
import type { ColaPosiciones, PuntoPosicion } from "@iris/sync";
import { borrarLocal, escribirLocal, leerLocal } from "@/lib/almacen";
import { hoyEnGuatemala } from "./fechas";

/** `preguntar`: el navegador todavía no preguntó; `desconocido`: no hay API de permisos para saberlo. */
export type Permiso = "desconocido" | "preguntar" | "concedido" | "denegado" | "no_disponible";

/** Última lectura: `buscando` mientras espera la primera; `sin_senal` si el GPS no respondió. */
export type LecturaGps = "inactiva" | "buscando" | "ok" | "sin_senal";

export interface EstadoUbicacion {
    permiso: Permiso;
    enTurno: boolean;
    gps: LecturaGps;
    /** Hora (ms) de la última lectura guardada. */
    ultimaLectura: number | null;
    /** En turno, con permiso y tomando puntos. */
    activa: boolean;
}

export interface DependenciasUbicacion {
    geolocalizacion: Geolocation | null;
    permisos: Pick<Permissions, "query"> | null;
    /** Día de hoy en Guatemala (`YYYY-MM-DD`). */
    hoy: () => string;
    /** Día del turno guardado (o `undefined`). */
    leerTurno: () => string | undefined;
    guardarTurno: (fecha: string | null) => void;
    /** ¿Este usuario ya aceptó la explicación de la ubicación? */
    leerExplicacion: () => string | undefined;
    guardarExplicacion: (valor: "aceptada") => void;
    /** Cada cuánto se toma un punto (60 s). */
    intervaloMs: number;
    /** Espera máxima de una lectura (20 s). */
    esperaLecturaMs: number;
    /** Para leer de nuevo al volver a la app (`visibilitychange`). */
    documento: Pick<Document, "visibilityState" | "addEventListener" | "removeEventListener"> | null;
}

const claveTurno = (usuarioId: number) => `iris-campo-turno-${usuarioId}`;
const claveExplicacion = (usuarioId: number) => `iris-campo-ubicacion-explicada-${usuarioId}`;

/** Turno guardado por usuario (comodidad local: si se pierde, el técnico lo inicia otra vez). */
export function turnoGuardado(usuarioId: number) {
    return {
        leerTurno: () => leerLocal(claveTurno(usuarioId)),
        guardarTurno: (fecha: string | null) => (fecha === null ? borrarLocal(claveTurno(usuarioId)) : escribirLocal(claveTurno(usuarioId), fecha)),
    };
}

export function olvidarTurno(usuarioId: number): void {
    borrarLocal(claveTurno(usuarioId));
}

/** Dependencias del navegador (las pruebas pasan las suyas). */
export function dependenciasNavegador(usuarioId: number): DependenciasUbicacion {
    const hayNavegador = typeof navigator !== "undefined";
    return {
        geolocalizacion: hayNavegador && "geolocation" in navigator ? navigator.geolocation : null,
        permisos: hayNavegador && "permissions" in navigator && typeof navigator.permissions?.query === "function" ? navigator.permissions : null,
        hoy: () => hoyEnGuatemala(),
        ...turnoGuardado(usuarioId),
        leerExplicacion: () => leerLocal(claveExplicacion(usuarioId)),
        guardarExplicacion: (valor) => escribirLocal(claveExplicacion(usuarioId), valor),
        intervaloMs: 60_000,
        esperaLecturaMs: 20_000,
        documento: typeof document === "undefined" ? null : document,
    };
}

/** Punto del cable a partir de una lectura del navegador (la cola lo normaliza). */
export function puntoDeLectura(p: GeolocationPosition): PuntoPosicion {
    const punto: PuntoPosicion = { lat: p.coords.latitude, lon: p.coords.longitude, precision: p.coords.accuracy, registradaEn: new Date(p.timestamp).toISOString() };
    if (p.coords.speed !== null) {
        punto.velocidad = p.coords.speed;
    }
    if (p.coords.heading !== null) {
        punto.rumbo = p.coords.heading;
    }
    return punto;
}

const PERMISO_NEGADO = 1;

type Oyente = (estado: EstadoUbicacion) => void;

export class RastreadorUbicacion {
    private readonly cola: ColaPosiciones;
    private readonly deps: DependenciasUbicacion;
    private permiso: Permiso;
    private turno: string | null;
    private gps: LecturaGps = "inactiva";
    private ultimaLectura: number | null = null;
    /** Hora del teléfono de la última lectura correcta (para no leer dos veces seguidas). */
    private leidaEn: number | null = null;
    private temporizador: ReturnType<typeof setInterval> | null = null;
    private leyendo = false;
    private montado = false;
    private estadoPermiso: PermissionStatus | null = null;
    /** Órdenes abiertas del día vistas la última vez (`null` = aún no se vio ninguna lista). */
    private abiertasPrevias: number | null = null;
    private readonly oyentes = new Set<Oyente>();
    private instantanea: EstadoUbicacion;

    constructor(cola: ColaPosiciones, deps: DependenciasUbicacion) {
        this.cola = cola;
        this.deps = deps;
        this.permiso = deps.geolocalizacion ? "desconocido" : "no_disponible";
        const guardado = deps.leerTurno();
        this.turno = guardado === deps.hoy() ? guardado : null;
        if (guardado !== undefined && this.turno === null) {
            deps.guardarTurno(null);
        }
        this.instantanea = this.calcular();
    }

    suscribir = (oyente: Oyente): (() => void) => {
        this.oyentes.add(oyente);
        return () => {
            this.oyentes.delete(oyente);
        };
    };

    obtenerEstado = (): EstadoUbicacion => this.instantanea;

    /** Empieza a escuchar el permiso y la vuelta a la app; devuelve la limpieza. */
    montar(): () => void {
        this.montado = true;
        void this.consultarPermiso();
        const alVer = () => {
            if (this.deps.documento?.visibilityState === "visible") {
                this.revisarDia();
                if (this.activa && this.vencida()) {
                    void this.leer();
                }
            }
        };
        this.deps.documento?.addEventListener("visibilitychange", alVer);
        this.actualizar();
        return () => {
            this.montado = false;
            this.deps.documento?.removeEventListener("visibilitychange", alVer);
            if (this.estadoPermiso) {
                this.estadoPermiso.onchange = null;
            }
            this.pararTemporizador();
        };
    }

    iniciarTurno(): void {
        if (this.turno !== null) {
            return;
        }
        this.turno = this.deps.hoy();
        this.deps.guardarTurno(this.turno);
        // Al empezar el turno se lee enseguida, aunque haya una lectura reciente (de la acción o del permiso).
        this.leidaEn = null;
        this.actualizar();
    }

    /** La primera vez por usuario se explica aunque el permiso ya esté dado. */
    explicacionAceptada(): boolean {
        return this.deps.leerExplicacion() === "aceptada";
    }

    marcarExplicacionAceptada(): void {
        this.deps.guardarExplicacion("aceptada");
    }

    /**
     * El turno termina solo cuando el técnico se queda sin órdenes abiertas del día (pasa de alguna
     * a ninguna: cerró, falló, se canceló o se reasignó la última). Devuelve si lo terminó.
     */
    alCambiarOrdenesAbiertas(abiertas: number): boolean {
        const previas = this.abiertasPrevias;
        this.abiertasPrevias = abiertas;
        if (previas !== null && previas > 0 && abiertas === 0 && this.turno !== null) {
            this.terminarTurno();
            return true;
        }
        return false;
    }

    terminarTurno(): void {
        this.turno = null;
        this.deps.guardarTurno(null);
        this.gps = "inactiva";
        this.actualizar();
    }

    /**
     * Pide el permiso al navegador con una lectura (la pantalla lo llama tras la explicación).
     * Devuelve el permiso resultante; con permiso y en turno, ese punto ya queda guardado.
     */
    async pedirPermiso(): Promise<Permiso> {
        if (!this.deps.geolocalizacion) {
            return "no_disponible";
        }
        // Si el permiso ya llegó (p. ej. por el aviso del navegador) y hay una lectura reciente, basta.
        if (this.permiso !== "concedido" || this.vencida()) {
            await this.leer();
        }
        return this.permiso;
    }

    // --- Internos -----------------------------------------------------------------------------

    /** ¿Toca otra lectura? (la última correcta fue hace casi un intervalo, o nunca). */
    private vencida(): boolean {
        return this.leidaEn === null || Date.now() - this.leidaEn >= this.deps.intervaloMs - 5_000;
    }

    private get activa(): boolean {
        return this.turno !== null && this.permiso === "concedido";
    }

    private async consultarPermiso(): Promise<void> {
        if (!this.deps.geolocalizacion || !this.deps.permisos) {
            return;
        }
        try {
            const estado = await this.deps.permisos.query({ name: "geolocation" });
            if (!this.montado) {
                return;
            }
            this.estadoPermiso = estado;
            const aplicar = () => {
                this.permiso = estado.state === "granted" ? "concedido" : estado.state === "denied" ? "denegado" : "preguntar";
                this.actualizar();
            };
            estado.onchange = aplicar;
            aplicar();
        } catch {
            // Sin Permissions API para geolocalización (Safari viejo): se sabe al pedir.
        }
    }

    /** Arranca o detiene el temporizador según turno y permiso, y avisa. */
    private actualizar(): void {
        if (this.montado && this.activa) {
            if (this.temporizador === null) {
                this.temporizador = setInterval(() => {
                    this.revisarDia();
                    if (this.activa) {
                        void this.leer();
                    }
                }, this.deps.intervaloMs);
                if (this.gps === "inactiva") {
                    this.gps = "buscando";
                }
                if (this.vencida()) {
                    void this.leer();
                }
            }
        } else {
            this.pararTemporizador();
        }
        this.emitir();
    }

    private pararTemporizador(): void {
        if (this.temporizador !== null) {
            clearInterval(this.temporizador);
            this.temporizador = null;
        }
    }

    /** El turno es del día: al cambiar el día en Guatemala, termina solo. */
    private revisarDia(): void {
        if (this.turno !== null && this.turno !== this.deps.hoy()) {
            this.terminarTurno();
        }
    }

    private leer(): Promise<void> {
        const geo = this.deps.geolocalizacion;
        if (!geo || this.leyendo) {
            return Promise.resolve();
        }
        this.leyendo = true;
        return new Promise<void>((resolver) => {
            const terminar = () => {
                this.leyendo = false;
                this.actualizar();
                resolver();
            };
            geo.getCurrentPosition(
                (posicion) => {
                    this.permiso = "concedido";
                    this.gps = this.turno !== null ? "ok" : "inactiva";
                    if (this.turno === null) {
                        terminar();
                        return;
                    }
                    this.cola
                        .agregar(puntoDeLectura(posicion))
                        .then((guardado) => {
                            if (guardado) {
                                this.ultimaLectura = posicion.timestamp;
                            }
                            this.leidaEn = Date.now();
                        })
                        .catch(() => {
                            // Sin espacio en el teléfono: se reintenta en la próxima lectura.
                        })
                        .finally(terminar);
                },
                (error) => {
                    if (error.code === PERMISO_NEGADO) {
                        this.permiso = "denegado";
                    } else {
                        // Hay permiso, pero el GPS no respondió a tiempo o sin señal.
                        if (this.permiso !== "denegado") {
                            this.permiso = "concedido";
                        }
                        this.gps = "sin_senal";
                    }
                    terminar();
                },
                // Una lectura nueva cada vez: una guardada en caché repetiría la hora (la API la toma por duplicada).
                { enableHighAccuracy: true, timeout: this.deps.esperaLecturaMs, maximumAge: 0 },
            );
        });
    }

    private calcular(): EstadoUbicacion {
        return {
            permiso: this.permiso,
            enTurno: this.turno !== null,
            gps: this.turno === null ? "inactiva" : this.gps,
            ultimaLectura: this.ultimaLectura,
            activa: this.activa,
        };
    }

    private emitir(): void {
        const siguiente = this.calcular();
        const previo = this.instantanea;
        if (
            previo.permiso === siguiente.permiso &&
            previo.enTurno === siguiente.enTurno &&
            previo.gps === siguiente.gps &&
            previo.ultimaLectura === siguiente.ultimaLectura &&
            previo.activa === siguiente.activa
        ) {
            return;
        }
        this.instantanea = siguiente;
        for (const oyente of this.oyentes) {
            oyente(siguiente);
        }
    }
}
