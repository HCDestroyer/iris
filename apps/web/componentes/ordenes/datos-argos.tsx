"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import {
    avisar,
    Aviso,
    Boton,
    BotonEnviar,
    CampoArea,
    Casilla,
    DialogoCerrar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    InsigniaEstado,
    useCambiosSinGuardar,
} from "@iris/ui";
import { Check, RefreshCcwDot, X } from "lucide-react";
import { useRef, useState } from "react";
import { Controller, useForm } from "react-hook-form";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { formatearFecha } from "@/lib/fechas";
import {
    CAMPOS_APLICABLES,
    cambiosDeArgos,
    camposIniciales,
    cuerpoAplicar,
    cuerpoDescartar,
    errorDatosArgos,
    esquemaAplicarDatosArgos,
    esquemaDescartarDatosArgos,
    textoCamposArgos,
    type DatosAplicarArgos,
    type DatosArgosCambiados,
    type DatosDescartarArgos,
} from "@/lib/ordenes/datos-argos";
import { useSesion } from "@/lib/sesion";

type OrdenDetalle = Schema<"OrdenDetalle">;

/** Marca «Dato de Argos cambió» (cabecera de la orden y tablero). */
export function MarcaDatosArgos({ cambios }: { cambios: DatosArgosCambiados }) {
    const que = textoCamposArgos(cambios.campos);
    return (
        <InsigniaEstado
            tono="warning"
            icono={RefreshCcwDot}
            texto="Dato de Argos cambió"
            title={que ? `Cambió en Argos: ${que}` : "Cambió un dato en Argos"}
            data-testid="datos-argos-cambiados"
        />
    );
}

/**
 * Aviso en la ficha: qué cambió en Argos y cuándo, con «Aplicar» (elige qué datos copiar de Argos)
 * y «Descartar» (la orden se queda con lo suyo), cada uno con su permiso. Ambos mandan la versión y
 * el aviso leídos: si Argos avisó algo después, se recarga y se muestra el aviso nuevo.
 */
export function AvisoDatosArgos({ orden, recargar }: { orden: OrdenDetalle; recargar: () => Promise<void> }) {
    const { puede } = useSesion();
    const [abierto, setAbierto] = useState<null | "aplicar" | "descartar">(null);
    const [mensaje, setMensaje] = useState<{ titulo: string; texto: string } | null>(null);
    const cambios = cambiosDeArgos(orden);
    if (!cambios) {
        return mensaje ? <MensajeDatosArgos mensaje={mensaje} cerrar={() => setMensaje(null)} /> : null;
    }
    const que = textoCamposArgos(cambios.campos);
    const aplicar = puede(["ordenes.aplicar_datos_argos"]);
    const descartar = puede(["ordenes.descartar_datos_argos"]);
    const alTerminar = async (resultado: Resultado) => {
        setAbierto(null);
        if (resultado.tipo === "mensaje") {
            setMensaje(resultado);
        } else {
            setMensaje(null);
        }
        await recargar();
        if (resultado.tipo === "aviso_nuevo") {
            setMensaje({ titulo: "Argos avisó otro cambio", texto: "Mientras lo revisabas llegó un aviso nuevo de Argos: revisa lo que cambió y vuelve a decidir." });
        }
    };

    return (
        <>
            {mensaje ? <MensajeDatosArgos mensaje={mensaje} cerrar={() => setMensaje(null)} /> : null}
            <Aviso
                tono="warning"
                titulo="Dato de Argos cambió"
                icono={RefreshCcwDot}
                data-testid="aviso-datos-argos"
                accion={
                    aplicar || descartar ? (
                        <div className="flex flex-wrap gap-2">
                            {aplicar ? (
                                <Boton variante="secundario" tamano="sm" onClick={() => setAbierto("aplicar")}>
                                    <Check />
                                    Aplicar
                                </Boton>
                            ) : null}
                            {descartar ? (
                                <Boton variante="fantasma" tamano="sm" onClick={() => setAbierto("descartar")}>
                                    <X />
                                    Descartar
                                </Boton>
                            ) : null}
                        </div>
                    ) : undefined
                }
            >
                {que ? `Cambió en Argos: ${que}` : "Cambió un dato del cliente o del servicio en Argos"} ({formatearFecha(cambios.en)}). La orden conserva lo que copió al
                crearse: aplícalo o descártalo después de revisarlo.
            </Aviso>
            <DialogoProtegido open={abierto === "aplicar"} onOpenChange={(v) => setAbierto(v ? "aplicar" : null)}>
                {abierto === "aplicar" ? <AplicarDatos orden={orden} cambios={cambios} alTerminar={alTerminar} /> : null}
            </DialogoProtegido>
            <DialogoProtegido open={abierto === "descartar"} onOpenChange={(v) => setAbierto(v ? "descartar" : null)}>
                {abierto === "descartar" ? <DescartarDatos orden={orden} cambios={cambios} alTerminar={alTerminar} /> : null}
            </DialogoProtegido>
        </>
    );
}

type Resultado = { tipo: "hecho" | "aviso_nuevo" | "version" } | { tipo: "mensaje"; titulo: string; texto: string };

function MensajeDatosArgos({ mensaje, cerrar }: { mensaje: { titulo: string; texto: string }; cerrar: () => void }) {
    return (
        <Aviso
            tono="error"
            titulo={mensaje.titulo}
            data-testid="mensaje-datos-argos"
            accion={
                <Boton variante="fantasma" tamano="sm" onClick={cerrar}>
                    Entendido
                </Boton>
            }
        >
            {mensaje.texto}
        </Aviso>
    );
}

/** Envía la acción; los 409 y los 422 conocidos cierran el diálogo y los atiende el aviso. */
function useEnviarDatosArgos(alTerminar: (r: Resultado) => Promise<void>) {
    const clave = useRef(nuevaClaveIdempotencia());
    const [error, setError] = useState<unknown>(null);
    const enviar = async (llamada: (clave: string) => Promise<unknown>, hecho: string) => {
        setError(null);
        try {
            await llamada(clave.current);
            clave.current = nuevaClaveIdempotencia();
            avisar({ titulo: hecho });
            await alTerminar({ tipo: "hecho" });
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            const r = errorDatosArgos(e);
            if (r.tipo === "otro") {
                setError(e);
                return;
            }
            await alTerminar(r);
        }
    };
    return { enviar, error };
}

function AplicarDatos({ orden, cambios, alTerminar }: { orden: OrdenDetalle; cambios: DatosArgosCambiados; alTerminar: (r: Resultado) => Promise<void> }) {
    const form = useForm<DatosAplicarArgos>({ resolver: zodResolver(esquemaAplicarDatosArgos), defaultValues: { campos: camposIniciales(cambios) } });
    const { errors, isSubmitting, isDirty } = form.formState;
    useCambiosSinGuardar(isDirty, isSubmitting);
    const { enviar, error } = useEnviarDatosArgos(alTerminar);
    const guardar = (d: DatosAplicarArgos) =>
        enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/ordenes/{id}/aplicar-datos-argos", {
                        params: { path: { id: orden.id }, header: { "Idempotency-Key": clave } },
                        body: cuerpoAplicar(d, orden.version, cambios),
                    }),
                ),
            "Datos de Argos aplicados a la orden",
        );
    return (
        <DialogoContenido titulo="Aplicar datos de Argos" descripcion="Se copian de Argos a la orden; lo que Argos no tenga se queda como está.">
            <form noValidate className="grid gap-4" onSubmit={(e) => void form.handleSubmit(guardar)(e)}>
                <Controller
                    control={form.control}
                    name="campos"
                    render={({ field }) => (
                        <fieldset className="grid gap-2">
                            <legend className="mb-1 text-sm font-medium">Qué aplicar</legend>
                            {CAMPOS_APLICABLES.map((c) => (
                                <Casilla
                                    key={c.campo}
                                    etiqueta={c.texto}
                                    checked={field.value.includes(c.campo)}
                                    onChange={(e) => field.onChange(e.target.checked ? [...field.value, c.campo] : field.value.filter((x) => x !== c.campo))}
                                />
                            ))}
                            {errors.campos?.message ? <p className="text-sm text-error">{errors.campos.message}</p> : null}
                        </fieldset>
                    )}
                />
                {error ? (
                    <Aviso tono="error" titulo="No se pudo aplicar">
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <BotonEnviar enviando={isSubmitting} textoEnviando="Aplicando…">
                        Aplicar
                    </BotonEnviar>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}

function DescartarDatos({ orden, cambios, alTerminar }: { orden: OrdenDetalle; cambios: DatosArgosCambiados; alTerminar: (r: Resultado) => Promise<void> }) {
    const form = useForm<DatosDescartarArgos>({ resolver: zodResolver(esquemaDescartarDatosArgos), defaultValues: { motivo: "" } });
    const { errors, isSubmitting, isDirty } = form.formState;
    useCambiosSinGuardar(isDirty, isSubmitting);
    const { enviar, error } = useEnviarDatosArgos(alTerminar);
    const guardar = (d: DatosDescartarArgos) =>
        enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/ordenes/{id}/descartar-datos-argos", {
                        params: { path: { id: orden.id }, header: { "Idempotency-Key": clave } },
                        body: cuerpoDescartar(d, orden.version, cambios),
                    }),
                ),
            "Aviso de Argos descartado",
        );
    return (
        <DialogoContenido titulo="Descartar el aviso de Argos" descripcion="La orden se queda con sus datos y el aviso desaparece.">
            <form noValidate className="grid gap-4" onSubmit={(e) => void form.handleSubmit(guardar)(e)}>
                <CampoArea etiqueta="Motivo (opcional)" rows={3} maxLength={500} error={errors.motivo?.message} {...form.register("motivo")} />
                {error ? (
                    <Aviso tono="error" titulo="No se pudo descartar">
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <BotonEnviar enviando={isSubmitting} textoEnviando="Descartando…">
                        Descartar aviso
                    </BotonEnviar>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}
