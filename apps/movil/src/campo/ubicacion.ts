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
/** Base SQLite (nombre) cuya cola recibe los puntos mientras hay una orden en curso; ausente = no se rastrea. */
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

/**
 * - `fuera_de_curso`: ninguna orden en curso; no se registra nada.
 * - `explicar`: hay una en curso y falta que el técnico lea la explicación (o dijo «Ahora no»).
 * - `sin_permiso`: el sistema negó el permiso (`fondo`: solo el de segundo plano).
 * - `activa`: se registra un punto por minuto, también con la app en segundo plano.
 */
export type EstadoRastreo =
    | { tipo: "fuera_de_curso" }
    | { tipo: "explicar"; ahoraNo: boolean }
    | { tipo: "sin_permiso"; fondo: boolean }
    | { tipo: "activa"; soloPrimerPlano: boolean }
    | { tipo: "error"; mensaje: string };

export interface OpcionesRastreo {
    gps: GpsFondo;
    claves: AlmacenClaves;
    usuarioId: number;
    /** Base SQLite de la cola (para la tarea cuando la app no está abierta). */
    base: string;
    alCambiar?: (estado: EstadoRastreo) => void;
}

/**
 * Enciende y apaga el GPS en segundo plano según las órdenes en curso. Una llamada a la vez: el
 * último `actualizar` gana. La marca `CLAVE_RASTREO` la lee la tarea en segundo plano: sin ella
 * descarta los puntos (nunca se guarda una posición fuera de una orden en curso).
 */
export class RastreoUbicacion {
    private estado: EstadoRastreo = { tipo: "fuera_de_curso" };
    private enCurso = 0;
    private ahoraNoEnVisita = false;
    private cola: Promise<void> = Promise.resolve();

    constructor(private readonly o: OpcionesRastreo) {}

    obtenerEstado(): EstadoRastreo {
        return this.estado;
    }

    get explicacionAceptada(): boolean {
        return this.o.claves.leer(claveExplicacion(this.o.usuarioId)) === "1";
    }

    /** Nuevas órdenes en curso (del estado del motor). */
    actualizar(enCurso: number): Promise<void> {
        this.enCurso = enCurso;
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

    /** Al cerrar sesión: apaga el GPS y borra la marca (lo pendiente queda en la cola). */
    detener(): Promise<void> {
        this.enCurso = 0;
        return this.serializar(() => this.aplicar());
    }

    private async aplicar(): Promise<void> {
        try {
            if (this.enCurso === 0) {
                await this.apagar();
                this.poner({ tipo: "fuera_de_curso" });
                return;
            }
            if (!this.explicacionAceptada || this.ahoraNoEnVisita) {
                await this.apagar();
                this.poner({ tipo: "explicar", ahoraNo: this.ahoraNoEnVisita });
                return;
            }
            const permisos = await this.o.gps.permisos();
            if (permisos.primerPlano !== "concedido") {
                await this.apagar();
                this.poner({ tipo: "sin_permiso", fondo: false });
                return;
            }
            this.o.claves.escribir(CLAVE_RASTREO, this.o.base);
            if (!(await this.o.gps.activo())) {
                await this.o.gps.iniciar();
            }
            // Sin el permiso «siempre» (Android 10+ / iOS) el sistema solo da puntos con la app abierta.
            this.poner({ tipo: "activa", soloPrimerPlano: permisos.fondo !== "concedido" });
        } catch (e) {
            this.poner({ tipo: "error", mensaje: e instanceof Error ? e.message : "No se pudo usar el GPS" });
        }
    }

    private async apagar(): Promise<void> {
        this.o.claves.borrar(CLAVE_RASTREO);
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
    /** Sin marca de rastreo: apaga las actualizaciones (la orden ya no está en curso). */
    detenerGps: () => Promise<void>;
}

/**
 * Lo que hace la tarea en segundo plano con cada tanda de lecturas. Devuelve cuántos puntos guardó.
 * Sin orden en curso (sin marca) no guarda nada y apaga el GPS.
 */
export async function recibirLecturas(lecturas: readonly LecturaGps[], deps: DependenciasTarea): Promise<number> {
    const base = deps.claves.leer(CLAVE_RASTREO);
    if (base === null) {
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
    await deps.guardarEnBase(base, puntos);
    return puntos.length;
}
