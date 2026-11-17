# Iris Voz — el servidor de IA de Iris

Un paquete aparte para un servidor dedicado con GPU NVIDIA. Corre Ollama para el asistente de chat
de Iris detrás de una puerta autenticada (`puerta`). **No** es parte de la instalación de Iris: sin
Postgres, sin Redis, sin panel, y nunca abre una conexión hacia Iris. Guía completa:
`docs/runbooks/servidor-ia.md`. Contrato: `.claude/integration/contratos/voz.md` §4.

## Qué corre

| Servicio | Qué es | Red |
| --- | --- | --- |
| `puerta` | El único que escucha (puerto de control, 8443). TLS 1.3, TLS mutuo con huellas fijadas, firma HMAC y nonce en toda ruta. Solo deja pasar a Ollama el chat y la lista de modelos, y reserializa cada petición con una lista cerrada de campos | puerto publicado + interna |
| `llm` | Ollama. Volumen de modelos de solo lectura, sin salida | solo interna |
| `medidor` | Lee la GPU con `nvidia-smi` para el tablero de Iris | ninguna |
| `descarga` + `proxy-descarga` | Contenedores de un solo uso de `iris-voz modelo descargar`: la única salida, y solo al registro de modelos | interna + proxy |

Todos los contenedores son de solo lectura, sin privilegios (uid 65532), sin capacidades y con
límites de procesos y de memoria.

## Instalar

Requisitos: Ubuntu o Debian, Docker con el plugin compose (2.24 o más nuevo), el controlador de
NVIDIA y `nvidia-container-toolkit`, NTP activo e intercambio a disco apagado (o cifrado).

```sh
V=0.1.0
base=https://github.com/HCDestroyer/iris/releases/download/voz-v$V
curl -fsSLO $base/install-voz.sh
curl -fsSLO $base/install-voz.sh.sigstore.json
# Verifica el instalador ANTES de ejecutarlo (cosign 2.4 o más nuevo, de una fuente tuya):
cosign verify-blob --new-bundle-format --bundle install-voz.sh.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity https://github.com/HCDestroyer/iris/.github/workflows/voz-release.yml@refs/tags/voz-v$V \
  install-voz.sh
sudo bash install-voz.sh --version $V
```

El instalador verifica la firma del paquete **sin red** (bundle de Sigstore, raíz de confianza
embebida en el script, prueba del registro de transparencia obligatoria) y que lo firmó el workflow
`voz-release` de este repositorio en la etiqueta `voz-v<versión>`. Falla cerrado. Las imágenes van
fijadas por digest y los modelos por defecto, por el digest del manifiesto firmado.

Después se enlaza desde el panel de Iris (Administración → Integraciones → Servidor de IA): el panel
muestra un código de un solo uso, `sudo iris-voz enlazar` lo pide (no se ve al escribirlo) y el
panel muestra la huella del certificado que vio Iris: **compárala con la que imprimió este servidor
antes de pulsar «Activar»**.

## Día a día

```sh
sudo iris-voz estado                       # versión, contenedores, huella, IP de Iris, modelos
sudo iris-voz modelo descargar qwen2.5:7b  # baja un modelo (solo llega al registro de modelos)
sudo iris-voz modelo permitir qwen2.5:7b   # deja que Iris lo use (Iris nunca baja, borra ni crea modelos)
sudo iris-voz actualizar                   # se verifica igual que la instalación; nunca baja de versión
sudo iris-voz renovar-certificado          # certificado nuevo, anunciado a Iris por el canal vigente
sudo iris-voz desenlazar                   # olvida a Iris (credenciales de Iris robadas: desenlaza y enlaza de nuevo)
sudo iris-voz ip restringir                # opcional: aceptar solo la IP de Iris vista al enlazar
sudo iris-voz ayuda
```

Los ajustes viven en `/opt/iris-voz/.env`: `IRIS_VOZ_PUERTO`, `IRIS_VOZ_ESCUCHA`,
`IRIS_VOZ_LLM_SIMULTANEAS` (0 = se calcula con la VRAM y los modelos permitidos),
`IRIS_VOZ_LLM_MAX_TOKENS`, `IRIS_VOZ_LLM_KEEP_ALIVE`, `IRIS_VOZ_LLM_CONTEXTO`,
`IRIS_VOZ_LLM_PARALELO`, `IRIS_VOZ_LLM_MEMORIA`, `IRIS_VOZ_DESCARGA_HOSTS`. Después de cambiarlos:
`sudo iris-voz arrancar`.

## Qué se guarda y qué se registra

Solo las credenciales del canal (token, huella fijada y el certificado del servidor) en el volumen
`estado`, y los modelos. **Nunca prompts ni respuestas**: la puerta registra ruta, tamaño, duración
y código; Ollama corre sin historial ni registros de depuración.
