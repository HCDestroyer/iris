# Memoria del líder de Iris

Este archivo es la memoria del líder entre sesiones. La conversación se comprime y el contenedor se reinicia; lo que no esté aquí o en `CLAUDE.md` se pierde. **El líder lo lee al empezar y lo actualiza cuando cambia una regla, una decisión del PM o el estado del trabajo.**

## Reglas del PM

1. **Siempre en español**, sin excepción (también en `CLAUDE.md`). Mensajes breves, con el resultado primero: el PM no es técnico y lee en el teléfono.
2. **Avanzar sin esperar.** Si el PM ya dijo cómo lo quiere, se construye y se le muestra el resultado. Solo se le pregunta cuando de verdad no se puede decidir; mientras responde, se sigue con lo demás. Quedarse esperando un «sí» que ya dio es un error (16 nov 2026: «¿Por qué no has avanzado?»). Esto vale para decisiones de producto y de diseño: no sustituye el CI en verde, la revisión de seguridad de cada PR ni la confirmación de acciones irreversibles o que afecten a otros sistemas.
3. **Diseño antes de código en pantallas nuevas:** una maqueta funcional, consistente y práctica (teléfono primero). Si pide cambios, se rehace la maqueta y se arranca; no se pide un segundo visto bueno.
4. **Amigable y práctico:** lenguaje llano, pocas pantallas, lo avanzado escondido, y una forma de probar antes de guardar.
5. **Todo configurable:** lo que puede o no hacer el asistente lo decide cada empresa; nada prohibido de fábrica salvo lo que exige la seguridad o un contrato.
6. **Proveedor de IA libre:** Ollama, compatibles con OpenAI y Anthropic; ninguno obligatorio.
7. Los secretos que el PM configuró (Firebase, claves) nunca pasan por el chat.

## Cómo trabaja el líder

- Coordina agentes (servidor I1/I2, panel I3, campo I4, infra I5), cada uno en su worktree y su rama `agente/<nombre>`; el contrato se escribe y se sube primero para que el panel trabaje en paralelo.
- **Revisión de seguridad por un subagente en cada PR**; los hallazgos vuelven al agente dueño y se corrigen con prueba.
- **Merge:** de una en una, solo squash con `sha=` y CI en verde contra el `main` actual. Tras cada merge, `update-branch` en las demás. Conflictos de squash contra historia: gana `main`. Sin force-push, sin `git stash` a secas, sin saltar pruebas, sin commits vacíos, sin IDs de modelo en commits ni PRs.
- **Publicar:** PR de release-please → `ci.yml` e `imagenes.yml` en esa rama → merge → verificar `install.sh` y el `.tar.gz` con sus `.sha256` → si cambió la app, `movil-apk.yml` en `main` con `api_url=https://iris.kns.gt` y enlace del artefacto al PM.
- **Otros sistemas:** no se edita `argos-isp`. Los cambios de contrato se acuerdan con el líder de Argos por mensaje entre sesiones y se espejan con PRs enlazadas (mismo sha256 del archivo).
- GitHub por REST (`gh api`); GraphQL no está disponible. Listo para revisión: `POST /pulls/N/ccr/ready_for_review`.
- Un fallo de CI por red (504 de GitHub, descargas) se reintenta una vez; si se repite, se arregla la causa en CI.

## Estado (16 nov 2026)

- **Publicado:** 0.22.0. Fase 3 sin autonomía: categorías de mensajes (cola y SLA por categoría), clasificación, pantalla simple con «Pruébalo antes de guardar», efecto en el SLA; aprobar y rechazar boletas por una persona (rol `caja`) y «Vincular mi usuario de Argos». Antes, 0.21.0: asistente que sugiere, aviso del tope, saldo sin monto, diagnóstico del conector de Argos.
- **Contrato con Argos:** 0.7.0 en los dos repos (A23). La 0.7.1 (`verificado_iris_actor`, PR #164) espera a que Argos mergee su espejo argos-isp#245 con el mismo sha256.
- **En curso, fase 3b (el asistente atiende solo):**
  - Servidor: PR #163, cinco rondas de revisión de seguridad, la última apta. Dinero apagado de fábrica; saldo, promesa y reconexión piden verificación fuerte con vigencia (60 min por defecto).
  - Decisión del líder (el PM puede cambiarla): recibir una boleta basta con reconocer el número, porque la aprueba una persona de caja; dar el saldo pide verificación fuerte de fábrica y cada empresa lo configura.
  - Panel: rama `agente/i3-asistente-3b`, sobre la del servidor.
  - Falta: `agendar_visita` necesita la lectura de huecos libres de la agenda (I2), sin empezar.
- **Pendiente del PM:** captura de «Revisar ahora» del conector de Argos (bodegas: Iris muestra «Argos < 1.6.0»); qué correo usar para `IRIS_SMTP_*`; si Ollama corre en el mismo servidor (`IRIS_ASISTENTE_PERMITIR_LOOPBACK`).
- **Después:** fase 4 (voz: Issabel o WhatsApp), Iris Cloud en `cloud.iris.kns.gt` (falta servidor), cambios de Wi-Fi por Argos ↔ SmartOLT.

## Maquetas

Las maquetas viven en el scratchpad de la sesión y se pierden con ella. Las pantallas reales y sus reglas quedan en `docs/ui/guia.md`.
