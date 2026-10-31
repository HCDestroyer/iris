"use client";

import type { OrdenVista, RutaCampo } from "@iris/sync";
import { Aviso, Boton, Cargando, EstadoVacio, InsigniaEstado } from "@iris/ui";
import { CalendarX, ChevronRight, Clock, CloudUpload, MapPin, RefreshCw, Route, TriangleAlert, UserX } from "lucide-react";
import type { MouseEvent } from "react";
import { diaLargo, horaCorta, hoyEnGuatemala, ventanaCita } from "@/lib/campo/fechas";
import { TIPOS_ORDEN } from "@/lib/campo/textos";
import { urlDeVista, useCampo } from "./contexto";
import { InsigniaOrden } from "./insignias";
import type { CargaPaquete } from "./pantallas";

/**
 * Mis órdenes del día en orden de ruta publicada (`ruta.ordenIds` del paquete, cuando I2 la
 * publique) y, sin ruta, por la hora de la cita de la agenda (el orden lo da `@iris/sync`).
 */
export function ListaOrdenes({ carga, bajarPaquete }: { carga: CargaPaquete; bajarPaquete: () => Promise<void> }) {
    const { estado } = useCampo();
    const fecha = estado.fecha ?? hoyEnGuatemala();
    const conRuta = (estado.ruta?.ordenIds?.length ?? 0) > 0;
    const activas = estado.ordenes.filter((o) => !["cerrada", "fallida", "cancelada"].includes(o.estado) && !o.reasignada);
    const terminadas = estado.ordenes.filter((o) => !activas.includes(o));

    return (
        <section aria-labelledby="titulo-dia" className="grid gap-4">
            <div className="flex items-end justify-between gap-3">
                <div>
                    <h1 id="titulo-dia" className="text-2xl font-semibold tracking-[-0.015em]">
                        Mis órdenes de hoy
                    </h1>
                    <p className="text-sm text-muted-foreground first-letter:uppercase">
                        {diaLargo(fecha)} · {conRuta ? "en orden de ruta" : "por hora de cita"}
                    </p>
                </div>
                {carga.estado === "cargando" ? <Cargando visible texto="Bajando…" /> : null}
            </div>

            {conRuta && estado.ruta?.desactualizada ? (
                <Aviso tono="warning" icono={Route} titulo="Tu ruta cambió">
                    Una orden cambió de técnico o de hora después de publicarla. Despacho publicará otra; mientras tanto se muestra la
                    anterior.
                </Aviso>
            ) : null}

            {estado.caducado ? (
                <Aviso
                    tono="warning"
                    titulo="Pasaron 7 días sin sincronizar"
                    accion={
                        <Boton tamano="lg" variante="secundario" onClick={() => void bajarPaquete()}>
                            Bajar órdenes
                        </Boton>
                    }
                >
                    Se borraron las órdenes viejas del teléfono; lo que hiciste sigue guardado.
                </Aviso>
            ) : null}
            {carga.estado === "error" ? (
                <Aviso
                    tono={estado.fecha ? "warning" : "error"}
                    titulo={carga.mensaje}
                    accion={
                        <Boton tamano="lg" variante="secundario" onClick={() => void bajarPaquete()}>
                            <RefreshCw aria-hidden />
                            Reintentar
                        </Boton>
                    }
                />
            ) : null}

            {estado.ordenes.length === 0 ? (
                carga.estado === "cargando" || carga.estado === "inactiva" ? (
                    <Cargando visible texto="Cargando tus órdenes…" />
                ) : estado.fecha === null ? (
                    <EstadoVacio
                        icono={CalendarX}
                        titulo="Sin órdenes en el teléfono"
                        descripcion="Conéctate para bajar tus órdenes del día."
                    />
                ) : (
                    <EstadoVacio icono={CalendarX} titulo="No tienes órdenes para hoy" descripcion="Si despacho te asigna una, aparecerá aquí." />
                )
            ) : (
                <ol className="grid gap-3" aria-label="Órdenes del día">
                    {activas.map((o, i) => (
                        <TarjetaOrden key={o.id} orden={o} posicion={conRuta ? i + 1 : null} parada={paradaDe(estado.ruta, o.id)} />
                    ))}
                </ol>
            )}
            {terminadas.length > 0 ? (
                <>
                    <h2 className="pt-2 text-sm font-semibold text-muted-foreground">Terminadas</h2>
                    <ol className="grid gap-3" aria-label="Órdenes terminadas">
                        {terminadas.map((o) => (
                            <TarjetaOrden key={o.id} orden={o} posicion={null} />
                        ))}
                    </ol>
                </>
            ) : null}
        </section>
    );
}

type Parada = NonNullable<RutaCampo["paradas"]>[number];

/** Parada de la ruta publicada para esa orden (llegada estimada), si la ruta la trae. */
function paradaDe(ruta: RutaCampo | null, ordenId: number): Parada | null {
    return ruta?.paradas?.find((p) => p.ordenId === ordenId) ?? null;
}

function TarjetaOrden({ orden, posicion, parada = null }: { orden: OrdenVista; posicion: number | null; parada?: Parada | null }) {
    const { navegar } = useCampo();
    const hora = horaCorta(orden.cita?.inicio ?? orden.programadaInicio);
    const ventana = ventanaCita(orden.cita);
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "Cliente sin nombre";
    const abrir = (e: MouseEvent<HTMLAnchorElement>) => {
        e.preventDefault();
        navegar({ tipo: "orden", id: orden.id });
    };
    return (
        <li>
            <a
                href={urlDeVista({ tipo: "orden", id: orden.id })}
                onClick={abrir}
                aria-label={`${orden.numero}, ${nombre}`}
                className="surface-card flex items-stretch gap-3 rounded-card p-4 transition-colors hover:bg-elevated focus-visible:outline-2 focus-visible:outline-offset-2"
            >
                <div className="grid w-12 shrink-0 content-start justify-items-center gap-1 text-center">
                    {posicion !== null ? (
                        <span aria-label={`Parada ${posicion}`} className="grid size-7 place-content-center rounded-full bg-primary text-sm font-semibold text-primary-foreground">
                            {posicion}
                        </span>
                    ) : null}
                    <span className="text-sm font-semibold tabular-nums">{hora ?? "—"}</span>
                </div>
                <div className="grid min-w-0 flex-1 gap-1.5">
                    <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-sm font-semibold">{orden.numero}</span>
                        <InsigniaOrden estado={orden.estado} />
                    </div>
                    <p className="truncate text-base font-semibold">{nombre}</p>
                    <p className="text-sm text-muted-foreground">{TIPOS_ORDEN[orden.tipo]}</p>
                    {parada ? (
                        <p className={parada.fueraDeVentana ? "flex items-center gap-1 text-sm text-warning" : "flex items-center gap-1 text-sm text-muted-foreground"}>
                            {parada.fueraDeVentana ? <TriangleAlert aria-hidden className="size-3.5 shrink-0" /> : <Route aria-hidden className="size-3.5 shrink-0" />}
                            Llegada estimada {horaCorta(parada.llegadaEstimada)}
                            {parada.fueraDeVentana ? " · fuera de la ventana acordada" : ""}
                        </p>
                    ) : null}
                    {ventana ? (
                        <p className="flex items-center gap-1 text-sm text-muted-foreground">
                            <Clock aria-hidden className="size-3.5 shrink-0" />
                            Llegada acordada {ventana}
                        </p>
                    ) : null}
                    {orden.direccion ? (
                        <p className="flex items-start gap-1 text-sm text-muted-foreground">
                            <MapPin aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                            <span className="line-clamp-2">{orden.direccion}</span>
                        </p>
                    ) : null}
                    <div className="flex flex-wrap gap-1.5 empty:hidden">
                        {orden.reasignada ? <InsigniaEstado tono="warning" icono={UserX} texto="Reasignada" /> : null}
                        {orden.rechazos > 0 ? <InsigniaEstado tono="error" icono={TriangleAlert} texto={`${orden.rechazos} rechazada${orden.rechazos === 1 ? "" : "s"}`} /> : null}
                        {orden.pendientes + orden.subidasPendientes > 0 ? (
                            <InsigniaEstado tono="warning" icono={CloudUpload} texto={`${orden.pendientes + orden.subidasPendientes} sin enviar`} />
                        ) : null}
                    </div>
                </div>
                <ChevronRight aria-hidden className="size-5 shrink-0 self-center text-muted-foreground" />
            </a>
        </li>
    );
}
