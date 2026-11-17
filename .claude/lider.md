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

- **Publicado:** 0.24.0, el asistente agenda visitas técnicas (`ofrecer_visita`, `agendar_visita`; §17.10). Antes: 0.23.0, el asistente que atiende solo (fase 3b) y contrato 0.7.1; 0.22.0, categorías, «Pruébalo antes de guardar», boletas con el rol `caja` y «Vincular mi usuario de Argos».
- **Regla de merge, recordatorio (16 nov 2026):** se lee el resultado del CI antes de mergear; nunca encadenar el merge en el mismo comando que espera el CI (así entró la #167 con una prueba inestable en rojo).
- **Contrato con Argos:** 0.7.1 en los dos repos (A24); Argos 1.16.0 lo publica. Un Argos 1.15.x sigue siendo compatible (no manda `verificado_iris_actor`).
- **Fase 3b, cómo quedó:** apagada de fábrica; acciones de dinero apagadas; saldo, promesa y reconexión piden verificación fuerte (DPI o NIT contra Argos) con vigencia de 60 min configurable; reconexión solo fuera de horario; perfiles por horario.
  - Decisión del líder (el PM puede cambiarla): recibir una boleta basta con reconocer el número, porque la aprueba una persona de caja; dar el saldo pide verificación fuerte de fábrica y cada empresa lo configura.
  - Visitas: apagado de fábrica; hasta 3 ventanas (8–12 y 13–17), sin técnico; basta el número (DPI o NIT opcional por empresa, vale para todos los perfiles); ticket previo; con mora pasa a persona; con una visita abierta el asistente dice su número y fecha (decisión del líder). Falta: avisar en la conversación cuando una orden queda sin programar, origen de la orden en la app de campo, feriados en la agenda.
- **Pendiente del PM:** captura de «Revisar ahora» del conector de Argos (Administración → Integraciones → Argos; por bodegas: Iris muestra «Argos < 1.6.0»); quién da el correo de kns.gt (Google, Microsoft, cPanel) para decirle servidor y puerto; para voz: grabar o no y retención, versión y ubicación de Issabel, número de pruebas de Meta con llamadas.
- **Respuestas del PM (16–17 nov 2026):** el correo del sistema se configura desde el panel, no en el `.env` (remitente `info@kns.gt`). Ollama no corre en el mismo servidor que Iris. Voz: los números pueden ser por API o por QR (las llamadas solo en los de API); el asistente también contesta llamadas, con reglas de seguridad de fábrica contra extracción de datos, inyección de instrucciones y mal uso; el motor de voz es un producto propio («Iris Voz») para un servidor privado dedicado con GPU; los agentes también contestan (en el panel).
- **En curso:** correo del sistema configurable (servidor y panel). Fase de voz: contrato en `contratos/voz.md`, revisado por seguridad (E1 personas en el panel y E2 asistente, aptas; E3 condicionada a la prueba con Meta; E4 Issabel). Orden: E1 → E2 → E3 → E4. El chat ya cuenta los intentos de verificación por cliente (§17.8.2); la voz se suma en E3.
- **Después:** Iris Cloud en `cloud.iris.kns.gt` (falta servidor), cambios de Wi-Fi por Argos ↔ SmartOLT, feriados en la agenda, aviso en la conversación de una orden sin programar, origen de la orden en la app de campo.

## Maquetas

Las maquetas viven en el scratchpad de la sesión y se pierden con ella. Las pantallas reales y sus reglas quedan en `docs/ui/guia.md`.
