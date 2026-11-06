"use client";

import { ErrorTransporte, type OrdenLibre } from "@iris/sync";
import {
    avisoYaTomada,
    cuandoLibre,
    distanciaTexto,
    subtituloLibres,
    TIPOS_ORDEN,
} from "@iris/sync/campo";
import {
    Aviso,
    Boton,
    Cargando,
    Dialogo,
    DialogoContenido,
    EstadoVacio,
} from "@iris/ui";
import {
    ArrowLeft,
    Hand,
    Info,
    MapPin,
    RefreshCw,
    WifiOff,
} from "lucide-react";
import { useEffect, useState } from "react";
import { useCampo } from "./contexto";
import { useLibres } from "./libres-datos";
import { BOTON_TINTA } from "./tareas";

function nombreDe(o: OrdenLibre): string {
    return o.clienteNombre ?? o.contacto.nombre ?? o.direccion ?? o.numero;
}

/**
 * «Órdenes libres» (desde el «+»): las de tu zona sin técnico, más cercanas primero. Tomar una
 * requiere señal (no va por la cola): si otro la tomó primero, se avisa y sale de la lista.
 */
export function OrdenesLibresPantalla() {
    const { estado, navegar, notificar, motor } = useCampo();
    const { api, libres, cargarLibres, quitar } = useLibres();
    const [confirmar, setConfirmar] = useState<OrdenLibre | null>(null);
    const [ocupado, setOcupado] = useState(false);
    const [yaTomada, setYaTomada] = useState<string | null>(null);
    const sinSenal = estado.conexion === "sin_conexion";
    useEffect(() => {
        if (!sinSenal) {
            void cargarLibres();
        }
    }, [sinSenal, cargarLibres]);

    const tomar = async (o: OrdenLibre) => {
        setOcupado(true);
        try {
            await api.tomar(o.id);
            quitar(o.id);
            setConfirmar(null);
            notificar({
                tono: "success",
                texto: `${o.numero} quedó a tu nombre. Despacho recibe el aviso.`,
            });
            // La orden llega por los cambios: se piden ya.
            motor.reintentarAhora();
        } catch (e) {
            setConfirmar(null);
            if (e instanceof ErrorTransporte && e.problema?.status === 409) {
                quitar(o.id);
                setYaTomada(
                    avisoYaTomada(o.numero, e.problema.tomadaPor?.nombre),
                );
                return;
            }
            if (e instanceof ErrorTransporte && e.problema?.status === 404) {
                // Ya no está libre (otro estado o fuera de tu zona): sale de la lista.
                quitar(o.id);
                notificar({
                    tono: "warning",
                    texto: `${o.numero} ya no está libre.`,
                });
                return;
            }
            notificar({
                tono: "error",
                texto:
                    e instanceof ErrorTransporte && e.tipo === "red"
                        ? "Sin señal: tomar una orden necesita conexión."
                        : "No se pudo tomar la orden. Inténtalo de nuevo.",
            });
        } finally {
            setOcupado(false);
        }
    };

    const datos = libres.datos;
    const lista = datos?.data ?? [];
    return (
        <section
            aria-labelledby="titulo-libres"
            className="grid gap-4"
            data-slot="ordenes-libres"
        >
            <header className="grid grid-cols-[3rem_1fr_3rem] items-center">
                <Boton
                    variante="secundario"
                    aria-label="Atrás"
                    className="size-12 rounded-full p-0"
                    onClick={() => navegar({ tipo: "lista" })}
                >
                    <ArrowLeft aria-hidden className="size-5" />
                </Boton>
                <h1
                    id="titulo-libres"
                    className="text-center text-xl font-semibold"
                >
                    Órdenes libres
                </h1>
            </header>
            {yaTomada ? (
                <div
                    role="status"
                    className="flex gap-3 rounded-[1.375rem] border border-[var(--campo-aviso-borde)] bg-[var(--campo-aviso-fondo)] p-4 text-[var(--hx-warning,inherit)]"
                    data-slot="ya-tomada"
                >
                    <Info aria-hidden className="mt-0.5 size-5 shrink-0" />
                    <p className="font-medium">{yaTomada}</p>
                </div>
            ) : null}
            {datos && lista.length > 0 ? (
                <p className="px-1 text-sm text-muted-foreground">
                    {subtituloLibres(datos.zonas ?? [], datos.orden)}
                </p>
            ) : null}
            {sinSenal && !datos ? (
                <EstadoVacio
                    icono={WifiOff}
                    titulo="Sin señal"
                    descripcion="Tomar una orden libre necesita conexión. Vuelve a intentarlo con señal."
                />
            ) : libres.estado === "error" && !datos ? (
                <Aviso
                    tono="error"
                    titulo="No se pudieron leer las órdenes libres"
                    accion={
                        <Boton
                            variante="secundario"
                            tamano="lg"
                            onClick={() => void cargarLibres()}
                        >
                            <RefreshCw aria-hidden />
                            Reintentar
                        </Boton>
                    }
                >
                    Revisa la señal.
                </Aviso>
            ) : !datos ? (
                <Cargando visible texto="Buscando órdenes libres…" />
            ) : lista.length === 0 ? (
                <EstadoVacio
                    icono={Hand}
                    titulo="No hay órdenes libres"
                    descripcion="Ninguna orden de tu zona está sin técnico ahora."
                />
            ) : (
                <ul className="grid gap-3">
                    {lista.map((o) => {
                        const distancia = distanciaTexto(o.distanciaM);
                        return (
                            <li
                                key={o.id}
                                className="grid gap-3 rounded-[1.375rem] border border-border bg-[var(--campo-panel)] p-4"
                                data-slot="orden-libre"
                            >
                                <div className="flex items-start justify-between gap-3">
                                    <div className="grid min-w-0 gap-0.5">
                                        <p className="truncate text-[1.0625rem] font-semibold">
                                            {nombreDe(o)}
                                        </p>
                                        <p className="text-sm text-muted-foreground">
                                            <span className="font-mono">
                                                {o.numero}
                                            </span>{" "}
                                            · {TIPOS_ORDEN[o.tipo]}
                                        </p>
                                        {distancia ? (
                                            <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
                                                <MapPin
                                                    aria-hidden
                                                    className="size-4"
                                                />
                                                {distancia}
                                            </p>
                                        ) : null}
                                    </div>
                                    <p className="shrink-0 text-sm text-muted-foreground">
                                        {cuandoLibre(o)}
                                    </p>
                                </div>
                                <Boton
                                    variante="secundario"
                                    tamano="lg"
                                    className="h-[3.25rem] w-full rounded-full"
                                    disabled={sinSenal}
                                    onClick={() => setConfirmar(o)}
                                >
                                    <Hand aria-hidden />
                                    Tomarla
                                </Boton>
                            </li>
                        );
                    })}
                </ul>
            )}
            {/* Confirmación corta: tarjeta centrada con «X» y [Cancelar | Tomarla] del mismo tamaño. */}
            <Dialogo
                open={confirmar !== null}
                onOpenChange={(a) =>
                    a || ocupado ? undefined : setConfirmar(null)
                }
            >
                <DialogoContenido
                    centrado
                    className="campo-hoja"
                    titulo={confirmar ? `¿Tomar ${confirmar.numero}?` : ""}
                    descripcion={
                        confirmar
                            ? `${nombreDe(confirmar)} · ${TIPOS_ORDEN[confirmar.tipo]} · ${cuandoLibre(confirmar).toLowerCase()}. Queda a tu nombre y despacho recibe el aviso.`
                            : undefined
                    }
                >
                    <div
                        className="grid grid-cols-2 gap-3"
                        data-slot="confirmar-tomar"
                    >
                        <Boton
                            variante="secundario"
                            tamano="lg"
                            className="h-[3.25rem] w-full rounded-full"
                            disabled={ocupado}
                            onClick={() => setConfirmar(null)}
                        >
                            Cancelar
                        </Boton>
                        <Boton
                            variante="tinta"
                            tamano="lg"
                            className={BOTON_TINTA}
                            disabled={ocupado}
                            onClick={() =>
                                confirmar ? void tomar(confirmar) : undefined
                            }
                        >
                            {ocupado ? "Tomando…" : "Tomarla"}
                        </Boton>
                    </div>
                </DialogoContenido>
            </Dialogo>
        </section>
    );
}
