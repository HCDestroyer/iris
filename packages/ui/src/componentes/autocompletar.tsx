"use client";

import { ComboBox, Input, Label, ListBox } from "@heroui/react";
import type { Key, ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";
import { Cargando } from "./cargando";

export type OpcionAutocompletar = {
    valor: string;
    /** Texto visible y el que se busca al escribir. */
    etiqueta: string;
    /** Texto secundario bajo la etiqueta (código, correo…). */
    detalle?: string;
    deshabilitada?: boolean;
};

export type PropsAutocompletar = {
    etiqueta: ReactNode;
    opciones: readonly OpcionAutocompletar[];
    valor: string | null;
    alCambiar: (valor: string | null) => void;
    /**
     * Búsqueda en el servidor: recibe lo escrito y la app cambia `opciones`. Sin ella, filtra las
     * opciones en el navegador (contiene, sin distinguir mayúsculas).
     */
    alBuscar?: (texto: string) => void;
    /** Texto de la búsqueda (controlado, junto con `alBuscar`). */
    texto?: string;
    cargando?: boolean;
    /** Mensaje cuando no hay resultados. */
    sinResultados?: string;
    placeholder?: string;
    ayuda?: ReactNode;
    error?: string | undefined;
    disabled?: boolean;
    obligatorio?: boolean;
    id?: string;
    className?: string;
};

/**
 * Autocompletar (HeroUI `ComboBox`, React Aria): se escribe para filtrar y se elige con flechas y
 * Enter. Rol `combobox` con su `listbox`. Para listas largas o búsquedas en el servidor (clientes,
 * técnicos); con pocas opciones fijas, `Selector`.
 */
export function Autocompletar({
    etiqueta,
    opciones,
    valor,
    alCambiar,
    alBuscar,
    texto,
    cargando = false,
    sinResultados = "Sin resultados",
    placeholder,
    ayuda,
    error,
    disabled,
    obligatorio,
    id,
    className,
}: PropsAutocompletar) {
    const ids = useIdsCampo(id, error, ayuda);
    const deshabilitadas = opciones.filter((o) => o.deshabilitada).map((o) => o.valor);
    return (
        <ComboBox
            data-slot="autocompletar"
            className={cn("grid w-full gap-1.5", className)}
            selectedKey={valor}
            onSelectionChange={(clave: Key | null) => alCambiar(clave === null ? null : String(clave))}
            {...(alBuscar ? { onInputChange: alBuscar, items: opciones } : { defaultItems: opciones })}
            {...(texto !== undefined ? { inputValue: texto } : {})}
            disabledKeys={deshabilitadas}
            allowsEmptyCollection
            menuTrigger="focus"
            isDisabled={disabled ?? false}
            isRequired={obligatorio ?? false}
            isInvalid={Boolean(error)}
        >
            <Label className="text-sm font-medium">{etiqueta}</Label>
            <ComboBox.InputGroup>
                <Input id={ids.idCampo} aria-describedby={ids.control["aria-describedby"]} placeholder={placeholder} className="h-10" />
                <ComboBox.Trigger aria-label="Ver opciones" />
            </ComboBox.InputGroup>
            <ComboBox.Popover>
                <ListBox
                    renderEmptyState={() => (
                        <div className="px-3 py-2 text-sm text-muted-foreground">{cargando ? <Cargando texto="Buscando…" visible /> : sinResultados}</div>
                    )}
                >
                    {(o: OpcionAutocompletar) => (
                        <ListBox.Item id={o.valor} textValue={o.etiqueta}>
                            <span className="grid min-w-0">
                                <span className="truncate">{o.etiqueta}</span>
                                {o.detalle ? <span className="truncate text-xs text-muted-foreground">{o.detalle}</span> : null}
                            </span>
                            <ListBox.ItemIndicator />
                        </ListBox.Item>
                    )}
                </ListBox>
            </ComboBox.Popover>
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
        </ComboBox>
    );
}
