"use client";

import { avisar, Boton, cn, MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento, PanelEmergente } from "@iris/ui";
import { Bell, CheckCheck, ClipboardPlus, Handshake, LifeBuoy, Plus, RotateCw, type LucideIcon } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { detalleAvisoPanel, enlaceAvisoPanel, hayQueActuar, iconoAvisoPanel, marcarLeido, tonoAvisoPanel, type AvisoPanel } from "@/lib/campana";
import { useLectura, type Lectura } from "@/lib/lectura";
import type { Permiso } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";
import { useResumenTickets } from "@/lib/tickets/resumen";

type Creable = { href: string; etiqueta: string; icono: LucideIcon; permiso: Permiso };

/** Lo que se puede crear desde la barra superior: cada uno con su pantalla propia (§10). */
export const CREABLES: readonly Creable[] = [
    { href: "/tickets/nuevo", etiqueta: "Nuevo ticket", icono: LifeBuoy, permiso: "tickets.crear" },
    { href: "/ordenes/nueva", etiqueta: "Nueva orden", icono: ClipboardPlus, permiso: "ordenes.crear" },
    { href: "/comercial/nuevo", etiqueta: "Nuevo prospecto", icono: Handshake, permiso: "prospectos.crear" },
];

/** «Crear»: un menú con lo que la persona puede crear; sin permiso para nada, no aparece. */
export function BotonCrear({ className }: { className?: string }) {
    const { puede } = useSesion();
    const router = useRouter();
    const opciones = CREABLES.filter((c) => puede([c.permiso]));
    if (opciones.length === 0) return null;
    return (
        <MenuDesplegable>
            <MenuDesplegableDisparador>
                <Boton tamano="sm" className={cn("h-9 rounded-lg px-3.5 text-[0.8125rem] font-semibold", className)} aria-label="Crear">
                    <Plus />
                    <span className="hidden sm:inline">Crear</span>
                </Boton>
            </MenuDesplegableDisparador>
            <MenuDesplegableContenido align="end">
                {opciones.map((c) => (
                    <MenuDesplegableElemento key={c.href} icono={c.icono} onSelect={() => router.push(c.href)}>
                        {c.etiqueta}
                    </MenuDesplegableElemento>
                ))}
            </MenuDesplegableContenido>
        </MenuDesplegable>
    );
}

type Pendiente = {
    id: string;
    texto: string;
    detalle?: string | null;
    href: string | null;
    tono: "error" | "warning" | "info";
    icono?: LucideIcon;
    /** Solo los avisos personales (`/yo/avisos`). */
    aviso?: AvisoPanel;
};

/** Cada cuánto se vuelven a leer los avisos de la campana (y al abrirla). */
const INTERVALO_AVISOS_MS = 60_000;
/** Los avisos más recientes que muestra la campana. */
const LIMITE_AVISOS = 10;

/**
 * Avisos personales de la campana (`GET /yo/avisos`, solo los de la empresa activa): hoy, el tope
 * del asistente de IA para los administradores. Se relee cada minuto y al cambiar de empresa.
 */
function useAvisosPanel(): Lectura<AvisoPanel[]> {
    const { yo } = useSesion();
    const lectura = useLectura(async (signal) => (await resolver(api().GET("/api/v1/yo/avisos", { params: { query: { limit: LIMITE_AVISOS } }, signal }))).data ?? []);
    const { refrescar, reintentar } = lectura;
    const empresa = yo.empresaActiva?.id ?? 0;
    const primera = useRef(true);
    useEffect(() => {
        if (primera.current) {
            primera.current = false;
            return;
        }
        reintentar();
    }, [empresa, reintentar]);
    useEffect(() => {
        const id = window.setInterval(() => {
            if (document.visibilityState === "visible") void refrescar();
        }, INTERVALO_AVISOS_MS);
        return () => window.clearInterval(id);
    }, [refrescar]);
    return lectura;
}

const claseTono = (t: Pendiente["tono"]) => (t === "error" ? "text-error" : t === "warning" ? "text-warning" : "text-info");
const fondoTono = (t: Pendiente["tono"]) => (t === "error" ? "bg-error" : t === "warning" ? "bg-warning" : "bg-info");

/** Lo que pide actuar según el resumen de la bandeja de tickets. */
function pendientesDeTickets(datos: { slaVencidos: number; slaEnRiesgo: number; sinAsignar: number } | null): Pendiente[] {
    if (!datos) return [];
    const lista: Pendiente[] = [];
    if (datos.slaVencidos > 0) {
        lista.push({ id: "vencidos", texto: datos.slaVencidos === 1 ? "1 ticket con el SLA vencido" : `${datos.slaVencidos} tickets con el SLA vencido`, href: "/tickets", tono: "error" });
    }
    if (datos.slaEnRiesgo > 0) {
        lista.push({ id: "riesgo", texto: datos.slaEnRiesgo === 1 ? "1 ticket con el SLA en riesgo" : `${datos.slaEnRiesgo} tickets con el SLA en riesgo`, href: "/tickets", tono: "warning" });
    }
    if (datos.sinAsignar > 0) {
        lista.push({ id: "sin-asignar", texto: datos.sinAsignar === 1 ? "1 ticket sin asignar" : `${datos.sinAsignar} tickets sin asignar`, href: "/tickets?vista=sin_asignar", tono: "info" });
    }
    return lista;
}

/**
 * La campana de la barra superior: los avisos personales (el tope del asistente de IA al 80 % y al
 * 100 %, §16.9) y lo que pide actuar según el resumen de la bandeja (SLA vencido, en riesgo, sin
 * asignar). Es un punto discreto (§10): aparece con avisos sin leer o tickets vencidos o en riesgo,
 * y crece solo cuando hay que actuar (un aviso `peligro` sin leer: el asistente se pausó). Abrir un
 * aviso lo marca leído.
 */
export function BotonAvisos() {
    const resumen = useResumenTickets();
    const avisos = useAvisosPanel();
    const [abierto, setAbierto] = useState(false);
    const [marcando, setMarcando] = useState(false);
    const datos = resumen?.estado === "listo" ? resumen.datos : null;
    const lista = avisos.estado === "listo" ? avisos.datos : [];
    const noLeidos = lista.filter((a) => !a.leido).length;

    function abrirAviso(a: AvisoPanel) {
        setAbierto(false);
        if (a.leido) return;
        avisos.reemplazar(marcarLeido(lista, a.id, new Date().toISOString()));
        // Es el estado personal de la campana: si falla, la próxima lectura lo vuelve a mostrar.
        resolver(api().POST("/api/v1/yo/avisos/{id}/leer", { params: { path: { id: a.id } } })).catch(() => void avisos.refrescar());
    }

    async function leerTodos() {
        setMarcando(true);
        try {
            const r = await resolver(api().POST("/api/v1/yo/avisos/leer-todos", {}));
            avisar({ titulo: r.leidos === 1 ? "1 aviso marcado como leído" : `${r.leidos} avisos marcados como leídos` });
            await avisos.refrescar();
        } catch (e) {
            const m = mensajeDeError(e);
            avisar({ tono: "error", titulo: "No se pudieron marcar como leídos", descripcion: m.detalle ?? m.titulo });
        } finally {
            setMarcando(false);
        }
    }

    const pendientes: Pendiente[] = [
        ...lista.map((a) => ({
            id: `aviso-${a.id}`,
            aviso: a,
            texto: a.titulo,
            detalle: detalleAvisoPanel(a),
            href: enlaceAvisoPanel(a.enlace),
            tono: tonoAvisoPanel(a.nivel),
            icono: iconoAvisoPanel(a.tipo),
        })),
        ...pendientesDeTickets(datos),
    ];
    const urgentes = datos ? datos.slaVencidos + datos.slaEnRiesgo : 0;
    const actuar = hayQueActuar(lista);
    const hayPunto = urgentes > 0 || noLeidos > 0;
    const partes = [
        noLeidos > 0 ? (noLeidos === 1 ? "1 aviso sin leer" : `${noLeidos} avisos sin leer`) : null,
        urgentes > 0 ? (urgentes === 1 ? "1 ticket necesita atención" : `${urgentes} tickets necesitan atención`) : null,
    ].filter((p): p is string => p !== null);
    const nombre = partes.length > 0 ? `Avisos: ${partes.join(" y ")}` : "Avisos";
    const errores = [
        avisos.estado === "error" ? { id: "avisos", que: "tus avisos", reintentar: avisos.reintentar } : null,
        resumen?.estado === "error" ? { id: "tickets", que: "los avisos de tickets", reintentar: resumen.reintentar } : null,
    ].filter((e): e is { id: string; que: string; reintentar: () => void } => e !== null);
    const cargando = avisos.estado === "cargando" || resumen?.estado === "cargando";
    return (
        <PanelEmergente
            titulo="Avisos"
            tituloVisible
            lado="bottom end"
            abierto={abierto}
            alCambiar={(a) => {
                setAbierto(a);
                if (a && avisos.estado === "listo") void avisos.refrescar();
            }}
            className="w-80 max-w-[calc(100vw-2rem)]"
            disparador={
                <Boton variante="secundario" tamano="icono-sm" className="relative size-9 rounded-lg" aria-label={nombre} title="Avisos">
                    <Bell />
                    {hayPunto ? (
                        <span
                            aria-hidden
                            data-testid="punto-avisos"
                            data-actuar={actuar ? "si" : "no"}
                            className={cn(
                                "absolute rounded-full",
                                actuar ? "top-1.5 right-1.5 size-2.5 bg-error ring-2 ring-card" : "top-2 right-2 size-[0.4375rem]",
                                !actuar && (urgentes > 0 ? "bg-error" : "bg-warning"),
                            )}
                        />
                    ) : null}
                </Boton>
            }
        >
            <div className="grid gap-3">
                {errores.map((e) => (
                    <div key={e.id} className="grid gap-2 text-sm">
                        <p className="text-muted-foreground">No se pudieron leer {e.que}.</p>
                        <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={e.reintentar}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    </div>
                ))}
                {pendientes.length > 0 ? (
                    <ul className="grid gap-1" aria-label="Pendientes">
                        {pendientes.map((p) => {
                            const Icono = p.icono;
                            const sinLeer = p.aviso ? !p.aviso.leido : false;
                            const contenido = (
                                <>
                                    {Icono ? (
                                        <Icono aria-hidden className={cn("mt-0.5 size-4 shrink-0", claseTono(p.tono))} />
                                    ) : (
                                        <span aria-hidden className={cn("mt-1.5 size-2 shrink-0 rounded-full", fondoTono(p.tono))} />
                                    )}
                                    <span className="grid min-w-0 gap-0.5">
                                        <span className={cn("break-words", sinLeer && "font-medium", p.aviso?.leido && "text-muted-foreground")}>{p.texto}</span>
                                        {p.detalle ? <span className="text-xs text-muted-foreground">{p.detalle}</span> : null}
                                        {sinLeer ? <span className="sr-only">Sin leer</span> : null}
                                    </span>
                                </>
                            );
                            const clase = "flex items-start gap-2.5 rounded-md px-2 py-1.5 text-[0.8125rem]";
                            const aviso = p.aviso;
                            return (
                                <li key={p.id} data-aviso={aviso ? aviso.tipo : p.id} data-leido={aviso ? String(aviso.leido) : undefined}>
                                    {p.href ? (
                                        <Link href={p.href} onClick={() => (aviso ? abrirAviso(aviso) : setAbierto(false))} className={cn(clase, "hover:bg-elevated")}>
                                            {contenido}
                                        </Link>
                                    ) : (
                                        <div className={clase}>{contenido}</div>
                                    )}
                                </li>
                            );
                        })}
                    </ul>
                ) : cargando ? (
                    <p className="text-sm text-muted-foreground">Cargando…</p>
                ) : errores.length === 0 ? (
                    <p className="text-sm text-muted-foreground">{resumen === null ? "Todo al día: no tienes avisos." : "Todo al día: nada vencido, en riesgo ni sin asignar."}</p>
                ) : null}
                {noLeidos > 0 ? (
                    <Boton variante="fantasma" tamano="sm" className="justify-self-start" onClick={() => void leerTodos()} disabled={marcando}>
                        <CheckCheck />
                        {marcando ? "Marcando…" : "Marcar todo como leído"}
                    </Boton>
                ) : null}
            </div>
        </PanelEmergente>
    );
}
