package unit_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/arandu-io/framework/foundation"
)

// viewCompiler is the command line whose view compiler the published views are
// held against, at the release an application builds them with.
//
// It is a version and never "latest", so this test changes its answer when this
// line changes and never because somebody published something. Moving it is
// how a stricter compiler is taken on: raise it, run the suite, and fix the
// view it refuses here rather than in every application that publishes it.
const viewCompiler = "github.com/arandu-io/aru@v0.74.0"

// TestEveryPublishedViewCompiles publishes the views into a project of their
// own, builds them with the view compiler an application runs, and compiles
// what it wrote.
//
// Nothing else in this repository reads the markup. The sources open with a
// build tag, so go build, go vet and go test never look past it, and the view
// compiler refuses what the Go compiler would accept -- a value written into an
// address behind text it cannot read, a value in a position no escape covers.
// A view it refuses is a view that stops the build of every application that
// publishes it, and without this test the first place to say so is somebody
// else's terminal.
//
// The project is laid out as an application's is once it has published: each
// file where the publication writes it, and a go.mod that resolves this package
// to the checkout under test. What the view compiler writes then goes through
// the Go compiler, which is what catches a field the markup names and the page
// data does not have.
//
// It does not skip. A gate that steps aside when its input is missing is how
// the views stopped compiling with nothing here saying so.
func TestEveryPublishedViewCompiles(t *testing.T) {
	root := packageRoot(t)
	project := t.TempDir()

	publications, err := foundation.Publications(module(t))
	if err != nil {
		t.Fatalf("reading what the module publishes: %v", err)
	}
	var published int
	for _, publication := range publications {
		from := publication.From
		if from == "" {
			from = "."
		}
		err := fs.WalkDir(publication.Files, from, func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			relative := name
			if from != "." {
				relative = strings.TrimPrefix(strings.TrimPrefix(name, from), "/")
			}
			body, err := fs.ReadFile(publication.Files, name)
			if err != nil {
				return err
			}
			target := filepath.Join(project, filepath.FromSlash(path.Join(publication.To, relative)))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			published++
			return os.WriteFile(target, body, 0o644)
		})
		if err != nil {
			t.Fatalf("publishing the %s files: %v", publication.Tag, err)
		}
	}
	if published == 0 {
		t.Fatal("the module publishes no file, so this gate would compile nothing")
	}

	// The application's go.mod names this package and points it at the
	// checkout, so the views compile against the code beside them rather than
	// against a release. The sums come from here: the application needs no
	// module this package does not already require.
	goMod := readReleaseFile(t, root, "go.mod")
	language := captureReleaseValue(t, goMod, `(?m)^go ([0-9.]+)$`, "go directive in go.mod")
	modulePath := captureReleaseValue(t, goMod, `(?m)^module (\S+)$`, "module path in go.mod")
	manifest := "module example.com/application\n\n" +
		"go " + language + "\n\n" +
		"require " + modulePath + " v0.0.0-00010101000000-000000000000\n\n" +
		"replace " + modulePath + " => " + strconv.Quote(root) + "\n"
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.sum"), []byte(readReleaseFile(t, root, "go.sum")), 0o644); err != nil {
		t.Fatal(err)
	}

	inProject(t, project, "the published views do not compile with "+viewCompiler+", so no application that publishes them can build",
		"go", "run", viewCompiler, "view:build")
	inProject(t, project, "the views "+viewCompiler+" compiled do not build against this package",
		"go", "build", "./...")

	// Compiling is not drawing. Each screen is rendered once, with a row of
	// every kind and every action switched on, under a layout that draws the
	// content section alone -- so a page that compiles and then fails on its
	// own data, or draws a key instead of a sentence, fails here.
	probe := filepath.Join(project, "renderprobe")
	if err := os.MkdirAll(probe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(probe, "main.go"), []byte(renderProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	inProject(t, project, "a published view does not render the page data its handler hands it",
		"go", "run", "./renderprobe")
}

// renderProbe renders every screen with sample data and checks what it drew.
const renderProbe = `package main

import (
	"context"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"

	"github.com/arandu-io/hesape/view"
	"github.com/arandu-io/kyse/components"
	agenda "github.com/hyz-is/arandu-agenda"

	_ "example.com/application/storage/framework/views/modules/agenda"
	_ "example.com/application/storage/framework/views/modules/agenda/admin"
)

func main() {
	view.RegisterLayout("layouts.app", func(w io.Writer, _ any, sections map[string]func(io.Writer) error) error {
		return sections["content"](w)
	})
	sc := agenda.Screen{
		Labels: agenda.Labels{Locale: "pt-BR"},
		URLs:   agenda.NewURLs("/painel/agendamentos", "/admin/agenda", agenda.PortuguesePaths),
	}
	row := agenda.BookingRow{ID: "b1", Code: "AG-000123", Service: "Reconfiguração de câmera", ServiceKey: "reconfiguracao",
		Subject: "Portaria", SubjectType: "camera", SubjectID: "42", Status: "Agendado", StatusKey: "scheduled",
		Day: "10/11/2026", Time: "09:00", Ends: "09:40", When: "10/11/2026 09:00", Requester: "Ana Souza", RequesterID: "u1", Space: "acme",
		Note: "A imagem está escura.", PaymentRef: "pix-9", HoldUntil: "09/11/2026 18:00", Confirmed: "09/11/2026 10:00", Reminded: "10/11/2026 06:00",
		Completed: "10/11/2026 10:00", CompletedBy: "Rita", TeamNote: "Trocado o ângulo.", ReleasedBy: "Rita", ReleasedReason: "Cortesia da casa",
		CancelReason: "Mudei de ideia", Created: "08/11/2026 12:00", Updated: "08/11/2026 12:00", Reschedules: 1, URL: "/painel/agendamentos/b1"}
	teamRow := row
	teamRow.URL = "/admin/agenda/b1"
	options := func(values ...string) []components.SelectOption {
		out := make([]components.SelectOption, 0, len(values))
		for _, v := range values {
			out = append(out, components.SelectOption{Value: v, Label: v})
		}
		return out
	}
	slots := []components.RadioOption{{Label: "09:00", Value: "2026-11-10T12:00:00Z"}, {Label: "09:20", Value: "2026-11-10T12:20:00Z"}}

	pages := []struct {
		name string
		data any
		want []string
		not  []string
	}{
		{agenda.ViewIndex, agenda.IndexPageData{Screen: sc, Rows: []agenda.BookingRow{row}, URL: "/painel/agendamentos", NewURL: "/painel/agendamentos/novo",
			CanBook: true, CurrentPage: 1, Pages: 1, Total: 1, PerPage: 25, StatusOptions: options("scheduled", "completed"), Pager: components.Paginator(nil)},
			[]string{"Agendamentos", "AG-000123", "Reconfiguração de câmera", "Portaria", "10/11/2026 09:00", "Agendado", "/painel/agendamentos/b1",
				"Novo agendamento", "/painel/agendamentos/novo", "Buscar em Agendamentos", "Situação"},
			[]string{"Ana Souza", "pix-9"}},
		{agenda.ViewIndex, agenda.IndexPageData{Screen: sc, URL: "/painel/agendamentos", CurrentPage: 1, Pages: 1, PerPage: 25},
			[]string{"Nenhum agendamento por aqui", "Quando você marcar um horário"}, []string{"Novo agendamento"}},
		{agenda.ViewNew, agenda.NewPageData{Screen: sc, Action: "/painel/agendamentos", FormURL: "/painel/agendamentos/novo", BackURL: "/painel/agendamentos",
			ServiceOptions: options("reconfiguracao"), ServiceKey: "reconfiguracao", ServiceNote: "Dura cerca de 40 minutos. Exige pagamento antecipado.",
			RequiresPayment: true, Day: "2026-11-10", MinDay: "2026-11-10", MaxDay: "2026-12-09", Searched: true, Slots: slots,
			Subject: agenda.BookingSubject{Type: "camera", ID: "42", Label: "Portaria"}},
			[]string{"Novo agendamento", "Serviço", "type=\"date\"", "Janeiro", "Dom", "Abrir o calendário", "Ver horários", "Horários livres", "09:00", "09:20",
				"2026-11-10T12:00:00Z", "Reservar horário", "Este serviço é pago", "name=\"subject_id\"", "Observação", "_token", "Portaria"}, nil},
		{agenda.ViewNew, agenda.NewPageData{Screen: sc, Action: "/painel/agendamentos", FormURL: "/painel/agendamentos/novo", BackURL: "/painel/agendamentos",
			ServiceOptions: options("reconfiguracao"), ServiceKey: "reconfiguracao", Day: "2026-11-10", Searched: true},
			[]string{"Nenhum horário livre neste dia"}, []string{"Reservar horário"}},
		{agenda.ViewNew, agenda.NewPageData{Screen: sc, NoServices: true},
			[]string{"não está recebendo agendamentos"}, []string{"Ver horários"}},
		{agenda.ViewShow, agenda.ShowPageData{Screen: sc, Booking: row, BackURL: "/painel/agendamentos",
			ConfirmURL: "/painel/agendamentos/b1/confirmacao", RescheduleURL: "/painel/agendamentos/b1/reagendamento", CancelURL: "/painel/agendamentos/b1/cancelamento",
			CanConfirm: true, CanReschedule: true, CanCancel: true},
			[]string{"AG-000123", "Reconfiguração de câmera", "10/11/2026", "09:00 – 09:40", "Portaria", "A imagem está escura.", "Confirmar presença",
				"/painel/agendamentos/b1/confirmacao", "Reagendar", "/painel/agendamentos/b1/reagendamento", "Cancelar agendamento", "/painel/agendamentos/b1/cancelamento"},
			[]string{"Cliente", "Trocado o ângulo.", "Cortesia da casa", "pix-9", "Aguardando pagamento"}},
		{agenda.ViewShow, agenda.ShowPageData{Screen: sc, Booking: row, BackURL: "/painel/agendamentos", AwaitingPayment: true, PaymentURL: "/pagamentos/b1"},
			[]string{"Aguardando pagamento", "O horário fica reservado para você até", "09/11/2026 18:00", "Pagar agora", "/pagamentos/b1"}, []string{"Confirmar presença"}},
		{agenda.ViewShow, agenda.ShowPageData{Screen: sc, Booking: row, BackURL: "/painel/agendamentos", HoldExpired: true, ChangeClosed: true},
			[]string{"prazo do pagamento acabou", "não pode mais ser alterado"}, []string{"Pagar agora"}},
		{agenda.ViewReschedule, agenda.ReschedulePageData{Screen: sc, Booking: row, Action: "/painel/agendamentos/b1/reagendamento",
			FormURL: "/painel/agendamentos/b1/reagendamento", BackURL: "/painel/agendamentos/b1", Day: "2026-11-11", MinDay: "2026-11-10", MaxDay: "2026-12-09",
			Searched: true, Slots: slots},
			[]string{"Reagendar", "AG-000123", "Escolha o novo dia", "type=\"date\"", "09:20", "/painel/agendamentos/b1/reagendamento", "_token"}, nil},
		{agenda.ViewTeamIndex, agenda.TeamIndexPageData{Screen: sc, Rows: []agenda.BookingRow{teamRow}, URL: "/admin/agenda", Day: "2026-11-10",
			CurrentPage: 1, Pages: 3, Total: 60, PerPage: 25, StatusOptions: options("scheduled"), ServicesURL: "/admin/agenda/configuracoes/servicos",
			HoursURL: "/admin/agenda/configuracoes/horarios", ClosuresURL: "/admin/agenda/configuracoes/bloqueios", CanConfigure: true},
			[]string{"Agenda", "AG-000123", "Ana Souza", "acme", "Portaria", "Agendado", "/admin/agenda/b1", "Buscar por protocolo, cliente, serviço ou item",
				"/admin/agenda/configuracoes/servicos", "/admin/agenda/configuracoes/horarios", "/admin/agenda/configuracoes/bloqueios", "Filtrar por dia", "Todos os dias"}, nil},
		{agenda.ViewTeamShow, agenda.TeamShowPageData{Screen: sc, Booking: teamRow, BackURL: "/admin/agenda", CompleteURL: "/admin/agenda/b1/conclusao",
			ReleaseURL: "/admin/agenda/b1/liberacao", NoShowURL: "/admin/agenda/b1/ausencia", CancelURL: "/admin/agenda/b1/cancelamento",
			RescheduleURL: "/admin/agenda/b1/reagendamento", CanComplete: true, CanRelease: true, CanNoShow: true, CanCancel: true, CanReschedule: true,
			Day: "2026-11-10", Time: "09:00", MinDay: "2026-11-09"},
			[]string{"AG-000123 · acme", "Ana Souza", "pix-9", "Cortesia da casa", "A imagem está escura.", "Trocado o ângulo.", "Concluir atendimento", "/admin/agenda/b1/conclusao",
				"Liberar sem pagamento", "/admin/agenda/b1/liberacao", "Cliente não compareceu", "/admin/agenda/b1/ausencia", "/admin/agenda/b1/reagendamento",
				"type=\"time\"", "/admin/agenda/b1/cancelamento", "Lembrete enviado em"}, nil},
		{agenda.ViewTeamShow, agenda.TeamShowPageData{Screen: sc, Booking: teamRow, BackURL: "/admin/agenda"},
			[]string{"AG-000123 · acme"}, []string{"Concluir atendimento", "Liberar sem pagamento", "Cancelar agendamento"}},
		{agenda.ViewTeamServices, agenda.ServicesPageData{Screen: sc, Rows: []agenda.ServiceRow{{ID: "s1", Key: "reconfiguracao", Name: "Reconfiguração de câmera",
			Duration: "40 min", Payment: "Sim", Active: "Sim", EditURL: "/admin/agenda/configuracoes/servicos/s1", DeleteURL: "/admin/agenda/configuracoes/servicos/s1"}},
			URL: "/admin/agenda/configuracoes/servicos", NewURL: "/admin/agenda/configuracoes/servicos/novo", CurrentPage: 1, Pages: 1, Total: 1, PerPage: 25,
			CanConfigure: true, HoursURL: "/admin/agenda/configuracoes/horarios", ClosuresURL: "/admin/agenda/configuracoes/bloqueios"},
			[]string{"Serviços", "reconfiguracao", "Reconfiguração de câmera", "40 min", "/admin/agenda/configuracoes/servicos/s1", "Novo serviço", "hx-delete",
				"Excluir", "Buscar por nome ou chave"}, nil},
		{agenda.ViewTeamServices, agenda.ServicesPageData{Screen: sc, URL: "/admin/agenda/configuracoes/servicos", CurrentPage: 1, Pages: 1, PerPage: 25},
			[]string{"Nenhum serviço cadastrado"}, []string{"Novo serviço\"", "hx-delete"}},
		{agenda.ViewTeamServiceForm, agenda.ServiceFormPageData{Screen: sc, Action: "/admin/agenda/configuracoes/servicos/s1", BackURL: "/admin/agenda/configuracoes/servicos",
			Editing: true, Key: "reconfiguracao", Name: "Reconfiguração de câmera", Duration: "40", RequiresPayment: true, Active: true},
			[]string{"Editar serviço", "reconfiguracao", "Duração (minutos)", "Exige pagamento", "Ativo", "checked", "_token"}, []string{"Novo serviço"}},
		{agenda.ViewTeamHours, agenda.HoursPageData{Screen: sc, Rows: []agenda.HourRow{{ID: "h1", Weekday: "Segunda-feira", From: "09:00", To: "18:00",
			DeleteURL: "/admin/agenda/configuracoes/horarios/h1"}}, URL: "/admin/agenda/configuracoes/horarios", NewURL: "/admin/agenda/configuracoes/horarios/novo",
			CurrentPage: 1, Pages: 1, Total: 1, PerPage: 25, CanConfigure: true},
			[]string{"Horários de atendimento", "Segunda-feira", "09:00", "18:00", "Novo horário", "hx-delete"}, nil},
		{agenda.ViewTeamHourForm, agenda.HourFormPageData{Screen: sc, Action: "/admin/agenda/configuracoes/horarios", BackURL: "/admin/agenda/configuracoes/horarios",
			WeekdayOptions: options("0", "1"), Weekday: "1", Start: "09:00", End: "18:00"},
			[]string{"Novo horário de atendimento", "Dia da semana", "type=\"time\"", "name=\"from\"", "name=\"to\"", "_token"}, nil},
		{agenda.ViewTeamClosures, agenda.ClosuresPageData{Screen: sc, Rows: []agenda.ClosureRow{{ID: "c1", From: "25/12/2026 00:00", To: "25/12/2026 23:59",
			Reason: "Natal", DeleteURL: "/admin/agenda/configuracoes/bloqueios/c1"}}, URL: "/admin/agenda/configuracoes/bloqueios",
			NewURL: "/admin/agenda/configuracoes/bloqueios/novo", CurrentPage: 1, Pages: 1, Total: 1, PerPage: 25, CanConfigure: true},
			[]string{"Bloqueios", "Natal", "25/12/2026 00:00", "Novo bloqueio", "hx-delete"}, nil},
		{agenda.ViewTeamClosureForm, agenda.ClosureFormPageData{Screen: sc, Action: "/admin/agenda/configuracoes/bloqueios", BackURL: "/admin/agenda/configuracoes/bloqueios",
			FromTime: "00:00", ToTime: "23:59"},
			[]string{"Novo bloqueio", "name=\"from_day\"", "name=\"to_time\"", "Motivo", "type=\"date\"", "_token"}, nil},
	}

	inline := regexp.MustCompile(` + "`" + `(?i)(<script|\sstyle=|\son[a-z]+=|javascript:)` + "`" + `)
	failed := false
	for _, p := range pages {
		rec := httptest.NewRecorder()
		if err := view.NewRenderer().Render(context.Background(), rec, 200, p.name, p.data); err != nil {
			fmt.Printf("%s: %v\n", p.name, err)
			failed = true
			continue
		}
		body := rec.Body.String()
		for _, want := range p.want {
			if !strings.Contains(body, want) {
				fmt.Printf("%s: the page does not say %q\n", p.name, want)
				failed = true
			}
		}
		for _, not := range p.not {
			if strings.Contains(body, not) {
				fmt.Printf("%s: the page says %q, which this data must not draw\n", p.name, not)
				failed = true
			}
		}
		for _, key := range []string{"agenda.screen.", "agenda.field.", "agenda.control.", "agenda.calendar."} {
			if strings.Contains(body, key) {
				fmt.Printf("%s: the page draws a catalogue key (%s) instead of its sentence\n", p.name, key)
				failed = true
			}
		}
		// The content security policy is 'self': no inline script, no inline
		// style, no event attribute and no javascript: address.
		if found := inline.FindString(body); found != "" {
			fmt.Printf("%s: the page carries %q, which a policy of 'self' refuses\n", p.name, found)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
`

// inProject runs one command in the scratch project, outside any workspace,
// and fails with what it printed.
//
// -mod=mod lets the go command fill in the requirements the application's
// go.mod leaves to this package's, which is what an application's own `go mod
// tidy` would have written.
func inProject(t *testing.T, dir, failure string, name string, args ...string) {
	t.Helper()

	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS="+strings.TrimSpace(os.Getenv("GOFLAGS")+" -mod=mod"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s:\n$ %s %s\n%s", failure, name, strings.Join(args, " "), output)
	}
}
