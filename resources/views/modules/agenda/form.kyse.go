//go:build kyse

package agenda

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// NewData is what the handler hands this page.
type NewData = agenda.NewPageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-2xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<header>
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.new_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.new_lead") }}</p>
	</header>

	@if(.NoServices)
		<p class="card text-muted-foreground mt-6 p-5 text-sm">{{ .Labels.T("screen.no_services") }}</p>
	@else
		<form class="card mt-6 grid gap-4 p-5" method="get" action="{{ .FormURL }}">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.pick_title") }}</h2>
			{!! components.Select(components.SelectProps{ID: "agenda-service", Name: "service", Label: .Labels.T("field.service"), Value: .ServiceKey, Options: .ServiceOptions}) !!}
			@if(.ServiceNote != "")
				<p class="text-muted-foreground -mt-2 text-xs">{{ .ServiceNote }}</p>
			@endif
			{!! .DatePicker(components.DateTimePickerProps{Name: "day", ID: "agenda-day", Label: .Labels.T("field.day"), Value: .Day, Min: .MinDay, Max: .MaxDay, Required: true}) !!}
			@if(.Subject.Type != "")
				<input type="hidden" name="subject_type" value="{{ .Subject.Type }}">
			@endif
			@if(.Subject.ID != "")
				<input type="hidden" name="subject_id" value="{{ .Subject.ID }}">
			@endif
			@if(.Subject.Label != "")
				<input type="hidden" name="subject_label" value="{{ .Subject.Label }}">
			@endif
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.see_times"), Variant: "outline", Type: "submit"}) !!}</div>
		</form>

		@if(.Searched)
			<form class="card mt-6 grid gap-4 p-5" method="post" action="{{ .Action }}">
				@csrf
				<h2 class="text-base font-semibold">{{ .Labels.T("screen.times_title") }}</h2>
				@if(.HasErrors())
					<ul class="text-destructive grid gap-1 text-sm" role="alert">
						@foreach(.All() as line)
							<li>{{ line }}</li>
						@endforeach
					</ul>
				@endif
				<input type="hidden" name="service" value="{{ .ServiceKey }}">
				@if(.Subject.Type != "")
					<input type="hidden" name="subject_type" value="{{ .Subject.Type }}">
				@endif
				@if(.Subject.ID != "")
					<input type="hidden" name="subject_id" value="{{ .Subject.ID }}">
				@endif
				@if(.Subject.Label != "")
					<input type="hidden" name="subject_label" value="{{ .Subject.Label }}">
					<p class="text-muted-foreground text-sm">{{ .Labels.T("field.subject") }}: {{ .Subject.Label }}</p>
				@endif
				@if(len(.Slots) > 0)
					{!! components.RadioGroup(components.RadioGroupProps{ID: "agenda-start", Name: "start", Label: .Labels.T("field.time"), Options: .Slots, Required: true, Page: .Form()}) !!}
					{!! components.Textarea(components.TextareaProps{Name: "note", Label: .Labels.T("field.note"), Hint: .Labels.T("screen.note_hint"), Rows: 3, Page: .Form()}) !!}
					@if(.RequiresPayment)
						<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.payment_after") }}</p>
					@endif
					<div class="flex flex-wrap items-center gap-2">
						{!! components.Button(components.ButtonProps{Label: .Labels.T("control.book"), Type: "submit"}) !!}
						<a class="btn" data-variant="outline" href="{{ .BackURL }}">{{ .Labels.T("control.cancel") }}</a>
					</div>
				@else
					<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.no_times") }}</p>
				@endif
			</form>
		@else
			<p class="text-muted-foreground mt-4 text-sm">{{ .Labels.T("screen.pick_hint") }}</p>
		@endif
	@endif
</section>
@endsection
