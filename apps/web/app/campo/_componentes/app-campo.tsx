"use client";

import { Aviso, Boton, Cargando, EstadoVacio, MarcaIris } from "@iris/ui";
import { Building2, KeyRound, LogOut, RefreshCw, UserRoundX, WifiOff } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import { ErrorApi } from "@/lib/api/errores";
import { abrirMotor, type MotorAbierto } from "@/lib/campo/motor";
import { dependenciasNavegador, olvidarTurno, RastreadorUbicacion } from "@/lib/campo/ubicacion";
import {
    cerrarSesionApi,
    cierrePendiente,
    elegirEmpresa,
    empresaDeTrabajo,
    empresaElegida,
    guardarSesion,
    leerYo,
    marcarCierrePendiente,
    olvidarSesion,
    sesionDesdeYo,
    sesionGuardada,
    usableSinSenal,
    type SesionCampo,
} from "@/lib/campo/sesion";
import { ProveedorCampo, type CerrarSesion } from "./contexto";
import { ElegirEmpresa } from "./elegir-empresa";
import { Pantallas } from "./pantallas";
import { FinDeTurnoPorOrdenes, ProveedorUbicacion } from "./ubicacion";

type Arranque =
    | { fase: "cargando" }
    | { fase: "sin_sesion" }
    /** Sin señal y sin una verificación reciente de la sesión guardada (o tras cerrar sesión). */
    | { fase: "conectate"; motivo: "sin_verificar" | "sesion_cerrada" }
    | { fase: "error"; error: unknown }
    /** Entró otra persona en este navegador mientras la app estaba abierta. */
    | { fase: "ajena"; nombre: string }
    | { fase: "lista"; sesion: SesionCampo; desdeCache: boolean };

export const RUTA_ENTRAR = "/entrar?siguiente=%2Fcampo";

function Centro({ children }: { children: ReactNode }) {
    return <main className="mx-auto grid min-h-dvh w-full max-w-md place-content-center gap-6 px-4 py-10">{children}</main>;
}

/**
 * App de campo. Se abre con datos solo con un GET /yo correcto de ese usuario o, sin señal, con
 * la sesión guardada verificada en línea hace menos de HORAS_SIN_SENAL (y sin cierre de sesión).
 * Luego, empresa activa y el motor de ese usuario y empresa.
 */
export function AppCampo() {
    const [arranque, setArranque] = useState<Arranque>({ fase: "cargando" });
    const [intento, setIntento] = useState(0);

    useEffect(() => {
        let vivo = true;
        (async () => {
            if (cierrePendiente()) {
                // Se cerró sesión sin señal: primero se cierra en la API y luego a entrar.
                try {
                    await cerrarSesionApi();
                    marcarCierrePendiente(false);
                    window.location.replace(RUTA_ENTRAR);
                    return;
                } catch {
                    if (vivo) {
                        setArranque({ fase: "conectate", motivo: "sesion_cerrada" });
                    }
                    return;
                }
            }
            const cache = sesionGuardada();
            try {
                const yo = await leerYo(cache ? empresaElegida(cache.usuarioId) : null);
                const sesion = sesionDesdeYo(yo);
                guardarSesion(sesion);
                if (vivo) {
                    setArranque({ fase: "lista", sesion, desdeCache: false });
                }
            } catch (e) {
                if (!vivo) {
                    return;
                }
                if (e instanceof ErrorApi && e.estado === 401) {
                    setArranque({ fase: "sin_sesion" });
                    window.location.replace(RUTA_ENTRAR);
                } else if (e instanceof ErrorApi && e.estado === 0) {
                    // Sin señal: solo con una verificación reciente de este mismo usuario.
                    setArranque(cache && usableSinSenal(cache) ? { fase: "lista", sesion: cache, desdeCache: true } : { fase: "conectate", motivo: "sin_verificar" });
                } else {
                    setArranque({ fase: "error", error: e });
                }
            }
        })();
        return () => {
            vivo = false;
        };
    }, [intento]);

    const alSesionAjena = useCallback((nombre: string) => setArranque({ fase: "ajena", nombre }), []);
    const alCerrarSesion = useCallback((sinSenal: boolean) => {
        if (sinSenal) {
            setArranque({ fase: "conectate", motivo: "sesion_cerrada" });
        } else {
            window.location.assign(RUTA_ENTRAR);
        }
    }, []);

    if (arranque.fase === "cargando" || arranque.fase === "sin_sesion") {
        return (
            <Centro>
                <MarcaIris />
                <Cargando visible texto={arranque.fase === "cargando" ? "Abriendo Iris Campo…" : "Llevándote a iniciar sesión…"} />
            </Centro>
        );
    }
    if (arranque.fase === "conectate") {
        return (
            <Centro>
                <MarcaIris />
                <EstadoVacio
                    icono={arranque.motivo === "sesion_cerrada" ? LogOut : WifiOff}
                    titulo={arranque.motivo === "sesion_cerrada" ? "Cerraste sesión en este teléfono" : "Conéctate para entrar"}
                    descripcion={
                        arranque.motivo === "sesion_cerrada"
                            ? "Conéctate para entrar de nuevo. Lo que quedó sin enviar se guarda y se envía cuando su técnico vuelva a entrar."
                            : "Para abrir Iris Campo sin señal, antes tienes que haber entrado con señal en las últimas horas."
                    }
                    accion={
                        <Boton tamano="lg" onClick={() => setIntento((n) => n + 1)}>
                            <RefreshCw aria-hidden />
                            Reintentar
                        </Boton>
                    }
                />
            </Centro>
        );
    }
    if (arranque.fase === "ajena") {
        return (
            <Centro>
                <MarcaIris />
                <EstadoVacio
                    icono={UserRoundX}
                    titulo="Entró otra persona en este navegador"
                    descripcion={`No se envió nada. Lo pendiente de ${arranque.nombre} sigue guardado en este teléfono y se enviará cuando vuelva a entrar.`}
                    accion={
                        <Boton tamano="lg" onClick={() => window.location.reload()}>
                            <KeyRound aria-hidden />
                            Continuar
                        </Boton>
                    }
                />
            </Centro>
        );
    }
    if (arranque.fase === "error") {
        return (
            <Centro>
                <MarcaIris />
                <EstadoVacio
                    icono={RefreshCw}
                    titulo="No se pudo abrir la sesión"
                    descripcion="Inténtalo de nuevo en un momento."
                    accion={
                        <Boton tamano="lg" onClick={() => setIntento((n) => n + 1)}>
                            <RefreshCw aria-hidden />
                            Reintentar
                        </Boton>
                    }
                />
            </Centro>
        );
    }
    return <ConEmpresa sesion={arranque.sesion} alSesionAjena={alSesionAjena} alCerrarSesion={alCerrarSesion} />;
}

type Salidas = { alSesionAjena: (nombre: string) => void; alCerrarSesion: (sinSenal: boolean) => void };

function ConEmpresa({ sesion, ...salidas }: { sesion: SesionCampo } & Salidas) {
    const [elegida, setElegida] = useState(() => empresaElegida(sesion.usuarioId));
    const empresa = empresaDeTrabajo(sesion, elegida);
    const [eligiendo, setEligiendo] = useState(false);

    const cambiar = useCallback(
        (id: number) => {
            elegirEmpresa(sesion.usuarioId, id);
            setElegida(id);
            setEligiendo(false);
        },
        [sesion.usuarioId],
    );

    if (sesion.modoEmpresa === "sin_empresa" || (sesion.modoEmpresa !== "sin_conector" && sesion.empresas.length === 0)) {
        return (
            <Centro>
                <MarcaIris />
                <EstadoVacio
                    icono={Building2}
                    titulo="No tienes empresas asignadas"
                    descripcion="Pide a tu supervisor que te asigne la empresa en la que trabajas."
                />
            </Centro>
        );
    }
    if (empresa === undefined || eligiendo) {
        return (
            <Centro>
                <MarcaIris />
                <ElegirEmpresa empresas={sesion.empresas} actual={empresa ?? null} alElegir={cambiar} alCancelar={empresa === undefined ? undefined : () => setEligiendo(false)} />
            </Centro>
        );
    }
    return <ConMotor key={`${sesion.usuarioId}-${empresa ?? 0}`} sesion={sesion} empresa={empresa} pedirEmpresa={() => setEligiendo(true)} {...salidas} />;
}

function ConMotor({
    sesion,
    empresa,
    pedirEmpresa,
    alSesionAjena,
    alCerrarSesion,
}: { sesion: SesionCampo; empresa: number | null; pedirEmpresa: () => void } & Salidas) {
    const [abierto, setAbierto] = useState<MotorAbierto | null>(null);
    const [error, setError] = useState<unknown>(null);
    const [intento, setIntento] = useState(0);

    useEffect(() => {
        let vivo = true;
        let actual: MotorAbierto | null = null;
        // Entró otra persona: se detiene y se desmonta sin enviar nada; su base queda intacta.
        const ajena = () => {
            actual?.motor.detener();
            actual?.posiciones.detener();
            alSesionAjena(sesion.nombre);
        };
        abrirMotor({ usuarioId: sesion.usuarioId, empresaId: empresa }, ajena)
            .then((m) => {
                actual = m;
                if (vivo) {
                    setAbierto(m);
                } else {
                    m.motor.detener();
                    m.posiciones.detener();
                    m.almacen.cerrar();
                }
            })
            .catch((e: unknown) => vivo && setError(e));
        return () => {
            vivo = false;
            actual?.motor.detener();
            actual?.posiciones.detener();
            actual?.almacen.cerrar();
        };
    }, [sesion.usuarioId, sesion.nombre, empresa, intento, alSesionAjena]);

    // Uno por motor abierto: cerrar sesión termina su turno antes de esperar a nada.
    const rastreador = useMemo(
        () => (abierto ? new RastreadorUbicacion(abierto.posiciones, dependenciasNavegador(sesion.usuarioId)) : null),
        [abierto, sesion.usuarioId],
    );

    const cerrarSesion = useCallback<CerrarSesion>(
        async ({ conservarPendientes }) => {
            if (!abierto) {
                return;
            }
            const { motor, posiciones, almacen } = abierto;
            // Antes del primer await: con mala señal, `cerrarSesionApi` tarda y no deben entrar
            // puntos nuevos después de tocar «Cerrar sesión» (la cola, además, queda cerrada).
            rastreador?.terminarTurno();
            motor.detener();
            posiciones.detener();
            // El turno es de esta persona: el próximo que entre en el teléfono empieza fuera de turno.
            olvidarTurno(sesion.usuarioId);
            if (conservarPendientes) {
                // Solo la cola, los adjuntos y las ubicaciones pendientes; nada que se pueda ver sin entrar.
                await motor.olvidarDatosDelServidor();
            } else {
                await almacen.borrar();
            }
            olvidarSesion(sesion.usuarioId);
            try {
                await cerrarSesionApi();
                alCerrarSesion(false);
            } catch {
                // Sin señal: la cookie sigue en la API; se cierra allí al volver la señal.
                marcarCierrePendiente(true);
                alCerrarSesion(true);
            }
        },
        [abierto, rastreador, sesion.usuarioId, alCerrarSesion],
    );

    if (error) {
        return (
            <Centro>
                <MarcaIris />
                <Aviso
                    tono="error"
                    titulo="No se pudo abrir el almacenamiento del teléfono"
                    accion={
                        <Boton
                            variante="secundario"
                            onClick={() => {
                                setError(null);
                                setIntento((n) => n + 1);
                            }}
                        >
                            Reintentar
                        </Boton>
                    }
                >
                    Revisa que el navegador no esté en modo privado y que haya espacio libre.
                </Aviso>
            </Centro>
        );
    }
    if (!abierto || !rastreador) {
        return (
            <Centro>
                <MarcaIris />
                <Cargando visible texto="Abriendo tus órdenes…" />
            </Centro>
        );
    }
    return (
        <ProveedorUbicacion cola={abierto.posiciones} rastreador={rastreador}>
            <ProveedorCampo motor={abierto.motor} sesion={sesion} cerrarSesion={cerrarSesion}>
                <FinDeTurnoPorOrdenes />
                <Pantallas empresa={empresa} pedirEmpresa={pedirEmpresa} />
            </ProveedorCampo>
        </ProveedorUbicacion>
    );
}
