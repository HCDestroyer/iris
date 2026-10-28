"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_FALLA as CODIGOS_FALLA, type OrdenVista } from "@iris/sync";
import { Aviso, Boton, CampoArea, CampoSelector, Dialogo, DialogoContenido, DialogoPie, GrupoSegmentado } from "@iris/ui";
import { CircleCheck, CircleX } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { esquemaFallar, type DatosFallar } from "@/lib/campo/esquemas";
import { faltantesCierre } from "@/lib/campo/reglas";
import { MOTIVOS_FALLA } from "@/lib/campo/textos";
import { useCampo } from "./contexto";
import { useUbicacion } from "./ubicacion";

/** GPS del cierre o de la falla si el permiso ya está dado (no pregunta dentro del diálogo). */
function useConPosicion() {
    const { posicionSiPermitida } = useUbicacion();
    return async () => {
        const p = await posicionSiPermitida();
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
    const subiendo = orden.subidasPendientes > 0;

    const cerrar = async () => {
        setEnviando(true);
        const ok = await ejecutar(async () => motor.registrar("orden.cerrar", orden.id, { resultado, ...(await conPosicion()) }), "Orden cerrada");
        setEnviando(false);
        if (ok) {
            setAbierto(false);
        }
    };

    return (
        <Dialogo open={abierto} onOpenChange={setAbierto}>
            <Boton tamano="lg" className="flex-1" onClick={() => setAbierto(true)}>
                <CircleCheck aria-hidden />
                Cerrar orden
            </Boton>
            <DialogoContenido titulo={`Cerrar ${orden.numero}`} descripcion="Revisa que todo esté listo antes de cerrar.">
                {faltan.length > 0 ? (
                    <Aviso tono="warning" titulo="Antes de cerrar falta:">
                        <ul className="list-disc pl-4">
                            {faltan.map((f) => (
                                <li key={f}>{f}</li>
                            ))}
                        </ul>
                    </Aviso>
                ) : (
                    <>
                        <GrupoSegmentado
                            etiqueta="Resultado"
                            className="w-full"
                            opciones={[
                                { valor: "exitosa", etiqueta: "Exitosa" },
                                { valor: "parcial", etiqueta: "Parcial" },
                            ]}
                            valor={resultado}
                            alCambiar={setResultado}
                        />
                        {subiendo ? (
                            <Aviso tono="info" titulo="Las fotos o la firma aún se están subiendo">
                                El cierre queda guardado y se envía solo cuando terminen de subir.
                            </Aviso>
                        ) : null}
                    </>
                )}
                <DialogoPie>
                    <Boton variante="fantasma" onClick={() => setAbierto(false)}>
                        Volver
                    </Boton>
                    <Boton disabled={faltan.length > 0 || enviando} onClick={() => void cerrar()}>
                        {enviando ? "Guardando…" : "Confirmar cierre"}
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}

export function FallarOrden({ orden }: { orden: OrdenVista }) {
    const { motor, ejecutar } = useCampo();
    const conPosicion = useConPosicion();
    const [abierto, setAbierto] = useState(false);
    const form = useForm<DatosFallar>({ resolver: zodResolver(esquemaFallar), defaultValues: { motivoFalla: undefined, detalle: "" } });
    const enviar = form.handleSubmit(async (d) => {
        const ok = await ejecutar(
            async () =>
                motor.registrar("orden.fallar", orden.id, { motivoFalla: d.motivoFalla, ...(d.detalle ? { detalle: d.detalle } : {}), ...(await conPosicion()) }),
            "Orden marcada como no realizada",
        );
        if (ok) {
            setAbierto(false);
        }
    });
    return (
        <Dialogo open={abierto} onOpenChange={setAbierto}>
            <Boton variante="secundario" tamano="lg" className="flex-1" onClick={() => setAbierto(true)}>
                <CircleX aria-hidden />
                No se pudo
            </Boton>
            <DialogoContenido titulo={`No se pudo hacer ${orden.numero}`} descripcion="Indica el motivo; despacho la reprogramará.">
                <form onSubmit={enviar} className="grid gap-3" noValidate>
                    <CampoSelector etiqueta="Motivo" error={form.formState.errors.motivoFalla?.message} {...form.register("motivoFalla")}>
                        <option value="">Elige el motivo</option>
                        {CODIGOS_FALLA.map((m) => (
                            <option key={m} value={m}>
                                {MOTIVOS_FALLA[m]}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoArea etiqueta="Detalle" rows={3} error={form.formState.errors.detalle?.message} {...form.register("detalle")} />
                    <DialogoPie>
                        <Boton variante="fantasma" onClick={() => setAbierto(false)}>
                            Volver
                        </Boton>
                        <Boton type="submit" disabled={form.formState.isSubmitting}>
                            Confirmar
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
