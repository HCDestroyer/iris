"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Campo, CampoArea, CampoSelector, Dialogo, DialogoCerrar, DialogoContenido, DialogoDisparador, DialogoPie } from "@iris/ui";
import { ClipboardPlus, RotateCw } from "lucide-react";
import { useRouter } from "next/navigation";
import { useRef, useState } from "react";
import { useForm } from "react-hook-form";
import type { Usuario } from "@/componentes/tickets/catalogos";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useLectura } from "@/lib/lectura";
import { ORDEN_TIPOS, TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import {
    entradaCrearOrdenDeTicket,
    esquemaCrearOrdenDeTicket,
    MENSAJE_ASIGNACION_REQUERIDA,
    type DatosCrearOrdenDeTicket,
    type EntradaFormCrearOrdenDeTicket,
} from "@/lib/ordenes/ticket-orden";
import { tecnicosDe } from "./catalogos";

type TicketOrigen = Pick<Schema<"Ticket">, "id" | "numero" | "asunto" | "asignadoA" | "cuadrillaId">;

const CAMPOS = ["tipo", "tecnicoId", "cuadrillaId", "fecha", "hora", "descripcion", "direccion"] as const;

function inicial(ticket: TicketOrigen, tecnicos: readonly Usuario[] | null): EntradaFormCrearOrdenDeTicket {
    // Propone la asignación del ticket: su responsable si es técnico y su cuadrilla.
    const responsable = ticket.asignadoA !== null && (tecnicos === null || tecnicos.some((t) => t.id === ticket.asignadoA)) ? String(ticket.asignadoA) : "";
    return {
        tipo: "visita",
        tecnicoId: responsable,
        cuadrillaId: ticket.cuadrillaId === null ? "" : String(ticket.cuadrillaId),
        fecha: "",
        hora: "",
        descripcion: ticket.asunto,
        direccion: "",
    };
}

/**
 * «Crear orden y asignar» desde el ticket (`POST /tickets/{id}/crear-orden`): en un paso crea la
 * orden ligada (visita por defecto; hereda cliente, servicio, zona y contacto), la asigna al
 * técnico y/o la cuadrilla (propone los del ticket) y, si se elige fecha, la programa. No deja
 * guardar sin técnico ni cuadrilla. Al crear, abre la orden.
 */
export function DialogoCrearOrdenTicket({ ticket, usuarios }: { ticket: TicketOrigen; usuarios: readonly Usuario[] | null | undefined }) {
    const router = useRouter();
    const [abierto, setAbierto] = useState(false);
    // Sin la lista (sin usuarios.ver, o no se pudo leer) el técnico se escribe por número.
    const tecnicos = usuarios === null || usuarios === undefined ? null : tecnicosDe(usuarios);
    return (
        <Dialogo open={abierto} onOpenChange={setAbierto}>
            <DialogoDisparador asChild>
                <Boton variante="secundario" tamano="sm">
                    <ClipboardPlus />
                    Crear orden y asignar
                </Boton>
            </DialogoDisparador>
            <DialogoContenido titulo="Crear orden y asignar" descripcion={`${ticket.numero} · ${ticket.asunto}`}>
                {abierto ? (
                    <Formulario
                        ticket={ticket}
                        tecnicos={tecnicos}
                        usuarios={usuarios}
                        alCrear={(orden) => {
                            setAbierto(false);
                            router.push(`/ordenes/${orden.id}`);
                        }}
                    />
                ) : null}
            </DialogoContenido>
        </Dialogo>
    );
}

function Formulario({
    ticket,
    tecnicos,
    usuarios,
    alCrear,
}: {
    ticket: TicketOrigen;
    tecnicos: readonly Usuario[] | null;
    usuarios: readonly Usuario[] | null | undefined;
    alCrear: (orden: Schema<"Orden">) => void;
}) {
    const clave = useRef(nuevaClaveIdempotencia());
    const [errorGeneral, setErrorGeneral] = useState<unknown>(null);
    const cuadrillas = useLectura<Schema<"Cuadrilla">[]>(async (signal) =>
        ((await resolver(api().GET("/api/v1/cuadrillas", { signal }))).data ?? []).filter((c) => c.activa || c.id === ticket.cuadrillaId),
    );
    const form = useForm<EntradaFormCrearOrdenDeTicket, unknown, DatosCrearOrdenDeTicket>({
        resolver: zodResolver(esquemaCrearOrdenDeTicket),
        defaultValues: inicial(ticket, tecnicos),
    });
    const errores = form.formState.errors;
    const sinAsignacion = errores.tecnicoId?.message === MENSAJE_ASIGNACION_REQUERIDA || errores.cuadrillaId?.message === MENSAJE_ASIGNACION_REQUERIDA;

    async function crear(datos: DatosCrearOrdenDeTicket) {
        setErrorGeneral(null);
        try {
            const orden = await resolver(
                api().POST("/api/v1/tickets/{id}/crear-orden", {
                    params: { path: { id: ticket.id }, header: { "Idempotency-Key": clave.current } },
                    body: entradaCrearOrdenDeTicket(datos),
                }),
            );
            clave.current = nuevaClaveIdempotencia();
            alCrear(orden);
        } catch (error) {
            clave.current = claveTrasError(error, clave.current);
            if (!erroresACampos(error, CAMPOS, form.setError)) {
                setErrorGeneral(error);
            }
        }
    }

    const responsableNoTecnico = ticket.asignadoA !== null && tecnicos !== null && !tecnicos.some((t) => t.id === ticket.asignadoA);
    return (
        <form noValidate className="grid gap-4" onSubmit={(e) => void form.handleSubmit(crear)(e)} data-testid="form-crear-orden-ticket">
            {sinAsignacion ? (
                <Aviso tono="warning" titulo="Falta a quién asignarla" data-testid="aviso-asignacion">
                    Elige un técnico, una cuadrilla o los dos para crear la orden.
                </Aviso>
            ) : (
                <p className="text-sm text-muted-foreground">
                    La orden hereda el cliente, el servicio, la zona y el contacto del ticket, y el ticket recibe una nota interna.
                </p>
            )}
            <div className="grid gap-4 sm:grid-cols-2">
                <CampoSelector etiqueta="Tipo" error={errores.tipo?.message} {...form.register("tipo")}>
                    {ORDEN_TIPOS.map((t) => (
                        <option key={t} value={t}>
                            {TIPOS_ORDEN[t].texto}
                        </option>
                    ))}
                </CampoSelector>
                {tecnicos === null ? (
                    <Campo
                        etiqueta="Técnico (id de usuario)"
                        inputMode="numeric"
                        maxLength={15}
                        ayuda={
                            usuarios === null
                                ? "No tienes acceso a la lista de usuarios: escribe el número del técnico."
                                : "No se pudo leer la lista de usuarios: escribe el número del técnico."
                        }
                        error={errores.tecnicoId?.message}
                        {...form.register("tecnicoId")}
                    />
                ) : (
                    <CampoSelector
                        etiqueta="Técnico"
                        ayuda={responsableNoTecnico ? "El responsable del ticket no es técnico: elige quién va." : undefined}
                        error={errores.tecnicoId?.message}
                        {...form.register("tecnicoId")}
                    >
                        <option value="">Sin técnico</option>
                        {tecnicos.map((t) => (
                            <option key={t.id} value={t.id}>
                                {t.id === ticket.asignadoA ? `${t.nombre} (del ticket)` : t.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                )}
            </div>
            {cuadrillas.estado === "error" ? (
                <Aviso
                    tono="error"
                    titulo="No se pudieron cargar las cuadrillas"
                    accion={
                        <Boton type="button" variante="secundario" tamano="sm" onClick={cuadrillas.reintentar}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    Puedes asignar solo el técnico, o reintentar.
                </Aviso>
            ) : (
                <CampoSelector
                    etiqueta="Cuadrilla"
                    disabled={cuadrillas.estado === "cargando"}
                    error={errores.cuadrillaId?.message}
                    {...form.register("cuadrillaId")}
                >
                    <option value="">{cuadrillas.estado === "cargando" ? "Cargando…" : "Sin cuadrilla"}</option>
                    {cuadrillas.estado === "listo"
                        ? cuadrillas.datos.map((c) => (
                              <option key={c.id} value={c.id}>
                                  {c.id === ticket.cuadrillaId ? `${c.nombre} (del ticket)` : c.nombre}
                              </option>
                          ))
                        : null}
                </CampoSelector>
            )}
            <fieldset className="grid gap-2 rounded-md border p-3">
                <legend className="px-1 text-sm font-medium">Visita (opcional)</legend>
                <div className="grid grid-cols-2 gap-3">
                    <Campo etiqueta="Fecha" type="date" error={errores.fecha?.message} {...form.register("fecha")} />
                    <Campo etiqueta="Hora" type="time" step={300} error={errores.hora?.message} {...form.register("hora")} />
                </div>
                <p className="text-xs text-muted-foreground">Hora de Guatemala. Con fecha, la orden queda programada para el técnico; sin fecha, pendiente.</p>
            </fieldset>
            <CampoArea etiqueta="Trabajo a realizar" rows={3} maxLength={4000} error={errores.descripcion?.message} {...form.register("descripcion")} />
            <Campo
                etiqueta="Dirección (opcional)"
                ayuda="Vacía: la del servicio o el cliente en Argos."
                maxLength={300}
                autoComplete="off"
                error={errores.direccion?.message}
                {...form.register("direccion")}
            />
            {errorGeneral ? (
                <Aviso tono="error" titulo="No se pudo crear la orden">
                    {[mensajeDeError(errorGeneral).titulo, mensajeDeError(errorGeneral).detalle].filter(Boolean).join(". ")}
                </Aviso>
            ) : null}
            <DialogoPie>
                <DialogoCerrar asChild>
                    <Boton variante="contorno">Cancelar</Boton>
                </DialogoCerrar>
                <Boton type="submit" disabled={form.formState.isSubmitting}>
                    {form.formState.isSubmitting ? "Creando…" : "Crear y asignar"}
                </Boton>
            </DialogoPie>
        </form>
    );
}
