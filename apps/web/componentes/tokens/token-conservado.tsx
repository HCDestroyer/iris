import Link from "next/link";

/**
 * Revocar los tokens de un usuario conservó el de la integración activa (revocarlo la cortaría):
 * texto para el aviso de cerrar sesiones y de reiniciar el MFA, con el camino a Integraciones.
 */
export function TokenConservado() {
    return (
        <>
            Se conservó el token de la integración con Argos u Horus. Si está comprometida, desactiva el conector en{" "}
            <Link href="/administracion/integraciones" className="underline underline-offset-2">
                Integraciones
            </Link>
            .
        </>
    );
}
