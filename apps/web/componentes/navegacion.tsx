"use client";

import { cn, Globo } from "@iris/ui";
import Link from "next/link";
import { usePathname, useSearchParams } from "next/navigation";
import { useEffect, useId, useState, type ReactNode } from "react";
import { entradaActiva, type EntradaMenu, type GrupoVisible } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";
import { EVENTO_VISTAS, leerVistas, type Vista } from "@/lib/tickets/filtros";
import { textoContador } from "@/lib/tickets/resumen";

type Props = {
    grupos: readonly GrupoVisible[];
    /** Solo íconos (barra lateral contraída): la etiqueta queda para lectores y en un globo. */
    contraida?: boolean;
    alNavegar?: () => void;
    /** Número junto a una entrada (por `href`): solo donde hay datos leídos. */
    contadores?: Readonly<Record<string, number | undefined>>;
};

/** Clases de una entrada de la barra lateral: 34 px, texto de 13 px y fondo neutro en la actual. */
export function clasesEntrada(actual: boolean, contraida = false) {
    return cn(
        "flex h-[2.125rem] items-center gap-2.5 rounded-lg px-2.5 text-[0.8125rem] font-medium transition-colors duration-150",
        contraida && "justify-center px-0",
        actual ? "bg-elevated font-semibold text-foreground" : "text-muted-foreground hover:bg-elevated/70 hover:text-foreground",
    );
}

/**
 * Menú principal por grupos (la operación arriba, sin título visible; Negocio; Administración):
 * solo las entradas que el usuario puede ver (filtradas por `GET /yo`), con `aria-current` en la
 * actual y su contador donde hay datos (Tickets).
 */
export function Navegacion({ grupos, contraida = false, alNavegar, contadores }: Props) {
    const ruta = usePathname();
    const activa = entradaActiva(ruta);
    return (
        <nav aria-label="Principal" data-contraida={contraida || undefined}>
            <div className="grid gap-4">
                {grupos.map((g) => (
                    <Grupo
                        key={g.id}
                        etiqueta={g.etiqueta}
                        oculto={contraida || g.tituloOculto === true || (g.entradas.length === 1 && g.entradas[0]?.etiqueta === g.etiqueta)}
                    >
                        {g.entradas.map((e) => (
                            <li key={e.href}>
                                <Enlace
                                    entrada={e}
                                    actual={activa?.href === e.href}
                                    contraida={contraida}
                                    contador={contadores?.[e.href]}
                                    {...(alNavegar ? { alNavegar } : {})}
                                />
                            </li>
                        ))}
                    </Grupo>
                ))}
            </div>
        </nav>
    );
}

function Grupo({ etiqueta, oculto, children }: { etiqueta: string; oculto: boolean; children: ReactNode }) {
    const id = useId();
    return (
        <div className="grid gap-1">
            <p id={id} className={cn("px-2.5 pb-0.5 text-[0.6875rem] font-semibold tracking-[0.06em] text-muted-foreground uppercase", oculto && "sr-only")}>
                {etiqueta}
            </p>
            <ul aria-labelledby={id} className="grid gap-px">
                {children}
            </ul>
        </div>
    );
}

function Contador({ n }: { n: number }) {
    return (
        <span
            className="inline-flex h-[1.125rem] min-w-5 items-center justify-center rounded-full bg-elevated px-1.5 text-[0.6875rem] font-semibold text-muted-foreground tabular-nums"
            data-slot="contador"
        >
            {textoContador(n)}
        </span>
    );
}

function Enlace({
    entrada: e,
    actual,
    contraida,
    alNavegar,
    contador,
}: {
    entrada: EntradaMenu;
    actual: boolean;
    contraida: boolean;
    alNavegar?: () => void;
    contador: number | undefined;
}) {
    const Icono = e.icono;
    const nombre = contador !== undefined && contraida ? `${e.etiqueta}, ${contador}` : undefined;
    const enlace = (
        <Link href={e.href} onClick={alNavegar} aria-current={actual ? "page" : undefined} aria-label={nombre} className={clasesEntrada(actual, contraida)}>
            <Icono aria-hidden className="size-[1.0625rem] shrink-0" />
            <span className={cn("flex-1 truncate", contraida && "sr-only")}>{e.etiqueta}</span>
            {e.proximamente && !contraida ? <span className="text-[0.6875rem] font-normal text-dimmed">Pronto</span> : null}
            {contador !== undefined && !contraida ? <Contador n={contador} /> : null}
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

/**
 * «Mis vistas»: las vistas de la bandeja que la persona guardó en este navegador. Cada una abre
 * la bandeja con esos filtros (`/tickets?vista=`). Sin vistas guardadas (o sin `tickets.ver`) no
 * aparece. Se entera al guardar o borrar una (mismo navegador).
 */
export function MisVistas({ alNavegar }: { alNavegar?: () => void }) {
    const { yo, puede } = useSesion();
    const ruta = usePathname();
    const params = useSearchParams();
    const id = yo.usuario.id;
    const [vistas, setVistas] = useState<Vista[]>([]);
    useEffect(() => {
        const leer = () => setVistas(leerVistas(id));
        leer();
        window.addEventListener(EVENTO_VISTAS, leer);
        window.addEventListener("storage", leer);
        return () => {
            window.removeEventListener(EVENTO_VISTAS, leer);
            window.removeEventListener("storage", leer);
        };
    }, [id]);
    if (!puede(["tickets.ver"]) || vistas.length === 0) return null;
    const actual = ruta === "/tickets" ? params.get("vista") : null;
    return (
        <nav aria-label="Mis vistas">
            <Grupo etiqueta="Mis vistas" oculto={false}>
                {vistas.map((v) => (
                    <li key={v.id}>
                        <Link
                            href={`/tickets?vista=${encodeURIComponent(v.id)}`}
                            onClick={alNavegar}
                            aria-current={actual === v.id ? "page" : undefined}
                            className={clasesEntrada(actual === v.id)}
                        >
                            <span aria-hidden className="mx-[0.3125rem] size-[0.4375rem] shrink-0 rounded-full bg-primary" />
                            <span className="flex-1 truncate">{v.nombre}</span>
                        </Link>
                    </li>
                ))}
            </Grupo>
        </nav>
    );
}
