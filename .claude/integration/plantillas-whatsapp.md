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
- **Campana de avisos (notificaciones):** los eventos `plantilla_whatsapp.estado_cambiado` y
  `.categoria_cambiada` están listos para avisar «Meta aprobó / rechazó» (todavía nadie los consume).

## Lo que ofrece

- Fase 2 (panel): `/administracion/plantillas-whatsapp`, pantallas a/b/c/d/e de la maqueta.
