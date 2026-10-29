import type { EstadoOrden } from "@iris/sync";
import { ordenDePrueba } from "@iris/sync/pruebas";
import {
    apagarGpsYa,
    calcularHasta,
    clavePausaManual,
    CLAVE_RASTREO,
    claveExplicacion,
    finDelDiaGuatemala,
    leerMarca,
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

const marca = (base: string, hasta = Number.MAX_SAFE_INTEGER, usuarioId = 5, vencida = false) => JSON.stringify({ usuarioId, empresaId: null, base, hasta, vencida });

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
        const reloj = { ms: Date.UTC(2026, 9, 28, 15, 0, 0) }; // 09:00 en Guatemala
        const rastreo = new RastreoUbicacion({ gps, claves, usuarioId: 5, empresaId: null, base: "iris-campo-5-0.db", alCambiar: (e) => estados.push(e), ahora: () => reloj.ms });
        return { gps, claves, estados, rastreo, reloj };
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
        // Con permiso pero sin respuesta del servidor en esta sesión: espera.
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "sin_confirmar" });
        expect(gps.corriendo).toBe(false);
        await rastreo.confirmarServidor(1);
        // 09:00 GT: +12 h (21:00) llega antes que la medianoche.
        const tope = Date.UTC(2026, 9, 29, 3, 0, 0);
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "activa", soloPrimerPlano: false, hasta: tope });
        expect(gps.corriendo).toBe(true);
        expect(leerMarca(claves)).toEqual({ usuarioId: 5, empresaId: null, base: "iris-campo-5-0.db", hasta: tope, vencida: false });
        expect(claves.leer(claveExplicacion(5))).toBe("1");
    });

    it("pausar la última orden en curso apaga el GPS y borra la marca; reanudar lo vuelve a encender", async () => {
        const { gps, claves, rastreo } = montar();
        claves.escribir(claveExplicacion(5), "1");
        gps.permisosActuales = { primerPlano: "concedido", fondo: "concedido" };
        await rastreo.confirmarServidor(1);
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
        await rastreo.confirmarServidor(1);
        await rastreo.aceptarExplicacion();
        expect(rastreo.obtenerEstado()).toMatchObject({ tipo: "activa", soloPrimerPlano: true });
    });

    it("detener (cerrar sesión) apaga el GPS", async () => {
        const { gps, claves, rastreo } = montar();
        claves.escribir(claveExplicacion(5), "1");
        gps.permisosActuales = { primerPlano: "concedido", fondo: "concedido" };
        await rastreo.confirmarServidor(2);
        expect(gps.corriendo).toBe(true);
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
        expect(await recibirLecturas([lectura(1000)], { claves, detenerGps, guardarEnBase, sesionVigente: () => true })).toBe(0);
        expect(detenerGps).toHaveBeenCalled();
        expect(guardarEnBase).not.toHaveBeenCalled();
    });

    it("con la app abierta entrega los puntos a la cola de posiciones", async () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, marca("iris-campo-5-0.db"));
        const recibidos: unknown[] = [];
        registrarReceptor(async (p) => {
            recibidos.push(p);
            return true;
        });
        const guardarEnBase = jest.fn(async () => undefined);
        expect(await recibirLecturas([lectura(1000), lectura(61_000), lectura(0, { latitude: 200 })], { claves, detenerGps: jest.fn(), guardarEnBase, sesionVigente: () => true })).toBe(2);
        expect(recibidos).toHaveLength(2);
        expect(guardarEnBase).not.toHaveBeenCalled();
    });

    it("con la app cerrada los guarda en la base de la marca para enviarlos al abrir", async () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, marca("iris-campo-5-9.db"));
        const guardarEnBase = jest.fn(async () => undefined);
        expect(await recibirLecturas([lectura(1000)], { claves, detenerGps: jest.fn(), guardarEnBase, sesionVigente: () => true })).toBe(1);
        expect(guardarEnBase).toHaveBeenCalledWith("iris-campo-5-9.db", [expect.objectContaining({ lat: 14.6349, registradaEn: "1970-01-01T00:00:01.000Z" })]);
    });

    it("pasado el «hasta» la deja vencida (no la borra), descarta y apaga; vencida sigue descartando", async () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, marca("b.db", 1000));
        const detenerGps = jest.fn(async () => undefined);
        const guardarEnBase = jest.fn(async () => undefined);
        expect(await recibirLecturas([lectura(5000)], { claves, detenerGps, guardarEnBase, sesionVigente: () => true, ahora: () => 2000 })).toBe(0);
        expect(leerMarca(claves)).toMatchObject({ vencida: true });
        expect(detenerGps).toHaveBeenCalled();
        expect(await recibirLecturas([lectura(5000)], { claves, detenerGps, guardarEnBase, sesionVigente: () => true, ahora: () => 500 })).toBe(0);
        expect(guardarEnBase).not.toHaveBeenCalled();
    });

    it.each([
        ["la sesión guardada ya no vale sin señal (o es de otro)", marca("b.db"), () => false],
        ["la marca no se entiende (formato viejo)", "iris-campo-5-0.db", () => true],
    ])("descarta, borra la marca y apaga el GPS si %s", async (_caso, valor, sesionVigente) => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, valor);
        const detenerGps = jest.fn(async () => undefined);
        const guardarEnBase = jest.fn(async () => undefined);
        registrarReceptor(async () => true);
        expect(await recibirLecturas([lectura(5000)], { claves, detenerGps, guardarEnBase, sesionVigente, ahora: () => 2000 })).toBe(0);
        expect(detenerGps).toHaveBeenCalled();
        expect(guardarEnBase).not.toHaveBeenCalled();
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("pregunta por la sesión del usuario de la marca", async () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, marca("b.db", Number.MAX_SAFE_INTEGER, 9));
        const sesionVigente = jest.fn((usuarioId: number) => usuarioId === 5);
        expect(await recibirLecturas([lectura(1000)], { claves, detenerGps: jest.fn(async () => undefined), guardarEnBase: jest.fn(), sesionVigente })).toBe(0);
        expect(sesionVigente).toHaveBeenCalledWith(9);
    });
});

describe("topes del GPS", () => {
    it("hasta = medianoche de Guatemala o +12 h, lo primero", () => {
        const nueveGt = Date.UTC(2026, 9, 28, 15, 0, 0);
        expect(finDelDiaGuatemala(nueveGt)).toBe(Date.UTC(2026, 9, 29, 6, 0, 0));
        expect(calcularHasta(nueveGt)).toBe(nueveGt + 12 * 3600_000);
        // 15:00 GT: la medianoche (9 h) llega antes que +12 h.
        expect(calcularHasta(Date.UTC(2026, 9, 28, 21, 0, 0))).toBe(Date.UTC(2026, 9, 29, 6, 0, 0));
        // 23:30 GT: la medianoche está a 30 min.
        expect(calcularHasta(Date.UTC(2026, 9, 29, 5, 30, 0))).toBe(Date.UTC(2026, 9, 29, 6, 0, 0));
    });

    function encendido(claves = clavesMemoria(), gps = new GpsFalso()) {
        gps.permisosActuales = { primerPlano: "concedido", fondo: "concedido" };
        claves.escribir(claveExplicacion(5), "1");
        const reloj = { ms: Date.UTC(2026, 9, 28, 12, 0, 0) }; // 06:00 GT
        const rastreo = new RastreoUbicacion({ gps, claves, usuarioId: 5, empresaId: 3, base: "b.db", ahora: () => reloj.ms });
        return { gps, claves, reloj, rastreo };
    }

    it("un cambio local sin confirmación del servidor en esta sesión no crea «hasta»", async () => {
        const { gps, claves, rastreo } = encendido();
        await rastreo.actualizar(1); // «Llegué» sin señal al abrir
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "sin_confirmar" });
        expect(gps.corriendo).toBe(false);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("«hasta» sale de la hora de la confirmación; reabrir la app o acciones locales no lo extienden", async () => {
        const { claves, reloj, rastreo } = encendido();
        await rastreo.confirmarServidor(1);
        const primero = leerMarca(claves)?.hasta ?? 0;
        expect(primero).toBe(reloj.ms + 12 * 3600_000);
        reloj.ms += 3600_000;
        await rastreo.actualizar(1);
        expect(leerMarca(claves)?.hasta).toBe(primero);
        // Reabrir la app (otra instancia, sin confirmar): sigue la marca vigente con el mismo tope.
        const otra = encendido(claves);
        otra.reloj.ms = reloj.ms;
        await otra.rastreo.actualizar(1);
        expect(otra.rastreo.obtenerEstado()).toMatchObject({ tipo: "activa", hasta: primero });
        // Una confirmación nueva sí lo renueva.
        await rastreo.confirmarServidor(1);
        expect(leerMarca(claves)?.hasta).toBe(calcularHasta(reloj.ms));
    });

    it("al tope la marca queda vencida; reabrir sin señal y «Llegué» sin señal no encienden; con el paquete confirmado sí", async () => {
        const { gps, claves, reloj, rastreo } = encendido();
        await rastreo.confirmarServidor(1);
        const tope = leerMarca(claves)?.hasta ?? 0;
        reloj.ms = tope + 1;
        await rastreo.actualizar(1);
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "vencida" });
        expect(gps.corriendo).toBe(false);
        expect(leerMarca(claves)).toMatchObject({ vencida: true });

        // Reabrir sin señal: otra instancia sin confirmación.
        const reabierta = encendido(claves, gps);
        reabierta.reloj.ms = reloj.ms;
        await reabierta.rastreo.actualizar(1);
        expect(reabierta.rastreo.obtenerEstado()).toEqual({ tipo: "vencida" });
        // «Llegué» sin señal (cambio local, acción propia): sigue apagado.
        await reabierta.rastreo.accionPropia();
        await reabierta.rastreo.actualizar(1);
        expect(gps.corriendo).toBe(false);
        expect(leerMarca(claves)).toMatchObject({ vencida: true });
        // Vuelve la señal y el paquete confirma la orden en curso: enciende con un tope nuevo.
        await reabierta.rastreo.confirmarServidor(1);
        expect(reabierta.rastreo.obtenerEstado()).toMatchObject({ tipo: "activa", hasta: calcularHasta(reloj.ms) });
        expect(gps.corriendo).toBe(true);
        // El servidor dice que ya no hay órdenes en curso: apaga y borra.
        await reabierta.rastreo.confirmarServidor(0);
        expect(gps.corriendo).toBe(false);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("«Dejar de compartir» se guarda: reabrir y las sincronizaciones no lo levantan; una acción propia sí", async () => {
        const { gps, claves, rastreo } = encendido();
        await rastreo.confirmarServidor(2);
        await rastreo.dejarDeCompartir();
        expect(rastreo.obtenerEstado()).toEqual({ tipo: "pausada_por_ti" });
        expect(gps.corriendo).toBe(false);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
        expect(claves.leer(clavePausaManual(5, 3))).toBe("1");

        // Reabrir la app: sigue apagado.
        const reabierta = encendido(claves, gps);
        await reabierta.rastreo.confirmarServidor(2);
        expect(reabierta.rastreo.obtenerEstado()).toEqual({ tipo: "pausada_por_ti" });
        // Despacho manda una orden nueva y la sincronización la trae: sigue apagado.
        await reabierta.rastreo.actualizar(3);
        await reabierta.rastreo.confirmarServidor(3);
        expect(gps.corriendo).toBe(false);
        // El técnico marca «En camino»: vuelve.
        await reabierta.rastreo.accionPropia();
        expect(reabierta.rastreo.obtenerEstado().tipo).toBe("activa");
        expect(gps.corriendo).toBe(true);
        // Es por usuario y empresa: otra empresa no hereda la pausa.
        expect(claves.leer(clavePausaManual(5, 4))).toBeNull();
    });

    it("apagarGpsYa borra la marca en el acto y pide detener sin pasar por la cola", () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, marca("b.db"));
        const detener = jest.fn(async () => undefined);
        void apagarGpsYa(claves, { detener });
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
        expect(detener).toHaveBeenCalledTimes(1);
    });
});
