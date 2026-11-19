import type { Schema } from "@iris/contratos";

type Permiso = Schema<"Permiso">;

const CAMPOS_RED = ["routerId", "routerNombre", "ip", "prefijoEnlaceV6", "prefijoDelegadoV6", "userPpp", "tipoConexion", "planta"] as const;
const CAMPOS_UBICACION = ["direccion", "lat", "lon", "referencia", "contactoNombre", "contactoTelefono", "contactoTelefonos"] as const;

/** Como el servidor (contrato con Argos 0.8.0 §2.4): sin el permiso, el campo NO viene (ni vacío). */
export function recortarServicio<T extends Partial<Schema<"Servicio">>>(s: T, puede: (p: Permiso) => boolean): T {
    const fuera = new Set<string>([...(puede("servicios.ver_red") ? [] : CAMPOS_RED), ...(puede("servicios.ver_ubicacion") ? [] : CAMPOS_UBICACION)]);
    return Object.fromEntries(Object.entries(s).filter(([k]) => !fuera.has(k))) as T;
}
