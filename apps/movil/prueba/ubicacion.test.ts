import type { EstadoOrden } from "@iris/sync";
import { ordenDePrueba } from "@iris/sync/pruebas";
import {
    CLAVE_RASTREO,
    claveExplicacion,
    ordenesEnCurso,
    puntoDeLectura,
    RastreoUbicacion,
    recibirLecturas,
    registrarReceptor,
    type EstadoRastreo,
    type GpsFondo,
    type LecturaGps,
    type PermisosGps,
} from "../src/campo/ubicacion";
import { clavesMemoria } from "./preparar";

const vista = (estado: EstadoOrden, reasignada = false) => ({ ...ordenDePrueba(1, { estado }), reasignada });

class GpsFalso implements GpsFondo {
    corriendo = false;
    inicios = 0;
    permisosActuales: PermisosGps = { primerPlano: "preguntar", fondo: "preguntar" };
    alPedir: PermisosGps = { primerPlano: "concedido", fondo: "concedido" };
    pedidos = 0;
    permisos = async () => this.permisosActuales;
    pedirPermisos = async () => {
        this.pedidos += 1;
        this.permisosActuales = this.alPedir;
        return this.permisosActuales;
    };
    iniciar = async () => {
        this.corriendo = true;
        this.inicios += 1;
    };
    detener = async () => {
        this.corriendo = false;
    };
    activo = async () => this.corriendo;
}

const lectura = (ms: number, extra: Partial<LecturaGps["coords"]> = {}): LecturaGps => ({
    coords: { latitude: 14.6349, longitude: -90.5069, accuracy: 8, speed: 3.2, heading: 90, ...extra },
    timestamp: ms,
});

describe("órdenes en curso (misma regla que la PWA)", () => {
    it("en camino y en sitio cuentan; pausada, programada, cerrada y reasignada no", () => {
        expect(ordenesEnCurso([vista("en_camino"), vista("en_sitio")])).toBe(2);
        expect(ordenesEnCurso([vista("pausada"), vista("programada"), vista("cerrada"), vista("fallida"), vista("cancelada")])).toBe(0);
        expect(ordenesEnCurso([vista("en_sitio", true)])).toBe(0);
    });
});

describe("puntoDeLectura", () => {
    it("convierte la lectura de expo-location al punto del cable, normalizado", () => {
        expect(puntoDeLectura(lectura(Date.UTC(2026, 9, 28, 15, 0, 0), { latitude: 14.63491234567 }))).toEqual({
            lat: 14.634912,
            lon: -90.5069,
            registradaEn: "2026-10-28T15:00:00.000Z",
            precision: 8,
            velocidad: 3.2,
            rumbo: 90,
        });
    });
    it("omite lo que el GPS no da y descarta coordenadas imposibles", () => {
        expect(puntoDeLectura(lectura(0, { accuracy: null, speed: null, heading: null }))).toEqual({ lat: 14.6349, lon: -90.5069, registradaEn: "1970-01-01T00:00:00.000Z" });
        expect(puntoDeLectura(lectura(0, { latitude: 120 }))).toBeNull();
    });
});

describe("RastreoUbicacion", () => {
    function montar() {
        const gps = new GpsFalso();
        const claves = clavesMemoria();
        const estados: EstadoRastreo[] = [];
        const rastreo = new RastreoUbicacion({ gps, claves, usuarioId: 5, base: "iris-campo-5-0.db", alCambiar: (e) => estados.push(e) });
        return { gps, claves, estados, rastreo };
    }

    it("sin órdenes en curso no enciende el GPS ni deja la marca", async () => {
        const { gps, claves, rastreo } = montar();
        await rastreo.actualizar(0);
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "fuera_de_curso" });
        expect(gps.corriendo).toBe(false);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("con una en curso explica antes de pedir el permiso; al aceptar pide y enciende", async () => {
        const { gps, claves, rastreo } = montar();
        await rastreo.actualizar(1);
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "explicar", ahoraNo: false });
        expect(gps.pedidos).toBe(0);
        expect(gps.corriendo).toBe(false);

        await rastreo.aceptarExplicacion();
        expect(gps.pedidos).toBe(1);
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "activa", soloPrimerPlano: false });
        expect(gps.corriendo).toBe(true);
        expect(claves.leer(CLAVE_RASTREO)).toBe("iris-campo-5-0.db");
        expect(claves.leer(claveExplicacion(5))).toBe("1");
    });

    it("pausar la última orden en curso apaga el GPS y borra la marca; reanudar lo vuelve a encender", async () => {
        const { gps, claves, rastreo } = montar();
        claves.escribir(claveExplicacion(5), "1");
        gps.permisosActuales = { primerPlano: "concedido", fondo: "concedido" };
        await rastreo.actualizar(1);
        expect(gps.corriendo).toBe(true);
        await rastreo.actualizar(0); // la orden quedó pausada
        expect(gps.corriendo).toBe(false);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
        expect(rastreo.obtenerEstado().tipo).toBe("fuera_de_curso");
        await rastreo.actualizar(1); // reanudada
        expect(gps.corriendo).toBe(true);
        expect(gps.inicios).toBe(2);
    });

    it("«Ahora no» no vuelve a preguntar en la visita; sin permiso del sistema no enciende", async () => {
        const { gps, claves, rastreo } = montar();
        await rastreo.actualizar(1);
        rastreo.ahoraNo();
        await rastreo.actualizar(1);
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "explicar", ahoraNo: true });
        expect(gps.pedidos).toBe(0);

        gps.alPedir = { primerPlano: "negado", fondo: "negado" };
        await rastreo.aceptarExplicacion();
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "sin_permiso", fondo: false });
        expect(gps.corriendo).toBe(false);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("sin «Permitir todo el tiempo» rastrea solo con la app abierta y lo dice", async () => {
        const { gps, rastreo } = montar();
        gps.alPedir = { primerPlano: "concedido", fondo: "negado" };
        await rastreo.actualizar(1);
        await rastreo.aceptarExplicacion();
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "activa", soloPrimerPlano: true });
    });

    it("detener (cerrar sesión) apaga el GPS", async () => {
        const { gps, claves, rastreo } = montar();
        claves.escribir(claveExplicacion(5), "1");
        gps.permisosActuales = { primerPlano: "concedido", fondo: "concedido" };
        await rastreo.actualizar(2);
        await rastreo.detener();
        expect(gps.corriendo).toBe(false);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });
});

describe("tarea en segundo plano (recibirLecturas)", () => {
    afterEach(() => registrarReceptor(null));

    it("sin orden en curso (sin marca) no guarda nada y apaga el GPS", async () => {
        const claves = clavesMemoria();
        const detenerGps = jest.fn(async () => undefined);
        const guardarEnBase = jest.fn(async () => undefined);
        expect(await recibirLecturas([lectura(1000)], { claves, detenerGps, guardarEnBase })).toBe(0);
        expect(detenerGps).toHaveBeenCalled();
        expect(guardarEnBase).not.toHaveBeenCalled();
    });

    it("con la app abierta entrega los puntos a la cola de posiciones", async () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, "iris-campo-5-0.db");
        const recibidos: unknown[] = [];
        registrarReceptor(async (p) => {
            recibidos.push(p);
            return true;
        });
        const guardarEnBase = jest.fn(async () => undefined);
        expect(await recibirLecturas([lectura(1000), lectura(61_000), lectura(0, { latitude: 200 })], { claves, detenerGps: jest.fn(), guardarEnBase })).toBe(2);
        expect(recibidos).toHaveLength(2);
        expect(guardarEnBase).not.toHaveBeenCalled();
    });

    it("con la app cerrada los guarda en la base de la marca para enviarlos al abrir", async () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, "iris-campo-5-9.db");
        const guardarEnBase = jest.fn(async () => undefined);
        expect(await recibirLecturas([lectura(1000)], { claves, detenerGps: jest.fn(), guardarEnBase })).toBe(1);
        expect(guardarEnBase).toHaveBeenCalledWith("iris-campo-5-9.db", [expect.objectContaining({ lat: 14.6349, registradaEn: "1970-01-01T00:00:01.000Z" })]);
    });
});
