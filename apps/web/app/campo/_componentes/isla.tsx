"use client";

import type { PlantillaFormulario } from "@iris/sync";
import { accionesDe, accionesRapidas, avisoIsla, contadoresPestanas, estadoIslaPasos, nombreCliente, pestanaDe, PESTANAS, urlDeVista, type PasoOrden, type Pestana, type PrimarioIsla, type Vista } from "@iris/sync/campo";
import { Boton, GrupoOpciones, InsigniaEstado, cn, type Tono } from "@iris/ui";
import { ArrowLeft, ArrowRight, CalendarClock, CalendarDays, Check, CircleCheck, CloudUpload, Building2, FilePlus2, House, KeyRound, PackageOpen, PenLine, Plus, Save, TriangleAlert, UserRound, WifiOff, type LucideIcon } from "lucide-react";
import { useMemo, useState, type MouseEvent } from "react";
import { RUTA_ENTRAR } from "./app-campo";
import { useGuardarPaso, useSinGuardar } from "./borradores";
import { useCampo, useOrden } from "./contexto";
import { HojaCampo } from "./hoja";
import { useInventario } from "./inventario-datos";

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
            {/* Se monta al abrir: cada vez empieza sin elegir (con una sola orden, ya elegida). */}
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
    const { guardar, guardando, guardado } = useGuardarPaso();
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
 * Hoja del «+» con lo que ya tiene API: «Agregar formulario» a una orden con el trabajo empezado
 * (`orden.agregar_formulario`, también sin señal; solo con `ordenes.registrar`) y «Agendar tarea
 * propia», deshabilitada hasta que exista en la API.
 */
function HojaAcciones({ alCambiar }: { alCambiar: (abierta: boolean) => void }) {
    const { estado, sesion, motor, ejecutar } = useCampo();
    const r = useMemo(() => accionesRapidas(estado.ordenes, estado.plantillas, sesion.permisos), [estado.ordenes, estado.plantillas, sesion.permisos]);
    const ordenes = r.formulario?.ordenes ?? [];
    const [elegidaId, setOrdenId] = useState<string>("");
    // Con una sola orden donde se puede, va elegida (también si llega después de abrir la hoja).
    const ordenId = elegidaId || (ordenes.length === 1 ? String(ordenes[0]?.orden.id) : "");
    const [plantillaId, setPlantillaId] = useState<string>("");
    const [ocupado, setOcupado] = useState(false);

    const elegida = ordenes.find((o) => String(o.orden.id) === ordenId) ?? null;
    const plantilla: PlantillaFormulario | null = elegida?.plantillas.find((p) => String(p.id) === plantillaId) ?? null;

    const agregar = async () => {
        if (!elegida || !plantilla) {
            return;
        }
        setOcupado(true);
        const orden = elegida.orden;
        const ok = await ejecutar(
            () => motor.registrar("orden.agregar_formulario", orden.id, { plantillaId: plantilla.id, versionId: plantilla.versionId }),
            `«${plantilla.nombre}» agregado a ${orden.numero}`,
            {
                // El formulario lleva el id de la operación: quitarlo es el inverso exacto.
                deshacer: (r) => () => void ejecutar(() => motor.registrar("orden.quitar_formulario", orden.id, { formularioId: r.operacion.id }), `«${plantilla.nombre}» quitado`),
            },
        );
        setOcupado(false);
        if (ok) {
            alCambiar(false);
        }
    };

    return (
        <HojaCampo abierto alCambiar={alCambiar} titulo="Acciones rápidas" cambios={plantillaId !== "" && !ocupado} className="rounded-t-[2rem] sm:rounded-[2rem]">
                <div className="grid gap-3" data-slot="hoja-acciones">
                    {r.formulario ? (
                        <section aria-labelledby="titulo-agregar-formulario" className="grid gap-3 rounded-[1.375rem] bg-[var(--campo-panel-2)] p-3">
                            <div className="flex items-center gap-3">
                                <span aria-hidden className="grid size-11 shrink-0 place-content-center rounded-full bg-primary text-primary-foreground">
                                    <FilePlus2 className="size-5" />
                                </span>
                                <div className="grid gap-0.5">
                                    <h3 id="titulo-agregar-formulario" className="font-semibold">
                                        Agregar formulario
                                    </h3>
                                    <p className="text-sm text-muted-foreground">A una orden con el trabajo empezado. Se guarda en el teléfono.</p>
                                </div>
                            </div>
                            {ordenes.length === 0 ? (
                                <p className="text-sm text-muted-foreground">Ninguna orden en camino o en sitio: empieza una (o reanuda la pausada) para agregarle un formulario.</p>
                            ) : (
                                <>
                                    {ordenes.length > 1 ? (
                                        <GrupoOpciones
                                            leyenda="Orden"
                                            nombre="orden-formulario"
                                            valor={ordenId || undefined}
                                            alCambiar={(v) => {
                                                setOrdenId(v);
                                                setPlantillaId("");
                                            }}
                                            opciones={ordenes.map(({ orden }) => ({ valor: String(orden.id), etiqueta: `${orden.numero} · ${nombreCliente(orden)}` }))}
                                        />
                                    ) : elegida ? (
                                        <p className="text-sm">
                                            <span className="font-mono">{elegida.orden.numero}</span> · {nombreCliente(elegida.orden)}
                                        </p>
                                    ) : null}
                                    {elegida ? (
                                        elegida.plantillas.length === 0 ? (
                                            <p className="text-sm text-muted-foreground">Esta orden ya tiene todos los formularios del catálogo.</p>
                                        ) : (
                                            <GrupoOpciones
                                                leyenda="Plantilla"
                                                nombre="plantilla-formulario"
                                                valor={plantillaId || undefined}
                                                alCambiar={setPlantillaId}
                                                opciones={elegida.plantillas.map((p) => ({ valor: String(p.id), etiqueta: p.nombre, detalle: `Versión ${p.numeroVersion}` }))}
                                            />
                                        )
                                    ) : null}
                                    <Boton tamano="lg" className="h-[3.25rem] w-full rounded-full" disabled={!plantilla || ocupado} onClick={() => void agregar()}>
                                        {plantilla ? `Agregar «${plantilla.nombre}»` : "Elige una plantilla"}
                                    </Boton>
                                </>
                            )}
                        </section>
                    ) : null}
                    <div className="flex items-center gap-3 rounded-[1.375rem] p-3" aria-disabled="true" data-slot="accion-tarea">
                        <span aria-hidden className="grid size-11 shrink-0 place-content-center rounded-full bg-[var(--campo-panel-2)] text-muted-foreground">
                            <CalendarClock className="size-5" />
                        </span>
                        <div className="grid flex-1 gap-0.5">
                            <p className="font-semibold text-muted-foreground">Agendar tarea propia</p>
                            <p className="text-sm text-muted-foreground">Aparecerá en tu agenda y la verá tu supervisor.</p>
                        </div>
                        <InsigniaEstado tono="neutral" icono={CalendarClock} texto={r.tarea.motivo} />
                    </div>
                </div>
        </HojaCampo>
    );
}
