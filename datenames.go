package brick

import (
	"strings"
	"time"
)

// dateNames are month and weekday names for a language: full months,
// short months, full weekdays (Sunday first), short weekdays. English is
// Go's own and needs no entry.
type dateNames struct {
	months, shortMonths [12]string
	days, shortDays     [7]string
}

// localDateNames covers the languages whose names differ from English. A
// language not listed formats with Go's English names.
var localDateNames = map[string]dateNames{
	"fr": {
		months:      [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"},
		shortMonths: [12]string{"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
		days:        [7]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
		shortDays:   [7]string{"dim.", "lun.", "mar.", "mer.", "jeu.", "ven.", "sam."},
	},
	"de": {
		months:      [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
		shortMonths: [12]string{"Jan.", "Feb.", "März", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sept.", "Okt.", "Nov.", "Dez."},
		days:        [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
		shortDays:   [7]string{"So.", "Mo.", "Di.", "Mi.", "Do.", "Fr.", "Sa."},
	},
	"es": {
		months:      [12]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"},
		shortMonths: [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sept", "oct", "nov", "dic"},
		days:        [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
		shortDays:   [7]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
	},
	"it": {
		months:      [12]string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"},
		shortMonths: [12]string{"gen", "feb", "mar", "apr", "mag", "giu", "lug", "ago", "set", "ott", "nov", "dic"},
		days:        [7]string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"},
		shortDays:   [7]string{"dom", "lun", "mar", "mer", "gio", "ven", "sab"},
	},
	"pt": {
		months:      [12]string{"janeiro", "fevereiro", "março", "abril", "maio", "junho", "julho", "agosto", "setembro", "outubro", "novembro", "dezembro"},
		shortMonths: [12]string{"jan.", "fev.", "mar.", "abr.", "mai.", "jun.", "jul.", "ago.", "set.", "out.", "nov.", "dez."},
		days:        [7]string{"domingo", "segunda-feira", "terça-feira", "quarta-feira", "quinta-feira", "sexta-feira", "sábado"},
		shortDays:   [7]string{"dom.", "seg.", "ter.", "qua.", "qui.", "sex.", "sáb."},
	},
	"nl": {
		months:      [12]string{"januari", "februari", "maart", "april", "mei", "juni", "juli", "augustus", "september", "oktober", "november", "december"},
		shortMonths: [12]string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"},
		days:        [7]string{"zondag", "maandag", "dinsdag", "woensdag", "donderdag", "vrijdag", "zaterdag"},
		shortDays:   [7]string{"zo", "ma", "di", "wo", "do", "vr", "za"},
	},
}

// Go layout tokens that produce names, longest first so "January" is not
// read as "Jan" followed by "uary".
const (
	tokMonth      = "January"
	tokShortMonth = "Jan"
	tokDay        = "Monday"
	tokShortDay   = "Mon"
)

// formatLocal formats t with a Go layout, putting the locale's month and
// day names in place of Go's English ones. Every other layout token is
// formatted by Go itself, one run at a time.
func formatLocal(t time.Time, layout, locale string) string {
	lang, _, _ := strings.Cut(locale, "-")
	names, ok := localDateNames[lang]
	if !ok {
		return t.Format(layout)
	}
	var b, run strings.Builder
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(t.Format(run.String()))
			run.Reset()
		}
	}
	for i := 0; i < len(layout); {
		rest := layout[i:]
		switch {
		case strings.HasPrefix(rest, tokMonth):
			flush()
			b.WriteString(names.months[t.Month()-1])
			i += len(tokMonth)
		case strings.HasPrefix(rest, tokDay):
			flush()
			b.WriteString(names.days[t.Weekday()])
			i += len(tokDay)
		case strings.HasPrefix(rest, tokShortMonth):
			flush()
			b.WriteString(names.shortMonths[t.Month()-1])
			i += len(tokShortMonth)
		case strings.HasPrefix(rest, tokShortDay):
			flush()
			b.WriteString(names.shortDays[t.Weekday()])
			i += len(tokShortDay)
		default:
			run.WriteByte(layout[i])
			i++
		}
	}
	flush()
	return b.String()
}
