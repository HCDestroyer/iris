"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, CampoSelector, DialogoCerrar, DialogoPie } from "@iris/ui";
import { RotateCw } from "lucide-react";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";
import { DialogoFormulario } from "@/componentes/cierre-seguro";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import {
    entradaCrearTecnico,
    entradaEditarTecnico,
    erroresDe,
    esquemaTecnico,
    valoresTecnico,
    type CandidatoTecnico,
    type Tecnico,
    type TecnicoDetalle,
    type ValoresTecnico,
} from "@/lib/agenda/esquemas";
import type { Cuadrilla, Zona } from "@/lib/despacho/calendario";
import { erroresACampos } from "@/lib/formularios";
import { useClaveIdempotencia } from "@/lib/idempotencia";
import { useLectura } from "@/lib/lectura";

const CAMPOS = ["usuarioId", "cuadrillaId", "telefono", "habilidades", "bodegaId"] as const;

/** Zonas que atiende una cuadrilla, en texto. */
export function textoZonas(c: Cuadrilla | undefined, zonas: readonly Zona[] | undefined): string {
    if (!c) return "";
    const ids = c.zonaIds ?? [];
    if (ids.length === 0) return "Atiende todas las zonas";
    return `Atiende: ${ids.map((id) => zonas?.find((z) => z.id === id)?.nombre ?? `Zona ${id}`).join(", ")}`;
}

type Props = {
    /** Ficha que se edita; sin ella, alta. */
    tecnico?: Tecnico | undefined;
    cuadrillas: readonly Cuadrilla[];
    zonas: readonly Zona[] | undefined;
    alCerrar: () => void;
    alGuardar: (t: TecnicoDetalle) => void;
};

/**
 * Alta o edición de la ficha de un técnico: usuario (de `GET /tecnicos/candidatos`), cuadrilla
 * (define sus zonas), teléfono, habilidades y bodega de Argos. Sin usuario o sin cuadrilla no se
 * puede guardar; con cambios sin guardar, cerrar pregunta antes.
 */
export function FormularioTecnico({ tecnico, cuadrillas, zonas, alCerrar, alGuardar }: Props) {
    const [antes, setAntes] = useState<Tecnico | undefined>(tecnico);
    const editando = antes !== undefined;
    const clave = useClaveIdempotencia();
    const [error, setError] = useState<unknown>(null);
    const [recargando, setRecargando] = useState(false);
    const esquema = esquemaTecnico(!editando);
    const form = useForm<ValoresTecnico>({ resolver: zodResolver(esquema), defaultValues: valoresTecnico(tecnico), mode: "onTouched" });
    const valores = useWatch({ control: form.control }) as ValoresTecnico;
    const vivos = erroresDe(esquema.safeParse(valores));
    const { errors, isSubmitting, isDirty, touchedFields, isSubmitted } = form.formState;
    const candidatos = useLectura<CandidatoTecnico[]>(async (signal) => {
        if (editando) return [];
        const p = await resolver(api().GET("/api/v1/tecnicos/candidatos", { params: { query: { limit: 200 } }, signal }));
        return p.data ?? [];
    });
    const activas = cuadrillas.filter((c) => c.activa || String(c.id) === valores.cuadrillaId);
    const cuadrilla = cuadrillas.find((c) => String(c.id) === valores.cuadrillaId);
    // Las selecciones obligatorias avisan desde el inicio; los textos, al salir del campo o al enviar.
    const mensaje = (campo: (typeof CAMPOS)[number], seleccion = false) =>
        errors[campo]?.message ?? (seleccion || touchedFields[campo] || isSubmitted ? vivos[campo] : undefined);
    const conflicto = error instanceof ErrorApi && error.estado === 409 && error.codigo !== "idempotencia_en_proceso";

    async function guardar(v: ValoresTecnico) {
        setError(null);
        try {
            const params = { header: { "Idempotency-Key": clave.actual() } };
            const t = antes
                ? await resolver(api().POST("/api/v1/tecnicos/{id}/editar", { params: { ...params, path: { id: antes.id } }, body: entradaEditarTecnico(v, antes) }))
                : await resolver(api().POST("/api/v1/tecnicos", { params, body: entradaCrearTecnico(v) }));
            clave.renovar();
            alGuardar(t);
        } catch (e) {
            clave.trasError(e);
            if (!erroresACampos<ValoresTecnico>(e, CAMPOS, form.setError)) setError(e);
        }
    }

    async function recargar() {
        if (!antes) return;
        setRecargando(true);
        try {
            setAntes(await resolver(api().GET("/api/v1/tecnicos/{id}", { params: { path: { id: antes.id } } })));
            setError(null);
        } catch (e) {
            setError(e);
        } finally {
            setRecargando(false);
        }
    }

    const sinCandidatos = !editando && candidatos.estado === "listo" && candidatos.datos.length === 0;
    return (
        <DialogoFormulario
            titulo={editando ? `Editar a ${antes.nombre}` : "Nuevo técnico"}
            descripcion={editando ? antes.correo : "Da de alta la ficha de agenda de un usuario que trabaja órdenes."}
            sucio={isDirty}
            alCerrar={alCerrar}
            data-testid="formulario-tecnico"
        >
            <form noValidate className="grid gap-4" aria-label={editando ? "Editar técnico" : "Nuevo técnico"} onSubmit={form.handleSubmit(guardar)}>
                {editando ? null : candidatos.estado === "error" ? (
                    <Aviso
                        tono="error"
                        titulo="No se pudo cargar la lista de usuarios"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={candidatos.reintentar}>
                                <RotateCw />
                                Reintentar
                            </Boton>
                        }
                    >
                        {mensajeDeError(candidatos.error).titulo}
                    </Aviso>
                ) : (
                    <CampoSelector
                        etiqueta="Usuario"
                        disabled={candidatos.estado !== "listo" || sinCandidatos}
                        error={candidatos.estado === "listo" && !sinCandidatos ? mensaje("usuarioId", true) : undefined}
                        ayuda={
                            candidatos.estado === "cargando"
                                ? "Cargando usuarios…"
                                : sinCandidatos
                                  ? "No hay usuarios sin ficha que trabajen órdenes. Crea el usuario en «Usuarios» con el rol Técnico."
                                  : "Personas activas con el rol de técnico (o que ven órdenes) y aún sin ficha."
                        }
                        {...form.register("usuarioId")}
                    >
                        <option value="">Elige el usuario</option>
                        {candidatos.estado === "listo"
                            ? candidatos.datos.map((c) => (
                                  <option key={c.usuarioId} value={c.usuarioId}>
                                      {c.nombre} · {c.correo}
                                  </option>
                              ))
                            : null}
                    </CampoSelector>
                )}
                <CampoSelector
                    etiqueta="Cuadrilla"
                    error={mensaje("cuadrillaId", true)}
                    ayuda={cuadrilla ? textoZonas(cuadrilla, zonas) : activas.length === 0 ? "No hay cuadrillas activas: créalas en Administración › Cuadrillas." : undefined}
                    {...form.register("cuadrillaId")}
                >
                    <option value="">Elige la cuadrilla</option>
                    {activas.map((c) => (
                        <option key={c.id} value={c.id}>
                            {c.nombre}
                            {c.activa ? "" : " (inactiva)"}
                        </option>
                    ))}
                </CampoSelector>
                <Campo etiqueta="Teléfono" type="tel" inputMode="tel" autoComplete="tel" maxLength={30} placeholder="+502 5555-1234" error={mensaje("telefono")} {...form.register("telefono")} />
                <Campo
                    etiqueta="Habilidades"
                    placeholder="fibra, inalambrico, altura"
                    ayuda="Separadas por comas (opcional)."
                    error={mensaje("habilidades")}
                    {...form.register("habilidades")}
                />
                <Campo etiqueta="Bodega en Argos" inputMode="numeric" placeholder="Número de bodega (opcional)" error={mensaje("bodegaId")} {...form.register("bodegaId")} />

                {conflicto ? (
                    <Aviso
                        tono="warning"
                        titulo="Alguien más cambió esta ficha"
                        accion={
                            <Boton variante="secundario" tamano="sm" onClick={recargar} disabled={recargando}>
                                <RotateCw />
                                {recargando ? "Recargando…" : "Recargar"}
                            </Boton>
                        }
                    >
                        Recarga para tomar la versión nueva; lo que escribiste se conserva.
                    </Aviso>
                ) : error ? (
                    <Aviso tono="error" titulo={editando ? "No se pudo guardar la ficha" : "No se pudo crear el técnico"}>
                        {[mensajeDeError(error).titulo, mensajeDeError(error).detalle].filter(Boolean).join(". ")}
                    </Aviso>
                ) : null}
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={isSubmitting}>
                            Volver
                        </Boton>
                    </DialogoCerrar>
                    <Boton type="submit" disabled={isSubmitting || conflicto || Object.keys(vivos).length > 0 || (editando && !isDirty)}>
                        {isSubmitting ? "Guardando…" : editando ? "Guardar cambios" : "Crear técnico"}
                    </Boton>
                </DialogoPie>
            </form>
        </DialogoFormulario>
    );
}
