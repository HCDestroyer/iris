"use client";

import { Boton, InsigniaEstado } from "@iris/ui";
import { LocateFixed, RefreshCw } from "lucide-react";
import { nombreDe, type Cuadrilla } from "@/lib/despacho/calendario";
import { textoAntiguedad } from "@/lib/despacho/fechas";
import { FRESCURAS, frescura, type PosicionTecnico } from "@/lib/despacho/posiciones";

export type PosicionVista = PosicionTecnico & { antiguedad: number };

/** Lista de técnicos con su última posición y la antigüedad visible (lo mismo que el mapa, en texto). */
export function TecnicosEnVivo({
    posiciones,
    cuadrillas,
    actualizadaHace,
    actualizando,
    alActualizar,
    alEnfocar,
}: {
    posiciones: readonly PosicionVista[];
    cuadrillas: readonly Cuadrilla[] | undefined;
    actualizadaHace: number;
    actualizando: boolean;
    alActualizar: () => void;
    alEnfocar: (p: PosicionVista) => void;
}) {
    return (
        <div className="grid gap-2">
            <div className="flex items-center justify-between gap-2">
                <p className="text-xs text-muted-foreground" aria-live="polite" data-testid="posiciones-leidas">
                    Leídas {textoAntiguedad(actualizadaHace)} · se actualizan cada 30 s
                </p>
                <Boton variante="fantasma" tamano="sm" onClick={alActualizar} disabled={actualizando}>
                    <RefreshCw className={actualizando ? "animate-spin" : undefined} />
                    Actualizar
                </Boton>
            </div>
            {posiciones.length === 0 ? (
                <p className="text-sm text-muted-foreground">Ningún técnico ha enviado su ubicación todavía.</p>
            ) : (
                <ul className="grid gap-1.5 [&>*]:min-w-0" aria-label="Técnicos en vivo">
                    {[...posiciones]
                        .sort((a, b) => a.antiguedad - b.antiguedad)
                        .map((p) => {
                            const f = FRESCURAS[frescura(p.antiguedad)];
                            return (
                                <li key={p.tecnicoId} data-posicion={p.tecnicoId} className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md bg-elevated px-2.5 py-2 text-sm">
                                    <span className="font-medium">{p.nombre}</span>
                                    <InsigniaEstado tono={f.tono} icono={f.icono} texto={`${f.texto} · ${textoAntiguedad(p.antiguedad)}`} data-frescura={frescura(p.antiguedad)} />
                                    <span className="w-full text-xs text-muted-foreground tabular-nums sm:w-auto">
                                        {[
                                            nombreDe(cuadrillas, p.cuadrillaId, "Cuadrilla"),
                                            p.precision !== null ? `±${Math.round(p.precision)} m` : null,
                                            p.velocidad !== null && p.velocidad > 0.5 ? `${Math.round(p.velocidad * 3.6)} km/h` : null,
                                        ]
                                            .filter(Boolean)
                                            .join(" · ")}
                                    </span>
                                    <Boton variante="fantasma" tamano="icono-sm" className="ml-auto" aria-label={`Ver a ${p.nombre} en el mapa`} title="Ver en el mapa" onClick={() => alEnfocar(p)}>
                                        <LocateFixed />
                                    </Boton>
                                </li>
                            );
                        })}
                </ul>
            )}
        </div>
    );
}
