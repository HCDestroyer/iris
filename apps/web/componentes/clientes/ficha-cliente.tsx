"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Esqueleto, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { History, Mail, MapPin, Phone, PlugZap, UserRound, Wifi } from "lucide-react";
import { BotonVerEn } from "@/componentes/enlaces-externos";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { enlaceArgosCliente, type EnlacesExternos, type EnlaceVer } from "@/lib/enlaces";
import { formatearFecha } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import type { Ticket } from "@/lib/tickets/sla";

const ERRORES_CUENTA: Record<NonNullable<Schema<"Ficha">["estadoCuentaError"]>, string> = {
    sin_permiso: "No tienes permiso para ver el estado de cuenta.",
    argos_no_disponible: "Argos no respondió: el estado de cuenta no está disponible ahora.",
    argos_rechazo: "Argos no entregó el estado de cuenta.",
};

/** Cliente del ticket: ficha de Argos si hay `clienteId`; si no, el contacto capturado. */
export function FichaCliente({ ticket, enlaces }: { ticket: Ticket; enlaces: EnlacesExternos | null }) {
    const { puede } = useSesion();
    if (ticket.clienteId !== null && puede(["clientes.ver"])) {
        return (
            <FichaArgos
                clienteId={ticket.clienteId}
                servicioId={ticket.servicioId}
                contacto={ticket.contacto}
                verArgos={enlaceArgosCliente(enlaces, ticket.clienteId)}
            />
        );
    }
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>{ticket.clienteId !== null ? `Cliente ${ticket.clienteId} de Argos` : "Contacto"}</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <Contacto contacto={ticket.contacto} vacio={ticket.clienteId !== null ? "No tienes permiso para ver la ficha del cliente." : "Sin datos de contacto."} />
            </TarjetaContenido>
        </Tarjeta>
    );
}

/** Contacto capturado (ticket u orden): sin correo en las órdenes. */
export type ContactoCapturado = { nombre: string | null; telefono: string | null; correo?: string | null };

export function Contacto({ contacto, vacio }: { contacto: ContactoCapturado; vacio: string }) {
    const filas = [
        { icono: UserRound, valor: contacto.nombre, etiqueta: "Nombre" },
        { icono: Phone, valor: contacto.telefono, etiqueta: "Teléfono" },
        { icono: Mail, valor: contacto.correo, etiqueta: "Correo" },
    ].filter((f) => f.valor);
    if (filas.length === 0) {
        return <p className="text-sm text-muted-foreground">{vacio}</p>;
    }
    return (
        <dl className="grid gap-2 text-sm">
            {filas.map((f) => (
                <div key={f.etiqueta} className="flex items-start gap-2">
                    <dt>
                        <f.icono aria-hidden className="mt-0.5 size-4 text-muted-foreground" />
                        <span className="sr-only">{f.etiqueta}</span>
                    </dt>
                    <dd className="min-w-0 break-words">{f.valor}</dd>
                </div>
            ))}
        </dl>
    );
}

/** Ficha del cliente en Argos (`GET /clientes/{id}`), con `stale` visible y el conector apagado explicado. */
export function FichaArgos({
    clienteId,
    servicioId,
    contacto,
    verArgos,
    origen = "ticket",
}: {
    clienteId: number;
    servicioId: number | null;
    contacto: ContactoCapturado;
    verArgos: EnlaceVer | null;
    /** Dónde se capturó el contacto que se muestra si no se puede leer la ficha. */
    origen?: "ticket" | "orden";
}) {
    const ficha = useLectura((signal) => resolver(api().GET("/api/v1/clientes/{id}", { params: { path: { id: clienteId } }, signal })));
    const titulo = (
        <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
            <TarjetaTitulo>Cliente</TarjetaTitulo>
            {verArgos ? <BotonVerEn enlace={verArgos} /> : null}
        </TarjetaEncabezado>
    );

    if (ficha.estado === "cargando") {
        return (
            <Tarjeta aria-busy="true">
                {titulo}
                <TarjetaContenido className="grid gap-2">
                    <span className="sr-only" role="status">
                        Cargando la ficha del cliente…
                    </span>
                    <Esqueleto className="h-5 w-2/3" />
                    <Esqueleto className="h-4" />
                    <Esqueleto className="h-4 w-1/2" />
                </TarjetaContenido>
            </Tarjeta>
        );
    }
    if (ficha.estado === "error") {
        const apagado = ficha.error instanceof ErrorApi && ficha.error.codigo === "conector_apagado";
        return (
            <Tarjeta>
                {titulo}
                <TarjetaContenido className="grid gap-3">
                    {apagado ? (
                        <Aviso tono="neutral" titulo="Conector con Argos apagado" icono={PlugZap}>
                            No se puede leer la ficha del cliente {clienteId}. Se muestran los datos capturados en {origen === "orden" ? "la orden" : "el ticket"}.
                        </Aviso>
                    ) : (
                        <ErrorLectura error={ficha.error} que="la ficha del cliente" reintentar={ficha.reintentar} />
                    )}
                    <Contacto contacto={contacto} vacio={`Sin datos de contacto en ${origen === "orden" ? "la orden" : "el ticket"}.`} />
                </TarjetaContenido>
            </Tarjeta>
        );
    }

    const { cliente, servicios, estadoCuenta, estadoCuentaError, stale, sincronizadoEn } = ficha.datos;
    const servicio = (servicios ?? []).find((s) => s.id === servicioId);
    const lista = servicio ? [servicio] : (servicios ?? []);
    return (
        <Tarjeta data-testid="ficha-cliente">
            {titulo}
            <TarjetaContenido className="grid gap-3 text-sm">
                {stale ? (
                    <InsigniaEstado
                        tono="warning"
                        icono={History}
                        texto={`Desactualizado · ${formatearFecha(sincronizadoEn) ?? "sin fecha"}`}
                        title="Argos no respondió: es la última copia guardada"
                        className="justify-self-start"
                    />
                ) : null}
                <div className="grid gap-0.5">
                    <span className="text-base font-medium">{cliente.nombre}</span>
                    <span className="text-muted-foreground">
                        <span className="font-mono">{cliente.codigo}</span> · {cliente.estado}
                    </span>
                </div>
                <dl className="grid gap-2">
                    {cliente.telefono ? (
                        <div className="flex items-start gap-2">
                            <dt>
                                <Phone aria-hidden className="mt-0.5 size-4 text-muted-foreground" />
                                <span className="sr-only">Teléfono</span>
                            </dt>
                            <dd>{cliente.telefono}</dd>
                        </div>
                    ) : null}
                    {cliente.direccion ? (
                        <div className="flex items-start gap-2">
                            <dt>
                                <MapPin aria-hidden className="mt-0.5 size-4 text-muted-foreground" />
                                <span className="sr-only">Dirección</span>
                            </dt>
                            <dd className="min-w-0 break-words">{cliente.direccion}</dd>
                        </div>
                    ) : null}
                </dl>
                {lista.length ? (
                    <div className="grid gap-2 border-t pt-3">
                        <span className="text-xs font-medium text-muted-foreground">{servicio ? (origen === "orden" ? "Servicio de la orden" : "Servicio del ticket") : "Servicios"}</span>
                        <ul className="grid gap-2">
                            {lista.map((s) => (
                                <li key={s.id} className="flex items-start gap-2">
                                    <Wifi aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                                    <span className="grid min-w-0">
                                        <span className="truncate">
                                            {s.planNombre || "Sin plan"} · {s.estado}
                                        </span>
                                        <span className="truncate text-xs text-muted-foreground">
                                            <span className="font-mono">{s.codigo}</span>
                                            {s.zonaNombre ? ` · ${s.zonaNombre}` : null}
                                            {s.routerNombre ? ` · ${s.routerNombre}` : null}
                                        </span>
                                    </span>
                                </li>
                            ))}
                        </ul>
                    </div>
                ) : null}
                <div className="grid gap-1 border-t pt-3">
                    <span className="text-xs font-medium text-muted-foreground">Estado de cuenta</span>
                    {estadoCuenta ? (
                        <dl className="grid grid-cols-2 gap-x-3 gap-y-1 tabular-nums">
                            <dt className="text-muted-foreground">Saldo</dt>
                            <dd className="text-right">{estadoCuenta.saldo !== null ? `${estadoCuenta.moneda} ${estadoCuenta.saldo}` : "Sin dato"}</dd>
                            <dt className="text-muted-foreground">Facturas vencidas</dt>
                            <dd className="text-right">{estadoCuenta.facturasVencidas ?? "Sin dato"}</dd>
                            {estadoCuenta.montoVencido !== null ? (
                                <>
                                    <dt className="text-muted-foreground">Monto vencido</dt>
                                    <dd className="text-right">
                                        {estadoCuenta.moneda} {estadoCuenta.montoVencido}
                                    </dd>
                                </>
                            ) : null}
                        </dl>
                    ) : (
                        <p className="text-muted-foreground">{estadoCuentaError ? ERRORES_CUENTA[estadoCuentaError] : "No disponible."}</p>
                    )}
                </div>
            </TarjetaContenido>
        </Tarjeta>
    );
}
