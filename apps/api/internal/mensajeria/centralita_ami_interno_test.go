package mensajeria

import (
	"testing"

	"github.com/HCDestroyer/iris/apps/api/internal/mensajeria/ami"
)

func paquete(campos ...string) ami.Paquete {
	var p ami.Paquete
	for i := 0; i+1 < len(campos); i += 2 {
		p.Campos = append(p.Campos, [2]string{campos[i], campos[i+1]})
	}
	return p
}

func TestExtensionDeCanalIssabel5(t *testing.T) {
	casos := map[string]string{
		"PJSIP/201-0000001a":              "201",
		"SIP/201-0000001a":                "201",
		"IAX2/3001-1234":                  "3001",
		"Local/201@from-queue-0000002b;1": "",
		"Local/201@from-queue-0000002b;2": "",
		"PJSIP/claro-00000031":            "",
		"SIP/troncal-0001":                "",
		"DAHDI/i1/55123480-1":             "",
		"PJSIP/201":                       "",
		"PJSIP/201-0000001a\r\nAction":    "",
	}
	for canal, quiero := range casos {
		ext, ok := extensionDeCanal(canal)
		if quiero == "" && ok {
			t.Errorf("%q no es un teléfono, dio %q", canal, ext)
		}
		if quiero != "" && ext != quiero {
			t.Errorf("%q: %q, quería %q", canal, ext, quiero)
		}
	}
}

func TestNormalizarNumero(t *testing.T) {
	casos := map[string]string{
		"55123480":          "+50255123480",
		"50255123480":       "+50255123480",
		"+502 5512-3480":    "+50255123480",
		"(502) 5512 3480":   "+50255123480",
		"0055123480111":     "+55123480111",
		"955123480":         "+50255123480",
		"055123480":         "+50255123480",
		"":                  "",
		"anonymous":         "",
		"<unknown>":         "",
		"204":               "",
		"5512<script>":      "",
		"12345678901234567": "",
	}
	for crudo, quiero := range casos {
		if got := normalizarNumero(crudo); got != quiero {
			t.Errorf("%q: %q, quería %q", crudo, got, quiero)
		}
	}
}

func TestTraducirEventos(t *testing.T) {
	reg := func(ext string) bool { return ext == "201" || ext == "202" }
	// Entrante por troncal a un teléfono (chan_sip o PJSIP).
	for _, canal := range []string{"PJSIP/201-0000001a", "SIP/201-0000001a"} {
		h := traducirEvento(paquete("Event", "DialBegin", "Channel", "PJSIP/claro-00000031", "CallerIDNum", "55123480", "DestChannel", canal,
			"Linkedid", "1700000000.10"), reg)
		if h.tipo != hechoSuena || h.extension != "201" || h.numero != "+50255123480" || h.interno || h.linkedid != "1700000000.10" {
			t.Fatalf("%s: %+v", canal, h)
		}
	}
	// Cola: el DialBegin hacia el Local no hace nada; el del Local;2 al teléfono sí.
	if h := traducirEvento(paquete("Event", "DialBegin", "Channel", "PJSIP/claro-00000031", "CallerIDNum", "55123480",
		"DestChannel", "Local/201@from-queue-0000002b;1", "Linkedid", "1700.2"), reg); h.tipo != hechoSinInteres {
		t.Fatalf("el Local no debe sonar: %+v", h)
	}
	if h := traducirEvento(paquete("Event", "DialBegin", "Channel", "Local/201@from-queue-0000002b;2", "CallerIDNum", "55123480",
		"DestChannel", "PJSIP/201-0000003c", "Linkedid", "1700.2"), reg); h.tipo != hechoSuena || h.extension != "201" || h.interno {
		t.Fatalf("cola: %+v", h)
	}
	// Interna (de otro teléfono o con un CallerID con forma de extensión): no cuenta.
	if h := traducirEvento(paquete("Event", "DialBegin", "Channel", "PJSIP/202-00000001", "CallerIDNum", "202", "DestChannel", "PJSIP/201-00000002",
		"Linkedid", "1700.3"), reg); !h.interno {
		t.Fatalf("interna: %+v", h)
	}
	if h := traducirEvento(paquete("Event", "DialBegin", "Channel", "Local/201@from-queue-1;2", "CallerIDNum", "305", "DestChannel", "PJSIP/201-2",
		"Linkedid", "1700.4"), reg); !h.interno {
		t.Fatalf("interna por cola: %+v", h)
	}
	// Extensión no registrada: nada.
	if h := traducirEvento(paquete("Event", "DialBegin", "Channel", "PJSIP/claro-1", "CallerIDNum", "55123480", "DestChannel", "PJSIP/299-1",
		"Linkedid", "1700.5"), reg); h.tipo != hechoSinInteres {
		t.Fatalf("no registrada: %+v", h)
	}
	// Saliente a una troncal.
	if h := traducirEvento(paquete("Event", "DialBegin", "Channel", "PJSIP/201-1", "DestChannel", "PJSIP/claro-2", "DestExten", "955123480",
		"Linkedid", "1700.6"), reg); h.tipo != hechoMarca || h.numero != "+50255123480" || h.extension != "201" {
		t.Fatalf("saliente: %+v", h)
	}
	// Contesta, cuelga y CDR.
	if h := traducirEvento(paquete("Event", "DialEnd", "DestChannel", "PJSIP/201-1", "DialStatus", "ANSWER", "Linkedid", "1700.1"), reg); h.tipo != hechoContesta {
		t.Fatalf("contesta: %+v", h)
	}
	if h := traducirEvento(paquete("Event", "DialEnd", "DestChannel", "PJSIP/201-1", "DialStatus", "NOANSWER", "Linkedid", "1700.1"), reg); h.tipo != hechoSinInteres {
		t.Fatalf("no contestó: %+v", h)
	}
	if h := traducirEvento(paquete("Event", "Hangup", "Uniqueid", "1700.1", "Linkedid", "1700.1"), reg); h.tipo != hechoCuelga {
		t.Fatalf("cuelga: %+v", h)
	}
	if h := traducirEvento(paquete("Event", "Hangup", "Uniqueid", "1700.9", "Linkedid", "1700.1"), reg); h.tipo != hechoSinInteres {
		t.Fatalf("otro canal colgó: %+v", h)
	}
	cdr := linkedidDeCDR(paquete("Event", "Cdr", "UniqueID", "1700.1", "Disposition", "ANSWERED", "BillableSeconds", "245"))
	if h := traducirEvento(cdr, reg); h.tipo != hechoRegistro || !h.contestada || h.segundos != 245 || h.linkedid != "1700.1" {
		t.Fatalf("cdr: %+v", h)
	}
	// Linkedid con caracteres raros: se descarta todo el evento.
	if h := traducirEvento(paquete("Event", "Hangup", "Uniqueid", "x y", "Linkedid", "x y"), reg); h.tipo != hechoSinInteres {
		t.Fatalf("linkedid raro: %+v", h)
	}
}
