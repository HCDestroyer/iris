// Pausa de la orden (como la PWA): «Pausar» en camino o en sitio, con motivo (nota obligatoria con
// «Otro»); pausada, el motivo, el tiempo pausado y «Reanudar». Todo va a la cola sin conexión. Una
// orden pausada no cuenta como «en curso»: el GPS en segundo plano se apaga hasta reanudar.
import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_PAUSA as CODIGOS_PAUSA, segundosPausada, type OrdenVista } from "@iris/sync";
import { duracionCorta, esquemaPausar, horaCorta, MOTIVOS_PAUSA, type DatosPausar } from "@iris/sync/campo";
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

export function PausaOrden({ orden, pausar, reanudar }: { orden: OrdenVista; pausar: boolean; reanudar: boolean }) {
    const ahora = useAhora();
    const total = segundosPausada(orden, ahora);
    if (reanudar) {
        return <OrdenPausada orden={orden} segundos={total} />;
    }
    return (
        <>
            {pausar ? <PausarOrden orden={orden} /> : null}
            {total > 0 ? <Texto suave testID="tiempo-pausado">Tiempo pausado en esta visita: {duracionCorta(total)}</Texto> : null}
        </>
    );
}

function OrdenPausada({ orden, segundos }: { orden: OrdenVista; segundos: number }) {
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
            titulo={`Orden pausada${motivo ? `: ${motivo}` : ""}`}
            testID="orden-pausada"
            accion={
                <Boton variante="secundario" icono={CirclePlay} alPresionar={() => void reanudar()}>
                    Reanudar
                </Boton>
            }
        >
            <Texto testID="tiempo-pausado">
                Pausada {orden.pausadaEn ? `desde las ${horaCorta(orden.pausadaEn)} · ` : ""}
                {duracionCorta(segundos)} en total.
            </Texto>
            {pausa?.nota ? <Texto>{pausa.nota}</Texto> : null}
            <Texto suave>Puedes seguir con notas, fotos, checklist, materiales y equipos. Para cerrar o salir, primero reanuda.</Texto>
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
            <Dialogo abierto={abierto} alCerrar={() => setAbierto(false)} titulo={`Pausar ${orden.numero}`} descripcion="La orden queda pausada hasta que la reanudes. Despacho ve el motivo.">
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
                <Boton variante="fantasma" alPresionar={() => setAbierto(false)}>
                    Volver
                </Boton>
            </Dialogo>
        </>
    );
}
