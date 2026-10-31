"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { bodegaDeOrden, type OrdenVista } from "@iris/sync";
import { Aviso, Boton, Campo, CampoSelector, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { useForm } from "react-hook-form";
import { esquemaMaterial, type DatosMaterial, type EntradaMaterial } from "@/lib/campo/esquemas";
import { useCampo } from "./contexto";

const MOTIVOS: Record<string, string> = {
    sin_conector: "Iris no está conectado a Argos: no hay existencias para elegir.",
    argos_no_disponible: "Argos no respondió al bajar las existencias. Actualiza las órdenes con señal.",
    sin_bodega: "No tienes bodega asignada en Argos.",
};

const fmt = new Intl.NumberFormat("es-GT", { maximumFractionDigits: 3 });

/** Materiales desde las existencias de su bodega (la de la orden; no se elige bodega). */
export function Materiales({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const { estado, motor, ejecutar, notificar } = useCampo();
    const bodega = bodegaDeOrden(estado.existencias, orden.bodegaId);
    const lineas = bodega?.lineas ?? [];
    const form = useForm<EntradaMaterial, unknown, DatosMaterial>({
        resolver: zodResolver(esquemaMaterial),
        defaultValues: { productoId: "", cantidad: "" },
    });
    const descripcion = (id: number) => lineas.find((l) => l.productoId === id)?.descripcion ?? `Producto ${id}`;
    const usados = orden.materiales ?? [];

    const enviar = form.handleSubmit(async (d) => {
        const linea = lineas.find((l) => l.productoId === d.productoId);
        let excede = false;
        const ok = await ejecutar(async () => {
            const r = await motor.registrar("material.usar", orden.id, {
                productoId: d.productoId,
                cantidad: d.cantidad,
                ...(linea ? { descripcion: linea.descripcion } : {}),
            });
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
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Materiales</TarjetaTitulo>
                <TarjetaDescripcion>De tu bodega{bodega ? ` (${bodega.bodegaId})` : ""}</TarjetaDescripcion>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {usados.length > 0 ? (
                    <ul className="grid gap-1 text-sm">
                        {usados.map((m) => (
                            <li key={m.id} className="flex justify-between gap-2">
                                <span>{m.descripcion || descripcion(m.productoId)}</span>
                                <span className="font-medium tabular-nums">{fmt.format(m.cantidad)}</span>
                            </li>
                        ))}
                    </ul>
                ) : null}
                {!estado.existencias ? (
                    <p className="text-sm text-muted-foreground">Sin existencias en el teléfono: actualiza las órdenes con señal.</p>
                ) : !estado.existencias.disponible ? (
                    <Aviso tono="neutral" titulo="Existencias no disponibles">
                        {MOTIVOS[estado.existencias.motivo ?? ""] ?? "No se pudieron leer las existencias."}
                    </Aviso>
                ) : !bodega ? (
                    <Aviso tono="neutral" titulo="Sin bodega para esta orden">
                        La orden no tiene bodega con existencias en tu paquete.
                    </Aviso>
                ) : editable ? (
                    <form onSubmit={enviar} className="grid gap-3" noValidate>
                        <CampoSelector etiqueta="Material" error={form.formState.errors.productoId?.message} {...form.register("productoId")}>
                            <option value="">Elige un material</option>
                            {lineas.map((l) => (
                                <option key={l.productoId} value={l.productoId}>
                                    {l.descripcion} — quedan {fmt.format(l.cantidad)} {l.unidad}
                                </option>
                            ))}
                        </CampoSelector>
                        <Campo etiqueta="Cantidad" inputMode="decimal" autoComplete="off" error={form.formState.errors.cantidad?.message} {...form.register("cantidad")} />
                        <Boton tamano="lg" type="submit" variante="secundario" disabled={form.formState.isSubmitting}>
                            Registrar material
                        </Boton>
                    </form>
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}
