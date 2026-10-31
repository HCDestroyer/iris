/**
 * Id de un parámetro de la URL (`?ticket=12`, `?usuario=7`): solo dígitos, positivo y seguro.
 * Cualquier otra cosa (texto, negativo, decimal, notación científica) es `null`.
 */
export function idDeParametro(crudo: string | null | undefined): number | null {
    if (!crudo || !/^[1-9]\d{0,14}$/.test(crudo)) return null;
    const id = Number(crudo);
    return Number.isSafeInteger(id) ? id : null;
}
