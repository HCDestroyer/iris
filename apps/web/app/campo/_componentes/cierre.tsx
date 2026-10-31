"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { MOTIVOS_FALLA as CODIGOS_FALLA, type OrdenVista } from "@iris/sync";
import { encolarFirmaYCierre, estadoPasos, faltantesAntesDeFirmar, resumenTrabajo } from "@iris/sync/campo";
import {
    Aviso,
    Boton,
    Campo,
    CampoArea,
    CampoSelector,
    Dialogo,
    DialogoContenido,
    DialogoPie,
    GrupoSegmentado,
    InsigniaEstado,
    LienzoFirma,
    Tarjeta,
    TarjetaContenido,
    TarjetaDescripcion,
    TarjetaEncabezado,
    TarjetaTitulo,
    hayFirma,
    trazosAPng,
    trazosASvg,
    type Trazo,
} from "@iris/ui";
import { CircleCheck, CircleX, Lock, PenLine } from "lucide-react";
import { useRef, useState, type FormEvent } from "react";
import { useForm } from "react-hook-form";
import { esquemaFallar, esquemaFirma, type DatosFallar, type DatosFirma } from "@/lib/campo/esquemas";
import { accionesDe } from "@/lib/campo/reglas";
import { MOTIVOS_FALLA } from "@/lib/campo/textos";
import { useCampo } from "./contexto";
import { useUbicacion } from "./ubicacion";

/** GPS del cierre o de la falla si el permiso ya está dado (no pregunta dentro del diálogo). */
function useConPosicion() {
    const { posicionSiPermitida } = useUbicacion();
    return async () => {
        const p = await posicionSiPermitida();
        return p ? { lat: p.lat, lon: p.lon } : null;
    };
}

/** Lo hecho en la orden, en palabras del cliente: checklist, fotos, materiales y equipos. */
function ResumenTrabajo({ orden }: { orden: OrdenVista }) {
    const r = resumenTrabajo(orden);
    return (
        <Tarjeta data-slot="resumen-trabajo">
            <TarjetaEncabezado>
                <TarjetaTitulo>Resumen del trabajo</TarjetaTitulo>
                <TarjetaDescripcion>{r.descripcion}</TarjetaDescripcion>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-4 text-sm">
                {r.checklist.length > 0 ? (
                    <section aria-label="Checklist" className="grid gap-1">
                        <h3 className="text-xs font-semibold text-muted-foreground">Checklist</h3>
                        <ul className="grid gap-1">
                            {r.checklist.map((i) => (
                                <li key={i.clave} className="flex items-start justify-between gap-3">
                                    <span className="min-w-0 break-words">{i.etiqueta}</span>
                                    <span className="shrink-0 text-right font-medium">{i.valor}</span>
                                </li>
                            ))}
                        </ul>
                    </section>
                ) : null}
                {r.materiales.length > 0 ? (
                    <section aria-label="Materiales" className="grid gap-1">
                        <h3 className="text-xs font-semibold text-muted-foreground">Materiales</h3>
                        <ul className="grid gap-1">
                            {r.materiales.map((m) => (
                                <li key={m.clave} className="flex items-start justify-between gap-3">
                                    <span className="min-w-0 break-words">{m.descripcion}</span>
                                    <span className="shrink-0 font-medium tabular-nums">{m.cantidad}</span>
                                </li>
                            ))}
                        </ul>
                    </section>
                ) : null}
                {r.equipos.length > 0 ? (
                    <section aria-label="Equipos" className="grid gap-1">
                        <h3 className="text-xs font-semibold text-muted-foreground">Equipos</h3>
                        <ul className="grid gap-1">
                            {r.equipos.map((e) => (
                                <li key={e.clave} className="break-words">
                                    {e.texto}
                                    {e.serie ? <span className="font-mono"> · {e.serie}</span> : null}
                                </li>
                            ))}
                        </ul>
                    </section>
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}

/**
 * Paso final y único: «Firma del cliente y cierre». Se abre solo con todo lo obligatorio listo;
 * muestra al cliente lo hecho, el cliente firma y en el mismo paso se cierra la orden (firma y
 * cierre van juntos a la cola; el cierre espera a que suba la firma).
 */
export function PasoCierre({ orden }: { orden: OrdenVista }) {
    const { navegar } = useCampo();
    const acciones = accionesDe(orden);
    const faltan = faltantesAntesDeFirmar(orden);
    const pendientes = estadoPasos(orden).filter((p) => p.paso !== "cierre" && !p.completo);

    if (orden.estado === "cerrada") {
        return (
            <>
                <Aviso tono="success" icono={CircleCheck} titulo="Orden cerrada">
                    {orden.firma ? `Firmó ${orden.firma.nombre}.` : "Cerrada sin firma del cliente."}
                </Aviso>
                <ResumenTrabajo orden={orden} />
                <Boton tamano="lg" variante="secundario" onClick={() => navegar({ tipo: "lista" })}>
                    Volver a mis órdenes
                </Boton>
            </>
        );
    }
    if (orden.estado === "fallida" || orden.estado === "cancelada" || orden.reasignada) {
        return (
            <Aviso tono="neutral" titulo="Esta orden ya no se cierra">
                {orden.estado === "fallida" ? "Se marcó como no realizada." : orden.estado === "cancelada" ? "Despacho la canceló." : "Se asignó a otro técnico."}
            </Aviso>
        );
    }
    if (faltan.length > 0) {
        return (
            <Aviso tono="warning" icono={Lock} titulo="Antes de la firma falta:" data-testid="falta-para-firmar">
                <ul className="list-disc pl-4">
                    {faltan.map((f) => (
                        <li key={f}>{f}</li>
                    ))}
                </ul>
                <div className="mt-3 flex flex-wrap gap-2">
                    {pendientes.map((p) => (
                        <Boton key={p.paso} variante="secundario" tamano="lg" onClick={() => navegar({ tipo: "orden", id: orden.id, paso: p.paso })}>
                            Ir a {p.titulo.toLowerCase()}
                        </Boton>
                    ))}
                </div>
            </Aviso>
        );
    }
    if (!acciones.cerrar) {
        return (
            <Aviso tono="warning" titulo="La orden está pausada">
                Reanúdala desde el resumen para tomar la firma y cerrarla.
            </Aviso>
        );
    }
    return <FirmaYCierre orden={orden} />;
}

function FirmaYCierre({ orden }: { orden: OrdenVista }) {
    const { motor, ejecutar } = useCampo();
    const conPosicion = useConPosicion();
    const enviando = useRef(false);
    const [ocupado, setOcupado] = useState(false);
    const [resultado, setResultado] = useState<"exitosa" | "parcial">("exitosa");
    const [rehacer, setRehacer] = useState(false);
    const [trazos, setTrazos] = useState<{ trazos: Trazo[]; ancho: number; alto: number }>({ trazos: [], ancho: 0, alto: 0 });
    const [errorTrazo, setErrorTrazo] = useState<string | undefined>();
    const form = useForm<DatosFirma>({ resolver: zodResolver(esquemaFirma), defaultValues: { nombre: orden.contacto.nombre ?? "", documento: "" } });
    const firmada = orden.firma !== null && orden.firma !== undefined && !rehacer;
    const firmo = hayFirma(trazos.trazos);
    // Sin firma obligatoria y sin trazo, se cierra sin firma (no se valida el nombre).
    const tomaFirma = !firmada && (orden.requiereFirma || firmo);

    const cerrar = async (d: DatosFirma | null) => {
        // Una sola vez por toque, con o sin firma: mientras se espera el GPS o se guarda, un segundo
        // toque no encola otro cierre.
        if (enviando.current) {
            return;
        }
        if (!firmada && orden.requiereFirma && !firmo) {
            setErrorTrazo("Pide al cliente que firme en el recuadro");
            return;
        }
        setErrorTrazo(undefined);
        enviando.current = true;
        setOcupado(true);
        const ok = await ejecutar(async () => {
            const firma =
                d && firmo
                    ? {
                          nombre: d.nombre,
                          documento: d.documento,
                          svg: trazosASvg(trazos.trazos, trazos.ancho, trazos.alto),
                          png: new Uint8Array(await (await trazosAPng(trazos.trazos, trazos.ancho, trazos.alto)).arrayBuffer()),
                          tomadoEn: new Date().toISOString(),
                      }
                    : null;
            await encolarFirmaYCierre(motor, { orden, firma, resultado, posicion: await conPosicion() });
        }, "Orden cerrada");
        if (!ok) {
            // Si no se guardó, se puede volver a intentar; si se guardó, la orden ya está cerrada.
            enviando.current = false;
            setOcupado(false);
        }
    };
    const enviar = (e: FormEvent) => {
        if (tomaFirma) {
            void form.handleSubmit((d) => cerrar(d))(e);
            return;
        }
        e.preventDefault();
        void cerrar(null);
    };

    return (
        <form onSubmit={enviar} noValidate aria-label="Firma del cliente y cierre" className="grid gap-4">
            <ResumenTrabajo orden={orden} />
            <Tarjeta>
                <TarjetaEncabezado>
                    <TarjetaTitulo>Firma del cliente</TarjetaTitulo>
                    <TarjetaDescripcion>{orden.requiereFirma ? "Obligatoria para cerrar esta orden." : "Opcional."}</TarjetaDescripcion>
                </TarjetaEncabezado>
                <TarjetaContenido className="grid gap-3">
                    {firmada && orden.firma ? (
                        <div className="flex flex-wrap items-center gap-2">
                            <InsigniaEstado tono="success" icono={PenLine} texto="Firmada" />
                            <span className="text-sm">
                                {orden.firma.nombre}
                                {orden.firma.documento ? ` · ${orden.firma.documento}` : ""}
                            </span>
                            <Boton variante="fantasma" tamano="lg" className="ml-auto" onClick={() => setRehacer(true)}>
                                Volver a firmar
                            </Boton>
                        </div>
                    ) : (
                        <>
                            <Campo etiqueta="Nombre de quien firma" autoComplete="off" error={form.formState.errors.nombre?.message} {...form.register("nombre")} />
                            <Campo etiqueta="Documento (DPI)" inputMode="numeric" autoComplete="off" error={form.formState.errors.documento?.message} {...form.register("documento")} />
                            <LienzoFirma alCambiar={(t, tam) => setTrazos({ trazos: t, ...tam })} />
                            {errorTrazo ? <p className="text-sm text-error">{errorTrazo}</p> : null}
                        </>
                    )}
                </TarjetaContenido>
            </Tarjeta>
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
            {orden.subidasPendientes > 0 ? (
                <Aviso tono="info" titulo="Las fotos o la firma aún se están subiendo">
                    El cierre queda guardado y se envía solo cuando terminen de subir.
                </Aviso>
            ) : null}
            <div className="glass-material sticky bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-20 -mx-4 border-t border-border px-4 py-3">
                <Boton type="submit" tamano="lg" className="w-full" disabled={ocupado || form.formState.isSubmitting}>
                    <CircleCheck aria-hidden />
                    {ocupado ? "Guardando…" : tomaFirma ? "Firmar y cerrar orden" : "Cerrar orden"}
                </Boton>
            </div>
        </form>
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
                motor.registrar("orden.fallar", orden.id, { motivoFalla: d.motivoFalla, ...(d.detalle ? { detalle: d.detalle } : {}), ...((await conPosicion()) ?? {}) }),
            "Orden marcada como no realizada",
        );
        if (ok) {
            setAbierto(false);
        }
    });
    return (
        <Dialogo open={abierto} onOpenChange={setAbierto}>
            <Boton variante="secundario" tamano="lg" className="w-full" onClick={() => setAbierto(true)}>
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
                        <Boton tamano="lg" variante="fantasma" onClick={() => setAbierto(false)}>
                            Volver
                        </Boton>
                        <Boton tamano="lg" type="submit" disabled={form.formState.isSubmitting}>
                            Confirmar
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
