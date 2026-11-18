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

## Estado (18 nov 2026)

- **Publicado:** 0.28.0 (Servidor de IA con tablero, #195; páginas públicas de privacidad, términos y eliminación de datos con «Datos legales», #197). 0.28.1 en publicación (PR #200): corrige la lectura del límite de mensajería de Meta, que impedía encender las llamadas (#199). Antes: 0.27.0 (salientes), 0.26.0 (entrantes), 0.25.0 (correo del sistema), 0.24.0, 0.23.0, 0.22.0.
- **Regla de merge:** se lee el CI antes de mergear; nunca encadenar el merge con la espera del CI. No mergear nada a `main` mientras una PR de versión espera sus corridas (cambia su head y hay que relanzarlas).
- **Primera prueba contra Meta real (17–18 nov):** el PM conectó los conectores de Meta a la sesión (WhatsApp Business Tools y Meta Social Technologies; solo lectura salvo lo que confirme). Negocio «Kingnet Solution» (verificado, límite 2 000). Apps: «Iris Cloud» (nueva, modo desarrollo, webhooks a `iris.kns.gt`, campos `messages`, `calls`, `account_update`, `phone_number_quality_update`) y «Kingnet Solution» (publicada, apunta a otro sistema en `ctrl.kingnetsolution.com`; no tocar). Número nuevo para Iris: +502 4746 8639, registrado y suscrito a Iris Cloud. El +502 3838 3896 lo atiende el otro sistema. El +502 5758 5925 es de la app WhatsApp Business (coexistencia: sin llamadas por API). Los mensajes entrantes ya llegan a Iris con la app sin publicar.
  - Fallos hallados y su estado: límite de mensajería leído con un campo deprecado (corregido, 0.28.1); motivo del rechazo de Meta invisible y entrantes rechazadas sin rastro (PR #201, servidor; falta el panel); cuentas bancarias de Argos leídas con campos que no existen (rama `agente/cuentas-argos-campos`).
  - Falta para publicar la app en Meta: `irisctl upgrade` a 0.28.x, llenar «Datos legales», pegar `/en/privacy`, `/en/terms`, `/en/data-deletion` en Iris Cloud, categoría, icono, verificar correo, y la revisión de la app (caso de uso, video, llamadas reales, uso de datos). La acción para eliminar los datos de un contacto NO existe todavía y las páginas la prometen como manual.
- **En curso:**
  - Bandeja (I3, `agente/i3-bandeja-pulido`): abrir en el último mensaje, desbordes del compositor y de las etiquetas, adjuntar como WhatsApp Web con vista previa, indicador «Sin conexión» intermitente (el PM tiene Nginx Proxy Manager y luego Virtualmin delante; sospecha: búfer del proxy).
  - Asistente (I2, `agente/i2-asistente-conversacion`): «contestó» falso tras tomar y soltar, pausar/reanudar por conversación, resumen guardado; diseño de un historial por cada resolución (solo diseño, se confirma con el PM).
  - Contrato con Argos 0.8.0 candidato (`lider/contrato-argos-0.8.0`, head `91e955b`, revisado por Argos; Argos lo publica como 1.18.0): reconexión `sin_personal` y vigencia a medianoche, cuentas bancarias como única fuente, más datos del servicio (precio, IP, ubicación, router con permisos propios), conexión de un paso con primera sincronización. **No se espeja hasta que el PM confirme expresamente el criterio de `sin_personal`** (propuesto: horario hábil y ningún usuario con rol `caja` disponible; apagado de fábrica; verificación fuerte). Decisiones pendientes del PM en §10.2 del contrato.
  - Plantillas de WhatsApp: maqueta enviada (crear, diccionario de fábrica, sincronizar las existentes de Meta, enlace con los avisos del sistema); hoy Iris no crea ni sincroniza plantillas y varios textos de fábrica chocarían con las reglas de Meta. Se construye tras los fallos; las reglas de Meta se verifican contra su documentación antes.
- **Servidor de IA:** en `main` y publicado en 0.28.0. El paquete «Iris Voz» se publica aparte con etiqueta `voz-v*`; ese flujo nunca se ha ejecutado y espera la protección en GitHub (regla de etiquetas y entorno `voz-release`), que debe configurar el PM porque el proxy no deja al líder. Sin probar con GPU real ni con el instalador en un servidor. El PM preguntó por una Mac M4 de 16 GB: se le ofreció un «modo Mac», sin respuesta.
- **Pendiente del PM:** actualizar a 0.28.1 y encender las llamadas del +502 4746 8639; confirmar el criterio de `sin_personal`; decidir si se construye ya la acción de eliminar los datos de un contacto; visto bueno o cambios a la maqueta de plantillas; probar la configuración del proxy para «Sin conexión»; protección en GitHub para Iris Voz; correo `info@kns.gt`; grabar o no las llamadas; Issabel.
- **Lecciones (17–18 nov):** una prueba que busca un número corto en un JSON con ids al azar falla por casualidad (montos inconfundibles y comprobación estructural); los dobles de Argos y de Meta deben devolver los campos REALES (dos fallos de producción vinieron de dobles infieles); un dato no leído nunca es «Falta», es «Sin comprobar» y no bloquea; todo aviso o correo disparado por un sistema remoto lleva enfriamiento e histéresis; Argos caído o conector apagado no puede impedir registrar trabajo (se encola); el disco se llena con la caché de Go (`go clean -cache` solo sin procesos `go`) y con worktrees viejos (quitarlos al mergear).
- **Voz, lo que NO está validado:** ninguna llamada ha sonado aún contra Meta real. La marca «En pruebas» (`LLAMADAS_EN_PRUEBAS`) se quita cuando el PM complete el guion `docs/runbooks/voz-prueba-manual.md`. Supuesto 1 resuelto (campo `whatsapp_business_manager_messaging_limit`).
- **Después:** E1c grabación, E2 el asistente contesta llamadas, E3, E4 Issabel; Iris Cloud; Wi-Fi por SmartOLT; feriados en la agenda.

## Maquetas

Las maquetas viven en el scratchpad de la sesión y se pierden con ella. Las pantallas reales y sus reglas quedan en `docs/ui/guia.md`.
