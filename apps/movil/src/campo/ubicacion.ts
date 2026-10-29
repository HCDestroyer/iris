// GPS en segundo plano de la app nativa. Misma regla que la PWA: la ubicación se comparte solo
// mientras hay una orden **en curso** (`en_camino` o `en_sitio`, propia; una `pausada` no cuenta),
// con explicación antes de que el teléfono pida el permiso, y se apaga sola al quedar en 0. Los
// puntos van a la `ColaPosiciones` de `@iris/sync` (misma base SQLite) y salen en lote a
// `POST /campo/posiciones`, como en la PWA.
//
// Lo nativo (expo-location y expo-task-manager) está detrás de `GpsFondo` (`gps-nativo.ts`); aquí
// solo hay lógica, que se prueba con Jest.
import { normalizarPunto, type OrdenVista, type PuntoPosicion } from "@iris/sync";
import type { AlmacenClaves } from "../entorno";

export const TAREA_UBICACION = "iris-campo-ubicacion";
/** Marca del rastreo (`MarcaRastreo` en JSON) mientras hay una orden en curso; ausente = no se rastrea. */
export const CLAVE_RASTREO = "iris-campo-rastreo";
/** El técnico ya leyó la explicación y aceptó (por usuario). */
export const claveExplicacion = (usuarioId: number) => `iris-campo-ubicacion-explicada-${usuarioId}`;
/** Un punto por minuto, como la PWA (y no más de lo que la API acepta). */
export const INTERVALO_MS = 60_000;

const EN_CURSO: ReadonlySet<string> = new Set(["en_camino", "en_sitio"]);

/** Órdenes en curso: `en_camino` o `en_sitio`, propias (no reasignadas). Pausada no cuenta. */
export function ordenesEnCurso(ordenes: readonly Pick<OrdenVista, "estado" | "reasignada">[]): number {
    return ordenes.filter((o) => !o.reasignada && EN_CURSO.has(o.estado)).length;
}

/** Lectura del GPS como la da expo-location (`LocationObject`). */
export interface LecturaGps {
    coords: { latitude: number; longitude: number; accuracy: number | null; speed: number | null; heading: number | null };
    timestamp: number;
}

/** Lectura del GPS → punto del cable (normalizado; `null` si no sirve). */
export function puntoDeLectura(l: LecturaGps): PuntoPosicion | null {
    const punto: PuntoPosicion = { lat: l.coords.latitude, lon: l.coords.longitude, registradaEn: new Date(l.timestamp).toISOString() };
    if (l.coords.accuracy !== null) {
        punto.precision = l.coords.accuracy;
    }
    if (l.coords.speed !== null) {
        punto.velocidad = l.coords.speed;
    }
    if (l.coords.heading !== null) {
        punto.rumbo = l.coords.heading;
    }
    return normalizarPunto(punto);
}

export type EstadoPermiso = "concedido" | "negado" | "preguntar";

export interface PermisosGps {
    primerPlano: EstadoPermiso;
    fondo: EstadoPermiso;
}

/** Lo nativo: permisos y las actualizaciones en segundo plano de la tarea `TAREA_UBICACION`. */
export interface GpsFondo {
    permisos(): Promise<PermisosGps>;
    /** Pide primer plano y luego segundo plano (el sistema muestra sus diálogos). */
    pedirPermisos(): Promise<PermisosGps>;
    iniciar(): Promise<void>;
    detener(): Promise<void>;
    activo(): Promise<boolean>;
}

/** Guatemala no cambia de horario: UTC−6 todo el año. */
const DESFASE_GUATEMALA_MS = -6 * 60 * 60 * 1000;
const DIA_MS = 24 * 60 * 60 * 1000;
/** Tope de un encendido sin confirmación del servidor. */
export const TOPE_MS = 12 * 60 * 60 * 1000;

/** Medianoche de Guatemala que sigue a `ms`. */
export function finDelDiaGuatemala(ms: number): number {
    const local = ms + DESFASE_GUATEMALA_MS;
    return (Math.floor(local / DIA_MS) + 1) * DIA_MS - DESFASE_GUATEMALA_MS;
}

/** Hasta cuándo vale un encendido (o su renovación): medianoche de Guatemala o +12 h, lo primero. */
export function calcularHasta(ahora: number): number {
    return Math.min(finDelDiaGuatemala(ahora), ahora + TOPE_MS);
}

/**
 * Marca que lee la tarea en segundo plano: de quién son los puntos, a qué base van y hasta cuándo
 * valen. Sin marca, `vencida`, pasada de `hasta` o de otro usuario, la tarea descarta y apaga.
 */
export interface MarcaRastreo {
    usuarioId: number;
    empresaId: number | null;
    base: string;
    /** ms; `calcularHasta` de la última confirmación del servidor en una sesión de la app. */
    hasta: number;
    /** Llegó al tope: queda (no se borra) y nada local la revive; solo otra confirmación del servidor. */
    vencida: boolean;
}

export function leerMarca(claves: AlmacenClaves): MarcaRastreo | null {
    const texto = claves.leer(CLAVE_RASTREO);
    if (texto === null) {
        return null;
    }
    try {
        const m = JSON.parse(texto) as Partial<MarcaRastreo>;
        return typeof m.usuarioId === "number" && typeof m.base === "string" && typeof m.hasta === "number" && (m.empresaId === null || typeof m.empresaId === "number")
            ? { usuarioId: m.usuarioId, empresaId: m.empresaId, base: m.base, hasta: m.hasta, vencida: m.vencida === true }
            : null;
    } catch {
        return null;
    }
}

function escribirMarca(claves: AlmacenClaves, marca: MarcaRastreo): void {
    claves.escribir(CLAVE_RASTREO, JSON.stringify(marca));
}

/** «Dejar de compartir ubicación», guardado por usuario y empresa (sobrevive a reabrir la app). */
export const clavePausaManual = (usuarioId: number, empresaId: number | null) => `iris-campo-ubicacion-pausada-${usuarioId}-${empresaId ?? 0}`;

/**
 * - `fuera_de_curso`: ninguna orden en curso; no se registra nada.
 * - `explicar`: hay una en curso y falta que el técnico lea la explicación (o dijo «Ahora no»).
 * - `sin_permiso`: el sistema negó el permiso.
 * - `sin_confirmar`: hay una en curso pero en esta sesión de la app el servidor todavía no respondió
 *   (paquete o cambios) y no hay una marca vigente: se enciende al llegar la respuesta.
 * - `vencida`: pasó el tope (medianoche de Guatemala o 12 h desde la última confirmación); solo
 *   otra confirmación del servidor lo vuelve a encender.
 * - `pausada_por_ti`: «Dejar de compartir ubicación»; vuelve cuando el técnico cambia de estado una
 *   orden suya (en camino, llegué, reanudar).
 * - `activa`: un punto por minuto, también con la app en segundo plano.
 */
export type EstadoRastreo =
    | { tipo: "fuera_de_curso" }
    | { tipo: "explicar"; ahoraNo: boolean }
    | { tipo: "sin_permiso"; fondo: boolean }
    | { tipo: "sin_confirmar" }
    | { tipo: "vencida" }
    | { tipo: "pausada_por_ti" }
    | { tipo: "activa"; soloPrimerPlano: boolean; hasta: number }
    | { tipo: "error"; mensaje: string };

export interface OpcionesRastreo {
    gps: GpsFondo;
    claves: AlmacenClaves;
    usuarioId: number;
    empresaId: number | null;
    /** Base SQLite de la cola (para la tarea cuando la app no está abierta). */
    base: string;
    alCambiar?: (estado: EstadoRastreo) => void;
    ahora?: () => number;
}

/**
 * Enciende y apaga el GPS en segundo plano. Una llamada a la vez. Reglas:
 * - Solo con órdenes en curso, explicación aceptada, permiso y sin «Dejar de compartir».
 * - `hasta` nace solo de una respuesta del servidor **en esta sesión de la app**
 *   (`confirmarServidor`, al bajar el paquete o los cambios): `calcularHasta(hora de la respuesta)`.
 *   Lo local (abrir la app, «Llegué» sin señal) nunca crea ni extiende `hasta`; sin confirmación
 *   solo sigue una marca vigente de antes.
 * - Al llegar al tope, la marca queda `vencida` y el GPS apagado hasta la próxima confirmación.
 */
export class RastreoUbicacion {
    private estado: EstadoRastreo = { tipo: "fuera_de_curso" };
    private enCurso = 0;
    /** Hora de la última respuesta del servidor (paquete o cambios) en esta sesión de la app. */
    private confirmadoEn: number | null = null;
    private ahoraNoEnVisita = false;
    private cola: Promise<void> = Promise.resolve();
    private readonly ahora: () => number;

    constructor(private readonly o: OpcionesRastreo) {
        this.ahora = o.ahora ?? Date.now;
    }

    obtenerEstado(): EstadoRastreo {
        return this.estado;
    }

    get explicacionAceptada(): boolean {
        return this.o.claves.leer(claveExplicacion(this.o.usuarioId)) === "1";
    }

    get pausadaPorTi(): boolean {
        return this.o.claves.leer(clavePausaManual(this.o.usuarioId, this.o.empresaId)) === "1";
    }

    /** Órdenes en curso según el estado del motor (cambios locales incluidos). */
    actualizar(enCurso: number): Promise<void> {
        this.enCurso = enCurso;
        return this.serializar(() => this.aplicar());
    }

    /**
     * El servidor respondió en esta sesión (paquete o cambios ya aplicados en el motor) con
     * `enCurso` órdenes en curso: abre o renueva la ventana del GPS desde ahora.
     */
    confirmarServidor(enCurso: number): Promise<void> {
        this.enCurso = enCurso;
        this.confirmadoEn = this.ahora();
        return this.serializar(() => this.aplicar());
    }

    /** «Dejar de compartir ubicación»: apaga y lo guarda hasta una acción del propio técnico. */
    dejarDeCompartir(): Promise<void> {
        this.o.claves.escribir(clavePausaManual(this.o.usuarioId, this.o.empresaId), "1");
        return this.serializar(() => this.aplicar());
    }

    /** El técnico cambió de estado una orden suya (en camino, llegué, reanudar): levanta su pausa. */
    accionPropia(): Promise<void> {
        this.o.claves.borrar(clavePausaManual(this.o.usuarioId, this.o.empresaId));
        return this.serializar(() => this.aplicar());
    }

    /** «Permitir ubicación» tras leer la explicación: recién ahí pregunta el sistema. */
    aceptarExplicacion(): Promise<void> {
        this.o.claves.escribir(claveExplicacion(this.o.usuarioId), "1");
        this.ahoraNoEnVisita = false;
        return this.serializar(async () => {
            await this.o.gps.pedirPermisos();
            await this.aplicar();
        });
    }

    /** «Ahora no»: no vuelve a preguntar mientras la app siga abierta. */
    ahoraNo(): void {
        this.ahoraNoEnVisita = true;
        void this.serializar(() => this.aplicar());
    }

    /** Toda salida de la sesión (cerrar, vencida, ajena, cambiar de empresa): apaga y borra la marca. */
    detener(): Promise<void> {
        this.enCurso = 0;
        return this.serializar(() => this.aplicar());
    }

    private async aplicar(): Promise<void> {
        try {
            if (this.enCurso === 0) {
                await this.apagar(true);
                this.poner({ tipo: "fuera_de_curso" });
                return;
            }
            if (this.pausadaPorTi) {
                await this.apagar(true);
                this.poner({ tipo: "pausada_por_ti" });
                return;
            }
            if (!this.explicacionAceptada || this.ahoraNoEnVisita) {
                await this.apagar(true);
                this.poner({ tipo: "explicar", ahoraNo: this.ahoraNoEnVisita });
                return;
            }
            const permisos = await this.o.gps.permisos();
            if (permisos.primerPlano !== "concedido") {
                await this.apagar(true);
                this.poner({ tipo: "sin_permiso", fondo: false });
                return;
            }
            const ahora = this.ahora();
            const marca = leerMarca(this.o.claves);
            const propia = marca && marca.usuarioId === this.o.usuarioId && marca.base === this.o.base ? marca : null;
            let hasta: number;
            if (this.confirmadoEn !== null) {
                hasta = calcularHasta(this.confirmadoEn);
            } else if (propia && !propia.vencida) {
                hasta = propia.hasta;
            } else if (propia?.vencida) {
                await this.apagar(false);
                this.poner({ tipo: "vencida" });
                return;
            } else {
                await this.apagar(true);
                this.poner({ tipo: "sin_confirmar" });
                return;
            }
            if (hasta <= ahora) {
                escribirMarca(this.o.claves, { usuarioId: this.o.usuarioId, empresaId: this.o.empresaId, base: this.o.base, hasta, vencida: true });
                this.confirmadoEn = null;
                await this.apagar(false);
                this.poner({ tipo: "vencida" });
                return;
            }
            escribirMarca(this.o.claves, { usuarioId: this.o.usuarioId, empresaId: this.o.empresaId, base: this.o.base, hasta, vencida: false });
            if (!(await this.o.gps.activo())) {
                await this.o.gps.iniciar();
            }
            // Sin el permiso «siempre» (Android 10+ / iOS) el sistema solo da puntos con la app abierta.
            this.poner({ tipo: "activa", soloPrimerPlano: permisos.fondo !== "concedido", hasta });
        } catch (e) {
            this.poner({ tipo: "error", mensaje: e instanceof Error ? e.message : "No se pudo usar el GPS" });
        }
    }

    /** Apaga el GPS; `borrarMarca: false` deja una marca vencida en su lugar. */
    private async apagar(borrarMarca: boolean): Promise<void> {
        if (borrarMarca) {
            this.o.claves.borrar(CLAVE_RASTREO);
        }
        if (await this.o.gps.activo()) {
            await this.o.gps.detener();
        }
    }

    private poner(estado: EstadoRastreo): void {
        this.estado = estado;
        this.o.alCambiar?.(estado);
    }

    private serializar(tarea: () => Promise<void>): Promise<void> {
        const siguiente = this.cola.then(tarea);
        this.cola = siguiente.catch(() => undefined);
        return siguiente;
    }
}

/**
 * Apagado de emergencia, sin pasar por la cola del rastreo (lo usa la pantalla de error de la raíz):
 * borra la marca en el acto (la tarea deja de guardar puntos desde la próxima lectura), suelta el
 * receptor y pide al sistema detener las actualizaciones.
 */
export function apagarGpsYa(claves: AlmacenClaves, gps: Pick<GpsFondo, "detener">): Promise<void> {
    claves.borrar(CLAVE_RASTREO);
    registrarReceptor(null);
    return gps.detener().catch(() => undefined);
}

/** Recibe los puntos de la tarea: la cola abierta (app viva) o, si no, el almacén guardado. */
export type Receptor = (punto: PuntoPosicion) => Promise<boolean>;

let receptor: Receptor | null = null;

/** La app registra `ColaPosiciones.agregar` al abrir el motor (y lo quita al cerrar). */
export function registrarReceptor(r: Receptor | null): void {
    receptor = r;
}

export interface DependenciasTarea {
    claves: AlmacenClaves;
    /** Con la app cerrada: guarda en la cola de esa base para enviarlos al abrir. */
    guardarEnBase: (base: string, puntos: PuntoPosicion[]) => Promise<void>;
    /** Sin marca válida: apaga las actualizaciones. */
    detenerGps: () => Promise<void>;
    /** ¿La sesión guardada es de este usuario y cumple `usableSinSenal`? */
    sesionVigente: (usuarioId: number) => boolean;
    ahora?: () => number;
}

/**
 * Lo que hace la tarea en segundo plano con cada tanda de lecturas. Devuelve cuántos puntos guardó.
 * Descarta todo y apaga el GPS si no hay marca, si está vencida o pasó su `hasta` (la deja
 * `vencida`) o si la sesión guardada ya no es de ese usuario o ya no vale sin señal (la borra).
 */
export async function recibirLecturas(lecturas: readonly LecturaGps[], deps: DependenciasTarea): Promise<number> {
    const marca = leerMarca(deps.claves);
    const ahora = (deps.ahora ?? Date.now)();
    if (marca && !marca.vencida && marca.hasta <= ahora) {
        // Llegó al tope con la app cerrada: queda vencida (nada local la revive).
        escribirMarca(deps.claves, { ...marca, vencida: true });
        await deps.detenerGps();
        return 0;
    }
    if (!marca || marca.vencida || !deps.sesionVigente(marca.usuarioId)) {
        if (!marca?.vencida) {
            deps.claves.borrar(CLAVE_RASTREO);
        }
        await deps.detenerGps();
        return 0;
    }
    const puntos = lecturas.map(puntoDeLectura).filter((p): p is PuntoPosicion => p !== null);
    if (puntos.length === 0) {
        return 0;
    }
    if (receptor) {
        let guardados = 0;
        for (const p of puntos) {
            if (await receptor(p)) {
                guardados += 1;
            }
        }
        return guardados;
    }
    await deps.guardarEnBase(marca.base, puntos);
    return puntos.length;
}
