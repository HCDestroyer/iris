"use client";

import type { Schema } from "@iris/contratos";
import { Aviso, Boton, InsigniaEstado } from "@iris/ui";
import { FileDown, LoaderCircle, RotateCw } from "lucide-react";
import { useCallback, useRef, useState, useSyncExternalStore } from "react";
import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import {
    COTIZACION_VENCIDA,
    ESTADOS_CONTRATO,
    ESTADOS_COTIZACION,
    ESTADOS_PROSPECTO,
    RESULTADOS_FACTIBILIDAD,
    type Contrato,
    type Cotizacion,
    type Documento,
    type EstadoProspecto,
    type ResultadoFactibilidad,
} from "@/lib/comercial/presentacion";
import { erroresACampos } from "@/lib/formularios";
import { useLectura } from "@/lib/lectura";
import { useSesion } from "@/lib/sesion";

export type Zona = Schema<"Zona">;

// --- Insignias (icono + texto + color) ----------------------------------------------------------

export function InsigniaProspecto({ estado }: { estado: EstadoProspecto }) {
    const p = ESTADOS_PROSPECTO[estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-estado-prospecto={estado} />;
}

export function InsigniaFactibilidad({ resultado }: { resultado: ResultadoFactibilidad }) {
    const p = RESULTADOS_FACTIBILIDAD[resultado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-factibilidad={resultado} />;
}

export function InsigniaCotizacion({ cotizacion }: { cotizacion: Pick<Cotizacion, "estado" | "vencida"> }) {
    const p = cotizacion.estado === "vigente" && cotizacion.vencida ? COTIZACION_VENCIDA : ESTADOS_COTIZACION[cotizacion.estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} />;
}

export function InsigniaContrato({ contrato }: { contrato: Pick<Contrato, "estado"> }) {
    const p = ESTADOS_CONTRATO[contrato.estado];
    return <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} />;
}

// --- Catálogos -----------------------------------------------------------------------------------

/** Zonas de la agenda (`agenda.ver`); sin el permiso no se piden y se muestran por número. */
export function useZonas() {
    const { puede } = useSesion();
    const permitido = puede(["agenda.ver"]);
    return useLectura<Zona[] | null>(async (signal) => (permitido ? ((await resolver(api().GET("/api/v1/zonas", { signal }))).data ?? []) : null));
}

export function nombreZona(zonas: readonly Zona[] | null | undefined, id: number | null): string | undefined {
    if (id === null) return undefined;
    return zonas?.find((z) => z.id === id)?.nombre ?? `Zona ${id}`;
}

// --- Pantalla ancha ------------------------------------------------------------------------------

/**
 * ¿Coincide la media query? `null` mientras no se sabe (en el servidor): así no se piden datos
 * de una vista que se va a reemplazar al hidratar.
 */
export function useCoincide(consulta: string): boolean | null {
    const suscribir = useCallback(
        (avisar: () => void) => {
            const m = window.matchMedia(consulta);
            m.addEventListener("change", avisar);
            return () => m.removeEventListener("change", avisar);
        },
        [consulta],
    );
    return useSyncExternalStore(
        suscribir,
        () => window.matchMedia(consulta).matches,
        () => null,
    );
}

// --- Envío de acciones -------------------------------------------------------------------------

/**
 * Envío de una acción con `Idempotency-Key` (nueva tras una respuesta; la misma si no hubo red) y
 * su error: los `errors[]` de un 422 van a sus campos; el resto queda en `error` para el aviso.
 */
export function useEnvio() {
    const clave = useRef(nuevaClaveIdempotencia());
    const [error, setError] = useState<unknown>(null);
    const [enviando, setEnviando] = useState(false);

    /** Con `lanzar`, el error se relanza (para quien lo muestra por su cuenta) en vez de quedar en `error`. */
    async function enviar<R, T extends FieldValues = FieldValues>(
        peticion: (clave: string) => Promise<R>,
        campos?: { nombres: readonly Path<T>[]; setError: UseFormSetError<T> },
        opciones: { lanzar?: boolean } = {},
    ): Promise<R | undefined> {
        setError(null);
        setEnviando(true);
        try {
            const r = await peticion(clave.current);
            clave.current = nuevaClaveIdempotencia();
            return r;
        } catch (e) {
            clave.current = claveTrasError(e, clave.current);
            if (opciones.lanzar) throw e;
            if (!(campos && erroresACampos(e, campos.nombres, campos.setError))) {
                setError(e);
            }
            return undefined;
        } finally {
            setEnviando(false);
        }
    }

    const conflicto = error instanceof ErrorApi && error.estado === 409 && !error.codigo.startsWith("idempotencia");
    return { enviar, error, setError, enviando, conflicto };
}

export type Envio = ReturnType<typeof useEnvio>;

/**
 * 409 → «Alguien más cambió…» + «Recargar» (relee y deja enviar de nuevo con la versión nueva;
 * lo escrito se conserva); 422 y el resto, con el `detail` de la API.
 */
export function AvisoAccion({ envio, que, recargar }: { envio: Pick<Envio, "error" | "setError">; que: string; recargar: () => Promise<void> }) {
    const [recargando, setRecargando] = useState(false);
    const error = envio.error;
    if (!error) return null;
    if (error instanceof ErrorApi && error.estado === 409 && !error.codigo.startsWith("idempotencia")) {
        return (
            <Aviso
                tono="warning"
                titulo="Alguien más cambió este prospecto mientras lo veías"
                accion={
                    <Boton
                        type="button"
                        variante="secundario"
                        tamano="sm"
                        disabled={recargando}
                        onClick={async () => {
                            setRecargando(true);
                            try {
                                await recargar();
                                envio.setError(null);
                            } finally {
                                setRecargando(false);
                            }
                        }}
                    >
                        <RotateCw />
                        {recargando ? "Recargando…" : "Recargar"}
                    </Boton>
                }
            >
                Recarga para traer la versión actual y vuelve a intentarlo; lo que escribiste se conserva.
            </Aviso>
        );
    }
    const { titulo, detalle } = mensajeDeError(error);
    return (
        <Aviso tono="error" titulo={`No se pudo ${que}`}>
            {detalle ?? titulo}
        </Aviso>
    );
}

// --- PDF ---------------------------------------------------------------------------------------

const PATRON_UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Descarga autenticada de un PDF (adjunto del núcleo): lo pide con la sesión (cookies,
 * `X-Iris-Empresa`) y lo guarda con su nombre. Nunca sigue una URL que venga en los datos: arma
 * la ruta desde el id. Sin `adjuntos.ver` lo explica; si no se generó, ofrece «Generar PDF».
 */
export function DescargarPdf({ documento, nombre, etiqueta = "Descargar PDF", regenerar }: { documento: Documento | undefined; nombre: string; etiqueta?: string; regenerar?: () => Promise<void> }) {
    const { puede } = useSesion();
    const [bajando, setBajando] = useState(false);
    const [generando, setGenerando] = useState(false);
    const [error, setError] = useState<unknown>(null);

    if (!documento) return null;
    if (documento.estado === "no_generado" || documento.estado === "rechazado") {
        return (
            <div className="grid gap-2">
                <p className="text-sm text-muted-foreground">El PDF no se pudo guardar.</p>
                {regenerar ? (
                    <Boton
                        variante="secundario"
                        tamano="sm"
                        className="justify-self-start"
                        disabled={generando}
                        onClick={async () => {
                            setGenerando(true);
                            setError(null);
                            try {
                                await regenerar();
                            } catch (e) {
                                setError(e);
                            } finally {
                                setGenerando(false);
                            }
                        }}
                    >
                        <RotateCw />
                        {generando ? "Generando…" : "Generar PDF"}
                    </Boton>
                ) : null}
                {error ? (
                    <Aviso tono="error" titulo="No se pudo generar el PDF">
                        {mensajeDeError(error).detalle ?? mensajeDeError(error).titulo}
                    </Aviso>
                ) : null}
            </div>
        );
    }
    if (documento.estado !== "completo") {
        return (
            <p className="flex items-center gap-1.5 text-sm text-muted-foreground" role="status">
                <LoaderCircle aria-hidden className="size-4 animate-spin" />
                Preparando el PDF…
            </p>
        );
    }
    if (!puede(["adjuntos.ver"])) {
        return <p className="text-xs text-muted-foreground">Descargar el PDF requiere el permiso de ver adjuntos.</p>;
    }
    const id = PATRON_UUID.test(documento.adjuntoId) ? documento.adjuntoId.toLowerCase() : undefined;
    if (!id) return null;

    async function descargar(adjunto: string) {
        setBajando(true);
        setError(null);
        try {
            const blob = await resolver(api().GET("/api/v1/adjuntos/{id}", { params: { path: { id: adjunto } }, parseAs: "blob" }));
            if (!(blob instanceof Blob)) throw new Error("respuesta sin archivo");
            const url = URL.createObjectURL(blob);
            const a = document.createElement("a");
            a.href = url;
            a.download = `${nombre}.pdf`;
            document.body.append(a);
            a.click();
            a.remove();
            setTimeout(() => URL.revokeObjectURL(url), 60_000);
        } catch (e) {
            setError(e);
        } finally {
            setBajando(false);
        }
    }

    return (
        <div className="grid gap-2">
            <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={() => descargar(id)} disabled={bajando} data-adjunto={id}>
                <FileDown />
                {bajando ? "Descargando…" : etiqueta}
            </Boton>
            {error ? (
                <Aviso
                    tono="error"
                    titulo="No se pudo descargar el PDF"
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={() => descargar(id)}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {mensajeDeError(error).detalle}
                </Aviso>
            ) : null}
        </div>
    );
}
