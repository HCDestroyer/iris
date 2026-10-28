"use client";

import { GrupoSegmentado, Tarjeta } from "@iris/ui";
import { CalendarDays, CalendarRange } from "lucide-react";
import { useState } from "react";
import { AgendaTecnicos } from "@/componentes/despacho/agenda-tecnicos";
import type { Vista } from "@/componentes/despacho/calendario";
import { AvisoCatalogos } from "@/componentes/despacho/catalogos";
import { FiltrosAgenda, NavegacionDias } from "@/componentes/despacho/controles";
import { useCatalogosAgenda, useDatosAgenda, type FiltrosDespacho } from "@/componentes/despacho/datos";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { escribirLocal, leerLocal } from "@/lib/almacen";
import { etiquetaDia, etiquetaDiaLarga, hoyGuatemala, inicioSemana, sumarDias, type Dia } from "@/lib/despacho/fechas";

const CLAVE_VISTA = "iris-vista-agenda";

export function PantallaAgenda() {
    const [vista, setVista] = useState<Vista>(() => (leerLocal(CLAVE_VISTA) === "semana" ? "semana" : "dia"));
    const [dia, setDia] = useState<Dia>(() => hoyGuatemala());
    const [filtros, setFiltros] = useState<FiltrosDespacho>({ cuadrillaId: null, zonaId: null });
    const catalogos = useCatalogosAgenda();
    const desde = vista === "dia" ? dia : inicioSemana(dia);
    const titulo = vista === "dia" ? etiquetaDiaLarga(dia) : `Semana del ${etiquetaDia(desde)} al ${etiquetaDia(sumarDias(desde, 6))}`;

    return (
        <>
            <EncabezadoPagina titulo="Agenda" descripcion="Citas, ventanas y ausencias por técnico. Arrastra una cita para moverla o usa «Mover a…»." />
            <Tarjeta className="mb-4 flex flex-wrap items-end justify-between gap-3 p-4">
                <div className="flex flex-wrap items-end gap-3">
                    <GrupoSegmentado
                        etiqueta="Vista"
                        valor={vista}
                        alCambiar={(v) => {
                            setVista(v);
                            escribirLocal(CLAVE_VISTA, v);
                        }}
                        opciones={[
                            { valor: "dia", etiqueta: "Día", icono: CalendarDays },
                            { valor: "semana", etiqueta: "Semana", icono: CalendarRange },
                        ]}
                    />
                    <NavegacionDias dia={dia} paso={vista === "dia" ? 1 : 7} alCambiar={setDia} />
                </div>
                <FiltrosAgenda filtros={filtros} alCambiar={setFiltros} zonas={catalogos.zonas} cuadrillas={catalogos.cuadrillas} />
            </Tarjeta>
            <AvisoCatalogos {...catalogos} />
            <Tarjeta className="grid gap-3 p-4 [&>*]:min-w-0">
                <h2 className="text-base font-semibold first-letter:uppercase" aria-live="polite">
                    {titulo}
                </h2>
                <Calendario key={`${vista}|${desde}|${filtros.cuadrillaId}|${filtros.zonaId}`} vista={vista} desde={desde} filtros={filtros} catalogos={catalogos} />
            </Tarjeta>
        </>
    );
}

function Calendario({ vista, desde, filtros, catalogos }: { vista: Vista; desde: Dia; filtros: FiltrosDespacho; catalogos: ReturnType<typeof useCatalogosAgenda> }) {
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
