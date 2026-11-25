"use client";

import { accionesDe, accionesRapidas, estadoIslaPasos, pestanaDe, urlDeVista, type PasoOrden, type PrimarioIsla, type Vista } from "@iris/sync/campo";
import { Boton, cn } from "@iris/ui";
import { ArrowLeft, ArrowRight, CalendarClock, CalendarDays, Check, ChevronRight, CircleCheck, CircleUserRound, Hand, House, Inbox, PackageCheck, PenLine, Plus, Save, type LucideIcon } from "lucide-react";
import type { AccionPie, AccionPieRegistrada } from "@iris/sync/campo";
import { useEffect, useState, type MouseEvent } from "react";
import { useAccionPieActual } from "./accion-pie";
import { AccionFijaOrden } from "./acciones-orden";
import { useGuardarPaso, useRecienGuardado, useSinGuardar } from "./borradores";
import { useCampo, useOrden } from "./contexto";
import { HojaCampo } from "./hoja";
import { useLibres } from "./libres-datos";

type PestanaIsla = { id: "hoy" | "recibir" | "agenda" | "yo"; texto: string; vista: Vista; icono: LucideIcon };

/** Botones de la isla (rediseño aprobado por el PM): Hoy · Por recibir · «+» · Agenda · Yo. */
const PESTANAS_ISLA: readonly PestanaIsla[] = [
    { id: "hoy", texto: "Hoy", vista: { tipo: "lista" }, icono: House },
    { id: "recibir", texto: "Por recibir", vista: { tipo: "recibir" }, icono: Inbox },
    { id: "agenda", texto: "Agenda", vista: { tipo: "agenda" }, icono: CalendarDays },
    { id: "yo", texto: "Yo", vista: { tipo: "yo" }, icono: CircleUserRound },
];

/** Qué botón de la isla va marcado: «Por recibir» y sus traslados; el resto, como la app nativa. El chat va arriba. */
function pestanaActiva(vista: Vista): PestanaIsla["id"] | null {
    if (vista.tipo === "recibir" || vista.tipo === "traslado") return "recibir";
    const p = pestanaDe(vista);
    return p === "chat" ? null : p;
}

/** Zonas táctiles de la isla: 54 px en los pasos (48 px encogida). */
const BOTON_ISLA = "relative grid size-[3.375rem] place-content-center rounded-full transition-[width,height] focus-visible:outline-2 focus-visible:-outline-offset-2";
const BOTON_ENCOGIDO = "size-12";
/** Pestaña de la isla (rediseño): ícono y nombre debajo; la activa en índigo tenue. */
const PESTANA_ISLA =
    "relative flex h-[3.25rem] min-w-[3.5rem] flex-col items-center justify-center gap-0.5 rounded-full px-1.5 transition-[background-color,color,height] duration-200 ease-out motion-reduce:transition-none focus-visible:outline-2 focus-visible:-outline-offset-2";
const PESTANA_ENCOGIDA = "h-12";

/**
 * Isla de menú flotante (rediseño, captura «campo-hoy»): píldora centrada abajo con Hoy, Por recibir,
 * «+» al centro (círculo índigo de 52 px), Agenda y Yo; cada una con su ícono y su nombre, la activa en índigo
 * tenue. Al bajar se encoge y al subir vuelve. El chat del equipo está en el encabezado (con su punto
 * de no leídos) y «Por enviar» en la hoja de estado y en «Yo».
 */
export function IslaMenu({ encogida }: { encogida: boolean }) {
    const { vista, navegar, sesion } = useCampo();
    const [acciones, setAcciones] = useState(false);
    const actual = pestanaActiva(vista);
    const boton = (p: PestanaIsla) => {
        const activa = actual === p.id;
        const ir = (e: MouseEvent<HTMLAnchorElement>) => {
            e.preventDefault();
            navegar(p.vista);
        };
        const Icono = p.icono;
        return (
            <li key={p.id} className="flex-1">
                <a
                    href={urlDeVista(p.vista)}
                    onClick={ir}
                    aria-current={activa ? "page" : undefined}
                    aria-label={p.id === "yo" ? `Yo · ${sesion.nombre}` : p.texto}
                    title={p.texto}
                    data-activa={activa ? "true" : undefined}
                    className={cn(
                        PESTANA_ISLA,
                        encogida && PESTANA_ENCOGIDA,
                        activa ? "bg-[var(--campo-tinta)] text-[var(--campo-sobre-tinta)]" : "text-[var(--iris-texto-apagado)] hover:text-foreground",
                    )}
                >
                    <Icono aria-hidden className="size-5 shrink-0" strokeWidth={activa ? 2.3 : 2} />
                    <span aria-hidden data-slot="nombre-pestana" className="max-w-full truncate text-[0.65625rem] leading-tight font-semibold whitespace-nowrap">
                        {p.texto}
                    </span>
                </a>
            </li>
        );
    };
    const antes = PESTANAS_ISLA.slice(0, 2);
    const despues = PESTANAS_ISLA.slice(2);
    return (
        <>
            <nav
                aria-label="Secciones de la app"
                data-slot="isla-menu"
                data-encogida={encogida ? "true" : undefined}
                className={cn(
                    "campo-suave fixed bottom-[calc(1rem+env(safe-area-inset-bottom))] left-1/2 z-30 w-[min(22.375rem,calc(100%-2rem))] -translate-x-1/2 rounded-full border border-border bg-[var(--campo-isla)] px-2 py-1.5 shadow-[var(--campo-sombra-isla)]",
                    encogida && "py-1",
                )}
            >
                <ul className="flex items-center gap-0.5">
                    {antes.map(boton)}
                    <li className="flex-none px-0.5">
                        {/* «+»: círculo índigo con la acción principal de la app. */}
                        <Boton
                            aria-label="Acciones rápidas"
                            title="Acciones rápidas"
                            aria-haspopup="dialog"
                            data-slot="isla-mas"
                            className={cn("size-[3.25rem] rounded-full p-0 shadow-none md:size-[3.25rem] [&_svg]:size-6", encogida && "size-12 md:size-12")}
                            onClick={() => setAcciones(true)}
                        >
                            <Plus aria-hidden strokeWidth={2.2} />
                        </Boton>
                    </li>
                    {despues.map(boton)}
                </ul>
            </nav>
            {/* Se monta al abrir: cada vez relee el contador de órdenes libres. */}
            {acciones ? <HojaAcciones alCambiar={setAcciones} /> : null}
        </>
    );
}

const ICONO_PRIMARIO: Record<PrimarioIsla["icono"], LucideIcon> = { siguiente: ArrowRight, firma: PenLine, terminar: CircleCheck, guardar: Save };

/**
 * Isla de los pasos de la orden: la misma píldora que la isla de menú, con «Anterior» redondo y un
 * primario que avanza (`estadoIslaPasos` de `@iris/sync/campo`). Sin cambios por guardar no hay
 * «Guardar»: el primario es «Siguiente» («Ir a firma» antes de la firma). Con algo escrito sin
 * guardar es «Guardar y seguir» con el contador: guarda y avanza, o se queda y dice qué campo falló.
 * Al guardar, «Guardado» con un check un momento en la isla (sin bloquear). Sin «Guardar» aparte: a
 * 360 px no cabe sin apretar los botones, y cada campo ya se guarda al salir de él. Con la orden en
 * pausa (solo lectura) nunca guarda: solo navega.
 */
export function IslaPasos({ id, paso, encogida }: { id: number; paso: PasoOrden; encogida: boolean }) {
    const { navegar } = useCampo();
    const orden = useOrden(id);
    const { guardar, guardando } = useGuardarPaso();
    const guardado = useRecienGuardado();
    const sinGuardar = useSinGuardar();
    if (!orden) {
        return null;
    }
    const enPausa = accionesDe(orden).enPausa;
    const isla = estadoIslaPasos({ paso, pendientes: sinGuardar, enPausa, recienGuardado: guardado !== null });
    const vistaDe = (destino: PasoOrden | null): Vista => (destino ? { tipo: "orden", id, paso: destino } : { tipo: "orden", id });
    // Con la orden en pausa no se guarda nada: solo se navega.
    const ir = async (destino: PasoOrden | null) => {
        if (enPausa || (await guardar())) {
            navegar(vistaDe(destino));
        }
    };
    const { primario } = isla;
    const IconoPrimario = ICONO_PRIMARIO[primario.icono];
    const redondo = cn(BOTON_ISLA, encogida && BOTON_ENCOGIDO, "bg-[var(--campo-panel-2)] p-0 text-foreground shadow-none [&_svg]:size-6", encogida ? "md:size-12" : "md:size-[3.375rem]");
    return (
        <nav
            aria-label="Pasos"
            data-slot="barra-pasos"
            data-encogida={encogida ? "true" : undefined}
            className={cn(
                "campo-suave fixed bottom-[calc(1.75rem+env(safe-area-inset-bottom))] left-1/2 z-30 flex -translate-x-1/2 items-center gap-1.5 rounded-full border border-border bg-[var(--campo-isla)] p-1.5 shadow-[var(--campo-sombra-isla)]",
                encogida && "bottom-[calc(1rem+env(safe-area-inset-bottom))] gap-1 p-1",
            )}
        >
            <Boton variante="secundario" aria-label={isla.anterior.texto} title={isla.anterior.texto} className={redondo} onClick={() => void ir(isla.anterior.destino)}>
                <ArrowLeft aria-hidden />
            </Boton>
            {/* Región viva siempre montada: el lector anuncia «Guardado» al aparecer. */}
            <span role="status" data-slot="guardado-paso" className={cn("flex items-center gap-1 text-[0.8125rem] font-semibold text-success", isla.guardado && guardado ? "px-1.5" : "sr-only")}>
                {isla.guardado && guardado ? (
                    <>
                        <Check aria-hidden className="size-4" strokeWidth={2.6} />
                        <span aria-hidden>Guardado</span>
                        <span className="sr-only">{guardado}</span>
                    </>
                ) : null}
            </span>
            <Boton
                tamano="lg"
                data-slot={primario.accion === "guardar_y_seguir" ? "guardar-y-seguir" : "paso-siguiente"}
                disabled={guardando}
                aria-label={primario.etiqueta}
                title={primario.etiqueta}
                className={cn("min-w-[9.5rem] rounded-full px-6 text-base shadow-none", encogida ? "h-12" : "h-[3.375rem]")}
                onClick={() => void ir(primario.destino)}
            >
                <IconoPrimario aria-hidden />
                {primario.texto}
                {primario.cuenta !== null ? (
                    <span aria-hidden data-slot="cuenta-sin-guardar" className="grid min-w-5 place-content-center rounded-full bg-primary-foreground px-1.5 text-xs leading-5 font-bold text-primary tabular-nums">
                        {primario.cuenta}
                    </span>
                ) : null}
            </Boton>
        </nav>
    );
}

/**
 * La isla que toca a la vista abierta: la de la acción al pie si una pantalla con firma la registró
 * (sobre la isla no flota nada); si no, la de pasos en los pasos de la orden (no en el paso final) y
 * la de menú en lo demás (no en la conversación, que ocupa la pantalla).
 */
export function IslasCampo({ encogida }: { encogida: boolean }) {
    const { vista } = useCampo();
    const accion = useAccionPieActual();
    if (accion) {
        return <IslaAccion registrada={accion} encogida={encogida} />;
    }
    if (vista.tipo === "orden") {
        if (vista.paso === undefined) return <AccionFijaOrden id={vista.id} />;
        return vista.paso !== "cierre" ? <IslaPasos id={vista.id} paso={vista.paso} encogida={encogida} /> : null;
    }
    return vista.tipo !== "conversacion" ? <IslaMenu encogida={encogida} /> : null;
}

const ICONO_ACCION: Record<AccionPie["icono"], LucideIcon> = { cerrar: CircleCheck, recibir: PackageCheck };

/**
 * Isla de la acción al pie («Firmar y cerrar orden», «Confirmar recepción»; reporte del PM, nov
 * 2026: el botón quedaba al final y había que bajar). La misma píldora sólida que la isla de pasos,
 * fija abajo sobre el área segura: «volver» redondo y el primario. El contenido deja su espacio
 * debajo (el `pb` del marco), así que nunca tapa el recuadro de la firma.
 */
export function IslaAccion({ registrada, encogida }: { registrada: AccionPieRegistrada; encogida: boolean }) {
    const { navegar } = useCampo();
    const { accion, presionar } = registrada;
    const Icono = ICONO_ACCION[accion.icono];
    const redondo = cn(BOTON_ISLA, encogida && BOTON_ENCOGIDO, "bg-[var(--campo-panel-2)] p-0 text-foreground shadow-none [&_svg]:size-6", encogida ? "md:size-12" : "md:size-[3.375rem]");
    return (
        <div
            role="group"
            aria-label={accion.texto}
            data-slot="isla-accion"
            data-encogida={encogida ? "true" : undefined}
            className={cn(
                "campo-suave fixed bottom-[calc(1.75rem+env(safe-area-inset-bottom))] left-1/2 z-30 flex w-[calc(100%-2.25rem)] max-w-md -translate-x-1/2 items-center gap-1.5 rounded-full border border-border bg-[var(--campo-isla)] p-1.5 shadow-[var(--campo-sombra-isla)]",
                encogida && "bottom-[calc(1rem+env(safe-area-inset-bottom))] gap-1 p-1",
            )}
        >
            <Boton variante="secundario" aria-label={accion.volver.texto} title={accion.volver.texto} className={redondo} onClick={() => navegar(accion.volver.destino)}>
                <ArrowLeft aria-hidden />
            </Boton>
            <Boton
                tamano="lg"
                data-slot={accion.prueba}
                disabled={accion.ocupado}
                aria-busy={accion.ocupado || undefined}
                className={cn("min-w-0 flex-1 rounded-full px-5 text-base shadow-none", encogida ? "h-12" : "h-[3.375rem]")}
                onClick={presionar}
            >
                <Icono aria-hidden />
                <span className="truncate">{accion.texto}</span>
            </Boton>
        </div>
    );
}

/**
 * Hoja del «+» (diseño aprobado por el PM, nov 2026): «Agendar tarea propia» (también sin señal) y
 * «Tomar una orden libre» con su contador (requiere señal; oculta si la empresa no lo permite o sin
 * permiso). «Agregar formulario» vive en el menú «⋯» de la orden.
 */
function HojaAcciones({ alCambiar }: { alCambiar: (abierta: boolean) => void }) {
    const { estado, sesion, navegar } = useCampo();
    const { libres, cargarLibres } = useLibres();
    const r = accionesRapidas(sesion.permisos, estado.tomarLibres);
    const enLinea = estado.conexion !== "sin_conexion";
    useEffect(() => {
        if (r.libres && enLinea) {
            void cargarLibres();
        }
    }, [r.libres, enLinea, cargarLibres]);
    const ir = (v: Vista) => {
        alCambiar(false);
        navegar(v);
    };
    const total = libres.estado === "lista" ? (libres.datos?.total ?? null) : null;
    return (
        <HojaCampo abierto alCambiar={alCambiar} titulo="¿Qué quieres hacer?" className="rounded-t-[2rem] sm:rounded-[2rem]">
            <ul className="grid divide-y divide-border border-t border-border" data-slot="hoja-acciones">
                {r.tarea ? (
                    <li>
                        <FilaAccion
                            slot="accion-tarea"
                            icono={CalendarClock}
                            titulo="Agendar tarea propia"
                            detalle="Algo tuyo: recoger material, revisar una caja…"
                            alPresionar={() => ir({ tipo: "nueva_tarea" })}
                        />
                    </li>
                ) : null}
                {r.libres ? (
                    <li>
                        <FilaAccion
                            slot="accion-libres"
                            icono={Hand}
                            titulo="Tomar una orden libre"
                            detalle={enLinea ? "Órdenes de tu zona sin técnico" : "Necesita señal"}
                            cuenta={total}
                            alPresionar={() => ir({ tipo: "libres" })}
                        />
                    </li>
                ) : null}
            </ul>
        </HojaCampo>
    );
}

function FilaAccion({ slot, icono: Icono, titulo, detalle, cuenta = null, alPresionar }: { slot: string; icono: LucideIcon; titulo: string; detalle: string; cuenta?: number | null; alPresionar: () => void }) {
    return (
        <Boton
            variante="fantasma"
            data-slot={slot}
            className="flex h-auto w-full items-center justify-start gap-4 rounded-2xl px-2 py-4 text-left whitespace-normal"
            aria-label={cuenta !== null ? `${titulo} (${cuenta})` : titulo}
            onClick={alPresionar}
        >
            <span aria-hidden className="grid size-11 shrink-0 place-content-center rounded-2xl bg-[color-mix(in_srgb,var(--hx-text)_8%,transparent)] text-foreground">
                <Icono className="size-5" />
            </span>
            <span className="grid flex-1 gap-0.5">
                <span className="text-[1.0625rem] font-semibold text-foreground">{titulo}</span>
                <span className="text-sm font-normal text-muted-foreground">{detalle}</span>
            </span>
            {cuenta !== null && cuenta > 0 ? (
                <span aria-hidden className="grid size-8 place-content-center rounded-full bg-[var(--campo-tinta)] text-sm font-bold text-[var(--campo-sobre-tinta)] tabular-nums">
                    {cuenta > 99 ? "99+" : cuenta}
                </span>
            ) : null}
            <ChevronRight aria-hidden className="size-5 shrink-0 text-muted-foreground" />
        </Boton>
    );
}
