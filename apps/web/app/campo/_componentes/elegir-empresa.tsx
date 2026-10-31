"use client";

import { Boton, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Building2, Check } from "lucide-react";
import type { EmpresaResumen } from "@/lib/campo/sesion";

export function ElegirEmpresa({
    empresas,
    actual,
    alElegir,
    alCancelar,
}: {
    empresas: readonly EmpresaResumen[];
    actual: number | null;
    alElegir: (id: number) => void;
    alCancelar?: (() => void) | undefined;
}) {
    return (
        <Tarjeta>
            <TarjetaEncabezado>
                <TarjetaTitulo>Elige la empresa</TarjetaTitulo>
                <TarjetaDescripcion>Tus órdenes del día se bajan para la empresa que elijas.</TarjetaDescripcion>
            </TarjetaEncabezado>
            <TarjetaContenido className="grid gap-2">
                {empresas.map((e) => (
                    <Boton key={e.id} variante={e.id === actual ? "primario" : "secundario"} tamano="lg" className="justify-start" onClick={() => alElegir(e.id)}>
                        {e.id === actual ? <Check aria-hidden /> : <Building2 aria-hidden />}
                        {e.nombre}
                    </Boton>
                ))}
                {alCancelar ? (
                    <Boton tamano="lg" variante="fantasma" onClick={alCancelar}>
                        Cancelar
                    </Boton>
                ) : null}
            </TarjetaContenido>
        </Tarjeta>
    );
}
