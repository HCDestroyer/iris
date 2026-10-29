import { Building2 } from "lucide-react-native";
import { useState } from "react";
import { Aviso, Boton, Opciones, Pila, Texto, Titulo } from "../componentes";
import type { SesionCampo } from "../sesion/sesion";

/** Con varias empresas: con cuál trabaja el teléfono (la cola de cada una va aparte). */
export function ElegirEmpresa({ sesion, alElegir, alSalir }: { sesion: SesionCampo; alElegir: (id: number) => void; alSalir: () => void }) {
    const [elegida, setElegida] = useState<string | undefined>();
    if (sesion.empresas.length === 0) {
        return (
            <Pila>
                <Titulo>Sin empresa asignada</Titulo>
                <Aviso tono="warning" icono={Building2} titulo="Tu usuario no tiene empresas en Iris">
                    Pide a un administrador que te asigne una empresa.
                </Aviso>
                <Boton variante="secundario" alPresionar={alSalir}>
                    Cerrar sesión
                </Boton>
            </Pila>
        );
    }
    return (
        <Pila>
            <Titulo>¿Con qué empresa trabajas?</Titulo>
            <Texto suave>Lo que hagas sin señal se guarda aparte por empresa.</Texto>
            <Opciones etiqueta="Empresa" columna opciones={sesion.empresas.map((e) => ({ valor: String(e.id), etiqueta: e.nombre }))} valor={elegida} alCambiar={setElegida} />
            <Boton deshabilitado={!elegida} alPresionar={() => elegida && alElegir(Number(elegida))}>
                Continuar
            </Boton>
        </Pila>
    );
}
