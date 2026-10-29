// Formularios de la orden y su checklist (como la PWA): cada formulario con sus ítems, versión y
// origen; «Agregar formulario» del catálogo del teléfono (también sin señal) y «Quitar» solo el que
// agregó el técnico sin respuestas. Tipos de ítem: sí/no, selección, texto, número, medición y foto.
import { formularioConRespuestas, valorDe, type FormularioOrden, type ItemChecklist, type OrdenVista, type PlantillaFormulario } from "@iris/sync";
import { esquemaNumeroItem, faltantesPorFormulario, ORIGENES_FORMULARIO } from "@iris/sync/campo";
import { Camera, Check, CircleAlert, CircleDashed, FilePlus2, Trash2, X } from "lucide-react-native";
import { useState } from "react";
import { StyleSheet, View } from "react-native";
import { Boton, Campo, Dialogo, Insignia, Opciones, Tarjeta, Texto } from "../../componentes";
import { useCampo } from "../../campo/contexto";
import { EstadoAdjunto, Miniatura, useTomarFoto } from "./Fotos";

const claveFaltante = (formularioId: string | undefined | null, codigo: string) => `${formularioId ?? ""}:${codigo}`;

function resumen(items: readonly ItemChecklist[]): string {
    const hechos = items.filter((i) => valorDe(i) !== null && valorDe(i) !== "").length;
    return `${hechos} de ${items.length} completos`;
}

export function Checklist({ orden, editable, agregar }: { orden: OrdenVista; editable: boolean; agregar: boolean }) {
    const { estado } = useCampo();
    const items = [...(orden.checklist ?? [])].sort((a, b) => a.posicion - b.posicion);
    const formularios = orden.formularios ?? [];
    const conFormulario = new Set(formularios.map((f) => f.id));
    const sueltos = items.filter((i) => !i.formularioId || !conFormulario.has(i.formularioId));
    const rechazoCierre = estado.rechazos.find((op) => op.ordenId === orden.id && op.tipo === "orden.cerrar");
    const faltan = new Map(faltantesPorFormulario(rechazoCierre?.error).map((f) => [claveFaltante(f.formularioId, f.codigo), f.mensaje]));
    const disponibles = estado.plantillas.filter((p) => p.activa && !formularios.some((f) => f.plantillaId === p.id));
    if (items.length === 0 && !agregar) {
        return null;
    }
    return (
        <View style={estilos.pila} accessibilityLabel="Formularios">
            {formularios.map((f) => (
                <TarjetaFormulario key={f.id} orden={orden} formulario={f} items={items.filter((i) => i.formularioId === f.id)} editable={editable} faltan={faltan} />
            ))}
            {sueltos.length > 0 ? (
                <Tarjeta titulo="Checklist" descripcion={resumen(sueltos)}>
                    <ListaItems orden={orden} items={sueltos} editable={editable} faltan={faltan} />
                </Tarjeta>
            ) : null}
            {agregar ? <AgregarFormulario orden={orden} disponibles={disponibles} /> : null}
        </View>
    );
}

function TarjetaFormulario({
    orden,
    formulario,
    items,
    editable,
    faltan,
}: {
    orden: OrdenVista;
    formulario: FormularioOrden;
    items: ItemChecklist[];
    editable: boolean;
    faltan: ReadonlyMap<string, string>;
}) {
    const { motor, ejecutar } = useCampo();
    const quitable = editable && formulario.origen === "tecnico" && !formularioConRespuestas(orden, formulario.id);
    const pendientes = items.filter((i) => faltan.has(claveFaltante(formulario.id, i.codigo))).length;
    return (
        <Tarjeta
            testID={`formulario-${formulario.codigo}`}
            titulo={formulario.nombre}
            descripcion={`Versión ${formulario.numeroVersion} · ${ORIGENES_FORMULARIO[formulario.origen]} · ${items.length > 0 ? resumen(items) : "sin ítems"}`}
            accion={
                quitable ? (
                    <Boton
                        variante="fantasma"
                        icono={Trash2}
                        alPresionar={() =>
                            void ejecutar(() => motor.registrar("orden.quitar_formulario", orden.id, { formularioId: formulario.id }), `«${formulario.nombre}» quitado`)
                        }
                    >
                        Quitar
                    </Boton>
                ) : undefined
            }
        >
            {pendientes > 0 ? <Insignia tono="error" icono={CircleAlert} texto={`Faltan ${pendientes} para cerrar`} /> : null}
            {items.length > 0 ? <ListaItems orden={orden} items={items} editable={editable} faltan={faltan} /> : null}
        </Tarjeta>
    );
}

function ListaItems({ orden, items, editable, faltan }: { orden: OrdenVista; items: readonly ItemChecklist[]; editable: boolean; faltan: ReadonlyMap<string, string> }) {
    return (
        <View style={estilos.pila}>
            {items.map((item) => (
                <View key={`${item.formularioId ?? ""}-${item.itemId}`} style={estilos.item}>
                    <ItemFila orden={orden} item={item} editable={editable} falta={faltan.get(claveFaltante(item.formularioId, item.codigo))} />
                </View>
            ))}
        </View>
    );
}

function AgregarFormulario({ orden, disponibles }: { orden: OrdenVista; disponibles: readonly PlantillaFormulario[] }) {
    const { motor, ejecutar } = useCampo();
    const [abierto, setAbierto] = useState(false);
    const agregar = async (p: PlantillaFormulario) => {
        if (await ejecutar(() => motor.registrar("orden.agregar_formulario", orden.id, { plantillaId: p.id, versionId: p.versionId }), `«${p.nombre}» agregado`)) {
            setAbierto(false);
        }
    };
    return (
        <>
            <Boton variante="secundario" icono={FilePlus2} alPresionar={() => setAbierto(true)}>
                Agregar formulario
            </Boton>
            <Dialogo abierto={abierto} alCerrar={() => setAbierto(false)} titulo="Agregar formulario" descripcion="Elige uno del catálogo. Se guarda en el teléfono y se envía al volver la señal.">
                {disponibles.length === 0 ? (
                    <Texto suave>No hay más formularios en el catálogo del teléfono.</Texto>
                ) : (
                    disponibles.map((p) => (
                        <Boton key={p.id} variante="secundario" alPresionar={() => void agregar(p)} etiqueta={p.nombre}>
                            {`${p.nombre} · v${p.numeroVersion} · ${(p.items ?? []).length} ${(p.items ?? []).length === 1 ? "ítem" : "ítems"}${p.requiereFirma ? " · pide firma" : ""}`}
                        </Boton>
                    ))
                )}
                <Boton variante="fantasma" alPresionar={() => setAbierto(false)}>
                    Volver
                </Boton>
            </Dialogo>
        </>
    );
}

function Encabezado({ item, falta }: { item: ItemChecklist; falta?: string | undefined }) {
    const v = valorDe(item);
    const marcado = v !== null && v !== "";
    return (
        <View style={estilos.encabezado}>
            <View style={estilos.flex}>
                <Texto negrita>
                    {item.etiqueta}
                    {item.obligatorio ? " (obligatorio)" : ""}
                </Texto>
            </View>
            {falta ? <Insignia tono="error" icono={CircleAlert} texto="Falta para cerrar" /> : marcado ? null : <Insignia tono="neutral" icono={CircleDashed} texto="Sin marcar" />}
        </View>
    );
}

function ItemFila({ orden, item, editable, falta }: { orden: OrdenVista; item: ItemChecklist; editable: boolean; falta?: string | undefined }) {
    const { motor, ejecutar, estado } = useCampo();
    const v = valorDe(item);
    const marcar = (valor: boolean | number | string) => ejecutar(() => motor.registrar("checklist.marcar", orden.id, { itemId: item.itemId, valor }), `«${item.etiqueta}» guardado`);
    switch (item.tipo) {
        case "si_no":
            return (
                <>
                    <Encabezado item={item} falta={falta} />
                    <Opciones
                        etiqueta={item.etiqueta}
                        deshabilitado={!editable}
                        opciones={[
                            { valor: "si", etiqueta: "Sí", icono: Check },
                            { valor: "no", etiqueta: "No", icono: X },
                        ]}
                        valor={v === true ? "si" : v === false ? "no" : undefined}
                        alCambiar={(x) => void marcar(x === "si")}
                    />
                </>
            );
        case "foto": {
            const id = typeof v === "string" ? v : null;
            return <ItemFoto orden={orden} item={item} id={id} editable={editable} falta={falta} adjunto={estado.adjuntos.find((a) => a.id === id)} />;
        }
        case "seleccion":
            return (
                <>
                    <Encabezado item={item} falta={falta} />
                    <Opciones
                        etiqueta={item.etiqueta}
                        deshabilitado={!editable}
                        opciones={(item.opciones ?? []).map((o) => ({ valor: o, etiqueta: o }))}
                        valor={typeof v === "string" ? v : undefined}
                        alCambiar={(x) => void marcar(x)}
                    />
                </>
            );
        default:
            return <ItemValor item={item} editable={editable} falta={falta} alGuardar={marcar} />;
    }
}

function ItemFoto({
    orden,
    item,
    id,
    editable,
    falta,
    adjunto,
}: {
    orden: OrdenVista;
    item: ItemChecklist;
    id: string | null;
    editable: boolean;
    falta?: string | undefined;
    adjunto: Parameters<typeof EstadoAdjunto>[0]["adjunto"];
}) {
    const { motor, ejecutar } = useCampo();
    const { tomar, procesando } = useTomarFoto();
    return (
        <>
            <Encabezado item={item} falta={falta} />
            <View style={estilos.filaFoto}>
                {id ? <Miniatura id={id} alt={item.etiqueta} /> : null}
                {id ? <EstadoAdjunto adjunto={adjunto} /> : null}
            </View>
            {editable ? (
                <Boton
                    variante="secundario"
                    icono={Camera}
                    cargando={procesando}
                    etiqueta={`${id ? "Cambiar foto" : "Tomar foto"}: ${item.etiqueta}`}
                    alPresionar={() =>
                        void (async () => {
                            const nuevo = await tomar({ ordenId: orden.id, obligatorio: item.obligatorio, item: { itemId: item.itemId, codigo: item.codigo } });
                            if (nuevo) {
                                await ejecutar(() => motor.registrar("checklist.marcar", orden.id, { itemId: item.itemId, valor: nuevo }), "Foto guardada");
                            }
                        })()
                    }
                >
                    {procesando ? "Procesando…" : id ? "Cambiar foto" : "Tomar foto"}
                </Boton>
            ) : null}
        </>
    );
}

function ItemValor({ item, editable, falta, alGuardar }: { item: ItemChecklist; editable: boolean; falta?: string | undefined; alGuardar: (v: string | number) => Promise<boolean> }) {
    const v = valorDe(item);
    const [texto, setTexto] = useState(v === null ? "" : String(v));
    const [error, setError] = useState<string | undefined>();
    const numerico = item.tipo === "numero" || item.tipo === "medicion";
    const guardar = async () => {
        if (numerico) {
            const r = esquemaNumeroItem.safeParse(texto);
            if (!r.success) {
                setError(r.error.issues[0]?.message);
                return;
            }
            setError(undefined);
            await alGuardar(r.data);
            return;
        }
        const t = texto.trim();
        if (t === "" || t.length > 2000) {
            setError(t === "" ? "Escribe el valor" : "Máximo 2000 caracteres");
            return;
        }
        setError(undefined);
        await alGuardar(t);
    };
    return (
        <>
            <Encabezado item={item} falta={falta} />
            <Campo
                etiqueta={item.etiqueta}
                valor={texto}
                alCambiar={setTexto}
                editable={editable}
                keyboardType={numerico ? "numbers-and-punctuation" : "default"}
                sufijo={item.unidad}
                error={error}
            />
            {editable ? (
                <Boton variante="secundario" etiqueta={`Guardar ${item.etiqueta}`} alPresionar={() => void guardar()}>
                    Guardar
                </Boton>
            ) : null}
        </>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    pila: { gap: 12 },
    item: { gap: 8 },
    encabezado: { flexDirection: "row", alignItems: "flex-start", gap: 8 },
    filaFoto: { flexDirection: "row", alignItems: "center", gap: 12 },
});
