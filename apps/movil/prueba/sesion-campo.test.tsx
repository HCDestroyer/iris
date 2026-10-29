// Ciclo de vida de la sesión de campo y el GPS en segundo plano (revisión de seguridad de la PR #39):
// toda salida apaga el GPS y borra la marca (sesión vencida, sesión ajena, cambiar de empresa) y el
// técnico puede «Dejar de compartir ubicación».
import { ColaPosiciones, ErrorTransporte } from "@iris/sync";
import { montar, ordenDePrueba, paqueteDePrueba, TransportePosicionesFalso } from "@iris/sync/pruebas";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react-native";
import { useState } from "react";
import { Text } from "react-native";
import { CLAVE_RASTREO, claveExplicacion, leerMarca, type GpsFondo, type PermisosGps } from "../src/campo/ubicacion";
import { AppCampo, type DependenciasCampo } from "../src/pantallas/AppCampo";
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

async function preparar(fallaDeSesion = false) {
    const m = await montar();
    m.transporte.paquete = (fecha) => ({ ...paqueteDePrueba(fecha), ordenes: [ordenDePrueba(1, { estado: "en_camino" })] });
    await m.motor.cargarPaquete("2026-10-28");
    if (fallaDeSesion) {
        m.transporte.fallar(20, new ErrorTransporte("http", "No autenticado", 401, { type: "urn:iris:problema:no_autenticado", title: "No autenticado", status: 401 }));
    }
    const posiciones = await ColaPosiciones.abrir({ almacen: m.almacen, transporte: new TransportePosicionesFalso(m.reloj), reloj: m.reloj });
    const gps = new GpsFalso();
    const claves = clavesMemoria();
    claves.escribir(claveExplicacion(5), "1");
    const salidas: string[] = [];
    let sesionAjena: (() => void) | null = null;
    const deps: DependenciasCampo = {
        abrirMotor: async (_clave, alSesionAjena) => {
            sesionAjena = alSesionAjena;
            return { motor: m.motor, posiciones, nombre: "iris-campo-5-1.db", cerrar: async () => undefined, borrar: async () => undefined };
        },
        gps,
        claves,
        servicios: { tomarFoto: async () => null, posicion: async () => null, enLinea: () => true },
        red: { leer: async () => true, escuchar: () => () => undefined },
        alVolverAlFrente: () => () => undefined,
        cerrarSesionApi: async () => undefined,
        vaciarCacheFotos: jest.fn(),
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
    return { m, gps, claves, salidas, ajena: () => sesionAjena?.() };
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
});
