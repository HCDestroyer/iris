import { Migas } from "@iris/ui";
import type { Metadata } from "next";
import Link from "next/link";
import { BuscadorManual } from "@/componentes/manual/buscador";
import { entradasBusqueda, PAGINAS_CAMPO, rutasDe } from "@/lib/manual/contenido";

export const metadata: Metadata = { title: { absolute: "Manual · Iris Campo" } };

/** Índice de la sección del técnico, con buscador; todo funciona sin señal. */
export default function PaginaIndiceManualCampo() {
    return (
        <>
            <Migas migas={[{ etiqueta: "Manual de la app" }]} />
            <h1 className="text-[1.375rem] font-semibold tracking-[-0.015em]">Manual de la app del técnico</h1>
            <BuscadorManual entradas={entradasBusqueda(PAGINAS_CAMPO)} rutas={rutasDe(PAGINAS_CAMPO)}>
                <ul aria-label="Páginas de la app del técnico" className="grid gap-2">
                    {PAGINAS_CAMPO.map((p) => (
                        <li key={p.clave}>
                            <a
                                href={rutasDe([p])[p.clave]}
                                className="flex min-h-12 items-center rounded-[var(--hx-radius-card)] border border-border bg-card px-4 py-3 font-medium transition-colors hover:bg-elevated focus-visible:outline-2 focus-visible:outline-primary"
                            >
                                {p.titulo}
                            </a>
                        </li>
                    ))}
                </ul>
            </BuscadorManual>
            <p className="text-sm text-muted-foreground">
                El manual completo de Iris (panel y administración) está en{" "}
                <Link href="/manual" prefetch={false} className="font-medium text-primary underline underline-offset-2">
                    el manual de Iris
                </Link>
                ; se abre con señal.
            </p>
        </>
    );
}
