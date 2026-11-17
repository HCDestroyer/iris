"use client";

import { avisar, Aviso, Boton, CampoArea, CampoSelector, Cargando, cn, Dialogo, DialogoConfirmar, DialogoContenido, DialogoPie, InsigniaEstado } from "@iris/ui";
import { Banknote, CalendarPlus, LifeBuoy, Mic, MicOff, Phone, PhoneCall, PhoneIncoming, PhoneOff, RotateCw, UserRound, X } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { anotarLlamada, leerLlamada } from "@/lib/voz/api";
import type { EstadoVoz } from "@/lib/voz/maquina";
import { quienLlama, reloj, RESULTADOS, textoAviso } from "@/lib/voz/presentacion";
import type { Llamada, ResultadoLlamada } from "@/lib/voz/tipos";
import { DialogoTicket } from "../conversaciones/ficha";
import { useVoz } from "./proveedor";

/**
 * ¿El cliente que llama tiene saldo vencido? Solo viaja en el detalle de la llamada
 * (`GET /llamadas/{id}`), así que se pide al sonar. `true` solo si el servidor lo dice: ausente o
 * sin leer no se muestra nada (nunca «al día»).
 */
function useSaldoVencido(llamada: Llamada | null): boolean {
    const id = llamada?.contacto.tipo === "cliente" ? llamada.id : undefined;
    const [leido, setLeido] = useState<{ id: string; vencido: boolean } | null>(null);
    useEffect(() => {
        if (id === undefined) return;
        const control = new AbortController();
        leerLlamada(id, control.signal).then(
            (l) => !control.signal.aborted && setLeido({ id, vencido: l.senales?.saldoVencido === true }),
            () => undefined,
        );
        return () => control.abort();
    }, [id]);
    return leido !== null && leido.id === id && leido.vencido;
}

const hrefConversacion = (id: number) => `/conversaciones?c=${id}`;

/** Servicio y señales del cliente (saldo vencido, ticket abierto), solo lo que manda el servidor en la llamada. */
function Senales({ llamada, saldoVencido }: { llamada: Llamada; saldoVencido: boolean }) {
    const servicio = llamada.senales?.servicio;
    const ticket = llamada.senales?.ticketAbierto;
    if (!servicio && !ticket && !saldoVencido) return null;
    return (
        <ul aria-label="Datos del cliente" className="flex min-w-0 flex-wrap gap-1.5">
            {servicio ? (
                <li className="min-w-0">
                    <InsigniaEstado tono={servicio.estado === "suspendido" ? "warning" : "neutral"} texto={`${servicio.plan} · ${servicio.estado}`} className="max-w-full [&>span]:truncate" />
                </li>
            ) : null}
            {saldoVencido ? (
                <li>
                    <InsigniaEstado tono="warning" icono={Banknote} texto="Saldo vencido" data-slot="saldo-vencido" />
                </li>
            ) : null}
            {ticket ? (
                <li className="min-w-0">
                    <InsigniaEstado tono="info" icono={LifeBuoy} texto={`Ticket ${ticket.numero}`} title={ticket.asunto} className="max-w-full [&>span]:truncate" />
                </li>
            ) : null}
        </ul>
    );
}

const PASO: Record<Extract<EstadoVoz, { fase: "contestando" }>["paso"], string> = {
    reclamando: "Contestando…",
    microfono: "Abriendo el micrófono…",
    negociando: "Conectando el audio…",
    confirmando: "Conectando el audio…",
};

/**
 * Aviso de llamada entrante: aparece en cualquier pantalla del panel, con quién llama (o «Número
 * desconocido»), a qué número, su servicio y «Contestar» / «Rechazar». Mientras conecta dice en qué
 * paso va; si no se pudo (otro la tomó, venció el tiempo, sin micrófono), lo dice en llano.
 */
export function AvisoLlamada() {
    const voz = useVoz();
    const router = useRouter();
    const estado = voz?.estado;
    const llamada = estado && (estado.fase === "sonando" || estado.fase === "contestando") ? estado.llamada : null;
    const saldoVencido = useSaldoVencido(llamada);
    const { puede } = useSesion();
    const [paraTodos, setParaTodos] = useState(false);
    const cerrar = voz?.cerrar;
    const esAviso = estado?.fase === "aviso";
    // El mensaje llano se va solo: no pide hacer nada.
    useEffect(() => {
        if (!esAviso || !cerrar) return;
        const t = window.setTimeout(cerrar, 8000);
        return () => window.clearTimeout(t);
    }, [esAviso, cerrar]);

    if (!voz || !estado) return null;
    const marco = "fixed inset-x-2 top-[calc(env(safe-area-inset-top)+4rem)] z-50 mx-auto grid w-auto max-w-sm gap-3 rounded-xl border p-3.5 shadow-lg sm:inset-x-auto sm:right-4 sm:w-[22rem]";

    if (estado.fase === "aviso") {
        const t = textoAviso(estado.motivo, estado.quien);
        return (
            <section role="status" aria-label="Llamada" data-slot="aviso-llamada" data-fase="aviso" data-motivo={estado.motivo} className={cn(marco, "border-border bg-card")}>
                <div className="flex min-w-0 items-start gap-2.5">
                    <PhoneOff aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <div className="grid min-w-0 flex-1 gap-0.5">
                        <p className="text-sm font-semibold">{t.titulo}</p>
                        <p className="text-[0.8125rem] text-muted-foreground">{estado.detalle ?? t.detalle}</p>
                    </div>
                    <Boton variante="fantasma" tamano="icono-sm" aria-label="Cerrar" onClick={voz.cerrar}>
                        <X />
                    </Boton>
                </div>
            </section>
        );
    }
    if (!llamada) return null;
    const quien = quienLlama(llamada);
    const sonando = estado.fase === "sonando";
    return (
        <section
            role="alertdialog"
            aria-label={`Llamada entrante de ${quien.nombre}`}
            data-slot="aviso-llamada"
            data-fase={estado.fase}
            data-timbre={voz.timbre ? "sonando" : undefined}
            className={cn(marco, "border-primary/50 bg-card ring-2 ring-primary/25")}
        >
            <div className="flex min-w-0 items-center gap-3">
                <span aria-hidden className={cn("flex size-11 shrink-0 items-center justify-center rounded-full bg-primary/15 text-primary ring-2 ring-primary/40", sonando && "motion-safe:animate-pulse")}>
                    {quien.conocido ? <PhoneIncoming className="size-5" /> : <UserRound className="size-5" />}
                </span>
                <div className="grid min-w-0 flex-1">
                    <p className="truncate text-[0.9375rem] font-semibold">{quien.nombre}</p>
                    <p className="truncate text-[0.8125rem] text-muted-foreground">Llamada de WhatsApp · {llamada.numero}</p>
                    {llamada.canal ? <p className="truncate text-xs text-muted-foreground">Al número {llamada.canal.nombre}</p> : null}
                </div>
            </div>
            <Senales llamada={llamada} saldoVencido={saldoVencido} />
            {sonando && voz.microfono !== "concedido" ? (
                // El permiso aún no está dado en este navegador: que la pregunta no tome por sorpresa.
                <p className="flex items-start gap-1.5 text-xs text-muted-foreground" data-slot="aviso-permiso">
                    <Mic aria-hidden className="mt-0.5 size-3.5 shrink-0" />
                    Al contestar, el navegador te va a pedir permiso para usar el micrófono: acéptalo para poder hablar.
                </p>
            ) : null}
            {sonando ? (
                <div className="grid grid-cols-2 gap-2">
                    <Boton variante="peligro" onClick={voz.rechazar} disabled={!llamada.puede.rechazar}>
                        <PhoneOff />
                        Rechazar
                    </Boton>
                    <Boton
                        onClick={() => {
                            voz.contestar();
                            // La ficha del cliente a la vista: se abre su conversación, salvo que
                            // estés llenando un formulario (no se pierde lo escrito; queda «Ver cliente»).
                            const id = llamada.conversacionId;
                            const yaAhi = window.location.pathname === "/conversaciones" && new URLSearchParams(window.location.search).get("c") === String(id);
                            if (id !== undefined && !yaAhi && !document.querySelector('[data-slot="formulario-pagina"]')) router.push(hrefConversacion(id));
                        }}
                    >
                        <Phone />
                        Contestar
                    </Boton>
                </div>
            ) : (
                <Cargando texto={estado.fase === "contestando" ? PASO[estado.paso] : "Conectando…"} visible />
            )}
            {sonando && puede(["conversaciones.supervisar"]) && puede(["llamadas.rechazar"]) ? (
                // Quien supervisa: que deje de sonarles a todos (queda rechazada).
                <Boton variante="enlace" className="justify-self-start text-xs text-muted-foreground" onClick={() => setParaTodos(true)}>
                    Rechazar para todos
                </Boton>
            ) : null}
            <DialogoConfirmar
                open={paraTodos}
                onOpenChange={setParaTodos}
                titulo="¿Rechazar la llamada para todos?"
                descripcion={`Deja de sonarles a todas las personas y ${quien.conocido ? quien.nombre : "quien llama"} oye que no se le contestó. Queda como rechazada en la conversación.`}
                textoConfirmar="Rechazar para todos"
                textoCancelar="Dejar que suene"
                peligro
                alConfirmar={async () => {
                    await voz.rechazarParaTodos();
                    setParaTodos(false);
                }}
            />
        </section>
    );
}

/** Segundos desde `desde`, al segundo. */
function useTranscurrido(desde: number | null): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        if (desde === null) return;
        const id = window.setInterval(() => setAhora(Date.now()), 1000);
        return () => window.clearInterval(id);
    }, [desde]);
    return desde === null ? 0 : Math.max(0, Math.floor((ahora - desde) / 1000));
}

/**
 * Barra de la llamada en curso: bajo la barra superior, en todas las pantallas del panel. Tiempo,
 * «Silenciar», «Colgar» y el acceso a la conversación (con la ficha del cliente).
 */
export function BarraLlamada() {
    const voz = useVoz();
    const ruta = usePathname();
    const params = useSearchParams();
    const estado = voz?.estado;
    const enCurso = estado?.fase === "en_curso" ? estado : null;
    const segundos = useTranscurrido(enCurso?.desde ?? null);
    if (!voz || !enCurso) return null;
    const { llamada, silenciada } = enCurso;
    const quien = quienLlama(llamada);
    const enSuConversacion = llamada.conversacionId !== undefined && ruta === "/conversaciones" && params.get("c") === String(llamada.conversacionId);
    return (
        <section
            aria-label="Llamada en curso"
            data-slot="barra-llamada"
            data-silenciada={silenciada || undefined}
            className="sticky top-14 z-[15] flex h-[3.25rem] min-w-0 items-center gap-2 border-b border-primary/40 bg-[color-mix(in_oklab,var(--hx-primary)_12%,var(--hx-bg))] px-3 sm:gap-3 sm:px-4 lg:px-6"
        >
            <span aria-hidden className="size-2 shrink-0 rounded-full bg-success motion-safe:animate-pulse" />
            <PhoneCall aria-hidden className="size-4 shrink-0 text-primary" />
            <p className="min-w-0 flex-1 truncate text-sm">
                <span className="font-semibold">
                    <span className="hidden sm:inline">En llamada con </span>
                    {quien.conocido ? quien.nombre : llamada.numero}
                </span>
                <span className="sr-only">, </span>
                <span role="timer" aria-label="Tiempo de la llamada" className="ml-2 font-mono text-[0.8125rem] text-muted-foreground tabular-nums">
                    {reloj(segundos)}
                </span>
            </p>
            {llamada.conversacionId !== undefined && !enSuConversacion ? (
                <Boton asChild variante="fantasma" tamano="sm" className="px-2 sm:px-3">
                    <Link href={hrefConversacion(llamada.conversacionId)} aria-label="Ver cliente">
                        <UserRound className="sm:hidden" />
                        <span className="hidden sm:inline">Ver cliente</span>
                    </Link>
                </Boton>
            ) : null}
            <Boton variante={silenciada ? "primario" : "secundario"} tamano="sm" aria-pressed={silenciada} aria-label={silenciada ? "Activar micrófono" : "Silenciar"} onClick={() => voz.silenciar(!silenciada)}>
                {silenciada ? <MicOff /> : <Mic />}
                <span className="hidden sm:inline">{silenciada ? "Silenciada" : "Silenciar"}</span>
            </Boton>
            <Boton variante="peligro" tamano="sm" onClick={voz.colgar} disabled={!llamada.puede.colgar}>
                <PhoneOff />
                Colgar
            </Boton>
        </section>
    );
}

/**
 * Al terminar la llamada: una nota corta (queda en el hilo como nota interna) y los accesos a
 * «Crear ticket» y «Agendar visita». Nada es obligatorio: «Ahora no» cierra.
 */
export function NotaLlamada() {
    const voz = useVoz();
    const estado = voz?.estado;
    const terminada = estado?.fase === "terminada" ? estado : null;
    const [ticket, setTicket] = useState<{ id: number; titulo: string } | null>(null);
    if (!voz) return null;
    return (
        <>
            {terminada ? <FormularioNota key={terminada.llamada.id} terminada={terminada} alTicket={setTicket} /> : null}
            {ticket && !terminada ? <DialogoTicket conv={{ id: ticket.id, titulo: ticket.titulo }} cerrar={() => setTicket(null)} alHecho={() => undefined} /> : null}
        </>
    );
}

function FormularioNota({ terminada, alTicket }: { terminada: Extract<EstadoVoz, { fase: "terminada" }>; alTicket: (c: { id: number; titulo: string }) => void }) {
    const voz = useVoz();
    const { puede } = useSesion();
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const { llamada, duracionSeg, corte } = terminada;
    const [nota, setNota] = useState("");
    const [resultado, setResultado] = useState<ResultadoLlamada | "">("");
    const [guardando, setGuardando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    if (!voz) return null;
    const quien = quienLlama(llamada);
    const conversacionId = llamada.conversacionId;
    const puedeAnotar = puede(["llamadas.anotar"]);
    const texto = nota.trim();

    /** Guarda la nota si hay algo escrito. `false`: falló y el diálogo sigue abierto. */
    async function guardar(): Promise<boolean> {
        if (!texto || !puedeAnotar) return true;
        setGuardando(true);
        setError(null);
        try {
            await anotarLlamada(llamada.id, texto, resultado || undefined, clave.actual());
            avisar({ titulo: "Nota de la llamada guardada" });
            return true;
        } catch (e) {
            clave.trasError(e);
            setError(e);
            return false;
        } finally {
            setGuardando(false);
        }
    }
    async function seguir(despues: () => void) {
        if (await guardar()) {
            voz?.cerrar();
            despues();
        }
    }
    const errorNota = error instanceof ErrorApi ? error.errorDeCampo("nota") : undefined;
    return (
        <Dialogo open onOpenChange={(a) => !a && !guardando && voz.cerrar()}>
            <DialogoContenido
                titulo="Terminó la llamada"
                descripcion={`${quien.conocido ? quien.nombre : llamada.numero} · duró ${reloj(duracionSeg)}`}
                data-slot="nota-llamada"
            >
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={(e) => {
                        e.preventDefault();
                        void seguir(() => undefined);
                    }}
                >
                    {corte ? (
                        <Aviso tono="warning" titulo="La llamada se cortó">
                            Se perdió la conexión del audio. Si hace falta, escríbele al cliente.
                        </Aviso>
                    ) : null}
                    {voz.errorColgar ? (
                        <Aviso
                            tono="error"
                            titulo="No se pudo avisar que colgaste"
                            accion={
                                <Boton type="button" variante="secundario" tamano="sm" onClick={voz.reintentarColgar}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        >
                            {mensajeDeError(voz.errorColgar).titulo}. La llamada puede seguir abierta para el cliente.
                        </Aviso>
                    ) : null}
                    {puedeAnotar ? (
                        <>
                            <CampoArea
                                etiqueta="Nota (opcional)"
                                ayuda="Queda en la conversación como nota interna: el cliente no la ve."
                                rows={3}
                                maxLength={2000}
                                value={nota}
                                onChange={(e) => setNota(e.target.value)}
                                error={errorNota}
                            />
                            <CampoSelector
                                etiqueta="¿Cómo quedó? (opcional)"
                                value={resultado}
                                onChange={(e) => setResultado(e.target.value as ResultadoLlamada | "")}
                            >
                                <option value="">Sin indicar</option>
                                {RESULTADOS.map((r) => (
                                    <option key={r.valor} value={r.valor}>
                                        {r.texto}
                                    </option>
                                ))}
                            </CampoSelector>
                        </>
                    ) : null}
                    {error && !errorNota ? (
                        <Aviso tono="error" titulo="No se pudo guardar la nota">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    {conversacionId !== undefined && (puede(["tickets.crear"]) || puede(["ordenes.crear"])) ? (
                        <div className="flex flex-wrap gap-2" aria-label="Qué sigue">
                            {puede(["tickets.crear"]) ? (
                                <Boton type="button" variante="contorno" tamano="sm" disabled={guardando} onClick={() => void seguir(() => alTicket({ id: conversacionId, titulo: llamada.titulo }))}>
                                    <LifeBuoy />
                                    Crear ticket
                                </Boton>
                            ) : null}
                            {puede(["ordenes.crear"]) ? (
                                <Boton type="button" variante="contorno" tamano="sm" disabled={guardando} onClick={() => void seguir(() => router.push(`/conversaciones/${conversacionId}/orden`))}>
                                    <CalendarPlus />
                                    Agendar visita
                                </Boton>
                            ) : null}
                        </div>
                    ) : null}
                    <DialogoPie>
                        <Boton type="button" variante="contorno" disabled={guardando} onClick={voz.cerrar}>
                            {puedeAnotar ? "Ahora no" : "Cerrar"}
                        </Boton>
                        {puedeAnotar ? (
                            <Boton type="submit" disabled={guardando || !texto}>
                                {guardando ? "Guardando…" : "Guardar nota"}
                            </Boton>
                        ) : null}
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
