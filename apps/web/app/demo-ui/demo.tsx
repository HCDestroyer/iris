"use client";

import {
    Autocompletar,
    avisar,
    Boton,
    Campo,
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
                    <SeccionFormulario titulo="2. Detalle">
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
