//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// HoursData is what the handler hands this page.
type HoursData = agenda.HoursPageData

// hoursTable is the team's week: the windows it takes bookings in, paged and
// ordered by the server. A window is removed in place.
func hoursTable(data HoursData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, h := range data.Rows {
		cells := []components.TableCell{{Text: h.Weekday}, {Text: h.From}, {Text: h.To}}
		if data.CanConfigure {
			cells = append(cells, components.TableCell{HTML: components.DeleteRow(components.DeleteRowProps{
				URL: h.DeleteURL, Label: data.Labels.T("control.delete"), Confirm: data.Labels.T("screen.delete_hour_confirm"),
				Description: data.Labels.T("control.delete") + " " + h.Weekday + " " + h.From, Size: "sm",
			})})
		}
		rows = append(rows, components.TableRow{Key: h.ID, Cells: cells})
	}
	columns := []components.TableColumn{
		{Label: data.Labels.T("field.weekday"), Key: "weekday", Sortable: true},
		{Label: data.Labels.T("field.from"), Key: "from", Sortable: true},
		{Label: data.Labels.T("field.to"), Key: "to", Sortable: true},
	}
	if data.CanConfigure {
		columns = append(columns, components.TableColumn{Label: data.Labels.T("table.actions")})
	}
	return components.DataTableProps{
		ID: "agenda-hours", Label: data.Labels.T("screen.hours_title"), URL: data.URL,
		State: data.TableState(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.hours_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Columns: columns,
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.hours_empty"), Message: data.Labels.T("screen.hours_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-3xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0">
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.hours_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.hours_lead") }}</p>
		</div>
		<nav class="flex flex-wrap gap-2" aria-label="{{ .Labels.T("screen.settings") }}">
			<a class="btn" data-variant="outline" href="{{ .ServicesURL }}">{{ .Labels.T("control.services") }}</a>
			<a class="btn" data-variant="outline" href="{{ .ClosuresURL }}">{{ .Labels.T("control.closures") }}</a>
			@if(.CanConfigure)
				<a class="btn" href="{{ .NewURL }}">{{ .Labels.T("control.new_hour") }}</a>
			@endif
		</nav>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(hoursTable(.)) !!}</div>
</section>
@endsection
