"use client";

import { avisar, AreaTexto, Aviso, Boton, CampoBusqueda, cn, ControlesLlamada, ControlLlamada, Dialogo, DialogoContenido, DialogoPie, Esqueleto, GrupoOpciones, TecladoTelefono } from "@iris/ui";
import { ChevronDown, ChevronUp, Grid3x3, Info, Mic, MicOff, Pause, Phone, PhoneCall, PhoneForwarded, PhoneIncoming, PhoneOff, PhoneOutgoing, Play, Ticket, UserRound } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { anotarLlamadaCentralita, vincularTicketCentralita } from "@/lib/centralita/api";
import { quienCentralita, RESULTADOS_CENTRALITA } from "@/lib/centralita/presentacion";
import { useLectura } from "@/lib/lectura";
import { destinosTransferencia } from "@/lib/telefono/api";
import { numeroLegible, reloj } from "@/lib/telefono/presentacion";
import type { LlamadaTelefono } from "@/lib/telefono/tipos";
import type { Llamada, ResultadoLlamada } from "@/lib/voz/tipos";
import { CabeceraLlamada, MARCO_AVISO, Senales } from "../voz/llamada";
import { DialogoEncender } from "./indicador";
import { DialogoMarcar } from "./marcar";
import { useTelefono, type ContextoTelefono } from "./proveedor";

/*
 * La llamada del teléfono de Iris (maqueta E4c, pantallas 04–14): la ventana de la entrante (la
 * misma de E4a, con «Rechazar» y «Contestar»), la barra de la llamada bajo la barra superior y el
 * panel con los controles (silenciar, en espera, teclado, transferir, colgar), el cliente y la nota.
 */

/** Cómo se muestra quién está del otro lado. */
function quien(t: LlamadaTelefono, iris: Llamada | null): { nombre: string; conocido: boolean } {
    if (iris) {
        const q = quienCentralita(iris);
        if (q.conocido) return { nombre: q.nombre, conocido: true };
    }
    if (t.interna || t.direccion === "saliente") return { nombre: t.nombre, conocido: t.nombre !== t.numero };
    return { nombre: t.numero ? numeroLegible(t.numero) : "Número oculto", conocido: false };
}

function useSegundos(desde: number | null): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        if (desde === null) return;
        const t = window.setInterval(() => setAhora(Date.now()), 1000);
        return () => window.clearInterval(t);
    }, [desde]);
    return desde === null ? 0 : Math.max(0, Math.floor((ahora - desde) / 1000));
}

/** Todo lo flotante del teléfono: ventanas de las entrantes, el panel de la llamada y los diálogos. */
export function CapaTelefono() {
    const t = useTelefono();
    if (!t) return null;
    const llamadas = t.estado.llamadas;
    const sonando = llamadas.filter((l) => l.fase === "sonando");
    const activa = llamadas.find((l) => l.fase === "en_curso" && !l.enEspera) ?? llamadas.find((l) => l.fase !== "sonando") ?? null;
    const d = t.dialogo;
    return (
        <>
            {sonando.map((l, i) => (
                <VentanaEntrante key={l.id} t={t} llamada={l} otra={llamadas.some((x) => x.fase === "en_curso")} segunda={i > 0} />
            ))}
            {activa && t.panelAbierto ? <PanelLlamada t={t} llamada={activa} /> : null}
            {d === "encender" ? <DialogoEncender t={t} /> : null}
            {d === "marcar" ? <DialogoMarcar t={t} /> : null}
            {d !== null && typeof d === "object" && d.tipo === "tonos" ? <DialogoTonos t={t} id={d.id} /> : null}
            {d !== null && typeof d === "object" && d.tipo === "transferir" ? <DialogoTransferir t={t} id={d.id} /> : null}
        </>
    );
}

function VentanaEntrante({ t, llamada, otra, segunda }: { t: ContextoTelefono; llamada: LlamadaTelefono; otra: boolean; segunda: boolean }) {
    const iris = t.llamadaIris(llamada);
    const q = quien(llamada, iris);
    const ext = t.estado.extension;
    const [contestando, setContestando] = useState(false);
    return (
        <section
            role="alertdialog"
            aria-label={`Llamada entrante: ${q.nombre}`}
            data-slot="entrante-telefono"
            data-conocido={q.conocido || undefined}
            className={cn(MARCO_AVISO, "border-primary/50 bg-[var(--overlay)] ring-2 ring-primary/25", segunda && "top-[calc(env(safe-area-inset-top)+13rem)]")}
        >
            <CabeceraLlamada
                conocido={q.conocido}
                nombre={q.nombre}
                linea={otra ? "Otra llamada · Centralita" : `Centralita · suena en Iris${ext ? ` (ext. ${ext})` : ""}`}
                detalle={q.conocido && llamada.numero ? numeroLegible(llamada.numero) : undefined}
                sonando
            />
            {iris && iris.contacto.tipo === "cliente" ? <Senales llamada={iris} saldoVencido={false} /> : null}
            {!q.conocido ? <p className="text-[0.8125rem] text-muted-foreground">Este número no está en Argos ni en tus contactos. Al contestar puedes buscar al cliente o crear un ticket.</p> : null}
            {otra ? <p className="text-[0.8125rem] text-muted-foreground">Estás en otra llamada. Si contestas, esa queda <b>en espera</b> hasta que vuelvas.</p> : null}
            <div className="grid grid-cols-2 gap-2">
                <Boton variante="peligro" className="h-11 w-full" onClick={() => t.rechazar(llamada.id)}>
                    <PhoneOff />
                    Rechazar
                </Boton>
                <Boton
                    className="h-11 w-full"
                    disabled={contestando}
                    onClick={async () => {
                        setContestando(true);
                        await t.contestar(llamada.id);
                        setContestando(false);
                    }}
                >
                    {otra ? <Pause /> : <Phone />}
                    {otra ? "Pausar y contestar" : "Contestar"}
                </Boton>
            </div>
            <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                <Info aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                Si rechazas, la centralita la pasa a la siguiente persona o al buzón, como hoy.
            </p>
        </section>
    );
}

/** Bajo la barra superior, en todas las pantallas: quién, el tiempo, «Silenciar» y «Colgar». */
export function BarraTelefono() {
    const t = useTelefono();
    const activa = t?.estado.llamadas.find((l) => l.fase === "en_curso" && !l.enEspera) ?? t?.estado.llamadas.find((l) => l.fase === "en_curso" || l.fase === "llamando") ?? null;
    const segundos = useSegundos(activa?.desde ?? null);
    if (!t || !activa) return null;
    const iris = t.llamadaIris(activa);
    const q = quien(activa, iris);
    const llamando = activa.fase === "llamando";
    const esperando = t.estado.llamadas.filter((l) => l.enEspera && l.id !== activa.id).length;
    return (
        <section
            aria-label={llamando ? "Llamando" : "Llamada en curso"}
            data-slot="barra-telefono"
            data-fase={activa.fase}
            className="sticky top-0 z-[15] mb-3 flex h-[3.25rem] rounded-xl border min-w-0 items-center gap-2 border-b border-primary/40 bg-[color-mix(in_oklab,var(--hx-primary)_12%,var(--hx-bg))] px-3 sm:gap-3 sm:px-4 lg:px-6"
        >
            <span aria-hidden className={cn("size-2 shrink-0 rounded-full motion-safe:animate-pulse", llamando ? "bg-warning" : activa.enEspera ? "bg-warning" : "bg-success")} />
            {llamando ? <PhoneOutgoing aria-hidden className="size-4 shrink-0 text-primary" /> : <PhoneCall aria-hidden className="size-4 shrink-0 text-primary" />}
            <p className="min-w-0 flex-1 truncate text-sm" role={llamando ? "status" : undefined}>
                <span className="font-semibold">
                    {llamando ? "Llamando a " : <span className="hidden sm:inline">En llamada con </span>}
                    {q.nombre}
                    {llamando ? "…" : ""}
                </span>
                {!llamando ? (
                    <>
                        <span className="sr-only">, </span>
                        <span role="timer" aria-label="Tiempo de la llamada" className="ml-2 font-mono text-[0.8125rem] text-muted-foreground tabular-nums">
                            {activa.enEspera ? "En espera · " : ""}
                            {reloj(segundos)}
                        </span>
                    </>
                ) : null}
                {esperando > 0 ? <span className="ml-2 text-xs text-warning">+{esperando} en espera</span> : null}
            </p>
            <Boton variante="fantasma" tamano="sm" className="px-2 sm:px-3" aria-expanded={t.panelAbierto} onClick={() => t.setPanelAbierto(!t.panelAbierto)}>
                {t.panelAbierto ? <ChevronUp /> : <ChevronDown />}
                <span className="hidden sm:inline">{t.panelAbierto ? "Ocultar" : "Ver llamada"}</span>
                <span className="sr-only sm:hidden">{t.panelAbierto ? "Ocultar la llamada" : "Ver la llamada"}</span>
            </Boton>
            {!llamando ? (
                <Boton variante="secundario" tamano="sm" className="px-2 sm:px-3" aria-pressed={activa.silenciada} onClick={() => t.silenciar(activa.id, !activa.silenciada)}>
                    {activa.silenciada ? <MicOff /> : <Mic />}
                    <span className="hidden sm:inline">{activa.silenciada ? "Silenciada" : "Silenciar"}</span>
                    <span className="sr-only sm:hidden">{activa.silenciada ? "Quitar el silencio" : "Silenciar"}</span>
                </Boton>
            ) : null}
            <Boton variante="peligro" tamano="sm" onClick={() => t.colgar(activa.id)}>
                <PhoneOff />
                Colgar
            </Boton>
        </section>
    );
}

function PanelLlamada({ t, llamada }: { t: ContextoTelefono; llamada: LlamadaTelefono }) {
    const iris = t.llamadaIris(llamada);
    const q = quien(llamada, iris);
    const segundos = useSegundos(llamada.desde);
    const llamando = llamada.fase === "llamando";
    const ext = t.estado.extension;
    const transfiere = !!t.mio?.puede.transferir && !llamada.interna;
    return (
        <section
            aria-label="Llamada"
            data-slot="panel-llamada"
            data-fase={llamada.fase}
            className="fixed inset-x-0 top-[calc(3.75rem+3.25rem)] bottom-0 z-40 grid content-start gap-4 overflow-y-auto border-t border-border bg-[var(--overlay)] p-4 pb-[calc(1rem+env(safe-area-inset-bottom))] shadow-lg sm:inset-x-auto sm:top-[calc(4.75rem+3.25rem+0.75rem)] sm:right-4 sm:bottom-auto sm:max-h-[calc(100dvh-9.75rem)] sm:w-[22rem] sm:rounded-xl sm:border"
        >
            <div className="flex min-w-0 items-center gap-3">
                <span aria-hidden className={cn("flex size-11 shrink-0 items-center justify-center rounded-full bg-primary/15 text-primary", llamando && "motion-safe:animate-pulse")}>
                    {q.conocido ? <UserRound className="size-5" /> : llamada.direccion === "entrante" ? <PhoneIncoming className="size-5" /> : <PhoneOutgoing className="size-5" />}
                </span>
                <div className="grid min-w-0 flex-1">
                    <p className="truncate text-[0.9375rem] font-semibold">{q.nombre}</p>
                    <p className="truncate text-[0.8125rem] text-muted-foreground">
                        {llamada.numero && q.nombre !== numeroLegible(llamada.numero) ? `${numeroLegible(llamada.numero)} · ` : ""}
                        Centralita{ext ? ` · ext. ${ext}` : ""}
                    </p>
                </div>
                <Boton variante="fantasma" tamano="icono-sm" aria-label="Ocultar la llamada" onClick={() => t.setPanelAbierto(false)}>
                    <ChevronUp />
                </Boton>
            </div>
            <p className="text-center font-mono text-2xl font-semibold tabular-nums" role={llamando ? "status" : "timer"} aria-label={llamando ? undefined : "Tiempo de la llamada"}>
                {llamando ? <span className="text-base font-medium text-muted-foreground">Sonando…</span> : (
                    <>
                        {llamada.enEspera ? <span className="text-base text-warning">En espera · </span> : null}
                        {reloj(segundos)}
                    </>
                )}
            </p>
            {iris && iris.contacto.tipo === "cliente" ? <Senales llamada={iris} saldoVencido={false} /> : null}
            <ControlesLlamada>
                <ControlLlamada icono={llamada.silenciada ? MicOff : Mic} etiqueta={llamada.silenciada ? "Silenciada" : "Silenciar"} activo={llamada.silenciada} deshabilitado={llamando} onClick={() => t.silenciar(llamada.id, !llamada.silenciada)} />
                <ControlLlamada icono={llamada.enEspera ? Play : Pause} etiqueta={llamada.enEspera ? "Volver" : "En espera"} activo={llamada.enEspera} deshabilitado={llamando} onClick={() => t.espera(llamada.id, !llamada.enEspera)} />
                <ControlLlamada icono={Grid3x3} etiqueta="Teclado" deshabilitado={llamando} onClick={() => t.abrir({ tipo: "tonos", id: llamada.id })} />
                {transfiere ? <ControlLlamada icono={PhoneForwarded} etiqueta="Transferir" deshabilitado={llamando} onClick={() => t.abrir({ tipo: "transferir", id: llamada.id })} /> : null}
                <ControlLlamada icono={PhoneOff} etiqueta="Colgar" peligro onClick={() => t.colgar(llamada.id)} />
            </ControlesLlamada>
            {llamando ? (
                <p className="text-center text-[0.8125rem] text-muted-foreground">
                    Suena en el teléfono {llamada.interna ? "de tu compañero" : "del cliente"}. {llamada.interna ? "" : "Si contesta, la llamada queda en su ficha."}
                </p>
            ) : iris ? (
                <NotaYTicket t={t} llamada={iris} />
            ) : llamada.interna ? null : (
                <p className="text-[0.8125rem] text-muted-foreground">La centralita no dijo qué llamada es: la nota se escribe al colgar, en sus llamadas.</p>
            )}
        </section>
    );
}

/** La nota (se guarda sola), el resultado y el ticket de la llamada de Iris. */
function NotaYTicket({ t, llamada }: { t: ContextoTelefono; llamada: Llamada }) {
    const router = useRouter();
    const [nota, setNota] = useState(llamada.nota ?? "");
    const [estado, setEstado] = useState<"" | "guardando" | "guardada" | "error">("");
    const version = useRef(llamada.version ?? 1);
    const ticket = llamada.senales?.ticketAbierto;
    const puede = llamada.puede.anotar;
    const guardar = async (cambio: { nota?: string; resultado?: ResultadoLlamada }) => {
        setEstado("guardando");
        try {
            const l = await anotarLlamadaCentralita(llamada.id, { version: version.current, ...cambio }, nuevaClaveIdempotencia());
            version.current = l.version ?? version.current + 1;
            t.actualizarLlamadaIris(l);
            setEstado("guardada");
        } catch {
            setEstado("error");
        }
    };
    // La nota se guarda sola, 1,5 s después de dejar de escribir.
    useEffect(() => {
        if (!puede || nota === (llamada.nota ?? "")) return;
        const id = window.setTimeout(() => void guardar({ nota: nota.trim() || undefined }), 1500);
        return () => window.clearTimeout(id);
        // eslint-disable-next-line react-hooks/exhaustive-deps -- solo al cambiar el texto
    }, [nota]);
    if (!puede) return null;
    const crearTicket = () => {
        const q = new URLSearchParams({ llamada: llamada.id });
        if (llamada.contacto.clienteId) q.set("clienteId", String(llamada.contacto.clienteId));
        if (llamada.contacto.servicioId) q.set("servicioId", String(llamada.contacto.servicioId));
        if (llamada.numero) q.set("telefono", llamada.numero);
        router.push(`/tickets/nuevo?${q.toString()}`);
    };
    return (
        <div className="grid gap-3 border-t border-border pt-3" data-slot="nota-llamada">
            <label className="grid gap-1.5 text-sm font-medium">
                Nota de la llamada
                <AreaTexto value={nota} maxLength={2000} rows={3} placeholder="Qué pidió, qué le dijiste…" onChange={(e) => setNota(e.target.value)} />
                <span className="text-xs font-normal text-muted-foreground" aria-live="polite">
                    {estado === "guardando" ? "Guardando…" : estado === "guardada" ? "Guardada. Queda en la ficha del cliente y en el ticket." : estado === "error" ? "No se pudo guardar: se intenta otra vez al escribir." : "Se guarda sola. Queda en la ficha del cliente y en el ticket."}
                </span>
            </label>
            <GrupoOpciones
                leyenda="Resultado"
                nombre={`resultado-tel-${llamada.id}`}
                opciones={RESULTADOS_CENTRALITA.map((r) => ({ valor: r.valor, etiqueta: r.texto }))}
                valor={llamada.resultado && llamada.resultado !== "ticket_creado" ? llamada.resultado : undefined}
                alCambiar={(r) => void guardar({ resultado: r, nota: nota.trim() || undefined })}
            />
            <div className="flex flex-wrap gap-2">
                {ticket && !llamada.ticketId ? (
                    <Boton
                        onClick={async () => {
                            try {
                                t.actualizarLlamadaIris(await vincularTicketCentralita(llamada.id, ticket.id));
                                avisar({ titulo: `Llamada agregada al ticket ${ticket.numero}` });
                            } catch (e) {
                                avisar({ tono: "error", titulo: mensajeDeError(e).titulo });
                            }
                        }}
                    >
                        <Ticket />
                        Agregar a {ticket.numero}
                    </Boton>
                ) : null}
                {!llamada.ticketId ? (
                    <Boton variante={ticket ? "secundario" : "primario"} onClick={crearTicket}>
                        Crear ticket
                    </Boton>
                ) : null}
            </div>
        </div>
    );
}

/** Tonos durante la llamada (lo que pide la grabación del otro lado). */
function DialogoTonos({ t, id }: { t: ContextoTelefono; id: string }) {
    const [marcado, setMarcado] = useState("");
    const cerrar = () => t.abrir(null);
    return (
        <Dialogo open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo="Teclado" descripcion="Marca lo que te pida la grabación del otro lado." centrado>
                <p aria-live="polite" className="min-h-9 text-center font-mono text-2xl tracking-[0.2em] tabular-nums" data-slot="tonos-marcados">
                    {marcado}
                </p>
                <TecladoTelefono
                    etiqueta="Tonos"
                    alTocar={(d) => {
                        t.tono(id, d);
                        setMarcado((m) => (m + d).slice(-16));
                    }}
                />
                <DialogoPie>
                    <Boton variante="secundario" onClick={cerrar}>
                        Cerrar teclado
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}

/** Transferir a una persona o grupo de la empresa (de la lista; nunca a un número libre). */
function DialogoTransferir({ t, id }: { t: ContextoTelefono; id: string }) {
    const destinos = useLectura((signal) => destinosTransferencia(signal));
    const [busca, setBusca] = useState("");
    const [elegido, setElegido] = useState<string | undefined>(undefined);
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState(false);
    const llamada = t.estado.llamadas.find((l) => l.id === id);
    const cerrar = () => t.abrir(null);
    const filtro = busca.trim().toLowerCase();
    const opciones =
        destinos.estado === "listo"
            ? [
                  ...(destinos.datos.personas ?? []).map((p) => ({ valor: p.extension, etiqueta: `${p.nombre} · ext. ${p.extension}`, detalle: "Persona de tu equipo" })),
                  ...(destinos.datos.grupos ?? []).map((g) => ({ valor: g.numero, etiqueta: `${g.nombre} · grupo ${g.numero}`, detalle: "Suena a todo el grupo" })),
              ].filter((o) => filtro === "" || o.etiqueta.toLowerCase().includes(filtro))
            : [];
    const nombre = llamada ? quien(llamada, t.llamadaIris(llamada)).nombre : "";
    return (
        <Dialogo open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo={`Transferir a ${nombre}`} descripcion="Queda en espera mientras tanto. Solo personas y grupos de tu empresa con extensión.">
                <div className="grid gap-3" data-slot="transferir">
                    <CampoBusqueda etiqueta="Buscar persona o extensión" valor={busca} alCambiar={setBusca} />
                    {destinos.estado === "cargando" ? (
                        <Esqueleto className="h-24" />
                    ) : destinos.estado === "error" ? (
                        <Aviso
                            tono="error"
                            titulo="No se pudo leer a quién transferir"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={destinos.reintentar}>
                                    Reintentar
                                </Boton>
                            }
                        >
                            {mensajeDeError(destinos.error).titulo}
                        </Aviso>
                    ) : opciones.length === 0 ? (
                        <p className="text-sm text-muted-foreground">{filtro ? "Nadie con ese nombre o extensión." : "Nadie más de tu empresa tiene extensión."}</p>
                    ) : (
                        <GrupoOpciones leyenda="A quién" nombre={`transferir-${id}`} apiladas opciones={opciones} valor={elegido} alCambiar={setElegido} />
                    )}
                    <p className="text-xs text-muted-foreground">Se la pasas directo: le suena a quien elijas y tú quedas libre.</p>
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo transferir">
                            La centralita no aceptó la transferencia. La llamada sigue contigo.
                        </Aviso>
                    ) : null}
                </div>
                <DialogoPie>
                    <Boton variante="fantasma" onClick={cerrar}>
                        Cancelar
                    </Boton>
                    <Boton
                        disabled={!elegido || enviando || !llamada}
                        onClick={async () => {
                            if (!elegido || !llamada) return;
                            setEnviando(true);
                            setError(false);
                            const destino = opciones.find((o) => o.valor === elegido);
                            try {
                                if (!llamada.enEspera) t.espera(id, true);
                                await t.transferir(id, elegido);
                                avisar({ titulo: `Llamada transferida a ${destino?.etiqueta.split(" · ")[0] ?? elegido}` });
                                cerrar();
                            } catch {
                                setError(true);
                                t.espera(id, false);
                            } finally {
                                setEnviando(false);
                            }
                        }}
                    >
                        <PhoneForwarded />
                        Transferir
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}
