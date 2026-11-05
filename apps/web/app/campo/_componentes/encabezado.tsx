"use client";

import { buscarOrdenes, CHIPS_DIA, chipDe, citasDelDia, esVistaDeDia, fechaEncabezado, iniciales, mananaEnGuatemala, nombreCliente, primerNombre, resumenHoy, urlDeVista, type ChipDia } from "@iris/sync/campo";
import { Boton, Campo, Dialogo, DialogoContenido, cn } from "@iris/ui";
import { ChevronRight, Search } from "lucide-react";
import { forwardRef, useMemo, useState, type MouseEvent } from "react";
import { TIPOS_ORDEN } from "@/lib/campo/textos";
import { useAgenda } from "./agenda-datos";
import { PuntoConexion } from "./barra-conexion";
import { useCampo } from "./contexto";
import { InsigniaOrden } from "./insignias";
import { IndicadorUbicacion } from "./ubicacion";

/** Botón redondo de 48 px del encabezado (buscar, cuenta). */
const REDONDO = "size-12 shrink-0 rounded-full p-0 md:size-12";

/**
 * Encabezado de la app de campo (diseño «combinada»). No queda fijo: se va con el contenido al
 * bajar (la barra compacta vuelve al subir, ver `BarraCompacta`). Lleva la fecha con el punto de
 * conexión, buscar y la cuenta; en Hoy, Mañana y Semana además «Hola, <nombre>» y los chips con su
 * contador. La ubicación en vivo va siempre (se toca para ver el detalle).
 */
export const EncabezadoCampo = forwardRef<HTMLElement, { empresa?: string | undefined }>(function EncabezadoCampo({ empresa }, ref) {
    const { sesion, vista } = useCampo();
    const deDia = esVistaDeDia(vista);
    return (
        <header ref={ref} data-slot="encabezado-campo" className="grid gap-3 px-[1.125rem] pt-[calc(env(safe-area-inset-top)+1rem)] pb-1">
            <div className="flex items-start justify-between gap-3">
                <div className="grid min-w-0 gap-1.5">
                    <p className="flex items-center gap-2 text-[0.8125rem] font-medium text-muted-foreground">
                        <span className="truncate">{fechaEncabezado()}</span>
                        <PuntoConexion />
                    </p>
                    {deDia ? <h1 className="text-[2rem] leading-[1.05] font-bold tracking-[-0.03em]">Hola, {primerNombre(sesion.nombre)}</h1> : null}
                    {/* Quién está (un teléfono puede ser de varios técnicos) y, con varias, en qué empresa. */}
                    <p className="truncate text-xs text-muted-foreground">
                        {sesion.nombre}
                        {empresa ? ` · ${empresa}` : ""}
                    </p>
                </div>
                <BotonesCuenta />
            </div>
            <IndicadorUbicacion className="-mx-2 w-fit max-w-full" />
            {deDia ? <ChipsDia /> : null}
        </header>
    );
});

function BotonesCuenta() {
    const { sesion, navegar, vista } = useCampo();
    const [buscando, setBuscando] = useState(false);
    const enYo = vista.tipo === "yo" || vista.tipo === "bodega";
    return (
        <div className="flex shrink-0 gap-2">
            <Boton variante="secundario" aria-label="Buscar orden" title="Buscar orden" className={cn(REDONDO, "bg-[var(--campo-panel)] [&_svg]:size-5")} onClick={() => setBuscando(true)}>
                <Search aria-hidden />
            </Boton>
            <Boton
                aria-label="Tu cuenta"
                title="Tu cuenta"
                aria-current={enYo ? "page" : undefined}
                className={cn(REDONDO, "text-[0.9375rem] font-semibold shadow-none")}
                onClick={() => navegar({ tipo: "yo" })}
            >
                {iniciales(sesion.nombre) || "Yo"}
            </Boton>
            <BuscarOrden abierto={buscando} alCambiar={setBuscando} />
        </div>
    );
}

/** Chips Hoy · Mañana · Semana con su contador (solo con datos leídos: nunca un total inventado). */
function ChipsDia() {
    const { vista, navegar, estado } = useCampo();
    const { semana } = useAgenda();
    const manana = mananaEnGuatemala();
    const actual = chipDe(vista, manana);
    const cuentas: Partial<Record<ChipDia, number>> = {
        ...(estado.fecha !== null ? { hoy: resumenHoy(estado.ordenes).activas } : {}),
        ...(semana.datos ? { manana: citasDelDia(semana.datos.ordenes, manana).length } : {}),
    };
    return (
        <nav aria-label="Día" className="campo-sin-barra -mx-[1.125rem] flex gap-2 overflow-x-auto px-[1.125rem] pb-1">
            {CHIPS_DIA.map((c) => {
                const activo = actual === c.id;
                const cuenta = cuentas[c.id];
                const ir = (e: MouseEvent<HTMLAnchorElement>) => {
                    e.preventDefault();
                    navegar(c.vista);
                };
                return (
                    <a
                        key={c.id}
                        href={urlDeVista(c.vista)}
                        onClick={ir}
                        aria-current={activo ? "page" : undefined}
                        data-chip={c.id}
                        className={cn(
                            "inline-flex h-11 shrink-0 items-center rounded-full px-[1.125rem] text-sm whitespace-nowrap transition-colors focus-visible:outline-2 focus-visible:outline-offset-2",
                            activo ? "bg-[var(--campo-tinta)] font-semibold text-[var(--campo-sobre-tinta)]" : "border border-border bg-[var(--campo-panel)] font-medium text-foreground",
                        )}
                    >
                        {c.texto}
                        {cuenta !== undefined ? <span className="tabular-nums">{`\u00a0·\u00a0${cuenta}`}</span> : null}
                    </a>
                );
            })}
        </nav>
    );
}

/**
 * Barra compacta: vuelve arriba al subir con el encabezado fuera de vista (al bajar se va). Fecha
 * con el punto de conexión, el saludo corto, buscar y la cuenta.
 */
export function BarraCompacta({ visible }: { visible: boolean }) {
    const { sesion } = useCampo();
    return (
        <div
            data-slot="barra-compacta"
            aria-hidden={!visible}
            inert={!visible}
            className={cn(
                "campo-suave fixed inset-x-0 top-0 z-30 mx-auto w-full max-w-2xl border-b border-border bg-[var(--campo-panel)] px-[1.125rem] pt-[env(safe-area-inset-top)] shadow-[var(--campo-sombra)]",
                visible ? "translate-y-0 opacity-100" : "pointer-events-none -translate-y-full opacity-0",
            )}
        >
            <div className="flex items-center justify-between gap-3 py-2">
                <p className="flex min-w-0 items-center gap-2 font-semibold">
                    <span className="truncate">Hola, {primerNombre(sesion.nombre)}</span>
                </p>
                <BotonesCuenta />
            </div>
        </div>
    );
}

/** Busca entre las órdenes del teléfono (número, cliente o dirección); funciona sin señal. */
function BuscarOrden({ abierto, alCambiar }: { abierto: boolean; alCambiar: (abierto: boolean) => void }) {
    const { estado, navegar } = useCampo();
    const [texto, setTexto] = useState("");
    const encontradas = useMemo(() => buscarOrdenes(estado.ordenes, texto), [estado.ordenes, texto]);
    return (
        <Dialogo
            open={abierto}
            onOpenChange={(a) => {
                alCambiar(a);
                if (!a) {
                    setTexto("");
                }
            }}
        >
            <DialogoContenido titulo="Buscar orden" descripcion="Entre las órdenes de tu teléfono: número, cliente o dirección." className="campo-hoja rounded-t-[1.75rem]">
                <div className="grid gap-3">
                    <Campo etiqueta="Buscar" type="search" autoComplete="off" value={texto} onChange={(e) => setTexto(e.target.value)} />
                    {texto.trim().length === 0 ? null : encontradas.length === 0 ? (
                        <p className="text-sm text-muted-foreground">Ninguna orden del teléfono coincide.</p>
                    ) : (
                        <ul className="grid gap-2" aria-label="Órdenes encontradas">
                            {encontradas.map((o) => (
                                <li key={o.id}>
                                    <Boton
                                        variante="secundario"
                                        tamano="lg"
                                        className="h-auto min-h-14 w-full justify-start rounded-[1.375rem] py-2 text-left whitespace-normal"
                                        onClick={() => {
                                            alCambiar(false);
                                            setTexto("");
                                            navegar({ tipo: "orden", id: o.id });
                                        }}
                                    >
                                        <span className="grid flex-1 gap-0.5">
                                            <span className="flex flex-wrap items-center gap-2">
                                                <span className="font-mono text-sm">{o.numero}</span>
                                                <InsigniaOrden estado={o.estado} />
                                            </span>
                                            <span className="font-semibold">{nombreCliente(o)}</span>
                                            <span className="text-sm font-normal text-muted-foreground">{TIPOS_ORDEN[o.tipo]}</span>
                                        </span>
                                        <ChevronRight aria-hidden className="text-muted-foreground" />
                                    </Boton>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
            </DialogoContenido>
        </Dialogo>
    );
}
