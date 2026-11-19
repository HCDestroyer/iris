"use client";

import { AlbumFotos, Boton, Cargando, cn, FotoMensaje, VisorFotos, type FotoEnVisor } from "@iris/ui";
import { AlertTriangle, ArrowDown, ArrowLeftRight, Bot, CirclePause, FileText, History, MapPin, MessageCircle, PencilLine, PhoneCall, Plug, RotateCw, ShieldCheck, ShieldX, Smartphone, Sparkles, Timer, UserRound, Voicemail } from "lucide-react";
import { createContext, Fragment, use, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import type { Pendiente } from "@/lib/conversaciones/estado";
import { hora, separadorDia, tamanoLegible, textoEntrega, textoEvento } from "@/lib/conversaciones/presentacion";
import type { Mensaje } from "@/lib/conversaciones/tipos";
import { esDelAsistente } from "@/lib/asistente/fase3";
import { identidadQuedoVerificada, resultadoIdentidad } from "@/lib/asistente/fase3b";
import { esEventoBot } from "@/lib/conversaciones/atencion";
import { rutaDescargaAdjunto } from "@/lib/ordenes/adjuntos";
import { diaLocal } from "@/lib/conversaciones/presentacion";
import { LlamadaEnHilo } from "../voz/en-hilo";
import { FallaMensaje } from "./falla-mensaje";

type Props = {
    mensajes: readonly Mensaje[];
    pendientes: readonly Pendiente[];
    ahora: number;
    hayAnteriores: boolean;
    cargandoAnteriores: boolean;
    errorAnteriores: boolean;
    cargarAnteriores: () => void;
    reintentarPendiente: (p: Pendiente) => void;
    /** Quién escribe del otro lado (el título de la conversación): para el visor de fotos. */
    nombreCliente?: string;
};

/** Abre una foto del hilo en el visor (por id de mensaje). */
const AbrirFoto = createContext<(mensajeId: string) => void>(() => undefined);

/** A menos de esto del final se considera que estás abajo (y la vista sigue a lo que llega). */
const MARGEN_ABAJO = 80;

/**
 * El hilo: burbujas del cliente a la izquierda, las nuestras a la derecha, notas internas en
 * amarillo punteado y los eventos de sistema centrados en una línea. Todo el texto es plano
 * (`white-space: pre-wrap`, nunca HTML).
 *
 * Abre en el último mensaje y **se queda abajo** mientras estés abajo, pase lo que pase con la
 * altura: mensajes que llegan o envías, una foto que termina de cargar, la sugerencia del
 * asistente o el compositor que crecen, el teclado del teléfono. Si subiste a leer, no salta:
 * aparece «Mensajes nuevos» y al pulsarlo baja. Al cargar mensajes anteriores, lo que estabas
 * leyendo se queda en su sitio.
 */
export function Hilo({ mensajes, pendientes, ahora, hayAnteriores, cargandoAnteriores, errorAnteriores, cargarAnteriores, reintentarPendiente, nombreCliente = "Cliente" }: Props) {
    const caja = useRef<HTMLDivElement>(null);
    const contenido = useRef<HTMLDivElement>(null);
    const abajo = useRef(true);
    const primero = mensajes[0]?.id;
    const ultimo = mensajes.at(-1)?.id;
    const altoAntes = useRef(0);
    const previo = useRef<{ primero: string | undefined; ultimo: string | undefined; cuantos: number; pendientes: number }>({ primero: undefined, ultimo: undefined, cuantos: 0, pendientes: 0 });
    const [nuevos, setNuevos] = useState(0);
    // Visor: todas las fotos listas del hilo, en orden; se abre en la que se tocó.
    const fotos = useMemo(() => fotosDelHilo(mensajes, nombreCliente), [mensajes, nombreCliente]);
    const [visor, setVisor] = useState<string | null>(null);
    const indiceVisor = visor === null ? -1 : fotos.findIndex((f) => f.id === visor);
    const abrirFoto = useCallback((id: string) => setVisor(id), []);

    const irAlFinal = useCallback(() => {
        const el = caja.current;
        if (!el) return;
        el.scrollTop = el.scrollHeight;
        abajo.current = true;
        altoAntes.current = el.scrollHeight;
        setNuevos(0);
    }, []);

    useLayoutEffect(() => {
        const el = caja.current;
        if (!el) return;
        const antes = previo.current;
        // Lo que envías tú te lleva abajo siempre, como en cualquier chat.
        const envie = pendientes.length > antes.pendientes;
        const llegaron = antes.ultimo !== undefined && ultimo !== antes.ultimo && primero === antes.primero ? Math.max(1, mensajes.length - antes.cuantos) : 0;
        if (abajo.current || envie) {
            irAlFinal();
        } else {
            // Al cargar anteriores, la vista no salta: se conserva la distancia al final.
            if (primero !== antes.primero && altoAntes.current) el.scrollTop += el.scrollHeight - altoAntes.current;
            if (llegaron > 0) setNuevos((n) => n + llegaron);
        }
        altoAntes.current = el.scrollHeight;
        previo.current = { primero, ultimo, cuantos: mensajes.length, pendientes: pendientes.length };
    }, [mensajes, pendientes, primero, ultimo, irAlFinal]);

    useEffect(() => {
        const el = caja.current;
        if (!el) return;
        let topeAnterior = el.scrollTop;
        const alDesplazar = () => {
            const cerca = el.scrollHeight - el.scrollTop - el.clientHeight < MARGEN_ABAJO;
            // Solo subir a mano te saca de «abajo». Si la distancia creció porque creció el contenido
            // (una foto que cargó entre que se bajó y llegó este aviso), sigues abajo.
            if (cerca) abajo.current = true;
            else if (el.scrollTop < topeAnterior - 1) abajo.current = false;
            topeAnterior = el.scrollTop;
            altoAntes.current = el.scrollHeight;
            if (abajo.current) setNuevos(0);
        };
        el.addEventListener("scroll", alDesplazar, { passive: true });
        // La altura cambia sin que llegue ningún mensaje: una foto que carga, el compositor o la
        // sugerencia que crecen, el teclado del teléfono. Si estabas abajo, sigues abajo.
        const observador =
            typeof ResizeObserver === "undefined"
                ? null
                : new ResizeObserver(() => {
                      if (abajo.current) el.scrollTop = el.scrollHeight;
                      altoAntes.current = el.scrollHeight;
                  });
        observador?.observe(el);
        if (contenido.current) observador?.observe(contenido.current);
        return () => {
            el.removeEventListener("scroll", alDesplazar);
            observador?.disconnect();
        };
    }, []);

    // Separador de día antes del primer mensaje de cada día (hora de Guatemala).
    const separadores = mensajes.map((m, i) => {
        const anterior = mensajes[i - 1];
        return !anterior || diaLocal(Date.parse(anterior.creadoEn)) !== diaLocal(Date.parse(m.creadoEn)) ? separadorDia(m.creadoEn, ahora) : null;
    });
    return (
        <div className="relative flex min-h-0 flex-1 flex-col">
            <div ref={caja} role="log" aria-label="Mensajes" aria-live="polite" tabIndex={0} className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto outline-none focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-inset">
                <div ref={contenido} className="flex min-w-0 flex-col gap-2 px-[clamp(0.75rem,3cqw,2rem)] py-4">
                    <AbrirFoto value={abrirFoto}>
                    <Mensajes
                        mensajes={mensajes}
                        pendientes={pendientes}
                        separadores={separadores}
                        hayAnteriores={hayAnteriores}
                        cargandoAnteriores={cargandoAnteriores}
                        errorAnteriores={errorAnteriores}
                        cargarAnteriores={cargarAnteriores}
                        reintentarPendiente={reintentarPendiente}
                    />
                    </AbrirFoto>
                </div>
            </div>
            <VisorFotos fotos={fotos} indice={indiceVisor >= 0 ? indiceVisor : null} alCambiar={(i) => setVisor(i === null ? null : (fotos[i]?.id ?? null))} />
            {nuevos > 0 ? (
                <Boton variante="tinta" tamano="sm" data-slot="mensajes-nuevos" className="absolute bottom-3 left-1/2 h-8 max-w-[calc(100%-1.5rem)] -translate-x-1/2 rounded-full px-3 shadow-md" onClick={irAlFinal}>
                    <span className="truncate">Mensajes nuevos</span>
                    {nuevos > 1 ? <span className="tabular-nums opacity-80">{nuevos > 99 ? "99+" : nuevos}</span> : null}
                    <ArrowDown />
                </Boton>
            ) : null}
        </div>
    );
}

type PropsMensajes = Omit<Props, "ahora"> & { separadores: readonly (string | null)[] };

function Mensajes({ mensajes, pendientes, separadores, hayAnteriores, cargandoAnteriores, errorAnteriores, cargarAnteriores, reintentarPendiente }: PropsMensajes) {
    return (
        <>
            {hayAnteriores ? (
                <div className="flex justify-center">
                    <Boton variante="fantasma" tamano="sm" onClick={cargarAnteriores} disabled={cargandoAnteriores}>
                        {cargandoAnteriores ? <Cargando texto="Cargando…" /> : errorAnteriores ? <RotateCw /> : null}
                        {cargandoAnteriores ? null : errorAnteriores ? "No se pudieron leer. Reintentar" : "Ver mensajes anteriores"}
                    </Boton>
                </div>
            ) : null}
            {agruparFotos(mensajes, separadores).map((g) => {
                const primero = g.mensajes[0];
                if (!primero) return null;
                const separador = separadores[g.indice];
                return (
                    <Fragment key={primero.id}>
                        {separador ? <p className="self-center py-1.5 text-[0.71875rem] font-semibold tracking-wide text-muted-foreground uppercase">{separador}</p> : null}
                        {g.mensajes.length > 1 ? <Album ms={g.mensajes} /> : <Burbuja m={primero} />}
                    </Fragment>
                );
            })}
            {pendientes.map((p) => (
                <div
                    key={p.id}
                    data-pendiente={p.id}
                    className={cn(
                        "flex max-w-[min(72%,35rem)] flex-col gap-0.5 self-end rounded-[0.875rem] rounded-br-sm px-3 py-2",
                        p.modo === "nota" ? "border border-dashed border-warning/50 bg-warning/10" : "bg-primary/10",
                    )}
                >
                    {p.modo === "nota" ? <EtiquetaNota /> : null}
                    <p className="text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{p.texto}</p>
                    {p.error ? (
                        <span className="flex flex-wrap items-center justify-end gap-x-2 text-xs text-error">
                            <AlertTriangle aria-hidden className="size-3.5" />
                            {p.error}
                            {p.definitivo ? null : (
                                <Boton variante="enlace" className="text-xs" onClick={() => reintentarPendiente(p)}>
                                    Reintentar
                                </Boton>
                            )}
                        </span>
                    ) : (
                        <span className="self-end text-[0.71875rem] text-muted-foreground">Enviando…</span>
                    )}
                </div>
            ))}
        </>
    );
}

function EtiquetaNota({ delAsistente = false }: { delAsistente?: boolean }) {
    return (
        <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-warning">
            <PencilLine aria-hidden className="size-3.5" />
            {delAsistente ? "Nota interna del asistente" : "Nota interna"}
        </span>
    );
}

/** Fase 3: lo que escribió el asistente de IA (no una persona), en teal con `Sparkles`. */
function EtiquetaAsistente() {
    return (
        <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-primary" data-slot="etiqueta-asistente">
            <Sparkles aria-hidden className="size-3.5" />
            Asistente
        </span>
    );
}

function Burbuja({ m }: { m: Mensaje }) {
    // Voz E1: la llamada queda en el hilo como una tarjeta propia (contestada, perdida, rechazada…).
    if (m.tipo === "llamada" && m.llamada) return <LlamadaEnHilo m={m} />;
    if (m.direccion === "sistema") {
        const tipo = m.evento?.tipo ?? "";
        const bot = esEventoBot(tipo);
        const Icono =
            tipo === "bot_atendio" || tipo === "menu_elegido" || tipo === "bot_sin_respuesta"
                ? Bot
                : tipo === "asistente_pausado"
                  ? CirclePause
                  : tipo === "asistente_reanudado"
                    ? Sparkles
                : tipo === "aviso_sin_atender"
                  ? Timer
                  : tipo === "pasada"
                    ? ArrowLeftRight
                    : tipo === "reconexion_pedida"
                      ? Plug
                      : tipo === "identidad_verificada" && identidadQuedoVerificada(m.evento?.detalle)
                        ? ShieldCheck
                        : tipo === "identidad_verificada" && resultadoIdentidad(m.evento?.detalle)
                          ? ShieldX
                          : MessageCircle;
        return (
            <p
                className={cn(
                    "flex max-w-full min-w-0 items-center gap-1.5 self-center px-2.5 py-0.5 text-xs",
                    tipo === "bot_sin_respuesta" ? "rounded-xl" : "rounded-full",
                    // El bot contestó (información) o no pudo (aviso: el cliente sigue sin respuesta).
                    tipo === "bot_atendio" ? "border border-info/30 bg-info/10 text-info" : tipo === "bot_sin_respuesta" ? "border border-warning/40 bg-warning/10 text-warning" : "bg-elevated text-muted-foreground",
                )}
                data-mensaje={m.id}
                data-evento={tipo || undefined}
                data-detalle={tipo === "identidad_verificada" ? (m.evento?.detalle ?? undefined) : undefined}
                data-bot={bot || undefined}
            >
                <Icono aria-hidden className="size-3.5 shrink-0" />
                {/* Lo que pide actuar se lee entero (en el teléfono no cabe en una línea). */}
                <span className={tipo === "bot_sin_respuesta" ? "min-w-0 [overflow-wrap:anywhere]" : "truncate"}>{m.evento ? textoEvento(m.evento) : (m.texto ?? "")}</span>
                <span className="shrink-0 tabular-nums">· {hora(m.creadoEn)}</span>
            </p>
        );
    }
    const propia = m.direccion === "saliente";
    const nota = m.direccion === "nota";
    // Fase 1.1: mensajes del bot (`automatico: "bot"`) con su etiqueta y borde punteado de información.
    const automatico = m.automatico ?? null;
    const delBot = propia && automatico === "bot";
    // Voz E1b: la solicitud de permiso para llamar.
    const pidePermiso = propia && automatico === "permiso_llamada";
    // Fase 3 (§17): el asistente de IA respondió solo, o dejó una nota al pasar a una persona.
    const delAsistente = (propia || nota) && esDelAsistente(m);
    // §15.2: lo mandado desde la app WhatsApp Business del teléfono (sin autor) y lo traído del historial.
    const desdeTelefono = m.origen === "telefono";
    const delHistorial = m.origen === "historial";
    const pie = [
        delAsistente ? null : nota || (propia && !delBot && !m.origen) ? (m.autor?.nombre ?? (propia ? "Respuesta automática" : null)) : null,
        hora(m.creadoEn),
        propia ? textoEntrega(m) : null,
    ]
        .filter(Boolean)
        .join(" · ");
    // Una foto: la burbuja se ajusta a ella (sin relleno) y la hora va sobre la foto si no hay
    // texto; con texto, el pie va dentro de la burbuja, debajo.
    const foto = esFoto(m);
    const etiquetas = nota || (propia && delAsistente) || desdeTelefono || delHistorial || pidePermiso || delBot || m.buzonDeVoz || m.plantilla;
    const horaSobreFoto = foto && !m.texto && m.estadoEntrega !== "fallido" && !(nota && (m.menciones?.length ?? 0) > 0);
    return (
        <>
            {nota && delAsistente ? (
                // 3b: la nota del asistente es su relevo: desde aquí sigue una persona.
                <p data-slot="relevo-asistente" className="flex max-w-full min-w-0 items-center gap-1.5 self-center rounded-full border border-primary/30 bg-primary/[0.06] px-2.5 py-0.5 text-xs font-medium text-primary">
                    <UserRound aria-hidden className="size-3.5 shrink-0" />
                    <span className="truncate">El asistente la pasó a una persona</span>
                    <span className="shrink-0 tabular-nums">· {hora(m.creadoEn)}</span>
                </p>
            ) : null}
            <div
                data-mensaje={m.id}
                data-direccion={m.direccion}
                data-autor={delAsistente ? "asistente_ia" : undefined}
                data-foto={foto ? "" : undefined}
                className={cn(
                    "flex max-w-[min(72%,35rem)] min-w-0 flex-col gap-0.5 rounded-[0.875rem]",
                    foto ? "p-[3px] [&>:not([data-slot=foto-mensaje])]:mx-2 [&>:not([data-slot=foto-mensaje]):last-child]:mb-1" : "px-3 py-2",
                    foto && etiquetas && "pt-1.5",
                    m.direccion === "entrante" && "self-start rounded-bl-sm border border-border bg-card",
                    propia && !delBot && !delAsistente && !pidePermiso && "self-end rounded-br-sm bg-primary/10",
                    pidePermiso && "self-end rounded-br-sm border border-primary/40 bg-primary/[0.06]",
                    propia && delAsistente && "self-end rounded-br-sm border border-primary/30 bg-primary/[0.06]",
                    delBot && "self-end rounded-br-sm border border-dashed border-info/40 bg-card",
                    nota && "self-end rounded-br-sm border border-dashed border-warning/50 bg-warning/10",
                    propia && m.estadoEntrega === "fallido" && "ring-1 ring-error/40",
                )}
            >
                {nota ? <EtiquetaNota delAsistente={delAsistente} /> : null}
                {propia && delAsistente ? <EtiquetaAsistente /> : null}
                {desdeTelefono ? (
                    <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-muted-foreground" data-slot="etiqueta-telefono">
                        <Smartphone aria-hidden className="size-3.5" />
                        Desde el teléfono
                    </span>
                ) : delHistorial ? (
                    <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-muted-foreground" data-slot="etiqueta-historial">
                        <History aria-hidden className="size-3.5" />
                        Del historial del teléfono
                    </span>
                ) : null}
                {pidePermiso ? (
                    <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-primary" data-slot="etiqueta-permiso-llamada">
                        <PhoneCall aria-hidden className="size-3.5" />
                        Solicitud de permiso para llamar
                    </span>
                ) : null}
                {delBot ? (
                    <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-info" data-slot="etiqueta-bot">
                        <Bot aria-hidden className="size-3.5" />
                        Bot · mensaje automático
                    </span>
                ) : null}
                {m.buzonDeVoz ? (
                    // Voz E1: el recado que el cliente dejó en el buzón al llamar fuera de horario.
                    <span className="flex items-center gap-1 text-[0.6875rem] font-semibold text-warning" data-slot="etiqueta-buzon">
                        <Voicemail aria-hidden className="size-3.5" />
                        Mensaje de voz del buzón
                    </span>
                ) : null}
                {m.adjunto ? <Adjunto m={m} superpuesto={horaSobreFoto ? pie : undefined} /> : null}
                {m.ubicacion ? (
                    <a
                        className="flex items-center gap-1.5 text-sm text-primary underline-offset-2 hover:underline"
                        href={`https://www.openstreetmap.org/?mlat=${m.ubicacion.lat}&mlon=${m.ubicacion.lon}#map=17/${m.ubicacion.lat}/${m.ubicacion.lon}`}
                        target="_blank"
                        rel="noopener noreferrer"
                    >
                        <MapPin aria-hidden className="size-4" />
                        Ver ubicación
                    </a>
                ) : null}
                {m.plantilla ? <span className="text-[0.6875rem] font-semibold text-muted-foreground">Plantilla «{m.plantilla.codigo}»</span> : null}
                {m.texto ? <p className="text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{m.texto}</p> : null}
                {nota && (m.menciones?.length ?? 0) > 0 ? (
                    <span className="text-xs text-muted-foreground [overflow-wrap:anywhere]">Para {(m.menciones ?? []).map((u) => `@${u.nombre}`).join(", ")}</span>
                ) : null}
                {horaSobreFoto ? null : <span className={cn("self-end text-[0.71875rem] text-muted-foreground", m.estadoEntrega === "fallido" && "text-error", foto && "pt-0.5")}>{pie}</span>}
                {propia ? <FallaMensaje m={m} /> : null}
            </div>
        </>
    );
}

/** Imágenes que se muestran en línea. Otro `image/*` (un SVG, p. ej.) va como documento. */
const IMAGEN_EN_LINEA: ReadonlySet<string> = new Set(["image/jpeg", "image/png", "image/webp", "image/gif"]);

const esImagen = (m: Mensaje) => m.adjunto !== undefined && (IMAGEN_EN_LINEA.has(m.adjunto.mime.toLowerCase()) || (m.tipo === "imagen" && m.adjunto.mime === ""));

/**
 * ¿El adjunto se dibuja como foto? Una imagen lista, la que todavía se descarga de WhatsApp (con
 * su espacio reservado) o la que no se pudo descargar. Un adjunto sin id válido no es foto.
 */
function esFoto(m: Mensaje): boolean {
    const a = m.adjunto;
    if (!a || !esImagen(m)) return false;
    return a.estado !== "completo" || rutaDescargaAdjunto(a.id) !== undefined;
}

/** Hasta dos minutos entre fotos seguidas del mismo lado: van juntas en una cuadrícula. */
const VENTANA_ALBUM_MS = 120_000;

/** Foto sola (sin texto, sin etiquetas ni falla): la que puede ir en una cuadrícula. */
function fotoDeAlbum(m: Mensaje): boolean {
    return (
        esFoto(m) &&
        m.adjunto?.estado === "completo" &&
        !m.texto &&
        (m.direccion === "entrante" || m.direccion === "saliente") &&
        !m.automatico &&
        !m.origen &&
        !m.buzonDeVoz &&
        !m.plantilla &&
        !esDelAsistente(m) &&
        m.estadoEntrega !== "fallido"
    );
}

type GrupoHilo = { indice: number; mensajes: Mensaje[] };

/**
 * Agrupa fotos seguidas del mismo lado (y del mismo autor), sin separador de día entre ellas y a
 * menos de dos minutos una de otra, como un álbum de WhatsApp. Lo demás va de a uno.
 */
export function agruparFotos(mensajes: readonly Mensaje[], separadores: readonly (string | null)[]): GrupoHilo[] {
    const grupos: GrupoHilo[] = [];
    mensajes.forEach((m, i) => {
        const ultimo = grupos.at(-1);
        const anterior = ultimo?.mensajes.at(-1);
        if (
            ultimo &&
            anterior &&
            !separadores[i] &&
            fotoDeAlbum(m) &&
            fotoDeAlbum(anterior) &&
            m.direccion === anterior.direccion &&
            (m.autor?.id ?? null) === (anterior.autor?.id ?? null) &&
            Math.abs(Date.parse(m.creadoEn) - Date.parse(anterior.creadoEn)) <= VENTANA_ALBUM_MS
        ) {
            ultimo.mensajes.push(m);
            return;
        }
        grupos.push({ indice: i, mensajes: [m] });
    });
    return grupos;
}

/** Las fotos listas del hilo, en orden, para el visor. */
function fotosDelHilo(mensajes: readonly Mensaje[], nombreCliente: string): FotoEnVisor[] {
    const fotos: FotoEnVisor[] = [];
    for (const m of mensajes) {
        const a = m.adjunto;
        const url = a && a.estado === "completo" && esImagen(m) ? rutaDescargaAdjunto(a.id) : undefined;
        if (!url) continue;
        const quien = m.direccion === "entrante" ? nombreCliente : (m.autor?.nombre ?? "Iris");
        fotos.push({ id: m.id, src: url, descarga: url, titulo: quien, detalle: `${separadorDia(m.creadoEn, Date.now())} ${hora(m.creadoEn)}`, alt: `Foto de ${quien}, ${hora(m.creadoEn)}`, pie: m.texto ?? null });
    }
    return fotos;
}

function etiquetaFoto(m: Mensaje): string {
    return `Ver foto de las ${hora(m.creadoEn)}${m.adjunto ? ` (${tamanoLegible(m.adjunto.tamano)})` : ""}`;
}

/** Varias fotos seguidas en una sola burbuja, en cuadrícula; la hora y la entrega, de la última. */
function Album({ ms }: { ms: Mensaje[] }) {
    const abrir = use(AbrirFoto);
    const ultimo = ms.at(-1);
    const primero = ms[0];
    if (!ultimo || !primero) return null;
    const propia = ultimo.direccion === "saliente";
    const pie = [propia ? ultimo.autor?.nombre : null, hora(ultimo.creadoEn), propia ? textoEntrega(ultimo) : null].filter(Boolean).join(" · ");
    return (
        <div
            data-mensaje={primero.id}
            data-album={ms.map((m) => m.id).join(" ")}
            data-direccion={ultimo.direccion}
            className={cn("max-w-[min(72%,35rem)] min-w-0 rounded-[0.875rem] p-[3px]", propia ? "self-end rounded-br-sm bg-primary/10" : "self-start rounded-bl-sm border border-border bg-card")}
        >
            <AlbumFotos
                etiqueta={`${ms.length} fotos`}
                superpuesto={pie}
                fotos={ms.map((m) => ({
                    id: m.id,
                    src: m.adjunto ? rutaDescargaAdjunto(m.adjunto.id) : undefined,
                    ancho: m.adjunto?.ancho,
                    alto: m.adjunto?.alto,
                    estado: "lista" as const,
                    etiqueta: etiquetaFoto(m),
                    alAbrir: () => abrir(m.id),
                }))}
            />
        </div>
    );
}

/**
 * Adjunto: foto (con su proporción reservada, ver `FotoMensaje`) o documento con nombre, tamaño y
 * estado («descargando de WhatsApp…»). El enlace se arma siempre desde el id validado
 * (`rutaDescargaAdjunto`), nunca desde la URL de los datos.
 */
function Adjunto({ m, superpuesto }: { m: Mensaje; superpuesto?: string | undefined }) {
    const abrir = use(AbrirFoto);
    const a = m.adjunto;
    if (!a) return null;
    const url = rutaDescargaAdjunto(a.id);
    const listo = a.estado === "completo" && url !== undefined;
    if (esFoto(m)) {
        return (
            <FotoMensaje
                src={listo ? url : undefined}
                ancho={a.ancho}
                alto={a.alto}
                estado={listo ? "lista" : a.estado === "rechazado" ? "rechazada" : "pendiente"}
                textoPendiente={m.direccion === "entrante" ? "Descargando de WhatsApp…" : "Subiendo foto…"}
                textoRechazada="No se pudo descargar la foto"
                etiqueta={etiquetaFoto(m)}
                alAbrir={() => abrir(m.id)}
                superpuesto={superpuesto}
            />
        );
    }
    const nombre = a.mime === "application/pdf" ? "Documento PDF" : "Archivo";
    return (
        <span className="flex min-w-0 items-center gap-2.5 rounded-lg bg-background-secondary p-2">
            <span aria-hidden className="flex size-10 shrink-0 items-center justify-center rounded-md bg-elevated text-muted-foreground">
                <FileText className="size-4" />
            </span>
            <span className="grid min-w-0">
                {listo ? (
                    <a href={url} target="_blank" rel="noopener noreferrer" className="truncate text-[0.8125rem] font-semibold text-primary hover:underline">
                        {nombre}
                    </a>
                ) : (
                    <span className="truncate text-[0.8125rem] font-semibold">{nombre}</span>
                )}
                <span className="truncate text-xs text-muted-foreground">
                    {tamanoLegible(a.tamano)} · {a.estado === "pendiente" || a.estado === "subiendo" ? "descargando de WhatsApp…" : a.estado === "rechazado" ? "no se pudo descargar" : "listo"}
                </span>
            </span>
        </span>
    );
}
