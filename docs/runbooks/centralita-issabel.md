# Centralita Issabel: preparar Issabel 5 para Iris (E4a, solo lectura; E4c, el teléfono de Iris)

Para quien administra la centralita. Centralita de referencia: **Issabel 5** (issabel-pbx 5.0.0-4,
IssabelPBX 2.12.0-5) sobre Rocky Linux 8.10 con **Asterisk 18.19.0**. Contrato: `.claude/integration/
contratos/voz.md` §2.10 y §3.12.

En la parte de solo lectura (pasos 1–8) Iris **solo escucha**: no marca, no graba, no cambia nada en Issabel. Se conecta al AMI (Asterisk
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
  `message`, `aoc`, `cc`, `test` (o `all`) en `write`, Iris no se enciende. Iris no necesita ninguna clase de escritura: deja
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

## 9. El teléfono de Iris (E4c): contestar y llamar desde el panel y la app

Contrato: `.claude/integration/contratos/voz.md` §3.12, «E4c». **Iris es el puente**: el navegador y la
app hablan solo con Iris; Iris abre la conexión SIP con la Issabel por WSS (8089) y responde la contraseña
SIP en el servidor (nunca llega al navegador). El audio va siempre por el relevo TURN de Iris: la Issabel
solo habla con la IP de Iris, nunca con internet. Esta parte es aparte de la de solo lectura (1–8): el
teléfono funciona con o sin el AMI encendido, pero con el AMI la ventana con el cliente sale también en
las llamadas del teléfono y la duración de cada llamada sale del CDR.

### 9.1 WSS en la Issabel (8089)

En `/etc/asterisk/http.conf` (o en `http_custom.conf` si tu instalación lo incluye):

```ini
[general]
enabled = yes
bindaddr = 127.0.0.1
bindport = 8088
tlsenable = yes
tlsbindaddr = 0.0.0.0:8089
tlscertfile = /etc/asterisk/keys/ami.crt
tlsprivatekey = /etc/asterisk/keys/ami.key
```

Puede ser el mismo certificado del AMI (paso 2) u otro. Su huella (la compararás con la que muestra Iris):

```sh
openssl x509 -in /etc/asterisk/keys/ami.crt -noout -fingerprint -sha256
asterisk -rx "module reload http"
asterisk -rx "http show status"     # HTTPS Server Enabled and Bound to 0.0.0.0:8089
```

### 9.2 Transporte WSS de PJSIP

En `/etc/asterisk/pjsip_custom.conf` (o en «Asterisk SIP Settings › PJSIP» si tu versión ofrece WSS):

```ini
[transport-wss]
type = transport
protocol = wss
bind = 0.0.0.0
```

Y en `/etc/asterisk/rtp.conf`: `icesupport = yes` y el rango de audio (`rtpstart = 10000`, `rtpend = 20000`).
No hace falta STUN: el navegador siempre llega por el TURN de Iris.

### 9.3 Una extensión PJSIP por persona que usa el teléfono de Iris

En **PBX › Extensiones**, una extensión **PJSIP** propia para el teléfono de Iris de cada persona (no la del
teléfono de escritorio: así no se mezclan). Su contraseña (*secret*) es la que vas a escribir **una vez**
en Iris. En «Avanzado» (o en `pjsip_custom_post.conf` con `[201](+)`):

```ini
[201](+)
webrtc = yes                 ; DTLS-SRTP, ICE, AVPF y rtcp_mux
dtls_auto_generate_cert = yes
allow = !all,opus,ulaw
max_contacts = 1
remove_existing = yes
direct_media = no
```

`webrtc = yes` enciende `use_avpf`, `media_encryption = dtls`, `dtls_verify = fingerprint`,
`dtls_setup = actpass`, `ice_support`, `media_use_received_transport` y `rtcp_mux`. Luego:

```sh
asterisk -rx "pjsip reload"
asterisk -rx "pjsip show endpoint 201"     # webrtc: yes, con el transporte wss disponible
```

Para **salir a la calle**, la ruta de salida de la Issabel debe aceptar los 8 números de Guatemala con el
prefijo que uses (por ejemplo `9` + 8 números): ese prefijo se escribe en Iris como «Prefijo de salida».
Para **transferir a un grupo**, anota el número de cada grupo de timbrado o cola (PBX › Ring Groups /
Queues); en Iris se dan de alta en «Grupos».

### 9.4 Firewall de la Issabel

Solo la IP de Iris (o la red de la VPN) puede llegar a **TCP 8089** y al **rango UDP de audio**
(10000–20000). Nada de eso se abre a internet:

```sh
nc -zv <ip-publica-de-issabel> 8089     # desde fuera de la VPN: esperado, falla
```

### 9.5 En el servidor de Iris

En `/opt/iris/.env` (y `irisctl start` después):

```sh
IRIS_TELEFONO=true
IRIS_TURN_SECRETO=<la salida de: openssl rand -hex 32>
IRIS_TURN_URLS=turn:iris.kns.gt:3478?transport=udp,turn:iris.kns.gt:3478?transport=tcp
IRIS_TURN_RELAY_IP=10.20.0.9          # la IP con que la Issabel ve a Iris (VPN o misma red)
IRIS_TURN_ISSABEL_IP=10.20.0.5        # la única IP a la que el TURN deja pasar audio
```

- Abre en el firewall **del servidor de Iris** (desde internet): **UDP y TCP 3478** y **UDP 49160–49200**
  (`IRIS_TURN_PUERTO_MIN`–`MAX`). Desde la VPN, la Issabel tiene que llegar a esos mismos puertos UDP de
  `IRIS_TURN_RELAY_IP`.
- **Proxy delante de Iris:** el WebSocket del teléfono va directo a la API (el panel no reenvía
  WebSockets). La ruta exacta `/api/v1/mensajeria/telefono/sip` se manda a `127.0.0.1:<IRIS_API_PUERTO>`
  (8080) con *upgrade* (ver [install](install.md), «Reverse proxy»). En Nginx Proxy Manager, enciende
  «Websockets Support» en el Proxy Host.

### 9.6 En Iris

1. **Administración › Integraciones › Centralita › Teléfono**: puerto `8089`, ruta `/ws`, **Probar**:
   compara la huella con la del paso 9.1 (si no es la misma, no sigas). Guarda con la huella, el dominio SIP
   (vacío = la IP de la Issabel), el prefijo de salida y los límites (llamadas por persona y día, de
   fábrica 60; a la vez en la empresa, 10). **Probar** otra vez (con los datos guardados) y **Encender**.
2. **Extensiones**: a cada persona, su extensión con tipo **Navegador** y la contraseña SIP del paso 9.3
   (se guarda cifrada y no se vuelve a mostrar). **Probar registro** debe decir «ok».
3. En el panel, la persona abre su teléfono (permiso del micrófono la primera vez). Solo suena en una
   pestaña; las otras muestran «Usar el teléfono aquí».

### 9.7 Si algo falla

| Lo que ves en Iris | Qué revisar |
| --- | --- |
| Probar: «No conecta» | 8089 y `tlsenable` en `http.conf`; firewall solo con la IP de Iris; VPN |
| Probar: «No es SIP» | `transport-wss` en PJSIP y `pjsip reload`; la ruta `/ws` |
| Probar: «El certificado cambió» | Si renovaste el certificado del 8089, compara la huella nueva; si no, avisa |
| Probar registro: «Credenciales» | La contraseña de la extensión PJSIP y que sea la extensión del navegador |
| No hay audio o se corta a los pocos segundos | Puertos UDP del TURN en el servidor de Iris; `IRIS_TURN_RELAY_IP` alcanzable desde la Issabel; `webrtc = yes` |
| «La centralita no responde» al abrir el teléfono | Igual que «No conecta»; Iris reintenta al volver a abrir |
