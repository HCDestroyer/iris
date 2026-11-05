"use client";

import { Dialogo, DialogoConfirmar, DialogoContenido, cn } from "@iris/ui";
import { useEffect, useRef, useState, type ReactNode } from "react";

/*
 * «Atrás» del navegador (y el gesto o botón de atrás del teléfono) cierra la hoja abierta en vez de
 * cambiar la vista de debajo. Mientras haya alguna hoja abierta hay UNA entrada de historial propia
 * (misma URL) encima; «Atrás» la consume y pide cerrar a la hoja de más arriba (que con cambios
 * pregunta), y mientras quede alguna abierta la entrada se vuelve a poner (otro «Atrás» vuelve a
 * preguntar, nunca navega debajo). Al cerrarse la última de otra forma («X», Escape, guardar) se
 * quita la entrada, salvo que otra hoja se abra en el mismo momento (pasar de una a otra) o que la
 * app ya haya navegado a otra vista.
 */
const pilaHojas: { pedir: () => void }[] = [];
let conEntrada = false;
/** Quitar la entrada al terminar el commit (un microtask: también con el reloj falso de las pruebas). */
let quitarEntrada: { cancelado: boolean } | null = null;

function esNuestra(): boolean {
    return Boolean((window.history.state as { campoHoja?: boolean } | null)?.campoHoja);
}

function ponerEntrada() {
    window.history.pushState({ ...(window.history.state as object | null), campoHoja: true }, "", window.location.href);
    conEntrada = true;
}

function alAtrasGlobal() {
    const arriba = pilaHojas.at(-1);
    if (!arriba || !conEntrada || esNuestra()) {
        return;
    }
    // Se consumió la entrada: vuelve a ponerse mientras siga abierta alguna hoja.
    ponerEntrada();
    arriba.pedir();
}

export function useAtrasCierraHoja(abierto: boolean, pedirCierre: () => void): void {
    const pedir = useRef(pedirCierre);
    useEffect(() => {
        pedir.current = pedirCierre;
    });
    useEffect(() => {
        if (!abierto) {
            return;
        }
        const hoja = { pedir: () => pedir.current() };
        if (quitarEntrada) {
            // Otra hoja se acaba de cerrar: se reutiliza su entrada.
            quitarEntrada.cancelado = true;
            quitarEntrada = null;
        }
        if (pilaHojas.length === 0) {
            window.addEventListener("popstate", alAtrasGlobal);
        }
        if (!conEntrada || !esNuestra()) {
            ponerEntrada();
        }
        pilaHojas.push(hoja);
        return () => {
            const i = pilaHojas.indexOf(hoja);
            if (i >= 0) {
                pilaHojas.splice(i, 1);
            }
            if (pilaHojas.length > 0) {
                return;
            }
            window.removeEventListener("popstate", alAtrasGlobal);
            const tarea = { cancelado: false };
            quitarEntrada = tarea;
            queueMicrotask(() => {
                if (tarea.cancelado) {
                    return;
                }
                quitarEntrada = null;
                if (pilaHojas.length === 0 && conEntrada) {
                    conEntrada = false;
                    // Si la app ya navegó a otra vista, la entrada quedó debajo: no se vuelve atrás.
                    if (esNuestra()) {
                        window.history.back();
                    }
                }
            });
        };
    }, [abierto]);
}

export type PropsHojaCampo = {
    abierto: boolean;
    alCambiar: (abierto: boolean) => void;
    titulo: ReactNode;
    descripcion?: ReactNode;
    /** Hay algo escrito o elegido sin guardar: «X», Escape, tocar fuera y «Atrás» preguntan antes. */
    cambios?: boolean;
    className?: string;
    children: ReactNode;
};

/**
 * Hoja de la app de campo: el `Dialogo` de `@iris/ui` (HeroUI `Modal`: «X» con nombre accesible,
 * Escape y tocar fuera la cierran) que además se cierra con «Atrás» del navegador y, con
 * `cambios`, pregunta «¿Descartar los cambios?» antes de cerrar.
 */
export function HojaCampo({ abierto, alCambiar, titulo, descripcion, cambios = false, className, children }: PropsHojaCampo) {
    const [confirmando, setConfirmando] = useState(false);
    const pedirCierre = () => {
        if (cambios) {
            setConfirmando(true);
        } else {
            alCambiar(false);
        }
    };
    useAtrasCierraHoja(abierto, () => (confirmando ? setConfirmando(false) : pedirCierre()));
    return (
        <>
            <Dialogo open={abierto} onOpenChange={(a) => (a ? alCambiar(true) : pedirCierre())}>
                <DialogoContenido titulo={titulo} descripcion={descripcion} className={cn("campo-hoja rounded-t-[1.75rem]", className)}>
                    {children}
                </DialogoContenido>
            </Dialogo>
            <DialogoConfirmar
                open={abierto && confirmando}
                onOpenChange={setConfirmando}
                titulo="¿Descartar los cambios?"
                descripcion="Lo que escribiste aquí no se guardó."
                textoCancelar="Seguir editando"
                textoConfirmar="Descartar"
                peligro
                alConfirmar={() => {
                    setConfirmando(false);
                    alCambiar(false);
                }}
            />
        </>
    );
}
