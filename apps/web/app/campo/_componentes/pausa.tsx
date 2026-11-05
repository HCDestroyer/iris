"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_PAUSA as CODIGOS_PAUSA, segundosPausada, type OrdenVista } from "@iris/sync";
import { TEXTO_EN_PAUSA } from "@iris/sync/campo";
import { Aviso, Boton, CampoArea, CampoSelector, DialogoPie } from "@iris/ui";
import { CirclePause, CirclePlay } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { esquemaPausar, type DatosPausar } from "@/lib/campo/esquemas";
import { duracionCorta, horaCorta } from "@/lib/campo/fechas";
import { MOTIVOS_PAUSA } from "@/lib/campo/textos";
import { useCampo } from "./contexto";
import { HojaCampo } from "./hoja";
import { AVISO_TURNO_INICIADO, useUbicacion } from "./ubicacion";

/** Hora actual que se renueva cada 30 s (para el tiempo pausado). */
function useAhora(cadaMs = 30_000): number {
    const [ahora, setAhora] = useState(() => Date.now());
    useEffect(() => {
        const t = setInterval(() => setAhora(Date.now()), cadaMs);
        return () => clearInterval(t);
    }, [cadaMs]);
    return ahora;
}

/**
 * Pausa de la orden: «Pausar» (en camino o en sitio, con motivo; nota obligatoria con «Otro») o,
 * pausada, el aviso con el motivo, el tiempo pausado y «Reanudar». Todo va a la cola sin conexión.
 */
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
            {total > 0 ? (
                <p className="text-sm text-muted-foreground" data-testid="tiempo-pausado">
                    Tiempo pausado en esta visita: {duracionCorta(total)}
                </p>
            ) : null}
        </>
    );
}

/** Aviso «Orden en pausa» con el motivo, el tiempo pausado y «Reanudar». */
function OrdenPausada({ orden, segundos, puedeReanudar, enPasos }: { orden: OrdenVista; segundos: number; puedeReanudar: boolean; enPasos: boolean }) {
    const { motor, ejecutar } = useCampo();
    const ubicacion = useUbicacion();
    const pausa = (orden.pausas ?? []).find((p) => p.fin === null);
    const motivo = pausa ? MOTIVOS_PAUSA[pausa.motivo] : undefined;
    const reanudar = async () => {
        // Vuelve a estar en curso: hay trabajo real (como «En camino» / «Llegué»). Si eso enciende
        // la ubicación, se avisa en el mismo mensaje.
        const empiezaTurno = ubicacion.turnoEmpezariaSolo();
        const ok = await ejecutar(
            () => motor.registrar("orden.reanudar", orden.id, {}),
            empiezaTurno ? `Orden reanudada. ${AVISO_TURNO_INICIADO}` : "Orden reanudada",
        );
        if (ok) {
            ubicacion.iniciarTurnoSolo();
        }
    };
    return (
        <Aviso
            tono="warning"
            titulo={`Orden en pausa${motivo ? `: ${motivo}` : ""}`}
            data-testid="orden-pausada"
            accion={
                puedeReanudar ? (
                    <Boton tamano="lg" onClick={() => void reanudar()}>
                        <CirclePlay aria-hidden />
                        Reanudar
                    </Boton>
                ) : undefined
            }
        >
            <span data-testid="tiempo-pausado">
                Pausada {orden.pausadaEn ? `desde las ${horaCorta(orden.pausadaEn)} · ` : ""}
                {duracionCorta(segundos)} en total.
            </span>{" "}
            {pausa?.nota ? <span className="block whitespace-pre-wrap">{pausa.nota}</span> : null}
            <span className="block">{enPasos ? TEXTO_EN_PAUSA : "En pausa no se registra trabajo: solo puedes agregar notas. Para seguir, cerrar o salir, primero reanuda."}</span>
            {puedeReanudar ? null : <span className="block">No tienes permiso para reanudarla: pídeselo a despacho.</span>}
        </Aviso>
    );
}

function PausarOrden({ orden }: { orden: OrdenVista }) {
    const { motor, ejecutar } = useCampo();
    const [abierto, setAbierto] = useState(false);
    const form = useForm<DatosPausar>({ resolver: zodResolver(esquemaPausar), defaultValues: { motivo: undefined, nota: "" } });
    const enviar = form.handleSubmit(async (d) => {
        const ok = await ejecutar(() => motor.registrar("orden.pausar", orden.id, { motivo: d.motivo, ...(d.nota ? { nota: d.nota } : {}) }), "Orden pausada");
        if (ok) {
            setAbierto(false);
            form.reset();
        }
    });
    return (
        <>
            <Boton variante="secundario" tamano="lg" onClick={() => setAbierto(true)}>
                <CirclePause aria-hidden />
                Pausar
            </Boton>
            <HojaCampo
                abierto={abierto}
                alCambiar={(a) => {
                    setAbierto(a);
                    if (!a) {
                        form.reset();
                    }
                }}
                cambios={form.formState.isDirty}
                titulo={`Pausar ${orden.numero}`}
                descripcion="La orden queda pausada hasta que la reanudes. Despacho ve el motivo."
            >
                <form onSubmit={enviar} className="grid gap-3" noValidate>
                    <CampoSelector etiqueta="Motivo" error={form.formState.errors.motivo?.message} {...form.register("motivo")}>
                        <option value="">Elige el motivo</option>
                        {CODIGOS_PAUSA.map((m) => (
                            <option key={m} value={m}>
                                {MOTIVOS_PAUSA[m]}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoArea etiqueta="Nota" ayuda="Obligatoria con «Otro motivo»." rows={3} error={form.formState.errors.nota?.message} {...form.register("nota")} />
                    <DialogoPie>
                        <Boton tamano="lg" type="submit" disabled={form.formState.isSubmitting}>
                            Pausar
                        </Boton>
                    </DialogoPie>
                </form>
            </HojaCampo>
        </>
    );
}
