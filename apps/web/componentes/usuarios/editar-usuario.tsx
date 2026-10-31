"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import {
    Aviso,
    Boton,
    BotonEnviar,
    Campo,
    CampoArea,
    DialogoCerrar,
    DialogoConfirmar,
    DialogoContenido,
    DialogoPie,
    DialogoProtegido,
    ErrorFormulario,
    useCambiosSinGuardar,
} from "@iris/ui";
import { Pencil, RefreshCw, UserX } from "lucide-react";
import { useState, type ReactNode } from "react";
import { useForm, useWatch } from "react-hook-form";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useSesion } from "@/lib/sesion";
import {
    cambioCorreoCierraSSO,
    cuerpoDesactivarUsuario,
    cuerpoEditarUsuario,
    esquemaDesactivarUsuario,
    esquemaEditarUsuario,
    type DatosDesactivarUsuario,
    type DatosEditarUsuario,
    type Usuario,
} from "@/lib/usuarios/esquemas";

type Props = {
    usuario: Usuario;
    /** La API respondió el usuario actualizado (o «Recargar» trajo su versión actual). */
    alCambiar: (u: Usuario) => void;
};

/** Relee el usuario (`GET /usuarios/{id}`) tras un 409. */
function releer(id: number): Promise<Usuario> {
    return resolver(api().GET("/api/v1/usuarios/{id}", { params: { path: { id } } }));
}

/**
 * Acciones de la ficha del usuario: «Editar» (`usuarios.editar`) y «Desactivar»
 * (`usuarios.desactivar`, con motivo y confirmación). Cada botón solo con su permiso; desactivar,
 * nunca a uno mismo ni a un usuario ya inactivo (Iris no tiene acción para reactivarlo).
 */
export function AccionesUsuario({ usuario, alCambiar }: Props) {
    const { yo, puede } = useSesion();
    const propio = usuario.id === yo.usuario.id;
    const [editando, setEditando] = useState(false);
    const [desactivando, setDesactivando] = useState(false);
    const puedeEditar = puede(["usuarios.editar"]);
    const puedeDesactivar = puede(["usuarios.desactivar"]) && usuario.activo && !propio;
    if (!puedeEditar && !puedeDesactivar) return null;
    return (
        <div className="flex flex-wrap gap-2">
            {puedeEditar ? (
                <Boton variante="secundario" onClick={() => setEditando(true)}>
                    <Pencil />
                    Editar
                </Boton>
            ) : null}
            {puedeDesactivar ? (
                <Boton variante="contorno" className="border-error/40 text-error hover:bg-error/10" onClick={() => setDesactivando(true)}>
                    <UserX />
                    Desactivar
                </Boton>
            ) : null}
            <DialogoProtegido open={editando} onOpenChange={setEditando}>
                {editando ? (
                    <FormularioEditar
                        usuario={usuario}
                        propio={propio}
                        alCambiar={alCambiar}
                        alGuardar={(u) => {
                            alCambiar(u);
                            setEditando(false);
                        }}
                    />
                ) : null}
            </DialogoProtegido>
            {desactivando ? (
                <ConfirmarDesactivar
                    usuario={usuario}
                    alCambiar={alCambiar}
                    alCerrar={() => setDesactivando(false)}
                    alDesactivar={(u) => {
                        setDesactivando(false);
                        alCambiar(u);
                    }}
                />
            ) : null}
        </div>
    );
}

function FormularioEditar({ usuario, propio, alCambiar, alGuardar }: { usuario: Usuario; propio: boolean; alCambiar: (u: Usuario) => void; alGuardar: (u: Usuario) => void }) {
    const [antes, setAntes] = useState(usuario);
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const clave = useClaveIdempotencia();
    const form = useForm<DatosEditarUsuario>({ resolver: zodResolver(esquemaEditarUsuario), defaultValues: { nombre: usuario.nombre, correo: usuario.correo }, mode: "onTouched" });
    const { errors, isSubmitting, isDirty } = form.formState;
    useCambiosSinGuardar(isDirty, isSubmitting);
    const correo = useWatch({ control: form.control, name: "correo" });
    const nombre = useWatch({ control: form.control, name: "nombre" });
    const cuerpo = cuerpoEditarUsuario({ nombre, correo }, antes);
    const sinCambios = cuerpo.nombre === undefined && cuerpo.correo === undefined;

    async function enviar(d: DatosEditarUsuario) {
        setError(null);
        try {
            const u = await resolver(
                api().POST("/api/v1/usuarios/{id}/editar", {
                    params: { path: { id: antes.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: cuerpoEditarUsuario(d, antes),
                }),
            );
            clave.renovar();
            form.reset({ nombre: u.nombre, correo: u.correo });
            alGuardar(u);
        } catch (e) {
            clave.trasError(e);
            if (e instanceof ErrorApi && e.codigo === "correo_en_uso") {
                form.setError("correo", { type: "servidor", message: e.detalle ?? "Ya existe un usuario con ese correo" });
                return;
            }
            if (erroresACampos(e, ["nombre", "correo"], form.setError)) return;
            setError(e);
        }
    }

    async function recargar() {
        setRecargando(true);
        try {
            const actual = await releer(antes.id);
            setAntes(actual);
            alCambiar(actual);
            setError(null);
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    return (
        <DialogoContenido titulo={`Editar a ${antes.nombre}`} descripcion="Nombre y correo con que entra a Iris." data-testid="editar-usuario">
            <form noValidate className="grid gap-4" aria-label="Editar usuario" onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                <Campo etiqueta="Nombre" autoComplete="off" maxLength={120} error={errors.nombre?.message} {...form.register("nombre")} />
                <Campo etiqueta="Correo" type="email" autoComplete="off" spellCheck={false} autoCapitalize="off" maxLength={254} error={errors.correo?.message} {...form.register("correo")} />
                {cambioCorreoCierraSSO({ correo }, antes, propio) ? (
                    <Aviso tono="warning" titulo="Cambiar el correo cierra su autologin">
                        Se borran sus vínculos con Argos y Horus: tendrá que volver a vincular su cuenta la próxima vez que entre desde allá.
                    </Aviso>
                ) : null}
                {error ? <AvisoAccion error={error} que="guardar los cambios" recargando={recargando} recargar={recargar} /> : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <BotonEnviar enviando={isSubmitting} textoEnviando="Guardando…" disabled={sinCambios || recargando}>
                        Guardar cambios
                    </BotonEnviar>
                </DialogoPie>
            </form>
        </DialogoContenido>
    );
}

function ConfirmarDesactivar({
    usuario,
    alCambiar,
    alCerrar,
    alDesactivar,
}: {
    usuario: Usuario;
    alCambiar: (u: Usuario) => void;
    alCerrar: () => void;
    alDesactivar: (u: Usuario) => void;
}) {
    const [version, setVersion] = useState(usuario.version);
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const clave = useClaveIdempotencia();
    const form = useForm<DatosDesactivarUsuario>({ resolver: zodResolver(esquemaDesactivarUsuario), defaultValues: { motivo: "" } });
    const errors = form.formState.errors;

    async function enviar(d: DatosDesactivarUsuario) {
        setError(null);
        try {
            const u = await resolver(
                api().POST("/api/v1/usuarios/{id}/desactivar", {
                    params: { path: { id: usuario.id }, header: { "Idempotency-Key": clave.actual() } },
                    body: cuerpoDesactivarUsuario(d, version),
                }),
            );
            clave.renovar();
            alDesactivar(u);
        } catch (e) {
            clave.trasError(e);
            if (erroresACampos(e, ["motivo"], form.setError)) return;
            setError(e);
        }
    }

    async function recargar() {
        setRecargando(true);
        try {
            const actual = await releer(usuario.id);
            setVersion(actual.version);
            alCambiar(actual);
            setError(actual.activo ? null : new ErrorApi({ estado: 422, codigo: "usuario_inactivo", titulo: "El usuario ya está desactivado" }));
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    return (
        <DialogoConfirmar
            open
            onOpenChange={(a) => (a ? undefined : alCerrar())}
            peligro
            titulo={`¿Desactivar a ${usuario.nombre}?`}
            descripcion="No se borra: deja de poder entrar, se cierran sus sesiones y sus tokens dejan de valer. Sus vínculos de autologin se borran."
            textoConfirmar="Desactivar"
            textoOcupado="Desactivando…"
            alConfirmar={() => form.handleSubmit(enviar)()}
        >
            <form noValidate className="grid gap-3" aria-label="Desactivar usuario" onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                <CampoArea
                    etiqueta="Motivo"
                    rows={2}
                    maxLength={500}
                    ayuda="Queda en la auditoría. Por ejemplo: «Ya no trabaja en la empresa»."
                    error={errors.motivo?.message}
                    {...form.register("motivo")}
                />
                {error ? <AvisoAccion error={error} que="desactivar al usuario" recargando={recargando} recargar={recargar} /> : null}
            </form>
        </DialogoConfirmar>
    );
}

/** Error de editar o desactivar: 409 con «Recargar», último administrador, 403 y 404 claros. */
function AvisoAccion({ error, que, recargando, recargar }: { error: unknown; que: string; recargando: boolean; recargar: () => void }) {
    const estado = error instanceof ErrorApi ? error.estado : undefined;
    const codigo = error instanceof ErrorApi ? error.codigo : undefined;
    let titulo: string;
    let detalle: ReactNode = mensajeDeError(error).detalle;
    if (estado === 409 && codigo !== "correo_en_uso" && codigo !== "idempotencia_en_proceso") {
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
    if (codigo === "ultimo_admin") {
        titulo = "Es el último administrador activo";
        detalle = "Iris no puede quedarse sin nadie que lo administre. Da el rol de administrador a otra persona activa y vuelve a intentarlo.";
    } else if (codigo === "usuario_inactivo") {
        titulo = "Este usuario ya está desactivado";
        detalle = "No hay nada que desactivar: no se cambió nada.";
    } else if (codigo === "autodesactivacion") {
        titulo = "No puedes desactivar tu propio usuario";
        detalle = "Pídeselo a otro administrador.";
    } else if (estado === 404) {
        titulo = "Este usuario ya no está en tu alcance";
        detalle = "Puede que lo hayan movido a una empresa en que no operas o que ya no exista. No se cambió nada.";
    } else if (estado === 403) {
        titulo = `No puedes ${que}`;
        detalle = mensajeDeError(error).detalle ?? "Tiene permisos o empresas que tú no administras. Pídeselo a un administrador global.";
    } else if (codigo === "sin_conexion" || codigo === "api_no_disponible") {
        titulo = "No pudimos contactar a Iris";
        detalle = "Puede que no se haya aplicado. Revisa tu conexión y vuelve a enviar: se usa la misma clave, así que no se aplicará dos veces.";
    } else {
        titulo = `No se pudo ${que}: ${mensajeDeError(error).titulo}`;
    }
    return <ErrorFormulario titulo={titulo} detalle={detalle} />;
}
