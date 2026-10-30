"use client";

import { Boton, Dialogo, DialogoContenido, DialogoPie } from "@iris/ui";
import { Trash2 } from "lucide-react";
import { useState, type ReactNode } from "react";

/**
 * Cierre de un diálogo con formulario: si hay cambios sin guardar, pregunta antes de descartarlos.
 * `pedirCierre` va en `onOpenChange` del diálogo (Escape, clic fuera, «Cerrar» y «Volver» pasan
 * por ahí) y `confirmacion` se dibuja junto al diálogo.
 */
export function useCierreSeguro(sucio: boolean, cerrar: () => void): { pedirCierre: () => void; confirmacion: ReactNode } {
    const [preguntando, setPreguntando] = useState(false);
    const pedirCierre = () => (sucio ? setPreguntando(true) : cerrar());
    const confirmacion = preguntando ? (
        <Dialogo open onOpenChange={(abierto) => !abierto && setPreguntando(false)}>
            <DialogoContenido role="alertdialog" titulo="¿Descartar los cambios?" descripcion="Hay cambios sin guardar en este formulario. Si sales ahora, se pierden." data-testid="confirmar-descartar">
                <DialogoPie>
                    <Boton
                        variante="contorno"
                        className="border-error/40 text-error hover:bg-error/10"
                        onClick={() => {
                            setPreguntando(false);
                            cerrar();
                        }}
                    >
                        <Trash2 />
                        Descartar cambios
                    </Boton>
                    <Boton onClick={() => setPreguntando(false)}>Seguir editando</Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    ) : null;
    return { pedirCierre, confirmacion };
}

/** Un diálogo de formulario que no se cierra con cambios sin guardar sin preguntar. */
export function DialogoFormulario({
    titulo,
    descripcion,
    sucio,
    alCerrar,
    children,
    className,
    "data-testid": testId,
}: {
    titulo: ReactNode;
    descripcion?: ReactNode;
    sucio: boolean;
    alCerrar: () => void;
    children: ReactNode;
    className?: string;
    "data-testid"?: string;
}) {
    const { pedirCierre, confirmacion } = useCierreSeguro(sucio, alCerrar);
    return (
        <>
            <Dialogo open onOpenChange={(abierto) => !abierto && pedirCierre()}>
                <DialogoContenido titulo={titulo} descripcion={descripcion} className={className} data-testid={testId}>
                    {children}
                </DialogoContenido>
            </Dialogo>
            {confirmacion}
        </>
    );
}
