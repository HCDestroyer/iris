"use client";

import { Label, ListBox, Select } from "@heroui/react";
import { ChevronDown } from "lucide-react";
import { Children, Fragment, isValidElement, useEffect, useRef, type ComponentProps, type CSSProperties, type Key, type ReactElement, type ReactNode } from "react";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { useControlNativo, type PropsControlNativo } from "./control-nativo";
import { useErrorEstable } from "./error-estable";

/*
 * Lista desplegable: el `Select` de HeroUI (React Aria: botón + lista en un popover, teclado,
 * búsqueda por la primera letra y lector de pantalla). La API es la de un `<select>`, para que las
 * pantallas cambien poco:
 * - las opciones se escriben como `<option value="…">Texto</option>` (también en fragmentos,
 *   listas y condiciones); `disabled` deja la opción visible pero no elegible;
 * - `value` + `onChange(e)` con `e.target.value`, o `{...register("campo")}` de react-hook-form
 *   (el `ref` llega a un input oculto con el `name`; `reset` y `setValue` mueven la opción elegida,
 *   ver `control-nativo.tsx`);
 * - `disabled`, `required`, `title`, `aria-label`, `aria-labelledby` y `aria-describedby`.
 * El valor vacío (`""`) es una opción más («Todos», «Sin asignar»…).
 */

/** Nombre del `<select>` oculto de React Aria (solo para el autocompletado del navegador). */
const NOMBRE_OCULTO = "Copia oculta para el autocompletado del navegador";

/** Clave para el valor vacío: React Aria no admite `""` como clave de una opción. */
const VACIO = "__iris_vacio__";
const aClave = (valor: string): string => (valor === "" ? VACIO : valor);
const deClave = (clave: Key | null): string => (clave === null || clave === VACIO ? "" : String(clave));

export type OpcionSelector = { valor: string; texto: string; deshabilitada: boolean };

/** Texto plano de los hijos de una opción (`{t.nombre} ({t.codigo})` → «Ana (A1)»). */
function textoDe(nodo: ReactNode): string {
    if (nodo === null || nodo === undefined || typeof nodo === "boolean") return "";
    if (typeof nodo === "string" || typeof nodo === "number" || typeof nodo === "bigint") return String(nodo);
    if (Array.isArray(nodo)) return nodo.map(textoDe).join("");
    if (isValidElement<{ children?: ReactNode }>(nodo)) return textoDe(nodo.props.children);
    return "";
}

/** Las `<option>` de los hijos (también dentro de fragmentos, listas y `<optgroup>`). */
export function leerOpciones(hijos: ReactNode): OpcionSelector[] {
    const opciones: OpcionSelector[] = [];
    Children.forEach(hijos, (hijo) => {
        if (!isValidElement(hijo)) return;
        const elemento = hijo as ReactElement<{ children?: ReactNode; value?: unknown; disabled?: boolean }>;
        if (elemento.type === Fragment || elemento.type === "optgroup") {
            opciones.push(...leerOpciones(elemento.props.children));
            return;
        }
        if (elemento.type !== "option") return;
        const texto = textoDe(elemento.props.children).trim();
        const valor = elemento.props.value === undefined || elemento.props.value === null ? texto : String(elemento.props.value as string | number);
        opciones.push({ valor, texto, deshabilitada: Boolean(elemento.props.disabled) });
    });
    return opciones;
}

type PropsNativas = Omit<ComponentProps<"select">, "ref" | "onChange" | "onBlur" | "value" | "defaultValue" | "size" | "multiple">;

export type PropsSelector = PropsNativas &
    PropsControlNativo<HTMLSelectElement> & {
        tamano?: "md" | "sm";
        /** Texto cuando ninguna opción coincide con el valor. */
        placeholder?: string;
        /** Etiqueta dentro del `Select` (la pone `CampoSelector`). */
        etiqueta?: ReactNode;
        invalido?: boolean;
    };

/** Lista desplegable sin etiqueta visible: lleva `aria-label`, `aria-labelledby` o una `Etiqueta` externa con `htmlFor`. */
export function Selector({
    className,
    tamano = "md",
    children,
    placeholder = "Elegir…",
    etiqueta,
    invalido,
    ref,
    name,
    value,
    defaultValue,
    onChange,
    onBlur,
    id,
    disabled,
    required,
    autoFocus,
    title,
    style,
    "aria-label": ariaLabel,
    "aria-labelledby": ariaLabelledby,
    "aria-describedby": ariaDescribedby,
    "aria-invalid": ariaInvalid,
    ...resto
}: PropsSelector) {
    const disparador = useRef<HTMLButtonElement>(null);
    const abiertaRef = useRef(false);
    const nativo = useControlNativo<HTMLSelectElement>({ ref, name, value, defaultValue, onChange, onBlur }, () => disparador.current?.focus());
    const opciones = leerOpciones(children);
    const deshabilitadas = opciones.filter((o) => o.deshabilitada).map((o) => aClave(o.valor));
    const esInvalido = invalido ?? (ariaInvalid === true || ariaInvalid === "true");
    const elegida = opciones.some((o) => o.valor === nativo.valor);
    const datos = Object.fromEntries(Object.entries(resto).filter(([k]) => k.startsWith("data-")));
    const sinNombre = !etiqueta && !ariaLabel && !ariaLabelledby;
    useEffect(() => {
        const boton = disparador.current;
        if (!boton) return;
        // React Aria deja un `<select>` oculto (aria-hidden) para el autocompletado del navegador,
        // dentro de un `<label>` con el texto de todas las opciones: con su propio nombre no se
        // confunde con el campo al buscarlo por etiqueta.
        const oculto = boton.closest('[data-slot="selector"]')?.querySelector<HTMLSelectElement>("select[tabindex='-1']");
        if (oculto && !oculto.hasAttribute("aria-label")) oculto.setAttribute("aria-label", NOMBRE_OCULTO);
        // Una `Etiqueta htmlFor={id}` de afuera: el botón de React Aria se nombra con
        // `aria-labelledby` (el valor elegido), que tapa a `<label for>`; se le suma la etiqueta.
        if (!id || !sinNombre) return;
        const externa = document.querySelector<HTMLLabelElement>(`label[for="${CSS.escape(id)}"]`);
        if (!externa) return;
        if (!externa.id) externa.id = `${id}-etiqueta`;
        const ids = (boton.getAttribute("aria-labelledby") ?? "").split(" ").filter(Boolean);
        if (!ids.includes(externa.id)) boton.setAttribute("aria-labelledby", [...ids, externa.id].join(" "));
    });
    return (
        <>
            <Select
                data-slot="selector"
                className={cn("relative w-full min-w-0", etiqueta ? "grid gap-1.5" : "inline-flex", className)}
                {...(style ? { style: style as CSSProperties } : {})}
                value={elegida ? aClave(nativo.valor) : null}
                onChange={(clave) => nativo.cambiar(deClave(clave as Key | null))}
                onOpenChange={(abierta) => {
                    abiertaRef.current = abierta;
                }}
                disabledKeys={deshabilitadas}
                placeholder={placeholder}
                isDisabled={disabled ?? false}
                isRequired={required ?? false}
                isInvalid={esInvalido}
                {...(autoFocus ? { autoFocus } : {})}
                {...(id ? { id } : {})}
                {...(ariaLabelledby ? { "aria-labelledby": ariaLabelledby, ...(ariaLabel ? { "aria-label": ariaLabel } : {}) } : {})}
                {...(ariaDescribedby ? { "aria-describedby": ariaDescribedby } : {})}
                fullWidth
            >
                {etiqueta ? <Label className="text-sm font-medium text-foreground select-none">{etiqueta}</Label> : null}
                {/* Con solo `aria-label`, una etiqueta oculta: React Aria nombra el botón por `aria-labelledby`. */}
                {!etiqueta && ariaLabel && !ariaLabelledby ? <Label className="sr-only">{ariaLabel}</Label> : null}
                <Select.Trigger
                    ref={disparador}
                    {...datos}
                    data-valor={nativo.valor}
                    {...(title ? { title } : {})}
                    onBlur={() => {
                        // Abrir la lista mueve el foco a ella: eso no es «salir del campo».
                        if (!abiertaRef.current) nativo.salir();
                    }}
                    className={cn(
                        "w-full min-w-0 items-center py-0 pr-8 pl-3 shadow-none",
                        tamano === "sm" ? "h-8 min-h-8 text-sm" : "h-10 min-h-10 text-base sm:text-sm",
                        "data-[disabled=true]:cursor-not-allowed",
                        esInvalido && "border-error",
                    )}
                >
                    <Select.Value className={cn("truncate", tamano === "sm" ? "text-sm" : "text-base sm:text-sm")}>
                        {({ isPlaceholder, selectedText }) => (isPlaceholder ? placeholder : selectedText)}
                    </Select.Value>
                    <Select.Indicator className="absolute top-1/2 right-2.5 size-4 -translate-y-1/2 text-muted-foreground">
                        <ChevronDown aria-hidden className="size-4" />
                    </Select.Indicator>
                </Select.Trigger>
                <Select.Popover className="min-w-(--trigger-width) max-w-[calc(100vw-2rem)] overflow-y-auto">
                    <ListBox>
                        {opciones.map((o) => (
                            <ListBox.Item key={aClave(o.valor)} id={aClave(o.valor)} textValue={o.texto} data-valor={o.valor} className="min-h-10 sm:min-h-8">
                                <span className="min-w-0 flex-1 break-words">{o.texto}</span>
                                <ListBox.ItemIndicator />
                            </ListBox.Item>
                        ))}
                    </ListBox>
                </Select.Popover>
            </Select>
            {/* Fuera del `Select`: React Aria dibuja sus hijos dos veces para armar la colección. */}
            {nativo.oculto}
        </>
    );
}

export type PropsCampoSelector = Omit<PropsSelector, "etiqueta"> & PropsEnvolturaCampo;

/** Etiqueta + lista desplegable + ayuda/error. */
export function CampoSelector({ etiqueta, error, ayuda, id, className, children, ...props }: PropsCampoSelector) {
    // Un error que aparece al salir del campo no mueve el botón que se está pulsando.
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Selector etiqueta={etiqueta} id={ids.idCampo} invalido={Boolean(visible)} {...(ids.control["aria-describedby"] ? { "aria-describedby": ids.control["aria-describedby"] } : {})} {...props}>
                {children}
            </Selector>
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </div>
    );
}
