# Centralita Issabel: preparar Issabel 5 para Iris (E4a, solo lectura)

Para quien administra la centralita. Centralita de referencia: **Issabel 5** (issabel-pbx 5.0.0-4,
IssabelPBX 2.12.0-5) sobre Rocky Linux 8.10 con **Asterisk 18.19.0**. Contrato: `.claude/integration/
contratos/voz.md` §2.10 y §3.12.

Iris **solo escucha**: no marca, no graba, no cambia nada en Issabel. Se conecta al AMI (Asterisk
Manager Interface) **cifrado** en el puerto **5039**, con un usuario que solo puede leer los eventos
de llamadas y el registro de llamadas (CDR). Cuando suena la extensión de alguien de tu equipo, a esa
persona le sale en Iris una ventana con el cliente.

Los comandos van en inglés, tal como son; se corren como `root` en el servidor de Issabel.

## 1. Cómo llega Iris a la centralita

El AMI **nunca** se abre a internet. Elige una:

- **VPN o túnel** entre el servidor de Iris y la oficina (WireGuard o IPsec). Es lo recomendado.
- **IP fija de Iris** con una regla en el firewall de Issabel que solo deje entrar esa IP al 5039.

Anota la IP con la que Iris llega a Issabel (en los ejemplos, `10.20.0.9`).

## 2. Certificado para el AMI cifrado (TLS)

Si Issabel ya tiene un certificado (el de la interfaz web sirve), úsalo. Si no, uno autofirmado:

```sh
mkdir -p /etc/asterisk/keys
openssl req -x509 -newkey rsa:2048 -nodes -days 1825 \
  -keyout /etc/asterisk/keys/ami.key -out /etc/asterisk/keys/ami.crt -subj "/CN=issabel-ami"
chown asterisk:asterisk /etc/asterisk/keys/ami.*
chmod 600 /etc/asterisk/keys/ami.key
```

Huella SHA-256 del certificado (la vas a comparar con la que muestra Iris al probar):

```sh
openssl x509 -in /etc/asterisk/keys/ami.crt -noout -fingerprint -sha256
```

## 3. Encender el AMI cifrado

En `/etc/asterisk/manager.conf`, sección `[general]` (en IssabelPBX los cambios propios van en
`manager_custom.conf` cuando el archivo lo regenera la interfaz; revisa cuál incluye tu instalación):

```ini
[general]
enabled = yes
tlsenable = yes
tlsbindaddr = 0.0.0.0:5039
tlscertfile = /etc/asterisk/keys/ami.crt
tlsprivatekey = /etc/asterisk/keys/ami.key
```

El AMI sin cifrar (5038) puede seguir para la propia Issabel, pero **solo en `127.0.0.1`**; Iris no
lo acepta.

## 4. Usuario AMI de solo lectura para Iris

En `/etc/asterisk/manager_custom.conf` (o «PBX › Configuración avanzada › Usuarios AMI» si tu
versión lo ofrece):

```ini
[iris_lectura]
secret = (una contraseña larga, solo para Iris)
deny = 0.0.0.0/0.0.0.0
permit = 10.20.0.9/255.255.255.255
read = call,cdr
write =
eventfilter = !Event: RTCP*
eventfilter = !Event: VarSet
eventfilter = !Event: Newexten
```

- `read = call,cdr` y **`write =` vacío**: Iris comprueba al probar y en cada conexión que el usuario
  no pueda ejecutar acciones de escritura. Mira la clase de cada acción que el AMI le permite: con
  cualquiera de `system`, `call`, `originate`, `config`, `command`, `agent`, `user`, `dtmf`, `agi`,
  `message` (o `all`) en `write`, Iris no se enciende. Iris no necesita ninguna clase de escritura: deja
  `write =` vacío (ni siquiera `reporting`).
- `permit`: solo la IP de Iris.
- Los `eventfilter` quitan eventos que Iris no usa (menos tráfico).

## 5. Linkedid en el registro de llamadas (CDR)

Iris une los eventos de una llamada por su **Linkedid**. Para que el CDR por AMI lo traiga, en
`/etc/asterisk/cdr_manager.conf`:

```ini
[general]
enabled = yes

[mappings]
linkedid => LinkedID
```

Sin esto Iris usa el `UniqueID` del canal de origen (funciona en la mayoría de las llamadas, pero la
duración de algunas transferencias puede quedar incompleta).

## 6. Recargar

```sh
asterisk -rx "manager reload"
asterisk -rx "module reload cdr_manager.so"
asterisk -rx "manager show settings"
asterisk -rx "manager show user iris_lectura"
```

`manager show settings` debe decir `TLS enable: Yes` y `TLS Bind address: 0.0.0.0:5039`.

## 7. Firewall de Issabel

En **Security › Firewall**, agrega una regla que permita **TCP 5039 solo desde la IP de Iris** (o
desde la red de la VPN) y deja el resto cerrado. Desde otra máquina fuera de la VPN, el puerto debe
verse cerrado:

```sh
nc -zv <ip-publica-de-issabel> 5039   # esperado: falla
```

## 8. En Iris

1. **Administración › Integraciones › Centralita › Conectar**: nombre, dirección (la IP de Issabel en
   la VPN), usuario `iris_lectura` y su contraseña.
2. **Probar conexión**: compara la huella que muestra Iris con la del paso 2. Si no es la misma, no
   sigas (alguien está en medio).
3. Guardar y encender. El estado debe pasar a «Conectada».
4. **Extensiones**: asigna a cada persona su extensión (como aparece en Issabel). Solo a ellas les
   sale la ventana; Iris descarta los eventos de las demás extensiones sin guardarlos.

## Qué hace Iris con cada tipo de canal

- Teléfono de una extensión (`PJSIP/201-0000001a` o `SIP/201-0000001a`): es lo único de donde Iris
  saca una extensión.
- Colas y grupos de timbrado (`Local/201@from-queue-…;1` y `;2`): Iris ignora el canal Local y espera
  el que marca al teléfono (`PJSIP/201-…`) con el mismo Linkedid: la ventana sale a quienes de verdad
  les suena el teléfono, una sola vez por persona.
- Llamadas internas (de un teléfono a otro, o con un número de 2 a 7 dígitos): se ignoran.
- Salientes desde el teléfono de escritorio: se guardan **solo si el número es de un cliente**, sin
  ventana. Las personales no quedan en Iris.

## Si algo falla

| Lo que ves en Iris | Qué revisar |
| --- | --- |
| «No se pudo conectar» | VPN o ruta hasta la IP; `tlsenable` y el 5039; firewall; usuario y contraseña |
| «Este usuario puede hacer cambios» | `write =` vacío en `iris_lectura` y `manager reload` |
| «El certificado de la centralita cambió» | Si renovaste el certificado, compara la huella nueva y confírmala; si no, avisa: puede haber alguien en medio |
| «Sin conexión desde …» | Iris reintenta sola. Las llamadas de ese rato **no** quedan en Iris |

## Para E4c (más adelante): contestar y llamar desde el navegador

E4c todavía no existe. Cuando llegue, Asterisk tendrá que ofrecer SIP sobre WebRTC:

- `http.conf`: `tlsenable=yes`, `tlsbindaddr=0.0.0.0:8089`, el mismo certificado (de preferencia uno
  válido para el nombre con el que entra el navegador).
- `pjsip.conf`: un transporte `transport-wss` (`protocol=wss`) y, por cada persona que conteste en el
  navegador, un endpoint con `webrtc=yes` (DTLS, ICE, `rtcp_mux`, opus/ulaw).
- ICE: STUN, y TURN si el navegador no llega por la VPN.
- Abrir 8089 (WSS) y el rango RTP solo a donde haga falta.

La credencial SIP de cada extensión del navegador la guardará Iris cifrada, solo en el servidor, y la
entregará al navegador por sesión y con vida corta; nunca en un log.
