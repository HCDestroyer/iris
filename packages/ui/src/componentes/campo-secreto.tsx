"use client";

import { Eye, EyeOff } from "lucide-react";
import { useState, useSyncExternalStore, type ComponentProps } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { Entrada } from "./entrada";
import { useErrorEstable } from "./error-estable";
import { Etiqueta } from "./etiqueta";

/*
 * Credencial que se pega una vez (token de acceso, app secret): oculta lo escrito, con el ojo para
 * mostrarlo u ocultarlo, en monoespaciada y sin corrector ni mayúscula inicial. Sirve con
 * `register` como `Campo`. A diferencia de `CampoConIcono secreto` (la contraseña de la app de
 * campo, 48 px), tiene la medida de los campos del panel y el ojo dice qué muestra.
 *
 * Que el gestor de contraseñas del navegador no ofrezca guardarla: un `type="password"` le dice
 * «aquí hay una contraseña» y Chrome, Edge y Safari ofrecen guardarla aunque diga
 * `autocomplete="off"`. Por eso, donde el navegador sabe ocultar texto con
 * `-webkit-text-security: disc` (Chrome, Edge, Safari y Firefox reciente), el campo es
 * `type="text"` con los puntos por CSS. Donde no lo sabe, queda `type="password"` (nunca se ve en
 * claro) con `autocomplete="new-password"`, que los gestores tratan como «contraseña nueva» y no
 * rellenan con la guardada. En los dos casos van `data-1p-ignore` y `data-lpignore` (1Password y
 * LastPass). Al dibujarse en el servidor es `password` y pasa a `text` al montar.
 */

function sinSuscripcion(): () => void {
    return () => undefined;
}

/** El navegador oculta texto con `-webkit-text-security` (consulta, no cambia). */
function ocultaConCss(): boolean {
    return typeof CSS !== "undefined" && typeof CSS.supports === "function" && CSS.supports("-webkit-text-security", "disc");
}

export type PropsCampoSecreto = Omit<ComponentProps<"input">, "type" | "autoComplete"> &
    PropsEnvolturaCampo & {
        /** Nombre del dato para el ojo («Mostrar token de acceso»); por defecto, la etiqueta si es texto. */
        nombre?: string;
    };

export function CampoSecreto({ etiqueta, error, ayuda, id, className, nombre, ...props }: PropsCampoSecreto) {
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    const [mostrar, setMostrar] = useState(false);
    const discos = useSyncExternalStore(sinSuscripcion, ocultaConCss, () => false);
    const oculto = !mostrar;
    const que = nombre ?? (typeof etiqueta === "string" ? etiqueta.toLowerCase() : "el valor");
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <div className="relative">
                <Entrada
                    {...ids.control}
                    {...props}
                    type={oculto && !discos ? "password" : "text"}
                    autoComplete="new-password"
                    data-oculto={oculto ? "true" : "false"}
                    autoCorrect="off"
                    autoCapitalize="off"
                    spellCheck={false}
                    data-1p-ignore
                    data-lpignore="true"
                    className={cn("pr-11 font-mono text-sm", oculto && discos && "[-webkit-text-security:disc]")}
                />
                <Boton
                    variante="fantasma"
                    tamano="icono"
                    aria-label={mostrar ? `Ocultar ${que}` : `Mostrar ${que}`}
                    aria-pressed={mostrar}
                    aria-controls={ids.idCampo}
                    className="absolute top-1/2 right-0.5 size-9 -translate-y-1/2 md:size-9"
                    onClick={() => setMostrar((m) => !m)}
                >
                    {mostrar ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
                </Boton>
            </div>
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </div>
    );
}
