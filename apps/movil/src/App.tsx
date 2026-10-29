// Raíz de la app de campo nativa: sesión (con la regla sin señal de la PWA) → empresa → motor del
// usuario y la empresa → pantallas. El motor vive mientras la sesión esté abierta: bucle de envío,
// red (expo-network), volver a la app (AppState) y GPS en segundo plano según las órdenes en curso.
import type { Schema } from "@iris/contratos";
import { ErrorTransporte } from "@iris/sync";
import * as Network from "expo-network";
import { StatusBar } from "expo-status-bar";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, AppState, ScrollView, StyleSheet, View } from "react-native";
import { SafeAreaProvider, useSafeAreaInsets } from "react-native-safe-area-context";
import { ErrorApi } from "./api/errores";
import { Aviso, Boton, Pila } from "./componentes";
import { ProveedorCampo, useEstadoMotor, type CerrarSesion, type ServiciosNativos } from "./campo/contexto";
import { tomarFoto } from "./campo/foto";
import { gpsNativo, posicionSiPermitida } from "./campo/gps-nativo";
import { abrirMotor, borrarBase, type MotorAbierto } from "./campo/motor";
import { ordenesEnCurso, RastreoUbicacion, registrarReceptor, type EstadoRastreo } from "./campo/ubicacion";
import { entorno } from "./entorno";
import { ElegirEmpresa } from "./pantallas/ElegirEmpresa";
import { Entrar } from "./pantallas/Entrar";
import { Marco } from "./pantallas/Marco";
import {
    cargarCsrf,
    cerrarSesion as cerrarSesionApi,
    elegirEmpresa,
    empresaDeTrabajo,
    empresaElegida,
    guardarSesion,
    leerYo,
    sesionDesdeYo,
    sesionGuardada,
    usableSinSenal,
    type SesionCampo,
} from "./sesion/sesion";
import { ProveedorTema, useTema } from "./tema/tema";

type Paso =
    | { tipo: "cargando" }
    | { tipo: "entrar"; motivo?: "expirada" | "salida" }
    | { tipo: "aviso"; titulo: string; detalle: string }
    | { tipo: "empresa"; sesion: SesionCampo }
    | { tipo: "campo"; sesion: SesionCampo; empresa: number | null };

let enLinea = true;

const servicios: ServiciosNativos = { tomarFoto, posicion: posicionSiPermitida, enLinea: () => enLinea };

export default function App() {
    return (
        <SafeAreaProvider>
            <ProveedorTema>
                <Raiz />
            </ProveedorTema>
        </SafeAreaProvider>
    );
}

function Raiz() {
    const { oscuro } = useTema();
    const [paso, setPaso] = useState<Paso>({ tipo: "cargando" });

    const continuarCon = useCallback((sesion: SesionCampo) => {
        const empresa = empresaDeTrabajo(sesion, empresaElegida(sesion.usuarioId));
        setPaso(empresa === undefined ? { tipo: "empresa", sesion } : { tipo: "campo", sesion, empresa });
    }, []);

    /** GET /yo con la sesión del teléfono; sin señal abre con la guardada si es reciente. */
    const verificar = useCallback(async () => {
        await cargarCsrf();
        const guardada = sesionGuardada();
        try {
            const yo = await leerYo(guardada ? empresaElegida(guardada.usuarioId) : null);
            entrarCon(yo);
        } catch (e) {
            if (e instanceof ErrorApi && e.estado === 0 && guardada && usableSinSenal(guardada)) {
                continuarCon(guardada);
                return;
            }
            if (e instanceof ErrorApi && e.estado === 0) {
                setPaso({ tipo: "aviso", titulo: "Conéctate para entrar", detalle: "Sin señal solo se abre con una sesión verificada en las últimas horas." });
                return;
            }
            setPaso({ tipo: "entrar", ...(guardada ? { motivo: "expirada" as const } : {}) });
        }
        function entrarCon(yo: Schema<"Yo">) {
            if (yo.usuario.cambioContrasenaRequerido) {
                setPaso({ tipo: "aviso", titulo: "Tienes que cambiar tu contraseña", detalle: "Entra al panel de Iris desde un navegador, cambia la contraseña temporal y vuelve." });
                return;
            }
            const sesion = sesionDesdeYo(yo);
            guardarSesion(sesion);
            continuarCon(sesion);
        }
    }, [continuarCon]);

    useEffect(() => {
        const abrir = () => void verificar();
        abrir();
    }, [verificar]);

    const salir = useCallback((motivo: "expirada" | "salida") => setPaso({ tipo: "entrar", motivo }), []);

    let contenido;
    switch (paso.tipo) {
        case "cargando":
            contenido = <Centro><ActivityIndicator accessibilityLabel="Abriendo Iris" /></Centro>;
            break;
        case "entrar":
            contenido = (
                <Formulario>
                    <Entrar
                        {...(paso.motivo ? { motivo: paso.motivo } : {})}
                        alEntrar={() => {
                            setPaso({ tipo: "cargando" });
                            void verificar();
                        }}
                    />
                </Formulario>
            );
            break;
        case "aviso":
            contenido = (
                <Formulario>
                    <Aviso tono="warning" titulo={paso.titulo} accion={<Boton alPresionar={() => void verificar()}>Reintentar</Boton>}>
                        {paso.detalle}
                    </Aviso>
                </Formulario>
            );
            break;
        case "empresa":
            contenido = (
                <Formulario>
                    <ElegirEmpresa
                        sesion={paso.sesion}
                        alSalir={() => void cerrarSesionApi(paso.sesion.usuarioId).finally(() => setPaso({ tipo: "entrar", motivo: "salida" }))}
                        alElegir={(id) => {
                            elegirEmpresa(paso.sesion.usuarioId, id);
                            setPaso({ tipo: "campo", sesion: paso.sesion, empresa: id });
                        }}
                    />
                </Formulario>
            );
            break;
        case "campo":
            contenido = (
                <AppCampo
                    key={`${paso.sesion.usuarioId}-${paso.empresa ?? 0}`}
                    sesion={paso.sesion}
                    empresa={paso.empresa}
                    alSalir={salir}
                    cambiarEmpresa={
                        paso.sesion.empresas.length > 1
                            ? () => {
                                  elegirEmpresa(paso.sesion.usuarioId, null);
                                  setPaso({ tipo: "empresa", sesion: paso.sesion });
                              }
                            : undefined
                    }
                />
            );
            break;
    }
    return (
        <>
            <StatusBar style={oscuro ? "light" : "dark"} />
            {contenido}
        </>
    );
}

function Centro({ children }: { children: React.ReactNode }) {
    const { colores } = useTema();
    return <View style={[estilos.centro, { backgroundColor: colores.fondo }]}>{children}</View>;
}

function Formulario({ children }: { children: React.ReactNode }) {
    const { colores } = useTema();
    const margen = useSafeAreaInsets();
    return (
        <ScrollView style={{ backgroundColor: colores.fondo }} contentContainerStyle={[estilos.formulario, { paddingTop: margen.top + 32 }]} keyboardShouldPersistTaps="handled">
            <Pila espacio={16}>{children}</Pila>
        </ScrollView>
    );
}

/** Abre el motor del usuario y la empresa, y lo cierra al salir. */
function AppCampo({
    sesion,
    empresa,
    alSalir,
    cambiarEmpresa,
}: {
    sesion: SesionCampo;
    empresa: number | null;
    alSalir: (motivo: "expirada" | "salida") => void;
    cambiarEmpresa: (() => void) | undefined;
}) {
    const [abierto, setAbierto] = useState<MotorAbierto | null>(null);
    const [error, setError] = useState<string | null>(null);
    const actual = useRef<MotorAbierto | null>(null);

    useEffect(() => {
        let vivo = true;
        abrirMotor({ usuarioId: sesion.usuarioId, empresaId: empresa }, () => alSalir("expirada"))
            .then((m) => {
                if (!vivo) {
                    m.motor.detener();
                    m.posiciones.detener();
                    void m.almacen.cerrar();
                    return;
                }
                actual.current = m;
                setAbierto(m);
            })
            .catch((e: unknown) => setError(e instanceof Error ? e.message : "No se pudo abrir la base del teléfono"));
        return () => {
            vivo = false;
            const m = actual.current;
            actual.current = null;
            if (m) {
                registrarReceptor(null);
                m.motor.detener();
                m.posiciones.detener();
                void m.almacen.cerrar();
            }
        };
    }, [sesion.usuarioId, empresa, alSalir]);

    if (error) {
        return (
            <Formulario>
                <Aviso tono="error" titulo="No se pudo abrir la base del teléfono">
                    {error}
                </Aviso>
            </Formulario>
        );
    }
    if (!abierto) {
        return (
            <Centro>
                <ActivityIndicator accessibilityLabel="Abriendo tus órdenes" />
            </Centro>
        );
    }
    return <CampoAbierto abierto={abierto} sesion={sesion} empresa={empresa} alSalir={alSalir} cambiarEmpresa={cambiarEmpresa} />;
}

function CampoAbierto({
    abierto,
    sesion,
    empresa,
    alSalir,
    cambiarEmpresa,
}: {
    abierto: MotorAbierto;
    sesion: SesionCampo;
    empresa: number | null;
    alSalir: (motivo: "expirada" | "salida") => void;
    cambiarEmpresa: (() => void) | undefined;
}) {
    const { motor, posiciones } = abierto;
    const estado = useEstadoMotor(motor);
    const [estadoRastreo, setEstadoRastreo] = useState<EstadoRastreo>({ tipo: "fuera_de_curso" });
    const rastreo = useMemo(
        () => new RastreoUbicacion({ gps: gpsNativo, claves: entorno().claves, usuarioId: sesion.usuarioId, base: abierto.nombre, alCambiar: setEstadoRastreo }),
        [abierto.nombre, sesion.usuarioId],
    );

    // Bucle de envío, red y volver a la app.
    useEffect(() => {
        motor.iniciar();
        posiciones.iniciar();
        registrarReceptor((p) => posiciones.agregar(p));
        const aplicarRed = (s: Network.NetworkState) => {
            enLinea = s.isConnected === true && s.isInternetReachable !== false;
            motor.establecerConexion(enLinea);
            if (enLinea) {
                posiciones.reintentarAhora();
            }
        };
        void Network.getNetworkStateAsync().then(aplicarRed).catch(() => undefined);
        const red = Network.addNetworkStateListener(aplicarRed);
        const app = AppState.addEventListener("change", (s) => {
            if (s === "active") {
                motor.reintentarAhora();
                posiciones.reintentarAhora();
            }
        });
        return () => {
            red.remove();
            app.remove();
        };
    }, [motor, posiciones]);

    // GPS en segundo plano: solo con órdenes en curso (pausada no cuenta).
    const enCurso = ordenesEnCurso(estado.ordenes);
    useEffect(() => {
        void rastreo.actualizar(enCurso);
    }, [rastreo, enCurso]);

    const cerrarSesion = useCallback<CerrarSesion>(
        async ({ conservarPendientes }) => {
            await rastreo.detener();
            registrarReceptor(null);
            if (conservarPendientes) {
                await motor.olvidarDatosDelServidor();
            } else {
                await borrarBase(abierto);
            }
            try {
                await cerrarSesionApi(sesion.usuarioId);
            } catch (e) {
                // La API respondió con otro error: la sesión del teléfono se cierra igual.
                if (!(e instanceof ErrorApi || e instanceof ErrorTransporte)) {
                    throw e;
                }
            }
            alSalir("salida");
        },
        [rastreo, motor, abierto, sesion.usuarioId, alSalir],
    );

    const nombreEmpresa = empresa === null ? null : (sesion.empresas.find((e) => e.id === empresa)?.nombre ?? null);
    return (
        <ProveedorCampo
            motor={motor}
            posiciones={posiciones}
            rastreo={rastreo}
            estadoRastreo={estadoRastreo}
            sesion={sesion}
            servicios={servicios}
            cerrarSesion={cerrarSesion}
        >
            <Marco empresa={nombreEmpresa} alSesionVencida={() => alSalir("expirada")} {...(cambiarEmpresa ? { cambiarEmpresa } : {})} />
        </ProveedorCampo>
    );
}

const estilos = StyleSheet.create({
    centro: { flex: 1, alignItems: "center", justifyContent: "center" },
    formulario: { padding: 16, paddingBottom: 48 },
});
