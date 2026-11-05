"use client";

import {
    Aviso,
    Boton,
    Dialogo,
    DialogoCerrar,
    DialogoConfirmar,
    DialogoContenido,
    DialogoDisparador,
    DialogoPie,
    Globo,
    Hoja,
    HojaContenido,
    HojaDisparador,
    MenuDesplegable,
    MenuDesplegableContenido,
    MenuDesplegableDisparador,
    MenuDesplegableElemento,
    MenuDesplegableSeparador,
    PanelEmergente,
    Tarjeta,
} from "@iris/ui";
import { CalendarDays, Ellipsis, Filter, Pencil, Trash2 } from "lucide-react";
import { useState } from "react";

/**
 * Capas flotantes y avisos sobre un contenido cargado (e2e/capas-flotantes.spec.ts): todo panel
 * flotante es sólido y toda capa modal oscurece lo de atrás.
 */
export function DemoCapas() {
    const [confirmando, setConfirmando] = useState(false);
    return (
        <main className="mx-auto grid max-w-3xl gap-4 p-4 sm:p-6">
            <h1 className="text-xl font-semibold">Capas flotantes</h1>
            <div className="grid gap-2">
                <Aviso tono="info" titulo="Datos de Argos de hace 2 horas" data-testid="aviso-info">
                    Se actualizan con el siguiente evento.
                </Aviso>
                <Aviso tono="success" titulo="Servicio activo en Argos" data-testid="aviso-success" />
                <Aviso tono="warning" titulo="Falta la foto del equipo" data-testid="aviso-warning" />
                <Aviso
                    tono="error"
                    titulo="No se pudo leer el inventario"
                    data-testid="aviso-error"
                    accion={
                        <Boton variante="contorno" tamano="sm">
                            Reintentar
                        </Boton>
                    }
                />
            </div>
            <div className="flex flex-wrap items-center gap-2">
                <Dialogo>
                    <DialogoDisparador asChild>
                        <Boton variante="contorno">Abrir diálogo</Boton>
                    </DialogoDisparador>
                    <DialogoContenido titulo="Diálogo de demostración" descripcion="Una acción corta.">
                        <p className="text-sm">El panel es sólido y lo de atrás se oscurece.</p>
                        <DialogoPie>
                            <DialogoCerrar asChild>
                                <Boton variante="contorno">Listo</Boton>
                            </DialogoCerrar>
                        </DialogoPie>
                    </DialogoContenido>
                </Dialogo>
                <Boton variante="contorno" onClick={() => setConfirmando(true)}>
                    Quitar técnico
                </Boton>
                <DialogoConfirmar
                    open={confirmando}
                    onOpenChange={setConfirmando}
                    titulo="¿Quitar el técnico?"
                    descripcion="La orden queda sin asignar."
                    textoConfirmar="Quitar"
                    peligro
                    alConfirmar={() => setConfirmando(false)}
                />
                <Hoja>
                    <HojaDisparador asChild>
                        <Boton variante="contorno">Abrir panel lateral</Boton>
                    </HojaDisparador>
                    {/* `fondo="transparente"` ya no deja la página sin telón. */}
                    <HojaContenido titulo="Panel lateral de demostración" descripcion="OT-000042 · Instalación" lado="derecha" fondo="transparente">
                        <p className="text-sm">Acciones de la ficha.</p>
                    </HojaContenido>
                </Hoja>
                <MenuDesplegable>
                    <MenuDesplegableDisparador>
                        <Boton variante="contorno">
                            <Ellipsis />
                            Acciones
                        </Boton>
                    </MenuDesplegableDisparador>
                    <MenuDesplegableContenido>
                        <MenuDesplegableElemento icono={Pencil}>Editar</MenuDesplegableElemento>
                        <MenuDesplegableSeparador />
                        <MenuDesplegableElemento icono={Trash2}>Quitar</MenuDesplegableElemento>
                    </MenuDesplegableContenido>
                </MenuDesplegable>
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
                <Globo texto="Agenda del técnico">
                    <Boton variante="contorno" tamano="icono" aria-label="Agenda">
                        <CalendarDays />
                    </Boton>
                </Globo>
            </div>
            {/* Contenido cargado detrás de las capas. */}
            {Array.from({ length: 6 }, (_, i) => (
                <Tarjeta key={i} className="grid gap-1 p-4">
                    <p className="font-medium">OT-00004{i} · Instalación</p>
                    <p className="text-sm text-muted-foreground">Calle {i + 3}, zona {i + 1}, Guatemala · Luis Pérez · 09:00–11:00</p>
                </Tarjeta>
            ))}
        </main>
    );
}
