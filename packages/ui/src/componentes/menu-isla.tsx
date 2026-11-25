"use client";

import { Disclosure, Popover, Tooltip } from "@heroui/react";
import { ChevronDown, ChevronLeft, ChevronRight, type LucideIcon } from "lucide-react";
import { useId, type ReactNode } from "react";
import { Link } from "react-aria-components";
import { cn } from "../utilidades";
import { Boton } from "./boton";

export type TonoContadorIsla = "marca" | "atencion" | "peligro";

export type EntradaIsla = {
    href: string;
    etiqueta: string;
    /** Nombre corto bajo el ícono en la isla compacta y en la del teléfono («Órdenes»). */
    etiquetaCorta?: string;
    icono: LucideIcon;
    /** Número junto a la entrada (solo con datos leídos); en la compacta es un punto. */
    contador?: number | undefined;
    tonoContador?: TonoContadorIsla;
    /** Es la pantalla abierta (`aria-current="page"`). */
    actual?: boolean;
    /** Aún sin pantalla: «Pronto». */
    proximamente?: boolean;
};

export type GrupoIsla = {
    id: string;
    etiqueta: string;
    /** La operación diaria: sin título visible (solo para lectores). */
    tituloOculto?: boolean;
    /** Va al pie de la isla (Administración). */
    alPie?: boolean;
    entradas: readonly EntradaIsla[];
};

export type PropsMenuIsla = {
    grupos: readonly GrupoIsla[];
    /** Compacta (80 px: ícono y nombre corto) o expandida (248 px). */
    compacta: boolean;
    alAlternar: () => void;
    /** Logo (enlace a Inicio): `LogoIris` en la compacta, `MarcaIris` en la expandida. */
    marca: ReactNode;
    /** Solo en la expandida, bajo el menú: la empresa, «Mis vistas». */
    extra?: ReactNode;
    id?: string;
    className?: string;
};

const FONDO_CONTADOR: Record<TonoContadorIsla, string> = {
    marca: "bg-primario-suave text-primario-texto",
    atencion: "bg-atencion-fondo text-warning",
    peligro: "bg-peligro-fondo text-danger",
};

const PUNTO_CONTADOR: Record<TonoContadorIsla, string> = {
    marca: "bg-primary",
    atencion: "bg-[var(--iris-atencion-punto)]",
    peligro: "bg-[var(--iris-peligro-punto)]",
};

/** «99+» desde 100. */
export function textoContadorIsla(n: number): string {
    return n > 99 ? "99+" : String(n);
}

function Contador({ n, tono = "marca" }: { n: number; tono?: TonoContadorIsla | undefined }) {
    return (
        <span data-slot="contador" className={cn("ml-auto inline-flex h-[1.125rem] min-w-5 items-center justify-center rounded-full px-1.5 text-[0.6875rem] font-bold tabular-nums", FONDO_CONTADOR[tono])}>
            {textoContadorIsla(n)}
        </span>
    );
}

const FOCO = "outline-none data-focus-visible:outline-2 data-focus-visible:outline-offset-2 data-focus-visible:outline-primary";

/** Entrada de la isla expandida: ícono, nombre y contador; la actual en índigo tenue. */
function EntradaExpandida({ entrada: e }: { entrada: EntradaIsla }) {
    const Icono = e.icono;
    return (
        <Link
            href={e.href}
            aria-current={e.actual ? "page" : undefined}
            data-slot="entrada-isla"
            className={cn(
                "flex h-9 min-w-0 items-center gap-[0.6875rem] rounded-[11px] px-[0.6875rem] text-sm font-medium transition-colors",
                e.actual ? "bg-primario-suave font-semibold text-primario-texto" : "text-muted-foreground hover:bg-relleno hover:text-foreground",
                FOCO,
            )}
        >
            <Icono aria-hidden className="size-[1.125rem] shrink-0" />
            <span className="min-w-0 flex-1 truncate">{e.etiqueta}</span>
            {e.proximamente ? <span className="text-[0.6875rem] font-normal text-[var(--iris-texto-apagado)]">Pronto</span> : null}
            {e.contador !== undefined ? <Contador n={e.contador} tono={e.tonoContador} /> : null}
        </Link>
    );
}

/** Entrada de la isla compacta (riel): ícono y nombre corto debajo; el contador es un punto. */
function EntradaRiel({ entrada: e }: { entrada: EntradaIsla }) {
    const Icono = e.icono;
    const corto = e.etiquetaCorta ?? e.etiqueta;
    const nombre = e.contador !== undefined ? `${e.etiqueta}, ${e.contador}` : e.etiqueta;
    return (
        <Link
            href={e.href}
            aria-current={e.actual ? "page" : undefined}
            aria-label={nombre}
            data-slot="entrada-isla"
            className={cn(
                "relative flex w-[3.75rem] flex-col items-center gap-[3px] rounded-xl px-0.5 py-[7px] text-center text-[0.65625rem] leading-tight font-semibold transition-colors",
                e.actual ? "bg-primario-suave text-primario-texto" : "text-[var(--iris-texto-apagado)] hover:bg-relleno hover:text-foreground",
                FOCO,
            )}
        >
            <Icono aria-hidden className="size-5 shrink-0" />
            <span aria-hidden className="max-w-full truncate">
                {corto}
            </span>
            {e.contador !== undefined && e.contador > 0 ? (
                <span aria-hidden data-slot="punto-contador" className={cn("absolute top-[5px] right-3 size-2 rounded-full ring-2 ring-[var(--iris-tarjeta)]", PUNTO_CONTADOR[e.tonoContador ?? "marca"])} />
            ) : null}
        </Link>
    );
}

/** Un grupo con título en la isla expandida: se abre y se cierra (Disclosure de HeroUI). */
function GrupoExpandido({ grupo }: { grupo: GrupoIsla }) {
    const id = useId();
    const lista = (
        <ul aria-labelledby={id} className="grid gap-0.5">
            {grupo.entradas.map((e) => (
                <li key={e.href}>
                    <EntradaExpandida entrada={e} />
                </li>
            ))}
        </ul>
    );
    if (grupo.tituloOculto) {
        return (
            <div className="grid gap-0.5">
                <p id={id} className="sr-only">
                    {grupo.etiqueta}
                </p>
                {lista}
            </div>
        );
    }
    return (
        <Disclosure defaultExpanded data-slot="grupo-isla" className="grid">
            <Disclosure.Heading level={2} className="m-0">
                <Disclosure.Trigger className="mx-[0.6875rem] mt-2.5 mb-1 flex w-[calc(100%-1.375rem)] items-center text-left text-[0.6875rem] font-bold tracking-[0.06em] text-[var(--iris-texto-apagado)] uppercase hover:text-foreground">
                    <span id={id} className="flex-1">
                        {grupo.etiqueta}
                    </span>
                    <Disclosure.Indicator className="ms-0 [&_svg]:size-3.5">
                        <ChevronDown aria-hidden />
                    </Disclosure.Indicator>
                </Disclosure.Trigger>
            </Disclosure.Heading>
            <Disclosure.Content>{lista}</Disclosure.Content>
        </Disclosure>
    );
}

/**
 * Un grupo con título en la isla compacta: un solo botón (ícono de su primera entrada y el nombre
 * del grupo) que abre la lista al lado, en un `Popover` de HeroUI. Un grupo de una sola entrada
 * va como una entrada más.
 */
function GrupoRiel({ grupo }: { grupo: GrupoIsla }) {
    const id = useId();
    if (grupo.tituloOculto || grupo.entradas.length === 1) {
        return (
            <ul aria-label={grupo.etiqueta} className="grid justify-items-center gap-0.5">
                {grupo.entradas.map((e) => (
                    <li key={e.href}>
                        <EntradaRiel entrada={e} />
                    </li>
                ))}
            </ul>
        );
    }
    const primera = grupo.entradas[0];
    const Icono = primera?.icono;
    const actual = grupo.entradas.some((e) => e.actual);
    const pendientes = grupo.entradas.some((e) => (e.contador ?? 0) > 0);
    return (
        <Popover>
            <Boton
                variante="fantasma"
                aria-label={`${grupo.etiqueta}: abrir el grupo`}
                data-slot="grupo-riel"
                className={cn(
                    "relative flex h-auto w-[3.75rem] flex-col items-center gap-[3px] rounded-xl px-0.5 py-[7px] text-[0.65625rem] leading-tight font-semibold",
                    actual ? "bg-primario-suave text-primario-texto" : "text-[var(--iris-texto-apagado)]",
                    "[&_svg]:size-5",
                )}
            >
                {Icono ? <Icono aria-hidden /> : null}
                <span aria-hidden className="max-w-full truncate">
                    {grupo.etiqueta}
                </span>
                {pendientes ? <span aria-hidden className="absolute top-[5px] right-3 size-2 rounded-full bg-primary ring-2 ring-[var(--iris-tarjeta)]" /> : null}
            </Boton>
            <Popover.Content placement="end top" offset={14} className="w-60 border border-border p-0">
                <Popover.Dialog aria-label={grupo.etiqueta} className="grid gap-1 p-2 outline-none">
                    <p id={id} className="px-[0.6875rem] pt-1 pb-0.5 text-[0.6875rem] font-bold tracking-[0.06em] text-[var(--iris-texto-apagado)] uppercase">
                        {grupo.etiqueta}
                    </p>
                    <ul aria-labelledby={id} className="grid gap-0.5">
                        {grupo.entradas.map((e) => (
                            <li key={e.href}>
                                <EntradaExpandida entrada={e} />
                            </li>
                        ))}
                    </ul>
                </Popover.Dialog>
            </Popover.Content>
        </Popover>
    );
}

/**
 * Menú del panel como isla flotante (rediseño, nov. 2026): a 16 px de los bordes, radio de 20 px,
 * sólida, con sombra. **Compacta** (80 px: ícono y nombre corto; Negocio y Administración abren su
 * grupo al lado) o **expandida** (248 px: grupos que se abren y lo `extra`, p. ej. «Mis vistas»).
 * El botón `‹ ›` de 28 px va sobre el borde derecho a la altura del logo, en el mismo lugar en los
 * dos modos. Solo en escritorio (desde 1024 px); en el teléfono va `IslaInferior`.
 */
export function MenuIsla({ grupos, compacta, alAlternar, marca, extra, id, className }: PropsMenuIsla) {
    const generado = useId();
    const idIsla = id ?? generado;
    const arriba = grupos.filter((g) => !g.alPie);
    const pie = grupos.filter((g) => g.alPie);
    const texto = compacta ? "Expandir menú" : "Contraer menú";
    const Grupo = compacta ? GrupoRiel : GrupoExpandido;
    return (
        <aside
            id={idIsla}
            aria-label="Barra lateral"
            data-slot="menu-isla"
            data-contraida={compacta ? "true" : undefined}
            className={cn(
                "fixed top-4 bottom-4 left-4 z-30 hidden rounded-[var(--iris-radio-isla)] border border-border bg-[var(--iris-tarjeta)] shadow-[var(--iris-sombra-flotante)] lg:block",
                compacta ? "w-20" : "w-62",
                className,
            )}
        >
            <Tooltip delay={300} closeDelay={150}>
                <Boton
                    variante="contorno"
                    tamano="icono-sm"
                    aria-label={texto}
                    aria-expanded={!compacta}
                    aria-controls={idIsla}
                    data-slot="alternar-menu"
                    onClick={alAlternar}
                    className="absolute top-[1.1875rem] -right-3.5 z-[3] size-7 min-w-7 border border-border bg-[var(--iris-flotante)] p-0 text-primario-texto shadow-[var(--iris-sombra-flotante)] md:size-7 [&_svg]:size-4"
                >
                    {compacta ? <ChevronRight /> : <ChevronLeft />}
                </Boton>
                <Tooltip.Content offset={8} placement="end" data-slot="globo" className="rounded-md border border-border px-2.5 py-1.5 text-xs text-foreground">
                    {texto}
                </Tooltip.Content>
            </Tooltip>
            <div className={cn("flex h-full flex-col overflow-x-hidden overflow-y-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden", compacta ? "items-center gap-1 px-2.5 py-3.5" : "gap-0.5 px-3 py-3.5")}>
                <div className={cn("flex-none", compacta ? "pb-2.5" : "px-1 pb-3")}>{marca}</div>
                <nav aria-label="Principal" data-contraida={compacta ? "true" : undefined} className={cn("flex min-h-0 flex-1 flex-col", compacta ? "items-center gap-1" : "gap-0.5")}>
                    {arriba.map((g) => (
                        <Grupo key={g.id} grupo={g} />
                    ))}
                    {!compacta && extra ? <div className="grid gap-0.5">{extra}</div> : null}
                    {pie.length > 0 ? (
                        <div className={cn("mt-auto grid pt-3", compacta && "justify-items-center")}>
                            {pie.map((g) => (
                                <Grupo key={g.id} grupo={g} />
                            ))}
                        </div>
                    ) : null}
                </nav>
            </div>
        </aside>
    );
}

export type PropsIslaInferior = {
    /** Las 4 entradas principales (Inicio, Tickets, Órdenes, Agenda). */
    entradas: readonly EntradaIsla[];
    /** El botón «Más» (abre la hoja con el resto). */
    mas: ReactNode;
    /** Botón redondo a la derecha con la acción principal de la pantalla (p. ej. «Crear»). */
    accion?: ReactNode;
    className?: string;
};

/**
 * Isla del menú en el teléfono (debajo de 1024 px): píldora flotante abajo a 16 px de los bordes,
 * con las entradas principales y «Más», y a su derecha el botón redondo de la acción principal.
 * Respeta el área segura de abajo.
 */
export function IslaInferior({ entradas, mas, accion, className }: PropsIslaInferior) {
    return (
        <>
            <nav
                aria-label="Accesos rápidos"
                data-slot="barra-inferior"
                className={cn(
                    "fixed bottom-[calc(1rem+env(safe-area-inset-bottom))] left-4 z-30 flex h-[3.875rem] items-center rounded-full border border-border bg-[var(--iris-tarjeta)] px-1.5 shadow-[var(--iris-sombra-flotante)] lg:hidden",
                    accion ? "right-[4.75rem]" : "right-4",
                    className,
                )}
            >
                <ul className="flex w-full items-stretch justify-around">
                    {entradas.map((e) => {
                        const Icono = e.icono;
                        return (
                            <li key={e.href} className="flex min-w-0 flex-1">
                                <Link
                                    href={e.href}
                                    aria-current={e.actual ? "page" : undefined}
                                    className={cn(
                                        "relative flex min-w-0 flex-1 flex-col items-center justify-center gap-0.5 rounded-full py-1.5 text-[0.65625rem] font-semibold",
                                        e.actual ? "bg-primario-suave text-primario-texto" : "text-[var(--iris-texto-apagado)]",
                                        FOCO,
                                    )}
                                >
                                    <Icono aria-hidden className="size-5" />
                                    <span className="max-w-full truncate">{e.etiquetaCorta ?? e.etiqueta}</span>
                                    {e.contador !== undefined && e.contador > 0 ? (
                                        <span aria-hidden className={cn("absolute top-1 right-[calc(50%-1.125rem)] size-2 rounded-full ring-2 ring-[var(--iris-tarjeta)]", PUNTO_CONTADOR[e.tonoContador ?? "marca"])} />
                                    ) : null}
                                </Link>
                            </li>
                        );
                    })}
                    <li className="flex min-w-0 flex-1">{mas}</li>
                </ul>
            </nav>
            {accion ? <div className="fixed right-4 bottom-[calc(1.1875rem+env(safe-area-inset-bottom))] z-30 lg:hidden">{accion}</div> : null}
        </>
    );
}
