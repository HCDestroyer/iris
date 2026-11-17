"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { Aviso, Boton, Campo, Cargando, cn, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie } from "@iris/ui";
import { CircleAlert, CircleCheck, Send } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { probarCorreo } from "@/lib/correo/api";
import { entradaPrueba, esquemaPrueba, reglaDeCorreo, textoDemasiados, textoResultado, type Ambito, type ValoresCorreo, type ValoresPrueba } from "@/lib/correo/correo";
import type { AjusteCorreo, ResultadoPrueba } from "@/lib/correo/tipos";
import { useSesion } from "@/lib/sesion";

type Estado = { tipo: "nada" } | { tipo: "hecho"; resultado: ResultadoPrueba; para: string } | { tipo: "espera"; texto: string } | { tipo: "error"; texto: string };

/**
 * «Enviar correo de prueba» (`POST …/probar`, contratos/correo-sistema.md §3.3): un diálogo corto
 * que pide el destinatario (por defecto, el correo de quien prueba) y manda la prueba con los
 * valores del formulario **sin guardar**. El resultado se dice con icono + texto + color y la causa
 * en llano; el servidor nunca devuelve la respuesta cruda del servidor de correo. Con 429, cuánto
 * esperar.
 */
export function DialogoPrueba({ ambito, ajuste, valores, cerrar }: { ambito: Ambito; ajuste: Pick<AjusteCorreo, "tieneContrasena">; valores: ValoresCorreo; cerrar: () => void }) {
    const { yo } = useSesion();
    const [estado, setEstado] = useState<Estado>({ tipo: "nada" });
    const form = useForm<ValoresPrueba>({ resolver: zodResolver(esquemaPrueba), defaultValues: { para: yo.usuario.correo ?? "" } });

    async function enviar(d: ValoresPrueba) {
        setEstado({ tipo: "nada" });
        try {
            const para = d.para.trim();
            setEstado({ tipo: "hecho", resultado: await probarCorreo(ambito, entradaPrueba(valores, para)), para });
        } catch (e) {
            if (e instanceof ErrorApi && e.estado === 429) {
                setEstado({ tipo: "espera", texto: textoDemasiados(e.codigo, e.reintentarEn) });
            } else if (e instanceof ErrorApi && e.estado === 422 && e.errores.some((x) => x.field.endsWith("para"))) {
                form.setError("para", { message: e.errores.find((x) => x.field.endsWith("para"))?.message ?? "Revisa el correo de destino" }, { shouldFocus: true });
            } else if (e instanceof ErrorApi && e.estado === 422 && reglaDeCorreo(e.codigo, ajuste)) {
                // Una regla del servidor sobre lo del formulario: en llano, para corregirlo ahí.
                setEstado({ tipo: "error", texto: `${reglaDeCorreo(e.codigo, ajuste)?.mensaje ?? ""} Cierra y corrígelo en el formulario.` });
            } else if (e instanceof ErrorApi && e.estado === 422 && e.errores.length > 0) {
                // Un dato del formulario que el servidor no acepta: se dice cuál, tal como lo explica.
                setEstado({ tipo: "error", texto: `Revisa el formulario: ${e.errores.map((x) => x.message).join(" ")}` });
            } else setEstado({ tipo: "error", texto: `${mensajeDeError(e).titulo}. Vuelve a intentarlo.` });
        }
    }

    const enviando = form.formState.isSubmitting;
    const resultado = estado.tipo === "hecho" ? textoResultado(estado.resultado, estado.para) : null;
    return (
        <Dialogo open onOpenChange={(abierto) => !abierto && cerrar()}>
            <DialogoContenido titulo="Enviar correo de prueba" descripcion={`Sale por ${valores.host.trim()} con lo que hay en el formulario, aunque no esté guardado.`}>
                <form noValidate className="grid gap-4" onSubmit={(e) => void form.handleSubmit(enviar)(e)}>
                    <Campo
                        etiqueta="¿A qué correo lo mandamos?"
                        type="email"
                        inputMode="email"
                        maxLength={254}
                        autoComplete="email"
                        autoCapitalize="none"
                        spellCheck={false}
                        error={form.formState.errors.para?.message}
                        {...form.register("para")}
                    />
                    {enviando ? (
                        // Un servidor que no contesta tarda en fallar (unos 11 s): se dice que sigue probando y no se corta.
                        <p role="status" className="text-sm text-muted-foreground" data-slot="probando">
                            Probando… Si el servidor no contesta, puede tardar hasta medio minuto.
                        </p>
                    ) : resultado ? (
                        <div
                            role="status"
                            data-slot="resultado-prueba"
                            data-estado={resultado.ok ? "ok" : "error"}
                            data-causa={estado.tipo === "hecho" ? (estado.resultado.causa ?? undefined) : undefined}
                            className={cn("flex min-w-0 items-start gap-2 rounded-lg border px-3 py-2.5", resultado.ok ? "border-success/40 bg-success/10" : "border-error/40 bg-error/10")}
                        >
                            {resultado.ok ? <CircleCheck aria-hidden className="mt-0.5 size-[1.125rem] shrink-0 text-success" /> : <CircleAlert aria-hidden className="mt-0.5 size-[1.125rem] shrink-0 text-error" />}
                            <div className="grid min-w-0 gap-0.5">
                                <span className={cn("text-sm font-semibold", resultado.ok ? "text-success" : "text-error")}>{resultado.titulo}</span>
                                <span className="text-sm [overflow-wrap:anywhere]">{resultado.detalle}</span>
                            </div>
                        </div>
                    ) : estado.tipo === "espera" ? (
                        <Aviso tono="warning" titulo="Inténtalo más tarde" data-testid="prueba-espera">
                            {estado.texto}
                        </Aviso>
                    ) : estado.tipo === "error" ? (
                        <Aviso tono="error" titulo="No se pudo hacer la prueba" data-testid="prueba-error">
                            {estado.texto}
                        </Aviso>
                    ) : null}
                    <DialogoPie>
                        <DialogoCerrar asChild>
                            <Boton variante="contorno">Cerrar</Boton>
                        </DialogoCerrar>
                        <Boton type="submit" disabled={enviando}>
                            {enviando ? <Cargando texto="Probando…" /> : <Send />}
                            {enviando ? null : estado.tipo === "nada" ? "Enviar prueba" : "Enviar otra vez"}
                        </Boton>
                    </DialogoPie>
                </form>
            </DialogoContenido>
        </Dialogo>
    );
}
