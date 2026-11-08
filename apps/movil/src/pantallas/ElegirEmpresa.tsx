import { Building2 } from "lucide-react-native";
import { useState } from "react";
import { Aviso, Boton, Opciones } from "../componentes";
import type { SesionCampo } from "../sesion/sesion";
import { PantallaBloqueo } from "./Bloqueo";

/**
 * Con varias empresas: con cuál trabaja el teléfono (la cola de cada una va aparte). Sin empresas,
 * bloquea. Las dos son pantallas de arranque: «Usar otra cuenta» (y «Atrás» de Android) salen al login.
 */
export function ElegirEmpresa({ sesion, alElegir, alSalir }: { sesion: SesionCampo; alElegir: (id: number) => void; alSalir: () => Promise<void> | void }) {
    const [elegida, setElegida] = useState<string | undefined>();
    if (sesion.empresas.length === 0) {
        return (
            <PantallaBloqueo titulo="Sin empresa asignada" explicacion="Tu usuario todavía no trabaja en ninguna empresa de Iris." alUsarOtraCuenta={alSalir} testID="sin-empresa">
                <Aviso tono="warning" icono={Building2} titulo="Tu usuario no tiene empresas en Iris">
                    Pide a un administrador que te asigne una empresa y vuelve a entrar.
                </Aviso>
            </PantallaBloqueo>
        );
    }
    return (
        <PantallaBloqueo titulo="¿Con qué empresa trabajas?" explicacion="Lo que hagas sin señal se guarda aparte por empresa." alUsarOtraCuenta={alSalir} testID="elegir-empresa">
            <Opciones etiqueta="Empresa" columna opciones={sesion.empresas.map((e) => ({ valor: String(e.id), etiqueta: e.nombre }))} valor={elegida} alCambiar={setElegida} />
            <Boton variante="tinta" deshabilitado={!elegida} alPresionar={() => elegida && alElegir(Number(elegida))}>
                Continuar
            </Boton>
        </PantallaBloqueo>
    );
}
