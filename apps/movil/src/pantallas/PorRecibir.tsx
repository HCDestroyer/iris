// «Por recibir» (contrato 0.5.0): traslados que bodega mandó a la bodega del técnico. Al abrir uno
// marca lo que faltó (cantidad o series) y firma; la recepción va a la cola con
// `motor.recibirTraslado` de `@iris/sync` (la lógica está en `campo/recepcion.ts`). Como la PWA.
import { zodResolver } from "@hookform/resolvers/zod";
import type { OperacionTrasladoLocal, Traslado } from "@iris/sync";
import {
    esquemaFormularioRecepcion,
    fechaHoraCorta,
    formularioRecepcionInicial,
    sePuedeRehacerRecepcion,
    SITUACIONES_TRASLADO,
    situacionLocal,
    situacionTraslado,
    textoErrorLectura,
    textoProblema,
    type FormularioRecepcion,
    type SituacionTraslado,
} from "@iris/sync/campo";
import { ArrowLeft, ChevronRight, CircleCheck, CloudUpload, Hourglass, PackageCheck, PackageOpen, RefreshCw, Send, Trash2, TriangleAlert, Undo2, WifiOff, type LucideIcon } from "lucide-react-native";
import { ActivityIndicator, StyleSheet, View } from "react-native";
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form";
import { Aviso, Boton, Campo, Fila, Insignia, Opciones, Pila, Tarjeta, Texto, Titulo, Tocable } from "../componentes";
import { useCampo } from "../campo/contexto";
import { useCargarAlAbrir, useInventario, useReleerAlEnviar } from "../campo/inventario";
import { sePuedeRecibir, situacionDe, useRecepcion } from "../campo/recepcion";
import { useTema } from "../tema/tema";
import { EstadoAdjunto } from "./orden/Fotos";
import { LienzoFirma } from "./orden/LienzoFirma";

const ICONOS_SITUACION: Record<SituacionTraslado, LucideIcon> = {
    por_recibir: PackageOpen,
    en_telefono: CloudUpload,
    rechazado_telefono: TriangleAlert,
    enviando: Send,
    recibido: CircleCheck,
    rechazado_argos: TriangleAlert,
    agotado: Hourglass,
};

function InsigniaSituacion({ situacion }: { situacion: SituacionTraslado }) {
    const s = SITUACIONES_TRASLADO[situacion];
    return <Insignia tono={s.tono} icono={ICONOS_SITUACION[situacion]} texto={s.texto} testID={`situacion-${situacion}`} />;
}

/** Error de una lectura del inventario, siempre con «Reintentar» (nunca totales inventados). */
export function ErrorLectura({ error, que, reintentar, conDatos }: { error: unknown; que: string; reintentar: () => void; conDatos: boolean }) {
    const e = textoErrorLectura(error, que);
    return (
        <Aviso
            tono={conDatos || e.sinSenal ? "warning" : "error"}
            icono={e.sinSenal ? WifiOff : TriangleAlert}
            titulo={conDatos ? `${e.texto} Se muestra lo último que se leyó.` : e.texto}
            testID="error-lectura"
            accion={
                <Boton variante="secundario" icono={RefreshCw} alPresionar={reintentar}>
                    Reintentar
                </Boton>
            }
        />
    );
}

export function PorRecibir() {
    const { estado } = useCampo();
    const { colores } = useTema();
    const { traslados, cargarTraslados } = useInventario();
    useCargarAlAbrir(cargarTraslados);
    useReleerAlEnviar(cargarTraslados);
    const lista = traslados.datos ?? [];
    const conocidos = new Set(lista.map((t) => t.id));
    // Recibidos en este teléfono que la lista no trae (sin señal o ya no figuran como enviados).
    const soloLocales = estado.recepciones.filter((op) => !conocidos.has(op.datos.trasladoId));

    return (
        <Pila espacio={14}>
            <Fila entre>
                <View style={estilos.flex}>
                    <Titulo>Por recibir</Titulo>
                    <Texto suave>Material que bodega te mandó en Argos.</Texto>
                </View>
                {traslados.estado === "cargando" ? <ActivityIndicator color={colores.primario} accessibilityLabel="Leyendo" /> : null}
            </Fila>
            {traslados.estado === "error" ? <ErrorLectura error={traslados.error} que="tus traslados" reintentar={() => void cargarTraslados()} conDatos={traslados.datos !== null} /> : null}
            {traslados.datos !== null && lista.length === 0 && soloLocales.length === 0 ? (
                <Aviso tono="success" icono={PackageCheck} titulo="No tienes nada por recibir">
                    Cuando bodega te mande material, aparecerá aquí.
                </Aviso>
            ) : null}
            {traslados.datos === null && traslados.estado !== "error" && soloLocales.length === 0 ? (
                <ActivityIndicator color={colores.primario} accessibilityLabel="Leyendo tus traslados" />
            ) : null}
            {lista.map((t) => (
                <TarjetaTraslado key={t.id} id={t.id} lineas={t.lineas?.length ?? 0} enviadoEn={t.enviadoEn} situacion={situacionTraslado(t, estado.recepciones)} />
            ))}
            {soloLocales.map((op) => (
                <TarjetaTraslado key={op.id} id={op.datos.trasladoId} lineas={null} enviadoEn={undefined} situacion={situacionLocal(op)} />
            ))}
            {traslados.leidoEn !== null ? <Texto suave>Leído de Argos a las {fechaHoraCorta(new Date(traslados.leidoEn).toISOString()) ?? ""}</Texto> : null}
        </Pila>
    );
}

function TarjetaTraslado({ id, lineas, enviadoEn, situacion }: { id: number; lineas: number | null; enviadoEn: string | null | undefined; situacion: SituacionTraslado }) {
    const { navegar } = useCampo();
    const { colores, radioTarjeta } = useTema();
    return (
        <Tocable
            etiqueta={`Traslado ${id}, ${SITUACIONES_TRASLADO[situacion].texto}`}
            alPresionar={() => navegar({ tipo: "traslado", id })}
            estilo={[estilos.tarjeta, { borderColor: colores.borde, backgroundColor: colores.tarjeta, borderRadius: radioTarjeta }]}
        >
            <PackageOpen size={24} color={colores.textoSuave} />
            <View style={estilos.cuerpo}>
                <Texto mono negrita>{`Traslado ${id}`}</Texto>
                <Texto suave>
                    {lineas !== null ? `${lineas} ${lineas === 1 ? "producto" : "productos"}` : "Sin detalle en el teléfono"}
                    {enviadoEn ? ` · enviado ${fechaHoraCorta(enviadoEn) ?? ""}` : ""}
                </Texto>
                <InsigniaSituacion situacion={situacion} />
            </View>
            <ChevronRight size={20} color={colores.textoSuave} />
        </Tocable>
    );
}

/** Descripción de un producto: de las existencias del paquete o de «Mi bodega», si ya se leyó. */
function useDescripciones(): (productoId: number) => string {
    const { estado } = useCampo();
    const { bodega } = useInventario();
    return (productoId) =>
        estado.existencias?.bodegas?.flatMap((b) => b.lineas ?? []).find((l) => l.productoId === productoId)?.descripcion ??
        bodega.datos?.existencias?.find((e) => e.productoId === productoId)?.descripcion ??
        `Producto ${productoId}`;
}

/** Un traslado: lo que llegó, la firma del técnico y «Confirmar recepción». */
export function DetalleTraslado({ id }: { id: number }) {
    const { estado, navegar } = useCampo();
    const { colores } = useTema();
    const { traslados, cargarTraslados } = useInventario();
    useCargarAlAbrir(cargarTraslados);
    useReleerAlEnviar(cargarTraslados);
    const traslado = traslados.datos?.find((t) => t.id === id) ?? null;
    const local = estado.recepciones.find((op) => op.datos.trasladoId === id) ?? null;
    const situacion = situacionDe(traslado, local, estado.recepciones);

    return (
        <Pila espacio={14}>
            <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => navegar({ tipo: "recibir" })} estilo={estilos.volver}>
                Por recibir
            </Boton>
            <Fila>
                <Titulo>{`Traslado ${id}`}</Titulo>
                {situacion ? <InsigniaSituacion situacion={situacion} /> : null}
            </Fila>
            {local ? <RecepcionLocal op={local} /> : null}
            {traslados.estado === "error" && !traslado ? <ErrorLectura error={traslados.error} que="el traslado" reintentar={() => void cargarTraslados()} conDatos={false} /> : null}
            {!traslado && !local && traslados.estado === "lista" ? (
                <Aviso tono="neutral" icono={PackageCheck} titulo="Ese traslado ya no está por recibir">
                    Puede que ya se haya recibido o que bodega lo haya cancelado.
                </Aviso>
            ) : null}
            {!traslado && !local && (traslados.estado === "cargando" || traslados.estado === "inactiva") ? (
                <ActivityIndicator color={colores.primario} accessibilityLabel="Leyendo el traslado" />
            ) : null}
            {traslado ? <LineasTraslado traslado={traslado} /> : null}
            {traslado && situacion === "rechazado_argos" ? (
                <Aviso tono="error" titulo="Argos rechazó la recepción anterior">
                    {`${textoProblema({ type: `urn:iris:problema:${traslado.recepcion?.codigo ?? ""}`, title: traslado.recepcion?.detalle ?? "", status: 422 })} Revisa lo que llegó y vuelve a confirmar.`}
                </Aviso>
            ) : null}
            {traslado && situacion === "agotado" ? (
                <Aviso tono="warning" titulo="Argos no respondió">
                    Tu recepción quedó guardada; tu supervisor la reintenta desde el panel.
                </Aviso>
            ) : null}
            {traslado && sePuedeRecibir(situacion) ? <FormularioRecibir traslado={traslado} /> : null}
        </Pila>
    );
}

function LineasTraslado({ traslado }: { traslado: Traslado }) {
    const descripcion = useDescripciones();
    return (
        <Tarjeta titulo="Lo que te mandaron" {...(traslado.enviadoEn ? { descripcion: `Enviado ${fechaHoraCorta(traslado.enviadoEn) ?? ""}` } : {})}>
            {(traslado.lineas ?? []).map((l, i) => (
                <View key={`${l.lineaId}-${i}`}>
                    <Fila entre>
                        <Texto negrita estilo={estilos.flex}>
                            {descripcion(l.productoId)}
                        </Texto>
                        <Texto negrita>{l.cantidad.toLocaleString("es-GT")}</Texto>
                    </Fila>
                    {(l.series ?? []).length > 0 ? (
                        <Texto mono suave>
                            {(l.series ?? []).join(" · ")}
                        </Texto>
                    ) : null}
                </View>
            ))}
        </Tarjeta>
    );
}

/** La recepción guardada en este teléfono: por enviar, esperando su firma o rechazada. */
function RecepcionLocal({ op }: { op: OperacionTrasladoLocal }) {
    const { estado, motor, ejecutar } = useCampo();
    const firma = estado.adjuntos.find((a) => a.id === op.datos.firmaAdjuntoId);
    if (op.estado === "rechazada") {
        return (
            <Aviso
                tono="error"
                icono={TriangleAlert}
                titulo="El servidor rechazó tu recepción"
                testID="recepcion-rechazada"
                accion={
                    <>
                        <Boton variante="secundario" icono={Trash2} alPresionar={() => void ejecutar(() => motor.descartar(op.id), "Se descartó la recepción")}>
                            Descartar
                        </Boton>
                        {sePuedeRehacerRecepcion(op) ? (
                            <Boton variante="secundario" icono={Undo2} alPresionar={() => void ejecutar(() => motor.rehacer(op.id), "Se volverá a enviar")}>
                                Rehacer
                            </Boton>
                        ) : null}
                    </>
                }
            >
                {textoProblema(op.error)}
            </Aviso>
        );
    }
    if (firma?.estado === "rechazado") {
        return (
            <Aviso
                tono="error"
                titulo="La firma no se pudo subir"
                testID="firma-traslado-rechazada"
                accion={
                    <>
                        <Boton variante="secundario" icono={Trash2} alPresionar={() => void ejecutar(() => motor.descartarAdjunto(firma.id), "Se descartó la recepción")}>
                            Descartar
                        </Boton>
                        <Boton variante="secundario" icono={Undo2} alPresionar={() => void ejecutar(() => motor.rehacerAdjunto(firma.id), "Se volverá a subir")}>
                            Volver a subir
                        </Boton>
                    </>
                }
            >
                {textoProblema(firma.problema, firma.ultimoError ?? "El servidor rechazó la firma.")}
            </Aviso>
        );
    }
    const espera = firma?.esperaHasta !== undefined || op.esperaHasta !== undefined;
    return (
        <Aviso tono="info" icono={CloudUpload} titulo="Recepción guardada en el teléfono" testID="recepcion-en-telefono">
            <Texto>
                {espera
                    ? "El traslado todavía no llega a Iris desde Argos: se reintenta solo."
                    : estado.conexion === "sin_conexion"
                      ? "Se enviará al volver la señal."
                      : "Se está enviando."}
            </Texto>
            {firma ? (
                <Fila>
                    <Texto>Firma:</Texto>
                    <EstadoAdjunto adjunto={firma} />
                </Fila>
            ) : null}
        </Aviso>
    );
}

function FormularioRecibir({ traslado }: { traslado: Traslado }) {
    const { colores } = useTema();
    const descripcion = useDescripciones();
    const r = useRecepcion(traslado);
    const form = useForm<FormularioRecepcion>({ resolver: zodResolver(esquemaFormularioRecepcion), defaultValues: formularioRecepcionInicial(traslado) });
    const { fields } = useFieldArray({ control: form.control, name: "lineas" });
    const valores = useWatch({ control: form.control, name: "lineas" });
    const confirmar = form.handleSubmit((d) => r.confirmar(d));

    return (
        <Pila espacio={14}>
            <Tarjeta titulo="¿Llegó todo?" descripcion="Marca solo lo que faltó: lo que no llegó vuelve a la bodega de origen.">
                {fields.map((campo, i) => {
                    const linea = (traslado.lineas ?? [])[i];
                    if (!linea) {
                        return null;
                    }
                    const nombre = descripcion(linea.productoId);
                    const falta = valores?.[i]?.falta ?? false;
                    const series = linea.series ?? [];
                    return (
                        <View key={campo.id} style={estilos.linea} testID={`linea-${linea.lineaId}`}>
                            <Fila entre>
                                <Texto negrita estilo={estilos.flex}>
                                    {nombre}
                                </Texto>
                                <Texto negrita>{linea.cantidad.toLocaleString("es-GT")}</Texto>
                            </Fila>
                            {linea.lineaId > 0 ? (
                                <Controller
                                    control={form.control}
                                    name={`lineas.${i}.falta`}
                                    render={({ field }) => (
                                        <Opciones
                                            etiqueta={`¿Llegó completo? ${nombre}`}
                                            opciones={[
                                                { valor: "completo", etiqueta: "Llegó completo" },
                                                { valor: "falta", etiqueta: "Faltó algo" },
                                            ]}
                                            valor={field.value ? "falta" : "completo"}
                                            alCambiar={(v) => field.onChange(v === "falta")}
                                        />
                                    )}
                                />
                            ) : (
                                <Texto suave>Argos no identificó esta línea: se recibe completa.</Texto>
                            )}
                            {falta && series.length === 0 ? (
                                <Controller
                                    control={form.control}
                                    name={`lineas.${i}.cantidad`}
                                    render={({ field }) => (
                                        <Campo
                                            etiqueta={`Cantidad que llegó de ${nombre}`}
                                            keyboardType="decimal-pad"
                                            valor={field.value}
                                            alCambiar={field.onChange}
                                            onBlur={field.onBlur}
                                            error={form.formState.errors.lineas?.[i]?.cantidad?.message}
                                        />
                                    )}
                                />
                            ) : null}
                            {falta && series.length > 0
                                ? series.map((serie, j) => (
                                      <Controller
                                          key={serie}
                                          control={form.control}
                                          name={`lineas.${i}.series.${j}`}
                                          render={({ field }) => (
                                              <Opciones
                                                  etiqueta={`Serie ${serie}`}
                                                  opciones={[
                                                      { valor: "si", etiqueta: "Llegó" },
                                                      { valor: "no", etiqueta: "No llegó" },
                                                  ]}
                                                  valor={field.value ? "si" : "no"}
                                                  alCambiar={(v) => field.onChange(v === "si")}
                                              />
                                          )}
                                      />
                                  ))
                                : null}
                        </View>
                    );
                })}
            </Tarjeta>
            <Tarjeta titulo="Tu firma" descripcion="Confirma que recibiste lo que marcaste.">
                <LienzoFirma etiqueta="Recuadro para la firma del técnico" alCambiar={r.setLienzo} />
                {r.errorFirma ? (
                    <Texto testID="error-firma" estilo={{ color: colores.error }}>
                        {r.errorFirma}
                    </Texto>
                ) : null}
            </Tarjeta>
            <Boton icono={PackageCheck} cargando={r.ocupado || form.formState.isSubmitting} alPresionar={() => void confirmar()} testID="confirmar-recepcion">
                Confirmar recepción
            </Boton>
        </Pila>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    volver: { alignSelf: "flex-start", paddingHorizontal: 0 },
    tarjeta: { minHeight: 64, flexDirection: "row", alignItems: "center", gap: 12, padding: 16, borderWidth: 1 },
    cuerpo: { flex: 1, gap: 4 },
    linea: { gap: 8, paddingVertical: 4 },
});
