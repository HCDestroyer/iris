"use client";

import { avisar, Aviso, Boton, Campo, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie, Esqueleto, EstadoVacio, InsigniaEstado, Tarjeta } from "@iris/ui";
import { CopyPlus, Info, MessageSquare, Plus } from "lucide-react";
import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useState } from "react";
import { EncabezadoPagina } from "@/componentes/encabezado-pagina";
import { ErrorLectura } from "@/componentes/error-lectura";
import { nuevaClaveIdempotencia } from "@/lib/api/cliente";
import { useLectura } from "@/lib/lectura";
import * as P from "@/lib/plantillas-whatsapp/api";
import { estadoVisible, PERMISO_CREAR, RUTA, textoCategoria, textoUso, type PlantillaWA, type SugeridaEnCuenta } from "@/lib/plantillas-whatsapp/tipos";
import { useSesion } from "@/lib/sesion";
import { ErrorPlantilla, VistaPrevia } from "./comunes";

/** Pantalla d de la maqueta: las 7 de fábrica, ya conectadas con los datos de Iris y Argos. */
export function SugeridasPWA() {
    const waba = useSearchParams().get("cuenta") ?? "";
    const lectura = useLectura((signal) => P.leerSugeridas(waba, signal));
    const { puede } = useSesion();
    const crear = puede([PERMISO_CREAR]);
    const [pie, setPie] = useState<string | null>(null);
    const [editandoPie, setEditandoPie] = useState(false);
    const [creando, setCreando] = useState<string | null>(null);
    const [error, setError] = useState<{ clave: string; e: unknown } | null>(null);

    if (!waba) {
        return (
            <Tarjeta>
                <EstadoVacio icono={MessageSquare} titulo="Elige primero la cuenta de WhatsApp" descripcion="Entra desde la lista de plantillas de la cuenta." />
            </Tarjeta>
        );
    }
    const datos = lectura.estado === "listo" ? lectura.datos : null;
    const pieActual = pie ?? datos?.empresa ?? "";
    const faltan = datos?.data.filter((s) => !s.plantilla) ?? [];

    async function crearUna(s: SugeridaEnCuenta): Promise<boolean> {
        setCreando(s.clave);
        setError(null);
        try {
            const r = await P.crearSugerida(s.clave, { wabaId: waba, enviar: true, ...(pieActual ? { pie: pieActual } : {}) }, nuevaClaveIdempotencia());
            if (r.errorEnvio) {
                setError({ clave: s.clave, e: r.errorEnvio });
            }
            return !r.errorEnvio;
        } catch (e) {
            setError({ clave: s.clave, e });
            return false;
        } finally {
            setCreando(null);
        }
    }

    async function crearYAvisar(s: SugeridaEnCuenta) {
        if (await crearUna(s)) avisar({ titulo: `«${s.etiqueta}» enviada a Meta`, descripcion: "Meta la revisa en hasta 24 horas." });
        await lectura.refrescar();
    }

    async function crearTodas() {
        let bien = 0;
        for (const s of faltan) {
            if (!(await crearUna(s))) break;
            bien++;
        }
        if (bien > 0) avisar({ titulo: bien === 1 ? "1 plantilla enviada a Meta" : `${bien} plantillas enviadas a Meta`, descripcion: "Meta las revisa en hasta 24 horas." });
        await lectura.refrescar();
    }

    return (
        <>
            <EncabezadoPagina
                titulo="Plantillas sugeridas para tu ISP"
                descripcion="Listas para enviar a Meta, ya conectadas con los datos de Iris y Argos. Toca «Crear» y listo; si quieres, revísalas antes."
                acciones={
                    crear && faltan.length > 0 ? (
                        <Boton onClick={() => void crearTodas()} disabled={creando !== null}>
                            <CopyPlus />
                            {faltan.length === 1 ? "Crear la que falta" : `Crear las ${faltan.length} que faltan`}
                        </Boton>
                    ) : null
                }
            />
            {lectura.estado === "cargando" ? (
                <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3" aria-busy="true">
                    {[1, 2, 3].map((i) => (
                        <Esqueleto key={i} className="h-72" />
                    ))}
                </div>
            ) : lectura.estado === "error" ? (
                <ErrorLectura error={lectura.error} reintentar={lectura.reintentar} que="las plantillas sugeridas" />
            ) : (
                <div className="grid min-w-0 gap-4">
                    <Tarjeta className="flex flex-wrap items-center gap-2 p-3 text-sm" data-slot="se-crean-con">
                        <span className="text-muted-foreground">Se crean con:</span>
                        <span className="rounded-full bg-muted px-2.5 py-0.5">Cuenta {waba}</span>
                        <span className="rounded-full bg-muted px-2.5 py-0.5">Español</span>
                        <span className="rounded-full bg-muted px-2.5 py-0.5">{pieActual ? `Pie: «${pieActual}»` : "Sin pie"}</span>
                        <Boton variante="enlace" tamano="sm" onClick={() => setEditandoPie(true)}>
                            Cambiar
                        </Boton>
                    </Tarjeta>
                    <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3" aria-label="Plantillas sugeridas">
                        {lectura.datos.data.map((s) => (
                            <TarjetaSugerida
                                key={s.clave}
                                s={s}
                                waba={waba}
                                pie={pieActual}
                                puedeCrear={crear}
                                creando={creando === s.clave}
                                ocupado={creando !== null}
                                error={error?.clave === s.clave ? error.e : null}
                                alCrear={() => void crearYAvisar(s)}
                            />
                        ))}
                    </ul>
                    <Aviso tono="info" icono={Info} titulo="Meta revisa cada una en hasta 24 horas">
                        Las sugeridas siguen las reglas de Meta para avisos de servicio (sin promociones). Si Meta decide que alguna es de marketing, te avisamos y tú decides si la usas así.
                    </Aviso>
                </div>
            )}
            {editandoPie ? <DialogoPieSugeridas actual={pieActual} alCerrar={() => setEditandoPie(false)} alGuardar={(x) => (setPie(x), setEditandoPie(false))} /> : null}
        </>
    );
}

function TarjetaSugerida({
    s,
    waba,
    pie,
    puedeCrear,
    creando,
    ocupado,
    error,
    alCrear,
}: {
    s: SugeridaEnCuenta;
    waba: string;
    pie: string;
    puedeCrear: boolean;
    creando: boolean;
    ocupado: boolean;
    error: unknown;
    alCrear: () => void;
}) {
    const definicion = s.categoria === "AUTHENTICATION" || !pie ? s.definicion : { ...s.definicion, pie };
    const ejemplo = (n: string) => s.definicion.variables.find((v) => v.nombre === n)?.ejemplo ?? n;
    const ya = s.plantilla;
    const estado = ya ? estadoVisible({ estado: ya.estado as PlantillaWA["estado"], pausadaHasta: null, borradaPor: null }) : null;
    return (
        <li className="surface-card grid min-w-0 grid-rows-[auto_1fr_auto] overflow-hidden" data-sugerida={s.clave}>
            <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
                <h2 className="min-w-0 font-semibold break-words">{s.etiqueta}</h2>
                <span className="shrink-0 rounded-full bg-muted px-2 py-0.5 text-xs">{textoCategoria(s.categoria)}</span>
            </div>
            <div className="grid content-start gap-2 p-4">
                <VistaPrevia definicion={definicion} categoria={s.categoria} valor={ejemplo} titulo={s.para} />
                {s.usos.length ? (
                    <p className="flex flex-wrap gap-1">
                        {s.usos.map((u) => (
                            <span key={u} className="rounded-full bg-muted px-2 py-0.5 text-xs">
                                {textoUso(u)}
                            </span>
                        ))}
                    </p>
                ) : null}
                {error ? <ErrorPlantilla error={error} /> : null}
            </div>
            <div className="flex flex-wrap items-center gap-2 border-t border-border px-4 py-3">
                {ya && estado ? (
                    <>
                        <InsigniaEstado tono={estado.tono} texto={`Ya la tienes · ${estado.texto.toLowerCase()}`} />
                        <Boton asChild variante="contorno" tamano="sm" className="ml-auto">
                            <Link href={`${RUTA}/${ya.id}`}>Ver</Link>
                        </Boton>
                    </>
                ) : puedeCrear ? (
                    <>
                        <Boton asChild variante="contorno" tamano="sm">
                            <Link href={`${RUTA}/nueva?cuenta=${waba}&sugerida=${s.clave}`}>Revisar</Link>
                        </Boton>
                        <Boton tamano="sm" className="flex-1" onClick={alCrear} disabled={ocupado}>
                            <Plus />
                            {creando ? "Creando…" : "Crear"}
                        </Boton>
                    </>
                ) : (
                    <span className="text-sm text-muted-foreground">Aún no la tienes</span>
                )}
            </div>
        </li>
    );
}

/** El pie con que se crean (un campo: diálogo corto). */
function DialogoPieSugeridas({ actual, alCerrar, alGuardar }: { actual: string; alCerrar: () => void; alGuardar: (pie: string) => void }) {
    const [pie, setPie] = useState(actual);
    const largo = [...pie.trim()].length;
    return (
        <Dialogo open onOpenChange={(a) => !a && alCerrar()}>
            <DialogoContenido titulo="Pie de las sugeridas" descripcion="Texto gris bajo cada mensaje (por ejemplo, el nombre de tu ISP). Vacío: sin pie.">
                <Campo etiqueta="Pie" maxLength={60} value={pie} onChange={(e) => setPie(e.target.value)} ayuda={`${largo}/60, sin datos.`} />
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno">Cancelar</Boton>
                    </DialogoCerrar>
                    <Boton onClick={() => alGuardar(pie.trim())} disabled={/\{\{/.test(pie)}>
                        Usar este pie
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}
