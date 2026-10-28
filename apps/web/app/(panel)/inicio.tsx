"use client";

import { Tarjeta } from "@iris/ui";
import { ChevronRight } from "lucide-react";
import Link from "next/link";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { menuVisible } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";

export function Inicio() {
    const { yo, permisos } = useSesion();
    const modulos = menuVisible(permisos).filter((e) => e.href !== "/");
    const nombre = yo.usuario.nombre.split(" ")[0] ?? yo.usuario.nombre;
    return (
        <>
            <EncabezadoPagina titulo={`Hola, ${nombre}`} descripcion="Operación de campo y atención del ISP." />
            <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                {modulos.map((m) => {
                    const Icono = m.icono;
                    return (
                        <li key={m.href}>
                            <Tarjeta className="h-full transition-shadow duration-150 hover:shadow-[0_0_0_1px_var(--hx-primary)]">
                                <Link href={m.href} className="flex h-full items-center gap-3 rounded-[inherit] p-4">
                                    <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                                        <Icono aria-hidden className="size-5" />
                                    </span>
                                    <span className="grid min-w-0 flex-1">
                                        <span className="font-medium">{m.etiqueta}</span>
                                        <span className="text-sm text-muted-foreground">
                                            {m.proximamente ? "Próximamente" : "Abrir"}
                                        </span>
                                    </span>
                                    <ChevronRight aria-hidden className="size-4 text-dimmed" />
                                </Link>
                            </Tarjeta>
                        </li>
                    );
                })}
            </ul>
        </>
    );
}
