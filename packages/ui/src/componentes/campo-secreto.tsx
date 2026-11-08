"use client";

import { Eye, EyeOff } from "lucide-react";
import { useState, type ComponentProps } from "react";
import { cn } from "../utilidades";
import { Boton } from "./boton";
import { PieCampo, useIdsCampo, type PropsEnvolturaCampo } from "./campo";
import { Entrada } from "./entrada";
import { useErrorEstable } from "./error-estable";
import { Etiqueta } from "./etiqueta";

/*
 * Credencial que se pega una vez (token de acceso, app secret): oculta lo escrito, con el ojo para
 * mostrarlo u ocultarlo, en monoespaciada y sin que el navegador la recuerde ni la corrija
 * (`autoComplete="off"`, sin corrector ni mayúscula inicial). Sirve con `register` como `Campo`.
 * A diferencia de `CampoConIcono secreto` (la contraseña de la app de campo, 48 px), tiene la
 * medida de los campos del panel y el ojo dice qué muestra («Mostrar Token de acceso»).
 */

export type PropsCampoSecreto = Omit<ComponentProps<"input">, "type" | "autoComplete"> &
    PropsEnvolturaCampo & {
        /** Nombre del dato para el ojo («Mostrar token de acceso»); por defecto, la etiqueta si es texto. */
        nombre?: string;
    };

export function CampoSecreto({ etiqueta, error, ayuda, id, className, nombre, ...props }: PropsCampoSecreto) {
    const visible = useErrorEstable(error);
    const ids = useIdsCampo(id, visible, ayuda);
    const [mostrar, setMostrar] = useState(false);
    const que = nombre ?? (typeof etiqueta === "string" ? etiqueta.toLowerCase() : "el valor");
    return (
        <div className={cn("grid gap-1.5", className)}>
            <Etiqueta htmlFor={ids.idCampo}>{etiqueta}</Etiqueta>
            <div className="relative">
                <Entrada
                    {...ids.control}
                    {...props}
                    type={mostrar ? "text" : "password"}
                    autoComplete="off"
                    autoCorrect="off"
                    autoCapitalize="off"
                    spellCheck={false}
                    data-1p-ignore
                    data-lpignore="true"
                    className="pr-11 font-mono text-sm"
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
