"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Esqueleto, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Archive, Clock, ExternalLink, History, IdCard, Mail, Map as IconoMapa, MapPin, Network, Phone, PlugZap, RefreshCw, Router, UserRound, Wifi } from "lucide-react";
import { useState, type ReactNode } from "react";
import { BotonVerEn } from "@/componentes/enlaces-externos";
import { ErrorLectura } from "@/componentes/error-lectura";
import { useAhora } from "@/componentes/tickets/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { desactualizado, etiquetaDocumento, haceTiempo, presentacionEstado, TIPOS_CLIENTE, TIPOS_CONEXION } from "@/lib/datos/presentacion";
import { telefonos as partirTelefonos } from "@/lib/datos/telefono";
import type { EnlaceVer } from "@/lib/enlaces";
import { formatearFecha } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { enlaceMapa, textoCoordenadas } from "@/lib/ordenes/adjuntos";
import { useSesion } from "@/lib/sesion";
import { ListaTelefonos } from "./telefonos";

export { ListaTelefonos };

/*
 * «Datos del cliente / servicio»: el único componente que muestra lo que Iris sabe de Argos de un
 * cliente y su servicio (ficha, ticket, orden y comercial), siempre con el mismo formato:
 * teléfonos +502 con enlace para llamar, documento fiscal, dirección del cliente separada de la
 * de instalación del servicio, coordenadas, zona, plan, estado, IP y router, y cuándo se leyó de
 * Argos con «Refrescar». Si la ficha no se puede leer, muestra la copia guardada al crear el
 * ticket o la orden (o el contacto capturado), nunca datos inventados.
 */

type Ficha = Schema<"Ficha">;
type ClienteArgos = Schema<"ClienteResumen">;
type ServicioArgos = Schema<"Servicio">;
export type CopiaArgos = Schema<"CopiaArgos">;

/** Lo mínimo que se muestra de un cliente: la ficha trae todo; la copia, solo lo operativo. */
type ClienteVista = Pick<ClienteArgos, "id" | "codigo" | "nombre" | "direccion" | "lat" | "lon" | "zonaNombre" | "estado"> &
    Partial<Pick<ClienteArgos, "tipo" | "tipoDocumentoFiscal" | "nitCuiMostrar" | "correo" | "telefonos">>;
type ServicioVista = Pick<ServicioArgos, "id" | "planNombre" | "estado" | "direccion" | "lat" | "lon" | "ip" | "routerNombre"> &
    Partial<Pick<ServicioArgos, "userPpp" | "tipoConexion">>;

/** El cliente de una copia, con su teléfono principal (la copia no guarda los demás). */
function clienteDeCopia(copia: CopiaArgos): ClienteVista {
    return { ...copia.cliente, telefonos: copia.telefono ? [copia.telefono] : [] };
}

/** Correo con forma de correo (para no armar un `mailto:` con lo que venga). */
const CORREO = /^[^\s@<>()"',;:]+@[^\s@<>()"',;:]+\.[^\s@<>()"',;:]+$/;

/** Contacto capturado a mano (ticket u orden): sin correo en las órdenes. */
export type ContactoCapturado = { nombre: string | null; telefono: string | null; correo?: string | null };

export type OrigenDatos = "ticket" | "orden" | "prospecto";

const DEL_ORIGEN: Record<OrigenDatos, { el: string; servicio: string }> = {
    ticket: { el: "el ticket", servicio: "Servicio del ticket" },
    orden: { el: "la orden", servicio: "Servicio de la orden" },
    prospecto: { el: "el alta", servicio: "Servicio del alta" },
};

/** Por qué «Refrescar» no llamó a Argos (la API lo dice en `refrescoOmitido`). */
const NO_REFRESCADO: Record<NonNullable<Ficha["refrescoOmitido"]>, string> = {
    cuota: "No se refrescó: hubo demasiadas consultas a Argos en el último minuto. Intenta en un momento.",
    reciente: "Ya estaba al día: se leyó de Argos hace menos de 30 segundos.",
    en_espera: "No se refrescó: Argos falló hace menos de un minuto. Sigue la última copia guardada.",
};

const ERRORES_CUENTA: Record<NonNullable<Ficha["estadoCuentaError"]>, string> = {
    sin_permiso: "No tienes permiso para ver el estado de cuenta.",
    argos_no_disponible: "Argos no respondió: el estado de cuenta no está disponible ahora.",
    argos_rechazo: "Argos no entregó el estado de cuenta.",
};

export type PropsDatosClienteServicio = {
    clienteId: number;
    servicioId: number | null;
    /** Copia guardada al crear el ticket o la orden (`datosArgos` del detalle). */
    copia?: CopiaArgos | null;
    /** Contacto capturado: se muestra si no hay ficha ni copia. */
    contacto?: ContactoCapturado;
    origen: OrigenDatos;
    verArgos?: EnlaceVer | null;
    /** Mostrar el estado de cuenta (ticket y comercial; en la orden no hace falta). */
    conEstadoCuenta?: boolean;
};

/** Ficha de Argos (`GET /clientes/{id}`) con el formato único de «Datos del cliente / servicio». */
export function DatosClienteServicio({ clienteId, servicioId, copia, contacto, origen, verArgos = null, conEstadoCuenta = true }: PropsDatosClienteServicio) {
    const ficha = useLectura((signal) => resolver(api().GET("/api/v1/clientes/{id}", { params: { path: { id: clienteId } }, signal })));
    const [refresco, setRefresco] = useState<{ cargando: boolean; error: string | null }>({ cargando: false, error: null });
    const ahora = useAhora(60_000);
    const del = DEL_ORIGEN[origen];

    async function refrescar() {
        setRefresco({ cargando: true, error: null });
        try {
            const datos = await resolver(api().GET("/api/v1/clientes/{id}", { params: { path: { id: clienteId }, query: { refrescar: true } } }));
            ficha.reemplazar(datos);
            setRefresco({ cargando: false, error: datos.refrescoOmitido ? NO_REFRESCADO[datos.refrescoOmitido] : datos.stale ? "Argos no respondió: sigue la última copia guardada." : null });
        } catch (e) {
            const m = mensajeDeError(e);
            setRefresco({ cargando: false, error: `No se pudo refrescar: ${m.detalle ?? m.titulo}` });
        }
    }

    const encabezado = (
        <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
            <TarjetaTitulo>Cliente</TarjetaTitulo>
            {verArgos ? <BotonVerEn enlace={verArgos} /> : null}
        </TarjetaEncabezado>
    );

    if (ficha.estado === "cargando") {
        return (
            <Tarjeta aria-busy="true" data-testid="ficha-cliente">
                {encabezado}
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
            <Tarjeta data-testid="ficha-cliente">
                {encabezado}
                <TarjetaContenido className="grid gap-3 text-sm">
                    {apagado ? (
                        <Aviso tono="neutral" titulo="Conector con Argos apagado" icono={PlugZap}>
                            No se puede leer la ficha del cliente {clienteId}. {copia ? `Se muestran los datos que se copiaron al crear ${del.el}.` : `Se muestran los datos capturados en ${del.el}.`}
                        </Aviso>
                    ) : (
                        <ErrorLectura error={ficha.error} que="la ficha del cliente" reintentar={ficha.reintentar} />
                    )}
                    {copia?.anonimizada ? (
                        <p className="text-muted-foreground">Argos eliminó a este cliente: la copia guardada al crear {del.el} ya no tiene sus datos.</p>
                    ) : copia ? (
                        <VistaDatos
                            cliente={clienteDeCopia(copia)}
                            servicios={copia.servicio ? [copia.servicio] : []}
                            tituloServicio={del.servicio}
                            pie={<PieCopia copia={copia} el={del.el} />}
                        />
                    ) : contacto ? (
                        <Contacto contacto={contacto} vacio={`Sin datos de contacto en ${del.el}.`} />
                    ) : null}
                </TarjetaContenido>
            </Tarjeta>
        );
    }

    const { cliente, servicios, serviciosEliminados, estadoCuenta, estadoCuentaError, stale, sincronizadoEn } = ficha.datos;
    const lista = servicios ?? [];
    const delServicio = servicioId !== null ? lista.find((s) => s.id === servicioId) : undefined;
    const eliminado = servicioId !== null && !delServicio ? (serviciosEliminados ?? []).find((s) => s.id === servicioId) : undefined;
    // El servicio eliminado en Argos se muestra con la copia (si la hay) y marcado.
    const copiaEliminado = eliminado && copia?.servicio?.id === servicioId ? copia.servicio : undefined;
    const mostrados: readonly ServicioVista[] = delServicio ? [delServicio] : copiaEliminado ? [copiaEliminado] : eliminado ? [] : lista;
    const viejo = desactualizado(sincronizadoEn, ahora);
    return (
        <Tarjeta data-testid="ficha-cliente">
            {encabezado}
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
                {eliminado ? (
                    <Aviso tono="warning" titulo="Servicio eliminado en Argos" icono={Archive} data-testid="servicio-eliminado">
                        El servicio {eliminado.planNombre ? `${eliminado.planNombre} ` : ""}#{eliminado.id} ya no existe en Argos (eliminado el {formatearFecha(eliminado.eliminadoEn) ?? "—"}).
                        {copiaEliminado ? ` Se muestran los datos que se copiaron al crear ${del.el}.` : ""}
                    </Aviso>
                ) : null}
                <VistaDatos
                    cliente={cliente}
                    servicios={mostrados}
                    tituloServicio={delServicio || copiaEliminado ? del.servicio : "Servicios"}
                    pie={
                        <div className="grid gap-2 border-t pt-3">
                            <div className="flex flex-wrap items-center justify-between gap-2">
                                <span
                                    className={viejo || stale ? "inline-flex items-center gap-1.5 text-warning" : "inline-flex items-center gap-1.5 text-muted-foreground"}
                                    title={formatearFecha(sincronizadoEn)}
                                    data-testid="sincronizado"
                                >
                                    <Clock aria-hidden className="size-4" />
                                    Sincronizado con Argos {haceTiempo(sincronizadoEn, ahora)}
                                </span>
                                <Boton variante="fantasma" tamano="sm" onClick={() => void refrescar()} disabled={refresco.cargando}>
                                    <RefreshCw aria-hidden className={refresco.cargando ? "animate-spin motion-reduce:animate-none" : undefined} />
                                    {refresco.cargando ? "Refrescando…" : "Refrescar"}
                                </Boton>
                            </div>
                            {refresco.error ? (
                                <p role="status" className="text-warning">
                                    {refresco.error}
                                </p>
                            ) : null}
                        </div>
                    }
                />
                {conEstadoCuenta ? (
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
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function PieCopia({ copia, el }: { copia: CopiaArgos; el: string }) {
    return (
        <p className="inline-flex items-center gap-1.5 border-t pt-3 text-muted-foreground" data-testid="sincronizado">
            <History aria-hidden className="size-4 shrink-0" />
            Copia guardada al crear {el} ({formatearFecha(copia.copiadoEn) ?? "—"}); Argos la había enviado el {formatearFecha(copia.sincronizadoEn) ?? "—"}.
            {copia.completa ? "" : " Es de antes de los datos completos: puede faltar la IP o el móvil."}
        </p>
    );
}

/** Lo que se muestra de un cliente y sus servicios, venga de la ficha o de una copia: siempre igual. */
export function VistaDatos({ cliente, servicios, tituloServicio, pie }: { cliente: ClienteVista; servicios: readonly ServicioVista[]; tituloServicio: string; pie?: ReactNode }) {
    const estado = presentacionEstado(cliente.estado);
    const tipo = cliente.tipo ? TIPOS_CLIENTE[cliente.tipo] : undefined;
    const tels = cliente.telefonos ?? [];
    return (
        <div className="grid gap-3" data-testid="datos-cliente">
            <div className="grid gap-1">
                <div className="flex flex-wrap items-center gap-2">
                    <span className="text-base font-medium break-words">{cliente.nombre}</span>
                    {estado ? <InsigniaEstado tono={estado.tono} icono={estado.icono} texto={estado.texto} title="Resumen del estado de sus servicios" /> : null}
                </div>
                <span className="text-muted-foreground">
                    {cliente.codigo ? <span className="font-mono">{cliente.codigo}</span> : `Cliente #${cliente.id}`}
                    {tipo ? ` · ${tipo}` : null}
                </span>
            </div>
            <dl className="grid gap-2">
                {cliente.nitCuiMostrar ? (
                    <Fila icono={IdCard} etiqueta={etiquetaDocumento(cliente.tipoDocumentoFiscal ?? "")}>
                        <span className="tabular-nums">
                            {etiquetaDocumento(cliente.tipoDocumentoFiscal ?? "")} {cliente.nitCuiMostrar}
                        </span>
                    </Fila>
                ) : null}
                {tels.length > 0 ? (
                    <Fila icono={Phone} etiqueta="Teléfonos">
                        <ListaTelefonos telefonos={tels} />
                    </Fila>
                ) : null}
                {cliente.correo ? (
                    <Fila icono={Mail} etiqueta="Correo">
                        {CORREO.test(cliente.correo) ? (
                            <a href={`mailto:${encodeURIComponent(cliente.correo)}`} className="break-all text-primary hover:underline">
                                {cliente.correo}
                            </a>
                        ) : (
                            <span className="break-all">{cliente.correo}</span>
                        )}
                    </Fila>
                ) : null}
                <Fila icono={MapPin} etiqueta="Dirección del cliente">
                    <span className="grid">
                        <span className="text-xs text-muted-foreground">Dirección del cliente</span>
                        <span>{cliente.direccion || <span className="text-muted-foreground">Sin dirección en Argos</span>}</span>
                        <Coordenadas lat={cliente.lat} lon={cliente.lon} />
                    </span>
                </Fila>
                {cliente.zonaNombre ? (
                    <Fila icono={IconoMapa} etiqueta="Zona">
                        Zona {cliente.zonaNombre}
                    </Fila>
                ) : null}
            </dl>
            {servicios.length > 0 ? (
                <div className="grid gap-2 border-t pt-3">
                    <span className="text-xs font-medium text-muted-foreground">{tituloServicio}</span>
                    <ul className="grid gap-3">
                        {servicios.map((s) => (
                            <li key={s.id}>
                                <VistaServicio servicio={s} />
                            </li>
                        ))}
                    </ul>
                </div>
            ) : null}
            {pie}
        </div>
    );
}

function VistaServicio({ servicio: s }: { servicio: ServicioVista }) {
    const estado = presentacionEstado(s.estado);
    return (
        <div className="grid gap-1.5" data-testid="datos-servicio">
            <div className="flex flex-wrap items-center gap-2">
                <Wifi aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                <span className="font-medium">{s.planNombre || "Sin plan"}</span>
                {estado ? <InsigniaEstado tono={estado.tono} icono={estado.icono} texto={estado.texto} /> : null}
                <span className="text-xs text-muted-foreground tabular-nums">#{s.id}</span>
            </div>
            <dl className="grid gap-1.5 pl-6">
                <Fila icono={MapPin} etiqueta="Dirección de instalación">
                    <span className="grid">
                        <span className="text-xs text-muted-foreground">Dirección de instalación</span>
                        {s.direccion ? <span>{s.direccion}</span> : <span className="text-muted-foreground">Sin dirección propia en Argos: aplica la del cliente</span>}
                        <Coordenadas lat={s.lat} lon={s.lon} />
                    </span>
                </Fila>
                {s.ip || s.userPpp ? (
                    <Fila icono={Network} etiqueta="Conexión">
                        <span className="flex flex-wrap gap-x-3">
                            {s.ip ? <span className="font-mono">{s.ip}</span> : null}
                            {s.userPpp ? <span className="font-mono">{s.userPpp}</span> : null}
                            {TIPOS_CONEXION[s.tipoConexion ?? ""] ? <span className="text-muted-foreground">{TIPOS_CONEXION[s.tipoConexion ?? ""]}</span> : null}
                        </span>
                    </Fila>
                ) : null}
                {s.routerNombre ? (
                    <Fila icono={Router} etiqueta="Router">
                        {s.routerNombre}
                    </Fila>
                ) : null}
            </dl>
        </div>
    );
}

function Fila({ icono: Icono, etiqueta, children }: { icono: typeof Phone; etiqueta: string; children: ReactNode }) {
    return (
        <div className="flex items-start gap-2">
            <dt>
                <Icono aria-hidden className="mt-0.5 size-4 text-muted-foreground" />
                <span className="sr-only">{etiqueta}</span>
            </dt>
            <dd className="min-w-0 break-words">{children}</dd>
        </div>
    );
}

function Coordenadas({ lat, lon }: { lat: number | null; lon: number | null }) {
    if (lat === null || lon === null) return null;
    const mapa = enlaceMapa(lat, lon);
    const texto = textoCoordenadas(lat, lon);
    return mapa ? (
        <a href={mapa} target="_blank" rel="noopener noreferrer" className="inline-flex w-fit items-center gap-1 text-xs text-primary tabular-nums hover:underline">
            {texto}
            <ExternalLink aria-hidden className="size-3" />
        </a>
    ) : (
        <span className="text-xs tabular-nums">{texto}</span>
    );
}

/** Contacto capturado a mano (ticket u orden), con el teléfono en el mismo formato que los de Argos. */
export function Contacto({ contacto, vacio }: { contacto: ContactoCapturado; vacio: string }) {
    const tels = partirTelefonos("telefono", contacto.telefono);
    if (!contacto.nombre && tels.length === 0 && !contacto.correo) {
        return <p className="text-sm text-muted-foreground">{vacio}</p>;
    }
    return (
        <dl className="grid gap-2 text-sm">
            {contacto.nombre ? (
                <Fila icono={UserRound} etiqueta="Nombre">
                    {contacto.nombre}
                </Fila>
            ) : null}
            {tels.length > 0 ? (
                <Fila icono={Phone} etiqueta="Teléfono">
                    <ListaTelefonos telefonos={tels} />
                </Fila>
            ) : null}
            {contacto.correo ? (
                <Fila icono={Mail} etiqueta="Correo">
                    {contacto.correo}
                </Fila>
            ) : null}
        </dl>
    );
}

/** Con permiso `clientes.ver`, la ficha; sin él, la copia (si la API la entregó) o el contacto capturado. */
export function DatosClienteServicioConPermiso(props: PropsDatosClienteServicio & { contacto: ContactoCapturado }) {
    const { puede } = useSesion();
    const del = DEL_ORIGEN[props.origen];
    if (puede(["clientes.ver"])) {
        return <DatosClienteServicio {...props} />;
    }
    return (
        <Tarjeta data-testid="ficha-cliente">
            <TarjetaEncabezado>
                <TarjetaTitulo>{`Cliente ${props.clienteId} de Argos`}</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido>
                <Contacto contacto={props.contacto} vacio={`No tienes permiso para ver la ficha del cliente. Sin datos de contacto en ${del.el}.`} />
            </TarjetaContenido>
        </Tarjeta>
    );
}
