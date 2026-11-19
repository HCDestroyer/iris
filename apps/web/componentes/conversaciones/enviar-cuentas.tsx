"use client";

import { Aviso, avisar, Boton, Cargando, Dialogo, DialogoCerrar, DialogoContenido, DialogoPie } from "@iris/ui";
import { Landmark, RotateCw, Send } from "lucide-react";
import { useState } from "react";
import { api, resolver } from "@/lib/api/cliente";
import { ErrorApi, mensajeDeError } from "@/lib/api/errores";
import { EN_OTRA_EMPRESA, esEnOtraEmpresa } from "@/lib/conversaciones/atencion";
import { cabeceraIdem } from "@/lib/conversaciones/api";
import { leerTextoCuentas, textoSinTitular } from "@/lib/conversaciones/cuentas";
import { useIdEnvio } from "@/lib/conversaciones/id-envio";
import { esSesionCambiada, verificarSesion, type Identidad } from "@/lib/conversaciones/sesion";
import type { Conversacion, Mensaje } from "@/lib/conversaciones/tipos";
import { useLectura } from "@/lib/lectura";

/** Por qué no se mandaron las cuentas, en llano. */
export function textoErrorCuentas(e: unknown): string {
    if (esSesionCambiada(e)) return "La sesión de este navegador cambió: no se envió.";
    if (esEnOtraEmpresa(e)) return EN_OTRA_EMPRESA.texto;
    if (!(e instanceof ErrorApi)) return mensajeDeError(e).titulo;
    switch (e.codigo) {
        case "fuera_de_ventana":
            return "Pasaron más de 24 h desde el último mensaje del cliente: solo se le pueden mandar plantillas.";
        case "sin_cuentas_para_enviar":
            return "No hay cuentas para enviar: en Argos falta el titular de las cuentas o no hay cuentas activas en quetzales.";
        case "argos_no_disponible":
            return "Argos no responde y las cuentas guardadas tienen más de 24 horas. Vuelve a intentarlo en un momento.";
        case "conector_apagado":
            return "La conexión con Argos está apagada: las cuentas salen de Argos.";
        default:
            return mensajeDeError(e).titulo;
    }
}

/**
 * «Enviar cuentas» (contrato con Argos 0.8.0 §7.8): muestra el texto fijo con las cuentas de Argos de
 * la empresa de la conversación y lo manda al cliente. La persona no lo escribe ni lo edita: el
 * servidor vuelve a armarlo al enviar.
 */
export function BotonEnviarCuentas({ conv, identidad, alEnviado, deshabilitado = false }: { conv: Conversacion; identidad: Identidad; alEnviado: (m: Mensaje) => void; deshabilitado?: boolean }) {
    const [abierto, setAbierto] = useState(false);
    return (
        <>
            <Boton
                variante="fantasma"
                tamano="icono-sm"
                className="text-muted-foreground"
                aria-label="Cuentas bancarias"
                title="Enviar las cuentas para depositar"
                disabled={deshabilitado}
                onClick={() => setAbierto(true)}
                data-slot="enviar-cuentas"
            >
                <Landmark />
            </Boton>
            {abierto ? <DialogoEnviarCuentas conv={conv} identidad={identidad} cerrar={() => setAbierto(false)} alEnviado={alEnviado} /> : null}
        </>
    );
}

export function DialogoEnviarCuentas({ conv, identidad, cerrar, alEnviado }: { conv: Conversacion; identidad: Identidad; cerrar: () => void; alEnviado: (m: Mensaje) => void }) {
    const lectura = useLectura((signal) => leerTextoCuentas(conv.id, signal));
    const envio = useIdEnvio();
    const [enviando, setEnviando] = useState(false);
    const [error, setError] = useState<unknown>(null);
    const datos = lectura.estado === "listo" ? lectura.datos : null;

    async function enviar() {
        setError(null);
        setEnviando(true);
        const id = envio.actual();
        try {
            await verificarSesion(identidad);
            const m = await resolver(
                api().POST("/api/v1/mensajeria/conversaciones/{id}/mensajes", {
                    params: { path: { id: conv.id }, header: cabeceraIdem(id) },
                    body: { id, cuentas: true },
                }),
            );
            envio.renovar();
            if (m) alEnviado(m);
            avisar({ titulo: "Cuentas enviadas al cliente" });
            cerrar();
        } catch (e) {
            envio.trasError(e);
            setError(e);
        } finally {
            setEnviando(false);
        }
    }

    return (
        <Dialogo open onOpenChange={(a) => !a && !enviando && cerrar()}>
            <DialogoContenido titulo="Enviar cuentas" descripcion="Las cuentas de la empresa para depositar, tal como están en Argos. Iris pone el texto: no se escribe a mano.">
                <div className="grid gap-3" data-slot="vista-cuentas">
                    {lectura.estado === "cargando" ? <Cargando texto="Leyendo las cuentas de Argos…" visible /> : null}
                    {lectura.estado === "error" ? (
                        <Aviso
                            tono="error"
                            titulo="No se pudieron leer las cuentas"
                            accion={
                                <Boton variante="secundario" tamano="sm" onClick={lectura.reintentar}>
                                    <RotateCw />
                                    Reintentar
                                </Boton>
                            }
                        >
                            {textoErrorCuentas(lectura.error)}
                        </Aviso>
                    ) : null}
                    {datos && datos.texto ? (
                        <p className="rounded-lg bg-background-secondary p-3 text-sm whitespace-pre-wrap [overflow-wrap:anywhere]" data-testid="texto-cuentas">
                            {datos.texto}
                        </p>
                    ) : null}
                    {datos && !datos.texto ? (
                        <Aviso tono="warning" titulo="No hay cuentas para enviar">
                            {datos.sinTitular > 0
                                ? "Las cuentas de la empresa no tienen titular en Argos. Pide a un administrador que lo escriba en Argos: sin titular, Iris no se las da al cliente."
                                : "La empresa no tiene cuentas activas en quetzales en Argos."}
                        </Aviso>
                    ) : null}
                    {datos && datos.texto && datos.sinTitular > 0 ? <p className="text-xs text-muted-foreground">{textoSinTitular(datos.sinTitular)}</p> : null}
                    {error ? (
                        <p role="alert" className="text-sm text-error">
                            {textoErrorCuentas(error)}
                        </p>
                    ) : null}
                </div>
                <DialogoPie>
                    <DialogoCerrar asChild>
                        <Boton variante="contorno" disabled={enviando}>
                            Cancelar
                        </Boton>
                    </DialogoCerrar>
                    <Boton onClick={() => void enviar()} disabled={!datos?.texto || enviando}>
                        {enviando ? <Cargando texto="Enviando…" /> : <Send />}
                        {enviando ? null : "Enviar al cliente"}
                    </Boton>
                </DialogoPie>
            </DialogoContenido>
        </Dialogo>
    );
}
