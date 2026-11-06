"use client";

import type { OrdenVista, RutaCampo } from "@iris/sync";
import { accionPrincipal, anilloAvance, intercalarTareas, mosaicoPorRecibir, mosaicoRuta, nombreCliente, resumenHoy, tareasDelDia } from "@iris/sync/campo";
import { Aviso, Boton, Cargando, EstadoVacio, InsigniaEstado, cn } from "@iris/ui";
import { CalendarX, ChevronRight, Clock, CloudUpload, ListChecks, MapPin, Phone, Play, RefreshCw, Route, TriangleAlert, UserX } from "lucide-react";
import type { MouseEvent, ReactNode } from "react";
import { horaCorta, hoyEnGuatemala, ventanaCita } from "@/lib/campo/fechas";
import { enlaceComoLlegar } from "@/lib/campo/gps";
import { TIPOS_ORDEN } from "@/lib/campo/textos";
import { enlaceTel, principal, telefonos } from "@/lib/datos/telefono";
import { urlDeVista, useCampo } from "./contexto";
import { InsigniaOrden } from "./insignias";
import { useCargarAlAbrir } from "./inventario";
import { useInventario } from "./inventario-datos";
import { TarjetaTarea } from "./tareas";
import type { CargaPaquete } from "./pantallas";

/**
 * «Hoy» (diseño «combinada»): la siguiente orden en grande (estado, código y hora, cliente, tipo y
 * dirección, anillo de avance y «Continuar trabajo» / «Cómo llegar» / «Llamar»), los mosaicos
 * «Ruta de hoy» y «Por recibir» (solo con datos leídos) y la lista «Después». El orden es el de la
 * ruta publicada (`ruta.ordenIds`) o, sin ruta, el de la hora de la cita (lo da `@iris/sync`).
 */
export function ListaOrdenes({ carga, bajarPaquete }: { carga: CargaPaquete; bajarPaquete: () => Promise<void> }) {
    const { estado } = useCampo();
    const conRuta = (estado.ruta?.ordenIds?.length ?? 0) > 0;
    const { siguiente, despues, terminadas } = resumenHoy(estado.ordenes);
    // Las tareas propias de hoy van intercaladas por hora en «Después» (borde punteado).
    const tareas = tareasDelDia(estado.tareas, hoyEnGuatemala());
    // Las órdenes quedan en su orden (paradas o cita); cada tarea entra antes de la primera cuya hora
    // (llegada estimada con ruta, inicio de la cita sin ruta) sea posterior.
    const entradas = intercalarTareas(
        despues,
        (o) => (conRuta ? paradaDe(estado.ruta, o.id)?.llegadaEstimada : null) ?? o.cita?.inicio ?? o.programadaInicio,
        tareas,
    );
    const posicion = (o: OrdenVista) => (conRuta ? (estado.ruta?.ordenIds?.indexOf(o.id) ?? -1) + 1 || null : null);

    return (
        <section aria-labelledby="titulo-dia" className="grid gap-4">
            <div className="flex items-center justify-between gap-3">
                <h2 id="titulo-dia" className="sr-only">
                    Mis órdenes de hoy
                </h2>
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

            {estado.ordenes.length === 0 && tareas.length === 0 ? (
                carga.estado === "cargando" || carga.estado === "inactiva" ? (
                    <Cargando visible texto="Cargando tus órdenes…" />
                ) : estado.fecha === null ? (
                    <EstadoVacio icono={CalendarX} titulo="Sin órdenes en el teléfono" descripcion="Conéctate para bajar tus órdenes del día." />
                ) : (
                    <EstadoVacio icono={CalendarX} titulo="No tienes órdenes para hoy" descripcion="Si despacho te asigna una, aparecerá aquí." />
                )
            ) : (
                <div role="region" aria-label="Órdenes del día" className="grid gap-4">
                    {siguiente ? <SiguienteOrden orden={siguiente} posicion={posicion(siguiente)} parada={paradaDe(estado.ruta, siguiente.id)} /> : null}
                    <Mosaicos />
                    {entradas.length > 0 ? (
                        <section aria-labelledby="titulo-despues" className="grid gap-2">
                            <h3 id="titulo-despues" className="text-[0.8125rem] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
                                Después
                            </h3>
                            <ol className="grid gap-2" aria-labelledby="titulo-despues">
                                {entradas.map((e) =>
                                    e.tipo === "orden" ? (
                                        <FilaOrden key={e.clave} orden={e.orden} posicion={posicion(e.orden)} parada={paradaDe(estado.ruta, e.orden.id)} />
                                    ) : (
                                        <li key={e.clave}>
                                            <TarjetaTarea tarea={e.tarea} />
                                        </li>
                                    ),
                                )}
                            </ol>
                        </section>
                    ) : null}
                </div>
            )}
            {terminadas.length > 0 ? (
                <section aria-labelledby="titulo-terminadas" className="grid gap-2">
                    <h3 id="titulo-terminadas" className="pt-2 text-[0.8125rem] font-semibold tracking-[0.06em] text-muted-foreground uppercase">
                        Terminadas
                    </h3>
                    <ol className="grid gap-2" aria-label="Órdenes terminadas">
                        {terminadas.map((o) => (
                            <FilaOrden key={o.id} orden={o} posicion={null} />
                        ))}
                    </ol>
                </section>
            ) : null}
        </section>
    );
}

type Parada = NonNullable<RutaCampo["paradas"]>[number];

/** Parada de la ruta publicada para esa orden (llegada estimada), si la ruta la trae. */
function paradaDe(ruta: RutaCampo | null, ordenId: number): Parada | null {
    return ruta?.paradas?.find((p) => p.ordenId === ordenId) ?? null;
}

/** Llegada estimada (ruta), ventana acordada y lo que falta por enviar o fue rechazado. */
function Detalles({ orden, parada }: { orden: OrdenVista; parada: Parada | null }) {
    const ventana = ventanaCita(orden.cita);
    const sinEnviar = orden.pendientes + orden.subidasPendientes;
    return (
        <>
            {parada ? (
                <span className={cn("flex items-center gap-1 text-[0.8125rem]", parada.fueraDeVentana ? "text-warning" : "text-muted-foreground")}>
                    {parada.fueraDeVentana ? <TriangleAlert aria-hidden className="size-3.5 shrink-0" /> : <Route aria-hidden className="size-3.5 shrink-0" />}
                    Llegada estimada {horaCorta(parada.llegadaEstimada)}
                    {parada.fueraDeVentana ? " · fuera de la ventana acordada" : ""}
                </span>
            ) : null}
            {ventana ? (
                <span className="flex items-center gap-1 text-[0.8125rem] text-muted-foreground">
                    <Clock aria-hidden className="size-3.5 shrink-0" />
                    Llegada acordada {ventana}
                </span>
            ) : null}
            {orden.reasignada || orden.rechazos > 0 || sinEnviar > 0 ? (
                <span className="flex flex-wrap gap-1.5">
                    {orden.reasignada ? <InsigniaEstado tono="warning" icono={UserX} texto="Reasignada" /> : null}
                    {orden.rechazos > 0 ? <InsigniaEstado tono="error" icono={TriangleAlert} texto={`${orden.rechazos} rechazada${orden.rechazos === 1 ? "" : "s"}`} /> : null}
                    {sinEnviar > 0 ? <InsigniaEstado tono="warning" icono={CloudUpload} texto={`${sinEnviar} sin enviar`} /> : null}
                </span>
            ) : null}
        </>
    );
}

function useAbrir(orden: OrdenVista) {
    const { navegar } = useCampo();
    return (e: MouseEvent<HTMLAnchorElement>) => {
        e.preventDefault();
        navegar({ tipo: "orden", id: orden.id });
    };
}

/** Teléfono para «Llamar»: primero el contacto en sitio de la orden, luego el de Argos. */
function telefonoDe(orden: OrdenVista): string | null {
    const deArgos = orden.cliente ? (orden.cliente.telefonos ?? telefonos("telefono", orden.cliente.telefono)) : [];
    const lista = [...telefonos("telefono", orden.contacto.telefono), ...deArgos];
    const t = orden.contacto.telefono ? (lista[0] ?? null) : principal(lista);
    return t ? enlaceTel(t) : null;
}

/** La siguiente orden en grande: tarjeta de 28 px con el anillo de avance y sus tres botones. */
function SiguienteOrden({ orden, posicion, parada }: { orden: OrdenVista; posicion: number | null; parada: Parada | null }) {
    const { navegar } = useCampo();
    const abrir = useAbrir(orden);
    const nombre = nombreCliente(orden);
    const hora = horaCorta(orden.cita?.inicio ?? orden.programadaInicio);
    const anillo = anilloAvance(orden);
    const principalAccion = accionPrincipal(orden);
    const llamar = telefonoDe(orden);
    const comoLlegar = enlaceComoLlegar(orden.lat ?? orden.cliente?.lat ?? null, orden.lon ?? orden.cliente?.lon ?? null, orden.direccion);
    const IconoAccion = principalAccion.texto === "Empezar trabajo" ? Play : principalAccion.texto === "Continuar trabajo" ? ListChecks : ChevronRight;
    return (
        <section
            aria-label="Siguiente orden"
            data-slot="siguiente-orden"
            className="grid gap-3.5 rounded-[1.75rem] border border-[var(--campo-heroe-borde)] bg-[var(--campo-heroe)] p-[1.125rem] shadow-[var(--campo-sombra)]"
        >
            <a
                href={urlDeVista({ tipo: "orden", id: orden.id })}
                onClick={abrir}
                aria-label={`${orden.numero}, ${nombre}`}
                className="grid gap-3.5 rounded-[1.25rem] focus-visible:outline-2 focus-visible:outline-offset-4"
            >
                <span className="flex items-center justify-between gap-2">
                    <span className="flex items-center gap-2">
                        {posicion !== null ? (
                            <span aria-label={`Parada ${posicion}`} className="grid size-7 place-content-center rounded-full bg-primary text-sm font-semibold text-primary-foreground">
                                {posicion}
                            </span>
                        ) : null}
                        <InsigniaOrden estado={orden.estado} />
                    </span>
                    <span className="font-mono text-xs font-medium text-muted-foreground">
                        {orden.numero}
                        {hora ? ` · ${hora}` : ""}
                    </span>
                </span>
                <span className="flex items-center gap-4">
                    <span className="grid min-w-0 flex-1 gap-1">
                        <span className="text-[1.375rem] leading-tight font-bold tracking-[-0.01em]">{nombre}</span>
                        <span className="text-sm text-muted-foreground">{TIPOS_ORDEN[orden.tipo]}</span>
                        {orden.direccion ? (
                            <span className="flex items-start gap-1 text-[0.8125rem] text-muted-foreground">
                                <MapPin aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                                <span className="line-clamp-2">{orden.direccion}</span>
                            </span>
                        ) : null}
                        <Detalles orden={orden} parada={parada} />
                    </span>
                    {orden.estado !== "pendiente" ? <AnilloAvance fraccion={anillo.fraccion} texto={anillo.texto} etiqueta={anillo.etiqueta} /> : null}
                </span>
            </a>
            <div className="flex gap-2.5">
                {/* La acción principal en «tinta», como en la orden (decisión del PM). */}
                <Boton variante="tinta" tamano="lg" className="h-[3.25rem] flex-1 rounded-full text-[0.9375rem]" onClick={() => navegar(principalAccion.vista)}>
                    <IconoAccion aria-hidden />
                    {principalAccion.texto}
                </Boton>
                {comoLlegar ? (
                    <BotonRedondo etiqueta="Cómo llegar">
                        <a href={comoLlegar} target="_blank" rel="noopener noreferrer">
                            <Route aria-hidden />
                        </a>
                    </BotonRedondo>
                ) : null}
                {llamar ? (
                    <BotonRedondo etiqueta="Llamar al cliente">
                        <a href={llamar}>
                            <Phone aria-hidden />
                        </a>
                    </BotonRedondo>
                ) : null}
            </div>
        </section>
    );
}

function BotonRedondo({ etiqueta, children }: { etiqueta: string; children: ReactNode }) {
    return (
        <Boton asChild variante="secundario" aria-label={etiqueta} title={etiqueta} className="size-[3.25rem] shrink-0 rounded-full bg-[var(--campo-panel-2)] p-0 md:size-[3.25rem] [&_svg]:size-5">
            {children}
        </Boton>
    );
}

/** Anillo «2/4»: avance de los pasos de trabajo (el nombre accesible lo dice completo). */
export function AnilloAvance({ fraccion, texto, etiqueta }: { fraccion: number; texto: string; etiqueta: string }) {
    const grados = Math.round(fraccion * 360);
    return (
        <span
            role="img"
            aria-label={etiqueta}
            data-slot="anillo-avance"
            className="grid size-[4.125rem] shrink-0 place-content-center rounded-full"
            style={{ background: `conic-gradient(var(--hx-primary) 0 ${grados}deg, var(--campo-anillo) ${grados}deg 360deg)` }}
        >
            <span aria-hidden className="grid size-[3.25rem] place-content-center rounded-full bg-[var(--campo-heroe)] text-[0.9375rem] font-bold tabular-nums">
                {texto}
            </span>
        </span>
    );
}

/** «Ruta de hoy» y «Por recibir»: solo con datos leídos (nunca un total inventado). */
function Mosaicos() {
    const { estado, navegar } = useCampo();
    const { traslados, cargarTraslados } = useInventario();
    // El mosaico y el contador de la isla se leen al abrir «Hoy» con señal (como la app nativa).
    useCargarAlAbrir(traslados, cargarTraslados);
    const ruta = mosaicoRuta(estado);
    const recibir = mosaicoPorRecibir(traslados.datos, estado.recepciones);
    if (!ruta) {
        return null;
    }
    return (
        <section aria-label="Resumen del día" className="grid grid-cols-2 gap-2.5">
            <div className="grid content-start gap-1.5 rounded-[1.375rem] border border-border bg-[var(--campo-panel)] p-3.5" data-slot="mosaico-ruta">
                <span className="text-[0.8125rem] text-muted-foreground">Ruta de hoy</span>
                <span className="text-2xl font-bold tracking-[-0.02em]">{ruta.valor}</span>
                <span className="text-xs font-medium text-muted-foreground">{ruta.detalle}</span>
            </div>
            <a
                href={urlDeVista({ tipo: "recibir" })}
                onClick={(e) => {
                    e.preventDefault();
                    navegar({ tipo: "recibir" });
                }}
                data-slot="mosaico-recibir"
                className={cn(
                    "grid content-start gap-1.5 rounded-[1.375rem] border p-3.5 focus-visible:outline-2 focus-visible:outline-offset-2",
                    recibir && recibir.cantidad > 0 ? "border-[var(--campo-aviso-borde)] bg-[var(--campo-aviso-fondo)] text-warning" : "border-border bg-[var(--campo-panel)]",
                )}
            >
                <span className="text-[0.8125rem]">Por recibir</span>
                {recibir ? (
                    <>
                        <span className="text-2xl font-bold tracking-[-0.02em]">{recibir.valor}</span>
                        <span className="text-xs font-medium">{recibir.detalle}</span>
                    </>
                ) : (
                    <>
                        <span className="text-2xl font-bold tracking-[-0.02em] text-muted-foreground">—</span>
                        <span className="text-xs font-medium text-muted-foreground">{traslados.estado === "cargando" ? "Leyendo…" : "Sin leer: toca para ver"}</span>
                    </>
                )}
            </a>
        </section>
    );
}

/** Fila de «Después»: hora en un bloque, cliente, tipo y la insignia de estado. */
function FilaOrden({ orden, posicion, parada = null }: { orden: OrdenVista; posicion: number | null; parada?: Parada | null }) {
    const abrir = useAbrir(orden);
    const hora = horaCorta(orden.cita?.inicio ?? orden.programadaInicio);
    const nombre = nombreCliente(orden);
    return (
        <li>
            <a
                href={urlDeVista({ tipo: "orden", id: orden.id })}
                onClick={abrir}
                aria-label={`${orden.numero}, ${nombre}`}
                className="flex items-center gap-3 rounded-[1.375rem] border border-border bg-[var(--campo-panel)] p-3 transition-colors hover:bg-[var(--campo-panel-2)] focus-visible:outline-2 focus-visible:outline-offset-2"
            >
                <span className="relative grid size-[3.25rem] shrink-0 place-content-center rounded-2xl bg-[var(--campo-panel-2)] text-[0.9375rem] font-bold tabular-nums">
                    {hora ?? "—"}
                    {posicion !== null ? (
                        <span aria-label={`Parada ${posicion}`} className="absolute -top-1.5 -left-1.5 grid size-5 place-content-center rounded-full bg-primary text-[0.6875rem] font-semibold text-primary-foreground">
                            {posicion}
                        </span>
                    ) : null}
                </span>
                <span className="grid min-w-0 flex-1 gap-0.5">
                    <span className="truncate text-[0.9375rem] font-semibold">{nombre}</span>
                    <span className="text-[0.8125rem] text-muted-foreground">
                        <span className="font-mono">{orden.numero}</span> · {TIPOS_ORDEN[orden.tipo]}
                    </span>
                    <Detalles orden={orden} parada={parada} />
                </span>
                <InsigniaOrden estado={orden.estado} className="shrink-0" />
            </a>
        </li>
    );
}
