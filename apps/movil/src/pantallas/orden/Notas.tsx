import { zodResolver } from "@hookform/resolvers/zod";
import type { OrdenVista } from "@iris/sync";
import { esquemaNota, horaCorta, type DatosNota } from "@iris/sync/campo";
import { Controller, useForm } from "react-hook-form";
import { Boton, Campo, Tarjeta, Texto } from "../../componentes";
import { useCampo } from "../../campo/contexto";

/** Notas de la orden; se admiten también con la orden cancelada (evidencia). */
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
        <Tarjeta titulo="Notas">
            {notas.length > 0 ? (
                notas.map((n) => (
                    <Texto key={n.id}>
                        {n.texto}
                        {"\n"}
                        <Texto suave>
                            {n.autorNombre || "Tú"} · {horaCorta(n.ocurridoEn) ?? ""}
                        </Texto>
                    </Texto>
                ))
            ) : (
                <Texto suave>Sin notas.</Texto>
            )}
            {editable ? (
                <>
                    <Controller
                        control={form.control}
                        name="texto"
                        render={({ field }) => (
                            <Campo etiqueta="Nueva nota" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} multiline numberOfLines={3} error={form.formState.errors.texto?.message} />
                        )}
                    />
                    <Boton variante="secundario" cargando={form.formState.isSubmitting} alPresionar={() => void enviar()}>
                        Agregar nota
                    </Boton>
                </>
            ) : null}
        </Tarjeta>
    );
}
