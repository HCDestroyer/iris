"use client";

import {
    Aviso,
    Boton,
    DialogoConfirmar,
    Esqueleto,
    EstadoVacio,
    InsigniaEstado,
    Tarjeta,
    TarjetaContenido,
    TarjetaEncabezado,
    TarjetaTitulo,
} from "@iris/ui";
import { ArrowLeft, Ban, CircleCheck, Eye, History, RotateCw, SearchX, SquarePen } from "lucide-react";
import Link from "next/link";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { EditorPlantilla } from "@/componentes/plantillas/editor";
import { InsigniasPlantilla, textoTipoPlantilla } from "@/componentes/plantillas/insignias";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { formatearFecha } from "@/lib/fechas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";
import { cuerpoEditarPlantilla, datosDePlantilla, TIPOS_ITEM, type DatosCrearPlantilla, type PlantillaDetalle } from "@/lib/plantillas/esquemas";
import { puedeModificar } from "@/lib/plantillas/permisos";
import { useSesion } from "@/lib/sesion";

function Volver() {
    return (
        <Link href="/administracion/plantillas" className="mb-3 inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft aria-hidden className="size-4" />
            Plantillas
        </Link>
    );
}

export function DetallePlantilla() {
    const params = useParams<{ id: string }>();
    const busqueda = useSearchParams();
    const id = Number(params.id);
    const numero = Number(busqueda.get("version"));
    const version = Number.isSafeInteger(numero) && numero > 0 ? numero : null;
    if (!Number.isSafeInteger(id) || id < 1) return <NoEncontrada />;
    return <Detalle key={`${id}|${version ?? ""}`} id={id} numeroVersion={version} />;
}

function NoEncontrada() {
    return (
        <>
            <Volver />
            <Tarjeta>
                <EstadoVacio icono={SearchX} titulo="No encontramos esta plantilla" descripcion="Puede que no exista o que sea de otra empresa." />
            </Tarjeta>
        </>
    );
}

/**
 * Una plantilla: su versión actual (o una anterior en solo lectura, `?version=N`), el historial
 * de versiones con cuántos formularios usa cada una, activar o desactivar y editar.
 */
function Detalle({ id, numeroVersion }: { id: number; numeroVersion: number | null }) {
    const { yo, puede } = useSesion();
    const router = useRouter();
    const lectura = useLectura<PlantillaDetalle>((signal) =>
        resolver(api().GET("/api/v1/plantillas/{id}", { params: { path: { id }, query: numeroVersion !== null ? { numeroVersion } : {} }, signal })),
    );
    const [editando, setEditando] = useState(false);
    const [guardada, setGuardada] = useState<number | null>(null);
    const [errorEditar, setErrorEditar] = useState<unknown>(null);
    const [errorEstado, setErrorEstado] = useState<unknown>(null);
    const [cambiando, setCambiando] = useState(false);
    const [confirmar, setConfirmar] = useState(false);
    const claveEditar = useClaveIdempotencia();
    const claveEstado = useClaveIdempotencia();

    if (lectura.estado === "cargando") {
        return (
            <>
                <Volver />
                <Esqueleto className="mb-4 h-10 w-72" />
                <Esqueleto className="h-64" />
            </>
        );
    }
    if (lectura.estado === "error") {
        if (lectura.error instanceof ErrorApi && lectura.error.estado === 404) return <NoEncontrada />;
        return (
            <>
                <Volver />
                <ErrorLectura error={lectura.error} que="la plantilla" reintentar={lectura.reintentar} />
            </>
        );
    }
    const p = lectura.datos;
    const vieja = p.numeroVersion !== p.versionActual;
    const versiones = p.versiones ?? [];
    const actual = versiones.find((v) => v.numero === p.versionActual);
    const puedeEditar = !vieja && puedeModificar(p, "plantillas.editar", yo, puede);
    const accionEstado = p.activa ? "desactivar" : "activar";
    // La por defecto de un tipo no se desactiva (422 plantilla_por_defecto): no se ofrece.
    const porDefectoActiva = p.activa && p.porDefecto;
    const puedeEstado = !vieja && !porDefectoActiva && puedeModificar(p, p.activa ? "plantillas.desactivar" : "plantillas.activar", yo, puede);
    const comunBloqueada = p.empresaId === null && !puedeModificar(p, "plantillas.editar", yo, puede) && puede(["plantillas.editar"]);
    const conflicto = (e: unknown) => e instanceof ErrorApi && e.estado === 409 && !e.codigo.startsWith("idempotencia");

    async function cambiarEstado() {
        setCambiando(true);
        setErrorEstado(null);
        try {
            const ruta = accionEstado === "activar" ? "/api/v1/plantillas/{id}/activar" : "/api/v1/plantillas/{id}/desactivar";
            await resolver(api().POST(ruta, { params: { path: { id }, header: { "Idempotency-Key": claveEstado.actual() } }, body: { version: p.version } }));
            claveEstado.renovar();
            await lectura.refrescar();
        } catch (e) {
            claveEstado.trasError(e);
            setErrorEstado(e);
        } finally {
            setCambiando(false);
        }
    }

    const avisoConflicto = (e: unknown, limpiar: () => void) => (
        <Aviso
            tono="warning"
            titulo="Alguien más cambió esta plantilla mientras la veías"
            accion={
                <Boton
                    type="button"
                    variante="secundario"
                    tamano="sm"
                    onClick={async () => {
                        await lectura.refrescar();
                        limpiar();
                    }}
                >
                    <RotateCw />
                    Recargar
                </Boton>
            }
        >
            {mensajeDeError(e).detalle ?? "Recarga para ver la versión actual."}
        </Aviso>
    );

    return (
        <>
            <Volver />
            <header className="mb-4 grid gap-2">
                <span className="font-mono text-sm text-muted-foreground">{p.codigo}</span>
                <h1 className="text-2xl font-semibold tracking-[-0.015em] break-words">{p.nombre}</h1>
                <InsigniasPlantilla p={p} />
                {p.descripcion ? <p className="text-sm text-muted-foreground">{p.descripcion}</p> : null}
                {!editando && !vieja ? (
                    <div className="flex flex-wrap gap-2" role="group" aria-label="Acciones de la plantilla">
                        {puedeEditar ? (
                            <Boton
                                tamano="sm"
                                onClick={() => {
                                    setGuardada(null);
                                    setEditando(true);
                                }}
                            >
                                <SquarePen />
                                Editar
                            </Boton>
                        ) : null}
                        {puedeEstado ? (
                            <Boton
                                variante={p.activa ? "peligro" : "secundario"}
                                tamano="sm"
                                onClick={p.activa ? () => setConfirmar(true) : cambiarEstado}
                                disabled={cambiando || conflicto(errorEstado)}
                            >
                                {p.activa ? <Ban /> : <CircleCheck />}
                                {cambiando ? "Guardando…" : p.activa ? "Desactivar" : "Activar"}
                            </Boton>
                        ) : null}
                    </div>
                ) : null}
                <DialogoConfirmar
                    open={confirmar}
                    onOpenChange={setConfirmar}
                    peligro
                    titulo={`¿Desactivar «${p.nombre}»?`}
                    descripcion="No se podrá agregar a órdenes nuevas. Las órdenes que ya la tienen la conservan."
                    textoConfirmar="Desactivar"
                    alConfirmar={async () => {
                        setConfirmar(false);
                        await cambiarEstado();
                    }}
                />
                {porDefectoActiva && !vieja ? <p className="text-sm text-muted-foreground">Es la plantilla por defecto del tipo: no se puede desactivar.</p> : null}
                {comunBloqueada ? <p className="text-sm text-muted-foreground">Es común a todas las empresas: solo la modifica quien opera en todas.</p> : null}
                {errorEstado ? (
                    conflicto(errorEstado) ? (
                        avisoConflicto(errorEstado, () => setErrorEstado(null))
                    ) : (
                        <Aviso tono="error" titulo={`No se pudo ${accionEstado}`}>
                            {mensajeDeError(errorEstado).detalle ?? mensajeDeError(errorEstado).titulo}
                        </Aviso>
                    )
                ) : null}
                {guardada !== null ? (
                    <Aviso tono="success" titulo={`Guardada como versión ${guardada}`}>
                        {guardada > 1 ? `Las órdenes que ya usaban la versión ${guardada - 1} la conservan; las nuevas usan la ${guardada}.` : "Sigue siendo la versión 1."}
                    </Aviso>
                ) : null}
            </header>

            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_20rem] lg:items-start">
                <div className="min-w-0">
                    {editando ? (
                        <Tarjeta>
                            <TarjetaEncabezado>
                                <TarjetaTitulo>Editar plantilla</TarjetaTitulo>
                            </TarjetaEncabezado>
                            <TarjetaContenido>
                                <EditorPlantilla
                                    modo="editar"
                                    valores={{ ...datosDePlantilla(p), codigo: p.codigo, comun: false, borrador: false }}
                                    puedeComun={false}
                                    aviso={
                                        p.borrador
                                            ? "Es un borrador: se reescribe la versión actual (no crea versión nueva)."
                                            : `Se crea la versión ${p.versionActual + 1}.${actual && actual.formularios > 0 ? ` Las ${actual.formularios} órdenes que ya la usan conservan la versión ${p.versionActual}.` : ""}`
                                    }
                                    error={
                                        errorEditar ? (
                                            conflicto(errorEditar) ? (
                                                avisoConflicto(errorEditar, () => setErrorEditar(null))
                                            ) : (
                                                <Aviso tono="error" titulo="No se pudo guardar">
                                                    {mensajeDeError(errorEditar).detalle ?? mensajeDeError(errorEditar).titulo}
                                                </Aviso>
                                            )
                                        ) : null
                                    }
                                    alCancelar={() => {
                                        setErrorEditar(null);
                                        setEditando(false);
                                    }}
                                    alGuardar={async (d: DatosCrearPlantilla, setErrorCampo) => {
                                        setErrorEditar(null);
                                        try {
                                            const nueva = await resolver(
                                                api().POST("/api/v1/plantillas/{id}/editar", {
                                                    params: { path: { id }, header: { "Idempotency-Key": claveEditar.actual() } },
                                                    body: cuerpoEditarPlantilla(d, p.version),
                                                }),
                                            );
                                            claveEditar.renovar();
                                            setGuardada(nueva.numeroVersion);
                                            setEditando(false);
                                            await lectura.refrescar();
                                        } catch (e) {
                                            claveEditar.trasError(e);
                                            const nombres = ["nombre", "descripcion", "tipo", ...d.items.flatMap((_, i) => [`items.${i}.codigo`, `items.${i}.etiqueta`, `items.${i}.opciones`, `items.${i}.unidad`] as const)] as const;
                                            if (!erroresACampos(e, nombres, setErrorCampo)) setErrorEditar(e);
                                        }
                                    }}
                                />
                            </TarjetaContenido>
                        </Tarjeta>
                    ) : (
                        <Tarjeta data-testid="items-plantilla">
                            <TarjetaEncabezado className="flex flex-row flex-wrap items-center justify-between gap-2">
                                <TarjetaTitulo>
                                    Versión {p.numeroVersion}
                                    {vieja ? " (anterior, solo lectura)" : ""}
                                </TarjetaTitulo>
                                <span className="text-sm text-muted-foreground">
                                    {textoTipoPlantilla(p.tipo)}
                                    {p.requiereFirma ? " · pide firma" : ""}
                                </span>
                            </TarjetaEncabezado>
                            <TarjetaContenido className="grid gap-3">
                                {vieja ? (
                                    <Aviso tono="info" titulo={`Estás viendo la versión ${p.numeroVersion}; la actual es la ${p.versionActual}`}>
                                        <Link className="text-primary underline-offset-4 hover:underline" href={`/administracion/plantillas/${id}`}>
                                            Ver la versión actual
                                        </Link>
                                    </Aviso>
                                ) : null}
                                <ol className="grid gap-2" aria-label="Ítems">
                                    {[...(p.items ?? [])]
                                        .sort((a, b) => a.posicion - b.posicion)
                                        .map((i) => (
                                            <li key={i.id} className="grid gap-1 border-b pb-2 text-sm last:border-0 last:pb-0" data-item={i.codigo}>
                                                <span className="flex flex-wrap items-center justify-between gap-2">
                                                    <span>
                                                        <span className="text-muted-foreground tabular-nums">{i.posicion}. </span>
                                                        {i.etiqueta}
                                                        {i.obligatorio ? <span className="text-muted-foreground"> · obligatorio</span> : null}
                                                    </span>
                                                    <span className="text-xs text-muted-foreground">
                                                        {TIPOS_ITEM[i.tipo]}
                                                        {i.unidad ? ` (${i.unidad})` : ""}
                                                    </span>
                                                </span>
                                                {i.opciones && i.opciones.length > 0 ? <span className="text-xs text-muted-foreground">Opciones: {i.opciones.join(" · ")}</span> : null}
                                                <span className="font-mono text-xs text-muted-foreground">{i.codigo}</span>
                                            </li>
                                        ))}
                                </ol>
                            </TarjetaContenido>
                        </Tarjeta>
                    )}
                </div>
                <Tarjeta data-testid="versiones">
                    <TarjetaEncabezado>
                        <TarjetaTitulo>Versiones</TarjetaTitulo>
                    </TarjetaEncabezado>
                    <TarjetaContenido>
                        <ol className="grid gap-2 text-sm" aria-label="Versiones">
                            {versiones.map((v) => (
                                <li key={v.id} className="flex flex-wrap items-center justify-between gap-2 rounded-md border p-2" data-version={v.numero}>
                                    <span className="grid">
                                        <span className="font-medium">
                                            Versión {v.numero}
                                            {v.numero === p.versionActual ? <span className="font-normal text-muted-foreground"> · actual</span> : null}
                                        </span>
                                        <span className="text-xs text-muted-foreground tabular-nums">
                                            {formatearFecha(v.creadaEn)} · {v.formularios === 1 ? "1 formulario" : `${v.formularios} formularios`}
                                        </span>
                                    </span>
                                    {v.numero === p.numeroVersion ? (
                                        <InsigniaEstado tono="info" icono={Eye} texto="Viendo" />
                                    ) : (
                                        <Boton
                                            variante="fantasma"
                                            tamano="sm"
                                            onClick={() => {
                                                setEditando(false);
                                                router.push(v.numero === p.versionActual ? `/administracion/plantillas/${id}` : `/administracion/plantillas/${id}?version=${v.numero}`);
                                            }}
                                        >
                                            <History />
                                            Ver
                                        </Boton>
                                    )}
                                </li>
                            ))}
                        </ol>
                    </TarjetaContenido>
                </Tarjeta>
            </div>
        </>
    );
}
