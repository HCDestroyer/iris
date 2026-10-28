import { Camera } from "lucide-react";
import { useId, type ChangeEvent, type ReactNode } from "react";
import { cn } from "../utilidades";
import { variantesBoton, type PropsBoton } from "./boton";

export type PropsCamara = {
    /** Texto del botón, p. ej. «Tomar foto». */
    children: ReactNode;
    /** Archivo elegido (o tomado con la cámara del teléfono). */
    alElegir: (archivo: File) => void;
    /** `environment` = cámara trasera (por defecto); `user` = frontal. */
    camara?: "environment" | "user";
    accept?: string;
    disabled?: boolean;
    variante?: PropsBoton["variante"];
    tamano?: PropsBoton["tamano"];
    className?: string;
    id?: string;
    /** Nombre accesible si el texto visible no basta (p. ej. «Tomar foto de la fachada»). */
    "aria-label"?: string;
};

/**
 * Botón que abre la cámara del teléfono (`<input type="file" capture>`): en el teléfono abre la
 * cámara directo; en escritorio, el selector de archivos. El input real queda oculto pero
 * enfocable dentro de la etiqueta, así el teclado y los lectores de pantalla lo usan.
 */
export function Camara({
    children,
    alElegir,
    camara = "environment",
    accept = "image/*",
    disabled,
    variante = "secundario",
    tamano = "lg",
    className,
    id,
    "aria-label": etiqueta,
}: PropsCamara) {
    const generado = useId();
    const idCampo = id ?? generado;
    const alCambiar = (e: ChangeEvent<HTMLInputElement>) => {
        const archivo = e.target.files?.[0];
        // Se limpia para poder elegir otra vez el mismo archivo.
        e.target.value = "";
        if (archivo) {
            alElegir(archivo);
        }
    };
    return (
        <label
            htmlFor={idCampo}
            data-slot="camara"
            aria-disabled={disabled || undefined}
            className={cn(
                variantesBoton({ variante, tamano }),
                "cursor-pointer has-[input:focus-visible]:outline-2 has-[input:focus-visible]:outline-offset-2",
                disabled && "pointer-events-none opacity-55",
                className,
            )}
        >
            <Camera aria-hidden />
            {children}
            <input
                id={idCampo}
                type="file"
                accept={accept}
                capture={camara}
                disabled={disabled}
                aria-label={etiqueta}
                className="sr-only"
                onChange={alCambiar}
            />
        </label>
    );
}
