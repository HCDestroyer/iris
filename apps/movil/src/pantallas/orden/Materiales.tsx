import { zodResolver } from "@hookform/resolvers/zod";
import { bodegaDeOrden, type OrdenVista } from "@iris/sync";
import { esquemaMaterial, type DatosMaterial, type EntradaMaterial } from "@iris/sync/campo";
import { Controller, useForm } from "react-hook-form";
import { Aviso, Boton, Campo, Opciones, Tarjeta, Texto } from "../../componentes";
import { useCampo } from "../../campo/contexto";

const MOTIVOS: Record<string, string> = {
    sin_conector: "Iris no está conectado a Argos: no hay existencias para elegir.",
    argos_no_disponible: "Argos no respondió al bajar las existencias. Actualiza las órdenes con señal.",
    sin_bodega: "No tienes bodega asignada en Argos.",
};

const fmt = new Intl.NumberFormat("es-GT", { maximumFractionDigits: 3 });

/** Materiales desde las existencias de la bodega de la orden (no se elige bodega). */
export function Materiales({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const { estado, motor, ejecutar, notificar } = useCampo();
    const bodega = bodegaDeOrden(estado.existencias, orden.bodegaId);
    const lineas = bodega?.lineas ?? [];
    const form = useForm<EntradaMaterial, unknown, DatosMaterial>({ resolver: zodResolver(esquemaMaterial), defaultValues: { productoId: "", cantidad: "" } });
    const descripcion = (id: number) => lineas.find((l) => l.productoId === id)?.descripcion ?? `Producto ${id}`;
    const usados = orden.materiales ?? [];

    const enviar = form.handleSubmit(async (d) => {
        const linea = lineas.find((l) => l.productoId === d.productoId);
        let excede = false;
        const ok = await ejecutar(async () => {
            const r = await motor.registrar("material.usar", orden.id, { productoId: d.productoId, cantidad: d.cantidad, ...(linea ? { descripcion: linea.descripcion } : {}) });
            excede = r.avisos.includes("material_excede_existencia");
        }, "Material registrado");
        if (ok) {
            form.reset();
            if (excede) {
                notificar({ tono: "warning", texto: "Usaste más de lo que hay en tu bodega: se registra igual y se avisa al supervisor." });
            }
        }
    });

    return (
        <Tarjeta titulo="Materiales" descripcion={`De tu bodega${bodega ? ` (${bodega.bodegaId})` : ""}`}>
            {usados.map((m) => (
                <Texto key={m.id}>
                    {m.descripcion || descripcion(m.productoId)} · {fmt.format(m.cantidad)}
                </Texto>
            ))}
            {!estado.existencias ? (
                <Texto suave>Sin existencias en el teléfono: actualiza las órdenes con señal.</Texto>
            ) : !estado.existencias.disponible ? (
                <Aviso tono="neutral" titulo="Existencias no disponibles">
                    {MOTIVOS[estado.existencias.motivo ?? ""] ?? "No se pudieron leer las existencias."}
                </Aviso>
            ) : !bodega ? (
                <Aviso tono="neutral" titulo="Sin bodega para esta orden">
                    La orden no tiene bodega con existencias en tu paquete.
                </Aviso>
            ) : editable ? (
                <>
                    <Controller
                        control={form.control}
                        name="productoId"
                        render={({ field }) => (
                            <Opciones
                                etiqueta="Material"
                                columna
                                opciones={lineas.map((l) => ({ valor: String(l.productoId), etiqueta: l.descripcion, detalle: `Quedan ${fmt.format(l.cantidad)} ${l.unidad}` }))}
                                valor={field.value}
                                alCambiar={field.onChange}
                                error={form.formState.errors.productoId?.message}
                            />
                        )}
                    />
                    <Controller
                        control={form.control}
                        name="cantidad"
                        render={({ field }) => (
                            <Campo etiqueta="Cantidad" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} keyboardType="decimal-pad" error={form.formState.errors.cantidad?.message} />
                        )}
                    />
                    <Boton variante="secundario" cargando={form.formState.isSubmitting} alPresionar={() => void enviar()}>
                        Registrar material
                    </Boton>
                </>
            ) : null}
        </Tarjeta>
    );
}
