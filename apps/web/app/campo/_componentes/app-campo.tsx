"use client";

import type { MotorSync } from "@iris/sync";
import { Aviso, Boton, Cargando, EstadoVacio, MarcaIris } from "@iris/ui";
import { Building2, RefreshCw, WifiOff } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { ErrorApi } from "@/lib/api/errores";
import { abrirMotor } from "@/lib/campo/motor";
import {
    elegirEmpresa,
    empresaDeTrabajo,
    empresaElegida,
    guardarSesion,
    leerYo,
    sesionDesdeYo,
    sesionGuardada,
    type SesionCampo,
} from "@/lib/campo/sesion";
import { ElegirEmpresa } from "./elegir-empresa";
import { Pantallas } from "./pantallas";
import { ProveedorCampo } from "./contexto";

type Arranque =
    | { fase: "cargando" }
    | { fase: "sin_sesion" }
    | { fase: "error"; error: unknown }
    | { fase: "lista"; sesion: SesionCampo; desdeCache: boolean };

export const RUTA_ENTRAR = "/entrar?siguiente=%2Fcampo";

function Centro({ children }: { children: React.ReactNode }) {
    return <main className="mx-auto grid min-h-dvh w-full max-w-md place-content-center gap-6 px-4 py-10">{children}</main>;
}

/**
 * App de campo: sesión del panel (GET /yo; sin señal, la última guardada), empresa activa y el
 * motor de sincronización de ese usuario y empresa.
 */
export function AppCampo() {
    const [arranque, setArranque] = useState<Arranque>({ fase: "cargando" });
    const [intento, setIntento] = useState(0);

    useEffect(() => {
        let vivo = true;
        const cache = sesionGuardada();
        leerYo(cache ? empresaElegida(cache.usuarioId) : null)
            .then((yo) => {
                const sesion = sesionDesdeYo(yo);
                guardarSesion(sesion);
                if (vivo) {
                    setArranque({ fase: "lista", sesion, desdeCache: false });
                }
            })
            .catch((e: unknown) => {
                if (!vivo) {
                    return;
                }
                if (e instanceof ErrorApi && e.estado === 401) {
                    setArranque({ fase: "sin_sesion" });
                    window.location.replace(RUTA_ENTRAR);
                } else if (cache) {
                    // Sin señal (o la API caída): se trabaja con lo guardado en el teléfono.
                    setArranque({ fase: "lista", sesion: cache, desdeCache: true });
                } else {
                    setArranque({ fase: "error", error: e });
                }
            });
        return () => {
            vivo = false;
        };
    }, [intento]);

    if (arranque.fase === "cargando" || arranque.fase === "sin_sesion") {
        return (
            <Centro>
                <MarcaIris />
                <Cargando visible texto={arranque.fase === "cargando" ? "Abriendo Iris Campo…" : "Llevándote a iniciar sesión…"} />
            </Centro>
        );
    }
    if (arranque.fase === "error") {
        const sinSenal = arranque.error instanceof ErrorApi && arranque.error.estado === 0;
        return (
            <Centro>
                <MarcaIris />
                <EstadoVacio
                    icono={sinSenal ? WifiOff : RefreshCw}
                    titulo={sinSenal ? "Necesitas señal para entrar la primera vez" : "No se pudo abrir la sesión"}
                    descripcion={
                        sinSenal
                            ? "Después de entrar con señal, Iris Campo abre y guarda tu trabajo aunque no haya conexión."
                            : "Inténtalo de nuevo en un momento."
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
    return <ConEmpresa sesion={arranque.sesion} />;
}

function ConEmpresa({ sesion }: { sesion: SesionCampo }) {
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
    return <ConMotor key={`${sesion.usuarioId}-${empresa ?? 0}`} sesion={sesion} empresa={empresa} pedirEmpresa={() => setEligiendo(true)} />;
}

function ConMotor({ sesion, empresa, pedirEmpresa }: { sesion: SesionCampo; empresa: number | null; pedirEmpresa: () => void }) {
    const [motor, setMotor] = useState<MotorSync | null>(null);
    const [error, setError] = useState<unknown>(null);
    const [intento, setIntento] = useState(0);

    useEffect(() => {
        let vivo = true;
        let abierto: MotorSync | null = null;
        abrirMotor({ usuarioId: sesion.usuarioId, empresaId: empresa })
            .then((m) => {
                abierto = m;
                if (vivo) {
                    setMotor(m);
                } else {
                    m.detener();
                }
            })
            .catch((e: unknown) => vivo && setError(e));
        return () => {
            vivo = false;
            abierto?.detener();
        };
    }, [sesion.usuarioId, empresa, intento]);

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
    if (!motor) {
        return (
            <Centro>
                <MarcaIris />
                <Cargando visible texto="Abriendo tus órdenes…" />
            </Centro>
        );
    }
    return (
        <ProveedorCampo motor={motor} sesion={sesion}>
            <Pantallas empresa={empresa} pedirEmpresa={pedirEmpresa} />
        </ProveedorCampo>
    );
}
