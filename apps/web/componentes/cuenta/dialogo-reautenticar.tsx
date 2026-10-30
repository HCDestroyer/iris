"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, Dialogo, DialogoContenido, DialogoPie } from "@iris/ui";
import { LockKeyhole } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { registrarReautenticacion } from "@/lib/api/reautenticacion";
import { formatearEspera } from "@/lib/tiempo";

const esquema = z.object({ contrasena: z.string().min(1, "Escribe tu contraseña") });
type Datos = z.infer<typeof esquema>;

/**
 * «Confirma tu contraseña»: en una sesión abierta desde Argos, las acciones sensibles responden 403
 * `reautenticacion_requerida`. El cliente HTTP abre este diálogo y, al confirmar
 * (`POST /api/v1/sesion/reautenticar`, vale 15 min), repite la acción. Cancelar la deja con su error.
 */
export function DialogoReautenticar() {
    const [abierto, setAbierto] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const pendiente = useRef<((ok: boolean) => void) | null>(null);
    const formulario = useForm<Datos>({ resolver: zodResolver(esquema), defaultValues: { contrasena: "" } });
    const { errors, isSubmitting } = formulario.formState;

    useEffect(
        () =>
            registrarReautenticacion(
                () =>
                    new Promise<boolean>((resolver) => {
                        pendiente.current = resolver;
                        setError(null);
                        formulario.reset({ contrasena: "" });
                        setAbierto(true);
                    }),
            ),
        [formulario],
    );

    function terminar(ok: boolean) {
        // La contraseña no se queda en el formulario: ni al confirmar ni al cancelar.
        formulario.reset({ contrasena: "" });
        const r = pendiente.current;
        pendiente.current = null;
        setAbierto(false);
        r?.(ok);
    }

    async function enviar(d: Datos) {
        setError(null);
        try {
            await resolver(api().POST("/api/v1/sesion/reautenticar", { body: { contrasena: d.contrasena } }));
            terminar(true);
        } catch (e) {
            // Ante cualquier error se borra lo escrito.
            formulario.reset({ contrasena: "" });
            if (e instanceof ErrorApi && e.estado === 401) {
                formulario.setError("contrasena", { message: "La contraseña no es correcta" });
                return;
            }
            setError(e);
        }
    }

    const bloqueado = error instanceof ErrorApi && error.estado === 429;
    return (
        <Dialogo open={abierto} onOpenChange={(a) => (a ? setAbierto(true) : terminar(false))}>
            <DialogoContenido
                titulo="Confirma tu contraseña"
                descripcion="Entraste desde Argos. Para esta acción confirma la contraseña de tu cuenta de Iris; vale 15 minutos."
            >
                <form className="grid gap-4" noValidate onSubmit={(e) => void formulario.handleSubmit(enviar)(e)}>
                    <Campo
                        etiqueta="Contraseña"
                        type="password"
                        autoComplete="current-password"
                        autoFocus
                        error={errors.contrasena?.message}
                        disabled={isSubmitting || bloqueado}
                        {...formulario.register("contrasena")}
                    />
                    {error ? (
                        <Aviso tono="error" titulo={bloqueado ? "Demasiados intentos" : "No se pudo confirmar"}>
                            {bloqueado
                                ? `Espera ${formatearEspera((error as ErrorApi).reintentarEn ?? 15 * 60)} antes de volver a intentarlo.`
                                : mensajeDeError(error).detalle}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <Boton type="button" variante="secundario" onClick={() => terminar(false)}>
                            Cancelar
                        </Boton>
                        <Boton type="submit" disabled={isSubmitting || bloqueado}>
                            <LockKeyhole />
                            {isSubmitting ? "Confirmando…" : "Confirmar y seguir"}
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
