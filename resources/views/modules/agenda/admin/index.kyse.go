//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// IndexData is what the handler hands this page.
type IndexData = agenda.TeamIndexPageData

// bookingsTable is the team's agenda: the bookings of every space, paged,
// filtered, searched and ordered by the server. Each row opens the booking.
func bookingsTable(data IndexData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, b := range data.Rows {
		rows = append(rows, components.TableRow{Key: b.ID, Cells: []components.TableCell{
			{Text: b.Code},
			{HTML: components.Link(components.LinkProps{Label: b.Service, URL: b.URL, Variant: "hover"})},
			{Text: b.Requester},
			{Text: b.Space},
			{Text: b.Subject},
			{Text: b.When},
			{HTML: components.Badge(components.BadgeProps{Label: b.Status, Variant: agenda.StatusVariant(b.StatusKey)})},
		}})
	}
	return components.DataTableProps{
		ID: "agenda-team-list", Label: data.Labels.T("screen.team_index_title"), URL: data.URL,
		State: data.TableState(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.team_index_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("screen.team_search"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.team_index_title")),
		Filters: []components.DataTableFilter{
			{Key: "status", Label: data.Labels.T("field.status"), Options: data.StatusOptions},
		},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.code"), Key: "code", Sortable: true},
			{Label: data.Labels.T("field.service"), Key: "service", Sortable: true},
			{Label: data.Labels.T("field.requester"), Key: "requester", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.space"), Key: "tenant", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.subject"), Key: "subject", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.when"), Key: "starts_at", Sortable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
		},
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.team_index_empty"), Message: data.Labels.T("screen.team_index_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-7xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0">
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.team_index_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.team_index_lead") }}</p>
		</div>
		<nav class="flex flex-wrap gap-2" aria-label="{{ .Labels.T("screen.settings") }}">
			<a class="btn" data-variant="outline" href="{{ .ServicesURL }}">{{ .Labels.T("control.services") }}</a>
			<a class="btn" data-variant="outline" href="{{ .HoursURL }}">{{ .Labels.T("control.hours") }}</a>
			<a class="btn" data-variant="outline" href="{{ .ClosuresURL }}">{{ .Labels.T("control.closures") }}</a>
		</nav>
	</header>

	<form class="mt-6 flex flex-wrap items-end gap-3" method="get" action="{{ .URL }}">
		{!! .DatePicker(components.DateTimePickerProps{Name: "day", ID: "agenda-day-filter", Label: .Labels.T("field.day"), Value: .Day}) !!}
		<div class="flex gap-2">
			{!! components.Button(components.ButtonProps{Label: .Labels.T("control.filter_day"), Variant: "outline", Type: "submit"}) !!}
			@if(.Day != "")
				<a class="btn" data-variant="ghost" href="{{ .URL }}">{{ .Labels.T("control.all_days") }}</a>
			@endif
		</div>
	</form>

	<div class="mt-6 min-w-0">{!! components.DataTable(bookingsTable(.)) !!}</div>
</section>
@endsection
