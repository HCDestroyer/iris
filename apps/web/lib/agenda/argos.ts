import type { Schema } from "@iris/contratos";
import { ErrorApi } from "@/lib/api/errores";

/** Texto fijo cuando Argos no tiene la capacidad (contrato 0.5.0). */
export const SIN_INVENTARIO_POR_TECNICO = "Argos < 1.6.0: inventario por técnico no disponible";

/**
 * Con los conectores leídos: `true` si el de Argos activo informa `inventario_por_tecnico`,
 * `false` si no, `null` si no hay conector de Argos activo (no se sabe: lo dirá la API al vincular).
 */
export function conInventarioPorTecnico(conectores: readonly Pick<Schema<"Conector">, "sistema" | "estado" | "capacidades">[]): boolean | null {
    const argos = conectores.find((c) => c.sistema === "argosisp" && c.estado === "activo");
    if (!argos) return null;
    return (argos.capacidades ?? []).includes("inventario_por_tecnico");
}

type TecnicoArgos = Schema<"TecnicoDeArgos">;
type BodegaArgos = Schema<"BodegaTecnico">;

/** Nombre de la bodega de Argos (contrato 0.6.4); un Argos anterior no lo manda: «Bodega <id>». */
export function nombreBodega(b: Pick<BodegaArgos, "id" | "nombre">): string {
    const n = (b.nombre ?? "").trim();
    return n !== "" ? n : `Bodega ${b.id}`;
}

/**
 * La bodega técnico activa del usuario de Argos en la empresa activa (sin empresa activa, la
 * primera activa). Es la que Argos usa para sus consumos y la que queda en la ficha.
 */
export function bodegaEnEmpresa(a: Pick<TecnicoArgos, "bodegas">, empresaId: number | null | undefined): BodegaArgos | undefined {
    const activas = (a.bodegas ?? []).filter((b) => b.activa);
    return empresaId ? activas.find((b) => b.empresaId === empresaId) : activas[0];
}

/** Texto de un técnico de Argos para elegirlo: «Bodega Norte · Juan Pérez». */
export function textoBodegaTecnico(a: Pick<TecnicoArgos, "bodegas" | "nombre">, empresaId: number | null | undefined): string {
    const b = bodegaEnEmpresa(a, empresaId);
    const bodegas = b ? [b] : (a.bodegas ?? []).filter((x) => x.activa);
    return [bodegas.length > 0 ? bodegas.map(nombreBodega).join(", ") : "Sin bodega activa", a.nombre].join(" · ");
}

/** Opciones del selector de bodega del formulario del técnico (los de Argos sin ficha). */
export function opcionesBodegaArgos(
    lista: readonly TecnicoArgos[],
    empresaId: number | null | undefined,
): { valor: string; etiqueta: string; detalle: string; deshabilitada: boolean }[] {
    return lista.map((a) => ({
        valor: a.idExterno,
        etiqueta: textoBodegaTecnico(a, empresaId),
        detalle: a.enEmpresa ? a.correo : `${a.correo} · sin bodega en esta empresa`,
        deshabilitada: !a.enEmpresa || bodegaEnEmpresa(a, empresaId) === undefined,
    }));
}

/**
 * Argos no pudo dar la lista (conector apagado, Argos caído o sin inventario por técnico): el
 * formulario deja escribir el número de bodega a mano como respaldo. Otros errores (permiso,
 * validación) no abren el respaldo.
 */
export function sinListaDeArgos(error: unknown): boolean {
    if (!(error instanceof ErrorApi)) return true;
    return (
        error.estado === 0 ||
        error.estado >= 500 ||
        ["conector_apagado", "argos_no_disponible", "argos_sin_inventario_tecnico", "sin_conexion"].includes(error.codigo)
    );
}

/** Una opción del selector de técnicos de Argos: texto y si se puede elegir para esta ficha. */
export function opcionTecnicoArgos(a: TecnicoArgos, tecnicoId: number): { texto: string; elegible: boolean } {
    const otraFicha = a.vinculadoA !== undefined && a.vinculadoA.tecnicoId !== tecnicoId;
    const bodegas = (a.bodegas ?? []).filter((b) => b.activa).map(nombreBodega);
    const partes = [a.nombre, a.correo, bodegas.length > 0 ? bodegas.join(", ") : "sin bodega activa"];
    if (!a.enEmpresa) partes.push("sin bodega en esta empresa");
    if (otraFicha) partes.push("ya vinculado a otra ficha");
    return { texto: partes.join(" · "), elegible: a.enEmpresa && !otraFicha };
}

/** Explicación de los errores de negocio de `vincular-argos`; `undefined` si es otro. */
export function textoErrorVinculo(error: unknown): string | undefined {
    if (!(error instanceof ErrorApi)) return undefined;
    switch (error.codigo) {
        case "argos_sin_inventario_tecnico":
            return SIN_INVENTARIO_POR_TECNICO;
        case "tecnico_no_encontrado":
            return "Argos ya no tiene ese usuario: vuelve a abrir la lista.";
        case "tecnico_sin_bodega":
            return "Ese usuario no tiene bodega técnico activa en esta empresa: dásela en Argos.";
        case "argos_vinculado_a_otro":
            return "Ese usuario de Argos ya está vinculado a otra ficha: desvincúlalo allá primero.";
        default:
            return undefined;
    }
}

/** Alta de la ficha con el técnico de Argos precargado (su bodega elegida y su correo en la búsqueda). */
export function rutaCrearFicha(a: Pick<TecnicoArgos, "idExterno" | "correo">): string {
    return `/tecnicos/nuevo?${new URLSearchParams({ argos: a.idExterno, correo: a.correo }).toString()}`;
}
