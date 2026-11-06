"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { AreaTexto, Aviso, Boton, CampoBusqueda, Casilla, cn, GrupoSegmentado, PanelEmergente, SelectorArchivo } from "@iris/ui";
import { AtSign, BookOpen, Clock, Send, Smile, X, Zap } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { ACEPTA_ADJUNTO, motivoArchivoInvalido, subirAdjunto } from "@/lib/conversaciones/adjuntar";
import { cabeceraIdem, nuevoIdMensaje } from "@/lib/conversaciones/api";
import { guardarBorrador, leerBorrador } from "@/lib/conversaciones/borradores";
import type { Pendiente } from "@/lib/conversaciones/estado";
import { aplicarRespuesta, textoVentana } from "@/lib/conversaciones/presentacion";
import { esSesionCambiada, verificarSesion, type Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion, Mensaje, RespuestaRapida } from "@/lib/conversaciones/tipos";
import { useSesion } from "@/lib/sesion";
import { agentesDeCola, useCatalogos } from "./catalogos";
import { SelectorPlantilla } from "./plantillas";

type Modo = "responder" | "nota";

/** Espejo del validador del servidor: texto plano de WhatsApp (≤ 4096). */
export const esquemaMensaje = z.object({ texto: z.string().max(4096, "Hasta 4096 caracteres (límite de WhatsApp).") });

type Props = {
    conv: Conversacion;
    identidad: Identidad;
    ahora: number;
    alPendiente: (p: Pendiente) => void;
    alConfirmado: (m: Mensaje) => void;
    alActualizar: (c: Conversacion) => void;
    recargar: () => Promise<void>;
    abrirAyuda: () => void;
    registrarReintento: (f: (p: Pendiente) => void) => void;
    registrarUsarTexto: (f: (t: string) => void) => void;
};

const EMOJIS = ["👍", "🙏", "😊", "🙂", "👋", "✅", "📶", "🔧", "📅", "⏰", "📍", "💳", "🧾", "⚠️", "❗", "🎉"];

type Envio = { id: string; modo: Modo; texto: string; menciones: number[]; respuestaRapidaId: number | null; adjuntoId: string | null };

/**
 * Compositor único (diseño del PM): pestañas Responder / Nota interna, tiempo que queda de la
 * ventana de 24 h, el texto, la barra de íconos (adjuntar, plantillas, respuestas rápidas, emoji)
 * y «Enviar» en tinta. Fuera de la ventana, al cliente solo se le puede mandar una plantilla
 * aprobada en ese número (lo dice en una línea). Enter envía; Mayús + Enter baja de línea.
 *
 * Antes de guardar un borrador o de enviar, se comprueba con `GET /yo` que la sesión del navegador
 * sigue siendo la de quien está en pantalla: si cambió, nada sale (ver `lib/conversaciones/sesion`).
 */
export function Compositor({ conv, identidad, ahora, alPendiente, alConfirmado, alActualizar, recargar, abrirAyuda, registrarReintento, registrarUsarTexto }: Props) {
    const { puede } = useSesion();
    const { respuestas, agentes } = useCatalogos();
    const [modo, setModo] = useState<Modo>("responder");
    const form = useForm({ resolver: zodResolver(esquemaMensaje), defaultValues: { texto: "" } });
    const texto = useWatch({ control: form.control, name: "texto" });
    const [respuestaId, setRespuestaId] = useState<number | null>(null);
    const [menciones, setMenciones] = useState<number[]>([]);
    const [error, setError] = useState<string | null>(null);
    const [subiendo, setSubiendo] = useState<string | null>(null);
    const campo = useRef<HTMLTextAreaElement | null>(null);
    const ventana = textoVentana(conv.ventana, ahora);
    const cerrada = ventana === null;

    // Borradores: solo con la sesión confirmada para esta persona y empresa.
    const [verificada, setVerificada] = useState(false);
    useEffect(() => {
        let vivo = true;
        verificarSesion(identidad).then(
            () => {
                if (!vivo) return;
                setVerificada(true);
                const b = leerBorrador(identidad, conv.id, modo);
                if (b && !form.getValues("texto")) form.reset({ texto: b });
            },
            () => undefined,
        );
        return () => {
            vivo = false;
        };
        // Al abrir la conversación y al cambiar de pestaña.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [identidad, conv.id, modo]);
    useEffect(() => {
        if (!verificada) return;
        const t = setTimeout(() => guardarBorrador(identidad, conv.id, modo, texto), 400);
        return () => clearTimeout(t);
    }, [verificada, identidad, conv.id, modo, texto]);

    async function mandar(e: Envio): Promise<void> {
        const pendiente: Pendiente = { id: e.id, modo: e.modo, texto: e.texto || (e.adjuntoId ? "Archivo adjunto" : ""), creadoEn: new Date().toISOString(), error: null };
        alPendiente(pendiente);
        try {
            await verificarSesion(identidad);
            const m =
                e.modo === "nota"
                    ? await resolver(
                          api().POST("/api/v1/mensajeria/conversaciones/{id}/notas", {
                              params: { path: { id: conv.id }, header: cabeceraIdem(e.id) },
                              body: { id: e.id, texto: e.texto, menciones: e.menciones },
                          }),
                      )
                    : await resolver(
                          api().POST("/api/v1/mensajeria/conversaciones/{id}/mensajes", {
                              params: { path: { id: conv.id }, header: cabeceraIdem(e.id) },
                              body: {
                                  id: e.id,
                                  ...(e.texto ? { texto: e.texto } : {}),
                                  ...(e.adjuntoId ? { adjuntoId: e.adjuntoId } : {}),
                                  ...(e.respuestaRapidaId ? { respuestaRapidaId: e.respuestaRapidaId } : {}),
                              },
                          }),
                      );
            alConfirmado(m);
        } catch (err) {
            const texto = esSesionCambiada(err)
                ? "La sesión de este navegador cambió: no se envió"
                : err instanceof ErrorApi && err.codigo === "fuera_de_ventana"
                  ? "La ventana de 24 h se cerró: usa una plantilla"
                  : `No se envió: ${mensajeDeError(err).titulo}`;
            alPendiente({ ...pendiente, error: texto });
            if (err instanceof ErrorApi && (err.codigo === "fuera_de_ventana" || err.estado === 409)) void recargar();
        }
    }

    // El hilo reintenta un pendiente con el mismo id (el servidor no lo duplica).
    const envios = useRef(new Map<string, Envio>());
    useEffect(() => {
        registrarReintento((p) => {
            const e = envios.current.get(p.id);
            if (e) void mandar(e);
        });
        registrarUsarTexto((t) => {
            setModo("responder");
            const actual = form.getValues("texto");
            form.setValue("texto", actual ? `${actual}\n\n${t}` : t, { shouldDirty: true });
            campo.current?.focus();
        });
    });

    function enviar() {
        const limpio = form.getValues("texto").trim();
        if (!limpio) return;
        if (limpio.length > 4096) {
            form.setError("texto", { message: "Hasta 4096 caracteres (límite de WhatsApp)." });
            return;
        }
        const e: Envio = { id: nuevoIdMensaje(), modo, texto: limpio, menciones: modo === "nota" ? menciones : [], respuestaRapidaId: modo === "responder" ? respuestaId : null, adjuntoId: null };
        envios.current.set(e.id, e);
        form.reset({ texto: "" });
        guardarBorrador(identidad, conv.id, modo, "");
        setRespuestaId(null);
        setMenciones([]);
        setError(null);
        void mandar(e);
    }

    async function adjuntar(archivo: File) {
        const motivo = motivoArchivoInvalido(archivo);
        if (motivo) {
            setError(motivo);
            return;
        }
        setError(null);
        setSubiendo(archivo.name);
        try {
            await verificarSesion(identidad);
            const adjuntoId = await subirAdjunto(archivo, conv.id);
            const pie = form.getValues("texto").trim();
            const e: Envio = { id: nuevoIdMensaje(), modo: "responder", texto: pie, menciones: [], respuestaRapidaId: null, adjuntoId };
            envios.current.set(e.id, e);
            form.reset({ texto: "" });
            void mandar(e);
        } catch (err) {
            setError(esSesionCambiada(err) ? "La sesión de este navegador cambió: no se subió el archivo." : `No se pudo subir «${archivo.name}»: ${mensajeDeError(err).titulo}`);
        } finally {
            setSubiendo(null);
        }
    }

    function alTeclear(e: KeyboardEvent<HTMLTextAreaElement>) {
        if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            enviar();
        }
    }

    function usarRespuesta(r: RespuestaRapida) {
        const t = aplicarRespuesta(r.texto, conv.titulo);
        const actual = form.getValues("texto").trim();
        form.setValue("texto", actual ? `${actual} ${t}` : t, { shouldDirty: true });
        setRespuestaId(r.id);
        campo.current?.focus();
    }

    const deLaCola = useMemo(
        () => (respuestas.estado === "listo" && respuestas.datos ? respuestas.datos.filter((r) => r.colaId === null || r.colaId === conv.cola?.id) : []),
        [respuestas, conv.cola?.id],
    );
    const candidatos = agentes.estado === "listo" ? agentesDeCola(agentes.datos, conv.cola?.id).filter((a) => a.id !== identidad.usuarioId) : [];
    const soloPlantilla = modo === "responder" && cerrada;
    const { ref: refCampo, ...registro } = form.register("texto");

    return (
        <div className="shrink-0 px-3 pt-2 pb-3 @min-[700px]/conv:px-3.5">
            {modo === "responder" && !cerrada && deLaCola.length > 0 ? (
                <div
                    role="group"
                    aria-label="Respuestas rápidas"
                    className="flex gap-1.5 overflow-x-auto pb-2 [mask-image:linear-gradient(90deg,#000_85%,transparent)] [scrollbar-width:none]"
                >
                    {deLaCola.slice(0, 8).map((r) => (
                        <Boton key={r.id} variante="secundario" tamano="sm" className="h-7 shrink-0 rounded-full px-2.5 text-xs font-medium" onClick={() => usarRespuesta(r)} title={r.texto}>
                            {r.titulo}
                        </Boton>
                    ))}
                </div>
            ) : null}
            <div className={cn("overflow-hidden rounded-[0.875rem] border bg-card shadow-xs", modo === "nota" ? "border-warning/50 bg-warning/5" : "border-border")}>
                <div className="flex min-w-0 items-center gap-1 px-2 pt-1.5">
                    <GrupoSegmentado
                        etiqueta="Tipo de mensaje"
                        className="bg-transparent"
                        valor={modo}
                        alCambiar={(v) => {
                            setModo(v);
                            form.clearErrors();
                        }}
                        opciones={[
                            { valor: "responder", etiqueta: "Responder" },
                            { valor: "nota", etiqueta: "Nota interna" },
                        ]}
                    />
                    <span className="ml-auto flex min-w-0 items-center gap-1 text-xs text-muted-foreground" data-ventana={cerrada ? "cerrada" : "abierta"}>
                        <Clock aria-hidden className="size-3.5 shrink-0" />
                        <span className="truncate @max-[30rem]/conv:hidden">{ventana ? ventana.largo : "Ventana de 24 h cerrada"}</span>
                        <span className="hidden whitespace-nowrap @max-[30rem]/conv:inline">{ventana ? ventana.corto : "Cerrada"}</span>
                    </span>
                </div>
                {soloPlantilla ? (
                    <div className="grid gap-2 px-3 pt-2 pb-2.5">
                        <p className="text-sm text-muted-foreground">El cliente no escribe hace más de 24 h: WhatsApp solo deja mandarle una plantilla aprobada en este número.</p>
                        <SelectorPlantilla conv={conv} identidad={identidad} alEnviado={alConfirmado} alActualizar={alActualizar} principal />
                    </div>
                ) : (
                    <>
                        {modo === "nota" && menciones.length > 0 ? (
                            <div className="flex flex-wrap gap-1 px-3 pt-2">
                                {menciones.map((id) => {
                                    const a = candidatos.find((x) => x.id === id);
                                    return (
                                        <span key={id} className="inline-flex h-6 max-w-full items-center gap-1 rounded-full bg-elevated pr-0.5 pl-2 text-xs">
                                            <span className="truncate">@{a?.nombre ?? id}</span>
                                            <Boton variante="fantasma" tamano="icono-sm" className="size-5" aria-label={`Quitar a ${a?.nombre ?? id}`} onClick={() => setMenciones((m) => m.filter((x) => x !== id))}>
                                                <X />
                                            </Boton>
                                        </span>
                                    );
                                })}
                            </div>
                        ) : null}
                        <label className="sr-only" htmlFor={`compositor-${conv.id}`}>
                            {modo === "nota" ? "Nota interna (solo la ve el equipo)" : `Mensaje para ${conv.titulo}`}
                        </label>
                        <AreaTexto
                            id={`compositor-${conv.id}`}
                            rows={2}
                            maxLength={4096}
                            placeholder={modo === "nota" ? "Nota para el equipo; el cliente no la ve" : "Escribe un mensaje"}
                            className="block max-h-40 min-h-11 w-full resize-none rounded-none border-0 bg-transparent px-3 pt-2 pb-1 text-sm leading-relaxed shadow-none outline-none [field-sizing:content] focus:ring-0 focus-visible:ring-0 sm:text-sm"
                            onKeyDown={alTeclear}
                            aria-invalid={form.formState.errors.texto ? true : undefined}
                            {...registro}
                            ref={(el) => {
                                refCampo(el);
                                campo.current = el;
                            }}
                        />
                    </>
                )}
                {form.formState.errors.texto?.message || error ? (
                    <p role="alert" className="px-3 pb-1 text-xs text-error">
                        {form.formState.errors.texto?.message ?? error}
                    </p>
                ) : null}
                <div className="flex min-w-0 items-center gap-0.5 px-1.5 pt-0.5 pb-1.5">
                    {modo === "responder" ? (
                        <>
                            <SelectorArchivo etiqueta={subiendo ? `Subiendo ${subiendo}…` : "Adjuntar foto o PDF"} accept={ACEPTA_ADJUNTO} alElegir={adjuntar} disabled={cerrada || subiendo !== null} className="text-muted-foreground" />
                            {puede(["manuales.ver"]) ? (
                                <Boton variante="fantasma" tamano="icono-sm" className="text-muted-foreground" aria-label="Manuales" title="Manuales" onClick={abrirAyuda}>
                                    <BookOpen />
                                </Boton>
                            ) : null}
                            {soloPlantilla ? null : <SelectorPlantilla conv={conv} identidad={identidad} alEnviado={alConfirmado} alActualizar={alActualizar} />}
                            {cerrada ? null : <MenuRespuestas respuestas={deLaCola} alElegir={usarRespuesta} />}
                        </>
                    ) : (
                        <MenuMenciones candidatos={candidatos} elegidos={menciones} alCambiar={setMenciones} />
                    )}
                    {soloPlantilla ? null : <MenuEmoji alElegir={(e) => form.setValue("texto", `${form.getValues("texto")}${e}`, { shouldDirty: true })} />}
                    {soloPlantilla ? null : (
                        <Boton variante="tinta" tamano="sm" className="ml-auto h-8 rounded-lg px-3" onClick={enviar} disabled={!texto.trim()} title="Enviar (Enter)">
                            {modo === "nota" ? "Guardar nota" : "Enviar"}
                            <Send />
                        </Boton>
                    )}
                </div>
            </div>
            {subiendo ? (
                <Aviso tono="info" titulo={`Subiendo «${subiendo}»…`} className="mt-2">
                    Se enviará al terminar.
                </Aviso>
            ) : null}
        </div>
    );
}

function MenuRespuestas({ respuestas, alElegir }: { respuestas: readonly RespuestaRapida[]; alElegir: (r: RespuestaRapida) => void }) {
    const [abierto, setAbierto] = useState(false);
    const [q, setQ] = useState("");
    const lista = respuestas.filter((r) => `${r.atajo} ${r.titulo} ${r.texto}`.toLowerCase().includes(q.trim().toLowerCase()));
    return (
        <PanelEmergente
            titulo="Respuestas rápidas"
            tituloVisible
            abierto={abierto}
            alCambiar={setAbierto}
            lado="top start"
            className="w-[min(20rem,calc(100vw-2rem))]"
            disparador={
                <Boton variante="fantasma" tamano="icono-sm" className="text-muted-foreground" aria-label="Respuestas rápidas" title="Respuestas rápidas">
                    <Zap />
                </Boton>
            }
        >
            <div className="grid gap-2">
                <CampoBusqueda etiqueta="Buscar respuestas" etiquetaVisible={false} tamano="sm" placeholder="Buscar o escribir /atajo" valor={q} alCambiar={setQ} autoFocus />
                <ul className="grid max-h-64 gap-0.5 overflow-y-auto">
                    {lista.length === 0 ? <li className="px-2 py-1.5 text-sm text-muted-foreground">Sin respuestas rápidas.</li> : null}
                    {lista.map((r) => (
                        <li key={r.id}>
                            <Boton
                                variante="fantasma"
                                className="h-auto w-full flex-col items-start gap-0 px-2 py-1.5 text-left"
                                onClick={() => {
                                    alElegir(r);
                                    setAbierto(false);
                                }}
                            >
                                <span className="flex w-full min-w-0 items-baseline gap-2">
                                    <span className="truncate text-sm font-semibold">{r.titulo}</span>
                                    <span className="ml-auto shrink-0 font-mono text-xs text-muted-foreground">{r.atajo}</span>
                                </span>
                                <span className="line-clamp-2 w-full text-xs font-normal text-muted-foreground">{r.texto}</span>
                            </Boton>
                        </li>
                    ))}
                </ul>
            </div>
        </PanelEmergente>
    );
}

function MenuMenciones({ candidatos, elegidos, alCambiar }: { candidatos: readonly { id: number; nombre: string; supervisa?: boolean }[]; elegidos: readonly number[]; alCambiar: (ids: number[]) => void }) {
    return (
        <PanelEmergente
            titulo="Mencionar"
            tituloVisible
            lado="top start"
            className="w-[min(18rem,calc(100vw-2rem))]"
            disparador={
                <Boton variante="fantasma" tamano="sm" className="h-8 px-2 text-muted-foreground" title="Mencionar a un compañero">
                    <AtSign />
                    Mencionar
                </Boton>
            }
        >
            <div className="grid max-h-64 gap-0.5 overflow-y-auto">
                {candidatos.length === 0 ? <p className="text-sm text-muted-foreground">No hay compañeros en esta cola.</p> : null}
                {candidatos.map((a) => (
                    <Casilla
                        key={a.id}
                        etiqueta={`${a.nombre}${a.supervisa ? " · supervisa" : ""}`}
                        checked={elegidos.includes(a.id)}
                        onChange={(e) => alCambiar(e.target.checked ? [...elegidos, a.id] : elegidos.filter((x) => x !== a.id))}
                    />
                ))}
            </div>
        </PanelEmergente>
    );
}

function MenuEmoji({ alElegir }: { alElegir: (e: string) => void }) {
    return (
        <PanelEmergente
            titulo="Emoji"
            lado="top start"
            className="w-auto p-1.5"
            disparador={
                <Boton variante="fantasma" tamano="icono-sm" className="text-muted-foreground" aria-label="Emoji" title="Emoji">
                    <Smile />
                </Boton>
            }
        >
            <div className="grid grid-cols-8 gap-0.5">
                {EMOJIS.map((e) => (
                    <Boton key={e} variante="fantasma" tamano="icono-sm" className="text-lg" aria-label={`Insertar ${e}`} onClick={() => alElegir(e)}>
                        {e}
                    </Boton>
                ))}
            </div>
        </PanelEmergente>
    );
}
