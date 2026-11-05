// Pausa de la orden (como la PWA): «Pausar» en camino o en sitio, con motivo (nota obligatoria con
// «Otro»); pausada, el motivo, el tiempo pausado y «Reanudar». Todo va a la cola sin conexión. Una
// orden pausada no cuenta como «en curso»: el GPS en segundo plano se apaga hasta reanudar.
import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_PAUSA as CODIGOS_PAUSA, segundosPausada, type OrdenVista } from "@iris/sync";
import { duracionCorta, esquemaPausar, horaCorta, MOTIVOS_PAUSA, TEXTO_EN_PAUSA, type DatosPausar } from "@iris/sync/campo";
import { CirclePause, CirclePlay } from "lucide-react-native";
import { useEffect, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { Aviso, Boton, Campo, Dialogo, Opciones, Texto } from "../../componentes";
import { useCampo } from "../../campo/contexto";

function useAhora(cadaMs = 30_000): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = setInterval(() => setAhora(Date.now()), cadaMs);
        return () => clearInterval(t);
    }, [cadaMs]);
    return ahora;
}

export function PausaOrden({
    orden,
    pausar,
    reanudar,
    puedeReanudar = true,
    enPasos = false,
}: {
    orden: OrdenVista;
    pausar: boolean;
    reanudar: boolean;
    /** Sin `ordenes.reanudar` no se ofrece «Reanudar» (lo hace despacho). */
    puedeReanudar?: boolean;
    /** Arriba de un paso de trabajo: dice que el paso es de solo lectura. */
    enPasos?: boolean;
}) {
    const ahora = useAhora();
    const total = segundosPausada(orden, ahora);
    if (reanudar) {
        return <OrdenPausada orden={orden} segundos={total} puedeReanudar={puedeReanudar} enPasos={enPasos} />;
    }
    return (
        <>
            {pausar ? <PausarOrden orden={orden} /> : null}
            {total > 0 ? <Texto suave testID="tiempo-pausado">Tiempo pausado en esta visita: {duracionCorta(total)}</Texto> : null}
        </>
    );
}

/** Aviso «Orden en pausa» con el motivo, el tiempo pausado y «Reanudar». */
function OrdenPausada({ orden, segundos, puedeReanudar, enPasos }: { orden: OrdenVista; segundos: number; puedeReanudar: boolean; enPasos: boolean }) {
    const { motor, ejecutar, rastreo } = useCampo();
    const reanudar = async () => {
        if (await ejecutar(() => motor.registrar("orden.reanudar", orden.id, {}), "Orden reanudada")) {
            // Acción del propio técnico: levanta su «Dejar de compartir ubicación».
            void rastreo?.accionPropia();
        }
    };
    const pausa = (orden.pausas ?? []).find((p) => p.fin === null);
    const motivo = pausa ? MOTIVOS_PAUSA[pausa.motivo] : undefined;
    return (
        <Aviso
            tono="warning"
            icono={CirclePause}
            titulo={`Orden en pausa${motivo ? `: ${motivo}` : ""}`}
            testID="orden-pausada"
            accion={
                puedeReanudar ? (
                    <Boton icono={CirclePlay} alPresionar={() => void reanudar()}>
                        Reanudar
                    </Boton>
                ) : undefined
            }
        >
            <Texto testID="tiempo-pausado">
                Pausada {orden.pausadaEn ? `desde las ${horaCorta(orden.pausadaEn)} · ` : ""}
                {duracionCorta(segundos)} en total.
            </Texto>
            {pausa?.nota ? <Texto>{pausa.nota}</Texto> : null}
            <Texto suave>{enPasos ? TEXTO_EN_PAUSA : "En pausa no se registra trabajo: solo puedes agregar notas. Para seguir, cerrar o salir, primero reanuda."}</Texto>
            {puedeReanudar ? null : <Texto suave>No tienes permiso para reanudarla: pídeselo a despacho.</Texto>}
        </Aviso>
    );
}

function PausarOrden({ orden }: { orden: OrdenVista }) {
    const { motor, ejecutar } = useCampo();
    const [abierto, setAbierto] = useState(false);
    const form = useForm<DatosPausar>({ resolver: zodResolver(esquemaPausar), defaultValues: { nota: "" } });
    const enviar = form.handleSubmit(async (d) => {
        if (await ejecutar(() => motor.registrar("orden.pausar", orden.id, { motivo: d.motivo, ...(d.nota ? { nota: d.nota } : {}) }), "Orden pausada")) {
            setAbierto(false);
            form.reset({ nota: "" });
        }
    });
    return (
        <>
            <Boton variante="secundario" icono={CirclePause} alPresionar={() => setAbierto(true)}>
                Pausar
            </Boton>
            <Dialogo
                abierto={abierto}
                alCerrar={() => {
                    setAbierto(false);
                    form.reset({ nota: "" });
                }}
                cambios={form.formState.isDirty}
                titulo={`Pausar ${orden.numero}`} descripcion="La orden queda pausada hasta que la reanudes. Despacho ve el motivo.">
                <Controller
                    control={form.control}
                    name="motivo"
                    render={({ field }) => (
                        <Opciones
                            etiqueta="Motivo"
                            columna
                            opciones={CODIGOS_PAUSA.map((m) => ({ valor: m, etiqueta: MOTIVOS_PAUSA[m] }))}
                            valor={field.value}
                            alCambiar={field.onChange}
                            error={form.formState.errors.motivo?.message}
                        />
                    )}
                />
                <Controller
                    control={form.control}
                    name="nota"
                    render={({ field }) => (
                        <Campo etiqueta="Nota" ayuda="Obligatoria con «Otro motivo»." valor={field.value} alCambiar={field.onChange} onBlur={field.onBlur} multiline numberOfLines={3} error={form.formState.errors.nota?.message} />
                    )}
                />
                <Boton cargando={form.formState.isSubmitting} alPresionar={() => void enviar()} etiqueta="Confirmar pausa">
                    Pausar
                </Boton>
            </Dialogo>
        </>
    );
}
