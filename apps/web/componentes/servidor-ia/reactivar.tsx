"use client";

import { avisar, Aviso, Boton, Casilla, Esqueleto, Tarjeta } from "@iris/ui";
import { Link2, Power } from "lucide-react";
import type { Route } from "next";
import Link from "next/link";
import { useState } from "react";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { activarMotor, leerMotor } from "@/lib/servidor-ia/api";
import { avisoIncompatibleAlEnlazar, COMANDO, huellaComoElServidor, textoErrorEnlace } from "@/lib/servidor-ia/presentacion";
import { Comando, RUTA } from "./comun";

/**
 * Un servidor **desactivado** se vuelve a usar por el camino corto: «Activar de nuevo»
 * (`POST …/activar { huellaTls }`), sin tocar el servidor. Se enseña la huella guardada para
 * confirmarla, con la misma regla que al enlazar: se guarda **qué huella** se marcó, así que si la
 * de pantalla cambia la marca cae sola. «Enlazar de nuevo» queda como salida secundaria, para
 * cuando se cambió de servidor o se reinstaló.
 */
export function ReactivarServidorIa({ alActivar }: { alActivar: () => void }) {
    const lectura = useLectura((signal) => leerMotor(signal));
    const [huellaMarcada, setHuellaMarcada] = useState<string | null>(null);
    const [ocupado, setOcupado] = useState(false);
    const [error, setError] = useState<unknown>(null);

    if (lectura.estado === "cargando") return <Esqueleto className="h-40" />;
    if (lectura.estado === "error") return <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="el servidor de IA" />;
    const motor = lectura.datos;
    if (!motor || motor.estado !== "desactivado") return null;

    const huella = motor.huellaTls;
    const coincide = huella !== null && huellaMarcada === huella;
    const incompatible = avisoIncompatibleAlEnlazar(motor.paquete);
    const codigo = error instanceof ErrorApi ? error.codigo : "";
    const enCurso = codigo === "rotacion_en_curso";

    async function activar() {
        if (!motor || huella === null || !coincide) return;
        setError(null);
        setOcupado(true);
        try {
            // Va la huella que está en pantalla y que se marcó: el servidor activa solo si sigue siendo esa.
            await activarMotor(motor.id, huella, nuevaClaveIdempotencia());
            avisar({ titulo: "Servidor de IA activado de nuevo", descripcion: "El asistente de las empresas que lo tenían elegido vuelve a usarlo." });
            alActivar();
        } catch (e) {
            setError(e);
            if (e instanceof ErrorApi && e.codigo === "huella_cambiada") {
                // Lo comparado ya no vale: se desmarca y se relee la tarjeta. Nunca se reintenta solo.
                setHuellaMarcada(null);
                try {
                    const actual = await leerMotor();
                    if (actual) lectura.reemplazar(actual);
                } catch {
                    // Se queda la de antes, ya desmarcada.
                }
            }
        } finally {
            setOcupado(false);
        }
    }

    return (
        <Tarjeta className="grid min-w-0 gap-3 p-4 sm:p-5" data-slot="reactivar">
            <h2 className="flex items-center gap-2 text-base font-semibold">
                <Power aria-hidden className="size-4 text-muted-foreground" />
                Activar de nuevo
            </h2>
            <p className="text-sm text-muted-foreground">Iris conserva el enlace con este servidor ({motor.url}). Para volver a usarlo no hay que tocar el servidor: confirma que su huella sigue siendo esta y actívalo.</p>
            {huella === null ? (
                <Aviso tono="warning" titulo="No hay una huella guardada" data-slot="reactivar-sin-huella">
                    Sin ella no se puede confirmar que es el mismo servidor: enlázalo de nuevo.
                </Aviso>
            ) : (
                <>
                    <p aria-label="Huella del servidor" data-slot="huella" className="rounded-lg bg-elevated p-3 font-mono text-sm font-semibold [overflow-wrap:anywhere]">
                        {huellaComoElServidor(huella)}
                    </p>
                    <p className="text-[0.8125rem] text-muted-foreground">Es la que muestra «{COMANDO.estado}» en el servidor.</p>
                    {incompatible ? (
                        <Aviso tono="warning" titulo={incompatible.titulo} data-slot="enlace-incompatible">
                            <span className="grid gap-2">
                                <span>{incompatible.detalle}</span>
                                {incompatible.comando ? <Comando texto={incompatible.comando} /> : null}
                            </span>
                        </Aviso>
                    ) : null}
                    <Casilla etiqueta="Coincide con la del servidor" checked={coincide} disabled={ocupado} onChange={(ev) => setHuellaMarcada(ev.target.checked ? huella : null)} />
                </>
            )}
            {error ? (
                <Aviso
                    tono={enCurso ? "warning" : "error"}
                    titulo={enCurso ? "Espera unos segundos" : codigo === "huella_cambiada" ? "La huella ya no es la que comparaste" : "No se pudo activar"}
                    data-slot="error-reactivar"
                    data-codigo={codigo}
                >
                    {error instanceof ErrorApi ? (textoErrorEnlace(error.codigo, error.estado) ?? (error.detalle || error.titulo)) : mensajeDeError(error).titulo}
                </Aviso>
            ) : null}
            <div className="flex flex-wrap items-center gap-2">
                <Boton disabled={!coincide || ocupado || incompatible !== null} onClick={() => void activar()}>
                    <Power />
                    {ocupado ? "Activando…" : "Activar de nuevo"}
                </Boton>
                <Boton asChild variante="fantasma">
                    <Link href={`${RUTA}/enlazar` as Route}>
                        <Link2 />
                        Enlazar de nuevo
                    </Link>
                </Boton>
            </div>
            <p className="text-[0.8125rem] text-muted-foreground">«Enlazar de nuevo» es para cuando cambiaste de servidor o lo reinstalaste: pide un código nuevo.</p>
        </Tarjeta>
    );
}
