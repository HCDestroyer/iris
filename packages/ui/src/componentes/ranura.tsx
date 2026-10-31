import { Children, cloneElement, isValidElement, type HTMLAttributes, type ReactElement, type ReactNode, type Ref, type RefCallback } from "react";
import { cn } from "../utilidades";

type PropsHijo = HTMLAttributes<HTMLElement> & { ref?: Ref<HTMLElement> };

/** Junta varias refs (función u objeto) en una. */
export function unirRefs<T>(...refs: (Ref<T> | undefined)[]): RefCallback<T> {
    return (valor) => {
        for (const ref of refs) {
            if (typeof ref === "function") ref(valor);
            else if (ref) ref.current = valor;
        }
    };
}

/**
 * Combina las props de la ranura con las del hijo: las clases se suman, los manejadores `on*` se
 * encadenan (primero el del hijo; si llama `preventDefault`, el de la ranura no corre) y el resto
 * lo decide el hijo.
 */
export function unirProps(ranura: Record<string, unknown>, hijo: Record<string, unknown>): Record<string, unknown> {
    const resultado: Record<string, unknown> = { ...ranura, ...hijo };
    for (const clave of Object.keys(ranura)) {
        const deRanura = ranura[clave];
        const deHijo = hijo[clave];
        if (/^on[A-Z]/.test(clave) && typeof deRanura === "function" && typeof deHijo === "function") {
            resultado[clave] = (...args: unknown[]) => {
                deHijo(...args);
                const evento = args[0];
                if (evento && typeof evento === "object" && "defaultPrevented" in evento && evento.defaultPrevented) return;
                deRanura(...args);
            };
        } else if (clave === "className") {
            resultado[clave] = cn(deRanura as string | undefined, deHijo as string | undefined);
        } else if (clave === "style" && deRanura && deHijo) {
            resultado[clave] = { ...(deRanura as object), ...(deHijo as object) };
        } else if (deHijo === undefined) {
            resultado[clave] = deRanura;
        }
    }
    return resultado;
}

export type PropsRanura = HTMLAttributes<HTMLElement> & { children?: ReactNode; ref?: Ref<HTMLElement> };

/**
 * `asChild`: pasa sus props (clases, manejadores, aria-*) al único hijo en lugar de crear un
 * elemento propio, p. ej. un `<Link>` con el estilo de `Boton`.
 */
export function Ranura({ children, ref, ...props }: PropsRanura) {
    const hijo = Children.only(children);
    if (!isValidElement<PropsHijo>(hijo)) return null;
    const elemento = hijo as ReactElement<PropsHijo>;
    const unidas = unirProps(props as Record<string, unknown>, elemento.props as Record<string, unknown>);
    return cloneElement(elemento, { ...unidas, ref: unirRefs(ref, elemento.props.ref) } as PropsHijo);
}
