"use client";

import { InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { ChevronRight, Hourglass } from "lucide-react";
import Link from "next/link";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { SECCIONES } from "@/lib/administracion/secciones";
import { useSesion } from "@/lib/sesion";

/** Índice de Administración: solo las secciones que el usuario puede ver. */
export function IndiceAdministracion() {
    const { puede } = useSesion();
    const visibles = SECCIONES.filter((s) => puede(s.permisos));
    return (
        <>
            <EncabezadoPagina titulo="Administración" />
            <ul className="grid gap-3 sm:grid-cols-2" aria-label="Secciones de administración">
                {visibles.map((s) => {
                    const cuerpo = (
                        <Tarjeta className={s.href ? "h-full transition-colors hover:bg-elevated" : "h-full"}>
                            <TarjetaEncabezado className="flex flex-row items-start gap-3">
                                <s.icono aria-hidden className="mt-0.5 size-5 shrink-0 text-muted-foreground" />
                                <div className="grid min-w-0 flex-1 gap-1">
                                    <TarjetaTitulo>{s.titulo}</TarjetaTitulo>
                                    <TarjetaDescripcion>{s.descripcion}</TarjetaDescripcion>
                                </div>
                                {s.href ? <ChevronRight aria-hidden className="size-5 shrink-0 text-muted-foreground" /> : null}
                            </TarjetaEncabezado>
                            {s.href ? null : (
                                <TarjetaContenido>
                                    <InsigniaEstado tono="neutral" icono={Hourglass} texto="Próximamente" />
                                </TarjetaContenido>
                            )}
                        </Tarjeta>
                    );
                    return (
                        <li key={s.titulo}>
                            {s.href ? (
                                <Link href={s.href} className="block h-full rounded-[var(--hx-radius-card)]">
                                    {cuerpo}
                                </Link>
                            ) : (
                                cuerpo
                            )}
                        </li>
                    );
                })}
            </ul>
        </>
    );
}
