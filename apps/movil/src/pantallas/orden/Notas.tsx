// Notas de la orden (como la PWA): en cualquier estado, también pausada o cancelada (evidencia).
// «Agregar» abre la hoja de la nota nueva; tocar una nota la abre con autor · hora · canal y, si es
// suya (o con `ordenes.asignar`), [Eliminar | Editar]. Editar lleva el contador /4000 y eliminar
// pregunta antes. «Atrás» de Android cierra cada hoja (es el `Modal` de `Dialogo`).
import { zodResolver } from "@hookform/resolvers/zod";
import type { NotaOrden, OrdenVista } from "@iris/sync";
import { contadorNota, esquemaNota, MAX_NOTA, metaNota, notasOrdenadas, puedeCambiarNota, type DatosNota } from "@iris/sync/campo";
import { Pencil, Plus, Trash2 } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import { StyleSheet, Text, View } from "react-native";
import { Boton, Campo, Dialogo, Tarjeta, Texto, Tocable } from "../../componentes";
import { useCampo } from "../../campo/contexto";
import { useTema } from "../../tema/tema";

function useAhora(cadaMs = 60_000): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = setInterval(() => setAhora(Date.now()), cadaMs);
        return () => clearInterval(t);
    }, [cadaMs]);
    return ahora;
}

export function Notas({ orden, editable, agregando, alAgregar }: { orden: OrdenVista; editable: boolean; agregando: boolean; alAgregar: (abierto: boolean) => void }) {
    const { sesion } = useCampo();
    const { colores } = useTema();
    const ahora = useAhora();
    const notas = notasOrdenadas(orden);
    const [abiertaId, setAbiertaId] = useState<string | null>(null);
    const abierta = notas.find((n) => n.id === abiertaId) ?? null;
    return (
        <Tarjeta
            titulo="Notas"
            testID="notas-orden"
            accion={
                editable ? (
                    <Boton variante="secundario" icono={Plus} etiqueta="Agregar nota" alPresionar={() => alAgregar(true)} estilo={estilos.agregar} testID="agregar-nota">
                        Agregar
                    </Boton>
                ) : undefined
            }
        >
            {notas.length > 0 ? (
                notas.map((n, i) => {
                    const meta = metaNota(n, sesion.usuarioId, ahora);
                    return (
                        <Tocable
                            key={n.id}
                            etiqueta={`Nota: ${n.texto}. ${meta.corta}`}
                            alPresionar={() => setAbiertaId(n.id)}
                            testID={`nota-${n.id}`}
                            estilo={[estilos.nota, i > 0 ? { borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: colores.borde } : null]}
                        >
                            <Text numberOfLines={3} style={[estilos.textoNota, { color: colores.texto }]}>
                                {n.texto}
                            </Text>
                            <Text style={[estilos.metaNota, { color: colores.textoSuave }]}>
                                {meta.corta}
                                {meta.editada ? " · editada" : ""}
                            </Text>
                        </Tocable>
                    );
                })
            ) : (
                <Texto suave>Sin notas.</Texto>
            )}
            <HojaNuevaNota orden={orden} abierto={agregando} alCambiar={alAgregar} />
            {abierta ? (
                <HojaNota
                    orden={orden}
                    nota={abierta}
                    puedeCambiar={editable && puedeCambiarNota(abierta, sesion.usuarioId, sesion.permisos)}
                    meta={metaNota(abierta, sesion.usuarioId, ahora).larga}
                    alCerrar={() => setAbiertaId(null)}
                />
            ) : null}
        </Tarjeta>
    );
}

function Contador({ texto }: { texto: string }) {
    const { colores } = useTema();
    return (
        <Text accessibilityLiveRegion="polite" testID="contador-nota" style={[estilos.contador, { color: texto.length > MAX_NOTA ? colores.tono.error : colores.textoSuave }]}>
            {contadorNota(texto)}
        </Text>
    );
}

function HojaNuevaNota({ orden, abierto, alCambiar }: { orden: OrdenVista; abierto: boolean; alCambiar: (abierto: boolean) => void }) {
    const { motor, ejecutar } = useCampo();
    const form = useForm<DatosNota>({ resolver: zodResolver(esquemaNota), defaultValues: { texto: "" } });
    const texto = useWatch({ control: form.control, name: "texto" }) ?? "";
    const enviar = form.handleSubmit(async (d) => {
        if (await ejecutar(() => motor.registrar("nota.agregar", orden.id, { texto: d.texto }), "Nota guardada")) {
            form.reset({ texto: "" });
            alCambiar(false);
        }
    });
    return (
        <Dialogo
            abierto={abierto}
            alCerrar={() => {
                alCambiar(false);
                form.reset({ texto: "" });
            }}
            cambios={form.formState.isDirty}
            titulo="Nueva nota"
            descripcion={`Para ${orden.numero}. Despacho la ve; el cliente no.`}
            testID="hoja-nueva-nota"
        >
            <Controller
                control={form.control}
                name="texto"
                render={({ field }) => (
                    <Campo etiqueta="Nueva nota" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} multiline numberOfLines={4} error={form.formState.errors.texto?.message} />
                )}
            />
            <Contador texto={texto} />
            <Boton cargando={form.formState.isSubmitting} alPresionar={() => void enviar()}>
                Agregar nota
            </Boton>
        </Dialogo>
    );
}

function HojaNota({ orden, nota, puedeCambiar, meta, alCerrar }: { orden: OrdenVista; nota: NotaOrden; puedeCambiar: boolean; meta: string; alCerrar: () => void }) {
    const { motor, ejecutar } = useCampo();
    const { colores } = useTema();
    const [modo, setModo] = useState<"ver" | "editar" | "eliminar">("ver");
    const form = useForm<DatosNota>({ resolver: zodResolver(esquemaNota), defaultValues: { texto: nota.texto } });
    const texto = useWatch({ control: form.control, name: "texto" }) ?? "";
    const sinCambios = texto.trim() === nota.texto.trim();
    const guardar = form.handleSubmit(async (d) => {
        if (await ejecutar(() => motor.registrar("nota.editar", orden.id, { notaId: nota.id, versionNota: nota.version, texto: d.texto }), "Nota editada")) {
            alCerrar();
        }
    });
    const eliminar = async () => {
        if (await ejecutar(() => motor.registrar("nota.eliminar", orden.id, { notaId: nota.id, versionNota: nota.version }), "Nota eliminada")) {
            alCerrar();
        }
    };
    if (modo === "eliminar") {
        // «Atrás» o la «X» vuelven a la nota (no la eliminan).
        return (
            <Dialogo abierto alCerrar={() => setModo("ver")} titulo="¿Eliminar la nota?" descripcion="Se quita de la orden. Queda registrada en el historial de la orden." testID="hoja-eliminar-nota">
                <View style={estilos.fila}>
                    <View style={estilos.flex}>
                        <Boton variante="secundario" alPresionar={() => setModo("ver")}>
                            Cancelar
                        </Boton>
                    </View>
                    <View style={estilos.flex}>
                        <Boton variante="peligro" alPresionar={() => void eliminar()} testID="confirmar-eliminar-nota">
                            Eliminar
                        </Boton>
                    </View>
                </View>
            </Dialogo>
        );
    }
    if (modo === "editar") {
        return (
            <Dialogo abierto alCerrar={alCerrar} cambios={form.formState.isDirty} titulo="Editar nota" testID="hoja-editar-nota">
                <Controller
                    control={form.control}
                    name="texto"
                    render={({ field }) => (
                        <Campo etiqueta="Texto de la nota" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} multiline numberOfLines={5} error={form.formState.errors.texto?.message} />
                    )}
                />
                <Contador texto={texto} />
                <View style={estilos.fila}>
                    <View style={estilos.flex}>
                        <Boton
                            variante="secundario"
                            alPresionar={() => {
                                form.reset({ texto: nota.texto });
                                setModo("ver");
                            }}
                        >
                            Cancelar
                        </Boton>
                    </View>
                    <View style={estilos.flex}>
                        <Boton cargando={form.formState.isSubmitting} deshabilitado={sinCambios} alPresionar={() => void guardar()} testID="guardar-nota">
                            Guardar
                        </Boton>
                    </View>
                </View>
            </Dialogo>
        );
    }
    return (
        <Dialogo abierto alCerrar={alCerrar} titulo="Nota" descripcion={meta} testID="hoja-nota">
            <Text style={[estilos.textoAbierta, { color: colores.texto }]} testID="texto-nota">
                {nota.texto}
            </Text>
            {puedeCambiar ? (
                <View style={estilos.fila}>
                    <View style={estilos.flex}>
                        <Boton variante="peligro" icono={Trash2} alPresionar={() => setModo("eliminar")} testID="eliminar-nota">
                            Eliminar
                        </Boton>
                    </View>
                    <View style={estilos.flex}>
                        <Boton variante="secundario" icono={Pencil} alPresionar={() => setModo("editar")} testID="editar-nota">
                            Editar
                        </Boton>
                    </View>
                </View>
            ) : (
                <Texto suave>La escribió otra persona: solo ella (o despacho) la cambia.</Texto>
            )}
        </Dialogo>
    );
}

const estilos = StyleSheet.create({
    agregar: { minHeight: 44, borderRadius: 999 },
    nota: { paddingVertical: 12, gap: 4, alignItems: "flex-start" },
    textoNota: { fontSize: 16, lineHeight: 22 },
    metaNota: { fontSize: 13 },
    textoAbierta: { fontSize: 16, lineHeight: 23 },
    contador: { alignSelf: "flex-end", fontSize: 12, fontVariant: ["tabular-nums"] },
    fila: { flexDirection: "row", gap: 8 },
    flex: { flex: 1 },
});
