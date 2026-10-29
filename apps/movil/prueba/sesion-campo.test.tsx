// Ciclo de vida de la sesión de campo y el GPS en segundo plano (revisión de seguridad de la PR #39):
// toda salida apaga el GPS y borra la marca (sesión vencida, sesión ajena, cambiar de empresa) y el
// técnico puede «Dejar de compartir ubicación».
import { ColaPosiciones, ErrorTransporte } from "@iris/sync";
import { montar, ordenDePrueba, paqueteDePrueba, TransportePosicionesFalso } from "@iris/sync/pruebas";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react-native";
import { useState } from "react";
import { Text } from "react-native";
import { CLAVE_RASTREO, claveExplicacion, clavePausaManual, leerMarca, type GpsFondo, type PermisosGps } from "../src/campo/ubicacion";
import { AppCampo, type DependenciasCampo } from "../src/pantallas/AppCampo";
import { RecuperarError } from "../src/pantallas/RecuperarError";
import type { SesionCampo } from "../src/sesion/sesion";
import { ProveedorTema } from "../src/tema/tema";
import { clavesMemoria } from "./preparar";

jest.mock("expo-camera", () => ({ CameraView: () => null, useCameraPermissions: () => [null, jest.fn()] }));

class GpsFalso implements GpsFondo {
    corriendo = false;
    permisos = async (): Promise<PermisosGps> => ({ primerPlano: "concedido", fondo: "concedido" });
    pedirPermisos = this.permisos;
    iniciar = async () => {
        this.corriendo = true;
    };
    detener = async () => {
        this.corriendo = false;
    };
    activo = async () => this.corriendo;
}

const sesion: SesionCampo = {
    usuarioId: 5,
    nombre: "Téc. Ana",
    correo: "ana@isp.gt",
    empresas: [
        { id: 1, nombre: "ISP Norte" },
        { id: 2, nombre: "ISP Sur" },
    ],
    modoEmpresa: "activa",
    empresaActiva: 1,
    verificadoEn: Date.now(),
};

async function preparar(fallaDeSesion = false, sinRed = false) {
    const m = await montar();
    m.transporte.paquete = (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [ordenDePrueba(1, { estado: "en_camino" })] });
    await m.motor.cargarPaquete("2026-10-28");
    if (fallaDeSesion) {
        m.transporte.fallar(20, new ErrorTransporte("http", "No autenticado", 401, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 }));
    }
    m.transporte.sinRed = sinRed;
    const transportePosiciones = new TransportePosicionesFalso(m.reloj);
    const posiciones = await ColaPosiciones.abrir({ almacen: m.almacen, transporte: transportePosiciones, reloj: m.reloj });
    const gps = new GpsFalso();
    const claves = clavesMemoria();
    claves.escribir(claveExplicacion(5), "1");
    const salidas: string[] = [];
    let sesionAjena: (() => void) | null = null;
    const deps: DependenciasCampo = {
        abrirMotor: async (_clave, alSesionAjena, alConfirmarServidor) => {
            sesionAjena = alSesionAjena;
            // Como `conConfirmacion` del transporte nativo: cada paquete o cambios del servidor avisa.
            const paquete = m.transporte.obtenerPaquete.bind(m.transporte);
            const cambios = m.transporte.obtenerCambios.bind(m.transporte);
            m.transporte.obtenerPaquete = async (f) => {
                const r = await paquete(f);
                alConfirmarServidor();
                return r;
            };
            m.transporte.obtenerCambios = async (c) => {
                const r = await cambios(c);
                alConfirmarServidor();
                return r;
            };
            return { motor: m.motor, posiciones, nombre: "iris-campo-5-1.db", cerrar: async () => undefined, borrar: async () => undefined };
        },
        gps,
        claves,
        servicios: { tomarFoto: async () => null, posicion: async () => null, enLinea: () => true },
        red: { leer: async () => true, escuchar: () => () => undefined },
        alVolverAlFrente: () => () => undefined,
        cerrarSesionApi: async () => undefined,
        vaciarCacheFotos: jest.fn(),
        olvidarSesionGuardada: jest.fn(),
    };

    function Arnes() {
        const [dentro, setDentro] = useState(true);
        return (
            <ProveedorTema>
                {dentro ? (
                    <AppCampo
                        sesion={sesion}
                        empresa={1}
                        deps={deps}
                        alSalir={(motivo) => {
                            salidas.push(motivo);
                            setDentro(false);
                        }}
                        cambiarEmpresa={() => {
                            salidas.push("empresa");
                            setDentro(false);
                        }}
                    />
                ) : (
                    <Text>Fuera</Text>
                )}
            </ProveedorTema>
        );
    }
    await render(<Arnes />);
    return { m, gps, claves, salidas, deps, posiciones, transportePosiciones, ajena: () => sesionAjena?.() };
}

async function esperarEncendido(gps: GpsFalso) {
    await waitFor(() => expect(screen.getByTestId("estado-ubicacion")).toHaveTextContent(/Ubicación activa/));
    expect(gps.corriendo).toBe(true);
}

afterEach(() => {
    jest.useRealTimers();
});

describe("GPS y salidas de la sesión de campo", () => {
    it("con una orden en curso enciende con la marca del usuario, la empresa y el tope", async () => {
        const { gps, claves } = await preparar();
        await esperarEncendido(gps);
        expect(leerMarca(claves)).toMatchObject({ usuarioId: 5, empresaId: 1, base: "iris-campo-5-1.db" });
    });

    it("«Dejar de compartir ubicación» apaga el GPS y borra la marca", async () => {
        const { gps, claves } = await preparar();
        await esperarEncendido(gps);
        await fireEvent.press(screen.getByRole("button", { name: /Dejar de compartir ubicación/ }));
        await waitFor(() => expect(gps.corriendo).toBe(false));
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
        expect(screen.getByTestId("estado-ubicacion")).toHaveTextContent(/Dejaste de compartir la ubicación/);
        expect(claves.leer(clavePausaManual(5, 1))).toBe("1");
    });

    it("sesión vencida (401): apaga el GPS aunque la orden siga en curso, y al salir sigue apagado", async () => {
        const { gps, claves, salidas } = await preparar(true);
        await waitFor(() => expect(salidas).toContain("expirada"));
        await waitFor(() => expect(gps.corriendo).toBe(false));
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
        expect(screen.getByText("Fuera")).toBeTruthy();
    });

    it("sesión de otra persona (verificarIdentidad): sale y apaga el GPS", async () => {
        const { gps, claves, salidas, ajena } = await preparar();
        await esperarEncendido(gps);
        await act(async () => {
            ajena();
        });
        expect(salidas).toEqual(["expirada"]);
        await waitFor(() => expect(gps.corriendo).toBe(false));
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("cambiar de empresa: apaga el GPS y borra la marca", async () => {
        const { gps, claves, salidas } = await preparar();
        await esperarEncendido(gps);
        await fireEvent.press(screen.getByRole("button", { name: "Cambiar de empresa" }));
        expect(salidas).toEqual(["empresa"]);
        await waitFor(() => expect(gps.corriendo).toBe(false));
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("la orden se canceló mientras la app estaba cerrada: se apaga en la primera sincronización", async () => {
        const { m, gps, claves } = await preparar();
        await esperarEncendido(gps);
        m.transporte.cambios = (cursor) => ({ cambios: [{ tipo: "cancelada", ordenId: 1, orden: ordenDePrueba(1, { estado: "cancelada" }) }], cursor, hayMas: false });
        await act(() => m.motor.sincronizar());
        expect(m.motor.obtenerOrden(1)?.estado).toBe("cancelada");
        // Solo una bajada del paquete al abrir (no una por render).
        expect(m.transporte.llamadas.filter((l) => l.tipo === "paquete")).toHaveLength(2);
        await waitFor(() => expect(gps.corriendo).toBe(false));
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });

    it("abrir sin señal y sin marca: no enciende hasta que el servidor responda en esta sesión", async () => {
        const { m, gps } = await preparar(false, true);
        await waitFor(() => expect(screen.getByTestId("estado-ubicacion")).toHaveTextContent(/se enciende al conectar con el servidor/));
        expect(gps.corriendo).toBe(false);
        m.transporte.sinRed = false;
        await act(() => m.motor.sincronizar());
        await esperarEncendido(gps);
    });
});

describe("401 después de un GET /yo correcto", () => {
    const error401 = () => new ErrorTransporte("http", "No autenticado", 401, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 });

    it("401 del motor (lote o cambios): olvida la sesión guardada y apaga el GPS con la app abierta", async () => {
        const { m, gps, claves, deps, salidas } = await preparar();
        await esperarEncendido(gps);
        m.transporte.fallar(5, error401());
        await act(() => m.motor.sincronizar().catch(() => undefined));
        await waitFor(() => expect(deps.olvidarSesionGuardada).toHaveBeenCalled());
        await waitFor(() => expect(gps.corriendo).toBe(false));
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
        expect(salidas).toEqual([]);
    });

    it("401 de la cola de posiciones: olvida la sesión guardada, apaga el GPS y borra la marca", async () => {
        const { gps, claves, deps, posiciones, transportePosiciones } = await preparar();
        await esperarEncendido(gps);
        transportePosiciones.fallar(1, error401());
        await act(async () => {
            await posiciones.agregar({ lat: 14.6, lon: -90.5, registradaEn: new Date().toISOString() });
            await posiciones.enviar().catch(() => undefined);
        });
        await waitFor(() => expect(posiciones.obtenerEstado().detenida).toBe(true));
        await waitFor(() => expect(deps.olvidarSesionGuardada).toHaveBeenCalled());
        await waitFor(() => expect(gps.corriendo).toBe(false));
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
    });
});

describe("pantalla de error de la raíz", () => {
    it("ante un error de render apaga el GPS y borra la marca en el acto, y «Reintentar» vuelve a dibujar", async () => {
        const claves = clavesMemoria();
        claves.escribir(CLAVE_RASTREO, JSON.stringify({ usuarioId: 5, empresaId: 1, base: "b.db", hasta: Number.MAX_SAFE_INTEGER, vencida: false }));
        const detener = jest.fn(async () => undefined);
        let falla = true;
        function Fragil() {
            if (falla) {
                throw new Error("pantalla rota");
            }
            return <Text>Bien</Text>;
        }
        const errorConsola = jest.spyOn(console, "error").mockImplementation(() => undefined);
        await render(
            <ProveedorTema>
                <RecuperarError claves={() => claves} gps={{ detener }}>
                    <Fragil />
                </RecuperarError>
            </ProveedorTema>,
        );
        expect(await screen.findByTestId("pantalla-error")).toHaveTextContent(/se apagó la ubicación/);
        expect(claves.leer(CLAVE_RASTREO)).toBeNull();
        expect(detener).toHaveBeenCalled();
        falla = false;
        await fireEvent.press(screen.getByRole("button", { name: "Reintentar" }));
        expect(await screen.findByText("Bien")).toBeTruthy();
        errorConsola.mockRestore();
    });
});
