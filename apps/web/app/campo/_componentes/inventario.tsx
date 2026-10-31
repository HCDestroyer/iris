"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { ErrorTransporte, type MiBodega as DatosBodega, type OperacionTrasladoLocal, type Traslado } from "@iris/sync";
import {
    esquemaFormularioRecepcion,
    formularioRecepcionInicial,
    lineasDeRecepcion,
    sePuedeRehacerRecepcion,
    situacionTraslado,
    textoProblema,
    type FormularioRecepcion,
    type SituacionTraslado,
} from "@iris/sync/campo";
import {
    Aviso,
    Boton,
    Campo,
    Cargando,
    Casilla,
    EstadoVacio,
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
    type Tono,
    type Trazo,
} from "@iris/ui";
import {
    ArrowLeft,
    ChevronRight,
    CircleCheck,
    CloudUpload,
    Hourglass,
    PackageCheck,
    PackageOpen,
    RefreshCw,
    Send,
    Trash2,
    TriangleAlert,
    Undo2,
    WifiOff,
    type LucideIcon,
} from "lucide-react";
import { useEffect, useRef, useState, type MouseEvent } from "react";
import { Controller, useFieldArray, useForm, useWatch } from "react-hook-form";
import { useAhora } from "@/componentes/tickets/insignias";
import { haceTiempo } from "@/lib/datos/presentacion";
import { formatearFecha } from "@/lib/fechas";
import { urlDeVista } from "@/lib/campo/vista";
import { useCampo } from "./contexto";
import { EstadoAdjunto } from "./fotos";
import { useInventario, type Lectura } from "./inventario-datos";

const SITUACIONES: Record<SituacionTraslado, { tono: Tono; icono: LucideIcon; texto: string }> = {
    por_recibir: { tono: "neutral", icono: PackageOpen, texto: "Por recibir" },
    en_telefono: { tono: "warning", icono: CloudUpload, texto: "Recibido · por enviar" },
    rechazado_telefono: { tono: "error", icono: TriangleAlert, texto: "Rechazado" },
    enviando: { tono: "info", icono: Send, texto: "Enviando a Argos" },
    recibido: { tono: "success", icono: CircleCheck, texto: "Recibido" },
    rechazado_argos: { tono: "error", icono: TriangleAlert, texto: "Argos lo rechazó" },
    agotado: { tono: "warning", icono: Hourglass, texto: "Argos no respondió" },
};

/** Problemas de la lectura del inventario en palabras del técnico. */
const PROBLEMAS_INVENTARIO: Record<string, string> = {
    tecnico_sin_vincular: "Tu ficha no está vinculada con tu usuario de Argos. Pide a tu supervisor que la vincule.",
    argos_sin_inventario_tecnico: "Argos todavía no maneja inventario por técnico.",
    tecnico_sin_bodega: "No tienes bodega en Argos. Pide a bodega que te asigne una.",
    tecnico_ambiguo: "Argos tiene más de un usuario para ti. Pide a tu supervisor que revise el vínculo.",
    conector_apagado: "Iris no está conectado a Argos en este momento.",
    empresa_requerida: "Elige la empresa en la que trabajas.",
};

function textoErrorLectura(error: unknown, que: string): { sinSenal: boolean; texto: string } {
    if (error instanceof ErrorTransporte && error.tipo === "red") {
        return { sinSenal: true, texto: `Sin señal: conéctate para ver ${que}.` };
    }
    const codigo = error instanceof ErrorTransporte ? error.codigo : null;
    return { sinSenal: false, texto: (codigo ? PROBLEMAS_INVENTARIO[codigo] : undefined) ?? `No se pudo leer ${que}.` };
}

/** Descripción de un producto: de las existencias del paquete o de «Mi bodega», si ya se leyó. */
function useDescripciones(): (productoId: number) => string {
    const { estado } = useCampo();
    const { bodega } = useInventario();
    return (productoId) =>
        estado.existencias?.bodegas?.flatMap((b) => b.lineas ?? []).find((l) => l.productoId === productoId)?.descripcion ??
        bodega.datos?.existencias?.find((e) => e.productoId === productoId)?.descripcion ??
        `Producto ${productoId}`;
}

function useCargarAlAbrir<T>(lectura: Lectura<T>, cargar: () => Promise<void>) {
    const { estado } = useCampo();
    const sinSenal = estado.conexion === "sin_conexion";
    useEffect(() => {
        // Al abrir la vista: se relee con señal; sin señal queda lo último leído (si hay).
        if (!sinSenal) {
            void cargar();
        }
        // Solo al abrir la vista o al volver la señal.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [sinSenal]);
    return lectura;
}

/** Cuando una recepción del teléfono sale (o cambia), se relee la lista para ver cómo va en Argos. */
function useReleerAlEnviar(cargar: () => Promise<void>) {
    const { estado } = useCampo();
    const ids = estado.recepciones.map((r) => `${r.id}:${r.estado}`).join(",");
    const previos = useRef(ids);
    const sinSenal = estado.conexion === "sin_conexion";
    useEffect(() => {
        if (previos.current !== ids && !sinSenal) {
            void cargar();
        }
        previos.current = ids;
        // Solo cuando cambia la cola de recepciones.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [ids]);
}

function ErrorLectura({ error, que, reintentar, conDatos }: { error: unknown; que: string; reintentar: () => void; conDatos: boolean }) {
    const e = textoErrorLectura(error, que);
    return (
        <Aviso
            tono={conDatos || e.sinSenal ? "warning" : "error"}
            icono={e.sinSenal ? WifiOff : TriangleAlert}
            titulo={conDatos ? `${e.texto} Se muestra lo último que se leyó.` : e.texto}
            accion={
                <Boton variante="secundario" tamano="lg" onClick={reintentar}>
                    <RefreshCw aria-hidden />
                    Reintentar
                </Boton>
            }
        />
    );
}

/** «Por recibir»: traslados de Argos hacia la bodega del técnico. */
export function PorRecibir() {
    const { estado } = useCampo();
    const { traslados, cargarTraslados } = useInventario();
    useCargarAlAbrir(traslados, cargarTraslados);
    useReleerAlEnviar(cargarTraslados);
    const lista = traslados.datos ?? [];
    const conocidos = new Set(lista.map((t) => t.id));
    // Recibidos en este teléfono que la lista no trae (sin señal o ya no figuran como enviados).
    const soloLocales = estado.recepciones.filter((op) => !conocidos.has(op.datos.trasladoId));

    return (
        <section aria-labelledby="titulo-recibir" className="grid gap-4">
            <div className="flex items-end justify-between gap-3">
                <div>
                    <h1 id="titulo-recibir" className="text-2xl font-semibold tracking-[-0.015em]">
                        Por recibir
                    </h1>
                    <p className="text-sm text-muted-foreground">Material que bodega te mandó en Argos.</p>
                </div>
                {traslados.estado === "cargando" ? <Cargando visible texto="Leyendo…" /> : null}
            </div>
            {traslados.estado === "error" ? <ErrorLectura error={traslados.error} que="tus traslados" reintentar={() => void cargarTraslados()} conDatos={traslados.datos !== null} /> : null}
            {traslados.datos !== null && lista.length === 0 && soloLocales.length === 0 ? (
                <EstadoVacio icono={PackageCheck} titulo="No tienes nada por recibir" descripcion="Cuando bodega te mande material, aparecerá aquí." />
            ) : null}
            {traslados.datos === null && traslados.estado !== "error" && soloLocales.length === 0 ? <Cargando visible texto="Leyendo tus traslados…" /> : null}
            {lista.length > 0 || soloLocales.length > 0 ? (
                <ul className="grid gap-3" aria-label="Traslados">
                    {lista.map((t) => (
                        <TarjetaTraslado key={t.id} id={t.id} lineas={t.lineas?.length ?? 0} enviadoEn={t.enviadoEn} situacion={situacionTraslado(t, estado.recepciones)} />
                    ))}
                    {soloLocales.map((op) => (
                        <TarjetaTraslado key={op.id} id={op.datos.trasladoId} lineas={null} enviadoEn={undefined} situacion={op.estado === "rechazada" ? "rechazado_telefono" : "en_telefono"} />
                    ))}
                </ul>
            ) : null}
            {traslados.leidoEn !== null ? <LeidoEn ms={traslados.leidoEn} /> : null}
        </section>
    );
}

function LeidoEn({ ms }: { ms: number }) {
    const ahora = useAhora(60_000);
    return <p className="text-xs text-muted-foreground">Leído de Argos {haceTiempo(new Date(ms).toISOString(), ahora)}</p>;
}

function TarjetaTraslado({ id, lineas, enviadoEn, situacion }: { id: number; lineas: number | null; enviadoEn: string | undefined; situacion: SituacionTraslado }) {
    const { navegar } = useCampo();
    const s = SITUACIONES[situacion];
    const abrir = (e: MouseEvent<HTMLAnchorElement>) => {
        e.preventDefault();
        navegar({ tipo: "traslado", id });
    };
    return (
        <li>
            <a
                href={urlDeVista({ tipo: "traslado", id })}
                onClick={abrir}
                aria-label={`Traslado ${id}, ${s.texto}`}
                className="surface-card flex min-h-16 items-center gap-3 rounded-card p-4 transition-colors hover:bg-elevated focus-visible:outline-2 focus-visible:outline-offset-2"
            >
                <PackageOpen aria-hidden className="size-6 shrink-0 text-muted-foreground" />
                <div className="grid min-w-0 flex-1 gap-1">
                    <span className="font-mono text-sm font-semibold">Traslado {id}</span>
                    <span className="text-sm text-muted-foreground">
                        {lineas !== null ? `${lineas} ${lineas === 1 ? "producto" : "productos"}` : "Sin detalle en el teléfono"}
                        {enviadoEn ? ` · enviado ${formatearFecha(enviadoEn)}` : ""}
                    </span>
                    <InsigniaEstado tono={s.tono} icono={s.icono} texto={s.texto} className="w-fit" />
                </div>
                <ChevronRight aria-hidden className="size-5 shrink-0 text-muted-foreground" />
            </a>
        </li>
    );
}

/** Un traslado: lo que llegó, la firma del técnico y «Confirmar recepción». */
export function DetalleTraslado({ id }: { id: number }) {
    const { estado, navegar } = useCampo();
    const { traslados, cargarTraslados } = useInventario();
    useCargarAlAbrir(traslados, cargarTraslados);
    useReleerAlEnviar(cargarTraslados);
    const traslado = traslados.datos?.find((t) => t.id === id) ?? null;
    const local = estado.recepciones.find((op) => op.datos.trasladoId === id) ?? null;
    const volver = (
        <Boton variante="fantasma" tamano="lg" className="-ml-2 w-fit" onClick={() => navegar({ tipo: "recibir" })}>
            <ArrowLeft aria-hidden />
            Por recibir
        </Boton>
    );
    const situacion = traslado ? situacionTraslado(traslado, estado.recepciones) : local ? (local.estado === "rechazada" ? "rechazado_telefono" : "en_telefono") : null;
    const s = situacion ? SITUACIONES[situacion] : null;

    return (
        <article aria-labelledby="titulo-traslado" className="grid gap-4">
            {volver}
            <header className="flex flex-wrap items-center gap-2">
                <h1 id="titulo-traslado" className="font-mono text-xl font-semibold">
                    Traslado {id}
                </h1>
                {s ? <InsigniaEstado tono={s.tono} icono={s.icono} texto={s.texto} /> : null}
            </header>
            {local ? <RecepcionLocal op={local} /> : null}
            {traslados.estado === "error" && !traslado ? (
                <ErrorLectura error={traslados.error} que="el traslado" reintentar={() => void cargarTraslados()} conDatos={false} />
            ) : null}
            {!traslado && !local && traslados.estado === "lista" ? (
                <EstadoVacio icono={PackageCheck} titulo="Ese traslado ya no está por recibir" descripcion="Puede que ya se haya recibido o que bodega lo haya cancelado." />
            ) : null}
            {!traslado && !local && (traslados.estado === "cargando" || traslados.estado === "inactiva") ? <Cargando visible texto="Leyendo el traslado…" /> : null}
            {traslado ? <LineasTraslado traslado={traslado} /> : null}
            {traslado && situacion === "rechazado_argos" ? (
                <Aviso tono="error" titulo="Argos rechazó la recepción anterior">
                    {textoProblema({ type: `urn:iris:problema:${traslado.recepcion?.codigo ?? ""}`, title: traslado.recepcion?.detalle ?? "", status: 422 })} Revisa lo que llegó
                    y vuelve a confirmar.
                </Aviso>
            ) : null}
            {traslado && situacion === "agotado" ? (
                <Aviso tono="warning" titulo="Argos no respondió">
                    Tu recepción quedó guardada; tu supervisor la reintenta desde el panel.
                </Aviso>
            ) : null}
            {traslado && (situacion === "por_recibir" || situacion === "rechazado_argos") ? <FormularioRecibir traslado={traslado} /> : null}
        </article>
    );
}

function LineasTraslado({ traslado }: { traslado: Traslado }) {
    const descripcion = useDescripciones();
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Lo que te mandaron</TarjetaTitulo>
                {traslado.enviadoEn ? <TarjetaDescripcion>Enviado {formatearFecha(traslado.enviadoEn)}</TarjetaDescripcion> : null}
            </TarjetaEncabezado>
            <TarjetaContenido>
                <ul className="grid gap-3 text-sm" aria-label="Productos del traslado">
                    {(traslado.lineas ?? []).map((l, i) => (
                        <li key={`${l.lineaId}-${i}`} className="grid gap-1">
                            <div className="flex items-baseline justify-between gap-2">
                                <span className="min-w-0 break-words font-medium">{descripcion(l.productoId)}</span>
                                <span className="font-semibold tabular-nums">{l.cantidad.toLocaleString("es-GT")}</span>
                            </div>
                            {(l.series ?? []).length > 0 ? <p className="break-all font-mono text-xs text-muted-foreground">{(l.series ?? []).join(" · ")}</p> : null}
                        </li>
                    ))}
                </ul>
            </TarjetaContenido>
        </Tarjeta>
    );
}

/** La recepción guardada en este teléfono: por enviar, esperando su firma o rechazada. */
function RecepcionLocal({ op }: { op: OperacionTrasladoLocal }) {
    const { estado, motor, ejecutar } = useCampo();
    const firma = estado.adjuntos.find((a) => a.id === op.datos.firmaAdjuntoId);
    if (op.estado === "rechazada") {
        const rehacer = sePuedeRehacerRecepcion(op);
        return (
            <Aviso tono="error" icono={TriangleAlert} titulo="El servidor rechazó tu recepción" data-testid="recepcion-rechazada">
                <p>{textoProblema(op.error)}</p>
                <div className="mt-3 flex flex-wrap gap-2">
                    <Boton variante="secundario" tamano="lg" onClick={() => void ejecutar(() => motor.descartar(op.id), "Se descartó la recepción")}>
                        <Trash2 aria-hidden />
                        Descartar
                    </Boton>
                    {rehacer ? (
                        <Boton variante="secundario" tamano="lg" onClick={() => void ejecutar(() => motor.rehacer(op.id), "Se volverá a enviar")}>
                            <Undo2 aria-hidden />
                            Rehacer
                        </Boton>
                    ) : null}
                </div>
            </Aviso>
        );
    }
    if (firma?.estado === "rechazado") {
        return (
            <Aviso tono="error" titulo="La firma no se pudo subir" data-testid="firma-traslado-rechazada">
                <p>{textoProblema(firma.problema, firma.ultimoError ?? "El servidor rechazó la firma.")}</p>
                <div className="mt-3 flex flex-wrap gap-2">
                    <Boton variante="secundario" tamano="lg" onClick={() => void ejecutar(() => motor.descartarAdjunto(firma.id), "Se descartó la recepción")}>
                        <Trash2 aria-hidden />
                        Descartar
                    </Boton>
                    <Boton variante="secundario" tamano="lg" onClick={() => void ejecutar(() => motor.rehacerAdjunto(firma.id), "Se volverá a subir")}>
                        <Undo2 aria-hidden />
                        Volver a subir
                    </Boton>
                </div>
            </Aviso>
        );
    }
    const espera = firma?.esperaHasta !== undefined || op.esperaHasta !== undefined;
    return (
        <Aviso tono="info" icono={CloudUpload} titulo="Recepción guardada en el teléfono" data-testid="recepcion-en-telefono">
            <p>
                {espera
                    ? "El traslado todavía no llega a Iris desde Argos: se reintenta solo."
                    : estado.conexion === "sin_conexion"
                      ? "Se enviará al volver la señal."
                      : "Se está enviando."}
            </p>
            {firma ? (
                <p className="mt-2 flex flex-wrap items-center gap-2">
                    Firma: <EstadoAdjunto adjunto={firma} />
                </p>
            ) : null}
        </Aviso>
    );
}

function FormularioRecibir({ traslado }: { traslado: Traslado }) {
    const { motor, ejecutar } = useCampo();
    const descripcion = useDescripciones();
    const [firma, setFirma] = useState<{ trazos: Trazo[]; ancho: number; alto: number }>({ trazos: [], ancho: 0, alto: 0 });
    const [errorFirma, setErrorFirma] = useState<string | undefined>();
    const form = useForm<FormularioRecepcion>({ resolver: zodResolver(esquemaFormularioRecepcion), defaultValues: formularioRecepcionInicial(traslado) });
    const { fields } = useFieldArray({ control: form.control, name: "lineas" });
    const valores = useWatch({ control: form.control, name: "lineas" });

    const confirmar = form.handleSubmit(async (d) => {
        if (!hayFirma(firma.trazos)) {
            setErrorFirma("Firma en el recuadro para confirmar que recibiste");
            return;
        }
        setErrorFirma(undefined);
        await ejecutar(async () => {
            const png = new Uint8Array(await (await trazosAPng(firma.trazos, firma.ancho, firma.alto)).arrayBuffer());
            await motor.recibirTraslado({ trasladoId: traslado.id, firma: png, lineas: lineasDeRecepcion(traslado, d) });
        }, "Recepción guardada");
    });

    return (
        <form onSubmit={confirmar} noValidate aria-label="Confirmar recepción" className="grid gap-4">
            <Tarjeta>
                <TarjetaEncabezado>
                    <TarjetaTitulo>¿Llegó todo?</TarjetaTitulo>
                    <TarjetaDescripcion>Marca solo lo que faltó: lo que no llegó vuelve a la bodega de origen.</TarjetaDescripcion>
                </TarjetaEncabezado>
                <TarjetaContenido className="grid gap-5">
                    {fields.map((campo, i) => {
                        const linea = (traslado.lineas ?? [])[i];
                        if (!linea) {
                            return null;
                        }
                        const nombre = descripcion(linea.productoId);
                        const falta = valores?.[i]?.falta ?? false;
                        const series = linea.series ?? [];
                        return (
                            <fieldset key={campo.id} className="grid gap-3" data-linea={linea.lineaId}>
                                <legend className="mb-2 flex w-full items-baseline justify-between gap-2 text-sm font-semibold">
                                    <span className="min-w-0 break-words">{nombre}</span>
                                    <span className="tabular-nums">{linea.cantidad.toLocaleString("es-GT")}</span>
                                </legend>
                                {linea.lineaId > 0 ? (
                                    <Controller
                                        control={form.control}
                                        name={`lineas.${i}.falta`}
                                        render={({ field }) => (
                                            <GrupoSegmentado
                                                etiqueta={`¿Llegó completo? ${nombre}`}
                                                className="w-full"
                                                opciones={[
                                                    { valor: "completo", etiqueta: "Llegó completo" },
                                                    { valor: "falta", etiqueta: "Faltó algo" },
                                                ]}
                                                valor={field.value ? "falta" : "completo"}
                                                alCambiar={(v) => field.onChange(v === "falta")}
                                            />
                                        )}
                                    />
                                ) : (
                                    <p className="text-xs text-muted-foreground">Argos no identificó esta línea: se recibe completa.</p>
                                )}
                                {falta && series.length === 0 ? (
                                    <Campo
                                        etiqueta={`Cantidad que llegó de ${nombre}`}
                                        inputMode="decimal"
                                        autoComplete="off"
                                        className="min-h-11"
                                        error={form.formState.errors.lineas?.[i]?.cantidad?.message}
                                        {...form.register(`lineas.${i}.cantidad`)}
                                    />
                                ) : null}
                                {falta && series.length > 0 ? (
                                    <div className="grid gap-1" role="group" aria-label={`Series que llegaron de ${nombre}`}>
                                        {series.map((serie, j) => (
                                            <Casilla key={serie} tamano="lg" etiqueta={<span className="font-mono">{serie}</span>} {...form.register(`lineas.${i}.series.${j}`)} />
                                        ))}
                                    </div>
                                ) : null}
                            </fieldset>
                        );
                    })}
                </TarjetaContenido>
            </Tarjeta>
            <Tarjeta>
                <TarjetaEncabezado>
                    <TarjetaTitulo>Tu firma</TarjetaTitulo>
                    <TarjetaDescripcion>Confirma que recibiste lo que marcaste.</TarjetaDescripcion>
                </TarjetaEncabezado>
                <TarjetaContenido className="grid gap-2">
                    <LienzoFirma etiqueta="Firma del técnico" alCambiar={(t, tam) => setFirma({ trazos: t, ...tam })} />
                    {errorFirma ? <p className="text-sm text-error">{errorFirma}</p> : null}
                </TarjetaContenido>
            </Tarjeta>
            <div className="glass-material sticky bottom-[calc(3.5rem+env(safe-area-inset-bottom))] z-20 -mx-4 border-t border-border px-4 py-3">
                <Boton type="submit" tamano="lg" className="w-full" disabled={form.formState.isSubmitting}>
                    <PackageCheck aria-hidden />
                    Confirmar recepción
                </Boton>
            </div>
        </form>
    );
}

/** «Mi bodega»: existencias, unidades y kardex reciente de Argos (solo lectura). */
export function MiBodega() {
    const { navegar } = useCampo();
    const { bodega, cargarBodega } = useInventario();
    useCargarAlAbrir(bodega, cargarBodega);
    const d = bodega.datos;
    return (
        <section aria-labelledby="titulo-bodega" className="grid gap-4">
            <Boton variante="fantasma" tamano="lg" className="-ml-2 w-fit" onClick={() => navegar({ tipo: "yo" })}>
                <ArrowLeft aria-hidden />
                Yo
            </Boton>
            <div className="flex items-end justify-between gap-3">
                <div>
                    <h1 id="titulo-bodega" className="text-2xl font-semibold tracking-[-0.015em]">
                        Mi bodega
                    </h1>
                    <p className="text-sm text-muted-foreground">{d ? d.bodega.nombre : "Tu inventario en Argos (solo lectura)."}</p>
                </div>
                {bodega.estado === "cargando" ? <Cargando visible texto="Leyendo…" /> : null}
            </div>
            {bodega.estado === "error" ? <ErrorLectura error={bodega.error} que="tu bodega" reintentar={() => void cargarBodega()} conDatos={d !== null} /> : null}
            {!d && bodega.estado !== "error" ? <Cargando visible texto="Leyendo tu bodega…" /> : null}
            {d ? <ContenidoBodega bodega={d} /> : null}
            {d && bodega.leidoEn !== null ? <p className="text-xs text-muted-foreground">Leído de Argos {formatearFecha(d.leidoEn)}</p> : null}
        </section>
    );
}

const mayuscula = (texto: string) => texto.charAt(0).toUpperCase() + texto.slice(1);

const MODALIDADES: Record<string, string> = {
    comodato: "Comodato",
    consumible_cobro: "Se cobra al cliente",
    consumible_gasto: "Gasto",
};

function ContenidoBodega({ bodega }: { bodega: DatosBodega }) {
    const { navegar } = useCampo();
    const existencias = bodega.existencias ?? [];
    const unidades = bodega.unidades ?? [];
    const kardex = bodega.kardex ?? [];
    const porRecibir = bodega.trasladosPendientes ?? [];
    return (
        <>
            {porRecibir.length > 0 ? (
                <Aviso
                    tono="info"
                    icono={PackageOpen}
                    titulo={`Tienes ${porRecibir.length} ${porRecibir.length === 1 ? "traslado" : "traslados"} por recibir`}
                    accion={
                        <Boton variante="secundario" tamano="lg" onClick={() => navegar({ tipo: "recibir" })}>
                            Ver
                        </Boton>
                    }
                />
            ) : null}
            <Tarjeta>
                <TarjetaEncabezado>
                    <TarjetaTitulo>Existencias</TarjetaTitulo>
                </TarjetaEncabezado>
                <TarjetaContenido>
                    {existencias.length === 0 ? (
                        <p className="text-sm text-muted-foreground">Tu bodega está vacía.</p>
                    ) : (
                        <ul className="grid gap-2 text-sm" aria-label="Existencias">
                            {existencias.map((e) => (
                                <li key={e.productoId} className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
                                    <span className="min-w-0 break-words">
                                        {e.descripcion}
                                        {MODALIDADES[e.modalidad] ? <span className="block text-xs text-muted-foreground">{MODALIDADES[e.modalidad]}</span> : null}
                                    </span>
                                    <span className="font-semibold tabular-nums">
                                        {e.cantidad.toLocaleString("es-GT")} {e.unidad}
                                    </span>
                                </li>
                            ))}
                        </ul>
                    )}
                </TarjetaContenido>
            </Tarjeta>
            {unidades.length > 0 ? (
                <Tarjeta>
                    <TarjetaEncabezado>
                        <TarjetaTitulo>Equipos con serie ({unidades.length})</TarjetaTitulo>
                    </TarjetaEncabezado>
                    <TarjetaContenido>
                        <ul className="grid gap-2 text-sm" aria-label="Equipos con serie">
                            {unidades.map((u) => (
                                <li key={u.id} className="grid gap-0.5">
                                    <span className="font-medium">{u.descripcion}</span>
                                    <span className="font-mono text-xs break-words text-muted-foreground">
                                        {u.serie}
                                        {u.mac ? ` · ${u.mac}` : ""}
                                        {u.estado ? ` · ${u.estado.replaceAll("_", " ")}` : ""}
                                    </span>
                                </li>
                            ))}
                        </ul>
                    </TarjetaContenido>
                </Tarjeta>
            ) : null}
            <Tarjeta>
                <TarjetaEncabezado>
                    <TarjetaTitulo>Movimientos recientes</TarjetaTitulo>
                </TarjetaEncabezado>
                <TarjetaContenido>
                    {kardex.length === 0 ? (
                        <p className="text-sm text-muted-foreground">Sin movimientos recientes.</p>
                    ) : (
                        <ol className="grid gap-3 text-sm" aria-label="Movimientos recientes">
                            {kardex.map((m) => (
                                <li key={m.id} className="grid gap-0.5">
                                    <div className="flex items-baseline justify-between gap-2">
                                        <span className="min-w-0 break-words font-medium">{m.descripcion}</span>
                                        <span className="font-semibold tabular-nums">{m.cantidad.toLocaleString("es-GT")}</span>
                                    </div>
                                    <span className="text-xs text-muted-foreground">
                                        {mayuscula(m.tipo.replaceAll("_", " "))}
                                        {m.referencia ? ` · ${m.referencia}` : ""}
                                        {m.ocurridoEn ? ` · ${formatearFecha(m.ocurridoEn)}` : ""}
                                    </span>
                                </li>
                            ))}
                        </ol>
                    )}
                </TarjetaContenido>
            </Tarjeta>
        </>
    );
}
