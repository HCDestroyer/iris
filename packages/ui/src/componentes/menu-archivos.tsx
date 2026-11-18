"use client";

import { Paperclip, type LucideIcon } from "lucide-react";
import { useRef, type ChangeEvent } from "react";
import { cn } from "../utilidades";
import { Boton, type PropsBoton } from "./boton";
import { MenuDesplegable, MenuDesplegableContenido, MenuDesplegableDisparador, MenuDesplegableElemento } from "./menu-desplegable";

export type OpcionArchivo = {
    clave: string;
    etiqueta: string;
    icono: LucideIcon;
    /** Tipos MIME que ofrece el selector del sistema (`accept`). */
    accept: string;
    /** Deja elegir varios a la vez. */
    varios?: boolean;
    /** Abre la cámara del teléfono en vez del selector de archivos. */
    camara?: boolean;
};

export type PropsMenuArchivos = {
    /** Nombre accesible del botón (y globo): «Adjuntar». */
    etiqueta: string;
    opciones: readonly OpcionArchivo[];
    /** Lo elegido, en el orden del selector del sistema, y de qué opción salió. */
    alElegir: (archivos: File[], clave: string) => void;
    disabled?: boolean;
    icono?: LucideIcon;
    variante?: PropsBoton["variante"];
    tamano?: PropsBoton["tamano"];
    className?: string;
};

/**
 * Botón de adjuntar con menú (como el clip de WhatsApp Web): cada opción abre el selector de
 * archivos del sistema con sus tipos, o la cámara. Los `<input type="file">` reales quedan fuera
 * del menú, ocultos; se limpian después de cada elección para poder elegir el mismo archivo otra
 * vez. Con una sola opción no hay menú: el botón abre el selector directamente.
 */
export function MenuArchivos({ etiqueta, opciones, alElegir, disabled, icono: Icono = Paperclip, variante = "fantasma", tamano = "icono-sm", className }: PropsMenuArchivos) {
    const entradas = useRef(new Map<string, HTMLInputElement>());
    const abrir = (clave: string) => entradas.current.get(clave)?.click();
    const alCambiar = (clave: string) => (e: ChangeEvent<HTMLInputElement>) => {
        const archivos = [...(e.target.files ?? [])];
        e.target.value = "";
        if (archivos.length > 0) alElegir(archivos, clave);
    };
    const boton = (
        <Boton
            variante={variante}
            tamano={tamano}
            aria-label={etiqueta}
            title={etiqueta}
            data-adjuntar=""
            disabled={disabled}
            className={cn("shrink-0", className)}
            {...(opciones.length === 1 && opciones[0] ? { onClick: () => abrir(opciones[0]?.clave ?? "") } : {})}
        >
            <Icono />
        </Boton>
    );
    return (
        <>
            {opciones.length === 1 ? (
                boton
            ) : (
                <MenuDesplegable>
                    <MenuDesplegableDisparador>{boton}</MenuDesplegableDisparador>
                    <MenuDesplegableContenido align="start">
                        {opciones.map((o) => (
                            <MenuDesplegableElemento key={o.clave} icono={o.icono} onSelect={() => abrir(o.clave)}>
                                {o.etiqueta}
                            </MenuDesplegableElemento>
                        ))}
                    </MenuDesplegableContenido>
                </MenuDesplegable>
            )}
            {opciones.map((o) => (
                <input
                    key={o.clave}
                    ref={(el) => {
                        if (el) entradas.current.set(o.clave, el);
                        else entradas.current.delete(o.clave);
                    }}
                    type="file"
                    hidden
                    tabIndex={-1}
                    aria-hidden
                    data-archivos={o.clave}
                    accept={o.accept}
                    multiple={o.varios ?? false}
                    {...(o.camara ? { capture: "environment" as const } : {})}
                    disabled={disabled}
                    onChange={alCambiar(o.clave)}
                />
            ))}
        </>
    );
}
