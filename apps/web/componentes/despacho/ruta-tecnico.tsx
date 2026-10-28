"use client";

import { Aviso, Boton, CampoSelector, Esqueleto, GrupoSegmentado, InsigniaEstado, cn } from "@iris/ui";
import { ArrowDown, ArrowUp, CircleAlert, EyeOff, Lock, Pin, RotateCw, Route, Send, Sparkles } from "lucide-react";
import Link from "next/link";
import { useEffect, useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import type { CitaAgenda, FilaTecnico } from "@/lib/despacho/calendario";
import { horaDe, type Dia } from "@/lib/despacho/fechas";
import {
    MOTIVOS_MOTOR,
    MOTIVOS_SIN_RECORRIDO,
    coordenadasDeGeometria,
    coordenadasReales,
    llegadasDeCitas,
    ordenTrasMover,
    presentacionDesvio,
    rutasDelDia,
    sePuedeReordenar,
    type ParadaRuta,
    type Ruta,
    type RutaDetalle,
} from "@/lib/despacho/rutas";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { ESTADOS_ORDEN, TIPOS_ORDEN } from "@/lib/ordenes/presentacion";
import { useSesion } from "@/lib/sesion";
import type { LineaRuta } from "./mapa";

type Props = {
    filas: readonly FilaTecnico[];
    dia: Dia;
    citas: readonly CitaAgenda[];
    tecnicoId: number | null;
    alElegir: (id: number | null) => void;
    /** Líneas para el mapa: la propuesta (`geometria`) y el recorrido real; vacías si no hay. */
    alLineas: (lineas: LineaRuta[]) => void;
};

/**
 * Ruta del día del técnico elegido: la propuesta frente a la real (API de rutas). Proponer,
 * reordenar y publicar solo con `rutas.proponer` y `rutas.publicar`.
 */
export function RutaDelTecnico({ filas, dia, citas, tecnicoId, alElegir, alLineas }: Props) {
    const { puede } = useSesion();
    return (
        <div className="grid gap-3 [&>*]:min-w-0">
            <CampoSelector etiqueta="Técnico de la ruta" className="sm:max-w-xs" value={tecnicoId ?? ""} onChange={(e) => alElegir(e.target.value === "" ? null : Number(e.target.value))}>
                <option value="">Elige un técnico</option>
                {filas.map((f) => (
                    <option key={f.usuarioId} value={f.usuarioId}>
                        {f.nombre}
                    </option>
                ))}
            </CampoSelector>
            {tecnicoId === null ? null : !puede(["rutas.ver"]) ? (
                <Aviso tono="neutral" icono={Lock} titulo="Sin permiso para ver rutas">
                    Hace falta el permiso «rutas.ver».
                </Aviso>
            ) : (
                <RutasDelDia
                    key={`${tecnicoId}|${dia}`}
                    tecnicoId={tecnicoId}
                    nombre={filas.find((f) => f.usuarioId === tecnicoId)?.nombre ?? `Técnico ${tecnicoId}`}
                    dia={dia}
                    citas={citas}
                    alLineas={alLineas}
                />
            )}
        </div>
    );
}

type Vista = "borrador" | "publicada";

function RutasDelDia({ tecnicoId, nombre, dia, citas, alLineas }: { tecnicoId: number; nombre: string; dia: Dia; citas: readonly CitaAgenda[]; alLineas: (l: LineaRuta[]) => void }) {
    const { puede } = useSesion();
    const lista = useLectura<Ruta[]>(async (signal) => (await resolver(api().GET("/api/v1/rutas", { params: { query: { fecha: dia, tecnicoId } }, signal }))).data ?? []);
    const [vista, setVista] = useState<Vista | null>(null);
    const [error, setError] = useState<unknown>(null);
    const [enviando, setEnviando] = useState(false);
    const [hecho, setHecho] = useState<string | null>(null);
    const clave = useClaveIdempotencia();
    const listo = lista.estado === "listo" ? lista.datos : null;
    const { borrador, publicada } = rutasDelDia(listo ?? []);
    const elegida = vista === "publicada" ? (publicada ?? borrador) : vista === "borrador" ? (borrador ?? publicada) : (borrador ?? publicada);
    const sinRuta = listo !== null && elegida === null;

    useEffect(() => {
        if (sinRuta || lista.estado === "error") alLineas([]);
    }, [sinRuta, lista.estado, alLineas]);
    useEffect(() => () => alLineas([]), [alLineas]);

    async function ejecutar(accion: "proponer" | "publicar", cuerpo: { version?: number; ordenIds?: number[] }, rutaId?: number) {
        setError(null);
        setHecho(null);
        setEnviando(true);
        try {
            const params = { header: { "Idempotency-Key": clave.actual() } };
            const r: RutaDetalle =
                accion === "publicar" && rutaId !== undefined
                    ? await resolver(api().POST("/api/v1/rutas/{id}/publicar", { params: { ...params, path: { id: rutaId } }, body: { version: cuerpo.version ?? 0 } }))
                    : await resolver(
                          api().POST("/api/v1/rutas/proponer", {
                              params,
                              body: { tecnicoId, fecha: dia, ...(cuerpo.version !== undefined ? { version: cuerpo.version } : {}), ...(cuerpo.ordenIds ? { ordenIds: cuerpo.ordenIds } : {}) },
                          }),
                      );
            clave.renovar();
            setVista(r.estado === "publicada" ? "publicada" : "borrador");
            setHecho(accion === "publicar" ? `Ruta de ${nombre} publicada: el técnico ya la ve.` : cuerpo.ordenIds ? "Orden de las paradas actualizado." : `Ruta de ${nombre} propuesta.`);
            await lista.refrescar();
        } catch (e) {
            clave.trasError(e);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    if (lista.estado === "error") return <ErrorLectura error={lista.error} reintentar={lista.reintentar} que="las rutas del técnico" />;
    if (lista.estado === "cargando") return <Esqueleto className="h-24" />;

    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";
    const puedeProponer = puede(["rutas.proponer"]);
    const avisoError = conflicto ? (
        <Aviso
            tono="warning"
            titulo="La ruta cambió mientras la veías"
            accion={
                <Boton
                    variante="secundario"
                    tamano="sm"
                    onClick={async () => {
                        setError(null);
                        await lista.refrescar();
                    }}
                >
                    <RotateCw />
                    Recargar
                </Boton>
            }
        >
            Otra persona la volvió a proponer o la publicó. Recarga y vuelve a intentarlo.
        </Aviso>
    ) : error ? (
        <Aviso tono="error" titulo="No se pudo completar la acción sobre la ruta">
            {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
        </Aviso>
    ) : null;

    return (
        <div className="grid gap-3 [&>*]:min-w-0" data-testid="ruta-tecnico">
            {hecho ? (
                <Aviso tono="success" titulo="Listo" data-testid="ruta-hecha">
                    {hecho}
                </Aviso>
            ) : null}
            {avisoError}
            {borrador && publicada ? (
                <GrupoSegmentado
                    etiqueta="Ruta que se muestra"
                    valor={elegida?.estado === "publicada" ? "publicada" : "borrador"}
                    alCambiar={setVista}
                    opciones={[
                        { valor: "borrador", etiqueta: "Borrador" },
                        { valor: "publicada", etiqueta: "Publicada" },
                    ]}
                />
            ) : null}
            {elegida ? (
                <DetalleRuta key={`${elegida.id}:${elegida.version}`} ruta={elegida} enviando={enviando} ejecutar={ejecutar} alLineas={alLineas} />
            ) : (
                <SinRuta nombre={nombre} citas={citas} tecnicoId={tecnicoId}>
                    {puedeProponer ? (
                        <Boton onClick={() => ejecutar("proponer", {})} disabled={enviando}>
                            <Sparkles />
                            {enviando ? "Proponiendo…" : "Proponer ruta"}
                        </Boton>
                    ) : null}
                </SinRuta>
            )}
        </div>
    );
}

function SinRuta({ nombre, citas, tecnicoId, children }: { nombre: string; citas: readonly CitaAgenda[]; tecnicoId: number; children: React.ReactNode }) {
    const llegadas = llegadasDeCitas(citas, tecnicoId);
    return (
        <div className="grid gap-2" data-testid="sin-ruta">
            <p className="text-sm text-muted-foreground">{nombre} no tiene ruta para este día.</p>
            {llegadas.length > 0 ? (
                <ol className="grid gap-1 text-sm" aria-label="Llegadas reales">
                    {llegadas.map((c, i) => (
                        <li key={c.ordenId} data-llegada={c.numero} className="flex items-center gap-2">
                            <span className="text-xs text-muted-foreground tabular-nums">{i + 1}.</span>
                            <span className="font-medium">{c.numero}</span>
                            <span className="text-xs text-muted-foreground tabular-nums">llegó {horaDe(c.enSitioEn)}</span>
                        </li>
                    ))}
                </ol>
            ) : null}
            <div>{children}</div>
        </div>
    );
}

const ESTADOS_RUTA: Record<Ruta["estado"], string> = { borrador: "Borrador", publicada: "Publicada", reemplazada: "Reemplazada" };

function DetalleRuta({
    ruta,
    enviando,
    ejecutar,
    alLineas,
}: {
    ruta: Ruta;
    enviando: boolean;
    ejecutar: (accion: "proponer" | "publicar", cuerpo: { version?: number; ordenIds?: number[] }, rutaId?: number) => Promise<void>;
    alLineas: (l: LineaRuta[]) => void;
}) {
    const { puede } = useSesion();
    const detalle = useLectura<RutaDetalle>((signal) => resolver(api().GET("/api/v1/rutas/{id}", { params: { path: { id: ruta.id } }, signal })));
    const d = detalle.estado === "listo" ? detalle.datos : null;

    useEffect(() => {
        if (!d) {
            alLineas([]);
            return;
        }
        alLineas([
            { id: "propuesta", coordenadas: coordenadasDeGeometria(d.geometria) ?? [] },
            { id: "real", coordenadas: coordenadasReales(d.real) ?? [] },
        ]);
    }, [d, alLineas]);

    if (detalle.estado === "error") return <ErrorLectura error={detalle.error} reintentar={detalle.reintentar} que="la ruta" />;
    if (!d) return <Esqueleto className="h-32" />;

    const paradas = d.paradas ?? [];
    const reordenable = sePuedeReordenar(d) && puede(["rutas.proponer"]);
    const puedePublicar = d.estado === "borrador" && puede(["rutas.publicar"]);
    const puedeProponer = puede(["rutas.proponer"]) && d.estado !== "reemplazada";

    return (
        <div className="grid gap-3 [&>*]:min-w-0" data-ruta={d.id} data-estado-ruta={d.estado}>
            <div className="flex flex-wrap items-center gap-2 text-sm">
                <InsigniaEstado tono={d.estado === "publicada" ? "success" : "neutral"} icono={d.estado === "publicada" ? Send : Route} texto={ESTADOS_RUTA[d.estado]} />
                <span className="text-muted-foreground tabular-nums">
                    {d.totalParadas} {d.totalParadas === 1 ? "parada" : "paradas"} · sale {horaDe(d.saleEn)}
                    {d.terminaEn ? ` · termina ${horaDe(d.terminaEn)}` : ""} · {(d.distanciaM / 1000).toFixed(1)} km · {d.modo === "manual" ? "orden manual" : "orden optimizado"}
                </span>
                <span className="ml-auto flex flex-wrap gap-2">
                    {puedeProponer ? (
                        <Boton variante="secundario" tamano="sm" disabled={enviando} onClick={() => ejecutar("proponer", d.estado === "borrador" ? { version: d.version } : {})}>
                            <Sparkles />
                            Volver a proponer
                        </Boton>
                    ) : null}
                    {puedePublicar ? (
                        <Boton tamano="sm" disabled={enviando || d.desactualizada} onClick={() => ejecutar("publicar", { version: d.version }, d.id)}>
                            <Send />
                            Publicar
                        </Boton>
                    ) : null}
                </span>
            </div>
            {d.desactualizada ? (
                <Aviso tono="warning" titulo="La ruta quedó desactualizada" data-testid="ruta-desactualizada">
                    {d.motivoDesactualizada ?? "Una orden cambió de técnico o de hora después de proponerla."}{" "}
                    {d.estado === "borrador" ? "Vuelve a proponerla para poder publicarla." : "El técnico sigue con esta ruta hasta que publiques otra."}
                </Aviso>
            ) : null}
            {d.degradada ? (
                <Aviso tono="warning" icono={CircleAlert} titulo="Tiempos aproximados" data-testid="ruta-degradada">
                    Se calculó sin el servicio de rutas{d.motivoMotor ? ` (${MOTIVOS_MOTOR[d.motivoMotor]})` : ""}: distancias en línea recta, el trazo no sigue las calles.
                </Aviso>
            ) : null}
            {d.excedeTurno ? (
                <Aviso tono="warning" titulo="La ruta termina después del turno">
                    {d.finTurno ? `El turno termina a las ${horaDe(d.finTurno)}.` : null}
                </Aviso>
            ) : null}
            {paradas.some((p) => p.oculta) ? (
                <p className="text-xs text-muted-foreground">Hay paradas fuera de tu alcance: se muestra solo su lugar en la secuencia y el mapa no dibuja el trazo.</p>
            ) : null}
            <ol className="grid gap-1.5 [&>*]:min-w-0" aria-label="Paradas" data-testid="paradas">
                {paradas.map((p, i) => (
                    <FilaParada
                        key={`${p.posicion}:${p.ordenId}`}
                        p={p}
                        subir={reordenable ? ordenTrasMover(paradas, i, -1) : null}
                        bajar={reordenable ? ordenTrasMover(paradas, i, 1) : null}
                        enviando={enviando}
                        mover={(ordenIds) => ejecutar("proponer", { version: d.version, ordenIds })}
                    />
                ))}
            </ol>
            {d.real.disponible ? (
                <p className="text-xs text-muted-foreground" data-testid="recorrido-real">
                    Recorrido real: {d.real.totalPuntos} posiciones, {(d.real.distanciaM / 1000).toFixed(1)} km
                    {d.real.primeraEn && d.real.ultimaEn ? ` (${horaDe(d.real.primeraEn)}–${horaDe(d.real.ultimaEn)})` : ""}.
                </p>
            ) : (
                <p className="text-xs text-muted-foreground" data-testid="recorrido-real">
                    {d.real.motivo ? MOTIVOS_SIN_RECORRIDO[d.real.motivo] : "El recorrido real no está disponible."}
                </p>
            )}
        </div>
    );
}

function FilaParada({ p, subir, bajar, enviando, mover }: { p: ParadaRuta; subir: number[] | null; bajar: number[] | null; enviando: boolean; mover: (ordenIds: number[]) => void }) {
    if (p.oculta) {
        return (
            <li data-parada="oculta" className="flex items-center gap-2 rounded-md bg-elevated px-2.5 py-2 text-sm text-muted-foreground">
                <Posicion n={p.posicion} />
                <EyeOff aria-hidden className="size-4" />
                Parada fuera de tu alcance
            </li>
        );
    }
    const desvio = presentacionDesvio(p.desvioSeg);
    const estado = p.estado === "" ? null : ESTADOS_ORDEN[p.estado];
    return (
        <li data-parada={p.numero} className={cn("grid gap-1 rounded-md bg-elevated px-2.5 py-2 text-sm", !p.vigente && "opacity-70")}>
            <div className="flex flex-wrap items-center gap-2">
                <Posicion n={p.posicion} />
                <Link href={`/ordenes/${p.ordenId}`} className="font-medium text-primary underline-offset-4 hover:underline">
                    {p.numero}
                </Link>
                {p.tipo !== "" ? <span className="text-xs text-muted-foreground">{TIPOS_ORDEN[p.tipo].texto}</span> : null}
                {estado ? <InsigniaEstado tono={estado.tono} icono={estado.icono} texto={estado.texto} /> : null}
                {p.fija ? <InsigniaEstado tono="neutral" icono={Pin} texto="Fija" title="Ya en curso o terminada al proponer: va primero, en el orden real" /> : null}
                {!p.vigente ? <InsigniaEstado tono="warning" texto="Ya no está en esta ruta" /> : null}
                {p.fueraDeVentana ? <InsigniaEstado tono="warning" texto="Fuera de la ventana" /> : null}
                {subir || bajar ? (
                    <span className="ml-auto flex gap-1">
                        <Boton variante="fantasma" tamano="icono-sm" aria-label={`Subir ${p.numero}`} title="Subir" disabled={!subir || enviando} onClick={() => subir && mover(subir)}>
                            <ArrowUp />
                        </Boton>
                        <Boton variante="fantasma" tamano="icono-sm" aria-label={`Bajar ${p.numero}`} title="Bajar" disabled={!bajar || enviando} onClick={() => bajar && mover(bajar)}>
                            <ArrowDown />
                        </Boton>
                    </span>
                ) : null}
            </div>
            <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground tabular-nums">
                <span>Estimada: {p.llegadaEstimada ? horaDe(p.llegadaEstimada) : "sin hora"}</span>
                {p.ventanaInicio && p.ventanaFin ? (
                    <span>
                        Ventana {horaDe(p.ventanaInicio)}–{horaDe(p.ventanaFin)}
                    </span>
                ) : null}
                <span>Real: {p.llegadaReal ? horaDe(p.llegadaReal) : "aún no llega"}</span>
                {desvio ? <InsigniaEstado tono={desvio.tono} icono={desvio.icono} texto={desvio.texto} data-desvio={p.desvioSeg} /> : null}
                {p.sinCoordenadas ? <span>sin coordenadas</span> : null}
            </div>
        </li>
    );
}

function Posicion({ n }: { n: number }) {
    return (
        <span className="grid size-6 shrink-0 place-items-center rounded-full bg-background text-xs font-semibold tabular-nums" aria-label={`Parada ${n}`}>
            {n}
        </span>
    );
}
