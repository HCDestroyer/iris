import { zodResolver } from "@hookform/resolvers/zod";
import { bodegaDeOrden, type Existencia, type OrdenVista } from "@iris/sync";
import {
    buscarMateriales,
    detalleMaterial,
    esquemaMaterial,
    existenciaDespues,
    nombreBodegaCampo,
    pasoCantidad,
    textoCantidad,
    tipoMaterial,
    type DatosMaterial,
    type EntradaMaterial,
} from "@iris/sync/campo";
import { CircleAlert, CircleCheck, Minus, PackageCheck, Plus, ScanBarcode, Search } from "lucide-react-native";
import { useState } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import { StyleSheet, Text, View } from "react-native";
import { Aviso, Boton, BotonRedondo, Campo, Insignia, Opciones, Tarjeta, Texto, Tocable } from "../../componentes";
import { useCampo } from "../../campo/contexto";
import { useTema } from "../../tema/tema";

const MOTIVOS: Record<string, string> = {
    sin_conector: "Iris no está conectado a Argos: no hay existencias para elegir.",
    argos_no_disponible: "Argos no respondió al bajar las existencias. Actualiza las órdenes con señal.",
    sin_bodega: "No tienes bodega asignada en Argos.",
};

/** Cuántos resultados se muestran a la vez (el resto, escribiendo para afinar). */
const VISIBLES = 8;
/** Atajos para cantidades grandes (metros de cable): suman a lo que ya hay. */
const ATAJOS = [5, 10] as const;

/**
 * Materiales desde las existencias de la bodega de la orden (no se elige bodega). Se busca por
 * nombre o código; cada material muestra lo que queda y su modalidad (un equipo en comodato se
 * registra en «Equipos» con su serie). La cantidad va con − / + de 48 px (y atajos) y dice cuánto
 * quedaría. Todo sale del paquete del día: funciona sin señal.
 */
export function Materiales({ orden, editable }: { orden: OrdenVista; editable: boolean }) {
    const { estado } = useCampo();
    const bodega = bodegaDeOrden(estado.existencias, orden.bodegaId);
    const lineas = bodega?.lineas ?? [];
    const descripcion = (id: number) => lineas.find((l) => l.productoId === id)?.descripcion ?? `Producto ${id}`;
    const usados = orden.materiales ?? [];

    return (
        <Tarjeta titulo="Materiales" descripcion={bodega ? `De tu bodega: ${nombreBodegaCampo(bodega)}` : "De tu bodega"}>
            {usados.map((m) => (
                <Texto key={m.id}>
                    {m.descripcion || descripcion(m.productoId)} · {textoCantidad(m.cantidad, lineas.find((l) => l.productoId === m.productoId)?.unidad ?? "")}
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
                <FormularioMaterial orden={orden} lineas={lineas} />
            ) : null}
        </Tarjeta>
    );
}

function FormularioMaterial({ orden, lineas }: { orden: OrdenVista; lineas: readonly Existencia[] }) {
    const { motor, ejecutar, notificar } = useCampo();
    const { campo, colores } = useTema();
    const [texto, setTexto] = useState("");
    const form = useForm<EntradaMaterial, unknown, DatosMaterial>({ resolver: zodResolver(esquemaMaterial), defaultValues: { productoId: "", cantidad: "" } });
    const productoId = useWatch({ control: form.control, name: "productoId" });
    const cantidad = useWatch({ control: form.control, name: "cantidad" });
    const elegida = lineas.find((l) => String(l.productoId) === productoId);
    const tipo = tipoMaterial(elegida?.modalidad);
    const despues = elegida ? existenciaDespues(elegida, cantidad) : null;
    const resultados = buscarMateriales(lineas, texto);
    // La elegida sigue a la vista aunque la búsqueda cambie.
    const visibles = [...(elegida && !resultados.slice(0, VISIBLES).includes(elegida) ? [elegida] : []), ...resultados.slice(0, VISIBLES)];
    const cambiar = (delta: number) => form.setValue("cantidad", pasoCantidad(form.getValues("cantidad"), delta), { shouldValidate: form.formState.isSubmitted });

    const enviar = form.handleSubmit(async (d) => {
        const linea = lineas.find((l) => l.productoId === d.productoId);
        if (linea && !tipoMaterial(linea.modalidad).registrable) return;
        let excede = false;
        const ok = await ejecutar(async () => {
            const r = await motor.registrar("material.usar", orden.id, { productoId: d.productoId, cantidad: d.cantidad, ...(linea ? { descripcion: linea.descripcion } : {}) });
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
        <View style={estilos.pila} testID="formulario-material">
            <Campo
                etiqueta="Buscar material"
                icono={Search}
                valor={texto}
                alCambiar={setTexto}
                placeholder="Nombre o código"
                autoCorrect={false}
                autoCapitalize="none"
                ayuda={`${lineas.length} materiales en tu bodega (del paquete del día, sin señal también).`}
            />
            <Controller
                control={form.control}
                name="productoId"
                render={({ field }) =>
                    visibles.length === 0 ? (
                        <Texto suave>Ningún material de tu bodega coincide.</Texto>
                    ) : (
                        <Opciones
                            etiqueta="Material"
                            columna
                            opciones={visibles.map((l) => ({ valor: String(l.productoId), etiqueta: l.descripcion, detalle: detalleMaterial(l) }))}
                            valor={field.value}
                            alCambiar={(v) => {
                                field.onChange(v);
                                // Al elegir, 1 de entrada: un toque en «Registrar» basta para lo más común.
                                if (form.getValues("cantidad") === "") form.setValue("cantidad", "1");
                            }}
                            error={form.formState.errors.productoId?.message}
                        />
                    )
                }
            />
            {resultados.length > VISIBLES ? <Texto suave>Y {resultados.length - VISIBLES} más: escribe para afinar.</Texto> : null}
            {elegida ? (
                <View style={[estilos.elegida, { backgroundColor: campo.panel2, borderColor: colores.borde }]} testID="material-elegido">
                    <View style={estilos.filaEntre}>
                        <View style={estilos.flex}>
                            <Texto negrita>{elegida.descripcion}</Texto>
                            {elegida.codigo ? <Texto suave>Código {elegida.codigo}</Texto> : null}
                        </View>
                        {tipo.texto ? <Insignia tono={tipo.tono} icono={tipo.registrable ? PackageCheck : ScanBarcode} texto={tipo.texto} testID="modalidad-material" /> : null}
                    </View>
                    <Texto testID="existencia-material">En tu bodega: {textoCantidad(elegida.cantidad, elegida.unidad)}</Texto>
                    {!tipo.registrable ? (
                        <Aviso tono="info" titulo="Se registra en «Equipos»" icono={ScanBarcode}>
                            {tipo.ayuda}
                        </Aviso>
                    ) : (
                        <>
                            <Controller
                                control={form.control}
                                name="cantidad"
                                render={({ field }) => (
                                    <View style={estilos.paso}>
                                        <View style={estilos.boton}>
                                            <BotonRedondo icono={Minus} etiqueta="Quitar uno" tamano={48} fondo={campo.panel} tinta={colores.texto} borde={colores.borde} alPresionar={() => cambiar(-1)} />
                                        </View>
                                        <View style={estilos.flex}>
                                            <Campo
                                                etiqueta="Cantidad"
                                                valor={field.value}
                                                alCambiar={field.onChange}
                                                onBlur={field.onBlur}
                                                keyboardType="decimal-pad"
                                                textAlign="center"
                                                {...(elegida.unidad ? { ayuda: `En ${elegida.unidad}` } : {})}
                                                error={form.formState.errors.cantidad?.message}
                                            />
                                        </View>
                                        <View style={estilos.boton}>
                                            <BotonRedondo icono={Plus} etiqueta="Agregar uno" tamano={48} fondo={campo.tinta} tinta={campo.sobreTinta} alPresionar={() => cambiar(1)} />
                                        </View>
                                    </View>
                                )}
                            />
                            <View style={estilos.atajos}>
                                {ATAJOS.map((n) => (
                                    <Boton key={n} variante="secundario" etiqueta={`Agregar ${n}`} alPresionar={() => cambiar(n)} estilo={estilos.pildora}>
                                        {`+${n}`}
                                    </Boton>
                                ))}
                            </View>
                            {despues ? (
                                <View style={estilos.fila} testID="existencia-despues">
                                    {despues.excede ? <CircleAlert size={16} color={colores.tono.warning} /> : <CircleCheck size={16} color={colores.tono.success} />}
                                    <Texto suave={!despues.excede} estilo={despues.excede ? { color: colores.tono.warning } : null}>
                                        {despues.excede
                                            ? `Supera lo que tienes por ${textoCantidad(-despues.quedan, elegida.unidad)}: se registra igual y se avisa al supervisor.`
                                            : `Te quedarían ${textoCantidad(despues.quedan, elegida.unidad)}.`}
                                    </Texto>
                                </View>
                            ) : null}
                            {tipo.ayuda ? <Texto suave>{tipo.ayuda}</Texto> : null}
                            <Tocable
                                etiqueta="Registrar material"
                                alPresionar={() => void enviar()}
                                deshabilitado={form.formState.isSubmitting}
                                estilo={[estilos.registrar, { backgroundColor: campo.tinta }]}
                            >
                                <Text style={[estilos.textoRegistrar, { color: campo.sobreTinta }]}>{form.formState.isSubmitting ? "Registrando…" : "Registrar material"}</Text>
                            </Tocable>
                        </>
                    )}
                </View>
            ) : null}
        </View>
    );
}

const estilos = StyleSheet.create({
    pila: { gap: 12 },
    flex: { flex: 1 },
    fila: { flexDirection: "row", alignItems: "center", gap: 6 },
    filaEntre: { flexDirection: "row", alignItems: "flex-start", justifyContent: "space-between", gap: 8 },
    elegida: { gap: 12, borderRadius: 20, borderWidth: 1, padding: 12 },
    paso: { flexDirection: "row", alignItems: "flex-start", gap: 8 },
    // A la altura de la entrada (bajo la etiqueta de «Cantidad»).
    boton: { paddingTop: 24 },
    atajos: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
    pildora: { borderRadius: 999, minHeight: 48 },
    registrar: { minHeight: 52, borderRadius: 999, alignItems: "center", justifyContent: "center", paddingHorizontal: 20 },
    textoRegistrar: { fontSize: 16, fontWeight: "600" },
});
