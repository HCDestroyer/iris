import { Migas, Tarjeta, TarjetaContenido } from "@iris/ui";
import type { Metadata } from "next";
import { BuscadorManual } from "@/componentes/manual/buscador";
import { MarkdownManual } from "@/componentes/manual/markdown";
import { entradasBusqueda, INDICE_MANUAL, rutaDeClave, rutasDe } from "@/lib/manual/contenido";

export const metadata: Metadata = { title: "Manual" };

/** Índice del manual con el buscador (docs/manual/indice.md). */
export default function PaginaIndiceManual() {
    return (
        <>
            <Migas migas={[{ etiqueta: "Manual" }]} />
            <h1 className="text-[1.375rem] font-semibold tracking-[-0.015em]">Manual de Iris</h1>
            <BuscadorManual entradas={entradasBusqueda()} rutas={rutasDe()}>
                <Tarjeta>
                    <TarjetaContenido className="p-5 sm:p-7">
                        <MarkdownManual fuente={INDICE_MANUAL} ruta={rutaDeClave} sinTitulo />
                    </TarjetaContenido>
                </Tarjeta>
            </BuscadorManual>
        </>
    );
}
