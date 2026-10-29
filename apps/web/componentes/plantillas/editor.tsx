"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoArea, CampoSelector, Casilla } from "@iris/ui";
import { ArrowDown, ArrowUp, Plus, Save, Trash2 } from "lucide-react";
import type { FocusEvent, ReactNode } from "react";
import { useFieldArray, useForm, useWatch, type UseFormSetError } from "react-hook-form";
import { TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import {
    CON_UNIDAD,
    codigoDesde,
    esquemaCrearPlantilla,
    ITEM_VACIO,
    MAX_ITEMS,
    ORDEN_TIPOS_ITEM,
    TIPOS_ITEM,
    type DatosCrearPlantilla,
    type TipoOrdenPlantilla,
} from "@/lib/plantillas/esquemas";

export type PropsEditor = {
    modo: "crear" | "editar";
    valores: DatosCrearPlantilla;
    /** Puede crear comunes (empresas.todas global o sin conector). */
    puedeComun: boolean;
    /** Aviso sobre lo que hace guardar (versión nueva, borrador…). */
    aviso?: ReactNode;
    error?: ReactNode;
    alGuardar: (d: DatosCrearPlantilla, setError: UseFormSetError<DatosCrearPlantilla>) => Promise<void>;
    alCancelar: () => void;
};

/**
 * Editor de una plantilla: datos, firma y la lista de ítems (agregar, quitar, subir y bajar; el
 * orden de la lista es la posición). En edición el código no cambia (lo exige la API).
 */
export function EditorPlantilla({ modo, valores, puedeComun, aviso, error, alGuardar, alCancelar }: PropsEditor) {
    const formulario = useForm<DatosCrearPlantilla>({ resolver: zodResolver(esquemaCrearPlantilla), defaultValues: valores });
    const { fields, append, remove, move } = useFieldArray({ control: formulario.control, name: "items" });
    const items = useWatch({ control: formulario.control, name: "items" });
    const { errors, isSubmitting } = formulario.formState;

    return (
        <form noValidate onSubmit={formulario.handleSubmit((d) => alGuardar(d, formulario.setError))} className="grid gap-5" aria-label="Plantilla">
            <div className="grid gap-4 sm:grid-cols-2">
                <Campo etiqueta="Nombre" error={errors.nombre?.message} {...formulario.register("nombre")} />
                <Campo
                    etiqueta="Código"
                    readOnly={modo === "editar"}
                    ayuda={modo === "editar" ? "El código no se edita." : "Minúsculas, números y _. Único en la empresa."}
                    className="[&_input]:font-mono"
                    error={errors.codigo?.message}
                    {...formulario.register("codigo")}
                />
            </div>
            <CampoArea etiqueta="Descripción (opcional)" rows={2} error={errors.descripcion?.message} {...formulario.register("descripcion")} />
            <div className="grid gap-4 sm:grid-cols-2">
                <CampoSelector etiqueta="Tipo de orden" ayuda="Con «Cualquiera», se ofrece en todas." error={errors.tipo?.message} {...formulario.register("tipo")}>
                    <option value="">Cualquiera</option>
                    {(Object.keys(TIPOS_ORDEN) as TipoOrdenPlantilla[]).map((t) => (
                        <option key={t} value={t}>
                            {TIPOS_ORDEN[t].texto}
                        </option>
                    ))}
                </CampoSelector>
                <div className="grid content-end gap-3 pb-1">
                    <Casilla etiqueta="Pide la firma del cliente" {...formulario.register("requiereFirma")} />
                    {modo === "crear" && puedeComun ? <Casilla etiqueta="Común a todas las empresas" {...formulario.register("comun")} /> : null}
                    {modo === "crear" ? (
                        <Casilla etiqueta="Guardar como borrador" ayuda="Nace inactiva y se edita sin crear versiones hasta activarla." {...formulario.register("borrador")} />
                    ) : null}
                </div>
            </div>

            <fieldset className="grid gap-3">
                <legend className="mb-2 text-sm font-medium">Ítems</legend>
                <ol className="grid gap-3" aria-label="Ítems de la plantilla">
                    {fields.map((campo, i) => {
                        const it = items[i];
                        const e = errors.items?.[i];
                        return (
                            <li key={campo.id} className="grid gap-3 rounded-lg border p-3" data-item-editor={i + 1}>
                                <div className="flex items-center justify-between gap-2">
                                    <span className="text-sm font-medium tabular-nums">Ítem {i + 1}</span>
                                    <span className="flex items-center gap-1">
                                        <Boton type="button" variante="fantasma" tamano="icono-sm" aria-label={`Subir el ítem ${i + 1}`} title="Subir" disabled={i === 0} onClick={() => move(i, i - 1)}>
                                            <ArrowUp />
                                        </Boton>
                                        <Boton type="button" variante="fantasma" tamano="icono-sm" aria-label={`Bajar el ítem ${i + 1}`} title="Bajar" disabled={i === fields.length - 1} onClick={() => move(i, i + 1)}>
                                            <ArrowDown />
                                        </Boton>
                                        <Boton type="button" variante="fantasma" tamano="icono-sm" aria-label={`Quitar el ítem ${i + 1}`} title="Quitar" disabled={fields.length === 1} onClick={() => remove(i)}>
                                            <Trash2 />
                                        </Boton>
                                    </span>
                                </div>
                                <Campo
                                    etiqueta="Pregunta o indicación"
                                    error={e?.etiqueta?.message}
                                    {...formulario.register(`items.${i}.etiqueta`, {
                                        onBlur: (ev: FocusEvent<HTMLInputElement>) => {
                                            // Código sugerido mientras no se haya escrito uno.
                                            if (!formulario.getValues(`items.${i}.codigo`)) formulario.setValue(`items.${i}.codigo`, codigoDesde(ev.target.value));
                                        },
                                    })}
                                />
                                <div className="grid gap-3 sm:grid-cols-2">
                                    <Campo etiqueta="Código del ítem" className="[&_input]:font-mono" error={e?.codigo?.message} {...formulario.register(`items.${i}.codigo`)} />
                                    <CampoSelector etiqueta="Tipo de respuesta" error={e?.tipo?.message} {...formulario.register(`items.${i}.tipo`)}>
                                        {ORDEN_TIPOS_ITEM.map((t) => (
                                            <option key={t} value={t}>
                                                {TIPOS_ITEM[t]}
                                            </option>
                                        ))}
                                    </CampoSelector>
                                </div>
                                {it && CON_UNIDAD.includes(it.tipo) ? <Campo etiqueta="Unidad (opcional)" placeholder="dBm, Mbps…" error={e?.unidad?.message} {...formulario.register(`items.${i}.unidad`)} /> : null}
                                {it?.tipo === "seleccion" ? (
                                    <CampoArea etiqueta="Opciones" rows={3} ayuda="Una por línea, sin repetir." error={e?.opciones?.message} {...formulario.register(`items.${i}.opciones`)} />
                                ) : null}
                                <Casilla etiqueta="Obligatorio para cerrar la orden" {...formulario.register(`items.${i}.obligatorio`)} />
                            </li>
                        );
                    })}
                </ol>
                {errors.items?.message || errors.items?.root?.message ? (
                    <p className="text-sm text-error" role="alert">
                        {errors.items.message ?? errors.items.root?.message}
                    </p>
                ) : null}
                <Boton type="button" variante="contorno" tamano="sm" className="justify-self-start" disabled={fields.length >= MAX_ITEMS} onClick={() => append({ ...ITEM_VACIO })}>
                    <Plus />
                    Agregar ítem
                </Boton>
            </fieldset>

            {aviso ? <Aviso tono="info" titulo="Al guardar">{aviso}</Aviso> : null}
            {error}
            <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                <Boton type="button" variante="contorno" onClick={alCancelar} disabled={isSubmitting}>
                    Cancelar
                </Boton>
                <Boton type="submit" disabled={isSubmitting}>
                    <Save />
                    {isSubmitting ? "Guardando…" : modo === "crear" ? "Crear plantilla" : "Guardar cambios"}
                </Boton>
            </div>
        </form>
    );
}
