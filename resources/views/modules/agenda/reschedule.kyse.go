//go:build kyse

package agenda

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// RescheduleData is what the handler hands this page.
type RescheduleData = agenda.ReschedulePageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-2xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>
	<header class="mt-3">
		<p class="text-muted-foreground text-sm">{{ .Booking.Code }} · {{ .Booking.Service }}</p>
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.reschedule_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.reschedule_lead") }} {{ .Booking.When }}.</p>
	</header>

	<form class="card mt-6 grid gap-4 p-5" method="get" action="{{ .FormURL }}">
		<h2 class="text-base font-semibold">{{ .Labels.T("screen.pick_day") }}</h2>
		{!! .DatePicker(components.DateTimePickerProps{Name: "day", ID: "agenda-day", Label: .Labels.T("field.day"), Value: .Day, Min: .MinDay, Max: .MaxDay, Required: true}) !!}
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
			@if(len(.Slots) > 0)
				{!! components.RadioGroup(components.RadioGroupProps{ID: "agenda-start", Name: "start", Label: .Labels.T("field.time"), Options: .Slots, Required: true, Page: .Form()}) !!}
				<div class="flex flex-wrap items-center gap-2">
					{!! components.Button(components.ButtonProps{Label: .Labels.T("control.reschedule"), Type: "submit"}) !!}
					<a class="btn" data-variant="outline" href="{{ .BackURL }}">{{ .Labels.T("control.cancel") }}</a>
				</div>
			@else
				<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.no_times") }}</p>
			@endif
		</form>
	@else
		<p class="text-muted-foreground mt-4 text-sm">{{ .Labels.T("screen.pick_hint") }}</p>
	@endif
</section>
@endsection
