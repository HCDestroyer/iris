"use client";

import { useCallback, useEffect, useRef, useState, type ChangeEvent, type FocusEvent, type Ref } from "react";

/*
 * Puente entre un control de React Aria (Select, DatePicker, TimeField…) y quien espera un
 * control nativo: `register` de react-hook-form o `value` + `onChange(e)` con `e.target.value`.
 *
 * - Se dibuja un `<input type="hidden">` con el `name` del campo: es el que recibe el `ref`, así
 *   que react-hook-form lee `value` de ahí y el formulario nativo lo envía.
 * - react-hook-form escribe `value` directo en ese elemento (valores iniciales, `reset`,
 *   `setValue`), sin pasar por React. Por eso recibe el input envuelto (`conAvisoValor`): cada
 *   escritura de `value` también actualiza el estado del control visible.
 * - Al enfocar (p. ej. el primer campo con error tras enviar), el foco va al control visible.
 * - `onChange` y `onBlur` reciben un evento con `target` = el input oculto (con su `name` y su
 *   `value` ya puestos), que es lo que leen react-hook-form y las pantallas.
 */

/** Envuelve el input para enterarse cuando alguien le escribe `value` o le pide el foco. */
export function conAvisoValor<T extends object>(input: T, alEscribir: (valor: string) => void, alEnfocar: () => void): T {
    return new Proxy(input, {
        get(destino, clave) {
            if (clave === "focus") return () => alEnfocar();
            const valor: unknown = Reflect.get(destino, clave, destino);
            return typeof valor === "function" ? (valor as (...args: unknown[]) => unknown).bind(destino) : valor;
        },
        set(destino, clave, valor: unknown) {
            const hecho = Reflect.set(destino, clave, valor, destino);
            if (clave === "value") alEscribir(valor === null || valor === undefined ? "" : String(valor));
            return hecho;
        },
    });
}

/** Entrega `valor` al `ref` de quien usa el control (función u objeto), como hace React. */
export function asignarRef<T>(ref: Ref<T> | undefined, valor: T | null): void {
    if (typeof ref === "function") ref(valor);
    else if (ref) (ref as { current: T | null }).current = valor;
}

type ValorNativo = string | number | readonly string[] | undefined;

/**
 * Props de un control nativo. `E` es el elemento que la pantalla cree recibir en el evento
 * (`HTMLSelectElement` para `Selector`): en realidad es el input oculto, que tiene `name` y
 * `value` como cualquier control.
 */
export type PropsControlNativo<E extends HTMLElement = HTMLInputElement> = {
    ref?: Ref<E> | undefined;
    name?: string | undefined;
    value?: ValorNativo;
    defaultValue?: ValorNativo;
    onChange?: ((e: ChangeEvent<E>) => void) | undefined;
    onBlur?: ((e: FocusEvent<E>) => void) | undefined;
};

function aTexto(valor: ValorNativo): string {
    if (valor === undefined || valor === null) return "";
    return Array.isArray(valor) ? String(valor[0] ?? "") : String(valor);
}

/**
 * Estado del valor (controlado con `value` o interno) y el input oculto que lo refleja.
 * `alEnfocar` lleva el foco al control visible.
 */
export function useControlNativo<E extends HTMLElement = HTMLInputElement>({ ref, name, value, defaultValue, onChange, onBlur }: PropsControlNativo<E>, alEnfocar: () => void) {
    const input = useRef<HTMLInputElement | null>(null);
    // Un solo envoltorio por input: react-hook-form compara el elemento para no registrarlo dos veces.
    const envuelto = useRef<{ input: HTMLInputElement; proxy: HTMLInputElement } | null>(null);
    const [interno, setInterno] = useState(() => aTexto(defaultValue));
    const enfocar = useRef(alEnfocar);
    useEffect(() => {
        enfocar.current = alEnfocar;
    }, [alEnfocar]);
    const controlado = value !== undefined;
    const valor = controlado ? aTexto(value) : interno;

    const conectar = useCallback(
        (el: HTMLInputElement | null) => {
            input.current = el;
            let paraRef: E | null = null;
            if (el) {
                if (envuelto.current?.input !== el) {
                    envuelto.current = { input: el, proxy: conAvisoValor(el, setInterno, () => enfocar.current()) };
                }
                paraRef = envuelto.current.proxy as unknown as E;
            }
            asignarRef(ref, paraRef);
        },
        [ref],
    );

    const evento = (tipo: string) => {
        const el = input.current;
        return { target: el, currentTarget: el, type: tipo, nativeEvent: { type: tipo } } as unknown;
    };

    const cambiar = (nuevo: string) => {
        const el = input.current;
        if (el) el.value = nuevo;
        setInterno(nuevo);
        onChange?.(evento("change") as ChangeEvent<E>);
    };

    const salir = () => onBlur?.(evento("blur") as FocusEvent<E>);

    const oculto = <input type="hidden" ref={conectar} {...(name !== undefined ? { name } : {})} value={valor} data-slot="valor-nativo" />;

    return { valor, cambiar, salir, oculto };
}
