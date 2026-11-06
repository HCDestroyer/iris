"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Campo, CampoArea, FormularioPagina, GrupoOpciones, GrupoSegmentado, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { BookOpen, Users } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { MarkdownSeguro } from "@/componentes/manuales/markdown";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";

type Manual = Schema<"Manual">;

/** «sin servicio, ONU zte» → ["sin servicio", "onu zte"] (minúsculas, sin repetir, hasta 10). */
export function leerEtiquetas(texto: string): string[] {
    return [...new Set(texto.split(",").map((e) => e.trim().toLowerCase().replace(/\s+/g, " ")).filter(Boolean))].slice(0, 10);
}

/** Espejo del validador del servidor (§11). */
export const esquemaManual = z.object({
    titulo: z.string().trim().min(3, "Escribe el título").max(140, "Hasta 140 caracteres"),
    resumen: z.string().trim().min(1, "Escribe un resumen de una o dos líneas").max(300, "Hasta 300 caracteres"),
    cuerpo: z.string().trim().min(1, "Escribe el contenido").max(20000, "Hasta 20000 caracteres"),
    etiquetas: z.string().max(400).refine((t) => leerEtiquetas(t).every((e) => e.length <= 40), "Cada etiqueta, hasta 40 caracteres"),
    equipo: z.string().max(80, "Hasta 80 caracteres"),
    visibilidad: z.enum(["interna", "cliente"]),
});
type Valores = z.infer<typeof esquemaManual>;

/**
 * Nuevo o editar manual (pantalla propia): título, resumen, cuerpo en Markdown con vista previa
 * segura, etiquetas, equipo y visibilidad («Para el cliente» se puede enviar desde la bandeja).
 */
export function FormularioManual({ manual }: { manual?: Manual }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [vista, setVista] = useState<"escribir" | "vista">("escribir");
    const form = useForm<Valores>({
        resolver: zodResolver(esquemaManual),
        defaultValues: {
            titulo: manual?.titulo ?? "",
            resumen: manual?.resumen ?? "",
            cuerpo: manual?.cuerpo ?? "",
            etiquetas: manual?.etiquetas.join(", ") ?? "",
            equipo: manual?.equipo ?? "",
            visibilidad: manual?.visibilidad === "cliente" ? "cliente" : "interna",
        },
    });
    const cuerpo = useWatch({ control: form.control, name: "cuerpo" });
    const errores = form.formState.errors;

    async function guardar(d: Valores) {
        setError(null);
        const entrada = { titulo: d.titulo.trim(), resumen: d.resumen.trim(), cuerpo: d.cuerpo, etiquetas: leerEtiquetas(d.etiquetas), equipo: d.equipo.trim(), visibilidad: d.visibilidad };
        try {
            const m = manual
                ? await resolver(
                      api().POST("/api/v1/mensajeria/manuales/{id}/actualizar", {
                          params: { path: { id: manual.id }, header: { "Idempotency-Key": clave.actual() } },
                          body: { ...entrada, version: manual.version },
                      }),
                  )
                : await resolver(api().POST("/api/v1/mensajeria/manuales", { params: { header: { "Idempotency-Key": clave.actual() } }, body: entrada }));
            clave.renovar();
            avisar({ titulo: manual ? `Manual «${m.titulo}» guardado` : `Manual «${m.titulo}» creado` });
            await liberarProteccion();
            router.replace("/administracion/manuales");
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos(e, ["titulo", "resumen", "cuerpo", "etiquetas", "equipo", "visibilidad"], form.setError)) setError(e);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <FormularioPagina
            titulo={manual ? `Editar «${manual.titulo}»` : "Nuevo manual"}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion/manuales"
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: manual ? "Guardar cambios" : "Crear manual", enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más lo cambió: vuelve a abrirlo." } : {}) }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió este manual" : "No se pudo guardar"}>
                        {conflicto ? "Vuelve a abrirlo desde la biblioteca para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Artículo">
                <Campo etiqueta="Título" maxLength={140} autoComplete="off" error={errores.titulo?.message} {...form.register("titulo")} />
                <CampoArea etiqueta="Resumen" ayuda="Se ve en las sugerencias de la bandeja." rows={2} maxLength={300} error={errores.resumen?.message} {...form.register("resumen")} />
                <div className="grid gap-2">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <span className="text-sm font-medium">Contenido</span>
                        <GrupoSegmentado
                            etiqueta="Ver el contenido"
                            valor={vista}
                            alCambiar={setVista}
                            opciones={[
                                { valor: "escribir", etiqueta: "Escribir" },
                                { valor: "vista", etiqueta: "Vista previa" },
                            ]}
                        />
                    </div>
                    {vista === "escribir" ? (
                        <CampoArea
                            etiqueta="Contenido en Markdown"
                            ayuda="# Título, - lista, 1. pasos, **negrita**, [enlace](https://…). Nunca HTML."
                            className="[&_label]:sr-only"
                            rows={14}
                            maxLength={20000}
                            error={errores.cuerpo?.message}
                            {...form.register("cuerpo")}
                        />
                    ) : (
                        <div className="min-h-40 rounded-lg border border-border bg-card p-4" aria-label="Vista previa" role="region">
                            {cuerpo.trim() ? <MarkdownSeguro fuente={cuerpo} /> : <p className="text-sm text-muted-foreground">Aún no hay contenido.</p>}
                        </div>
                    )}
                </div>
            </SeccionFormulario>
            <SeccionFormulario titulo="Para encontrarlo">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Etiquetas" ayuda="Separadas por coma: sin servicio, onu, potencia" maxLength={400} autoComplete="off" error={errores.etiquetas?.message} {...form.register("etiquetas")} />
                    <Campo etiqueta="Equipo (opcional)" ayuda="Modelo, p. ej. ZTE F670L" maxLength={80} autoComplete="off" error={errores.equipo?.message} {...form.register("equipo")} />
                </div>
                <Controller
                    control={form.control}
                    name="visibilidad"
                    render={({ field }) => (
                        <GrupoOpciones
                            leyenda="Quién lo ve"
                            nombre="visibilidad"
                            valor={field.value}
                            alCambiar={(v) => field.onChange(v)}
                            opciones={[
                                { valor: "interna", etiqueta: "Del equipo", detalle: "Solo para los agentes; nunca se envía al cliente.", icono: BookOpen },
                                { valor: "cliente", etiqueta: "Para el cliente", detalle: "Se puede enviar desde la bandeja como enlace o texto.", icono: Users },
                            ]}
                        />
                    )}
                />
            </SeccionFormulario>
        </FormularioPagina>
    );
}
