"use client";

import { useMemo } from "react";
import { encode } from "uqr";
import { trazoQr } from "@/lib/cuenta/qr";

/**
 * QR del `otpauth://` para la app de autenticación. Siempre oscuro sobre blanco (también en
 * modo oscuro): los lectores necesitan ese contraste y la zona de silencio de 4 módulos.
 */
export function CodigoQr({ texto, etiqueta, className }: { texto: string; etiqueta: string; className?: string }) {
    const qr = useMemo(() => encode(texto, { ecc: "M", border: 4 }), [texto]);
    const trazo = useMemo(() => trazoQr(qr.data), [qr]);
    return (
        <svg
            role="img"
            aria-label={etiqueta}
            viewBox={`0 0 ${qr.size} ${qr.size}`}
            shapeRendering="crispEdges"
            className={className}
            data-testid="codigo-qr"
        >
            <rect width={qr.size} height={qr.size} fill="#ffffff" />
            <path d={trazo} fill="#000000" />
        </svg>
    );
}
