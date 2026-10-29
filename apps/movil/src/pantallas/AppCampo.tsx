// Sesión de campo abierta: abre el motor del usuario y la empresa, lo conecta a la red y a la app
// (volver al frente), enciende el GPS en segundo plano solo con órdenes en curso y lo apaga en
// **toda** salida: desmontar (sesión vencida, sesión ajena, cambiar de empresa), cerrar sesión o
// perder la sesión con la app abierta. Lo nativo llega por `DependenciasCampo` (las pruebas lo
// reemplazan).
import { ErrorTransporte, type ColaPosiciones, type MotorSync } from "@iris/sync";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { ErrorApi } from "../api/errores";
import { Aviso } from "../componentes";
import { ProveedorCampo, useEstadoMotor, type CerrarSesion, type ServiciosNativos } from "../campo/contexto";
import { ordenesEnCurso, RastreoUbicacion, registrarReceptor, type EstadoRastreo, type GpsFondo } from "../campo/ubicacion";
import type { AlmacenClaves } from "../entorno";
import type { SesionCampo } from "../sesion/sesion";
import { useTema } from "../tema/tema";
import { Marco } from "./Marco";

export interface MotorDeSesion {
    motor: MotorSync;
    posiciones: ColaPosiciones;
    /** Archivo de la base (lo lee la tarea del GPS con la app cerrada). */
    nombre: string;
    /** Cierra la conexión con la base. */
    cerrar: () => Promise<void>;
    /** Cierra y borra la base (cerrar sesión sin conservar). */
    borrar: () => Promise<void>;
}

export interface DependenciasCampo {
    abrirMotor: (clave: { usuarioId: number; empresaId: number | null }, alSesionAjena: () => void) => Promise<MotorDeSesion>;
    gps: GpsFondo;
    claves: AlmacenClaves;
    servicios: ServiciosNativos;
    /** Estado de la red y sus cambios (`true` = hay internet). */
    red: { leer: () => Promise<boolean>; escuchar: (fn: (enLinea: boolean) => void) => () => void };
    /** La app vuelve al frente. */
    alVolverAlFrente: (fn: () => void) => () => void;
    /** `DELETE /sesion` y olvido local (con «cierre pendiente» si no hay señal). */
    cerrarSesionApi: (usuarioId: number) => Promise<void>;
    /** Borra fotos temporales de la cámara y del compresor. */
    vaciarCacheFotos: () => void;
}

export type MotivoSalida = "expirada" | "salida";

function Centro({ children }: { children: ReactNode }) {
    const { colores } = useTema();
    return <View style={[estilos.centro, { backgroundColor: colores.fondo }]}>{children}</View>;
}

export function AppCampo({
    sesion,
    empresa,
    alSalir,
    cambiarEmpresa,
    deps,
}: {
    sesion: SesionCampo;
    empresa: number | null;
    alSalir: (motivo: MotivoSalida) => void;
    cambiarEmpresa: (() => void) | undefined;
    deps: DependenciasCampo;
}) {
    const [abierto, setAbierto] = useState<MotorDeSesion | null>(null);
    const [error, setError] = useState<string | null>(null);
    const actual = useRef<MotorDeSesion | null>(null);

    useEffect(() => {
        let vivo = true;
        deps.abrirMotor({ usuarioId: sesion.usuarioId, empresaId: empresa }, () => alSalir("expirada"))
            .then((m) => {
                if (!vivo) {
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
                m.motor.detener();
                m.posiciones.detener();
                void m.cerrar();
            }
        };
    }, [sesion.usuarioId, empresa, alSalir, deps]);

    if (error) {
        return (
            <Centro>
                <Aviso tono="error" titulo="No se pudo abrir la base del teléfono">
                    {error}
                </Aviso>
            </Centro>
        );
    }
    if (!abierto) {
        return (
            <Centro>
                <ActivityIndicator accessibilityLabel="Abriendo tus órdenes" />
            </Centro>
        );
    }
    return <CampoAbierto abierto={abierto} sesion={sesion} empresa={empresa} alSalir={alSalir} cambiarEmpresa={cambiarEmpresa} deps={deps} />;
}

function CampoAbierto({
    abierto,
    sesion,
    empresa,
    alSalir,
    cambiarEmpresa,
    deps,
}: {
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
    const sesionVigente = estado.conexion !== "sin_sesion" && estado.conexion !== "sin_empresa";
    const enCurso = sesionVigente ? ordenesEnCurso(estado.ordenes) : 0;
    const firma = estado.ordenes.map((o) => `${o.id}:${o.estado}${o.reasignada ? "!" : ""}`).join(",");
    useEffect(() => {
        void rastreo.actualizar(enCurso, firma);
    }, [rastreo, enCurso, firma]);

    // Cada contacto correcto con el servidor con órdenes en curso renueva el tope del GPS.
    const ultimaSincronizacion = estado.ultimaSincronizacion;
    useEffect(() => {
        if (ultimaSincronizacion !== null && enCurso > 0) {
            void rastreo.renovar();
        }
    }, [rastreo, ultimaSincronizacion, enCurso]);

    const cerrarSesion = useCallback<CerrarSesion>(
        async ({ conservarPendientes }) => {
            await rastreo.detener();
            registrarReceptor(null);
            if (conservarPendientes) {
                await motor.olvidarDatosDelServidor();
            } else {
                await abierto.borrar();
            }
            deps.vaciarCacheFotos();
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
            <Marco empresa={nombreEmpresa} alSesionVencida={sesionVencida} {...(cambiarEmpresa ? { cambiarEmpresa } : {})} />
        </ProveedorCampo>
    );
}

const estilos = StyleSheet.create({
    centro: { flex: 1, alignItems: "center", justifyContent: "center", padding: 16 },
});
