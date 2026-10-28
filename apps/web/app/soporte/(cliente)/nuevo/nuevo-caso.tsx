"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoArea, CampoSelector, Tarjeta } from "@iris/ui";
import { RotateCw, Send } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { ErrorEnvio } from "@/componentes/portal/error-envio";
import { VolverACasos } from "@/componentes/portal/volver";
import { resolver } from "@/lib/api/cliente";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { apiPortal } from "@/lib/portal/cliente";
import { entradaNuevoTicket, esquemaNuevoTicket, type DatosNuevoTicket, type EntradaFormNuevo } from "@/lib/portal/esquemas";

const VACIO: EntradaFormNuevo = { asunto: "", categoriaId: "", descripcion: "" };
const CAMPOS = ["asunto", "categoriaId", "descripcion"] as const;

export function NuevoCaso() {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [errorGeneral, setErrorGeneral] = useState<unknown>(null);
    const categorias = useLectura((signal) => resolver(apiPortal().GET("/api/v1/portal/categorias", { signal })));
    const form = useForm<EntradaFormNuevo, unknown, DatosNuevoTicket>({ resolver: zodResolver(esquemaNuevoTicket), defaultValues: VACIO });
    const errores = form.formState.errors;

    async function enviar(datos: DatosNuevoTicket) {
        setErrorGeneral(null);
        try {
            const ticket = await resolver(
                apiPortal().POST("/api/v1/portal/tickets", { body: entradaNuevoTicket(datos), params: { header: { "Idempotency-Key": clave.actual() } } }),
            );
            clave.renovar();
            router.push(`/soporte/${ticket.id}`);
        } catch (error) {
            clave.trasError(error);
            if (!erroresACampos(error, CAMPOS, form.setError)) {
                setErrorGeneral(error);
            }
        }
    }

    const lista = categorias.estado === "listo" ? (categorias.datos.data ?? []) : [];

    return (
        <>
            <VolverACasos />
            <h1 className="mb-1 text-2xl font-semibold tracking-[-0.015em]">Reportar un problema</h1>
            <p className="mb-5 text-sm text-muted-foreground">Cuéntanos qué pasa y te responderemos por aquí.</p>
            <Tarjeta className="p-5">
                <form noValidate className="grid gap-4" onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                    <Campo etiqueta="Asunto" placeholder="Ej.: No tengo internet desde la mañana" maxLength={200} error={errores.asunto?.message} {...form.register("asunto")} />
                    {categorias.estado === "error" ? (
                        <Aviso
                            tono="warning"
                            titulo="No pudimos cargar los tipos de problema"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={categorias.reintentar}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        >
                            Puedes enviar tu caso sin elegirlo.
                        </Aviso>
                    ) : (
                        <CampoSelector
                            etiqueta="Tipo de problema (opcional)"
                            disabled={categorias.estado === "cargando"}
                            error={errores.categoriaId?.message}
                            {...form.register("categoriaId")}
                        >
                            <option value="">{categorias.estado === "cargando" ? "Cargando…" : "No estoy seguro"}</option>
                            {lista.map((c) => (
                                <option key={c.id} value={String(c.id)}>
                                    {c.nombre}
                                </option>
                            ))}
                        </CampoSelector>
                    )}
                    <CampoArea
                        etiqueta="¿Qué está pasando?"
                        rows={6}
                        maxLength={20000}
                        ayuda="Desde cuándo pasa, qué luces ves en tu equipo y qué ya intentaste."
                        error={errores.descripcion?.message}
                        {...form.register("descripcion")}
                    />
                    {errorGeneral ? <ErrorEnvio error={errorGeneral} /> : null}
                    <div className="flex justify-end">
                        <Boton type="submit" disabled={form.formState.isSubmitting}>
                            <Send />
                            {form.formState.isSubmitting ? "Enviando…" : "Enviar"}
                        </Boton>
                    </div>
                </form>
            </Tarjeta>
        </>
    );
}
