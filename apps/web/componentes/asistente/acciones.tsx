"use client";

import { Aviso, Casilla, Interruptor } from "@iris/ui";
import { BookOpen, CalendarDays, CircleDollarSign, Handshake, Plug, Receipt, ShieldCheck, Ticket, WifiOff, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import type { UseFormRegisterReturn } from "react-hook-form";
import { NOMBRE_ACCION, type ClaveAccion } from "@/lib/asistente/fase3";
import { ACCIONES_COMUNES, ACCIONES_DINERO, esDeDinero, requisitoDe } from "@/lib/asistente/fase3b";

const ACCIONES: Record<ClaveAccion, { nota?: string; icono: LucideIcon }> = {
    responder_dudas: { nota: "Con tus manuales", icono: BookOpen },
    avisar_fallas: { icono: WifiOff },
    crear_ticket: { icono: Ticket },
    agendar_visita: { nota: "Le ofrece horarios al cliente y crea la orden", icono: CalendarDays },
    recibir_boleta: { icono: Receipt },
    dar_saldo: { icono: CircleDollarSign },
    promesa_pago: { icono: Handshake },
    reconexion_temporal: { icono: Plug },
};

type Props = {
    /** `form.register("acciones.<clave>")` del formulario que las usa. */
    registrar: (clave: ClaveAccion) => UseFormRegisterReturn;
    /** `form.register("saldoConNumero")`. */
    registrarSaldoConNumero: () => UseFormRegisterReturn;
    /** Al apagar «Dar el saldo»: el formulario vuelve `saldoConNumero` a `false`. */
    alApagarSaldo: () => void;
    /** Valores vivos del formulario: deciden el texto de «Dar el saldo» y si se ofrece bajarlo. */
    saldoConNumero: boolean;
    daSaldo: boolean;
    deshabilitado?: boolean;
    /** Debajo de «Agendar visitas» (p. ej. el bloque «Cómo agenda» cuando está encendido). */
    bajoVisitas?: ReactNode;
};

/**
 * «¿Qué puede hacer solo?» (contratos/conversaciones.md §17.1 y §17.8.2): primero lo de todos los
 * días y, aparte, lo que toca dinero o el servicio, que viene apagado y dice en llano qué le pide
 * al cliente antes de hacerlo. La usan la pantalla de las tres preguntas (perfil general) y el
 * editor de perfiles por horario.
 */
export function AccionesAsistente({ registrar, registrarSaldoConNumero, alApagarSaldo, saldoConNumero, daSaldo, deshabilitado = false, bajoVisitas }: Props) {
    // «Con solo su número» no sobrevive a apagar el saldo: al encenderlo otra vez pide identidad.
    const registrarAccion = (clave: ClaveAccion): UseFormRegisterReturn => {
        const r = registrar(clave);
        if (clave !== "dar_saldo") return r;
        return {
            ...r,
            onChange: async (e) => {
                if (!(e.target as HTMLInputElement).checked) alApagarSaldo();
                return r.onChange(e);
            },
        };
    };
    return (
        <>
            <ul aria-label="Lo que el asistente puede hacer solo" className="grid">
                {ACCIONES_COMUNES.map((clave) => (
                    <FilaAccion key={clave} clave={clave} registro={registrarAccion(clave)} deshabilitado={deshabilitado} nota={ACCIONES[clave].nota}>
                        {clave === "agendar_visita" ? bajoVisitas : null}
                    </FilaAccion>
                ))}
            </ul>
            <div className="grid min-w-0 gap-2.5" data-slot="acciones-dinero">
                <h3 className="text-sm font-semibold">Con dinero o con el servicio</h3>
                <Aviso tono="info" icono={ShieldCheck} titulo="Vienen apagadas: tú decides cuáles encender" data-testid="aviso-dinero">
                    Antes de hacerlas, el asistente comprueba que habla con el titular: le pide su DPI o NIT y lo confirma con Argos. Solo las hace en los tipos de mensaje que dejes en «Responde solo». «Dar el
                    saldo» y «Recibir boletas» antes venían encendidas: si las quieres, enciéndelas otra vez.
                </Aviso>
                <ul aria-label="Lo que el asistente puede hacer solo con dinero o con el servicio" className="grid">
                    {ACCIONES_DINERO.map((clave) => (
                        <FilaAccion key={clave} clave={clave} registro={registrarAccion(clave)} deshabilitado={deshabilitado} nota={requisitoDe(clave, saldoConNumero)}>
                            {clave === "dar_saldo" && daSaldo ? (
                                <Casilla className="pl-[1.875rem]" etiqueta="Darlo con solo su número, sin pedirle documento" disabled={deshabilitado} {...registrarSaldoConNumero()} />
                            ) : null}
                        </FilaAccion>
                    ))}
                </ul>
            </div>
        </>
    );
}

/** Una acción con su interruptor; las de dinero llevan debajo lo que se le pide al cliente. */
function FilaAccion({ clave, registro, deshabilitado, nota, children }: { clave: ClaveAccion; registro: UseFormRegisterReturn; deshabilitado: boolean; nota?: string | undefined; children?: ReactNode }) {
    const Icono = ACCIONES[clave].icono;
    return (
        <li className="grid min-w-0 gap-2 border-b border-border py-2.5 first:pt-0" data-accion={clave} data-dinero={esDeDinero(clave) || undefined}>
            <div className="flex min-w-0 items-center gap-3">
                <Icono aria-hidden className="size-[1.125rem] shrink-0 text-muted-foreground" />
                <Interruptor
                    alFinal
                    className="min-w-0 flex-1"
                    disabled={deshabilitado}
                    etiqueta={
                        <span className="grid min-w-0">
                            <span className="[overflow-wrap:anywhere]">{NOMBRE_ACCION[clave]}</span>
                            {nota ? <span className="text-xs font-normal text-muted-foreground [overflow-wrap:anywhere]">{nota}</span> : null}
                        </span>
                    }
                    {...registro}
                />
            </div>
            {children}
        </li>
    );
}
