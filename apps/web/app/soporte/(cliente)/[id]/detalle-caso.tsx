"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, CampoArea, cn, Esqueleto, EstadoVacio, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { CircleCheck, Info, MessageSquare, Plus, RefreshCw, SearchX, Send } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { PieLista } from "@/componentes/pie-lista";
import { ErrorEnvio } from "@/componentes/portal/error-envio";
import { ErrorPortal } from "@/componentes/portal/error-portal";
import { AvisoFallaGeneral, InsigniaEstadoPortal } from "@/componentes/portal/insignias";
import { VolverACasos } from "@/componentes/portal/volver";
import { resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { formatearFecha } from "@/lib/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura, type Lectura } from "@/lib/lectura";
import { usePaginado, type Paginado } from "@/lib/paginado";
import { apiPortal } from "@/lib/portal/cliente";
import { esquemaResponderPortal, type DatosResponderPortal } from "@/lib/portal/esquemas";
import { ESTADOS_PORTAL, type TicketPortal } from "@/lib/portal/presentacion";

type Mensaje = Schema<"MensajePortal">;

const PAGINA = 100;

export function DetalleCaso() {
    const params = useParams<{ id: string }>();
    const id = Number(params.id);
    if (!Number.isSafeInteger(id) || id < 1) {
        return <NoEncontrado />;
    }
    return <Detalle key={id} id={id} />;
}

function NoEncontrado() {
    return (
        <>
            <VolverACasos />
            <Tarjeta>
                <EstadoVacio
                    icono={SearchX}
                    titulo="No encontramos este caso"
                    descripcion="Puede que el enlace esté mal escrito o que el caso no sea de tu cuenta."
                    accion={
                        <Boton asChild variante="secundario">
                            <Link href="/soporte">Ver mis casos</Link>
                        </Boton>
                    }
                />
            </Tarjeta>
        </>
    );
}

function Detalle({ id }: { id: number }) {
    const ticket: Lectura<TicketPortal> = useLectura((signal) => resolver(apiPortal().GET("/api/v1/portal/tickets/{id}", { params: { path: { id } }, signal })));
    const mensajes = usePaginado<Mensaje>((cursor, signal) =>
        resolver(
            apiPortal().GET("/api/v1/portal/tickets/{id}/mensajes", {
                params: { path: { id }, query: { limit: PAGINA, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const [conflicto, setConflicto] = useState(false);
    const [recargando, setRecargando] = useState(false);

    async function recargar() {
        setRecargando(true);
        await Promise.all([ticket.refrescar(), mensajes.refrescar()]);
        setConflicto(false);
        setRecargando(false);
    }

    if (ticket.estado === "cargando") {
        return (
            <>
                <VolverACasos />
                <div className="grid gap-4" aria-busy="true">
                    <Esqueleto className="h-28" />
                    <Esqueleto className="h-48" />
                </div>
            </>
        );
    }
    if (ticket.estado === "error") {
        if (ticket.error instanceof ErrorApi && ticket.error.estado === 404) {
            return <NoEncontrado />;
        }
        return (
            <>
                <VolverACasos />
                <ErrorPortal error={ticket.error} que="tu caso" reintentar={ticket.reintentar} />
            </>
        );
    }
    const t = ticket.datos;
    const estado = ESTADOS_PORTAL[t.estado];

    return (
        <>
            <VolverACasos />
            <div className="grid gap-4">
                <Tarjeta className="grid gap-2 p-5" data-testid="cabecera-caso">
                    <div className="flex flex-wrap items-center gap-2">
                        <InsigniaEstadoPortal estado={t.estado} />
                        <span className="font-mono text-xs text-muted-foreground">{t.numero}</span>
                    </div>
                    <h1 className="text-xl font-semibold tracking-[-0.015em] break-words">{t.asunto}</h1>
                    <p className="text-sm text-muted-foreground">{estado.explicacion}</p>
                    <p className="text-xs text-muted-foreground tabular-nums">
                        {t.categoria ? `${t.categoria} · ` : ""}Abierto el {formatearFecha(t.creadoEn)}
                    </p>
                </Tarjeta>

                {conflicto ? (
                    <Aviso
                        tono="warning"
                        titulo="Tu caso cambió mientras lo veías"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={() => void recargar()} disabled={recargando}>
                                <RefreshCw />
                                {recargando ? "Recargando…" : "Recargar"}
                            </Boton>
                        }
                    >
                        Nuestro equipo actualizó tu caso. Recarga para ver lo último; lo que escribiste no se pierde.
                    </Aviso>
                ) : null}

                {t.afectadoPorIncidente ? <AvisoFallaGeneral /> : null}

                {t.puedeConfirmar ? <Confirmar ticket={t} alConfirmar={ticket.reemplazar} alConflicto={() => setConflicto(true)} /> : null}

                <Conversacion paginado={mensajes} />

                {t.estado === "cerrado" ? (
                    <Tarjeta>
                        <EstadoVacio
                            icono={CircleCheck}
                            titulo="Este caso está cerrado"
                            descripcion="Si necesitas más ayuda, abre un caso nuevo y con gusto te atendemos."
                            className="py-8"
                            accion={
                                <Boton asChild variante="secundario">
                                    <Link href="/soporte/nuevo">
                                        <Plus />
                                        Reportar un problema
                                    </Link>
                                </Boton>
                            }
                        />
                    </Tarjeta>
                ) : (
                    <Responder
                        ticket={t}
                        alResponder={(r) => {
                            ticket.reemplazar(r.ticket);
                            if (!mensajes.siguiente) {
                                mensajes.agregar(r.mensaje);
                            }
                        }}
                        alConflicto={() => setConflicto(true)}
                    />
                )}
            </div>
        </>
    );
}

function Confirmar({ ticket, alConfirmar, alConflicto }: { ticket: TicketPortal; alConfirmar: (t: TicketPortal) => void; alConflicto: () => void }) {
    const clave = useClaveIdempotencia();
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);

    async function confirmar() {
        setEnviando(true);
        setError(null);
        try {
            const t = await resolver(
                apiPortal().POST("/api/v1/portal/tickets/{id}/confirmar-solucion", {
                    params: { path: { id: ticket.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: { version: ticket.version },
                }),
            );
            clave.renovar();
            alConfirmar(t);
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && (e.estado === 409 || (e.estado === 422 && e.codigo === "transicion_invalida"))) {
                alConflicto();
            } else {
                setError(e);
            }
        } finally {
            setEnviando(false);
        }
    }

    return (
        <Aviso
            tono="success"
            icono={CircleCheck}
            titulo="¿Quedó resuelto?"
            data-testid="confirmar-solucion"
            accion={
                <Boton onClick={() => void confirmar()} disabled={enviando}>
                    <CircleCheck />
                    {enviando ? "Confirmando…" : "Confirmar que quedó resuelto"}
                </Boton>
            }
        >
            <p>Si todo funciona bien, confírmalo y cerramos tu caso. Si algo sigue mal, respóndenos abajo y lo seguimos atendiendo.</p>
            {error ? <ErrorEnvio error={error} /> : null}
        </Aviso>
    );
}

function Conversacion({ paginado }: { paginado: Paginado<Mensaje> }) {
    const { primera, items } = paginado;
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Conversación</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {primera.estado === "cargando" ? (
                    <>
                        <Esqueleto className="h-16" />
                        <Esqueleto className="ml-auto h-16 w-3/4" />
                    </>
                ) : primera.estado === "error" ? (
                    <ErrorPortal error={primera.error} que="la conversación" reintentar={primera.reintentar} />
                ) : items.length === 0 ? (
                    <EstadoVacio icono={MessageSquare} titulo="Sin mensajes" descripcion="Aún no hay mensajes en este caso." className="py-6" />
                ) : (
                    <ol className="grid gap-3" aria-label="Mensajes">
                        {items.map((m) => (
                            <BurbujaMensaje key={m.id} mensaje={m} />
                        ))}
                    </ol>
                )}
                <PieLista paginado={paginado} que="mensajes" />
            </TarjetaContenido>
        </Tarjeta>
    );
}

function BurbujaMensaje({ mensaje: m }: { mensaje: Mensaje }) {
    const fecha = (
        <time className="text-xs text-muted-foreground tabular-nums" dateTime={m.creadoEn}>
            {formatearFecha(m.creadoEn)}
        </time>
    );
    if (m.autorTipo === "sistema") {
        return (
            <li data-autor="sistema" className="grid gap-1 rounded-lg border border-dashed bg-muted/40 p-3 text-sm">
                <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <Info aria-hidden className="size-4 shrink-0 text-info" />
                    <span className="font-medium">Aviso automático</span>
                    <span className="ml-auto">{fecha}</span>
                </span>
                <p className="break-words whitespace-pre-wrap">{m.cuerpo}</p>
            </li>
        );
    }
    const propio = m.autorTipo === "cliente";
    return (
        <li
            data-autor={m.autorTipo}
            className={cn("grid max-w-[85%] gap-1 rounded-lg border p-3 text-sm sm:max-w-[75%]", propio ? "ml-auto border-primary/30 bg-primary/10" : "mr-auto bg-elevated")}
        >
            <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="font-medium break-all">{propio ? "Tú" : m.autorNombre || "Soporte"}</span>
                {propio ? null : <span className="text-xs text-muted-foreground">Equipo de soporte</span>}
                <span className="ml-auto">{fecha}</span>
            </span>
            <p className="break-words whitespace-pre-wrap">{m.cuerpo}</p>
        </li>
    );
}

function Responder({
    ticket,
    alResponder,
    alConflicto,
}: {
    ticket: TicketPortal;
    alResponder: (r: Schema<"TicketPortalConMensaje">) => void;
    alConflicto: () => void;
}) {
    const clave = useClaveIdempotencia();
    const [errorGeneral, setErrorGeneral] = useState<unknown>(null);
    const form = useForm<{ cuerpo: string }, unknown, DatosResponderPortal>({ resolver: zodResolver(esquemaResponderPortal), defaultValues: { cuerpo: "" } });

    async function enviar(datos: DatosResponderPortal) {
        setErrorGeneral(null);
        try {
            const r = await resolver(
                apiPortal().POST("/api/v1/portal/tickets/{id}/responder", {
                    params: { path: { id: ticket.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: { cuerpo: datos.cuerpo, version: ticket.version },
                }),
            );
            clave.renovar();
            form.reset({ cuerpo: "" });
            alResponder(r);
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.estado === 409) {
                alConflicto();
            } else if (!erroresACampos(e, ["cuerpo"] as const, form.setError)) {
                setErrorGeneral(e);
            }
        }
    }

    return (
        <Tarjeta className="p-5">
            <form noValidate className="grid gap-3" onSubmit={(e) => void form.handleSubmit(enviar)(e)} aria-label="Responder">
                <CampoArea
                    etiqueta="Escribe tu mensaje"
                    rows={4}
                    maxLength={20000}
                    error={form.formState.errors.cuerpo?.message}
                    ayuda={ticket.estado === "resuelto" ? "Si nos escribes, volveremos a abrir tu caso." : undefined}
                    {...form.register("cuerpo")}
                />
                {errorGeneral ? <ErrorEnvio error={errorGeneral} /> : null}
                <div className="flex justify-end">
                    <Boton type="submit" variante={ticket.puedeConfirmar ? "secundario" : "primario"} disabled={form.formState.isSubmitting}>
                        <Send />
                        {form.formState.isSubmitting ? "Enviando…" : "Enviar"}
                    </Boton>
                </div>
            </form>
        </Tarjeta>
    );
}
