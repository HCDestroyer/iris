"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Campo, CampoArea, CampoSelector, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { aplicarRespuesta } from "@/lib/conversaciones/presentacion";
import type { RespuestaRapida } from "@/lib/conversaciones/tipos";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

/** Espejo del validador del servidor (`EntradaRespuestaRapida`). */
export const esquemaRespuesta = z.object({
    atajo: z
        .string()
        .trim()
        .regex(/^\/[a-z0-9_-]{1,30}$/, "Empieza con / y usa minúsculas, números, - o _ (hasta 30)"),
    titulo: z.string().trim().min(1, "Escribe el título").max(80, "Hasta 80 caracteres"),
    colaId: z.string(),
    texto: z.string().trim().min(1, "Escribe el texto").max(4000, "Hasta 4000 caracteres"),
});
type Valores = z.infer<typeof esquemaRespuesta>;

/** Nueva o editar respuesta rápida: 4 campos, en pantalla propia (`FormularioPagina`). */
export function FormularioRespuesta({ respuesta }: { respuesta?: RespuestaRapida }) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const colas = useLectura(async (signal) => (await resolver(api().GET("/api/v1/mensajeria/colas", { signal }))).data ?? []);
    const form = useForm<Valores>({
        resolver: zodResolver(esquemaRespuesta),
        defaultValues: { atajo: respuesta?.atajo ?? "/", titulo: respuesta?.titulo ?? "", colaId: respuesta?.colaId ? String(respuesta.colaId) : "", texto: respuesta?.texto ?? "" },
    });
    const texto = useWatch({ control: form.control, name: "texto" });

    async function guardar(v: Valores) {
        setError(null);
        const cuerpo = { atajo: v.atajo.trim(), titulo: v.titulo.trim(), texto: v.texto.trim(), colaId: v.colaId ? Number(v.colaId) : null };
        try {
            const r = respuesta
                ? await resolver(
                      api().POST("/api/v1/mensajeria/respuestas-rapidas/{id}/actualizar", {
                          params: { path: { id: respuesta.id }, header: { "Idempotency-Key": clave.actual() } },
                          body: { ...cuerpo, version: respuesta.version },
                      }),
                  )
                : await resolver(api().POST("/api/v1/mensajeria/respuestas-rapidas", { params: { header: { "Idempotency-Key": clave.actual() } }, body: cuerpo }));
            clave.renovar();
            avisar({ titulo: respuesta ? `Respuesta «${r.titulo}» guardada` : `Respuesta «${r.titulo}» creada` });
            await liberarProteccion();
            router.replace("/administracion/respuestas-rapidas");
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos(e, ["atajo", "titulo", "colaId", "texto"], form.setError)) setError(e);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo === "conflicto_version";
    return (
        <FormularioPagina
            titulo={respuesta ? `Editar «${respuesta.titulo}»` : "Nueva respuesta rápida"}
            descripcion="Texto plano (WhatsApp no muestra formato). {{nombre}} se cambia por el nombre del cliente."
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion/respuestas-rapidas"
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: respuesta ? "Guardar cambios" : "Crear respuesta", enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más la cambió: vuelve a abrirla para guardar." } : {}) }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió esta respuesta" : "No se pudo guardar"}>
                        {conflicto ? "Vuelve a abrirla desde la lista para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Respuesta">
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo etiqueta="Atajo" ayuda="Se escribe en la bandeja, p. ej. /visita" maxLength={31} autoComplete="off" error={form.formState.errors.atajo?.message} {...form.register("atajo")} />
                    <Campo etiqueta="Título" ayuda="Lo que se ve en el botón" maxLength={80} autoComplete="off" error={form.formState.errors.titulo?.message} {...form.register("titulo")} />
                </div>
                <CampoSelector etiqueta="Cola" ayuda="Solo aparece en las conversaciones de esa cola" error={form.formState.errors.colaId?.message} {...form.register("colaId")}>
                    <option value="">Todas las colas</option>
                    {(colas.estado === "listo" ? colas.datos : []).map((c) => (
                        <option key={c.id} value={String(c.id)}>
                            {c.nombre}
                        </option>
                    ))}
                </CampoSelector>
                <CampoArea etiqueta="Texto" rows={5} maxLength={4000} error={form.formState.errors.texto?.message} {...form.register("texto")} />
                {texto.trim() ? (
                    <div className="grid gap-1">
                        <span className="text-xs font-medium text-muted-foreground">Así lo verá Carlos Ruiz</span>
                        <p className="max-w-md rounded-[0.875rem] rounded-br-sm bg-primary/10 px-3 py-2 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">{aplicarRespuesta(texto, "Carlos Ruiz")}</p>
                    </div>
                ) : null}
            </SeccionFormulario>
        </FormularioPagina>
    );
}
