import type { PlantillaManual } from "./tipos";

/*
 * Manuales de inicio para cualquier tecnología (fibra o inalámbrico). Textos para el cliente, en
 * español de Guatemala y de «usted». Los datos de la empresa van entre corchetes para que el
 * administrador los complete: [Nombre del ISP], [número de soporte], [número de WhatsApp]…
 * Nunca prometen precios ni plazos.
 */

export const MANUALES_GENERALES: PlantillaManual[] = [
    {
        codigo: "sin-internet-primeros-pasos",
        revision: 1,
        tecnologia: "general",
        titulo: "Sin internet: primeros pasos",
        resumen: "Qué revisar antes de llamar: si falla en todos los dispositivos, la luz y los cables del equipo, reiniciar en orden y cuándo reportarlo.",
        etiquetas: ["general", "sin servicio", "sin internet", "reinicio", "diagnostico"],
        visibilidad: "cliente",
        cuerpo: `Si se quedó sin internet, estos pasos resuelven la mayoría de los casos en pocos minutos.

## 1. ¿Falla en todos los dispositivos?

- Pruebe con otro teléfono o computadora.
- Si solo falla uno, el problema está en ese dispositivo: desactive y vuelva a activar su WiFi, o reinícielo.
- Si fallan todos, siga con el paso 2.

## 2. Revise la energía y los cables

- Confirme que el router y el equipo del servicio estén encendidos: en fibra es la **ONT** (el módem de fibra); en inalámbrico es el **inyector PoE** de la antena.
- Revise que los cables estén bien conectados y que ninguno esté doblado, aplastado o mordido por mascotas.
- Si hubo un corte de luz, espere unos minutos a que los equipos terminen de encender.

## 3. Reinicie los equipos en orden

1. Desconecte de la corriente la ONT o el inyector PoE y también el router.
2. Espere **30 segundos**.
3. Conecte primero la ONT o el inyector PoE y espere de 2 a 3 minutos.
4. Después conecte el router y espere otros 2 minutos.

Para más detalle, vea el manual «Reiniciar correctamente sus equipos».

> No presione el botón **Reset** (el agujerito): borra la configuración del equipo y el servicio puede quedar sin funcionar hasta que lo configure un técnico.

## 4. Revise el estado de su cuenta

Si tiene un pago pendiente, el servicio pudo suspenderse. Vea el manual «Pagos y reconexión».

## Cuándo llamar a soporte

Escríbanos al [número de WhatsApp] o llame al [número de soporte] si:

- después de reiniciar sigue sin internet;
- en la ONT ve la luz **LOS** en rojo, o la luz **PON** apagada o parpadeando;
- el inyector PoE de la antena no enciende;
- ve un cable cortado o dañado.

Tenga a mano el nombre del titular del servicio y cuéntenos qué luces ve encendidas. Así lo podemos ayudar más rápido.`,
    },
    {
        codigo: "reiniciar-equipos",
        revision: 1,
        tecnologia: "general",
        titulo: "Reiniciar correctamente sus equipos (router, ONT o antena)",
        resumen: "El orden correcto para reiniciar el router y la ONT de fibra o el inyector PoE de la antena, cuánto esperar y qué no hacer.",
        etiquetas: ["general", "reinicio", "router", "ont", "poe"],
        visibilidad: "cliente",
        cuerpo: `Reiniciar los equipos resuelve muchas fallas. Hacerlo en el orden correcto evita que el router arranque antes de tener señal.

## Antes de empezar

Identifique sus equipos:

- **Router**: el aparato que da el WiFi. En algunos hogares el router viene dentro de la ONT y es un solo equipo.
- **Fibra (FTTH)**: la **ONT** (también llamada ONU o módem de fibra) es la caja a la que llega el cable delgado de fibra, con un conector verde o azul.
- **Inalámbrico (WISP)**: el **inyector PoE** es un adaptador pequeño que lleva corriente a la antena del techo por el mismo cable de red.

## Pasos

1. Desconecte de la corriente la ONT o el inyector PoE y también el router. Desconecte el cable de corriente, no el de fibra ni el de la antena.
2. Espere **30 segundos**.
3. Conecte primero la ONT o el inyector PoE.
4. Espere de 2 a 3 minutos. En fibra, espere a que la luz **PON** quede fija.
5. Conecte el router y espere unos 2 minutos.
6. Pruebe de nuevo su internet.

Si la ONT y el router son un solo equipo, desconéctelo, espere 30 segundos, conéctelo y espere de 3 a 4 minutos.

## Lo que no debe hacer

- No presione el botón **Reset** ni lo mantenga apretado: devuelve el equipo a fábrica y borra la configuración del servicio.
- No desconecte el cable de fibra (conector verde o azul) de la ONT.
- No conecte el cable que sube a la antena directo a una computadora ni a otro puerto: el puerto **POE** del inyector lleva corriente.
- No reinicie una y otra vez seguidas: espere a que los equipos terminen de encender.

## Cuándo llamar a soporte

Si después de un reinicio completo sigue sin servicio, o si un equipo no enciende, escríbanos al [número de WhatsApp] o llame al [número de soporte]. Díganos qué luces ve encendidas y de qué color.`,
    },
    {
        codigo: "medir-velocidad",
        revision: 1,
        tecnologia: "general",
        titulo: "Internet lento: cómo medir la velocidad correctamente",
        resumen: "Cómo hacer una prueba de velocidad que sirva: por cable, sin otros dispositivos usando internet y a distintas horas. Qué datos enviarnos.",
        etiquetas: ["general", "lentitud", "velocidad", "prueba de velocidad", "wifi"],
        visibilidad: "cliente",
        cuerpo: `Una prueba de velocidad hecha por WiFi, lejos del router o con otros dispositivos descargando, casi siempre da un resultado más bajo que su plan. Así se mide bien.

## Prepare la prueba

- Si puede, conecte una computadora al router **con cable de red**. Es la única forma de medir la velocidad que llega a su casa sin que influya el WiFi.
- Si solo puede usar WiFi, póngase cerca del router, en el mismo ambiente y sin paredes de por medio, y conéctese a la red de **5 GHz** si su router la tiene.
- Pause descargas, actualizaciones, videos y videollamadas en los demás dispositivos de la casa.
- Cierre las demás aplicaciones y pestañas del navegador.

## Haga la prueba

1. Entre a [sitio de prueba de velocidad recomendado].
2. Haga la prueba **3 veces** seguidas y anote los resultados de bajada, subida y ping.
3. Repita en otro momento del día: en las noches suele haber más uso en todas las redes.

## Por qué puede salir más bajo

- **Por WiFi** la velocidad baja con la distancia, las paredes y la interferencia de otras redes.
- **Dispositivos antiguos** o de gama básica no alcanzan velocidades altas, aunque el servicio sí llegue.
- **Varios dispositivos a la vez** se reparten la velocidad del plan.
- **El sitio o la aplicación** que usa puede tener su propio límite.

## Qué enviarnos

Si por cable los resultados quedan muy por debajo de su plan en varias pruebas, envíenos al [número de WhatsApp]:

- capturas de las pruebas, con la fecha y la hora;
- si la prueba fue por cable o por WiFi;
- el modelo del dispositivo con el que midió.

## Cuándo llamar a soporte

- La velocidad por cable está muy por debajo de su plan a distintas horas del día.
- La conexión se corta seguido, aunque la velocidad sea buena.
- El ping es muy alto o variable y le afecta en videollamadas o juegos.`,
    },
    {
        codigo: "mejorar-wifi",
        revision: 1,
        tecnologia: "general",
        titulo: "Mejorar el WiFi en casa",
        resumen: "Dónde poner el router, cuándo usar 2.4 GHz o 5 GHz, qué causa interferencia y cuándo conviene un repetidor o un sistema de malla.",
        etiquetas: ["general", "wifi", "cobertura", "router", "repetidor"],
        visibilidad: "cliente",
        cuerpo: `Muchas veces el servicio llega bien a la casa, pero el WiFi no alcanza a todos los ambientes. Estos consejos ayudan.

## Ubique bien el router

- En un lugar **central** de la casa, no en una esquina ni junto a una ventana.
- **Elevado**, por ejemplo sobre un mueble o en la pared, y nunca en el piso.
- **A la vista**: no lo meta en gabinetes, gavetas ni detrás del televisor.
- Lejos de hornos de microondas, teléfonos inalámbricos, espejos, peceras y objetos de metal.

## 2.4 GHz o 5 GHz

Muchos routers tienen dos redes con nombres parecidos, por ejemplo «MiCasa» y «MiCasa-5G».

- **5 GHz**: más rápida, pero llega menos lejos y le afectan más las paredes. Úsela cerca del router.
- **2.4 GHz**: más lenta, pero llega más lejos y atraviesa mejor las paredes. Úsela en ambientes alejados y para aparatos sencillos (cámaras, enchufes inteligentes).

## Reduzca la interferencia

- Las paredes de block, concreto o con metal debilitan mucho la señal.
- Muchas redes vecinas cerca también la afectan. Si el problema sigue, podemos revisar con usted el canal del router.

## Repetidores y sistemas de malla

- Un **repetidor** extiende la señal, pero suele dar menos velocidad. Colóquelo a medio camino, donde todavía llegue buena señal del router.
- Un **sistema de malla** (varios equipos que trabajan juntos) cubre mejor una casa grande o de dos niveles.
- Si puede pasar un **cable de red** hasta el segundo equipo, el resultado es mucho mejor.

## Cuándo llamar a soporte

- Cerca del router el WiFi también está lento o se corta.
- Por cable funciona bien, pero el WiFi se desconecta con frecuencia.
- Quiere asesoría para cubrir toda su casa: escríbanos al [número de WhatsApp].`,
    },
    {
        codigo: "cambiar-nombre-clave-wifi",
        revision: 1,
        tecnologia: "general",
        titulo: "Cambiar el nombre y la contraseña del WiFi",
        resumen: "Pasos generales para cambiar el nombre de la red y la contraseña desde el navegador. Cada router es distinto: si tiene dudas, le ayudamos.",
        etiquetas: ["general", "wifi", "contraseña", "router", "seguridad"],
        visibilidad: "cliente",
        cuerpo: `Cambiar la contraseña del WiFi de vez en cuando protege su red. Los menús cambian según la marca y el modelo del router; estos son los pasos generales.

## Antes de empezar

- Use un teléfono o computadora conectado a su WiFi, o mejor, por cable al router.
- Busque la **etiqueta** del router (abajo o atrás). Ahí suelen venir la dirección para entrar (por ejemplo, 192.168.1.1 o 192.168.0.1), el usuario y la contraseña de administración.
- Si su router fue configurado por [Nombre del ISP] y no tiene esos datos, pídalos al [número de soporte].

## Pasos

1. Abra el navegador y escriba en la barra de direcciones la dirección de la etiqueta.
2. Entre con el usuario y la contraseña de administración.
3. Busque la sección **WiFi**, **WLAN** o **Inalámbrico**.
4. Cambie el **nombre de la red** (SSID), si quiere.
5. Cambie la **contraseña** (puede llamarse «clave», «WPA key» o «passphrase»).
6. Si el router lo permite, elija la seguridad **WPA2-Personal** o **WPA3-Personal**.
7. Guarde los cambios. El WiFi se reiniciará y tendrá que volver a conectar todos sus dispositivos con la contraseña nueva.

Si el router tiene dos redes (2.4 GHz y 5 GHz), cambie la contraseña en las dos.

## Una buena contraseña

- Al menos **12 caracteres**, con letras, números y algún símbolo.
- Que no sea su nombre, su número de teléfono, su fecha de nacimiento ni su dirección.
- Una frase fácil de recordar también sirve: «MiPerroComeTortillas!24».

## Tenga cuidado

- No cambie otras opciones del router si no sabe para qué sirven: el servicio puede dejar de funcionar.
- Si pierde la contraseña de administración, **no presione Reset**: llámenos.

## Cuándo llamar a soporte

Si no logra entrar al router o después del cambio no se conecta ningún dispositivo, escríbanos al [número de WhatsApp]. Podemos guiarlo paso a paso para su modelo.`,
    },
    {
        codigo: "cortes-de-energia",
        revision: 1,
        tecnologia: "general",
        titulo: "Cortes de energía: qué pasa con su servicio y cómo protegerlo",
        resumen: "Por qué se va el internet cuando se va la luz, qué hacer cuando vuelve la energía y qué UPS sirve para el router y la ONT o la antena.",
        etiquetas: ["general", "energia", "corte de luz", "ups", "proteccion"],
        visibilidad: "cliente",
        cuerpo: `Sus equipos de internet necesitan electricidad. Cuando se va la luz en su casa, el servicio se detiene aunque la red de [Nombre del ISP] siga funcionando.

## Cuando se va la luz

- El router, la ONT (fibra) y el inyector PoE de la antena (inalámbrico) se apagan.
- Si el corte es en todo el sector, también pueden quedar sin energía equipos de nuestra red en la zona. En ese caso el servicio vuelve cuando se restablece la energía y los equipos terminan de encender.

## Cuando vuelve la luz

1. Espere de 3 a 5 minutos a que los equipos enciendan por completo.
2. Si sigue sin internet, reinícielos en orden (vea el manual «Reiniciar correctamente sus equipos»).
3. Si las luces se ven raras o un equipo no enciende, llámenos: una subida de voltaje pudo dañarlo.

## Proteja sus equipos

- Conecte el router y la ONT o el inyector PoE a un **regulador de voltaje** o a un **protector contra picos**.
- En tormentas eléctricas fuertes puede desconectar de la corriente el inyector PoE y el router. Vuelva a conectarlos cuando pase la tormenta.

## Una UPS para seguir conectado

Una **UPS** (batería de respaldo) mantiene encendidos los equipos durante un corte en su casa.

- Para el router con la ONT o con el inyector PoE suele bastar una UPS pequeña, por ejemplo de 500 a 800 VA.
- Conecte a la UPS **solo** los equipos de internet. Si conecta también la computadora o el televisor, la batería dura mucho menos.
- Cuánto dura depende del consumo de sus equipos y del estado de la batería. Pruébela de vez en cuando.

> La UPS solo sirve si el corte es en su casa o en su edificio. Si la falla de energía afecta a los equipos de la zona, el servicio vuelve cuando se restablezca la energía allí.

## Cuándo llamar a soporte

- Volvió la luz hace más de 10 minutos, ya reinició y sigue sin internet.
- Un equipo no enciende o huele a quemado después del corte. Desconéctelo y no lo vuelva a conectar.
- Escríbanos al [número de WhatsApp] o llame al [número de soporte].`,
    },
    {
        codigo: "visita-tecnica",
        revision: 1,
        tecnologia: "general",
        titulo: "Visita técnica: qué esperar y cómo prepararse",
        resumen: "Cómo se programa la visita, quién debe estar en casa, cómo identificar al técnico y cómo preparar el lugar para instalación de fibra o antena.",
        etiquetas: ["general", "visita tecnica", "instalacion", "tecnico"],
        visibilidad: "cliente",
        cuerpo: `Cuando una falla no se resuelve por teléfono, o para instalar el servicio, enviamos a un técnico. Así puede prepararse.

## La cita

- Le confirmamos la fecha y una **franja de horario**. El técnico puede llegar en cualquier momento dentro de esa franja.
- Si no podrá estar, avísenos cuanto antes al [número de WhatsApp] para cambiar la cita.
- Si el técnico se atrasa, le avisaremos por los canales oficiales.

## Quién debe estar presente

- Una **persona mayor de edad**: el titular del servicio o alguien autorizado por él.
- Tenga a mano su **DPI** si se trata de una instalación, un cambio de equipo o un retiro.

## Cómo reconocer al técnico

- El técnico se identifica con [carné o uniforme de Nombre del ISP].
- Si tiene dudas, llame al [número de soporte] antes de dejarlo entrar.
- Si le pide un pago, confírmelo antes con el [número de soporte]. Los pagos se hacen solo por los medios oficiales.

## Prepare el lugar

- Despeje el paso hasta donde está o donde irá el equipo.
- Asegure a las mascotas.
- **Fibra (FTTH)**: piense dónde quiere la ONT. Debe quedar cerca de un tomacorriente, en un lugar ventilado y donde el cable de fibra no quede doblado ni a la pasada.
- **Inalámbrico (WISP)**: el técnico necesitará subir al techo o a la terraza. Avísenos si el acceso es difícil o si necesita permiso de alguien más (administración, vecinos).

## Durante la visita

- El técnico le explicará lo que hizo y probará el servicio con usted.
- Al terminar, le pedirá su **firma** en el teléfono para cerrar la orden. Revise el resumen antes de firmar.
- Pregunte todo lo que necesite: cómo reiniciar, qué significan las luces o cómo cambiar la contraseña del WiFi.

## Después de la visita

Puede que le enviemos una encuesta corta. Su opinión nos ayuda a mejorar.

## Cuándo llamar a soporte

- Necesita cambiar la fecha o la franja de la cita.
- Ya pasó la franja de horario y el técnico no llegó ni le avisamos.
- Llegó alguien sin cita o sin identificación diciendo que es de [Nombre del ISP].
- Después de la visita el problema volvió.

Escríbanos al [número de WhatsApp] o llame al [número de soporte].`,
    },
    {
        codigo: "pagos-y-reconexion",
        revision: 1,
        tecnologia: "general",
        titulo: "Pagos y reconexión: reportar un pago y promesa de pago",
        resumen: "Cómo reportar un pago con la foto de la boleta por WhatsApp, cómo pedir una promesa de pago y qué pasa con la reconexión del servicio.",
        etiquetas: ["general", "pagos", "boleta", "reconexion", "promesa de pago"],
        visibilidad: "cliente",
        cuerpo: `Si su servicio se suspendió por falta de pago, o quiere avisarnos que ya pagó, estos son los pasos.

## Dónde pagar

[Complete aquí los bancos, números de cuenta, agencias y otros medios de pago de Nombre del ISP.]

Pague siempre a nombre de **[Nombre del ISP]**. Nunca deposite a cuentas personales, aunque se lo pidan por teléfono o por WhatsApp.

## Reportar un pago

Si pagó en el banco o por transferencia, envíenos el comprobante por WhatsApp al [número de WhatsApp]:

1. Tome una **foto clara** de la boleta o una captura de la transferencia, donde se lean el número de boleta o de autorización, la fecha y el monto.
2. Escriba el **nombre del titular** del servicio.
3. Si tiene varios servicios, díganos a cuál aplica el pago.

Le confirmaremos por el mismo medio cuando el pago quede registrado.

## Promesa de pago

Si no puede pagar hoy, puede pedir una **promesa de pago**: se compromete a pagar en una fecha y, si se aprueba, el servicio puede seguir activo o reconectarse mientras tanto.

- Pídala por WhatsApp al [número de WhatsApp] o llamando al [número de soporte].
- Indique la **fecha** en que va a pagar.
- Cada promesa se revisa según su historial de pagos y las condiciones de [Nombre del ISP]. Le diremos si fue aprobada.
- Si no paga en la fecha acordada, el servicio puede suspenderse de nuevo.

## Reconexión

- La reconexión empieza cuando el pago queda **registrado**. Le avisaremos cuando el servicio esté activo.
- Cuando le confirmemos la reconexión, si todavía no tiene internet, reinicie sus equipos en orden (vea «Reiniciar correctamente sus equipos»).
- Si su servicio estuvo suspendido mucho tiempo, puede aplicar un cargo de reconexión según las condiciones de su contrato.

## Cuándo llamar a soporte

- Ya le confirmamos el pago o la reconexión y sigue sin internet después de reiniciar.
- Su boleta fue rechazada y no sabe por qué.
- Quiere cambiar la fecha de una promesa de pago.`,
    },
    {
        codigo: "traslado-de-domicilio",
        revision: 1,
        tecnologia: "general",
        titulo: "Traslado o cambio de domicilio",
        resumen: "Cómo pedir el traslado de su servicio a otra dirección, por qué primero revisamos la cobertura y qué hacer con los equipos.",
        etiquetas: ["general", "traslado", "cambio de domicilio", "cobertura", "factibilidad"],
        visibilidad: "cliente",
        cuerpo: `Si se va a mudar, puede llevarse su servicio de [Nombre del ISP]. Avísenos con anticipación para planificarlo.

## 1. Solicite el traslado

Escríbanos al [número de WhatsApp] o llame al [número de soporte] con:

- el nombre del titular del servicio;
- la **dirección completa** del nuevo domicilio, con referencias (color de la casa, negocio cercano);
- si puede, la **ubicación** de Google Maps o WhatsApp;
- la fecha aproximada de su mudanza.

## 2. Revisamos la cobertura

Antes de confirmar, revisamos si llegamos a la nueva dirección:

- **Fibra (FTTH)**: si hay red de fibra cerca y espacio en la caja de distribución.
- **Inalámbrico (WISP)**: si desde el techo de la nueva casa hay **línea de vista** a una de nuestras torres o antenas, sin edificios, cerros ni árboles en medio.

Puede que en la nueva dirección el servicio sea de otra tecnología o de otro plan. Se lo explicaremos antes de hacer cualquier cambio.

## 3. Coordinamos la visita

- Programamos el retiro de los equipos en la dirección actual y la instalación en la nueva. A veces se hacen el mismo día y a veces en días distintos.
- El traslado puede tener un costo según las condiciones de su contrato. Se lo informaremos antes.

## Sobre los equipos

- La ONT, la antena, el inyector PoE y el router que le prestamos son de [Nombre del ISP]. Cuídelos y entréguelos completos.
- **No desinstale usted mismo** la antena ni el cable de fibra: puede dañarlos. El técnico los retira.
- Si se mudó sin avisar y dejó los equipos en la casa anterior, avísenos lo antes posible.

## Cuándo llamar a soporte

- Ya tiene fecha de mudanza: escríbanos con tiempo.
- La nueva dirección no tiene cobertura y quiere conocer otras opciones.`,
    },
    {
        codigo: "seguridad-y-estafas",
        revision: 1,
        tecnologia: "general",
        titulo: "Seguridad: cuide su WiFi y evite estafas",
        resumen: "Por qué no compartir la contraseña del WiFi, cómo detectar dispositivos desconocidos y cómo reconocer estafas que se hacen pasar por su proveedor.",
        etiquetas: ["general", "seguridad", "estafas", "wifi", "contraseña"],
        visibilidad: "cliente",
        cuerpo: `Su conexión es parte de su casa. Estos cuidados la protegen a usted y a su familia.

## No comparta la contraseña del WiFi

- Cada persona conectada usa parte de la velocidad de su plan.
- Quien está en su red podría ver otros dispositivos de la casa (cámaras, impresoras, televisores).
- Lo que alguien haga en internet desde su red sale a nombre de **su** conexión.
- Si su router lo permite, cree una **red de invitados** para las visitas.

## Dispositivos desconocidos

- Si el internet anda lento sin razón, revise en la página del router la lista de **dispositivos conectados** (vea «Cambiar el nombre y la contraseña del WiFi»).
- Si ve aparatos que no reconoce, **cambie la contraseña del WiFi** y vuelva a conectar solo los suyos.
- Desactive la opción **WPS** del router si no la usa: permite conectarse sin la contraseña.

## Estafas que se hacen pasar por [Nombre del ISP]

[Nombre del ISP] **nunca** le va a pedir:

- su contraseña del WiFi, de su banca en línea ni de sus redes sociales;
- códigos que le lleguen por mensaje de texto (SMS) o por WhatsApp;
- depósitos a cuentas personales ni pagos con tarjetas de regalo o recargas;
- instalar aplicaciones para controlar su teléfono o computadora a distancia.

Desconfíe si:

- le escriben desde un número que no es el oficial: [número de WhatsApp] y [número de soporte];
- le ofrecen un premio, un descuento muy grande o le dicen que su servicio se cortará **hoy** si no paga en ese momento;
- le mandan un enlace para «actualizar sus datos» o «verificar su cuenta».

Ante la duda, **cuelgue y llame usted** al [número de soporte].

## Visitas a domicilio

Nuestros técnicos se identifican con [carné o uniforme de Nombre del ISP] y llegan solo con una visita programada. Si alguien llega sin cita diciendo que es de [Nombre del ISP], llámenos antes de dejarlo entrar.

## Cuándo llamar a soporte

- Recibió un mensaje o una llamada sospechosa a nombre de [Nombre del ISP].
- Cree que alguien usa su WiFi sin permiso y no logra cambiar la contraseña.
- Ya compartió un dato o hizo un pago y cree que fue una estafa: llámenos y avise también a su banco.`,
    },
];
