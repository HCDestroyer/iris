"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Campo, CampoArea, Casilla, cn, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { Bot, Settings2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { z } from "zod";
import { SelectorPersonas } from "@/componentes/conversaciones/selector-personas";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { actualizarCola, crearCola, fijarMiembros, fijarSupervisores, type CuerpoCola } from "@/lib/conversaciones/api-equipo";
import type { Agente, Cola, Numero } from "@/lib/conversaciones/tipos";
import { MINUTOS_SIN_CONFIRMACION, MINUTOS_SIN_DISPONIBLES } from "@/lib/conversaciones/menu-bot";
import { CLASE_PUNTO, formatearNumero, tonoDeCanal } from "@/lib/conversaciones/presentacion";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";

/** Minutos opcionales hasta `max`: vacío = el valor del número (o sin SLA). */
const minutosOpcionales = (max: number) =>
    z
        .string()
        .trim()
        .refine((v) => v === "" || (/^\d+$/.test(v) && Number(v) >= 1 && Number(v) <= max), `Entre 1 y ${max} minutos, o vacío`);

/** Espejo del validador del servidor (`POST /colas`, `/actualizar`, `/miembros`, `/supervisores`; §2 y §14.1). */
export const esquemaCola = z.object({
    nombre: z.string().trim().min(1, "Escribe el nombre de la cola").max(80, "Hasta 80 caracteres"),
    descripcion: z.string().trim().max(300, "Hasta 300 caracteres"),
    // Como el servidor: el SLA hasta una semana; los minutos del bot hasta un día.
    slaRespuestaMin: minutosOpcionales(10080),
    botSinConfirmacionMin: minutosOpcionales(1440),
    botSinDisponiblesMin: minutosOpcionales(1440),
    miembros: z.array(z.number()),
    supervisores: z.array(z.number()).max(20, "Hasta 20 supervisores"),
});
type Valores = z.infer<typeof esquemaCola>;

const aMinutos = (v: string) => (v.trim() === "" ? undefined : Number(v));
const mismos = (a: readonly number[], b: readonly number[]) => a.length === b.length && a.every((x) => b.includes(x));
/** ¿Lo guardado de la cola ya es lo que hay en el formulario? */
const mismaCola = (c: Cola, cuerpo: CuerpoCola) =>
    c.nombre === cuerpo.nombre &&
    c.descripcion === (cuerpo.descripcion ?? "") &&
    c.slaRespuestaMin === (cuerpo.slaRespuestaMin ?? null) &&
    c.botSinConfirmacionMin === (cuerpo.botSinConfirmacionMin ?? null) &&
    c.botSinDisponiblesMin === (cuerpo.botSinDisponiblesMin ?? null);

type Props = { cola?: Cola; agentes: Agente[]; canales: Numero[] };

/**
 * `/administracion/colas/nueva` y `/[id]/editar` (fase 1.1): nombre, miembros y supervisores (con
 * su punto de disponibilidad), los números que llegan a la cola y los minutos del bot, que mandan
 * sobre los del número.
 */
export function FormularioCola({ cola, agentes, canales }: Props) {
    const router = useRouter();
    const claveCola = useClaveIdempotencia();
    const claveMiembros = useClaveIdempotencia();
    const claveSupervisores = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    // Lo ya guardado de un intento que falló a medias (p. ej. la cola sí, los miembros no).
    const [guardada, setGuardada] = useState<Cola | null>(null);
    const form = useForm<Valores>({
        resolver: zodResolver(esquemaCola),
        defaultValues: {
            nombre: cola?.nombre ?? "",
            descripcion: cola?.descripcion ?? "",
            slaRespuestaMin: cola?.slaRespuestaMin ? String(cola.slaRespuestaMin) : "",
            botSinConfirmacionMin: cola?.botSinConfirmacionMin ? String(cola.botSinConfirmacionMin) : "",
            botSinDisponiblesMin: cola?.botSinDisponiblesMin ? String(cola.botSinDisponiblesMin) : "",
            miembros: (cola?.miembros ?? []).map((m) => m.id),
            supervisores: (cola?.supervisores ?? []).map((m) => m.id),
        },
    });
    const v = useWatch({ control: form.control });
    const errores = form.formState.errors;
    const numeros = cola ? canales.filter((c) => c.colaId === cola.id) : [];

    async function guardar(d: Valores) {
        setError(null);
        const base = guardada ?? cola;
        const cuerpo: CuerpoCola = {
            nombre: d.nombre.trim(),
            descripcion: d.descripcion.trim(),
            slaRespuestaMin: aMinutos(d.slaRespuestaMin) ?? null,
            // Lo que el formulario no edita se conserva (al actualizar, ausente = null).
            slaPorVencerMin: base?.slaPorVencerMin ?? null,
            ...(base?.horarioId !== undefined ? { horarioId: base.horarioId } : {}),
            ...(aMinutos(d.botSinConfirmacionMin) !== undefined ? { botSinConfirmacionMin: Number(d.botSinConfirmacionMin) } : {}),
            ...(aMinutos(d.botSinDisponiblesMin) !== undefined ? { botSinDisponiblesMin: Number(d.botSinDisponiblesMin) } : {}),
        };
        let actual = base ?? null;
        let paso: "cola" | "miembros" | "supervisores" = "cola";
        // Tras un guardado a medias, el paso de la cola se repite si sus campos cambiaron desde entonces.
        const colaPendiente = !guardada || !mismaCola(guardada, cuerpo);
        try {
            if (colaPendiente) {
                actual = base ? await actualizarCola(base.id, { ...cuerpo, version: base.version }, claveCola.actual()) : await crearCola(cuerpo, claveCola.actual());
                claveCola.renovar();
                setGuardada(actual);
            }
            if (!actual) return;
            paso = "miembros";
            if (!mismos(d.miembros, (actual.miembros ?? []).map((m) => m.id))) {
                actual = await fijarMiembros(actual.id, actual.version, d.miembros, claveMiembros.actual());
                claveMiembros.renovar();
                setGuardada(actual);
            }
            paso = "supervisores";
            if (!mismos(d.supervisores, (actual.supervisores ?? []).map((m) => m.id))) {
                actual = await fijarSupervisores(actual.id, actual.version, d.supervisores, claveSupervisores.actual());
                claveSupervisores.renovar();
            }
            avisar({ titulo: cola ? `Cola «${actual.nombre}» guardada` : `Cola «${actual.nombre}» creada` });
            await liberarProteccion();
            router.replace("/administracion/colas");
        } catch (e) {
            (paso === "cola" ? claveCola : paso === "miembros" ? claveMiembros : claveSupervisores).trasError(e);
            const campos = paso === "cola" ? (["nombre", "descripcion", "slaRespuestaMin", "botSinConfirmacionMin", "botSinDisponiblesMin"] as const) : paso === "miembros" ? (["miembros"] as const) : (["supervisores"] as const);
            if (!erroresACampos(e, campos, form.setError)) setError(e);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409;
    const resumen = cola
        ? `${cola.miembros.length} ${cola.miembros.length === 1 ? "miembro" : "miembros"} · ${cola.supervisores?.length ?? 0} ${(cola.supervisores?.length ?? 0) === 1 ? "supervisor" : "supervisores"} · ${numeros.length} ${numeros.length === 1 ? "número" : "números"}`
        : "Reúne a quienes atienden las conversaciones de uno o varios números.";
    return (
        <FormularioPagina
            titulo={cola ? `Editar cola «${cola.nombre}»` : "Nueva cola"}
            descripcion={resumen}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA="/administracion/colas"
            sucio={form.formState.isDirty}
            barra={{
                textoGuardar: cola ? "Guardar cambios" : "Crear cola",
                enviando: form.formState.isSubmitting,
                ...(conflicto ? { faltan: ["la versión nueva"], motivo: "Alguien más cambió esta cola: vuelve a abrirla." } : {}),
            }}
            error={
                error ? (
                    <Aviso tono="error" titulo={conflicto ? "Alguien más cambió esta cola" : guardada ? "Se guardó la cola, pero no todo" : "No se pudo guardar"}>
                        {conflicto ? "Vuelve a abrirla desde la lista para ver el cambio." : guardada ? `${mensajeDeError(error).titulo}. Vuelve a tocar «Guardar»: sigue desde ahí.` : mensajeDeError(error).titulo}
                    </Aviso>
                ) : null
            }
        >
            <SeccionFormulario titulo="Cola">
                <Campo etiqueta="Nombre" maxLength={80} error={errores.nombre?.message} {...form.register("nombre")} />
                <CampoArea etiqueta="Descripción (opcional)" rows={2} maxLength={300} error={errores.descripcion?.message} {...form.register("descripcion")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="Personas" descripcion="El punto verde dice quién está disponible ahora.">
                <div className="grid gap-1.5">
                    <span className="text-sm font-medium">Miembros</span>
                    <SelectorPersonas
                        etiqueta="Miembros"
                        placeholder="Agregar persona…"
                        ayuda="Reciben las conversaciones nuevas de la cola y las atienden."
                        personas={agentes}
                        elegidos={v.miembros ?? []}
                        alCambiar={(ids) => form.setValue("miembros", ids, { shouldDirty: true })}
                        error={errores.miembros?.message}
                    />
                </div>
                <div className="grid gap-1.5">
                    <span className="text-sm font-medium">Supervisores</span>
                    <SelectorPersonas
                        etiqueta="Supervisores"
                        placeholder="Agregar supervisor…"
                        ayuda="Ven, asignan y pasan todo lo de esta cola, aunque no sea suyo. No hace falta que sean miembros."
                        personas={agentes}
                        elegidos={v.supervisores ?? []}
                        alCambiar={(ids) => form.setValue("supervisores", ids, { shouldDirty: true, shouldValidate: form.formState.isSubmitted })}
                        maximo={20}
                        error={errores.supervisores?.message}
                    />
                </div>
                <div className="flex items-start gap-3 rounded-lg bg-background-secondary p-3" data-fase="2">
                    <Casilla etiqueta="Asistente de IA (fase 2)" ayuda="Supervisará la cola y sugerirá a quién pasar cada conversación." checked={false} disabled onChange={() => undefined} />
                    <Bot aria-hidden className="mt-0.5 ml-auto size-4 shrink-0 text-muted-foreground" />
                </div>
            </SeccionFormulario>
            {cola ? (
                <SeccionFormulario titulo="Números de WhatsApp" descripcion="Las conversaciones nuevas de estos números llegan a esta cola. La cola de cada número se elige en su configuración.">
                    {numeros.length === 0 ? <p className="text-sm text-muted-foreground">Ningún número llega a esta cola.</p> : null}
                    <ul className="grid gap-2">
                        {numeros.map((n) => (
                            <li key={n.id} className="flex min-w-0 items-center gap-3 rounded-lg border border-border px-3 py-2.5">
                                <span aria-hidden className={cn("size-2 shrink-0 rounded-full", CLASE_PUNTO[tonoDeCanal(n)])} />
                                <span className="grid min-w-0 flex-1">
                                    <span className="truncate text-sm font-semibold">{n.nombre}</span>
                                    <span className="truncate font-mono text-xs text-muted-foreground">{formatearNumero(n.numero)}</span>
                                </span>
                                <Link href={`/administracion/numeros/${n.id}/configurar`} className="inline-flex shrink-0 items-center gap-1 text-sm text-primary underline-offset-2 hover:underline">
                                    <Settings2 aria-hidden className="size-4" />
                                    Configurar
                                </Link>
                            </li>
                        ))}
                    </ul>
                </SeccionFormulario>
            ) : null}
            <SeccionFormulario titulo="Tiempos" descripcion="Vacío: valen los del número. Los de la cola mandan sobre los del número.">
                <Campo
                    etiqueta="Tiempo de espera de respuesta (min)"
                    inputMode="numeric"
                    placeholder="Sin tiempo"
                    ayuda="Pasado este tiempo sin responder, el punto de la conversación se pone naranja."
                    error={errores.slaRespuestaMin?.message}
                    {...form.register("slaRespuestaMin")}
                />
                <div className="grid gap-4 sm:grid-cols-2">
                    <Campo
                        etiqueta="Minutos sin confirmación antes de que conteste el bot"
                        inputMode="numeric"
                        placeholder={`Del número (predeterminado: ${MINUTOS_SIN_CONFIRMACION})`}
                        ayuda="En horario, si nadie dice «La atiendo», contesta el bot."
                        error={errores.botSinConfirmacionMin?.message}
                        {...form.register("botSinConfirmacionMin")}
                    />
                    <Campo
                        etiqueta="Minutos sin agentes disponibles"
                        inputMode="numeric"
                        placeholder={`Del número (predeterminado: ${MINUTOS_SIN_DISPONIBLES})`}
                        ayuda="Si no hay nadie disponible, el saludo sale enseguida y el bot toma la conversación pasado este tiempo."
                        error={errores.botSinDisponiblesMin?.message}
                        {...form.register("botSinDisponiblesMin")}
                    />
                </div>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
