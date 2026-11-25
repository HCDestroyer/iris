# Centralita

## Para qué sirve

Conectar la centralita Issabel de la empresa. Cuando suena la extensión de alguien, Iris le muestra quién llama. Si se enciende el teléfono en Iris, también se contesta y se llama desde el panel con la extensión de cada persona.

## Cómo se usa

1. En **Administración**, abre **Centralita**.
2. Conecta la centralita con sus datos de conexión.
3. En **Extensiones**, asigna a cada persona su extensión: dónde le suenan las llamadas de la centralita.
4. Para contestar y llamar desde el panel, abre **Teléfono en Iris**:
    - escribe el **WSS de la Issabel** y la **Huella del certificado del WSS**;
    - configura **Salir a la calle**: dominio SIP, prefijo de salida y límites de llamadas;
    - revisa **Antes de encender** y **Qué configurar en Issabel (una vez)**;
    - marca **Confirmo que la Issabel está configurada como dice la guía** y guarda. Iris lo prueba y lo enciende.

## Qué significa cada estado

- **Primero conecta la centralita:** falta la conexión básica.
- **Teléfono guardado y encendido:** ya se puede contestar y llamar desde Iris.
- Sin la confirmación de la guía, el teléfono se apaga al guardar.

## Preguntas frecuentes

**¿A qué números se puede llamar?**
Solo a números de Guatemala (8 dígitos que empiezan por 2 a 7) y a extensiones del equipo. Nunca internacionales ni números especiales.

**Cambié el usuario SIP de una extensión.**
Escribe otra vez su contraseña.

## Permisos necesarios

- Configurar: `centralita.configurar`. Asignar extensiones: `centralita.asignar_extension` (**Supervisor**).
