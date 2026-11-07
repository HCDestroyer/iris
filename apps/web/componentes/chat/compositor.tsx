"use client";

import { AreaTexto, avisar, Boton, Camara, cn, idOpcion, inicialesDe, ListaSugerencias, teclasSugerencias } from "@iris/ui";
import { MAXIMO_TEXTO, textoValido, type MotorChat } from "@iris/sync/chat";
import { Camera, Info, Lock, MapPin, Send } from "lucide-react";
import { useEffect, useId, useRef, useState, useSyncExternalStore, type KeyboardEvent } from "react";
import { comprimirFoto } from "@/lib/campo/imagen";
import { aplicarMencion, candidatosMencion, consultaMencion, mencionesVigentes, textoMotivo, type ConsultaMencion } from "@/lib/chat/presentacion";
import type { MiembroChat } from "@/lib/chat/api";

type Props = {
    motor: MotorChat;
    conversacionId: number;
    tipo: "directa" | "cuadrilla" | "orden";
    /** «Escribe en OT-000214…». */
    nombre: string;
    miembros: readonly MiembroChat[];
    yoId: number;
    /** Orden cerrada (o sin `chat.enviar`): el motivo, y el compositor no deja escribir. */
    bloqueado: string | null;
    className?: string;
};

const CONSULTA_TELEFONO = "(max-width: 639px)";
function suscribirTelefono(avisarCambio: () => void) {
    const c = window.matchMedia(CONSULTA_TELEFONO);
    c.addEventListener("change", avisarCambio);
    return () => c.removeEventListener("change", avisarCambio);
}
/** Ancho de teléfono: foto y ubicación (como en la app de campo); en escritorio, deshabilitadas. */
function useAnchoTelefono(): boolean {
    return useSyncExternalStore(suscribirTelefono, () => window.matchMedia(CONSULTA_TELEFONO).matches, () => false);
}

/** Compositor del chat del equipo: Enter envía, Mayús + Enter baja de línea, `@` menciona. */
export function CompositorChat({ motor, conversacionId, tipo, nombre, miembros, yoId, bloqueado, className }: Props) {
    const [texto, setTexto] = useState("");
    const [elegidos, setElegidos] = useState<{ id: number; nombre: string }[]>([]);
    const [consulta, setConsulta] = useState<ConsultaMencion | null>(null);
    const [activa, setActiva] = useState(0);
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const area = useRef<HTMLTextAreaElement>(null);
    const idLista = useId();
    const telefono = useAnchoTelefono();
    const conGeo = typeof navigator !== "undefined" && "geolocation" in navigator;

    // Otra conversación: el borrador no se arrastra.
    useEffect(() => {
        setTexto("");
        setElegidos([]);
        setConsulta(null);
        setError(null);
    }, [conversacionId]);

    const candidatos = consulta ? candidatosMencion(miembros, consulta.texto, yoId) : [];
    const abierta = candidatos.length > 0;
    const valido = textoValido(texto);

    const leerConsulta = (valor: string, cursor: number) => {
        const c = consultaMencion(valor, cursor);
        setConsulta(c);
        setActiva(0);
    };

    const elegir = (indice: number) => {
        const m = candidatos[indice];
        if (!m || !consulta) return;
        const r = aplicarMencion(texto, consulta, m.nombre);
        setTexto(r.texto);
        setElegidos((e) => (e.some((x) => x.id === m.usuario_id) ? e : [...e, { id: m.usuario_id, nombre: m.nombre }]));
        setConsulta(null);
        requestAnimationFrame(() => {
            area.current?.focus();
            area.current?.setSelectionRange(r.cursor, r.cursor);
        });
    };

    // Todo envío pasa por la cola del motor (id uuid v7 al escribir, idempotente y sin señal).
    const enviar = async () => {
        if (bloqueado || enviando || !valido.valido) return;
        const limpio = texto.trim();
        const menciones = mencionesVigentes(limpio, elegidos);
        setError(null);
        setEnviando(true);
        try {
            await motor.escribir(conversacionId, { texto: limpio, ...(menciones.length > 0 ? { menciones } : {}) });
            setTexto("");
            setElegidos([]);
            setConsulta(null);
        } catch (e) {
            setError(e instanceof Error ? e.message : "No se pudo enviar.");
        } finally {
            setEnviando(false);
        }
    };

    const alTeclear = (e: KeyboardEvent<HTMLTextAreaElement>) => {
        if (abierta && teclasSugerencias(e, { total: candidatos.length, activa, alResaltar: setActiva, alElegir: elegir, alCerrar: () => setConsulta(null) })) return;
        if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
            e.preventDefault();
            void enviar();
        }
    };

    const adjuntar = async (accion: () => Promise<void>, hecho: string) => {
        setError(null);
        try {
            await accion();
            avisar({ titulo: hecho });
        } catch (e) {
            setError(e instanceof Error ? e.message : "No se pudo adjuntar.");
        }
    };
    const enviarFoto = (archivo: File) =>
        void adjuntar(async () => {
            const foto = await comprimirFoto(archivo);
            await motor.escribir(conversacionId, { foto: { datos: foto.bytes, mime: foto.mime, tomadoEn: new Date().toISOString() } });
        }, "Foto en camino");
    const enviarUbicacion = () =>
        void adjuntar(
            () =>
                new Promise<void>((resolver, rechazar) => {
                    navigator.geolocation.getCurrentPosition(
                        (p) =>
                            void motor.escribir(conversacionId, { ubicacion: { lat: p.coords.latitude, lon: p.coords.longitude } }).then(
                                () => resolver(),
                                rechazar,
                            ),
                        () => rechazar(new Error("No se pudo leer tu ubicación. Revisa el permiso del navegador.")),
                        { enableHighAccuracy: true, timeout: 15_000 },
                    );
                }),
            "Ubicación compartida",
        );

    if (bloqueado) {
        return (
            <div className={cn("m-3 flex items-center gap-2 rounded-xl border border-border bg-background-secondary px-3.5 py-3 text-sm text-muted-foreground", className)} data-slot="compositor-bloqueado">
                <Lock aria-hidden className="size-4 shrink-0" />
                <span>{bloqueado}</span>
            </div>
        );
    }

    return (
        <div className={cn("relative m-3 grid gap-2 rounded-xl border border-border bg-card p-2.5 focus-within:border-primary", className)} data-slot="compositor-chat">
            {abierta ? (
                <ListaSugerencias
                    id={idLista}
                    etiqueta="Mencionar a…"
                    activa={activa}
                    alResaltar={setActiva}
                    alElegir={(v) => elegir(candidatos.findIndex((c) => String(c.usuario_id) === v))}
                    className="absolute right-2 bottom-full left-2 z-10 mb-1"
                    opciones={candidatos.map((m) => ({
                        valor: String(m.usuario_id),
                        etiqueta: m.nombre,
                        detalle: textoMotivo(m.motivo, tipo) ?? undefined,
                        inicio: (
                            <span aria-hidden className="grid size-6 shrink-0 place-content-center rounded-full bg-elevated text-[0.625rem] font-semibold">
                                {inicialesDe(m.nombre)}
                            </span>
                        ),
                    }))}
                />
            ) : null}
            <AreaTexto
                ref={area}
                rows={2}
                value={texto}
                maxLength={MAXIMO_TEXTO + 200}
                placeholder={`Escribe en ${nombre}…`}
                aria-label={`Mensaje para ${nombre}`}
                role="combobox"
                aria-autocomplete="list"
                aria-expanded={abierta}
                aria-controls={abierta ? idLista : undefined}
                aria-activedescendant={abierta ? idOpcion(idLista, activa) : undefined}
                className="max-h-40 min-h-11 resize-none border-0 bg-transparent p-1 shadow-none focus-visible:ring-0"
                onChange={(e) => {
                    setTexto(e.target.value);
                    leerConsulta(e.target.value, e.target.selectionStart);
                }}
                onSelect={(e) => leerConsulta(e.currentTarget.value, e.currentTarget.selectionStart)}
                onBlur={() => setConsulta(null)}
                onKeyDown={alTeclear}
            />
            {error ? (
                <p role="alert" className="px-1 text-xs text-error">
                    {error}
                </p>
            ) : valido.motivo ? (
                <p className="px-1 text-xs text-warning">{valido.motivo}</p>
            ) : null}
            <div className="flex min-w-0 items-center gap-1">
                {telefono ? (
                    <>
                        <Camara alElegir={enviarFoto} variante="fantasma" tamano="icono-sm" aria-label="Enviar una foto">
                            <Camera />
                        </Camara>
                        {conGeo ? (
                            <Boton variante="fantasma" tamano="icono-sm" aria-label="Compartir tu ubicación" onClick={enviarUbicacion}>
                                <MapPin />
                            </Boton>
                        ) : null}
                    </>
                ) : (
                    <>
                        <Boton variante="fantasma" tamano="icono-sm" disabled aria-label="Foto: desde la app de campo o el teléfono">
                            <Camera />
                        </Boton>
                        <Boton variante="fantasma" tamano="icono-sm" disabled aria-label="Ubicación: desde la app de campo o el teléfono">
                            <MapPin />
                        </Boton>
                        <span className="ml-1 hidden min-w-0 items-center gap-1 truncate text-xs text-muted-foreground md:inline-flex">
                            <Info aria-hidden className="size-3.5 shrink-0" />
                            Foto y ubicación, desde la app de campo
                        </span>
                    </>
                )}
                <Boton tamano="sm" className="ml-auto" disabled={!valido.valido || enviando} onClick={() => void enviar()}>
                    {enviando ? "Enviando…" : "Enviar"}
                    <Send />
                </Boton>
            </div>
        </div>
    );
}
