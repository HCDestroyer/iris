"use client";

import { Aviso, Boton, CampoFecha, CampoSelector, Casilla, GrupoSegmentado, Tarjeta } from "@iris/ui";
import { CalendarRange, FilterX } from "lucide-react";
import { useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useZonas } from "@/componentes/comercial/comun";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { useCategorias } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import type { Cuadrilla, Tecnico } from "@/lib/despacho/calendario";
import { useLectura } from "@/lib/lectura";
import {
    ATAJOS,
    atajoDe,
    CON_CATEGORIA,
    DE_TICKETS,
    diasPara,
    errorDeRango,
    esReporte,
    filtrosDesdeUrl,
    periodoDe,
    queryDe,
    REPORTES,
    urlDe,
    type Atajo,
    type Filtros,
    type Reporte,
} from "@/lib/reportes/filtros";
import { NOMBRES_REPORTE } from "@/lib/reportes/presentacion";
import { useSesion } from "@/lib/sesion";
import { ORDEN_PRIORIDADES, PRIORIDADES } from "@/lib/tickets/presentacion";
import { ReportePrimeraVisita } from "./reporte-primera-visita";
import { ReporteReincidencias } from "./reporte-reincidencias";
import { ReporteResolucion } from "./reporte-resolucion";
import { ReporteSatisfaccion } from "./reporte-satisfaccion";
import { ReporteSla } from "./reporte-sla";
import { ReporteTecnicos } from "./reporte-tecnicos";

const OPCIONES_REPORTE = REPORTES.map((r) => ({ valor: r, etiqueta: NOMBRES_REPORTE[r].titulo }));
const OPCIONES_ATAJO = (Object.keys(ATAJOS) as Atajo[]).map((a) => ({ valor: a, etiqueta: ATAJOS[a] }));
const VENTANAS: Partial<Record<Reporte, number[]>> = {
    "primera-visita": [7, 15, 30, 60, 90],
    reincidencias: [1, 7, 15, 30, 60, 90],
};

/** Cuadrillas y técnicos de la agenda (`agenda.ver`); sin el permiso no se piden. */
function useCatalogoAgenda<T>(ruta: "/api/v1/cuadrillas" | "/api/v1/tecnicos") {
    const { puede } = useSesion();
    const permitido = puede(["agenda.ver"]);
    return useLectura<T[] | null>(async (signal) => {
        if (!permitido) return null;
        const pagina =
            ruta === "/api/v1/tecnicos"
                ? await resolver(api().GET("/api/v1/tecnicos", { params: { query: { limit: 200 } }, signal }))
                : await resolver(api().GET("/api/v1/cuadrillas", { signal }));
        return (pagina.data ?? []) as T[];
    });
}

/**
 * `/reportes`: SLA, tiempo de resolución, órdenes por técnico, primera visita y reincidencias,
 * con los filtros en la URL. Cada reporte se lee entero o falla entero (nunca totales parciales).
 */
export function Reportes() {
    const router = useRouter();
    const params = useSearchParams();
    const { puede } = useSesion();
    const reporte: Reporte = esReporte(params.get("reporte")) ? (params.get("reporte") as Reporte) : "sla";
    const filtros = filtrosDesdeUrl(params);
    const ir = (r: Reporte, f: Filtros) => router.replace(urlDe(r, f), { scroll: false });
    const cambiar = (parcial: Partial<Filtros>) => ir(reporte, { ...filtros, ...parcial });

    const zonas = useZonas();
    const cuadrillas = useCatalogoAgenda<Cuadrilla>("/api/v1/cuadrillas");
    const tecnicos = useCatalogoAgenda<Tecnico>("/api/v1/tecnicos");
    const verTickets = puede(["tickets.ver"]);
    const categorias = useCategorias();

    const errorRango = errorDeRango(filtros.desde, filtros.hasta);
    const query = queryDe(reporte, filtros);
    const deTickets = DE_TICKETS.includes(reporte);
    const conCategoria = CON_CATEGORIA.includes(reporte);
    const ventanas = VENTANAS[reporte];
    const dias = diasPara(reporte, filtros);
    const hayFiltros =
        filtros.zonaId !== null ||
        filtros.cuadrillaId !== null ||
        filtros.tecnicoId !== null ||
        filtros.categoriaId !== null ||
        filtros.prioridades.length > 0 ||
        filtros.dias !== null ||
        filtros.entidad !== null;

    const sinAgenda = !puede(["agenda.ver"]);
    const ayudaCatalogo = (estado: "cargando" | "error" | "listo") =>
        sinAgenda ? "Requiere el permiso de ver la agenda." : estado === "error" ? "No se pudo cargar la lista." : undefined;

    return (
        <>
            <EncabezadoPagina titulo="Reportes" descripcion="Indicadores de soporte y de campo por período, en la hora de Guatemala." />

            <div className="mb-4 hidden md:block">
                <GrupoSegmentado etiqueta="Reporte" opciones={OPCIONES_REPORTE} valor={reporte} alCambiar={(r) => ir(r, filtros)} className="flex-wrap" />
            </div>
            <CampoSelector etiqueta="Reporte" className="mb-4 md:hidden" value={reporte} onChange={(e) => ir(e.target.value as Reporte, filtros)}>
                {OPCIONES_REPORTE.map((o) => (
                    <option key={o.valor} value={o.valor}>
                        {o.etiqueta}
                    </option>
                ))}
            </CampoSelector>

            <Tarjeta className="mb-4 grid gap-4 p-4" role="search" aria-label="Filtros del reporte">
                <Periodo desde={filtros.desde} hasta={filtros.hasta} alCambiar={(desde, hasta) => cambiar({ desde, hasta })} />

                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                    <CampoSelector
                        etiqueta="Zona"
                        value={filtros.zonaId ?? ""}
                        disabled={sinAgenda}
                        ayuda={ayudaCatalogo(zonas.estado)}
                        onChange={(e) => cambiar({ zonaId: e.target.value ? Number(e.target.value) : null })}
                    >
                        <option value="">Todas</option>
                        {opciones(zonas.estado === "listo" ? zonas.datos : null, filtros.zonaId, (z) => z.id, (z) => z.nombre, "Zona")}
                    </CampoSelector>
                    <CampoSelector
                        etiqueta="Cuadrilla"
                        value={filtros.cuadrillaId ?? ""}
                        disabled={sinAgenda}
                        ayuda={ayudaCatalogo(cuadrillas.estado)}
                        onChange={(e) => cambiar({ cuadrillaId: e.target.value ? Number(e.target.value) : null })}
                    >
                        <option value="">Todas</option>
                        {opciones(cuadrillas.estado === "listo" ? cuadrillas.datos : null, filtros.cuadrillaId, (c) => c.id, (c) => c.nombre, "Cuadrilla")}
                    </CampoSelector>
                    <CampoSelector
                        etiqueta={deTickets || reporte === "satisfaccion" ? "Técnico o responsable" : "Técnico"}
                        value={filtros.tecnicoId ?? ""}
                        disabled={sinAgenda}
                        ayuda={ayudaCatalogo(tecnicos.estado)}
                        onChange={(e) => cambiar({ tecnicoId: e.target.value ? Number(e.target.value) : null })}
                    >
                        <option value="">Todos</option>
                        {opciones(tecnicos.estado === "listo" ? tecnicos.datos : null, filtros.tecnicoId, (t) => t.usuarioId, (t) => t.nombre, "Técnico")}
                    </CampoSelector>
                    {ventanas && dias !== undefined ? (
                        <CampoSelector
                            etiqueta={reporte === "reincidencias" ? "Volvió dentro de" : "Sin retorno durante"}
                            value={dias}
                            onChange={(e) => cambiar({ dias: Number(e.target.value) })}
                            ayuda={reporte === "reincidencias" ? "Días tras la resolución." : "Días tras el cierre."}
                        >
                            {(ventanas.includes(dias) ? ventanas : [...ventanas, dias].sort((a, b) => a - b)).map((d) => (
                                <option key={d} value={d}>
                                    {d === 1 ? "1 día" : `${d} días`}
                                </option>
                            ))}
                        </CampoSelector>
                    ) : null}
                    {reporte === "satisfaccion" ? (
                        <CampoSelector
                            etiqueta="Tipo"
                            value={filtros.entidad ?? ""}
                            onChange={(e) => cambiar({ entidad: e.target.value === "ticket" || e.target.value === "orden" ? e.target.value : null })}
                        >
                            <option value="">Tickets y órdenes</option>
                            <option value="ticket">Tickets</option>
                            <option value="orden">Órdenes</option>
                        </CampoSelector>
                    ) : null}
                    {conCategoria ? (
                        <CampoSelector
                            etiqueta="Categoría"
                            value={filtros.categoriaId ?? ""}
                            disabled={!verTickets}
                            ayuda={!verTickets ? "Requiere el permiso de ver tickets." : categorias.estado === "error" ? "No se pudo cargar la lista." : undefined}
                            onChange={(e) => cambiar({ categoriaId: e.target.value ? Number(e.target.value) : null })}
                        >
                            <option value="">Todas</option>
                            {opciones(categorias.estado === "listo" && verTickets ? categorias.datos : null, filtros.categoriaId, (c) => c.id, (c) => c.nombre, "Categoría")}
                        </CampoSelector>
                    ) : null}
                </div>

                {deTickets ? (
                    <fieldset className="grid gap-2">
                        <legend className="mb-1 text-sm font-medium">Prioridad</legend>
                        <div className="flex flex-wrap gap-x-5 gap-y-2">
                            {[...ORDEN_PRIORIDADES].reverse().map((p) => (
                                <Casilla
                                    key={p}
                                    etiqueta={PRIORIDADES[p].texto}
                                    checked={filtros.prioridades.includes(p)}
                                    onChange={(e) => {
                                        const marcada = e.target.checked;
                                        cambiar({ prioridades: ORDEN_PRIORIDADES.filter((x) => (x === p ? marcada : filtros.prioridades.includes(x))) });
                                    }}
                                />
                            ))}
                        </div>
                    </fieldset>
                ) : null}

                {hayFiltros ? (
                    <Boton
                        variante="fantasma"
                        tamano="sm"
                        className="justify-self-start"
                        onClick={() =>
                            ir(reporte, { ...filtros, zonaId: null, cuadrillaId: null, tecnicoId: null, categoriaId: null, prioridades: [], dias: null, entidad: null })
                        }
                    >
                        <FilterX />
                        Quitar filtros
                    </Boton>
                ) : null}
            </Tarjeta>

            {errorRango ? (
                <Aviso tono="warning" titulo="Revisa el período" icono={CalendarRange}>
                    {errorRango}.
                </Aviso>
            ) : (
                <ReporteActivo key={`${reporte}|${JSON.stringify(query)}`} reporte={reporte} query={query} />
            )}
        </>
    );
}

function ReporteActivo({ reporte, query }: { reporte: Reporte; query: ReturnType<typeof queryDe> }) {
    switch (reporte) {
        case "sla":
            return <ReporteSla query={query} />;
        case "resolucion":
            return <ReporteResolucion query={query} />;
        case "tecnicos":
            return <ReporteTecnicos query={query} />;
        case "primera-visita":
            return <ReportePrimeraVisita query={query} />;
        case "reincidencias":
            return <ReporteReincidencias query={query} />;
        case "satisfaccion":
            return <ReporteSatisfaccion query={query} />;
    }
}

/** Opciones de un catálogo; el valor elegido que no está en la lista aparece por número. */
function opciones<T>(lista: readonly T[] | null, elegido: number | null, id: (x: T) => number, nombre: (x: T) => string, que: string) {
    const items = (lista ?? []).map((x) => ({ id: id(x), nombre: nombre(x) }));
    if (elegido !== null && !items.some((x) => x.id === elegido)) items.unshift({ id: elegido, nombre: `${que} ${elegido}` });
    return items.map((x) => (
        <option key={x.id} value={x.id}>
            {x.nombre}
        </option>
    ));
}

/** Período: atajos y las dos fechas. Una fecha a medio escribir no se aplica hasta ser válida. */
function Periodo({ desde, hasta, alCambiar }: { desde: string; hasta: string; alCambiar: (desde: string, hasta: string) => void }) {
    const [borrador, setBorrador] = useState({ desde, hasta });
    // La fecha se escribe por partes (día, mes, año) y cada parte puede dejar una fecha válida a
    // medias (15/01 mientras se escribe 15/11): el período se aplica 500 ms después del último
    // cambio, y el campo no se vuelve a montar (perdería el foco a medio escribir). Si la URL cambia
    // por otra cosa (atajo, «Quitar filtros», Atrás), el borrador toma el período nuevo; el eco de lo
    // que se mandó desde aquí (aunque llegue tarde) no pisa lo que se sigue escribiendo.
    const actual = `${desde}|${hasta}`;
    const [previo, setPrevio] = useState(actual);
    const [enviados, setEnviados] = useState<readonly string[]>([]);
    if (actual !== previo) {
        setPrevio(actual);
        const eco = enviados.indexOf(actual);
        if (eco >= 0) {
            // El eco se consume (con los anteriores): si después se vuelve a ese período con Atrás,
            // ya no es un eco y los campos lo toman.
            setEnviados(enviados.slice(eco + 1));
        } else {
            setBorrador({ desde, hasta });
            setEnviados([]);
        }
    }
    // Solo lo que la persona escribió; un período inválido en la URL lo explica el aviso de abajo.
    const editado = borrador.desde !== desde || borrador.hasta !== hasta;
    const error = editado ? errorDeRango(borrador.desde, borrador.hasta) : undefined;
    const espera = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
    useEffect(() => () => clearTimeout(espera.current), []);
    const editar = (parcial: Partial<typeof borrador>) => {
        const nuevo = { ...borrador, ...parcial };
        setBorrador(nuevo);
        clearTimeout(espera.current);
        if (errorDeRango(nuevo.desde, nuevo.hasta)) return;
        espera.current = setTimeout(() => {
            setEnviados((e) => [...e.slice(-9), `${nuevo.desde}|${nuevo.hasta}`]);
            alCambiar(nuevo.desde, nuevo.hasta);
        }, 500);
    };
    return (
        <div className="grid gap-3 lg:grid-cols-[auto_minmax(0,1fr)] lg:items-end">
            <div className="grid gap-1.5">
                <span className="text-sm font-medium" aria-hidden>Período</span>
                <GrupoSegmentado
                    etiqueta="Período"
                    opciones={OPCIONES_ATAJO}
                    valor={atajoDe({ desde, hasta })}
                    alCambiar={(a) => {
                        // Un atajo gana a lo que se estaba escribiendo: la espera de 500 ms no lo pisa.
                        clearTimeout(espera.current);
                        const p = periodoDe(a);
                        alCambiar(p.desde, p.hasta);
                    }}
                    className="flex-wrap justify-self-start"
                />
            </div>
            <div className="grid grid-cols-2 gap-3 sm:max-w-md">
                <CampoFecha etiqueta="Desde" value={borrador.desde} max={borrador.hasta || undefined} onChange={(e) => editar({ desde: e.target.value })} />
                <CampoFecha etiqueta="Hasta" value={borrador.hasta} min={borrador.desde || undefined} onChange={(e) => editar({ hasta: e.target.value })} />
            </div>
            {error ? (
                <p className="text-sm text-error lg:col-span-2" role="status">
                    {error}. El reporte sigue con el período anterior.
                </p>
            ) : null}
        </div>
    );
}
