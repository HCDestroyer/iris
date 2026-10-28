"use client";

import { Boton, Esqueleto, EstadoVacio, GrupoSegmentado, Tarjeta, type OpcionSegmentada } from "@iris/ui";
import { ChevronRight, LifeBuoy, Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ErrorPortal } from "@/componentes/portal/error-portal";
import { PieLista } from "@/componentes/pie-lista";
import { AvisoFallaGeneral, InsigniaEstadoPortal, InsigniaFallaGeneral } from "@/componentes/portal/insignias";
import { resolver } from "@/lib/api/cliente";
import { formatearFecha } from "@/lib/fechas";
import { usePaginado } from "@/lib/paginado";
import { apiPortal } from "@/lib/portal/cliente";
import { ESTADOS_EN_CURSO, type TicketPortal } from "@/lib/portal/presentacion";
import { useSesionPortal } from "@/lib/portal/sesion";

type Filtro = "en_curso" | "todos";

const FILTROS: readonly OpcionSegmentada<Filtro>[] = [
    { valor: "en_curso", etiqueta: "En curso" },
    { valor: "todos", etiqueta: "Todos" },
];

const PAGINA = 20;

export function MisCasos() {
    const { cliente } = useSesionPortal();
    const [filtro, setFiltro] = useState<Filtro>("en_curso");
    const primerNombre = cliente.nombre.trim().split(/\s+/)[0] ?? "";
    return (
        <div className="grid gap-5">
            <div className="flex flex-wrap items-end justify-between gap-3">
                <div className="grid gap-1">
                    <h1 className="text-2xl font-semibold tracking-[-0.015em]">{primerNombre ? `Hola, ${primerNombre}` : "Hola"}</h1>
                    <p className="text-sm text-muted-foreground">Aquí ves tus casos de soporte y lo que te respondimos.</p>
                </div>
                <Boton asChild>
                    <Link href="/soporte/nuevo">
                        <Plus />
                        Reportar un problema
                    </Link>
                </Boton>
            </div>
            <GrupoSegmentado etiqueta="Mostrar" opciones={FILTROS} valor={filtro} alCambiar={setFiltro} className="justify-self-start" />
            <Lista key={filtro} filtro={filtro} />
        </div>
    );
}

function Lista({ filtro }: { filtro: Filtro }) {
    const paginado = usePaginado<TicketPortal>((cursor, signal) =>
        resolver(
            apiPortal().GET("/api/v1/portal/tickets", {
                params: { query: { limit: PAGINA, ...(cursor ? { cursor } : {}), ...(filtro === "en_curso" ? { estado: ESTADOS_EN_CURSO } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const { primera, items } = paginado;
    if (primera.estado === "cargando") {
        return (
            <div className="grid gap-3" aria-busy="true">
                <Esqueleto className="h-24" />
                <Esqueleto className="h-24" />
            </div>
        );
    }
    if (primera.estado === "error") {
        return <ErrorPortal error={primera.error} que="tus casos" reintentar={primera.reintentar} />;
    }
    if (items.length === 0) {
        return (
            <Tarjeta>
                <EstadoVacio
                    icono={LifeBuoy}
                    titulo={filtro === "en_curso" ? "No tienes casos en curso" : "Aún no tienes casos"}
                    descripcion="Si algo no funciona con tu servicio, cuéntanos y te ayudamos."
                    accion={
                        <Boton asChild variante="secundario">
                            <Link href="/soporte/nuevo">
                                <Plus />
                                Reportar un problema
                            </Link>
                        </Boton>
                    }
                />
            </Tarjeta>
        );
    }
    const hayFalla = items.some((t) => t.afectadoPorIncidente);
    return (
        <div className="grid gap-3">
            {hayFalla ? <AvisoFallaGeneral /> : null}
            <ul className="grid gap-3" aria-label="Mis casos">
                {items.map((t) => (
                    <li key={t.id}>
                        <Link
                            href={`/soporte/${t.id}`}
                            data-ticket={t.numero}
                            className="surface-card group flex items-center gap-3 p-4 transition-colors hover:border-primary/50 focus-visible:outline-2 focus-visible:outline-offset-2"
                        >
                            <div className="grid min-w-0 flex-1 gap-1.5">
                                <div className="flex flex-wrap items-center gap-2">
                                    <InsigniaEstadoPortal estado={t.estado} />
                                    {t.afectadoPorIncidente ? <InsigniaFallaGeneral /> : null}
                                    <span className="font-mono text-xs text-muted-foreground">{t.numero}</span>
                                </div>
                                <span className="font-medium break-words">{t.asunto}</span>
                                <span className="text-xs text-muted-foreground tabular-nums">
                                    {t.categoria ? `${t.categoria} · ` : ""}Actualizado {formatearFecha(t.actualizadoEn)}
                                </span>
                                {t.puedeConfirmar ? <span className="text-sm text-success">Confírmanos si quedó resuelto</span> : null}
                            </div>
                            <ChevronRight aria-hidden className="size-5 shrink-0 text-muted-foreground group-hover:text-foreground" />
                        </Link>
                    </li>
                ))}
            </ul>
            <PieLista paginado={paginado} que="casos" />
        </div>
    );
}
