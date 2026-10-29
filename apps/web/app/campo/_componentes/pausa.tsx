"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_PAUSA as CODIGOS_PAUSA, segundosPausada, type OrdenVista } from "@iris/sync";
import { Aviso, Boton, CampoArea, CampoSelector, Dialogo, DialogoContenido, DialogoPie } from "@iris/ui";
import { CirclePause, CirclePlay } from "lucide-react";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import { esquemaPausar, type DatosPausar } from "@/lib/campo/esquemas";
import { duracionCorta, horaCorta } from "@/lib/campo/fechas";
import { MOTIVOS_PAUSA } from "@/lib/campo/textos";
import { useCampo } from "./contexto";
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
export function PausaOrden({ orden, pausar, reanudar }: { orden: OrdenVista; pausar: boolean; reanudar: boolean }) {
    const ahora = useAhora();
    const total = segundosPausada(orden, ahora);
    if (reanudar) {
        return <OrdenPausada orden={orden} segundos={total} />;
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

function OrdenPausada({ orden, segundos }: { orden: OrdenVista; segundos: number }) {
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
            titulo={`Orden pausada${motivo ? `: ${motivo}` : ""}`}
            data-testid="orden-pausada"
            accion={
                <Boton variante="secundario" onClick={() => void reanudar()}>
                    <CirclePlay aria-hidden />
                    Reanudar
                </Boton>
            }
        >
            <span data-testid="tiempo-pausado">
                Pausada {orden.pausadaEn ? `desde las ${horaCorta(orden.pausadaEn)} · ` : ""}
                {duracionCorta(segundos)} en total.
            </span>{" "}
            {pausa?.nota ? <span className="block whitespace-pre-wrap">{pausa.nota}</span> : null}
            <span className="block">Puedes seguir con notas, fotos, checklist, materiales y equipos. Para cerrar o salir, primero reanuda.</span>
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
        <Dialogo open={abierto} onOpenChange={setAbierto}>
            <Boton variante="secundario" tamano="lg" onClick={() => setAbierto(true)}>
                <CirclePause aria-hidden />
                Pausar
            </Boton>
            <DialogoContenido titulo={`Pausar ${orden.numero}`} descripcion="La orden queda pausada hasta que la reanudes. Despacho ve el motivo.">
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
                        <Boton variante="fantasma" onClick={() => setAbierto(false)}>
                            Volver
                        </Boton>
                        <Boton type="submit" disabled={form.formState.isSubmitting}>
                            Pausar
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
