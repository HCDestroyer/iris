"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { bodegaDeOrden, type Existencia, type OrdenVista } from "@iris/sync";
import { buscarMateriales, detalleMaterial, existenciaDespues, nombreBodegaCampo, pasoCantidad, textoCantidad, tipoMaterial } from "@iris/sync/campo";
import { Autocompletar, Aviso, Boton, Campo, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { CircleAlert, CircleCheck, Minus, PackageCheck, Plus, ScanBarcode } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { esquemaMaterial, type DatosMaterial, type EntradaMaterial } from "@/lib/campo/esquemas";
import { useCampo } from "./contexto";

const MOTIVOS: Record<string, string> = {
    sin_conector: "Iris no está conectado a Argos: no hay existencias para elegir.",
    argos_no_disponible: "Argos no respondió al bajar las existencias. Actualiza las órdenes con señal.",
    sin_bodega: "No tienes bodega asignada en Argos.",
};

/** Atajos para cantidades grandes (metros de cable): suman a lo que ya hay. */
const ATAJOS = [5, 10] as const;

/**
 * Materiales desde las existencias de su bodega (la de la orden; no se elige bodega). Se busca por
 * nombre o código; cada material muestra lo que queda en la bodega y su modalidad (un equipo en
 * comodato se registra en «Equipos» con su serie). La cantidad va con − / + de 48 px (y atajos),
 * y dice cuánto quedaría. Todo sale del paquete del día: funciona sin señal.
 */
export function Materiales({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const { estado } = useCampo();
    const bodega = bodegaDeOrden(estado.existencias, orden.bodegaId);
    const lineas = bodega?.lineas ?? [];
    const descripcion = (id: number) => lineas.find((l) => l.productoId === id)?.descripcion ?? `Producto ${id}`;
    const usados = orden.materiales ?? [];

    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Materiales</TarjetaTitulo>
                <TarjetaDescripcion>{bodega ? `De tu bodega: ${nombreBodegaCampo(bodega)}` : "De tu bodega"}</TarjetaDescripcion>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {usados.length > 0 ? (
                    <ul className="grid gap-1 text-sm" aria-label="Materiales usados">
                        {usados.map((m) => (
                            <li key={m.id} className="flex justify-between gap-2">
                                <span>{m.descripcion || descripcion(m.productoId)}</span>
                                <span className="font-medium tabular-nums">{textoCantidad(m.cantidad, lineas.find((l) => l.productoId === m.productoId)?.unidad ?? "")}</span>
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
                    <FormularioMaterial orden={orden} lineas={lineas} />
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function FormularioMaterial({ orden, lineas }: { orden: OrdenVista; lineas: readonly Existencia[] }) {
    const { motor, ejecutar, notificar } = useCampo();
    const [texto, setTexto] = useState("");
    const form = useForm<EntradaMaterial, unknown, DatosMaterial>({
        resolver: zodResolver(esquemaMaterial),
        defaultValues: { productoId: "", cantidad: "" },
    });
    const productoId = useWatch({ control: form.control, name: "productoId" });
    const cantidad = useWatch({ control: form.control, name: "cantidad" });
    const elegida = lineas.find((l) => String(l.productoId) === productoId);
    const tipo = tipoMaterial(elegida?.modalidad);
    const despues = elegida ? existenciaDespues(elegida, cantidad) : null;
    const resultados = buscarMateriales(lineas, elegida && texto === elegida.descripcion ? "" : texto);
    const cambiar = (delta: number) => form.setValue("cantidad", pasoCantidad(form.getValues("cantidad"), delta), { shouldValidate: form.formState.isSubmitted });

    const enviar = form.handleSubmit(async (d) => {
        const linea = lineas.find((l) => l.productoId === d.productoId);
        if (linea && !tipoMaterial(linea.modalidad).registrable) return;
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
            setTexto("");
            if (excede) {
                notificar({ tono: "warning", texto: "Usaste más de lo que hay en tu bodega: se registra igual y se avisa al supervisor." });
            }
        }
    });

    return (
        <form onSubmit={enviar} className="grid gap-3" noValidate data-testid="formulario-material">
            <Autocompletar
                etiqueta="Material"
                opciones={resultados.map((l) => ({ valor: String(l.productoId), etiqueta: l.descripcion, detalle: detalleMaterial(l) }))}
                valor={productoId === "" ? null : productoId}
                texto={texto}
                alBuscar={setTexto}
                alCambiar={(v) => {
                    form.setValue("productoId", v ?? "", { shouldValidate: form.formState.isSubmitted });
                    const l = lineas.find((x) => String(x.productoId) === v);
                    if (l) setTexto(l.descripcion);
                    // Al elegir, 1 de entrada: un toque en «Registrar» basta para lo más común.
                    if (l && form.getValues("cantidad") === "") form.setValue("cantidad", "1");
                }}
                placeholder="Busca por nombre o código"
                sinResultados="Ningún material de tu bodega coincide."
                ayuda={`${lineas.length} materiales en tu bodega (del paquete del día, sin señal también).`}
                error={form.formState.errors.productoId?.message}
            />
            {elegida ? (
                <div className="grid gap-3 rounded-2xl border border-border bg-[var(--campo-panel-2)] p-3" data-testid="material-elegido">
                    <div className="flex flex-wrap items-start justify-between gap-2">
                        <span className="grid min-w-0">
                            <span className="font-semibold">{elegida.descripcion}</span>
                            {elegida.codigo ? <span className="text-xs text-muted-foreground">Código {elegida.codigo}</span> : null}
                        </span>
                        {tipo.texto ? <InsigniaEstado tono={tipo.tono} icono={tipo.registrable ? PackageCheck : ScanBarcode} texto={tipo.texto} /> : null}
                    </div>
                    <p className="text-sm">
                        En tu bodega: <span className="font-semibold tabular-nums">{textoCantidad(elegida.cantidad, elegida.unidad)}</span>
                    </p>
                    {!tipo.registrable ? (
                        <Aviso tono="info" titulo="Se registra en «Equipos»" icono={ScanBarcode}>
                            {tipo.ayuda}
                        </Aviso>
                    ) : (
                        <>
                            <div className="flex items-start gap-2">
                                <Boton
                                    type="button"
                                    variante="secundario"
                                    aria-label="Quitar uno"
                                    className="mt-[1.625rem] size-12 shrink-0 rounded-full p-0 md:size-12"
                                    onClick={() => cambiar(-1)}
                                >
                                    <Minus aria-hidden />
                                </Boton>
                                <Campo
                                    etiqueta="Cantidad"
                                    inputMode="decimal"
                                    autoComplete="off"
                                    className="flex-1"
                                    ayuda={elegida.unidad ? `En ${elegida.unidad}` : undefined}
                                    error={form.formState.errors.cantidad?.message}
                                    {...form.register("cantidad")}
                                />
                                <Boton
                                    type="button"
                                    aria-label="Agregar uno"
                                    className="mt-[1.625rem] size-12 shrink-0 rounded-full bg-[var(--campo-tinta)] p-0 text-[var(--campo-sobre-tinta)] shadow-none md:size-12"
                                    onClick={() => cambiar(1)}
                                >
                                    <Plus aria-hidden />
                                </Boton>
                            </div>
                            <div className="flex flex-wrap gap-2">
                                {ATAJOS.map((n) => (
                                    <Boton key={n} type="button" variante="secundario" aria-label={`Agregar ${n}`} className="min-h-12 rounded-full px-4" onClick={() => cambiar(n)}>
                                        +{n}
                                    </Boton>
                                ))}
                            </div>
                            {despues ? (
                                despues.excede ? (
                                    <p className="flex items-center gap-1.5 text-sm text-warning" data-testid="existencia-despues">
                                        <CircleAlert aria-hidden className="size-4 shrink-0" />
                                        Supera lo que tienes por {textoCantidad(-despues.quedan, elegida.unidad)}: se registra igual y se avisa al supervisor.
                                    </p>
                                ) : (
                                    <p className="flex items-center gap-1.5 text-sm text-muted-foreground" data-testid="existencia-despues">
                                        <CircleCheck aria-hidden className="size-4 shrink-0 text-success" />
                                        Te quedarían {textoCantidad(despues.quedan, elegida.unidad)}.
                                    </p>
                                )
                            ) : null}
                            {tipo.ayuda ? <p className="text-xs text-muted-foreground">{tipo.ayuda}</p> : null}
                            <Boton
                                tamano="lg"
                                type="submit"
                                className="min-h-12 rounded-full bg-[var(--campo-tinta)] text-[var(--campo-sobre-tinta)] shadow-none"
                                disabled={form.formState.isSubmitting}
                            >
                                Registrar material
                            </Boton>
                        </>
                    )}
                </div>
            ) : null}
        </form>
    );
}
