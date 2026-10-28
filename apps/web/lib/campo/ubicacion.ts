// Ubicación en vivo del técnico en la PWA: toma un punto con `navigator.geolocation` cada minuto
// mientras la app está abierta y el técnico está en turno, y lo guarda en la cola de posiciones de
// `@iris/sync` (IndexedDB del usuario y la empresa), que lo envía en lote a `POST /campo/posiciones`.
//
// «En turno»:
// - Con turno en la agenda (`Paquete.turno`, guardado en el motor): al empezar un tramo se enciende
//   solo (si ya aceptó la explicación y hay permiso; con aviso) y al terminar el tramo se apaga solo.
//   Un «Terminar turno» a mano dentro de un tramo se respeta hasta el tramo siguiente. Fuera de tramo
//   solo lo mantiene una orden en curso (`en_camino` o `en_sitio`): termina cuando no queda ninguna,
//   y como máximo 2 h después del fin del tramo. «En camino» / «Llegué» lo encienden igual.
// - Sin turno en la agenda: lo empieza el técnico («Iniciar turno») o «En camino» / «Llegué», y
//   termina cuando no le queda ninguna orden en curso o cuando lo termina él. Las pendientes,
//   programadas, atrasadas o pausadas no lo mantienen.
// - Siempre: al cambiar el día en Guatemala termina solo (red de seguridad).
// La explicación se muestra la primera vez por usuario aunque el permiso ya esté dado, y el navegador
// pregunta solo después de ella; si se niega, la app sigue funcionando y queda «Sin permiso».
import type { ColaPosiciones, PuntoPosicion, TurnoDelDia } from "@iris/sync";
import { borrarLocal, escribirLocal, leerLocal } from "@/lib/almacen";
import { hoyEnGuatemala } from "./fechas";
import { escribirTurnoLocal, EXTENSION_MAX_MS, leerTurnoLocal, proximaFrontera, siguienteTramo, tramoEn, tramosDeHoy, type Tramo, type TurnoLocal } from "./turno";

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
    /** Tramos de hoy según la agenda (`null` = sin turno en la agenda hoy). */
    tramos: readonly Tramo[] | null;
    /** En turno dentro de un tramo: hora (ms) en que termina ese tramo. */
    hasta: number | null;
    /** Fuera de turno: inicio (ms) del próximo tramo de hoy. */
    empieza: number | null;
    /** En turno fuera de tramo por una orden en curso: hora (ms) en que termina como máximo. */
    tope: number | null;
}

/** Cambios del turno que hizo el rastreador solo (la pantalla los avisa). */
export type AvisoTurno = "iniciado_por_agenda" | "terminado_por_agenda" | "terminado_por_ordenes" | "terminado_por_tope";

export interface DependenciasUbicacion {
    geolocalizacion: Geolocation | null;
    permisos: Pick<Permissions, "query"> | null;
    /** Día de hoy en Guatemala (`YYYY-MM-DD`). */
    hoy: () => string;
    /** Turno guardado (`TurnoLocal` en JSON; o `undefined`). */
    leerTurno: () => string | undefined;
    guardarTurno: (valor: string | null) => void;
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

/** Clave de antes (por usuario, compartida entre empresas): se descarta sin usarla. */
const claveTurnoVieja = (usuarioId: number) => `iris-campo-turno-${usuarioId}`;
const claveTurno = (usuarioId: number, empresaId: number | null) => `iris-campo-turno-${usuarioId}-${empresaId ?? 0}`;
const claveExplicacion = (usuarioId: number) => `iris-campo-ubicacion-explicada-${usuarioId}`;

/**
 * Turno guardado por usuario y empresa (comodidad local: si se pierde, el técnico lo inicia otra
 * vez o lo enciende la agenda). La clave vieja, por usuario, se lee una sola vez y se borra: un
 * turno no pasa de una empresa a otra.
 */
export function turnoGuardado(usuarioId: number, empresaId: number | null) {
    if (leerLocal(claveTurnoVieja(usuarioId)) !== undefined) {
        borrarLocal(claveTurnoVieja(usuarioId));
    }
    const clave = claveTurno(usuarioId, empresaId);
    return {
        leerTurno: () => leerLocal(clave),
        guardarTurno: (valor: string | null) => (valor === null ? borrarLocal(clave) : escribirLocal(clave, valor)),
    };
}

export function olvidarTurno(usuarioId: number, empresaId: number | null): void {
    borrarLocal(claveTurno(usuarioId, empresaId));
    borrarLocal(claveTurnoVieja(usuarioId));
}

/** Dependencias del navegador (las pruebas pasan las suyas). */
export function dependenciasNavegador(usuarioId: number, empresaId: number | null): DependenciasUbicacion {
    const hayNavegador = typeof navigator !== "undefined";
    return {
        geolocalizacion: hayNavegador && "geolocation" in navigator ? navigator.geolocation : null,
        permisos: hayNavegador && "permissions" in navigator && typeof navigator.permissions?.query === "function" ? navigator.permissions : null,
        hoy: () => hoyEnGuatemala(),
        ...turnoGuardado(usuarioId, empresaId),
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

/** Espera máxima entre revisiones del turno (por si el teléfono durmió o cambió la hora). */
const REVISION_MAX_MS = 10 * 60_000;

export class RastreadorUbicacion {
    private readonly cola: ColaPosiciones;
    private readonly deps: DependenciasUbicacion;
    private permiso: Permiso;
    /** Turno de hoy (`null` = nada guardado hoy). */
    private local: TurnoLocal | null;
    /** Turno de la agenda tal como lo guarda el motor (con el día del paquete). */
    private agenda: TurnoDelDia | null = null;
    private gps: LecturaGps = "inactiva";
    private ultimaLectura: number | null = null;
    /** Hora del teléfono de la última lectura correcta (para no leer dos veces seguidas). */
    private leidaEn: number | null = null;
    private temporizador: ReturnType<typeof setInterval> | null = null;
    private revision: ReturnType<typeof setTimeout> | null = null;
    private leyendo = false;
    private montado = false;
    private evaluando = false;
    private estadoPermiso: PermissionStatus | null = null;
    /** Órdenes en curso (`en_camino` o `en_sitio`) vistas la última vez (`null` = aún no se vio la lista). */
    private enCurso: number | null = null;
    /** «Ahora no» a la explicación en esta visita: nada vuelve a preguntar ni se enciende solo. */
    private declinado = false;
    private readonly oyentes = new Set<Oyente>();
    private readonly oyentesAviso = new Set<(aviso: AvisoTurno) => void>();
    private instantanea: EstadoUbicacion;

    constructor(cola: ColaPosiciones, deps: DependenciasUbicacion) {
        this.cola = cola;
        this.deps = deps;
        this.permiso = deps.geolocalizacion ? "desconocido" : "no_disponible";
        const guardado = leerTurnoLocal(deps.leerTurno());
        this.local = guardado?.fecha === deps.hoy() ? guardado : null;
        if (deps.leerTurno() !== undefined && this.local === null) {
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

    /** Avisos de lo que el rastreador hizo solo (encender o apagar el turno). */
    alAvisar(oyente: (aviso: AvisoTurno) => void): () => void {
        this.oyentesAviso.add(oyente);
        return () => {
            this.oyentesAviso.delete(oyente);
        };
    }

    /** Empieza a escuchar el permiso y la vuelta a la app; devuelve la limpieza. */
    montar(): () => void {
        this.montado = true;
        void this.consultarPermiso();
        const alVer = () => {
            if (this.deps.documento?.visibilityState === "visible") {
                this.evaluar();
                if (this.activa && this.vencida()) {
                    void this.leer();
                }
            }
        };
        this.deps.documento?.addEventListener("visibilitychange", alVer);
        this.evaluar();
        return () => {
            this.montado = false;
            this.deps.documento?.removeEventListener("visibilitychange", alVer);
            if (this.estadoPermiso) {
                this.estadoPermiso.onchange = null;
            }
            this.pararTemporizador();
            this.pararRevision();
        };
    }

    /** Turno de la agenda guardado en el motor (paquete o cambios). */
    establecerAgenda(turno: TurnoDelDia | null): void {
        this.agenda = turno;
        this.evaluar();
    }

    /** «Iniciar turno» a mano. */
    iniciarTurno(): void {
        if (this.enTurno) {
            return;
        }
        this.encender(false);
    }

    /** La primera vez por usuario se explica aunque el permiso ya esté dado. */
    explicacionAceptada(): boolean {
        return this.deps.leerExplicacion() === "aceptada";
    }

    marcarExplicacionAceptada(): void {
        this.deps.guardarExplicacion("aceptada");
        this.declinado = false;
    }

    /** «Ahora no» a la explicación: en esta visita no se vuelve a preguntar ni se enciende solo. */
    declinarEnVisita(): void {
        this.declinado = true;
    }

    declinadoEnVisita(): boolean {
        return this.declinado;
    }

    /**
     * ¿El turno puede encenderse solo? Solo con la explicación aceptada, sin «Ahora no» en esta
     * visita, con permiso y fuera de turno.
     */
    puedeEmpezarSolo(): boolean {
        return !this.enTurno && this.permiso === "concedido" && this.explicacionAceptada() && !this.declinado;
    }

    /**
     * «En camino» / «Llegué»: hay trabajo real. Fuera de turno lo enciende (si puede empezar solo).
     * Devuelve si lo encendió. Lo que lo mantiene después es la orden en curso, no la acción.
     */
    alTrabajar(): boolean {
        // La orden acaba de quedar en curso, aunque la lista todavía no lo haya contado.
        this.enCurso = Math.max(this.enCurso ?? 0, 1);
        if (this.enTurno || !this.puedeEmpezarSolo()) {
            return false;
        }
        this.encender(true);
        return true;
    }

    /**
     * Órdenes en curso (`en_camino` o `en_sitio`; ni pendientes, ni programadas, ni atrasadas, ni
     * pausadas). Fuera de tramo (o sin agenda), al pasar de alguna a ninguna el turno termina solo;
     * dentro de un tramo sigue hasta el fin del tramo. Devuelve si lo terminó.
     */
    alCambiarOrdenesEnCurso(enCurso: number): boolean {
        const previas = this.enCurso;
        this.enCurso = enCurso;
        if (previas === null || previas === 0 || enCurso > 0 || !this.enTurno) {
            return false;
        }
        const tramos = this.tramos();
        if (tramos && tramoEn(tramos, Date.now())) {
            return false;
        }
        this.apagar();
        this.avisar("terminado_por_ordenes");
        return true;
    }

    /**
     * «Terminar turno» a mano (también al cerrar sesión). Dentro de un tramo, no se vuelve a
     * encender solo hasta el tramo siguiente.
     */
    terminarTurno(): void {
        const tramos = this.tramos();
        const tramo = tramos ? tramoEn(tramos, Date.now()) : null;
        this.apagar(tramo?.fin ?? null);
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

    private get enTurno(): boolean {
        return this.local?.enTurno === true;
    }

    /** Tramos de hoy según la agenda (`null` = sin turno en la agenda hoy). */
    private tramos(): Tramo[] | null {
        return tramosDeHoy(this.agenda, this.deps.hoy());
    }

    private guardar(turno: TurnoLocal | null): void {
        this.local = turno;
        this.deps.guardarTurno(turno === null ? null : escribirTurnoLocal(turno));
    }

    /**
     * Enciende el turno. Con agenda, fuera de tramo y por trabajo («En camino» / «Llegué») después
     * de algún tramo de hoy, lleva tope: 2 h desde el fin del último tramo o desde ahora, lo que sea
     * más tarde (antes del primer tramo, se ata al tramo cuando empiece).
     */
    private encender(porTrabajo: boolean): void {
        const tramos = this.tramos();
        const ahora = Date.now();
        const tramo = tramos ? tramoEn(tramos, ahora) : null;
        const ultimoFin = tramos?.filter((t) => t.fin <= ahora).at(-1)?.fin ?? null;
        const limite = porTrabajo && !tramo && ultimoFin !== null ? Math.max(ultimoFin, ahora) + EXTENSION_MAX_MS : null;
        this.guardar({ fecha: this.deps.hoy(), enTurno: true, hastaTramo: tramo?.fin ?? null, limite, apagadoHasta: null });
        // Al empezar el turno se lee enseguida, aunque haya una lectura reciente (de la acción o del permiso).
        this.leidaEn = null;
        this.evaluar();
    }

    private apagar(apagadoHasta: number | null = null): void {
        this.guardar(apagadoHasta === null ? null : { fecha: this.deps.hoy(), enTurno: false, hastaTramo: null, limite: null, apagadoHasta });
        this.gps = "inactiva";
        this.evaluar();
    }

    private avisar(aviso: AvisoTurno): void {
        for (const oyente of this.oyentesAviso) {
            oyente(aviso);
        }
    }

    /**
     * Revisa día y agenda: enciende el turno al empezar un tramo y lo apaga al terminar; luego
     * arranca o detiene la lectura y programa la próxima revisión (inicio o fin de tramo).
     */
    private evaluar(): void {
        if (this.evaluando) {
            return;
        }
        this.evaluando = true;
        try {
            this.revisarDia();
            const tramos = this.tramos();
            const ahora = Date.now();
            const local = this.local;
            if (tramos) {
                const tramo = tramoEn(tramos, ahora);
                if (local?.enTurno) {
                    if (tramo) {
                        if (local.hastaTramo !== tramo.fin || local.limite !== null) {
                            this.guardar({ ...local, hastaTramo: tramo.fin, limite: null });
                        }
                    } else if (local.hastaTramo !== null) {
                        // Terminó el tramo. Solo una orden en curso lo mantiene, y como mucho 2 h.
                        const limite = local.hastaTramo + EXTENSION_MAX_MS;
                        if ((this.enCurso ?? 0) > 0 && ahora < limite) {
                            this.guardar({ ...local, hastaTramo: null, limite });
                        } else {
                            this.terminarSolo("terminado_por_agenda");
                        }
                    } else if (local.limite !== null) {
                        if (ahora >= local.limite) {
                            this.terminarSolo("terminado_por_tope");
                        } else if (this.enCurso === 0) {
                            this.terminarSolo("terminado_por_ordenes");
                        }
                    }
                } else if (tramo && !(ahora < (local?.apagadoHasta ?? 0)) && this.puedeEmpezarSolo()) {
                    // Al empezar un tramo (o al abrir la app dentro de él), salvo que lo haya terminado a mano en este tramo.
                    this.guardar({ fecha: this.deps.hoy(), enTurno: true, hastaTramo: tramo.fin, limite: null, apagadoHasta: null });
                    this.leidaEn = null;
                    this.avisar("iniciado_por_agenda");
                }
            }
            this.programarRevision(tramos, ahora);
        } finally {
            this.evaluando = false;
        }
        this.actualizar();
    }

    private terminarSolo(aviso: AvisoTurno): void {
        this.guardar(null);
        this.gps = "inactiva";
        this.avisar(aviso);
    }

    private programarRevision(tramos: readonly Tramo[] | null, ahora: number): void {
        this.pararRevision();
        if (!this.montado || (tramos === null && this.local === null)) {
            return;
        }
        const limite = this.local?.enTurno && this.local.limite !== null && this.local.limite > ahora ? this.local.limite : null;
        const borde = tramos ? proximaFrontera(tramos, ahora) : null;
        const frontera = borde === null ? limite : limite === null ? borde : Math.min(borde, limite);
        const espera = Math.min(frontera === null ? REVISION_MAX_MS : frontera - ahora, REVISION_MAX_MS);
        this.revision = setTimeout(() => {
            this.revision = null;
            this.evaluar();
        }, Math.max(espera, 0));
    }

    private pararRevision(): void {
        if (this.revision !== null) {
            clearTimeout(this.revision);
            this.revision = null;
        }
    }

    /** ¿Toca otra lectura? (la última correcta fue hace casi un intervalo, o nunca). */
    private vencida(): boolean {
        return this.leidaEn === null || Date.now() - this.leidaEn >= this.deps.intervaloMs - 5_000;
    }

    private get activa(): boolean {
        return this.enTurno && this.permiso === "concedido";
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
                this.evaluar();
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
                    this.evaluar();
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
        if (this.local !== null && this.local.fecha !== this.deps.hoy()) {
            this.guardar(null);
            this.gps = "inactiva";
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
                // Con el permiso recién dado, el turno de la agenda puede encenderse.
                this.evaluar();
                resolver();
            };
            geo.getCurrentPosition(
                (posicion) => {
                    this.permiso = "concedido";
                    this.gps = this.enTurno ? "ok" : "inactiva";
                    if (!this.enTurno) {
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
        const tramos = this.tramos();
        const ahora = Date.now();
        const enTurno = this.enTurno;
        return {
            permiso: this.permiso,
            enTurno,
            gps: enTurno ? this.gps : "inactiva",
            ultimaLectura: this.ultimaLectura,
            activa: this.activa,
            tramos,
            hasta: tramos && enTurno ? (tramoEn(tramos, ahora)?.fin ?? null) : null,
            empieza: tramos && !enTurno ? (siguienteTramo(tramos, ahora)?.inicio ?? null) : null,
            tope: tramos && enTurno && this.local?.hastaTramo === null ? this.local.limite : null,
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
            previo.activa === siguiente.activa &&
            previo.hasta === siguiente.hasta &&
            previo.empieza === siguiente.empieza &&
            previo.tope === siguiente.tope &&
            mismosTramos(previo.tramos, siguiente.tramos)
        ) {
            return;
        }
        this.instantanea = siguiente;
        for (const oyente of this.oyentes) {
            oyente(siguiente);
        }
    }
}

function mismosTramos(a: readonly Tramo[] | null, b: readonly Tramo[] | null): boolean {
    if (a === null || b === null) {
        return a === b;
    }
    return a.length === b.length && a.every((t, i) => t.inicio === b[i]?.inicio && t.fin === b[i]?.fin);
}
