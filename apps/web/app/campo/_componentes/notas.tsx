"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { OrdenVista } from "@iris/sync";
import { Boton, CampoArea, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { useForm } from "react-hook-form";
import { formatearFecha } from "@/lib/fechas";
import { esquemaNota, type DatosNota } from "@/lib/campo/esquemas";
import { useCampo } from "./contexto";

export function Notas({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const { motor, ejecutar } = useCampo();
    const form = useForm<DatosNota>({ resolver: zodResolver(esquemaNota), defaultValues: { texto: "" } });
    const notas = [...(orden.notas ?? [])].sort((a, b) => a.ocurridoEn.localeCompare(b.ocurridoEn));
    const enviar = form.handleSubmit(async (d) => {
        if (await ejecutar(() => motor.registrar("nota.agregar", orden.id, { texto: d.texto }), "Nota guardada")) {
            form.reset();
        }
    });
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Notas</TarjetaTitulo>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {notas.length > 0 ? (
                    <ul className="grid gap-2">
                        {notas.map((n) => (
                            <li key={n.id} className="rounded-md bg-elevated p-3 text-sm">
                                <p className="whitespace-pre-wrap">{n.texto}</p>
                                <p className="mt-1 text-xs text-muted-foreground">
                                    {n.autorNombre || "Tú"} · {formatearFecha(n.ocurridoEn)}
                                </p>
                            </li>
                        ))}
                    </ul>
                ) : (
                    <p className="text-sm text-muted-foreground">Sin notas.</p>
                )}
                {editable ? (
                    <form onSubmit={enviar} className="grid gap-2" noValidate>
                        <CampoArea etiqueta="Nueva nota" rows={3} error={form.formState.errors.texto?.message} {...form.register("texto")} />
                        <Boton tamano="lg" type="submit" variante="secundario" disabled={form.formState.isSubmitting}>
                            Agregar nota
                        </Boton>
                    </form>
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}
