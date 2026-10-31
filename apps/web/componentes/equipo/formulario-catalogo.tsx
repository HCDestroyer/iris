"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, Casilla, DialogoCerrar, DialogoPie } from "@iris/ui";
import { RotateCw } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { DialogoFormulario } from "@/componentes/cierre-seguro";
import { erroresDe, esquemaCuadrilla, esquemaZona, type ValoresCuadrilla, type ValoresZona } from "@/lib/agenda/esquemas";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import type { Cuadrilla, Zona } from "@/lib/despacho/calendario";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";

function AvisoError({ error, recargar, recargando, que }: { error: unknown; recargar?: () => void; recargando: boolean; que: string }) {
    if (!error) return null;
    if (error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso" && recargar) {
        return (
            <Aviso
                tono="warning"
                titulo="Alguien más hizo cambios"
                accion={
                    <Boton variante="secundario" tamano="sm" onClick={recargar} disabled={recargando}>
                        <RotateCw />
                        {recargando ? "Recargando…" : "Recargar"}
                    </Boton>
                }
            >
                Recarga para tomar la versión nueva; lo que escribiste se conserva.
            </Aviso>
        );
    }
    const texto =
        error instanceof ErrorApi && error.estado === 403
            ? "Administrar el catálogo exige el permiso sin límite de zona o cuadrilla. Pide a un administrador un rol con alcance global."
            : [mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ");
    return (
        <Aviso tono="error" titulo={`No se pudo guardar ${que}`}>
            {texto}
        </Aviso>
    );
}

/** Alta o edición de una cuadrilla: nombre y las zonas que atiende (ninguna marcada = todas). */
export function FormularioCuadrilla({ cuadrilla, zonas, alCerrar, alGuardar }: { cuadrilla?: Cuadrilla | undefined; zonas: readonly Zona[]; alCerrar: () => void; alGuardar: (c: Cuadrilla) => void }) {
    const [antes, setAntes] = useState(cuadrilla);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const form = useForm<ValoresCuadrilla>({
        resolver: zodResolver(esquemaCuadrilla),
        defaultValues: { nombre: cuadrilla?.nombre ?? "", zonaIds: (cuadrilla?.zonaIds ?? []).map(String) },
        mode: "onTouched",
    });
    const valores = useWatch({ control: form.control }) as ValoresCuadrilla;
    const vivos = erroresDe(esquemaCuadrilla.safeParse(valores));
    const { errors, isSubmitting, isDirty, touchedFields, isSubmitted } = form.formState;
    const visibles = zonas.filter((z) => z.activa || (antes?.zonaIds ?? []).includes(z.id));

    async function guardar(v: ValoresCuadrilla) {
        setError(null);
        const zonaIds = v.zonaIds.map(Number);
        try {
            const params = { header: { "Idempotency-Key": clave.actual() } };
            const c = antes
                ? await resolver(api().POST("/api/v1/cuadrillas/{id}/editar", { params: { ...params, path: { id: antes.id } }, body: { version: antes.version, nombre: v.nombre.trim(), zonaIds } }))
                : await resolver(api().POST("/api/v1/cuadrillas", { params, body: { nombre: v.nombre.trim(), zonaIds } }));
            clave.renovar();
            alGuardar(c);
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos<ValoresCuadrilla>(e, ["nombre", "zonaIds"], form.setError)) setError(e);
        }
    }

    async function recargar() {
        if (!antes) return;
        setRecargando(true);
        try {
            const todas = await resolver(api().GET("/api/v1/cuadrillas", { params: { query: { todas: true } } }));
            const nueva = (todas.data ?? []).find((c) => c.id === antes.id);
            if (nueva) setAntes(nueva);
            setError(null);
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    return (
        <DialogoFormulario titulo={antes ? `Editar ${antes.nombre}` : "Nueva cuadrilla"} descripcion="Equipo de técnicos y las zonas que atiende." sucio={isDirty} alCerrar={alCerrar} data-testid="formulario-cuadrilla">
            <form noValidate className="grid gap-4" aria-label={antes ? "Editar cuadrilla" : "Nueva cuadrilla"} onSubmit={form.handleSubmit(guardar)}>
                <Campo etiqueta="Nombre" maxLength={120} error={errors.nombre?.message ?? (touchedFields.nombre || isSubmitted ? vivos.nombre : undefined)} {...form.register("nombre")} />
                <fieldset className="grid gap-2">
                    <legend className="mb-1 text-sm font-medium">Zonas que atiende</legend>
                    <p className="text-sm text-muted-foreground">Sin ninguna marcada, la cuadrilla atiende todas las zonas.</p>
                    {visibles.length === 0 ? (
                        <p className="text-sm text-muted-foreground">No hay zonas activas.</p>
                    ) : (
                        <div className="grid max-h-56 gap-2 overflow-y-auto sm:grid-cols-2">
                            {visibles.map((z) => (
                                <Casilla
                                    key={z.id}
                                    etiqueta={z.activa ? z.nombre : `${z.nombre} (inactiva)`}
                                    checked={(valores.zonaIds ?? []).includes(String(z.id))}
                                    onChange={(e) => {
                                        const actuales = new Set(form.getValues("zonaIds"));
                                        if (e.target.checked) actuales.add(String(z.id));
                                        else actuales.delete(String(z.id));
                                        const orden = (antes?.zonaIds ?? []).map(String);
                                        // Mismo orden que la lectura: desmarcar y volver a marcar no deja el formulario «sucio».
                                        const lista = [...actuales].sort((a, b) => (orden.indexOf(a) + 1 || Infinity) - (orden.indexOf(b) + 1 || Infinity) || Number(a) - Number(b));
                                        form.setValue("zonaIds", lista, { shouldDirty: true, shouldValidate: true });
                                    }}
                                />
                            ))}
                        </div>
                    )}
                </fieldset>
                <AvisoError error={error} recargar={antes ? recargar : undefined} recargando={recargando} que="la cuadrilla" />
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Volver
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting || Object.keys(vivos).length > 0 || (antes !== undefined && !isDirty)}>
                        {isSubmitting ? "Guardando…" : antes ? "Guardar cambios" : "Crear cuadrilla"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoFormulario>
    );
}

/** Alta de una zona local o cambio de nombre (las de Argos no se renombran en Iris). */
export function FormularioZona({ zona, alCerrar, alGuardar }: { zona?: Zona | undefined; alCerrar: () => void; alGuardar: (z: Zona) => void }) {
    const [antes, setAntes] = useState(zona);
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const form = useForm<ValoresZona>({ resolver: zodResolver(esquemaZona), defaultValues: { nombre: zona?.nombre ?? "" }, mode: "onTouched" });
    const valores = useWatch({ control: form.control }) as ValoresZona;
    const vivos = erroresDe(esquemaZona.safeParse(valores));
    const { errors, isSubmitting, isDirty, touchedFields, isSubmitted } = form.formState;

    async function guardar(v: ValoresZona) {
        setError(null);
        try {
            const params = { header: { "Idempotency-Key": clave.actual() } };
            const z = antes
                ? await resolver(api().POST("/api/v1/zonas/{id}/editar", { params: { ...params, path: { id: antes.id } }, body: { version: antes.version, nombre: v.nombre.trim() } }))
                : await resolver(api().POST("/api/v1/zonas", { params, body: { nombre: v.nombre.trim() } }));
            clave.renovar();
            alGuardar(z);
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos<ValoresZona>(e, ["nombre"], form.setError)) setError(e);
        }
    }

    async function recargar() {
        if (!antes) return;
        setRecargando(true);
        try {
            const todas = await resolver(api().GET("/api/v1/zonas", { params: { query: { todas: true } } }));
            const nueva = (todas.data ?? []).find((z) => z.id === antes.id);
            if (nueva) setAntes(nueva);
            setError(null);
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    return (
        <DialogoFormulario titulo={antes ? `Editar ${antes.nombre}` : "Nueva zona"} descripcion="Zona local de Iris (las de Argos llegan solas de su catálogo)." sucio={isDirty} alCerrar={alCerrar} data-testid="formulario-zona">
            <form noValidate className="grid gap-4" aria-label={antes ? "Editar zona" : "Nueva zona"} onSubmit={form.handleSubmit(guardar)}>
                <Campo etiqueta="Nombre" maxLength={120} error={errors.nombre?.message ?? (touchedFields.nombre || isSubmitted ? vivos.nombre : undefined)} {...form.register("nombre")} />
                <AvisoError error={error} recargar={antes ? recargar : undefined} recargando={recargando} que="la zona" />
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Volver
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting || Object.keys(vivos).length > 0 || !isDirty}>
                        {isSubmitting ? "Guardando…" : antes ? "Guardar cambios" : "Crear zona"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoFormulario>
    );
}
