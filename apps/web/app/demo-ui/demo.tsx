"use client";

import {
    Autocompletar,
    avisar,
    Boton,
    Campo,
    CampoFecha,
    CampoHora,
    CampoSelector,
    FormularioPagina,
    Globo,
    PanelEmergente,
    Pestanas,
    ProveedorAvisosEmergentes,
    ProveedorRutas,
    SeccionFormulario,
    SelectorFecha,
    useCambiosSinGuardar,
} from "@iris/ui";
import { CalendarDays, Filter, MessagesSquare } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";
import { useForm, useWatch } from "react-hook-form";

const TECNICOS = [
    { valor: "1", etiqueta: "Luis Pérez", detalle: "3 órdenes hoy" },
    { valor: "2", etiqueta: "Ana Cux", detalle: "Libre" },
    { valor: "3", etiqueta: "Marta Xol", detalle: "1 orden hoy" },
];

/** El formulario reporta sus cambios con el mismo hook que en los diálogos. */
function CamposOrden() {
    const [asunto, setAsunto] = useState("");
    useCambiosSinGuardar(asunto !== "");
    return <Campo etiqueta="Asunto" value={asunto} onChange={(e) => setAsunto(e.target.value)} autoComplete="off" />;
}

type ValoresRhf = { prioridad: string; dia: string; hora: string };

/**
 * Lista, fecha y hora de HeroUI con `register` de react-hook-form: valores iniciales, `setValue`,
 * `reset` y el foco del primer campo con error (e2e/componentes-base.spec.ts).
 */
function CamposRhf() {
    const form = useForm<ValoresRhf>({ defaultValues: { prioridad: "media", dia: "2026-11-04", hora: "08:30" } });
    const [enviado, setEnviado] = useState<ValoresRhf | null>(null);
    const valores = useWatch({ control: form.control });
    const errores = form.formState.errors;
    return (
        <div className="grid gap-3" role="group" aria-label="Con react-hook-form">
            <CampoSelector etiqueta="Prioridad" error={errores.prioridad?.message} {...form.register("prioridad", { required: "Elige la prioridad" })}>
                <option value="">Sin prioridad</option>
                <option value="baja">Baja</option>
                <option value="media">Media</option>
                <option value="alta">Alta</option>
                <option value="urgente" disabled>
                    Urgente (sin permiso)
                </option>
            </CampoSelector>
            <div className="grid gap-3 sm:grid-cols-2">
                <CampoFecha etiqueta="Día" error={errores.dia?.message} {...form.register("dia", { required: "Elige el día" })} />
                <CampoHora etiqueta="Hora" error={errores.hora?.message} {...form.register("hora", { required: "Escribe la hora" })} />
            </div>
            <output data-testid="valores-rhf" className="font-mono text-xs">
                {JSON.stringify(valores)}
            </output>
            {enviado ? (
                <output data-testid="enviado-rhf" className="font-mono text-xs">
                    {JSON.stringify(enviado)}
                </output>
            ) : null}
            <div className="flex flex-wrap gap-2">
                <Boton variante="secundario" tamano="sm" onClick={() => form.reset({ prioridad: "", dia: "", hora: "" })}>
                    Vaciar
                </Boton>
                <Boton
                    variante="secundario"
                    tamano="sm"
                    onClick={() => {
                        form.setValue("prioridad", "alta", { shouldDirty: true });
                        form.setValue("dia", "2026-12-24", { shouldDirty: true });
                        form.setValue("hora", "17:45", { shouldDirty: true });
                    }}
                >
                    Llenar
                </Boton>
                <Boton variante="secundario" tamano="sm" onClick={() => void form.handleSubmit((v) => setEnviado(v))()}>
                    Validar
                </Boton>
            </div>
        </div>
    );
}

export function DemoComponentes() {
    const router = useRouter();
    const [fecha, setFecha] = useState<string | null>("2026-11-04");
    const [tecnico, setTecnico] = useState<string | null>(null);
    return (
        <ProveedorRutas navegar={router.push}>
            <main className="p-4 pb-0 sm:p-6 sm:pb-0">
                <FormularioPagina
                    titulo="Nueva orden de demostración"
                    descripcion="Componentes base de la fase 0 sobre HeroUI."
                    migas={[{ etiqueta: "Destino", href: "/demo-ui/destino" }, { etiqueta: "Nueva orden" }]}
                    alEnviar={(e) => {
                        e.preventDefault();
                        avisar({ titulo: "Orden de demostración creada", accion: { texto: "Deshacer", alPresionar: () => avisar({ tono: "info", titulo: "Se deshizo" }) } });
                    }}
                    navegar={router.push}
                    barra={{ textoGuardar: "Crear orden" }}
                >
                    <SeccionFormulario titulo="1. Trabajo">
                        <CamposOrden />
                        <SelectorFecha etiqueta="Fecha de la visita" valor={fecha} alCambiar={setFecha} ayuda="Se muestra en formato de Guatemala." />
                        <Autocompletar etiqueta="Técnico" opciones={TECNICOS} valor={tecnico} alCambiar={setTecnico} placeholder="Escribe un nombre" />
                    </SeccionFormulario>
                    <SeccionFormulario titulo="2. Con react-hook-form">
                        <CamposRhf />
                    </SeccionFormulario>
                    <SeccionFormulario titulo="3. Detalle">
                        <Pestanas
                            etiqueta="Secciones de la orden"
                            pestanas={[
                                { id: "resumen", etiqueta: "Resumen", contenido: <p>Resumen de la orden.</p> },
                                { id: "conversacion", etiqueta: "Conversación", icono: MessagesSquare, contenido: <p>Mensajes del ticket.</p> },
                                { id: "historial", etiqueta: "Historial", contenido: <p>Cambios de estado.</p> },
                            ]}
                        />
                        <div className="flex flex-wrap items-center gap-2">
                            <Globo texto="Agenda del técnico">
                                <Boton variante="contorno" tamano="icono" aria-label="Agenda">
                                    <CalendarDays />
                                </Boton>
                            </Globo>
                            <PanelEmergente
                                titulo="Filtros rápidos"
                                tituloVisible
                                disparador={
                                    <Boton variante="contorno">
                                        <Filter />
                                        Filtros
                                    </Boton>
                                }
                            >
                                <p className="text-sm">Solo órdenes de hoy.</p>
                            </PanelEmergente>
                            <Link href="/demo-ui/destino" className="text-sm text-primary underline">
                                Ir al destino
                            </Link>
                        </div>
                    </SeccionFormulario>
                </FormularioPagina>
            </main>
            <ProveedorAvisosEmergentes />
        </ProveedorRutas>
    );
}
