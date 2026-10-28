"use client";

import { ErrorTransporte } from "@iris/sync";
import { Aviso, Boton, Dialogo, DialogoContenido, DialogoPie, LogoIris, cn } from "@iris/ui";
import { Building2, LogOut, Menu, RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { SelectorTema } from "@/componentes/selector-tema";
import { api } from "@/lib/api/cliente";
import { hoyEnGuatemala } from "@/lib/campo/fechas";
import { BarraConexion } from "./barra-conexion";
import { useCampo } from "./contexto";
import { DetalleOrden } from "./detalle-orden";
import { ListaOrdenes } from "./lista-ordenes";
import { Pendientes } from "./pendientes";
import { RUTA_ENTRAR } from "./app-campo";

export type CargaPaquete = { estado: "inactiva" | "cargando" | "lista" } | { estado: "error"; mensaje: string };

/** Marco de la app: encabezado, barra de conexión, la vista actual y el aviso flotante. */
export function Pantallas({ empresa, pedirEmpresa }: { empresa: number | null; pedirEmpresa: () => void }) {
    const { motor, estado, sesion, vista, notificacion } = useCampo();
    const [carga, setCarga] = useState<CargaPaquete>({ estado: "inactiva" });
    const bajadoEnVisita = useRef(false);

    const bajarPaquete = useCallback(async () => {
        setCarga({ estado: "cargando" });
        try {
            await motor.cargarPaquete(hoyEnGuatemala());
            bajadoEnVisita.current = true;
            setCarga({ estado: "lista" });
        } catch (e) {
            const sinSenal = e instanceof ErrorTransporte && e.tipo === "red";
            setCarga({
                estado: "error",
                mensaje: sinSenal ? "Sin señal: se usan las órdenes guardadas en el teléfono." : "No se pudieron bajar las órdenes del día.",
            });
        }
    }, [motor]);

    // Conexión del navegador → motor; paquete al abrir (y al cambiar de empresa: otro motor) y al
    // volver la señal o cambiar de día; luego el bucle pide cambios cada minuto.
    useEffect(() => {
        const revisar = () => {
            const enLinea = navigator.onLine;
            motor.establecerConexion(enLinea);
            const estadoActual = motor.obtenerEstado();
            if (enLinea && (!bajadoEnVisita.current || estadoActual.fecha !== hoyEnGuatemala())) {
                void bajarPaquete();
            }
        };
        const alVer = () => document.visibilityState === "visible" && revisar();
        const sinSenal = () => motor.establecerConexion(false);
        revisar();
        motor.iniciar();
        window.addEventListener("online", revisar);
        window.addEventListener("offline", sinSenal);
        document.addEventListener("visibilitychange", alVer);
        return () => {
            window.removeEventListener("online", revisar);
            window.removeEventListener("offline", sinSenal);
            document.removeEventListener("visibilitychange", alVer);
            motor.detener();
        };
    }, [motor, bajarPaquete]);

    const nombreEmpresa = sesion.empresas.find((e) => e.id === empresa)?.nombre;

    return (
        <div className="relative mx-auto flex min-h-dvh w-full max-w-2xl flex-col pb-24">
            <header className="glass-material sticky top-0 z-30 flex items-center gap-3 border-b border-border px-4 py-2.5 pt-[max(0.625rem,env(safe-area-inset-top))]">
                <LogoIris className="size-8" />
                <div className="min-w-0 flex-1 leading-tight">
                    <p className="font-semibold tracking-[-0.015em]">Iris Campo</p>
                    <p className="truncate text-xs text-muted-foreground">
                        {sesion.nombre}
                        {nombreEmpresa ? ` · ${nombreEmpresa}` : ""}
                    </p>
                </div>
                <MenuCampo nombreEmpresa={nombreEmpresa} pedirEmpresa={pedirEmpresa} bajarPaquete={bajarPaquete} />
            </header>
            <BarraConexion />
            {estado.conexion === "sin_sesion" ? (
                <div className="px-4 pt-3">
                    <Aviso
                        tono="error"
                        titulo="Tu sesión terminó"
                        accion={
                            <Boton variante="secundario" onClick={() => window.location.assign(`${RUTA_ENTRAR}&motivo=expirada`)}>
                                Entrar de nuevo
                            </Boton>
                        }
                    >
                        Lo que hiciste sigue guardado en el teléfono y se enviará al volver a entrar.
                    </Aviso>
                </div>
            ) : null}
            {estado.conexion === "sin_empresa" ? (
                <div className="px-4 pt-3">
                    <Aviso
                        tono="error"
                        titulo="Ya no puedes trabajar en esta empresa"
                        accion={
                            <Boton variante="secundario" onClick={pedirEmpresa}>
                                Elegir empresa
                            </Boton>
                        }
                    >
                        Lo pendiente sigue guardado en el teléfono.
                    </Aviso>
                </div>
            ) : null}
            <main className="flex-1 px-4 pt-4">
                {vista.tipo === "orden" ? (
                    <DetalleOrden id={vista.id} />
                ) : vista.tipo === "pendientes" ? (
                    <Pendientes />
                ) : (
                    <ListaOrdenes carga={carga} bajarPaquete={bajarPaquete} />
                )}
            </main>
            <div
                aria-live="polite"
                className="pointer-events-none fixed inset-x-0 bottom-0 z-40 mx-auto w-full max-w-2xl px-4 pb-[max(1rem,env(safe-area-inset-bottom))]"
            >
                {notificacion ? (
                    <div
                        data-slot="notificacion"
                        data-tono={notificacion.tono}
                        className={cn(
                            "pointer-events-auto rounded-lg border px-4 py-3 text-sm font-medium shadow-lg",
                            notificacion.tono === "error" ? "border-error/40 bg-card text-error" : "border-border bg-card text-foreground",
                        )}
                    >
                        {notificacion.texto}
                    </div>
                ) : null}
            </div>
        </div>
    );
}

function MenuCampo({
    nombreEmpresa,
    pedirEmpresa,
    bajarPaquete,
}: {
    nombreEmpresa: string | undefined;
    pedirEmpresa: () => void;
    bajarPaquete: () => Promise<void>;
}) {
    const { estado, sesion } = useCampo();
    const [abierto, setAbierto] = useState(false);
    const [confirmarEmpresa, setConfirmarEmpresa] = useState(false);
    const porEnviar = estado.pendientes + estado.rechazos.length + estado.subidas.pendientes;

    const salir = async () => {
        try {
            await api().DELETE("/api/v1/sesion");
        } finally {
            window.location.assign("/entrar?siguiente=%2Fcampo");
        }
    };

    return (
        <Dialogo open={abierto} onOpenChange={setAbierto}>
            <Boton variante="fantasma" tamano="icono" aria-label="Menú" title="Menú" onClick={() => setAbierto(true)}>
                <Menu />
            </Boton>
            <DialogoContenido titulo="Menú" descripcion={`${sesion.nombre} · ${sesion.correo}`}>
                <div className="grid gap-3">
                    <SelectorTema />
                    <Boton
                        variante="secundario"
                        tamano="lg"
                        className="justify-start"
                        onClick={() => {
                            setAbierto(false);
                            void bajarPaquete();
                        }}
                    >
                        <RefreshCw aria-hidden />
                        Actualizar órdenes del día
                    </Boton>
                    {sesion.empresas.length > 1 ? (
                        confirmarEmpresa ? (
                            <Aviso tono="warning" titulo={`Quedan ${porEnviar} cosas por enviar en ${nombreEmpresa ?? "esta empresa"}`}>
                                <p>Se enviarán cuando vuelvas a esta empresa. ¿Cambiar de todos modos?</p>
                                <DialogoPie className="mt-3">
                                    <Boton variante="fantasma" onClick={() => setConfirmarEmpresa(false)}>
                                        Quedarme
                                    </Boton>
                                    <Boton variante="secundario" onClick={pedirEmpresa}>
                                        Cambiar de empresa
                                    </Boton>
                                </DialogoPie>
                            </Aviso>
                        ) : (
                            <Boton
                                variante="secundario"
                                tamano="lg"
                                className="justify-start"
                                onClick={() => (porEnviar > 0 ? setConfirmarEmpresa(true) : pedirEmpresa())}
                            >
                                <Building2 aria-hidden />
                                Cambiar de empresa
                            </Boton>
                        )
                    ) : null}
                    <Boton variante="fantasma" tamano="lg" className="justify-start" onClick={() => void salir()}>
                        <LogOut aria-hidden />
                        Cerrar sesión
                    </Boton>
                    {porEnviar > 0 ? (
                        <p className="text-sm text-muted-foreground">
                            Lo que queda por enviar se guarda en este teléfono y se envía cuando vuelvas a entrar.
                        </p>
                    ) : null}
                </div>
            </DialogoContenido>
        </Dialogo>
    );
}
