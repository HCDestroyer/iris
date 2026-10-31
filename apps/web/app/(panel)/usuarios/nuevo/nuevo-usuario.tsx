"use client";

import { useSearchParams } from "next/navigation";
import { PantallaCrearUsuario } from "@/componentes/usuarios/crear-usuario";

/**
 * `/usuarios/nuevo`. Con `?para=tecnico` (desde «Nuevo técnico»): rol Técnico fijo, «Cancelar»
 * vuelve a la ficha nueva y «Continuar con la ficha» regresa con el usuario elegido
 * (`/tecnicos/nuevo?usuario=<id>`; la ficha lee el usuario de la API).
 */
export function NuevoUsuario() {
    const paraTecnico = useSearchParams().get("para") === "tecnico";
    return paraTecnico ? (
        <PantallaCrearUsuario rolFijo="tecnico" volverA="/tecnicos/nuevo" textoContinuar="Continuar con la ficha" destino={(u) => `/tecnicos/nuevo?usuario=${u.id}`} />
    ) : (
        <PantallaCrearUsuario volverA="/usuarios" textoContinuar="Ver usuario" destino={(u) => `/usuarios/${u.id}`} />
    );
}
