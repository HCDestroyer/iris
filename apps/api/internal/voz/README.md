# voz — protocolo con el Servidor de IA («Iris Voz»)

Contrato: `.claude/integration/contratos/voz.md` §4. Guía de instalación: `docs/runbooks/servidor-ia.md`.

Dos mitades en un paquete que **no importa `internal/mensajeria` ni nada con acceso a la base**
(`TestSinDependenciasDeIris` lo comprueba):

- **La puerta** (`cmd/voz-puerta`, que corre en el servidor dedicado): termina TLS 1.3 y verifica en
  toda ruta el certificado de cliente fijado por huella, la firma HMAC (método, ruta, marca, nonce,
  versión de Iris y sha256 del cuerpo), la ventana de tiempo y el nonce. Rutas: `/enlazar` (solo con
  la ventana de enlace abierta), `/version`, `/salud`, `/metricas`, `/credencial` y el proxy
  `/llm/v1/chat/completions` y `/llm/v1/models`, que reserializa la petición con una lista cerrada de
  campos antes de mandarla a Ollama. No hay cola: sin plaza, 503.
- **El cliente** (`Cliente`, `Enlazar`), que usa mensajería: fija la huella del servidor, presenta su
  certificado, firma cada petición y exige la firma de la respuesta.

Otras órdenes del mismo binario: `medidor` (lee la GPU con `nvidia-smi`; sin red) y `proxy-descarga`
(la única salida, solo al registro de modelos).

No emite ni consume eventos: no toca la base ni el outbox. Los eventos del Servidor de IA
(`voz.motor_*`, `servidor_ia.*`) los emite mensajería.

## Qué guarda

Solo el directorio de estado (`IRIS_VOZ_ESTADO`, archivos 0600): token, huella del certificado de
cliente fijado, certificado del servidor, IP observada, modelos permitidos y, mientras dura, la
ventana de enlace. Nunca prompts ni respuestas, tampoco en logs.

## Pruebas

`go test ./internal/voz/` levanta la puerta real (TLS de verdad en loopback) delante de un Ollama
falso (`vozprueba`): emparejamiento, mTLS, firma, repetición, rotación, renovación de certificado,
reserialización, topes y caída. Con Docker: `sudo infra/voz/prueba.sh falso|real` (etiqueta de
compilación `vozcompose`).
