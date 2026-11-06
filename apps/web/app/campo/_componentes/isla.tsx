"use client";

import { accionesDe, accionesRapidas, contadoresPestanas, estadoIslaPasos, pestanaDe, pestanasIsla, urlDeVista, type PasoOrden, type Pestana, type PrimarioIsla, type Vista } from "@iris/sync/campo";
import { Boton, cn } from "@iris/ui";
import { ArrowLeft, ArrowRight, CalendarClock, CalendarDays, Check, ChevronRight, CircleCheck, Hand, House, MessageCircle, PenLine, Plus, Save, User, type LucideIcon } from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { useGuardarPaso, useRecienGuardado, useSinGuardar } from "./borradores";
import { useChatOpcional } from "./chat-datos";
import { useCampo, useOrden } from "./contexto";
import { HojaCampo } from "./hoja";
import { useLibres } from "./libres-datos";

const ICONOS: Record<Pestana, LucideIcon> = { hoy: House, chat: MessageCircle, agenda: CalendarDays, yo: User };

/** Zonas táctiles de la isla: 54 px en los pasos, 52 px en el menú (48 px encogida). */
const BOTON_ISLA = "relative grid size-[3.375rem] place-content-center rounded-full transition-[width,height] focus-visible:outline-2 focus-visible:-outline-offset-2";
const BOTON_ENCOGIDO = "size-12";
/**
 * Pestaña de la isla: la activa es una píldora en «tinta» con icono y nombre; las demás, solo el
 * icono en gris. El nombre entra y sale con una transición (sin ella con movimiento reducido).
 */
const PESTANA_ISLA =
    "relative flex h-[3.25rem] min-w-[3.25rem] items-center justify-center rounded-full px-3.5 transition-[background-color,color,padding,height,min-width] duration-300 ease-out motion-reduce:transition-none focus-visible:outline-2 focus-visible:-outline-offset-2";
const PESTANA_ENCOGIDA = "h-12 min-w-12 px-3";

/**
 * Isla de menú flotante (estilo elegido por el PM, nov 2026): píldora centrada abajo con Hoy, Chat
 * (punto teal de no leídos), «+» al centro (solo el icono, sin relleno), Agenda y Yo. La activa es
 * una píldora en «tinta» con su nombre; las demás, solo el icono. Al bajar se encoge
 * y al subir vuelve. «Por recibir» es el mosaico de Hoy; «Por enviar» se abre desde la hoja de
 * estado (los indicadores de encima) y desde «Yo». Sin chat (sin `chat.ver`), sin «Chat».
 */
export function IslaMenu({ encogida }: { encogida: boolean }) {
    const { vista, navegar, sesion } = useCampo();
    const chat = useChatOpcional();
    const [acciones, setAcciones] = useState(false);
    const actual = pestanaDe(vista);
    const cuentas = contadoresPestanas(chat?.estado.noLeidos ?? 0);
    const pestanas = pestanasIsla(chat !== null);
    const boton = (p: ReturnType<typeof pestanasIsla>[number]) => {
        const activa = actual === p.id;
        const cuenta = cuentas[p.id];
        const ir = (e: MouseEvent<HTMLAnchorElement>) => {
            e.preventDefault();
            navegar(p.vista);
        };
        const Icono = ICONOS[p.id];
        return (
            <li key={p.id}>
                <a
                    href={urlDeVista(p.vista)}
                    onClick={ir}
                    aria-current={activa ? "page" : undefined}
                    aria-label={p.id === "yo" ? `Yo · ${sesion.nombre}` : cuenta > 0 ? `${p.texto} (${cuenta} sin leer)` : p.texto}
                    title={p.texto}
                    data-activa={activa ? "true" : undefined}
                    className={cn(
                        PESTANA_ISLA,
                        encogida && PESTANA_ENCOGIDA,
                        activa ? "bg-[var(--campo-tinta)] text-[var(--campo-sobre-tinta)]" : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    <Icono aria-hidden className="size-6 shrink-0" strokeWidth={activa ? 2.3 : 2} />
                    {/* El nombre solo en la activa (se despliega al cambiar). */}
                    <span
                        aria-hidden
                        data-slot="nombre-pestana"
                        className={cn(
                            "overflow-hidden text-base font-semibold whitespace-nowrap transition-[max-width,margin,opacity] duration-300 ease-out motion-reduce:transition-none",
                            activa ? "ml-1.5 max-w-24 opacity-100" : "ml-0 max-w-0 opacity-0",
                        )}
                    >
                        {p.texto}
                    </span>
                    {/* El chat: un punto teal (lo único teal de la isla) con mensajes sin leer; no con el chat abierto. */}
                    {cuenta > 0 && !activa ? <span aria-hidden data-slot="punto-no-leidos" className="absolute top-2.5 right-2.5 size-2.5 rounded-full bg-primary ring-2 ring-[var(--campo-isla)]" /> : null}
                </a>
            </li>
        );
    };
    const antes = pestanas.filter((p) => p.id === "hoy" || p.id === "chat");
    const despues = pestanas.filter((p) => p.id === "agenda" || p.id === "yo");
    return (
        <>
            <nav
                aria-label="Secciones de la app"
                data-slot="isla-menu"
                data-encogida={encogida ? "true" : undefined}
                className={cn(
                    "campo-suave fixed bottom-[calc(1.75rem+env(safe-area-inset-bottom))] left-1/2 z-30 -translate-x-1/2 rounded-full border border-border bg-[var(--campo-isla)] p-1.5 shadow-[var(--campo-sombra-isla)]",
                    encogida && "bottom-[calc(1rem+env(safe-area-inset-bottom))] p-1",
                )}
            >
                <ul className="flex items-center gap-0.5">
                    {antes.map(boton)}
                    <li>
                        {/* «+» sin relleno ni borde: icono de 26 px en color de texto. */}
                        <Boton
                            variante="fantasma"
                            aria-label="Acciones rápidas"
                            title="Acciones rápidas"
                            aria-haspopup="dialog"
                            data-slot="isla-mas"
                            className={cn(BOTON_ISLA, "size-[3.25rem] bg-transparent p-0 text-foreground shadow-none hover:bg-[var(--campo-panel-2)] [&_svg]:size-[1.625rem]", encogida ? "size-12 md:size-12" : "md:size-[3.25rem]")}
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
