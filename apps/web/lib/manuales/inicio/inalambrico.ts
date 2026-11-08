import type { PlantillaManual } from "./tipos";

/* Manuales de inicio para el servicio inalámbrico (WISP): antena exterior, inyector PoE y línea de vista. */

export const MANUALES_INALAMBRICO: PlantillaManual[] = [
    {
        codigo: "wisp-antena-poe",
        revision: 2,
        tecnologia: "wisp",
        titulo: "Inalámbrico (WISP): la antena y el inyector PoE",
        resumen: "Cómo funcionan la antena del techo y el inyector PoE, cómo van los cables, qué luces revisar y por qué no hay que mover la antena.",
        etiquetas: ["wisp", "inalambrico", "antena", "poe", "cpe"],
        cuerpo: `En el servicio inalámbrico la señal llega por aire, desde una torre o antena de [Nombre del ISP], hasta la **antena** instalada en su techo. Así funciona su instalación.

## Los equipos

- **Antena** (también llamada CPE): está afuera, en el techo o en un mástil, apuntando a nuestra torre.
- **Inyector PoE**: un adaptador pequeño que se conecta a la corriente dentro de la casa. Le da energía a la antena por el mismo cable de red que baja del techo.
- **Router**: el equipo que da el WiFi en su casa.

## Cómo van los cables

El inyector PoE tiene dos puertos:

- **POE** (o «PoE», «Ant»): aquí va el cable que sube a la antena.
- **LAN**: aquí va el cable que va al router (al puerto **WAN** o **Internet** del router).

> Nunca conecte el puerto **POE** a una computadora, un televisor ni a otro equipo que no sea la antena: lleva corriente y puede dañarlo.

## Las luces

- El inyector PoE tiene una luz de **encendido**. Si está apagada, revise que esté bien conectado a la corriente.
- Muchas antenas tienen luces de **señal** (varias barras o puntos). No suba a verlas: si se ven desde el suelo, sin subir, y el técnico se lo pide, puede describirlas.

## Cuide la antena

- **No la mueva ni la gire.** Está apuntada con precisión a nuestra torre: unos pocos grados de diferencia pueden bajar mucho la señal.
- No cuelgue ropa, antenas de televisión ni adornos en el mástil.
- Si hace trabajos en el techo, tenga cuidado de no golpear la antena ni pisar el cable.
- El cable que baja del techo debe ir sujeto y sin quedar colgando ni rozando filos.

## Lluvia y viento

- Una lluvia fuerte puede bajar un poco la señal por un momento. Si al pasar la lluvia sigue fallando, avísenos.
- Un viento fuerte puede mover la antena o el mástil. Si después de un ventarrón el servicio quedó lento o se corta, puede que la antena se haya desalineado.
- Si se anuncia una tormenta eléctrica, desconecte de la corriente el inyector PoE y el router antes de que empiece. Durante la tormenta no toque los equipos ni el cable que baja de la antena. Vuelva a conectarlos cuando haya pasado.

## Cuándo llamar a soporte

- La luz del inyector PoE no enciende, aunque esté conectado.
- La antena se ve movida, inclinada o caída, o el cable está dañado.
- Después de lluvia o viento el servicio sigue intermitente.

Escríbanos al [número de WhatsApp] o llame al [número de soporte]. **No suba al techo** a revisar la antena: es peligroso. Un técnico lo hará.`,
    },
    {
        codigo: "wisp-senal-baja",
        revision: 2,
        tecnologia: "wisp",
        titulo: "Inalámbrico (WISP): señal baja o intermitente",
        resumen: "Por qué la señal de la antena puede bajar (árboles, construcciones, línea de vista, cables), qué puede revisar usted y cuándo pedir una visita.",
        etiquetas: ["wisp", "inalambrico", "señal", "intermitencia", "linea de vista"],
        cuerpo: `Si su internet inalámbrico se corta seguido, va lento a ratos o falla más en ciertos momentos, puede ser un problema de señal entre su antena y nuestra torre.

## La línea de vista

La antena de su techo y nuestra torre necesitan **verse**: un camino libre de obstáculos entre las dos. A eso se le llama **línea de vista**. Cualquier cosa en medio debilita la señal:

- **Árboles** que crecieron. Las hojas tienen agua y bloquean mucho la señal. En época de lluvia, cuando hay más hojas, suele empeorar.
- **Construcciones nuevas**: una casa de dos niveles, un muro, una bodega o una valla publicitaria.
- **Objetos en el techo**: tinacos, láminas, antenas de televisión o mástiles nuevos.

## Señales de que es la antena

- Falla en **todos** los dispositivos al mismo tiempo, también por cable.
- Empeora con lluvia o viento.
- Empezó después de un ventarrón, de trabajos en el techo o de una construcción cercana.

## Qué puede revisar usted

1. Que el inyector PoE esté encendido y que los cables estén bien conectados (vea «La antena y el inyector PoE»).
2. Lo que se ve del cable desde el suelo o dentro de la casa: que no esté pelado, quemado por el sol o mordido.
3. Desde el suelo, sin subir al techo: si la antena se ve movida, inclinada o con algo enfrente.
4. Si hay árboles que crecieron entre su casa y la dirección de nuestra torre.
5. Reinicie el inyector PoE y el router en orden.

> No gire ni mueva la antena para «buscar señal». Sin el equipo de medición se puede perder la señal por completo.

## Qué puede hacer el técnico

- Medir la señal y volver a apuntar la antena.
- Cambiar el cable o los conectores dañados.
- Subir la antena con un mástil más alto para librar un obstáculo.
- Revisar si conviene apuntar a otra torre.

Algunos trabajos, como un mástil más alto o una poda grande, pueden tener un costo. Se lo explicaremos antes de hacerlos.

## Cuándo llamar a soporte

- El servicio se corta varias veces al día, también por cable.
- La antena se ve movida o el cable dañado.
- Hay un obstáculo nuevo entre su casa y la torre.

Escríbanos al [número de WhatsApp] o llame al [número de soporte]. Díganos desde cuándo pasa, a qué horas falla más y si coincide con lluvia o viento. Si hace falta, programaremos una visita técnica.`,
    },
];
