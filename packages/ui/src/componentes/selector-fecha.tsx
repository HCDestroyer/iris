"use client";

import { Calendar, DateField, DatePicker, DateRangePicker, Description, FieldError, Label, RangeCalendar, TimeField } from "@heroui/react";
import { parseDate, Time, today, type CalendarDate } from "@internationalized/date";
import { CircleAlert } from "lucide-react";
import { useRef, type FocusEvent, type ReactNode } from "react";
import { I18nProvider } from "react-aria-components";
import { cn } from "../utilidades";
import { PieCampo, useIdsCampo } from "./campo";
import { useControlNativo, type PropsControlNativo } from "./control-nativo";
import { useErrorEstable } from "./error-estable";

/*
 * Selector de fecha y de rango (HeroUI `DatePicker` / `DateRangePicker`, React Aria): se escribe
 * por segmentos (día, mes, año) o se elige en el calendario, con teclado completo. Los valores son
 * fechas sin hora (`AAAA-MM-DD`): la zona `America/Guatemala` solo decide qué día es «hoy» y el
 * formato es el de Guatemala (`es-GT`, día/mes/año). La base sigue en UTC.
 */

export const ZONA_GUATEMALA = "America/Guatemala";
const LOCALE = "es-GT";

/** Hoy en Guatemala como `AAAA-MM-DD`. */
export function hoyEnGuatemala(): string {
    return today(ZONA_GUATEMALA).toString();
}

/** `AAAA-MM-DD` → fecha de calendario; `null` si viene vacía o mal formada. */
export function aFecha(valor: string | null | undefined): CalendarDate | null {
    if (!valor) return null;
    try {
        return parseDate(valor);
    } catch {
        return null;
    }
}

type PropsComunes = {
    etiqueta: ReactNode;
    ayuda?: ReactNode;
    error?: string | undefined;
    /** Primer y último día permitidos (`AAAA-MM-DD`). */
    min?: string;
    max?: string;
    disabled?: boolean;
    obligatorio?: boolean;
    id?: string;
    className?: string;
};

export type PropsSelectorFecha = PropsComunes & {
    valor: string | null;
    alCambiar: (valor: string | null) => void;
    /** Nombre del campo oculto para formularios nativos. */
    name?: string;
};

/** `HH:MM` (24 h) → hora; `null` si viene vacía o mal formada. */
export function aHora(valor: string | null | undefined): Time | null {
    const m = valor ? /^(\d{1,2}):(\d{2})(?::\d{2})?$/.exec(valor) : null;
    if (!m) return null;
    const horas = Number(m[1]);
    const minutos = Number(m[2]);
    if (horas > 23 || minutos > 59) return null;
    return new Time(horas, minutos);
}

/** Hora → `HH:MM`, como la de un `<input type="time">`. */
export function deHora(hora: Time | null): string {
    if (!hora) return "";
    return `${String(hora.hour).padStart(2, "0")}:${String(hora.minute).padStart(2, "0")}`;
}

/** Ayuda y error enlazados por React Aria (`Description` / `FieldError` de HeroUI). */
function AyudaYError({ ayuda, error }: { ayuda?: ReactNode; error?: string | undefined }) {
    return (
        <>
            {ayuda ? <Description className="text-sm text-muted-foreground">{ayuda}</Description> : null}
            <FieldError className="flex items-center gap-1.5 px-0 text-sm text-error">
                <CircleAlert aria-hidden className="size-4 shrink-0" />
                {error}
            </FieldError>
        </>
    );
}

function Cabecera({ rango }: { rango?: boolean }) {
    const C = rango ? RangeCalendar : Calendar;
    return (
        <C.Header>
            <C.YearPickerTrigger>
                <C.YearPickerTriggerHeading />
                <C.YearPickerTriggerIndicator />
            </C.YearPickerTrigger>
            <C.NavButton slot="previous" />
            <C.NavButton slot="next" />
        </C.Header>
    );
}

export function SelectorFecha({ etiqueta, ayuda, error, min, max, disabled, obligatorio, id, className, valor, alCambiar, name }: PropsSelectorFecha) {
    const minimo = aFecha(min);
    const maximo = aFecha(max);
    return (
        <I18nProvider locale={LOCALE}>
            <DatePicker
                data-slot="selector-fecha"
                className={cn("grid gap-1.5", className)}
                value={aFecha(valor)}
                onChange={(fecha) => alCambiar(fecha ? fecha.toString() : null)}
                {...(minimo ? { minValue: minimo } : {})}
                {...(maximo ? { maxValue: maximo } : {})}
                {...(name ? { name } : {})}
                isDisabled={disabled ?? false}
                isRequired={obligatorio ?? false}
                isInvalid={Boolean(error)}
                {...(id ? { id } : {})}
            >
                <Label className="text-sm font-medium">{etiqueta}</Label>
                <DateField.Group fullWidth>
                    <DateField.Input>{(segmento) => <DateField.Segment segment={segmento} />}</DateField.Input>
                    <DateField.Suffix>
                        <DatePicker.Trigger aria-label="Abrir calendario">
                            <DatePicker.TriggerIndicator />
                        </DatePicker.Trigger>
                    </DateField.Suffix>
                </DateField.Group>
                <DatePicker.Popover>
                    <Calendar aria-label="Calendario">
                        <Cabecera />
                        <Calendar.Grid>
                            <Calendar.GridHeader>{(dia) => <Calendar.HeaderCell>{dia}</Calendar.HeaderCell>}</Calendar.GridHeader>
                            <Calendar.GridBody>{(fecha) => <Calendar.Cell date={fecha} />}</Calendar.GridBody>
                        </Calendar.Grid>
                        <Calendar.YearPickerGrid>
                            <Calendar.YearPickerGridBody>{({ year }) => <Calendar.YearPickerCell year={year} />}</Calendar.YearPickerGridBody>
                        </Calendar.YearPickerGrid>
                    </Calendar>
                </DatePicker.Popover>
                <AyudaYError ayuda={ayuda} error={error} />
            </DatePicker>
        </I18nProvider>
    );
}

export type RangoFechas = { inicio: string; fin: string };

export type PropsSelectorRangoFechas = PropsComunes & {
    valor: RangoFechas | null;
    alCambiar: (valor: RangoFechas | null) => void;
};

/** Rango de fechas (desde–hasta), p. ej. el periodo de un reporte o una ausencia. */
export function SelectorRangoFechas({ etiqueta, ayuda, error, min, max, disabled, obligatorio, id, className, valor, alCambiar }: PropsSelectorRangoFechas) {
    const inicio = aFecha(valor?.inicio);
    const fin = aFecha(valor?.fin);
    const minimo = aFecha(min);
    const maximo = aFecha(max);
    return (
        <I18nProvider locale={LOCALE}>
            <DateRangePicker
                data-slot="selector-rango-fechas"
                className={cn("grid gap-1.5", className)}
                value={inicio && fin ? { start: inicio, end: fin } : null}
                onChange={(r) => alCambiar(r ? { inicio: r.start.toString(), fin: r.end.toString() } : null)}
                {...(minimo ? { minValue: minimo } : {})}
                {...(maximo ? { maxValue: maximo } : {})}
                isDisabled={disabled ?? false}
                isRequired={obligatorio ?? false}
                isInvalid={Boolean(error)}
                {...(id ? { id } : {})}
            >
                <Label className="text-sm font-medium">{etiqueta}</Label>
                <DateField.Group fullWidth>
                    <DateField.Input slot="start">{(segmento) => <DateField.Segment segment={segmento} />}</DateField.Input>
                    <DateRangePicker.RangeSeparator />
                    <DateField.Input slot="end">{(segmento) => <DateField.Segment segment={segmento} />}</DateField.Input>
                    <DateField.Suffix>
                        <DateRangePicker.Trigger aria-label="Abrir calendario">
                            <DateRangePicker.TriggerIndicator />
                        </DateRangePicker.Trigger>
                    </DateField.Suffix>
                </DateField.Group>
                <DateRangePicker.Popover>
                    <RangeCalendar aria-label="Calendario">
                        <Cabecera rango />
                        <RangeCalendar.Grid>
                            <RangeCalendar.GridHeader>{(dia) => <RangeCalendar.HeaderCell>{dia}</RangeCalendar.HeaderCell>}</RangeCalendar.GridHeader>
                            <RangeCalendar.GridBody>{(fecha) => <RangeCalendar.Cell date={fecha} />}</RangeCalendar.GridBody>
                        </RangeCalendar.Grid>
                        <RangeCalendar.YearPickerGrid>
                            <RangeCalendar.YearPickerGridBody>{({ year }) => <RangeCalendar.YearPickerCell year={year} />}</RangeCalendar.YearPickerGridBody>
                        </RangeCalendar.YearPickerGrid>
                    </RangeCalendar>
                </DateRangePicker.Popover>
                <AyudaYError ayuda={ayuda} error={error} />
            </DateRangePicker>
        </I18nProvider>
    );
}

/*
 * `CampoFecha` y `CampoHora`: el reemplazo de `<Campo type="date">` y `<Campo type="time">`. Misma
 * forma que un campo nativo (`value`/`onChange(e)` con `e.target.value`, o `{...register()}`),
 * con el `DatePicker` y el `TimeField` de HeroUI por dentro. Los valores son los del input nativo:
 * `AAAA-MM-DD` y `HH:MM` (24 h), hora local de Guatemala; la pantalla los pasa a UTC al enviar.
 */

type PropsCampoFechaHora = PropsControlNativo & {
    etiqueta: ReactNode;
    ayuda?: ReactNode;
    error?: string | undefined;
    id?: string;
    className?: string;
    disabled?: boolean | undefined;
    required?: boolean | undefined;
};

/** Ids, error estable y el «salir del campo» de un campo de fecha u hora (foco dentro del grupo o de su calendario). */
function useCampoSegmentos(props: PropsCampoFechaHora) {
    const visible = useErrorEstable(props.error);
    const ids = useIdsCampo(props.id, visible, props.ayuda);
    const caja = useRef<HTMLDivElement>(null);
    const abiertaRef = useRef(false);
    const nativo = useControlNativo(props, () => caja.current?.querySelector<HTMLElement>('[role="spinbutton"]:not([aria-readonly="true"])')?.focus());
    const alSalir = (e: FocusEvent<HTMLDivElement>) => {
        const destino = e.relatedTarget;
        if (abiertaRef.current || (destino instanceof Node && caja.current?.contains(destino))) return;
        nativo.salir();
    };
    return { visible, ids, caja, abiertaRef, nativo, alSalir };
}

export type PropsCampoFecha = PropsCampoFechaHora & {
    /** Primer y último día permitidos (`AAAA-MM-DD`). */
    min?: string | number | undefined;
    max?: string | number | undefined;
};

/** Fecha (`AAAA-MM-DD`): segmentos día/mes/año y calendario. */
export function CampoFecha(props: PropsCampoFecha) {
    const { etiqueta, ayuda, className, disabled, required, min, max } = props;
    const { visible, ids, caja, abiertaRef, nativo, alSalir } = useCampoSegmentos(props);
    const minimo = aFecha(min === undefined ? undefined : String(min));
    const maximo = aFecha(max === undefined ? undefined : String(max));
    return (
        <div ref={caja} className={cn("grid gap-1.5", className)} onBlur={alSalir} data-valor={nativo.valor}>
            <I18nProvider locale={LOCALE}>
                <DatePicker
                    data-slot="campo-fecha"
                    className="grid gap-1.5"
                    id={ids.idCampo}
                    value={aFecha(nativo.valor)}
                    onChange={(fecha) => nativo.cambiar(fecha ? fecha.toString() : "")}
                    onOpenChange={(a) => {
                        abiertaRef.current = a;
                    }}
                    {...(minimo ? { minValue: minimo } : {})}
                    {...(maximo ? { maxValue: maximo } : {})}
                    isDisabled={disabled ?? false}
                    isRequired={required ?? false}
                    isInvalid={Boolean(visible)}
                    validationBehavior="aria"
                    {...(ids.control["aria-describedby"] ? { "aria-describedby": ids.control["aria-describedby"] } : {})}
                >
                    <Label className="text-sm font-medium">{etiqueta}</Label>
                    <DateField.Group fullWidth className="h-10 shadow-none">
                        <DateField.Input>{(segmento) => <DateField.Segment segment={segmento} />}</DateField.Input>
                        <DateField.Suffix>
                            <DatePicker.Trigger aria-label="Abrir calendario">
                                <DatePicker.TriggerIndicator />
                            </DatePicker.Trigger>
                        </DateField.Suffix>
                    </DateField.Group>
                    <DatePicker.Popover className="max-w-[calc(100vw-1rem)]">
                        <Calendar aria-label="Calendario">
                            <Cabecera />
                            <Calendar.Grid>
                                <Calendar.GridHeader>{(dia) => <Calendar.HeaderCell>{dia}</Calendar.HeaderCell>}</Calendar.GridHeader>
                                <Calendar.GridBody>{(fecha) => <Calendar.Cell date={fecha} />}</Calendar.GridBody>
                            </Calendar.Grid>
                            <Calendar.YearPickerGrid>
                                <Calendar.YearPickerGridBody>{({ year }) => <Calendar.YearPickerCell year={year} />}</Calendar.YearPickerGridBody>
                            </Calendar.YearPickerGrid>
                        </Calendar>
                    </DatePicker.Popover>
                </DatePicker>
            </I18nProvider>
            {nativo.oculto}
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </div>
    );
}

export type PropsCampoHora = PropsCampoFechaHora;

/** Hora (`HH:MM`, 24 h): segmentos hora y minutos. */
export function CampoHora(props: PropsCampoHora) {
    const { etiqueta, ayuda, className, disabled, required } = props;
    const { visible, ids, caja, nativo, alSalir } = useCampoSegmentos(props);
    return (
        <div ref={caja} className={cn("grid gap-1.5", className)} onBlur={alSalir} data-valor={nativo.valor}>
            <I18nProvider locale={LOCALE}>
                <TimeField
                    data-slot="campo-hora"
                    className="grid gap-1.5"
                    id={ids.idCampo}
                    value={aHora(nativo.valor)}
                    onChange={(hora) => nativo.cambiar(deHora(hora))}
                    hourCycle={24}
                    granularity="minute"
                    shouldForceLeadingZeros
                    isDisabled={disabled ?? false}
                    isRequired={required ?? false}
                    isInvalid={Boolean(visible)}
                    validationBehavior="aria"
                    {...(ids.control["aria-describedby"] ? { "aria-describedby": ids.control["aria-describedby"] } : {})}
                >
                    <Label className="text-sm font-medium">{etiqueta}</Label>
                    <TimeField.Group fullWidth className="h-10 shadow-none">
                        <TimeField.Input>{(segmento) => <TimeField.Segment segment={segmento} />}</TimeField.Input>
                    </TimeField.Group>
                </TimeField>
            </I18nProvider>
            {nativo.oculto}
            <PieCampo idAyuda={ids.idAyuda} idError={ids.idError} ayuda={ayuda} error={visible} />
        </div>
    );
}
