"use client";

import { avisar, Boton, cn, InsigniaEstado } from "@iris/ui";
import { RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { api, claveTrasError, nuevaClaveIdempotencia, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import type { Permiso } from "@/lib/menu";
import { ANCLA_DIAGNOSTICO_ARGOS, presentacionVersion, type Conector } from "@/lib/integraciones/presentacion";
import { textoFechaHora } from "@/lib/reportes/presentacion";
import { useSesion } from "@/lib/sesion";

/** Permiso para pedir «Revisar ahora». */
export const PERMISO_REVISAR_VERSION: Permiso = "integraciones.revisar_version";

/** Tras el 202, cuándo se vuelve a leer el conector (el job tarda unos segundos). */
const RELECTURAS_MS = [3000, 8000];

/** El error de «Revisar ahora» en palabras del PM. */
function textoErrorRevision(e: unknown): string {
    if (e instanceof ErrorApi) {
        if (e.codigo === "conector_apagado") return "el conector está apagado. Actívalo y vuelve a revisar.";
        if (e.codigo === "sistema_sin_version") return "este sistema no informa versión.";
        if (e.estado === 404) return "el conector ya no existe. Recarga la página.";
    }
    const m = mensajeDeError(e);
    return m.detalle ?? m.titulo;
}

/**
 * Diagnóstico del conector de Argos: qué versión informó, cuándo se leyó (hora de Guatemala) y el
 * resultado de esa lectura con el detalle del servidor (p. ej. «la URL base es la del panel y no la
 * de la API»). Así el PM entiende por qué sale «Argos < 1.6.0: inventario por técnico no
 * disponible». «Revisar ahora» vuelve a leerla (`POST …/revisar-version`), avisa y refresca.
 */
export function DiagnosticoArgos({ conector: c, alRevisar }: { conector: Conector; alRevisar: () => void }) {
    const { puede } = useSesion();
    const [clave, setClave] = useState(nuevaClaveIdempotencia);
    const [revisando, setRevisando] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const ref = useRef<HTMLElement>(null);
    const p = presentacionVersion(c.versionEstado);
    const detalle = c.versionDetalle?.trim() || p.ayuda;

    const temporizadores = useRef<number[]>([]);

    // Se llega con «Ver el conector de Argos» (`#diagnostico-argos`): la lista carga después.
    useEffect(() => {
        if (window.location.hash === `#${ANCLA_DIAGNOSTICO_ARGOS}`) ref.current?.scrollIntoView({ block: "start" });
        const pendientes = temporizadores.current;
        return () => pendientes.forEach((t) => window.clearTimeout(t));
    }, []);

    async function revisar() {
        setRevisando(true);
        setError(null);
        try {
            // 202: el servidor encola la lectura y el resultado llega en unos segundos.
            await resolver(api().POST("/api/v1/integraciones/conectores/{id}/revisar-version", { params: { path: { id: c.id }, header: { "Idempotency-Key": clave } } }));
            setClave(nuevaClaveIdempotencia());
            avisar({ titulo: "Revisando la versión de Argos", descripcion: "El resultado aparece aquí en unos segundos." });
            alRevisar();
            for (const ms of RELECTURAS_MS) temporizadores.current.push(window.setTimeout(alRevisar, ms));
        } catch (e) {
            setClave(claveTrasError(e, clave));
            setError(textoErrorRevision(e));
        } finally {
            setRevisando(false);
        }
    }

    return (
        <section
            ref={ref}
            id={ANCLA_DIAGNOSTICO_ARGOS}
            aria-labelledby={`${ANCLA_DIAGNOSTICO_ARGOS}-titulo`}
            className="grid scroll-mt-20 gap-2 rounded-lg border p-3"
            data-testid="diagnostico-argos"
        >
            <div className="flex flex-wrap items-center justify-between gap-2">
                <h3 id={`${ANCLA_DIAGNOSTICO_ARGOS}-titulo`} className="text-sm font-medium">
                    Diagnóstico de la versión
                </h3>
                <InsigniaEstado tono={p.tono} icono={p.icono} texto={p.texto} data-testid="estado-version" />
            </div>
            <dl className="grid gap-x-3 gap-y-1 text-sm sm:grid-cols-[auto_minmax(0,1fr)]">
                <dt className="text-muted-foreground">Versión de Argos</dt>
                <dd className="tabular-nums" data-testid="version-remota">
                    {c.versionRemota ?? <span className="text-muted-foreground">Sin leer</span>}
                </dd>
                <dt className="text-muted-foreground">Última revisión</dt>
                <dd className="tabular-nums" data-testid="version-consultada">
                    {c.versionConsultadaEn ? textoFechaHora(c.versionConsultadaEn) : <span className="text-muted-foreground">Nunca</span>}
                </dd>
            </dl>
            <p className={cn("text-sm break-words", p.tono === "success" || p.tono === "neutral" ? "text-muted-foreground" : "text-foreground")} data-testid="detalle-version">
                {detalle}
            </p>
            {error ? (
                <p role="alert" className="text-sm text-error">
                    No se pudo revisar: {error}
                </p>
            ) : null}
            {puede([PERMISO_REVISAR_VERSION]) ? (
                <Boton variante="secundario" tamano="sm" className="justify-self-start" onClick={() => void revisar()} disabled={revisando}>
                    <RefreshCw className={cn(revisando && "animate-spin")} />
                    {revisando ? "Revisando…" : "Revisar ahora"}
                </Boton>
            ) : null}
        </section>
    );
}
