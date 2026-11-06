"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, Campo, CampoFecha, CampoHora, CampoSelector, Casilla, cn, FormularioPagina, Interruptor, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { Plus, Trash2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useFieldArray, useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { DIAS_SEMANA, resumirFranjas, validarFranjas } from "@/lib/horarios/franjas";
import { useClaveIdempotencia } from "@/lib/idempotencia";

type Horario = Schema<"Horario">;

const ZONAS = ["America/Guatemala", "America/El_Salvador", "America/Tegucigalpa", "America/Managua", "America/Costa_Rica", "America/Mexico_City"];

const franja = z.object({ dias: z.array(z.number()), inicio: z.string(), fin: z.string(), perfil: z.string().nullable() });

/** Espejo del validador del servidor (§10.1). */
export const esquemaHorario = z
    .object({
        nombre: z.string().trim().min(1, "Escribe el nombre").max(80, "Hasta 80 caracteres"),
        zona: z.string().min(1),
        franjas: z.array(franja).max(28, "Hasta 28 franjas"),
        feriados: z
            .array(z.object({ fecha: z.string().regex(/^\d{4}-\d{2}-\d{2}$/, "Elige la fecha"), nombre: z.string().trim().min(1, "Escribe el nombre").max(80), cerrado: z.boolean(), inicio: z.string(), fin: z.string() }))
            .max(60, "Hasta 60 feriados"),
    })
    .superRefine((v, ctx) => {
        for (const e of validarFranjas(v.franjas)) ctx.addIssue({ code: "custom", path: ["franjas", e.indice, e.campo], message: e.mensaje });
        v.feriados.forEach((f, i) => {
            if (!f.cerrado && validarFranjas([{ dias: [1], inicio: f.inicio, fin: f.fin }]).length > 0) ctx.addIssue({ code: "custom", path: ["feriados", i, "fin"], message: "Revisa las horas" });
            if (v.feriados.findIndex((x) => x.fecha === f.fecha) !== i) ctx.addIssue({ code: "custom", path: ["feriados", i, "fecha"], message: "Esa fecha ya está" });
        });
    });
type Valores = z.infer<typeof esquemaHorario>;

function valores(h?: Horario): Valores {
    return {
        nombre: h?.nombre ?? "",
        zona: h?.zona ?? "America/Guatemala",
        franjas: h?.franjas?.map((f) => ({ dias: [...(f.dias ?? [])], inicio: f.inicio, fin: f.fin, perfil: f.perfil ?? null })) ?? [
            { dias: [1, 2, 3, 4, 5], inicio: "08:00", fin: "18:00", perfil: null },
            { dias: [6], inicio: "08:00", fin: "12:00", perfil: null },
        ],
        feriados:
            h?.feriados?.map((f) => ({ fecha: f.fecha, nombre: f.nombre, cerrado: (f.franjas ?? []).length === 0, inicio: f.franjas?.[0]?.inicio ?? "08:00", fin: f.franjas?.[0]?.fin ?? "12:00" })) ?? [],
    };
}

/**
 * Nuevo o editar horario (pantalla propia): nombre, zona, franjas por día y feriados. Una franja
 * que cruza la medianoche se escribe como dos. `perfil` (fase 2) se conserva tal cual.
 */
export function FormularioHorario({ horario }: { horario?: Horario }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const form = useForm<Valores>({ resolver: zodResolver(esquemaHorario), defaultValues: valores(horario) });
    const franjas = useFieldArray({ control: form.control, name: "franjas" });
    const feriados = useFieldArray({ control: form.control, name: "feriados" });
    const v = useWatch({ control: form.control });
    const errores = form.formState.errors;

    async function guardar(d: Valores) {
        setError(null);
        const cuerpo = {
            nombre: d.nombre.trim(),
            zona: d.zona,
            franjas: d.franjas.map((f) => ({ dias: [...f.dias].sort((a, b) => a - b), inicio: f.inicio, fin: f.fin, perfil: f.perfil })),
            feriados: d.feriados.map((f) => ({ fecha: f.fecha, nombre: f.nombre.trim(), franjas: f.cerrado ? [] : [{ dias: [], inicio: f.inicio, fin: f.fin, perfil: null }] })),
        };
        try {
            const h = horario
                ? await resolver(
                      api().POST("/api/v1/mensajeria/horarios/{id}/actualizar", {
                          params: { path: { id: horario.id }, header: { "Idempotency-Key": clave.actual() } },
                          body: { ...cuerpo, version: horario.version },
                      }),
                  )
                : await resolver(api().POST("/api/v1/mensajeria/horarios", { params: { header: { "Idempotency-Key": clave.actual() } }, body: cuerpo }));
            clave.renovar();
            avisar({ titulo: horario ? `Horario «${h.nombre}» guardado` : `Horario «${h.nombre}» creado` });
            await liberarProteccion();
            router.replace("/administracion/horarios");
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos(e, ["nombre", "zona"], form.setError)) setError(e);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    return (
        <FormularioPagina
            titulo={horario ? `Editar «${horario.nombre}»` : "Nuevo horario"}
            descripcion={v.franjas ? resumirFranjas(v.franjas.map((f) => ({ dias: f?.dias ?? [], inicio: f?.inicio ?? "", fin: f?.fin ?? "" }))) : undefined}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion/horarios"
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: horario ? "Guardar cambios" : "Crear horario", enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más lo cambió: vuelve a abrirlo." } : {}) }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió este horario" : "No se pudo guardar"}>
                        {conflicto ? "Vuelve a abrirlo desde la lista para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Horario">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Nombre" placeholder="Soporte · extendido" maxLength={80} autoComplete="off" error={errores.nombre?.message} {...form.register("nombre")} />
                    <CampoSelector etiqueta="Zona horaria" {...form.register("zona")}>
                        {ZONAS.map((z) => (
                            <option key={z} value={z}>
                                {z.replace("America/", "").replace("_", " ")}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
            </SeccionFormulario>
            <SeccionFormulario titulo="Franjas" descripcion="Si cruza la medianoche (22:00 a 06:00), escríbela como dos franjas.">
                <ul className="grid gap-3" aria-label="Franjas">
                    {franjas.fields.map((campo, i) => {
                        const dias = v.franjas?.[i]?.dias ?? [];
                        const e = errores.franjas?.[i];
                        return (
                            <li key={campo.id} className="grid gap-3 rounded-lg border border-border p-3" data-franja={i + 1}>
                                <fieldset className="flex flex-wrap gap-1.5">
                                    <legend className="mb-1.5 text-sm font-medium">Franja {i + 1}: días</legend>
                                    {DIAS_SEMANA.map((d) => (
                                        <span key={d.dia} className={cn("rounded-md border px-2 py-1", dias.includes(d.dia) ? "border-primary/40 bg-primary/5" : "border-border")}>
                                            <Casilla
                                                etiqueta={d.corto}
                                                aria-label={d.nombre}
                                                checked={dias.includes(d.dia)}
                                                onChange={(ev) =>
                                                    form.setValue(`franjas.${i}.dias`, ev.target.checked ? [...dias, d.dia] : dias.filter((x) => x !== d.dia), { shouldDirty: true, shouldValidate: form.formState.isSubmitted })
                                                }
                                            />
                                        </span>
                                    ))}
                                </fieldset>
                                {e?.dias?.message ? (
                                    <p role="alert" className="text-sm text-error">
                                        {e.dias.message}
                                    </p>
                                ) : null}
                                <div className="flex flex-wrap items-end gap-3">
                                    <CampoHora etiqueta="Desde" className="w-32" error={e?.inicio?.message} {...form.register(`franjas.${i}.inicio`)} />
                                    <CampoHora etiqueta="Hasta" className="w-32" error={e?.fin?.message} {...form.register(`franjas.${i}.fin`)} />
                                    <Boton variante="fantasma" tamano="sm" className="ml-auto text-error" onClick={() => franjas.remove(i)} aria-label={`Quitar la franja ${i + 1}`}>
                                        <Trash2 />
                                        Quitar
                                    </Boton>
                                </div>
                            </li>
                        );
                    })}
                </ul>
                {franjas.fields.length === 0 ? <Aviso tono="warning" titulo="Sin franjas el horario queda siempre cerrado" /> : null}
                <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={() => franjas.append({ dias: [], inicio: "08:00", fin: "17:00", perfil: null })}>
                    <Plus />
                    Agregar franja
                </Boton>
            </SeccionFormulario>
            <SeccionFormulario titulo="Feriados" descripcion="Un feriado cerrado no atiende en todo el día; si no, reemplaza las franjas de ese día.">
                <ul className="grid gap-3" aria-label="Feriados">
                    {feriados.fields.map((campo, i) => {
                        const e = errores.feriados?.[i];
                        const cerrado = v.feriados?.[i]?.cerrado ?? true;
                        return (
                            <li key={campo.id} className="grid gap-3 rounded-lg border border-border p-3" data-feriado={i + 1}>
                                <div className="grid gap-3 sm:grid-cols-[12rem_minmax(0,1fr)]">
                                    <CampoFecha etiqueta="Fecha" error={e?.fecha?.message} {...form.register(`feriados.${i}.fecha`)} />
                                    <Campo etiqueta="Nombre" placeholder="Navidad" maxLength={80} autoComplete="off" error={e?.nombre?.message} {...form.register(`feriados.${i}.nombre`)} />
                                </div>
                                <div className="flex flex-wrap items-end gap-3">
                                    <Interruptor etiqueta="Cerrado todo el día" {...form.register(`feriados.${i}.cerrado`)} />
                                    {cerrado ? null : (
                                        <>
                                            <CampoHora etiqueta="Desde" className="w-32" {...form.register(`feriados.${i}.inicio`)} />
                                            <CampoHora etiqueta="Hasta" className="w-32" error={e?.fin?.message} {...form.register(`feriados.${i}.fin`)} />
                                        </>
                                    )}
                                    <Boton variante="fantasma" tamano="sm" className="ml-auto text-error" onClick={() => feriados.remove(i)} aria-label={`Quitar el feriado ${i + 1}`}>
                                        <Trash2 />
                                        Quitar
                                    </Boton>
                                </div>
                            </li>
                        );
                    })}
                </ul>
                <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={() => feriados.append({ fecha: "", nombre: "", cerrado: true, inicio: "08:00", fin: "12:00" })}>
                    <Plus />
                    Agregar feriado
                </Boton>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
