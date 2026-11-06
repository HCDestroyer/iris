"use client";

import type { Schema } from "@iris/contratos";
import { Boton, Campo, CampoArea, CampoSelector, Casilla, Interruptor, ListaEleccion, SeccionFormulario } from "@iris/ui";
import { Bot, Plus, Sparkles, Trash2 } from "lucide-react";
import { useFieldArray, useWatch, type Control, type FieldErrors, type UseFormRegister, type UseFormSetValue } from "react-hook-form";
import { MAX_OPCIONES_MENU, textoBotParaCliente } from "@/lib/conversaciones/menu-bot";
import type { ValoresNumero } from "./configurar";

type Props = {
    control: Control<ValoresNumero>;
    register: UseFormRegister<ValoresNumero>;
    setValue: UseFormSetValue<ValoresNumero>;
    errores: FieldErrors<ValoresNumero>;
    colas: Schema<"ColaConversaciones">[];
    etiquetas: Schema<"EtiquetaConversacion">[];
    enviado: boolean;
};

/**
 * «Cuando no hay agentes» (fase 1.1, §14.6): quién atiende (el bot; el agente autónomo llega en la
 * fase 3), el saludo, un menú de hasta 9 opciones (cada una lleva a una cola con sus etiquetas) y
 * los minutos. Una cola puede cambiar los minutos en su propia pantalla.
 */
export function SeccionBot({ control, register, setValue, errores, colas, etiquetas, enviado }: Props) {
    const menu = useFieldArray({ control, name: "bot.menu" });
    const bot = useWatch({ control, name: "bot" });
    const e = errores.bot;
    const lleno = menu.fields.length >= MAX_OPCIONES_MENU;
    const vista = textoBotParaCliente(bot?.saludo ?? "", bot?.conMenu ? (bot.menu ?? []).map((o) => ({ texto: o?.texto ?? "" })) : []);
    return (
        <SeccionFormulario
            titulo="Cuando no hay agentes"
            descripcion="En horario: si nadie dice «La atiendo» a tiempo, o no hay nadie disponible, contesta el bot. La conversación sigue en la cola para que alguien la tome."
        >
            <Interruptor etiqueta="Activar el bot en horario" ayuda="Apagado: no se avisa ni contesta nadie en horario; fuera de horario sigue la respuesta de siempre." {...register("bot.activo")} />
            <ListaEleccion
                leyenda="Atiende"
                nombre="bot-atiende"
                valor={bot?.atiende ?? "bot"}
                alCambiar={(v) => setValue("bot.atiende", v === "agente_autonomo" ? "agente_autonomo" : "bot", { shouldDirty: true })}
                error={e?.atiende?.message}
                opciones={[
                    {
                        valor: "bot",
                        texto: "Bot",
                        contenido: (
                            <>
                                <Bot aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                                <span className="grid min-w-0">
                                    <span className="font-semibold">Bot</span>
                                    <span className="text-xs text-muted-foreground">Manda el saludo y el menú. Una vez por espera; nunca a quien se dio de baja.</span>
                                </span>
                            </>
                        ),
                    },
                    {
                        valor: "agente_autonomo",
                        texto: "Agente autónomo (fase 3)",
                        deshabilitada: true,
                        contenido: (
                            <>
                                <Sparkles aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                                <span className="grid min-w-0">
                                    <span className="font-semibold">Agente autónomo (fase 3)</span>
                                    <span className="text-xs text-muted-foreground">Responderá con los manuales y pasará a una persona cuando haga falta.</span>
                                </span>
                            </>
                        ),
                    },
                ]}
            />
            <CampoArea
                etiqueta="Saludo del bot"
                rows={3}
                maxLength={1024}
                ayuda={`${(bot?.saludo ?? "").length} de 1024 caracteres. Texto plano.`}
                error={e?.saludo?.message}
                {...register("bot.saludo")}
            />
            <Interruptor etiqueta="Con menú de opciones" ayuda={`Hasta ${MAX_OPCIONES_MENU}. El cliente contesta con el número y la conversación pasa a la cola de la opción.`} {...register("bot.conMenu")} />
            {bot?.conMenu ? (
                <div className="grid gap-3" data-slot="menu-bot">
                    {menu.fields.length === 0 ? <p className="text-sm text-muted-foreground">Sin opciones todavía.</p> : null}
                    <ol className="grid gap-3" aria-label="Opciones del menú">
                        {menu.fields.map((campo, i) => {
                            const elegidas = bot.menu?.[i]?.etiquetaIds ?? [];
                            const eo = e?.menu?.[i];
                            return (
                                <li key={campo.id} className="grid gap-3 rounded-lg border border-border p-3" data-opcion={i + 1}>
                                    <div className="flex items-center gap-2">
                                        <span aria-hidden className="flex size-6 shrink-0 items-center justify-center rounded-full bg-elevated text-xs font-bold">
                                            {i + 1}
                                        </span>
                                        <span className="flex-1 text-sm font-medium">Opción {i + 1}</span>
                                        <Boton variante="fantasma" tamano="icono-sm" aria-label={`Quitar la opción ${i + 1}`} onClick={() => menu.remove(i)}>
                                            <Trash2 />
                                        </Boton>
                                    </div>
                                    <div className="grid gap-3 sm:grid-cols-2">
                                        <Campo etiqueta={`Texto de la opción ${i + 1}`} maxLength={60} error={eo?.texto?.message} {...register(`bot.menu.${i}.texto`)} />
                                        <CampoSelector etiqueta={`Cola de la opción ${i + 1}`} error={eo?.colaId?.message} {...register(`bot.menu.${i}.colaId`)}>
                                            <option value="">Elegir…</option>
                                            {colas
                                                .filter((c) => c.activa)
                                                .map((c) => (
                                                    <option key={c.id} value={String(c.id)}>
                                                        {c.nombre}
                                                    </option>
                                                ))}
                                        </CampoSelector>
                                    </div>
                                    {etiquetas.length > 0 ? (
                                        <fieldset className="grid gap-1.5">
                                            <legend className="mb-1 text-xs font-medium text-muted-foreground">Etiquetas (hasta 5)</legend>
                                            <div className="flex flex-wrap gap-x-4 gap-y-1">
                                                {etiquetas.map((et) => (
                                                    <Casilla
                                                        key={et.id}
                                                        etiqueta={et.nombre}
                                                        checked={elegidas.includes(et.id)}
                                                        disabled={!elegidas.includes(et.id) && elegidas.length >= 5}
                                                        onChange={(ev) =>
                                                            setValue(`bot.menu.${i}.etiquetaIds`, ev.target.checked ? [...elegidas, et.id] : elegidas.filter((x) => x !== et.id), { shouldDirty: true })
                                                        }
                                                    />
                                                ))}
                                            </div>
                                        </fieldset>
                                    ) : null}
                                </li>
                            );
                        })}
                    </ol>
                    <div className="flex flex-wrap items-center gap-3">
                        <Boton variante="contorno" tamano="sm" onClick={() => menu.append({ texto: "", colaId: "", etiquetaIds: [] })} disabled={lleno}>
                            <Plus />
                            Agregar opción
                        </Boton>
                        <span className={lleno ? "text-sm font-medium text-warning" : "text-sm text-muted-foreground"}>
                            {menu.fields.length} de {MAX_OPCIONES_MENU} opciones
                        </span>
                    </div>
                    {e?.menu?.message || e?.menu?.root?.message ? (
                        <p role="alert" className="text-sm text-error">
                            {e.menu.message ?? e.menu.root?.message}
                        </p>
                    ) : null}
                </div>
            ) : null}
            {vista ? (
                <div className="grid gap-1.5">
                    <span className="text-xs font-medium text-muted-foreground">Así lo recibe el cliente</span>
                    <p data-slot="vista-bot" className="rounded-lg border border-dashed border-info/40 bg-background-secondary px-3 py-2 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]">
                        {vista}
                    </p>
                </div>
            ) : null}
            <div className="grid gap-4 sm:grid-cols-2">
                <Campo
                    etiqueta="Minutos sin confirmación"
                    inputMode="numeric"
                    ayuda="Con gente disponible: si nadie dice «La atiendo» en este tiempo, contesta el bot. Si la cola tiene su propio tiempo, vale el de la cola."
                    error={e?.minutosSinConfirmacion?.message}
                    {...register("bot.minutosSinConfirmacion")}
                />
                <Campo
                    etiqueta="Minutos sin agentes disponibles"
                    inputMode="numeric"
                    ayuda="Sin nadie disponible el saludo sale enseguida; pasado este tiempo, el bot toma la conversación y manda el menú."
                    error={e?.minutosSinDisponibles?.message}
                    {...register("bot.minutosSinDisponibles")}
                />
            </div>
            {enviado && e && !e.saludo && !e.menu && !e.minutosSinConfirmacion && !e.minutosSinDisponibles && !e.atiende ? (
                <p role="alert" className="text-sm text-error">
                    Revisa «Cuando no hay agentes».
                </p>
            ) : null}
        </SeccionFormulario>
    );
}
