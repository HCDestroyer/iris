"use client";

import { Boton } from "@iris/ui";
import { Mic, Phone, UserCheck } from "lucide-react";
import { useDisponibilidad } from "@/lib/conversaciones/disponibilidad";
import { useVoz } from "./proveedor";

/** El paso que le falta a esta persona para que le suenen las llamadas aquí, o `null` si no falta nada que se sepa. */
export type PasoQueFalta =
    | { paso: "navegador"; texto: string }
    | { paso: "microfono"; texto: string }
    | { paso: "recibir"; texto: string }
    | { paso: "disponible"; texto: string }
    | { paso: "conexion"; texto: string };

/**
 * Qué le falta a quien contesta llamadas (`llamadas.contestar`) para que le suenen en este navegador:
 * navegador sin micrófono o sin https, micrófono bloqueado, «Recibir llamadas» apagado, «No
 * disponible» o sin conexión en vivo. Lo que el panel no sabe (si es de la cola) no se adivina.
 */
export function usePasoQueFalta(): PasoQueFalta | null {
    const voz = useVoz();
    const d = useDisponibilidad();
    if (!voz?.contesta) return null;
    if (!voz.admite) return { paso: "navegador", texto: "este navegador no puede contestar llamadas (hace falta micrófono y una conexión segura)" };
    if (voz.recibir.estado !== "listo") return null;
    if (voz.microfono === "denegado") return { paso: "microfono", texto: "el navegador bloqueó el micrófono para Iris" };
    if (!voz.recibir.recibe) return { paso: "recibir", texto: "«Recibir llamadas» está apagado en este navegador" };
    if (d?.lectura.estado === "listo" && d.lectura.datos.estado !== "disponible") return { paso: "disponible", texto: "estás «No disponible»" };
    if (voz.feed === "sin_conexion" || voz.feed === "sondeo" || voz.feed === "detenido") return { paso: "conexion", texto: "no hay conexión en vivo con Iris (se reconecta sola)" };
    return null;
}

/**
 * El paso que falta, dicho en llano y con su acción en un toque: «Encender recibir llamadas»,
 * «Ponerme disponible» o volver a pedir el micrófono. `cuando`: «No te está sonando» o «No te sonó».
 */
export function PasoQueFaltaLlamadas({ paso, cuando }: { paso: PasoQueFalta; cuando: string }) {
    const voz = useVoz();
    const d = useDisponibilidad();
    if (!voz) return null;
    return (
        <div data-slot="paso-que-falta" data-paso={paso.paso} className="grid gap-1.5 rounded-lg border border-warning/40 bg-warning/[0.06] p-2">
            <p className="text-xs text-foreground">
                {cuando} porque {paso.texto}.
            </p>
            {paso.paso === "recibir" ? (
                <Boton tamano="sm" className="justify-self-start" disabled={voz.cambiando} onClick={() => void voz.encender()}>
                    <Phone />
                    {voz.cambiando ? "Esperando al navegador…" : "Encender recibir llamadas"}
                </Boton>
            ) : paso.paso === "disponible" && d ? (
                <Boton tamano="sm" className="justify-self-start" disabled={d.cambiando} onClick={() => void d.cambiar("disponible")}>
                    <UserCheck />
                    Ponerme disponible
                </Boton>
            ) : paso.paso === "microfono" ? (
                <>
                    <p className="text-xs text-muted-foreground">Toca el candado de la barra de direcciones, permite el micrófono y vuelve a intentar.</p>
                    <Boton variante="secundario" tamano="sm" className="justify-self-start" disabled={voz.cambiando} onClick={() => void voz.encender()}>
                        <Mic />
                        Volver a intentar
                    </Boton>
                </>
            ) : null}
        </div>
    );
}
