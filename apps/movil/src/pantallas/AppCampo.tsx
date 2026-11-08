// Sesión de campo abierta: abre el motor del usuario y la empresa, lo conecta a la red y a la app
// (volver al frente), enciende el GPS en segundo plano solo con órdenes en curso y lo apaga en
// **toda** salida: desmontar (sesión vencida, sesión ajena, cambiar de empresa), cerrar sesión o
// perder la sesión con la app abierta. Lo nativo llega por `DependenciasCampo` (las pruebas lo
// reemplazan).
import { ErrorTransporte, type ColaPosiciones, type Conexion, type InventarioCampo, type LibresCampo, type MotorSync } from "@iris/sync";
import type { MotorChat } from "@iris/sync/chat";
import { ProveedorChat } from "../campo/chat";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ScrollView, StyleSheet, View } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";
import { ErrorApi } from "../api/errores";
import { Aviso, Boton, Cargando } from "../componentes";
import { ProveedorAgenda, type LeerAgenda } from "../campo/agenda";
import { ProveedorHistorial, type LeerHistorial } from "../campo/historial";
import { ProveedorInventario } from "../campo/inventario";
import { ProveedorLibres } from "../campo/libres";
import { ProveedorCampo, useEstadoMotor, useEstadoPosiciones, type CerrarSesion, type ServiciosNativos } from "../campo/contexto";
import { ordenesEnCurso, RastreoUbicacion, registrarReceptor, type EstadoRastreo, type GpsFondo } from "../campo/ubicacion";
import type { AlmacenClaves } from "../entorno";
import type { SesionCampo } from "../sesion/sesion";
import { useTema } from "../tema/tema";
import type { ApiPush } from "../push/api";
import { ProveedorPush } from "../push/contexto";
import { cerrarPushDeSesion } from "../push/locales";
import type { RegistroDispositivo } from "../push/registro";
import type { ServicioPush } from "../push/servicio";
import { PantallaBloqueo } from "./Bloqueo";
import { Marco } from "./Marco";
import { PrimerUsoPush } from "./Notificaciones";

export interface MotorDeSesion {
    motor: MotorSync;
    posiciones: ColaPosiciones;
    /** «Por recibir» y «Mi bodega» (lecturas de la API con señal). */
    inventario: InventarioCampo;
    /** Agenda de «Mañana» y «Semana» (lectura de la API con señal). */
    agenda: LeerAgenda;
    /** Chat interno del equipo (misma base). */
    chat: MotorChat;
    /** «Historial del cliente» (lectura de la API con señal). */
    historial?: LeerHistorial;
    /** Órdenes libres y tomar una (con señal). */
    libres: LibresCampo;
    /** Archivo de la base (lo lee la tarea del GPS con la app cerrada). */
    nombre: string;
    /** Cierra la conexión con la base. */
    cerrar: () => Promise<void>;
    /** Cierra y borra la base (cerrar sesión sin conservar). */
    borrar: () => Promise<void>;
}

export interface DependenciasCampo {
    /**
     * `alConfirmarServidor`: el transporte lo llama cada vez que la API responde el paquete o los
     * cambios (antes de que el motor los aplique).
     */
    abrirMotor: (clave: { usuarioId: number; empresaId: number | null }, alSesionAjena: () => void, alConfirmarServidor: () => void) => Promise<MotorDeSesion>;
    gps: GpsFondo;
    claves: AlmacenClaves;
    servicios: ServiciosNativos;
    /** Estado de la red y sus cambios (`true` = hay internet). */
    red: { leer: () => Promise<boolean>; escuchar: (fn: (enLinea: boolean) => void) => () => void };
    /** La app vuelve al frente. */
    alVolverAlFrente: (fn: () => void) => () => void;
    /** La app pasa al frente (`true`) o a segundo plano (`false`): el chat abre o cierra su sondeo. */
    alCambiarPlano?: (fn: (alFrente: boolean) => void) => () => void;
    /** `DELETE /sesion` y olvido local (con «cierre pendiente» si no hay señal). */
    cerrarSesionApi: (usuarioId: number) => Promise<void>;
    /** Borra fotos temporales de la cámara y del compresor. */
    vaciarCacheFotos: () => void;
    /** La API dijo que la sesión ya no vale: olvida la sesión guardada (abrir sin señal, tarea del GPS). */
    olvidarSesionGuardada: () => void;
    /**
     * Notificaciones push (`contratos/push.md`): lo nativo, la API y el registro del teléfono. Sin
     * él (pruebas de otras pantallas), la app no registra ni muestra nada.
     */
    push?: { servicio: ServicioPush | null; api: ApiPush; registro: RegistroDispositivo | null; baseUrl: string; generarId: () => string };
}

export type MotivoSalida = "expirada" | "salida";

/** Con la sesión o la empresa inválidas no se rastrea, aunque la orden siga en curso. */
const vigente = (c: Conexion) => c !== "sin_sesion" && c !== "sin_empresa";

/**
 * Respuestas del servidor (paquete o cambios) de esta sesión de la app, para el GPS: el transporte
 * avisa al recibirlas y la confirmación se toma con el primer estado del motor ya aplicado (sin
 * sincronización en curso).
 */
export class ConfirmacionesServidor {
    private pendiente = false;

    avisar = () => {
        this.pendiente = true;
    };

    /** `true` una sola vez por respuesta, cuando el motor terminó de aplicarla. */
    tomar(sincronizando: boolean): boolean {
        if (!this.pendiente || sincronizando) {
            return false;
        }
        this.pendiente = false;
        return true;
    }
}

/** Lienzo de una pantalla de bloqueo (como el login: desplazable, con el área segura). */
function Bloqueada({ children }: { children: ReactNode }) {
    const { campo } = useTema();
    const margen = useSafeAreaInsets();
    return (
        <ScrollView style={{ backgroundColor: campo.lienzo }} contentContainerStyle={[estilos.bloqueada, { paddingTop: margen.top + 32, paddingBottom: Math.max(48, margen.bottom + 24) }]}>
            {children}
        </ScrollView>
    );
}

function Centro({ children }: { children: ReactNode }) {
    const { colores } = useTema();
    return <View style={[estilos.centro, { backgroundColor: colores.fondo }]}>{children}</View>;
}

export function AppCampo({
    sesion,
    empresa,
    alSalir,
    alExigirContrasena,
    cambiarEmpresa,
    deps,
}: {
    sesion: SesionCampo;
    empresa: number | null;
    alSalir: (motivo: MotivoSalida) => void;
    /** La API pide cambiar la contraseña (temporal puesta con la app abierta). */
    alExigirContrasena?: () => void;
    cambiarEmpresa: (() => void) | undefined;
    deps: DependenciasCampo;
}) {
    const [abierto, setAbierto] = useState<MotorDeSesion | null>(null);
    const [error, setError] = useState<string | null>(null);
    const [intento, setIntento] = useState(0);
    const [confirmaciones] = useState(() => new ConfirmacionesServidor());
    const actual = useRef<MotorDeSesion | null>(null);

    useEffect(() => {
        let vivo = true;
        deps.abrirMotor({ usuarioId: sesion.usuarioId, empresaId: empresa }, () => alSalir("expirada"), confirmaciones.avisar)
            .then((m) => {
                if (!vivo) {
                    m.chat.detener();
                    m.motor.detener();
                    m.posiciones.detener();
                    void m.cerrar();
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
                m.chat.detener();
                m.motor.detener();
                m.posiciones.detener();
                void m.cerrar();
            }
        };
    }, [sesion.usuarioId, empresa, alSalir, deps, confirmaciones, intento]);

    if (error) {
        // Pantalla de bloqueo: «Reintentar» y, siempre, «Usar otra cuenta» (y «Atrás» de Android).
        return (
            <Bloqueada>
                <PantallaBloqueo
                    titulo="No se pudo abrir Iris"
                    explicacion="La base de datos del teléfono no abrió. Lo que guardaste sigue en el teléfono."
                    alUsarOtraCuenta={async () => {
                        await deps.cerrarSesionApi(sesion.usuarioId).catch(() => undefined);
                        alSalir("salida");
                    }}
                    testID="error-base"
                >
                    <Aviso
                        tono="error"
                        titulo="No se pudo abrir la base del teléfono"
                        accion={
                            <Boton
                                variante="secundario"
                                alPresionar={() => {
                                    setError(null);
                                    setIntento((i) => i + 1);
                                }}
                            >
                                Reintentar
                            </Boton>
                        }
                    >
                        {error}
                    </Aviso>
                </PantallaBloqueo>
            </Bloqueada>
        );
    }
    if (!abierto) {
        return (
            <Centro>
                <Cargando etiqueta="Abriendo tus órdenes" />
            </Centro>
        );
    }
    return <CampoAbierto abierto={abierto} sesion={sesion} empresa={empresa} alSalir={alSalir} {...(alExigirContrasena ? { alExigirContrasena } : {})} cambiarEmpresa={cambiarEmpresa} deps={deps} confirmaciones={confirmaciones} />;
}

function CampoAbierto({
    abierto,
    sesion,
    empresa,
    alSalir,
    alExigirContrasena,
    cambiarEmpresa,
    deps,
    confirmaciones,
}: {
    alExigirContrasena?: () => void;
    confirmaciones: ConfirmacionesServidor;
    abierto: MotorDeSesion;
    sesion: SesionCampo;
    empresa: number | null;
    alSalir: (motivo: MotivoSalida) => void;
    cambiarEmpresa: (() => void) | undefined;
    deps: DependenciasCampo;
}) {
    const { motor, posiciones } = abierto;
    const estado = useEstadoMotor(motor);
    const [estadoRastreo, setEstadoRastreo] = useState<EstadoRastreo>({ tipo: "fuera_de_curso" });
    const rastreo = useMemo(
        () => new RastreoUbicacion({ gps: deps.gps, claves: deps.claves, usuarioId: sesion.usuarioId, empresaId: empresa, base: abierto.nombre, alCambiar: setEstadoRastreo }),
        [deps, abierto.nombre, sesion.usuarioId, empresa],
    );

    // Toda salida de la sesión apaga el GPS y borra la marca (desmontar = vencida, ajena, cambiar de empresa).
    useEffect(() => () => void rastreo.detener(), [rastreo]);

    // Bucle de envío, red y volver a la app.
    useEffect(() => {
        motor.iniciar();
        posiciones.iniciar();
        registrarReceptor((p) => posiciones.agregar(p));
        const aplicarRed = (enLinea: boolean) => {
            motor.establecerConexion(enLinea);
            if (enLinea) {
                posiciones.reintentarAhora();
            }
        };
        void deps.red.leer().then(aplicarRed).catch(() => undefined);
        const quitarRed = deps.red.escuchar(aplicarRed);
        const quitarFrente = deps.alVolverAlFrente(() => {
            motor.reintentarAhora();
            posiciones.reintentarAhora();
        });
        return () => {
            quitarRed();
            quitarFrente();
        };
    }, [motor, posiciones, deps]);

    // GPS en segundo plano: solo con órdenes en curso (pausada no cuenta) y con la sesión vigente.
    // La cola de posiciones `detenida` = 401/403 de sesión al enviar puntos: igual que el motor.
    const colaPosiciones = useEstadoPosiciones(posiciones);
    const sinSesion = !vigente(estado.conexion) || colaPosiciones?.detenida === true;
    const enCurso = sinSesion ? 0 : ordenesEnCurso(estado.ordenes);
    useEffect(() => {
        if (estado.conexion === "sin_sesion" || colaPosiciones?.detenida === true) {
            deps.olvidarSesionGuardada();
        }
    }, [estado.conexion, colaPosiciones?.detenida, deps]);
    useEffect(() => {
        void rastreo.actualizar(enCurso);
    }, [rastreo, enCurso]);

    // Solo una respuesta del servidor de esta sesión (paquete o cambios, ya aplicada) abre o renueva
    // la ventana del GPS; nunca lo cargado de la base al abrir ni una acción local.
    useEffect(
        () =>
            motor.suscribir((e) => {
                if (confirmaciones.tomar(e.sincronizando)) {
                    void rastreo.confirmarServidor(vigente(e.conexion) ? ordenesEnCurso(e.ordenes) : 0);
                }
            }),
        [motor, rastreo, confirmaciones],
    );

    const cerrarSesion = useCallback<CerrarSesion>(
        async ({ conservarPendientes }) => {
            await rastreo.detener();
            registrarReceptor(null);
            abierto.chat.detener();
            if (conservarPendientes) {
                await motor.olvidarDatosDelServidor();
                // La caché del chat se borra; lo que quedó por enviar sale al volver a entrar.
                await abierto.chat.olvidarCache();
            } else {
                await abierto.borrar();
            }
            deps.vaciarCacheFotos();
            // Push: se borra lo guardado y lo visible, y este teléfono deja de recibir los de esta persona
            // (con tope de 3 s; el servidor también los quita al cerrar la sesión).
            if (deps.push) {
                await cerrarPushDeSesion({ servicio: deps.push.servicio, registro: deps.push.registro, claves: deps.claves, usuarioId: sesion.usuarioId, conservarPendientes });
            }
            try {
                await deps.cerrarSesionApi(sesion.usuarioId);
            } catch (e) {
                // La API respondió con otro error: la sesión del teléfono se cierra igual.
                if (!(e instanceof ErrorApi || e instanceof ErrorTransporte)) {
                    throw e;
                }
            }
            alSalir("salida");
        },
        [rastreo, motor, abierto, deps, sesion.usuarioId, alSalir],
    );

    // Estable: `Marco` baja el paquete al montarse y no debe repetirlo en cada render.
    const sesionVencida = useCallback(() => alSalir("expirada"), [alSalir]);
    const nombreEmpresa = empresa === null ? null : (sesion.empresas.find((e) => e.id === empresa)?.nombre ?? null);
    const conChat = sesion.permisos?.includes("chat.ver") === true;
    const marco = <Marco empresa={nombreEmpresa} alSesionVencida={sesionVencida} {...(alExigirContrasena ? { alExigirContrasena } : {})} {...(cambiarEmpresa ? { cambiarEmpresa } : {})} />;
    const push = deps.push;
    const conPush = push ? (
        <ProveedorPush
            servicio={push.servicio}
            api={push.api}
            registro={push.registro}
            claves={deps.claves}
            empresa={empresa}
            baseUrl={push.baseUrl}
            generarId={push.generarId}
            chat={conChat ? abierto.chat : null}
            alVolverAlFrente={deps.alVolverAlFrente}
            primerUso={(p) => <PrimerUsoPush {...p} />}
        >
            {marco}
        </ProveedorPush>
    ) : (
        marco
    );
    return (
        <ProveedorCampo
            motor={motor}
            posiciones={posiciones}
            rastreo={rastreo}
            estadoRastreo={estadoRastreo}
            sesion={sesion}
            servicios={deps.servicios}
            cerrarSesion={cerrarSesion}
        >
            <ProveedorInventario inventario={abierto.inventario}>
                <ProveedorAgenda leer={abierto.agenda}>
                    <ProveedorLibres api={abierto.libres}>
                        <ProveedorHistorial leer={abierto.historial ?? null}>
                            {/* Sin `chat.ver` el chat no arranca ni se ofrece. */}
                            {conChat ? (
                                <ProveedorChat chat={abierto.chat} puedeEnviar={sesion.permisos?.includes("chat.enviar") === true} red={deps.red} alCambiarPlano={deps.alCambiarPlano}>
                                    {conPush}
                                </ProveedorChat>
                            ) : (
                                conPush
                            )}
                        </ProveedorHistorial>
                    </ProveedorLibres>
                </ProveedorAgenda>
            </ProveedorInventario>
        </ProveedorCampo>
    );
}

const estilos = StyleSheet.create({
    centro: { flex: 1, alignItems: "center", justifyContent: "center", padding: 16 },
    bloqueada: { paddingHorizontal: 20 },
});
