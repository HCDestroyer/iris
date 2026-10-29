// Cerrar la orden o marcarla como no realizada (con motivo). El cierre queda en la cola del
// teléfono y la API lo recibe cuando terminan de subir las fotos y la firma obligatorias.
import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_FALLA as CODIGOS_FALLA, type OrdenVista } from "@iris/sync";
import { esquemaFallar, faltantesCierre, MOTIVOS_FALLA, type DatosFallar } from "@iris/sync/campo";
import { CircleCheck, CircleX } from "lucide-react-native";
import { useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { Aviso, Boton, Campo, Dialogo, Opciones, Texto } from "../../componentes";
import { useCampo } from "../../campo/contexto";

/** GPS del cierre o de la falla si el permiso ya está dado (no pregunta). */
function useConPosicion() {
    const { servicios } = useCampo();
    return async () => {
        const p = await servicios.posicion();
        return p ? { lat: p.lat, lon: p.lon } : {};
    };
}

export function CerrarOrden({ orden }: { orden: OrdenVista }) {
    const { motor, ejecutar } = useCampo();
    const conPosicion = useConPosicion();
    const [abierto, setAbierto] = useState(false);
    const [resultado, setResultado] = useState<"exitosa" | "parcial">("exitosa");
    const [enviando, setEnviando] = useState(false);
    const faltan = faltantesCierre(orden);
    const cerrar = async () => {
        setEnviando(true);
        const ok = await ejecutar(async () => motor.registrar("orden.cerrar", orden.id, { resultado, ...(await conPosicion()) }), "Orden cerrada");
        setEnviando(false);
        if (ok) {
            setAbierto(false);
        }
    };
    return (
        <>
            <Boton icono={CircleCheck} alPresionar={() => setAbierto(true)}>
                Cerrar orden
            </Boton>
            <Dialogo abierto={abierto} alCerrar={() => setAbierto(false)} titulo={`Cerrar ${orden.numero}`} descripcion="Revisa que todo esté listo antes de cerrar.">
                {faltan.length > 0 ? (
                    <Aviso tono="warning" titulo="Antes de cerrar falta:" testID="faltan-cierre">
                        {faltan.map((f) => (
                            <Texto key={f}>• {f}</Texto>
                        ))}
                    </Aviso>
                ) : (
                    <>
                        <Opciones
                            etiqueta="Resultado"
                            opciones={[
                                { valor: "exitosa", etiqueta: "Exitosa" },
                                { valor: "parcial", etiqueta: "Parcial" },
                            ]}
                            valor={resultado}
                            alCambiar={setResultado}
                        />
                        {orden.subidasPendientes > 0 ? (
                            <Aviso tono="info" titulo="Las fotos o la firma aún se están subiendo">
                                El cierre queda guardado y se envía solo cuando terminen de subir.
                            </Aviso>
                        ) : null}
                    </>
                )}
                <Boton deshabilitado={faltan.length > 0} cargando={enviando} alPresionar={() => void cerrar()}>
                    Confirmar cierre
                </Boton>
                <Boton variante="fantasma" alPresionar={() => setAbierto(false)}>
                    Volver
                </Boton>
            </Dialogo>
        </>
    );
}

export function FallarOrden({ orden }: { orden: OrdenVista }) {
    const { motor, ejecutar } = useCampo();
    const conPosicion = useConPosicion();
    const [abierto, setAbierto] = useState(false);
    const form = useForm<DatosFallar>({ resolver: zodResolver(esquemaFallar), defaultValues: { detalle: "" } });
    const enviar = form.handleSubmit(async (d) => {
        const ok = await ejecutar(
            async () => motor.registrar("orden.fallar", orden.id, { motivoFalla: d.motivoFalla, ...(d.detalle ? { detalle: d.detalle } : {}), ...(await conPosicion()) }),
            "Orden marcada como no realizada",
        );
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
