/*
 * Números de la lista «Números de WhatsApp» (contratos/conversaciones.md §15.3). Con
 * `empresas.todas`, `GET /canales` trae también la entrada **prestada** a la empresa activa con el
 * mismo `id` que la de la dueña (y `empresaId` = la activa). Para no mostrar el número dos veces,
 * con empresa activa quedan solo los de esa empresa (o sin empresa, en modo sin conector), y nunca
 * dos con el mismo `id` (gana la de la empresa activa).
 */
export function deLaEmpresa<T extends { id: number; empresaId: number | null }>(numeros: readonly T[], empresaActiva: number | null | undefined): T[] {
    const visibles = empresaActiva == null ? [...numeros] : numeros.filter((n) => n.empresaId === empresaActiva || n.empresaId === null);
    const porId = new Map<number, T>();
    for (const n of visibles) {
        const ya = porId.get(n.id);
        if (!ya || (empresaActiva != null && n.empresaId === empresaActiva && ya.empresaId !== empresaActiva)) porId.set(n.id, n);
    }
    return visibles.filter((n) => porId.get(n.id) === n);
}

/** «Compartido con 2 empresas». */
export function textoCompartido(n: number): string {
    return `Compartido con ${n} ${n === 1 ? "empresa" : "empresas"}`;
}

/** Usos en una frase («avisos y soporte»). */
export function listaUsos(usos: readonly string[], nombre: (u: string) => string): string {
    const n = usos.map((u) => nombre(u).toLowerCase());
    return n.length > 1 ? `${n.slice(0, -1).join(", ")} y ${n.at(-1)}` : (n[0] ?? "");
}
