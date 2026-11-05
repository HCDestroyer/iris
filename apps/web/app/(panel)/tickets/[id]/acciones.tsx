"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    avisar,
    Aviso,
    Boton,
    BotonEnviar,
    Campo,
    CampoArea,
    CampoSelector,
    DialogoCerrar,
    DialogoConfirmar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    GrupoSegmentado,
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    useCambiosSinGuardar,
    useProteccionSalida,
} from "@iris/ui";
import {
    Archive,
    ArrowUpCircle,
    CircleCheck,
    CirclePause,
    CirclePlay,
    ClipboardPlus,
    Ellipsis,
    GitMerge,
    Lock,
    MessageSquareReply,
    RotateCcw,
    RotateCw,
    UserRoundPlus,
    type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { useForm, useWatch, type FieldValues, type Path, type UseFormSetError } from "react-hook-form";
import type { Usuario } from "@/componentes/tickets/catalogos";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import type { Permiso } from "@/lib/menu";
import { useSesion } from "@/lib/sesion";
import {
    esquemaAsignar,
    esquemaCerrar,
    esquemaEscalar,
    esquemaFusionar,
    esquemaNotaInterna,
    esquemaPonerEnEspera,
    esquemaReabrir,
    esquemaReanudar,
    esquemaResolver,
    esquemaResponder,
    normalizarNumeroTicket,
} from "@/lib/tickets/esquemas";
import { ORDEN_PRIORIDADES, PRIORIDADES, prioridadMayor } from "@/lib/tickets/presentacion";
import type { Ticket } from "@/lib/tickets/sla";
import { avisoTicket } from "@/lib/avisos";

export type Accion = "responder" | "nota_interna" | "asignar" | "escalar" | "poner_en_espera" | "reanudar" | "resolver" | "reabrir" | "cerrar" | "fusionar";

type DefAccion = {
    etiqueta: string;
    icono: LucideIcon;
    permiso: Permiso;
    disponible: (t: Ticket) => boolean;
    titulo: string;
};

const abiertoOEspera = (t: Ticket) => t.estado === "nuevo" || t.estado === "abierto" || t.estado === "en_espera";

/** Acciones explícitas (nunca PATCH de estado): permiso del mismo nombre y estados en que aplican. */
export const ACCIONES: Record<Accion, DefAccion> = {
    responder: {
        etiqueta: "Responder",
        icono: MessageSquareReply,
        permiso: "tickets.responder",
        disponible: (t) => t.estado !== "cerrado",
        titulo: "Responder al cliente",
    },
    nota_interna: { etiqueta: "Nota interna", icono: Lock, permiso: "tickets.nota_interna", disponible: () => true, titulo: "Nota interna" },
    asignar: { etiqueta: "Asignar", icono: UserRoundPlus, permiso: "tickets.asignar", disponible: (t) => t.estado !== "cerrado", titulo: "Asignar ticket" },
    escalar: { etiqueta: "Escalar", icono: ArrowUpCircle, permiso: "tickets.escalar", disponible: abiertoOEspera, titulo: "Escalar ticket" },
    poner_en_espera: {
        etiqueta: "Poner en espera",
        icono: CirclePause,
        permiso: "tickets.poner_en_espera",
        disponible: (t) => t.estado === "abierto",
        titulo: "Poner en espera",
    },
    reanudar: { etiqueta: "Reanudar", icono: CirclePlay, permiso: "tickets.reanudar", disponible: (t) => t.estado === "en_espera", titulo: "Reanudar ticket" },
    resolver: { etiqueta: "Resolver", icono: CircleCheck, permiso: "tickets.resolver", disponible: abiertoOEspera, titulo: "Resolver ticket" },
    reabrir: { etiqueta: "Reabrir", icono: RotateCcw, permiso: "tickets.reabrir", disponible: (t) => t.estado === "resuelto", titulo: "Reabrir ticket" },
    cerrar: { etiqueta: "Cerrar", icono: Archive, permiso: "tickets.cerrar", disponible: (t) => t.estado !== "cerrado", titulo: "Cerrar ticket" },
    fusionar: {
        etiqueta: "Fusionar",
        icono: GitMerge,
        permiso: "tickets.fusionar",
        disponible: (t) => t.estado !== "cerrado",
        titulo: "Fusionar con otro ticket",
    },
};

const ORDEN: readonly Accion[] = [
    "responder",
    "nota_interna",
    "asignar",
    "escalar",
    "poner_en_espera",
    "reanudar",
    "resolver",
    "reabrir",
    "cerrar",
    "fusionar",
];

/** Acciones que el usuario puede hacer ahora sobre el ticket (permiso + estado). */
export function accionesDisponibles(ticket: Ticket, puede: (p: readonly Permiso[]) => boolean): Accion[] {
    return ORDEN.filter((a) => puede([ACCIONES[a].permiso]) && ACCIONES[a].disponible(ticket));
}

type PropsBarra = {
    ticket: Ticket;
    usuarios: readonly Usuario[] | null | undefined;
    /** La acción terminó: el ticket que devolvió la API. */
    alHecho: (ticket: Ticket) => void;
    /** Relee el ticket (tras un 409). */
    recargar: () => Promise<void>;
    /** Acción pedida por la URL (`?accion=asignar`, desde Inicio): se abre al entrar si está disponible. */
    inicial?: Accion | null;
};

/** Acciones con botón propio en la barra (en este orden); el resto va en «⋯». Responder y la nota interna van en la conversación. */
const EN_BARRA: readonly Accion[] = ["asignar", "escalar"];
const EN_CONVERSACION: readonly Accion[] = ["responder", "nota_interna"];

/**
 * Barra de acciones de la ficha (nuevo diseño): Asignar, Escalar, Crear orden, Resolver como
 * botón principal y «⋯» para el resto (poner en espera, reanudar, reabrir, fusionar, cerrar). Cada
 * acción se abre en un diálogo corto con telón (1 a 3 campos, §10).
 */
export function BarraAcciones({ ticket, usuarios, alHecho, recargar, inicial }: PropsBarra) {
    const { puede } = useSesion();
    const disponibles = accionesDisponibles(ticket, puede);
    const [abierta, setAbierta] = useState<Accion | null>(null);
    const pedida = useRef(inicial ?? null);
    useEffect(() => {
        const a = pedida.current;
        pedida.current = null;
        if (a && disponibles.includes(a) && !EN_CONVERSACION.includes(a)) setAbierta(a);
        // Solo al entrar: la acción de la URL se ofrece una vez.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);
    const enBarra = EN_BARRA.filter((a) => disponibles.includes(a));
    const puedeResolver = disponibles.includes("resolver");
    const resto = disponibles.filter((a) => !EN_BARRA.includes(a) && !EN_CONVERSACION.includes(a) && a !== "resolver");
    const crearOrden = ticket.estado !== "cerrado" && puede(["ordenes.crear"]);
    if (enBarra.length === 0 && !puedeResolver && resto.length === 0 && !crearOrden) {
        return null;
    }
    return (
        <>
            <div className="flex flex-wrap gap-2" role="toolbar" aria-label="Acciones del ticket">
                {enBarra.map((a) => {
                    const def = ACCIONES[a];
                    const Icono = def.icono;
                    return (
                        <Boton key={a} variante="secundario" tamano="sm" className="h-9 rounded-lg" onClick={() => setAbierta(a)}>
                            <Icono />
                            {def.etiqueta}
                        </Boton>
                    );
                })}
                {crearOrden ? (
                    <Boton asChild variante="secundario" tamano="sm" className="h-9 rounded-lg">
                        <Link href={`/ordenes/nueva?ticket=${ticket.id}`}>
                            <ClipboardPlus />
                            {puede(["tickets.crear_orden"]) ? "Crear orden y asignar" : "Crear orden"}
                        </Link>
                    </Boton>
                ) : null}
                {puedeResolver ? (
                    <Boton tamano="sm" className="h-9 rounded-lg" onClick={() => setAbierta("resolver")}>
                        <CircleCheck />
                        Resolver
                    </Boton>
                ) : null}
                {resto.length > 0 ? (
                    <MenuDesplegable>
                        <MenuDesplegableDisparador>
                            <Boton variante="secundario" tamano="icono-sm" className="size-9 rounded-lg" aria-label="Más acciones" title="Más acciones">
                                <Ellipsis />
                            </Boton>
                        </MenuDesplegableDisparador>
                        <MenuDesplegableContenido align="end">
                            {resto.map((a) => (
                                <MenuDesplegableElemento key={a} icono={ACCIONES[a].icono} onSelect={() => setAbierta(a)}>
                                    {ACCIONES[a].etiqueta}
                                </MenuDesplegableElemento>
                            ))}
                        </MenuDesplegableContenido>
                    </MenuDesplegable>
                ) : null}
            </div>
            {abierta ? (
                <DialogoAccion
                    key={abierta}
                    accion={abierta}
                    ticket={ticket}
                    usuarios={usuarios}
                    alCerrar={() => setAbierta(null)}
                    alHecho={(t) => {
                        setAbierta(null);
                        alHecho(t);
                    }}
                    recargar={recargar}
                />
            ) : null}
        </>
    );
}

type Envio = <T extends FieldValues>(peticion: (clave: string) => Promise<Ticket>, campos: readonly Path<T>[], setError: UseFormSetError<T>) => Promise<void>;

type PropsFormulario = { ticket: Ticket; enviar: Envio; pie: (etiqueta: string, enviando: boolean, faltan?: readonly string[]) => ReactNode };

function DialogoAccion({
    accion,
    ticket,
    usuarios,
    alCerrar,
    alHecho,
    recargar,
}: {
    accion: Accion;
    ticket: Ticket;
    usuarios: readonly Usuario[] | null | undefined;
    alCerrar: () => void;
    alHecho: (t: Ticket) => void;
    recargar: () => Promise<void>;
}) {
    const clave = useRef(nuevaClaveIdempotencia());
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const def = ACCIONES[accion];

    const enviar: Envio = async (peticion, campos, setErrorCampo) => {
        setError(null);
        try {
            const t = await peticion(clave.current);
            clave.current = nuevaClaveIdempotencia();
            avisar({ titulo: avisoTicket(accion, t, usuarios) });
            alHecho(t);
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            if (!erroresACampos(e, campos, setErrorCampo)) {
                setError(e);
            }
        }
    };

    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";

    async function recargarTicket() {
        setRecargando(true);
        try {
            await recargar();
            setError(null);
        } finally {
            setRecargando(false);
        }
    }

    const pie = (etiqueta: string, enviando: boolean, faltan: readonly string[] = []) => (
        <>
            {conflicto ? (
                <Aviso
                    tono="warning"
                    titulo="Alguien más cambió este ticket"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={recargarTicket} disabled={recargando}>
                            <RotateCw />
                            {recargando ? "Recargando…" : "Recargar"}
                        </Boton>
                    }
                >
                    Recarga para ver el cambio y vuelve a intentarlo; lo que escribiste se conserva.
                </Aviso>
            ) : error ? (
                <Aviso tono="error" titulo={`No se pudo ${def.etiqueta.toLowerCase()}`}>
                    {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                </Aviso>
            ) : null}
            <DialogoPie>
                <DialogoCerrar asChild>
                    <Boton variante="contorno" disabled={enviando}>
                        Cancelar
                    </Boton>
                </DialogoCerrar>
                <BotonEnviar enviando={enviando} textoEnviando="Enviando…" disabled={conflicto} faltan={faltan}>
                    {etiqueta}
                </BotonEnviar>
            </DialogoPie>
        </>
    );

    const props: PropsFormulario = { ticket, enviar, pie };
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo={def.titulo} descripcion={`${ticket.numero} · ${ticket.asunto}`} data-accion={accion}>
                {accion === "responder" ? <FormResponder {...props} /> : null}
                {accion === "nota_interna" ? <FormNota {...props} /> : null}
                {accion === "asignar" ? <FormAsignar {...props} usuarios={usuarios} /> : null}
                {accion === "escalar" ? <FormEscalar {...props} /> : null}
                {accion === "poner_en_espera" ? <FormMotivo {...props} accion="poner_en_espera" /> : null}
                {accion === "reabrir" ? <FormMotivo {...props} accion="reabrir" /> : null}
                {accion === "reanudar" ? <FormReanudar {...props} /> : null}
                {accion === "resolver" ? <FormResolver {...props} /> : null}
                {accion === "cerrar" ? <FormCerrar {...props} /> : null}
                {accion === "fusionar" ? <FormFusionar {...props} /> : null}
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const cabecera = (clave: string) => ({ "Idempotency-Key": clave });

/**
 * Redactor al pie de la conversación (nuevo diseño): «Responder al cliente» o «Nota interna»
 * según los permisos, un campo y «Enviar». Es la misma acción explícita de siempre
 * (`POST /tickets/{id}/responder` o `/nota-interna`, con `version` e `Idempotency-Key`). Si hay
 * texto sin enviar, salir de la ficha pregunta antes.
 */
export function Redactor({ ticket, usuarios, alHecho, recargar }: Omit<PropsBarra, "inicial">) {
    const { puede } = useSesion();
    const router = useRouter();
    const modos = (["responder", "nota_interna"] as const).filter((a) => puede([ACCIONES[a].permiso]) && ACCIONES[a].disponible(ticket));
    const [modo, setModo] = useState<"responder" | "nota_interna">(modos[0] ?? "responder");
    // La clave de la próxima petición: la misma si se reintenta tras un corte de red (claveTrasError).
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [error, setError] = useState<unknown>(null);
    const form = useForm({ resolver: zodResolver(modo === "responder" ? esquemaResponder : esquemaNotaInterna), defaultValues: { cuerpo: "" } });
    const sucio = form.formState.isDirty && !form.formState.isSubmitting;
    const proteccion = useProteccionSalida(sucio, router.push);
    if (modos.length === 0) return null;
    const actual = modos.includes(modo) ? modo : (modos[0] ?? "responder");
    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";

    async function enviarMensaje(cuerpo: string) {
        setError(null);
        try {
            const opciones = { params: ruta(ticket, clave), body: { cuerpo, version: ticket.version } };
            const r =
                actual === "responder"
                    ? await resolver(api().POST("/api/v1/tickets/{id}/responder", opciones))
                    : await resolver(api().POST("/api/v1/tickets/{id}/nota-interna", opciones));
            setClave(nuevaClaveIdempotencia());
            form.reset({ cuerpo: "" });
            avisar({ titulo: avisoTicket(actual, r.ticket, usuarios) });
            alHecho(r.ticket);
        } catch (e) {
            setClave(claveTrasError(e, clave));
            if (!erroresACampos(e, ["cuerpo"], form.setError)) setError(e);
        }
    }

    return (
        <form
            noValidate
            aria-label="Redactar"
            className="grid gap-2 border-t border-border px-4 pt-3 pb-3.5"
            onSubmit={form.handleSubmit((d) => enviarMensaje(d.cuerpo))}
        >
            {modos.length > 1 ? (
                <GrupoSegmentado
                    etiqueta="Tipo de mensaje"
                    className="justify-self-start"
                    valor={actual}
                    alCambiar={(v) => {
                        setModo(v);
                        form.clearErrors();
                    }}
                    opciones={[
                        { valor: "responder", etiqueta: "Responder al cliente", icono: MessageSquareReply },
                        { valor: "nota_interna", etiqueta: "Nota interna", icono: Lock },
                    ]}
                />
            ) : null}
            <CampoArea
                etiqueta={actual === "responder" ? "Respuesta" : "Nota interna"}
                ayuda={actual === "responder" ? "La verá el cliente." : "Solo la ve el equipo; el cliente no."}
                rows={3}
                maxLength={20000}
                error={form.formState.errors.cuerpo?.message}
                {...form.register("cuerpo")}
            />
            {conflicto ? (
                <Aviso
                    tono="warning"
                    titulo="Alguien más cambió este ticket"
                    accion={
                        <Boton
                            variante="secundario"
                            tamano="sm"
                            onClick={async () => {
                                await recargar();
                                setError(null);
                            }}
                        >
                            <RotateCw />
                            Recargar
                        </Boton>
                    }
                >
                    Recarga para ver el cambio y vuelve a intentarlo; lo que escribiste se conserva.
                </Aviso>
            ) : error ? (
                <Aviso tono="error" titulo="No se pudo enviar">
                    {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                </Aviso>
            ) : null}
            <div className="flex justify-end">
                <BotonEnviar enviando={form.formState.isSubmitting} textoEnviando="Enviando…" disabled={conflicto} className="rounded-lg">
                    {actual === "responder" ? "Enviar respuesta" : "Guardar nota"}
                </BotonEnviar>
            </div>
            <DialogoConfirmar
                open={proteccion.pendiente !== null}
                onOpenChange={(abierto) => !abierto && proteccion.seguir()}
                titulo="¿Descartar el mensaje?"
                descripcion="Escribiste un mensaje que no se envió. Si sales ahora, se pierde."
                textoCancelar="Seguir escribiendo"
                textoConfirmar="Descartar mensaje"
                peligro
                alConfirmar={() => proteccion.confirmar()}
            />
        </form>
    );
}
const ruta = (t: Ticket, clave: string) => ({ path: { id: t.id }, header: cabecera(clave) });

function FormResponder({ ticket, enviar, pie }: PropsFormulario) {
    const form = useForm({ resolver: zodResolver(esquemaResponder), defaultValues: { cuerpo: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) =>
                        (
                            await resolver(
                                api().POST("/api/v1/tickets/{id}/responder", {
                                    params: ruta(ticket, clave),
                                    body: { cuerpo: d.cuerpo, version: ticket.version },
                                }),
                            )
                        ).ticket,
                    ["cuerpo"],
                    form.setError,
                ),
            )}
        >
            <CampoArea
                etiqueta="Respuesta"
                ayuda="La verá el cliente."
                rows={5}
                maxLength={20000}
                autoFocus
                error={form.formState.errors.cuerpo?.message}
                {...form.register("cuerpo")}
            />
            {pie("Enviar respuesta", form.formState.isSubmitting)}
        </form>
    );
}

function FormNota({ ticket, enviar, pie }: PropsFormulario) {
    const form = useForm({ resolver: zodResolver(esquemaNotaInterna), defaultValues: { cuerpo: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) =>
                        (
                            await resolver(
                                api().POST("/api/v1/tickets/{id}/nota-interna", {
                                    params: ruta(ticket, clave),
                                    body: { cuerpo: d.cuerpo, version: ticket.version },
                                }),
                            )
                        ).ticket,
                    ["cuerpo"],
                    form.setError,
                ),
            )}
        >
            <CampoArea
                etiqueta="Nota interna"
                ayuda="Solo la ve el equipo; el cliente no."
                rows={5}
                maxLength={20000}
                autoFocus
                error={form.formState.errors.cuerpo?.message}
                {...form.register("cuerpo")}
            />
            {pie("Guardar nota", form.formState.isSubmitting)}
        </form>
    );
}

function FormAsignar({ ticket, enviar, pie, usuarios }: PropsFormulario & { usuarios: readonly Usuario[] | null | undefined }) {
    const { yo } = useSesion();
    const form = useForm({ resolver: zodResolver(esquemaAsignar), defaultValues: { usuarioId: ticket.asignadoA === null ? "" : String(ticket.asignadoA) } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const otros = (usuarios ?? []).filter((u) => u.tipo === "persona" && u.activo && u.id !== yo.usuario.id);
    // Sin responsable elegido y el ticket sin asignar: no hay nada que guardar.
    const sinResponsable = useWatch({ control: form.control, name: "usuarioId" }) === "" && ticket.asignadoA === null;
    const actualDesconocido = ticket.asignadoA !== null && ticket.asignadoA !== yo.usuario.id && !otros.some((u) => u.id === ticket.asignadoA);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) =>
                        resolver(
                            api().POST("/api/v1/tickets/{id}/asignar", {
                                params: ruta(ticket, clave),
                                body: { version: ticket.version, ...(d.usuarioId !== undefined ? { usuarioId: d.usuarioId } : {}) },
                            }),
                        ),
                    ["usuarioId"],
                    form.setError,
                ),
            )}
        >
            <CampoSelector
                etiqueta="Responsable"
                autoFocus
                ayuda={
                    usuarios === null
                        ? "No tienes acceso a la lista de usuarios: puedes asignártelo o dejarlo sin asignar."
                        : "Debe poder ver el ticket y operar en esta empresa."
                }
                error={form.formState.errors.usuarioId?.message}
                {...form.register("usuarioId")}
            >
                <option value="">Sin asignar</option>
                <option value={yo.usuario.id}>{yo.usuario.nombre} (yo)</option>
                {actualDesconocido ? <option value={ticket.asignadoA ?? ""}>Usuario {ticket.asignadoA}</option> : null}
                {otros.map((u) => (
                    <option key={u.id} value={u.id}>
                        {u.nombre}
                    </option>
                ))}
            </CampoSelector>
            {pie("Asignar", form.formState.isSubmitting, sinResponsable ? ["el responsable"] : [])}
        </form>
    );
}

function FormEscalar({ ticket, enviar, pie }: PropsFormulario) {
    const form = useForm({ resolver: zodResolver(esquemaEscalar(ticket.prioridad)), defaultValues: { motivo: "", prioridad: "" as const } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const mayores = ORDEN_PRIORIDADES.filter((p) => prioridadMayor(p, ticket.prioridad));
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) =>
                        resolver(
                            api().POST("/api/v1/tickets/{id}/escalar", {
                                params: ruta(ticket, clave),
                                body: { version: ticket.version, motivo: d.motivo, ...(d.prioridad ? { prioridad: d.prioridad } : {}) },
                            }),
                        ),
                    ["motivo", "prioridad"],
                    form.setError,
                ),
            )}
        >
            <CampoSelector
                etiqueta="Nueva prioridad"
                ayuda={`Actual: ${PRIORIDADES[ticket.prioridad].texto}. Subirla recalcula el SLA pendiente.`}
                error={form.formState.errors.prioridad?.message}
                {...form.register("prioridad")}
            >
                <option value="">Mantener {PRIORIDADES[ticket.prioridad].texto.toLowerCase()}</option>
                {[...mayores].reverse().map((p) => (
                    <option key={p} value={p}>
                        {PRIORIDADES[p].texto}
                    </option>
                ))}
            </CampoSelector>
            <CampoArea etiqueta="Motivo" rows={3} maxLength={500} autoFocus error={form.formState.errors.motivo?.message} {...form.register("motivo")} />
            {pie("Escalar", form.formState.isSubmitting)}
        </form>
    );
}

function FormMotivo({ ticket, enviar, pie, accion }: PropsFormulario & { accion: "poner_en_espera" | "reabrir" }) {
    const form = useForm({ resolver: zodResolver(accion === "reabrir" ? esquemaReabrir : esquemaPonerEnEspera), defaultValues: { motivo: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) => {
                        const opciones = { params: ruta(ticket, clave), body: { version: ticket.version, motivo: d.motivo } };
                        return accion === "reabrir"
                            ? resolver(api().POST("/api/v1/tickets/{id}/reabrir", opciones))
                            : resolver(api().POST("/api/v1/tickets/{id}/poner-en-espera", opciones));
                    },
                    ["motivo"],
                    form.setError,
                ),
            )}
        >
            <CampoArea
                etiqueta={accion === "reabrir" ? "Por qué se reabre" : "Qué se espera"}
                ayuda={accion === "reabrir" ? undefined : "Del cliente, de un proveedor… Mientras tanto la resolución no cuenta para el SLA."}
                rows={3}
                maxLength={500}
                autoFocus
                error={form.formState.errors.motivo?.message}
                {...form.register("motivo")}
            />
            {pie(accion === "reabrir" ? "Reabrir" : "Poner en espera", form.formState.isSubmitting)}
        </form>
    );
}

function FormReanudar({ ticket, enviar, pie }: PropsFormulario) {
    const form = useForm({ resolver: zodResolver(esquemaReanudar), defaultValues: { motivo: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) =>
                        resolver(
                            api().POST("/api/v1/tickets/{id}/reanudar", {
                                params: ruta(ticket, clave),
                                body: { version: ticket.version, ...(d.motivo ? { motivo: d.motivo } : {}) },
                            }),
                        ),
                    ["motivo"],
                    form.setError,
                ),
            )}
        >
            <CampoArea
                etiqueta="Motivo (opcional)"
                rows={3}
                maxLength={500}
                autoFocus
                error={form.formState.errors.motivo?.message}
                {...form.register("motivo")}
            />
            {pie("Reanudar", form.formState.isSubmitting)}
        </form>
    );
}

function FormResolver({ ticket, enviar, pie }: PropsFormulario) {
    const form = useForm({ resolver: zodResolver(esquemaResolver), defaultValues: { solucion: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) =>
                        resolver(
                            api().POST("/api/v1/tickets/{id}/resolver", {
                                params: ruta(ticket, clave),
                                body: { version: ticket.version, solucion: d.solucion },
                            }),
                        ),
                    ["solucion"],
                    form.setError,
                ),
            )}
        >
            <CampoArea
                etiqueta="Solución"
                ayuda="Qué se hizo; la verá el cliente."
                rows={4}
                maxLength={20000}
                autoFocus
                error={form.formState.errors.solucion?.message}
                {...form.register("solucion")}
            />
            {pie("Resolver", form.formState.isSubmitting)}
        </form>
    );
}

function FormCerrar({ ticket, enviar, pie }: PropsFormulario) {
    const resuelto = ticket.estado === "resuelto";
    const form = useForm({ resolver: zodResolver(esquemaCerrar(resuelto)), defaultValues: { motivo: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    async (clave) =>
                        resolver(
                            api().POST("/api/v1/tickets/{id}/cerrar", {
                                params: ruta(ticket, clave),
                                body: { version: ticket.version, ...(d.motivo ? { motivo: d.motivo } : {}) },
                            }),
                        ),
                    ["motivo"],
                    form.setError,
                ),
            )}
        >
            {resuelto ? null : (
                <Aviso tono="warning" titulo="El ticket no está resuelto">
                    Cerrarlo es definitivo: no se puede reabrir. Explica por qué se cierra sin resolver.
                </Aviso>
            )}
            <CampoArea
                etiqueta={resuelto ? "Motivo (opcional)" : "Motivo"}
                rows={3}
                maxLength={500}
                autoFocus
                error={form.formState.errors.motivo?.message}
                {...form.register("motivo")}
            />
            {pie("Cerrar ticket", form.formState.isSubmitting)}
        </form>
    );
}

function FormFusionar({ ticket, enviar, pie }: PropsFormulario) {
    const form = useForm({ resolver: zodResolver(esquemaFusionar), defaultValues: { destino: "", motivo: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit(async (d) => {
                const { numero } = normalizarNumeroTicket(d.destino);
                if (numero === ticket.numero) {
                    form.setError("destino", { message: "Elige otro ticket: este es el que se cierra" });
                    return;
                }
                let destinoId: number | undefined;
                try {
                    const pagina = await resolver(api().GET("/api/v1/tickets", { params: { query: { q: numero, limit: 10 } } }));
                    destinoId = (pagina.data ?? []).find((t) => t.numero === numero)?.id;
                } catch (e) {
                    form.setError("destino", { message: `No se pudo buscar el ticket: ${mensajeDeError(e).titulo}` });
                    return;
                }
                if (destinoId === undefined) {
                    form.setError("destino", { message: `No encontramos ${numero} en esta empresa` });
                    return;
                }
                const id = destinoId;
                await enviar(
                    async (clave) =>
                        (
                            await resolver(
                                api().POST("/api/v1/tickets/{id}/fusionar", {
                                    params: ruta(ticket, clave),
                                    body: { version: ticket.version, destinoId: id, ...(d.motivo ? { motivo: d.motivo } : {}) },
                                }),
                            )
                        ).origen,
                    ["destino", "motivo"],
                    form.setError,
                );
            })}
        >
            <p className="text-sm text-muted-foreground">
                Este ticket se cierra como duplicado y su conversación sigue disponible desde el ticket que se queda.
            </p>
            <Campo
                etiqueta="Ticket que se queda"
                placeholder="TK-000123"
                autoFocus
                autoComplete="off"
                className="font-mono"
                maxLength={20}
                error={form.formState.errors.destino?.message}
                {...form.register("destino")}
            />
            <CampoArea etiqueta="Motivo (opcional)" rows={2} maxLength={500} error={form.formState.errors.motivo?.message} {...form.register("motivo")} />
            {pie("Fusionar", form.formState.isSubmitting)}
        </form>
    );
}
