"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Cargando, CampoArea, EstadoVacio, GrupoOpciones, Tarjeta, TarjetaContenido, TarjetaDescripcion, TarjetaEncabezado, TarjetaTitulo } from "@iris/ui";
import { Ban, CircleCheck, Clock, LinkIcon, RotateCw, Send, TimerOff, WifiOff } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { Controller, useForm, useWatch } from "react-hook-form";
import type { z } from "zod";
import { ErrorApi } from "@/lib/api/errores";
import { consultarEncuesta, responderEncuesta, type EncuestaPublica as Datos } from "@/lib/encuestas/api-publica";
import { CALIFICACIONES, MAX_COMENTARIO, TEXTO_CALIFICACION, textoCalificacion } from "@/lib/encuestas/presentacion";
import { cuerpoRespuesta, esquemaRespuestaEncuesta, pasoPorProblema, tokenDesdeFragmento, type DatosRespuestaEncuesta, type PasoEncuesta } from "@/lib/encuestas/publica";

const OPCIONES = CALIFICACIONES.map((c) => ({ valor: c, etiqueta: TEXTO_CALIFICACION[c].texto, icono: TEXTO_CALIFICACION[c].icono, detalle: `${c} de 5` }));

/** «28 de octubre de 2026» en la hora de Guatemala. */
function fechaLarga(iso: string): string {
    const d = new Date(iso);
    if (Number.isNaN(d.getTime())) return "";
    return new Intl.DateTimeFormat("es-GT", { timeZone: "America/Guatemala", day: "numeric", month: "long", year: "numeric" }).format(d);
}

function estadoInicial(): { paso: PasoEncuesta; token: string | null } {
    const leido = tokenDesdeFragmento(window.location.hash);
    if (leido === null) return { paso: "sin_enlace", token: null };
    if ("invalido" in leido) return { paso: "invalido", token: null };
    return { paso: "cargando", token: leido.token };
}

function pasoPorEstado(estado: string): PasoEncuesta {
    if (estado === "pendiente") return "formulario";
    if (estado === "respondida") return "ya_respondida";
    if (estado === "anulada") return "anulada";
    return "vencida";
}

/**
 * Encuesta pública (`/encuesta#<token>`). El token se lee del fragmento, se borra de la URL antes
 * de pedir nada y solo vive en memoria; no se manda a ningún otro lado. Sin sesión ni cookies.
 */
export function EncuestaPublica() {
    const [inicio] = useState(estadoInicial);
    const token = useRef<string | null>(inicio.token);
    const [paso, setPaso] = useState<PasoEncuesta>(inicio.paso);
    const [datos, setDatos] = useState<Datos | null>(null);
    const [espera, setEspera] = useState<number | undefined>(undefined);
    const [errorEnvio, setErrorEnvio] = useState<{ sinConexion: boolean; detalle: string | undefined } | null>(null);
    const [sinConexion, setSinConexion] = useState(false);
    const consultado = useRef(false);

    const form = useForm<z.input<typeof esquemaRespuestaEncuesta>, unknown, DatosRespuestaEncuesta>({
        resolver: zodResolver(esquemaRespuestaEncuesta),
        defaultValues: { calificacion: undefined, comentario: "" },
    });
    const comentario = useWatch({ control: form.control, name: "comentario" }) ?? "";

    const fallo = useCallback((error: unknown) => {
        if (error instanceof ErrorApi && error.estado !== 0 && error.estado < 500) {
            if (error.estado === 429) setEspera(error.reintentarEn);
            const p = pasoPorProblema(error.estado, error.codigo);
            if (p !== "error") token.current = null;
            setPaso(p);
            return;
        }
        setSinConexion(error instanceof ErrorApi && (error.codigo === "sin_conexion" || error.codigo === "api_no_disponible"));
        setPaso("error");
    }, []);

    const consultar = useCallback(async () => {
        const t = token.current;
        if (!t) return;
        setPaso("cargando");
        try {
            const d = await consultarEncuesta(t);
            setDatos(d);
            setPaso(pasoPorEstado(d.estado));
        } catch (error) {
            fallo(error);
        }
    }, [fallo]);

    useEffect(() => {
        const limpiarUrl = () => {
            // Fuera de la URL antes de nada: no queda en el historial, ni al recargar ni al compartir.
            if (window.location.hash) {
                window.history.replaceState(window.history.state, "", `${window.location.pathname}${window.location.search}`);
            }
        };
        limpiarUrl();
        if (token.current && !consultado.current) {
            consultado.current = true;
            void consultar();
        }
        // Otro enlace abierto en la misma pestaña (solo cambia el fragmento): se empieza de nuevo.
        const alCambiarFragmento = () => {
            const leido = tokenDesdeFragmento(window.location.hash);
            limpiarUrl();
            if (leido === null) return;
            setDatos(null);
            setErrorEnvio(null);
            form.reset({ calificacion: undefined, comentario: "" });
            if ("invalido" in leido) {
                token.current = null;
                setPaso("invalido");
                return;
            }
            token.current = leido.token;
            void consultar();
        };
        window.addEventListener("hashchange", alCambiarFragmento);
        return () => window.removeEventListener("hashchange", alCambiarFragmento);
    }, [consultar, form]);

    async function enviar(d: DatosRespuestaEncuesta) {
        const t = token.current;
        if (!t) return;
        setErrorEnvio(null);
        try {
            const r = await responderEncuesta(t, cuerpoRespuesta(d));
            setDatos(r);
            token.current = null;
            setPaso("gracias");
        } catch (error) {
            if (error instanceof ErrorApi && error.estado === 422) {
                const campo = error.errorDeCampo("calificacion");
                const com = error.errorDeCampo("comentario");
                if (campo) form.setError("calificacion", { message: campo });
                if (com) form.setError("comentario", { message: com });
                if (!campo && !com) setErrorEnvio({ sinConexion: false, detalle: error.detalle ?? error.titulo });
                return;
            }
            if (error instanceof ErrorApi && error.estado !== 0 && error.estado < 500) {
                fallo(error);
                return;
            }
            setErrorEnvio({
                sinConexion: error instanceof ErrorApi && (error.codigo === "sin_conexion" || error.codigo === "api_no_disponible"),
                detalle: error instanceof ErrorApi ? error.detalle : undefined,
            });
        }
    }

    const encabezado = datos ? (
        <p className="text-sm text-muted-foreground" data-testid="datos-encuesta">
            {datos.entidad === "ticket" ? "Ticket" : "Orden"} <span className="font-medium text-foreground">{datos.numero}</span>
            {fechaLarga(datos.fecha) ? ` · ${fechaLarga(datos.fecha)}` : ""}
            {datos.empresa ? ` · ${datos.empresa}` : ""}
        </p>
    ) : null;

    switch (paso) {
        case "cargando":
            return (
                <Tarjeta className="grid place-items-center py-16">
                    <Cargando texto="Abriendo la encuesta…" visible />
                </Tarjeta>
            );
        case "formulario":
            return (
                <Tarjeta>
                    <TarjetaEncabezado>
                        <TarjetaTitulo>
                            <span className="text-xl">¿Cómo te atendimos?</span>
                        </TarjetaTitulo>
                        <TarjetaDescripcion>Son dos preguntas y te toma menos de un minuto.</TarjetaDescripcion>
                        {encabezado}
                    </TarjetaEncabezado>
                    <TarjetaContenido>
                        <form className="grid gap-5" noValidate onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                            <Controller
                                control={form.control}
                                name="calificacion"
                                render={({ field, fieldState }) => (
                                    <GrupoOpciones
                                        leyenda="Califica la atención"
                                        nombre="calificacion"
                                        opciones={OPCIONES}
                                        valor={field.value as (typeof CALIFICACIONES)[number] | undefined}
                                        alCambiar={(v) => field.onChange(v)}
                                        error={fieldState.error?.message}
                                        disabled={form.formState.isSubmitting}
                                    />
                                )}
                            />
                            <CampoArea
                                etiqueta="¿Algo que quieras contarnos? (opcional)"
                                rows={4}
                                maxLength={MAX_COMENTARIO}
                                ayuda={`${comentario.length} de ${MAX_COMENTARIO} caracteres`}
                                error={form.formState.errors.comentario?.message}
                                disabled={form.formState.isSubmitting}
                                {...form.register("comentario")}
                            />
                            {errorEnvio ? (
                                <Aviso tono="error" titulo="No se pudo enviar tu respuesta" icono={errorEnvio.sinConexion ? WifiOff : undefined}>
                                    {errorEnvio.sinConexion ? "No hay conexión. Revisa tu red y vuelve a enviar: tu respuesta sigue aquí." : `${errorEnvio.detalle ?? "Inténtalo de nuevo en un momento"}. Tu respuesta sigue aquí.`}
                                </Aviso>
                            ) : null}
                            <Boton type="submit" className="justify-self-start" disabled={form.formState.isSubmitting}>
                                <Send />
                                {form.formState.isSubmitting ? "Enviando…" : errorEnvio ? "Reintentar" : "Enviar respuesta"}
                            </Boton>
                        </form>
                    </TarjetaContenido>
                </Tarjeta>
            );
        case "gracias":
            return (
                <Tarjeta data-paso="gracias">
                    <EstadoVacio
                        icono={CircleCheck}
                        titulo="¡Gracias por tu respuesta!"
                        descripcion={
                            <>
                                Calificaste la atención con {textoCalificacion(datos?.calificacion)}. Ya puedes cerrar esta página.
                                {encabezado ? <span className="mt-2 block">{encabezado}</span> : null}
                            </>
                        }
                    />
                </Tarjeta>
            );
        case "ya_respondida":
            return (
                <Tarjeta data-paso="ya_respondida">
                    <EstadoVacio
                        icono={CircleCheck}
                        titulo="Esta encuesta ya fue respondida"
                        descripcion={
                            <>
                                {datos?.calificacion ? `La calificación registrada es ${textoCalificacion(datos.calificacion)}. ` : ""}
                                Cada encuesta se responde una sola vez. ¡Gracias!
                            </>
                        }
                    />
                </Tarjeta>
            );
        case "vencida":
            return (
                <Tarjeta data-paso="vencida">
                    <EstadoVacio icono={TimerOff} titulo="El enlace de la encuesta venció" descripcion="Las encuestas se pueden responder durante 7 días. Si quieres contarnos algo, escríbenos por el canal de soporte de siempre." />
                </Tarjeta>
            );
        case "anulada":
            return (
                <Tarjeta data-paso="anulada">
                    <EstadoVacio icono={Ban} titulo="Esta encuesta ya no está disponible" descripcion="El caso se volvió a abrir o se unió a otro, así que la encuesta se anuló. Cuando quede resuelto te podremos enviar una nueva." />
                </Tarjeta>
            );
        case "invalido":
            return (
                <Tarjeta data-paso="invalido">
                    <EstadoVacio icono={LinkIcon} titulo="El enlace no es válido" descripcion="Revisa que lo hayas abierto completo desde el mensaje que te enviamos." />
                </Tarjeta>
            );
        case "sin_enlace":
            return (
                <Tarjeta data-paso="sin_enlace">
                    <EstadoVacio icono={LinkIcon} titulo="Abre la encuesta desde tu enlace" descripcion="Esta página funciona con el enlace personal que te enviamos al cerrar tu caso." />
                </Tarjeta>
            );
        case "limite":
            return (
                <Tarjeta data-paso="limite">
                    <EstadoVacio
                        icono={Clock}
                        titulo="Demasiados intentos"
                        descripcion={`Hay muchos intentos en este momento. Vuelve a abrir tu enlace ${espera ? `en ${Math.max(1, Math.ceil(espera / 60))} min` : "en unos minutos"}.`}
                    />
                </Tarjeta>
            );
        case "error":
            return (
                <Aviso
                    tono="error"
                    titulo="No se pudo abrir la encuesta"
                    icono={sinConexion ? WifiOff : undefined}
                    accion={
                        <Boton variante="secundario" tamano="sm" onClick={() => void consultar()}>
                            <RotateCw />
                            Reintentar
                        </Boton>
                    }
                >
                    {sinConexion ? "No hay conexión. Revisa tu red e inténtalo de nuevo." : "Algo salió mal de nuestro lado. Inténtalo de nuevo en un momento."}
                </Aviso>
            );
    }
}
