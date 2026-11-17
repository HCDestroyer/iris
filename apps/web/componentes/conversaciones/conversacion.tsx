"use client";

import { Aviso, Boton, cn, Esqueleto, Hoja, HojaContenido } from "@iris/ui";
import { ArrowLeft, PanelRight, RotateCw } from "lucide-react";
import Link from "next/link";
import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { api } from "@/lib/api/cliente";
import { leerDetalle, leerMensajes } from "@/lib/conversaciones/api";
import { fusionarMensajes, pendientesSinConfirmar, type Pendiente } from "@/lib/conversaciones/estado";
import { CLASE_PUNTO, tituloVisible, formatearNumero, iniciales, textoVentana, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import type { Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion, ConversacionDetalle, Mensaje } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { ErrorLectura } from "@/componentes/error-lectura";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { AccionesConversacion } from "./acciones";
import { AvisoAtencion, pideAtencion } from "./atencion";
import type { OyenteFeed } from "./bandeja";
import { BarraCategoria } from "./categoria";
import { Compositor } from "./compositor";
import { PildoraCanal } from "./filtros";
import { FichaCliente, type PestanaFicha } from "./ficha";
import { Hilo } from "./hilo";
import { useAhora, useAncho } from "./reloj";
import { BotonSugerir, PausaAsistente, TarjetaSugerencia, useAsistente } from "./sugerencia";
import { IndicadorVerificacion } from "./verificacion";
import { BotonLlamada } from "../voz/llamar";
import { faltaParaVencer, nivelVerificacion, trasFallarRelectura } from "@/lib/asistente/fase3b";

/** Cada cuánto se relee una conversación con la identidad verificada, por si ya venció. */
const RELEER_IDENTIDAD_MS = 30_000;
/** Tiempo límite de cada relectura: pasada esa espera se corta y se prueba en el siguiente tic. */
const LIMITE_RELECTURA_MS = 10_000;
/** Margen tras `verificacionVenceEn`, por si el reloj del navegador va unos milisegundos adelantado. */
const MARGEN_VENCIMIENTO_MS = 500;

type Props = {
    id: number;
    identidad: Identidad;
    suscribir: (o: OyenteFeed) => () => void;
    alActualizar: (c: Conversacion) => void;
    hrefLista: string;
    detenido: "sin_sesion" | "sin_permiso" | "sesion_cambiada" | null;
};

/** Ancho a partir del cual la ficha va al lado (si no, en una hoja). */
const ANCHO_FICHA = 1100;
const CLAVE_FICHA = "iris-bandeja-ficha";

function leerFichaVisible(): boolean {
    try {
        return window.localStorage.getItem(CLAVE_FICHA) !== "0";
    } catch {
        return true;
    }
}

/** ¿Hay espacio para la ficha al lado? (ancho de la bandeja, que ya descuenta el menú) */
function useFichaAlLado(): boolean {
    return useSyncExternalStore(
        (avisar) => {
            const el = document.querySelector('[data-slot="bandeja"]');
            if (!el) return () => undefined;
            const obs = new ResizeObserver(avisar);
            obs.observe(el);
            return () => obs.disconnect();
        },
        () => (document.querySelector('[data-slot="bandeja"]')?.clientWidth ?? window.innerWidth) >= ANCHO_FICHA,
        () => true,
    );
}

/** Conversación abierta: cabecera, hilo, compositor y ficha del cliente (al lado o en una hoja). */
export function PanelConversacion({ id, identidad, suscribir, alActualizar, hrefLista, detenido }: Props) {
    const { puede } = useSesion();
    const ahora = useAhora(30_000);
    const detalle = useLectura<ConversacionDetalle>((signal) => leerDetalle(id, signal));
    const conv = detalle.estado === "listo" ? detalle.datos : null;
    useNombreEnMigas(conv?.titulo);
    const reemplazarDetalle = detalle.reemplazar;
    const refrescarDetalle = detalle.refrescar;

    // Mensajes: la última página y las anteriores a pedido; el feed agrega y actualiza.
    const [mensajes, setMensajes] = useState<Mensaje[]>([]);
    // El estado de la lectura lleva su intento: «Reintentar» vuelve a «cargando» sin un efecto.
    const [lecturaMensajes, setEstadoMensajes] = useState<{ intento: number; estado: "listo" | unknown }>({ intento: -1, estado: "listo" });
    const [cursorAnteriores, setCursorAnteriores] = useState<string | null>(null);
    const [cargandoAnteriores, setCargandoAnteriores] = useState(false);
    const [errorAnteriores, setErrorAnteriores] = useState(false);
    const [pendientes, setPendientes] = useState<Pendiente[]>([]);
    const [intento, setIntento] = useState(0);
    // Sube con cada mensaje de sistema de una boleta (registrada, aprobada, corregida…): la ficha las relee.
    const [senalBoletas, setSenalBoletas] = useState(0);

    const estadoMensajes = lecturaMensajes.intento === intento ? lecturaMensajes.estado : "cargando";
    useEffect(() => {
        const control = new AbortController();
        leerMensajes(id, undefined, control.signal).then(
            (p) => {
                if (control.signal.aborted) return;
                setMensajes((m) => fusionarMensajes(m, p.data ?? []));
                setCursorAnteriores(p.nextCursor ?? null);
                setEstadoMensajes({ intento, estado: "listo" });
            },
            (e: unknown) => !control.signal.aborted && setEstadoMensajes({ intento, estado: e ?? new Error("error") }),
        );
        return () => control.abort();
    }, [id, intento]);

    /** Relee la última página sin «cargando» (un envío repetido responde 200 sin el mensaje). */
    async function releerMensajes() {
        try {
            const p = await leerMensajes(id, undefined);
            setMensajes((m) => fusionarMensajes(m, p.data ?? []));
            setPendientes((l) => pendientesSinConfirmar(l, p.data ?? []));
        } catch {
            // Llegará por el flujo.
        }
    }

    async function cargarAnteriores() {
        if (!cursorAnteriores) return;
        setCargandoAnteriores(true);
        setErrorAnteriores(false);
        try {
            const p = await leerMensajes(id, cursorAnteriores);
            setMensajes((m) => fusionarMensajes(m, p.data ?? []));
            setCursorAnteriores(p.nextCursor ?? null);
        } catch {
            setErrorAnteriores(true);
        } finally {
            setCargandoAnteriores(false);
        }
    }

    const aplicarConversacion = useCallback(
        (c: Conversacion) => {
            alActualizar(c);
            if (c.id !== id) return;
            if (detalle.estado === "listo" && detalle.datos.version > c.version) return;
            // El detalle conserva sus vínculos y su resumen hasta releerlo.
            if (detalle.estado === "listo") reemplazarDetalle({ ...detalle.datos, ...c });
        },
        [alActualizar, detalle, id, reemplazarDetalle],
    );
    const aplicarRef = useRef(aplicarConversacion);
    useEffect(() => {
        aplicarRef.current = aplicarConversacion;
    });

    // §17.8.2: «identidad verificada» vence sola (`verificacionFuerteMin`) y el servidor no avisa:
    // mientras el nivel sea `fuerte` se relee el detalle con el reloj de 30 s, para que el
    // indicador baje a «su número coincide» cuando toca. (Con `verificacionVenceEn` bastará un temporizador.)
    // La relectura es de fondo: si falla (red, 5xx) lo que hay en pantalla se queda y se prueba en el
    // siguiente tic. Sin sesión o sin permiso (401, 403) se detiene y el indicador baja a «su número
    // coincide»: sin poder leer, «verificada» no se queda fija. Con la pestaña oculta no se pide nada
    // y al volver a verse se relee enseguida. Cada petición tiene su tiempo límite, para que una
    // colgada no frene las siguientes.
    // Con `contacto.verificacionVenceEn`, además, a esa hora el indicador baja en el acto (bajar es
    // lo seguro) y se relee; el reloj de 30 s queda de respaldo.
    const identidadVigente = detalle.estado === "listo" && nivelVerificacion(detalle.datos.contacto) === "fuerte";
    const venceEn = detalle.estado === "listo" && identidadVigente ? (detalle.datos.contacto.verificacionVenceEn ?? null) : null;
    const vista = useRef<ConversacionDetalle | null>(null);
    useEffect(() => {
        vista.current = detalle.estado === "listo" ? detalle.datos : null;
    });
    useEffect(() => {
        if (!identidadVigente) return;
        const control = new AbortController();
        let ocupado = false;
        let reloj: ReturnType<typeof setInterval> | undefined = undefined;
        const releer = () => {
            if (ocupado || control.signal.aborted || document.visibilityState === "hidden") return;
            ocupado = true;
            // Tiempo límite propio de esta petición (con `setTimeout`, como `AbortSignal.timeout`).
            const peticion = new AbortController();
            const cortar = () => peticion.abort();
            control.signal.addEventListener("abort", cortar);
            const limite = setTimeout(cortar, LIMITE_RELECTURA_MS);
            leerDetalle(id, peticion.signal)
                .then(
                    (datos) => {
                        // Una respuesta más vieja que lo que ya trajo una acción o el feed no pisa nada.
                        if (!control.signal.aborted && datos.version >= (vista.current?.version ?? 0)) reemplazarDetalle(datos);
                    },
                    (error: unknown) => {
                        if (control.signal.aborted || trasFallarRelectura(error) !== "detener") return;
                        clearInterval(reloj);
                        const actual = vista.current;
                        if (actual?.contacto.verificacion === "fuerte") reemplazarDetalle({ ...actual, contacto: { ...actual.contacto, verificacion: "numero", verificacionVenceEn: null } });
                    },
                )
                .finally(() => {
                    clearTimeout(limite);
                    control.signal.removeEventListener("abort", cortar);
                    ocupado = false;
                });
        };
        reloj = setInterval(releer, RELEER_IDENTIDAD_MS);
        const alVerse = () => {
            if (document.visibilityState === "visible") releer();
        };
        document.addEventListener("visibilitychange", alVerse);
        const falta = faltaParaVencer({ verificacion: "fuerte", verificacionVenceEn: venceEn }, Date.now());
        const vencimiento =
            falta === null
                ? undefined
                : setTimeout(() => {
                      const actual = vista.current;
                      // Solo si sigue siendo la misma verificación (otra más nueva trae otra fecha).
                      if (!actual || actual.contacto.verificacion !== "fuerte" || actual.contacto.verificacionVenceEn !== venceEn) return;
                      reemplazarDetalle({ ...actual, contacto: { ...actual.contacto, verificacion: "numero", verificacionVenceEn: null } });
                      // Y se pide lo que diga el servidor (este efecto ya terminó: la petición va por su cuenta).
                      // Si su reloj va atrás y aún dice «fuerte» con la misma fecha, se queda lo de aquí.
                      leerDetalle(id).then(
                          (datos) => {
                              const ahora = vista.current;
                              const mismaVerificacion = datos.contacto.verificacion === "fuerte" && datos.contacto.verificacionVenceEn === venceEn;
                              if (ahora && ahora.id === datos.id && datos.version >= ahora.version && !mismaVerificacion) reemplazarDetalle(datos);
                          },
                          () => undefined,
                      );
                  }, falta + MARGEN_VENCIMIENTO_MS);
        return () => {
            clearInterval(reloj);
            clearTimeout(vencimiento);
            document.removeEventListener("visibilitychange", alVerse);
            control.abort();
        };
    }, [identidadVigente, venceEn, id, reemplazarDetalle]);

    useEffect(
        () =>
            suscribir((s) => {
                if (s.tipo === "recargar") {
                    void refrescarDetalle();
                    setIntento((n) => n + 1);
                    return;
                }
                const e = s.evento;
                if (e.conversacionId !== id) return;
                if (e.mensaje) {
                    const m = e.mensaje;
                    setMensajes((actual) => fusionarMensajes(actual, [m]));
                    setPendientes((p) => p.filter((x) => x.id !== m.id));
                    // Un evento de sistema puede traer un vínculo nuevo (ticket, orden…).
                    if (m.direccion === "sistema") void refrescarDetalle();
                    if (m.direccion === "sistema" && (m.evento?.tipo ?? "").startsWith("boleta_")) setSenalBoletas((n) => n + 1);
                }
                if (e.conversacion && e.conversacion.id === id) aplicarRef.current(e.conversacion);
            }),
        [id, suscribir, refrescarDetalle],
    );

    // Leer: avanza la lectura hasta el último mensaje del cliente que se ve (nunca retrocede).
    const ultimoEntrante = [...mensajes].reverse().find((m) => m.direccion === "entrante")?.id;
    const leido = useRef<string | null>(null);
    useEffect(() => {
        if (!ultimoEntrante || leido.current === ultimoEntrante || !conv || conv.noLeidos === 0 || detenido) return;
        if (typeof document !== "undefined" && document.visibilityState !== "visible") return;
        leido.current = ultimoEntrante;
        void api()
            .POST("/api/v1/mensajeria/conversaciones/{id}/leer", { params: { path: { id } }, body: { hastaMensajeId: ultimoEntrante } })
            .then(({ data }) => {
                if (data && conv) aplicarRef.current({ ...conv, noLeidos: data.noLeidos });
            })
            .catch(() => {
                leido.current = null;
            });
    }, [ultimoEntrante, conv, id, detenido]);

    // Ficha: al lado con espacio (se puede ocultar y se recuerda); si no, en una hoja.
    const alLado = useFichaAlLado();
    const [fichaVisible, setFichaVisible] = useState(leerFichaVisible);
    const [hojaAbierta, setHojaAbierta] = useState(false);
    const [pestana, setPestana] = useState<PestanaFicha>("resumen");
    function alternarFicha() {
        if (!alLado) {
            setHojaAbierta(true);
            return;
        }
        setFichaVisible((v) => {
            try {
                window.localStorage.setItem(CLAVE_FICHA, v ? "0" : "1");
            } catch {
                // Sin almacenamiento: dura lo que la página.
            }
            return !v;
        });
    }
    function abrirAyuda() {
        setPestana("ayuda");
        if (alLado) setFichaVisible(true);
        else setHojaAbierta(true);
    }

    const [refSeccion, anchoSeccion] = useAncho<HTMLElement>();
    const visiblesPendientes = pendientesSinConfirmar(pendientes, mensajes);
    // Puentes hacia el compositor: reintentar un pendiente desde el hilo y usar un manual de la ficha.
    const reintentar = useRef<((p: Pendiente) => void) | null>(null);
    const usarTexto = useRef<((t: string) => void) | null>(null);
    const usarSugerencia = useRef<((t: string, id: string) => void) | null>(null);
    const responde = puede(["conversaciones.responder"]) && !detenido && conv !== null && !conv.soloLectura;
    const asistente = useAsistente({ id }, suscribir, responde);

    if (detalle.estado === "error") {
        return (
            <section aria-label="Conversación" className="min-h-0 overflow-y-auto p-4 @min-[1100px]/bandeja:col-span-2">
                <Boton asChild variante="fantasma" tamano="sm" className="mb-3 @min-[700px]/bandeja:hidden">
                    <Link href={hrefLista} scroll={false}>
                        <ArrowLeft />
                        Conversaciones
                    </Link>
                </Boton>
                <ErrorLectura error={detalle.error} reintentar={detalle.reintentar} que="la conversación" />
            </section>
        );
    }
    if (!conv) {
        return (
            <section aria-label="Conversación" aria-busy="true" className="flex min-h-0 flex-col gap-3 p-4 @min-[1100px]/bandeja:col-span-2">
                <span className="sr-only" role="status">
                    Cargando la conversación…
                </span>
                <Esqueleto className="h-10 w-1/2" />
                <Esqueleto className="h-16 w-2/3" />
                <Esqueleto className="ml-auto h-16 w-2/3" />
            </section>
        );
    }

    const tono = tonoDeCanal(conv.canal);
    const conVerificacion = conv.contacto.tipo === "cliente" && nivelVerificacion(conv.contacto) !== undefined;
    const ficha = (
        <FichaCliente
            conv={conv}
            identidad={identidad}
            pestana={pestana}
            alCambiarPestana={setPestana}
            alActualizar={(c) => {
                aplicarConversacion(c);
                void refrescarDetalle();
            }}
            recargar={refrescarDetalle}
            alUsarTexto={(t) => usarTexto.current?.(t)}
            asistente={asistente.estado}
            senalBoletas={senalBoletas}
        />
    );
    return (
        <>
            <section ref={refSeccion} aria-label={`Conversación con ${conv.titulo}`} className="@container/conv relative flex min-h-0 min-w-0 flex-col">
                <header className="flex h-[3.75rem] shrink-0 items-center gap-2 border-b border-border bg-card px-3 @min-[700px]/conv:gap-2.5 @min-[700px]/conv:px-3.5">
                    <Boton asChild variante="fantasma" tamano="icono-sm" className="@min-[700px]/bandeja:hidden">
                        <Link href={hrefLista} scroll={false} aria-label="Volver a las conversaciones" title="Volver a las conversaciones">
                            <ArrowLeft />
                        </Link>
                    </Boton>
                    <span aria-hidden className="relative flex size-9 shrink-0 items-center justify-center rounded-full bg-elevated text-[0.8125rem] font-semibold">
                        {iniciales(conv.titulo)}
                        <span className={cn("absolute -right-0.5 -bottom-0.5 size-3.5 rounded-full ring-2 ring-card", CLASE_PUNTO[tono])} />
                    </span>
                    <div className="flex min-w-0 flex-1 flex-col">
                        <h2 className="truncate text-[0.9375rem] font-semibold">{tituloVisible(conv)}</h2>
                        <span className="flex min-w-0 items-center gap-1.5 text-[0.78125rem] text-muted-foreground">
                            <PildoraCanal canal={conv.canal} className="shrink-0" />
                            {/* En el teléfono no caben los dos: con el nivel de verificación a la vista, el número queda en la ficha. */}
                            <span className={cn("truncate tabular-nums", conVerificacion && "hidden @min-[700px]/conv:inline")}>{formatearNumero(conv.numero)}</span>
                            <IndicadorVerificacion contacto={conv.contacto} />
                        </span>
                    </div>
                    <AccionesConversacion conv={conv} estrecho={anchoSeccion > 0 && anchoSeccion < 480} alActualizar={aplicarConversacion} recargar={refrescarDetalle} abrirAyuda={abrirAyuda} />
                    {detenido ? null : <BotonLlamada conv={{ id: conv.id, titulo: conv.titulo }} />}
                    <Boton
                        variante="contorno"
                        tamano="icono-sm"
                        aria-label={alLado && fichaVisible ? "Ocultar la ficha del cliente" : "Ver la ficha del cliente"}
                        title={alLado && fichaVisible ? "Ocultar la ficha del cliente" : "Ver la ficha del cliente"}
                        aria-expanded={alLado ? fichaVisible : hojaAbierta}
                        onClick={alternarFicha}
                        className="shrink-0"
                    >
                        <PanelRight />
                    </Boton>
                </header>
                <BarraCategoria conv={conv} alActualizar={aplicarConversacion} recargar={refrescarDetalle} bloqueada={detenido !== null} asistenteActivo={asistente.estado?.activo === true} />
                {pideAtencion(conv) && !detenido ? (
                    <div className="shrink-0 px-3 pt-3 @min-[700px]/conv:px-4">
                        <AvisoAtencion conv={conv} ahora={ahora} alHecho={aplicarConversacion} />
                    </div>
                ) : null}
                {detenido ? (
                    <Aviso
                        tono="warning"
                        titulo={detenido === "sesion_cambiada" ? "La sesión de este navegador cambió" : "Tu sesión se cerró"}
                        className="m-3 mb-0"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={() => window.location.reload()}>
                                <RotateCw />
                                Recargar
                            </Boton>
                        }
                    >
                        No se enviará nada desde esta pestaña hasta recargar.
                    </Aviso>
                ) : null}
                {estadoMensajes === "cargando" ? (
                    <div className="flex flex-1 flex-col gap-2 p-4" aria-busy="true">
                        <Esqueleto className="h-14 w-2/3" />
                        <Esqueleto className="ml-auto h-14 w-2/3" />
                    </div>
                ) : estadoMensajes !== "listo" ? (
                    <div className="flex-1 p-4">
                        <ErrorLectura error={estadoMensajes} reintentar={() => setIntento((n) => n + 1)} que="los mensajes" />
                    </div>
                ) : (
                    <Hilo
                        mensajes={mensajes}
                        pendientes={visiblesPendientes}
                        ahora={ahora}
                        hayAnteriores={cursorAnteriores !== null}
                        cargandoAnteriores={cargandoAnteriores}
                        errorAnteriores={errorAnteriores}
                        cargarAnteriores={cargarAnteriores}
                        reintentarPendiente={(p) => reintentar.current?.(p)}
                    />
                )}
                {responde && textoVentana(conv.ventana, ahora) !== null ? (
                    <>
                        <PausaAsistente asistente={asistente} />
                        <TarjetaSugerencia key={asistente.sugerencia?.id ?? "sin-sugerencia"} conv={conv} asistente={asistente} alUsar={(t, sid) => usarSugerencia.current?.(t, sid)} alActualizar={aplicarConversacion} />
                    </>
                ) : null}
                {responde ? (
                    <Compositor
                        conv={conv}
                        identidad={identidad}
                        ahora={ahora}
                        alPendiente={(p, reemplaza) => setPendientes((l) => [...l.filter((x) => x.id !== p.id && x.id !== reemplaza), p])}
                        alConfirmado={(m) => {
                            setMensajes((l) => fusionarMensajes(l, [m]));
                            setPendientes((l) => l.filter((x) => x.id !== m.id));
                        }}
                        alReenvio={() => void releerMensajes()}
                        alActualizar={aplicarConversacion}
                        recargar={refrescarDetalle}
                        abrirAyuda={abrirAyuda}
                        registrarReintento={(f) => (reintentar.current = f)}
                        registrarUsarTexto={(f) => (usarTexto.current = f)}
                        registrarUsarSugerencia={(f) => (usarSugerencia.current = f)}
                        accesorio={<BotonSugerir asistente={asistente} />}
                    />
                ) : (
                    <p className="border-t border-border bg-card px-4 py-3 text-sm text-muted-foreground">
                        {detenido
                            ? "Recarga la página para seguir."
                            : conv.soloLectura
                              ? "Solo lectura: esta conversación pasó a otra empresa o el número ya no se comparte con la tuya. Si el cliente vuelve a escribir, llega a la empresa dueña del número."
                              : "Puedes ver esta conversación, pero no responderla."}
                    </p>
                )}
            </section>
            {alLado ? (
                fichaVisible ? (
                    <aside aria-label="Ficha del cliente" className="hidden min-h-0 w-[18.75rem] flex-col overflow-y-auto border-l border-border bg-card @min-[1100px]/bandeja:flex">
                        {ficha}
                    </aside>
                ) : null
            ) : (
                <Hoja open={hojaAbierta} onOpenChange={setHojaAbierta}>
                    <HojaContenido titulo="Ficha del cliente" lado="derecha" etiquetaCerrar="Cerrar la ficha" className="w-[min(22rem,92vw)] p-0 sm:w-[min(22rem,92vw)] [&>div:first-child]:px-4 [&>div:first-child]:pt-4">
                        {ficha}
                    </HojaContenido>
                </Hoja>
            )}
        </>
    );
}
