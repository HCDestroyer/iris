"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import type { Schema } from "@iris/contratos";
import { avisar, Aviso, Boton, Campo, CampoArea, Casilla, DialogoCerrar, DialogoContenido, DialogoPie, DialogoProtegido, useCambiosSinGuardar } from "@iris/ui";
import { CircleCheck, KeyRound, RefreshCw, RotateCw, WandSparkles } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { BotonCopiar, useBorradorPortapapeles } from "@/componentes/cuenta/boton-copiar";
import { TokenConservado } from "@/componentes/tokens/token-conservado";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { cuerpoRestablecerContrasena, esquemaRestablecerContrasena, generarContrasenaTemporal, type DatosRestablecerContrasena } from "@/lib/cuenta/esquemas";
import { puedeRestablecerContrasena } from "@/lib/cuenta/sesion";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import { textoTokensRevocados } from "@/lib/tokens/presentacion";

type Usuario = Schema<"Usuario">;

type Props = {
    usuario: Usuario;
    /** La API respondió el usuario actualizado (o «Recargar» trajo su versión actual). */
    alCambiar: (u: Usuario) => void;
    /** 404: ya no está en tu alcance; quien muestra el botón vuelve a leer. */
    alPerderAlcance?: () => void;
    tamano?: "sm" | "md";
};

/**
 * «Restablecer contraseña» de otra persona (`usuarios.restablecer_contrasena`): el botón y su
 * diálogo. Solo con el permiso, sobre otro usuario persona y activo.
 */
export function BotonRestablecerContrasena({ usuario, alCambiar, alPerderAlcance, tamano = "md" }: Props) {
    const { yo, permisos } = useSesion();
    const [abierto, setAbierto] = useState(false);
    if (!puedeRestablecerContrasena(permisos, yo.usuario.id, usuario)) {
        return null;
    }
    return (
        <>
            <Boton variante="contorno" tamano={tamano} onClick={() => setAbierto(true)} aria-label={`Restablecer contraseña de ${usuario.nombre}`}>
                <KeyRound />
                Restablecer contraseña
            </Boton>
            <DialogoProtegido open={abierto} onOpenChange={setAbierto}>
                {abierto ? (
                    <DialogoRestablecer
                        usuario={usuario}
                        alCambiar={alCambiar}
                        alPerderAlcance={() => {
                            setAbierto(false);
                            alPerderAlcance?.();
                        }}
                        alCerrar={() => setAbierto(false)}
                    />
                ) : null}
            </DialogoProtegido>
        </>
    );
}

type Resultado = { usuario: Usuario; temporal: string; revocoTokens: boolean; revocados: number; conservados: number; sesiones: number };

/**
 * Motivo, contraseña temporal (generada al abrir; se entrega fuera de banda y se muestra por
 * última vez en el resultado) y «Revocar tokens» (apagado). Manda `version` e `Idempotency-Key`;
 * 409 → «Recargar», 404 → fuera de tu alcance.
 */
function DialogoRestablecer({ usuario, alCambiar, alPerderAlcance, alCerrar }: { usuario: Usuario; alCambiar: (u: Usuario) => void; alPerderAlcance: () => void; alCerrar: () => void }) {
    const [version, setVersion] = useState(usuario.version);
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const [resultado, setResultado] = useState<Resultado | null>(null);
    const clave = useClaveIdempotencia();
    const portapapeles = useBorradorPortapapeles();
    const [inicial] = useState(() => generarContrasenaTemporal());
    const formulario = useForm<DatosRestablecerContrasena>({
        resolver: zodResolver(esquemaRestablecerContrasena),
        defaultValues: { motivo: "", contrasenaTemporal: inicial, revocarTokens: false },
    });
    const { errors, isSubmitting } = formulario.formState;
    useCambiosSinGuardar(formulario.formState.isDirty, formulario.formState.isSubmitting);
    const temporal = useWatch({ control: formulario.control, name: "contrasenaTemporal" });

    async function enviar(datos: DatosRestablecerContrasena) {
        setError(null);
        try {
            const nuevo = await resolver(
                api().POST("/api/v1/usuarios/{id}/restablecer-contrasena", {
                    params: { path: { id: usuario.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: cuerpoRestablecerContrasena(datos, version),
                }),
            );
            clave.renovar();
            formulario.reset({ motivo: "", contrasenaTemporal: "", revocarTokens: false });
            setResultado({
                usuario: nuevo,
                temporal: datos.contrasenaTemporal,
                revocoTokens: datos.revocarTokens,
                revocados: nuevo.tokensRevocados,
                conservados: nuevo.tokensConservados,
                sesiones: nuevo.sesionesCerradas,
            });
            avisar({
                titulo: `Contraseña de ${nuevo.nombre} restablecida`,
                ...(nuevo.tokensConservados > 0 ? { descripcion: <TokenConservado /> } : {}),
            });
            alCambiar(nuevo);
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
            const actual = await resolver(api().GET("/api/v1/usuarios/{id}", { params: { path: { id: usuario.id } } }));
            setVersion(actual.version);
            alCambiar(actual);
            setError(null);
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    if (resultado) {
        return (
            <DialogoContenido titulo="Contraseña restablecida">
                <div className="grid gap-4">
                    <div className="flex items-start gap-3" role="status">
                        <CircleCheck aria-hidden className="mt-0.5 size-5 shrink-0 text-success" />
                        <div className="grid gap-2 text-sm">
                            <p className="font-medium">{resultado.usuario.nombre} ya tiene una contraseña temporal.</p>
                            <ul className="grid list-disc gap-1 pl-4 text-muted-foreground" aria-label="Resultado">
                                <li>{resultado.sesiones === 1 ? "Se cerró su sesión abierta." : resultado.sesiones > 1 ? `Se cerraron sus ${resultado.sesiones} sesiones abiertas.` : "No tenía sesiones abiertas."}</li>
                                <li data-testid="resultado-tokens">
                                    {resultado.revocoTokens
                                        ? resultado.conservados > 0
                                            ? textoTokensRevocados(resultado.revocados, resultado.conservados)
                                            : "Se revocaron sus tokens de API."
                                        : "Su verificación en dos pasos y sus tokens de API siguen igual."}
                                </li>
                                <li>Se desvincularon sus accesos desde Argos; podrá volver a vincularlos.</li>
                                <li>Al entrar con ella, Iris le pedirá elegir una contraseña nueva.</li>
                            </ul>
                        </div>
                    </div>
                    <div className="grid gap-2 rounded-lg border p-3">
                        <span className="text-sm text-muted-foreground">Contraseña temporal</span>
                        <div className="flex flex-wrap items-center gap-2">
                            <code className="font-mono text-base tracking-wide" data-testid="contrasena-temporal">
                                {resultado.temporal}
                            </code>
                            <BotonCopiar texto={resultado.temporal} etiqueta="Copiar" copiado="Copiada" sensible borrador={portapapeles} />
                        </div>
                    </div>
                    <Aviso tono="info" titulo="Entrégala fuera de banda">
                        En persona o por teléfono, nunca por correo ni chat. Es la última vez que se muestra: Iris no la guarda donde se pueda
                        volver a leer.
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
            titulo={`Restablecer contraseña de ${usuario.nombre}`}
            descripcion="Reemplaza su contraseña por una temporal y cierra todas sus sesiones; también se desvinculan sus accesos desde Argos. Su verificación en dos pasos no cambia."
        >
            <form noValidate onSubmit={formulario.handleSubmit(enviar)} className="grid gap-4" aria-label="Restablecer contraseña">
                <AvisoError error={error} recargando={recargando} recargar={recargar} perderAlcance={alPerderAlcance} reintentar={formulario.handleSubmit(enviar)} />
                <CampoArea
                    etiqueta="Motivo"
                    rows={2}
                    ayuda="Queda en la auditoría. Por ejemplo: «Olvidó su contraseña; verificado por llamada»."
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
                        ayuda="Entrégala en persona o por teléfono, nunca por correo ni chat."
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
                            Generar otra
                        </Boton>
                        <BotonCopiar texto={temporal} etiqueta="Copiar" copiado="Copiada" sensible borrador={portapapeles} />
                    </div>
                </div>
                <Casilla
                    etiqueta="Revocar también sus tokens de API"
                    ayuda="Solo si sospechas que alguien más usa su cuenta. La integración activa con Argos u Horus no se corta."
                    {...formulario.register("revocarTokens")}
                />
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="fantasma">Cancelar</Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting || recargando}>
                        <KeyRound />
                        {isSubmitting ? "Restableciendo…" : "Restablecer"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}

type PropsError = { error: unknown; recargando: boolean; recargar: () => void; perderAlcance: () => void; reintentar: () => void };

function AvisoError({ error, recargando, recargar, perderAlcance, reintentar }: PropsError) {
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
                    <Boton type="button" variante="secundario" tamano="sm" onClick={perderAlcance}>
                        <RotateCw />
                        Recargar
                    </Boton>
                }
            >
                Puede que lo hayan movido a una empresa en que no operas o que ya no exista. No se cambió nada.
            </Aviso>
        );
    }
    if (estado === 403) {
        return (
            <Aviso tono="error" titulo="No puedes restablecer la contraseña de este usuario">
                {mensajeDeError(error).detalle ?? "Tiene permisos o empresas que tú no administras. Pídeselo a un administrador global."}
            </Aviso>
        );
    }
    if (codigo === "restablecer_propia") {
        return (
            <Aviso tono="warning" titulo="No puedes restablecer tu propia contraseña">
                Usa «Cambiar mi contraseña» en tu cuenta.
            </Aviso>
        );
    }
    const { titulo, detalle } = mensajeDeError(error);
    const sinConexion = codigo === "sin_conexion" || codigo === "api_no_disponible";
    return (
        <Aviso
            tono="error"
            titulo={sinConexion ? "No pudimos contactar a Iris" : `No se pudo restablecer: ${titulo}`}
            accion={
                <Boton type="button" variante="secundario" tamano="sm" onClick={reintentar}>
                    <RotateCw />
                    Reintentar
                </Boton>
            }
        >
            {sinConexion ? "Puede que no se haya aplicado. Reintenta: se usa la misma clave, así que no se aplicará dos veces." : detalle}
        </Aviso>
    );
}
