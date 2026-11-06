"use client";

import { accionesDe, accionesRapidas, avisoIsla, contadoresPestanas, estadoIslaPasos, pestanaDe, PESTANAS, urlDeVista, type PasoOrden, type Pestana, type PrimarioIsla, type Vista } from "@iris/sync/campo";
import { Boton, cn, type Tono } from "@iris/ui";
import { ArrowLeft, ArrowRight, CalendarClock, CalendarDays, Check, ChevronRight, CircleCheck, CloudUpload, Building2, Hand, House, KeyRound, PackageOpen, PenLine, Plus, Save, TriangleAlert, UserRound, WifiOff, type LucideIcon } from "lucide-react";
import { useEffect, useState, type MouseEvent } from "react";
import { RUTA_ENTRAR } from "./app-campo";
import { useGuardarPaso, useRecienGuardado, useSinGuardar } from "./borradores";
import { useCampo, useOrden } from "./contexto";
import { HojaCampo } from "./hoja";
import { useInventario } from "./inventario-datos";
import { useLibres } from "./libres-datos";

const ICONOS: Record<Pestana, LucideIcon> = { hoy: House, recibir: PackageOpen, agenda: CalendarDays, yo: UserRound };

/** Zonas táctiles de la isla: 54 px (44 px encogida, el mínimo de Iris). */
const BOTON_ISLA = "relative grid size-[3.375rem] place-content-center rounded-full transition-[width,height] focus-visible:outline-2 focus-visible:-outline-offset-2";
const BOTON_ENCOGIDO = "size-11";

/**
 * Isla de menú flotante (diseño «combinada»): píldora centrada abajo con Hoy, Por recibir (con su
 * contador), «+» en teal al centro, Agenda y Yo. Al bajar se encoge y al subir vuelve. Reemplaza la
 * barra inferior de la fase 4; «Por enviar» se abre desde la píldora de estado o desde «Yo».
 */
export function IslaMenu({ encogida }: { encogida: boolean }) {
    const { vista, navegar, estado } = useCampo();
    const { traslados } = useInventario();
    const [acciones, setAcciones] = useState(false);
    const actual = pestanaDe(vista);
    const cuentas = contadoresPestanas(estado, traslados.datos);
    const boton = (p: (typeof PESTANAS)[number]) => {
        const Icono = ICONOS[p.id];
        const activa = actual === p.id;
        const cuenta = cuentas[p.id];
        const ir = (e: MouseEvent<HTMLAnchorElement>) => {
            e.preventDefault();
            navegar(p.vista);
        };
        return (
            <li key={p.id}>
                <a
                    href={urlDeVista(p.vista)}
                    onClick={ir}
                    aria-current={activa ? "page" : undefined}
                    aria-label={cuenta > 0 ? `${p.texto} (${cuenta})` : p.texto}
                    title={p.texto}
                    className={cn(
                        BOTON_ISLA,
                        encogida && BOTON_ENCOGIDO,
                        activa ? "bg-[var(--campo-tinta)] text-[var(--campo-sobre-tinta)]" : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    <Icono aria-hidden className="size-6" strokeWidth={activa ? 2.4 : 2} />
                    {cuenta > 0 ? (
                        <span aria-hidden className="absolute top-2 right-2 grid min-w-4 place-content-center rounded-full bg-primary px-1 text-[0.625rem] leading-4 font-bold text-primary-foreground tabular-nums">
                            {cuenta > 99 ? "99+" : cuenta}
                        </span>
                    ) : null}
                </a>
            </li>
        );
    };
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
                <ul className={cn("flex items-center gap-1", encogida && "gap-0.5")}>
                    {PESTANAS.slice(0, 2).map(boton)}
                    <li>
                        <Boton
                            aria-label="Acciones rápidas"
                            title="Acciones rápidas"
                            aria-haspopup="dialog"
                            className={cn(BOTON_ISLA, encogida && BOTON_ENCOGIDO, "p-0 shadow-none [&_svg]:size-6", encogida ? "md:size-11" : "md:size-[3.375rem]")}
                            onClick={() => setAcciones(true)}
                        >
                            <Plus aria-hidden strokeWidth={2.4} />
                        </Boton>
                    </li>
                    {PESTANAS.slice(2).map(boton)}
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
    const redondo = cn(BOTON_ISLA, encogida && BOTON_ENCOGIDO, "bg-[var(--campo-panel-2)] p-0 text-foreground shadow-none [&_svg]:size-6", encogida ? "md:size-11" : "md:size-[3.375rem]");
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
                className={cn("min-w-[9.5rem] rounded-full px-6 text-base shadow-none", encogida ? "h-11" : "h-[3.375rem]")}
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

const TONO_PILDORA: Record<Tono, string> = {
    neutral: "border-border bg-[var(--campo-panel)] text-foreground",
    info: "border-[var(--campo-info-borde)] bg-[var(--campo-info-fondo)] text-info",
    success: "border-border bg-[var(--campo-panel)] text-success",
    warning: "border-[var(--campo-aviso-borde)] bg-[var(--campo-aviso-fondo)] text-warning",
    error: "border-[var(--campo-error-borde)] bg-[var(--campo-error-fondo)] text-error",
};

/**
 * Píldora de estado encima de la isla: la barra de estado discreta de la fase 4 (`avisoIsla` de
 * `@iris/sync/campo`). Con todo enviado no sale (queda el punto del encabezado); con algo por
 * enviar o un rechazo lleva a «Por enviar»; sesión vencida y empresa no válida, a lo suyo.
 */
export function AvisoIsla({ alto, pedirEmpresa }: { /** Hay algo abajo (la isla o la barra de pasos): va encima. */ alto: boolean; pedirEmpresa: () => void }) {
    const { estado, navegar } = useCampo();
    const a = avisoIsla(estado);
    if (!a) {
        return null;
    }
    const Icono = a.rechazo ? TriangleAlert : estado.conexion === "sin_sesion" ? KeyRound : estado.conexion === "sin_empresa" ? Building2 : estado.conexion === "sin_conexion" ? WifiOff : CloudUpload;
    const tocar = () => {
        if (a.accion === "pendientes") {
            navegar({ tipo: "pendientes" });
        } else if (a.accion === "entrar") {
            window.location.assign(`${RUTA_ENTRAR}&motivo=expirada`);
        } else if (a.accion === "empresa") {
            pedirEmpresa();
        }
    };
    return (
        <section
            aria-label="Estado de la sincronización"
            data-slot="barra-conexion"
            data-conexion={estado.conexion}
            className={cn(
                "campo-suave campo-entra-arriba fixed left-1/2 z-30 w-max max-w-[calc(100vw-2rem)] -translate-x-1/2",
                alto ? "bottom-[calc(6.5rem+env(safe-area-inset-bottom))]" : "bottom-[calc(1.75rem+env(safe-area-inset-bottom))]",
            )}
        >
            <Boton
                variante="secundario"
                aria-label={a.etiqueta}
                className={cn("h-auto min-h-11 max-w-full gap-2 rounded-full border px-3.5 py-1.5 text-[0.8125rem] font-semibold shadow-[var(--campo-sombra)] whitespace-normal", TONO_PILDORA[a.tono])}
                onClick={tocar}
            >
                <Icono aria-hidden className="shrink-0" />
                <span data-testid="estado-conexion">{a.estado}</span>
                {a.detalle ? (
                    <>
                        <span aria-hidden>·</span>
                        <span data-testid="pendientes-por-enviar">{a.detalle}</span>
                    </>
                ) : null}
            </Boton>
        </section>
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
