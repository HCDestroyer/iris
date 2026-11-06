"use client";

import type { OrdenHistorial, OrdenVista } from "@iris/sync";
import { accionesPantallaOrden, encabezadoOrden, motivoSinFormulario, ESTADOS_TEXTO, fechaHoraCorta, puedeReanudar, siguientePaso, TIPOS_ORDEN, textoProblema } from "@iris/sync/campo";
import { ErrorTransporte } from "@iris/sync";
import {
    Aviso,
    Boton,
    Cargando,
    GrupoBotones,
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    cn,
} from "@iris/ui";
import { ArrowLeft, CircleX, Clock, Copy, Ellipsis, FilePlus2, History, ListChecks, Navigation, PauseCircle, PenLine, Play } from "lucide-react";
import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { HojaAgregarFormulario } from "./checklist";
import { HojaFallar } from "./cierre";
import { useCampo } from "./contexto";
import { HojaCampo } from "./hoja";
import { HojaPausar, useReanudar } from "./pausa";
import { AVISO_TURNO_INICIADO, useUbicacion } from "./ubicacion";

const PUNTO: Record<string, string> = {
    neutral: "bg-muted-foreground",
    info: "bg-primary",
    success: "bg-success",
    warning: "bg-warning",
    error: "bg-error",
};

/**
 * Encabezado de la orden: ← | número con «● Estado · Tipo · ventana» | «⋯» (Agregar nota, Agregar
 * formulario, Historial del cliente, Copiar dirección). «Agregar formulario» sale deshabilitado con
 * el motivo si no aplica. El chat de la orden se suma al menú (tras «Agregar formulario») con el chat.
 */
export function EncabezadoOrden({ orden, alVolver, alAgregarNota }: { orden: OrdenVista; alVolver: () => void; alAgregarNota: (() => void) | null }) {
    const { notificar, sesion } = useCampo();
    const [historial, setHistorial] = useState(false);
    const [formulario, setFormulario] = useState(false);
    const sinFormulario = motivoSinFormulario(orden, sesion.permisos);
    const e = encabezadoOrden(orden);
    const clienteId = orden.cliente?.id ?? orden.clienteId ?? null;
    const copiar = async () => {
        try {
            await navigator.clipboard.writeText(orden.direccion);
            notificar({ tono: "success", texto: "Dirección copiada" });
        } catch {
            notificar({ tono: "error", texto: "No se pudo copiar la dirección" });
        }
    };
    return (
        <header className="grid grid-cols-[3rem_minmax(0,1fr)_3rem] items-center gap-2" data-slot="encabezado-orden">
            <Boton
                variante="secundario"
                aria-label="Mis órdenes"
                title="Mis órdenes"
                className="size-12 shrink-0 rounded-full bg-[var(--campo-panel)] p-0 md:size-12 [&_svg]:size-5"
                onClick={alVolver}
            >
                <ArrowLeft aria-hidden />
            </Boton>
            <div className="grid min-w-0 justify-items-center gap-0.5 text-center">
                <h1 id="titulo-orden" className="font-mono text-xl font-semibold tracking-[-0.01em]">
                    {orden.numero}
                </h1>
                <p className="flex max-w-full items-center gap-1.5 truncate text-sm text-muted-foreground" data-slot="linea-estado">
                    <span aria-hidden className={cn("size-2 shrink-0 rounded-full", PUNTO[e.tono])} />
                    <span className="truncate">{e.texto}</span>
                </p>
            </div>
            <MenuDesplegable>
                <MenuDesplegableDisparador>
                    <Boton variante="secundario" aria-label="Más acciones de la orden" title="Más acciones" className="size-12 shrink-0 rounded-full bg-[var(--campo-panel)] p-0 md:size-12 [&_svg]:size-5">
                        <Ellipsis aria-hidden />
                    </Boton>
                </MenuDesplegableDisparador>
                <MenuDesplegableContenido className="min-w-60">
                    {alAgregarNota ? (
                        <MenuDesplegableElemento icono={PenLine} className="min-h-12 text-base" onSelect={alAgregarNota}>
                            Agregar nota
                        </MenuDesplegableElemento>
                    ) : null}
                    <MenuDesplegableElemento
                        icono={FilePlus2}
                        className="min-h-12 text-base"
                        disabled={sinFormulario !== null}
                        textValue="Agregar formulario"
                        onSelect={() => setFormulario(true)}
                    >
                        <span className="grid">
                            <span>Agregar formulario</span>
                            {sinFormulario ? (
                                <span className="text-xs text-muted-foreground" data-slot="motivo-formulario">
                                    {sinFormulario}
                                </span>
                            ) : null}
                        </span>
                    </MenuDesplegableElemento>
                    {clienteId !== null ? (
                        <MenuDesplegableElemento icono={History} className="min-h-12 text-base" onSelect={() => setHistorial(true)}>
                            Historial del cliente
                        </MenuDesplegableElemento>
                    ) : null}
                    {orden.direccion ? (
                        <MenuDesplegableElemento icono={Copy} className="min-h-12 text-base" onSelect={() => void copiar()}>
                            Copiar dirección
                        </MenuDesplegableElemento>
                    ) : null}
                </MenuDesplegableContenido>
            </MenuDesplegable>
            <HojaAgregarFormulario orden={orden} abierto={formulario} alCambiar={setFormulario} />
            {historial && clienteId !== null ? <HojaHistorial orden={orden} clienteId={clienteId} alCerrar={() => setHistorial(false)} /> : null}
        </header>
    );
}

/** «En camino» o «Llegué»: con GPS si el técnico lo permite; también empieza el turno (con aviso). */
export function useMarcarEstado(orden: OrdenVista) {
    const { motor, ejecutar } = useCampo();
    const ubicacion = useUbicacion();
    const [ocupado, setOcupado] = useState<null | "camino" | "llegar">(null);
    const correr = async (cual: "camino" | "llegar") => {
        setOcupado(cual);
        // Con GPS si el técnico lo permite (explicado antes de que el teléfono pregunte); sin él,
        // la acción se registra igual en la cola sin conexión.
        const p = await ubicacion.posicionParaAccion();
        const datos = p ? { lat: p.lat, lon: p.lon, precision: p.precision } : {};
        // Salir hacia el cliente o llegar es estar en turno: empieza la ubicación en vivo, con aviso
        // (solo si el técnico aceptó la explicación y dio el permiso).
        const empiezaTurno = ubicacion.turnoEmpezariaSolo();
        const texto =
            cual === "camino" ? (p ? "En camino, con ubicación" : "En camino, sin ubicación") : p ? "Llegada registrada con ubicación" : "Llegada registrada sin ubicación";
        const guardada = await ejecutar(
            () => motor.registrar(cual === "camino" ? "orden.iniciar_camino" : "orden.llegar", orden.id, datos),
            empiezaTurno ? `${texto}. ${AVISO_TURNO_INICIADO}` : texto,
        );
        if (guardada) {
            // Fuera de turno lo empieza (si puede); en turno, lo mantiene hasta la última orden.
            ubicacion.iniciarTurnoSolo();
        }
        setOcupado(null);
    };
    return { correr, ocupado };
}

/**
 * Lo de arriba del resumen (decisión del PM): la acción principal en «tinta» a todo el ancho y,
 * debajo, [En camino | No se pudo] (programada) o [Pausar | No se pudo] (en camino o en sitio).
 * Pausada: solo el aviso naranja «En pausa desde HH:MM · motivo» y «Reanudar».
 */
export function AccionesPrincipales({ orden }: { orden: OrdenVista }) {
    const { navegar, sesion } = useCampo();
    const a = accionesPantallaOrden(orden, puedeReanudar(sesion.permisos));
    const { correr, ocupado } = useMarcarEstado(orden);
    const reanudar = useReanudar(orden);
    const [pausando, setPausando] = useState(false);
    const [fallando, setFallando] = useState(false);
    if (a.pausa) {
        return (
            <div className="grid gap-3">
                <p role="status" data-testid="pausa-orden" className="flex min-h-12 items-center gap-2.5 rounded-2xl border border-warning/40 bg-warning/10 px-4 py-2.5 text-base font-semibold text-warning">
                    <Clock aria-hidden className="size-5 shrink-0" />
                    {a.pausa.texto}
                </p>
                {a.reanudar ? (
                    <Boton tamano="lg" className="h-[3.25rem] w-full rounded-full text-base md:h-[3.25rem]" onClick={() => void reanudar()}>
                        <Play aria-hidden />
                        Reanudar
                    </Boton>
                ) : (
                    <p className="text-sm text-muted-foreground">No tienes permiso para reanudarla: pídeselo a despacho.</p>
                )}
            </div>
        );
    }
    if (!a.principal && a.grupo.length === 0) {
        return null;
    }
    return (
        <div className="grid gap-3">
            {a.principal ? (
                <Boton
                    variante="tinta"
                    tamano="lg"
                    data-slot="accion-principal"
                    className="h-[3.25rem] w-full rounded-full text-base md:h-[3.25rem]"
                    onClick={() => navegar({ tipo: "orden", id: orden.id, paso: a.principal?.empezado ? siguientePaso(orden) : "llegada" })}
                >
                    {a.principal.empezado ? <ListChecks aria-hidden /> : <Play aria-hidden />}
                    {a.principal.texto}
                </Boton>
            ) : null}
            {a.grupo.length > 0 ? (
                <GrupoBotones etiqueta="Más acciones de la orden" anchoCompleto>
                    {a.grupo.map((g) =>
                        g === "en_camino" ? (
                            <Boton key={g} variante="fantasma" tamano="lg" disabled={ocupado !== null} onClick={() => void correr("camino")}>
                                <Navigation aria-hidden />
                                {ocupado === "camino" ? "Ubicando…" : "En camino"}
                            </Boton>
                        ) : g === "pausar" ? (
                            <Boton key={g} variante="fantasma" tamano="lg" onClick={() => setPausando(true)}>
                                <PauseCircle aria-hidden />
                                Pausar
                            </Boton>
                        ) : (
                            <Boton key={g} variante="fantasma" tamano="lg" className="text-error [--button-fg:var(--hx-error)]" onClick={() => setFallando(true)}>
                                <CircleX aria-hidden />
                                No se pudo
                            </Boton>
                        ),
                    )}
                </GrupoBotones>
            ) : null}
            <HojaPausar orden={orden} abierto={pausando} alCambiar={setPausando} />
            <HojaFallar orden={orden} abierto={fallando} alCambiar={setFallando} />
        </div>
    );
}

// Historial del cliente --------------------------------------------------------------------------

type LeerHistorial = (clienteId: number) => Promise<OrdenHistorial[]>;

const ContextoHistorial = createContext<LeerHistorial | null>(null);

/** El lector del historial (`GET /ordenes?clienteId=`, solo con señal) del motor abierto. */
export function ProveedorHistorial({ leer, children }: { leer: LeerHistorial; children: ReactNode }) {
    return <ContextoHistorial.Provider value={leer}>{children}</ContextoHistorial.Provider>;
}

type EstadoHistorial = { estado: "cargando" } | { estado: "lista"; ordenes: OrdenHistorial[] } | { estado: "error"; texto: string };

/** Hoja «Historial del cliente»: sus órdenes que fueron del técnico, con «Reintentar» si falla. */
function HojaHistorial({ orden, clienteId, alCerrar }: { orden: OrdenVista; clienteId: number; alCerrar: () => void }) {
    const leer = useContext(ContextoHistorial);
    const [estado, setEstado] = useState<EstadoHistorial>(leer ? { estado: "cargando" } : { estado: "error", texto: "El historial no está disponible en este teléfono." });
    // Solo con señal y sin guardar: se lee al abrir la hoja (y con «Reintentar»).
    const pedir = useCallback(() => {
        if (!leer) {
            return;
        }
        leer(clienteId)
            .then((ordenes) => setEstado({ estado: "lista", ordenes: ordenes.filter((o) => o.id !== orden.id) }))
            .catch((e: unknown) =>
                setEstado({
                    estado: "error",
                    texto: e instanceof ErrorTransporte && e.tipo === "red" ? "Sin señal: el historial se lee con conexión." : textoProblema(e instanceof ErrorTransporte ? e.problema : null, "No se pudo leer el historial."),
                }),
            );
    }, [leer, clienteId, orden.id]);
    useEffect(() => {
        pedir();
    }, [pedir]);
    const cargar = () => {
        setEstado({ estado: "cargando" });
        pedir();
    };
    const nombre = orden.cliente?.nombre ?? orden.contacto.nombre ?? "el cliente";
    return (
        <HojaCampo abierto alCambiar={(a) => !a && alCerrar()} titulo="Historial del cliente" descripcion={`Las órdenes de ${nombre} que fueron tuyas.`}>
            {estado.estado === "cargando" ? (
                <Cargando visible texto="Leyendo el historial…" />
            ) : estado.estado === "error" ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo leer el historial"
                    accion={
                        <Boton variante="secundario" tamano="lg" onClick={cargar}>
                            Reintentar
                        </Boton>
                    }
                >
                    {estado.texto}
                </Aviso>
            ) : estado.ordenes.length === 0 ? (
                <p className="text-sm text-muted-foreground">No hay otras órdenes tuyas con este cliente.</p>
            ) : (
                <ul className="grid gap-2" aria-label="Órdenes del cliente">
                    {estado.ordenes.map((o) => (
                        <li key={o.id} className="grid gap-0.5 rounded-2xl border border-border p-3 text-sm">
                            <span className="flex items-center justify-between gap-2">
                                <span className="font-mono font-semibold">{o.numero}</span>
                                <span className="text-muted-foreground">{ESTADOS_TEXTO[o.estado].texto}</span>
                            </span>
                            <span className="text-muted-foreground">
                                {TIPOS_ORDEN[o.tipo]}
                                {o.programadaInicio ? ` · ${fechaHoraCorta(o.programadaInicio)}` : ""}
                            </span>
                        </li>
                    ))}
                </ul>
            )}
        </HojaCampo>
    );
}
