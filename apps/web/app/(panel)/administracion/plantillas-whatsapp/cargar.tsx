"use client";

import { useParams, useSearchParams } from "next/navigation";
import { CargaRegistro } from "@/componentes/carga-registro";
import { useNombreEnMigas } from "@/componentes/migas-panel";
import { ErrorApi } from "@/lib/api/errores";
import { useLectura } from "@/lib/lectura";
import { idDeParametro } from "@/lib/parametros";
import * as P from "@/lib/plantillas-whatsapp/api";
import { nombreMetaDe, valoresDe } from "@/lib/plantillas-whatsapp/reglas";
import { RUTA } from "@/lib/plantillas-whatsapp/tipos";
import { EstadoVacio, Tarjeta } from "@iris/ui";
import { MessageSquare } from "lucide-react";
import { DetallePlantillaWA } from "./detalle";
import { FormularioPlantillaWA } from "./formulario";

/** La plantilla de la ruta (null si no existe o es de otra empresa: 404). */
function usePlantilla() {
    const id = idDeParametro(useParams<{ id: string }>().id);
    const lectura = useLectura(async (signal) => {
        if (id === null) return null;
        try {
            return await P.leerPlantilla(id, signal);
        } catch (e) {
            if (e instanceof ErrorApi && e.estado === 404) return null;
            throw e;
        }
    });
    useNombreEnMigas(lectura.estado === "listo" ? lectura.datos?.etiqueta : null);
    return lectura;
}

/** `/administracion/plantillas-whatsapp/[id]`: la ficha (pantalla c). */
export function CargarDetalle() {
    const lectura = usePlantilla();
    return (
        <CargaRegistro lectura={lectura} que="la plantilla" noEncontrado="Plantilla no encontrada">
            {(p) => <DetallePlantillaWA plantilla={p} alCambiar={(x) => lectura.reemplazar(x)} recargar={lectura.reintentar} />}
        </CargaRegistro>
    );
}

/** `/[id]/editar`: un borrador se edita; una que ya está en Meta se corrige y vuelve a enviarse. */
export function CargarEdicion() {
    const lectura = usePlantilla();
    return (
        <CargaRegistro lectura={lectura} que="la plantilla" noEncontrado="Plantilla no encontrada">
            {(p) =>
                p.estado === "BORRADOR" ? (
                    <FormularioPlantillaWA modo="borrador" plantilla={p} />
                ) : ["APPROVED", "REJECTED", "PAUSED", "FLAGGED"].includes(p.estado) ? (
                    <FormularioPlantillaWA modo="corregir" plantilla={p} />
                ) : (
                    <Tarjeta>
                        <EstadoVacio icono={MessageSquare} titulo="Esta plantilla no se puede editar ahora" descripcion="Meta la está revisando o ya no la tiene. Vuelve a la plantilla para ver su estado." />
                    </Tarjeta>
                )
            }
        </CargaRegistro>
    );
}

/** `/nueva?cuenta=…[&sugerida=…]`: en blanco o desde una sugerida («Revisar»). */
export function CargarNueva() {
    const params = useSearchParams();
    const waba = params.get("cuenta") ?? "";
    const clave = params.get("sugerida");
    const sugerida = useLectura(async (signal) => {
        if (!waba) return { sinCuenta: true as const };
        if (!clave) return { sinCuenta: false as const, inicial: undefined };
        const r = await P.leerSugeridas(waba, signal);
        const s = r.data.find((x) => x.clave === clave);
        if (!s) return { sinCuenta: false as const, inicial: undefined };
        const pie = r.empresa ? { ...s.definicion, pie: s.definicion.pie ?? r.empresa } : s.definicion;
        return {
            sinCuenta: false as const,
            inicial: valoresDe({ etiqueta: s.etiqueta, nombre: nombreMetaDe(s.clave), idioma: "es", categoria: s.categoria, usos: s.usos, definicion: pie }),
        };
    });
    return (
        <CargaRegistro lectura={sugerida} que="la plantilla sugerida" noEncontrado="Plantilla no encontrada">
            {(s) =>
                s.sinCuenta ? (
                    <Tarjeta>
                        <EstadoVacio icono={MessageSquare} titulo="Elige primero la cuenta de WhatsApp" descripcion="Entra desde la lista de plantillas de la cuenta." />
                    </Tarjeta>
                ) : (
                    <FormularioPlantillaWA modo="nueva" waba={waba} {...(s.inicial ? { inicial: s.inicial } : {})} />
                )
            }
        </CargaRegistro>
    );
}

export const RUTA_LISTA = RUTA;
