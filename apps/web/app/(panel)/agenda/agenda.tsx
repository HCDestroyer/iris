"use client";

import { GrupoSegmentado, Tarjeta } from "@iris/ui";
import { CalendarDays, CalendarRange, LayoutGrid } from "lucide-react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useRef } from "react";
import { VistaMes } from "@/componentes/agenda/vista-mes";
import { AgendaTecnicos } from "@/componentes/despacho/agenda-tecnicos";
import { AvisoCatalogos } from "@/componentes/despacho/catalogos";
import { FiltrosAgenda, NavegacionDias } from "@/componentes/despacho/controles";
import { useCatalogosAgenda, useDatosAgenda, type FiltrosDespacho } from "@/componentes/despacho/datos";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { cuadriculaMes, etiquetaMes, vistaDeUrl, type VistaAgenda } from "@/lib/agenda/mes";
import { api, resolver } from "@/lib/api/cliente";
import type { Tecnico } from "@/lib/despacho/calendario";
import { esDia, etiquetaDia, etiquetaDiaLarga, hoyGuatemala, inicioSemana, sumarDias, type Dia } from "@/lib/despacho/fechas";
import { useLectura } from "@/lib/lectura";

const idDe = (v: string | null) => (v !== null && /^\d{1,15}$/.test(v) ? Number(v) : null);

/** Estado de la agenda en la URL (`?vista=&dia=&tecnico=&cuadrilla=&zona=`): se puede compartir y volver atrás. */
function useEstadoAgenda() {
    const params = useSearchParams();
    const router = useRouter();
    const ruta = usePathname();
    const { vista, dia } = vistaDeUrl(params.get("vista"), params.get("dia"), hoyGuatemala(), esDia);
    const filtros: FiltrosDespacho = { tecnicoId: idDe(params.get("tecnico")), cuadrillaId: idDe(params.get("cuadrilla")), zonaId: idDe(params.get("zona")) };
    // Lo último pedido: dos cambios seguidos (antes de que la URL nueva llegue al render) se suman.
    const clave = params.toString();
    const ultimo = useRef({ vista, dia, filtros });
    useEffect(() => {
        ultimo.current = { vista, dia, filtros };
        // Solo cuando cambia la URL (vista, día y filtros salen de ella).
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [clave]);
    const ir = (cambios: { vista?: VistaAgenda; dia?: Dia; filtros?: FiltrosDespacho }, apilar = false) => {
        const v = cambios.vista ?? ultimo.current.vista;
        const d = cambios.dia ?? ultimo.current.dia;
        // Los filtros llegan completos desde el render: se aplica solo lo que cambió respecto a él.
        const f = { ...ultimo.current.filtros };
        if (cambios.filtros) {
            for (const k of ["tecnicoId", "cuadrillaId", "zonaId"] as const) {
                if ((cambios.filtros[k] ?? null) !== (filtros[k] ?? null)) f[k] = cambios.filtros[k] ?? null;
            }
        }
        ultimo.current = { ...ultimo.current, vista: v, dia: d, filtros: f };
        const q = new URLSearchParams();
        if (v !== "mes") q.set("vista", v);
        if (d !== hoyGuatemala()) q.set("dia", d);
        if (f.tecnicoId) q.set("tecnico", String(f.tecnicoId));
        if (f.cuadrillaId !== null) q.set("cuadrilla", String(f.cuadrillaId));
        if (f.zonaId !== null) q.set("zona", String(f.zonaId));
        const url = q.size > 0 ? `${ruta}?${q.toString()}` : ruta;
        if (apilar) router.push(url, { scroll: false });
        else router.replace(url, { scroll: false });
    };
    return { vista, dia, filtros, ir };
}

/**
 * Técnicos para el filtro (activos e inactivos: un inactivo puede tener citas), de la cuadrilla y
 * la zona elegidas. Hasta 200: si hay más, el filtro lo dice.
 */
function useTecnicosFiltro(cuadrillaId: number | null, zonaId: number | null) {
    return useLectura<{ lista: Tecnico[]; cortada: boolean }>(async (signal) => {
        const p = await resolver(
            api().GET("/api/v1/tecnicos", {
                params: { query: { limit: 200, ...(cuadrillaId !== null ? { cuadrillaId } : {}), ...(zonaId !== null ? { zonaId } : {}) } },
                signal,
            }),
        );
        return { lista: p.data ?? [], cortada: p.nextCursor !== null };
    });
}

function FiltrosConTecnicos(props: Omit<Parameters<typeof FiltrosAgenda>[0], "tecnicos">) {
    const tecnicos = useTecnicosFiltro(props.filtros.cuadrillaId, props.filtros.zonaId);
    return <FiltrosAgenda {...props} tecnicos={tecnicos} />;
}

/**
 * Agenda: calendario de mes (por día, cuántas órdenes y en qué estado), semana y día (por técnico
 * y franja horaria, con las órdenes sin programar). Al tocar un día del mes se abre su detalle,
 * desde donde se programa o asigna con las acciones explícitas de campo.
 */
export function PantallaAgenda() {
    const { vista, dia, filtros, ir } = useEstadoAgenda();
    const catalogos = useCatalogosAgenda();
    const desde = vista === "semana" ? inicioSemana(dia) : dia;
    const titulo = vista === "mes" ? etiquetaMes(dia) : vista === "dia" ? etiquetaDiaLarga(dia) : `Semana del ${etiquetaDia(desde)} al ${etiquetaDia(sumarDias(desde, 6))}`;
    const claveFiltros = `${filtros.tecnicoId}|${filtros.cuadrillaId}|${filtros.zonaId}`;

    return (
        <>
            <EncabezadoPagina
                titulo="Agenda"
                descripcion="Toca un día para ver sus órdenes por técnico y programar las pendientes. En semana y día, arrastra una cita o usa «Mover a…»."
            />
            <Tarjeta className="mb-4 grid gap-3 p-4 [&>*]:min-w-0">
                <div className="flex flex-wrap items-end gap-3">
                    <GrupoSegmentado
                        etiqueta="Vista"
                        valor={vista}
                        alCambiar={(v) => ir({ vista: v })}
                        opciones={[
                            { valor: "mes", etiqueta: "Mes", icono: LayoutGrid },
                            { valor: "semana", etiqueta: "Semana", icono: CalendarRange },
                            { valor: "dia", etiqueta: "Día", icono: CalendarDays },
                        ]}
                    />
                    <NavegacionDias dia={dia} paso={vista === "dia" ? 1 : vista === "semana" ? 7 : "mes"} alCambiar={(d) => ir({ dia: d })} />
                </div>
                <FiltrosConTecnicos
                    key={`${filtros.cuadrillaId}|${filtros.zonaId}`}
                    filtros={filtros}
                    alCambiar={(f) => ir({ filtros: f })}
                    zonas={catalogos.zonas}
                    cuadrillas={catalogos.cuadrillas}
                />
            </Tarjeta>
            <AvisoCatalogos {...catalogos} />
            <Tarjeta className="grid gap-3 p-4 [&>*]:min-w-0">
                <h2 className="text-base font-semibold first-letter:uppercase" aria-live="polite">
                    {titulo}
                </h2>
                {vista === "mes" ? (
                    <VistaMes key={`${cuadriculaMes(dia).desde}|${claveFiltros}`} dia={dia} filtros={filtros} alElegirDia={(d) => ir({ vista: "dia", dia: d }, true)} />
                ) : (
                    <Calendario key={`${vista}|${desde}|${claveFiltros}`} vista={vista} desde={desde} filtros={filtros} catalogos={catalogos} />
                )}
            </Tarjeta>
        </>
    );
}

function Calendario({ vista, desde, filtros, catalogos }: { vista: "dia" | "semana"; desde: Dia; filtros: FiltrosDespacho; catalogos: ReturnType<typeof useCatalogosAgenda> }) {
    const datos = useDatosAgenda(desde, vista === "dia" ? 1 : 7, filtros);
    return (
        <AgendaTecnicos
            datos={datos}
            vista={vista}
            desde={desde}
            zonas={catalogos.zonas.estado === "listo" ? catalogos.zonas.datos : undefined}
            cuadrillas={catalogos.cuadrillas.estado === "listo" ? catalogos.cuadrillas.datos : undefined}
        />
    );
}
