// Raíz de la app de campo nativa: sesión (con la regla sin señal de la PWA) → empresa → motor del
// usuario y la empresa → pantallas. El motor vive mientras la sesión esté abierta: bucle de envío,
// red (expo-network), volver a la app (AppState) y GPS en segundo plano según las órdenes en curso.
import type { Schema } from "@iris/contratos";
import * as Network from "expo-network";
import { StatusBar } from "expo-status-bar";
import { useCallback, useEffect, useState } from "react";
import { AppState, ScrollView, StyleSheet, View } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider, useSafeAreaInsets } from "react-native-safe-area-context";
import "./global.css";
import { ErrorApi } from "./api/errores";
import { Aviso, Boton, Cargando, Pila, useAltoTeclado } from "./componentes";
import type { ServiciosNativos } from "./campo/contexto";
import { tomarFoto, vaciarCacheFotos } from "./campo/foto";
import { gpsNativo, posicionSiPermitida } from "./campo/gps-nativo";
import { abrirMotor, borrarBase } from "./campo/motor";
import { entorno } from "./entorno";
import { AppCampo, type DependenciasCampo } from "./pantallas/AppCampo";
import { ElegirEmpresa } from "./pantallas/ElegirEmpresa";
import { RecuperarError } from "./pantallas/RecuperarError";
import { Entrar } from "./pantallas/Entrar";
import { avisoSinConexion } from "@iris/sync/campo";
import {
    autenticarConHuella,
    cargarCsrf,
    marcarEntradaConContrasena,
    olvidarUsuarioRecordado,
    pedirHuella,
    recordarUsuario,
    usuarioRecordado,
    cerrarSesion as cerrarSesionApi,
    reintentarCierrePendiente,
    elegirEmpresa,
    empresaDeTrabajo,
    empresaElegida,
    guardarSesion,
    leerYo,
    olvidarSesionGuardada,
    sesionDesdeYo,
    sesionGuardada,
    usableSinSenal,
    type SesionCampo,
} from "./sesion/sesion";
import { ProveedorTema, useTema } from "./tema/tema";

type Paso =
    | { tipo: "cargando" }
    | { tipo: "entrar"; motivo?: "expirada" | "salida"; huella?: boolean; sinConexion?: { titulo: string; detalle: string } | null }
    | { tipo: "aviso"; titulo: string; detalle: string }
    | { tipo: "empresa"; sesion: SesionCampo }
    | { tipo: "campo"; sesion: SesionCampo; empresa: number | null };

let enLinea = true;

const servicios: ServiciosNativos = { tomarFoto, posicion: posicionSiPermitida, enLinea: () => enLinea };

const conInternet = (s: Network.NetworkState) => s.isConnected === true && s.isInternetReachable !== false;

/** Lo nativo de la sesión de campo (en pruebas se reemplaza). Se crea una vez: el motor depende de esto. */
let depsNativas: DependenciasCampo | null = null;
function dependenciasNativas(): DependenciasCampo {
    depsNativas ??= {
        async abrirMotor(clave, alSesionAjena, alConfirmarServidor) {
            const m = await abrirMotor(clave, alSesionAjena, alConfirmarServidor);
            return { motor: m.motor, posiciones: m.posiciones, inventario: m.inventario, agenda: m.agenda, nombre: m.nombre, cerrar: () => m.almacen.cerrar(), borrar: () => borrarBase(m) };
        },
        gps: gpsNativo,
        claves: entorno().claves,
        servicios,
        red: {
            leer: async () => {
                enLinea = conInternet(await Network.getNetworkStateAsync());
                return enLinea;
            },
            escuchar: (fn) => {
                const s = Network.addNetworkStateListener((e) => {
                    enLinea = conInternet(e);
                    fn(enLinea);
                });
                return () => s.remove();
            },
        },
        alVolverAlFrente: (fn) => {
            const s = AppState.addEventListener("change", (e) => e === "active" && fn());
            return () => s.remove();
        },
        cerrarSesionApi,
        vaciarCacheFotos,
        olvidarSesionGuardada,
    };
    return depsNativas;
}

/**
 * `GestureHandlerRootView` va por fuera de todo (lo pide HeroUI Native y Gesture Handler 3); dentro,
 * el área segura y el tema (HeroUI con Horus).
 */
export default function App() {
    return (
        <GestureHandlerRootView style={estilos.raiz}>
            <SafeAreaProvider>
                <ProveedorTema>
                    <RecuperarError claves={() => entorno().claves} gps={gpsNativo}>
                        <Raiz />
                    </RecuperarError>
                </ProveedorTema>
            </SafeAreaProvider>
        </GestureHandlerRootView>
    );
}

function Raiz() {
    const { oscuro } = useTema();
    const [paso, setPaso] = useState<Paso>({ tipo: "cargando" });
    // La huella desbloquea la sesión guardada una vez por apertura de la app.
    const [desbloqueada, setDesbloqueada] = useState(false);

    const continuarCon = useCallback((sesion: SesionCampo) => {
        const empresa = empresaDeTrabajo(sesion, empresaElegida(sesion.usuarioId));
        setPaso(empresa === undefined ? { tipo: "empresa", sesion } : { tipo: "campo", sesion, empresa });
    }, []);

    /** GET /yo con la sesión del teléfono; sin señal abre con la guardada si es reciente. */
    const verificar = useCallback(async () => {
        await cargarCsrf();
        // Se cerró sesión sin señal: antes de cualquier GET /yo se cierra en la API; mientras no se
        // pueda, nada entra solo (la cookie vieja no abre la app).
        if (!(await reintentarCierrePendiente())) {
            setPaso({ tipo: "entrar", motivo: "salida" });
            return;
        }
        const guardada = sesionGuardada();
        // Con biometría y la sesión del usuario recordado guardada: primero la huella (o la contraseña).
        if (!desbloqueada && (await pedirHuella(guardada))) {
            const red = await Network.getNetworkStateAsync().catch(() => null);
            const sinRed = red !== null && !conInternet(red);
            setPaso({ tipo: "entrar", huella: true, sinConexion: sinRed && guardada ? avisoSinConexion(usableSinSenal(guardada)) : null });
            return;
        }
        try {
            const yo = await leerYo(guardada ? empresaElegida(guardada.usuarioId) : null);
            entrarCon(yo);
        } catch (e) {
            if (e instanceof ErrorApi && e.estado === 0 && guardada && usableSinSenal(guardada)) {
                continuarCon(guardada);
                return;
            }
            if (e instanceof ErrorApi && e.estado === 0) {
                setPaso({ tipo: "entrar", sinConexion: avisoSinConexion(false) });
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
            recordarUsuario({ nombre: sesion.nombre, correo: sesion.correo });
            continuarCon(sesion);
        }
    }, [continuarCon, desbloqueada]);

    useEffect(() => {
        const abrir = () => void verificar();
        abrir();
    }, [verificar]);

    const salir = useCallback((motivo: "expirada" | "salida") => {
        if (motivo === "expirada") {
            // Sesión vencida o de otra persona: no se vuelve a abrir sin señal con la guardada.
            olvidarSesionGuardada();
        }
        setPaso({ tipo: "entrar", motivo });
    }, []);

    let contenido;
    switch (paso.tipo) {
        case "cargando":
            contenido = (
                <Centro>
                    <Cargando etiqueta="Abriendo Iris" />
                </Centro>
            );
            break;
        case "entrar":
            contenido = (
                <Formulario>
                    <Entrar
                        {...(paso.motivo ? { motivo: paso.motivo } : {})}
                        recordado={usuarioRecordado()}
                        alOlvidarRecordado={olvidarUsuarioRecordado}
                        sinConexion={paso.sinConexion ?? null}
                        huella={
                            paso.huella
                                ? async () => {
                                      if (await autenticarConHuella()) {
                                          setDesbloqueada(true);
                                          setPaso({ tipo: "cargando" });
                                      }
                                  }
                                : null
                        }
                        alEntrar={() => {
                            // Entró con la contraseña: el primer aviso puede ofrecer la huella (una vez).
                            marcarEntradaConContrasena();
                            setPaso({ tipo: "cargando" });
                            // Con contraseña ya no se pide la huella; si ya estaba desbloqueada, verifica de nuevo.
                            if (desbloqueada) {
                                void verificar();
                            } else {
                                setDesbloqueada(true);
                            }
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
                    deps={dependenciasNativas()}
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
    const { campo } = useTema();
    const margen = useSafeAreaInsets();
    // Con el teclado abierto el contenido sube por encima (edge-to-edge: la ventana no se encoge sola).
    const teclado = useAltoTeclado();
    return (
        <ScrollView
            style={{ backgroundColor: campo.lienzo }}
            contentContainerStyle={[estilos.formulario, { paddingTop: margen.top + 32, paddingBottom: Math.max(48, margen.bottom + 24) + teclado }]}
            keyboardShouldPersistTaps="handled"
        >
            <Pila espacio={16}>{children}</Pila>
        </ScrollView>
    );
}

const estilos = StyleSheet.create({
    raiz: { flex: 1 },
    centro: { flex: 1, alignItems: "center", justifyContent: "center" },
    formulario: { paddingHorizontal: 20 },
});
