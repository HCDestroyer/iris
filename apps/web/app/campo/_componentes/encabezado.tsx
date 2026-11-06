"use client";

import { buscarOrdenes, CHIPS_DIA, chipDe, ESPERA_BUSQUEDA_MS, citasDelDia, esVistaDeDia, fechaEncabezado, mananaEnGuatemala, nombreCliente, primerNombre, resumenHoy, type ChipDia } from "@iris/sync/campo";
import { Boton, Campo, GrupoSegmentado, cn } from "@iris/ui";
import { ChevronRight, Search } from "lucide-react";
import { forwardRef, useMemo, useState } from "react";
import { TIPOS_ORDEN } from "@/lib/campo/textos";
import { useAgenda } from "./agenda-datos";
import { IndicadoresEstado } from "./estado";
import { useCampo } from "./contexto";
import { HojaCampo } from "./hoja";
import { useDemorado } from "./teclado";
import { InsigniaOrden } from "./insignias";

/** Botón redondo de 48 px del encabezado (buscar, cuenta). */
const REDONDO = "size-12 shrink-0 rounded-full p-0 md:size-12";

/**
 * Encabezado de la app de campo (opción elegida por el PM, nov 2026). No queda fijo: se va con el
 * contenido al bajar (la barra compacta vuelve al subir, ver `BarraCompacta`). Solo la fecha,
 * «Hola, <nombre>» (en Hoy, Mañana y Semana) a la izquierda; a la derecha los indicadores de
 * conexión, ubicación y pendientes (abren la hoja de estado) y buscar. La cuenta está en «Yo» de la
 * isla. Con varias empresas, la
 * empresa se ve en «Yo». En Hoy, Mañana y Semana, la barra segmentada del día con su contador.
 */
export const EncabezadoCampo = forwardRef<HTMLElement, { empresa?: string | undefined; pedirEmpresa: () => void }>(function EncabezadoCampo({ pedirEmpresa }, ref) {
    const { sesion, vista } = useCampo();
    const deDia = esVistaDeDia(vista);
    return (
        <header ref={ref} data-slot="encabezado-campo" className="grid gap-3 px-[1.125rem] pt-[calc(env(safe-area-inset-top)+1rem)] pb-1">
            <div className="flex items-start justify-between gap-3">
                <div className="grid min-w-0 gap-1.5">
                    <p className="truncate text-[0.8125rem] font-medium text-muted-foreground">{fechaEncabezado()}</p>
                    {deDia ? <h1 className="text-[2rem] leading-[1.05] font-bold tracking-[-0.03em]">Hola, {primerNombre(sesion.nombre)}</h1> : null}
                </div>
                <BotonesCuenta pedirEmpresa={pedirEmpresa} />
            </div>
            {deDia ? <ChipsDia /> : null}
        </header>
    );
});

function BotonesCuenta({ pedirEmpresa, compacta = false }: { pedirEmpresa: () => void; compacta?: boolean }) {
    const [buscando, setBuscando] = useState(false);
    return (
        <div className="flex shrink-0 items-center gap-1.5">
            <IndicadoresEstado pedirEmpresa={pedirEmpresa} compacta={compacta} />
            <Boton variante="secundario" aria-label="Buscar orden" title="Buscar orden" className={cn(REDONDO, "bg-[var(--campo-panel)] [&_svg]:size-5")} onClick={() => setBuscando(true)}>
                <Search aria-hidden />
            </Boton>
            <BuscarOrden abierto={buscando} alCambiar={setBuscando} />
        </div>
    );
}

/**
 * Hoy · Mañana · Semana en una sola barra segmentada (`GrupoSegmentado`), con su contador (solo
 * con datos leídos: nunca un total inventado). Otro día de la agenda no marca ninguno.
 */
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
        <GrupoSegmentado
            etiqueta="Día"
            className="campo-dias"
            valor={actual ?? undefined}
            alCambiar={(id) => {
                const c = CHIPS_DIA.find((x) => x.id === id);
                if (c) {
                    navegar(c.vista);
                }
            }}
            opciones={CHIPS_DIA.map((c) => ({ valor: c.id, etiqueta: cuentas[c.id] !== undefined ? `${c.texto} · ${cuentas[c.id]}` : c.texto }))}
        />
    );
}

/**
 * Barra compacta: entra desde arriba al subir con el encabezado fuera de vista; al bajar se va
 * deslizándose entera. El saludo corto, los indicadores de estado y buscar. La franja bajo la barra de estado del
 * teléfono (`FranjaSuperior`) es sólida: el contenido nunca se ve detrás de la hora.
 */
export function BarraCompacta({ visible, pedirEmpresa }: { visible: boolean; pedirEmpresa: () => void }) {
    const { sesion } = useCampo();
    return (
        <div
            data-slot="barra-compacta"
            aria-hidden={!visible}
            inert={!visible}
            data-visible={visible ? "true" : "false"}
            className={cn(
                // Sólida siempre (el panel del tema) y solo se desliza: nunca queda a medio
                // transparentar sobre el contenido. Oculta, sube entera (con su sombra) fuera de vista.
                "campo-desliza fixed inset-x-0 top-0 z-30 mx-auto w-full max-w-2xl border-b border-border bg-[var(--campo-panel)] px-[1.125rem] pt-[env(safe-area-inset-top)] shadow-[var(--campo-sombra)]",
                visible ? "translate-y-0" : "pointer-events-none -translate-y-[calc(100%+2.5rem)]",
            )}
        >
            <div className="flex items-center justify-between gap-3 py-2">
                <p className="flex min-w-0 items-center gap-2 font-semibold">
                    <span className="truncate">Hola, {primerNombre(sesion.nombre)}</span>
                </p>
                <BotonesCuenta pedirEmpresa={pedirEmpresa} compacta />
            </div>
        </div>
    );
}

/** Franja sólida bajo la barra de estado del teléfono (PWA instalada, `viewport-fit=cover`). */
export function FranjaSuperior() {
    return <div aria-hidden data-slot="franja-superior" className="pointer-events-none fixed inset-x-0 top-0 z-40 h-[env(safe-area-inset-top)] bg-[var(--campo-lienzo)]" />;
}

/**
 * Busca entre las órdenes del teléfono (número, cliente o dirección); funciona sin señal. En vivo:
 * filtra al dejar de escribir (`ESPERA_BUSQUEDA_MS`); el campo queda fijo arriba de la hoja y la
 * hoja queda por encima del teclado (el `Modal` de HeroUI mide la ventana visible).
 */
function BuscarOrden({ abierto, alCambiar }: { abierto: boolean; alCambiar: (abierto: boolean) => void }) {
    const { estado, navegar } = useCampo();
    const [texto, setTexto] = useState("");
    const consulta = useDemorado(texto, ESPERA_BUSQUEDA_MS);
    const encontradas = useMemo(() => buscarOrdenes(estado.ordenes, consulta), [estado.ordenes, consulta]);
    const cambiar = (a: boolean) => {
        alCambiar(a);
        if (!a) {
            setTexto("");
        }
    };
    return (
        <HojaCampo abierto={abierto} alCambiar={cambiar} titulo="Buscar orden" descripcion="Entre las órdenes de tu teléfono: número, cliente o dirección." className="campo-hoja-busqueda">
                <div className="grid min-h-0 gap-3" data-slot="buscar-orden">
                    <div className="sticky top-0 z-10 -mx-1 bg-[var(--overlay)] px-1 pb-1">
                        <Campo etiqueta="Buscar" type="search" autoComplete="off" enterKeyHint="search" autoFocus value={texto} onChange={(e) => setTexto(e.target.value)} />
                    </div>
                    {consulta.trim().length === 0 ? null : encontradas.length === 0 ? (
                        <p className="text-sm text-muted-foreground" role="status" data-slot="sin-resultados">
                            Ninguna orden del teléfono coincide con «{consulta.trim()}».
                        </p>
                    ) : (
                        <ul className="grid gap-2" aria-label="Órdenes encontradas" data-slot="resultados-busqueda">
                            <li className="text-sm text-muted-foreground" role="status">
                                {encontradas.length === 1 ? "1 orden" : `${encontradas.length} órdenes`}
                            </li>
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
        </HojaCampo>
    );
}
