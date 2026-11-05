"use client";

import type { PlantillaFormulario } from "@iris/sync";
import { accionesDe, accionesRapidas, contadoresPestanas, nombreCliente, pestanaDe, pestanasIsla, TITULOS_PASO, urlDeVista, vecinosPaso, type PasoOrden, type Pestana, type Vista } from "@iris/sync/campo";
import { Boton, GrupoOpciones, InsigniaEstado, cn } from "@iris/ui";
import { ArrowLeft, ArrowRight, CalendarClock, CalendarDays, FilePlus2, House, MessageCircle, Plus, Save, User, type LucideIcon } from "lucide-react";
import { useMemo, useState, type MouseEvent } from "react";
import { useGuardarPaso, useSinGuardar } from "./borradores";
import { useChatOpcional } from "./chat-datos";
import { useCampo, useOrden } from "./contexto";
import { HojaCampo } from "./hoja";

const ICONOS: Record<Pestana, LucideIcon> = { hoy: House, chat: MessageCircle, agenda: CalendarDays, yo: User };

/** Zonas táctiles de la isla: 54 px (44 px encogida, el mínimo de Iris). */
const BOTON_ISLA = "relative grid size-[3.375rem] place-content-center rounded-full transition-[width,height] focus-visible:outline-2 focus-visible:-outline-offset-2";
const BOTON_ENCOGIDO = "size-11";

/**
 * Isla de menú flotante (opción elegida por el PM, nov 2026): píldora centrada abajo con Hoy, Chat
 * (punto de no leídos), «+» en teal al centro, Agenda y Yo (icono de usuario). Al bajar se encoge
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
                    className={cn(
                        BOTON_ISLA,
                        encogida && BOTON_ENCOGIDO,
                        activa ? "bg-[var(--campo-tinta)] text-[var(--campo-sobre-tinta)]" : "text-muted-foreground hover:text-foreground",
                    )}
                >
                    <Icono aria-hidden className="size-6" strokeWidth={activa ? 2.4 : 2} />
                    {/* El chat: un punto (discreto) cuando hay mensajes sin leer. */}
                    {cuenta > 0 ? <span aria-hidden data-slot="punto-no-leidos" className="absolute top-2.5 right-2.5 size-2.5 rounded-full bg-primary ring-2 ring-[var(--campo-isla)]" /> : null}
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
                <ul className={cn("flex items-center gap-1", encogida && "gap-0.5")}>
                    {antes.map(boton)}
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
                    {despues.map(boton)}
                </ul>
            </nav>
            {/* Se monta al abrir: cada vez empieza sin elegir (con una sola orden, ya elegida). */}
            {acciones ? <HojaAcciones alCambiar={setAcciones} /> : null}
        </>
    );
}

/**
 * Isla de los pasos de la orden: la misma píldora que la isla de menú, con «Anterior» y «Siguiente»
 * redondos y un solo botón primario «Guardar» al centro, que guarda lo escrito y todavía sin guardar
 * de todo el paso (con su contador) y avisa qué pasó. Lo demás ya se guardó al hacerlo. Pasar de
 * paso también guarda primero (si algo no valida, se queda y lo dice). Con la orden en pausa,
 * «Guardar» está deshabilitado: no hay trabajo que registrar.
 */
export function IslaPasos({ id, paso, encogida }: { id: number; paso: PasoOrden; encogida: boolean }) {
    const { navegar } = useCampo();
    const orden = useOrden(id);
    const { guardar, guardando } = useGuardarPaso();
    const sinGuardar = useSinGuardar();
    if (!orden) {
        return null;
    }
    const { anterior, siguiente } = vecinosPaso(paso);
    const vistaAnterior: Vista = anterior ? { tipo: "orden", id, paso: anterior } : { tipo: "orden", id };
    const textoAnterior = anterior ? "Anterior" : "Resumen";
    const textoSiguiente = siguiente ? (siguiente === "cierre" ? "Firma y cierre" : `Siguiente: ${TITULOS_PASO[siguiente].toLowerCase()}`) : null;
    const enPausa = accionesDe(orden).enPausa;
    const ir = async (vista: Vista) => {
        if (await guardar({ silencioso: true })) {
            navegar(vista);
        }
    };
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
            <Boton variante="secundario" aria-label={textoAnterior} title={textoAnterior} className={redondo} onClick={() => void ir(vistaAnterior)}>
                <ArrowLeft aria-hidden />
            </Boton>
            <Boton
                tamano="lg"
                data-slot="guardar-paso"
                disabled={enPausa || guardando}
                aria-label={sinGuardar > 0 ? `Guardar (${sinGuardar} sin guardar)` : "Guardar"}
                className={cn("min-w-[9.5rem] rounded-full px-6 text-base shadow-none", encogida ? "h-11" : "h-[3.375rem]")}
                onClick={() => void guardar()}
            >
                <Save aria-hidden />
                {sinGuardar > 0 ? `Guardar · ${sinGuardar}` : "Guardar"}
            </Boton>
            {siguiente && textoSiguiente ? (
                <Boton variante="secundario" aria-label={textoSiguiente} title={textoSiguiente} className={redondo} onClick={() => void ir({ tipo: "orden", id, paso: siguiente })}>
                    <ArrowRight aria-hidden />
                </Boton>
            ) : null}
        </nav>
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
