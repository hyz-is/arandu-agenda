//go:build kyse

package agenda

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// IndexData is what the handler hands this page.
//
// It is an alias rather than a struct declared here, so the shape is written
// once, beside the code that fills it.
type IndexData = agenda.IndexPageData

// bookingsTable is the customer's list, paged, filtered, searched and ordered by
// the server. Each row opens the booking.
func bookingsTable(data IndexData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, b := range data.Rows {
		rows = append(rows, components.TableRow{Key: b.ID, Cells: []components.TableCell{
			{Text: b.Code},
			{HTML: components.Link(components.LinkProps{Label: b.Service, URL: b.URL, Variant: "hover"})},
			{Text: b.Subject},
			{Text: b.When},
			{HTML: components.Badge(components.BadgeProps{Label: b.Status, Variant: agenda.StatusVariant(b.StatusKey)})},
		}})
	}
	return components.DataTableProps{
		ID: "agenda-list", Label: data.Labels.T("screen.index_title"), URL: data.URL,
		State: data.TableState(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.index_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("control.search_placeholder"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.index_title")),
		Filters: []components.DataTableFilter{
			{Key: "status", Label: data.Labels.T("field.status"), Options: data.StatusOptions},
		},
		Columns: []components.TableColumn{
			{Label: data.Labels.T("field.code"), Key: "code", Sortable: true},
			{Label: data.Labels.T("field.service"), Key: "service", Sortable: true},
			{Label: data.Labels.T("field.subject"), Key: "subject", Sortable: true, Hideable: true},
			{Label: data.Labels.T("field.when"), Key: "starts_at", Sortable: true},
			{Label: data.Labels.T("field.status"), Key: "status", Sortable: true},
		},
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.index_empty"), Message: data.Labels.T("screen.index_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-5xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0">
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.index_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.index_lead") }}</p>
		</div>
		@if(.CanBook)
			<a class="btn" href="{{ .NewURL }}">{{ .Labels.T("control.new") }}</a>
		@endif
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(bookingsTable(.)) !!}</div>
</section>
@endsection
