"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Boton, Campo, CampoArea, CampoSelector, Esqueleto, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { CalendarClock, Plus } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { AvisoAccion, useEnvio } from "@/componentes/comercial/comun";
import { ErrorLectura } from "@/componentes/error-lectura";
import { PieLista } from "@/componentes/pie-lista";
import { nombreUsuario, type Usuario } from "@/componentes/tickets/catalogos";
import { api, resolver } from "@/lib/api/cliente";
import { ACTIVIDAD_VACIA, cuerpoActividad, esquemaActividad, type DatosActividad } from "@/lib/comercial/esquemas";
import { TIPOS_ACTIVIDAD, type Actividad, type TipoActividad } from "@/lib/comercial/presentacion";
import { formatearFecha } from "@/lib/fechas";
import { usePaginado } from "@/lib/paginado";
import { useSesion } from "@/lib/sesion";

/** Actividades (llamada, visita, nota, mensaje) con su próxima acción; alta con `prospectos.registrar_actividad`. */
export function Actividades({ prospectoId, usuarios, recargar }: { prospectoId: number; usuarios: readonly Usuario[] | null; recargar: () => Promise<void> }) {
    const { yo, puede } = useSesion();
    const paginado = usePaginado<Actividad>((cursor, signal) =>
        resolver(
            api().GET("/api/v1/prospectos/{id}/actividades", {
                params: { path: { id: prospectoId }, query: { limit: 50, ...(cursor ? { cursor } : {}) } },
                ...(signal ? { signal } : {}),
            }),
        ),
    );
    const [formulario, setFormulario] = useState(false);
    const { primera, items } = paginado;

    return (
        <Tarjeta data-testid="actividades">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Actividades</TarjetaTitulo>
                {puede(["prospectos.registrar_actividad"]) && !formulario ? (
                    <Boton variante="secundario" tamano="sm" onClick={() => setFormulario(true)}>
                        <Plus />
                        Registrar
                    </Boton>
                ) : null}
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {formulario ? (
                    <NuevaActividad
                        prospectoId={prospectoId}
                        alCancelar={() => setFormulario(false)}
                        alHecho={(a) => {
                            paginado.agregar(a);
                            setFormulario(false);
                            void recargar();
                        }}
                    />
                ) : null}
                {primera.estado === "cargando" ? (
                    <Esqueleto className="h-16" />
                ) : primera.estado === "error" ? (
                    <ErrorLectura error={primera.error} que="las actividades" reintentar={primera.reintentar} />
                ) : items.length === 0 ? (
                    <p className="text-sm text-muted-foreground">Sin actividades todavía.</p>
                ) : (
                    <ol className="grid gap-3" aria-label="Actividades">
                        {items.map((a) => {
                            const t = TIPOS_ACTIVIDAD[a.tipo];
                            const Icono = t.icono;
                            return (
                                <li key={a.id} className="grid gap-1 border-l-2 pl-3 text-sm" data-actividad={a.tipo}>
                                    <span className="flex flex-wrap items-center gap-x-2 text-xs text-muted-foreground">
                                        <Icono aria-hidden className="size-3.5" />
                                        <span className="font-medium text-foreground">{t.texto}</span>
                                        <span className="tabular-nums">{formatearFecha(a.ocurridaEn)}</span>
                                        {a.autorId !== null ? <span>· {nombreUsuario(usuarios, a.autorId, { id: yo.usuario.id, nombre: yo.usuario.nombre })}</span> : null}
                                    </span>
                                    <p className="whitespace-pre-wrap">{a.descripcion}</p>
                                    {a.resultado ? <p className="text-muted-foreground">Resultado: {a.resultado}</p> : null}
                                    {a.proximaAccion ? (
                                        <p className="flex items-center gap-1.5 text-info">
                                            <CalendarClock aria-hidden className="size-4" />
                                            Próxima: {a.proximaAccion}
                                            {a.proximaFecha ? <span className="tabular-nums"> · {formatearFecha(a.proximaFecha)}</span> : null}
                                        </p>
                                    ) : null}
                                </li>
                            );
                        })}
                    </ol>
                )}
                <PieLista paginado={paginado} que="actividades" />
            </TarjetaContenido>
        </Tarjeta>
    );
}

function NuevaActividad({ prospectoId, alCancelar, alHecho }: { prospectoId: number; alCancelar: () => void; alHecho: (a: Actividad) => void }) {
    const envio = useEnvio();
    const formulario = useForm<DatosActividad>({ resolver: zodResolver(esquemaActividad), defaultValues: ACTIVIDAD_VACIA });
    const { errors } = formulario.formState;
    async function enviar(d: DatosActividad) {
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/prospectos/{id}/actividades", {
                        params: { path: { id: prospectoId }, header: { "Idempotency-Key": clave } },
                        body: cuerpoActividad(d),
                    }),
                ),
            { nombres: ["tipo", "descripcion", "resultado", "proximaAccion"], setError: formulario.setError },
        );
        if (r) alHecho(r);
    }
    return (
        <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-3 rounded-lg border p-3" aria-label="Nueva actividad">
            <CampoSelector etiqueta="Tipo" error={errors.tipo?.message} {...formulario.register("tipo")}>
                {(Object.keys(TIPOS_ACTIVIDAD) as TipoActividad[]).map((t) => (
                    <option key={t} value={t}>
                        {TIPOS_ACTIVIDAD[t].texto}
                    </option>
                ))}
            </CampoSelector>
            <CampoArea etiqueta="Qué pasó" rows={2} error={errors.descripcion?.message} {...formulario.register("descripcion")} />
            <Campo etiqueta="Resultado (opcional)" error={errors.resultado?.message} {...formulario.register("resultado")} />
            <Campo etiqueta="Próxima acción (opcional)" error={errors.proximaAccion?.message} {...formulario.register("proximaAccion")} />
            <div className="grid gap-3 sm:grid-cols-2">
                <Campo etiqueta="Día de la próxima acción" type="date" error={errors.proximaDia?.message} {...formulario.register("proximaDia")} />
                <Campo etiqueta="Hora (opcional)" type="time" error={errors.proximaHora?.message} {...formulario.register("proximaHora")} />
            </div>
            <AvisoAccion envio={envio} que="registrar la actividad" recargar={async () => undefined} />
            <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                <Boton type="button" variante="contorno" onClick={alCancelar} disabled={envio.enviando}>
                    Cancelar
                </Boton>
                <Boton type="submit" disabled={envio.enviando}>
                    {envio.enviando ? "Guardando…" : "Guardar actividad"}
                </Boton>
            </div>
        </form>
    );
}
