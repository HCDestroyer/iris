import type { PlantillaManual } from "./tipos";

/* Manuales de inicio para el servicio de fibra óptica hasta el hogar (FTTH). */

export const MANUALES_FIBRA: PlantillaManual[] = [
    {
        codigo: "ftth-luces-ont",
        revision: 2,
        tecnologia: "ftth",
        titulo: "Fibra (FTTH): qué significan las luces de la ONT",
        resumen: "Cómo leer las luces PWR, PON, LOS, LAN y WiFi de la ONT o módem de fibra, qué es normal y qué hacer si LOS está en rojo o PON parpadea.",
        etiquetas: ["ftth", "fibra", "ont", "onu", "luces", "los", "pon"],
        cuerpo: `La **ONT** (también llamada ONU o módem de fibra) es la caja a la que llega el cable de fibra. Sus luces dicen en qué estado está el servicio. Los nombres cambian un poco según el modelo, pero casi todas tienen estas.

## Las luces y lo que es normal

- **PWR o POWER** (encendido): debe estar **fija**, normalmente en verde. Apagada: el equipo no tiene corriente.
- **PON** (registro en la red): debe estar **fija**. Parpadeando: la ONT está intentando conectarse. Apagada: no se ha conectado.
- **LOS** (pérdida de señal): lo normal es que esté **apagada**. En **rojo**, fija o parpadeando: no llega luz por la fibra.
- **LAN o ETH** (puertos de red): encendida o parpadeando si hay un dispositivo conectado por cable en ese puerto. Apagada si no hay nada conectado, y eso es normal.
- **WiFi, WLAN, 2.4G o 5G**: encendida o parpadeando si el WiFi de la ONT está activo. Si su WiFi lo da otro router, puede estar apagada.
- **INTERNET** (en algunos modelos): encendida cuando la conexión a internet está lista.

## Qué hacer según lo que ve

### PWR apagada

1. Revise que el adaptador de corriente esté bien conectado a la ONT y al tomacorriente.
2. Pruebe en otro tomacorriente.
3. Si sigue apagada, llámenos: el adaptador o la ONT pueden estar dañados.

### LOS en rojo

No llega señal por la fibra. Casi siempre es un cable doblado, aplastado o desconectado, o una falla en la calle.

1. Revise el cable de fibra desde la ONT hasta donde entra a su casa (solo mírelo, sin jalarlo ni moverlo): que no esté doblado, pisado por un mueble ni prensado por una puerta o ventana.
2. **No** desconecte ni limpie el conector verde o azul.
3. Reinicie la ONT una vez.
4. Si LOS sigue en rojo, llámenos. Normalmente se necesita una revisión técnica.

### PON parpadeando o apagada (con LOS apagada)

1. Espere 3 minutos: al encender, la ONT tarda un poco en conectarse.
2. Reinicie la ONT (desconéctela de la corriente 30 segundos).
3. Si después de 5 minutos PON sigue sin quedar fija, llámenos.

### Todo se ve normal, pero no hay internet

1. Reinicie la ONT y el router en orden (vea «Reiniciar correctamente sus equipos»).
2. Revise el estado de su cuenta (vea «Pagos y reconexión»).
3. Si sigue igual, llámenos.

## Cuándo llamar a soporte

- **LOS** en rojo después de revisar el cable y reiniciar.
- **PON** que no queda fija después de reiniciar.
- **PWR** apagada aunque el adaptador esté conectado.

Escríbanos al [número de WhatsApp] o llame al [número de soporte]. Si puede, envíenos una **foto de las luces** de la ONT: nos ayuda mucho.`,
    },
    {
        codigo: "ftth-cuidados-fibra",
        revision: 2,
        tecnologia: "ftth",
        titulo: "Fibra (FTTH): cuidados del cable de fibra",
        resumen: "El cable de fibra es delgado y delicado: cómo evitar doblarlo, pisarlo o desconectarlo, y por qué no hay que mirar el conector verde o azul.",
        etiquetas: ["ftth", "fibra", "cable", "conector", "cuidados"],
        cuerpo: `La fibra óptica lleva la señal como luz por un hilo de vidrio muy delgado. Es muy confiable, pero un mal trato puede cortar el servicio.

## No lo doble

- Evite curvas cerradas o ángulos. Las curvas deben ser **suaves y amplias**, más o menos del tamaño de una lata.
- No lo enrolle apretado para guardar lo que sobra. Si sobra cable, haga vueltas grandes y flojas.
- No lo amarre fuerte con cinchos, alambre ni cinta.

## No lo pise ni lo prense

- No lo deje pasar bajo puertas, ventanas, alfombras ni patas de muebles.
- No lo engrape ni le clave clavos encima.
- Manténgalo lejos de donde pasan personas, mascotas o la escoba.
- Si tiene mascotas, cuide que no lo muerdan.

## No lo jale

- No mueva la ONT de lugar jalando el cable.
- Si necesita mover la ONT o el cable, pídanos una visita.

## El conector verde o azul

- El cable entra a la ONT con un conector **verde** o **azul**. **No lo desconecte.**
- Si alguna vez está suelto, no mire nunca dentro del conector ni del puerto: la luz que viaja por la fibra es **invisible** y puede dañar los ojos.
- No sople, limpie ni toque la punta del conector: el polvo o la grasa de los dedos ensucian la fibra y bajan la señal.

## Al pintar, remodelar o cambiar muebles

- Proteja el cable y la ONT antes de pintar o de hacer trabajos en la pared.
- Si necesita cambiar la ruta del cable, avísenos antes. Un técnico puede hacerlo sin dañarlo.

## Cuándo llamar a soporte

- El cable está doblado, aplastado, cortado o se ve dañado.
- El conector verde o azul se salió de la ONT.
- La luz **LOS** de la ONT está en rojo.

Escríbanos al [número de WhatsApp] o llame al [número de soporte]. No intente reparar ni empalmar la fibra usted mismo.`,
    },
];
