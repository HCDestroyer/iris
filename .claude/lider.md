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
8. **Comandos de servidor en inglés, nunca en español** (17 nov 2026: «los comandos del server de iris, que se mantengan en ingles»). Vale para `irisctl` y para `iris-voz`: nombres de comandos y opciones (`install`, `upgrade`, `pair`, `status`, `model pull`…). Interpretación del líder: los mensajes que imprimen y la documentación siguen en español.
9. **Agentes nuevos con Opus** (18 nov 2026: «seria lo ideal, que los nuevos agentes sean con opus»): al lanzar un agente nuevo se le pide el modelo Opus, para gastar menos del modelo de la sesión. Los que ya corren siguen con el suyo. El modelo de la propia sesión lo cambia el PM desde la app; el líder no puede.

## Cómo trabaja el líder

- Coordina agentes (servidor I1/I2, panel I3, campo I4, infra I5), cada uno en su worktree y su rama `agente/<nombre>`; el contrato se escribe y se sube primero para que el panel trabaje en paralelo.
- **Revisión de seguridad por un subagente en cada PR**; los hallazgos vuelven al agente dueño y se corrigen con prueba.
- **Merge:** de una en una, solo squash con `sha=` y CI en verde contra el `main` actual. Tras cada merge, `update-branch` en las demás. Conflictos de squash contra historia: gana `main`. Sin force-push, sin `git stash` a secas, sin saltar pruebas, sin commits vacíos, sin IDs de modelo en commits ni PRs.
- **Publicar:** PR de release-please → `ci.yml` e `imagenes.yml` en esa rama → merge → verificar `install.sh` y el `.tar.gz` con sus `.sha256` → si cambió la app, `movil-apk.yml` en `main` con `api_url=https://iris.kns.gt` y enlace del artefacto al PM.
- **Otros sistemas:** no se edita `argos-isp`. Los cambios de contrato se acuerdan con el líder de Argos por mensaje entre sesiones y se espejan con PRs enlazadas (mismo sha256 del archivo).
- GitHub por REST (`gh api`); GraphQL no está disponible. Listo para revisión: `POST /pulls/N/ccr/ready_for_review`.
- Un fallo de CI por red (504 de GitHub, descargas) se reintenta una vez; si se repite, se arregla la causa en CI.

## Estado (20 nov 2026)

- **Publicado:** 0.34.0 (E4a centralita Issabel por AMI, solo lectura, TLS 5039 con huella fijada, llamadas en `mensajeria.llamadas` con `origen=centralita`, #235). 0.33.0 (asociar cliente sin servicios #233; B4 reconexión `sin_personal` #230; Iris informa contrato 0.8.1 #237). 0.32.0 (restablecer contraseña de otro usuario #234). 0.31.0–0.29.1: B5 cuentas bancarias #228, B7 conexión de un paso #231, eliminar tokens y conectores #229, SSE optimizado #224/#227, compositor e imágenes del chat #225, servicios identificables #220, `buscar_red` #222.
- **Contrato con Argos 0.8.1: implementado completo en Iris** (B4–B7) y espejado (iris#238 = argos-isp#264, sha256 `016fada5…c9dd447581`). Argos ≥ 1.21.0 (publicó hasta 1.22.1).
- **URL de Iris del PM: `https://iris.kns.gt`** (dicho por el PM el 20 nov; recordarla siempre). Es la `api_url` de la APK (`gh api -X POST …/actions/workflows/movil-apk.yml/dispatches -f ref=main -f 'inputs[api_url]=https://iris.kns.gt'`). Las releases fallan el job «APK de Android / Versión» hasta que el PM cree la variable de Actions `IRIS_MOVIL_API_URL` con ese valor (el proxy no deja al líder tocar variables) y los secretos del entorno `android-firma` (`docs/runbooks/movil-apk.md`).
- **Centralita:** E4a publicada (0.34.0). El PM: Iris llega a la Issabel por la misma red o por WireGuard (las dos sirven); E4c (softphone SIP sobre WebRTC dentro de Iris) decidida por el PM, va después. Falta que el PM la configure con el runbook `docs/runbooks/centralita-issabel.md` (usuario AMI con `write =` vacío).
- **Regla de merge:** se lee el CI antes de mergear; nunca encadenar el merge con la espera del CI. No mergear nada a `main` mientras una PR de versión espera sus corridas. release-please NO actualiza su PR con commits `test:`/`docs:`: si queda `behind`, `update-branch` y despachar sobre el head nuevo.
- **Migraciones:** `goose.NewProvider` no admite fuera de orden. Una migración con marca anterior a la última ya PUBLICADA rompe el `irisctl upgrade`: las PRs que se quedan atrás de una versión re-sellan sus migraciones con `goose create` antes de entrar.
- **Excepción a §6 de `CLAUDE.md` (pendiente de que el PM autorice anotarla ahí; está en el README de integración):** `ArgsRelay` no usa `unique` de River (River obliga a contar `running`, y eso dejaba eventos esperando al sondeo). Lo acotan su cola propia `relay` (1 worker), el advisory lock y que ni el listener ni el periódico encolan si ya hay un job `available`.
- **Meta real (17–18 nov):** conectores de Meta en la sesión (solo lectura salvo lo que el PM confirme). Negocio «Kingnet Solution» (verificado, límite 2 000). App «Iris Cloud» (modo desarrollo, webhooks a `iris.kns.gt`); app «Kingnet Solution» (publicada, de otro sistema: no tocar). Número de Iris: +502 4746 8639. El +502 3838 3896 lo atiende el otro sistema; el +502 5758 5925 es de la app WhatsApp Business (sin llamadas por API).
  - Falta para publicar la app en Meta: llenar «Datos legales», pegar `/en/privacy`, `/en/terms`, `/en/data-deletion` en Iris Cloud, categoría, icono, verificar correo, y la revisión de la app (caso de uso, video, llamadas reales, uso de datos). La acción para eliminar los datos de un contacto NO existe todavía y las páginas la prometen como manual.
- **Deuda anotada:** el buscador global (Ctrl K) y el menú «Crear» navegan sin pasar por la protección de salida (resolver una vez en `useProteccionSalida`); el estado del número marcado por un envío rechazado no llega en vivo al panel; `permisoBoleta` audita dos veces un rechazo; el asistente lee Argos dos veces al registrar una boleta; algunos `fetch` directos (`lib/voz/api.ts`, `lib/chat/*`) no pasan por el borrado de datos locales ante un 401 (lo cubre la siguiente petición del cliente del panel).
- **Lecciones (17–19 nov):** «flake» nunca es causa: tres pruebas «inestables» resultaron ser un defecto real del relay (#213), una prueba dependiente de la medianoche de Guatemala (#210) y una respuesta falsa consumida por otra petición (#215); un doble de prueba debe portarse como el sistema real (Argos solo guarda la idempotencia de los 2xx); los dobles devuelven los campos REALES; toda relectura de fondo del panel es silenciosa, cancelable y con guarda de `version` (si falla, lo que hay en pantalla se queda); un flujo SSE por un proxy propio necesita exención de plazos solo en rutas exactas, GET, sin cuerpo y sin escapes; medir las consultas del camino caliente con datos a escala (`JobList` de River: 76 ms contra 0,008 ms); el contenedor se reinicia: los agentes hacen commit y push pronto; el disco se llena con la caché de Go (`go clean -cache` solo sin procesos `go`) y con worktrees viejos.
- **Voz, lo que NO está validado:** ninguna llamada ha sonado aún contra Meta real (el PM no ha confirmado la prueba tras 0.28.1). La marca «En pruebas» (`LLAMADAS_EN_PRUEBAS`) se quita cuando complete `docs/runbooks/voz-prueba-manual.md`.
- **Pendiente del PM:** variable `IRIS_MOVIL_API_URL`; configurar la Issabel; decisión: clientes de Argos sin servicios ¿visibles en todas las empresas o solo con `empresas.todas`?; GPU del servidor de IA y entorno `voz-release` + ruleset de etiquetas `voz-v*`; autorizar la nota de la excepción en `CLAUDE.md` §6; maqueta de plantillas de WhatsApp; preguntas de D2; correo `info@kns.gt`; grabar o no las llamadas.
- **Pendiente de construir:** CLI `iris user reset-password --email --password-stdin`; E4c softphone; plantillas de WhatsApp; D2 historial por conversación resuelta; video y audio en adjuntos; acción de eliminar los datos de un contacto. Deuda nueva: el aviso emergente tapa «Guardar» en escritorio; `AvisoLlamada` con `bg-card` transparente en modo oscuro; fechas sin zona en soporte/campo durante una caída de Argos.
- **Después:** E1c grabación, E2 el asistente contesta llamadas, E3; Iris Cloud; Wi-Fi por SmartOLT; feriados en la agenda.

## Maquetas

Las maquetas viven en el scratchpad de la sesión y se pierden con ella. Las pantallas reales y sus reglas quedan en `docs/ui/guia.md`.

## Rediseño de Iris (pedido del PM, 2026-11-21)

- Iris adopta el rediseño de Argos con sus mismas reglas. **El teal ya no es el color**: se usa la paleta del rediseño de Argos (índigo #4F46E5, falta que Argos confirme cuáles son los tokens vigentes).
- «Sistema visual» = el kit que Iris ya usa: HeroUI v3 envuelto en `@iris/ui`. Nada de elementos crudos.
- Se puede personalizar con CSS propio cuando haga falta, dentro de los envoltorios de `packages/ui`.
- Los modales se usan según haga falta (Argos los mejoró). Se agregan menús contextuales e íconos de Lucide donde ayuden.
- Campos de formulario: los componentes que trae HeroUI v3, envueltos en `@iris/ui` (DatePicker y DateRangePicker para fechas, NumberField con moneda para precios, TextField, TextArea, Select, ComboBox, Switch, etc.). Nunca campos hechos a mano.
- Flujo: maqueta y capturas al PM → aprobación → actualizar §10 de CLAUDE.md, `docs/ui/guia.md` y la regla de ESLint → implementar.
