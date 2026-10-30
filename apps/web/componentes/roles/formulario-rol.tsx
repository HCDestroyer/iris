"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoArea, Casilla, Esqueleto, InsigniaEstado, Tarjeta, TarjetaContenido, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Save, Search, ShieldAlert } from "lucide-react";
import { useMemo, useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { ErrorLectura } from "@/componentes/error-lectura";
import { esquemaRol, type DatosRol } from "@/lib/accesos/esquemas";
import { agruparPermisos, sensiblesAgregados, type PermisoInfo } from "@/lib/accesos/presentacion";
import { api, resolver } from "@/lib/api/cliente";
import { mensajeDeError } from "@/lib/api/errores";
import { erroresACampos } from "@/lib/formularios";
import { useLectura } from "@/lib/lectura";

/** Catálogo de permisos (`GET /permisos`), compartido por el formulario y la vista de un rol base. */
export function useCatalogoPermisos() {
    return useLectura<PermisoInfo[]>(async (signal) => (await resolver(api().GET("/api/v1/permisos", { signal }))).data ?? []);
}

type Props = {
    /** Valores iniciales (al editar). */
    inicial?: DatosRol;
    /** Al editar, el código no cambia. */
    edicion?: boolean;
    textoGuardar: string;
    alGuardar: (d: DatosRol) => Promise<void>;
};

/**
 * Nombre, descripción y permisos agrupados por módulo con su descripción legible. «Guardar» queda
 * deshabilitado mientras no haya ningún permiso elegido, y lo dice. Si agrega un permiso sensible,
 * avisa que se pedirá la contraseña (la API responde 403 `reautenticacion_requerida` y el panel
 * abre el diálogo).
 */
export function FormularioRol({ inicial, edicion = false, textoGuardar, alGuardar }: Props) {
    const catalogo = useCatalogoPermisos();
    const formulario = useForm<DatosRol>({
        resolver: zodResolver(esquemaRol),
        defaultValues: inicial ?? { codigo: "", nombre: "", descripcion: "", permisos: [] },
    });
    const { errors, isSubmitting } = formulario.formState;
    const elegidos = useWatch({ control: formulario.control, name: "permisos" });
    const [buscar, setBuscar] = useState("");
    const [error, setError] = useState<unknown>(null);
    // Los permisos con que abrió el formulario (el rol guardado): un sensible que ya tenía no se avisa.
    const [previos] = useState<readonly string[]>(() => inicial?.permisos ?? []);
    const grupos = useMemo(() => (catalogo.estado === "listo" ? agruparPermisos(catalogo.datos) : []), [catalogo]);

    if (catalogo.estado === "cargando") {
        return <Esqueleto className="h-96" />;
    }
    if (catalogo.estado === "error") {
        return <ErrorLectura error={catalogo.error} que="el catálogo de permisos" reintentar={catalogo.reintentar} />;
    }

    const termino = buscar.trim().toLowerCase();
    const filtrar = (p: PermisoInfo) => !termino || p.codigo.includes(termino) || p.descripcion.toLowerCase().includes(termino);
    const sensibles = sensiblesAgregados(catalogo.datos, previos, elegidos);
    const faltaPermiso = elegidos.length === 0;

    function alternar(codigos: string[], marcar: boolean) {
        const actual = new Set(formulario.getValues("permisos"));
        for (const c of codigos) {
            if (marcar) actual.add(c);
            else actual.delete(c);
        }
        formulario.setValue("permisos", [...actual].sort(), { shouldValidate: formulario.formState.isSubmitted, shouldDirty: true });
    }

    async function enviar(d: DatosRol) {
        setError(null);
        try {
            await alGuardar(d);
        } catch (e) {
            if (!erroresACampos(e, ["codigo", "nombre", "descripcion", "permisos"], formulario.setError)) {
                setError(e);
            }
        }
    }

    return (
        <form className="grid gap-4" noValidate onSubmit={(e) => void formulario.handleSubmit(enviar)(e)} aria-label="Rol">
            <Tarjeta>
                <TarjetaContenido className="grid gap-3 pt-5 sm:grid-cols-2">
                    <Campo
                        etiqueta="Código"
                        {...formulario.register("codigo")}
                        disabled={edicion}
                        error={errors.codigo?.message}
                        ayuda={edicion ? "El código no cambia." : "Minúsculas, números y guion bajo (p. ej. despacho_norte)."}
                        autoComplete="off"
                    />
                    <Campo etiqueta="Nombre" {...formulario.register("nombre")} error={errors.nombre?.message} autoComplete="off" />
                    <CampoArea etiqueta="Descripción" className="sm:col-span-2" rows={2} {...formulario.register("descripcion")} error={errors.descripcion?.message} />
                </TarjetaContenido>
            </Tarjeta>

            <Tarjeta>
                <TarjetaEncabezado className="grid gap-3">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                        <TarjetaTitulo>Permisos</TarjetaTitulo>
                        <span className="text-sm text-muted-foreground" aria-live="polite">
                            {elegidos.length === 1 ? "1 permiso elegido" : `${elegidos.length} permisos elegidos`}
                        </span>
                    </div>
                    <Campo
                        etiqueta="Buscar permiso"
                        type="search"
                        value={buscar}
                        onChange={(e) => setBuscar(e.target.value)}
                        placeholder="tickets, cerrar, ver…"
                        autoComplete="off"
                    />
                </TarjetaEncabezado>
                <TarjetaContenido className="grid gap-5">
                    {grupos.map((g) => {
                        const visibles = g.permisos.filter(filtrar);
                        if (visibles.length === 0) return null;
                        const codigos = visibles.map((p) => p.codigo);
                        const todos = codigos.every((c) => elegidos.includes(c));
                        return (
                            <fieldset key={g.modulo} className="grid gap-2" data-modulo={g.modulo}>
                                <legend className="mb-1 flex w-full flex-wrap items-center justify-between gap-2">
                                    <span className="font-medium">{g.nombre}</span>
                                    <Casilla etiqueta="Todo el módulo" checked={todos} onChange={(e) => alternar(codigos, e.target.checked)} />
                                </legend>
                                <div className="grid gap-2 md:grid-cols-2">
                                    {visibles.map((p) => (
                                        <div key={p.codigo} className="grid gap-1 rounded-md border border-border p-2">
                                            <Casilla
                                                etiqueta={p.descripcion}
                                                ayuda={<code className="text-xs">{p.codigo}</code>}
                                                checked={elegidos.includes(p.codigo)}
                                                onChange={(e) => alternar([p.codigo], e.target.checked)}
                                            />
                                            {p.sensible ? <InsigniaEstado className="justify-self-start" tono="warning" icono={ShieldAlert} texto="Sensible" /> : null}
                                        </div>
                                    ))}
                                </div>
                            </fieldset>
                        );
                    })}
                    {termino && grupos.every((g) => !g.permisos.some(filtrar)) ? (
                        <p className="inline-flex items-center gap-2 text-sm text-muted-foreground">
                            <Search aria-hidden className="size-4" />
                            Ningún permiso coincide con «{buscar}».
                        </p>
                    ) : null}
                </TarjetaContenido>
            </Tarjeta>

            {sensibles.length > 0 ? (
                <Aviso tono="warning" titulo="Incluye permisos sensibles" icono={ShieldAlert}>
                    {sensibles.length === 1 ? "Agregas 1 permiso de administración" : `Agregas ${sensibles.length} permisos de administración`}: al guardar te pediremos confirmar tu
                    contraseña.
                </Aviso>
            ) : null}
            {error ? (
                <Aviso tono="error" titulo="No se pudo guardar el rol">
                    {mensajeDeError(error).detalle}
                </Aviso>
            ) : null}
            <div className="flex flex-wrap items-center gap-3">
                <Boton type="submit" disabled={faltaPermiso || isSubmitting}>
                    <Save />
                    {isSubmitting ? "Guardando…" : textoGuardar}
                </Boton>
                {faltaPermiso ? (
                    <p className="text-sm text-muted-foreground" role="status">
                        Elige al menos un permiso para guardar el rol.
                    </p>
                ) : null}
                {errors.permisos?.message && !faltaPermiso ? <p className="text-sm text-error">{errors.permisos.message}</p> : null}
            </div>
        </form>
    );
}
