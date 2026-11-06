// La cuenta propia en «Yo» (app nativa): `GET /yo` para el rol y la verificación en dos pasos, el
// cambio de contraseña y la activación del MFA, con la sesión del teléfono (cookie + CSRF del
// llavero). Nada de esto va a la cola sin conexión: sin señal no se intenta (lo dice la pantalla).
import type { Schema } from "@iris/contratos";
import { crearGeneradorUuid7 } from "@iris/sync";
import * as Crypto from "expo-crypto";
import { useCallback, useEffect, useState } from "react";
import { resolver } from "../api/errores";
import { cliente, leerYo } from "../sesion/sesion";

const uuid7 = crearGeneradorUuid7(Date.now, (bytes) => {
    Crypto.getRandomValues(bytes);
});

/** Una `Idempotency-Key` nueva por intento (`POST /yo/mfa` no se repite con la misma: 409). */
export function nuevaClave(): string {
    return uuid7();
}

export type LecturaCuenta = { estado: "cargando" } | { estado: "listo"; yo: Schema<"Yo"> } | { estado: "error"; error: unknown };

/** `GET /yo` al abrir la pantalla, con «Reintentar». */
export function useCuenta(empresa: number | null): LecturaCuenta & { reintentar: () => void } {
    const [lectura, setLectura] = useState<LecturaCuenta>({ estado: "cargando" });
    const [vez, setVez] = useState(0);
    useEffect(() => {
        let vivo = true;
        leerYo(empresa)
            // Una respuesta sin `usuario` no se toma por buena (nunca un estado inventado).
            .then((yo: Partial<Schema<"Yo">> | undefined) => vivo && setLectura(yo?.usuario ? { estado: "listo", yo: yo as Schema<"Yo"> } : { estado: "error", error: new Error("Respuesta de /yo incompleta") }))
            .catch((error: unknown) => vivo && setLectura({ estado: "error", error }));
        return () => {
            vivo = false;
        };
    }, [empresa, vez]);
    const reintentar = useCallback(() => {
        setLectura({ estado: "cargando" });
        setVez((v) => v + 1);
    }, []);
    return { ...lectura, reintentar };
}

export function cambiarContrasena(cuerpo: Schema<"EntradaCambiarContrasena">, clave: string): Promise<Schema<"Usuario">> {
    return resolver(cliente().POST("/api/v1/yo/contrasena", { body: cuerpo, params: { header: { "Idempotency-Key": clave } } }));
}

export function configurarDosPasos(clave: string): Promise<Schema<"MFAConfigurado">> {
    return resolver(cliente().POST("/api/v1/yo/mfa", { params: { header: { "Idempotency-Key": clave } } }));
}

export function activarDosPasos(cuerpo: Schema<"EntradaActivarMFA">, clave: string): Promise<Schema<"Usuario">> {
    return resolver(cliente().POST("/api/v1/yo/mfa/activar", { body: cuerpo, params: { header: { "Idempotency-Key": clave } } }));
}
