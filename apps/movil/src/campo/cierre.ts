// «Firma del cliente y cierre» (paso final y único): qué falta para abrirlo, si toma firma y el
// cierre con la firma encolados juntos (`encolarFirmaYCierre` de `@iris/sync/campo`), con guarda
// contra el doble toque. La pantalla solo dibuja.
import type { OrdenVista } from "@iris/sync";
import { accionesDe, encolarFirmaYCierre, estadoPasos, faltantesAntesDeFirmar, type DatosFirma, type EstadoPaso } from "@iris/sync/campo";
import { hayFirma, trazosASvg } from "@iris/ui/firma";
import { useState } from "react";
import { useCampo } from "./contexto";
import type { LienzoListo } from "./firma";
import { useAccionUnica } from "./una-vez";

export type EtapaCierre =
    | { tipo: "cerrada" }
    | { tipo: "no_se_cierra"; motivo: "fallida" | "cancelada" | "reasignada" }
    /** Falta algo obligatorio: qué es y a qué pasos llevar. */
    | { tipo: "falta"; faltan: string[]; pasos: EstadoPaso[] }
    | { tipo: "pausada" }
    | { tipo: "firmar" };

/** En qué está el paso final de la orden. */
export function etapaCierre(orden: OrdenVista): EtapaCierre {
    if (orden.estado === "cerrada") {
        return { tipo: "cerrada" };
    }
    if (orden.estado === "fallida" || orden.estado === "cancelada" || orden.reasignada) {
        return { tipo: "no_se_cierra", motivo: orden.reasignada ? "reasignada" : orden.estado === "fallida" ? "fallida" : "cancelada" };
    }
    const faltan = faltantesAntesDeFirmar(orden);
    if (faltan.length > 0) {
        return { tipo: "falta", faltan, pasos: estadoPasos(orden).filter((p) => p.paso !== "cierre" && !p.completo) };
    }
    return accionesDe(orden).cerrar ? { tipo: "firmar" } : { tipo: "pausada" };
}

export function useFirmaYCierre(orden: OrdenVista) {
    const { motor, ejecutar, servicios } = useCampo();
    const guarda = useAccionUnica();
    const [resultado, setResultado] = useState<"exitosa" | "parcial">("exitosa");
    const [rehacer, setRehacer] = useState(false);
    const [lienzo, setLienzo] = useState<LienzoListo | null>(null);
    const [errorTrazo, setErrorTrazo] = useState<string | undefined>();
    const firmada = orden.firma !== null && orden.firma !== undefined && !rehacer;
    const firmo = lienzo !== null && hayFirma(lienzo.trazos);
    // Sin firma obligatoria y sin trazo, se cierra sin firma (no se valida el nombre).
    const tomaFirma = !firmada && (orden.requiereFirma || firmo);

    /** «Firmar y cerrar orden» (o «Cerrar orden»): una sola vez por toque. */
    const cerrar = (d: DatosFirma | null) =>
        guarda.correr(async () => {
            if (!firmada && orden.requiereFirma && !firmo) {
                setErrorTrazo("Pide al cliente que firme en el recuadro");
                return false;
            }
            setErrorTrazo(undefined);
            return ejecutar(async () => {
                const firma =
                    d && lienzo && firmo
                        ? { nombre: d.nombre, documento: d.documento, svg: trazosASvg(lienzo.trazos, lienzo.ancho, lienzo.alto), png: await lienzo.png(), tomadoEn: new Date().toISOString() }
                        : null;
                const p = await servicios.posicion();
                await encolarFirmaYCierre(motor, { orden, firma, resultado, posicion: p ? { lat: p.lat, lon: p.lon } : null });
            }, "Orden cerrada");
        });

    return { resultado, setResultado, firmada, tomaFirma, setLienzo, errorTrazo, volverAFirmar: () => setRehacer(true), cerrar, ocupado: guarda.ocupado };
}
