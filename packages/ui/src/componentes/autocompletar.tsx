"use client";

import { ComboBox, Input, Label, ListBox } from "@heroui/react";
import { Search } from "lucide-react";
import { useContext, useEffect, type Key, type KeyboardEvent, type ReactNode } from "react";
import { ComboBoxStateContext } from "react-aria-components";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";
import { Cargando } from "./cargando";

export type OpcionAutocompletar = {
    valor: string;
    /** Texto visible y el que se busca al escribir. */
    etiqueta: string;
    /** Texto secundario bajo la etiqueta (código, correo…). */
    detalle?: string;
    /**
     * Contenido propio de la opción (nombre, dirección, servicios…) en lugar de etiqueta y
     * detalle. `etiqueta` sigue siendo su texto para el teclado y el lector de pantalla.
     */
    contenido?: ReactNode;
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
    /** Mientras se busca: la lista dice «Buscando…» si está vacía, o lo indica arriba si ya tiene resultados. */
    cargando?: boolean;
    /** Texto de «Buscando…». */
    textoCargando?: string;
    /** Lo que muestra la lista cuando no hay opciones (sin resultados, «escribe 3 letras»…). */
    sinResultados?: ReactNode;
    /** Arriba de la lista, dentro del panel (p. ej. «Recientes»). */
    cabeceraLista?: ReactNode;
    /** Nombre de la lista para el lector de pantalla («Clientes recientes»). */
    etiquetaLista?: string;
    /** Lupa a la izquierda del campo (búsquedas). */
    conLupa?: boolean;
    placeholder?: string;
    ayuda?: ReactNode;
    error?: string | undefined;
    disabled?: boolean;
    obligatorio?: boolean;
    autoFocus?: boolean;
    maxLength?: number;
    id?: string;
    className?: string;
    "data-testid"?: string;
};

/**
 * Con búsqueda en el servidor, la primera opción que llega queda resaltada (como en un buscador):
 * Enter la elige sin bajar con la flecha. React Aria limpia el resaltado cada vez que cambia el
 * texto, así que se vuelve a poner cuando llegan los resultados, la lista está abierta y la
 * opción ya se ve.
 */
function ResaltarPrimera({ primera }: { primera: string | undefined }) {
    const estado = useContext(ComboBoxStateContext);
    const abierta = estado?.isOpen ?? false;
    const resaltada = estado?.selectionManager.focusedKey ?? null;
    const texto = estado?.inputValue ?? "";
    useEffect(() => {
        if (!estado || !abierta || primera === undefined || resaltada !== null || texto.trim() === "") return undefined;
        // Resaltar hace que React Aria lleve la opción a la vista; si el panel todavía se está
        // acomodando con los resultados nuevos, eso desplaza la página y React Aria cierra la lista.
        // Se espera (cuadro a cuadro, hasta medio segundo) a que la opción se vea entera.
        let cuadro = 0;
        let id = 0;
        const probar = () => {
            const opcion = [...document.querySelectorAll<HTMLElement>('[role="listbox"] [role="option"]')].find((o) => o.dataset.key === primera);
            const caja = opcion?.getBoundingClientRect();
            if (caja && caja.height > 0 && caja.top >= 0 && caja.bottom <= window.innerHeight) {
                estado.selectionManager.setFocusedKey(primera);
                return;
            }
            if (++cuadro < 30) id = requestAnimationFrame(probar);
        };
        id = requestAnimationFrame(probar);
        return () => cancelAnimationFrame(id);
    }, [estado, abierta, primera, resaltada, texto]);
    return null;
}

/**
 * Enter nunca envía el formulario desde el campo: con la lista abierta elige la opción resaltada
 * (React Aria, que sigue recibiendo la tecla) y con la lista cerrada no hace nada. Sin esto, un
 * Enter con lo escrito sin elegir mandaba el formulario sin el cliente.
 */
function sinEnviarConEnter(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Enter") e.preventDefault();
}

/**
 * Autocompletar (HeroUI `ComboBox`, React Aria): se escribe para filtrar y se elige con flechas y
 * Enter. Rol `combobox` con su `listbox`. Para listas largas o búsquedas en el servidor (clientes,
 * técnicos); con pocas opciones fijas, `Selector`. Con `alBuscar`, la app trae las opciones (y
 * dice `cargando`); la lista se abre al enfocar, así que puede mostrar recientes con el campo vacío.
 */
export function Autocompletar({
    etiqueta,
    opciones,
    valor,
    alCambiar,
    alBuscar,
    texto,
    cargando = false,
    textoCargando = "Buscando…",
    sinResultados = "Sin resultados",
    cabeceraLista,
    etiquetaLista,
    conLupa = false,
    placeholder,
    ayuda,
    error,
    disabled,
    obligatorio,
    autoFocus,
    maxLength,
    id,
    className,
    "data-testid": testid,
}: PropsAutocompletar) {
    const ids = useIdsCampo(id, error, ayuda);
    const deshabilitadas = opciones.filter((o) => o.deshabilitada).map((o) => o.valor);
    return (
        <ComboBox
            data-slot="autocompletar"
            {...(testid ? { "data-testid": testid } : {})}
            className={cn("grid w-full gap-1.5", className)}
            selectedKey={valor}
            onSelectionChange={(clave: Key | null) => {
                const elegida = clave === null ? null : String(clave);
                // Con el texto controlado, React Aria espera que la app ponga el de la opción elegida.
                const etiquetaElegida = opciones.find((o) => o.valor === elegida)?.etiqueta;
                if (alBuscar && texto !== undefined && etiquetaElegida !== undefined) alBuscar(etiquetaElegida);
                alCambiar(elegida);
            }}
            {...(alBuscar ? { onInputChange: alBuscar, items: opciones } : { defaultItems: opciones })}
            // En una búsqueda, salir del campo o Escape no borran lo escrito (el aviso de error
            // con «Reintentar» queda bajo el campo y se puede tocar).
            allowsCustomValue={Boolean(alBuscar)}
            {...(texto !== undefined ? { inputValue: texto } : {})}
            disabledKeys={deshabilitadas}
            allowsEmptyCollection
            menuTrigger="focus"
            isDisabled={disabled ?? false}
            isRequired={obligatorio ?? false}
            isInvalid={Boolean(error)}
        >
            <Label className="text-sm font-medium">{etiqueta}</Label>
            <ComboBox.InputGroup className="relative">
                {conLupa ? <Search aria-hidden className="pointer-events-none absolute top-1/2 left-3 z-10 size-4 -translate-y-1/2 text-muted-foreground" /> : null}
                <Input
                    id={ids.idCampo}
                    aria-describedby={ids.control["aria-describedby"]}
                    placeholder={placeholder}
                    className={cn("h-10", conLupa && "pl-9")}
                    autoComplete="off"
                    onKeyDown={sinEnviarConEnter}
                    {...(autoFocus ? { autoFocus } : {})}
                    {...(maxLength ? { maxLength } : {})}
                />
                <ComboBox.Trigger aria-label="Ver opciones" />
            </ComboBox.InputGroup>
            <ComboBox.Popover className="max-w-[calc(100vw-1rem)]">
                {cabeceraLista || (cargando && opciones.length > 0) ? (
                    <div className="flex items-center justify-between gap-2 px-3 pt-2 pb-1 text-xs font-medium text-muted-foreground">
                        <span className="flex min-w-0 items-center gap-1.5">{cabeceraLista}</span>
                        {cargando && opciones.length > 0 ? <Cargando texto={textoCargando} visible /> : null}
                    </div>
                ) : null}
                <ListBox
                    {...(etiquetaLista ? { "aria-label": etiquetaLista } : {})}
                    renderEmptyState={() => <div className="px-3 py-2 text-sm text-muted-foreground">{cargando ? <Cargando texto={textoCargando} visible /> : sinResultados}</div>}
                >
                    {(o: OpcionAutocompletar) => (
                        <ListBox.Item id={o.valor} textValue={o.etiqueta}>
                            {o.contenido ?? (
                                <span className="grid min-w-0">
                                    <span className="truncate">{o.etiqueta}</span>
                                    {o.detalle ? <span className="truncate text-xs text-muted-foreground">{o.detalle}</span> : null}
                                </span>
                            )}
                            <ListBox.ItemIndicator />
                        </ListBox.Item>
                    )}
                </ListBox>
            </ComboBox.Popover>
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={error} />
            {alBuscar ? <ResaltarPrimera primera={opciones.find((o) => !o.deshabilitada)?.valor} /> : null}
        </ComboBox>
    );
}
