"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    avisar,
    Aviso,
    Boton,
    Campo,
    CampoArea,
    CampoSelector,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
    useCambiosSinGuardar,
} from "@iris/ui";
import { ClipboardCheck, Radar } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { AvisoAccion, InsigniaFactibilidad, useEnvio } from "@/componentes/comercial/comun";
import { api, resolver } from "@/lib/api/cliente";
import { cuerpoFactibilidadManual, esquemaFactibilidadManual, FACTIBILIDAD_MANUAL_VACIA, MINIMO_NOTAS_VISITA, type DatosFactibilidadManual } from "@/lib/comercial/esquemas";
import { RESULTADOS_FACTIBILIDAD, textoDistancia, textoMotivo, type Cobertura, type ProspectoDetalle } from "@/lib/comercial/presentacion";
import { useLectura } from "@/lib/lectura";
import { formatearFecha } from "@/lib/fechas";
import { useSesion } from "@/lib/sesion";

type Props = { detalle: ProspectoDetalle; recargar: () => Promise<void> };

/**
 * Factibilidad: «Evaluar» (planta externa por distancia al router) y su resultado, o la visita
 * técnica (`manual`, notas obligatorias). Indeterminado explica por qué y ofrece la manual.
 */
export function TarjetaFactibilidad({ detalle, recargar }: Props) {
    const { puede } = useSesion();
    const f = detalle.factibilidad;
    const puedeEvaluar = detalle.acciones.evaluar && puede(["factibilidad.evaluar"]);
    // La visita técnica tiene su permiso (supervisor, técnico, admin; ventas no).
    const puedeManual = detalle.acciones.evaluar && puede(["factibilidad.registrar_manual"]);
    const envio = useEnvio();
    const [manual, setManual] = useState(false);

    async function evaluar() {
        const r = await envio.enviar((clave) =>
            resolver(
                api().POST("/api/v1/prospectos/{id}/factibilidad", {
                    params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                    body: { version: detalle.version },
                }),
            ),
        );
        if (r) {
            avisar({ titulo: `Factibilidad de ${detalle.numero} evaluada` });
            await recargar();
        }
    }

    return (
        <Tarjeta data-testid="factibilidad">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Factibilidad</TarjetaTitulo>
                {f ? <InsigniaFactibilidad resultado={f.resultado} /> : null}
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {!f ? (
                    <p className="text-sm text-muted-foreground">Aún no se evalúa si se puede instalar en esta dirección.</p>
                ) : (
                    <>
                        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-sm">
                            <dt className="text-muted-foreground">Cómo</dt>
                            <dd>{f.motor === "manual" ? "Visita técnica" : f.motor === "distancia" ? "Distancia al router más cercano" : f.motor}</dd>
                            {f.routerNombre ? (
                                <>
                                    <dt className="text-muted-foreground">Router sugerido</dt>
                                    <dd>{f.routerNombre}</dd>
                                </>
                            ) : null}
                            {textoDistancia(f.distanciaM) ? (
                                <>
                                    <dt className="text-muted-foreground">Distancia</dt>
                                    <dd className="tabular-nums" data-testid="distancia">
                                        {textoDistancia(f.distanciaM)}
                                    </dd>
                                </>
                            ) : null}
                            {f.viableM !== null && f.obraM !== null ? (
                                <>
                                    <dt className="text-muted-foreground">Umbrales</dt>
                                    <dd className="tabular-nums">
                                        viable hasta {textoDistancia(f.viableM)}, con obra hasta {textoDistancia(f.obraM)}
                                    </dd>
                                </>
                            ) : null}
                            {f.caja ? (
                                <>
                                    <dt className="text-muted-foreground">Caja</dt>
                                    <dd>{f.caja}</dd>
                                </>
                            ) : null}
                            {f.notas ? (
                                <>
                                    <dt className="text-muted-foreground">Notas</dt>
                                    <dd className="whitespace-pre-wrap">{f.notas}</dd>
                                </>
                            ) : null}
                            <dt className="text-muted-foreground">Evaluada</dt>
                            <dd className="tabular-nums">{formatearFecha(f.creadoEn)}</dd>
                        </dl>
                        {f.motivoAnulacion ? (
                            <Aviso tono="warning" titulo="Esta visita anula una evaluación automática «no viable»" data-testid="anula">
                                {f.motivoAnulacion}
                            </Aviso>
                        ) : null}
                        {f.resultado === "indeterminado" ? (
                            <Aviso tono="info" titulo="No se pudo determinar con los datos de la red" data-testid="motivo-indeterminado">
                                {textoMotivo(f.motivo)}{" "}
                                {puedeManual
                                    ? "Puedes registrar el resultado de una visita técnica."
                                    : "Pide a un supervisor o a un técnico que registre la visita técnica."}
                            </Aviso>
                        ) : null}
                    </>
                )}
                {puedeEvaluar || puedeManual ? (
                    <div className="flex flex-wrap gap-2">
                        {puedeEvaluar ? (
                            <Boton variante={f ? "secundario" : "primario"} tamano="sm" onClick={evaluar} disabled={envio.enviando || envio.conflicto}>
                                <Radar />
                                {envio.enviando ? "Evaluando…" : f ? "Volver a evaluar" : "Evaluar"}
                            </Boton>
                        ) : null}
                        {puedeManual ? (
                            <Boton variante={f?.resultado === "indeterminado" ? "primario" : "contorno"} tamano="sm" onClick={() => setManual(true)}>
                                <ClipboardCheck />
                                Registrar visita técnica
                            </Boton>
                        ) : null}
                    </div>
                ) : null}
                <AvisoAccion envio={envio} que="evaluar la factibilidad" recargar={recargar} />
            </TarjetaContenido>
            <DialogoProtegido open={manual} onOpenChange={setManual}>
                {manual ? (
                    <FactibilidadManual
                        detalle={detalle}
                        recargar={recargar}
                        alHecho={async () => {
                            setManual(false);
                            await recargar();
                        }}
                    />
                ) : null}
            </DialogoProtegido>
        </Tarjeta>
    );
}

function FactibilidadManual({ detalle, recargar, alHecho }: Props & { alHecho: () => Promise<void> }) {
    const envio = useEnvio();
    const f = detalle.factibilidad;
    const anula = f !== undefined && f.motor !== "manual" && f.resultado === "no_viable";
    const cobertura = useLectura<Cobertura>((signal) => resolver(api().GET("/api/v1/comercial/cobertura", { signal })));
    const routers = cobertura.estado === "listo" && cobertura.datos.disponible ? (cobertura.datos.routers ?? []) : [];
    const formulario = useForm<DatosFactibilidadManual>({
        resolver: zodResolver(esquemaFactibilidadManual(anula)),
        defaultValues: { ...FACTIBILIDAD_MANUAL_VACIA, routerId: f?.routerId ? String(f.routerId) : "" },
    });
    useCambiosSinGuardar(formulario.formState.isDirty, formulario.formState.isSubmitting);
    const { errors } = formulario.formState;
    const resultado = useWatch({ control: formulario.control, name: "resultado" });
    async function enviar(d: DatosFactibilidadManual) {
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/prospectos/{id}/factibilidad-manual", {
                        params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                        body: cuerpoFactibilidadManual(d, detalle.version),
                    }),
                ),
            { nombres: ["notas", "resultado", "caja", "routerId", "motivoAnulacion"], setError: formulario.setError },
        );
        if (r) {
            avisar({ titulo: `Factibilidad de ${detalle.numero} registrada` });
            await alHecho();
        }
    }
    return (
        <DialogoContenido titulo="Visita técnica" descripcion="El resultado de revisar la dirección en el terreno.">
            <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4" aria-label="Factibilidad manual">
                <CampoSelector etiqueta="Resultado" error={errors.resultado?.message} {...formulario.register("resultado")}>
                    {(["viable", "requiere_obra", "no_viable"] as const).map((r) => (
                        <option key={r} value={r}>
                            {RESULTADOS_FACTIBILIDAD[r].texto}
                        </option>
                    ))}
                </CampoSelector>
                {routers.length > 0 ? (
                    <CampoSelector etiqueta="Router desde donde se atiende" ayuda="Se usa para el servicio en Argos." error={errors.routerId?.message} {...formulario.register("routerId")}>
                        <option value="">Sin indicar</option>
                        {routers.map((r) => (
                            <option key={r.id} value={String(r.id)}>
                                {r.nombre}
                            </option>
                        ))}
                    </CampoSelector>
                ) : null}
                <Campo etiqueta="Caja o punto de conexión (opcional)" error={errors.caja?.message} {...formulario.register("caja")} />
                <CampoArea
                    etiqueta="Notas"
                    rows={3}
                    ayuda={`Qué se vio y con qué criterio (al menos ${MINIMO_NOTAS_VISITA} caracteres).`}
                    error={errors.notas?.message}
                    {...formulario.register("notas")}
                />
                {anula && resultado !== "no_viable" ? (
                    <>
                        <Aviso tono="warning" titulo="La evaluación automática dio «no viable»">
                            Si la visita dice otra cosa, la anula: explica por qué.
                        </Aviso>
                        <CampoArea etiqueta="Motivo de la anulación" rows={2} error={errors.motivoAnulacion?.message} {...formulario.register("motivoAnulacion")} />
                    </>
                ) : null}
                <AvisoAccion envio={envio} que="registrar la visita" recargar={recargar} />
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={envio.enviando}>
                            Volver
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={envio.enviando || envio.conflicto}>
                        {envio.enviando ? "Guardando…" : "Guardar resultado"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}
