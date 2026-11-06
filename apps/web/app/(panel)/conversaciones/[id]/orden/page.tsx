"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { avisar, Aviso, Campo, CampoArea, CampoSelector, FormularioPagina, liberarProteccion, SeccionFormulario } from "@iris/ui";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { AvisoAccion, useEnvio, useZonas } from "@/componentes/comercial/comun";
import { DesdeConversacion } from "@/componentes/conversaciones/desde-conversacion";
import { api, resolver } from "@/lib/api/cliente";
import type { ConversacionDetalle } from "@/lib/conversaciones/tipos";

const TIPOS = [
    ["visita", "Visita técnica"],
    ["instalacion", "Instalación"],
    ["cambio_equipo", "Cambio de equipo"],
    ["mantenimiento", "Mantenimiento"],
    ["retiro", "Retiro"],
] as const;

/** Espejo de `EntradaOrdenConv` del servidor. */
const esquema = z.object({
    tipo: z.enum(["instalacion", "visita", "retiro", "cambio_equipo", "mantenimiento"]),
    servicioId: z.string(),
    ticketId: z.string(),
    descripcion: z.string().max(4000, "Hasta 4000 caracteres"),
    direccion: z.string().max(300, "Hasta 300 caracteres"),
    zonaId: z.string(),
});
type Valores = z.infer<typeof esquema>;

/** Orden de trabajo desde la conversación (§6): cliente y servicio de la conversación, ticket ligado por defecto. */
export default function Pagina() {
    return <DesdeConversacion permisos={["ordenes.crear"]}>{(c) => <FormularioOrden conv={c} />}</DesdeConversacion>;
}

function FormularioOrden({ conv }: { conv: ConversacionDetalle }) {
    const router = useRouter();
    const envio = useEnvio();
    const zonas = useZonas();
    const servicios = conv.clienteResumen?.servicios ?? [];
    const tickets = (conv.vinculos ?? []).filter((v) => v.tipo === "ticket");
    const volverA = `/conversaciones?c=${conv.id}`;
    const form = useForm<Valores>({
        resolver: zodResolver(esquema),
        defaultValues: {
            tipo: "visita",
            servicioId: String(conv.contacto.servicioId ?? servicios[0]?.id ?? ""),
            ticketId: tickets.at(-1)?.id ?? "",
            descripcion: conv.ultimoMensaje?.direccion === "entrante" ? (conv.ultimoMensaje.texto ?? "") : "",
            direccion: "",
            zonaId: "",
        },
    });
    const errores = form.formState.errors;

    async function guardar(d: Valores) {
        const r = await envio.enviar(
            (clave) =>
                resolver(
                    api().POST("/api/v1/mensajeria/conversaciones/{id}/crear-orden", {
                        params: { path: { id: conv.id }, header: { "Idempotency-Key": clave } },
                        body: {
                            tipo: d.tipo,
                            ...(d.descripcion.trim() ? { descripcion: d.descripcion.trim() } : {}),
                            ...(d.servicioId ? { servicioId: Number(d.servicioId) } : {}),
                            ...(d.ticketId ? { ticketId: Number(d.ticketId) } : {}),
                            ...(d.direccion.trim() ? { direccion: d.direccion.trim() } : {}),
                            ...(d.zonaId ? { zonaId: Number(d.zonaId) } : {}),
                        },
                    }),
                ),
            { nombres: ["tipo", "servicioId", "ticketId", "descripcion", "direccion", "zonaId"], setError: form.setError },
        );
        if (r) {
            avisar({ titulo: `Orden ${r.vinculo.referencia} creada` });
            await liberarProteccion();
            router.replace(volverA);
        }
    }

    return (
        <FormularioPagina
            titulo="Nueva orden"
            descripcion={`Para ${conv.titulo}: queda ligada a la conversación.`}
            alEnviar={form.handleSubmit(guardar)}
            navegar={router.push}
            volverA={volverA}
            sucio={form.formState.isDirty}
            barra={{ textoGuardar: "Crear orden", textoEnviando: "Creando…", enviando: form.formState.isSubmitting }}
            error={<AvisoAccion envio={envio} que="crear la orden" recargar={async () => undefined} />}
        >
            {servicios.length === 0 ? <Aviso tono="warning" titulo="La conversación no tiene un servicio de Argos: escribe la dirección." /> : null}
            <SeccionFormulario titulo="1. Qué y para qué servicio">
                <div className="grid gap-4 sm:grid-cols-2">
                    <CampoSelector etiqueta="Tipo" error={errores.tipo?.message} {...form.register("tipo")}>
                        {TIPOS.map(([v, t]) => (
                            <option key={v} value={v}>
                                {t}
                            </option>
                        ))}
                    </CampoSelector>
                    <CampoSelector etiqueta="Servicio" error={errores.servicioId?.message} disabled={servicios.length === 0} {...form.register("servicioId")}>
                        <option value="">Sin servicio</option>
                        {servicios.map((s) => (
                            <option key={s.id} value={String(s.id)}>
                                {s.plan} · {s.codigo}
                            </option>
                        ))}
                    </CampoSelector>
                </div>
                <CampoSelector etiqueta="Ticket de origen" error={errores.ticketId?.message} {...form.register("ticketId")}>
                    <option value="">Sin ticket</option>
                    {tickets.map((t) => (
                        <option key={t.id} value={t.id}>
                            {t.referencia}
                        </option>
                    ))}
                </CampoSelector>
                <CampoArea etiqueta="Descripción" rows={4} maxLength={4000} error={errores.descripcion?.message} {...form.register("descripcion")} />
            </SeccionFormulario>
            <SeccionFormulario titulo="2. Dónde" descripcion="Si el servicio es de Argos, la dirección y la zona salen de su ficha.">
                <Campo etiqueta="Dirección (opcional)" maxLength={300} autoComplete="off" error={errores.direccion?.message} {...form.register("direccion")} />
                <CampoSelector etiqueta="Zona (opcional)" error={errores.zonaId?.message} disabled={zonas.estado !== "listo"} {...form.register("zonaId")}>
                    <option value="">La del servicio</option>
                    {((zonas.estado === "listo" ? zonas.datos : null) ?? []).filter((z) => z.activa).map((z) => (
                        <option key={z.id} value={String(z.id)}>
                            {z.nombre}
                        </option>
                    ))}
                </CampoSelector>
            </SeccionFormulario>
        </FormularioPagina>
    );
}
