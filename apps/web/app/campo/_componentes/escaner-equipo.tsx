"use client";

import { candidatoUnico, preguntaConfirmacion, SesionEscaneo, textoRepetido, textosEscaneo, type Candidato, type EstadoEscaneo, type ObjetivoEscaneo, type UsoCodigo } from "@iris/sync/campo";
import { Boton, InsigniaEstado, VisorEscaner, type CodigoLeido, type GeometriaVisor, type Lector } from "@iris/ui";
import { Barcode, CircleCheck, CircleSlash, Info, Keyboard, Package, QrCode, ScanLine, TriangleAlert, type LucideIcon } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { codigosQueCuentan, vibrarCorto } from "@/lib/campo/escaner";
import { useAtrasCierraHoja } from "./hoja";

/** Botones de la hoja: llenan su columna y el texto baja de línea antes que salirse (390 px). */
const ANCHO_BOTON = "h-auto min-h-11 w-full py-2 whitespace-normal";

/** Icono de lo que parece: encaja, código de producto, MAC (cuando se pide la serie) o no encaja. */
function iconoDe(c: Candidato): LucideIcon {
    return c.encaja ? CircleCheck : c.clase === "producto" ? Package : c.clase === "mac" ? Info : CircleSlash;
}

/**
 * Escáner de un campo del equipo (serie o MAC). Junta los códigos distintos dentro del recuadro
 * durante una ventana corta (`SesionEscaneo` de `@iris/sync/campo`) y luego muestra la hoja con lo
 * detectado: nunca acepta en silencio. Un solo candidato que encaja se muestra como confirmación;
 * un QR con serie y MAC llena los dos.
 */
export function EscanerEquipo({
    objetivo,
    existentes,
    alElegir,
    alEscribirAMano,
    alCerrar,
    crearLector,
}: {
    objetivo: ObjetivoEscaneo;
    /** Equipos ya registrados en esta orden (aviso de serie o MAC repetida). */
    existentes: readonly { serie?: string | null; mac?: string | null }[];
    alElegir: (uso: UsoCodigo) => void;
    alEscribirAMano: () => void;
    alCerrar: () => void;
    crearLector?: () => Promise<Lector>;
}) {
    const textos = textosEscaneo(objetivo);
    const [estado, setEstado] = useState<EstadoEscaneo>({ detectados: 0, candidatos: null });
    // Una sesión por apertura (el formulario la monta al tocar «Escanear…»): mientras está abierta,
    // ni el objetivo ni los equipos de la orden cambian.
    const [sesion] = useState(() => new SesionEscaneo({ objetivo, existentes: () => existentes, alCambiar: setEstado, alPrimerCodigo: () => vibrarCorto() }));
    useEffect(() => () => sesion.liberar(), [sesion]);
    // «Atrás» del navegador cierra el escáner (como su «X» y Escape), no la vista de debajo.
    useAtrasCierraHoja(true, alCerrar);

    const alDetectar = useCallback((codigos: CodigoLeido[], g: GeometriaVisor) => sesion.detectar(codigosQueCuentan(codigos, g)), [sesion]);
    const seguirEscaneando = () => sesion.seguir();
    const { detectados, candidatos } = estado;

    const unico = candidatos ? candidatoUnico(candidatos) : null;
    const otros = candidatos?.filter((c) => c !== unico) ?? [];

    return (
        <VisorEscaner
            objetivo={textos.titulo}
            ayuda={textos.ayuda}
            detectados={detectados}
            pausado={candidatos !== null}
            alDetectar={alDetectar}
            alCerrar={alCerrar}
            alEscribirAMano={alEscribirAMano}
            {...(crearLector ? { crearLector } : {})}
        >
            {candidatos ? (
                <section
                    role="dialog"
                    aria-label="Códigos detectados"
                    className="absolute inset-x-0 bottom-0 z-20 mx-auto grid max-h-[80%] max-w-xl gap-3 overflow-y-auto rounded-t-2xl border-t border-border bg-background p-4 pb-[max(1rem,env(safe-area-inset-bottom))] text-foreground shadow-2xl"
                >
                    {unico ? (
                        <div className="grid gap-3">
                            <h3 className="text-lg font-semibold break-words">{preguntaConfirmacion(objetivo, unico)}</h3>
                            <FichaCandidato candidato={unico} />
                            <div className="grid grid-cols-2 gap-2">
                                <Boton tamano="lg" onClick={() => alElegir(unico.uso)} className={ANCHO_BOTON}>
                                    <CircleCheck aria-hidden />
                                    Usar
                                </Boton>
                                <Boton variante="secundario" tamano="lg" onClick={seguirEscaneando} className={ANCHO_BOTON}>
                                    <ScanLine aria-hidden />
                                    Seguir escaneando
                                </Boton>
                            </div>
                        </div>
                    ) : (
                        <div className="grid gap-1">
                            <h3 className="text-lg font-semibold">{objetivo === "serie" ? "Elige el código del número de serie" : "Elige el código de la MAC"}</h3>
                            <p className="text-sm text-muted-foreground">{candidatos.length === 1 ? "1 código detectado" : `${candidatos.length} códigos detectados`}. Toca el que corresponde.</p>
                        </div>
                    )}
                    {otros.length > 0 ? (
                        <>
                            {unico ? <h4 className="text-sm font-semibold text-muted-foreground">Otros códigos detectados</h4> : null}
                            <ul aria-label={unico ? "Otros códigos detectados" : "Códigos detectados"} className="grid gap-2">
                                {otros.map((c) => (
                                    <li key={c.valor}>
                                        <Boton
                                            variante="contorno"
                                            disabled={!c.elegible}
                                            onClick={() => alElegir(c.uso)}
                                            className="h-auto min-h-11 w-full justify-start py-2.5 text-left whitespace-normal disabled:opacity-70"
                                        >
                                            <FichaCandidato candidato={c} />
                                        </Boton>
                                    </li>
                                ))}
                            </ul>
                        </>
                    ) : null}
                    <div className={unico ? "grid" : "grid grid-cols-2 gap-2"}>
                        {unico ? null : (
                            <Boton variante="secundario" tamano="lg" onClick={seguirEscaneando} className={ANCHO_BOTON}>
                                <ScanLine aria-hidden />
                                Seguir escaneando
                            </Boton>
                        )}
                        <Boton variante="fantasma" tamano="lg" onClick={alEscribirAMano} className={ANCHO_BOTON}>
                            <Keyboard aria-hidden />
                            Escribir a mano
                        </Boton>
                    </div>
                </section>
            ) : null}
        </VisorEscaner>
    );
}

/** Tipo y formato, valor en monoespaciada y lo que parece (icono + texto + color). */
function FichaCandidato({ candidato: c }: { candidato: Candidato }) {
    const Tipo = c.tipo === "qr" ? QrCode : Barcode;
    const repetido = textoRepetido(c.repetidos);
    return (
        <span className="grid min-w-0 gap-1">
            <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
                <Tipo aria-hidden className="size-4 shrink-0" />
                <span className="font-medium text-foreground">{c.tipoTexto}</span>
                <span className="text-xs">{c.formatoTexto}</span>
            </span>
            {c.uso.serie && c.uso.mac ? (
                <span className="grid font-mono text-lg leading-snug break-all text-foreground">
                    <span>S/N {c.uso.serie}</span>
                    <span>MAC {c.uso.mac}</span>
                </span>
            ) : (
                <span className="font-mono text-lg leading-snug break-all text-foreground">{c.valor}</span>
            )}
            <span className="flex flex-wrap gap-1.5">
                <InsigniaEstado tono={c.tono} icono={iconoDe(c)} texto={c.insignia} />
                {repetido ? <InsigniaEstado tono="warning" icono={TriangleAlert} texto={repetido} className="whitespace-normal" /> : null}
            </span>
        </span>
    );
}
