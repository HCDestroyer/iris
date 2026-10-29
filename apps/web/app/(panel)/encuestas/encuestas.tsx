"use client";

import { Aviso, Boton, Campo, CampoSelector, Casilla, Esqueleto, EstadoVacio, Tarjeta } from "@iris/ui";
import { CalendarRange, FilterX, MessageSquareHeart } from "lucide-react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { CalificacionEncuesta, InsigniaAlerta, InsigniaEstadoEncuesta } from "@/componentes/encuestas/calificacion";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import { api, resolver } from "@/lib/api/cliente";
import {
    consultaEncuestas,
    errorFechasEncuestas,
    ESTADOS_FILTRO,
    filtrosEncuestasDesdeUrl,
    hayFiltrosEncuestas,
    urlEncuestas,
    FILTROS_ENCUESTAS_VACIOS,
    type ConsultaEncuestas,
    type FiltrosEncuestas,
} from "@/lib/encuestas/filtros";
import { CALIFICACIONES, ENTIDADES, presentacionEstado, TEXTO_CALIFICACION, type Encuesta } from "@/lib/encuestas/presentacion";
import { usePaginado } from "@/lib/paginado";
import { textoFechaHora } from "@/lib/reportes/presentacion";

/** `/encuestas`: encuestas de satisfacción con cursor y filtros en la URL (`encuestas.ver`). */
export function Encuestas() {
    const router = useRouter();
    const params = useSearchParams();
    const filtros = filtrosEncuestasDesdeUrl(params);
    const cambiar = (parcial: Partial<FiltrosEncuestas>) => router.replace(urlEncuestas({ ...filtros, ...parcial }), { scroll: false });
    const errorFechas = errorFechasEncuestas(filtros);
    const consulta = consultaEncuestas(filtros);

    return (
        <>
            <EncabezadoPagina titulo="Encuestas" descripcion="Satisfacción de los clientes tras resolver un ticket o cerrar una instalación o visita." />

            <Tarjeta className="mb-4 grid gap-4 p-4" role="search" aria-label="Filtros de encuestas">
                <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
                    <Campo etiqueta="Creadas desde" type="date" value={filtros.desde} max={filtros.hasta || undefined} onChange={(e) => cambiar({ desde: e.target.value })} />
                    <Campo etiqueta="Hasta" type="date" value={filtros.hasta} min={filtros.desde || undefined} onChange={(e) => cambiar({ hasta: e.target.value })} />
                    <CampoSelector etiqueta="Tipo" value={filtros.entidad} onChange={(e) => cambiar({ entidad: e.target.value === "ticket" || e.target.value === "orden" ? e.target.value : "" })}>
                        <option value="">Tickets y órdenes</option>
                        <option value="ticket">Tickets</option>
                        <option value="orden">Órdenes</option>
                    </CampoSelector>
                    <CampoSelector etiqueta="Estado" value={filtros.estado} onChange={(e) => cambiar({ estado: (ESTADOS_FILTRO as readonly string[]).includes(e.target.value) ? (e.target.value as FiltrosEncuestas["estado"]) : "" })}>
                        <option value="">Todos</option>
                        {ESTADOS_FILTRO.map((e) => (
                            <option key={e} value={e}>
                                {presentacionEstado(e).texto}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
                <fieldset className="grid gap-2">
                    <legend className="mb-1 text-sm font-medium">Calificación</legend>
                    <div className="flex flex-wrap gap-x-5 gap-y-2">
                        {[...CALIFICACIONES].reverse().map((c) => (
                            <Casilla
                                key={c}
                                etiqueta={`${c} · ${TEXTO_CALIFICACION[c].texto}`}
                                checked={filtros.calificaciones.includes(c)}
                                onChange={(e) => {
                                    const marcada = e.target.checked;
                                    cambiar({ calificaciones: CALIFICACIONES.filter((x) => (x === c ? marcada : filtros.calificaciones.includes(x))) });
                                }}
                            />
                        ))}
                    </div>
                </fieldset>
                {hayFiltrosEncuestas(filtros) ? (
                    <Boton variante="fantasma" tamano="sm" className="justify-self-start" onClick={() => cambiar(FILTROS_ENCUESTAS_VACIOS)}>
                        <FilterX />
                        Quitar filtros
                    </Boton>
                ) : null}
            </Tarjeta>

            {errorFechas ? (
                <Aviso tono="warning" titulo="Revisa las fechas" icono={CalendarRange}>
                    {errorFechas}.
                </Aviso>
            ) : (
                <ListaEncuestas key={JSON.stringify(consulta)} consulta={consulta} hayFiltros={hayFiltrosEncuestas(filtros)} />
            )}
        </>
    );
}

function ListaEncuestas({ consulta, hayFiltros }: { consulta: ConsultaEncuestas; hayFiltros: boolean }) {
    const paginado = usePaginado<Encuesta>((cursor, signal) =>
        resolver(api().GET("/api/v1/encuestas", { params: { query: { ...consulta, ...(cursor ? { cursor } : {}) } }, signal })),
    );
    const { primera } = paginado;
    if (primera.estado === "cargando") {
        return (
            <div className="grid gap-3" role="status" aria-label="Cargando encuestas">
                <Esqueleto className="h-24" />
                <Esqueleto className="h-24" />
                <Esqueleto className="h-24" />
            </div>
        );
    }
    if (primera.estado === "error") {
        return <ErrorLectura error={primera.error} reintentar={primera.reintentar} que="las encuestas" />;
    }
    if (paginado.items.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio
                    icono={MessageSquareHeart}
                    titulo={hayFiltros ? "Ninguna encuesta con estos filtros" : "Todavía no hay encuestas"}
                    descripcion={hayFiltros ? "Prueba con otras fechas o quita filtros." : "Se envían al resolver un ticket o cerrar con éxito una instalación o visita."}
                />
            </Tarjeta>
        );
    }
    return (
        <div className="grid gap-3">
            <ul className="grid gap-3" aria-label="Encuestas">
                {paginado.items.map((e) => (
                    <FilaEncuesta key={e.id} encuesta={e} />
                ))}
            </ul>
            <PieLista paginado={paginado} que="encuestas" />
        </div>
    );
}

function FilaEncuesta({ encuesta: e }: { encuesta: Encuesta }) {
    const href = e.ticketId ? `/tickets/${e.ticketId}` : e.ordenId ? `/ordenes/${e.ordenId}` : null;
    return (
        <li className="surface-card grid gap-2 p-4" data-encuesta={e.id}>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                <span className="text-xs text-muted-foreground">{ENTIDADES[e.entidad]}</span>
                {href ? (
                    <Link href={href} className="font-medium text-primary underline-offset-4 hover:underline">
                        {e.numero}
                    </Link>
                ) : (
                    <span className="font-medium">{e.numero}</span>
                )}
                <InsigniaEstadoEncuesta estado={e.estado} />
                <CalificacionEncuesta calificacion={e.calificacion} />
                {e.alerta ? <InsigniaAlerta /> : null}
            </div>
            {e.comentario ? <p className="line-clamp-3 text-sm break-words whitespace-pre-line">«{e.comentario}»</p> : null}
            <p className="text-xs text-muted-foreground tabular-nums">
                Creada {textoFechaHora(e.creadaEn)} · {e.canal === "correo" ? "por correo" : "por enlace"}
                {e.respondidaEn ? ` · respondida ${textoFechaHora(e.respondidaEn)}` : e.estado === "pendiente" ? ` · vence ${textoFechaHora(e.expiraEn)}` : ""}
            </p>
        </li>
    );
}
