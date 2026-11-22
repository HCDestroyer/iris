# Ficha del prospecto

## Para qué sirve

Llevar todo el proceso de venta de un prospecto: sus datos, la factibilidad de instalar en su dirección, la cotización, el contrato con firma y las actividades (llamadas, visitas, notas). Al ganarlo, Iris lo da de alta en Argos y abre la orden de instalación.

## Cómo se usa

1. Abre un prospecto desde [Comercial](/manual/comercial/embudo).
2. Revisa las pestañas **Resumen**, **Factibilidad**, **Cotización**, **Contrato** y **Actividad**.
3. Registra lo que hablas con él en **Nueva actividad**: qué pasó, el resultado y la próxima acción con su día.
4. **Factibilidad:** pulsa **Volver a evaluar** para medir la distancia al router más cercano. Si alguien fue al terreno, registra la **Factibilidad manual** con el router desde donde se atiende.
5. **Cotización:** cuando la factibilidad es viable o con obra, crea la cotización: [Cotización](/manual/comercial/cotizacion). Cuando el cliente la acepta, pulsa **El cliente la aceptó**.
6. **Contrato:** pulsa **Generar contrato**. El cliente firma en la pantalla, con su nombre y, si quiere, su DPI o NIT. Pulsa **Guardar firma**.
7. Usa las acciones: **Marcar contactado**, **Asignar vendedor**, **Ganar**, **Perder** (con el motivo) o **Reabrir**.
8. Al **Ganar**, revisa los **Requisitos para ganar**. Si la factibilidad requiere obra, confirma que el cliente lo aceptó. Si no hay router sugerido, elige el **Router del servicio**.

## Qué significa cada estado

Etapas: **Nuevo**, **Contactado**, **Factibilidad**, **Cotizado**, **Contrato**, **Ganado** y **Perdido**.

Factibilidad:

- **Viable:** se puede instalar.
- **Requiere obra:** se puede, pero con trabajo extra.
- **No viable:** no hay cobertura.
- **Indeterminado:** no se pudo saber con los datos de la red (por ejemplo, sin coordenadas o sin conexión con Argos).

Cotización: **Vigente**, **Aceptada**, **Reemplazada** (hay una revisión nueva) o **Vencida**.

Contrato: **Borrador**, **Firmado** o **Anulado**.

Alta en Argos al ganar:

- **Creando el cliente en Argos** y **Creando el servicio en Argos:** Iris los está dando de alta.
- **Alta completa:** ya existen en Argos y se creó la orden de instalación.
- **Alta local:** Iris trabaja sin conexión con Argos; se creó la orden de instalación con los datos del prospecto.
- **El alta en Argos se detuvo y necesita revisión:** revisa los **Pendientes del alta en Argos** y pulsa **Reintentar**.

## Preguntas frecuentes

**¿Qué hace falta para ganar?**
Factibilidad viable (o con obra confirmada), cotización aceptada por el cliente y contrato firmado.

**La factibilidad dio «no viable», pero el técnico dice que sí se puede.**
Registra la factibilidad manual y escribe el motivo de la anulación.

**¿Puedo reabrir un prospecto perdido?**
Sí, con **Reabrir**.

## Permisos necesarios

- Ver: `prospectos.ver`. Acciones: `prospectos.contactar`, `prospectos.asignar`, `prospectos.ganar`, `prospectos.perder`, `prospectos.reabrir` y `prospectos.registrar_actividad`.
- Factibilidad: `factibilidad.evaluar`; la manual, `factibilidad.registrar_manual` (**Supervisor** y **Técnico**).
- Cotización: `cotizaciones.crear` y `cotizaciones.aceptar`. Contrato: `contratos.crear` y `contratos.firmar`.
- **Ventas** y **Supervisor** tienen todo el seguimiento comercial.
