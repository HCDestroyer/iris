// Raíz de la app de campo nativa: sesión (con la regla sin señal de la PWA) → empresa → motor del
// usuario y la empresa → pantallas. El motor vive mientras la sesión esté abierta: bucle de envío,
// red (expo-network), volver a la app (AppState) y GPS en segundo plano según las órdenes en curso.
import * as Network from "expo-network";
import { StatusBar } from "expo-status-bar";
import { useCallback, useEffect, useState } from "react";
import { AppState, BackHandler, ScrollView, StyleSheet, View } from "react-native";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { KeyboardProvider } from "react-native-keyboard-controller";
import { SafeAreaProvider, useSafeAreaInsets } from "react-native-safe-area-context";
import "./global.css";
import { AvisoEmergente, Cargando, Pila, useAltoTeclado, type ContenidoAvisoEmergente } from "./componentes";
import type { ServiciosNativos } from "./campo/contexto";
import { tomarFoto, vaciarCacheFotos } from "./campo/foto";
import { gpsNativo, posicionSiPermitida, ubicacionPermitida } from "./campo/gps-nativo";
import { useVolverTrasAusencia } from "./campo/bloqueo";
import { useAtrasAndroid } from "./campo/atras";
import { escucharPlano } from "./campo/plano";
import { abrirMotor, borrarBase, borrarCachesChat } from "./campo/motor";
import { entorno } from "./entorno";
import { AppCampo, type DependenciasCampo } from "./pantallas/AppCampo";
import { pushNativo } from "./push";
import { cerrarPushDeSesion, olvidarPushLocal } from "./push/locales";
import { CrearContrasena } from "./pantallas/Bloqueo";
import { ElegirEmpresa } from "./pantallas/ElegirEmpresa";
import { RecuperarError } from "./pantallas/RecuperarError";
import { Entrar } from "./pantallas/Entrar";
import { avisoSinConexion } from "@iris/sync/campo";
import {
    abrirApp,
    autenticarConHuella,
    noSoyYo,
    marcarEntradaConContrasena,
    olvidarUsuarioRecordado,
    pedirHuella,
    recordarUsuario,
    usuarioRecordado,
    cerrarSesion as cerrarSesionApi,
    elegirEmpresa,
    empresaDeTrabajo,
    empresaElegida,
    guardarSesion,
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
    | { tipo: "contrasena"; nombre: string; usuarioId: number }
    /** `desde`: la empresa con la que trabajaba si llegó por «Cambiar empresa» («Atrás» vuelve a ella). */
    | { tipo: "empresa"; sesion: SesionCampo; desde?: number | null }
    | { tipo: "campo"; sesion: SesionCampo; empresa: number | null };

let enLinea = true;

const servicios: ServiciosNativos = { tomarFoto, posicion: posicionSiPermitida, enLinea: () => enLinea, ubicacionPermitida };

const conInternet = (s: Network.NetworkState) => s.isConnected === true && s.isInternetReachable !== false;

/** Lo nativo de la sesión de campo (en pruebas se reemplaza). Se crea una vez: el motor depende de esto. */
let depsNativas: DependenciasCampo | null = null;
function dependenciasNativas(): DependenciasCampo {
    depsNativas ??= {
        async abrirMotor(clave, alSesionAjena, alConfirmarServidor) {
            const m = await abrirMotor(clave, alSesionAjena, alConfirmarServidor);
            return { motor: m.motor, posiciones: m.posiciones, inventario: m.inventario, agenda: m.agenda, libres: m.libres, historial: m.historial, chat: m.chat, nombre: m.nombre, cerrar: () => m.almacen.cerrar(), borrar: () => borrarBase(m) };
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
        alCambiarPlano: escucharPlano,
        cerrarSesionApi,
        vaciarCacheFotos,
        olvidarSesionGuardada,
        push: pushNativo(),
    };
    return depsNativas;
}

/**
 * «¿No eres tú?» / «Usar otra cuenta»: cierra la sesión del teléfono por completo (en la API o como
 * cierre pendiente si no hay señal), olvida al usuario recordado y la caché de su chat (no la cola).
 */
export async function cerrarParaOtraCuenta(usuarioId?: number): Promise<void> {
    // La caché del chat de esa sesión también se va (no la cola): se toma antes de olvidarla.
    const guardada = sesionGuardada();
    // Lo mismo que el cierre normal (`cerrarSesion` de AppCampo): las fotos en caché y lo local de
    // push (lo pendiente de esta persona se conserva) y sus dispositivos, con la sesión aún abierta.
    vaciarCacheFotos();
    const id = usuarioId ?? guardada?.usuarioId;
    const push = dependenciasNativas().push;
    if (id !== undefined) {
        await cerrarPushDeSesion({ servicio: push?.servicio ?? null, registro: push?.registro ?? null, claves: entorno().claves, usuarioId: id, conservarPendientes: true }).catch(() => undefined);
    } else {
        olvidarPushLocal(entorno().claves, 0, false);
    }
    await noSoyYo();
    if (guardada) {
        await borrarCachesChat(guardada.usuarioId, [null, ...guardada.empresas.map((e) => e.id)]).catch(() => undefined);
    }
}

/**
 * `GestureHandlerRootView` va por fuera de todo (lo pide HeroUI Native y Gesture Handler 3); dentro,
 * el área segura, el seguimiento del teclado y el tema (HeroUI con Horus).
 */
export default function App() {
    return (
        <GestureHandlerRootView style={estilos.raiz}>
            <SafeAreaProvider>
                {/* Sigue el teclado con edge-to-edge (también dentro de un `Modal`): `EvitarTeclado`, `useAltoTeclado`. */}
                <KeyboardProvider>
                    <ProveedorTema>
                        <RecuperarError claves={() => entorno().claves} gps={gpsNativo} usarOtraCuenta={cerrarParaOtraCuenta}>
                            <Raiz />
                        </RecuperarError>
                    </ProveedorTema>
                </KeyboardProvider>
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

    /**
     * Regla de apertura (`abrirApp`): cierre pendiente → entrar; con la huella encendida y sesión
     * guardada → la huella (o la contraseña); si no, GET /yo y, sin señal, la guardada si es reciente.
     */
    const verificar = useCallback(async () => {
        const a = await abrirApp(desbloqueada);
        switch (a.tipo) {
            case "entrar":
                setPaso({ tipo: "entrar", ...(a.motivo ? { motivo: a.motivo } : {}) });
                return;
            case "sin_conexion":
                setPaso({ tipo: "entrar", sinConexion: avisoSinConexion(false) });
                return;
            case "guardada":
                continuarCon(a.sesion);
                return;
            case "huella": {
                const red = await Network.getNetworkStateAsync().catch(() => null);
                const sinRed = red !== null && !conInternet(red);
                setPaso({ tipo: "entrar", huella: true, sinConexion: sinRed ? avisoSinConexion(usableSinSenal(a.guardada)) : null });
                return;
            }
            case "yo": {
                const yo = a.yo;
                if (yo.usuario.cambioContrasenaRequerido) {
                    // Contraseña temporal: se cambia aquí mismo (la sesión solo puede eso o salir).
                    setPaso({ tipo: "contrasena", nombre: yo.usuario.nombre.split(" ")[0] ?? "", usuarioId: yo.usuario.id });
                    return;
                }
                const sesion = sesionDesdeYo(yo);
                guardarSesion(sesion);
                recordarUsuario({ nombre: sesion.nombre, correo: sesion.correo });
                continuarCon(sesion);
            }
        }
    }, [continuarCon, desbloqueada]);

    // Con la huella encendida, volver al frente tras más de 5 min fuera vuelve a pedirla (encima
    // de la app abierta: el motor y el GPS siguen).
    const [bloqueada, setBloqueada] = useState(false);
    useVolverTrasAusencia(paso.tipo === "campo" || paso.tipo === "empresa", () => {
        void pedirHuella(sesionGuardada()).then((pedir) => pedir && setBloqueada(true));
    });

    /** «¿No eres tú?» con una sesión guardada: la cierra por completo y pide entrar. */
    const noEsEl = useCallback(async (usuarioId?: number) => {
        await cerrarParaOtraCuenta(usuarioId);
        setBloqueada(false);
        setDesbloqueada(false);
        setPaso({ tipo: "entrar", motivo: "salida" });
    }, []);

    useEffect(() => {
        const abrir = () => void verificar();
        abrir();
    }, [verificar]);

    // Aviso emergente de las pantallas de arranque («Contraseña guardada»): se va solo.
    const [aviso, setAviso] = useState<ContenidoAvisoEmergente | null>(null);
    useEffect(() => {
        if (!aviso) {
            return undefined;
        }
        const t = setTimeout(() => setAviso(null), 4000);
        return () => clearTimeout(t);
    }, [aviso]);
    const cerrarAviso = useCallback(() => setAviso(null), []);

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
                        alOlvidarRecordado={() => {
                            // Con una sesión guardada en el teléfono, «¿No eres tú?» la cierra entera
                            // (si no, el bloqueo con huella se saltaría al reabrir).
                            if (sesionGuardada() || paso.huella) {
                                void noEsEl();
                            } else {
                                olvidarUsuarioRecordado();
                            }
                        }}
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
        case "contrasena":
            contenido = (
                <Formulario>
                    <CrearContrasena
                        nombre={paso.nombre}
                        alUsarOtraCuenta={() => noEsEl(paso.usuarioId)}
                        alSesionCerrada={() => salir("expirada")}
                        alListo={() => {
                            // La sesión sigue (la API solo cierra las otras): entra directo.
                            setAviso({ tono: "success", texto: "Contraseña guardada" });
                            setPaso({ tipo: "cargando" });
                            void verificar();
                        }}
                    />
                </Formulario>
            );
            break;
        case "empresa":
            contenido = (
                <Formulario>
                    <ElegirEmpresa
                        sesion={paso.sesion}
                        alSalir={() => noEsEl(paso.sesion.usuarioId)}
                        {...(paso.desde !== undefined
                            ? {
                                  // Llegó por «Cambiar empresa»: «Atrás» vuelve a trabajar con la de antes.
                                  alVolver: () => {
                                      const desde = paso.desde ?? null;
                                      elegirEmpresa(paso.sesion.usuarioId, desde);
                                      setPaso({ tipo: "campo", sesion: paso.sesion, empresa: desde });
                                  },
                              }
                            : {})}
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
                    alExigirContrasena={() => {
                        // Con una temporal la sesión guardada ya no abre la app sin señal.
                        olvidarSesionGuardada();
                        setPaso({ tipo: "contrasena", nombre: paso.sesion.nombre.split(" ")[0] ?? "", usuarioId: paso.sesion.usuarioId });
                    }}
                    alUsarOtraCuenta={() => noEsEl(paso.sesion.usuarioId)}
                    deps={dependenciasNativas()}
                    cambiarEmpresa={
                        paso.sesion.empresas.length > 1
                            ? () => {
                                  elegirEmpresa(paso.sesion.usuarioId, null);
                                  setPaso({ tipo: "empresa", sesion: paso.sesion, desde: paso.empresa });
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
            <AvisoEmergente aviso={aviso} alCerrar={cerrarAviso} />
            {bloqueada ? (
                <View style={StyleSheet.absoluteFill} testID="bloqueo-huella">
                    <AtrasBloqueoHuella />
                    <Formulario>
                        <Entrar
                            recordado={usuarioRecordado()}
                            alOlvidarRecordado={() => void noEsEl()}
                            huella={async () => {
                                if (await autenticarConHuella()) {
                                    setBloqueada(false);
                                }
                            }}
                            alEntrar={() => setBloqueada(false)}
                        />
                    </Formulario>
                </View>
            ) : null}
        </>
    );
}

/**
 * «Atrás» de Android con la app bloqueada por la huella: sale de la app (sigue bloqueada) sin que
 * la vista de abajo cambie ni se cierre la sesión.
 */
function AtrasBloqueoHuella() {
    useAtrasAndroid(
        useCallback(() => {
            BackHandler.exitApp();
            return true;
        }, []),
    );
    return null;
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
