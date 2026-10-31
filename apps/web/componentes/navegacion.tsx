"use client";

import { cn, Globo } from "@iris/ui";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useId, type ReactNode } from "react";
import { entradaActiva, type EntradaMenu, type GrupoVisible } from "@/lib/menu";

type Props = {
    grupos: readonly GrupoVisible[];
    /** Solo íconos (barra lateral contraída): la etiqueta queda para lectores y en un globo. */
    contraida?: boolean;
    alNavegar?: () => void;
};

/**
 * Menú principal por grupos (Operación, Equipo, Ventas, Análisis, Administración): solo las
 * entradas que el usuario puede ver (filtradas por `GET /yo`), con `aria-current` en la actual.
 */
export function Navegacion({ grupos, contraida = false, alNavegar }: Props) {
    const ruta = usePathname();
    const activa = entradaActiva(ruta);
    return (
        <nav aria-label="Principal" data-contraida={contraida || undefined}>
            <div className="grid gap-4">
                {grupos.map((g) => (
                    <Grupo key={g.id} grupo={g} contraida={contraida}>
                        {g.entradas.map((e) => (
                            <li key={e.href}>
                                <Enlace entrada={e} actual={activa?.href === e.href} contraida={contraida} {...(alNavegar ? { alNavegar } : {})} />
                            </li>
                        ))}
                    </Grupo>
                ))}
            </div>
        </nav>
    );
}

function Grupo({ grupo, contraida, children }: { grupo: GrupoVisible; contraida: boolean; children: ReactNode }) {
    const id = useId();
    // «Administración» con su única entrada del mismo nombre no repite el título.
    const repetido = grupo.entradas.length === 1 && grupo.entradas[0]?.etiqueta === grupo.etiqueta;
    return (
        <div className="grid gap-1">
            <p id={id} className={cn("px-2.5 text-xs font-medium text-dimmed", (contraida || repetido) && "sr-only")}>
                {grupo.etiqueta}
            </p>
            <ul aria-labelledby={id} className="grid gap-0.5">
                {children}
            </ul>
        </div>
    );
}

function Enlace({ entrada: e, actual, contraida, alNavegar }: { entrada: EntradaMenu; actual: boolean; contraida: boolean; alNavegar?: () => void }) {
    const Icono = e.icono;
    const enlace = (
        <Link
            href={e.href}
            onClick={alNavegar}
            aria-current={actual ? "page" : undefined}
            className={cn(
                "flex h-9 items-center gap-2.5 rounded-md px-2.5 text-sm font-medium transition-colors duration-150",
                contraida && "justify-center px-0",
                actual ? "bg-primary/12 text-primary" : "text-muted-foreground hover:bg-elevated hover:text-foreground",
            )}
        >
            <Icono aria-hidden className="size-4 shrink-0" />
            <span className={cn("flex-1 truncate", contraida && "sr-only")}>{e.etiqueta}</span>
            {e.proximamente && !contraida ? <span className="text-[0.6875rem] font-normal text-dimmed">Pronto</span> : null}
        </Link>
    );
    return contraida ? (
        <Globo texto={e.etiqueta} lado="end" retraso={300}>
            {enlace}
        </Globo>
    ) : (
        enlace
    );
}
