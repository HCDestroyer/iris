# Agente de plantillas de WhatsApp: dependencias

Contrato: `contratos/plantillas-whatsapp.md`.

## Lo que pide a otros

- **Líder y PM (Meta, fuera del repositorio):** suscribir la app de Meta («Iris Cloud») a los campos de
  webhook `message_template_status_update`, `template_category_update` y
  `message_template_quality_update` de la cuenta. Sin eso, el estado llega solo con la relectura de cada
  noche o el botón «Sincronizar con Meta».
- **Líder y PM:** el token de cada número debe tener `whatsapp_business_management` (además de
  `whatsapp_business_messaging`). Si falta, Iris lo dice en llano (424 `meta_sin_permiso`).
- **I1 (núcleo):** se agregaron `Problema.sugerencia` y `Problema.disponibleEn` (extensiones de RFC 9457)
  y los permisos `plantillas_whatsapp.*` (supervisor y soporte: ver y sincronizar).
- **Números de WhatsApp (mensajeria, formulario):** al crear o actualizar un número con WABA, Iris
  fija la empresa dueña de la cuenta (409 `waba_de_otra_empresa`) y comprueba la WABA en Meta con el
  token del número (422 `waba_no_coincide`). El panel de números debe mostrar los dos errores.
  Cambiar `plantillas` en el formulario exige `plantillas_whatsapp.editar`.
- **Bandeja (conversaciones):** una plantilla con datos del cliente responde 422
  `cliente_sin_confirmar` si la conversación no tiene el cliente confirmado; el panel debe pedir
  confirmar el cliente.
- **Campana de avisos (notificaciones):** los eventos `plantilla_whatsapp.estado_cambiado` y
  `.categoria_cambiada` están listos para avisar «Meta aprobó / rechazó» (todavía nadie los consume).

## Lo que ofrece

- Fase 2 (panel): `/administracion/plantillas-whatsapp`, pantallas a/b/c/d/e de la maqueta.
