/** Trazo SVG (un cuadrado por módulo oscuro) de un QR; la matriz ya trae la zona de silencio. */
export function trazoQr(datos: readonly (readonly boolean[])[]): string {
    const partes: string[] = [];
    datos.forEach((fila, y) => {
        fila.forEach((oscuro, x) => {
            if (oscuro) {
                partes.push(`M${x} ${y}h1v1h-1z`);
            }
        });
    });
    return partes.join("");
}
