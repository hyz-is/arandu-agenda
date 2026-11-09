//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// ClosuresData is what the handler hands this page.
type ClosuresData = agenda.ClosuresPageData

// closuresTable is the team's closures, paged, searched and ordered by the
// server. A closure is removed in place.
func closuresTable(data ClosuresData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, c := range data.Rows {
		cells := []components.TableCell{{Text: c.From}, {Text: c.To}, {Text: c.Reason}}
		if data.CanConfigure {
			cells = append(cells, components.TableCell{HTML: components.DeleteRow(components.DeleteRowProps{
				URL: c.DeleteURL, Label: data.Labels.T("control.delete"), Confirm: data.Labels.T("screen.delete_closure_confirm"),
				Description: data.Labels.T("control.delete") + " " + c.From, Size: "sm",
			})})
		}
		rows = append(rows, components.TableRow{Key: c.ID, Cells: cells})
	}
	columns := []components.TableColumn{
		{Label: data.Labels.T("field.from"), Key: "from", Sortable: true},
		{Label: data.Labels.T("field.to"), Key: "to", Sortable: true},
		{Label: data.Labels.T("field.reason"), Key: "reason", Sortable: true, Hideable: true},
	}
	if data.CanConfigure {
		columns = append(columns, components.TableColumn{Label: data.Labels.T("table.actions")})
	}
	return components.DataTableProps{
		ID: "agenda-closures", Label: data.Labels.T("screen.closures_title"), URL: data.URL,
		State: data.TableState(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.closures_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("screen.closures_search"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.closures_title")),
		Columns: columns,
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.closures_empty"), Message: data.Labels.T("screen.closures_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-4xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0">
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.closures_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.closures_lead") }}</p>
		</div>
		<nav class="flex flex-wrap gap-2" aria-label="{{ .Labels.T("screen.settings") }}">
			<a class="btn" data-variant="outline" href="{{ .ServicesURL }}">{{ .Labels.T("control.services") }}</a>
			<a class="btn" data-variant="outline" href="{{ .HoursURL }}">{{ .Labels.T("control.hours") }}</a>
			@if(.CanConfigure)
				<a class="btn" href="{{ .NewURL }}">{{ .Labels.T("control.new_closure") }}</a>
			@endif
		</nav>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(closuresTable(.)) !!}</div>
</section>
@endsection
