"use client";

import {
    avisar,
    Aviso,
    Boton,
    BotonEnviar,
    CampoSelector,
    cn,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    EstadoVacio,
    GrupoSegmentado,
    InsigniaEstado,
    useCambiosSinGuardar,
} from "@iris/ui";
import { Network, Route, UserRoundPlus, X } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { useAhora, usePosiciones } from "@/componentes/despacho/datos";
import { MapaDespacho, type Enfoque, type PuntoOrden, type PuntoTecnico } from "@/componentes/despacho/mapa";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { avisoOrden } from "@/lib/avisos";
import type { Calendario, Tecnico } from "@/lib/despacho/calendario";
import { diaDesdeIso, horaDe, hoyGuatemala, rangoUtc, textoAntiguedad } from "@/lib/despacho/fechas";
import { antiguedadAhora, frescura, iniciales, INTERVALO_POSICIONES_MS, textoPosicion } from "@/lib/despacho/posiciones";
import { equipoDeHoy, ESTADOS_TECNICO, type TecnicoHoy } from "@/lib/inicio/equipo";
import { useLectura, type Lectura } from "@/lib/lectura";
import { TIPOS_ORDEN, type Orden } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";

type Lista = "tecnicos" | "ordenes" | "red";

/** Órdenes sin técnico que se leen para el mapa. */
const LIMITE_SIN_TECNICO = 100;

/** Color del marcador de un técnico según su estado del día (en sitio, en camino, sin señal…). */
const TONO_MARCADOR = {
    en_sitio: "success",
    en_camino: "info",
    en_pausa: "warning",
    sin_senal: "neutral",
    ausente: "neutral",
    termino: "success",
    sin_iniciar: "neutral",
    sin_ordenes: "neutral",
} as const;

/**
 * Mapa (nuevo diseño): técnicos en vivo y órdenes sin asignar sobre MapLibre (teselas OSM), con
 * la lista al lado (Técnicos · Órdenes · Red) y la leyenda. Tocar un técnico abre su tarjeta: la
 * orden actual, la siguiente y «Asignar orden». Usa lo que ya existe: `GET /campo/posiciones/
 * ultimas`, `GET /agenda` de hoy, `GET /tecnicos` y `GET /ordenes?sinTecnico=true`.
 */
export function PantallaMapa() {
    const { puede } = useSesion();
    const veAgenda = puede(["agenda.ver"]);
    const vePosiciones = puede(["campo.ver_posiciones"]);
    const veOrdenes = puede(["ordenes.ver"]);
    const hoy = hoyGuatemala();
    const ahora = useAhora(5_000);
    const [lista, setLista] = useState<Lista>(vePosiciones || veAgenda ? "tecnicos" : "ordenes");
    const [elegido, setElegido] = useState<number | null>(null);
    const [enfoque, setEnfoque] = useState<Enfoque | null>(null);
    const [asignarA, setAsignarA] = useState<TecnicoHoy | null>(null);

    const agenda = useLectura<Calendario | null>((signal) =>
        veAgenda ? resolver(api().GET("/api/v1/agenda", { params: { query: rangoUtc(hoy, 1) }, signal })) : Promise.resolve(null),
    );
    const tecnicos = useLectura<Tecnico[] | null>(async (signal) =>
        veAgenda ? ((await resolver(api().GET("/api/v1/tecnicos", { params: { query: { activo: "true", limit: 200 } }, signal }))).data ?? []) : null,
    );
    const posiciones = usePosiciones({ cuadrillaId: null, zonaId: null }, vePosiciones, INTERVALO_POSICIONES_MS);
    const sinTecnico = useLectura<{ ordenes: Orden[]; cortada: boolean } | null>(async (signal) => {
        if (!veOrdenes) return null;
        const p = await resolver(
            api().GET("/api/v1/ordenes", { params: { query: { sinTecnico: true, estado: ["pendiente", "programada"], limit: LIMITE_SIN_TECNICO } }, signal }),
        );
        return { ordenes: p.data ?? [], cortada: p.nextCursor !== null };
    });

    const leidas = posiciones.estado === "listo" && posiciones.datos ? posiciones.datos : null;
    const vivas = useMemo(
        () => (leidas ? (leidas.respuesta.data ?? []).map((p) => ({ ...p, antiguedad: antiguedadAhora(p, leidas.leidaEn, ahora) })) : null),
        [leidas, ahora],
    );
    const equipo: TecnicoHoy[] = useMemo(() => {
        if (agenda.estado !== "listo" || tecnicos.estado !== "listo") return [];
        const base = tecnicos.datos ?? [];
        // Sin agenda.ver: los técnicos salen de sus posiciones (sin avance del día).
        if (!agenda.datos) {
            return (vivas ?? []).map((p) => ({
                usuarioId: p.tecnicoId,
                nombre: p.nombre,
                estado: frescura(p.antiguedad) === "sin_senal" ? ("sin_senal" as const) : ("sin_iniciar" as const),
                total: 0,
                terminadas: 0,
                actual: undefined,
                siguiente: undefined,
                antiguedadSeg: p.antiguedad,
            }));
        }
        return equipoDeHoy(base, agenda.datos.citas ?? [], agenda.datos.ausencias ?? [], vivas, hoy, ahora);
    }, [agenda, tecnicos, vivas, hoy, ahora]);

    const puntosTecnicos: PuntoTecnico[] = (vivas ?? []).map((p) => {
        const fila = equipo.find((f) => f.usuarioId === p.tecnicoId);
        const estado = fila ? ESTADOS_TECNICO[fila.estado] : null;
        const f = frescura(p.antiguedad);
        return {
            tecnicoId: p.tecnicoId,
            nombre: p.nombre,
            iniciales: iniciales(p.nombre),
            lat: p.lat,
            lon: p.lon,
            frescura: f,
            texto: estado ? `${estado.texto} · ${textoPosicion(p.antiguedad)}` : textoPosicion(p.antiguedad),
            ...(fila ? { tono: f === "sin_senal" ? "neutral" : TONO_MARCADOR[fila.estado] } : {}),
            etiqueta: `${p.nombre.split(" ")[0] ?? p.nombre} · ${(f === "sin_senal" ? "sin señal" : (estado?.texto ?? "en vivo")).toLowerCase()}`,
        };
    });
    const ordenesSinTecnico = sinTecnico.estado === "listo" && sinTecnico.datos ? sinTecnico.datos.ordenes : [];
    const puntosOrdenes: PuntoOrden[] = ordenesSinTecnico
        .filter((o): o is Orden & { lat: number; lon: number } => o.lat !== null && o.lon !== null)
        .map((o) => ({
            ordenId: o.id,
            numero: o.numero,
            estado: o.estado,
            lat: o.lat,
            lon: o.lon,
            detalle: `${TIPOS_ORDEN[o.tipo].texto} · ${o.direccion}`,
            sinAsignar: true,
        }));
    const sinCoordenadas = ordenesSinTecnico.length - puntosOrdenes.length;

    const filaElegida = elegido !== null ? equipo.find((f) => f.usuarioId === elegido) : undefined;
    const posElegida = elegido !== null ? vivas?.find((p) => p.tecnicoId === elegido) : undefined;
    const enfocar = (lat: number, lon: number) => setEnfoque({ lat, lon, n: Date.now() });

    const nTecnicos = vePosiciones ? (vivas ? vivas.length : undefined) : equipo.length || undefined;
    const nOrdenes =
        veOrdenes && sinTecnico.estado === "listo" && sinTecnico.datos
            ? sinTecnico.datos.cortada
                ? `${ordenesSinTecnico.length}+`
                : String(ordenesSinTecnico.length)
            : undefined;

    return (
        <div className="grid gap-4 [&>*]:min-w-0 lg:grid-cols-[20rem_minmax(0,1fr)]">
            <section
                aria-label="Lista del mapa"
                className="grid min-w-0 content-start rounded-xl border border-border bg-card lg:max-h-[calc(100dvh-8.5rem)] lg:overflow-hidden"
            >
                <div className="grid gap-2.5 border-b border-border px-3.5 py-3">
                    <div className="flex items-center justify-between gap-2">
                        <h1 className="text-lg font-semibold">Mapa</h1>
                        {veAgenda ? (
                            <Boton asChild variante="fantasma" tamano="sm" className="text-[0.8125rem] text-muted-foreground">
                                <Link href="/despacho">
                                    <Route />
                                    Rutas y despacho
                                </Link>
                            </Boton>
                        ) : null}
                    </div>
                    <GrupoSegmentado
                        etiqueta="Qué mostrar"
                        className="grid w-full grid-cols-3 [&>*]:justify-center"
                        valor={lista}
                        alCambiar={setLista}
                        opciones={[
                            { valor: "tecnicos", etiqueta: nTecnicos !== undefined ? `Técnicos · ${nTecnicos}` : "Técnicos" },
                            { valor: "ordenes", etiqueta: nOrdenes !== undefined ? `Órdenes · ${nOrdenes}` : "Órdenes" },
                            { valor: "red", etiqueta: "Red" },
                        ]}
                    />
                </div>
                <div className="min-h-0 overflow-y-auto">
                    {lista === "tecnicos" ? (
                        <ListaTecnicos
                            equipo={equipo}
                            vivas={vivas}
                            cargando={agenda.estado === "cargando" || tecnicos.estado === "cargando" || (vePosiciones && posiciones.estado === "cargando")}
                            errores={[agenda, tecnicos, posiciones].flatMap((l) =>
                                l.estado === "error" ? [{ error: l.error, reintentar: l.reintentar }] : [],
                            )}
                            vePosiciones={vePosiciones}
                            elegido={elegido}
                            alElegir={(id) => {
                                setElegido(id);
                                const p = vivas?.find((x) => x.tecnicoId === id);
                                if (p) enfocar(p.lat, p.lon);
                            }}
                        />
                    ) : lista === "ordenes" ? (
                        <ListaOrdenes lectura={sinTecnico} veOrdenes={veOrdenes} sinCoordenadas={sinCoordenadas} alEnfocar={enfocar} />
                    ) : (
                        <EstadoVacio
                            icono={Network}
                            titulo="Sin datos de red en el mapa"
                            descripcion="Iris todavía no recibe de Horus la ubicación de routers y nodos. Las caídas de red siguen llegando como tickets de incidente."
                            className="py-8"
                        />
                    )}
                </div>
            </section>

            <MapaDespacho
                etiqueta="Mapa de técnicos y órdenes"
                ordenes={puntosOrdenes}
                tecnicos={puntosTecnicos}
                lineas={[]}
                enfoque={enfoque}
                alElegirTecnico={(id) => setElegido((x) => (x === id ? null : id))}
                tecnicoElegido={elegido}
                className="h-[26rem] rounded-xl sm:h-[32rem] lg:h-[calc(100dvh-8.5rem)] lg:min-h-[32rem]"
            >
                <ul aria-label="Leyenda" className="pointer-events-none absolute bottom-8 left-2 flex max-w-[calc(100%-1rem)] flex-wrap gap-1.5">
                    {(
                        [
                            ["success", "rounded-full", "En sitio"],
                            ["info", "rounded-full", "En camino"],
                            ["neutral", "rounded-full", "Sin señal"],
                            ["warning", "rounded-[2px]", "Orden sin asignar"],
                        ] as const
                    ).map(([tono, forma, texto]) => (
                        <li
                            key={texto}
                            className="inline-flex h-7 items-center gap-1.5 rounded-lg border border-border bg-background px-2.5 text-[0.6875rem] font-medium shadow-sm"
                        >
                            <span
                                aria-hidden
                                className={cn(
                                    "size-2",
                                    forma,
                                    tono === "success" ? "bg-success" : tono === "info" ? "bg-info" : tono === "warning" ? "bg-warning" : "bg-muted-foreground",
                                )}
                            />
                            {texto}
                        </li>
                    ))}
                </ul>
                {elegido !== null ? (
                    <TarjetaTecnico
                        fila={filaElegida}
                        nombre={filaElegida?.nombre ?? posElegida?.nombre ?? `Técnico ${elegido}`}
                        antiguedad={posElegida?.antiguedad}
                        puedeAsignar={puede(["ordenes.asignar"]) && veOrdenes}
                        alCerrar={() => setElegido(null)}
                        alAsignar={() => filaElegida && setAsignarA(filaElegida)}
                    />
                ) : null}
            </MapaDespacho>

            {asignarA ? (
                <DialogoAsignarOrden
                    tecnico={asignarA}
                    ordenes={ordenesSinTecnico}
                    alCerrar={() => setAsignarA(null)}
                    alHecho={() => {
                        setAsignarA(null);
                        void sinTecnico.refrescar();
                        void agenda.refrescar();
                    }}
                />
            ) : null}
        </div>
    );
}

function ListaTecnicos({
    equipo,
    vivas,
    cargando,
    errores,
    vePosiciones,
    elegido,
    alElegir,
}: {
    equipo: readonly TecnicoHoy[];
    vivas: readonly { tecnicoId: number; antiguedad: number }[] | null;
    cargando: boolean;
    errores: { error: unknown; reintentar: () => void }[];
    vePosiciones: boolean;
    elegido: number | null;
    alElegir: (id: number) => void;
}) {
    if (errores[0]) {
        return (
            <div className="p-3">
                <ErrorLectura error={errores[0].error} reintentar={errores[0].reintentar} que="los técnicos" />
            </div>
        );
    }
    if (cargando) return <p className="px-3.5 py-4 text-[0.8125rem] text-muted-foreground">Cargando técnicos…</p>;
    if (equipo.length === 0) return <p className="px-3.5 py-4 text-[0.8125rem] text-muted-foreground">No hay técnicos activos.</p>;
    return (
        <ul aria-label="Técnicos" className="divide-y divide-border/70">
            {equipo.map((f) => {
                const e = ESTADOS_TECNICO[f.estado];
                const pos = vivas?.find((p) => p.tecnicoId === f.usuarioId);
                const detalle = f.actual
                    ? `${e.texto} · ${f.actual.numero}`
                    : f.siguiente
                      ? `Siguiente: ${f.siguiente.numero} · ${horaDe(f.siguiente.inicio)}`
                      : e.texto;
                const sel = elegido === f.usuarioId;
                return (
                    <li key={f.usuarioId}>
                        <Boton
                            variante="fantasma"
                            className={cn(
                                "h-auto w-full justify-start gap-2.5 rounded-none px-3.5 py-2.5 text-left font-normal",
                                sel && "bg-primary/5 shadow-[inset_3px_0_0_var(--hx-primary)]",
                            )}
                            aria-pressed={sel}
                            data-tecnico-mapa={f.usuarioId}
                            onClick={() => alElegir(f.usuarioId)}
                        >
                            <span aria-hidden className={cn("size-2.5 shrink-0 rounded-full", TONO_PUNTO[TONO_MARCADOR[f.estado]])} />
                            <span className="grid min-w-0 flex-1 gap-0.5">
                                <span className="truncate text-[0.8125rem] font-semibold">{f.nombre}</span>
                                <span className="truncate text-xs text-muted-foreground">{detalle}</span>
                            </span>
                            <span className="shrink-0 text-xs text-muted-foreground">
                                {vePosiciones ? (pos ? textoAntiguedad(pos.antiguedad) : "sin posición") : f.total > 0 ? `${f.terminadas} de ${f.total}` : ""}
                            </span>
                        </Boton>
                    </li>
                );
            })}
        </ul>
    );
}

const TONO_PUNTO = { success: "bg-success", info: "bg-info", warning: "bg-warning", neutral: "bg-muted-foreground", error: "bg-error" } as const;

function ListaOrdenes({
    lectura,
    veOrdenes,
    sinCoordenadas,
    alEnfocar,
}: {
    lectura: Lectura<{ ordenes: Orden[]; cortada: boolean } | null>;
    veOrdenes: boolean;
    sinCoordenadas: number;
    alEnfocar: (lat: number, lon: number) => void;
}) {
    if (!veOrdenes)
        return <p className="px-3.5 py-4 text-[0.8125rem] text-muted-foreground">Hace falta el permiso «ordenes.ver» para ver las órdenes sin asignar.</p>;
    if (lectura.estado === "error") {
        return (
            <div className="p-3">
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las órdenes sin técnico" />
            </div>
        );
    }
    if (lectura.estado === "cargando" || !lectura.datos) return <p className="px-3.5 py-4 text-[0.8125rem] text-muted-foreground">Cargando órdenes…</p>;
    const { ordenes, cortada } = lectura.datos;
    if (ordenes.length === 0) return <p className="px-3.5 py-4 text-[0.8125rem] text-muted-foreground">Todas las órdenes tienen técnico.</p>;
    return (
        <>
            <ul aria-label="Órdenes sin asignar" className="divide-y divide-border/70">
                {ordenes.map((o) => (
                    <li key={o.id}>
                        <Boton
                            variante="fantasma"
                            className="h-auto w-full justify-start gap-2.5 rounded-none px-3.5 py-2.5 text-left font-normal"
                            disabled={o.lat === null || o.lon === null}
                            data-orden-mapa={o.numero}
                            onClick={() => o.lat !== null && o.lon !== null && alEnfocar(o.lat, o.lon)}
                        >
                            <span aria-hidden className="size-2.5 shrink-0 rounded-[2px] bg-warning" />
                            <span className="grid min-w-0 flex-1 gap-0.5">
                                <span className="truncate text-[0.8125rem] font-semibold">
                                    {o.numero} · {TIPOS_ORDEN[o.tipo].texto}
                                </span>
                                <span className="truncate text-xs text-muted-foreground">{o.direccion}</span>
                            </span>
                            <span className="shrink-0 text-xs text-muted-foreground">{o.programadaInicio ? horaDe(o.programadaInicio) : "sin hora"}</span>
                        </Boton>
                    </li>
                ))}
            </ul>
            <p className="px-3.5 py-2.5 text-xs text-muted-foreground">
                {cortada ? `Se muestran las primeras ${ordenes.length}.` : null}{" "}
                {sinCoordenadas > 0 ? `${sinCoordenadas} sin coordenadas (no están en el mapa).` : null}
            </p>
        </>
    );
}

function TarjetaTecnico({
    fila,
    nombre,
    antiguedad,
    puedeAsignar,
    alCerrar,
    alAsignar,
}: {
    fila: TecnicoHoy | undefined;
    nombre: string;
    antiguedad: number | undefined;
    puedeAsignar: boolean;
    alCerrar: () => void;
    alAsignar: () => void;
}) {
    const e = fila ? ESTADOS_TECNICO[fila.estado] : null;
    return (
        <section
            aria-label={`Técnico: ${nombre}`}
            data-testid="tarjeta-tecnico"
            className="absolute inset-x-2 top-2 grid gap-2 rounded-xl border border-border bg-[var(--overlay)] p-3.5 shadow-[var(--overlay-shadow)] sm:right-auto sm:left-2 sm:w-[18rem]"
        >
            <div className="flex items-start justify-between gap-2">
                <div className="grid min-w-0 gap-1">
                    <h2 className="truncate text-sm font-semibold">{nombre}</h2>
                    {e ? <InsigniaEstado tono={e.tono} icono={e.icono} texto={e.texto} /> : null}
                </div>
                <Boton variante="fantasma" tamano="icono-sm" className="-mt-1 -mr-1" aria-label="Cerrar la tarjeta" onClick={alCerrar}>
                    <X />
                </Boton>
            </div>
            <dl className="grid gap-1 text-[0.8125rem]">
                <div className="flex gap-1.5">
                    <dt className="text-muted-foreground">Ahora:</dt>
                    <dd className="min-w-0 truncate">
                        {fila?.actual
                            ? `${fila.actual.numero} · ${fila.actual.direccion}${fila.actual.enSitioEn ? ` · desde ${horaDe(fila.actual.enSitioEn)}` : ""}`
                            : "Sin orden en curso"}
                    </dd>
                </div>
                <div className="flex gap-1.5">
                    <dt className="text-muted-foreground">Siguiente:</dt>
                    <dd className="min-w-0 truncate">
                        {fila?.siguiente ? `${fila.siguiente.numero} · ${horaDe(fila.siguiente.inicio)}` : "Nada más programado hoy"}
                    </dd>
                </div>
                {fila ? (
                    <div className="flex gap-1.5">
                        <dt className="text-muted-foreground">Avance:</dt>
                        <dd className="tabular-nums">{fila.total > 0 ? `${fila.terminadas} de ${fila.total}` : "sin órdenes hoy"}</dd>
                    </div>
                ) : null}
                <div className="flex gap-1.5">
                    <dt className="text-muted-foreground">Posición:</dt>
                    <dd>{antiguedad !== undefined ? textoPosicion(antiguedad) : "sin posición"}</dd>
                </div>
            </dl>
            <div className="flex gap-2">
                {puedeAsignar && fila ? (
                    <Boton tamano="sm" className="flex-1 rounded-lg" onClick={alAsignar}>
                        <UserRoundPlus />
                        Asignar orden
                    </Boton>
                ) : null}
                <Boton asChild variante="secundario" tamano="sm" className="rounded-lg">
                    <Link href="/despacho">
                        <Route />
                        Ruta
                    </Link>
                </Boton>
            </div>
        </section>
    );
}

/** Diálogo corto (1 campo): una orden sin técnico para el técnico elegido (`POST /ordenes/{id}/asignar`). */
function DialogoAsignarOrden({
    tecnico,
    ordenes,
    alCerrar,
    alHecho,
}: {
    tecnico: TecnicoHoy;
    ordenes: readonly Orden[];
    alCerrar: () => void;
    alHecho: () => void;
}) {
    const form = useForm<{ ordenId: string }>({ defaultValues: { ordenId: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const [error, setError] = useState<unknown>(null);
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const elegida = useWatch({ control: form.control, name: "ordenId" });
    const asignables = ordenes.filter((o) => o.estado === "pendiente" || o.estado === "programada");
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido
                titulo={`Asignar a ${tecnico.nombre}`}
                descripcion="Elige una orden sin técnico. Si ya tenía hora, se le guarda esa misma hora en su agenda."
            >
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async (d) => {
                        const o = asignables.find((x) => String(x.id) === d.ordenId);
                        if (!o) return;
                        setError(null);
                        try {
                            const nueva = await resolver(
                                api().POST("/api/v1/ordenes/{id}/asignar", {
                                    params: { path: { id: o.id }, header: { "Idempotency-Key": clave } },
                                    body: {
                                        version: o.version,
                                        tecnicoId: tecnico.usuarioId,
                                        ...(o.cuadrillaId !== null ? { cuadrillaId: o.cuadrillaId } : {}),
                                    },
                                }),
                            );
                            setClave(nuevaClaveIdempotencia());
                            avisar({ titulo: avisoOrden("asignar", nueva, [{ id: tecnico.usuarioId, nombre: tecnico.nombre }]) });
                            alHecho();
                        } catch (e) {
                            setClave(nuevaClaveIdempotencia());
                            setError(e);
                        }
                    })}
                >
                    <CampoSelector etiqueta="Orden" autoFocus {...form.register("ordenId")}>
                        <option value="">Elegir orden…</option>
                        {asignables.map((o) => (
                            <option key={o.id} value={o.id}>
                                {o.numero} · {TIPOS_ORDEN[o.tipo].texto} · {o.direccion}
                                {o.programadaInicio && diaDesdeIso(o.programadaInicio) ? ` · ${horaDe(o.programadaInicio)}` : ""}
                            </option>
                        ))}
                    </CampoSelector>
                    {asignables.length === 0 ? <p className="text-sm text-muted-foreground">No hay órdenes sin técnico para asignar.</p> : null}
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo asignar">
                            {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Asignando…" faltan={elegida === "" ? ["la orden"] : []}>
                            Asignar
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
