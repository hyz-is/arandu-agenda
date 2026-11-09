//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// ServicesData is what the handler hands this page.
type ServicesData = agenda.ServicesPageData

// servicesTable is the team's catalogue of services, paged, searched and ordered
// by the server. A service is opened to be changed and deleted in place.
func servicesTable(data ServicesData) components.DataTableProps {
	rows := make([]components.TableRow, 0, len(data.Rows))
	for _, s := range data.Rows {
		cells := []components.TableCell{
			{Text: s.Key},
			{HTML: components.Link(components.LinkProps{Label: s.Name, URL: s.EditURL, Variant: "hover"})},
			{Text: s.Duration},
			{Text: s.Payment},
			{Text: s.Active},
		}
		if data.CanConfigure {
			cells = append(cells, components.TableCell{HTML: components.DeleteRow(components.DeleteRowProps{
				URL: s.DeleteURL, Label: data.Labels.T("control.delete"), Confirm: data.Labels.T("screen.delete_service_confirm"),
				Description: data.Labels.T("control.delete") + " " + s.Name, Size: "sm",
			})})
		}
		rows = append(rows, components.TableRow{Key: s.ID, Cells: cells})
	}
	columns := []components.TableColumn{
		{Label: data.Labels.T("field.key"), Key: "key", Sortable: true},
		{Label: data.Labels.T("field.name"), Key: "name", Sortable: true},
		{Label: data.Labels.T("field.duration"), Key: "duration", Sortable: true},
		{Label: data.Labels.T("field.requires_payment"), Key: "payment", Sortable: true, Hideable: true},
		{Label: data.Labels.T("field.active"), Key: "active", Sortable: true, Hideable: true},
	}
	if data.CanConfigure {
		columns = append(columns, components.TableColumn{Label: data.Labels.T("table.actions")})
	}
	return components.DataTableProps{
		ID: "agenda-services", Label: data.Labels.T("screen.services_title"), URL: data.URL,
		State: data.TableState(), Paginator: data.Pager, FullPager: true, HeaderMenu: true,
		Caption: data.Labels.T("screen.services_lead"), ColumnsLabel: data.Labels.T("control.columns"), Lines: data.TableLines(),
		Searchable: true, SearchPlaceholder: data.Labels.T("screen.services_search"),
		SearchLabel: data.SearchLabel(data.Labels.T("screen.services_title")),
		Columns: columns,
		Rows: rows,
		Empty: components.EmptyProps{
			Title: data.Labels.T("screen.services_empty"), Message: data.Labels.T("screen.services_empty_lead"),
		},
	}
}
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-5xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header class="flex flex-wrap items-start justify-between gap-4">
		<div class="min-w-0">
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.services_title") }}</h1>
			<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.services_lead") }}</p>
		</div>
		<nav class="flex flex-wrap gap-2" aria-label="{{ .Labels.T("screen.settings") }}">
			<a class="btn" data-variant="outline" href="{{ .HoursURL }}">{{ .Labels.T("control.hours") }}</a>
			<a class="btn" data-variant="outline" href="{{ .ClosuresURL }}">{{ .Labels.T("control.closures") }}</a>
			@if(.CanConfigure)
				<a class="btn" href="{{ .NewURL }}">{{ .Labels.T("control.new_service") }}</a>
			@endif
		</nav>
	</header>

	<div class="mt-6 min-w-0">{!! components.DataTable(servicesTable(.)) !!}</div>
</section>
@endsection
