"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { Aviso, Boton, Campo, CampoArea, Casilla, DialogoCerrar, DialogoContenido, DialogoPie, useCambiosSinGuardar } from "@iris/ui";
import { CircleCheck, RefreshCw, RotateCcw, RotateCw, WandSparkles } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { BotonCopiar, useBorradorPortapapeles } from "@/componentes/cuenta/boton-copiar";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cuerpoReiniciarMfa, esquemaReiniciarMfa, generarContrasenaTemporal, type DatosReiniciarMfa } from "@/lib/cuenta/esquemas";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";

type Usuario = Schema<"Usuario">;

type Props = {
    usuario: Usuario;
    /** La API respondió 200: el usuario ya sin MFA (y con su `version` nueva). */
    alReiniciar: (usuario: Usuario) => void;
    /** «Recargar» tras un 409 trajo la versión actual del usuario. */
    alRecargar: (usuario: Usuario) => void;
    /** 404: ya no está en tu alcance (o no existe); se vuelve a leer la lista. */
    alRecargarLista: () => void;
    alCerrar: () => void;
};

type Resultado = { usuario: Usuario; revocoTokens: boolean };

/** Usuario por id con `GET /usuarios?q=<correo>` (no hay `GET /usuarios/{id}`); `null` si ya no se ve. */
async function releerUsuario(u: Usuario): Promise<Usuario | null> {
    const pagina = await resolver(api().GET("/api/v1/usuarios", { params: { query: { q: u.correo, limit: 50 } } }));
    return pagina.data?.find((x) => x.id === u.id) ?? null;
}

/**
 * «Reiniciar MFA» de otro usuario (`usuarios.reiniciar_mfa`): motivo, contraseña temporal
 * (generada aquí, se entrega fuera de banda y no se vuelve a mostrar) y «Revocar tokens». Manda
 * `version` e `Idempotency-Key`; 409 → «Recargar», 404 → fuera de tu alcance.
 */
export function DialogoReiniciarMfa({ usuario, alReiniciar, alRecargar, alRecargarLista, alCerrar }: Props) {
    const [version, setVersion] = useState(usuario.version);
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const [resultado, setResultado] = useState<Resultado | null>(null);
    const clave = useClaveIdempotencia();
    // Del diálogo entero: pasar al resultado no borra lo recién copiado; cerrarlo sí.
    const portapapeles = useBorradorPortapapeles();
    const formulario = useForm<DatosReiniciarMfa>({
        resolver: zodResolver(esquemaReiniciarMfa),
        defaultValues: { motivo: "", contrasenaTemporal: "", revocarTokens: true },
    });
    const { errors, isSubmitting } = formulario.formState;
    useCambiosSinGuardar(formulario.formState.isDirty, formulario.formState.isSubmitting);
    const temporal = useWatch({ control: formulario.control, name: "contrasenaTemporal" });

    async function enviar(datos: DatosReiniciarMfa) {
        setError(null);
        try {
            const nuevo = await resolver(
                api().POST("/api/v1/usuarios/{id}/reiniciar-mfa", {
                    params: { path: { id: usuario.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: cuerpoReiniciarMfa(datos, version),
                }),
            );
            clave.renovar();
            // La contraseña temporal no se vuelve a mostrar: se borra del formulario.
            formulario.reset({ motivo: "", contrasenaTemporal: "", revocarTokens: true });
            setResultado({ usuario: nuevo, revocoTokens: datos.revocarTokens });
            alReiniciar(nuevo);
        } catch (e) {
            clave.trasError(e);
            if (erroresACampos(e, ["motivo", "contrasenaTemporal"], formulario.setError)) {
                return;
            }
            setError(e);
        }
    }

    async function recargar() {
        setRecargando(true);
        try {
            const actual = await releerUsuario(usuario);
            if (!actual) {
                setError(new ErrorApi({ estado: 404, codigo: "no_encontrado", titulo: "No encontrado" }));
                return;
            }
            setVersion(actual.version);
            alRecargar(actual);
            setError(actual.mfaActivo ? null : new ErrorApi({ estado: 422, codigo: "mfa_no_configurado", titulo: "Sin MFA" }));
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    if (resultado) {
        return (
            <DialogoContenido titulo="MFA reiniciado">
                <div className="grid gap-4">
                    <div className="flex items-start gap-3" role="status">
                        <CircleCheck aria-hidden className="mt-0.5 size-5 shrink-0 text-success" />
                        <div className="grid gap-2 text-sm">
                            <p className="font-medium">
                                Reiniciamos la verificación en dos pasos de {resultado.usuario.nombre}.
                            </p>
                            <ul className="grid list-disc gap-1 pl-4 text-muted-foreground" aria-label="Resultado">
                                <li>Se cerraron todas sus sesiones abiertas.</li>
                                <li>{resultado.revocoTokens ? "Se revocaron sus tokens de API." : "Sus tokens de API siguen vigentes."}</li>
                                <li>Al entrar con la contraseña temporal, Iris le pedirá cambiarla y luego configurar de nuevo su verificación.</li>
                            </ul>
                        </div>
                    </div>
                    <Aviso tono="info" titulo="La contraseña temporal ya no se muestra">
                        Si no la entregaste aún, hazlo en persona o por teléfono. Iris no la guarda en ningún lugar donde se pueda
                        volver a leer. Si la copiaste, se borra del portapapeles a los 45 s o al cerrar este diálogo.
                    </Aviso>
                    <DialogoPie>
                        <Boton onClick={alCerrar}>Listo</Boton>
                    </DialogoPie>
                </div>
            </DialogoContenido>
        );
    }

    return (
        <DialogoContenido
            titulo={`Reiniciar MFA de ${usuario.nombre}`}
            descripcion="Borra su verificación en dos pasos y reemplaza su contraseña por una temporal. Cerrará todas sus sesiones."
        >
            <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4" aria-label="Reiniciar MFA">
                <AvisoError error={error} recargando={recargando} recargar={recargar} recargarLista={alRecargarLista} />
                <CampoArea
                    etiqueta="Motivo"
                    rows={2}
                    ayuda="Queda en la auditoría. Por ejemplo: «Cambió de teléfono; verificado por llamada»."
                    error={errors.motivo?.message}
                    {...formulario.register("motivo")}
                />
                <div className="grid gap-2">
                    <Campo
                        etiqueta="Contraseña temporal"
                        autoComplete="off"
                        spellCheck={false}
                        autoCapitalize="off"
                        className="[&_input]:font-mono [&_input]:tracking-wide"
                        ayuda="Entrégala en persona o por teléfono, nunca por correo ni chat. No se vuelve a mostrar."
                        error={errors.contrasenaTemporal?.message}
                        {...formulario.register("contrasenaTemporal")}
                    />
                    <div className="flex flex-wrap items-start gap-2">
                        <Boton
                            type="button"
                            variante="secundario"
                            onClick={() => formulario.setValue("contrasenaTemporal", generarContrasenaTemporal(), { shouldValidate: true, shouldDirty: true })}
                        >
                            <WandSparkles />
                            Generar una segura
                        </Boton>
                        <BotonCopiar texto={temporal} etiqueta="Copiar" copiado="Copiada" sensible borrador={portapapeles} />
                    </div>
                </div>
                <Casilla
                    etiqueta="Revocar también sus tokens de API"
                    ayuda="Los scripts o integraciones que usen sus tokens dejarán de funcionar."
                    {...formulario.register("revocarTokens")}
                />
                <DialogoPie>
                    {/* Por el diálogo: si hay cambios, pide confirmar antes de descartarlos. */}
                    <DialogoCerrar asChild>
                        <Boton variante="fantasma">Cancelar</Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting || recargando}>
                        <RotateCcw />
                        {isSubmitting ? "Reiniciando…" : "Reiniciar MFA"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}

function AvisoError({ error, recargando, recargar, recargarLista }: { error: unknown; recargando: boolean; recargar: () => void; recargarLista: () => void }) {
    if (!error) {
        return null;
    }
    const codigo = error instanceof ErrorApi ? error.codigo : undefined;
    const estado = error instanceof ErrorApi ? error.estado : undefined;
    if (estado === 409) {
        return (
            <Aviso
                tono="warning"
                titulo="Alguien más cambió este usuario mientras lo veías"
                accion={
                    <Boton type="button" variante="secundario" tamano="sm" onClick={recargar} disabled={recargando}>
                        <RefreshCw />
                        {recargando ? "Recargando…" : "Recargar"}
                    </Boton>
                }
            >
                Recarga para traer su versión actual; lo que escribiste se conserva.
            </Aviso>
        );
    }
    if (estado === 404) {
        return (
            <Aviso
                tono="error"
                titulo="Este usuario ya no está en tu alcance"
                accion={
                    <Boton type="button" variante="secundario" tamano="sm" onClick={recargarLista}>
                        <RotateCw />
                        Recargar la lista
                    </Boton>
                }
            >
                Puede que lo hayan movido a una empresa en que no operas o que ya no exista. No se cambió nada.
            </Aviso>
        );
    }
    if (estado === 403) {
        return (
            <Aviso tono="error" titulo="No puedes reiniciar el MFA de este usuario">
                {mensajeDeError(error).detalle ?? "Tiene permisos o empresas que tú no administras. Pídeselo a un administrador global."}
            </Aviso>
        );
    }
    if (codigo === "mfa_no_configurado") {
        return (
            <Aviso tono="warning" titulo="Este usuario ya no tiene MFA configurado">
                No hay nada que reiniciar: no se cambió nada.
            </Aviso>
        );
    }
    if (codigo === "reinicio_mfa_propio") {
        return (
            <Aviso tono="warning" titulo="No puedes reiniciar tu propio MFA">
                Pídeselo a otro administrador.
            </Aviso>
        );
    }
    if (codigo === "sin_conexion" || codigo === "api_no_disponible") {
        return (
            <Aviso tono="error" titulo="No pudimos contactar a Iris">
                Puede que no se haya aplicado. Revisa tu conexión y vuelve a enviar: se usa la misma clave, así que no se aplicará dos
                veces.
            </Aviso>
        );
    }
    const { titulo, detalle } = mensajeDeError(error);
    return (
        <Aviso tono="error" titulo={`No se pudo reiniciar el MFA: ${titulo}`}>
            {detalle}
        </Aviso>
    );
}
