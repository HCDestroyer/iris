"use client";

import { Aviso, Boton, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Building2, ChevronRight, LogOut, RefreshCw, Warehouse } from "lucide-react";
import { useState } from "react";
import { SelectorTema } from "@/componentes/selector-tema";
import { useCampo } from "./contexto";

/**
 * «Yo»: quién está en el teléfono, tema, «Mi bodega», actualizar el día, cambiar de empresa y cerrar
 * sesión (con lo pendiente: enviar y salir, o salir conservándolo).
 */
export function Yo({
    nombreEmpresa,
    pedirEmpresa,
    bajarPaquete,
}: {
    nombreEmpresa: string | undefined;
    pedirEmpresa: () => void;
    bajarPaquete: () => Promise<void>;
}) {
    const { estado, sesion, motor, cerrarSesion, notificar, navegar } = useCampo();
    const [confirmarEmpresa, setConfirmarEmpresa] = useState(false);
    const [confirmarSalida, setConfirmarSalida] = useState(false);
    const [saliendo, setSaliendo] = useState(false);
    const porEnviar = estado.pendientes + estado.rechazos.length + estado.esperandoAdjuntos.length + estado.subidas.pendientes;
    const conSenal = estado.conexion === "en_linea" || estado.conexion === "desconocida";

    const salir = async (conservarPendientes: boolean) => {
        setSaliendo(true);
        try {
            await cerrarSesion({ conservarPendientes });
        } catch {
            notificar({ tono: "error", texto: "No se pudo cerrar la sesión en el teléfono. Vuelve a intentarlo." });
            setSaliendo(false);
        }
    };

    const enviarYSalir = async () => {
        setSaliendo(true);
        try {
            await motor.sincronizar();
        } catch {
            // Lo que no salió se informa abajo.
        }
        const e = motor.obtenerEstado();
        const quedan = e.pendientes + e.rechazos.length + e.esperandoAdjuntos.length + e.subidas.pendientes;
        if (quedan === 0) {
            await salir(false);
            return;
        }
        setSaliendo(false);
        notificar({ tono: "warning", texto: `No se pudo enviar todo: quedan ${quedan}. Revisa «Pendientes» o sal conservándolos.` });
    };

    return (
        <section aria-labelledby="titulo-yo" className="grid gap-4">
            <div>
                <h1 id="titulo-yo" className="text-2xl font-semibold tracking-[-0.015em]">
                    {sesion.nombre}
                </h1>
                <p className="text-sm text-muted-foreground">
                    {sesion.correo}
                    {nombreEmpresa ? ` · ${nombreEmpresa}` : ""}
                </p>
            </div>

            <Boton variante="secundario" tamano="lg" className="h-auto min-h-14 justify-start py-3" onClick={() => navegar({ tipo: "bodega" })}>
                <Warehouse aria-hidden />
                <span className="flex-1 text-left">Mi bodega</span>
                <ChevronRight aria-hidden className="text-muted-foreground" />
            </Boton>

            <Tarjeta>
                <TarjetaEncabezado>
                    <TarjetaTitulo>Este teléfono</TarjetaTitulo>
                </TarjetaEncabezado>
                <TarjetaContenido className="grid gap-3">
                    <SelectorTema />
                    <Boton variante="secundario" tamano="lg" className="justify-start" onClick={() => void bajarPaquete()}>
                        <RefreshCw aria-hidden />
                        Actualizar órdenes del día
                    </Boton>
                    {sesion.empresas.length > 1 ? (
                        confirmarEmpresa ? (
                            <Aviso tono="warning" titulo={`Quedan ${porEnviar} cosas por enviar en ${nombreEmpresa ?? "esta empresa"}`}>
                                <p>Se enviarán cuando vuelvas a esta empresa. ¿Cambiar de todos modos?</p>
                                <div className="mt-3 flex flex-wrap justify-end gap-2">
                                    <Boton variante="fantasma" tamano="lg" onClick={() => setConfirmarEmpresa(false)}>
                                        Quedarme
                                    </Boton>
                                    <Boton variante="secundario" tamano="lg" onClick={pedirEmpresa}>
                                        Cambiar de empresa
                                    </Boton>
                                </div>
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
                </TarjetaContenido>
            </Tarjeta>

            {confirmarSalida && porEnviar > 0 ? (
                <Aviso tono="warning" titulo={`Tienes ${porEnviar} ${porEnviar === 1 ? "cambio" : "cambios"} sin enviar`}>
                    <p>
                        {conSenal
                            ? "Envíalos antes de salir, o sal y consérvalos: se guardan sin datos de clientes y se envían cuando vuelvas a entrar."
                            : "Sin señal no se pueden enviar. Si sales, se conservan sin datos de clientes y se envían cuando vuelvas a entrar."}
                    </p>
                    <div className="mt-3 flex flex-wrap justify-end gap-2">
                        <Boton variante="fantasma" tamano="lg" disabled={saliendo} onClick={() => setConfirmarSalida(false)}>
                            Quedarme
                        </Boton>
                        <Boton variante="secundario" tamano="lg" disabled={saliendo} onClick={() => void salir(true)}>
                            Salir y conservarlos
                        </Boton>
                        {conSenal ? (
                            <Boton tamano="lg" disabled={saliendo} onClick={() => void enviarYSalir()}>
                                {saliendo ? "Enviando…" : "Enviar y salir"}
                            </Boton>
                        ) : null}
                    </div>
                </Aviso>
            ) : (
                <Boton
                    variante="peligro"
                    tamano="lg"
                    className="justify-start"
                    disabled={saliendo}
                    onClick={() => (porEnviar > 0 ? setConfirmarSalida(true) : void salir(false))}
                >
                    <LogOut aria-hidden />
                    {saliendo ? "Cerrando sesión…" : "Cerrar sesión"}
                </Boton>
            )}
        </section>
    );
}
