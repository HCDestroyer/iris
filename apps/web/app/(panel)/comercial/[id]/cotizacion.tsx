"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import {
    avisar,
    Aviso,
    Boton,
    Campo,
    CampoArea,
    CampoSelector,
    Casilla,
    Esqueleto,
    FormularioPagina,
    SeccionFormulario,
    Tabla,
    TablaCelda,
    TablaCuerpo,
    TablaEncabezado,
    TablaFila,
    TablaTitulo,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { Calculator, CircleCheck, FilePlus, History, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useFieldArray, useForm, useWatch } from "react-hook-form";
import { AvisoAccion, DescargarPdf, InsigniaCotizacion, useEnvio } from "@/componentes/comercial/comun";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { cuerpoCotizacion, esquemaCotizacion, lineaVacia, type DatosCotizacion } from "@/lib/comercial/esquemas";
import { TIPOS_LINEA, textoMonto, textoMotivo, type Cotizacion, type PlanesDisponibles, type ProspectoDetalle, type TipoLinea } from "@/lib/comercial/presentacion";
import { formatearFecha } from "@/lib/fechas";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

type Props = { detalle: ProspectoDetalle; recargar: () => Promise<void> };

/**
 * Cotización viva: líneas, totales (los calcula la API: aquí solo se muestran), vigencia, PDF,
 * aceptar y revisiones. Una revisión nueva reemplaza la vigente sin borrarla.
 */
export function TarjetaCotizacion({ detalle, recargar }: Props) {
    const { puede } = useSesion();
    const c = detalle.cotizacion;
    const puedeCotizar = detalle.acciones.cotizar && puede(["cotizaciones.crear"]);
    const puedeAceptar = detalle.acciones.aceptar && puede(["cotizaciones.aceptar"]);
    const [revisiones, setRevisiones] = useState(false);
    const envio = useEnvio();

    async function aceptar(cot: Cotizacion) {
        const r = await envio.enviar((clave) =>
            resolver(api().POST("/api/v1/cotizaciones/{id}/aceptar", { params: { path: { id: cot.id }, header: { "Idempotency-Key": clave } }, body: { version: cot.version } })),
        );
        if (r) {
            avisar({ titulo: `Cotización ${cot.numero} aceptada` });
            await recargar();
        }
    }

    const envioPdf = useEnvio();
    async function regenerarPdf(cot: Cotizacion) {
        await envioPdf.enviar(
            (clave) => resolver(api().POST("/api/v1/cotizaciones/{id}/pdf", { params: { path: { id: cot.id }, header: { "Idempotency-Key": clave } } })),
            undefined,
            { lanzar: true },
        );
        await recargar();
    }

    return (
        <Tarjeta data-testid="cotizacion">
            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                <TarjetaTitulo>Cotización</TarjetaTitulo>
                {c ? <InsigniaCotizacion cotizacion={c} /> : null}
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-3">
                {!c ? (
                    <p className="text-sm text-muted-foreground">
                        {detalle.factibilidad && (detalle.factibilidad.resultado === "viable" || detalle.factibilidad.resultado === "requiere_obra")
                            ? "Aún no hay cotización."
                            : "Se cotiza cuando la factibilidad es viable o con obra."}
                    </p>
                ) : (
                    <DetalleCotizacion c={c} />
                )}
                {c?.vencida && c.estado === "vigente" ? (
                    <Aviso tono="warning" titulo={`Venció el ${formatearFecha(c.vigenteHasta)}`}>
                        Para que el cliente la acepte, crea una revisión nueva.
                    </Aviso>
                ) : null}
                {c ? (
                    <DescargarPdf
                        documento={c.pdf}
                        nombre={`${c.numero}-r${c.revision}`}
                        {...(puede(["cotizaciones.crear"]) ? { regenerar: () => regenerarPdf(c) } : {})}
                    />
                ) : null}
                <div className="flex flex-wrap gap-2">
                    {c && puedeAceptar ? (
                        <Boton tamano="sm" onClick={() => aceptar(c)} disabled={envio.enviando || envio.conflicto}>
                            <CircleCheck />
                            {envio.enviando ? "Aceptando…" : "El cliente la aceptó"}
                        </Boton>
                    ) : null}
                    {puedeCotizar ? (
                        <Boton asChild variante={c ? "secundario" : "primario"} tamano="sm">
                            <Link href={`/comercial/${detalle.id}/cotizacion`}>
                                <FilePlus />
                                {c ? "Nueva revisión" : "Cotizar"}
                            </Link>
                        </Boton>
                    ) : null}
                    {c && c.revision > 1 ? (
                        <Boton variante="fantasma" tamano="sm" onClick={() => setRevisiones((v) => !v)} aria-expanded={revisiones}>
                            <History />
                            {revisiones ? "Ocultar revisiones" : "Ver revisiones"}
                        </Boton>
                    ) : null}
                </div>
                <AvisoAccion envio={envio} que="aceptar la cotización" recargar={recargar} />
                {revisiones ? <Revisiones prospectoId={detalle.id} /> : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

function DetalleCotizacion({ c }: { c: Cotizacion }) {
    const lineas = c.lineas ?? [];
    return (
        <div className="grid gap-3">
            <p className="text-sm">
                <span className="font-mono tabular-nums">{c.numero}</span> · revisión {c.revision} · vigente hasta{" "}
                <span className="tabular-nums">{formatearFecha(c.vigenteHasta)}</span>
            </p>
            {/* Teléfono: cada línea como tarjeta (sin tabla que desborde). */}
            <ul className="grid gap-2 sm:hidden" aria-label={`Líneas de ${c.numero}`}>
                {lineas.map((l) => (
                    <li key={l.posicion} className="grid gap-1 rounded-md border p-3 text-sm">
                        <span className="flex items-start justify-between gap-3">
                            <span className="min-w-0 font-medium break-words">{l.descripcion}</span>
                            <span className="shrink-0 font-semibold tabular-nums">{textoMonto(l.total, c.moneda)}</span>
                        </span>
                        <span className="text-xs text-muted-foreground tabular-nums">
                            {TIPOS_LINEA[l.tipo]} · {l.recurrente ? "mensual" : "único"} · {l.cantidad} × {textoMonto(l.precioUnitario, c.moneda)}
                        </span>
                    </li>
                ))}
            </ul>
            <div className="hidden rounded-md border sm:block">
                <Tabla aria-label={`Líneas de ${c.numero}`}>
                    <TablaEncabezado>
                        <TablaFila className="hover:bg-transparent">
                            <TablaTitulo>Concepto</TablaTitulo>
                            <TablaTitulo className="text-right">Cant.</TablaTitulo>
                            <TablaTitulo className="text-right">Precio</TablaTitulo>
                            <TablaTitulo className="text-right">Total</TablaTitulo>
                        </TablaFila>
                    </TablaEncabezado>
                    <TablaCuerpo>
                        {lineas.map((l) => (
                            <TablaFila key={l.posicion}>
                                <TablaCelda>
                                    <span className="grid">
                                        <span>{l.descripcion}</span>
                                        <span className="text-xs text-muted-foreground">
                                            {TIPOS_LINEA[l.tipo]} · {l.recurrente ? "mensual" : "único"}
                                        </span>
                                    </span>
                                </TablaCelda>
                                <TablaCelda className="text-right tabular-nums">{l.cantidad}</TablaCelda>
                                <TablaCelda className="text-right whitespace-nowrap tabular-nums">{textoMonto(l.precioUnitario, c.moneda)}</TablaCelda>
                                <TablaCelda className="text-right whitespace-nowrap tabular-nums">{textoMonto(l.total, c.moneda)}</TablaCelda>
                            </TablaFila>
                        ))}
                    </TablaCuerpo>
                </Tabla>
            </div>
            <dl className="grid grid-cols-[1fr_auto] gap-x-3 gap-y-1 text-sm" aria-label="Totales">
                <dt className="text-muted-foreground">Total mensual</dt>
                <dd className="text-right font-semibold tabular-nums" data-testid="total-mensual">
                    {textoMonto(c.totalMensual, c.moneda)}
                </dd>
                <dt className="text-muted-foreground">Pago único</dt>
                <dd className="text-right font-semibold tabular-nums" data-testid="total-unico">
                    {textoMonto(c.totalUnico, c.moneda)}
                </dd>
            </dl>
            {c.notas ? <p className="text-sm whitespace-pre-wrap text-muted-foreground">{c.notas}</p> : null}
        </div>
    );
}

function Revisiones({ prospectoId }: { prospectoId: number }) {
    const lectura = useLectura((signal) => resolver(api().GET("/api/v1/prospectos/{id}/cotizaciones", { params: { path: { id: prospectoId } }, signal })));
    if (lectura.estado === "cargando") return <Esqueleto className="h-16" />;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} que="las revisiones" reintentar={lectura.reintentar} />;
    const lista = lectura.datos.data ?? [];
    return (
        <ul className="grid gap-2" aria-label="Revisiones de la cotización">
            {lista.map((c) => (
                <li key={c.id} className="flex flex-wrap items-center justify-between gap-2 rounded-md border p-2 text-sm">
                    <span>
                        <span className="font-mono tabular-nums">{c.numero}</span> · revisión {c.revision} · {formatearFecha(c.creadoEn)}
                    </span>
                    <span className="flex items-center gap-2">
                        <span className="tabular-nums">{textoMonto(c.totalMensual, c.moneda)}/mes</span>
                        <InsigniaCotizacion cotizacion={c} />
                    </span>
                </li>
            ))}
        </ul>
    );
}

const TIPOS_EXTRA: readonly TipoLinea[] = ["instalacion", "equipo", "descuento", "otro"];

/**
 * Nueva cotización o revisión en pantalla propia (`/comercial/[id]/cotizacion`): los planes salen
 * de `GET /comercial/planes`. Si no se pueden leer, no hay formulario (no se cotiza a ciegas).
 * Sin conector, el plan va con descripción y precio a mano. Al guardar, aviso y ficha.
 */
export function NuevaCotizacion({ detalle, recargar }: Props) {
    // Los planes dependen del router de la factibilidad (son de la instalación).
    const routerId = detalle.factibilidad?.routerId ?? null;
    const planes = useLectura<PlanesDisponibles>((signal) =>
        resolver(api().GET("/api/v1/comercial/planes", { params: { query: routerId !== null ? { routerId } : {} }, signal })),
    );
    if (planes.estado === "cargando") return <Esqueleto className="mx-auto h-40 w-full max-w-3xl" />;
    if (planes.estado === "error") return <ErrorLectura error={planes.error} que="los planes de Argos" reintentar={planes.reintentar} />;
    if (!planes.datos.disponible && planes.datos.motivo !== "sin_conector") {
        return (
            <Aviso tono="warning" titulo="No se pueden leer los planes de Argos">
                {textoMotivo(planes.datos.motivo)} Sin ellos no se puede cotizar; inténtalo de nuevo cuando Argos responda.
            </Aviso>
        );
    }
    return <FormCotizacion detalle={detalle} planes={planes.datos.disponible ? (planes.datos.data ?? []) : null} recargar={recargar} />;
}

function FormCotizacion({ detalle, planes, recargar }: Props & { planes: Schema<"PlanComercial">[] | null }) {
    const envio = useEnvio();
    const router = useRouter();
    const activos = planes?.filter((p) => p.activo) ?? null;
    const plan = detalle.planInteresId !== null ? activos?.find((p) => p.id === detalle.planInteresId) : undefined;
    const formulario = useForm<DatosCotizacion>({
        resolver: zodResolver(esquemaCotizacion(activos)),
        defaultValues: {
            lineas: [{ ...lineaVacia("plan"), planId: plan ? String(plan.id) : "", descripcion: activos === null ? (detalle.planInteresNombre ?? "") : "" }],
            vigenteDia: "",
            notas: "",
        },
    });
    const { fields, append, remove } = useFieldArray({ control: formulario.control, name: "lineas" });
    const { errors } = formulario.formState;
    const lineas = useWatch({ control: formulario.control, name: "lineas" });
    // Con el catálogo de Argos, cada línea de plan necesita su plan elegido.
    const sinPlan = activos !== null && lineas.some((l) => l.tipo === "plan" && !l.planId);

    async function enviar(d: DatosCotizacion) {
        const nombres = d.lineas.flatMap((_, i) => [`lineas.${i}.planId`, `lineas.${i}.descripcion`, `lineas.${i}.cantidad`, `lineas.${i}.precioUnitario`] as const);
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/prospectos/{id}/cotizaciones", {
                        params: { path: { id: detalle.id }, header: { "Idempotency-Key": clave } },
                        body: cuerpoCotizacion(d, detalle.version, activos),
                    }),
                ),
            { nombres: [...nombres, "notas"], setError: formulario.setError },
        );
        if (r) {
            avisar({ titulo: `Cotización ${r.numero} creada` });
            router.replace(`/comercial/${detalle.id}`);
        }
    }

    return (
        <FormularioPagina
            titulo={detalle.cotizacion ? "Nueva revisión de la cotización" : "Cotizar"}
            descripcion={`${detalle.numero} · ${detalle.nombre}`}
            alEnviar={formulario.handleSubmit(enviar)}
            navegar={router.push}
            volverA={`/comercial/${detalle.id}`}
            sucio={formulario.formState.isDirty}
            barra={{
                textoGuardar: "Guardar cotización",
                enviando: envio.enviando,
                ...(envio.conflicto ? { faltan: ["la versión nueva"], motivo: "Recarga el prospecto para guardar." } : { faltan: sinPlan ? ["el plan"] : [] }),
            }}
            error={<AvisoAccion envio={envio} que="crear la cotización" recargar={recargar} />}
        >
            {activos === null ? (
                <Aviso tono="info" titulo="Sin conexión con Argos">
                    Escribe el plan con su descripción y precio. Al conectar Argos, los planes salen de su catálogo.
                </Aviso>
            ) : null}
            <SeccionFormulario titulo="1. Líneas">
            <ol className="grid gap-3" aria-label="Líneas">
                {fields.map((campo, i) => {
                    const l = lineas[i];
                    const e = errors.lineas?.[i];
                    const esPlan = l?.tipo === "plan";
                    const planElegido = esPlan && activos ? activos.find((p) => String(p.id) === l.planId) : undefined;
                    return (
                        <li key={campo.id} className="grid gap-3 rounded-lg border p-3" data-linea={l?.tipo}>
                            <div className="flex items-center justify-between gap-2">
                                <span className="text-sm font-medium">{TIPOS_LINEA[l?.tipo ?? "otro"]}</span>
                                {!esPlan ? (
                                    <Boton type="button" variante="fantasma" tamano="icono-sm" aria-label={`Quitar la línea ${i + 1}`} title="Quitar" onClick={() => remove(i)}>
                                        <Trash2 />
                                    </Boton>
                                ) : null}
                            </div>
                            {esPlan && activos ? (
                                <CampoSelector etiqueta="Plan" error={e?.planId?.message} {...formulario.register(`lineas.${i}.planId`)}>
                                    <option value="">Elige el plan…</option>
                                    {activos.map((p) => (
                                        <option key={p.id} value={String(p.id)}>
                                            {p.nombre}
                                            {p.precio !== null ? ` · ${textoMonto(p.precio)}/mes` : " · precio a mano"}
                                        </option>
                                    ))}
                                </CampoSelector>
                            ) : (
                                <Campo etiqueta="Descripción" error={e?.descripcion?.message} {...formulario.register(`lineas.${i}.descripcion`)} />
                            )}
                            <div className="grid gap-3 sm:grid-cols-2">
                                {esPlan ? null : <Campo etiqueta="Cantidad" inputMode="decimal" error={e?.cantidad?.message} {...formulario.register(`lineas.${i}.cantidad`)} />}
                                {esPlan && planElegido && planElegido.precio !== null ? (
                                    <p className="self-end pb-2 text-sm text-muted-foreground">Precio de Argos: {textoMonto(planElegido.precio)} al mes</p>
                                ) : (
                                    <Campo
                                        etiqueta={l?.tipo === "descuento" ? "Monto a restar" : "Precio unitario"}
                                        inputMode="decimal"
                                        placeholder="0.00"
                                        error={e?.precioUnitario?.message}
                                        {...formulario.register(`lineas.${i}.precioUnitario`)}
                                    />
                                )}
                            </div>
                            {l && (l.tipo === "equipo" || l.tipo === "descuento" || l.tipo === "otro") ? (
                                <Casilla etiqueta="Se cobra cada mes" {...formulario.register(`lineas.${i}.recurrente`)} />
                            ) : null}
                        </li>
                    );
                })}
            </ol>
            {errors.lineas?.message || errors.lineas?.root?.message ? (
                <p className="text-sm text-error" role="alert">
                    {errors.lineas.message ?? errors.lineas.root?.message}
                </p>
            ) : null}
            <div className="flex flex-wrap gap-2" role="group" aria-label="Agregar línea">
                {TIPOS_EXTRA.map((t) => (
                    <Boton key={t} type="button" variante="contorno" tamano="sm" onClick={() => append(lineaVacia(t))}>
                        <Plus />
                        {TIPOS_LINEA[t]}
                    </Boton>
                ))}
            </div>
            </SeccionFormulario>
            <SeccionFormulario titulo="2. Vigencia y notas">
            <div className="grid gap-4 sm:grid-cols-2">
                <Campo etiqueta="Vigente hasta (opcional)" type="date" ayuda="Si lo dejas vacío, 15 días." error={errors.vigenteDia?.message} {...formulario.register("vigenteDia")} />
            </div>
            <CampoArea etiqueta="Notas (opcional)" rows={2} error={errors.notas?.message} {...formulario.register("notas")} />
            <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
                <Calculator aria-hidden className="size-4" />
                Los totales los calcula Iris al guardar.
            </p>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
