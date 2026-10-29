// «Por enviar»: la cola del teléfono, lo que espera archivos y los rechazos del servidor con su
// motivo y qué hacer (Descartar / Rehacer), con «Reintentar» siempre a mano. Como la PWA.
import { CODIGOS_TERMINALES, codigoProblema, faltantesDe, type AdjuntoLocal, type OperacionLocal } from "@iris/sync";
import { describirCampo, detallesProblema, horaCorta, OPERACIONES, textoProblema } from "@iris/sync/campo";
import { ArrowLeft, CloudCheck, CloudUpload, Hourglass, RefreshCw, Trash2, TriangleAlert, Undo2 } from "lucide-react-native";
import { StyleSheet, View } from "react-native";
import { Aviso, Boton, Fila, Insignia, Pila, Tarjeta, Texto, Titulo } from "../componentes";
import { useCampo } from "../campo/contexto";

function numeroOrden(ordenes: readonly { id: number; numero: string }[], id: number): string {
    return ordenes.find((o) => o.id === id)?.numero ?? `Orden ${id}`;
}

export function Pendientes() {
    const { estado, navegar, motor } = useCampo();
    const adjuntosRechazados = estado.adjuntos.filter((a) => a.estado === "rechazado");
    const subiendo = estado.adjuntos.filter((a) => a.estado === "por_crear" || a.estado === "subiendo");
    const nada = estado.cola.length + estado.rechazos.length + estado.esperandoAdjuntos.length + estado.adjuntos.filter((a) => a.estado !== "subido").length === 0;
    return (
        <Pila espacio={14}>
            <Boton variante="fantasma" icono={ArrowLeft} alPresionar={() => navegar({ tipo: "lista" })} estilo={estilos.volver}>
                Mis órdenes
            </Boton>
            <Fila entre>
                <Titulo>Por enviar</Titulo>
                {!nada ? (
                    <Boton variante="secundario" icono={RefreshCw} alPresionar={() => motor.reintentarAhora()}>
                        Reintentar
                    </Boton>
                ) : null}
            </Fila>
            {estado.ultimoError ? (
                <Aviso tono={estado.conexion === "sin_conexion" ? "warning" : "error"} titulo="No se pudo enviar" testID="error-envio">
                    {estado.conexion === "sin_conexion" ? "Sin señal: se envía solo cuando vuelva." : textoProblema(estado.ultimoError.problema, estado.ultimoError.mensaje)}
                </Aviso>
            ) : null}
            {nada ? (
                <Aviso tono="success" icono={CloudCheck} titulo="Todo enviado">
                    No queda nada por enviar desde este teléfono.
                </Aviso>
            ) : null}
            {estado.rechazos.length > 0 ? (
                <Pila>
                    <Texto negrita>Rechazados ({estado.rechazos.length})</Texto>
                    {estado.rechazos.map((op) => (
                        <Rechazo key={op.id} op={op} />
                    ))}
                </Pila>
            ) : null}
            {adjuntosRechazados.length > 0 ? (
                <Pila>
                    <Texto negrita>Archivos rechazados ({adjuntosRechazados.length})</Texto>
                    {adjuntosRechazados.map((a) => (
                        <AdjuntoRechazado key={a.id} adjunto={a} />
                    ))}
                </Pila>
            ) : null}
            {estado.esperandoAdjuntos.length > 0 ? (
                <Pila>
                    <Texto negrita>Esperando archivos</Texto>
                    {estado.esperandoAdjuntos.map((op) => (
                        <Esperando key={op.id} op={op} />
                    ))}
                </Pila>
            ) : null}
            {subiendo.length > 0 ? (
                <Tarjeta titulo={`Archivos por subir (${subiendo.length})`}>
                    {subiendo.map((a) => (
                        <Fila key={a.id} entre>
                            <Texto>
                                {a.tipo === "firma" ? "Firma" : "Foto"} · {numeroOrden(estado.ordenes, a.ordenId)}
                            </Texto>
                            <Insignia tono="warning" icono={CloudUpload} texto={a.estado === "por_crear" ? "Por subir" : `${Math.floor((a.offset / Math.max(1, a.tamano)) * 100)} %`} />
                        </Fila>
                    ))}
                    {subiendo.some((a) => a.ultimoError) ? <Texto suave>Se reintenta solo con espera.</Texto> : null}
                </Tarjeta>
            ) : null}
            {estado.cola.length > 0 ? (
                <Tarjeta titulo={`Pendientes por enviar (${estado.cola.length})`} testID="cola">
                    {estado.cola.map((op) => (
                        <Fila key={op.id} entre>
                            <Texto>
                                {OPERACIONES[op.tipo]} · {numeroOrden(estado.ordenes, op.ordenId)}
                            </Texto>
                            <Texto suave>{horaCorta(op.ocurridoEn) ?? ""}</Texto>
                        </Fila>
                    ))}
                </Tarjeta>
            ) : null}
        </Pila>
    );
}

function Rechazo({ op }: { op: OperacionLocal }) {
    const { estado, motor, ejecutar } = useCampo();
    const orden = estado.ordenes.find((o) => o.id === op.ordenId);
    const terminal = CODIGOS_TERMINALES.has(codigoProblema(op.error) ?? "");
    const detalles = detallesProblema(op.error, orden);
    return (
        <Aviso
            tono="error"
            icono={TriangleAlert}
            titulo={`${OPERACIONES[op.tipo]} · ${orden?.numero ?? `Orden ${op.ordenId}`}`}
            testID="rechazo"
            accion={
                <>
                    <Boton variante="secundario" icono={Trash2} alPresionar={() => void ejecutar(() => motor.descartar(op.id), "Se descartó")}>
                        Descartar
                    </Boton>
                    {!terminal ? (
                        <Boton variante="secundario" icono={Undo2} alPresionar={() => void ejecutar(() => motor.rehacer(op.id), "Se volverá a enviar")}>
                            Rehacer
                        </Boton>
                    ) : (
                        <Boton variante="secundario" icono={Trash2} alPresionar={() => void ejecutar(() => motor.descartarOrden(op.ordenId), "Se descartó lo pendiente de la orden")}>
                            Descartar todo de esta orden
                        </Boton>
                    )}
                </>
            }
        >
            <Texto>{textoProblema(op.error)}</Texto>
            {detalles.map((d) => (
                <Texto key={d}>• {d}</Texto>
            ))}
        </Aviso>
    );
}

function Esperando({ op }: { op: OperacionLocal }) {
    const { estado } = useCampo();
    const orden = estado.ordenes.find((o) => o.id === op.ordenId);
    const faltan = faltantesDe(op.error).map((f) => describirCampo(`adjuntos.${f}`, "", orden));
    return (
        <Aviso tono="info" icono={Hourglass} titulo={`${OPERACIONES[op.tipo]} · ${orden?.numero ?? `Orden ${op.ordenId}`}`} testID="esperando-adjuntos">
            <Texto>Se reintenta solo cuando terminen de subir:</Texto>
            {faltan.map((f) => (
                <Texto key={f}>• {f}</Texto>
            ))}
        </Aviso>
    );
}

function AdjuntoRechazado({ adjunto }: { adjunto: AdjuntoLocal }) {
    const { estado, motor, ejecutar } = useCampo();
    return (
        <Aviso
            tono="error"
            titulo={`${adjunto.tipo === "firma" ? "Firma" : "Foto"} · ${numeroOrden(estado.ordenes, adjunto.ordenId)}`}
            testID="adjunto-rechazado"
            accion={
                <View style={estilos.fila}>
                    <Boton variante="secundario" icono={Trash2} alPresionar={() => void ejecutar(() => motor.descartarAdjunto(adjunto.id), "Se descartó el archivo")}>
                        Descartar
                    </Boton>
                    <Boton variante="secundario" icono={Undo2} alPresionar={() => void ejecutar(() => motor.rehacerAdjunto(adjunto.id), "Se volverá a subir")}>
                        Volver a subir
                    </Boton>
                </View>
            }
        >
            <Texto>{textoProblema(adjunto.problema, adjunto.ultimoError ?? "El servidor rechazó el archivo.")}</Texto>
        </Aviso>
    );
}

const estilos = StyleSheet.create({
    volver: { alignSelf: "flex-start", paddingHorizontal: 0 },
    fila: { flexDirection: "row", flexWrap: "wrap", gap: 8 },
});
