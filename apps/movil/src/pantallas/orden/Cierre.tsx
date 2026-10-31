// «Firma del cliente y cierre», el paso final y único de la orden, y «No se pudo» (con motivo).
// Solo se abre con lo obligatorio listo (si no, dice qué falta y lleva ahí); muestra al cliente el
// resumen del trabajo, el cliente firma y «Firmar y cerrar orden» encola la firma y el cierre
// juntos. La lógica está en `campo/cierre.ts`; aquí solo se dibuja.
import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_FALLA as CODIGOS_FALLA, type OrdenVista } from "@iris/sync";
import { esquemaFallar, esquemaFirma, MOTIVOS_FALLA, resumenTrabajo, type DatosFallar, type DatosFirma } from "@iris/sync/campo";
import { CircleCheck, CircleX, Lock, PenLine } from "lucide-react-native";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { StyleSheet, View } from "react-native";
import { Aviso, Boton, Campo, Dialogo, Fila, Insignia, Opciones, Pila, Tarjeta, Texto } from "../../componentes";
import { etapaCierre, useFirmaYCierre } from "../../campo/cierre";
import { useCampo } from "../../campo/contexto";
import { useTema } from "../../tema/tema";
import { EstadoAdjunto } from "./Fotos";
import { LienzoFirma } from "./LienzoFirma";

/** Lo hecho en la orden, en palabras del cliente: checklist, fotos, materiales y equipos. */
export function ResumenTrabajo({ orden }: { orden: OrdenVista }) {
    const r = resumenTrabajo(orden);
    return (
        <Tarjeta titulo="Resumen del trabajo" descripcion={r.descripcion} testID="resumen-trabajo">
            {r.checklist.length > 0 ? (
                <Pila espacio={4}>
                    <Texto suave negrita>
                        Checklist
                    </Texto>
                    {r.checklist.map((i) => (
                        <Fila key={i.clave} entre>
                            <Texto estilo={estilos.flex}>{i.etiqueta}</Texto>
                            <Texto negrita>{i.valor}</Texto>
                        </Fila>
                    ))}
                </Pila>
            ) : null}
            {r.materiales.length > 0 ? (
                <Pila espacio={4}>
                    <Texto suave negrita>
                        Materiales
                    </Texto>
                    {r.materiales.map((m) => (
                        <Fila key={m.clave} entre>
                            <Texto estilo={estilos.flex}>{m.descripcion}</Texto>
                            <Texto negrita>{m.cantidad}</Texto>
                        </Fila>
                    ))}
                </Pila>
            ) : null}
            {r.equipos.length > 0 ? (
                <Pila espacio={4}>
                    <Texto suave negrita>
                        Equipos
                    </Texto>
                    {r.equipos.map((e) => (
                        <Texto key={e.clave}>
                            {e.texto}
                            {e.serie ? <Texto mono>{` · ${e.serie}`}</Texto> : null}
                        </Texto>
                    ))}
                </Pila>
            ) : null}
        </Tarjeta>
    );
}

const NO_SE_CIERRA = { fallida: "Se marcó como no realizada.", cancelada: "Despacho la canceló.", reasignada: "Se asignó a otro técnico." } as const;

export function PasoCierre({ orden }: { orden: OrdenVista }) {
    const { navegar } = useCampo();
    const etapa = etapaCierre(orden);
    switch (etapa.tipo) {
        case "cerrada":
            return (
                <Pila espacio={14}>
                    <Aviso tono="success" icono={CircleCheck} titulo="Orden cerrada" testID="orden-cerrada">
                        {orden.firma ? `Firmó ${orden.firma.nombre}.` : "Cerrada sin firma del cliente."}
                    </Aviso>
                    <ResumenTrabajo orden={orden} />
                    <Boton variante="secundario" alPresionar={() => navegar({ tipo: "lista" })}>
                        Volver a mis órdenes
                    </Boton>
                </Pila>
            );
        case "no_se_cierra":
            return (
                <Aviso tono="neutral" titulo="Esta orden ya no se cierra">
                    {NO_SE_CIERRA[etapa.motivo]}
                </Aviso>
            );
        case "falta":
            return (
                <Aviso
                    tono="warning"
                    icono={Lock}
                    titulo="Antes de la firma falta:"
                    testID="falta-para-firmar"
                    accion={etapa.pasos.map((p) => (
                        <Boton key={p.paso} variante="secundario" alPresionar={() => navegar({ tipo: "orden", id: orden.id, paso: p.paso })}>
                            {`Ir a ${p.titulo.toLowerCase()}`}
                        </Boton>
                    ))}
                >
                    {etapa.faltan.map((f) => (
                        <Texto key={f}>• {f}</Texto>
                    ))}
                </Aviso>
            );
        case "pausada":
            return (
                <Aviso tono="warning" titulo="La orden está pausada">
                    Reanúdala desde el resumen para tomar la firma y cerrarla.
                </Aviso>
            );
        case "firmar":
            return <FirmaYCierre orden={orden} />;
    }
}

function FirmaYCierre({ orden }: { orden: OrdenVista }) {
    const { estado } = useCampo();
    const { colores } = useTema();
    const c = useFirmaYCierre(orden);
    const form = useForm<DatosFirma>({ resolver: zodResolver(esquemaFirma), defaultValues: { nombre: orden.contacto.nombre ?? "", documento: "" } });
    const enviar = () => {
        if (c.tomaFirma) {
            void form.handleSubmit((d) => c.cerrar(d))();
            return;
        }
        void c.cerrar(null);
    };

    return (
        <Pila espacio={14}>
            <ResumenTrabajo orden={orden} />
            <Tarjeta titulo="Firma del cliente" descripcion={orden.requiereFirma ? "Obligatoria para cerrar esta orden." : "Opcional."}>
                {c.firmada && orden.firma ? (
                    <View style={estilos.pila}>
                        <Insignia tono="success" icono={PenLine} texto="Firmada" />
                        <Texto>
                            {orden.firma.nombre}
                            {orden.firma.documento ? ` · ${orden.firma.documento}` : ""}
                        </Texto>
                        {orden.firma.adjuntoId ? <EstadoAdjunto adjunto={estado.adjuntos.find((a) => a.id === orden.firma?.adjuntoId)} /> : null}
                        <Boton variante="fantasma" alPresionar={c.volverAFirmar}>
                            Volver a firmar
                        </Boton>
                    </View>
                ) : (
                    <View style={estilos.pila}>
                        <Controller
                            control={form.control}
                            name="nombre"
                            render={({ field }) => <Campo etiqueta="Nombre de quien firma" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} error={form.formState.errors.nombre?.message} />}
                        />
                        <Controller
                            control={form.control}
                            name="documento"
                            render={({ field }) => (
                                <Campo etiqueta="Documento (DPI)" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} keyboardType="number-pad" error={form.formState.errors.documento?.message} />
                            )}
                        />
                        <LienzoFirma etiqueta="Recuadro para la firma del cliente" alCambiar={c.setLienzo} />
                        {c.errorTrazo ? (
                            <Texto testID="error-firma" estilo={{ color: colores.error }}>
                                {c.errorTrazo}
                            </Texto>
                        ) : null}
                    </View>
                )}
            </Tarjeta>
            <Opciones
                etiqueta="Resultado"
                opciones={[
                    { valor: "exitosa", etiqueta: "Exitosa" },
                    { valor: "parcial", etiqueta: "Parcial" },
                ]}
                valor={c.resultado}
                alCambiar={c.setResultado}
            />
            {orden.subidasPendientes > 0 ? (
                <Aviso tono="info" titulo="Las fotos o la firma aún se están subiendo">
                    El cierre queda guardado y se envía solo cuando terminen de subir.
                </Aviso>
            ) : null}
            <Boton icono={CircleCheck} cargando={c.ocupado} alPresionar={enviar} testID="firmar-y-cerrar">
                {c.ocupado ? "Guardando…" : c.tomaFirma ? "Firmar y cerrar orden" : "Cerrar orden"}
            </Boton>
        </Pila>
    );
}

export function FallarOrden({ orden }: { orden: OrdenVista }) {
    const { motor, ejecutar, servicios } = useCampo();
    const [abierto, setAbierto] = useState(false);
    const form = useForm<DatosFallar>({ resolver: zodResolver(esquemaFallar), defaultValues: { detalle: "" } });
    const enviar = form.handleSubmit(async (d) => {
        const ok = await ejecutar(async () => {
            const p = await servicios.posicion();
            return motor.registrar("orden.fallar", orden.id, { motivoFalla: d.motivoFalla, ...(d.detalle ? { detalle: d.detalle } : {}), ...(p ? { lat: p.lat, lon: p.lon } : {}) });
        }, "Orden marcada como no realizada");
        if (ok) {
            setAbierto(false);
        }
    });
    return (
        <>
            <Boton variante="secundario" icono={CircleX} alPresionar={() => setAbierto(true)}>
                No se pudo
            </Boton>
            <Dialogo abierto={abierto} alCerrar={() => setAbierto(false)} titulo={`No se pudo hacer ${orden.numero}`} descripcion="Indica el motivo; despacho la reprogramará.">
                <Controller
                    control={form.control}
                    name="motivoFalla"
                    render={({ field }) => (
                        <Opciones
                            etiqueta="Motivo"
                            columna
                            opciones={CODIGOS_FALLA.map((m) => ({ valor: m, etiqueta: MOTIVOS_FALLA[m] }))}
                            valor={field.value}
                            alCambiar={field.onChange}
                            error={form.formState.errors.motivoFalla?.message}
                        />
                    )}
                />
                <Controller
                    control={form.control}
                    name="detalle"
                    render={({ field }) => (
                        <Campo etiqueta="Detalle" valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} multiline numberOfLines={3} error={form.formState.errors.detalle?.message} />
                    )}
                />
                <Boton cargando={form.formState.isSubmitting} alPresionar={() => void enviar()}>
                    Confirmar
                </Boton>
                <Boton variante="fantasma" alPresionar={() => setAbierto(false)}>
                    Volver
                </Boton>
            </Dialogo>
        </>
    );
}

const estilos = StyleSheet.create({
    flex: { flex: 1 },
    pila: { gap: 12 },
});
