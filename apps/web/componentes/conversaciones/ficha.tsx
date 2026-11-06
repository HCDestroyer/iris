"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Aviso,
    avisar,
    Boton,
    BotonEnviar,
    Campo,
    CampoArea,
    CampoFecha,
    CampoSelector,
    Cargando,
    cn,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    InsigniaEstado,
    useCambiosSinGuardar,
} from "@iris/ui";
import { AlertTriangle, Banknote, CalendarClock, ClipboardList, ExternalLink, Link2, LifeBuoy, RotateCw, UserPlus, UserRound, Wrench } from "lucide-react";
import Link from "next/link";
import { useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { api, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import { CLASE_ETIQUETA, formatearNumero, horaLista, iniciales } from "@/lib/conversaciones/presentacion";
import type { Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion, ConversacionDetalle } from "@/lib/conversaciones/tipos";
import { haceCuanto } from "@/lib/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";
import { SelectorCliente } from "@/componentes/clientes/selector-cliente";
import { AyudaManuales } from "./ayuda";
import { useAhora } from "./reloj";

export type PestanaFicha = "resumen" | "historial" | "ayuda";

type Props = {
    conv: ConversacionDetalle;
    identidad: Identidad;
    pestana: PestanaFicha;
    alCambiarPestana: (p: PestanaFicha) => void;
    /** Una acción devolvió la conversación (asociar, contacto…): la bandeja y el detalle se actualizan. */
    alActualizar: (c: Conversacion) => void;
    recargar: () => Promise<void>;
    /** «Usar en la respuesta» (manuales): pasa el texto al compositor. */
    alUsarTexto: (t: string) => void;
};

const BASE = "/api/v1/mensajeria/conversaciones/{id}";

/**
 * Ficha del cliente a la derecha (o en una hoja): datos de Argos (de la instantánea, nunca una
 * copia), las 4 acciones (Ticket, Orden, Boleta, Promesa) y las pestañas Resumen / Historial.
 * Sin cliente: coincidencias por el número con «Asociar», buscar en Argos, crear prospecto o
 * guardar un contacto de Iris.
 */
export function FichaCliente({ conv, identidad, pestana, alCambiarPestana, alActualizar, recargar, alUsarTexto }: Props) {
    const { puede } = useSesion();
    const conCliente = conv.contacto.tipo === "cliente";
    const conAyuda = puede(["manuales.ver"]);
    const actual = pestana === "ayuda" && !conAyuda ? "resumen" : pestana;
    return (
        <div className="flex min-h-0 flex-col" data-slot="ficha-cliente">
            {conCliente ? <CabeceraCliente conv={conv} alActualizar={alActualizar} /> : <SinCliente conv={conv} alActualizar={alActualizar} />}
            <nav aria-label="Secciones de la ficha" className="flex gap-4 border-b border-border px-4">
                {(
                    [
                        ["resumen", "Resumen"],
                        ["historial", "Historial"],
                        ...(conAyuda ? ([["ayuda", "Ayuda"]] as const) : []),
                    ] as const
                ).map(([id, texto]) => (
                    <Boton
                        key={id}
                        variante="fantasma"
                        className={cn(
                            "h-10 rounded-none px-0 text-[0.8125rem] font-semibold hover:bg-transparent",
                            actual === id ? "text-foreground shadow-[inset_0_-2px_0_var(--hx-text)]" : "text-muted-foreground",
                        )}
                        aria-current={actual === id ? "true" : undefined}
                        onClick={() => alCambiarPestana(id)}
                    >
                        {texto}
                    </Boton>
                ))}
            </nav>
            {actual === "historial" ? <Historial conv={conv} /> : actual === "ayuda" ? <AyudaManuales conv={conv} identidad={identidad} alUsarTexto={alUsarTexto} /> : <Resumen conv={conv} recargar={recargar} />}
        </div>
    );
}

function CabeceraCliente({ conv, alActualizar }: { conv: ConversacionDetalle; alActualizar: (c: Conversacion) => void }) {
    const { puede } = useSesion();
    const ahora = useAhora(60_000);
    const r = conv.clienteResumen;
    const servicio = (r?.servicios ?? []).find((s) => s.id === conv.contacto.servicioId) ?? r?.servicios?.[0];
    const [accion, setAccion] = useState<"ticket" | "promesa" | null>(null);
    const nombre = r?.nombre ?? conv.titulo;
    const acciones: { id: string; etiqueta: string; icono: typeof LifeBuoy; permitido: boolean; href?: string; alPresionar?: () => void }[] = [
        { id: "ticket", etiqueta: "Ticket", icono: LifeBuoy, permitido: puede(["tickets.crear"]), alPresionar: () => setAccion("ticket") },
        { id: "orden", etiqueta: "Orden", icono: Wrench, permitido: puede(["ordenes.crear"]), href: `/conversaciones/${conv.id}/orden` },
        { id: "boleta", etiqueta: "Boleta", icono: Banknote, permitido: puede(["conversaciones.registrar_boleta"]), href: `/conversaciones/${conv.id}/boleta` },
        { id: "promesa", etiqueta: "Promesa", icono: CalendarClock, permitido: puede(["conversaciones.registrar_promesa"]), alPresionar: () => setAccion("promesa") },
    ];
    const visibles = acciones.filter((a) => a.permitido);
    return (
        <div className="grid gap-3 border-b border-border p-4">
            <div className="flex min-w-0 items-center gap-2.5">
                <span aria-hidden className="flex size-9 shrink-0 items-center justify-center rounded-full bg-elevated text-[0.8125rem] font-semibold">
                    {iniciales(nombre)}
                </span>
                <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-[0.9375rem] font-semibold">{nombre}</span>
                    <span className="truncate text-[0.78125rem] text-muted-foreground">Cliente de Argos{r?.codigo ? ` · ${r.codigo}` : ""}</span>
                </div>
                {conv.contacto.clienteId ? (
                    <Boton asChild variante="contorno" tamano="icono-sm">
                        <Link href={`/tickets?clienteId=${conv.contacto.clienteId}`} aria-label="Tickets del cliente" title="Tickets del cliente">
                            <ExternalLink />
                        </Link>
                    </Boton>
                ) : null}
            </div>
            {r ? (
                <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[0.8125rem]">
                    {servicio ? (
                        <>
                            <dt className="text-muted-foreground">Servicio</dt>
                            <dd className="truncate text-right">
                                {servicio.plan} · {servicio.codigo}
                            </dd>
                            <dt className="text-muted-foreground">Estado</dt>
                            <dd className="flex justify-end">
                                <InsigniaEstado tono={servicio.estado === "activo" ? "success" : servicio.estado === "suspendido" ? "warning" : "neutral"} texto={servicio.estado} />
                            </dd>
                            <dt className="text-muted-foreground">Zona</dt>
                            <dd className="truncate text-right">{servicio.zona || "—"}</dd>
                        </>
                    ) : (
                        <>
                            <dt className="text-muted-foreground">Servicio</dt>
                            <dd className="text-right text-muted-foreground">Sin servicios en esta empresa</dd>
                        </>
                    )}
                    <dt className="text-muted-foreground">Teléfono</dt>
                    <dd className="truncate text-right tabular-nums">{formatearNumero(conv.numero)}</dd>
                </dl>
            ) : (
                <Aviso tono="warning" titulo="Sin datos de Argos">
                    Aún no hay instantánea de este cliente.
                </Aviso>
            )}
            {visibles.length > 0 ? (
                <div className="grid grid-cols-4 gap-1">
                    {visibles.map((a) => {
                        const Icono = a.icono;
                        const contenido = (
                            <>
                                <span className="flex size-9 items-center justify-center rounded-[0.625rem] bg-elevated">
                                    <Icono aria-hidden className="size-4" />
                                </span>
                                {a.etiqueta}
                            </>
                        );
                        const clase = "flex h-auto min-w-0 flex-col items-center gap-1 rounded-[0.625rem] px-0.5 py-2 text-[0.71875rem] font-semibold";
                        return a.href ? (
                            <Boton key={a.id} asChild variante="fantasma" className={clase}>
                                <Link href={a.href}>{contenido}</Link>
                            </Boton>
                        ) : (
                            <Boton key={a.id} variante="fantasma" className={clase} onClick={a.alPresionar}>
                                {contenido}
                            </Boton>
                        );
                    })}
                </div>
            ) : null}
            {r ? <p className="text-xs text-muted-foreground">Datos de Argos {haceCuanto(r.sincronizadoEn, ahora)}</p> : null}
            {accion === "ticket" ? <DialogoTicket conv={conv} cerrar={() => setAccion(null)} alHecho={alActualizar} /> : null}
            {accion === "promesa" ? <DialogoPromesa conv={conv} cerrar={() => setAccion(null)} /> : null}
        </div>
    );
}

function Seccion({ titulo, accion, children }: { titulo: string; accion?: ReactNode; children: ReactNode }) {
    return (
        <section className="grid gap-2 border-b border-border px-4 py-3">
            <h3 className="flex items-center justify-between text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">
                {titulo}
                {accion}
            </h3>
            {children}
        </section>
    );
}

const ENLACE_VINCULO: Record<string, (id: string) => string> = {
    ticket: (id) => `/tickets/${id}`,
    orden: (id) => `/ordenes/${id}`,
    prospecto: (id) => `/comercial/${id}`,
};

const ICONO_VINCULO: Record<string, typeof LifeBuoy> = { ticket: LifeBuoy, orden: ClipboardList, prospecto: UserPlus, boleta: Banknote, promesa: CalendarClock };

function Vinculo({ v }: { v: NonNullable<ConversacionDetalle["vinculos"]>[number] }) {
    const Icono = ICONO_VINCULO[v.tipo] ?? Link2;
    const href = ENLACE_VINCULO[v.tipo]?.(v.id);
    const ahora = useAhora(60_000);
    return (
        <li className="flex min-w-0 items-center gap-2 text-[0.8125rem]">
            <Icono aria-hidden className="size-4 shrink-0 text-muted-foreground" />
            {href ? (
                <Link href={href} className="min-w-0 truncate font-semibold text-primary hover:underline">
                    {v.referencia}
                </Link>
            ) : (
                <span className="min-w-0 truncate font-semibold">{v.referencia}</span>
            )}
            <span className="ml-auto shrink-0 text-xs text-muted-foreground">{horaLista(v.creadoEn, ahora)}</span>
        </li>
    );
}

function Resumen({ conv }: { conv: ConversacionDetalle; recargar: () => Promise<void> }) {
    const abiertos = (conv.vinculos ?? []).filter((v) => v.tipo === "ticket" || v.tipo === "orden").slice(-3);
    return (
        <div>
            <Seccion titulo="Creado desde aquí">
                {abiertos.length === 0 ? <p className="text-[0.8125rem] text-muted-foreground">Todavía no hay tickets ni órdenes de esta conversación.</p> : null}
                <ul className="grid gap-1.5">
                    {abiertos.map((v) => (
                        <Vinculo key={`${v.tipo}-${v.id}`} v={v} />
                    ))}
                </ul>
            </Seccion>
            <Seccion titulo="Etiquetas">
                {(conv.etiquetas ?? []).length === 0 ? <p className="text-[0.8125rem] text-muted-foreground">Sin etiquetas (se ponen desde «⋯»).</p> : null}
                <div className="flex flex-wrap gap-1.5">
                    {(conv.etiquetas ?? []).map((e) => (
                        <span key={e.id} className={cn("inline-flex h-[1.375rem] max-w-full items-center truncate rounded-md px-2 text-xs font-medium", CLASE_ETIQUETA[e.color] ?? CLASE_ETIQUETA.neutro)}>
                            {e.nombre}
                        </span>
                    ))}
                </div>
            </Seccion>
            <Seccion titulo="Atención">
                <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1.5 text-[0.8125rem]">
                    <dt className="text-muted-foreground">Cola</dt>
                    <dd className="truncate text-right">{conv.cola?.nombre ?? "Sin cola"}</dd>
                    <dt className="text-muted-foreground">Asignada</dt>
                    <dd className="truncate text-right">{conv.asignado?.nombre ?? "Sin asignar"}</dd>
                    {(conv.participantes ?? []).length > 0 ? (
                        <>
                            <dt className="text-muted-foreground">También</dt>
                            <dd className="truncate text-right">{(conv.participantes ?? []).map((p) => p.nombre).join(", ")}</dd>
                        </>
                    ) : null}
                </dl>
            </Seccion>
        </div>
    );
}

function Historial({ conv }: { conv: ConversacionDetalle }) {
    return (
        <Seccion titulo="Creado desde esta conversación">
            {(conv.vinculos ?? []).length === 0 ? <p className="text-[0.8125rem] text-muted-foreground">Nada todavía.</p> : null}
            <ul className="grid gap-1.5">
                {[...(conv.vinculos ?? [])].reverse().map((v) => (
                    <Vinculo key={`${v.tipo}-${v.id}`} v={v} />
                ))}
            </ul>
        </Seccion>
    );
}

/** Dónde coincide el número: el contacto de un servicio no es el titular (contrato §5.1). */
export function textoCoincidencia(en: readonly string[]): string {
    if (en.includes("telefono") || en.includes("movil")) return "teléfono del cliente";
    if (en.includes("contacto_servicio")) return "contacto de un servicio";
    return "";
}

/** Sin cliente asociado: coincidencias por el número, buscar en Argos, prospecto o contacto de Iris. */
function SinCliente({ conv, alActualizar }: { conv: ConversacionDetalle; alActualizar: (c: Conversacion) => void }) {
    const { puede } = useSesion();
    const asocia = puede(["conversaciones.asociar"]);
    const [contacto, setContacto] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const [asociando, setAsociando] = useState<number | null>(null);
    const coincidencias = useLectura(async (signal) => await resolver(api().GET(`${BASE}/coincidencias`, { params: { path: { id: conv.id } }, signal })));

    async function asociar(clienteId: number, servicioId: number | null) {
        setError(null);
        setAsociando(clienteId);
        try {
            const c = await resolver(
                api().POST(`${BASE}/asociar-cliente`, {
                    params: { path: { id: conv.id }, header: cabeceraIdem(nuevaClaveIdempotencia()) },
                    body: { version: conv.version, clienteId, ...(servicioId ? { servicioId } : {}) },
                }),
            );
            avisar({ titulo: `${c.titulo} asociado a la conversación` });
            alActualizar(c);
        } catch (e) {
            setError(e);
        } finally {
            setAsociando(null);
        }
    }

    const titulo = conv.contacto.tipo === "contacto_iris" ? "Contacto de Iris" : conv.contacto.tipo === "prospecto" ? "Prospecto" : null;
    return (
        <div className="grid gap-3 border-b border-border p-4">
            {titulo ? (
                <div className="flex min-w-0 items-center gap-2.5">
                    <span aria-hidden className="flex size-9 shrink-0 items-center justify-center rounded-full bg-elevated text-[0.8125rem] font-semibold">
                        {iniciales(conv.titulo)}
                    </span>
                    <div className="flex min-w-0 flex-col">
                        <span className="truncate text-[0.9375rem] font-semibold">{conv.titulo}</span>
                        <span className="truncate text-[0.78125rem] text-muted-foreground">
                            {titulo} · {formatearNumero(conv.numero)}
                        </span>
                    </div>
                </div>
            ) : (
                <div className="flex items-start gap-2 rounded-[0.625rem] bg-warning/10 p-2.5 text-[0.8125rem]">
                    <AlertTriangle aria-hidden className="mt-0.5 size-4 shrink-0 text-warning" />
                    <span>
                        <b>Sin cliente asociado.</b> {conv.perfilNombre ? `WhatsApp: «${conv.perfilNombre}». ` : ""}
                        {formatearNumero(conv.numero)}
                    </span>
                </div>
            )}
            {asocia ? (
                <>
                    <SelectorCliente etiqueta="Buscar cliente en Argos" valor={null} alCambiar={(s) => s && void asociar(s.cliente.id, s.servicio?.id ?? null)} />
                    <section className="grid gap-1.5" aria-label="Posibles coincidencias">
                        <h3 className="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">Posibles coincidencias</h3>
                        {coincidencias.estado === "cargando" ? <Cargando texto="Buscando por el número…" /> : null}
                        {coincidencias.estado === "error" ? (
                            <Aviso
                                tono="error"
                                titulo="No se pudieron buscar"
                                accion={
                                    <Boton variante="secundario" tamano="sm" onClick={coincidencias.reintentar}>
                                        <RotateCw />
                                        Reintentar
                                    </Boton>
                                }
                            />
                        ) : null}
                        {coincidencias.estado === "listo" ? (
                            (coincidencias.datos.clientes ?? []).length === 0 ? (
                                <p className="text-[0.8125rem] text-muted-foreground">Ningún cliente tiene este número en Argos.</p>
                            ) : (
                                <ul className="grid gap-1.5">
                                    {(coincidencias.datos.clientes ?? []).map((c) => (
                                        <li key={c.clienteId} className="flex min-w-0 items-center gap-2.5 rounded-[0.625rem] border border-border p-2">
                                            <span aria-hidden className="flex size-[1.875rem] shrink-0 items-center justify-center rounded-full bg-elevated text-[0.6875rem] font-semibold">
                                                {iniciales(c.nombre)}
                                            </span>
                                            <span className="flex min-w-0 flex-1 flex-col">
                                                <span className="truncate text-[0.8125rem] font-semibold">{c.nombre}</span>
                                                <span className="truncate text-[0.71875rem] text-muted-foreground">
                                                    {[c.codigo, textoCoincidencia(c.coincideEn ?? []), c.origen === "instantanea" ? "datos locales" : null].filter(Boolean).join(" · ")}
                                                </span>
                                            </span>
                                            <Boton variante="secundario" tamano="sm" className="shrink-0" disabled={asociando !== null} onClick={() => void asociar(c.clienteId, null)}>
                                                <Link2 />
                                                {asociando === c.clienteId ? "Asociando…" : "Asociar"}
                                            </Boton>
                                        </li>
                                    ))}
                                </ul>
                            )
                        ) : null}
                    </section>
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo asociar">
                            {error instanceof ErrorApi && error.estado === 409 ? "Alguien más cambió la conversación; vuelve a intentarlo." : mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    {conv.contacto.tipo === "desconocido" ? (
                        <section className="grid gap-1.5" aria-label="No es cliente">
                            <h3 className="text-[0.6875rem] font-semibold tracking-wide text-muted-foreground uppercase">No es cliente</h3>
                            {puede(["prospectos.crear"]) ? (
                                <Boton asChild variante="tinta" tamano="sm" className="w-full">
                                    <Link href={`/conversaciones/${conv.id}/prospecto`}>
                                        <UserPlus />
                                        Crear prospecto
                                    </Link>
                                </Boton>
                            ) : null}
                            <Boton variante="secundario" tamano="sm" className="w-full" onClick={() => setContacto(true)}>
                                <UserRound />
                                Guardar como contacto de Iris
                            </Boton>
                            <p className="text-xs text-muted-foreground">Para proveedores o personal. Si se vuelve cliente, se une a su ficha de Argos.</p>
                        </section>
                    ) : null}
                </>
            ) : (
                <p className="text-[0.8125rem] text-muted-foreground">No tienes permiso para asociar clientes.</p>
            )}
            {contacto ? <DialogoContactoIris conv={conv} cerrar={() => setContacto(false)} alHecho={alActualizar} /> : null}
        </div>
    );
}

const esquemaContacto = z.object({
    nombre: z.string().trim().min(1, "Escribe el nombre").max(120, "Hasta 120 caracteres"),
    tipo: z.enum(["proveedor", "personal", "prospecto", "otro"], { message: "Elige el tipo" }),
    nota: z.string().max(500, "Hasta 500 caracteres"),
});

function DialogoContactoIris({ conv, cerrar, alHecho }: { conv: ConversacionDetalle; cerrar: () => void; alHecho: (c: Conversacion) => void }) {
    const form = useForm({ resolver: zodResolver(esquemaContacto), defaultValues: { nombre: conv.perfilNombre ?? "", tipo: "otro" as const, nota: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo="Guardar como contacto de Iris" descripcion={formatearNumero(conv.numero)}>
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async (d) => {
                        setError(null);
                        try {
                            const c = await resolver(
                                api().POST(`${BASE}/contacto-iris`, {
                                    params: { path: { id: conv.id }, header: cabeceraIdem(clave.actual()) },
                                    body: { version: conv.version, nombre: d.nombre.trim(), tipo: d.tipo, ...(d.nota.trim() ? { nota: d.nota.trim() } : {}) },
                                }),
                            );
                            avisar({ titulo: `Contacto «${d.nombre.trim()}» guardado` });
                            alHecho(c);
                            cerrar();
                        } catch (e) {
                            clave.trasError(e);
                            if (!erroresACampos(e, ["nombre", "tipo", "nota"], form.setError)) setError(e);
                        }
                    })}
                >
                    <Campo etiqueta="Nombre" maxLength={120} autoComplete="off" error={form.formState.errors.nombre?.message} {...form.register("nombre")} />
                    <CampoSelector etiqueta="Tipo" error={form.formState.errors.tipo?.message} {...form.register("tipo")}>
                        <option value="proveedor">Proveedor</option>
                        <option value="personal">Personal</option>
                        <option value="prospecto">Posible cliente</option>
                        <option value="otro">Otro</option>
                    </CampoSelector>
                    <CampoArea etiqueta="Nota (opcional)" rows={2} maxLength={500} error={form.formState.errors.nota?.message} {...form.register("nota")} />
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo guardar">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Guardando…">
                            Guardar contacto
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const esquemaTicket = z.object({
    asunto: z.string().trim().min(3, "Escribe el asunto").max(200, "Hasta 200 caracteres"),
    descripcion: z.string().max(4000, "Hasta 4000 caracteres"),
});

/** Ticket desde la conversación (cliente y servicio de la conversación): asunto y descripción. */
function DialogoTicket({ conv, cerrar, alHecho }: { conv: ConversacionDetalle; cerrar: () => void; alHecho: (c: Conversacion) => void }) {
    const form = useForm({ resolver: zodResolver(esquemaTicket), defaultValues: { asunto: "", descripcion: conv.ultimoMensaje?.direccion === "entrante" ? (conv.ultimoMensaje.texto ?? "") : "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo="Crear ticket" descripcion={`${conv.titulo} · con el cliente y el servicio de la conversación`}>
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async (d) => {
                        setError(null);
                        try {
                            const r = await resolver(
                                api().POST(`${BASE}/crear-ticket`, {
                                    params: { path: { id: conv.id }, header: cabeceraIdem(clave.actual()) },
                                    body: { asunto: d.asunto.trim(), ...(d.descripcion.trim() ? { descripcion: d.descripcion.trim() } : {}) },
                                }),
                            );
                            avisar({ titulo: `Ticket ${r.vinculo.referencia} creado` });
                            alHecho(r.conversacion);
                            cerrar();
                        } catch (e) {
                            clave.trasError(e);
                            if (!erroresACampos(e, ["asunto", "descripcion"], form.setError)) setError(e);
                        }
                    })}
                >
                    <Campo etiqueta="Asunto" maxLength={200} autoComplete="off" error={form.formState.errors.asunto?.message} {...form.register("asunto")} />
                    <CampoArea etiqueta="Descripción (opcional)" rows={4} maxLength={4000} error={form.formState.errors.descripcion?.message} {...form.register("descripcion")} />
                    {error ? (
                        <Aviso tono="error" titulo="No se pudo crear el ticket">
                            {mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Creando…">
                            Crear ticket
                        </BotonEnviar>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const esquemaPromesa = z.object({
    servicioId: z.string().min(1, "Elige el servicio"),
    venceEl: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Elige el día"),
});

/**
 * Promesa de pago (2 campos): primero simula con Argos (qué regla aplica) y luego la pide. Argos
 * caído: 503 y nada queda a medias (se repite con la misma clave).
 */
function DialogoPromesa({ conv, cerrar }: { conv: ConversacionDetalle; cerrar: () => void }) {
    const servicios = conv.clienteResumen?.servicios ?? [];
    const form = useForm({
        resolver: zodResolver(esquemaPromesa),
        defaultValues: { servicioId: String(conv.contacto.servicioId ?? (servicios.length === 1 ? (servicios[0]?.id ?? "") : "")), venceEl: "" },
    });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [simulacion, setSimulacion] = useState<{ texto: string; permitida: boolean } | null>(null);
    const [simulando, setSimulando] = useState(false);

    async function simular() {
        const ok = await form.trigger();
        if (!ok) return;
        const d = form.getValues();
        setSimulando(true);
        setError(null);
        try {
            const r = await resolver(api().POST(`${BASE}/promesas/simular`, { params: { path: { id: conv.id } }, body: { servicioId: Number(d.servicioId), venceEl: d.venceEl } }));
            const permitida = r.resultado !== "rechazada";
            const texto =
                r.resultado === "pendiente_aprobacion"
                    ? "Argos la dejaría pendiente de aprobación por un supervisor."
                    : [r.motivo, r.regla ? `Regla: ${r.regla}` : null].filter(Boolean).join(". ") || (permitida ? "Cumple las reglas de Argos." : "No cumple las reglas de Argos.");
            setSimulacion({ texto, permitida });
        } catch (e) {
            setError(e);
        } finally {
            setSimulando(false);
        }
    }

    return (
        <DialogoProtegido open onOpenChange={(a) => !a && cerrar()}>
            <DialogoContenido titulo="Promesa de pago" descripcion={conv.titulo}>
                <form
                    noValidate
                    className="grid gap-4"
                    onSubmit={form.handleSubmit(async (d) => {
                        setError(null);
                        try {
                            const r = await resolver(
                                api().POST(`${BASE}/promesas`, {
                                    params: { path: { id: conv.id }, header: cabeceraIdem(clave.actual()) },
                                    body: { servicioId: Number(d.servicioId), venceEl: d.venceEl },
                                }),
                            );
                            avisar({ titulo: r.estado === "activa" ? "Promesa registrada en Argos" : r.estado === "pendiente_aprobacion" ? "Promesa enviada: espera aprobación en Argos" : "Argos rechazó la promesa" });
                            cerrar();
                        } catch (e) {
                            clave.trasError(e);
                            if (!erroresACampos(e, ["servicioId", "venceEl"], form.setError)) setError(e);
                        }
                    })}
                >
                    <CampoSelector etiqueta="Servicio" error={form.formState.errors.servicioId?.message} {...form.register("servicioId", { onChange: () => setSimulacion(null) })}>
                        <option value="">Elegir…</option>
                        {servicios.map((s) => (
                            <option key={s.id} value={String(s.id)}>
                                {s.plan} · {s.codigo}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoFecha etiqueta="Paga a más tardar el" error={form.formState.errors.venceEl?.message} {...form.register("venceEl", { onChange: () => setSimulacion(null) })} />
                    {simulacion ? (
                        <Aviso tono={simulacion.permitida ? "success" : "warning"} titulo={simulacion.permitida ? "Argos la aceptaría" : "Argos no la aceptaría"}>
                            {simulacion.texto}
                        </Aviso>
                    ) : null}
                    {error ? (
                        <Aviso tono="error" titulo={error instanceof ErrorApi && error.codigo === "argos_no_disponible" ? "Argos no responde" : "No se pudo"}>
                            {error instanceof ErrorApi && error.codigo === "argos_no_disponible" ? "No quedó nada a medias. Vuelve a intentarlo en un momento." : mensajeDeError(error).titulo}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno" disabled={form.formState.isSubmitting}>
                                Cancelar
                            </Boton>
                        </DialogoCerrar>
                        {simulacion?.permitida ? (
                            <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Registrando…">
                                Registrar promesa
                            </BotonEnviar>
                        ) : (
                            <Boton variante="primario" onClick={() => void simular()} disabled={simulando}>
                                {simulando ? "Consultando…" : "Consultar con Argos"}
                            </Boton>
                        )}
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </DialogoProtegido>
    );
}
