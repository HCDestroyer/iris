"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, BotonEnviar, Campo, CampoArea, CampoSelector, DialogoCerrar, DialogoContenido, DialogoPie, DialogoProtegido, useCambiosSinGuardar } from "@iris/ui";
import { Ban, CalendarClock, CalendarSync, CirclePause, CirclePlay, RotateCcw, RotateCw, UserRoundPlus, type LucideIcon } from "lucide-react";
import { useRef, useState, type ReactNode } from "react";
import { useForm, useWatch, type FieldValues, type Path, type UseFormRegisterReturn, type UseFormSetError } from "react-hook-form";
import { tecnicosDe } from "@/componentes/ordenes/catalogos";
import type { Usuario } from "@/componentes/tickets/catalogos";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import type { Permiso } from "@/lib/menu";
import { entradaPausar, entradaProgramarOrden, esquemaAsignarOrden, esquemaMotivoOrden, esquemaPausar, esquemaProgramarOrden, guatemalaDesdeIso, type DatosPausar } from "@/lib/ordenes/esquemas";
import { MOTIVOS_PAUSA, type EstadoOrden, type MotivoPausa, type Orden } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";

export type AccionOrden = "asignar" | "programar" | "reprogramar" | "pausar" | "reanudar" | "cancelar" | "reabrir";

type DefAccion = { etiqueta: string; icono: LucideIcon; permiso: Permiso; estados: readonly EstadoOrden[]; titulo: string };

/**
 * Acciones del panel (nunca PATCH de estado): permiso del mismo nombre y estados en que la API
 * las acepta (README de campo). Salir, llegar, cerrar y fallar son del técnico en la app de campo
 * (con la orden pausada la API los rechaza: 422 `orden_pausada`). Una pausada se reanuda,
 * reprograma o cancela; no se reasigna sin reprogramar.
 */
export const ACCIONES_ORDEN: Record<AccionOrden, DefAccion> = {
    asignar: { etiqueta: "Asignar", icono: UserRoundPlus, permiso: "ordenes.asignar", estados: ["pendiente", "programada", "en_camino"], titulo: "Asignar técnico" },
    programar: { etiqueta: "Programar", icono: CalendarClock, permiso: "ordenes.programar", estados: ["pendiente"], titulo: "Programar visita" },
    reprogramar: { etiqueta: "Reprogramar", icono: CalendarSync, permiso: "ordenes.reprogramar", estados: ["programada", "en_camino", "pausada", "fallida"], titulo: "Reprogramar visita" },
    pausar: { etiqueta: "Pausar", icono: CirclePause, permiso: "ordenes.pausar", estados: ["en_camino", "en_sitio"], titulo: "Pausar la orden" },
    reanudar: { etiqueta: "Reanudar", icono: CirclePlay, permiso: "ordenes.reanudar", estados: ["pausada"], titulo: "Reanudar la orden" },
    reabrir: { etiqueta: "Reabrir", icono: RotateCcw, permiso: "ordenes.reabrir", estados: ["fallida", "cancelada"], titulo: "Reabrir orden" },
    cancelar: { etiqueta: "Cancelar orden", icono: Ban, permiso: "ordenes.cancelar", estados: ["pendiente", "programada", "en_camino", "en_sitio", "pausada"], titulo: "Cancelar orden" },
};

const ORDEN: readonly AccionOrden[] = ["reanudar", "programar", "asignar", "reprogramar", "pausar", "reabrir", "cancelar"];

export function accionesOrdenDisponibles(orden: Pick<Orden, "estado">, puede: (p: readonly Permiso[]) => boolean): AccionOrden[] {
    return ORDEN.filter((a) => puede([ACCIONES_ORDEN[a].permiso]) && ACCIONES_ORDEN[a].estados.includes(orden.estado));
}

type Props = {
    orden: Orden;
    usuarios: readonly Usuario[] | null | undefined;
    alHecho: (orden: Orden) => void;
    recargar: () => Promise<void>;
};

export function BarraAccionesOrden({ orden, usuarios, alHecho, recargar }: Props) {
    const { puede } = useSesion();
    const [abierta, setAbierta] = useState<AccionOrden | null>(null);
    const disponibles = accionesOrdenDisponibles(orden, puede);
    if (disponibles.length === 0) {
        return null;
    }
    return (
        <>
            <div className="flex flex-wrap gap-2" role="group" aria-label="Acciones de la orden">
                {disponibles.map((a, i) => {
                    const def = ACCIONES_ORDEN[a];
                    const Icono = def.icono;
                    return (
                        <Boton key={a} variante={i === 0 && a === "programar" ? "primario" : "secundario"} tamano="sm" onClick={() => setAbierta(a)}>
                            <Icono />
                            {def.etiqueta}
                        </Boton>
                    );
                })}
            </div>
            {abierta ? (
                <DialogoAccionOrden
                    key={abierta}
                    accion={abierta}
                    orden={orden}
                    usuarios={usuarios}
                    alCerrar={() => setAbierta(null)}
                    alHecho={(o) => {
                        setAbierta(null);
                        alHecho(o);
                    }}
                    recargar={recargar}
                />
            ) : null}
        </>
    );
}

type Envio = <T extends FieldValues>(peticion: (clave: string) => Promise<Orden>, campos: readonly Path<T>[], setError: UseFormSetError<T>) => Promise<void>;
type PropsFormulario = { orden: Orden; enviar: Envio; pie: (etiqueta: string, enviando: boolean, faltan?: readonly string[]) => ReactNode };

function DialogoAccionOrden({
    accion,
    orden,
    usuarios,
    alCerrar,
    alHecho,
    recargar,
}: {
    accion: AccionOrden;
    orden: Orden;
    usuarios: readonly Usuario[] | null | undefined;
    alCerrar: () => void;
    alHecho: (o: Orden) => void;
    recargar: () => Promise<void>;
}) {
    const clave = useRef(nuevaClaveIdempotencia());
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const def = ACCIONES_ORDEN[accion];

    const enviar: Envio = async (peticion, campos, setErrorCampo) => {
        setError(null);
        try {
            const o = await peticion(clave.current);
            clave.current = nuevaClaveIdempotencia();
            alHecho(o);
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            if (!erroresACampos(e, campos, setErrorCampo)) {
                setError(e);
            }
        }
    };

    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";

    async function recargarOrden() {
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
                    titulo="Alguien más cambió esta orden"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={recargarOrden} disabled={recargando}>
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
                        Volver
                    </Boton>
                </DialogoCerrar>
                <BotonEnviar enviando={enviando} textoEnviando="Enviando…" disabled={conflicto} faltan={faltan}>
                    {etiqueta}
                </BotonEnviar>
            </DialogoPie>
        </>
    );

    const props: PropsFormulario = { orden, enviar, pie };
    return (
        <DialogoProtegido open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo={def.titulo} descripcion={`${orden.numero} · ${orden.direccion || "Sin dirección"}`}>
                {accion === "asignar" ? <FormAsignar {...props} usuarios={usuarios} /> : null}
                {accion === "programar" ? <FormProgramar {...props} usuarios={usuarios} reprogramar={false} /> : null}
                {accion === "reprogramar" ? <FormProgramar {...props} usuarios={usuarios} reprogramar /> : null}
                {accion === "cancelar" ? <FormMotivo {...props} accion="cancelar" /> : null}
                {accion === "reabrir" ? <FormMotivo {...props} accion="reabrir" /> : null}
                {accion === "pausar" ? <FormPausar {...props} /> : null}
                {accion === "reanudar" ? <FormReanudar {...props} /> : null}
            </DialogoContenido>
        </DialogoProtegido>
    );
}

const ruta = (o: Orden, clave: string) => ({ path: { id: o.id }, header: { "Idempotency-Key": clave } });

/**
 * Técnico: lista de usuarios con rol `tecnico` (con `usuarios.ver`); sin la lista, el id a mano.
 * La API valida que vea la orden y opere en la empresa (422 `tecnico_fuera_de_empresa`).
 */
function CampoTecnico({
    usuarios,
    actual,
    vacio,
    error,
    registro,
}: {
    usuarios: readonly Usuario[] | null | undefined;
    actual: number | null;
    vacio: string;
    error: string | undefined;
    registro: UseFormRegisterReturn;
}) {
    if (usuarios === null) {
        return (
            <Campo
                etiqueta="Técnico (id de usuario)"
                inputMode="numeric"
                maxLength={15}
                ayuda="No tienes acceso a la lista de usuarios: escribe el número del técnico."
                error={error}
                {...registro}
            />
        );
    }
    const tecnicos = tecnicosDe(usuarios);
    const desconocido = actual !== null && !tecnicos.some((t) => t.id === actual);
    return (
        <CampoSelector etiqueta="Técnico" ayuda="Debe ver la orden y operar en esta empresa." error={error} {...registro}>
            <option value="">{vacio}</option>
            {desconocido ? <option value={actual}>{usuarios?.find((u) => u.id === actual)?.nombre ?? `Usuario ${actual}`}</option> : null}
            {tecnicos.map((t) => (
                <option key={t.id} value={t.id}>
                    {t.nombre}
                </option>
            ))}
        </CampoSelector>
    );
}

function FormAsignar({ orden, enviar, pie, usuarios }: PropsFormulario & { usuarios: readonly Usuario[] | null | undefined }) {
    const form = useForm({ resolver: zodResolver(esquemaAsignarOrden), defaultValues: { tecnicoId: orden.tecnicoId === null ? "" : String(orden.tecnicoId) } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const tecnicoAsignar = useWatch({ control: form.control, name: "tecnicoId" });
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    (clave) =>
                        resolver(
                            api().POST("/api/v1/ordenes/{id}/asignar", {
                                params: ruta(orden, clave),
                                body: {
                                    version: orden.version,
                                    ...(d.tecnicoId !== undefined ? { tecnicoId: d.tecnicoId } : {}),
                                    ...(orden.cuadrillaId !== null ? { cuadrillaId: orden.cuadrillaId } : {}),
                                },
                            }),
                        ),
                    ["tecnicoId"],
                    form.setError,
                ),
            )}
        >
            <CampoTecnico usuarios={usuarios} actual={orden.tecnicoId} vacio="Sin técnico" error={form.formState.errors.tecnicoId?.message} registro={form.register("tecnicoId")} />
            {orden.estado === "programada" || orden.estado === "en_camino" ? (
                <p className="text-sm text-muted-foreground">Sin técnico la orden vuelve a pendiente; con otro técnico queda programada para él.</p>
            ) : null}
            {pie("Asignar", form.formState.isSubmitting, orden.tecnicoId === null && !tecnicoAsignar ? ["el técnico"] : [])}
        </form>
    );
}

function FormProgramar({ orden, enviar, pie, usuarios, reprogramar }: PropsFormulario & { usuarios: readonly Usuario[] | null | undefined; reprogramar: boolean }) {
    const previo = guatemalaDesdeIso(orden.programadaInicio);
    const form = useForm({
        resolver: zodResolver(esquemaProgramarOrden({ requiereTecnico: orden.tecnicoId === null, reprogramar })),
        defaultValues: { tecnicoId: orden.tecnicoId === null ? "" : String(orden.tecnicoId), fecha: previo.fecha, hora: previo.hora, motivo: "" },
    });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const errores = form.formState.errors;
    const [tecnicoElegido, fecha, hora] = useWatch({ control: form.control, name: ["tecnicoId", "fecha", "hora"] });
    const faltan = [
        ...(orden.tecnicoId === null && !tecnicoElegido ? ["el técnico"] : []),
        ...(!fecha ? ["la fecha"] : []),
        ...(!hora ? ["la hora"] : []),
    ];
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    (clave) => {
                        const opciones = { params: ruta(orden, clave), body: entradaProgramarOrden(d, orden.version) };
                        return reprogramar
                            ? resolver(api().POST("/api/v1/ordenes/{id}/reprogramar", opciones))
                            : resolver(api().POST("/api/v1/ordenes/{id}/programar", opciones));
                    },
                    ["tecnicoId", "fecha", "hora", "motivo"],
                    form.setError,
                ),
            )}
        >
            <CampoTecnico
                usuarios={usuarios}
                actual={orden.tecnicoId}
                vacio={orden.tecnicoId === null ? "Elige el técnico" : "Mantener el técnico"}
                error={errores.tecnicoId?.message}
                registro={form.register("tecnicoId")}
            />
            <div className="grid grid-cols-2 gap-3">
                <Campo etiqueta="Fecha" type="date" error={errores.fecha?.message} {...form.register("fecha")} />
                <Campo etiqueta="Hora" type="time" step={300} error={errores.hora?.message} {...form.register("hora")} />
            </div>
            <p className="-mt-2 text-sm text-muted-foreground">Hora de Guatemala. El fin se calcula con la duración del tipo de orden.</p>
            {reprogramar ? (
                <CampoArea etiqueta="Motivo" rows={2} maxLength={500} error={errores.motivo?.message} {...form.register("motivo")} />
            ) : null}
            {pie(reprogramar ? "Reprogramar" : "Programar", form.formState.isSubmitting, faltan)}
        </form>
    );
}

function FormMotivo({ orden, enviar, pie, accion }: PropsFormulario & { accion: "cancelar" | "reabrir" }) {
    const form = useForm({ resolver: zodResolver(esquemaMotivoOrden), defaultValues: { motivo: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar(
                    (clave) => {
                        const opciones = { params: ruta(orden, clave), body: { version: orden.version, motivo: d.motivo } };
                        return accion === "cancelar"
                            ? resolver(api().POST("/api/v1/ordenes/{id}/cancelar", opciones))
                            : resolver(api().POST("/api/v1/ordenes/{id}/reabrir", opciones));
                    },
                    ["motivo"],
                    form.setError,
                ),
            )}
        >
            {accion === "cancelar" ? (
                <Aviso tono="warning" titulo="La orden deja de estar en la ruta del técnico">
                    Si viene de un ticket, el ticket recibe una nota interna. Se puede reabrir después.
                </Aviso>
            ) : (
                <p className="text-sm text-muted-foreground">La orden vuelve a pendiente para asignarla y programarla de nuevo.</p>
            )}
            <CampoArea etiqueta={accion === "cancelar" ? "Por qué se cancela" : "Por qué se reabre"} rows={3} maxLength={500} autoFocus error={form.formState.errors.motivo?.message} {...form.register("motivo")} />
            {pie(accion === "cancelar" ? "Cancelar orden" : "Reabrir", form.formState.isSubmitting)}
        </form>
    );
}

function FormPausar({ orden, enviar, pie }: PropsFormulario) {
    const form = useForm<DatosPausar>({ resolver: zodResolver(esquemaPausar), defaultValues: { motivo: "esperando_material", nota: "" } });
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    const motivo = useWatch({ control: form.control, name: "motivo" });
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit((d) =>
                enviar((clave) => resolver(api().POST("/api/v1/ordenes/{id}/pausar", { params: ruta(orden, clave), body: entradaPausar(d, orden.version) })), ["motivo", "nota"], form.setError),
            )}
        >
            <p className="text-sm text-muted-foreground">La cita sigue en la agenda y en la ruta. Para seguir, reanúdala; también se puede reprogramar o cancelar.</p>
            <CampoSelector etiqueta="Motivo" error={form.formState.errors.motivo?.message} {...form.register("motivo")}>
                {(Object.keys(MOTIVOS_PAUSA) as MotivoPausa[]).map((m) => (
                    <option key={m} value={m}>
                        {MOTIVOS_PAUSA[m]}
                    </option>
                ))}
            </CampoSelector>
            <CampoArea
                etiqueta={motivo === "otro" ? "Nota" : "Nota (opcional)"}
                rows={3}
                maxLength={2000}
                error={form.formState.errors.nota?.message}
                {...form.register("nota")}
            />
            {pie("Pausar", form.formState.isSubmitting)}
        </form>
    );
}

function FormReanudar({ orden, enviar, pie }: PropsFormulario) {
    const form = useForm();
    useCambiosSinGuardar(form.formState.isDirty, form.formState.isSubmitting);
    return (
        <form
            noValidate
            className="grid gap-4"
            onSubmit={form.handleSubmit(() => enviar((clave) => resolver(api().POST("/api/v1/ordenes/{id}/reanudar", { params: ruta(orden, clave), body: { version: orden.version } })), [], form.setError))}
        >
            <p className="text-sm text-muted-foreground">
                La orden vuelve a {orden.pausadaDesde === "en_sitio" ? "«En sitio»" : "«En camino»"} y el tiempo de esta pausa se suma al total.
            </p>
            {pie("Reanudar", form.formState.isSubmitting)}
        </form>
    );
}
