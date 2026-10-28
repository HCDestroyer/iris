"use client";

import { Boton, cn, Dialogo, DialogoContenido, Esqueleto } from "@iris/ui";
import { ChevronLeft, ChevronRight, ImageOff, RotateCw } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi } from "@/lib/api/errores";
import { esIdAdjunto, tipoImagen } from "@/lib/ordenes/adjuntos";

type EstadoImagen = { estado: "cargando" } | { estado: "error"; motivo: string } | { estado: "listo"; url: string };

/**
 * Descarga un adjunto de imagen por el endpoint autenticado (`GET /adjuntos/{id}`, con la cookie,
 * `X-Iris-Empresa` y la auditoría de lectura de la API) y lo expone como `blob:` local. Nunca se
 * enlaza la URL de la API en una pestaña nueva: lo que no es una imagen permitida no se muestra.
 */
export function useImagenAdjunto(id: string): EstadoImagen & { reintentar: () => void } {
    const [intento, setIntento] = useState(0);
    const clave = `${id}:${intento}`;
    // El resultado se guarda con su clave: si cambió el id o se reintentó, lo anterior no cuenta.
    const [resultado, setResultado] = useState<{ clave: string; estado: EstadoImagen } | null>(null);
    const valido = esIdAdjunto(id);

    useEffect(() => {
        if (!valido) return;
        const control = new AbortController();
        let url: string | undefined;
        resolver(api().GET("/api/v1/adjuntos/{id}", { params: { path: { id } }, parseAs: "blob", signal: control.signal })).then(
            (blob) => {
                if (control.signal.aborted) return;
                if (!(blob instanceof Blob) || !tipoImagen(blob.type)) {
                    setResultado({ clave, estado: { estado: "error", motivo: "No es una imagen" } });
                    return;
                }
                url = URL.createObjectURL(blob);
                setResultado({ clave, estado: { estado: "listo", url } });
            },
            (error: unknown) => {
                if (control.signal.aborted) return;
                const incompleto = error instanceof ErrorApi && error.codigo === "adjunto_incompleto";
                setResultado({ clave, estado: { estado: "error", motivo: incompleto ? "Aún se está subiendo" : "No se pudo cargar" } });
            },
        );
        return () => {
            control.abort();
            if (url) URL.revokeObjectURL(url);
        };
    }, [id, clave, valido]);

    const reintentar = () => setIntento((n) => n + 1);
    if (!valido) return { estado: "error", motivo: "Adjunto inválido", reintentar };
    if (!resultado || resultado.clave !== clave) return { estado: "cargando", reintentar };
    return { ...resultado.estado, reintentar };
}

/** Imagen de un adjunto, con su estado de carga y error con «Reintentar». */
export function ImagenAdjunto({ id, alt, className, ajuste = "cover" }: { id: string; alt: string; className?: string; ajuste?: "cover" | "contain" }) {
    const imagen = useImagenAdjunto(id);
    if (imagen.estado === "cargando") {
        return <Esqueleto className={cn("size-full", className)} aria-label={`Cargando ${alt}`} />;
    }
    if (imagen.estado === "error") {
        return (
            <span className={cn("flex size-full flex-col items-center justify-center gap-1 bg-elevated p-2 text-center text-xs text-muted-foreground", className)} role="img" aria-label={`${alt}: ${imagen.motivo}`}>
                <ImageOff aria-hidden className="size-5" />
                {imagen.motivo}
            </span>
        );
    }
    return (
        // eslint-disable-next-line @next/next/no-img-element -- blob: local de un adjunto autenticado; next/image no aplica.
        <img src={imagen.url} alt={alt} className={cn("size-full", ajuste === "cover" ? "object-cover" : "object-contain", className)} />
    );
}

export type Foto = { id: string; titulo: string; detalle?: ReactNode };

/** Miniaturas que abren un visor dentro del panel (nunca la imagen cruda en otra pestaña). */
export function Galeria({ fotos, etiqueta }: { fotos: readonly Foto[]; etiqueta: string }) {
    const [abierta, setAbierta] = useState<number | null>(null);
    const actual = abierta === null ? undefined : fotos[abierta];
    return (
        <>
            <ul className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4" aria-label={etiqueta}>
                {fotos.map((f, i) => (
                    <li key={f.id} className="grid gap-1">
                        <Boton
                            variante="fantasma"
                            className="aspect-square h-auto w-full overflow-hidden rounded-md border p-0"
                            aria-label={`Ver foto: ${f.titulo}`}
                            onClick={() => setAbierta(i)}
                            data-foto={f.id}
                        >
                            <ImagenAdjunto id={f.id} alt={f.titulo} />
                        </Boton>
                        <span className="truncate text-xs text-muted-foreground" title={f.titulo}>
                            {f.titulo}
                        </span>
                    </li>
                ))}
            </ul>
            <Dialogo open={actual !== undefined} onOpenChange={(a) => !a && setAbierta(null)}>
                {actual && abierta !== null ? (
                    <DialogoContenido titulo={actual.titulo} descripcion={fotos.length > 1 ? `Foto ${abierta + 1} de ${fotos.length}` : undefined} className="sm:w-[min(56rem,calc(100vw-2rem))]">
                        <div className="flex h-[min(70dvh,40rem)] items-center justify-center overflow-hidden rounded-md bg-elevated" data-testid="visor-foto">
                            <ImagenAdjunto key={actual.id} id={actual.id} alt={actual.titulo} ajuste="contain" />
                        </div>
                        {actual.detalle ? <div className="text-sm text-muted-foreground">{actual.detalle}</div> : null}
                        {fotos.length > 1 ? (
                            <div className="flex justify-between gap-2">
                                <Boton variante="secundario" tamano="sm" onClick={() => setAbierta((abierta - 1 + fotos.length) % fotos.length)}>
                                    <ChevronLeft />
                                    Anterior
                                </Boton>
                                <Boton variante="secundario" tamano="sm" onClick={() => setAbierta((abierta + 1) % fotos.length)}>
                                    Siguiente
                                    <ChevronRight />
                                </Boton>
                            </div>
                        ) : null}
                    </DialogoContenido>
                ) : null}
            </Dialogo>
        </>
    );
}

/** Reintento visible para una imagen que falló (p. ej. la firma). */
export function ImagenConReintento({ id, alt, className }: { id: string; alt: string; className?: string }) {
    const imagen = useImagenAdjunto(id);
    if (imagen.estado === "listo") {
        // eslint-disable-next-line @next/next/no-img-element -- blob: local de un adjunto autenticado; next/image no aplica.
        return <img src={imagen.url} alt={alt} className={cn("object-contain", className)} />;
    }
    if (imagen.estado === "cargando") {
        return <Esqueleto className={cn("h-28 w-full", className)} aria-label={`Cargando ${alt}`} />;
    }
    return (
        <span className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground" role="img" aria-label={`${alt}: ${imagen.motivo}`}>
            <ImageOff aria-hidden className="size-4" />
            {imagen.motivo}
            <Boton variante="secundario" tamano="sm" onClick={imagen.reintentar}>
                <RotateCw />
                Reintentar
            </Boton>
        </span>
    );
}
