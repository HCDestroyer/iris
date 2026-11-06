"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Casilla, CampoArea, CampoSelector, FormularioPagina, Interruptor, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { Bot } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearNumero, nombreUso } from "@/lib/conversaciones/presentacion";
import { erroresACampos } from "@/lib/formularios";
import { resumirFranjas } from "@/lib/horarios/franjas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import { leerCanalesConBot } from "@/lib/conversaciones/api-11";
import type { NumeroConBot } from "@/lib/conversaciones/contrato-11";
import { cuerpoCuandoNoHayAgentes, esquemaCuandoNoHayAgentes, MINUTOS_SIN_CONFIRMACION, MINUTOS_SIN_DISPONIBLES } from "@/lib/conversaciones/menu-bot";
import { SeccionBot } from "./bot";

type Canal = NumeroConBot;
const USOS = ["soporte", "cobros", "ventas", "avisos"] as const;

/** Espejo del validador del servidor (`POST /canales/{id}/configurar`, contrato §1 y §10.2). */
export const esquemaNumero = z
    .object({
        usos: z.array(z.enum(USOS)).min(1, "Elige al menos un uso"),
        avisosEmpresa: z.boolean(),
        colaId: z.string(),
        horarioId: z.string(),
        responder: z.boolean(),
        mensaje: z.string().max(1000, "Hasta 1000 caracteres"),
        guardia: z.boolean(),
        guardiaUsuarioId: z.string(),
        guardiaEtiquetas: z.array(z.number()),
        fallaMasiva: z.boolean(),
        bot: esquemaCuandoNoHayAgentes,
    })
    .superRefine((v, ctx) => {
        if (v.avisosEmpresa && !v.usos.includes("avisos")) ctx.addIssue({ code: "custom", path: ["avisosEmpresa"], message: "Marca también el uso «Avisos»" });
        if (v.responder && !v.mensaje.trim()) ctx.addIssue({ code: "custom", path: ["mensaje"], message: "Escribe el mensaje automático" });
        if (v.guardia && !v.guardiaUsuarioId) ctx.addIssue({ code: "custom", path: ["guardiaUsuarioId"], message: "Elige quién está de guardia" });
        if (v.guardia && v.guardiaEtiquetas.length === 0 && !v.fallaMasiva) ctx.addIssue({ code: "custom", path: ["fallaMasiva"], message: "Elige cuándo avisar: falla masiva o una etiqueta" });
    });
export type ValoresNumero = z.infer<typeof esquemaNumero>;
type Valores = ValoresNumero;

/** Saludo de un número que aún no tiene bot: el del contrato (§14.6). */
const SALUDO_INICIAL = "Gracias por escribir. En este momento todos nuestros agentes están ocupados; te respondemos en cuanto se libere uno.";

/** `/administracion/numeros/[id]/configurar`: uso, cola, horario y fuera de horario (más de 3 campos: pantalla propia). */
export function ConfigurarNumero() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        const [canales, colas, horarios, agentes, etiquetas] = await Promise.all([
            leerCanalesConBot(signal),
            resolver(api().GET("/api/v1/mensajeria/colas", { signal })),
            resolver(api().GET("/api/v1/mensajeria/horarios", { signal })),
            resolver(api().GET("/api/v1/mensajeria/conversaciones/agentes", { signal })),
            resolver(api().GET("/api/v1/mensajeria/etiquetas", { signal })),
        ]);
        const canal = canales.find((c) => c.id === id);
        return canal ? { canal, colas: colas.data ?? [], horarios: horarios.data ?? [], agentes: agentes.data ?? [], etiquetas: (etiquetas.data ?? []).filter((e) => e.activa) } : null;
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.canal.nombre : null);
    return (
        <CargaRegistro lectura={lectura} que="el número" noEncontrado="Número no encontrado">
            {(d) => <Formulario {...d} />}
        </CargaRegistro>
    );
}

function Formulario({
    canal,
    colas,
    horarios,
    agentes,
    etiquetas,
}: {
    canal: Canal;
    colas: Schema<"ColaConversaciones">[];
    horarios: Schema<"Horario">[];
    agentes: Schema<"AgenteConversaciones">[];
    etiquetas: Schema<"EtiquetaConversacion">[];
}) {
    const router = useRouter();
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const f = canal.fueraDeHorario;
    const form = useForm<Valores>({
        resolver: zodResolver(esquemaNumero),
        defaultValues: {
            usos: (canal.usos ?? []).filter((u): u is (typeof USOS)[number] => (USOS as readonly string[]).includes(u)),
            avisosEmpresa: canal.avisosEmpresa ?? false,
            colaId: canal.colaId ? String(canal.colaId) : "",
            horarioId: canal.horarioId ? String(canal.horarioId) : "",
            responder: f?.responder ?? false,
            mensaje: f?.mensaje ?? "",
            guardia: f?.guardia?.activa ?? false,
            guardiaUsuarioId: f?.guardia?.usuarioId ? String(f.guardia.usuarioId) : "",
            guardiaEtiquetas: f?.guardia?.etiquetaIds ?? [],
            fallaMasiva: f?.guardia?.fallaMasiva ?? false,
            bot: {
                activo: canal.bot?.activo ?? false,
                atiende: canal.bot?.atiende ?? "bot",
                saludo: canal.bot?.saludo ?? SALUDO_INICIAL,
                conMenu: (canal.bot?.menu ?? []).length > 0,
                menu: (canal.bot?.menu ?? []).map((o) => ({ texto: o.texto, colaId: String(o.colaId), etiquetaIds: o.etiquetaIds ?? [] })),
                minutosSinConfirmacion: String(canal.bot?.minutosSinConfirmacion ?? MINUTOS_SIN_CONFIRMACION),
                minutosSinDisponibles: String(canal.bot?.minutosSinDisponibles ?? MINUTOS_SIN_DISPONIBLES),
            },
        },
    });
    const v = useWatch({ control: form.control });
    const errores = form.formState.errors;
    const horario = horarios.find((h) => String(h.id) === v.horarioId);

    async function guardar(d: Valores) {
        setError(null);
        try {
            await resolver(
                api().POST("/api/v1/mensajeria/canales/{id}/configurar", {
                    params: { path: { id: canal.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: {
                        version: canal.version,
                        usos: d.usos,
                        avisosEmpresa: d.avisosEmpresa,
                        colaId: d.colaId ? Number(d.colaId) : null,
                        horarioId: d.horarioId ? Number(d.horarioId) : null,
                        fueraDeHorario: {
                            responder: d.responder,
                            mensaje: d.mensaje.trim(),
                            guardia: { activa: d.guardia, usuarioId: d.guardiaUsuarioId ? Number(d.guardiaUsuarioId) : null, etiquetaIds: d.guardiaEtiquetas, fallaMasiva: d.fallaMasiva },
                            asistente: { activo: false },
                        },
                        // PROVISIONAL: `bot` (§14.6) aún no está en el cuerpo generado.
                        ...({ bot: cuerpoCuandoNoHayAgentes(d.bot) } as object),
                    },
                }),
            );
            clave.renovar();
            avisar({ titulo: `Número «${canal.nombre}» guardado` });
            await liberarProteccion();
            router.replace("/administracion/numeros");
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos(e, ["usos", "avisosEmpresa", "colaId", "horarioId", "mensaje", "bot.saludo", "bot.menu", "bot.minutosSinConfirmacion", "bot.minutosSinDisponibles", "bot.atiende"], form.setError)) setError(e);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const usos = v.usos ?? [];
    return (
        <FormularioPagina
            titulo={`Configurar ${canal.nombre}`}
            descripcion={formatearNumero(canal.numero)}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion/numeros"
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: "Guardar", enviando: form.formState.isSubmitting, ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió este número: vuelve a abrirlo." } : {}) }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió este número" : "No se pudo guardar"}>
                        {conflicto ? "Vuelve a abrirlo desde la lista para ver el cambio." : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Uso" descripcion="Da color al número en la bandeja y sirve para filtrar; no limita lo que se puede contestar.">
                <fieldset className="grid gap-2 sm:grid-cols-2">
                    <legend className="sr-only">Usos del número</legend>
                    {USOS.map((u) => (
                        <Casilla
                            key={u}
                            etiqueta={nombreUso(u)}
                            checked={usos.includes(u)}
                            onChange={(e) =>
                                form.setValue("usos", e.target.checked ? [...usos, u] : usos.filter((x) => x !== u), { shouldDirty: true, shouldValidate: form.formState.isSubmitted })
                            }
                        />
                    ))}
                </fieldset>
                {errores.usos?.message ? (
                    <p role="alert" className="text-sm text-error">
                        {errores.usos.message}
                    </p>
                ) : null}
                <Interruptor etiqueta="Los avisos de la empresa salen por este número" ayuda="Solo uno por empresa: marcarlo lo quita del anterior." error={errores.avisosEmpresa?.message} {...form.register("avisosEmpresa")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="Bandeja">
                <CampoSelector etiqueta="Cola de las conversaciones nuevas" error={errores.colaId?.message} {...form.register("colaId")}>
                    <option value="">Sin cola</option>
                    {colas
                        .filter((c) => c.activa || String(c.id) === v.colaId)
                        .map((c) => (
                            <option key={c.id} value={String(c.id)}>
                                {c.nombre}
                            </option>
                        ))}
                </CampoSelector>
            </SeccionFormulario>
            <SeccionFormulario titulo="Horario" descripcion="Si la cola tiene horario, vale el de la cola; sin ninguno, siempre abierto.">
                <CampoSelector etiqueta="Horario de atención" error={errores.horarioId?.message} {...form.register("horarioId")}>
                    <option value="">Siempre abierto</option>
                    {horarios.map((h) => (
                        <option key={h.id} value={String(h.id)}>
                            {h.nombre}
                        </option>
                    ))}
                </CampoSelector>
                <p className="text-sm text-muted-foreground">
                    {horario ? resumirFranjas(horario.franjas) : "Sin horario: el SLA corre siempre y no hay respuesta fuera de horario."}{" "}
                    <Link href="/administracion/horarios" className="text-primary underline-offset-2 hover:underline">
                        Administrar horarios
                    </Link>
                </p>
            </SeccionFormulario>
            <SeccionBot control={form.control} register={form.register} setValue={form.setValue} errores={errores} colas={colas} etiquetas={etiquetas} enviado={form.formState.isSubmitted} />
            <SeccionFormulario titulo="Fuera de horario">
                <Interruptor etiqueta="Responder con un mensaje" ayuda="Una sola vez por noche aunque el cliente escriba varias veces; nunca a quien se dio de baja." {...form.register("responder")} />
                {v.responder ? <CampoArea etiqueta="Mensaje automático" rows={3} maxLength={1000} error={errores.mensaje?.message} {...form.register("mensaje")} /> : null}
                <Interruptor etiqueta="Avisar a la guardia" ayuda="Lo agrega a la conversación y le llega un aviso; una vez por conversación y noche." {...form.register("guardia")} />
                {v.guardia ? (
                    <div className="grid gap-3 rounded-lg border border-border p-3">
                        <CampoSelector etiqueta="De guardia" error={errores.guardiaUsuarioId?.message} {...form.register("guardiaUsuarioId")}>
                            <option value="">Elegir…</option>
                            {agentes.map((a) => (
                                <option key={a.id} value={String(a.id)}>
                                    {a.nombre}
                                </option>
                            ))}
                        </CampoSelector>
                        <Casilla etiqueta="Cuando hay una falla masiva en la zona del cliente" {...form.register("fallaMasiva")} />
                        <fieldset className="grid gap-1.5">
                            <legend className="mb-1 text-sm font-medium">O cuando la conversación tiene la etiqueta</legend>
                            {etiquetas.length === 0 ? <p className="text-sm text-muted-foreground">No hay etiquetas activas.</p> : null}
                            {etiquetas.map((e) => {
                                const lista = v.guardiaEtiquetas ?? [];
                                return (
                                    <Casilla
                                        key={e.id}
                                        etiqueta={e.nombre}
                                        checked={lista.includes(e.id)}
                                        onChange={(ev) => form.setValue("guardiaEtiquetas", ev.target.checked ? [...lista, e.id] : lista.filter((x) => x !== e.id), { shouldDirty: true })}
                                    />
                                );
                            })}
                        </fieldset>
                        {errores.fallaMasiva?.message ? (
                            <p role="alert" className="text-sm text-error">
                                {errores.fallaMasiva.message}
                            </p>
                        ) : null}
                    </div>
                ) : null}
                <div className="flex items-start gap-3 rounded-lg bg-background-secondary p-3 opacity-80" data-fase="2">
                    <Bot aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                    <Interruptor etiqueta="Que atienda un asistente" ayuda="Disponible en la fase 2." checked={false} disabled onChange={() => undefined} />
                </div>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
